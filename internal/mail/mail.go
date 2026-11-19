package mail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"wdev/internal/config"
	"wdev/internal/pdns"
	"wdev/internal/store"
)

type Manager struct {
	cfg     *config.Config
	store   *store.Store
	pdnsMgr *pdns.Manager
}

type MailStatus struct {
	StalwartActive   bool                   `json:"stalwart_active"`
	SnappyMailActive bool                   `json:"snappymail_active"`
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
	Provider    string `json:"provider"` // "resend", "brevo", "custom", "direct"
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
	Provider string `json:"provider"` // "resend", "brevo", "custom", "direct"
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type SetupMailOptions struct {
	Domain    string `json:"domain"`
	SSL       bool   `json:"ssl"`
	RelayProv string `json:"relay_provider"` // "direct", "resend", "brevo", "custom"
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
	if os.Geteuid() == 0 {
		_ = os.MkdirAll(dir, 0755)
		return os.WriteFile(path, data, perm)
	}
	tmp := fmt.Sprintf("/tmp/wdev_mail_%d.tmp", time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	defer os.Remove(tmp)
	_ = exec.Command("sudo", "-n", "mkdir", "-p", dir).Run()
	_ = exec.Command("sudo", "-n", "install", "-m", fmt.Sprintf("%04o", perm), tmp, path).Run()
	return nil
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
	sudoArgs := append([]string{"-n", "env"}, envVars...)
	sudoArgs = append(sudoArgs, cliPath)
	sudoArgs = append(sudoArgs, args...)
	cmd := exec.Command("sudo", sudoArgs...)
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
	return writeRootFile(path, []byte(content), 0644)
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
	return writeRootFile(path, []byte(content), 0644)
}

func restartStalwart() error {
	if os.Geteuid() == 0 {
		_ = exec.Command("systemctl", "restart", "stalwart").Run()
	} else {
		_ = exec.Command("sudo", "-n", "systemctl", "restart", "stalwart").Run()
	}
	if !waitStalwartReady(20 * time.Second) {
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
	_ = restartStalwart()

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
	tmpFile := fmt.Sprintf("/tmp/stalwart_bs_%d.json", time.Now().UnixNano())
	_ = os.WriteFile(tmpFile, pBytes, 0600)
	defer os.Remove(tmpFile)

	out, err := m.runStalwartCli("wdev-bootstrap", recPass, "update", "Bootstrap", "--file", tmpFile)
	_ = unsetStalwartEnv("STALWART_RECOVERY_ADMIN")
	_ = restartStalwart()

	if err != nil || !strings.Contains(strings.ToLower(string(out)), "updated bootstrap") {
		return fmt.Errorf("bootstrap Stalwart gagal: %s", string(out))
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

func (m *Manager) ensureSnappyMailDomain(domain string) error {
	snapRoot := "/var/www/snappymail"
	if _, err := os.Stat(snapRoot); err != nil {
		return nil
	}
	snapDomDir := filepath.Join(snapRoot, "data", "_data_", "_default_", "domains")
	_ = os.MkdirAll(snapDomDir, 0700)

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
	_ = exec.Command("chown", "www-data:www-data", snapDomDir, targetFile).Run()
	return nil
}

func (m *Manager) ensureLocalMailStack() error {
	baseDir := "/mnt/f7d8a899-1e8d-4c44-afaf-2f7c69f2cce5/mail-data"
	if _, err := os.Stat(baseDir); err != nil {
		baseDir = "/var/mail-data"
	}
	stDir := filepath.Join(baseDir, "stalwart")
	snDir := filepath.Join(baseDir, "snappymail")
	_ = os.MkdirAll(stDir, 0755)
	_ = os.MkdirAll(snDir, 0755)
	_ = os.MkdirAll("/var/www/snappymail", 0755)

	if _, err := os.Lstat("/var/lib/stalwart"); err != nil {
		_ = os.MkdirAll("/var/lib", 0755)
		_ = os.Symlink(stDir, "/var/lib/stalwart")
	}

	_ = exec.Command("systemctl", "enable", "--now", "stalwart").Run()

	localVhost := "/etc/nginx/sites-available/wdev_webmail.conf"
	if _, err := os.Stat(localVhost); err != nil {
		phpSock := "/run/php/php8.4-fpm.sock"
		if _, err := os.Stat(phpSock); err != nil {
			matches, _ := filepath.Glob("/run/php/*fpm.sock")
			if len(matches) > 0 {
				phpSock = matches[0]
			}
		}
		vhostContent := fmt.Sprintf(`# Managed by wdev (Local SnappyMail Web Client)
server {
    listen 80;
    server_name webmail.* webmail.test;

    root /var/www/snappymail;
    index index.php index.html;
    client_max_body_size 50M;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:%s;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }

    location ~ ^/(data|config)/ {
        deny all;
        return 404;
    }
}
`, phpSock)
		_ = writeRootFile(localVhost, []byte(vhostContent), 0644)
		_ = os.Symlink(localVhost, "/etc/nginx/sites-enabled/wdev_webmail.conf")
		_ = exec.Command("systemctl", "reload", "nginx").Run()
	}
	return nil
}

// -----------------------------------------------------------------------------
// Manager Public Interface
// -----------------------------------------------------------------------------

func (m *Manager) GetStatus() (MailStatus, error) {
	status := MailStatus{
		DomainRelays: make(map[string]RelayConfig),
		Domains:      make([]DomainSummary, 0),
	}

	// 1. Cek status service stalwart
	if err := exec.Command("systemctl", "is-active", "--quiet", "stalwart").Run(); err == nil {
		status.StalwartActive = true
	}

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
	adminCredPath := filepath.Join(m.cfg.WdevEtc, "mail-admin.cred")
	if data, err := readRootFile(adminCredPath); err == nil {
		var credMap map[string]struct {
			Email     string `json:"email"`
			Password  string `json:"password"`
			UpdatedAt string `json:"updatedAt"`
		}
		if err := json.Unmarshal(data, &credMap); err == nil {
			for d, info := range credMap {
				status.Domains = append(status.Domains, DomainSummary{
					Domain:    d,
					AdminURL:  fmt.Sprintf("https://mailadmin.%s/admin/", d),
					Webmail:   fmt.Sprintf("https://webmail.%s", d),
					AdminUser: info.Email,
				})
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

	return status, nil
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
			AdminURL: fmt.Sprintf("https://mailadmin.%s/admin/", d.Name),
			Webmail:  fmt.Sprintf("https://webmail.%s", d.Name),
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
	domainPart := strings.TrimSuffix(parts[1], ".")

	adminEmail, adminPass, err := m.ensureStalwartMailDomain(domainPart)
	if err != nil {
		return fmt.Errorf("gagal memastikan domain Stalwart: %w", err)
	}

	did, err := m.findDomainID(adminEmail, adminPass, domainPart)
	if err != nil || did == "" {
		return fmt.Errorf("domain '%s' tidak ditemukan di Stalwart", domainPart)
	}

	accID, _, _ := m.findAccountByUsername(adminEmail, adminPass, did, userPart)
	credJSON := fmt.Sprintf(`{"0":{"@type":"Password","secret":"%s"}}`, password)

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

	_ = m.ensureSnappyMailDomain(domainPart)
	return nil
}

func (m *Manager) SetRelay(opts SetRelayOptions) error {
	opts.Domain = strings.TrimSpace(opts.Domain)
	if opts.Domain == "" {
		return fmt.Errorf("domain wajib diisi")
	}
	cleanDom := strings.TrimSuffix(opts.Domain, ".")

	adminEmail, adminPass, err := m.pickValidAdminCred(cleanDom)
	if err != nil || adminEmail == "" {
		return fmt.Errorf("kredensial admin Stalwart belum ada atau tidak valid. Jalankan admin-reset terlebih dahulu")
	}

	prov := strings.ToLower(opts.Provider)
	if prov == "direct" || prov == "disable" || prov == "none" {
		return m.disableRelay(adminEmail, adminPass, cleanDom)
	}

	host := opts.Host
	port := opts.Port
	implicitTLS := false
	user := opts.Username
	pass := opts.Password

	switch prov {
	case "resend":
		if pass == "" {
			return fmt.Errorf("API Key Resend wajib diisi")
		}
		host = "smtp.resend.com"
		port = 465
		implicitTLS = true
		user = "resend"
	case "brevo":
		if pass == "" {
			return fmt.Errorf("SMTP Key Brevo wajib diisi")
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
		return fmt.Errorf("provider relay tidak dikenal: %s (pilihan: resend, brevo, custom, direct)", opts.Provider)
	}

	return m.applyRelay(adminEmail, adminPass, cleanDom, prov, host, port, implicitTLS, user, pass)
}

func (m *Manager) applyRelay(adminEmail, adminPass, domain, provider, host string, port int, implicitTLS bool, user, pass string) error {
	isGlobal := (domain == "global" || domain == "")
	routeName := "relay"
	if !isGlobal {
		routeName = "relay_" + strings.ReplaceAll(domain, ".", "_")
	}

	// 1. Cari MtaRoute yang ada
	rOut, _ := m.runStalwartCli(adminEmail, adminPass, "query", "MtaRoute", "--json")
	var existingID string
	scanner := bufio.NewScanner(bytes.NewReader(rOut))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &r); err == nil && r.Name == routeName {
			existingID = r.ID
			break
		}
	}

	payload := map[string]any{
		"name":        routeName,
		"host":        host,
		"port":        port,
		"protocol":    "smtp",
		"implicitTls": implicitTLS,
	}
	if user != "" {
		payload["authUsername"] = user
		payload["authSecret"] = map[string]any{"@type": "Value", "secret": pass}
	}
	pBytes, _ := json.Marshal(payload)
	tmpFile := fmt.Sprintf("/tmp/wdev_route_%d.json", time.Now().UnixNano())
	_ = os.WriteFile(tmpFile, pBytes, 0600)
	defer os.Remove(tmpFile)

	if existingID != "" {
		_, _ = m.runStalwartCli(adminEmail, adminPass, "update", "MtaRoute", existingID, "--file", tmpFile)
	} else {
		_, _ = m.runStalwartCli(adminEmail, adminPass, "create", "MtaRoute/Relay", "--file", tmpFile)
	}

	// 2. Perbarui MtaOutboundStrategy
	stratOut, err := m.runStalwartCli(adminEmail, adminPass, "get", "MtaOutboundStrategy", "--json")
	if err != nil {
		return fmt.Errorf("gagal membaca MtaOutboundStrategy: %w", err)
	}
	var stratMap map[string]any
	if err := json.Unmarshal(stratOut, &stratMap); err != nil {
		return fmt.Errorf("format MtaOutboundStrategy tidak valid: %w", err)
	}

	routeMap, _ := stratMap["route"].(map[string]any)
	if routeMap == nil {
		routeMap = make(map[string]any)
		stratMap["route"] = routeMap
	}

	if isGlobal {
		routeMap["else"] = "'relay'"
	} else {
		matchMap, _ := routeMap["match"].(map[string]any)
		if matchMap == nil {
			matchMap = make(map[string]any)
		}
		cond := fmt.Sprintf("sender_domain == '%s'", domain)
		targetThen := fmt.Sprintf("'%s'", routeName)

		// Filter out duplicate conditions
		var branches []map[string]string
		for _, v := range matchMap {
			if vm, ok := v.(map[string]any); ok {
				if ifStr, _ := vm["if"].(string); ifStr != cond {
					thenStr, _ := vm["then"].(string)
					branches = append(branches, map[string]string{"if": ifStr, "then": thenStr})
				}
			}
		}
		branches = append(branches, map[string]string{"if": cond, "then": targetThen})

		newMatch := make(map[string]any)
		for i, b := range branches {
			newMatch[fmt.Sprintf("%d", i)] = b
		}
		routeMap["match"] = newMatch
	}

	newStratBytes, _ := json.Marshal(map[string]any{"route": routeMap})
	_, _ = m.runStalwartCli(adminEmail, adminPass, "update", "MtaOutboundStrategy", "--json", string(newStratBytes))

	_ = restartStalwart()

	// 3. Simpan kredensial ke disk
	if isGlobal {
		credPath := filepath.Join(m.cfg.WdevEtc, "mail-relay.cred")
		relData := map[string]any{
			"provider":    provider,
			"host":        host,
			"port":        port,
			"implicitTls": implicitTLS,
			"username":    user,
			"password":    pass,
			"updatedAt":   time.Now().UTC().Format(time.RFC3339),
		}
		b, _ := json.MarshalIndent(relData, "", "  ")
		_ = writeRootFile(credPath, b, 0600)
	} else {
		credPath := filepath.Join(m.cfg.WdevEtc, "mail-relay-domains.cred")
		rMap := make(map[string]any)
		if curData, err := readRootFile(credPath); err == nil {
			_ = json.Unmarshal(curData, &rMap)
		}
		rMap[domain] = map[string]any{
			"provider":    provider,
			"host":        host,
			"port":        port,
			"implicitTls": implicitTLS,
			"username":    user,
			"password":    pass,
			"route":       routeName,
			"updatedAt":   time.Now().UTC().Format(time.RFC3339),
		}
		b, _ := json.MarshalIndent(rMap, "", "  ")
		_ = writeRootFile(credPath, b, 0600)
	}

	return nil
}

func (m *Manager) disableRelay(adminEmail, adminPass, domain string) error {
	isGlobal := (domain == "global" || domain == "")

	stratOut, err := m.runStalwartCli(adminEmail, adminPass, "get", "MtaOutboundStrategy", "--json")
	if err != nil {
		return fmt.Errorf("gagal membaca MtaOutboundStrategy: %w", err)
	}
	var stratMap map[string]any
	if err := json.Unmarshal(stratOut, &stratMap); err != nil {
		return fmt.Errorf("format MtaOutboundStrategy tidak valid: %w", err)
	}

	routeMap, _ := stratMap["route"].(map[string]any)
	if routeMap == nil {
		routeMap = make(map[string]any)
		stratMap["route"] = routeMap
	}

	routeName := "relay"
	if isGlobal {
		routeMap["else"] = "'mx'"
	} else {
		routeName = "relay_" + strings.ReplaceAll(domain, ".", "_")
		matchMap, _ := routeMap["match"].(map[string]any)
		if matchMap != nil {
			cond := fmt.Sprintf("sender_domain == '%s'", domain)
			var branches []map[string]string
			for _, v := range matchMap {
				if vm, ok := v.(map[string]any); ok {
					if ifStr, _ := vm["if"].(string); ifStr != cond {
						thenStr, _ := vm["then"].(string)
						branches = append(branches, map[string]string{"if": ifStr, "then": thenStr})
					}
				}
			}
			newMatch := make(map[string]any)
			for i, b := range branches {
				newMatch[fmt.Sprintf("%d", i)] = b
			}
			routeMap["match"] = newMatch
		}
	}

	newStratBytes, _ := json.Marshal(map[string]any{"route": routeMap})
	_, _ = m.runStalwartCli(adminEmail, adminPass, "update", "MtaOutboundStrategy", "--json", string(newStratBytes))

	// Hapus MtaRoute jika ada
	rOut, _ := m.runStalwartCli(adminEmail, adminPass, "query", "MtaRoute", "--json")
	scanner := bufio.NewScanner(bytes.NewReader(rOut))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &r); err == nil && r.Name == routeName && r.ID != "" {
			_, _ = m.runStalwartCli(adminEmail, adminPass, "delete", "--ids", r.ID, "MtaRoute")
			break
		}
	}

	_ = restartStalwart()

	// Bersihkan berkas kredensial
	if isGlobal {
		_ = os.Remove(filepath.Join(m.cfg.WdevEtc, "mail-relay.cred"))
		_ = exec.Command("sudo", "-n", "rm", "-f", filepath.Join(m.cfg.WdevEtc, "mail-relay.cred")).Run()
	} else {
		credPath := filepath.Join(m.cfg.WdevEtc, "mail-relay-domains.cred")
		rMap := make(map[string]any)
		if curData, err := readRootFile(credPath); err == nil {
			_ = json.Unmarshal(curData, &rMap)
			delete(rMap, domain)
			b, _ := json.MarshalIndent(rMap, "", "  ")
			_ = writeRootFile(credPath, b, 0600)
		}
	}

	return nil
}

func (m *Manager) ResetAdmin(domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("domain wajib diisi")
	}
	_, _, err := m.resetAdminInternal(domain)
	return err
}

func (m *Manager) SetupMail(ctx context.Context, cb func(string), opts SetupMailOptions) error {
	if cb == nil {
		cb = func(string) {}
	}
	opts.Domain = strings.TrimSpace(opts.Domain)
	if opts.Domain == "" {
		return fmt.Errorf("domain wajib diisi")
	}
	cleanDom := strings.TrimSuffix(opts.Domain, ".")

	cb(fmt.Sprintf("=== MEMULAI SETUP MAIL SERVER & DOMAIN (%s) ===", cleanDom))

	localNB := m.pdnsMgr.GetLocalNetbirdIP()
	if localNB == "" {
		return fmt.Errorf("IP NetBird lokal (wt0) tidak terdeteksi. Pastikan NetBird aktif: 'netbird status'")
	}
	cb(fmt.Sprintf("✓ IP NetBird lokal terdeteksi: %s", localNB))

	// 1. Pastikan mail stack lokal siap (Stalwart & SnappyMail)
	cb("[1/3] Menyiapkan Mail Stack Lokal (Storage di Harddisk PC)...")
	if err := m.ensureLocalMailStack(); err != nil {
		cb(fmt.Sprintf("  Peringatan stack lokal: %v", err))
	}
	_ = m.ensureStalwartBootstrap(cleanDom)

	// Set STALWART_PUBLIC_URL
	pubURL := fmt.Sprintf("https://mailadmin.%s", cleanDom)
	_ = setStalwartEnv("STALWART_PUBLIC_URL", pubURL)
	_ = restartStalwart()

	// Ensure Domain & Admin user di Stalwart
	adminUser, adminPass, err := m.ensureStalwartMailDomain(cleanDom)
	if err != nil {
		cb(fmt.Sprintf("  Peringatan domain Stalwart: %v", err))
	} else {
		cb(fmt.Sprintf("  ✓ Akun Admin Stalwart siap: %s", adminUser))
	}

	// Prekonfigurasi domain di SnappyMail
	_ = m.ensureSnappyMailDomain(cleanDom)

	// 2. Standarisasi DNS Record di PowerDNS VPS
	zone, err := m.pdnsMgr.FindMatchingZone(cleanDom)
	if err != nil {
		return fmt.Errorf("gagal menemukan zone DNS yang cocok untuk %s: %w", cleanDom, err)
	}
	cb(fmt.Sprintf("[2/3] Membuat Standarisasi DNS Record di PowerDNS VPS (Zone: %s)...", zone))

	records := []struct {
		name    string
		rtype   string
		content string
		ttl     int
	}{
		{"mail." + cleanDom, "A", "auto", 300},
		{"mx." + cleanDom, "A", "auto", 300},
		{"webmail." + cleanDom, "A", "auto", 300},
		{"mailadmin." + cleanDom, "A", "auto", 300},
		{"mail." + cleanDom, "TXT", "\"v=spf1 a ip4:100.64.0.0/10 -all\"", 300},
		{"mx." + cleanDom, "TXT", "\"v=spf1 a ip4:100.64.0.0/10 -all\"", 300},
		{"@", "MX", "10 mail." + cleanDom + ".", 300},
		{"@", "TXT", fmt.Sprintf("\"v=spf1 mx a:mail.%s ~all\"", cleanDom), 300},
		{"_dmarc", "TXT", fmt.Sprintf("\"v=DMARC1; p=none; sp=none; rua=mailto:admin@%s\"", cleanDom), 300},
	}

	for _, rec := range records {
		cb(fmt.Sprintf("  -> %s %s -> %s", rec.rtype, rec.name, rec.content))
		if err := m.pdnsMgr.AddRecord(zone, rec.name, rec.rtype, rec.content, rec.ttl); err != nil {
			cb(fmt.Sprintf("     (catatan DNS: %v)", err))
		}
	}
	cb("✓ Semua DNS Record Mail (A, MX, SPF, DMARC) berhasil dibuat!")

	// 3. Mendaftarkan VPS MX Inbound Gateway (Queue) & Reverse Proxy ke Lokal via Edge Agent
	cb("[3/3] Mendaftarkan VPS MX Inbound Gateway (Queue) & Reverse Proxy ke Edge Agent...")
	var relayPayload any = nil
	if opts.RelayHost != "" || (opts.RelayProv != "" && opts.RelayProv != "direct") {
		host := opts.RelayHost
		port := opts.RelayPort
		user := opts.RelayUser
		pass := opts.RelayPass
		switch strings.ToLower(opts.RelayProv) {
		case "resend":
			host = "smtp.resend.com"
			port = 465
			user = "resend"
		case "brevo":
			host = "smtp-relay.brevo.com"
			if port <= 0 {
				port = 587
			}
			if user == "" {
				user = pass
			}
		}
		if host != "" {
			relayPayload = map[string]any{
				"domain": cleanDom,
				"host":   host,
				"port":   port,
				"user":   user,
				"pass":   pass,
			}
		}
	}

	agentPayload := map[string]any{
		"domain":    cleanDom,
		"target_ip": localNB,
		"ssl":       opts.SSL,
		"relay":     relayPayload,
	}

	agentResp, err := m.pdnsMgr.AgentPost("/api/v1/mail/setup-domain", agentPayload)
	if err != nil {
		cb(fmt.Sprintf("Peringatan Edge Agent VPS: %v", err))
	} else {
		var respMap map[string]any
		_ = json.Unmarshal(agentResp, &respMap)
		cb("✓ Gateway & Reverse Proxy VPS berhasil didaftarkan!")
		if adminURL, ok := respMap["admin_url"].(string); ok {
			cb(fmt.Sprintf("  Panel Admin : %s", adminURL))
		}
		if webmailURL, ok := respMap["webmail_url"].(string); ok {
			cb(fmt.Sprintf("  Webmail     : %s", webmailURL))
		}
	}

	// 4. Konfigurasi Outbound Relay lokal di Stalwart jika ditentukan
	if opts.RelayProv != "" && strings.ToLower(opts.RelayProv) != "direct" {
		cb(fmt.Sprintf("Mengonfigurasi relay outbound (%s)...", opts.RelayProv))
		relayOpts := SetRelayOptions{
			Domain:   cleanDom,
			Provider: opts.RelayProv,
			Host:     opts.RelayHost,
			Port:     opts.RelayPort,
			Username: opts.RelayUser,
			Password: opts.RelayPass,
		}
		if err := m.SetRelay(relayOpts); err != nil {
			cb(fmt.Sprintf("  Peringatan relay: %v", err))
		} else {
			cb(fmt.Sprintf("  ✓ Outbound relay %s berhasil dikonfigurasi!", opts.RelayProv))
		}
	}

	cb("================================================================================")
	cb("✓ EMAIL SERVER & WEBMAIL (HYBRID LOCAL STORAGE) BERHASIL AKTIF!")
	cb("================================================================================")
	cb(fmt.Sprintf("  Domain Email        : %s", cleanDom))
	cb(fmt.Sprintf("  Webmail Klien       : https://webmail.%s", cleanDom))
	cb(fmt.Sprintf("  Panel Admin (WebUI) : https://mailadmin.%s/admin/", cleanDom))
	if adminUser != "" && adminPass != "" {
		cb(fmt.Sprintf("  Username Admin      : %s", adminUser))
		cb(fmt.Sprintf("  Password Admin      : %s", adminPass))
	}
	return nil
}
