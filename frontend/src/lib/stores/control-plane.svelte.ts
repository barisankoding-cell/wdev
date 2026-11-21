import { api, ApiError } from '#lib/api/client.js';
import type { ServiceConfigKey, SystemStats, TaskEvent, TaskRecord } from '#lib/types/api.js';
import { toast } from 'svelte-sonner';

export interface TerminalLogEntry {
	id: number;
	type: TaskEvent['type'];
	message: string;
	status?: string;
	timestamp: string;
}

export interface ConfirmOptions {
	title?: string;
	description: string;
	confirmText?: string;
	cancelText?: string;
	destructive?: boolean;
}

export const COMPONENT_TITLES: Record<string, string> = {
	rust: 'Rust Toolchain, Mold Linker & Sccache',
	go: 'Golang (Go Language Toolchain)',
	node: 'Node.js LTS & NPM',
	java: 'OpenJDK Java JDK',
	flutter: 'Flutter SDK Channel Stable',
	android: 'Android SDK & CLI Tools',
	redis: 'Redis / Valkey Cache Server',
	base: 'Dependensi Dasar Sistem',
	update: 'Perbarui Seluruh Toolchain & Paket Sistem',
	'full-install': 'Instalasi Lengkap (Full Auto Setup)',
	'check-updates': 'Pemeriksaan Ketersediaan Pembaruan',
	'self-check': 'Validasi & Self-Check Sistem',
	postgres: 'PostgreSQL Database Server',
	nginx: 'Nginx Web Server',
	php: 'PHP & PHP-FPM',
	composer: 'Composer Package Manager',
	ssl: 'Local SSL (mkcert)',
	'php-stack': 'Full PHP Web Stack',
	mail: 'Mail Server Stack (Stalwart + SnappyMail)',
	'mail-stack': 'Mail Server Stack (Stalwart + SnappyMail)',
	cloudflare: 'Cloudflare Tunnel (cloudflared)',
	netbird: 'NetBird Mesh VPN',
	pdns: 'PowerDNS Authoritative Server',
	powerdns: 'PowerDNS Authoritative Server',
	zsh: 'Oh My Zsh Shell',
	'zsh-p10k': 'Oh My Zsh (Tema Powerlevel10k)',
	'zsh-agnoster': 'Oh My Zsh (Tema Agnoster)',
	'zsh-robbyrussell': 'Oh My Zsh (Tema Robbyrussell)',
	orca: 'Orca ADE Desktop GUI',
	'orca-desktop': 'Orca ADE Desktop GUI',
	'orca-headless': 'Orca ADE Headless Server',
	'orca-update': 'Pembaruan Orca ADE',
	'orca-switch-desktop': 'Alih Mode Orca ADE ke Desktop GUI',
	'orca-switch-headless': 'Alih Mode Orca ADE ke Headless Server',
	'orca-uninstall': 'Uninstall Orca ADE',
	'orca-pair': 'Info Pairing Orca ADE',
	'orca-ai': 'Konfigurasi Orca Commit AI',
	audit: 'Audit Duplikat Path & Toolchain',
	'audit-fix': 'Perbaikan Duplikat Path & Toolchain',
	'sync-hosts': 'Sinkronisasi Hosts & SSL Lokal',
	'cf-doctor': 'Diagnostik Cloudflare Tunnel'
};

const TERMINAL_STORAGE_KEY = 'wdev:terminal:v1';
const MAX_TERMINAL_LOGS = 1500;

interface PersistedTerminalState {
	instanceId: string;
	lastSeq: number;
	clearedSeq: number;
	currentTaskName: string;
	lastTaskStatus: 'idle' | 'running' | 'success' | 'failed';
	logs: TerminalLogEntry[];
}

class ControlPlaneStore {
	systemStatus = $state<SystemStats | null>(null);
	statusLoading = $state(false);
	statusError = $state<string | null>(null);

