package edge

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ProvisionOptions mengatur pemasangan VPS Edge Gateway (native Go).
type ProvisionOptions struct {
	PublicIP  string
	NetbirdIP string
	APIKey    string
	PDNSPort  string
	AgentPort string
}

const pdnsSchema = `PRAGMA foreign_keys = 1;
CREATE TABLE IF NOT EXISTS domains (
  id INTEGER PRIMARY KEY, name VARCHAR(255) NOT NULL COLLATE NOCASE,
  master VARCHAR(128) DEFAULT NULL, last_check INTEGER DEFAULT NULL,
  type VARCHAR(8) NOT NULL, notified_serial INTEGER DEFAULT NULL,
  account VARCHAR(40) DEFAULT NULL, options VARCHAR(65535) DEFAULT NULL,
  catalog VARCHAR(255) DEFAULT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS name_index ON domains(name);
CREATE INDEX IF NOT EXISTS catalog_idx ON domains(catalog);
CREATE TABLE IF NOT EXISTS records (
  id INTEGER PRIMARY KEY, domain_id INTEGER DEFAULT NULL,
  name VARCHAR(255) DEFAULT NULL, type VARCHAR(10) DEFAULT NULL,
  content VARCHAR(65535) DEFAULT NULL, ttl INTEGER DEFAULT NULL,
  prio INTEGER DEFAULT NULL, disabled BOOLEAN DEFAULT 0,
  ordername VARCHAR(255), auth BOOL DEFAULT 1,
  FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS records_lookup_idx ON records(name, type);
CREATE INDEX IF NOT EXISTS records_lookup_id_idx ON records(domain_id, name, type);
CREATE INDEX IF NOT EXISTS records_order_idx ON records(domain_id, ordername);
CREATE TABLE IF NOT EXISTS supermasters (
  ip VARCHAR(64) NOT NULL, nameserver VARCHAR(255) NOT NULL COLLATE NOCASE,
  account VARCHAR(40) NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS ip_nameserver_pk ON supermasters(ip, nameserver);
CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY, domain_id INTEGER NOT NULL, name VARCHAR(255) NOT NULL,
  type VARCHAR(10) NOT NULL, modified_at INT NOT NULL, account VARCHAR(40) DEFAULT NULL,
  comment VARCHAR(65535) NOT NULL,
  FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS comments_idx ON comments(domain_id, name, type);
CREATE INDEX IF NOT EXISTS comments_order_idx ON comments (domain_id, modified_at);
CREATE TABLE IF NOT EXISTS domainmetadata (
  id INTEGER PRIMARY KEY, domain_id INT NOT NULL,
  kind VARCHAR(32) COLLATE NOCASE, content TEXT,
  FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS domainmetaidindex ON domainmetadata(domain_id);
CREATE TABLE IF NOT EXISTS cryptokeys (
  id INTEGER PRIMARY KEY, domain_id INT NOT NULL, flags INT NOT NULL,
  active BOOL, published BOOL DEFAULT 1, content TEXT,
  FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS domainidindex ON cryptokeys(domain_id);
CREATE TABLE IF NOT EXISTS tsigkeys (
  id INTEGER PRIMARY KEY, name VARCHAR(255) COLLATE NOCASE,
  algorithm VARCHAR(50) COLLATE NOCASE, secret VARCHAR(255)
);
CREATE UNIQUE INDEX IF NOT EXISTS namealgoindex ON tsigkeys(name, algorithm);
`

func command(ctx context.Context, cb func(string), name string, args ...string) error {
	return commandStdin(ctx, cb, "", name, args...)
}

func commandStdin(ctx context.Context, cb func(string), stdin string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 && cb != nil {
			for _, line := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
				if line != "" {
					cb(line)
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return cmd.Wait()
}

func detectDistro() (id string, family string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), `"'`)
		}
	}
	switch id {
	case "debian", "ubuntu", "pop", "linuxmint", "dietpi", "raspbian":
		return id, "debian"
	case "fedora", "rhel", "centos", "almalinux", "rocky", "ol":
		return id, "rhel"
	}
	return id, ""
}

