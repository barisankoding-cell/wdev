<script lang="ts">
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { FitAddon } from '@xterm/addon-fit';
	import { WebLinksAddon } from '@xterm/addon-web-links';
	import { Terminal } from '@xterm/xterm';
	import '@xterm/xterm/css/xterm.css';
	import { onMount, tick } from 'svelte';
	import { toast } from 'svelte-sonner';

	export type TerminalPaneStatus = 'connecting' | 'connected' | 'exited' | 'error';

	export interface TerminalPaneMeta {
		status: TerminalPaneStatus;
		sessionId?: string;
		user?: string;
		isRoot?: boolean;
		host?: string;
		shell?: string;
		cwd?: string;
		idleTimeoutSeconds?: number;
		exitCode?: number;
		error?: string;
	}

	let {
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

	let hostEl = $state<HTMLDivElement | null>(null);
	let term: Terminal | null = null;
	let fitAddon: FitAddon | null = null;
	let socket: WebSocket | null = null;
	let resizeObserver: ResizeObserver | null = null;
	let disposed = false;
	let fitTimer: ReturnType<typeof setTimeout> | null = null;
	const encoder = new TextEncoder();

	let currentMeta: TerminalPaneMeta = {
		status: 'connecting'
	};

	function emitMeta(patch: Partial<TerminalPaneMeta>) {
		currentMeta = { ...currentMeta, ...patch };
		onMeta?.(currentMeta);
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

	let activeToken = '';

	function connectWS(overrideToken?: string) {
		if (disposed || !term) return;
		closeSocket();

		fitNow();
		emitMeta({ status: 'connecting', error: undefined, exitCode: undefined });

		const useToken = overrideToken !== undefined ? overrideToken : activeToken || token;
		if (overrideToken !== undefined) {
			activeToken = ''; // token peluncuran satu kali pakai
		}

		const wsUrl = api.terminalWsUrl({
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

		ws.onopen = () => {
			if (socket !== ws || disposed) return;
			wasOpened = true;
			fitNow();
			if (term) {
				sendResize(term.cols, term.rows);
			}
			emitMeta({ status: 'connected' });
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
						session_id?: string;
						cwd?: string;
						shell?: string;
						user?: string;
						is_root?: boolean;
						host?: string;
						idle_timeout_seconds?: number;
						code?: number;
						message?: string;
					};
					if (msg.type === 'ready') {
						emitMeta({
							status: 'connected',
							sessionId: msg.session_id,
							cwd: msg.cwd || currentMeta.cwd,
							shell: msg.shell || currentMeta.shell,
							user: msg.user,
							isRoot: Boolean(msg.is_root),
							host: msg.host,
							idleTimeoutSeconds: msg.idle_timeout_seconds
						});
					} else if (msg.type === 'exit') {
						const code = typeof msg.code === 'number' ? msg.code : 0;
						emitMeta({ status: 'exited', exitCode: code });
						term.writeln(`\r\n\x1b[90m${t('terminal.pane.exitLine', { code })}\x1b[0m`);
					} else if (msg.type === 'error') {
						emitMeta({ status: 'error', error: msg.message || t('terminal.pane.ptyOpenFailed') });
						term.writeln(`\r\n\x1b[31m[Error: ${msg.message || t('terminal.pane.ptySessionFailed')}]\x1b[0m`);
					}
				} catch {
					// Abaikan frame JSON yang tidak dikenal
				}
			}
		};

		ws.onerror = () => {
			if (socket !== ws || disposed) return;
			emitMeta({
				status: 'error',
				error: t('terminal.pane.wsFailed')
			});
		};

		ws.onclose = (ev) => {
			if (socket !== ws || disposed) return;
			socket = null;
			if (!wasOpened) {
				emitMeta({
					status: 'error',
					error: t('terminal.pane.authRequired')
				});
				onUnlockRequired?.();
				return;
			}
			if (currentMeta.status === 'connected' || currentMeta.status === 'connecting') {
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

	export function reconnect(overrideToken?: string) {
		if (!term) return;
		term.writeln(`\r\n\x1b[36m${t('terminal.pane.reconnectingLine')}\x1b[0m`);
		connectWS(overrideToken);
	}
</script>

<div
	bind:this={hostEl}
	class="h-full w-full overflow-hidden rounded-b-lg bg-[#0b101b] p-2"
	role="application"
	aria-label={t('terminal.pane.ariaLabel')}
></div>