	// SSE & Live Terminal state
	terminalOpen = $state(false);
	sseConnected = $state(false);
	isTaskRunning = $state(false);
	currentTaskName = $state('wdev-runner');
	lastTaskStatus = $state<'idle' | 'running' | 'success' | 'failed'>('idle');
	terminalLogs = $state<TerminalLogEntry[]>([]);
	recentTasks = $state<TaskRecord[]>([]);

	// Sudo modal state
	sudoOpen = $state(false);
	sudoMessage = $state('');
	sudoError = $state('');
	private sudoResolve: ((password: string | null) => void) | null = null;

	// Confirm AlertDialog state
	confirmOpen = $state(false);
	confirmConfig = $state<ConfirmOptions>({
		title: 'Konfirmasi Tindakan',
		description: '',
		confirmText: 'Lanjutkan',
		cancelText: 'Batal',
		destructive: false
	});
	private confirmResolve: ((confirmed: boolean) => void) | null = null;

	// Service Config Dialog state
	serviceConfigOpen = $state(false);
	serviceConfigTarget = $state<ServiceConfigKey>('redis');
	serviceConfigVersion = $state('');

	// Refresh listener signal incremented whenever SSE task finishes
	refreshTick = $state(0);

	private eventSource: EventSource | null = null;
	private logSeq = 1;
	private instanceId = '';
	private lastSeq = 0;
	private clearedSeq = 0;
	private pollTimer: ReturnType<typeof setInterval> | null = null;
	private isPrivilegedPending = false;

	constructor() {
		this.restoreTerminalState();
	}

	private restoreTerminalState() {
		if (typeof window === 'undefined') return;
		try {
			const raw = window.localStorage.getItem(TERMINAL_STORAGE_KEY);
			if (!raw) return;
			const parsed = JSON.parse(raw) as Partial<PersistedTerminalState>;
			if (typeof parsed.instanceId === 'string') this.instanceId = parsed.instanceId;
			if (typeof parsed.lastSeq === 'number' && parsed.lastSeq >= 0) this.lastSeq = parsed.lastSeq;
			if (typeof parsed.clearedSeq === 'number' && parsed.clearedSeq >= 0) {
				this.clearedSeq = parsed.clearedSeq;
			}
			if (typeof parsed.currentTaskName === 'string' && parsed.currentTaskName) {
				this.currentTaskName = parsed.currentTaskName;
			}
			if (
				parsed.lastTaskStatus === 'idle' ||
				parsed.lastTaskStatus === 'running' ||
				parsed.lastTaskStatus === 'success' ||
				parsed.lastTaskStatus === 'failed'
			) {
				this.lastTaskStatus = parsed.lastTaskStatus;
			}
			if (Array.isArray(parsed.logs)) {
				const validLogs = parsed.logs
					.filter(
						(entry): entry is TerminalLogEntry =>
							Boolean(entry) && typeof entry.message === 'string' && typeof entry.type === 'string'
					)
					.slice(-MAX_TERMINAL_LOGS);
				this.terminalLogs = validLogs;
				let maxId = 0;
				for (const item of validLogs) {
					if (typeof item.id === 'number' && item.id > maxId) maxId = item.id;
				}
				this.logSeq = maxId + 1;
			}
		} catch {
			// Ignore malformed persisted terminal state
		}
	}

	private saveTerminalState() {
		if (typeof window === 'undefined') return;
		try {
			const payload: PersistedTerminalState = {
				instanceId: this.instanceId,
				lastSeq: this.lastSeq,
				clearedSeq: this.clearedSeq,
				currentTaskName: this.currentTaskName,
				lastTaskStatus: this.lastTaskStatus,
				logs: this.terminalLogs.slice(-MAX_TERMINAL_LOGS)
			};
			window.localStorage.setItem(TERMINAL_STORAGE_KEY, JSON.stringify(payload));
		} catch {
			// Ignore storage quota / privacy mode errors
		}
	}

