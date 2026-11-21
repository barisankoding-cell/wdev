export interface ServiceStatus {
	name: string;
	display_name: string;
	category: string;
	active: boolean;
	status_text: string;
	version?: string;
	port?: string;
	is_remote?: boolean;
	installed: boolean;
	latest_version?: string;
	has_update: boolean;
	install_component?: string;
}

export interface SystemStats {
	wdev_version?: string;
	hostname: string;
	os_name: string;
	kernel: string;
	uptime_string: string;
	load_avg: string;
	cpu_cores?: number;
	load_percent?: number;
	load_1m?: number;
	load_5m?: number;
	load_15m?: number;
	load_1m_percent?: number;
	load_5m_percent?: number;
	load_15m_percent?: number;
	mem_total_mb: number;
	mem_used_mb: number;
	mem_percent: number;
	disk_total_gb: number;
	disk_used_gb: number;
	disk_percent: number;
	lan_ip: string;
	netbird_ip: string;
	services: ServiceStatus[];
	runtimes: Record<string, string>;
}

export interface SiteRecord {
	domain: string;
	root_path: string;
	php_version: string;
	framework: string;
	proxy_port: number;
	ssl_enabled: boolean;
	exposed_to: string;
	created_at: string;
}

export interface CreateSitePayload {
	domain: string;
	root_path: string;
	php_version: string;
	framework: string;
	proxy_port: number;
	ssl_enabled: boolean;
}

export interface RuntimeInfo {
	name: string;
	active_version: string;
	active_major: string;
	available_versions: string[];
}

export type SwitchersMap = Record<string, RuntimeInfo>;

export interface ToolchainStatus {
	id: string;
	name: string;
	category: string;
	description: string;
	installed: boolean;
	installed_version: string;
	extra_info?: string;
	latest_version: string;
	has_update: boolean;
	install_component: string;
}

export interface OrcaInfo {
	installed: boolean;
	active: boolean;
	status_text: string;
	mode: string;
	version: string;
	latest_version?: string;
	has_update: boolean;
	port?: number;
	pid?: number;
	web_client_url?: string;
	pairing_url?: string;
	endpoint?: string;
}

export interface ShellInfo {
	zsh_installed: boolean;
	oh_my_zsh: boolean;
	current_shell: string;
	active_theme: string;
	nerd_font: string;
	plugins: string[] | null;
}

export interface WorkspaceStatus {
	orca: OrcaInfo;
	shell: ShellInfo;
}

export interface Exposure {
	local_domain: string;
	public_host: string;
	profile: string;
}

export interface TunnelStatus {
	installed: boolean;
	binary: string;
	active: boolean;
	profiles: string[];
	exposures: Exposure[];
}

export interface ModeResponse {
	mode: 'vps' | 'edge';
	is_set: boolean;
	public_ip: string;
	netbird_ip: string;
	has_local_pdns: boolean;
	reachable: boolean;
}

export interface EdgeConfig {
	configured: boolean;
	vps_netbird_ip: string;
	vps_public_ip: string;
	api_key: string;
	pdns_port: string;
	agent_port: string;
	local_netbird_ip: string;
	reachable: boolean;
	mode?: 'vps' | 'edge';
	mode_is_set?: boolean;
	has_local_pdns?: boolean;
}

export interface ZoneInfo {
	id: string;
	name: string;
	kind: string;
	serial: number;
	url: string;
	rrsets?: RRSet[];
}

export interface DNSRecord {
	content: string;
	disabled: boolean;
}

export interface RRSet {
	name: string;
	type: string;
	ttl: number;
	changetype?: string;
	records: DNSRecord[];
}

export interface DoctorCheck {
	name: string;
	status: 'ok' | 'warn' | 'fail' | string;
	detail: string;
}

export interface ZoneDoctorReport {
	zone: string;
	nameservers: string[];
	ok: boolean;
	checks: DoctorCheck[];
	error?: string;
	report?: ZoneDoctorReport;
}

export interface ProxyExposure {
	domain: string;
	enabled: boolean;
	config_file: string;
	target?: string;
	local_site?: string;
	ssl?: string;
}

export interface PDNSExposePayload {
	mode: 'site' | 'port';
	local_domain?: string;
	public_domain: string;
	target_port?: number;
	use_ssl: boolean;
	websocket: boolean;
}

export interface VPSDeployPayload {
	host: string;
	port: number;
	user: string;
	auth_method: 'password' | 'key';
	password: string;
	key_path: string;
}

export interface VPSConnectPayload {
	vps_netbird_ip: string;
	vps_public_ip: string;
	api_key: string;
	pdns_port: string;
	agent_port: string;
}

export interface DomainSummary {
	domain: string;
	admin_url: string;
	webmail: string;
	admin_user: string;
}

export interface RelayConfig {
	domain?: string;
	provider: 'resend' | 'brevo' | 'custom' | string;
	host: string;
	port: number;
	implicit_tls: boolean;
	username: string;
	updated_at?: string;
}

export interface MailStatus {
	stalwart_active: boolean;
	snappymail_active: boolean;
	relay_only: boolean;
	outbound_default?: string;
	outbound_routes?: Record<string, string>;
	storage_free: string;
	global_relay?: RelayConfig;
	domain_relays: Record<string, RelayConfig>;
	domains: DomainSummary[];
}

export interface MailAccount {
	id: string;
	name: string;
	email: string;
	is_admin: boolean;
	domain_id: string;
}

export interface DomainAccounts {
	domain: string;
	enabled: boolean;
	admin_url: string;
	webmail: string;
	relay?: RelayConfig;
	accounts: MailAccount[];
}

