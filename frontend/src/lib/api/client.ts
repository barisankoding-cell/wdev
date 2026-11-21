import type {
	AddPDNSZoneResponse,
	AuthMeResponse,
	ChangeCredentialsPayload,
	CreateSitePayload,
	DomainAccounts,
	DomainActionReport,
	EdgeConfig,
	EnvStatusResponse,
	LoginPayload,
	MailStatus,
	ManagedRelayRecord,
	ModeResponse,
	NginxConfig,
	PDNSExposePayload,
	PHPConfig,
	PostgresConfig,
	PostgresCreateDBPayload,
	PostgresInspectResult,
	PreflightStatus,
	ProxyExposure,
	RedisConfig,
	RedisInspectResult,
	RelayDNSReport,
	RRSet,
	ServerMode,
	ServiceConfigKey,
	SetRelayPayload,
	SetupMailPayload,
	SiteRecord,
	SwitchersMap,
	SystemStats,
	TasksResponse,
	ToolchainStatus,
	TunnelStatus,
	UpdaterStatus,
	VPSConnectPayload,
	VPSDeployPayload,
	WorkspaceStatus,
	ZoneDoctorReport,
	ZoneInfo
} from '#lib/types/api.js';

export class ApiError extends Error {
	status: number;
	payload: unknown;

	constructor(message: string, status: number, payload?: unknown) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.payload = payload;
	}
}

export async function apiFetch<T>(url: string, init?: RequestInit): Promise<T> {
	const headers = new Headers(init?.headers);
	if (init?.body && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json');
	}
	const res = await fetch(url, { ...init, headers });
	const text = await res.text();
	let data: Record<string, unknown> | unknown = {};
	if (text) {
		try {
			data = JSON.parse(text);
		} catch {
			data = { error: text };
		}
	}
	if (!res.ok) {
		if (res.status === 401 && !url.includes('/api/auth/login') && !url.includes('/api/auth/me')) {
			if (typeof window !== 'undefined') {
				window.dispatchEvent(new CustomEvent('wdev:unauthorized'));
			}
		}
		const errMsg =
			typeof data === 'object' && data !== null && 'error' in data && typeof (data as { error?: unknown }).error === 'string'
				? (data as { error: string }).error
				: `HTTP ${res.status} ${res.statusText}`;
		throw new ApiError(errMsg, res.status, data);
	}
	return data as T;
}

export interface SudoOrConfigResponse<T> {
	status: 'ok' | 'saved' | 'flushed' | 'created' | 'sudo_required' | 'started';
	message?: string;
	config?: T;
	inspect?: T;
	unit?: string;
	logs?: string;
	task_id?: number;
}

