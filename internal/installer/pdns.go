package installer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

const pdnsSQLiteSchema = `PRAGMA foreign_keys = 1;
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

// buildStandalonePDNSConf menyusun berkas konfigurasi PowerDNS standalone dengan
// webserver API dikunci hanya pada antarmuka loopback.
func buildStandalonePDNSConf(ownerLines, localAddr, dbFile, apiKey string) string {
	return fmt.Sprintf(`# PowerDNS Configuration - wdev Standalone Authoritative Server
include-dir=/etc/powerdns/pdns.d

%slocal-address=%s
local-port=53

launch=gsqlite3
gsqlite3-database=%s
gsqlite3-pragma-synchronous=1
gsqlite3-pragma-foreign-keys=1

api=yes
api-key=%s
webserver=yes
webserver-address=127.0.0.1
webserver-port=8081
webserver-allow-from=127.0.0.1,::1

version-string=anonymous
security-poll-suffix=

default-soa-edit=INCEPTION-INCREMENT
`, ownerLines, localAddr, dbFile, apiKey)
}

// InstallPowerDNS memasang dan mengonfigurasi PowerDNS Authoritative Server secara standalone
// (SQLite3 + HTTP API di 127.0.0.1:8081) tanpa ketergantungan NetBird, Postfix, atau Edge Agent,
// sehingga kompatibel penuh dengan mode Lokal, VPS, maupun Edge.
func (inst *Installer) InstallPowerDNS(ctx context.Context, cb LogCallback) error {
	if cb == nil {
		cb = func(string) {}
	}
	cb("=== Memasang PowerDNS Authoritative Server (Standalone) ===")

	// 1. Bebaskan port 53 terlebih dahulu agar paket distro bisa langsung start pdns tanpa bentrok.
	cb(">>> [1/5] Memeriksa dan membebaskan port 53...")
	inst.freePort53(ctx, cb)

	// 2. Pasang paket PowerDNS sesuai distro.
	cb(">>> [2/5] Memasang paket PowerDNS + SQLite3 backend...")
	if inst.isDebian {
		_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", "--no-install-recommends",
			"pdns-server", "pdns-backend-sqlite3", "sqlite3", "curl"); err != nil {
			_ = inst.runCmd(ctx, cb, "dpkg", "--configure", "-a")
			if retryErr := inst.runCmd(ctx, cb, "apt-get", "install", "-f", "-y"); retryErr != nil {
				return fmt.Errorf("gagal memasang pdns-server & sqlite3: %w", err)
			}
		}
		_ = inst.runCmd(ctx, cb, "apt-get", "install", "-y", "--no-install-recommends", "dnsutils")
	} else if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y",
			"pdns", "pdns-backend-sqlite3", "sqlite", "curl", "bind-utils"); err != nil {
			return fmt.Errorf("gagal memasang paket pdns (%s): %w", inst.pkgMgr, err)
		}
	} else if inst.pkgMgr == "pacman" {
		if err := inst.runCmd(ctx, cb, "pacman", "-S", "--noconfirm", "--needed",
			"powerdns", "sqlite", "curl", "bind"); err != nil {
			return fmt.Errorf("gagal memasang paket powerdns (pacman): %w", err)
		}
	} else if inst.pkgMgr == "zypper" {
		if err := inst.runCmd(ctx, cb, "zypper", "install", "-y",
			"pdns", "pdns-backend-sqlite3", "sqlite3", "curl", "bind-utils"); err != nil {
			return fmt.Errorf("gagal memasang paket pdns (zypper): %w", err)
		}
	} else {
		return fmt.Errorf("package manager '%s' belum didukung untuk instalasi PowerDNS otomatis", inst.pkgMgr)
	}

	// 3. Inisialisasi database SQLite3 & API key PowerDNS.
	cb(">>> [3/5] Mengonfigurasi database SQLite3 & API PowerDNS...")
	const dbDir = "/var/lib/powerdns"
	const dbFile = "/var/lib/powerdns/pdns.sqlite3"
	const pdnsConfPath = "/etc/powerdns/pdns.conf"

	pdnsUser := detectPDNSServiceUser()
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", dbDir, "/etc/powerdns/pdns.d")

	if err := inst.initPDNSSQLiteDB(ctx, cb, dbFile); err != nil {
		return fmt.Errorf("gagal menginisialisasi database SQLite PowerDNS: %w", err)
	}
	if pdnsUser != "" {
		_ = inst.runCmd(ctx, cb, "chown", "-R", pdnsUser+":"+pdnsUser, dbDir)
	}
	_ = inst.runCmd(ctx, cb, "chmod", "775", dbDir)
	_ = inst.runCmd(ctx, cb, "chmod", "664", dbFile)

	apiKey := inst.resolveExistingPDNSAPIKey()
	if apiKey == "" {
		var genErr error
		apiKey, genErr = generatePDNSAPIKey()
		if genErr != nil {
			return fmt.Errorf("gagal membuat API key PowerDNS: %w", genErr)
		}
	}

	localAddr := "0.0.0.0"
	if hasNonLoopbackIPv6() {
		localAddr = "0.0.0.0, ::"
	}
	ownerLines := ""
	if pdnsUser != "" {
		ownerLines = fmt.Sprintf("setuid=%s\nsetgid=%s\n", pdnsUser, pdnsUser)
	}

	pdnsConf := buildStandalonePDNSConf(ownerLines, localAddr, dbFile, apiKey)

	if err := inst.writeSystemFile(ctx, cb, pdnsConfPath, []byte(pdnsConf), "0640"); err != nil {
		return fmt.Errorf("gagal menulis %s: %w", pdnsConfPath, err)
	}
	if pdnsUser != "" {
		_ = inst.runCmd(ctx, cb, "chown", "root:"+pdnsUser, pdnsConfPath)
	}

	// 4. Jalankan service systemd PowerDNS dan buka port 53 bila firewall aktif.
	cb(">>> [4/5] Mengaktifkan layanan PowerDNS...")
	_ = inst.runCmd(ctx, cb, "systemctl", "enable", "pdns")
	if err := inst.runCmd(ctx, cb, "systemctl", "restart", "pdns"); err != nil {
		_ = inst.runCmd(ctx, cb, "journalctl", "-u", "pdns.service", "-n", "25", "--no-pager")
		return fmt.Errorf("layanan pdns gagal dijalankan: %w", err)
	}
	inst.openPDNSFirewallPorts(ctx, cb)

	// 5. Simpan edge-dns.conf lokal agar Control Plane langsung terhubung tanpa NetBird.
	cb(">>> [5/5] Menyimpan konfigurasi integrasi DNS lokal (127.0.0.1:8081)...")
	hostIP := inst.detectHostRecordIP(ctx)
	confContent := fmt.Sprintf(
		"VPS_NETBIRD_IP=\"127.0.0.1\"\nVPS_PUBLIC_IP=%q\nAPI_KEY=%q\nPDNS_PORT=\"8081\"\nAGENT_PORT=\"8082\"\n",
		hostIP, apiKey,
	)
	if err := inst.writeWdevEdgeDNSConf(ctx, cb, confContent); err != nil {
		return fmt.Errorf("gagal menyimpan edge-dns.conf: %w", err)
	}

	if waitErr := waitPDNSAPIReady("127.0.0.1", "8081", apiKey); waitErr != nil {
		cb(fmt.Sprintf("[WARN] PowerDNS terpasang namun API 127.0.0.1:8081 belum merespons penuh: %v", waitErr))
	} else {
		cb(fmt.Sprintf("✓ PowerDNS aktif di port 53 & API 127.0.0.1:8081 (IP host: %s)", hostIP))
	}
	return nil
}

func (inst *Installer) freePort53(ctx context.Context, cb LogCallback) {
	hadResolved := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "systemd-resolved").Run() == nil
	var fallbackResolv []byte
	if data, err := os.ReadFile("/run/systemd/resolve/resolv.conf"); err == nil && len(data) > 0 {
		fallbackResolv = data
	}

	for _, svc := range []string{"systemd-resolved", "bind9", "named", "dnsmasq"} {
		if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", svc).Run() == nil {
			cb(fmt.Sprintf("  Menghentikan %s agar port 53 bebas untuk PowerDNS...", svc))
			_ = inst.runCmd(ctx, cb, "systemctl", "stop", svc)
			_ = inst.runCmd(ctx, cb, "systemctl", "disable", svc)
		}
	}

	if hadResolved {
		needsReplace := false
		if fi, err := os.Lstat("/etc/resolv.conf"); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			needsReplace = true
		} else if data, err := os.ReadFile("/etc/resolv.conf"); err == nil && strings.Contains(string(data), "127.0.0.53") {
			needsReplace = true
		}
		if needsReplace {
			if len(fallbackResolv) == 0 || strings.Contains(string(fallbackResolv), "127.0.0.53") {
				fallbackResolv = []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n")
			}
			_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/resolv.conf")
			_ = inst.writeSystemFile(ctx, cb, "/etc/resolv.conf", fallbackResolv, "0644")
		}
	}
}

func (inst *Installer) initPDNSSQLiteDB(ctx context.Context, cb LogCallback, dbFile string) error {
	if fi, err := os.Stat(dbFile); err == nil && fi.Size() > 0 {
		if data, readErr := os.ReadFile(dbFile); readErr == nil && strings.HasPrefix(string(data), "SQLite format 3") {
			return nil
		}
	}

	schemaSQL := pdnsSQLiteSchema
	for _, candidate := range []string{
		"/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql",
		"/usr/share/doc/pdns-backend-sqlite3/schema.sqlite3.sql",
	} {
		if data, err := os.ReadFile(candidate); err == nil && strings.TrimSpace(string(data)) != "" {
			schemaSQL = string(data)
			break
		}
	}

	tmpDir, err := os.MkdirTemp("", "wdev-pdns-db-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpDB := filepath.Join(tmpDir, "pdns.sqlite3")
	cmd := exec.CommandContext(ctx, "sqlite3", tmpDB)
	cmd.Stdin = strings.NewReader(schemaSQL)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("sqlite3 init gagal (%v): %s", runErr, strings.TrimSpace(string(out)))
	}

	for _, q := range []string{
		"ALTER TABLE domains ADD COLUMN options VARCHAR(65535) DEFAULT NULL;",
		"ALTER TABLE domains ADD COLUMN catalog VARCHAR(255) DEFAULT NULL;",
		"CREATE INDEX IF NOT EXISTS catalog_idx ON domains(catalog);",
	} {
		_ = exec.CommandContext(ctx, "sqlite3", tmpDB, q).Run()
	}

	return inst.runCmd(ctx, cb, "cp", "-f", tmpDB, dbFile)
}

func detectPDNSServiceUser() string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	content := string(data)
	if strings.Contains(content, "pdns:") {
		return "pdns"
	}
	if strings.Contains(content, "powerdns:") {
		return "powerdns"
	}
	return ""
}

func (inst *Installer) resolveExistingPDNSAPIKey() string {
	var candidates []string
	if inst.cfg != nil && inst.cfg.WdevEtc != "" {
		candidates = append(candidates, filepath.Join(inst.cfg.WdevEtc, "edge-dns.conf"))
	}
	candidates = append(candidates, "/etc/wdev/edge-dns.conf", "/etc/wdev-edge/api.key")
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.HasSuffix(path, "api.key") {
			if k := strings.TrimSpace(string(data)); k != "" {
				return k
			}
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "API_KEY=") {
				v := strings.Trim(strings.TrimPrefix(line, "API_KEY="), "\"'")
				if v != "" {
					return v
				}
			}
		}
	}
	if data, err := os.ReadFile("/etc/powerdns/pdns.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "api-key=") {
				v := strings.TrimSpace(strings.TrimPrefix(line, "api-key="))
				if v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func generatePDNSAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (inst *Installer) writeWdevEdgeDNSConf(ctx context.Context, cb LogCallback, content string) error {
	etcDir := "/etc/wdev"
	if inst.cfg != nil && inst.cfg.WdevEtc != "" {
		etcDir = inst.cfg.WdevEtc
	}
	confPath := filepath.Join(etcDir, "edge-dns.conf")
	if err := os.MkdirAll(etcDir, 0755); err == nil {
		if err := os.WriteFile(confPath, []byte(content), 0600); err == nil {
			return nil
		}
	}
	return inst.writeSystemFile(ctx, cb, confPath, []byte(content), "0600")
}

func (inst *Installer) openPDNSFirewallPorts(ctx context.Context, cb LogCallback) {
	if _, err := exec.LookPath("ufw"); err == nil {
		if out, err := inst.runCmdOutput(ctx, "ufw", "status"); err == nil && strings.Contains(strings.ToLower(out), "active") {
			_ = inst.runCmd(ctx, cb, "ufw", "allow", "53/tcp")
			_ = inst.runCmd(ctx, cb, "ufw", "allow", "53/udp")
			_ = inst.runCmd(ctx, cb, "ufw", "reload")
		}
		return
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "firewalld").Run() == nil {
			_ = inst.runCmd(ctx, cb, "firewall-cmd", "--permanent", "--add-port=53/tcp")
			_ = inst.runCmd(ctx, cb, "firewall-cmd", "--permanent", "--add-port=53/udp")
			_ = inst.runCmd(ctx, cb, "firewall-cmd", "--reload")
		}
	}
}

func waitPDNSAPIReady(host, port, apiKey string) error {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost", host, port)
	var lastErr error
	for i := 0; i < 6; i++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-API-Key", apiKey)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

func hasNonLoopbackIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil && !ipnet.IP.IsLoopback() && !ipnet.IP.IsLinkLocalUnicast() {
				return true
			}
		}
	}
	return false
}

func isInstallerPublicIPv4(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127 {
		return false
	}
	return true
}

func (inst *Installer) detectHostRecordIP(ctx context.Context) string {
	explicitMode := ""
	if inst != nil && inst.store != nil {
		if val, err := inst.store.GetSetting("server_mode"); err == nil {
			explicitMode = strings.ToLower(strings.TrimSpace(val))
		}
	}

	var lanFallback string
	var publicNICIP string
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, ifi := range ifaces {
			low := strings.ToLower(ifi.Name)
			if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 ||
				low == "wt0" || strings.HasPrefix(low, "wg") || strings.HasPrefix(low, "tun") ||
				strings.HasPrefix(low, "docker") || strings.HasPrefix(low, "br-") || strings.HasPrefix(low, "veth") {
				continue
			}
			addrs, err := ifi.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ipnet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				ip4 := ipnet.IP.To4()
				if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsMulticast() || ip4.IsUnspecified() {
					continue
				}
				if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
					continue
				}
				if ip4.IsPrivate() {
					if lanFallback == "" {
						lanFallback = ip4.String()
					}
					continue
				}
				if publicNICIP == "" {
					publicNICIP = ip4.String()
				}
			}
		}
	}

	if explicitMode == "local" {
		if lanFallback != "" {
			return lanFallback
		}
		return "127.0.0.1"
	}

	if publicNICIP != "" {
		return publicNICIP
	}

	_, edgeConfErr := os.Stat("/etc/wdev-edge/edge.conf")
	hasEdgeConf := edgeConfErr == nil
	if hasEdgeConf {
		if data, err := os.ReadFile("/etc/wdev-edge/edge.conf"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "PUBLIC_IP=") {
					v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "PUBLIC_IP=")), "\"")
					if p := net.ParseIP(v); p != nil && isInstallerPublicIPv4(p.To4()) {
						return v
					}
				}
			}
		}
	}

	if explicitMode == "vps" || hasEdgeConf {
		transport := &http.Transport{
			DialContext: func(c context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(c, "tcp4", addr)
			},
		}
		client := &http.Client{Timeout: 4 * time.Second, Transport: transport}
		for _, u := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://ipv4.icanhazip.com"} {
			req, reqErr := http.NewRequestWithContext(ctx, "GET", u, nil)
			if reqErr != nil {
				continue
			}
			resp, respErr := client.Do(req)
			if respErr != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			_ = resp.Body.Close()
			ip := strings.TrimSpace(string(body))
			if p := net.ParseIP(ip); p != nil && isInstallerPublicIPv4(p.To4()) {
				return ip
			}
		}
		if explicitMode == "vps" {
			return ""
		}
	}
	if lanFallback != "" {
		return lanFallback
	}
	return "127.0.0.1"
}
