package serviceconfig

import (
	"strings"
	"testing"
)

func TestParseAndApplyRedisConfig(t *testing.T) {
	raw := `# Redis configuration
daemonize yes
supervised no
port 6379
bind 127.0.0.1 -::1
protected-mode yes
requirepass secret123
maxmemory 256mb
maxmemory-policy allkeys-lru
save 3600 1
save 300 100
appendonly no
maxclients 5000
databases 16
`
	parsed := ParseRedisConfig(raw)
	if parsed.Port != 6379 || parsed.Bind != "127.0.0.1 -::1" || !parsed.HasPassword {
		t.Fatalf("unexpected parsed redis config: %+v", parsed)
	}
	if parsed.PersistenceMode != "rdb" {
		t.Fatalf("expected persistence rdb, got %q", parsed.PersistenceMode)
	}

	// Update to AOF + clear password + custom memory
	next := parsed
	next.Port = 6380
	next.Bind = "0.0.0.0"
	next.ProtectedMode = "no"
	next.ClearPassword = true
	next.MaxMemory = "1GB"
	next.MaxMemoryPolicy = "noeviction"
	next.PersistenceMode = "aof"
	next.Databases = 32

	if err := ValidateRedisConfig(next); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	updatedLines := ApplyRedisConfigLines(strings.Split(strings.TrimRight(raw, "\n"), "\n"), next)
	updated := strings.Join(updatedLines, "\n")

	if !strings.Contains(updated, "daemonize no") || !strings.Contains(updated, "supervised systemd") {
		t.Fatalf("systemd compatibility directives missing:\n%s", updated)
	}
	if strings.Contains(updated, "requirepass") {
		t.Fatalf("requirepass should have been removed:\n%s", updated)
	}
	if !strings.Contains(updated, `save ""`) || !strings.Contains(updated, "appendonly yes") {
		t.Fatalf("expected AOF persistence with disabled RDB:\n%s", updated)
	}

	reParsed := ParseRedisConfig(updated)
	if reParsed.Port != 6380 || reParsed.HasPassword || reParsed.PersistenceMode != "aof" || reParsed.MaxMemory != "1gb" {
		t.Fatalf("unexpected re-parsed redis config: %+v", reParsed)
	}
}

func TestRedisValidationRejectsInvalid(t *testing.T) {
	bad := RedisConfig{
		Port:            70000,
		Bind:            "127.0.0.1",
		ProtectedMode:   "yes",
		MaxMemory:       "256mb",
		MaxMemoryPolicy: "allkeys-lru",
		PersistenceMode: "rdb",
		MaxClients:      100,
		Databases:       16,
	}
	if err := ValidateRedisConfig(bad); err == nil {
		t.Fatal("expected invalid port error")
	}

	bad.Port = 6379
	bad.Bind = "127.0.0.1\nevil"
	if err := ValidateRedisConfig(bad); err == nil {
		t.Fatal("expected newline injection error")
	}
}