	private appendTerminalLog(entry: Omit<TerminalLogEntry, 'id'>) {
		const next = [...this.terminalLogs, { ...entry, id: this.logSeq++ }];
		this.terminalLogs = next.length > MAX_TERMINAL_LOGS ? next.slice(-MAX_TERMINAL_LOGS) : next;
	}

	async loadStatus(refreshUpdates = false) {
		this.statusLoading = true;
		try {
			const data = await api.getStatus(refreshUpdates);
			this.systemStatus = data;
			this.statusError = null;
		} catch (err) {
			this.statusError = err instanceof Error ? err.message : 'Gagal memuat status sistem';
		} finally {
			this.statusLoading = false;
		}
	}

	async loadTasks() {
		try {
			const res = await api.getTasks(30);
			this.recentTasks = res.tasks ?? [];
			if (res.active_task) {
				this.isTaskRunning = true;
				this.lastTaskStatus = 'running';
				this.currentTaskName = res.active_task.command;
			} else {
				this.isTaskRunning = false;
				if (this.lastTaskStatus === 'running') {
					const latest = this.recentTasks[0];
					if (latest?.status === 'success' || latest?.status === 'failed') {
						this.lastTaskStatus = latest.status;
					} else {
						this.lastTaskStatus = 'idle';
					}
					this.saveTerminalState();
				}
			}
		} catch {
			// Ignore background poll error
		}
	}

	startPollingAndSSE() {
		if (typeof window === 'undefined') return;
		this.loadStatus();
		this.loadTasks();
		if (!this.eventSource || this.eventSource.readyState === EventSource.CLOSED) {
			this.connectSSE();
		}

		if (!this.pollTimer) {
			this.pollTimer = setInterval(() => {
				if (document.visibilityState === 'visible') {
					this.loadStatus(false);
				}
			}, 5000);
		}
	}

	stopPollingAndSSE() {
		if (this.pollTimer) {
			clearInterval(this.pollTimer);
			this.pollTimer = null;
		}
		if (this.eventSource) {
			this.eventSource.close();
			this.eventSource = null;
			this.sseConnected = false;
		}
	}

	private connectSSE() {
		if (this.eventSource) {
			this.eventSource.close();
		}
		const params = new URLSearchParams();
		if (this.lastSeq > 0) params.set('last_seq', String(this.lastSeq));
		if (this.instanceId) params.set('instance_id', this.instanceId);
		const qs = params.toString();
		const es = new EventSource(qs ? `/api/tasks/events?${qs}` : '/api/tasks/events');
		this.eventSource = es;

		es.onopen = () => {
			this.sseConnected = true;
		};

		es.onmessage = (event) => {
			try {
				const data = JSON.parse(event.data) as TaskEvent;
				if (data.type === 'ping') {
					this.sseConnected = true;
					if (data.instance_id && data.instance_id !== this.instanceId) {
						this.instanceId = data.instance_id;
						this.lastSeq = 0;
						this.clearedSeq = 0;
						this.saveTerminalState();
					}
					return;
				}
				this.handleTaskEvent(data);
			} catch {
				// Ignore malformed event line
			}
		};

		es.onerror = () => {
			this.sseConnected = false;
		};
	}

