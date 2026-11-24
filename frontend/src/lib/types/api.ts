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
	config_preset?: string;
}

export interface CPUCoreUsage {
	core: number;
	percent: number;
}

export interface MountDriveInfo {
	name: string;
	mount_point: string;
	device?: string;
	fs_type?: string;
	total_gb: number;
	used_gb: number;
	free_gb: number;
	total_bytes: number;
	used_bytes: number;
	percent: number;
	percent_exact?: number;
}

export interface SystemStats {
	wdev_version?: string;
	hostname: string;
	os_name: string;
	kernel: string;
	kernel_full?: string;
	boot_mode?: string;
	uptime_string: string;
	uptime_seconds?: number;
	load_avg: string;
	cpu_cores?: number;
	cpu_model?: string;
	cpu_sockets?: number;
	cpu_usage_percent?: number;
	cpu_cores_usage?: CPUCoreUsage[];
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
	mem_total_bytes?: number;
	mem_used_bytes?: number;
	mem_percent_exact?: number;
	swap_total_bytes?: number;
	swap_used_bytes?: number;
	swap_percent?: number;
	disk_total_gb: number;
	disk_used_gb: number;
	disk_percent: number;
	disk_total_bytes?: number;
	disk_used_bytes?: number;
	disk_percent_exact?: number;
	mount_drives?: MountDriveInfo[];
	other_mounts?: MountDriveInfo[];
	lan_ip: string;
	netbird_ip: string;
	public_ip?: string;
	server_mode?: string;
	services: ServiceStatus[];
	runtimes: Record<string, string>;
}

export type SiteAppRuntime = 'nodejs' | 'go' | 'rust';

export interface SiteAppConfig {
	domain: string;
	runtime: SiteAppRuntime;
	working_dir: string;
	build_command?: string;
	start_command: string;
	port: number;
	run_user: string;
	auto_start: boolean;
	service_name: string;
	env_file: string;
	use_project_env?: boolean;
	last_build_status?: string;
	last_build_at?: string;
	created_at: string;
	updated_at: string;
}

export interface SiteAppRuntimeStatus {
	service_name: string;
	unit_exists: boolean;
	active_state: 'active' | 'activating' | 'inactive' | 'failed' | string;
	sub_state: string;
	enabled: boolean;
	main_pid: number;
	memory_bytes: number;
	started_at?: string;
}

export interface SiteAppScriptCandidate {
	label: string;
	build_command?: string;
	start_command: string;
}

export interface SiteAppInspectResult {
	working_dir: string;
	detected: boolean;
	runtime?: SiteAppRuntime;
	app_name?: string;
	package_manager?: string;
	scripts?: string[];
	candidates?: SiteAppScriptCandidate[];
	recommended_build?: string;
	recommended_start?: string;
	has_dot_env: boolean;
	dot_env_var_count: number;
	project_env_path?: string;
	detected_port_hint?: number;
	toolchain_installed: boolean;
	toolchain_binary?: string;
}

export interface SiteAppEnvEntry {
	key: string;
	value: string;
}

export interface SiteRecord {
	domain: string;
	root_path: string;
	php_version: string;
	framework: string;
	proxy_port: number;
	ssl_enabled: boolean;
	ssl_provider?: string;
	ssl_expires_at?: string;
	ssl_days_left?: number;
	exposed_to: string;
	created_at: string;
	app?: SiteAppConfig;
	app_status?: SiteAppRuntimeStatus;
}

export interface SiteAppInputConfig {
	build_command?: string;
	start_command: string;
	run_user?: string;
	auto_start?: boolean;
	run_build_now?: boolean;
	use_project_env?: boolean;
	import_project_env?: boolean;
	initial_env_raw?: string;
}

export interface CreateSitePayload {
	domain: string;
	root_path: string;
	php_version: string;
	framework: string;
	proxy_port: number;
	ssl_enabled: boolean;
	skip_hosts?: boolean;
	add_hosts?: boolean;
	expose_edge?: boolean;
	edge_domain?: string;
	edge_ssl?: boolean;
	sudo_pass?: string;
	app_config?: SiteAppInputConfig;
}

export interface CreateSiteResponse {
	status: string;
	message?: string;
	dns_synced?: boolean;
	dns_zone?: string;
	dns_message?: string;
	edge_exposed?: boolean;
	edge_domain?: string;
	edge_ssl?: boolean;
	edge_error?: string;
	app?: SiteAppConfig;
	build_task_id?: number;
	build_error?: string;
}

