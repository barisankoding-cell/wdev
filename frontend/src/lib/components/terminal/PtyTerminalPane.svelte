<script lang="ts">
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { FitAddon } from '@xterm/addon-fit';
	import { WebLinksAddon } from '@xterm/addon-web-links';
	import { Terminal } from '@xterm/xterm';
	import '@xterm/xterm/css/xterm.css';
	import { onMount, tick } from 'svelte';
	import { toast } from 'svelte-sonner';

	export type TerminalPaneStatus =
		| 'connecting'
		| 'connected'
		| 'reconnecting'
		| 'exited'
		| 'idle_timeout'
		| 'error';

	export interface TerminalPaneMeta {
		status: TerminalPaneStatus;
		sessionId?: string;
		user?: string;
		isRoot?: boolean;
		host?: string;
		shell?: string;
		cwd?: string;
		keepAlive?: boolean;
		idleTimeoutSeconds?: number;
		detachTimeoutSeconds?: number;
		idleWarningSeconds?: number;
		exitCode?: number;
		closeReason?: string;
		error?: string;
	}

	let {
		sessionId = '',
		cwd = '',
		shell = '',
		user = '',
		token = '',
		composeProject = '',
		composeService = '',
		fontSize = 14,
		active = true,
		onMeta,
		onUnlockRequired
	}: {
		sessionId?: string;
		cwd?: string;
		shell?: string;
		user?: string;
		token?: string;
		composeProject?: string;
		composeService?: string;
		fontSize?: number;
		active?: boolean;
		onMeta?: (meta: TerminalPaneMeta) => void;
		onUnlockRequired?: () => void;
	} = $props();

	const MAX_AUTO_RETRIES = 3;

	let hostEl = $state<HTMLDivElement | null>(null);
	let term: Terminal | null = null;
	let fitAddon: FitAddon | null = null;
	let socket: WebSocket | null = null;
	let resizeObserver: ResizeObserver | null = null;
	let disposed = false;
	let fitTimer: ReturnType<typeof setTimeout> | null = null;
	let retryTimer: ReturnType<typeof setTimeout> | null = null;
	let idleCountdownTimer: ReturnType<typeof setInterval> | null = null;
	const encoder = new TextEncoder();

	let activeSessionId = '';
	let singleUseToken = '';
	let autoRetryCount = 0;
	let wasReplaced = false;
	let explicitClose = false;

	let currentMeta: TerminalPaneMeta = {
		status: 'connecting'
	};

	function emitMeta(patch: Partial<TerminalPaneMeta>) {
		currentMeta = { ...currentMeta, ...patch };
		onMeta?.(currentMeta);
	}

	function stopIdleCountdown() {
		if (idleCountdownTimer) {
			clearInterval(idleCountdownTimer);
			idleCountdownTimer = null;
		}
	}

	function startIdleCountdown(initialSeconds: number) {
		stopIdleCountdown();
		let remaining = Math.max(1, Math.round(initialSeconds));
		emitMeta({ idleWarningSeconds: remaining });
		idleCountdownTimer = setInterval(() => {
			if (disposed) {
				stopIdleCountdown();
				return;
			}
			remaining = Math.max(0, remaining - 1);
			emitMeta({ idleWarningSeconds: remaining > 0 ? remaining : undefined });
			if (remaining <= 0) {
				stopIdleCountdown();
			}
		}, 1000);
	}

	function sendResize(cols: number, rows: number) {
		if (!socket || socket.readyState !== WebSocket.OPEN) return;
		socket.send(JSON.stringify({ type: 'resize', cols, rows }));
	}

	function scheduleFit() {
		if (fitTimer) clearTimeout(fitTimer);
		fitTimer = setTimeout(() => {
			fitNow();
		}, 40);
	}

	function fitNow() {
		if (!term || !fitAddon || !hostEl) return;
		if (hostEl.offsetParent === null || hostEl.clientWidth < 40 || hostEl.clientHeight < 40) {
			return;
		}
		try {
			fitAddon.fit();
			sendResize(term.cols, term.rows);
		} catch {
			// Abaikan pengukuran sementara saat transisi DOM berlangsung.
		}
	}

	function closeSocket() {
		if (retryTimer) {
			clearTimeout(retryTimer);
			retryTimer = null;
		}
		stopIdleCountdown();
		if (socket) {
			const ws = socket;
			socket = null;
			ws.onopen = null;
			ws.onmessage = null;
			ws.onerror = null;
			ws.onclose = null;
			if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
				ws.close(1000, 'closed by client');
			}
		}
	}

	function connectWS(overrideToken?: string, options?: { forceFresh?: boolean }) {
		if (disposed || !term) return;
		closeSocket();

		if (options?.forceFresh) {
			activeSessionId = '';
			emitMeta({ sessionId: undefined });
		}

		fitNow();
		const isReattach = Boolean(activeSessionId);
		emitMeta({
			status: isReattach && autoRetryCount > 0 ? 'reconnecting' : 'connecting',
			error: undefined,
			exitCode: undefined,
			closeReason: undefined,
			idleWarningSeconds: undefined
		});

		let useToken = '';
		if (overrideToken !== undefined) {
			useToken = overrideToken;
		} else if (singleUseToken) {
			useToken = singleUseToken;
			singleUseToken = '';
		}

		const wsUrl = api.terminalWsUrl({
			session_id: activeSessionId || undefined,
			cwd: currentMeta.cwd || cwd,
			shell: shell || undefined,
			user: user || undefined,
			token: useToken || undefined,
			compose_project: composeProject || undefined,
			compose_service: composeService || undefined,
			cols: term.cols,
			rows: term.rows
		});

		const ws = new WebSocket(wsUrl);
		ws.binaryType = 'arraybuffer';
		socket = ws;
		let wasOpened = false;
		wasReplaced = false;

		ws.onopen = () => {
			if (socket !== ws || disposed) return;
			wasOpened = true;
			fitNow();
			if (term) {
				sendResize(term.cols, term.rows);
			}
			emitMeta({ status: 'connected', error: undefined });
			term?.focus();
		};

		ws.onmessage = (ev) => {
			if (socket !== ws || disposed || !term) return;
			if (ev.data instanceof ArrayBuffer) {
				term.write(new Uint8Array(ev.data));
				return;
			}
			if (typeof ev.data === 'string') {
				try {
					const msg = JSON.parse(ev.data) as {
						type?: string;
						code?: string | number;
						reason?: string;
						session_id?: string;
						reattached?: boolean;
						cwd?: string;
						shell?: string;
						user?: string;
						is_root?: boolean;
						host?: string;
						keep_alive?: boolean;
						enabled?: boolean;
						idle_timeout_seconds?: number;
						detach_timeout_seconds?: number;
						remaining_seconds?: number;
						message?: string;
					};
					if (msg.type === 'ready') {
						const wasRetry = autoRetryCount > 0;
						autoRetryCount = 0;
						if (msg.reattached) {
							term.reset();
						}
						activeSessionId = msg.session_id || activeSessionId;
						emitMeta({
							status: 'connected',
							sessionId: activeSessionId || undefined,
							cwd: msg.cwd || currentMeta.cwd,
							shell: msg.shell || currentMeta.shell,
							user: msg.user,
							isRoot: Boolean(msg.is_root),
							host: msg.host,
							keepAlive: Boolean(msg.keep_alive),
							idleTimeoutSeconds: msg.idle_timeout_seconds,
							detachTimeoutSeconds: msg.detach_timeout_seconds,
							idleWarningSeconds: undefined,
							closeReason: undefined,
							error: undefined,
							exitCode: undefined
						});
						if (msg.reattached && wasRetry) {
							toast.success(t('terminal.toast.reattached'));
						}
					} else if (msg.type === 'idle_warning') {
						startIdleCountdown(typeof msg.remaining_seconds === 'number' ? msg.remaining_seconds : 60);
					} else if (msg.type === 'idle_extended') {
						stopIdleCountdown();
						emitMeta({
							idleWarningSeconds: undefined,
							idleTimeoutSeconds: msg.idle_timeout_seconds ?? currentMeta.idleTimeoutSeconds
						});
					} else if (msg.type === 'keepalive') {
						const enabled = Boolean(msg.enabled);
						if (enabled) {
							stopIdleCountdown();
						}
						emitMeta({
							keepAlive: enabled,
							...(enabled ? { idleWarningSeconds: undefined } : {})
						});
					} else if (msg.type === 'replaced') {
						wasReplaced = true;
						stopIdleCountdown();
						emitMeta({
							status: 'error',
							error: msg.message || t('terminal.pane.replacedLine')
						});
						term.writeln(`\r\n\x1b[33m${t('terminal.pane.replacedLine')}\x1b[0m`);
					} else if (msg.type === 'exit') {
						stopIdleCountdown();
						activeSessionId = '';
						const code = typeof msg.code === 'number' ? msg.code : 0;
						const isIdle = msg.reason === 'idle_timeout' || currentMeta.status === 'idle_timeout';
						emitMeta({
							status: isIdle ? 'idle_timeout' : 'exited',
							sessionId: undefined,
							exitCode: code,
							closeReason: msg.reason || currentMeta.closeReason,
							idleWarningSeconds: undefined
						});
						if (!isIdle) {
							term.writeln(`\r\n\x1b[90m${t('terminal.pane.exitLine', { code })}\x1b[0m`);
						}
					} else if (msg.type === 'error') {
						stopIdleCountdown();
						if (msg.code === 'idle_timeout') {
							activeSessionId = '';
							emitMeta({
								status: 'idle_timeout',
								sessionId: undefined,
								closeReason: 'idle_timeout',
								idleWarningSeconds: undefined,
								error: msg.message || t('terminal.idleClosedBanner')
							});
							term.writeln(
								`\r\n\x1b[33m[Idle Timeout: ${msg.message || t('terminal.idleClosedBanner')}]\x1b[0m`
							);
						} else if (msg.code === 'session_not_found') {
							activeSessionId = '';
							emitMeta({
								status: 'error',
								sessionId: undefined,
								error: msg.message || t('terminal.pane.sessionNotFoundLine')
							});
							term.writeln(`\r\n\x1b[33m${t('terminal.pane.sessionNotFoundLine')}\x1b[0m`);
						} else {
							emitMeta({ status: 'error', error: msg.message || t('terminal.pane.ptyOpenFailed') });
							term.writeln(
								`\r\n\x1b[31m[Error: ${msg.message || t('terminal.pane.ptySessionFailed')}]\x1b[0m`
							);
						}
					}
				} catch {
					// Abaikan frame JSON yang tidak dikenal
				}
			}
		};

		ws.onerror = () => {
			if (socket !== ws || disposed) return;
			if (!wasOpened) {
				emitMeta({
					status: 'error',
					error: t('terminal.pane.wsFailed')
				});
			}
		};

		ws.onclose = (ev) => {
			if (socket !== ws || disposed) return;
			socket = null;
			stopIdleCountdown();

			if (wasReplaced) {
				return;
			}

			if (!wasOpened) {
				emitMeta({
					status: 'error',
					error: t('terminal.pane.authRequired')
				});
				onUnlockRequired?.();
				return;
			}

			if (
				explicitClose ||
				currentMeta.status === 'exited' ||
				currentMeta.status === 'idle_timeout'
			) {
				return;
			}

			// Jika terputus mendadak dan sesi backend masih ada, coba auto-reattach terlebih dahulu.
			if (!ev.wasClean && activeSessionId && autoRetryCount < MAX_AUTO_RETRIES) {
				autoRetryCount += 1;
				const attempt = autoRetryCount;
				const delayMs = attempt * 1200;
				emitMeta({ status: 'reconnecting', error: undefined });
				term?.writeln(
					`\r\n\x1b[36m${t('terminal.pane.autoReconnectingLine', { attempt, max: MAX_AUTO_RETRIES })}\x1b[0m`
				);
				retryTimer = setTimeout(() => {
					if (!disposed) {
						connectWS();
					}
				}, delayMs);
				return;
			}

			if (
				currentMeta.status === 'connected' ||
				currentMeta.status === 'connecting' ||
				currentMeta.status === 'reconnecting'
			) {
				emitMeta({
					status: ev.wasClean ? 'exited' : 'error',
					error: ev.wasClean ? undefined : t('terminal.pane.disconnectedError')
				});
				if (!ev.wasClean) {
					term?.writeln(`\r\n\x1b[33m${t('terminal.pane.disconnectedLine')}\x1b[0m`);
				}
			}
		};
	}

	onMount(() => {
		if (!hostEl) return;

		const xterm = new Terminal({
			cursorBlink: true,
			cursorStyle: 'block',
			fontSize,
			lineHeight: 1.25,
			fontFamily:
				'"JetBrains Mono", "Fira Code", "Cascadia Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
			scrollback: 5000,
			allowProposedApi: false,
			theme: {
				background: '#0b101b',
				foreground: '#e2e8f0',
				cursor: '#38bdf8',
				cursorAccent: '#0b101b',
				selectionBackground: 'rgba(56, 189, 248, 0.28)',
				black: '#1e293b',
				red: '#f87171',
				green: '#4ade80',
				yellow: '#facc15',
				blue: '#60a5fa',
				magenta: '#c084fc',
				cyan: '#22d3ee',
				white: '#f1f5f9',
				brightBlack: '#64748b',
				brightRed: '#fca5a5',
				brightGreen: '#86efac',
				brightYellow: '#fde047',
				brightBlue: '#93c5fd',
				brightMagenta: '#d8b4fe',
				brightCyan: '#67e8f9',
				brightWhite: '#ffffff'
			}
		});

		const fit = new FitAddon();
		const links = new WebLinksAddon((_event, uri) => {
			window.open(uri, '_blank', 'noopener,noreferrer');
		});
		xterm.loadAddon(fit);
		xterm.loadAddon(links);
		xterm.open(hostEl);

		xterm.attachCustomKeyEventHandler((ev) => {
			if (ev.type === 'keydown' && ev.ctrlKey && ev.shiftKey && ev.key.toUpperCase() === 'C') {
				const sel = xterm.getSelection();
				if (sel) {
					void navigator.clipboard?.writeText(sel);
					return false;
				}
			}
			return true;
		});

		const dataSub = xterm.onData((data) => {
			if (currentMeta.idleWarningSeconds !== undefined) {
				stopIdleCountdown();
				emitMeta({ idleWarningSeconds: undefined });
			}
			if (socket && socket.readyState === WebSocket.OPEN) {
				socket.send(encoder.encode(data));
			}
		});

		const binSub = xterm.onBinary((data) => {
			if (!socket || socket.readyState !== WebSocket.OPEN) return;
			const bytes = new Uint8Array(data.length);
			for (let i = 0; i < data.length; i++) {
				bytes[i] = data.charCodeAt(i) & 0xff;
			}
			socket.send(bytes);
		});

		const resizeSub = xterm.onResize(({ cols, rows }) => {
			sendResize(cols, rows);
		});

		term = xterm;
		fitAddon = fit;
		activeSessionId = sessionId || '';
		singleUseToken = token || '';
		currentMeta.sessionId = activeSessionId || undefined;

		resizeObserver = new ResizeObserver(() => {
			scheduleFit();
		});
		resizeObserver.observe(hostEl);

		connectWS();

		return () => {
			disposed = true;
			if (fitTimer) clearTimeout(fitTimer);
			resizeObserver?.disconnect();
			dataSub.dispose();
			binSub.dispose();
			resizeSub.dispose();
			closeSocket();
			xterm.dispose();
			term = null;
			fitAddon = null;
		};
	});

	$effect(() => {
		const nextSize = fontSize;
		if (term && term.options.fontSize !== nextSize) {
			term.options.fontSize = nextSize;
			scheduleFit();
		}
	});

	$effect(() => {
		if (active && term) {
			void tick().then(() => {
				fitNow();
				term?.focus();
			});
		}
	});

	export function focus() {
		term?.focus();
	}

	export function fit() {
		fitNow();
	}

	export function clear() {
		term?.clear();
		term?.focus();
	}

	export function sendText(text: string) {
		if (!socket || socket.readyState !== WebSocket.OPEN) {
			toast.error(t('terminal.pane.notConnected'));
			return;
		}
		if (currentMeta.idleWarningSeconds !== undefined) {
			stopIdleCountdown();
			emitMeta({ idleWarningSeconds: undefined });
		}
		socket.send(encoder.encode(text));
		term?.focus();
	}

	export async function copySelection() {
		const sel = term?.getSelection() ?? '';
		if (!sel) {
			toast.info(t('terminal.pane.selectTextFirst'));
			return;
		}
		try {
			await navigator.clipboard.writeText(sel);
			toast.success(t('terminal.pane.copied'));
		} catch {
			toast.error(t('terminal.pane.copyFailed'));
		}
		term?.focus();
	}

	export async function pasteClipboard() {
		try {
			const text = await navigator.clipboard.readText();
			if (text) {
				sendText(text);
			}
		} catch {
			toast.error(t('terminal.pane.pasteDenied'));
		}
	}

	export function extendIdle() {
		if (socket && socket.readyState === WebSocket.OPEN) {
			socket.send(JSON.stringify({ type: 'extend' }));
			stopIdleCountdown();
			emitMeta({ idleWarningSeconds: undefined });
			const mins = currentMeta.idleTimeoutSeconds
				? Math.max(1, Math.round(currentMeta.idleTimeoutSeconds / 60))
				: 15;
			toast.success(t('terminal.toast.extended', { minutes: mins }));
			term?.focus();
			return;
		}
		if (activeSessionId) {
			void api.extendTerminalSession(activeSessionId).then((res) => {
				stopIdleCountdown();
				emitMeta({ idleWarningSeconds: undefined, idleTimeoutSeconds: res.idle_timeout_seconds });
				const mins = Math.max(1, Math.round((res.idle_timeout_seconds || 900) / 60));
				toast.success(t('terminal.toast.extended', { minutes: mins }));
			});
		}
	}

	export function setKeepAlive(enabled: boolean) {
		if (socket && socket.readyState === WebSocket.OPEN) {
			socket.send(JSON.stringify({ type: 'keepalive', enabled }));
			if (enabled) stopIdleCountdown();
			emitMeta({
				keepAlive: enabled,
				...(enabled ? { idleWarningSeconds: undefined } : {})
			});
			toast.success(enabled ? t('terminal.toast.keepAliveEnabled') : t('terminal.toast.keepAliveDisabled'));
			term?.focus();
			return;
		}
		if (activeSessionId) {
			void api.setTerminalSessionKeepAlive(activeSessionId, enabled).then((res) => {
				if (res.keep_alive) stopIdleCountdown();
				emitMeta({
					keepAlive: res.keep_alive,
					...(res.keep_alive ? { idleWarningSeconds: undefined } : {})
				});
				toast.success(
					res.keep_alive ? t('terminal.toast.keepAliveEnabled') : t('terminal.toast.keepAliveDisabled')
				);
			});
		}
	}

	export function reconnect(overrideToken?: string, forceFresh = false) {
		if (!term) return;
		autoRetryCount = 0;
		explicitClose = false;
		if (forceFresh || !activeSessionId) {
			term.writeln(`\r\n\x1b[36m${t('terminal.pane.reconnectingLine')}\x1b[0m`);
		}
		connectWS(overrideToken, { forceFresh });
	}

	export function closeSession() {
		explicitClose = true;
		if (socket && socket.readyState === WebSocket.OPEN) {
			try {
				socket.send(JSON.stringify({ type: 'close' }));
			} catch {
				// Abaikan error pengiriman close frame
			}
		}
		closeSocket();
	}
</script>

<div
	bind:this={hostEl}
	class="h-full w-full overflow-hidden rounded-b-lg bg-[#0b101b] p-2"
	role="application"
	aria-label={t('terminal.pane.ariaLabel')}
></div>
