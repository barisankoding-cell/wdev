package site

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

type Manager struct {
	cfg   *config.Config
	store *store.Store
}

type CreateOptions struct {
	Domain     string `json:"domain"`
	RootPath   string `json:"root_path"`
	PHPVersion string `json:"php_version"`
	Framework  string `json:"framework"`
	ProxyPort  int    `json:"proxy_port"`
	SSLEnabled bool   `json:"ssl_enabled"`
}

type TemplateData struct {
	Domain        string
	EffectiveRoot string
	Framework     string
	ProxyPort     int
	SSLEnabled    bool
	CertFile      string
	KeyFile       string
	PHPSocket     string
}

func New(cfg *config.Config, s *store.Store) *Manager {
	return &Manager{
		cfg:   cfg,
		store: s,
	}
}

func (m *Manager) ListSites() ([]store.SiteRecord, error) {
	return m.store.ListSites()
}

func (m *Manager) CreateSite(opts CreateOptions) error {
	opts.Domain = strings.ToLower(strings.TrimSpace(opts.Domain))
	if opts.Domain == "" {
		return fmt.Errorf("domain tidak boleh kosong")
	}

	if opts.Framework == "proxy" {
		if opts.ProxyPort <= 0 || opts.ProxyPort > 65535 {
			return fmt.Errorf("port proxy tidak valid: %d", opts.ProxyPort)
		}
	} else {
		if opts.RootPath == "" {
			opts.RootPath = filepath.Join("/var/www", opts.Domain)
		}
		if opts.PHPVersion == "" {
			opts.PHPVersion = "8.4"
		}
	}

	effectiveRoot := opts.RootPath
	switch opts.Framework {
	case "laravel", "ci4", "symfony":
		effectiveRoot = filepath.Join(opts.RootPath, "public")
	}

	// Prepare directories
	if opts.Framework != "proxy" {
		_ = os.MkdirAll(effectiveRoot, 0755)
		indexFile := filepath.Join(effectiveRoot, "index.php")
		if _, err := os.Stat(indexFile); os.IsNotExist(err) {
			placeholder := fmt.Sprintf("<?php echo '<h1>Selamat Datang di %s</h1><p>Ditenagai oleh wdev & Nginx.</p>'; ?>", opts.Domain)
			_ = os.WriteFile(indexFile, []byte(placeholder), 0644)
		}
	}

	// SSL certs
	certDir := "/etc/ssl/local-certs"
	_ = os.MkdirAll(certDir, 0755)
	certFile := filepath.Join(certDir, opts.Domain+".pem")
	keyFile := filepath.Join(certDir, opts.Domain+"-key.pem")

	if opts.SSLEnabled {
		if err := m.generateSSL(opts.Domain, certFile, keyFile); err != nil {
			// If SSL fails, disable SSL to not break Nginx
			opts.SSLEnabled = false
		}
	}

	phpSocket := fmt.Sprintf("/run/php/php%s-fpm.sock", opts.PHPVersion)

	tpl, err := template.New("nginx").Parse(nginxVHostTemplate)
	if err != nil {
		return fmt.Errorf("gagal parse template nginx: %w", err)
	}

	data := TemplateData{
		Domain:        opts.Domain,
		EffectiveRoot: effectiveRoot,
		Framework:     opts.Framework,
		ProxyPort:     opts.ProxyPort,
		SSLEnabled:    opts.SSLEnabled,
		CertFile:      certFile,
		KeyFile:       keyFile,
		PHPSocket:     phpSocket,
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("gagal render template nginx: %w", err)
	}

	// Write to sites-available
	_ = os.MkdirAll(m.cfg.NginxSitesAvail, 0755)
	_ = os.MkdirAll(m.cfg.NginxSitesEnable, 0755)

	availPath := filepath.Join(m.cfg.NginxSitesAvail, opts.Domain)
	enablePath := filepath.Join(m.cfg.NginxSitesEnable, opts.Domain)

	if err := os.WriteFile(availPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("gagal menulis berkas nginx: %w", err)
	}

	// Create symlink
	_ = os.Remove(enablePath)
	if err := os.Symlink(availPath, enablePath); err != nil {
		return fmt.Errorf("gagal membuat symlink nginx: %w", err)
	}

	// Test Nginx
	testCmd := exec.Command("nginx", "-t")
	if out, err := testCmd.CombinedOutput(); err != nil {
		_ = os.Remove(enablePath)
		_ = os.Remove(availPath)
		return fmt.Errorf("konfigurasi nginx tidak valid: %s", string(out))
	}

	// Reload Nginx
	_ = exec.Command("systemctl", "reload", "nginx").Run()

	// Update /etc/hosts
	m.addHostsEntry(opts.Domain)

	// Save to DB
	record := store.SiteRecord{
		Domain:     opts.Domain,
		RootPath:   opts.RootPath,
		PHPVersion: opts.PHPVersion,
		Framework:  opts.Framework,
		ProxyPort:  opts.ProxyPort,
		SSLEnabled: opts.SSLEnabled,
		CreatedAt:  time.Now(),
	}
	_ = m.store.SaveSite(record)

	return nil
}