func TestParseAndApplyPostgresConfig(t *testing.T) {
	conf := `# PostgreSQL configuration file
#listen_addresses = 'localhost'		# what IP address(es) to listen on
port = 5432				# (change requires restart)
max_connections = 100
shared_buffers = 128MB
#effective_cache_size = 4GB
#work_mem = 4MB
#maintenance_work_mem = 64MB
password_encryption = scram-sha-256
timezone = 'UTC'
#log_min_duration_statement = -1
`
	hba := `# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             postgres                                peer
host    all             all             127.0.0.1/32            scram-sha-256
`
	parsed := ParsePostgresConfig(conf, hba)
	if parsed.Port != 5432 || parsed.ListenAddresses != "localhost" || parsed.RemoteAccessMode != "local" {
		t.Fatalf("unexpected parsed postgres config: %+v", parsed)
	}

	next := parsed
	next.ListenAddresses = "*"
	next.MaxConnections = 200
	next.SharedBuffers = "512MB"
	next.EffectiveCacheSize = "2GB"
	next.WorkMem = "16MB"
	next.Timezone = "Asia/Jakarta"
	next.LogMinDurationStatement = 500
	next.RemoteAccessMode = "private_vpn"

	if err := ValidatePostgresConfig(next); err != nil {
		t.Fatalf("expected valid postgres config, got: %v", err)
	}

	newConf := strings.Join(ApplyPostgresConfLines(strings.Split(strings.TrimRight(conf, "\n"), "\n"), next), "\n")
	newHba := ApplyPostgresHba(hba, next.RemoteAccessMode, next.PasswordEncryption)

	reParsed := ParsePostgresConfig(newConf, newHba)
	if reParsed.ListenAddresses != "*" || reParsed.MaxConnections != 200 || reParsed.SharedBuffers != "512MB" ||
		reParsed.Timezone != "Asia/Jakarta" || reParsed.LogMinDurationStatement != 500 || reParsed.RemoteAccessMode != "private_vpn" {
		t.Fatalf("unexpected re-parsed postgres config: %+v", reParsed)
	}
	if !strings.Contains(newHba, "100.64.0.0/10") || !strings.Contains(newHba, pgHbaBlockBegin) {
		t.Fatalf("expected NetBird/LAN block in pg_hba.conf:\n%s", newHba)
	}

	// Revert to local: managed block should disappear cleanly without hurting existing rules
	localHba := ApplyPostgresHba(newHba, "local", "scram-sha-256")
	if strings.Contains(localHba, pgHbaBlockBegin) || strings.Contains(localHba, "100.64.0.0/10") {
		t.Fatalf("managed block should be removed in local mode:\n%s", localHba)
	}
	if !strings.Contains(localHba, "127.0.0.1/32") {
		t.Fatalf("existing local rule should be preserved:\n%s", localHba)
	}
}

func TestParseAndApplyPHPConfig(t *testing.T) {
	baseIni := `[PHP]
memory_limit = 128M
upload_max_filesize = 2M
post_max_size = 8M
max_execution_time = 30
max_input_vars = 1000
;date.timezone =
display_errors = Off
[opcache]
opcache.enable=1
opcache.memory_consumption=128
`
	dropIn := `; Managed by wdev
memory_limit = 512M
date.timezone = Asia/Jakarta
`
	pool := `[www]
user = www-data
group = www-data
pm = dynamic
pm.max_children = 10
pm.start_servers = 3
pm.min_spare_servers = 2
pm.max_spare_servers = 5
;pm.max_requests = 0
`
	parsed := ParsePHPIniAndPool([]string{baseIni, dropIn}, pool)
	if parsed.MemoryLimit != "512M" || parsed.Timezone != "Asia/Jakarta" || parsed.PM != "dynamic" || parsed.PMMaxChildren != 10 {
		t.Fatalf("unexpected parsed php config: %+v", parsed)
	}

	next := parsed
	next.UploadMaxFilesize = "128M"
	next.PostMaxSize = "128M"
	next.DisplayErrors = true
	next.PMMaxChildren = 20
	next.PMStartServers = 4
	next.PMMinSpareServers = 2
	next.PMMaxSpareServers = 6
	next.PMMaxRequests = 500

	if err := ValidatePHPConfig(next); err != nil {
		t.Fatalf("expected valid php config, got: %v", err)
	}

	renderedIni := RenderWdevPHPIni(next)
	updatedPool := strings.Join(ApplyPHPPoolLines(strings.Split(strings.TrimRight(pool, "\n"), "\n"), next), "\n")

	reParsed := ParsePHPIniAndPool([]string{baseIni, renderedIni}, updatedPool)
	if reParsed.UploadMaxFilesize != "128M" || !reParsed.DisplayErrors || reParsed.PMMaxChildren != 20 || reParsed.PMMaxRequests != 500 {
		t.Fatalf("unexpected re-parsed php config: %+v", reParsed)
	}

	// Validate dynamic spare bounds check
	invalid := next
	invalid.PMStartServers = 10
	invalid.PMMaxSpareServers = 6
	if err := ValidatePHPConfig(invalid); err == nil {
		t.Fatal("expected pm.start_servers > pm.max_spare_servers error")
	}
}

