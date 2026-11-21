package mail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/config"
	"wdev/internal/pdns"
	"wdev/internal/store"
)

type Manager struct {
	cfg              *config.Config
	store            *store.Store
	pdnsMgr          *pdns.Manager
	ensureStackFn    func(ctx context.Context, cb func(string), domain string) error
	portProbeFn      func(host string, port int, timeout time.Duration) bool
	resendAPIBaseURL string

	// LastRelayDNS menyimpan hasil rekonsiliasi DNS dari operasi relay terakhir
	// agar handler HTTP/CLI bisa melaporkan record yang dihapus & dibuat.
	relayDNSMu   sync.Mutex
	LastRelayDNS RelayDNSReport
}

// SetInstaller mendaftarkan fungsi pemasang Mail Stack otomatis bila Stalwart/SnappyMail belum tersedia.
func (m *Manager) SetInstaller(fn func(ctx context.Context, cb func(string), domain string) error) {
	m.ensureStackFn = fn
}

// SetPortProbe memungkinkan penggantian fungsi cek TCP port untuk pengujian unit.
func (m *Manager) SetPortProbe(fn func(host string, port int, timeout time.Duration) bool) {
	m.portProbeFn = fn
}

// setLastRelayDNS mencatat hasil rekonsiliasi DNS secara aman.
func (m *Manager) setLastRelayDNS(rep RelayDNSReport) {
	m.relayDNSMu.Lock()
	m.LastRelayDNS = rep
	m.relayDNSMu.Unlock()
}

// LastRelayDNSReport mengembalikan hasil rekonsiliasi DNS terakhir.
func (m *Manager) LastRelayDNSReport() RelayDNSReport {
	m.relayDNSMu.Lock()
	defer m.relayDNSMu.Unlock()
	return m.LastRelayDNS
}

type PortStatus struct {
	Port         int    `json:"port"`
	Label        string `json:"label"`
	Open         bool   `json:"open"`
	Local        bool   `json:"local,omitempty"`
	Public       bool   `json:"public,omitempty"`
	InternalOnly bool   `json:"internal_only,omitempty"`
}

type MailStatus struct {
	Mode             string                 `json:"mode"`
	BehindNAT        bool                   `json:"behind_nat,omitempty"`
	StalwartActive   bool                   `json:"stalwart_active"`
	StalwartPorts    []PortStatus           `json:"stalwart_ports,omitempty"`
	SnappyMailActive bool                   `json:"snappymail_active"`
	RelayOnly        bool                   `json:"relay_only"`
	OutboundDefault  string                 `json:"outbound_default,omitempty"`
	OutboundRoutes   map[string]string      `json:"outbound_routes,omitempty"`
	StorageFree      string                 `json:"storage_free"`
	GlobalRelay      *RelayConfig           `json:"global_relay,omitempty"`
	DomainRelays     map[string]RelayConfig `json:"domain_relays"`
	Domains          []DomainSummary        `json:"domains"`
}

type DomainSummary struct {
	Domain    string `json:"domain"`
	AdminURL  string `json:"admin_url"`
	Webmail   string `json:"webmail"`
	AdminUser string `json:"admin_user"`
}

type RelayConfig struct {
	Domain      string `json:"domain,omitempty"`
	Provider    string `json:"provider"` // "resend", "brevo", "custom"
	Host        string `json:"host"`
	Port        int    `json:"port"`
	ImplicitTLS bool   `json:"implicit_tls"`
	Username    string `json:"username"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type DomainAccounts struct {
	Domain   string        `json:"domain"`
	Enabled  bool          `json:"enabled"`
	AdminURL string        `json:"admin_url"`
	Webmail  string        `json:"webmail"`
	Relay    *RelayConfig  `json:"relay,omitempty"`
	Accounts []MailAccount `json:"accounts"`
}

type MailAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	DomainID string `json:"domain_id"`
}

type SetRelayOptions struct {
	Domain   string `json:"domain"`
	Provider string `json:"provider"` // "resend", "brevo", "custom"
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type SetupMailOptions struct {
	Domain    string `json:"domain"`
	SSL       bool   `json:"ssl"`
	RelayProv string `json:"relay_provider"` // "", "resend", "brevo", "custom" (kosong = relay default)
	RelayHost string `json:"relay_host"`
	RelayPort int    `json:"relay_port"`
	RelayUser string `json:"relay_user"`
	RelayPass string `json:"relay_pass"`
}

func New(cfg *config.Config, s *store.Store, pdnsMgr *pdns.Manager) *Manager {
	return &Manager{
		cfg:     cfg,
		store:   s,
		pdnsMgr: pdnsMgr,
	}
}

// -----------------------------------------------------------------------------
// Utilities
// -----------------------------------------------------------------------------

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func readRootFile(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	return exec.Command("sudo", "-n", "cat", path).Output()
}

func writeRootFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err == nil {
		if err := os.WriteFile(path, data, perm); err == nil {
			return os.Chmod(path, perm)
		}
	}
	if os.Geteuid() == 0 {
		_ = os.MkdirAll(dir, 0755)
		if err := os.WriteFile(path, data, perm); err != nil {
			return err
		}
		return os.Chmod(path, perm)
	}
	tmpFile, err := os.CreateTemp("", "wdev-mail-*")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	defer os.Remove(tmp)
	_ = os.Chmod(tmp, perm)
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	_ = exec.Command("sudo", "-n", "mkdir", "-p", dir).Run()
	return exec.Command("sudo", "-n", "install", "-m", fmt.Sprintf("%04o", perm), tmp, path).Run()
}

func (m *Manager) runStalwartCli(user, pass string, args ...string) ([]byte, error) {
	cliPath := "/usr/local/bin/stalwart-cli"
	if _, err := os.Stat(cliPath); err != nil {
		if p, err := exec.LookPath("stalwart-cli"); err == nil {
			cliPath = p
		}
	}
	envVars := []string{
		"STALWART_URL=http://127.0.0.1:8080",
		"STALWART_USER=" + user,
		"STALWART_PASSWORD=" + pass,
	}
	if os.Geteuid() == 0 {
		cmd := exec.Command(cliPath, args...)
		cmd.Env = append(os.Environ(), envVars...)
		return cmd.CombinedOutput()
	}
	// Gunakan --preserve-env agar STALWART_PASSWORD tidak muncul di /proc/<pid>/cmdline
	sudoArgs := append([]string{"-n", "--preserve-env=STALWART_URL,STALWART_USER,STALWART_PASSWORD", cliPath}, args...)
	cmd := exec.Command("sudo", sudoArgs...)
	cmd.Env = append(os.Environ(), envVars...)
	return cmd.CombinedOutput()
}

func waitStalwartReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := http.Client{Timeout: 1500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://127.0.0.1:8080/admin/")
		if err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func setStalwartEnv(key, val string) error {
	path := "/etc/stalwart/stalwart.env"
	data, _ := readRootFile(path)
	lines := strings.Split(string(data), "\n")
	found := false
	var newLines []string
	prefix := key + "="
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, prefix) {
			newLines = append(newLines, fmt.Sprintf("%s=%s", key, val))
			found = true
		} else if trimmed != "" {
			newLines = append(newLines, l)
		}
	}
	if !found {
		newLines = append(newLines, fmt.Sprintf("%s=%s", key, val))
	}
	content := strings.Join(newLines, "\n") + "\n"
	return writeRootFile(path, []byte(content), 0640)
}

func unsetStalwartEnv(key string) error {
	path := "/etc/stalwart/stalwart.env"
	data, err := readRootFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var newLines []string
	prefix := key + "="
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, prefix) && trimmed != "" {
			newLines = append(newLines, l)
		}
	}
	content := strings.Join(newLines, "\n") + "\n"
	return writeRootFile(path, []byte(content), 0640)
}

func stalwartServiceFailureDetail() string {
	var out []byte
	var err error
	if os.Geteuid() == 0 {
		out, err = exec.Command("journalctl", "-u", "stalwart", "-n", "12", "--no-pager", "-o", "cat").CombinedOutput()
	} else {
		out, err = exec.Command("sudo", "-n", "journalctl", "-u", "stalwart", "-n", "12", "--no-pager", "-o", "cat").CombinedOutput()
	}
	if err == nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			lines := strings.Split(msg, "\n")
			if len(lines) > 4 {
				lines = lines[len(lines)-4:]
			}
			return strings.Join(lines, " | ")
		}
	}
	if os.Geteuid() == 0 {
		out, _ = exec.Command("systemctl", "status", "stalwart", "--no-pager", "-n", "6").CombinedOutput()
	} else {
		out, _ = exec.Command("sudo", "-n", "systemctl", "status", "stalwart", "--no-pager", "-n", "6").CombinedOutput()
	}
	return strings.TrimSpace(string(out))
}

func restartStalwart() error {
	out, err := runRootCombined("systemctl", "restart", "stalwart")
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if j := stalwartServiceFailureDetail(); j != "" {
			detail = j
		}
		if detail != "" {
			return fmt.Errorf("gagal me-restart service stalwart (%v): %s", err, detail)
		}
		return fmt.Errorf("gagal me-restart service stalwart: %w", err)
	}
	if !waitStalwartReady(20 * time.Second) {
		if j := stalwartServiceFailureDetail(); j != "" {
			return fmt.Errorf("stalwart belum merespons setelah restart: %s", j)
		}
		return fmt.Errorf("stalwart belum merespons setelah restart")
	}
	return nil
}

// -----------------------------------------------------------------------------
// Kredensial & Konfigurasi Lokal
// -----------------------------------------------------------------------------

type adminCredEntry struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	UpdatedAt string `json:"updatedAt"`
}

func (m *Manager) getAdminCredMap() map[string]adminCredEntry {
	adminCredPath := filepath.Join(m.cfg.WdevEtc, "mail-admin.cred")
	data, err := readRootFile(adminCredPath)
	if err != nil {
		return make(map[string]adminCredEntry)
	}
	var res map[string]adminCredEntry
	if err := json.Unmarshal(data, &res); err != nil {
		return make(map[string]adminCredEntry)
	}
	return res
}

func (m *Manager) saveAdminCred(domain, email, password string) error {
	adminCredPath := filepath.Join(m.cfg.WdevEtc, "mail-admin.cred")
	mMap := m.getAdminCredMap()
	mMap[domain] = adminCredEntry{
		Email:     email,
		Password:  password,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(mMap, "", "  ")
	if err != nil {
		return err
	}
	return writeRootFile(adminCredPath, data, 0600)
}

func (m *Manager) pickValidAdminCred(domainFilter string) (string, string, error) {
	mMap := m.getAdminCredMap()
	if domainFilter != "" {
		if entry, ok := mMap[domainFilter]; ok && entry.Email != "" && entry.Password != "" {
			// Cek validitas
			if _, err := m.runStalwartCli(entry.Email, entry.Password, "query", "Domain", "--fields", "id", "--json"); err == nil {
				return entry.Email, entry.Password, nil
			}
		}
	}
	for _, entry := range mMap {
		if entry.Email != "" && entry.Password != "" {
			if _, err := m.runStalwartCli(entry.Email, entry.Password, "query", "Domain", "--fields", "id", "--json"); err == nil {
				return entry.Email, entry.Password, nil
			}
		}
	}
	return "", "", fmt.Errorf("kredensial admin Stalwart belum ada atau tidak valid")
}

// -----------------------------------------------------------------------------
// Stalwart Queries & Mutations
// -----------------------------------------------------------------------------

func (m *Manager) findDomainID(user, pass, domain string) (string, error) {
	out, err := m.runStalwartCli(user, pass, "query", "Domain", "--where", "name="+domain, "--fields", "id,name", "--json")
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &item); err == nil && item.ID != "" {
			if item.Name == domain || domain == "" {
				return item.ID, nil
			}
		}
	}
	return "", nil
}

func (m *Manager) findAdminAccount(user, pass, domainID string) (id, email string, err error) {
	out, err := m.runStalwartCli(user, pass, "query", "Account", "--where", "domainId="+domainID, "--fields", "id,name,emailAddress,domainId,roles", "--json")
	if err != nil {
		return "", "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			EmailAddress string `json:"emailAddress"`
			Roles        struct {
				Type string `json:"@type"`
			} `json:"roles"`
		}
		if err := json.Unmarshal([]byte(line), &item); err == nil && item.ID != "" {
			if item.Roles.Type == "Admin" {
				return item.ID, item.EmailAddress, nil
			}
			if id == "" {
				id = item.ID
				email = item.EmailAddress
			}
		}
	}
	return id, email, nil
}

func (m *Manager) findAccountByUsername(user, pass, domainID, username string) (id, email string, err error) {
	out, err := m.runStalwartCli(user, pass, "query", "Account", "--where", "domainId="+domainID, "--fields", "id,name,emailAddress,domainId", "--json")
	if err != nil {
		return "", "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			EmailAddress string `json:"emailAddress"`
		}
		if err := json.Unmarshal([]byte(line), &item); err == nil && item.ID != "" {
			if item.Name == username {
				return item.ID, item.EmailAddress, nil
			}
		}
	}
	return "", "", nil
}

func (m *Manager) ensureStalwartBootstrap(domain string) error {
	configPath := "/etc/stalwart/config.json"
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}

	recPass := randomHex(16)
	if err := setStalwartEnv("STALWART_RECOVERY_ADMIN", "wdev-bootstrap:"+recPass); err != nil {
		return err
	}
	if err := restartStalwart(); err != nil {
		_ = unsetStalwartEnv("STALWART_RECOVERY_ADMIN")
		return fmt.Errorf("gagal memulai mode bootstrap Stalwart: %w", err)
	}

	payload := map[string]any{
		"defaultDomain":         domain,
		"defaultHostname":       "mail." + domain,
		"dataStore":             map[string]any{"@type": "RocksDb", "path": "/var/lib/stalwart/"},
		"blobStore":             map[string]any{"@type": "Default"},
		"searchStore":           map[string]any{"@type": "Default"},
		"inMemoryStore":         map[string]any{"@type": "Default"},
		"directory":             map[string]any{"@type": "Internal"},
		"dnsServer":             map[string]any{"@type": "Manual"},
		"requestTlsCertificate": false,
		"generateDkimKeys":      true,
	}
	pBytes, _ := json.Marshal(payload)
	tmpF, err := os.CreateTemp("", "stalwart-bs-*.json")
	if err != nil {
		return err
	}
	tmpFile := tmpF.Name()
	_ = os.Chmod(tmpFile, 0600)
	_, _ = tmpF.Write(pBytes)
	_ = tmpF.Close()
	defer os.Remove(tmpFile)

	out, err := m.runStalwartCli("wdev-bootstrap", recPass, "update", "Bootstrap", "--file", tmpFile)
	_ = unsetStalwartEnv("STALWART_RECOVERY_ADMIN")

	if err != nil || !strings.Contains(strings.ToLower(string(out)), "updated bootstrap") {
		return fmt.Errorf("bootstrap Stalwart gagal: %s", string(out))
	}
	if err := restartStalwart(); err != nil {
		return fmt.Errorf("gagal me-restart Stalwart setelah bootstrap: %w", err)
	}

	adminUser := ""
	adminPass := ""
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.Index(line, "username:"); idx != -1 {
			parts := strings.Fields(line[idx+9:])
			if len(parts) > 0 {
				adminUser = strings.Trim(parts[0], `"`)
			}
		}
		if idx := strings.Index(line, "secret:"); idx != -1 {
			parts := strings.Fields(line[idx+7:])
			if len(parts) > 0 {
				adminPass = strings.Trim(parts[0], `"`)
			}
		}
	}
	if adminUser != "" && adminPass != "" {
		_ = m.saveAdminCred(domain, adminUser, adminPass)
	}
	return nil
}

