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