func TestParseAndApplyNginxConfig(t *testing.T) {
	raw := `user  www-data;
worker_processes  auto;

events {
    worker_connections  1024;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;
    sendfile        on;
    keepalive_timeout  65;
    gzip  on;
    include /etc/nginx/conf.d/*.conf;
}
`
	parsed := ParseNginxConfig(raw)
	if parsed.WorkerProcesses != "auto" || parsed.WorkerConnections != 1024 || parsed.KeepaliveTimeout != 65 || !parsed.Gzip {
		t.Fatalf("unexpected parsed nginx config: %+v", parsed)
	}

	next := parsed
	next.WorkerProcesses = "4"
	next.WorkerConnections = 2048
	next.ClientMaxBodySize = "256M"
	next.KeepaliveTimeout = 90
	next.Gzip = true
	next.ServerTokens = false

	if err := ValidateNginxConfig(next); err != nil {
		t.Fatalf("expected valid nginx config, got: %v", err)
	}

	updated := strings.Join(ApplyNginxConfLines(strings.Split(strings.TrimRight(raw, "\n"), "\n"), next), "\n")
	if !strings.Contains(updated, "worker_processes 4;") ||
		!strings.Contains(updated, "worker_connections 2048;") ||
		!strings.Contains(updated, "client_max_body_size 256M;") ||
		!strings.Contains(updated, "server_tokens off;") {
		t.Fatalf("expected updated nginx directives:\n%s", updated)
	}

	reParsed := ParseNginxConfig(updated)
	if reParsed.WorkerProcesses != "4" || reParsed.WorkerConnections != 2048 || reParsed.ClientMaxBodySize != "256M" || reParsed.ServerTokens {
		t.Fatalf("unexpected re-parsed nginx config: %+v", reParsed)
	}
}

func TestParseRedisInfoAndPGIdent(t *testing.T) {
	info := `# Server
redis_version:7.4.2
redis_mode:standalone
uptime_in_seconds:1200

# Clients
connected_clients:3

# Memory
used_memory_human:2.15M
used_memory_peak_human:4.00M
maxmemory_human:256.00M

# Stats
total_commands_processed:5000
keyspace_hits:80
keyspace_misses:20

# Keyspace
db0:keys=42,expires=5,avg_ttl=12000
db2:keys=7,expires=0,avg_ttl=0
`
	parsed := ParseRedisInfo(info)
	if parsed.Version != "7.4.2" || parsed.ConnectedClients != 3 || parsed.UsedMemoryHuman != "2.15M" || len(parsed.Keyspaces) != 2 {
		t.Fatalf("unexpected parsed redis info: %+v", parsed)
	}
	if parsed.HitRatePercent != 80 {
		t.Fatalf("expected hit rate 80%%, got %.2f", parsed.HitRatePercent)
	}
	if parsed.Keyspaces[0].DB != "db0" || parsed.Keyspaces[0].Keys != 42 {
		t.Fatalf("unexpected db0 keyspace: %+v", parsed.Keyspaces[0])
	}

	if err := ValidatePGIdentifier("app_db_1", "db"); err != nil {
		t.Fatalf("expected valid PG identifier, got: %v", err)
	}
	if err := ValidatePGIdentifier(`bad"; DROP TABLE`, "db"); err == nil {
		t.Fatal("expected SQL injection identifier rejection")
	}
}

