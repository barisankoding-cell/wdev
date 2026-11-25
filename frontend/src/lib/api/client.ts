import type {
	AddPDNSZoneResponse,
	AuthMeResponse,
	BindPanelDomainPayload,
	BindPanelDomainResponse,
	CFDeleteProfileResult,
	CFExposeResult,
	CFHostCheckResult,
	CFLoginSession,
	CFProfileInfo,
	CFUnexposeResult,
	CFZoneInfo,
	ChangeCredentialsPayload,
	ComposeActionPayload,
	ComposeCreatePayload,
	ComposeDiskUsageSummary,
	ComposeExposePayload,
	ComposeProject,
	ComposeProjectFiles,
	ComposeStatusReport,
	CreateSitePayload,
	DockerConfig,
	DockerInstallOptions,
	DomainAccounts,
	DomainActionReport,
	EdgeConfig,
	EnvStatusResponse,
	FileContentResult,
	FileEntry,
	FileListResult,
	FileRootInfo,
	LoginPayload,
	MailStatus,
	ManagedRelayRecord,
	ModeResponse,
	NginxConfig,
	OrcaCommitAIConfig,
	OrcaCommitAIModelsResult,
	OrcaCommitAISavePayload,
	OrcaCommitAISyncStatus,
	PanelDomainStatus,
	PDNSExposePayload,
	PHPConfig,
	PHPInspectResult,
	PortStatusReport,
	PostgresConfig,
	PostgresCreateDBPayload,
	PostgresDeleteRolePayload,
	PostgresInspectResult,
	PostgresSaveRolePayload,
	PreflightStatus,
	ProxyExposure,
	RedisConfig,
	RedisInspectResult,
	RelayDNSReport,
	ResendSyncResult,
	RRSet,
	ServerMode,
	ServiceConfigKey,
	SetRelayPayload,
	TerminalAuditRecord,
	TerminalInfo,
	TerminalUnlockResponse,
	SetupComponentStatus,
	SetupMailPayload,
	SiteAppEnvEntry,
	SiteAppInspectResult,
	SiteAppRuntimeStatus,
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
	if (init?.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
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
		if (
			res.status === 403 &&
			typeof data === 'object' &&
			data !== null &&
			(data as { code?: unknown }).code === 'panel_domain_required' &&
			typeof window !== 'undefined'
		) {
			window.dispatchEvent(new CustomEvent('wdev:domain_required'));
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
	getServiceConfig: <T = RedisConfig | PostgresConfig | PHPConfig | NginxConfig | DockerConfig>(
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

	inspectService: <T = RedisInspectResult | PostgresInspectResult | PHPInspectResult>(
		service: 'redis' | 'postgresql' | 'php-fpm',
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

	savePostgresRole: (payload: PostgresSaveRolePayload) =>
		apiFetch<SudoOrConfigResponse<never>>('/api/service-config/postgresql/save-role', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),

	deletePostgresRole: (payload: PostgresDeleteRolePayload) =>
		apiFetch<SudoOrConfigResponse<never>>('/api/service-config/postgresql/delete-role', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),

	// Sites & Managed Apps
	listSites: () => apiFetch<SiteRecord[]>('/api/sites'),
	createSite: (payload: CreateSitePayload) =>
		apiFetch<{
			status: string;
			message?: string;
			dns_synced?: boolean;
			dns_zone?: string;
			dns_message?: string;
			edge_exposed?: boolean;
			edge_domain?: string;
			edge_ssl?: boolean;
			edge_error?: string;
			build_task_id?: number;
			build_error?: string;
		}>('/api/sites', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	updateSite: (
		domain: string,
		payload: Partial<CreateSitePayload> & { sudo_pass?: string }
	) =>
		apiFetch<{
			status: string;
			site?: SiteRecord;
			build_task_id?: number;
			build_error?: string;
			message?: string;
		}>(`/api/sites/${encodeURIComponent(domain)}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	inspectSiteDir: (path: string) =>
		apiFetch<{ inspect: SiteAppInspectResult; suggested_port: number; port_conflict_hint?: string }>('/api/sites/inspect-dir', {
			method: 'POST',
			body: JSON.stringify({ path })
		}),
	suggestSitePort: () => apiFetch<{ port: number }>('/api/sites/suggest-port'),
	siteAppAction: (
		domain: string,
		action: 'start' | 'stop' | 'restart' | 'enable' | 'disable',
		sudoPass?: string
	) =>
		apiFetch<{ status: string; message?: string; app_status?: SiteAppRuntimeStatus }>(
			'/api/sites/app/action',
			{
				method: 'POST',
				body: JSON.stringify({
					domain,
					action,
					...(sudoPass ? { sudo_pass: sudoPass } : {})
				})
			}
		),
	buildSiteApp: (domain: string, restart = true, sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/sites/app/build', {
			method: 'POST',
			body: JSON.stringify({
				domain,
				restart,
				...(sudoPass ? { sudo_pass: sudoPass } : {})
			})
		}),
	getSiteAppEnv: (domain: string) =>
		apiFetch<{
			status?: string;
			message?: string;
			domain?: string;
			env_file?: string;
			use_project_env?: boolean;
			entries?: SiteAppEnvEntry[];
			raw?: string;
		}>(`/api/sites/app/env?domain=${encodeURIComponent(domain)}`),
	saveSiteAppEnv: (payload: {
		domain: string;
		entries?: SiteAppEnvEntry[];
		raw?: string;
		restart?: boolean;
		sudo_pass?: string;
	}) =>
		apiFetch<{
			status: string;
			message?: string;
			env_file?: string;
			entries?: SiteAppEnvEntry[];
			raw?: string;
			restarted?: boolean;
			port_synced?: number;
			app_status?: SiteAppRuntimeStatus;
		}>('/api/sites/app/env', {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	getSiteAppLogs: (domain: string, lines = 200) =>
		apiFetch<{ status?: string; message?: string; domain?: string; service_name?: string; logs?: string }>(
			`/api/sites/app/logs?domain=${encodeURIComponent(domain)}&lines=${lines}`
		),
	siteAppLogsStreamUrl: (domain: string, tail = 100) =>
		`/api/sites/app/logs/stream?domain=${encodeURIComponent(domain)}&tail=${tail}`,
	enableSiteSSL: (domain: string, email = '', sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; domain?: string; message?: string }>('/api/sites/ssl', {
			method: 'POST',
			body: JSON.stringify({
				domain,
				...(email ? { email } : {}),
				...(sudoPass ? { sudo_pass: sudoPass } : {})
			})
		}),
	deleteSite: (
		domain: string,
		opts?: { unexpose?: boolean; delete_dns?: boolean; sudo_pass?: string }
	) => {
		const params = new URLSearchParams();
		if (opts?.unexpose) params.set('unexpose', '1');
		if (opts?.delete_dns) params.set('delete_dns', '1');
		const qs = params.toString();
		return apiFetch<{
			status: string;
			message?: string;
			unexposed?: boolean;
			unexpose_error?: string;
			dns_deleted?: boolean;
		}>(`/api/sites/${encodeURIComponent(domain)}${qs ? `?${qs}` : ''}`, {
			method: 'DELETE',
			...(opts?.sudo_pass ? { body: JSON.stringify({ sudo_pass: opts.sudo_pass }) } : {})
		});
	},

	// Switchers
	getSwitchers: () => apiFetch<SwitchersMap>('/api/switchers'),
	switchRuntime: (type: string, version: string, sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>(`/api/switchers/${encodeURIComponent(type)}`, {
			method: 'POST',
			body: JSON.stringify(sudoPass ? { version, sudo_pass: sudoPass } : { version })
		}),

	// Toolchains & Env & Setup Components
	getSetupComponents: () => apiFetch<Record<string, SetupComponentStatus>>('/api/setup/components'),
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
	getOrcaCommitAI: () => apiFetch<OrcaCommitAIConfig>('/api/workspace/orca-ai'),
	getOrcaCommitAIModels: (agent: string, refresh = false) => {
		const params = new URLSearchParams({ agent });
		if (refresh) params.set('refresh', '1');
		return apiFetch<OrcaCommitAIModelsResult>(`/api/workspace/orca-ai/models?${params.toString()}`);
	},
	saveOrcaCommitAI: (payload: OrcaCommitAISavePayload) =>
		apiFetch<{ status: string; message: string; config: OrcaCommitAIConfig; sync?: OrcaCommitAISyncStatus }>(
			'/api/workspace/orca-ai',
			{
				method: 'POST',
				body: JSON.stringify(payload)
			}
		),
	getOrcaCommitAISync: () => apiFetch<OrcaCommitAISyncStatus>('/api/workspace/orca-ai/sync'),
	applyOrcaCommitAISync: (force = true) =>
		apiFetch<{ status: string; message: string; sync?: OrcaCommitAISyncStatus }>(
			'/api/workspace/orca-ai/sync',
			{
				method: 'POST',
				body: JSON.stringify({ force })
			}
		),

	// Install & Tasks & Self-Update
	triggerInstall: (
		component: string,
		version?: string,
		sudoPass?: string,
		dockerOptions?: DockerInstallOptions
	) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/install', {
			method: 'POST',
			body: JSON.stringify({
				component,
				...(version ? { version } : {}),
				...(sudoPass ? { sudo_pass: sudoPass } : {}),
				...(dockerOptions ? { docker_options: dockerOptions } : {})
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
	exposeCF: (
		local_domain: string,
		public_host: string,
		profile = '',
		service_url = '',
		no_tls_verify?: boolean,
		overwrite = false
	) =>
		apiFetch<CFExposeResult>('/api/cloudflare/expose', {
			method: 'POST',
			body: JSON.stringify({
				local_domain,
				public_host,
				profile,
				...(service_url ? { service_url } : {}),
				...(no_tls_verify !== undefined ? { no_tls_verify } : {}),
				...(overwrite ? { overwrite: true } : {})
			})
		}),
	checkCFHost: (profile: string, host: string, local_domain = '') => {
		const params = new URLSearchParams({ profile, host });
		if (local_domain) params.set('local_domain', local_domain);
		return apiFetch<CFHostCheckResult>(`/api/cloudflare/check-host?${params.toString()}`);
	},
	unexposeCF: (local_domain: string, profile = '') =>
		apiFetch<CFUnexposeResult>('/api/cloudflare/unexpose', {
			method: 'POST',
			body: JSON.stringify({ local_domain, ...(profile ? { profile } : {}) })
		}),
	listCFProfileZones: (profile: string) =>
		apiFetch<{ profile: string; zones: CFZoneInfo[] }>(
			`/api/cloudflare/zones?profile=${encodeURIComponent(profile)}`
		),
	registerCFTokenProfile: (payload: {
		name: string;
		token: string;
		tunnel_name?: string;
		api_token?: string;
		auto_start?: boolean;
	}) =>
		apiFetch<{ status: string; profile: CFProfileInfo }>('/api/cloudflare/profiles/token', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	registerCFCertProfile: (payload: {
		name: string;
		cert_pem?: string;
		local_cert_path?: string;
		credentials_json?: string;
		tunnel_name?: string;
		tunnel_id?: string;
		api_token?: string;
		auto_start?: boolean;
	}) =>
		apiFetch<{ status: string; profile: CFProfileInfo }>('/api/cloudflare/profiles/cert', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	setCFProfileAPIToken: (name: string, api_token: string) =>
		apiFetch<{ status: string; profile: CFProfileInfo }>('/api/cloudflare/profiles/api-token', {
			method: 'POST',
			body: JSON.stringify({ name, api_token })
		}),
	startCFLogin: (name: string, tunnel_name = '', api_token = '') =>
		apiFetch<CFLoginSession>('/api/cloudflare/profiles/login/start', {
			method: 'POST',
			body: JSON.stringify({
				name,
				...(tunnel_name ? { tunnel_name } : {}),
				...(api_token ? { api_token } : {})
			})
		}),
	getCFLoginStatus: (id: string) =>
		apiFetch<CFLoginSession>(`/api/cloudflare/profiles/login/status?id=${encodeURIComponent(id)}`),
	cancelCFLogin: (id: string) =>
		apiFetch<{ status: string }>('/api/cloudflare/profiles/login/cancel', {
			method: 'POST',
			body: JSON.stringify({ id })
		}),
	controlCFProfile: (name: string, action: 'start' | 'stop' | 'restart' | 'enable' | 'disable') =>
		apiFetch<{ status: string }>('/api/cloudflare/profiles/control', {
			method: 'POST',
			body: JSON.stringify({ name, action })
		}),
	getCFProfileLogs: (name: string, lines = 50) =>
		apiFetch<{ profile: string; logs: string }>(
			`/api/cloudflare/profiles/logs?name=${encodeURIComponent(name)}&lines=${lines}`
		),
	deleteCFProfile: (name: string, opts?: { delete_remote?: boolean; delete_dns?: boolean }) =>
		apiFetch<CFDeleteProfileResult>('/api/cloudflare/profiles/delete', {
			method: 'POST',
			body: JSON.stringify({
				name,
				...(opts?.delete_remote ? { delete_remote: true } : {}),
				...(opts?.delete_dns ? { delete_dns: true } : {})
			})
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
	getMailStatus: (forceProbe = false) =>
		apiFetch<MailStatus>(forceProbe ? '/api/mail/status?probe=1' : '/api/mail/status'),
	getMailAccounts: (domain = '') =>
		apiFetch<DomainAccounts[]>(
			domain ? `/api/mail/accounts?domain=${encodeURIComponent(domain)}` : '/api/mail/accounts'
		),
	addMailAccount: (email: string, password: string) =>
		apiFetch<{ status: string; message: string }>('/api/mail/account', {
			method: 'POST',
			body: JSON.stringify({ email, password })
		}),
	changeMailAccountPassword: (email: string, password: string) =>
		apiFetch<{ status: string; message: string }>('/api/mail/account/password', {
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
	syncMailRelayDNS: (domain: string, api_key = '', provider = 'resend') =>
		apiFetch<ResendSyncResult>('/api/mail/relay/dns/sync', {
			method: 'POST',
			body: JSON.stringify({ domain, api_key, provider })
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
	getPanelDomain: () => apiFetch<PanelDomainStatus>('/api/panel/domain'),
	bindPanelDomain: (payload: BindPanelDomainPayload) =>
		apiFetch<BindPanelDomainResponse>('/api/panel/domain', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	setPanelBlockIP: (block: boolean) =>
		apiFetch<{ status: string; domain: string; block_direct_ip: boolean }>('/api/panel/domain/block-ip', {
			method: 'POST',
			body: JSON.stringify({ block })
		}),
	setPanelSSL: (enabled: boolean, hosts: string[] = []) =>
		apiFetch<{
			status: string;
			panel_ssl_enabled: boolean;
			panel_ssl_expires_at?: string;
			panel_ssl_days_left?: number;
			redirect_url?: string;
		}>('/api/panel/ssl', {
			method: 'POST',
			body: JSON.stringify({ enabled, hosts })
		}),
	unbindPanelDomain: (remove_vhost = true) =>
		apiFetch<{ status: string; message: string; deleted_domain?: string; dns_records_deleted?: number }>('/api/panel/domain', {
			method: 'DELETE',
			body: JSON.stringify({ remove_vhost })
		}),

	// Server Mode (Lokal Standalone vs Hybrid Edge vs aaPanel Full VPS)
	getMode: () => apiFetch<ModeResponse>('/api/mode'),
	setMode: (mode: ServerMode) =>
		apiFetch<{ status: string; mode: string; message: string }>('/api/mode', {
			method: 'POST',
			body: JSON.stringify({ mode })
		}),

	// File Manager
	getFileRoots: () => apiFetch<FileRootInfo[]>('/api/files/roots'),
	listFiles: (path = '', showHidden = false) => {
		const params = new URLSearchParams();
		if (path) params.set('path', path);
		if (showHidden) params.set('hidden', '1');
		const qs = params.toString();
		return apiFetch<FileListResult>(qs ? `/api/files/list?${qs}` : '/api/files/list');
	},
	searchFiles: (path: string, q: string, showHidden = false) => {
		const params = new URLSearchParams({ path, q });
		if (showHidden) params.set('hidden', '1');
		return apiFetch<{ entries: FileEntry[] }>(`/api/files/search?${params.toString()}`);
	},
	readFileContent: (path: string) =>
		apiFetch<FileContentResult>(`/api/files/read?path=${encodeURIComponent(path)}`),
	writeFileContent: (path: string, content: string) =>
		apiFetch<{ status: string; path: string }>('/api/files/write', {
			method: 'POST',
			body: JSON.stringify({ path, content })
		}),
	mkdirFile: (path: string) =>
		apiFetch<{ status: string }>('/api/files/mkdir', {
			method: 'POST',
			body: JSON.stringify({ path })
		}),
	renameFile: (path: string, new_name: string) =>
		apiFetch<{ status: string; path: string }>('/api/files/rename', {
			method: 'POST',
			body: JSON.stringify({ path, new_name })
		}),
	copyFiles: (sources: string[], dest_dir: string) =>
		apiFetch<{ status: string; count: number }>('/api/files/copy', {
			method: 'POST',
			body: JSON.stringify({ sources, dest_dir })
		}),
	moveFiles: (sources: string[], dest_dir: string) =>
		apiFetch<{ status: string; count: number }>('/api/files/move', {
			method: 'POST',
			body: JSON.stringify({ sources, dest_dir })
		}),
	deleteFiles: (paths: string[]) =>
		apiFetch<{ status: string; count: number }>('/api/files/delete', {
			method: 'POST',
			body: JSON.stringify({ paths })
		}),
	chmodFiles: (paths: string[], mode: string, recursive = false, web_owner = false) =>
		apiFetch<{ status: string }>('/api/files/chmod', {
			method: 'POST',
			body: JSON.stringify({ paths, mode, recursive, web_owner })
		}),
	uploadFiles: (
		dir: string,
		items: FileList | File[] | Array<{ file: File; relativePath?: string }>,
		options?: {
			overwrite?: boolean;
			onProgress?: (info: { loaded: number; total: number; percent: number }) => void;
			signal?: AbortSignal;
		}
	): Promise<{ status: string; count: number; skipped_count?: number; skipped?: string[]; entries: FileEntry[] }> => {
		const form = new FormData();
		form.append('dir', dir);
		form.append('overwrite', options?.overwrite === false ? 'false' : 'true');

		const list = Array.from(items as ArrayLike<File | { file: File; relativePath?: string }>);
		for (const item of list) {
			if (item instanceof File) {
				const rel = (item as File & { webkitRelativePath?: string }).webkitRelativePath || item.name;
				form.append('files', item, item.name);
				form.append('rel_paths', rel);
			} else {
				const rel = item.relativePath || item.file.name;
				form.append('files', item.file, item.file.name);
				form.append('rel_paths', rel);
			}
		}

		return new Promise((resolve, reject) => {
			const xhr = new XMLHttpRequest();
			xhr.open('POST', '/api/files/upload');
			xhr.withCredentials = true;

			if (options?.signal) {
				if (options.signal.aborted) {
					reject(new ApiError('Unggahan dibatalkan', 0));
					return;
				}
				const onAbort = () => xhr.abort();
				options.signal.addEventListener('abort', onAbort, { once: true });
			}

			xhr.upload.onprogress = (ev) => {
				if (!options?.onProgress || !ev.lengthComputable || ev.total <= 0) return;
				const percent = Math.min(100, Math.round((ev.loaded / ev.total) * 100));
				options.onProgress({ loaded: ev.loaded, total: ev.total, percent });
			};

			xhr.onabort = () => {
				reject(new ApiError('Unggahan dibatalkan', 0));
			};

			xhr.onerror = () => {
				reject(new ApiError('Gagal terhubung ke server saat mengunggah berkas', 0));
			};

			xhr.onload = () => {
				let parsed: Record<string, unknown> = {};
				if (xhr.responseText) {
					try {
						parsed = JSON.parse(xhr.responseText) as Record<string, unknown>;
					} catch {
						parsed = { error: xhr.responseText };
					}
				}
				if (xhr.status >= 200 && xhr.status < 300) {
					resolve(
						parsed as {
							status: string;
							count: number;
							skipped_count?: number;
							skipped?: string[];
							entries: FileEntry[];
						}
					);
					return;
				}
				if (xhr.status === 401 && typeof window !== 'undefined') {
					window.dispatchEvent(new CustomEvent('wdev:unauthorized'));
				}
				const msg =
					typeof parsed.error === 'string' && parsed.error
						? parsed.error
						: typeof parsed.message === 'string' && parsed.message
							? parsed.message
							: `Upload gagal (HTTP ${xhr.status})`;
				reject(new ApiError(msg, xhr.status, parsed));
			};

			xhr.send(form);
		});
	},
	fetchFileFromURL: (dir: string, url: string, filename = '', overwrite = true) =>
		apiFetch<{ status: string; entry: FileEntry }>('/api/files/fetch-url', {
			method: 'POST',
			body: JSON.stringify({ dir, url, filename, overwrite })
		}),
	compressFiles: (paths: string[], output_path = '') =>
		apiFetch<{ status: string; entry: FileEntry }>('/api/files/compress', {
			method: 'POST',
			body: JSON.stringify({ paths, output_path })
		}),
	extractFile: (path: string, dest_dir = '') =>
		apiFetch<{ status: string; count: number }>('/api/files/extract', {
			method: 'POST',
			body: JSON.stringify({ path, dest_dir })
		}),
	fileRawUrl: (path: string) => `/api/files/raw?path=${encodeURIComponent(path)}`,
	fileDownloadUrl: (paths: string[]) => {
		const params = new URLSearchParams();
		for (const p of paths) params.append('paths', p);
		return `/api/files/download?${params.toString()}`;
	},

	// Interactive PTY Terminal
	getTerminalInfo: () => apiFetch<TerminalInfo>('/api/terminal/info'),
	unlockTerminal: (password: string, user = '', remember = true) =>
		apiFetch<TerminalUnlockResponse>('/api/terminal/unlock', {
			method: 'POST',
			body: JSON.stringify({ password, user, remember })
		}),
	lockTerminal: () =>
		apiFetch<{ status: string; message: string }>('/api/terminal/lock', {
			method: 'POST'
		}),
	getTerminalAudit: (limit = 50) =>
		apiFetch<TerminalAuditRecord[]>(`/api/terminal/audit?limit=${limit}`),
	terminalWsUrl: (
		opts: {
			cwd?: string;
			shell?: string;
			user?: string;
			token?: string;
			cols?: number;
			rows?: number;
			compose_project?: string;
			compose_service?: string;
		} = {}
	) => {
		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const params = new URLSearchParams();
		if (opts.cwd) params.set('cwd', opts.cwd);
		if (opts.shell) params.set('shell', opts.shell);
		if (opts.user) params.set('user', opts.user);
		if (opts.token) params.set('token', opts.token);
		if (opts.cols) params.set('cols', String(opts.cols));
		if (opts.rows) params.set('rows', String(opts.rows));
		if (opts.compose_project) params.set('compose_project', opts.compose_project);
		if (opts.compose_service) params.set('compose_service', opts.compose_service);
		const qs = params.toString();
		return `${proto}//${window.location.host}/api/terminal/ws${qs ? `?${qs}` : ''}`;
	},

	// Docker & Docker Compose v5
	getComposeStatus: () => apiFetch<ComposeStatusReport>('/api/compose/status'),
	listComposeProjects: () => apiFetch<ComposeProject[]>('/api/compose/projects'),
	createComposeProject: (payload: ComposeCreatePayload) =>
		apiFetch<{ status: string; project: ComposeProject; task_id?: number }>('/api/compose/projects', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	getComposeProject: (name: string) =>
		apiFetch<ComposeProject>(`/api/compose/projects/${encodeURIComponent(name)}`),
	deleteComposeProject: (name: string, down = false) =>
		apiFetch<{ status: string; message?: string }>(
			`/api/compose/projects/${encodeURIComponent(name)}${down ? '?down=1' : ''}`,
			{ method: 'DELETE' }
		),
	runComposeAction: (name: string, payload: ComposeActionPayload) =>
		apiFetch<{ status: 'started' | 'sudo_required' | string; task_id?: number; message?: string }>(
			`/api/compose/projects/${encodeURIComponent(name)}/action`,
			{
				method: 'POST',
				body: JSON.stringify(payload)
			}
		),
	getComposeFiles: (name: string) =>
		apiFetch<ComposeProjectFiles>(`/api/compose/projects/${encodeURIComponent(name)}/files`),
	saveComposeFiles: (
		name: string,
		payload: { compose_content: string; env_content: string; update_env: boolean }
	) =>
		apiFetch<{ status: string; files: ComposeProjectFiles }>(
			`/api/compose/projects/${encodeURIComponent(name)}/files`,
			{
				method: 'PUT',
				body: JSON.stringify(payload)
			}
		),
	getComposeConfig: (name: string) =>
		apiFetch<{ project: string; config: Record<string, unknown> }>(
			`/api/compose/projects/${encodeURIComponent(name)}/config`
		),
	getComposeLogs: (name: string, service = '', tail = 200) => {
		const params = new URLSearchParams({ tail: String(tail) });
		if (service) params.set('service', service);
		return apiFetch<{ project: string; service?: string; logs: string }>(
			`/api/compose/projects/${encodeURIComponent(name)}/logs?${params.toString()}`
		);
	},
	composeLogsStreamUrl: (name: string, service = '', tail = 120) => {
		const params = new URLSearchParams({ tail: String(tail) });
		if (service) params.set('service', service);
		return `/api/compose/projects/${encodeURIComponent(name)}/logs/stream?${params.toString()}`;
	},
	exposeComposeService: (name: string, payload: ComposeExposePayload) =>
		apiFetch<{
			status: string;
			dns_synced?: boolean;
			dns_zone?: string;
			dns_message?: string;
			edge_exposed?: boolean;
			edge_domain?: string;
			edge_ssl?: boolean;
			edge_error?: string;
		}>(`/api/compose/projects/${encodeURIComponent(name)}/expose`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	getComposeDiskUsage: () => apiFetch<ComposeDiskUsageSummary>('/api/compose/system/df'),
	pruneComposeSystem: (target: string, sudoPass?: string) =>
		apiFetch<{ status: string; task_id?: number; message?: string }>('/api/compose/system/prune', {
			method: 'POST',
			body: JSON.stringify({ target, ...(sudoPass ? { sudo_pass: sudoPass } : {}) })
		}),

	// Diagnostik Port & Jaringan
	getPortStatus: (forceRefresh = false) =>
		apiFetch<PortStatusReport>(forceRefresh ? '/api/ports?refresh=1' : '/api/ports')
};