export const api = {
	// Status & System
	getStatus: (refresh = false) =>
		apiFetch<SystemStats>(refresh ? '/api/status?refresh=1' : '/api/status'),

	controlService: (name: string, action: 'start' | 'stop' | 'restart' | 'reload') =>
		apiFetch<{ status: string }>(`/api/service/${encodeURIComponent(name)}/${encodeURIComponent(action)}`, {
			method: 'POST'
		}),

	// Sudo
	verifySudo: (password: string) =>
		apiFetch<{ status: string; message: string }>('/api/sudo/verify', {
			method: 'POST',
			body: JSON.stringify({ password })
		}),

	// Service Config
	getServiceConfig: <T = RedisConfig | PostgresConfig | PHPConfig | NginxConfig>(
		service: ServiceConfigKey,
		version = ''
	) => {
		const qs = version ? `?version=${encodeURIComponent(version)}` : '';
		return apiFetch<SudoOrConfigResponse<T>>(`/api/service-config/${encodeURIComponent(service)}${qs}`);
	},

	saveServiceConfig: <T>(service: ServiceConfigKey, payload: Record<string, unknown>) =>
		apiFetch<SudoOrConfigResponse<T>>(`/api/service-config/${encodeURIComponent(service)}`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),

	inspectService: <T = RedisInspectResult | PostgresInspectResult>(
		service: 'redis' | 'postgresql',
		version = ''
	) => {
		const qs = version ? `?version=${encodeURIComponent(version)}` : '';
		return apiFetch<SudoOrConfigResponse<T>>(`/api/service-config/${encodeURIComponent(service)}/inspect${qs}`);
	},

	getServiceLogs: (service: ServiceConfigKey, version = '', lines = 100) => {
		const params = new URLSearchParams({ lines: String(lines) });
		if (version) params.set('version', version);
		return apiFetch<SudoOrConfigResponse<never>>(
			`/api/service-config/${encodeURIComponent(service)}/logs?${params.toString()}`
		);
	},

	flushRedisDB: (db: number, sudoPass?: string) =>
		apiFetch<SudoOrConfigResponse<never>>('/api/service-config/redis/flush', {
			method: 'POST',
			body: JSON.stringify(sudoPass ? { db, sudo_pass: sudoPass } : { db })
		}),

	createPostgresDB: (payload: PostgresCreateDBPayload) =>
		apiFetch<SudoOrConfigResponse<never>>('/api/service-config/postgresql/create-db', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),

	// Sites
	listSites: () => apiFetch<SiteRecord[]>('/api/sites'),
	createSite: (payload: CreateSitePayload) =>
		apiFetch<{ status: string }>('/api/sites', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	deleteSite: (domain: string) =>
		apiFetch<{ status: string }>(`/api/sites/${encodeURIComponent(domain)}`, {
			method: 'DELETE'
		}),

	// Switchers
	getSwitchers: () => apiFetch<SwitchersMap>('/api/switchers'),
	switchRuntime: (type: string, version: string, sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>(`/api/switchers/${encodeURIComponent(type)}`, {
			method: 'POST',
			body: JSON.stringify(sudoPass ? { version, sudo_pass: sudoPass } : { version })
		}),

	// Toolchains & Env
	getToolchains: (refresh = false) =>
		apiFetch<ToolchainStatus[]>(refresh ? '/api/toolchains?refresh=1' : '/api/toolchains'),
	getEnvStatus: () => apiFetch<EnvStatusResponse>('/api/env'),
	syncEnv: (sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/env/sync', {
			method: 'POST',
			body: JSON.stringify(sudoPass ? { sudo_pass: sudoPass } : {})
		}),

	// Workspace
	getWorkspaceStatus: (refresh = false) =>
		apiFetch<WorkspaceStatus>(refresh ? '/api/workspace/status?refresh=1' : '/api/workspace/status'),

	// Install & Tasks & Self-Update
	triggerInstall: (component: string, version?: string, sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/install', {
			method: 'POST',
			body: JSON.stringify({
				component,
				...(version ? { version } : {}),
				...(sudoPass ? { sudo_pass: sudoPass } : {})
			})
		}),
	triggerBatchInstall: (
		items: { component: string; version?: string }[],
		label?: string,
		sudoPass?: string
	) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/install', {
			method: 'POST',
			body: JSON.stringify({
				items,
				...(label ? { label } : {}),
				...(sudoPass ? { sudo_pass: sudoPass } : {})
			})
		}),
	getTasks: (limit = 30) => apiFetch<TasksResponse>(`/api/tasks?limit=${limit}`),
	getUpdaterStatus: (check = false) =>
		apiFetch<UpdaterStatus>(check ? '/api/updater/status?check=1' : '/api/updater/status'),
	applySelfUpdate: (sourceDir = '', sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/updater/apply', {
			method: 'POST',
			body: JSON.stringify({
				...(sourceDir ? { source_dir: sourceDir } : {}),
				...(sudoPass ? { sudo_pass: sudoPass } : {})
			})
		}),

	// Cloudflare
	getCFStatus: () => apiFetch<TunnelStatus>('/api/cloudflare/status'),
	exposeCF: (local_domain: string, public_host: string, profile = '') =>
		apiFetch<{ status: string }>('/api/cloudflare/expose', {
			method: 'POST',
			body: JSON.stringify({ local_domain, public_host, profile })
		}),
	unexposeCF: (local_domain: string) =>
		apiFetch<{ status: string }>('/api/cloudflare/unexpose', {
			method: 'POST',
			body: JSON.stringify({ local_domain })
		}),

	// PowerDNS & Edge
	getPDNSStatus: () => apiFetch<EdgeConfig>('/api/pdns/status'),
	listPDNSZones: () => apiFetch<ZoneInfo[]>('/api/pdns/zones'),
	addPDNSZone: (zone_name: string, create_vhost = true) =>
		apiFetch<AddPDNSZoneResponse>('/api/pdns/zones', {
			method: 'POST',
			body: JSON.stringify({ zone_name, create_vhost })
		}),
	deletePDNSZone: (zone_name: string) =>
		apiFetch<{ status: string }>('/api/pdns/zones', {
			method: 'DELETE',
			body: JSON.stringify({ zone_name })
		}),
	doctorPDNSZone: (zone: string) =>
		apiFetch<ZoneDoctorReport>(`/api/pdns/zone/doctor?zone=${encodeURIComponent(zone)}`),
	repairPDNSZone: (zone: string) =>
		apiFetch<ZoneDoctorReport>('/api/pdns/zone/repair', {
			method: 'POST',
			body: JSON.stringify({ zone })
		}),
	listPDNSRecords: (zone: string) =>
		apiFetch<RRSet[]>(`/api/pdns/records?zone=${encodeURIComponent(zone)}`),
	addPDNSRecord: (zone: string, name: string, type: string, content: string, ttl: number) =>
		apiFetch<{ status: string }>('/api/pdns/records', {
			method: 'POST',
			body: JSON.stringify({ zone, name, type, content, ttl })
		}),
	deletePDNSRecord: (zone: string, name: string, type: string) =>
		apiFetch<{ status: string }>('/api/pdns/records', {
			method: 'DELETE',
			body: JSON.stringify({ zone, name, type })
		}),
	listPDNSExposures: () => apiFetch<ProxyExposure[]>('/api/pdns/exposures'),
	exposePDNS: (payload: PDNSExposePayload) =>
		apiFetch<{ status: string }>('/api/pdns/expose', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	unexposePDNS: (domain: string, keep_dns = false) =>
		apiFetch<{ status: string }>('/api/pdns/unexpose', {
			method: 'POST',
			body: JSON.stringify({ domain, keep_dns })
		}),
	deployVPS: (payload: VPSDeployPayload) =>
		apiFetch<{ status: string; task_id: number }>('/api/pdns/deploy', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	connectVPS: (payload: VPSConnectPayload) =>
		apiFetch<{ status: string }>('/api/pdns/connect', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),

	// Mail Server
	getMailStatus: () => apiFetch<MailStatus>('/api/mail/status'),
	getMailAccounts: (domain = '') =>
		apiFetch<DomainAccounts[]>(
			domain ? `/api/mail/accounts?domain=${encodeURIComponent(domain)}` : '/api/mail/accounts'
		),
	addMailAccount: (email: string, password: string) =>
		apiFetch<{ status: string; message: string }>('/api/mail/account', {
			method: 'POST',
			body: JSON.stringify({ email, password })
		}),
	deleteMailAccount: (email: string) =>
		apiFetch<{ status: string; message: string }>('/api/mail/account/delete', {
			method: 'POST',
			body: JSON.stringify({ email })
		}),
	setMailRelay: (payload: SetRelayPayload) =>
		apiFetch<{ status: string; message: string; dns?: RelayDNSReport }>('/api/mail/relay', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	removeMailRelay: (domain: string) =>
		apiFetch<{ status: string; message: string; dns?: RelayDNSReport }>('/api/mail/relay/remove', {
			method: 'POST',
			body: JSON.stringify({ domain })
		}),
	getMailPreflight: (domain = '') =>
		apiFetch<PreflightStatus>(`/api/mail/preflight?domain=${encodeURIComponent(domain)}`),
	listMailRelayDNS: (domain: string) =>
		apiFetch<ManagedRelayRecord[]>(`/api/mail/relay/dns?domain=${encodeURIComponent(domain)}`),
	addMailRelayDNS: (domain: string, provider: string, records: Pick<ManagedRelayRecord, 'type' | 'name' | 'content'>[]) =>
		apiFetch<RelayDNSReport>('/api/mail/relay/dns', {
			method: 'POST',
			body: JSON.stringify({ domain, provider, records })
		}),
	resetMailAdmin: (domain: string) =>
		apiFetch<{ status: string; message: string }>('/api/mail/admin-reset', {
			method: 'POST',
			body: JSON.stringify({ domain })
		}),
	setupMailDomain: (payload: SetupMailPayload) =>
		apiFetch<{ status: string; task_id: number }>('/api/mail/setup', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	deleteMailDomain: (domain: string, delete_dns: boolean) =>
		apiFetch<DomainActionReport>('/api/mail/domain/delete', {
			method: 'POST',
			body: JSON.stringify({ domain, delete_dns })
		}),
	repairMailDomain: (domain: string) =>
		apiFetch<{ status: string; task_id: number }>('/api/mail/domain/repair', {
			method: 'POST',
			body: JSON.stringify({ domain })
		}),

	// Panel Auth & Security
	getAuthMe: () => apiFetch<AuthMeResponse>('/api/auth/me'),
	login: (payload: LoginPayload) =>
		apiFetch<{ status: string; message: string; username: string; token: string }>('/api/auth/login', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	logout: () =>
		apiFetch<{ status: string; message: string }>('/api/auth/logout', {
			method: 'POST'
		}),
	changeCredentials: (payload: ChangeCredentialsPayload) =>
		apiFetch<{ status: string; message: string }>('/api/auth/change-credentials', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),

	// Server Mode (Lokal Standalone vs Hybrid Edge vs aaPanel Full VPS)
	getMode: () => apiFetch<ModeResponse>('/api/mode'),
	setMode: (mode: ServerMode) =>
		apiFetch<{ status: string; mode: string; message: string }>('/api/mode', {
			method: 'POST',
			body: JSON.stringify({ mode })
		})
};