func TestParsePostgresRolesAndBuildRoleSQL(t *testing.T) {
	raw := "postgres\tt\tt\tt\tt\tt\tt\t-1\tpostgres,app_db\n" +
		"app_user\tf\tt\tt\tf\tf\tf\t25\tapp_db\n" +
		"readonly_grp\tf\tf\tf\tf\tf\tf\t-1\t\n"

	roles := ParsePostgresRoles(raw)
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d: %+v", len(roles), roles)
	}
	if roles[0].Name != "postgres" || !roles[0].Superuser || !roles[0].CanLogin || len(roles[0].Databases) != 2 {
		t.Fatalf("unexpected postgres role: %+v", roles[0])
	}
	if roles[1].Name != "app_user" || roles[1].Superuser || !roles[1].CanLogin || !roles[1].CreateDB || roles[1].ConnLimit != 25 {
		t.Fatalf("unexpected app_user role: %+v", roles[1])
	}
	if roles[2].CanLogin || roles[2].ConnLimit != -1 {
		t.Fatalf("unexpected readonly_grp role: %+v", roles[2])
	}

	// CREATE LOGIN role requires password
	if _, err := BuildPostgresRoleSQL(PostgresSaveRoleRequest{Role: "new_user", CanLogin: true, ConnLimit: -1}, false); err == nil {
		t.Fatal("expected error when creating LOGIN role without password")
	}

	sqlCreate, err := BuildPostgresRoleSQL(PostgresSaveRoleRequest{
		Role:       "new_user",
		Password:   "P@ss'123!",
		CanLogin:   true,
		CreateDB:   true,
		CreateRole: false,
		ConnLimit:  10,
	}, false)
	if err != nil {
		t.Fatalf("BuildPostgresRoleSQL create failed: %v", err)
	}
	wantSubstrs := []string{
		`CREATE ROLE "new_user" WITH`,
		"LOGIN NOSUPERUSER CREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 10",
		`PASSWORD 'P@ss''123!'`,
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(sqlCreate, s) {
			t.Fatalf("SQL %q missing %q", sqlCreate, s)
		}
	}

	// Protecting postgres system role from losing superuser/login
	if _, err := BuildPostgresRoleSQL(PostgresSaveRoleRequest{Role: "postgres", CanLogin: true, Superuser: false, ConnLimit: -1}, true); err == nil {
		t.Fatal("expected error when revoking superuser from postgres role")
	}

	// Alter role password optional
	sqlAlter, err := BuildPostgresRoleSQL(PostgresSaveRoleRequest{
		Role:      "app_user",
		CanLogin:  true,
		CreateDB:  true,
		BypassRLS: true,
		ConnLimit: -1,
	}, true)
	if err != nil {
		t.Fatalf("BuildPostgresRoleSQL alter failed: %v", err)
	}
	if !strings.Contains(sqlAlter, `ALTER ROLE "app_user" WITH`) || !strings.Contains(sqlAlter, "BYPASSRLS") || strings.Contains(sqlAlter, "PASSWORD") {
		t.Fatalf("unexpected alter SQL: %s", sqlAlter)
	}
}

func TestParsePHPInspectJSONAndModulesFallback(t *testing.T) {
	raw := `{
		"cli_version": "8.4.5",
		"sapi": "cli",
		"zend_version": "4.4.5",
		"loaded_ini": "/etc/php/8.4/cli/php.ini",
		"additional_ini_files": ["/etc/php/8.4/cli/conf.d/10-opcache.ini", "/etc/php/8.4/cli/conf.d/99-wdev.ini"],
		"extension_dir": "/usr/lib/php/20240924",
		"include_path": ".:/usr/share/php",
		"memory_limit": "512M",
		"max_execution_time": "0",
		"upload_max_filesize": "64M",
		"post_max_size": "64M",
		"timezone": "Asia/Jakarta",
		"opcache_enabled": true,
		"opcache_cli_enabled": false,
		"disabled_functions": [],
		"extensions": ["Core", "curl", "mbstring", "pdo_pgsql", "Zend OPcache"]
	}`
	parsed, err := ParsePHPInspectJSON(raw)
	if err != nil {
		t.Fatalf("ParsePHPInspectJSON failed: %v", err)
	}
	if parsed.CLIVersion != "8.4.5" || parsed.Timezone != "Asia/Jakarta" || !parsed.OpcacheEnabled || len(parsed.Extensions) != 5 {
		t.Fatalf("unexpected parsed PHPInspectResult: %+v", parsed)
	}
	if len(parsed.AdditionalINIFiles) != 2 || parsed.LoadedINI != "/etc/php/8.4/cli/php.ini" {
		t.Fatalf("unexpected INI info: %+v", parsed)
	}

	mods := parsePHPModulesOutput("[PHP Modules]\nxml\ncurl\nCore\n\n[Zend Modules]\nZend OPcache\n")
	if len(mods) != 4 || mods[0] != "Core" || mods[3] != "Zend OPcache" {
		t.Fatalf("unexpected fallback modules parse: %+v", mods)
	}
	if v := parseVersionBanner("PHP 8.4.5 (fpm-fcgi) (built: Mar 12 2025)"); v != "8.4.5" {
		t.Fatalf("unexpected FPM banner version: %q", v)
	}
}