// detectPublicIP hanya mengambil IPv4 agar tidak menghasilkan alamat ::1/2001:.
// resolveAPIKey memakai kunci yang diberikan, membaca kunci lama, atau membuat baru.
func resolveAPIKey(provided string) (string, error) {
	return resolveAPIKeyAt(provided, APIKeyFile)
}

func resolveAPIKeyAt(provided, keyFile string) (string, error) {
	if key := strings.TrimSpace(provided); key != "" {
		return key, nil
	}
	if data, err := os.ReadFile(keyFile); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key, nil
		}
	}
	out, err := exec.Command("openssl", "rand", "-hex", "24").Output()
	if err != nil {
		return "", fmt.Errorf("gagal membuat API key PowerDNS: %w", err)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", fmt.Errorf("API key PowerDNS kosong setelah digenerate")
	}
	if err := writeRootFile(keyFile, []byte(key), 0600); err != nil {
		return "", err
	}
	return key, nil
}

// DetectPublicIP mengekspos deteksi IP publik untuk paket internal lain.
func DetectPublicIP(ctx context.Context) string {
	return detectPublicIP(ctx)
}

func detectPublicIP(ctx context.Context) string {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", addr)
		},
	}
	client := &http.Client{Timeout: 6 * time.Second, Transport: transport}
	for _, u := range []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://ifconfig.me/ip",
	} {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return ip
		}
	}
	return ""
}

func detectNetbirdIP() string {
	out, err := exec.Command("ip", "-4", "addr", "show", "dev", "wt0").Output()
	if err != nil {
		return ""
	}
	for _, f := range strings.Fields(string(out)) {
		if strings.HasPrefix(f, "inet") || f == "" {
			continue
		}
		if i := strings.Index(f, "/"); i > 0 {
			return f[:i]
		}
	}
	return ""
}

func writeRootFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// Provision memasang VPS Edge Gateway (PowerDNS + Nginx + agent) tanpa skrip shell.
func Provision(ctx context.Context, cb func(string), opts ProvisionOptions) error {
	if cb == nil {
		cb = func(string) {}
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("provisioning harus dijalankan sebagai root")
	}
	if strings.TrimSpace(opts.PDNSPort) == "" {
		opts.PDNSPort = DefaultPDNSPort
	}
	if opts.AgentPort == "" {
		opts.AgentPort = DefaultAgentPort
	}

	id, family := detectDistro()
	if family == "" {
		return fmt.Errorf("distro '%s' belum didukung (butuh Debian/Ubuntu atau Fedora/RHEL)", id)
	}
	cb(fmt.Sprintf(">>> [1/7] Distro terdeteksi: %s (%s)", id, family))

	hasV6 := hasIPv6()
	cb(fmt.Sprintf(">>> [2/7] IPv6: %v", hasV6))
	if opts.PublicIP == "" {
		opts.PublicIP = detectPublicIP(ctx)
	}
	if opts.PublicIP == "" {
		return fmt.Errorf("IP publik VPS tidak terdeteksi; sertakan --public-ip")
	}
	cb("    IP publik: " + opts.PublicIP)

	if opts.NetbirdIP == "" {
		opts.NetbirdIP = detectNetbirdIP()
	}
	if opts.NetbirdIP == "" {
		cb("    [!] Interface Netbird (wt0) tidak aktif; memakai 0.0.0.0")
		opts.NetbirdIP = "0.0.0.0"
	} else {
		cb("    Netbird IP: " + opts.NetbirdIP)
	}

	// 3. Paket.
	cb(">>> [3/7] Memasang paket sistem...")
	if family == "debian" {
		_ = command(ctx, cb, "apt-get", "update", "-y")
		_ = os.MkdirAll(NginxAvailable, 0755)
		_ = os.MkdirAll(NginxEnabled, 0755)
		if !hasV6 {
			if data, err := os.ReadFile("/etc/nginx/sites-available/default"); err == nil {
				fixed := strings.ReplaceAll(string(data), "listen [::]:80", "#listen [::]:80")
				_ = os.WriteFile("/etc/nginx/sites-available/default", []byte(fixed), 0644)
			}
		}
		_ = commandStdin(ctx, cb, "postfix postfix/mailname string mail.wdev.local\npostfix postfix/main_mailer_type string \"Internet Site\"\n", "debconf-set-selections")
		args := []string{"install", "-y", "--no-install-recommends",
			"curl", "jq", "sqlite3", "nginx", "certbot", "python3-certbot-nginx", "python3",
			"pdns-server", "pdns-backend-sqlite3", "ufw", "rsyslog", "postfix"}
		if err := command(ctx, cb, "apt-get", args...); err != nil {
			_ = command(ctx, cb, "apt-get", "install", "-f", "-y")
		}
		_ = command(ctx, cb, "apt-get", "install", "-y", "libnginx-mod-stream")
	} else {
		_ = command(ctx, cb, "dnf", "install", "-y",
			"curl", "jq", "sqlite", "nginx", "certbot", "python3-certbot-nginx", "python3",
			"pdns", "pdns-backend-sqlite3", "firewalld", "rsyslog")
		_ = command(ctx, cb, "dnf", "install", "-y", "nginx-mod-stream")
	}
	_ = command(ctx, cb, "systemctl", "enable", "--now", "rsyslog")

	// 4. Port 53.
	cb(">>> [4/7] Membebaskan port 53...")
	for _, svc := range []string{"systemd-resolved", "bind9", "named", "dnsmasq"} {
		if exec.Command("systemctl", "is-active", "--quiet", svc).Run() == nil {
			_ = command(ctx, cb, "systemctl", "stop", svc)
			_ = command(ctx, cb, "systemctl", "disable", svc)
		}
	}
	_ = os.Remove("/etc/resolv.conf")
	_ = os.WriteFile("/etc/resolv.conf", []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644)

	// 5. PowerDNS.
	cb(">>> [5/7] Mengonfigurasi PowerDNS + SQLite3...")
	apiKey, err := resolveAPIKey(opts.APIKey)
	if err != nil {
		return err
	}
	opts.APIKey = apiKey
	if err := provisionPowerDNS(ctx, cb, opts, hasV6); err != nil {
		return err
	}

	// 6. Nginx, agent, systemd.
	cb(">>> [6/7] Memasang Edge Agent (Go) & Nginx...")
	if err := provisionAgent(ctx, cb, opts); err != nil {
		return err
	}

	// 6.1/6.2 mail cleanup + postfix alignment.
	// Jangan mematikan mail stack lokal bila mesin ini juga menjalankan wdev mail.
	localMailHost := false
	if _, err := os.Stat("/etc/wdev/mail-admin.cred"); err == nil {
		localMailHost = true
	}
	if !localMailHost {
		if exec.Command("systemctl", "cat", "stalwart.service").Run() == nil {
			cb("Menonaktifkan Stalwart lama di VPS (tidak dipakai gateway)...")
			_ = exec.Command("systemctl", "disable", "--now", "stalwart").Run()
		}
		_ = os.RemoveAll("/var/www/snappymail")
		_ = os.Remove("/tmp/snappymail.zip")
	} else {
		cb(" Mesin ini juga mail host lokal; Stalwart/SnappyMail dibiarkan aktif")
	}
	alignPostfix(cb)

	// 7. Firewall.
	cb(">>> [7/7] Mengonfigurasi firewall...")
	cb("    " + ensureEdgeFirewall(opts))

	// Metadata.
	if opts.APIKey == "" {
		return fmt.Errorf("API key PowerDNS kosong; edge.conf tidak ditulis")
	}
	conf := fmt.Sprintf("PUBLIC_IP=%s\nNETBIRD_IP=%s\nPDNS_API_PORT=%s\nAGENT_PORT=%s\nAPI_KEY=%s\n",
		opts.PublicIP, opts.NetbirdIP, opts.PDNSPort, opts.AgentPort, opts.APIKey)
	if err := writeRootFile(ConfigFile, []byte(conf), 0600); err != nil {
		return err
	}

	cb("")
	cb("==================================================================")
	cb(" Pemasangan VPS Edge Gateway Berhasil!")
	cb("==================================================================")
	cb(" IP Publik      : " + opts.PublicIP)
	cb(" Netbird IP     : " + opts.NetbirdIP)
	cb(" PowerDNS API   : " + opts.PDNSPort)
	cb(" Edge Agent     : " + opts.AgentPort)
	cb(" API Key        : " + opts.APIKey)
	cb("")
	cb("Hubungkan dari mesin lokal:")
	cb(fmt.Sprintf("  sudo wdev pdns connect %s %s %s", opts.NetbirdIP, opts.PublicIP, opts.APIKey))
	return nil
}