func (m *Manager) resetAdminInternal(domain string) (string, string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", "", fmt.Errorf("domain wajib diisi")
	}

	configPath := "/etc/stalwart/config.json"
	if _, err := os.Stat(configPath); err != nil {
		if err := m.ensureStalwartBootstrap(domain); err != nil {
			return "", "", err
		}
		email, pass, _ := m.pickValidAdminCred(domain)
		if email != "" && pass != "" {
			return email, pass, nil
		}
	}

	recPass := randomHex(16)
	if err := setStalwartEnv("STALWART_RECOVERY_ADMIN", "wdev-recovery:"+recPass); err != nil {
		return "", "", err
	}
	_ = restartStalwart()
	defer func() {
		_ = unsetStalwartEnv("STALWART_RECOVERY_ADMIN")
		_ = restartStalwart()
	}()

	// 1. Cari domain ID di Stalwart
	did, _ := m.findDomainID("wdev-recovery", recPass, domain)
	if did == "" {
		_, _ = m.runStalwartCli("wdev-recovery", recPass, "create", "Domain", "--field", "name="+domain, "--field", "enable=true")
		did, _ = m.findDomainID("wdev-recovery", recPass, domain)
	}
	if did == "" {
		return "", "", fmt.Errorf("domain %s tidak ditemukan dan gagal dibuat di Stalwart", domain)
	}

	// 2. Cari admin account untuk domain ini
	adminID, adminEmail, _ := m.findAdminAccount("wdev-recovery", recPass, did)
	newPass := randomHex(16)
	credJSON := fmt.Sprintf(`{"0":{"@type":"Password","secret":"%s"}}`, newPass)

	if adminID != "" {
		out, err := m.runStalwartCli("wdev-recovery", recPass, "update", "Account", adminID, "--field", "credentials="+credJSON)
		if err != nil || !strings.Contains(strings.ToLower(string(out)), "account") {
			return "", "", fmt.Errorf("gagal memperbarui password admin: %s", string(out))
		}
	} else {
		adminEmail = fmt.Sprintf("admin@%s", domain)
		out, err := m.runStalwartCli("wdev-recovery", recPass, "create", "Account/User",
			"--field", "name=admin",
			"--field", "domainId="+did,
			"--field", `roles={"@type":"Admin"}`,
			"--field", "description=System administrator",
			"--field", "credentials="+credJSON,
		)
		if err != nil || !strings.Contains(strings.ToLower(string(out)), "account") {
			return "", "", fmt.Errorf("gagal membuat akun admin: %s", string(out))
		}
	}

	if err := m.saveAdminCred(domain, adminEmail, newPass); err != nil {
		return "", "", err
	}
	return adminEmail, newPass, nil
}

func (m *Manager) ensureStalwartMailDomain(domain string) (string, string, error) {
	email, pass, err := m.pickValidAdminCred(domain)
	if err == nil && email != "" && pass != "" {
		did, _ := m.findDomainID(email, pass, domain)
		if did == "" {
			_, _ = m.runStalwartCli(email, pass, "create", "Domain", "--field", "name="+domain, "--field", "enable=true")
		}
		return email, pass, nil
	}
	return m.resetAdminInternal(domain)
}

func reloadLocalNginx() error {
	if err := exec.Command("nginx", "-t").Run(); err != nil {
		return fmt.Errorf("nginx -t gagal: %w", err)
	}
	if err := exec.Command("systemctl", "reload", "nginx").Run(); err != nil {
		return exec.Command("sudo", "-n", "systemctl", "reload", "nginx").Run()
	}
	return nil
}

func (m *Manager) ensureSnappyMailDomain(domain string) error {
	_ = m.ensureWebmailFallbackVhost()
	_ = m.ensureLocalMailVhost(domain)
	_ = reloadLocalNginx()
	return m.writeSnappyMailDomainConfig(domain)
}