func TestDetectServicePresets(t *testing.T) {
	if got := DetectNginxPreset(NginxConfig{WorkerProcesses: "auto", WorkerConnections: 1024, ClientMaxBodySize: "64M", KeepaliveTimeout: 65, Gzip: true, ServerTokens: true}); got != "Dev Standar (64M)" {
		t.Fatalf("nginx dev preset = %q", got)
	}
	if got := DetectNginxPreset(NginxConfig{WorkerProcesses: "auto", WorkerConnections: 4096, ClientMaxBodySize: "64m", KeepaliveTimeout: 65, Gzip: true, ServerTokens: false}); got != "Production Hardened" {
		t.Fatalf("nginx prod preset = %q", got)
	}
	if got := DetectNginxPreset(NginxConfig{WorkerProcesses: "2", WorkerConnections: 1024, ClientMaxBodySize: "64M", KeepaliveTimeout: 65, Gzip: true}); got != PresetCustom {
		t.Fatalf("nginx custom = %q, want Custom", got)
	}

	if got := DetectPHPPreset(PHPConfig{Timezone: "Asia/Jakarta", MemoryLimit: "512M", UploadMaxFilesize: "128M", PostMaxSize: "128M", MaxExecutionTime: 300, MaxInputVars: 5000, DisplayErrors: true, OpcacheEnable: true, OpcacheMemoryConsumption: 128, PM: "ondemand", PMMaxChildren: 10, PMMaxRequests: 500}); got != "Development (Debug On)" {
		t.Fatalf("php dev preset = %q", got)
	}
	if got := DetectPHPPreset(PHPConfig{Timezone: "Asia/Jakarta", MemoryLimit: "256M"}); got != PresetCustom {
		t.Fatalf("php custom = %q, want Custom", got)
	}

	if got := DetectPostgresPreset(PostgresConfig{MaxConnections: 50, SharedBuffers: "128MB", EffectiveCacheSize: "512MB", WorkMem: "4MB", MaintenanceWorkMem: "64MB", LogMinDurationStatement: 250}); got != "Dev Hemat RAM" {
		t.Fatalf("pg dev preset = %q", got)
	}
	if got := DetectPostgresPreset(PostgresConfig{MaxConnections: 150, SharedBuffers: "512MB", EffectiveCacheSize: "2GB", WorkMem: "16MB", MaintenanceWorkMem: "128MB", Timezone: "Asia/Jakarta", LogMinDurationStatement: 500}); got != "Balanced (2-4GB RAM)" {
		t.Fatalf("pg balanced preset = %q", got)
	}
	if got := DetectPostgresPreset(PostgresConfig{MaxConnections: 100, SharedBuffers: "128MB"}); got != PresetCustom {
		t.Fatalf("pg custom = %q, want Custom", got)
	}

	if got := DetectRedisPreset(RedisConfig{Port: 6379, Bind: "127.0.0.1 -::1", ProtectedMode: "yes", MaxMemory: "256mb", MaxMemoryPolicy: "noeviction", PersistenceMode: "rdb"}); got != "Dev Lokal" {
		t.Fatalf("redis local preset = %q", got)
	}
	if got := DetectRedisPreset(RedisConfig{MaxMemory: "256mb", MaxMemoryPolicy: "allkeys-lru", PersistenceMode: "none"}); got != "Cache LRU (256MB)" {
		t.Fatalf("redis cache preset = %q", got)
	}
	if got := DetectRedisPreset(RedisConfig{Port: 6380, MaxMemory: "1gb", MaxMemoryPolicy: "noeviction", PersistenceMode: "rdb"}); got != PresetCustom {
		t.Fatalf("redis custom = %q, want Custom", got)
	}
}