export interface DeleteSiteResponse {
	status: string;
	unexposed?: boolean;
	unexpose_error?: string;
	dns_deleted?: boolean;
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

export interface OrcaThinkingLevel {
	id: string;
	label: string;
}

export interface OrcaCommitAIModel {
	id: string;
	label: string;
	description?: string;
	thinkingLevels?: OrcaThinkingLevel[];
	defaultThinkingLevel?: string;
	isDefault?: boolean;
	source?: string;
}

export interface OrcaCommitAIModelsResult {
	agent_id: string;
	default_model_id: string;
	supports_discovery: boolean;
	models: OrcaCommitAIModel[];
	selected_model: string;
	thinking_level?: string;
	live_discovered: boolean;
	discovery_error?: string;
}

export interface OrcaCommitAISummary {
	configured: boolean;
	enabled: boolean;
	agent_id: string;
	agent_label: string;
	model_id?: string;
	thinking_level?: string;
	has_custom_prompt: boolean;
	repo_count: number;
	repo_override_count: number;
}

export interface OrcaCommitAIAgentOption {
	id: string;
	label: string;
	binary?: string;
	installed: boolean;
	path?: string;
	supports_tui: boolean;
}

export interface OrcaCommitTemplatePreset {
	id: string;
	label: string;
	description: string;
	prompt: string;
}

export interface OrcaCommitAIConfig {
	db_exists: boolean;
	db_path: string;
	profile_id: string;
	orca_installed: boolean;
	orca_running: boolean;
	orca_mode: string;
	enabled: boolean;
	agent_id: string;
	agent_label: string;
	model_id: string;
	thinking_level: string;
	custom_agent_command: string;
	custom_prompt: string;
	command_input_template: string;
	default_tui_agent: string;
	repo_count: number;
	repo_override_count: number;
	selected_models_by_agent: Record<string, string>;
	selected_thinking_by_model: Record<string, string>;
	cached_models_by_agent: Record<string, OrcaCommitAIModel[]>;
	available_agents: OrcaCommitAIAgentOption[];
	template_presets: OrcaCommitTemplatePreset[];
}

export interface OrcaCommitAISavePayload {
	enabled: boolean;
	agent_id: string;
	model_id: string;
	thinking_level: string;
	discovered_models?: OrcaCommitAIModel[];
	custom_agent_command: string;
	custom_prompt: string;
	update_default_tui_agent: boolean;
	clear_repo_overrides: boolean;
}

export interface OrcaCommitAISyncSide {
	enabled: boolean;
	agent_id: string;
	model_id: string;
	thinking_level: string;
	custom_prompt: string;
	custom_agent_command: string;
	repo_override_count: number;
}

export interface OrcaCommitAISyncStatus {
	managed: boolean;
	in_sync: boolean;
	state: 'unmanaged' | 'synced' | 'drift' | 'blocked' | 'conflict' | 'error';
	drift?: string[];
	desired?: OrcaCommitAISyncSide;
	actual?: OrcaCommitAISyncSide;
	last_applied_at_ms?: number;
	last_error?: string;
	profile_id: string;
	db_path: string;
	cloud_linked: boolean;
	orca_running: boolean;
	orca_desktop_open: boolean;
	settings_revision: number;
	settings_updated_at_ms: number;
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
	commit_ai?: OrcaCommitAISummary;
	sync_status?: OrcaCommitAISyncStatus;
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
	service_url?: string;
	http_host_header?: string;
	no_tls_verify?: boolean;
}

export interface CFProfileInfo {
	name: string;
	auth_mode: 'token' | 'cert' | string;
	tunnel_name?: string;
	tunnel_id?: string;
	account_tag?: string;
	has_cert: boolean;
	has_credentials: boolean;
	has_token: boolean;
	active: boolean;
	enabled: boolean;
	service_unit: string;
	config_path?: string;
	route_count: number;
	created_at?: string;
}

export interface TunnelStatus {
	installed: boolean;
	binary: string;
	active: boolean;
	service_unit_ready?: boolean;
	profiles: string[];
	profile_details?: CFProfileInfo[];
	exposures: Exposure[];
	detected_local_certs?: string[];
}

export interface CFExposeResult {
	status: string;
	profile: string;
	public_host: string;
	service_url: string;
	dns_warning?: string;
}

export interface CFLoginSession {
	session_id: string;
	profile: string;
	tunnel_name?: string;
	status: 'starting' | 'waiting_browser' | 'completed' | 'failed' | 'cancelled' | string;
	auth_url?: string;
	message?: string;
	error?: string;
	result?: CFProfileInfo;
	expires_at: string;
}

export type ServerMode = 'local' | 'edge' | 'vps';

export interface ModeResponse {
	mode: ServerMode;
	is_set: boolean;
	public_ip: string;
	netbird_ip: string;
	has_local_pdns: boolean;
	reachable: boolean;
}

export interface EdgeConfig {
	mode?: ServerMode;
	configured: boolean;
	vps_netbird_ip: string;
	vps_public_ip: string;
	api_key: string;
	pdns_port: string;
	agent_port: string;
	local_netbird_ip: string;
	reachable: boolean;
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

export interface AddPDNSZoneResponse {
	status: string;
	zone: string;
	nameservers: string[];
	zone_ip?: string;
	vhost_status?: 'created' | 'exists' | 'skipped' | 'error' | string;
	vhost_created?: boolean;
	vhost_root?: string;
	vhost_url?: string;
	edge_exposed?: boolean;
	vhost_message?: string;
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

export interface MailPortStatus {
	port: number;
	label: string;
	open: boolean;
	local?: boolean;
	public?: boolean;
	external_checked?: boolean;
	internal_only?: boolean;
}

export interface MailStatus {
	mode?: ServerMode;
	behind_nat?: boolean;
	stalwart_active: boolean;
	stalwart_ports?: MailPortStatus[];
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
	mode?: ServerMode;
	local_pdns?: boolean;
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
	verification_triggered?: boolean;
}

export interface ResendSyncResult {
	domain: string;
	domain_id?: string;
	domain_status?: string;
	verification_triggered?: boolean;
	records: ManagedRelayRecord[];
	skipped?: string[];
	report: RelayDNSReport;
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

export interface PostgresRoleInfo {
	name: string;
	superuser: boolean;
	can_login: boolean;
	create_db?: boolean;
	create_role?: boolean;
	replication?: boolean;
	bypass_rls?: boolean;
	conn_limit?: number;
	databases?: string[];
}

export interface PostgresInspectResult {
	version: string;
	port: number;
	databases: { name: string; owner: string; size: string }[];
	roles: PostgresRoleInfo[];
	connections: { pid: number; database: string; user: string; client_addr: string; state: string; query: string }[];
}

export interface PostgresCreateDBPayload {
	version: string;
	database: string;
	owner: string;
	password?: string;
	sudo_pass?: string;
}

export interface PostgresSaveRolePayload {
	version: string;
	role: string;
	password?: string;
	can_login: boolean;
	superuser: boolean;
	create_db: boolean;
	create_role: boolean;
	replication: boolean;
	bypass_rls: boolean;
	conn_limit: number;
	grant_db?: string;
	sudo_pass?: string;
}

export interface PostgresDeleteRolePayload {
	version: string;
	role: string;
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

export interface PHPInspectResult {
	target_version: string;
	available_versions: string[];
	cli_binary: string;
	cli_version: string;
	default_cli_binary: string;
	default_cli_version: string;
	sapi: string;
	zend_version: string;
	fpm_binary: string;
	fpm_version: string;
	composer_version: string;
	loaded_ini: string;
	additional_ini_files: string[];
	extension_dir: string;
	include_path: string;
	memory_limit: string;
	max_execution_time: string;
	upload_max_filesize: string;
	post_max_size: string;
	timezone: string;
	opcache_enabled: boolean;
	opcache_cli_enabled: boolean;
	disabled_functions: string[];
	extensions: string[];
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

export interface DockerConfig {
	config_path: string;
	service_unit: string;
	installed: boolean;
	active: boolean;
	docker_version?: string;
	compose_version?: string;
	data_root: string;
	log_driver: string;
	log_max_size: string;
	log_max_file: number;
	live_restore: boolean;
	no_new_privileges: boolean;
	ipv6: boolean;
	userland_proxy: boolean;
	dns?: string[];
	registry_mirrors?: string[];
	default_address_pool_base?: string;
	default_address_pool_size?: number;
	docker_group_user?: string;
	add_user_to_docker_group: boolean;
	docker_group_members?: string[];
	restart?: boolean;
}

export interface DockerInstallOptions {
	source?: 'official' | 'distro' | string;
	add_user_to_group: boolean;
	enable_service: boolean;
	daemon_config?: Partial<DockerConfig>;
}

export type ServiceConfigKey = 'redis' | 'postgresql' | 'php-fpm' | 'nginx' | 'docker';

export interface TaskEvent {
	seq?: number;
	task_id: number;
	type: 'log' | 'start' | 'finish' | 'ping';
	message: string;
	status?: string;
	replay?: boolean;
	instance_id?: string;
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
	log_output?: string;
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

export interface InstallItemPayload {
	component: string;
	version?: string;
}

export interface SetupComponentStatus {
	component: string;
	installed: boolean;
	version?: string;
}

export interface FileRootInfo {
	name: string;
	path: string;
	kind: 'webroot' | 'vhost' | 'home' | 'drive' | 'custom' | string;
	domain?: string;
}

export interface FileEntry {
	name: string;
	path: string;
	is_dir: boolean;
	is_symlink?: boolean;
	symlink_target?: string;
	size: number;
	mode: string;
	perm_text: string;
	owner: string;
	group: string;
	mod_time: string;
	ext?: string;
	editable: boolean;
	is_image: boolean;
	is_archive: boolean;
}

export interface FileListResult {
	path: string;
	parent?: string;
	can_go_up: boolean;
	roots: FileRootInfo[];
	entries: FileEntry[];
	total_dirs: number;
	total_files: number;
	total_size: number;
}

export interface FileContentResult {
	path: string;
	name: string;
	content: string;
	size: number;
	mode: string;
	mod_time: string;
}

export interface TerminalTargetUser {
	username: string;
	uid: number;
	gid: number;
	home_dir: string;
	shell: string;
	is_root: boolean;
}

export interface TerminalInfo {
	hostname: string;
	os_name: string;
	user: string;
	default_user: string;
	available_users: TerminalTargetUser[];
	is_daemon_root: boolean;
	shell: string;
	available_shells: string[];
	home_dir: string;
	server_mode?: string;
	active_sessions: number;
	max_sessions: number;
	idle_timeout_seconds: number;
	is_https: boolean;
	root_unlocked: boolean;
	root_unlock_expires_at?: string;
	step_up_always_required: boolean;
}

export interface TerminalUnlockResponse {
	status: string;
	token: string;
	expires_in: number;
	user: string;
	is_root: boolean;
	root_unlocked: boolean;
	root_unlock_expires_at?: string;
}

export interface TerminalAuditRecord {
	id: number;
	session_id: string;
	panel_user: string;
	target_user: string;
	shell: string;
	cwd: string;
	client_ip: string;
	status: string;
	exit_code: number;
	started_at: string;
	ended_at?: string;
}

export interface PanelDomainStatus {
	domain: string;
	bound?: boolean;
	domain_url?: string;
	detected_proxy_domain?: string;
	detected_proxy_url?: string;
	detected_proxy_ssl_provider?: string;
	detected_proxy_exposed_to?: string;
	ssl_enabled: boolean;
	block_direct_ip: boolean;
	ssl_provider?: string;
	ssl_expires_at?: string;
	ssl_days_left?: number;
	panel_ssl_enabled: boolean;
	panel_ssl_expires_at?: string;
	panel_ssl_days_left?: number;
	panel_port: number;
	entrance_path: string;
	public_ip?: string;
	server_mode: string;
	edge_configured?: boolean;
	available_domains: string[];
}

export interface BindPanelDomainPayload {
	domain: string;
	ssl_mode: 'letsencrypt' | 'edge' | 'local' | 'external' | 'none';
	email?: string;
	block_direct_ip?: boolean;
	sync_dns?: boolean;
	sudo_pass?: string;
}

export interface BindPanelDomainResponse {
	status: 'started' | 'bound' | 'sudo_required' | string;
	task_id?: number;
	domain?: string;
	target_url?: string;
	block_direct_ip?: boolean;
	message?: string;
}

export type PortScopeStatus = 'open' | 'blocked' | 'closed' | 'internal' | 'na' | 'unverified';
export type PortColorTone = 'green' | 'yellow' | 'red' | 'neutral';

export interface PortScopeDetail {
	status: PortScopeStatus;
	color: PortColorTone;
	label: string;
	target?: string;
	detail?: string;
	checked: boolean;
}

export interface PortEntry {
	id: string;
	port: number;
	protocol: string;
	service: string;
	name: string;
	category: 'system' | 'web' | 'database' | 'dns' | 'mail' | string;
	category_label: string;
	installed: boolean;
	expected_scope: string;
	bind_addresses?: string[];
	config_hint?: string;
	overall_status: 'open' | 'blocked' | 'closed' | 'internal' | 'not_installed' | string;
	overall_color: PortColorTone;
	overall_label: string;
	local: PortScopeDetail;
	vpn: PortScopeDetail;
	vps: PortScopeDetail;
	description: string;
	recommendation?: string;
}

export interface PortFirewallInfo {
	engine: 'ufw' | 'firewalld' | 'none' | string;
	active: boolean;
	default_incoming?: string;
	summary: string;
	rules_checked: boolean;
	rules_error?: string;
}

export interface PortNetworkContext {
	mode: ServerMode;
	local_ip: string;
	lan_ip?: string;
	vpn_ip?: string;
	vps_vpn_ip?: string;
	vps_public_ip?: string;
	netbird_active: boolean;
	firewall: PortFirewallInfo;
}

export interface PortSummaryCounts {
	total: number;
	installed: number;
	green: number;
	yellow: number;
	red: number;
	neutral: number;
	not_installed: number;
}

export interface PortStatusReport {
	checked_at: string;
	network: PortNetworkContext;
	summary: PortSummaryCounts;
	ports: PortEntry[];
}

export interface ComposeEngineInfo {
	docker_installed: boolean;
	docker_version?: string;
	compose_installed: boolean;
	compose_version?: string;
	compose_major: number;
	is_compose_v5: boolean;
	daemon_running: boolean;
	daemon_error?: string;
	api_version?: string;
	docker_root_dir?: string;
	needs_sudo: boolean;
}

export interface ComposePortBinding {
	service?: string;
	host_ip: string;
	host_port: number;
	container_port: number;
	protocol: string;
	exposed_public: boolean;
	sensitive_port: boolean;
	summary: string;
}

export interface ComposeServiceSpec {
	name: string;
	image?: string;
	build?: string;
	container_name?: string;
	restart?: string;
	ports?: ComposePortBinding[];
	profiles?: string[];
	depends_on?: string[];
	volumes_count: number;
	has_healthcheck: boolean;
	is_job?: boolean;
	schedule_cron?: string;
}

export interface ComposeContainerStatus {
	id: string;
	name: string;
	service: string;
	image: string;
	state: string;
	status: string;
	health?: string;
	exit_code: number;
	restarts?: number;
	command?: string;
	ports?: ComposePortBinding[];
	cpu_perc?: string;
	mem_usage?: string;
	mem_perc?: string;
	net_io?: string;
	block_io?: string;
	pids?: string;
}

export interface ComposeLinkedVHost {
	domain: string;
	proxy_port: number;
	ssl_enabled: boolean;
	exposed_to?: string;
}

export interface ComposeProject {
	name: string;
	dir_path: string;
	compose_file: string;
	config_files?: string[];
	description?: string;
	status: 'running' | 'partial' | 'exited' | 'stopped' | 'missing' | string;
	raw_status?: string;
	source: 'registered' | 'discovered' | 'both' | string;
	registered: boolean;
	file_exists: boolean;
	has_env_file: boolean;
	services_count: number;
	running_count: number;
	active_profiles?: string[];
	available_profiles?: string[];
	has_jobs: boolean;
	exposed_ports?: ComposePortBinding[];
	security_warnings?: string[];
	defined_services?: ComposeServiceSpec[];
	containers?: ComposeContainerStatus[];
	linked_vhosts?: ComposeLinkedVHost[];
	parse_error?: string;
	created_at?: string;
	updated_at?: string;
}

export interface ComposeProjectFiles {
	project_name: string;
	dir_path: string;
	compose_file: string;
	compose_content: string;
	env_content: string;
	has_env_file: boolean;
	env_keys?: string[];
}

export interface ComposeTemplateSpec {
	id: string;
	name: string;
	category: string;
	description: string;
	default_port?: number;
	compose_content: string;
	env_content: string;
}

export interface ComposeCreatePayload {
	name: string;
	dir_path: string;
	compose_file?: string;
	description?: string;
	profiles?: string[];
	template?: string;
	compose_content?: string;
	env_content?: string;
	overwrite?: boolean;
	start_after?: boolean;
}

export interface ComposeActionPayload {
	action: 'up' | 'down' | 'start' | 'stop' | 'restart' | 'pull' | 'build' | 'run-job';
	service?: string;
	profiles?: string[];
	build?: boolean;
	force_recreate?: boolean;
	remove_orphans?: boolean;
	remove_volumes?: boolean;
	no_cache?: boolean;
	sudo_pass?: string;
}

export interface ComposeExposePayload {
	domain: string;
	proxy_port: number;
	ssl_enabled?: boolean;
	expose_edge?: boolean;
	edge_domain?: string;
	edge_ssl?: boolean;
	add_hosts?: boolean;
}

export interface ComposeDiskUsageItem {
	type: string;
	total_count: string;
	active: string;
	size: string;
	reclaimable: string;
}

export interface ComposeDiskUsageSummary {
	items: ComposeDiskUsageItem[];
}

export interface ComposeStatusReport {
	engine: ComposeEngineInfo;
	projects: ComposeProject[];
	templates: ComposeTemplateSpec[];
	total: number;
	running: number;
	partial: number;
	stopped: number;
	warnings: number;
}