export interface PreflightStatus {
	domain: string;
	stalwart_binary: boolean;
	stalwart_cli: boolean;
	snappymail: boolean;
	netbird_ip: string;
	edge_configured: boolean;
	vps_public_ip: string;
	dns_zone: string;
	relay_configured: boolean;
	ptr?: string;
	ptr_match: boolean;
	blockers: string[] | null;
	warnings: string[] | null;
}

export interface ManagedRelayRecord {
	name: string;
	type: string;
	content: string;
	provider: string;
	created_at?: string;
}

export interface RelayDNSReport {
	domain: string;
	previous_provider?: string;
	provider?: string;
	removed: string[];
	added: string[];
	kept: string[];
	manual: string[];
}

export interface DomainCheck {
	name: string;
	status: string;
	detail: string;
}

export interface DomainActionReport {
	domain: string;
	zone?: string;
	ok: boolean;
	checks: DomainCheck[];
}

export interface SetupMailPayload {
	domain: string;
	ssl: boolean;
	relay_provider: string;
	relay_host: string;
	relay_port: number;
	relay_user: string;
	relay_pass: string;
}

export interface SetRelayPayload {
	domain: string;
	provider: string;
	host?: string;
	port?: number;
	username?: string;
	password?: string;
}

export interface RedisConfig {
	config_path: string;
	service_unit: string;
	active: boolean;
	port: number;
	bind: string;
	protected_mode: string;
	has_password: boolean;
	password?: string;
	clear_password?: boolean;
	maxmemory: string;
	maxmemory_policy: string;
	persistence_mode: 'rdb' | 'aof' | 'both' | 'none' | string;
	maxclients: number;
	databases: number;
	restart?: boolean;
}

export interface RedisKeyspaceInfo {
	db: string;
	index: number;
	keys: number;
	expires: number;
	avg_ttl: number;
}

export interface RedisInspectResult {
	version: string;
	mode: string;
	uptime_seconds: number;
	connected_clients: number;
	used_memory_human: string;
	peak_memory_human: string;
	maxmemory_human: string;
	total_commands_processed: number;
	keyspace_hits: number;
	keyspace_misses: number;
	hit_rate_percent: number;
	keyspaces: RedisKeyspaceInfo[];
}

export interface PostgresConfig {
	version: string;
	available_versions: string[];
	config_path: string;
	hba_path: string;
	service_unit: string;
	active: boolean;
	port: number;
	listen_addresses: string;
	max_connections: number;
	shared_buffers: string;
	effective_cache_size: string;
	work_mem: string;
	maintenance_work_mem: string;
	password_encryption: string;
	timezone: string;
	log_min_duration_statement: number;
	remote_access_mode: 'local' | 'private_vpn' | 'all' | string;
	new_postgres_password?: string;
	restart?: boolean;
}

export interface PostgresInspectResult {
	version: string;
	port: number;
	databases: { name: string; owner: string; size: string }[];
	roles: { name: string; superuser: boolean; can_login: boolean }[];
	connections: { pid: number; database: string; user: string; client_addr: string; state: string; query: string }[];
}

export interface PostgresCreateDBPayload {
	version: string;
	database: string;
	owner: string;
	password?: string;
	sudo_pass?: string;
}

export interface PHPConfig {
	version: string;
	available_versions: string[];
	ini_path: string;
	dropin_path: string;
	pool_path: string;
	service_unit: string;
	active: boolean;
	memory_limit: string;
	upload_max_filesize: string;
	post_max_size: string;
	max_execution_time: number;
	max_input_vars: number;
	timezone: string;
	display_errors: boolean;
	opcache_enable: boolean;
	opcache_memory_consumption: number;
	sync_cli: boolean;
	pm: 'dynamic' | 'ondemand' | 'static' | string;
	pm_max_children: number;
	pm_start_servers: number;
	pm_min_spare_servers: number;
	pm_max_spare_servers: number;
	pm_max_requests: number;
	restart?: boolean;
}

export interface NginxConfig {
	config_path: string;
	service_unit: string;
	active: boolean;
	worker_processes: string;
	worker_connections: number;
	client_max_body_size: string;
	keepalive_timeout: number;
	gzip: boolean;
	server_tokens: boolean;
	restart?: boolean;
}

export type ServiceConfigKey = 'redis' | 'postgresql' | 'php-fpm' | 'nginx';

export interface TaskEvent {
	task_id: number;
	type: 'log' | 'start' | 'finish' | 'ping';
	message: string;
	status?: string;
	timestamp?: string;
}

export interface TaskRecord {
	id: number;
	command: string;
	status: 'running' | 'success' | 'failed' | string;
	started_at: string;
	finished_at: string;
	log_output: string;
}

export interface ActiveTask {
	id: number;
	command: string;
	started_at: string;
}

export interface TasksResponse {
	active_task: ActiveTask | null;
	tasks: TaskRecord[];
}

export interface EnvStatusResponse {
	report: string[];
	path_entries: string[];
}

export interface UpdaterStatus {
	current: string;
	latest?: string;
	has_update: boolean;
	installed?: boolean;
	asset_name?: string;
	message?: string;
	error?: string;
	changelog?: string;
}

export interface AuthMeResponse {
	authenticated: boolean;
	username: string;
	auth_enabled: boolean;
	entrance_enabled: boolean;
}

export interface LoginPayload {
	username: string;
	password: string;
}

export interface ChangeCredentialsPayload {
	old_password: string;
	new_username?: string;
	new_password?: string;
}