func provisionPowerDNS(ctx context.Context, cb func(string), opts ProvisionOptions, hasV6 bool) error {
	dbDir := "/var/lib/powerdns"
	dbFile := filepath.Join(dbDir, "pdns.sqlite3")
	_ = os.MkdirAll(dbDir, 0775)
	_ = os.MkdirAll(ConfigDir, 0755)
	_ = os.MkdirAll("/etc/powerdns/pdns.d", 0755)
	_ = exec.Command("chown", "-R", "pdns:pdns", dbDir).Run()

	if opts.APIKey == "" {
		return fmt.Errorf("API key PowerDNS kosong")
	}
	if err := writeRootFile(APIKeyFile, []byte(opts.APIKey), 0600); err != nil {
		return err
	}

	if fi, err := os.Stat(dbFile); err != nil || fi.Size() == 0 {
		schema := "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql"
		if data, err := os.ReadFile(schema); err == nil {
			if err := commandStdin(ctx, cb, string(data), "sqlite3", dbFile); err != nil {
				return err
			}
		} else {
			if err := os.WriteFile(dbFile, []byte(pdnsSchema), 0644); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(dbFile); err == nil {
		for _, q := range []string{
			"ALTER TABLE domains ADD COLUMN options VARCHAR(65535) DEFAULT NULL;",
			"ALTER TABLE domains ADD COLUMN catalog VARCHAR(255) DEFAULT NULL;",
			"CREATE INDEX IF NOT EXISTS catalog_idx ON domains(catalog);",
		} {
			_ = exec.Command("sqlite3", dbFile, q).Run()
		}
	}

	localAddr := "0.0.0.0"
	if hasV6 {
		localAddr = "0.0.0.0, ::"
	}
	conf := fmt.Sprintf(`# PowerDNS Configuration - wdev VPS Edge Gateway
include-dir=/etc/powerdns/pdns.d

setuid=pdns
setgid=pdns

local-address=%s
local-port=53

launch=gsqlite3
gsqlite3-database=%s
gsqlite3-pragma-synchronous=1
gsqlite3-pragma-foreign-keys=1

api=yes
api-key=%s
webserver=yes
webserver-address=0.0.0.0
webserver-port=%s
webserver-allow-from=127.0.0.1,100.64.0.0/10,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16

version-string=anonymous
security-poll-suffix=

# Zone default: tolak query yang bukan milik zone kita (tetap authoritative-only).
default-soa-edit=INCEPTION-INCREMENT
`, localAddr, dbFile, opts.APIKey, opts.PDNSPort)
	if err := writeRootFile("/etc/powerdns/pdns.conf", []byte(conf), 0640); err != nil {
		return err
	}
	_ = exec.Command("chown", "-R", "pdns:pdns", dbDir).Run()
	_ = exec.Command("chmod", "664", dbFile).Run()

	_ = command(ctx, cb, "systemctl", "enable", "pdns")
	if err := command(ctx, cb, "systemctl", "restart", "pdns"); err != nil {
		_ = command(ctx, cb, "journalctl", "-u", "pdns.service", "-n", "30", "--no-pager")
		return fmt.Errorf("PowerDNS gagal start: %w", err)
	}
	cb("    PowerDNS aktif di port 53 & API " + opts.PDNSPort)
	return nil
}

func provisionAgent(ctx context.Context, cb func(string), opts ProvisionOptions) error {
	_ = os.MkdirAll(NginxAvailable, 0755)
	_ = os.MkdirAll(NginxEnabled, 0755)
	_ = os.MkdirAll(NginxStreamsDir, 0755)

	if data, err := os.ReadFile(NginxConf); err == nil {
		content := string(data)
		if !strings.Contains(content, "sites-enabled") {
			content = strings.Replace(content, "http {", "http {\n    include /etc/nginx/sites-enabled/*;", 1)
		}
		if !strings.Contains(content, "streams-enabled") {
			content += "\n# Managed by wdev-edge\ninclude " + NginxStreamsDir + "/*.conf;\n"
		}
		_ = os.WriteFile(NginxConf, []byte(content), 0644)
	}

	// Biner agent = biner Go yang sedang berjalan (di-upload oleh wdev pdns deploy).
	target := "/usr/local/bin/wdev-edge"
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if exe != target {
			if err := copyFile(exe, target, 0755); err != nil {
				return fmt.Errorf("gagal memasang biner agent: %w", err)
			}
		}
	}

	unit := fmt.Sprintf(`[Unit]
Description=wdev Edge Reverse-Proxy Agent (Go)
After=network.target nginx.service

[Service]
Type=simple
ExecStart=%s edge-agent serve --addr 0.0.0.0:%s
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
`, target, opts.AgentPort)
	if err := writeRootFile("/etc/systemd/system/wdev-edge-agent.service", []byte(unit), 0644); err != nil {
		return err
	}

	_ = command(ctx, cb, "systemctl", "daemon-reload")
	_ = command(ctx, cb, "systemctl", "enable", "nginx")
	_ = command(ctx, cb, "systemctl", "restart", "nginx")
	if err := command(ctx, cb, "systemctl", "enable", "--now", "wdev-edge-agent.service"); err != nil {
		return fmt.Errorf("gagal menjalankan wdev-edge-agent: %w", err)
	}
	cb("    wdev-edge-agent aktif di port " + opts.AgentPort)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func alignPostfix(cb func(string)) {
	if _, err := exec.LookPath("postconf"); err != nil {
		return
	}
	domains := loadMailDomains()
	settings := [][2]string{
		{"mydestination", "localhost.localdomain, localhost"},
		{"relay_domains", "hash:/etc/postfix/relay_domains"},
		{"transport_maps", "hash:/etc/postfix/transport"},
		{"smtp_sasl_auth_enable", "no"},
		{"smtp_tls_security_level", "may"},
		{"smtp_tls_verify_cert", "no"},
	}
	if len(domains) > 0 {
		current, _ := run("postconf", "-h", "myhostname")
		current = strings.TrimSpace(current)
		if current == "" || !strings.HasPrefix(current, "mx.") {
			settings = append(settings,
				[2]string{"myhostname", "mx." + domains[0]},
				[2]string{"smtp_helo_name", "mx." + domains[0]},
			)
		}
		cert, key := findLetsEncryptCert("mail." + domains[0])
		if cert != "" {
			settings = append(settings,
				[2]string{"smtpd_tls_cert_file", cert},
				[2]string{"smtpd_tls_key_file", key},
				[2]string{"smtpd_tls_security_level", "may"},
			)
		}
	}
	for _, s := range settings {
		_, _ = run("postconf", "-e", s[0]+"="+s[1])
	}
	_, _ = run("systemctl", "reload", "postfix")
	if cb != nil {
		cb("    Postfix: myhostname/mydestination diselaraskan")
	}
}

func ensureEdgeFirewall(opts ProvisionOptions) string {
	if _, err := exec.LookPath("ufw"); err == nil {
		for _, spec := range []string{"22/tcp", "53/tcp", "53/udp", "80/tcp", "443/tcp"} {
			_, _ = run("ufw", "allow", spec)
		}
		for _, p := range mailPorts() {
			_, _ = run("ufw", "allow", fmt.Sprintf("%d/tcp", p))
		}
		_, _ = run("ufw", "allow", "from", "100.64.0.0/10", "to", "any", "port", opts.PDNSPort, "proto", "tcp")
		_, _ = run("ufw", "allow", "from", "100.64.0.0/10", "to", "any", "port", opts.AgentPort, "proto", "tcp")
		_, _ = run("ufw", "--force", "enable")
		_, _ = run("ufw", "reload")
		return "ufw: port 22,53,80,443,25,465,587,993,143,995,110 dibuka"
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		for _, p := range []string{"22/tcp", "53/tcp", "53/udp", "80/tcp", "443/tcp", "25/tcp", "465/tcp", "587/tcp", "993/tcp", "143/tcp", "995/tcp", "110/tcp"} {
			_, _ = run("firewall-cmd", "--permanent", "--add-port="+p)
		}
		_, _ = run("firewall-cmd", "--reload")
		return "firewalld: port web, DNS, SSH, mail dibuka"
	}
	return "UFW/firewalld tidak tersedia; buka port manual di firewall penyedia VPS"
}