func TestNormalizePostgresConfigAndFirewallPlans(t *testing.T) {
	cfgAll := PostgresConfig{RemoteAccessMode: "all", ListenAddresses: "localhost"}
	normalizePostgresConfig(&cfgAll)
	if cfgAll.ListenAddresses != "*" {
		t.Fatalf("mode all dengan localhost harus jadi '*', dapat %q", cfgAll.ListenAddresses)
	}

	cfgVPN := PostgresConfig{RemoteAccessMode: "private_vpn", ListenAddresses: "127.0.0.1"}
	normalizePostgresConfig(&cfgVPN)
	if cfgVPN.ListenAddresses != "*" {
		t.Fatalf("mode private_vpn dengan 127.0.0.1 harus jadi '*', dapat %q", cfgVPN.ListenAddresses)
	}

	cfgCustom := PostgresConfig{RemoteAccessMode: "all", ListenAddresses: "100.68.154.24"}
	normalizePostgresConfig(&cfgCustom)
	if cfgCustom.ListenAddresses != "100.68.154.24" {
		t.Fatalf("listen_addresses spesifik tidak boleh ditimpa, dapat %q", cfgCustom.ListenAddresses)
	}

	cfgLocal := PostgresConfig{RemoteAccessMode: "local", ListenAddresses: "*"}
	normalizePostgresConfig(&cfgLocal)
	if cfgLocal.ListenAddresses != "localhost" {
		t.Fatalf("mode local dengan '*' harus kembali ke 'localhost', dapat %q", cfgLocal.ListenAddresses)
	}

	planHasArg := func(steps []firewallStep, substr string) bool {
		for _, s := range steps {
			if strings.Contains(strings.Join(s.args, " "), substr) {
				return true
			}
		}
		return false
	}

	ufwAll := buildUFWPostgresPlan(5432, 5432, "all")
	if !planHasArg(ufwAll, "allow 5432/tcp") {
		t.Fatalf("UFW plan mode all harus membuka 5432/tcp: %+v", ufwAll)
	}

	ufwVPN := buildUFWPostgresPlan(5432, 5433, "private_vpn")
	if !planHasArg(ufwVPN, "delete allow 5432/tcp") || !planHasArg(ufwVPN, "allow from 100.64.0.0/10 to any port 5433 proto tcp") {
		t.Fatalf("UFW plan mode private_vpn harus bersihkan port lama & izinkan NetBird di port baru: %+v", ufwVPN)
	}

	ufwLocal := buildUFWPostgresPlan(5432, 5432, "local")
	if planHasArg(ufwLocal, "allow 5432/tcp comment") || planHasArg(ufwLocal, "from 100.64.0.0/10 to any port 5432 proto tcp comment") {
		t.Fatalf("UFW plan mode local tidak boleh menambah rule allow baru: %+v", ufwLocal)
	}

	fwAll := buildFirewalldPostgresPlan(5432, 5432, "all")
	if !planHasArg(fwAll, "--add-port=5432/tcp") {
		t.Fatalf("firewalld plan mode all harus menambah 5432/tcp: %+v", fwAll)
	}

	fwVPN := buildFirewalldPostgresPlan(5432, 5432, "private_vpn")
	if !planHasArg(fwVPN, `--add-rich-rule=rule family="ipv4" source address="100.64.0.0/10" port protocol="tcp" port="5432" accept`) {
		t.Fatalf("firewalld plan mode private_vpn harus menambah rich-rule 100.64.0.0/10: %+v", fwVPN)
	}
}