func (m *Manager) DeleteSite(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	availPath := filepath.Join(m.cfg.NginxSitesAvail, domain)
	enablePath := filepath.Join(m.cfg.NginxSitesEnable, domain)

	_ = os.Remove(enablePath)
	_ = os.Remove(availPath)

	// Reload Nginx
	_ = exec.Command("systemctl", "reload", "nginx").Run()

	// Remove from /etc/hosts
	m.removeHostsEntry(domain)

	// Remove from store
	_ = m.store.DeleteSite(domain)

	return nil
}

func (m *Manager) generateSSL(domain, certFile, keyFile string) error {
	// Try mkcert first
	if _, err := exec.LookPath("mkcert"); err == nil {
		cmd := exec.Command("mkcert", "-cert-file", certFile, "-key-file", keyFile, domain)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Fallback to OpenSSL self-signed
	cmd := exec.Command("openssl", "req", "-x509", "-nodes", "-days", "365",
		"-newkey", "rsa:2048",
		"-keyout", keyFile,
		"-out", certFile,
		"-subj", fmt.Sprintf("/CN=%s", domain))
	return cmd.Run()
}

func (m *Manager) addHostsEntry(domain string) {
	content, err := os.ReadFile(m.cfg.HostsFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(content), "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		for _, f := range fields {
			if f == domain {
				return // already exists
			}
		}
	}

	entry := fmt.Sprintf("127.0.0.1 %s # wdev-managed\n", domain)
	f, err := os.OpenFile(m.cfg.HostsFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		_, _ = f.WriteString(entry)
		return
	}
	if os.Geteuid() != 0 {
		cmd := exec.Command("sudo", "tee", "-a", m.cfg.HostsFile)
		cmd.Stdin = strings.NewReader(entry)
		_ = cmd.Run()
	}
}

func (m *Manager) removeHostsEntry(domain string) {
	content, err := os.ReadFile(m.cfg.HostsFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(content), "\n")
	var newLines []string
	for _, l := range lines {
		fields := strings.Fields(l)
		hasDomain := false
		for _, f := range fields {
			if f == domain {
				hasDomain = true
				break
			}
		}
		if !hasDomain {
			newLines = append(newLines, l)
		}
	}
	data := []byte(strings.Join(newLines, "\n"))
	if err := os.WriteFile(m.cfg.HostsFile, data, 0644); err != nil && os.Geteuid() != 0 {
		cmd := exec.Command("sudo", "tee", m.cfg.HostsFile)
		cmd.Stdin = strings.NewReader(string(data))
		_ = cmd.Run()
	}
}

func (m *Manager) SyncAll() error {
	sites, err := m.ListSites()
	if err != nil {
		return err
	}
	certDir := "/etc/ssl/local-certs"
	_ = os.MkdirAll(certDir, 0755)
	for _, s := range sites {
		m.addHostsEntry(s.Domain)
		if s.SSLEnabled {
			certFile := filepath.Join(certDir, s.Domain+".pem")
			keyFile := filepath.Join(certDir, s.Domain+"-key.pem")
			if _, err := os.Stat(certFile); os.IsNotExist(err) {
				_ = m.generateSSL(s.Domain, certFile, keyFile)
			}
		}
	}
	testCmd := exec.Command("nginx", "-t")
	if os.Geteuid() != 0 {
		testCmd = exec.Command("sudo", "nginx", "-t")
	}
	_ = testCmd.Run()

	reloadCmd := exec.Command("systemctl", "reload", "nginx")
	if os.Geteuid() != 0 {
		reloadCmd = exec.Command("sudo", "systemctl", "reload", "nginx")
	}
	return reloadCmd.Run()
}