func (m *Manager) writeSnappyMailDomainConfig(domain string) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	snapRoot := "/var/www/snappymail"
	if _, err := os.Stat(snapRoot); err != nil {
		return nil
	}
	snapDomDir := filepath.Join(snapRoot, "data", "_data_", "_default_", "domains")
	if err := os.MkdirAll(snapDomDir, 0700); err != nil {
		_ = runRoot("mkdir", "-p", snapDomDir)
	}

	cfgContent := fmt.Sprintf(`{
    "IMAP": {
        "host": "127.0.0.1",
        "port": 993,
        "type": 1,
        "timeout": 300,
        "shortLogin": false,
        "lowerLogin": true,
        "ssl": {
            "verify_peer": false,
            "verify_peer_name": false,
            "allow_self_signed": true,
            "SNI_enabled": true,
            "disable_compression": true,
            "security_level": 1
        }
    },
    "SMTP": {
        "host": "127.0.0.1",
        "port": 465,
        "type": 1,
        "timeout": 60,
        "shortLogin": false,
        "lowerLogin": true,
        "useAuth": true,
        "ssl": {
            "verify_peer": false,
            "verify_peer_name": false,
            "allow_self_signed": true,
            "SNI_enabled": true,
            "disable_compression": true,
            "security_level": 1
        }
    },
    "Sieve": {
        "host": "127.0.0.1",
        "port": 4190,
        "type": 0,
        "timeout": 10,
        "shortLogin": false,
        "lowerLogin": true,
        "enabled": false,
        "ssl": {
            "verify_peer": false,
            "verify_peer_name": false,
            "allow_self_signed": true,
            "SNI_enabled": true,
            "disable_compression": true,
            "security_level": 1
        }
    },
    "whiteList": ""
}`)
	targetFile := filepath.Join(snapDomDir, domain+".json")
	_ = writeRootFile(targetFile, []byte(cfgContent), 0600)
	webOwner := "www-data:www-data"
	if data, err := os.ReadFile("/etc/passwd"); err == nil && !strings.Contains(string(data), "www-data:") && strings.Contains(string(data), "nginx:") {
		webOwner = "nginx:nginx"
	}
	_ = runRoot("chown", webOwner, snapDomDir, targetFile)
	return nil
}

func (m *Manager) mailDataDir() string {
	if v := strings.TrimSpace(os.Getenv("WDEV_MAIL_DATA")); v != "" {
		return v
	}
	return "/var/lib/wdev-mail"
}

func phpFPMsock() string {
	if _, err := os.Stat("/run/php/php8.4-fpm.sock"); err == nil {
		return "/run/php/php8.4-fpm.sock"
	}
	for _, pat := range []string{"/run/php/*fpm.sock", "/run/php-fpm/*.sock"} {
		if matches, _ := filepath.Glob(pat); len(matches) > 0 {
			return matches[0]
		}
	}
	return "/run/php/php8.4-fpm.sock"
}

// localMailVhostBlock membuat vhost lokal untuk satu domain: mailadmin -> admin
// Stalwart (127.0.0.1:8080), webmail -> SnappyMail, mail -> health check.
func localMailVhostBlock(domain, phpSock string) string {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	return fmt.Sprintf(`# Managed by wdev (mail vhost %[1]s)
server {
	listen 80;
	server_name webmail.%[1]s;

	root /var/www/snappymail;
	index index.php index.html;
	client_max_body_size 50M;

	location / {
		try_files $uri $uri/ /index.php?$query_string;
	}

	location ~ \.php$ {
		include fastcgi_params;
		fastcgi_pass unix:%[2]s;
		fastcgi_index index.php;
		fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
	}

	location ~ ^/(data|config)/ {
		deny all;
		return 404;
	}
}

server {
	listen 80;
	server_name mailadmin.%[1]s;

	location / {
		proxy_pass http://127.0.0.1:8080;
		proxy_http_version 1.1;
		proxy_set_header Host $host;
		proxy_set_header X-Real-IP $remote_addr;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
		proxy_read_timeout 300s;
	}
}

server {
	listen 80;
	server_name mail.%[1]s;
	location / { return 200 "mail server host ok\n"; }
}
`, domain, phpSock)
}

func (m *Manager) nginxMailPaths(domain string) (available, enabled string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	availDir := "/etc/nginx/sites-available"
	enableDir := "/etc/nginx/sites-enabled"
	if m.cfg != nil {
		if m.cfg.NginxSitesAvail != "" {
			availDir = m.cfg.NginxSitesAvail
		}
		if m.cfg.NginxSitesEnable != "" {
			enableDir = m.cfg.NginxSitesEnable
		}
	}
	name := "wdev_mail_" + domain + ".conf"
	return filepath.Join(availDir, name), filepath.Join(enableDir, name)
}

func (m *Manager) hostsFilePath() string {
	if m.cfg != nil && m.cfg.HostsFile != "" {
		return m.cfg.HostsFile
	}
	return "/etc/hosts"
}

func (m *Manager) mailSSLDir(domain string) string {
	etcDir := "/etc/wdev"
	if m.cfg != nil && m.cfg.WdevEtc != "" {
		etcDir = m.cfg.WdevEtc
	}
	return filepath.Join(etcDir, "ssl", "mail", domain)
}

func (m *Manager) ensureMailHostsEntries(domain, ip string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !validMailDomain(domain) {
		return
	}
	if strings.TrimSpace(ip) == "" {
		ip = "127.0.0.1"
	}
	hostsPath := m.hostsFilePath()
	content, err := readRootFile(hostsPath)
	if err != nil {
		return
	}
	marker := "# wdev-mail-" + domain
	var kept []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, marker) {
			continue
		}
		kept = append(kept, line)
	}
	entry := fmt.Sprintf("%s webmail.%s mailadmin.%s mail.%s mx.%s %s", ip, domain, domain, domain, domain, marker)
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	kept = append(kept, entry, "")
	_ = writeRootFile(hostsPath, []byte(strings.Join(kept, "\n")), 0644)
}

func (m *Manager) removeMailHostsEntries(domain string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return
	}
	hostsPath := m.hostsFilePath()
	content, err := readRootFile(hostsPath)
	if err != nil {
		return
	}
	marker := "# wdev-mail-" + domain
	changed := false
	var kept []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, marker) {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if changed {
		_ = writeRootFile(hostsPath, []byte(strings.Join(kept, "\n")), 0644)
	}
}

func localMailSSLVhostBlock(domain, phpSock, certFile, keyFile string) string {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	return fmt.Sprintf(`# Managed by wdev (mail vhost SSL %[1]s)
server {
	listen 80;
	server_name webmail.%[1]s mailadmin.%[1]s mail.%[1]s;
	return 301 https://$host$request_uri;
}

server {
	listen 443 ssl;
	server_name webmail.%[1]s;

	ssl_certificate %[3]s;
	ssl_certificate_key %[4]s;

	root /var/www/snappymail;
	index index.php index.html;
	client_max_body_size 50M;

	location / {
		try_files $uri $uri/ /index.php?$query_string;
	}

	location ~ \.php$ {
		include fastcgi_params;
		fastcgi_pass unix:%[2]s;
		fastcgi_index index.php;
		fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
	}

	location ~ ^/(data|config)/ {
		deny all;
		return 404;
	}
}

server {
	listen 443 ssl;
	server_name mailadmin.%[1]s;

	ssl_certificate %[3]s;
	ssl_certificate_key %[4]s;

	location / {
		proxy_pass http://127.0.0.1:8080;
		proxy_http_version 1.1;
		proxy_set_header Host $host;
		proxy_set_header X-Real-IP $remote_addr;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
		proxy_read_timeout 300s;
	}
}

server {
	listen 443 ssl;
	server_name mail.%[1]s;

	ssl_certificate %[3]s;
	ssl_certificate_key %[4]s;

	location / { return 200 "mail server host ok\n"; }
}
`, domain, phpSock, certFile, keyFile)
}

func (m *Manager) issueLocalMailSSLVhost(domain string) (bool, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !validMailDomain(domain) {
		return false, fmt.Errorf("domain vhost tidak valid: %q", domain)
	}
	sslDir := m.mailSSLDir(domain)
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		_ = runRoot("mkdir", "-p", sslDir)
	}
	certFile := filepath.Join(sslDir, "fullchain.pem")
	keyFile := filepath.Join(sslDir, "privkey.pem")

	tmpDir, err := os.MkdirTemp("", "wdev-mail-ssl-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmpDir)
	tmpCert := filepath.Join(tmpDir, "cert.pem")
	tmpKey := filepath.Join(tmpDir, "key.pem")

	hosts := []string{"webmail." + domain, "mailadmin." + domain, "mail." + domain, "mx." + domain}
	issued := false
	if mkcertBin, err := exec.LookPath("mkcert"); err == nil {
		args := append([]string{"-cert-file", tmpCert, "-key-file", tmpKey, "--"}, hosts...)
		if exec.Command(mkcertBin, args...).Run() == nil {
			issued = true
		}
	}
	if !issued {
		san := "subjectAltName=DNS:" + strings.Join(hosts, ",DNS:")
		if err := exec.Command("openssl", "req", "-x509", "-nodes", "-days", "365",
			"-newkey", "rsa:2048",
			"-keyout", tmpKey,
			"-out", tmpCert,
			"-subj", "/CN=webmail."+domain,
			"-addext", san).Run(); err != nil {
			return false, fmt.Errorf("gagal membuat sertifikat SSL lokal: %w", err)
		}
	}
	certBytes, err := os.ReadFile(tmpCert)
	if err != nil {
		return false, err
	}
	keyBytes, err := os.ReadFile(tmpKey)
	if err != nil {
		return false, err
	}
	if err := writeRootFile(certFile, certBytes, 0644); err != nil {
		return false, err
	}
	if err := writeRootFile(keyFile, keyBytes, 0600); err != nil {
		return false, err
	}

	available, enabled := m.nginxMailPaths(domain)
	if err := writeRootFile(available, []byte(localMailSSLVhostBlock(domain, phpFPMsock(), certFile, keyFile)), 0644); err != nil {
		return false, err
	}
	if err := ensureSymlink(available, enabled); err != nil {
		return false, err
	}
	if err := reloadLocalNginx(); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) issueVPSLetsEncrypt(domain string, cb func(string)) bool {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !validMailDomain(domain) {
		return false
	}
	if _, err := exec.LookPath("certbot"); err != nil {
		if cb != nil {
			cb(" ! certbot belum terpasang; SSL Let's Encrypt dilewati (pakai HTTP sementara)")
		}
		return false
	}
	args := []string{
		"--nginx",
		"--cert-name", "wdev-mail-" + domain,
		"-d", "webmail." + domain,
		"-d", "mailadmin." + domain,
		"-d", "mail." + domain,
		"--non-interactive", "--agree-tos", "--register-unsafely-without-email",
		"--redirect", "--deploy-hook", "systemctl reload nginx",
	}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.Command("certbot", args...)
	} else {
		cmd = exec.Command("sudo", append([]string{"-n", "certbot"}, args...)...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		if cb != nil {
			cb(fmt.Sprintf(" ! certbot gagal untuk %s (%v): %s", domain, err, strings.TrimSpace(string(out))))
		}
		return false
	}
	if cb != nil {
		cb(fmt.Sprintf(" ✓ Sertifikat SSL Let's Encrypt aktif untuk webmail/mailadmin/mail.%s", domain))
	}
	return true
}

