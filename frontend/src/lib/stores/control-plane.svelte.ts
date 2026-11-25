import { api, ApiError } from '#lib/api/client.js';
import { i18n, t } from '#lib/i18n/index.svelte.js';
import type {
	DockerInstallOptions,
	ServiceConfigKey,
	SystemStats,
	TaskEvent,
	TaskRecord
} from '#lib/types/api.js';
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

export function getComponentTitle(component: string): string {
	return i18n.tryTranslate(`stores.cp.components.${component}`) ?? component;
}

export const COMPONENT_TITLES: Record<string, string> = new Proxy(
	{},
	{
		get(_target, prop) {
			return typeof prop === 'string' ? getComponentTitle(prop) : undefined;
		}
	}
);

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
	pendingPanelDomainURL = $state('');
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
		title: '',
		description: '',
		confirmText: '',
		cancelText: '',
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
	private pendingRestartReload = false;
	private reloadTriggered = false;
	private reloadFallbackTimer: ReturnType<typeof setTimeout> | null = null;

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
			this.statusError = err instanceof Error ? err.message : t('stores.cp.loadStatusError');
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
		if (this.reloadFallbackTimer) {
			clearTimeout(this.reloadFallbackTimer);
			this.reloadFallbackTimer = null;
		}
		if (this.eventSource) {
			this.eventSource.close();
			this.eventSource = null;
			this.sseConnected = false;
		}
	}

	armSelfUpdateReload() {
		this.reloadTriggered = false;
		this.pendingRestartReload = true;
	}

	private cancelSelfUpdateReload() {
		this.pendingRestartReload = false;
		if (this.reloadFallbackTimer) {
			clearTimeout(this.reloadFallbackTimer);
			this.reloadFallbackTimer = null;
		}
	}

	private scheduleSelfUpdateFallbackReload(attempt = 0) {
		if (!this.pendingRestartReload || this.reloadTriggered || typeof window === 'undefined') return;
		if (this.reloadFallbackTimer) {
			clearTimeout(this.reloadFallbackTimer);
		}
		const delayMs = attempt === 0 ? 4500 : 2000;
		this.reloadFallbackTimer = setTimeout(async () => {
			if (!this.pendingRestartReload || this.reloadTriggered) return;
			try {
				await api.getStatus(false);
				this.triggerSelfUpdateReload();
			} catch {
				if (attempt < 5) {
					this.scheduleSelfUpdateFallbackReload(attempt + 1);
				}
			}
		}, delayMs);
	}

	private triggerSelfUpdateReload() {
		if (this.reloadTriggered || typeof window === 'undefined') return;
		this.reloadTriggered = true;
		this.cancelSelfUpdateReload();
		this.isTaskRunning = false;
		this.lastTaskStatus = 'success';
		this.saveTerminalState();
		toast.success(t('stores.cp.selfUpdateReloading'));
		setTimeout(() => {
			window.location.reload();
		}, 600);
	}

	private connectSSE() {
		if (this.eventSource) {
			this.eventSource.close();
		}
		const params = new URLSearchParams();
		if (this.lastSeq > 0) params.set('last_seq', String(this.lastSeq));
		if (this.instanceId) params.set('instance_id', this.instanceId);
		params.set('lang', i18n.locale);
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
						const hadInstance = this.instanceId !== '';
						this.instanceId = data.instance_id;
						this.lastSeq = 0;
						this.clearedSeq = 0;
						if (hadInstance && this.pendingRestartReload) {
							this.triggerSelfUpdateReload();
							return;
						}
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
			? i18n.formatTime(evt.timestamp)
			: i18n.formatTime();

		if (evt.type === 'start') {
			this.isTaskRunning = true;
			this.lastTaskStatus = 'running';
			const cleanTask = (evt.message || '').replace(/^(?:Memulai|Starting):\s*/i, '').trim();
			if (cleanTask) {
				this.currentTaskName = cleanTask;
				if (!evt.replay && !cleanTask.startsWith('Bind Domain Panel') && !cleanTask.startsWith('Bind Panel Domain')) {
					this.pendingPanelDomainURL = '';
				}
			}
			if (!evt.replay && (cleanTask === 'Pembaruan Biner wdev' || cleanTask === 'wdev Binary Update')) {
				this.armSelfUpdateReload();
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
			if (!evt.replay && !ok) {
				this.pendingPanelDomainURL = '';
			}
			this.appendTerminalLog({
				type: 'finish',
				status: evt.status,
				message: `<<< [${timeStr}] ${evt.message}`,
				timestamp: timeStr
			});
			this.saveTerminalState();
			if (!evt.replay) {
				if (this.pendingRestartReload) {
					if (ok) {
						toast.info(t('stores.cp.selfUpdateWaitingRestart'));
						this.scheduleSelfUpdateFallbackReload(0);
					} else {
						this.cancelSelfUpdateReload();
						toast.error(t('stores.cp.taskFailed', { name: this.currentTaskName }));
					}
				} else if (ok) {
					toast.success(t('stores.cp.taskSuccess', { name: this.currentTaskName }));
				} else {
					toast.error(t('stores.cp.taskFailed', { name: this.currentTaskName }));
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

	setPendingPanelDomainURL(url: string) {
		this.pendingPanelDomainURL = url;
		if (url && !this.isTaskRunning) {
			this.lastTaskStatus = 'running';
		}
	}

	clearPendingPanelDomainURL() {
		this.pendingPanelDomainURL = '';
	}

	clearTerminal() {
		this.terminalLogs = [];
		this.clearedSeq = this.lastSeq;
		this.pendingPanelDomainURL = '';
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
			title: opts.title ?? t('stores.cp.confirmDefaultTitle'),
			description: opts.description,
			confirmText: opts.confirmText ?? t('stores.cp.confirmDefaultContinue'),
			cancelText: opts.cancelText ?? t('stores.cp.confirmDefaultCancel'),
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
					errHint = t('stores.cp.sudoInvalidPassword');
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
				title: t('stores.cp.installConfirmTitle'),
				description: confirmDescription,
				confirmText: t('stores.cp.confirmDefaultContinue')
			});
			if (!ok) return;

			const res = await this.runWithSudoRetry(t('stores.cp.installSudoPrompt'), invoke);
			if (res) {
				this.openTerminal(terminalLabel);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('stores.cp.installStartFailed'));
		} finally {
			this.isPrivilegedPending = false;
		}
	}

	async triggerInstall(component: string, version?: string) {
		const friendly = getComponentTitle(component);
		const label = version ? `${friendly} (${version})` : friendly;
		await this.runInstallWithConfirm(
			label,
			t('stores.cp.installConfirmSingle', { label }),
			(sudoPass) => api.triggerInstall(component, version, sudoPass)
		);
	}

	async triggerDockerInstall(dockerOptions: DockerInstallOptions) {
		const label = getComponentTitle('docker');
		const sourceLabel =
			dockerOptions.source === 'distro'
				? t('stores.cp.dockerRepoDistro')
				: t('stores.cp.dockerRepoOfficial');
		await this.runInstallWithConfirm(
			label,
			t('stores.cp.dockerInstallConfirm', { source: sourceLabel }),
			(sudoPass) => api.triggerInstall('docker', undefined, sudoPass, dockerOptions)
		);
	}

	async triggerBatchInstall(
		label: string,
		items: { component: string; version?: string }[]
	) {
		if (items.length === 0) {
			toast.error(t('stores.cp.batchSelectAtLeastOne'));
			return;
		}
		const summary = items
			.map((it) => {
				const friendly = getComponentTitle(it.component);
				return it.version ? `${friendly} (${it.version})` : friendly;
			})
			.join(', ');
		await this.runInstallWithConfirm(
			label,
			t('stores.cp.batchInstallConfirm', { label, count: items.length, summary }),
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
				? t('stores.cp.switchPgConfirm', { version })
				: t('stores.cp.switchGenericConfirm', { label, version });

		try {
			const ok = await this.confirm({
				title: t('stores.cp.switchConfirmTitle'),
				description: confirmDescription,
				confirmText: t('stores.cp.switchNowBtn')
			});
			if (!ok) return;

			const res = await this.runWithSudoRetry(
				t('stores.cp.switchSudoPrompt', { label }),
				(sudoPass) => api.switchRuntime(type, version, sudoPass)
			);
			if (res) {
				this.openTerminal(`Switch ${label} -> ${version}`);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('stores.cp.switchFailed'));
		} finally {
			this.isPrivilegedPending = false;
		}
	}

	async syncToolchainEnv() {
		if (this.isPrivilegedPending) return;
		this.isPrivilegedPending = true;
		try {
			const ok = await this.confirm({
				title: t('stores.cp.syncEnvTitle'),
				description: t('stores.cp.syncEnvConfirm'),
				confirmText: t('stores.cp.syncEnvBtn')
			});
			if (!ok) return;

			const res = await this.runWithSudoRetry(
				t('stores.cp.syncEnvSudoPrompt'),
				(sudoPass) => api.syncEnv(sudoPass)
			);
			if (res) {
				this.openTerminal(t('stores.cp.syncEnvTaskLabel'));
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('stores.cp.syncEnvFailed'));
		} finally {
			this.isPrivilegedPending = false;
		}
	}
}

export const cp = new ControlPlaneStore();