	private handleTaskEvent(evt: TaskEvent) {
		if (typeof evt.seq === 'number' && evt.seq > 0) {
			if (evt.seq <= this.clearedSeq || evt.seq <= this.lastSeq) {
				return;
			}
			this.lastSeq = evt.seq;
		}

		const timeStr = evt.timestamp
			? new Date(evt.timestamp).toLocaleTimeString('id-ID')
			: new Date().toLocaleTimeString('id-ID');

		if (evt.type === 'start') {
			this.isTaskRunning = true;
			this.lastTaskStatus = 'running';
			const cleanTask = (evt.message || '').replace(/^Memulai:\s*/, '').trim();
			if (cleanTask) {
				this.currentTaskName = cleanTask;
			}
			if (!evt.replay) {
				this.terminalOpen = true;
			}
			this.appendTerminalLog({
				type: 'start',
				message: `>>> [${timeStr}] ${evt.message}`,
				timestamp: timeStr
			});
			this.saveTerminalState();
		} else if (evt.type === 'finish') {
			this.isTaskRunning = false;
			const ok = evt.status === 'success';
			this.lastTaskStatus = ok ? 'success' : 'failed';
			this.appendTerminalLog({
				type: 'finish',
				status: evt.status,
				message: `<<< [${timeStr}] ${evt.message}`,
				timestamp: timeStr
			});
			this.saveTerminalState();
			if (!evt.replay) {
				if (ok) {
					toast.success(`Tugas selesai: ${this.currentTaskName}`);
				} else {
					toast.error(`Tugas gagal: ${this.currentTaskName}`);
				}
			}
			setTimeout(() => {
				this.loadStatus(false);
				this.loadTasks();
				this.refreshTick += 1;
			}, 800);
		} else {
			if (!evt.message || evt.message.startsWith(': heartbeat')) return;
			this.appendTerminalLog({
				type: 'log',
				message: evt.message,
				timestamp: timeStr
			});
			this.saveTerminalState();
		}
	}

	openTerminal(taskName?: string) {
		if (taskName) {
			this.currentTaskName = taskName;
		}
		this.terminalOpen = true;
	}

	clearTerminal() {
		this.terminalLogs = [];
		this.clearedSeq = this.lastSeq;
		if (!this.isTaskRunning) {
			this.lastTaskStatus = 'idle';
		}
		this.saveTerminalState();
	}

	confirm(opts: ConfirmOptions): Promise<boolean> {
		if (this.confirmResolve) {
			this.confirmResolve(false);
			this.confirmResolve = null;
		}
		this.confirmConfig = {
			title: opts.title ?? 'Konfirmasi Tindakan',
			description: opts.description,
			confirmText: opts.confirmText ?? 'Lanjutkan',
			cancelText: opts.cancelText ?? 'Batal',
			destructive: opts.destructive ?? false
		};
		this.confirmOpen = true;
		return new Promise((resolve) => {
			this.confirmResolve = resolve;
		});
	}

	resolveConfirm(confirmed: boolean) {
		this.confirmOpen = false;
		const resolver = this.confirmResolve;
		this.confirmResolve = null;
		resolver?.(confirmed);
	}

	requestSudo(message: string, error = ''): Promise<string | null> {
		if (this.sudoResolve) {
			this.sudoResolve(null);
			this.sudoResolve = null;
		}
		this.sudoMessage = message;
		this.sudoError = error;
		this.sudoOpen = true;
		return new Promise((resolve) => {
			this.sudoResolve = resolve;
		});
	}

	resolveSudo(password: string | null) {
		this.sudoOpen = false;
		this.sudoError = '';
		const resolver = this.sudoResolve;
		this.sudoResolve = null;
		resolver?.(password);
	}

	openServiceConfig(service: ServiceConfigKey, version = '') {
		this.serviceConfigTarget = service;
		this.serviceConfigVersion = version;
		this.serviceConfigOpen = true;
	}

	async runWithSudoRetry<T>(
		sudoMessage: string,
		execFn: (sudoPass?: string) => Promise<T & { status?: string; message?: string }>
	): Promise<T | null> {
		let sudoPass: string | undefined = undefined;
		let errHint = '';

		while (true) {
			try {
				const res = await execFn(sudoPass);
				if (res && res.status === 'sudo_required') {
					const entered = await this.requestSudo(res.message || sudoMessage, errHint);
					if (!entered) return null;
					sudoPass = entered;
					errHint = '';
					continue;
				}
				return res;
			} catch (err) {
				if (err instanceof ApiError && err.status === 401) {
					errHint = 'Password sudo tidak valid. Silakan coba lagi.';
					const entered = await this.requestSudo(sudoMessage, errHint);
					if (!entered) return null;
					sudoPass = entered;
					continue;
				}
				throw err;
			}
		}
	}