func (m *Manager) ensureLocalMailVhost(domain string) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !validMailDomain(domain) {
		return fmt.Errorf("domain vhost tidak valid: %q", domain)
	}
	available, enabled := m.nginxMailPaths(domain)
	if err := writeRootFile(available, []byte(localMailVhostBlock(domain, phpFPMsock())), 0644); err != nil {
		return err
	}
	if err := ensureSymlink(available, enabled); err != nil {
		return err
	}
	return nil
}

func ensureSymlink(target, link string) error {
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if rmErr := os.Remove(link); rmErr != nil {
			_ = runRoot("rm", "-f", link)
		}
	}
	if err := os.Symlink(target, link); err != nil && !os.IsExist(err) {
		if runErr := runRoot("ln", "-sf", target, link); runErr != nil {
			return err
		}
	}
	return nil
}

func runRoot(name string, args ...string) error {
	_, err := runRootCombined(name, args...)
	return err
}

func runRootCombined(name string, args ...string) ([]byte, error) {
	if os.Geteuid() == 0 {
		return exec.Command(name, args...).CombinedOutput()
	}
	sudoArgs := append([]string{"-n", name}, args...)
	return exec.Command("sudo", sudoArgs...).CombinedOutput()
}

func (m *Manager) ensureLocalMailFirewall() string {
	results := []string{}
	ports := []int{25, 465, 587, 993, 143, 995, 110}
	mode := m.pdnsMgr.GetMode()

	if _, err := exec.LookPath("ufw"); err == nil {
		out, _ := exec.Command("ufw", "status").Output()
		if strings.Contains(string(out), "Status: active") {
			for _, port := range ports {
				switch mode {
				case "vps":
					_ = runRoot("ufw", "allow", fmt.Sprintf("%d/tcp", port))
				case "local":
					for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
						_ = runRoot("ufw", "allow", "from", cidr, "to", "any", "port", fmt.Sprintf("%d", port), "proto", "tcp")
					}
				default:
					_ = runRoot("ufw", "allow", "from", "100.64.0.0/10", "to", "any", "port", fmt.Sprintf("%d", port), "proto", "tcp")
				}
			}
			_ = runRoot("ufw", "reload")
			switch mode {
			case "vps":
				results = append(results, "ufw: port mail dibuka untuk publik (0.0.0.0/0)")
			case "local":
				results = append(results, "ufw: port mail dibuka untuk jaringan lokal LAN")
			default:
				results = append(results, "ufw: port mail dibuka untuk NetBird 100.64.0.0/10")
			}
		}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		out, _ := exec.Command("firewall-cmd", "--state").Output()
		if strings.TrimSpace(string(out)) == "running" {
			for _, port := range ports {
				switch mode {
				case "vps":
					_ = runRoot("firewall-cmd", "--permanent", fmt.Sprintf("--add-port=%d/tcp", port))
				case "local":
					for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
						_ = runRoot("firewall-cmd", "--permanent", "--add-rich-rule",
							fmt.Sprintf("rule family=ipv4 source address=%s port port=%d protocol=tcp accept", cidr, port))
					}
				default:
					_ = runRoot("firewall-cmd", "--permanent", "--add-rich-rule",
						fmt.Sprintf("rule family=ipv4 source address=100.64.0.0/10 port port=%d protocol=tcp accept", port))
				}
			}
			_ = runRoot("firewall-cmd", "--reload")
			switch mode {
			case "vps":
				results = append(results, "firewalld: port mail dibuka untuk publik")
			case "local":
				results = append(results, "firewalld: port mail dibuka untuk jaringan lokal LAN")
			default:
				results = append(results, "firewalld: port mail dibuka untuk NetBird 100.64.0.0/10")
			}
		}
	}
	if len(results) == 0 {
		return "tidak ada firewall lokal aktif"
	}
	return strings.Join(results, "; ")
}

func hasToolPath(defaultPath, cmdName string) bool {
	if defaultPath != "" {
		if fi, err := os.Stat(defaultPath); err == nil && !fi.IsDir() {
			return true
		}
	}
	if p, err := exec.LookPath(cmdName); err == nil && p != "" {
		return true
	}
	return false
}

func hasStalwartUnit() bool {
	if _, err := os.Stat("/etc/systemd/system/stalwart.service"); err == nil {
		return true
	}
	return exec.Command("systemctl", "cat", "stalwart.service").Run() == nil
}

func (m *Manager) ensureMailStackInstalled(ctx context.Context, cb func(string), domain string) error {
	var missing []string
	if !hasToolPath("/usr/local/bin/stalwart", "stalwart") || !hasStalwartUnit() {
		missing = append(missing, "Stalwart")
	}
	if !hasToolPath("/usr/local/bin/stalwart-cli", "stalwart-cli") {
		missing = append(missing, "stalwart-cli")
	}
	if _, err := os.Stat("/var/www/snappymail/index.php"); err != nil {
		missing = append(missing, "SnappyMail")
	}
	if len(missing) == 0 {
		return nil
	}
	if m.ensureStackFn == nil {
		return fmt.Errorf("komponen Mail Stack belum terpasang (%s); jalankan 'wdev install mail' terlebih dahulu", strings.Join(missing, ", "))
	}
	if cb != nil {
		cb(fmt.Sprintf("  Komponen Mail Stack belum lengkap (%s); memasang otomatis...", strings.Join(missing, ", ")))
	}
	if err := m.ensureStackFn(ctx, cb, domain); err != nil {
		return fmt.Errorf("gagal memasang Mail Stack (%s): %w", strings.Join(missing, ", "), err)
	}
	return nil
}

func (m *Manager) ensureStalwartDirsOwnership(stDir string) error {
	if _, err := exec.Command("id", "-u", "stalwart").Output(); err != nil {
		return fmt.Errorf("user sistem 'stalwart' belum tersedia; instalasi Stalwart belum lengkap")
	}
	targets := []string{"/etc/stalwart", "/var/log/stalwart", "/var/lib/stalwart/", stDir}
	if resolved, err := filepath.EvalSymlinks("/var/lib/stalwart"); err == nil && resolved != "" {
		targets = append(targets, resolved)
	}
	seen := make(map[string]bool)
	var unique []string
	for _, t := range targets {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		if _, err := os.Stat(t); err == nil {
			unique = append(unique, t)
		}
	}
	if len(unique) == 0 {
		return nil
	}
	if out, err := runRootCombined("chown", append([]string{"-R", "stalwart:stalwart"}, unique...)...); err != nil {
		return fmt.Errorf("gagal mengatur izin direktori Stalwart (%v): %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) ensureLocalMailStack() error {
	if !hasToolPath("/usr/local/bin/stalwart", "stalwart") || !hasStalwartUnit() {
		return fmt.Errorf("Stalwart Mail Server belum terpasang (/usr/local/bin/stalwart atau unit systemd stalwart.service tidak ditemukan)")
	}
	if !hasToolPath("/usr/local/bin/stalwart-cli", "stalwart-cli") {
		return fmt.Errorf("stalwart-cli belum terpasang (/usr/local/bin/stalwart-cli tidak ditemukan)")
	}

	baseDir := m.mailDataDir()
	stDir := filepath.Join(baseDir, "stalwart")
	snDir := filepath.Join(baseDir, "snappymail")
	for _, dir := range []string{stDir, snDir, "/etc/stalwart", "/var/log/stalwart"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			_ = runRoot("mkdir", "-p", dir)
		}
	}

	if _, err := os.Lstat("/var/lib/stalwart"); err != nil {
		_ = os.MkdirAll("/var/lib", 0755)
		if err := os.Symlink(stDir, "/var/lib/stalwart"); err != nil {
			_ = runRoot("ln", "-sfn", stDir, "/var/lib/stalwart")
		}
	}

	if err := m.ensureStalwartDirsOwnership(stDir); err != nil {
		return err
	}
	_ = m.ensureLocalMailFirewall()

	if out, err := runRootCombined("systemctl", "enable", "--now", "stalwart"); err != nil {
		detail := strings.TrimSpace(string(out))
		if j := stalwartServiceFailureDetail(); j != "" {
			detail = j
		}
		return fmt.Errorf("gagal mengaktifkan/menjalankan service stalwart (%v): %s", err, detail)
	}
	if exec.Command("systemctl", "is-active", "--quiet", "stalwart").Run() != nil {
		if j := stalwartServiceFailureDetail(); j != "" {
			return fmt.Errorf("service stalwart tidak berjalan setelah diaktifkan: %s", j)
		}
		return fmt.Errorf("service stalwart tidak berjalan setelah diaktifkan")
	}
	return nil
}

func (m *Manager) ensureWebmailFallbackVhost() error {
	name := "wdev_webmail.conf"
	available := filepath.Join(m.cfg.NginxSitesAvail, name)
	enabled := filepath.Join(m.cfg.NginxSitesEnable, name)
	content := fmt.Sprintf(`# Managed by wdev (fallback SnappyMail)
server {
	listen 80;
	server_name webmail.test;
	root /var/www/snappymail;
	index index.php index.html;
	location / { try_files $uri $uri/ /index.php?$query_string; }
	location ~ \.php$ {
		include fastcgi_params;
		fastcgi_pass unix:%s;
		fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
	}
	location ~ ^/(data|config)/ { deny all; return 404; }
}
`, phpFPMsock())
	if err := writeRootFile(available, []byte(content), 0644); err != nil {
		return err
	}
	return ensureSymlink(available, enabled)
}

// -----------------------------------------------------------------------------
// Manager Public Interface
// -----------------------------------------------------------------------------

func probeTCPPort(host string, port int, timeout time.Duration) bool {
	if host == "" || port <= 0 || port > 65535 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *Manager) checkStalwartPorts() []PortStatus {
	defs := []struct {
		port  int
		label string
	}{
		{25, "SMTP"},
		{465, "SMTPS"},
		{587, "Submission"},
		{993, "IMAPS"},
		{8080, "Admin HTTP"},
	}
	probe := m.portProbeFn
	if probe == nil {
		probe = probeTCPPort
	}

	publicIP := ""
	if m.pdnsMgr != nil && m.pdnsMgr.GetMode() == "vps" {
		if ip := strings.TrimSpace(m.pdnsMgr.LoadConfig().VPSPublicIP); ip != "" {
			publicIP = ip
		}
	}

	results := make([]PortStatus, len(defs))
	var wg sync.WaitGroup
	for i, d := range defs {
		wg.Add(1)
		go func(idx, p int, lbl string) {
			defer wg.Done()
			localOpen := probe("127.0.0.1", p, 250*time.Millisecond) || probe("::1", p, 150*time.Millisecond)
			pubOpen := false
			if publicIP != "" {
				pubOpen = probe(publicIP, p, 300*time.Millisecond)
			}
			results[idx] = PortStatus{
				Port:         p,
				Label:        lbl,
				Open:         localOpen || pubOpen,
				Local:        localOpen,
				Public:       pubOpen,
				InternalOnly: p == 8080,
			}
		}(i, d.port, d.label)
	}
	wg.Wait()
	return results
}

func isPublicIPBehindNAT(publicIP string) bool {
	publicIP = strings.TrimSpace(publicIP)
	if publicIP == "" {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil && ip4.String() == publicIP {
			return false
		}
	}
	return true
}

func (m *Manager) GetStatus() (MailStatus, error) {
	mode := "edge"
	var behindNAT bool
	if m.pdnsMgr != nil {
		mode = m.pdnsMgr.GetMode()
		if mode == "vps" {
			behindNAT = isPublicIPBehindNAT(m.pdnsMgr.LoadConfig().VPSPublicIP)
		}
	}
	status := MailStatus{
		Mode:         mode,
		BehindNAT:    behindNAT,
		DomainRelays: make(map[string]RelayConfig),
		Domains:      make([]DomainSummary, 0),
	}

	// 1. Cek status service stalwart & aksesibilitas port TCP
	if err := exec.Command("systemctl", "is-active", "--quiet", "stalwart").Run(); err == nil {
		status.StalwartActive = true
	}
	status.StalwartPorts = m.checkStalwartPorts()

	// 2. Cek SnappyMail
	if _, err := os.Stat("/var/www/snappymail/index.php"); err == nil {
		status.SnappyMailActive = true
	}

	// 3. Storage Free
	if out, err := exec.Command("df", "-h", "/").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 4 {
				status.StorageFree = fields[3]
			}
		}
	}

	// 4. Kredensial admin domain (/etc/wdev/mail-admin.cred)
	adminUsers := make(map[string]string)
	adminCredPath := filepath.Join(m.cfg.WdevEtc, "mail-admin.cred")
	if data, err := readRootFile(adminCredPath); err == nil {
		var credMap map[string]struct {
			Email     string `json:"email"`
			Password  string `json:"password"`
			UpdatedAt string `json:"updatedAt"`
		}
		if err := json.Unmarshal(data, &credMap); err == nil {
			for d, info := range credMap {
				adminUsers[strings.ToLower(d)] = info.Email
			}
		}
	}

	// 5. Relay per domain (/etc/wdev/mail-relay-domains.cred)
	domainRelayPath := filepath.Join(m.cfg.WdevEtc, "mail-relay-domains.cred")
	if data, err := readRootFile(domainRelayPath); err == nil {
		var rMap map[string]struct {
			Provider    string `json:"provider"`
			Host        string `json:"host"`
			Port        int    `json:"port"`
			ImplicitTLS bool   `json:"implicitTls"`
			Username    string `json:"username"`
			UpdatedAt   string `json:"updatedAt"`
		}
		if err := json.Unmarshal(data, &rMap); err == nil {
			for d, r := range rMap {
				status.DomainRelays[d] = RelayConfig{
					Domain:      d,
					Provider:    r.Provider,
					Host:        r.Host,
					Port:        r.Port,
					ImplicitTLS: r.ImplicitTLS,
					Username:    r.Username,
					UpdatedAt:   r.UpdatedAt,
				}
			}
		}
	}

	// 6. Global Relay (/etc/wdev/mail-relay.cred)
	globalRelayPath := filepath.Join(m.cfg.WdevEtc, "mail-relay.cred")
	if data, err := readRootFile(globalRelayPath); err == nil {
		var r struct {
			Provider    string `json:"provider"`
			Host        string `json:"host"`
			Port        int    `json:"port"`
			ImplicitTLS bool   `json:"implicitTls"`
			Username    string `json:"username"`
			UpdatedAt   string `json:"updatedAt"`
		}
		if err := json.Unmarshal(data, &r); err == nil && r.Host != "" {
			status.GlobalRelay = &RelayConfig{
				Domain:      "global",
				Provider:    r.Provider,
				Host:        r.Host,
				Port:        r.Port,
				ImplicitTLS: r.ImplicitTLS,
				Username:    r.Username,
				UpdatedAt:   r.UpdatedAt,
			}
		}
	}

	// 7. Daftar domain aktif: gabungkan semua sumber agar domain yang baru
	// di-setup tetap tampil walau kredensial admin lama masih dipakai.
	seen := make(map[string]bool)
	addDomain := func(name string) {
		d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		status.Domains = append(status.Domains, DomainSummary{
			Domain:    d,
			AdminURL:  m.adminURL(d),
			Webmail:   m.webmailURL(d),
			AdminUser: adminUsers[d],
		})
	}
	for d := range adminUsers {
		addDomain(d)
	}
	for d := range m.loadDomainsState() {
		addDomain(d)
	}
	for d := range status.DomainRelays {
		addDomain(d)
	}
	if status.StalwartActive {
		for _, d := range m.stalwartDomainNames() {
			addDomain(d)
		}
	}
	sort.Slice(status.Domains, func(i, j int) bool { return status.Domains[i].Domain < status.Domains[j].Domain })

	// 8. Routing efektif dari Stalwart (bukan hanya file kredensial)
	if info, err := m.GetOutboundRoutes(); err == nil {
		status.RelayOnly = info.RelayOnly
		status.OutboundDefault = info.Default
		status.OutboundRoutes = info.Domains
	}

	return status, nil
}

// stalwartDomainNames mengembalikan nama domain yang benar-benar terdaftar di
// Stalwart, sehingga domain hasil setup selalu muncul di dashboard.
func (m *Manager) stalwartDomainNames() []string {
	adminEmail, adminPass, err := m.pickValidAdminCred("")
	if err != nil {
		return nil
	}
	out, err := m.runStalwartCli(adminEmail, adminPass, "query", "Domain", "--fields", "id,name", "--json")
	if err != nil {
		return nil
	}
	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &item); err == nil && item.Name != "" {
			names = append(names, item.Name)
		}
	}
	return names
}

func (m *Manager) GetDomainAccounts(domainFilter string) ([]DomainAccounts, error) {
	adminEmail, adminPass, err := m.pickValidAdminCred(domainFilter)
	if err != nil {
		return nil, fmt.Errorf("kredensial admin Stalwart belum ada atau tidak valid: %w", err)
	}

	// 1. Query Domain dari Stalwart
	domOut, err := m.runStalwartCli(adminEmail, adminPass, "query", "Domain", "--fields", "id,name,enable", "--json")
	if err != nil {
		return nil, fmt.Errorf("gagal query Domain Stalwart: %w", err)
	}

	type domItem struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Enable bool   `json:"enable"`
	}
	var domains []domItem
	scanner := bufio.NewScanner(bytes.NewReader(domOut))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var d domItem
		if err := json.Unmarshal([]byte(line), &d); err == nil && d.ID != "" {
			domains = append(domains, d)
		}
	}

	// 2. Query Account dari Stalwart
	accOut, err := m.runStalwartCli(adminEmail, adminPass, "query", "Account", "--fields", "id,name,emailAddress,domainId,roles", "--json")
	if err != nil {
		return nil, fmt.Errorf("gagal query Account Stalwart: %w", err)
	}

	type accItem struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		EmailAddress string `json:"emailAddress"`
		DomainID     string `json:"domainId"`
		Roles        struct {
			Type string `json:"@type"`
		} `json:"roles"`
	}
	var rawAccounts []accItem
	accScanner := bufio.NewScanner(bytes.NewReader(accOut))
	for accScanner.Scan() {
		line := strings.TrimSpace(accScanner.Text())
		if line == "" {
			continue
		}
		var a accItem
		if err := json.Unmarshal([]byte(line), &a); err == nil && a.ID != "" {
			rawAccounts = append(rawAccounts, a)
		}
	}

	// 3. Baca Relay per domain
	domainRelays := make(map[string]RelayConfig)
	relayPath := filepath.Join(m.cfg.WdevEtc, "mail-relay-domains.cred")
	if rData, err := readRootFile(relayPath); err == nil {
		var rMap map[string]struct {
			Provider    string `json:"provider"`
			Host        string `json:"host"`
			Port        int    `json:"port"`
			ImplicitTLS bool   `json:"implicitTls"`
			Username    string `json:"username"`
			UpdatedAt   string `json:"updatedAt"`
		}
		if err := json.Unmarshal(rData, &rMap); err == nil {
			for d, r := range rMap {
				domainRelays[d] = RelayConfig{
					Domain:      d,
					Provider:    r.Provider,
					Host:        r.Host,
					Port:        r.Port,
					ImplicitTLS: r.ImplicitTLS,
					Username:    r.Username,
					UpdatedAt:   r.UpdatedAt,
				}
			}
		}
	}

	// 4. Susun grup per domain
	var results []DomainAccounts
	for _, d := range domains {
		if domainFilter != "" && d.Name != domainFilter {
			continue
		}
		group := DomainAccounts{
			Domain:   d.Name,
			Enabled:  d.Enable,
			AdminURL: m.adminURL(d.Name),
			Webmail:  m.webmailURL(d.Name),
			Accounts: make([]MailAccount, 0),
		}

		if r, ok := domainRelays[d.Name]; ok {
			group.Relay = &r
		}

		for _, a := range rawAccounts {
			if a.DomainID == d.ID {
				email := a.EmailAddress
				if email == "" {
					email = fmt.Sprintf("%s@%s", a.Name, d.Name)
				}
				group.Accounts = append(group.Accounts, MailAccount{
					ID:       a.ID,
					Name:     a.Name,
					Email:    email,
					IsAdmin:  a.Roles.Type == "Admin",
					DomainID: a.DomainID,
				})
			}
		}

		results = append(results, group)
	}

	return results, nil
}

var mailUserPartRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,63}$`)

func (m *Manager) AddAccount(email, password string) error {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return fmt.Errorf("email dan password wajib diisi")
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("format email tidak valid")
	}
	userPart := parts[0]
	if !mailUserPartRe.MatchString(userPart) {
		return fmt.Errorf("bagian username email tidak valid: %q", userPart)
	}
	domainPart := strings.TrimSuffix(parts[1], ".")
	if !validMailDomain(domainPart) {
		return fmt.Errorf("domain email tidak valid: %s", domainPart)
	}

	adminEmail, adminPass, err := m.ensureStalwartMailDomain(domainPart)
	if err != nil {
		return fmt.Errorf("gagal memastikan domain Stalwart: %w", err)
	}

	did, err := m.findDomainID(adminEmail, adminPass, domainPart)
	if err != nil || did == "" {
		return fmt.Errorf("domain '%s' tidak ditemukan di Stalwart", domainPart)
	}

	accID, _, _ := m.findAccountByUsername(adminEmail, adminPass, did, userPart)
	credBytes, err := json.Marshal(map[string]any{
		"0": map[string]any{
			"@type":  "Password",
			"secret": password,
		},
	})
	if err != nil {
		return fmt.Errorf("gagal menyusun payload kredensial: %w", err)
	}
	credJSON := string(credBytes)

	if accID != "" {
		out, err := m.runStalwartCli(adminEmail, adminPass, "update", "Account", accID, "--field", "credentials="+credJSON)
		if err != nil || !strings.Contains(strings.ToLower(string(out)), "account") {
			return fmt.Errorf("gagal memperbarui akun '%s': %s", email, string(out))
		}
	} else {
		out, err := m.runStalwartCli(adminEmail, adminPass, "create", "Account/User",
			"--field", "name="+userPart,
			"--field", "domainId="+did,
			"--field", "credentials="+credJSON,
		)
		if err != nil || !strings.Contains(strings.ToLower(string(out)), "account") {
			if strings.Contains(strings.ToLower(string(out)), "weak") {
				return fmt.Errorf("password ditolak Stalwart (terlalu lemah): minimal 8 karakter")
			}
			return fmt.Errorf("gagal membuat akun '%s': %s", email, string(out))
		}
	}

	_ = m.writeSnappyMailDomainConfig(domainPart)
	return nil
}

// DeleteAccount menghapus akun email domain dari Stalwart.
func (m *Manager) DeleteAccount(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || !validMailDomain(parts[1]) {
		return fmt.Errorf("format email tidak valid")
	}
	adminEmail, adminPass, err := m.pickValidAdminCred(parts[1])
	if err != nil {
		return fmt.Errorf("kredensial admin Stalwart belum valid: %w", err)
	}
	did, err := m.findDomainID(adminEmail, adminPass, parts[1])
	if err != nil || did == "" {
		return fmt.Errorf("domain '%s' tidak ditemukan di Stalwart", parts[1])
	}
	accID, _, _ := m.findAccountByUsername(adminEmail, adminPass, did, parts[0])
	if accID == "" {
		return fmt.Errorf("akun '%s' tidak ditemukan", email)
	}
	out, err := m.runStalwartCli(adminEmail, adminPass, "delete", "--ids", accID, "Account")
	if err != nil {
		return fmt.Errorf("gagal menghapus akun: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if accID, _, _ = m.findAccountByUsername(adminEmail, adminPass, did, parts[0]); accID != "" {
		return fmt.Errorf("akun '%s' masih ada setelah dihapus", email)
	}
	return nil
}

// ChangeAccountPassword memperbarui kata sandi akun email yang sudah terdaftar di Stalwart.
func (m *Manager) ChangeAccountPassword(email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return fmt.Errorf("email dan password wajib diisi")
	}
	if strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("password tidak boleh mengandung baris baru")
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || !mailUserPartRe.MatchString(parts[0]) || !validMailDomain(parts[1]) {
		return fmt.Errorf("format email tidak valid")
	}
	domainPart := parts[1]

	adminEmail, adminPass, err := m.pickValidAdminCred(domainPart)
	if err != nil {
		return fmt.Errorf("kredensial admin Stalwart belum valid: %w", err)
	}
	did, err := m.findDomainID(adminEmail, adminPass, domainPart)
	if err != nil || did == "" {
		return fmt.Errorf("domain '%s' tidak ditemukan di Stalwart", domainPart)
	}
	accID, _, _ := m.findAccountByUsername(adminEmail, adminPass, did, parts[0])
	if accID == "" {
		return fmt.Errorf("akun '%s' tidak ditemukan", email)
	}

	credBytes, err := json.Marshal(map[string]any{
		"0": map[string]any{
			"@type":  "Password",
			"secret": password,
		},
	})
	if err != nil {
		return fmt.Errorf("gagal menyusun payload kredensial: %w", err)
	}

	out, err := m.runStalwartCli(adminEmail, adminPass, "update", "Account", accID, "--field", "credentials="+string(credBytes))
	if err != nil || !strings.Contains(strings.ToLower(string(out)), "account") {
		if strings.Contains(strings.ToLower(string(out)), "weak") {
			return fmt.Errorf("password ditolak Stalwart (terlalu lemah): minimal 8 karakter")
		}
		return fmt.Errorf("gagal mengubah password akun '%s': %s", email, string(out))
	}

	// Bila password akun admin domain ikut diganti, sinkronkan mail-admin.cred agar panel tetap bisa mengelola Stalwart.
	if stored, ok := m.getAdminCredMap()[domainPart]; ok && strings.EqualFold(stored.Email, email) {
		_ = m.saveAdminCred(domainPart, email, password)
	}
	return nil
}

func (m *Manager) SetRelay(opts SetRelayOptions) error {
	opts.Domain = strings.TrimSpace(opts.Domain)
	if opts.Domain == "" {
		return fmt.Errorf("domain wajib diisi")
	}
	cleanDom := strings.ToLower(strings.TrimSuffix(opts.Domain, "."))

	adminEmail, adminPass, err := m.pickValidAdminCred(cleanDom)
	if err != nil || adminEmail == "" {
		return fmt.Errorf("kredensial admin Stalwart belum ada atau tidak valid. Jalankan admin-reset terlebih dahulu")
	}

	prov := strings.ToLower(strings.TrimSpace(opts.Provider))
	if prov == "disable" || prov == "none" || prov == "off" {
		if cleanDom == RelayScopeAll {
			return m.RemoveRelay(RelayScopeAll)
		}
		previous := ""
		if prev, ok := m.loadDomainRelayCred(cleanDom); ok {
			previous = strings.ToLower(prev.Provider)
		}
		if err := m.removeDomainRelay(adminEmail, adminPass, cleanDom); err != nil {
			return err
		}
		// Provider baru = relay default efektif setelah override dihapus.
		effective := "local"
		if cred, ok := m.loadGlobalRelayCred(); ok {
			effective = strings.ToLower(cred.Provider)
		}
		m.setLastRelayDNS(m.reconcileRelayDNS(cleanDom, previous, effective))
		return nil
	}
	if prov == "local" || prov == "direct" || prov == "mx" {
		previous := ""
		if cleanDom == RelayScopeAll {
			if prev, ok := m.loadGlobalRelayCred(); ok {
				previous = strings.ToLower(prev.Provider)
			}
		} else if prev, ok := m.loadDomainRelayCred(cleanDom); ok {
			previous = strings.ToLower(prev.Provider)
		}
		if err := m.applyLocalSMTP(adminEmail, adminPass, cleanDom); err != nil {
			return err
		}
		if cleanDom != RelayScopeAll {
			m.setLastRelayDNS(m.reconcileRelayDNS(cleanDom, previous, "local"))
		}
		return nil
	}

	host := opts.Host
	port := opts.Port
	implicitTLS := false
	user := opts.Username
	pass := opts.Password

	switch prov {
	case "resend":
		if pass == "" {
			if cleanDom != RelayScopeAll {
				if prev, ok := m.loadDomainRelayCred(cleanDom); ok && strings.EqualFold(prev.Provider, "resend") {
					pass = prev.Password
				}
			}
			if pass == "" {
				if prev, ok := m.loadGlobalRelayCred(); ok && strings.EqualFold(prev.Provider, "resend") {
					pass = prev.Password
				}
			}
		}
		if pass == "" {
			return fmt.Errorf("API Key Resend wajib diisi (kosongkan hanya saat mengubah relay yang sudah ada)")
		}
		host = "smtp.resend.com"
		port = 465
		implicitTLS = true
		user = "resend"
	case "brevo":
		if pass == "" {
			if prev, ok := m.loadDomainRelayCred(cleanDom); ok && strings.EqualFold(prev.Provider, "brevo") {
				pass = prev.Password
			} else if prev, ok := m.loadGlobalRelayCred(); ok && strings.EqualFold(prev.Provider, "brevo") {
				pass = prev.Password
			}
		}
		if pass == "" {
			return fmt.Errorf("SMTP Key Brevo wajib diisi (kosongkan hanya saat mengubah relay yang sudah ada)")
		}
		host = "smtp-relay.brevo.com"
		if port <= 0 {
			port = 587
		}
		implicitTLS = false
		if user == "" {
			user = pass
		}
	case "custom":
		if host == "" {
			return fmt.Errorf("host SMTP relay wajib diisi")
		}
		if port <= 0 {
			port = 587
		}
		implicitTLS = (port == 465 || port == 2465)
	default:
		return fmt.Errorf("provider relay tidak dikenal: %s (pilihan: local, resend, brevo, custom, off)", opts.Provider)
	}

	previous := ""
	if cleanDom == RelayScopeAll {
		if prev, ok := m.loadGlobalRelayCred(); ok {
			previous = strings.ToLower(prev.Provider)
		}
	} else if prev, ok := m.loadDomainRelayCred(cleanDom); ok {
		previous = strings.ToLower(prev.Provider)
	}
	if err := m.applyRelay(adminEmail, adminPass, cleanDom, prov, host, port, implicitTLS, user, pass); err != nil {
		return err
	}
	if cleanDom == RelayScopeAll {
		agg := RelayDNSReport{
			Domain:           RelayScopeAll,
			PreviousProvider: previous,
			Provider:         prov,
			Removed:          []string{},
			Added:            []string{},
			Kept:             []string{},
			Manual:           []string{},
		}
		for _, dom := range m.configuredMailDomains() {
			if strings.EqualFold(m.ActiveRelayProvider(dom), prov) {
				dRep := m.reconcileRelayDNS(dom, previous, prov)
				m.syncProviderRelayDNS(dom, prov, pass, &dRep)
				agg.Removed = append(agg.Removed, dRep.Removed...)
				agg.Added = append(agg.Added, dRep.Added...)
				agg.Kept = append(agg.Kept, dRep.Kept...)
				agg.Manual = append(agg.Manual, dRep.Manual...)
				if dRep.VerificationTriggered {
					agg.VerificationTriggered = true
				}
			}
		}
		m.setLastRelayDNS(agg)
	} else {
		rep := m.reconcileRelayDNS(cleanDom, previous, prov)
		m.syncProviderRelayDNS(cleanDom, prov, pass, &rep)
		m.setLastRelayDNS(rep)
	}
	return nil
}

func (m *Manager) configuredMailDomains() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(d string) {
		d = cleanMailDomain(d)
		if d != "" && d != RelayScopeAll && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for d := range m.getAdminCredMap() {
		add(d)
	}
	for d := range m.loadDomainsState() {
		add(d)
	}
	if data, err := readRootFile(m.domainRelayCredPath()); err == nil {
		var rMap map[string]relayCredential
		if json.Unmarshal(data, &rMap) == nil {
			for d := range rMap {
				add(d)
			}
		}
	}
	sort.Strings(out)
	return out
}

// AdminCredential mengembalikan kredensial admin domain tanpa mencetaknya ke log.
func (m *Manager) AdminCredential(domain string) (string, string, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return "", "", fmt.Errorf("domain wajib diisi")
	}
	entry, ok := m.getAdminCredMap()[domain]
	if !ok || entry.Email == "" || entry.Password == "" {
		return "", "", fmt.Errorf("kredensial admin untuk %s belum tercatat", domain)
	}
	return entry.Email, entry.Password, nil
}

func (m *Manager) ResetAdmin(domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("domain wajib diisi")
	}
	_, _, err := m.resetAdminInternal(domain)
	return err
}

// validMailDomain memvalidasi FQDN sebelum dipakai sebagai nama file/vhost.
func validMailDomain(d string) bool {
	d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
	if d == "" || len(d) > 253 || !strings.Contains(d, ".") || strings.Contains(d, "..") {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func (m *Manager) SetupMail(ctx context.Context, cb func(string), opts SetupMailOptions) error {
	if cb == nil {
		cb = func(string) {}
	}
	opts.Domain = strings.TrimSpace(opts.Domain)
	if opts.Domain == "" {
		return fmt.Errorf("domain wajib diisi")
	}
	cleanDom := strings.ToLower(strings.TrimSuffix(opts.Domain, "."))
	if !validMailDomain(cleanDom) {
		return fmt.Errorf("domain tidak valid: %s", opts.Domain)
	}

	cb(fmt.Sprintf("=== MEMULAI SETUP MAIL SERVER & DOMAIN (%s) ===", cleanDom))

	mode := m.pdnsMgr.GetMode()
	localNB := m.pdnsMgr.GetLocalNetbirdIP()
	switch mode {
	case "local":
		cb("✓ Mode Lokal Standalone aktif: berjalan mandiri tanpa NetBird / Edge Gateway")
	case "vps":
		localNB = "127.0.0.1"
		cb("✓ Mode Full VPS Standalone aktif: menggunakan loopback lokal (127.0.0.1) tanpa NetBird")
	default:
		if localNB == "" {
			return fmt.Errorf("IP NetBird lokal (wt0) tidak terdeteksi. Pastikan NetBird aktif: 'netbird status'")
		}
		cb(fmt.Sprintf("✓ IP NetBird lokal terdeteksi: %s", localNB))
	}

	// 1. Pastikan mail stack lokal terpasang, service aktif, dan ter-bootstrap (Stalwart & SnappyMail)
	cb("[1/4] Menyiapkan Mail Stack Lokal (Stalwart + SnappyMail)...")
	if err := m.ensureMailStackInstalled(ctx, cb, cleanDom); err != nil {
		return err
	}
	if err := m.ensureLocalMailStack(); err != nil {
		return err
	}
	if err := m.ensureStalwartBootstrap(cleanDom); err != nil {
		return err
	}

	// Stalwart hanya punya satu public URL. Jangan timpa domain pertama yang
	// sudah tercatat agar link admin domain lama tidak berubah.
	envPath := "/etc/stalwart/stalwart.env"
	envData, _ := readRootFile(envPath)
	if !strings.Contains(string(envData), "STALWART_PUBLIC_URL=") {
		scheme := "https"
		if mode != "edge" && !opts.SSL {
			scheme = "http"
		}
		pubURL := fmt.Sprintf("%s://mailadmin.%s", scheme, cleanDom)
		if err := setStalwartEnv("STALWART_PUBLIC_URL", pubURL); err != nil {
			return fmt.Errorf("gagal menyimpan STALWART_PUBLIC_URL: %w", err)
		}
		if err := restartStalwart(); err != nil {
			return err
		}
	}

	// Ensure Domain & Admin user di Stalwart
	adminUser, adminPass, err := m.ensureStalwartMailDomain(cleanDom)
	if err != nil {
		return fmt.Errorf("gagal mendaftarkan domain/admin di Stalwart: %w", err)
	}
	cb(fmt.Sprintf("  ✓ Akun Admin Stalwart siap: %s", adminUser))

	// Prekonfigurasi domain di SnappyMail
	_ = m.writeSnappyMailDomainConfig(cleanDom)

	relayProv := strings.ToLower(strings.TrimSpace(opts.RelayProv))
	if relayProv == "local" || relayProv == "direct" || relayProv == "mx" {
		relayProv = "local"
	}
	_, hasGlobalRelay := m.loadGlobalRelayCred()
	_, hasDomainRelay := m.loadDomainRelayCred(cleanDom)
	usesExternalRelay := (relayProv != "" && relayProv != "local") || (relayProv == "" && (hasGlobalRelay || hasDomainRelay))
	spfRecord := `"v=spf1 mx ~all"`
	if usesExternalRelay {
		spfRecord = `"v=spf1 -all"`
	}

	// 2. Standarisasi DNS Record di PowerDNS (wajib untuk Edge/VPS; opsional + /etc/hosts di mode Lokal)
	edgeCfg := m.pdnsMgr.LoadConfig()
	dnsIP := strings.TrimSpace(edgeCfg.VPSPublicIP)
	if dnsIP == "" && mode == "local" {
		if lan := pdns.DetectLocalLANIP(); lan != "" {
			dnsIP = lan
		} else {
			dnsIP = "127.0.0.1"
		}
	}
	zone, zoneErr := m.pdnsMgr.FindMatchingZone(cleanDom)
	if mode == "local" {
		m.ensureMailHostsEntries(cleanDom, "127.0.0.1")
		cb(fmt.Sprintf("✓ Entri /etc/hosts lokal ditambahkan untuk webmail.%s & mailadmin.%s", cleanDom, cleanDom))
	}

	if zoneErr != nil || !edgeCfg.Configured || dnsIP == "" {
		if mode != "local" {
			if zoneErr != nil {
				return fmt.Errorf("gagal menemukan zone DNS yang cocok untuk %s: %w", cleanDom, zoneErr)
			}
			return fmt.Errorf("IP publik VPS belum dikonfigurasi; atur VPS_PUBLIC_IP atau jalankan 'wdev pdns connect'")
		}
		cb(fmt.Sprintf("[2/4] PowerDNS / zone %s belum tersedia; lewati record PowerDNS (memakai /etc/hosts lokal).", cleanDom))
	} else {
		cb(fmt.Sprintf("[2/4] Membuat DNS Record di PowerDNS (Zone: %s, IP: %s)...", zone, dnsIP))

		type dnsRec struct {
			name        string
			rtype       string
			content     string
			keep        bool
			skipOnExist bool
		}
		records := []dnsRec{
			{name: "mail." + cleanDom, rtype: "A", content: dnsIP},
			{name: "mx." + cleanDom, rtype: "A", content: dnsIP},
			{name: "webmail." + cleanDom, rtype: "A", content: dnsIP},
			{name: "mailadmin." + cleanDom, rtype: "A", content: dnsIP},
			{name: "@", rtype: "MX", content: "10 mail." + cleanDom + ".", skipOnExist: true},
			{name: "@", rtype: "TXT", content: spfRecord, keep: true, skipOnExist: true},
			{name: "_dmarc", rtype: "TXT", content: fmt.Sprintf("\"v=DMARC1; p=none; sp=none; rua=mailto:dmarc@%s\"", cleanDom), skipOnExist: true},
		}

		var dnsFailed []string
		for _, rec := range records {
			existing, err := m.pdnsMgr.LookupRecords(zone, rec.name, rec.rtype)
			if err != nil {
				dnsFailed = append(dnsFailed, fmt.Sprintf("%s %s: %v", rec.rtype, rec.name, err))
				cb(fmt.Sprintf("    gagal membaca record: %v", err))
				continue
			}
			if rec.skipOnExist && len(existing) > 0 {
				if rec.rtype == "TXT" && containsSPF(existing) {
					cb(fmt.Sprintf(" -> %s %s: SPF sudah ada, tidak ditimpa", rec.rtype, rec.name))
					continue
				}
				if !rec.keep && !recordContentExists(existing, rec.content) {
					msg := fmt.Sprintf("%s %s sudah ada dan berbeda (%s); tidak ditimpa", rec.rtype, rec.name, strings.Join(existing, ", "))
					if rec.rtype == "MX" {
						dnsFailed = append(dnsFailed, msg)
					}
					cb(" -> " + msg)
					continue
				}
			}
			cb(fmt.Sprintf(" -> %s %s -> %s", rec.rtype, rec.name, rec.content))
			var addErr error
			if rec.keep {
				addErr = m.pdnsMgr.AddRecordKeep(zone, rec.name, rec.rtype, rec.content, 300)
			} else {
				addErr = m.pdnsMgr.AddRecord(zone, rec.name, rec.rtype, rec.content, 300)
			}
			if addErr != nil {
				dnsFailed = append(dnsFailed, fmt.Sprintf("%s %s: %v", rec.rtype, rec.name, addErr))
				cb(fmt.Sprintf("    gagal: %v", addErr))
			}
		}
		if len(dnsFailed) > 0 && mode != "local" {
			return fmt.Errorf("DNS inbound gagal dibuat: %s", strings.Join(dnsFailed, "; "))
		}
		if mode != "local" {
			if ptrName, ok := ptrMatches(m.pdnsMgr, dnsIP, "mail."+cleanDom); !ok {
				cb(fmt.Sprintf(" ! PTR/rDNS IP %s belum/tidak cocok ke mail.%s (terbaca: %s). Set PTR di panel VPS.", dnsIP, cleanDom, ptrName))
			} else {
				cb(" ✓ PTR/rDNS VPS cocok dengan mail." + cleanDom)
			}
		}
		if usesExternalRelay {
			cb("✓ DNS inbound (A, MX) & kebijakan outbound (SPF -all, DMARC) disinkronkan")
		} else {
			cb("✓ DNS inbound (A, MX) & kebijakan outbound (SPF mx ~all, DMARC) disinkronkan")
		}
	}

	switch mode {
	case "local":
		cb("[3/4] Menyiapkan Virtual Host Nginx Lokal (Mode Lokal Standalone)...")
		_ = m.ensureLocalMailFirewall()
		_ = m.ensureWebmailFallbackVhost()
		_ = m.ensureLocalMailVhost(cleanDom)
		_ = reloadLocalNginx()
		sslActive := false
		if opts.SSL {
			ok, sslErr := m.issueLocalMailSSLVhost(cleanDom)
			if sslErr != nil {
				cb(fmt.Sprintf("  ! Sertifikat SSL lokal gagal dibuat (%v); memakai HTTP", sslErr))
			} else if ok {
				sslActive = true
				cb("  ✓ Sertifikat SSL lokal (mkcert/OpenSSL) + HTTPS vhost aktif!")
			}
		}
		scheme := "http"
		if sslActive {
			scheme = "https"
		}
		adminURL := fmt.Sprintf("%s://mailadmin.%s", scheme, cleanDom)
		webmailURL := fmt.Sprintf("%s://webmail.%s", scheme, cleanDom)
		cb(fmt.Sprintf("  Panel Admin : %s", adminURL))
		cb(fmt.Sprintf("  Webmail     : %s", webmailURL))
		m.recordDomainState(cleanDom, sslActive, adminURL, webmailURL)

	case "vps":
		cb("[3/4] Menyiapkan Virtual Host Nginx Lokal untuk Full VPS...")
		_ = m.ensureLocalMailFirewall()
		_ = m.ensureLocalMailVhost(cleanDom)
		_ = m.ensureWebmailFallbackVhost()
		_ = reloadLocalNginx()
		sslActive := false
		if opts.SSL {
			sslActive = m.issueVPSLetsEncrypt(cleanDom, cb)
		}
		scheme := "http"
		if sslActive {
			scheme = "https"
		}
		adminURL := fmt.Sprintf("%s://mailadmin.%s", scheme, cleanDom)
		webmailURL := fmt.Sprintf("%s://webmail.%s", scheme, cleanDom)
		cb("✓ Gateway & Virtual Host Nginx lokal berhasil dikonfigurasi!")
		cb(fmt.Sprintf("  Panel Admin : %s", adminURL))
		cb(fmt.Sprintf("  Webmail     : %s", webmailURL))
		m.recordDomainState(cleanDom, sslActive, adminURL, webmailURL)

	default: // "edge"
		cb("[3/4] Mendaftarkan VPS MX Inbound Gateway (Queue) & Reverse Proxy ke Edge Agent...")
		agentPayload := map[string]any{
			"domain": cleanDom, "target_ip": localNB, "ssl": opts.SSL,
		}

		agentResp, err := m.pdnsMgr.AgentPost("/api/v1/mail/setup-domain", agentPayload)
		if err != nil {
			cb(fmt.Sprintf("Peringatan Edge Agent VPS: %v", err))
		} else {
			var respMap map[string]any
			_ = json.Unmarshal(agentResp, &respMap)
			cb("✓ Gateway & Reverse Proxy VPS berhasil didaftarkan!")
			adminURL, _ := respMap["admin_url"].(string)
			if adminURL != "" {
				cb(fmt.Sprintf("  Panel Admin : %s", adminURL))
			}
			webmailURL, _ := respMap["webmail_url"].(string)
			if webmailURL != "" {
				cb(fmt.Sprintf("  Webmail     : %s", webmailURL))
			}
			sslActive := opts.SSL
			if v, ok := respMap["ssl"].(string); ok {
				sslActive = strings.HasPrefix(v, "active")
			}
			m.recordDomainState(cleanDom, sslActive, adminURL, webmailURL)
		}
	}

	// 4. Konfigurasi routing outbound (relay eksternal atau SMTP lokal bawaan Stalwart).
	switch {
	case relayProv == "local":
		cb("Mengonfigurasi outbound ke SMTP lokal Stalwart (direct MX)...")
		if err := m.SetRelay(SetRelayOptions{Domain: cleanDom, Provider: "local"}); err != nil {
			cb(fmt.Sprintf("  [WARN] Gagal menyetel routing SMTP lokal: %v", err))
		} else {
			cb(" ✓ Outbound memakai SMTP lokal Stalwart (direct MX)")
			for _, line := range relayDNSPlan("local", cleanDom) {
				cb("   " + line)
			}
		}
	case relayProv != "":
		cb(fmt.Sprintf("Mengonfigurasi relay outbound (%s)...", relayProv))
		relayOpts := SetRelayOptions{
			Domain: cleanDom, Provider: relayProv, Host: opts.RelayHost, Port: opts.RelayPort, Username: opts.RelayUser, Password: opts.RelayPass,
		}
		if err := m.SetRelay(relayOpts); err != nil {
			return fmt.Errorf("relay outbound gagal dikonfigurasi: %w", err)
		}
		cb(fmt.Sprintf(" ✓ Outbound relay %s berhasil dikonfigurasi", relayProv))
		dnsRep := m.LastRelayDNSReport()
		for _, item := range dnsRep.Added {
			cb("   + DNS Relay dibuat: " + item)
		}
		for _, item := range dnsRep.Kept {
			cb("   = DNS Relay sudah ada: " + item)
		}
		if dnsRep.VerificationTriggered {
			cb("   ✓ Verifikasi domain Resend dipicu otomatis")
		}
		for _, line := range dnsRep.Manual {
			cb("   " + line)
		}
	default:
		if _, err := m.ensureDefaultRelay(adminUser, adminPass); err == nil {
			cb(" ✓ Domain memakai relay default yang sudah ada")
			if cred, ok := m.loadGlobalRelayCred(); ok {
				rep := RelayDNSReport{Domain: cleanDom, Provider: strings.ToLower(cred.Provider)}
				m.syncProviderRelayDNS(cleanDom, cred.Provider, cred.Password, &rep)
				for _, item := range rep.Added {
					cb("   + DNS Relay dibuat: " + item)
				}
				for _, item := range rep.Kept {
					cb("   = DNS Relay sudah ada: " + item)
				}
				if rep.VerificationTriggered {
					cb("   ✓ Verifikasi domain Resend dipicu otomatis")
				}
				for _, line := range rep.Manual {
					cb("   " + line)
				}
			}
		} else {
			if err := m.ensureLocalSMTPDefault(adminUser, adminPass); err != nil {
				cb(fmt.Sprintf(" [INFO] Routing outbound default memakai SMTP lokal Stalwart (%v)", err))
			} else {
				cb(" ✓ Belum ada relay eksternal; memakai SMTP lokal bawaan Stalwart (direct MX)")
			}
			for _, line := range relayDNSPlan("local", cleanDom) {
				cb("   " + line)
			}
		}
	}

	if exec.Command("systemctl", "is-active", "--quiet", "stalwart").Run() != nil {
		detail := stalwartServiceFailureDetail()
		if detail != "" {
			return fmt.Errorf("setup domain selesai namun service stalwart tidak aktif: %s", detail)
		}
		return fmt.Errorf("setup domain selesai namun service stalwart tidak aktif")
	}

	modeBanner := "HYBRID EDGE"
	switch mode {
	case "local":
		modeBanner = "LOKAL STANDALONE"
	case "vps":
		modeBanner = "VPS STANDALONE"
	}
	cb("================================================================================")
	cb(fmt.Sprintf("✓ EMAIL SERVER & WEBMAIL (%s) BERHASIL AKTIF!", modeBanner))
	cb("================================================================================")
	cb(fmt.Sprintf("  Domain Email        : %s", cleanDom))
	cb(fmt.Sprintf("  Webmail Klien       : %s", m.webmailURL(cleanDom)))
	cb(fmt.Sprintf("  Panel Admin (WebUI) : %s", m.adminURL(cleanDom)))
	if adminUser != "" && adminPass != "" {
		cb(fmt.Sprintf("  Username Admin      : %s", adminUser))
		cb("  Password Admin      : tersimpan di /etc/wdev/mail-admin.cred (jalankan: wdev mail admin-cred <domain>)")
	}
	return nil
}