	private async runInstallWithConfirm(
		terminalLabel: string,
		confirmDescription: string,
		invoke: (sudoPass?: string) => Promise<{ status: string; task_id?: number; message?: string }>
	) {
		if (this.isPrivilegedPending) return;
		this.isPrivilegedPending = true;
		try {
			const ok = await this.confirm({
				title: 'Konfirmasi Instalasi / Pembaruan',
				description: confirmDescription,
				confirmText: 'Lanjutkan'
			});
			if (!ok) return;

			const res = await this.runWithSudoRetry(
				'Operasi pemeliharaan/instalasi ini memerlukan hak akses administrator (sudo).',
				invoke
			);
			if (res) {
				this.openTerminal(terminalLabel);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai tugas instalasi');
		} finally {
			this.isPrivilegedPending = false;
		}
	}

	async triggerInstall(component: string, version?: string) {
		const friendly = COMPONENT_TITLES[component] ?? component;
		const label = version ? `${friendly} (${version})` : friendly;
		await this.runInstallWithConfirm(
			label,
			`Jalankan operasi instalasi / pembaruan untuk: ${label}?`,
			(sudoPass) => api.triggerInstall(component, version, sudoPass)
		);
	}

	async triggerBatchInstall(
		label: string,
		items: { component: string; version?: string }[]
	) {
		if (items.length === 0) {
			toast.error('Pilih minimal satu komponen untuk dipasang.');
			return;
		}
		const summary = items
			.map((it) => {
				const friendly = COMPONENT_TITLES[it.component] ?? it.component;
				return it.version ? `${friendly} (${it.version})` : friendly;
			})
			.join(', ');
		await this.runInstallWithConfirm(
			label,
			`Jalankan ${label} secara berurutan (${items.length} komponen): ${summary}?`,
			(sudoPass) => api.triggerBatchInstall(items, label, sudoPass)
		);
	}

	async switchRuntime(type: string, version: string) {
		if (this.isPrivilegedPending) return;
		this.isPrivilegedPending = true;
		const labels: Record<string, string> = {
			node: 'Node.js',
			php: 'PHP',
			pg: 'PostgreSQL',
			java: 'Java OpenJDK'
		};
		const label = labels[type] ?? type.toUpperCase();

		const confirmDescription =
			type === 'pg'
				? `Beralih ke PostgreSQL ${version}? Seluruh role dan database dari versi aktif akan dibackup dan dimigrasikan secara otomatis (cluster lama tetap disimpan aman).`
				: `Beralih versi runtime ${label} ke versi ${version}?`;

		try {
			const ok = await this.confirm({
				title: 'Konfirmasi Pengalihan Runtime',
				description: confirmDescription,
				confirmText: 'Beralih Sekarang'
			});
			if (!ok) return;

			const res = await this.runWithSudoRetry(
				`Pengalihan runtime ${label} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.switchRuntime(type, version, sudoPass)
			);
			if (res) {
				this.openTerminal(`Switch ${label} -> ${version}`);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal beralih runtime');
		} finally {
			this.isPrivilegedPending = false;
		}
	}

	async syncToolchainEnv() {
		if (this.isPrivilegedPending) return;
		this.isPrivilegedPending = true;
		try {
			const ok = await this.confirm({
				title: 'Sinkronkan Environment PATH',
				description:
					'Tulis ulang /etc/profile.d/wdev.sh, bersihkan profile duplikat, dan selaraskan symlink /usr/local/bin ke lokasi kanonik wdev?',
				confirmText: 'Sinkronkan Sekarang'
			});
			if (!ok) return;

			const res = await this.runWithSudoRetry(
				'Sinkronisasi environment toolchain memerlukan hak akses administrator (sudo).',
				(sudoPass) => api.syncEnv(sudoPass)
			);
			if (res) {
				this.openTerminal('Sinkronisasi Environment Toolchain (PATH)');
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menyinkronkan environment');
		} finally {
			this.isPrivilegedPending = false;
		}
	}
}

export const cp = new ControlPlaneStore();
