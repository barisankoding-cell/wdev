package site

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

var (
	siteDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	phpVerRe     = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

// ValidateDomain memastikan domain adalah hostname valid tanpa path traversal atau karakter injeksi.
func ValidateDomain(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || len(domain) > 253 {
		return fmt.Errorf("domain tidak valid")
	}
	if !siteDomainRe.MatchString(domain) {
		return fmt.Errorf("format domain tidak valid: %q", domain)
	}
	return nil
}

// ValidateRootPath memastikan path root situs aman untuk direktif Nginx dan tidak menembus sistem.
func ValidateRootPath(rootPath string) (string, error) {
	p := strings.TrimSpace(rootPath)
	if p == "" {
		return "", fmt.Errorf("root_path tidak boleh kosong")
	}
	if strings.ContainsAny(p, " \t\r\n\x00;\"'{}()$`\\") {
		return "", fmt.Errorf("root_path mengandung karakter terlarang")
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("root_path harus berupa path absolut")
	}
	clean := filepath.Clean(p)
	allowedRoots := []string{"/var/www", "/home", "/srv", "/opt", "/mnt", "/media"}
	ok := false
	for _, base := range allowedRoots {
		if clean == base || strings.HasPrefix(clean, base+"/") {
			ok = true
			break
		}
	}
	if !ok {
		return "", fmt.Errorf("root_path harus berada di bawah /var/www, /home, /srv, /opt, atau /mnt")
	}
	return clean, nil
}

type Manager struct {
	cfg           *config.Config
	store         *store.Store
	sudoPassFn    func() string
	skipNginxExec bool
	certbotExecFn func(ctx context.Context, cb func(string), bin string, args []string) error
}

type CreateOptions struct {
	Domain        string   `json:"domain"`
	ServerAliases []string `json:"server_aliases,omitempty"`
	RootPath      string   `json:"root_path"`
	PHPVersion    string   `json:"php_version"`
	Framework     string   `json:"framework"`
	ProxyPort     int      `json:"proxy_port"`
	SSLEnabled    bool     `json:"ssl_enabled"`
	SkipHosts     bool     `json:"skip_hosts,omitempty"`
}

type StaticVHostResult struct {
	Domain   string `json:"domain"`
	Status   string `json:"status"` // "created", "exists", "skipped", "error"
	Created  bool   `json:"created"`
	RootPath string `json:"root_path,omitempty"`
	URL      string `json:"url,omitempty"`
	Message  string `json:"message,omitempty"`
}

type TemplateData struct {
	Domain         string
	ServerNames    string
	EffectiveRoot  string
	Framework      string
	ProxyPort      int
	SSLEnabled     bool
	IPv6Enabled    bool
	CertFile       string
	KeyFile        string
	PHPSocket      string
	PHPFastCGIPass string
}

func New(cfg *config.Config, s *store.Store) *Manager {
	return &Manager{
		cfg:   cfg,
		store: s,
	}
}

// NewWithSudo membuat site.Manager dengan penyedia password sudo opsional.
func NewWithSudo(cfg *config.Config, s *store.Store, sudoPassFn func() string) *Manager {
	return &Manager{
		cfg:        cfg,
		store:      s,
		sudoPassFn: sudoPassFn,
	}
}

// SetSudoPasswordFn menyetel fungsi penyedia password sudo.
func (m *Manager) SetSudoPasswordFn(fn func() string) {
	m.sudoPassFn = fn
}

// SetSkipNginxExec menonaktifkan eksekusi biner nginx/systemctl (dipakai di unit test terisolasi).
func (m *Manager) SetSkipNginxExec(skip bool) {
	m.skipNginxExec = skip
}

// SetCertbotExecFn mengganti eksekutor certbot (dipakai di unit test terisolasi).
func (m *Manager) SetCertbotExecFn(fn func(ctx context.Context, cb func(string), bin string, args []string) error) {
	m.certbotExecFn = fn
}

// HasSite memeriksa apakah virtual host untuk domain sudah terdaftar di disk atau store.
func (m *Manager) HasSite(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return false
	}
	_, exists := m.findExistingSite(domain)
	return exists
}

// GetSite mengambil metadata vhost terdaftar (beserta masa berlaku SSL jika aktif) berdasarkan domain.
func (m *Manager) GetSite(domain string) (store.SiteRecord, bool) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return store.SiteRecord{}, false
	}
	sites, err := m.ListSites()
	if err != nil {
		return store.SiteRecord{}, false
	}
	for _, s := range sites {
		if strings.EqualFold(strings.TrimSpace(s.Domain), domain) {
			return s, true
		}
	}
	return store.SiteRecord{}, false
}

func (m *Manager) sitesAvailDir() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.NginxSitesAvail) != "" {
		return strings.TrimSpace(m.cfg.NginxSitesAvail)
	}
	return "/etc/nginx/sites-available"
}

func (m *Manager) sitesEnableDir() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.NginxSitesEnable) != "" {
		return strings.TrimSpace(m.cfg.NginxSitesEnable)
	}
	return "/etc/nginx/sites-enabled"
}

func (m *Manager) webRootBase() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.WebRootBase) != "" {
		return filepath.Clean(strings.TrimSpace(m.cfg.WebRootBase))
	}
	return "/var/www"
}

func (m *Manager) validateRootPath(rootPath string) (string, error) {
	if clean, err := ValidateRootPath(rootPath); err == nil {
		return clean, nil
	} else if m.cfg != nil && strings.TrimSpace(m.cfg.WebRootBase) != "" {
		p := strings.TrimSpace(rootPath)
		if p != "" && !strings.ContainsAny(p, " \t\r\n\x00;\"'{}()$`\\") && filepath.IsAbs(p) {
			clean := filepath.Clean(p)
			base := filepath.Clean(strings.TrimSpace(m.cfg.WebRootBase))
			if clean == base || strings.HasPrefix(clean, base+"/") {
				return clean, nil
			}
		}
		return "", err
	} else {
		return "", err
	}
}

func (m *Manager) ListSites() ([]store.SiteRecord, error) {
	if m.store == nil {
		return []store.SiteRecord{}, nil
	}
	sites, err := m.store.ListSites()
	if err != nil {
		return nil, err
	}
	for i := range sites {
		var certPath string
		if confPath, ok := m.findVHostConfigPath(sites[i].Domain); ok {
			if data, readErr := m.readFile(confPath); readErr == nil {
				if enabled, provider, detectedCert := DetectSSLInfoFromConfig(string(data)); enabled {
					sites[i].SSLEnabled = true
					sites[i].SSLProvider = provider
					certPath = detectedCert
				}
			}
		}
		if sites[i].SSLEnabled {
			m.populateSiteSSLExpiry(&sites[i], certPath)
		}
	}
	return sites, nil
}

func (m *Manager) CreateSite(opts CreateOptions) error {
	opts.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(opts.Domain), "."))
	if err := ValidateDomain(opts.Domain); err != nil {
		return err
	}
	opts.Framework = strings.ToLower(strings.TrimSpace(opts.Framework))

	var cleanAliases []string
	seenAlias := map[string]bool{opts.Domain: true}
	for _, rawAlias := range opts.ServerAliases {
		alias := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawAlias), "."))
		if alias == "" || seenAlias[alias] {
			continue
		}
		if err := ValidateDomain(alias); err != nil {
			return fmt.Errorf("alias domain tidak valid (%s): %w", rawAlias, err)
		}
		seenAlias[alias] = true
		cleanAliases = append(cleanAliases, alias)
	}
	serverNames := strings.Join(append([]string{opts.Domain}, cleanAliases...), " ")

	if opts.Framework == "proxy" {
		if opts.ProxyPort <= 0 || opts.ProxyPort > 65535 {
			return fmt.Errorf("port proxy tidak valid: %d", opts.ProxyPort)
		}
	} else {
		if opts.RootPath == "" {
			opts.RootPath = filepath.Join(m.webRootBase(), opts.Domain)
		}
		cleanRoot, err := m.validateRootPath(opts.RootPath)
		if err != nil {
			return err
		}
		opts.RootPath = cleanRoot
		if opts.Framework == "static" {
			opts.PHPVersion = ""
		} else {
			if opts.PHPVersion == "" {
				opts.PHPVersion = "8.4"
			}
			opts.PHPVersion = strings.TrimSpace(opts.PHPVersion)
			if !phpVerRe.MatchString(opts.PHPVersion) {
				return fmt.Errorf("versi PHP tidak valid: %q", opts.PHPVersion)
			}
		}
	}

	effectiveRoot := opts.RootPath
	switch opts.Framework {
	case "laravel", "ci4", "symfony":
		effectiveRoot = filepath.Join(opts.RootPath, "public")
	}

	// Siapkan direktori web & file index bawaan tanpa menimpa file yang sudah ada.
	if opts.Framework == "static" {
		if err := m.ensureDir(effectiveRoot, 0755); err != nil {
			return fmt.Errorf("gagal membuat direktori root %s: %w", effectiveRoot, err)
		}
		hasExistingIndex := false
		for _, candidate := range []string{"index.html", "index.htm", "index.php"} {
			if m.pathExists(filepath.Join(effectiveRoot, candidate)) {
				hasExistingIndex = true
				break
			}
		}
		if !hasExistingIndex {
			indexHTML := filepath.Join(effectiveRoot, "index.html")
			placeholder := renderStaticDefaultHTML(opts.Domain, effectiveRoot)
			if err := m.writeFile(indexHTML, []byte(placeholder), 0644); err != nil {
				return fmt.Errorf("gagal menulis template HTML default %s: %w", indexHTML, err)
			}
		}
	} else if opts.Framework != "proxy" {
		if err := m.ensureDir(effectiveRoot, 0755); err != nil {
			return fmt.Errorf("gagal membuat direktori root %s: %w", effectiveRoot, err)
		}
		indexFile := filepath.Join(effectiveRoot, "index.php")
		if !m.pathExists(indexFile) {
			placeholder := fmt.Sprintf("<?php echo '<h1>Selamat Datang di %s</h1><p>Ditenagai oleh wdev & Nginx.</p>'; ?>", opts.Domain)
			if err := m.writeFile(indexFile, []byte(placeholder), 0644); err != nil {
				return fmt.Errorf("gagal menulis index default %s: %w", indexFile, err)
			}
		}
	}

	// SSL certs
	certDir := "/etc/ssl/local-certs"
	certFile := filepath.Join(certDir, opts.Domain+".pem")
	keyFile := filepath.Join(certDir, opts.Domain+"-key.pem")

	if opts.SSLEnabled {
		_ = m.ensureDir(certDir, 0755)
		if err := m.generateSSL(opts.Domain, certFile, keyFile); err != nil {
			// If SSL fails, disable SSL to not break Nginx
			opts.SSLEnabled = false
		}
	}

	phpSocket := ""
	phpFastCGIPass := ""
	if opts.PHPVersion != "" {
		phpFastCGIPass, phpSocket = resolvePHPFastCGIPass(opts.PHPVersion)
	}

	tpl, err := template.New("nginx").Parse(nginxVHostTemplate)
	if err != nil {
		return fmt.Errorf("gagal parse template nginx: %w", err)
	}

	data := TemplateData{
		Domain:         opts.Domain,
		ServerNames:    serverNames,
		EffectiveRoot:  effectiveRoot,
		Framework:      opts.Framework,
		ProxyPort:      opts.ProxyPort,
		SSLEnabled:     opts.SSLEnabled,
		IPv6Enabled:    hasIPv6(),
		CertFile:       certFile,
		KeyFile:        keyFile,
		PHPSocket:      phpSocket,
		PHPFastCGIPass: phpFastCGIPass,
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("gagal render template nginx: %w", err)
	}

	sitesAvail := m.sitesAvailDir()
	sitesEnable := m.sitesEnableDir()

	if err := m.ensureDir(sitesAvail, 0755); err != nil {
		return fmt.Errorf("gagal membuat direktori sites-available: %w", err)
	}
	if err := m.ensureDir(sitesEnable, 0755); err != nil {
		return fmt.Errorf("gagal membuat direktori sites-enabled: %w", err)
	}

	availPath := filepath.Join(sitesAvail, opts.Domain)
	enablePath := filepath.Join(sitesEnable, opts.Domain)

	if err := m.writeFile(availPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("gagal menulis berkas nginx: %w", err)
	}

	if err := m.ensureSymlink(availPath, enablePath); err != nil {
		_ = m.removeFile(availPath)
		return fmt.Errorf("gagal membuat symlink nginx: %w", err)
	}

	if !m.skipNginxExec {
		_ = m.ensureDir("/var/log/nginx", 0755)
		m.ensureNginxSitesEnabledIncluded()
		_, _ = m.RepairEmptyServerNamesInVHosts()

		nginxBin := findNginxBinary()
		if nginxBin == "" {
			_ = m.removeFile(enablePath)
			_ = m.removeFile(availPath)
			return fmt.Errorf("biner nginx tidak ditemukan")
		}
		if out, err := m.runPrivileged(nginxBin, "-t"); err != nil {
			_ = m.removeFile(enablePath)
			_ = m.removeFile(availPath)
			return fmt.Errorf("konfigurasi nginx tidak valid: %s", out)
		}
		_ = m.reloadOrStartNginx()
	}

	// Update /etc/hosts kecuali diminta skip (mis. domain publik di VPS / Edge Gateway)
	if !opts.SkipHosts && m.cfg != nil && m.cfg.HostsFile != "" {
		m.addHostsEntries(append([]string{opts.Domain}, cleanAliases...)...)
	}

	// Save to DB
	if m.store != nil {
		sslProvider := ""
		if opts.SSLEnabled {
			sslProvider = "local"
		}
		record := store.SiteRecord{
			Domain:      opts.Domain,
			RootPath:    opts.RootPath,
			PHPVersion:  opts.PHPVersion,
			Framework:   opts.Framework,
			ProxyPort:   opts.ProxyPort,
			SSLEnabled:  opts.SSLEnabled,
			SSLProvider: sslProvider,
			CreatedAt:   time.Now(),
		}
		_ = m.store.SaveSite(record)
	}

	return nil
}

// EnsureDomainStaticVhost menyiapkan vhost Nginx HTTP (port 80) beserta template
// index.html statis default untuk domain yang ditambahkan di PowerDNS, tanpa
// menimpa vhost atau berkas situs yang sudah ada.
func (m *Manager) EnsureDomainStaticVhost(domain string) (StaticVHostResult, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	res := StaticVHostResult{Domain: domain}
	if err := ValidateDomain(domain); err != nil {
		res.Status = "error"
		res.Message = err.Error()
		return res, err
	}

	if existingRoot, exists := m.findExistingSite(domain); exists {
		res.Status = "exists"
		res.Created = false
		res.RootPath = existingRoot
		res.URL = "http://" + domain
		res.Message = fmt.Sprintf("Virtual host untuk %s sudah ada; konfigurasi yang ada dipertahankan.", domain)
		return res, nil
	}

	if !m.skipNginxExec && findNginxBinary() == "" {
		res.Status = "skipped"
		res.Created = false
		res.Message = "Nginx belum terpasang; vhost static HTML tidak dibuat."
		return res, nil
	}

	var aliases []string
	if !strings.HasPrefix(domain, "www.") {
		aliases = append(aliases, "www."+domain)
	}

	if err := m.CreateSite(CreateOptions{
		Domain:        domain,
		ServerAliases: aliases,
		Framework:     "static",
		SSLEnabled:    false,
	}); err != nil {
		res.Status = "error"
		res.Message = err.Error()
		return res, err
	}

	res.Status = "created"
	res.Created = true
	res.RootPath = filepath.Join(m.webRootBase(), domain)
	res.URL = "http://" + domain
	res.Message = fmt.Sprintf("VHost static HTML dibuat di %s (%s)", res.RootPath, res.URL)
	return res, nil
}

func (m *Manager) findExistingSite(domain string) (string, bool) {
	sitesAvail := m.sitesAvailDir()
	for _, name := range []string{domain, domain + ".conf", "wdev_" + domain + ".conf"} {
		p := filepath.Join(sitesAvail, name)
		if m.pathExists(p) {
			return m.extractRootFromVHost(p, domain), true
		}
	}
	if m.store != nil {
		if sites, err := m.store.ListSites(); err == nil {
			for _, s := range sites {
				if strings.EqualFold(strings.TrimSpace(s.Domain), domain) {
					return s.RootPath, true
				}
			}
		}
	}
	return "", false
}

func (m *Manager) extractRootFromVHost(confPath, domain string) string {
	data, err := os.ReadFile(confPath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "root ") {
				return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "root "), ";"))
			}
		}
	}
	return filepath.Join(m.webRootBase(), domain)
}

func (m *Manager) DeleteSite(domain string) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	if err := ValidateDomain(domain); err != nil {
		return err
	}

	// Hapus semua variasi berkas konfigurasi & symlink Nginx (tanpa menyentuh folder web root_path)
	// serta gagalkan operasi bila berkas disk tidak dapat dihapus agar tidak muncul kembali saat SyncFromDisk.
	candidates := []string{
		domain,
		domain + ".conf",
		"wdev_" + domain,
		"wdev_" + domain + ".conf",
		domain + "-le-ssl.conf",
	}
	availDir := m.sitesAvailDir()
	enableDir := m.sitesEnableDir()
	for _, name := range candidates {
		if err := m.removeFile(filepath.Join(enableDir, name)); err != nil {
			return fmt.Errorf("gagal menghapus symlink Nginx %s: %w", name, err)
		}
		if err := m.removeFile(filepath.Join(availDir, name)); err != nil {
			return fmt.Errorf("gagal menghapus konfigurasi Nginx %s: %w", name, err)
		}
	}

	if m.cfg != nil && strings.TrimSpace(m.cfg.WdevEtc) != "" {
		expDir := filepath.Join(strings.TrimSpace(m.cfg.WdevEtc), "exposures")
		for _, name := range []string{domain, domain + ".conf"} {
			if err := m.removeFile(filepath.Join(expDir, name)); err != nil {
				return fmt.Errorf("gagal menghapus penanda eksposur %s: %w", name, err)
			}
		}
	}

	if !m.skipNginxExec {
		_ = m.reloadOrStartNginx()
	}

	if m.cfg != nil && m.cfg.HostsFile != "" {
		m.removeHostsEntry(domain)
		if !strings.HasPrefix(domain, "www.") {
			m.removeHostsEntry("www." + domain)
		}
	}

	if m.store != nil {
		if err := m.store.DeleteSite(domain); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) generateSSL(domain, certFile, keyFile string) error {
	if err := ValidateDomain(domain); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "wdev-local-ssl-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpCert := filepath.Join(tmpDir, "cert.pem")
	tmpKey := filepath.Join(tmpDir, "key.pem")

	generated := false
	// Try mkcert first
	if _, err := exec.LookPath("mkcert"); err == nil {
		cmd := exec.Command("mkcert", "-cert-file", tmpCert, "-key-file", tmpKey, "--", domain)
		if err := cmd.Run(); err == nil {
			generated = true
		}
	}

	// Fallback to OpenSSL self-signed
	if !generated {
		cmd := exec.Command("openssl", "req", "-x509", "-nodes", "-days", "365",
			"-newkey", "rsa:2048",
			"-keyout", tmpKey,
			"-out", tmpCert,
			"-subj", fmt.Sprintf("/CN=%s", domain))
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	certBytes, err := os.ReadFile(tmpCert)
	if err != nil {
		return err
	}
	keyBytes, err := os.ReadFile(tmpKey)
	if err != nil {
		return err
	}
	if err := m.writeFile(certFile, certBytes, 0644); err != nil {
		return err
	}
	return m.writeFile(keyFile, keyBytes, 0600)
}

type fsProbe struct {
	statFn func(string) (os.FileInfo, error)
	globFn func(string) ([]string, error)
	readFn func(string) ([]byte, error)
}

func resolvePHPFastCGIPass(phpVersion string) (string, string) {
	return resolvePHPFastCGIPassWithFS(phpVersion, fsProbe{
		statFn: os.Stat,
		globFn: filepath.Glob,
		readFn: os.ReadFile,
	})
}

func resolvePHPFastCGIPassWithFS(phpVersion string, probe fsProbe) (string, string) {
	ver := strings.TrimSpace(phpVersion)
	verNoDot := strings.ReplaceAll(ver, ".", "")

	// 1. Socket spesifik versi PHP target (Debian/Ubuntu, Remi Multi-PHP SCL, dll.)
	var versionedCandidates []string
	if ver != "" {
		versionedCandidates = []string{
			fmt.Sprintf("/run/php/php%s-fpm.sock", ver),
			fmt.Sprintf("/var/opt/remi/php%s/run/php-fpm/www.sock", verNoDot),
			fmt.Sprintf("/run/php%s-php-fpm/www.sock", verNoDot),
			fmt.Sprintf("/run/php-fpm/php%s-fpm.sock", ver),
			fmt.Sprintf("/run/php-fpm/php%s-www.sock", verNoDot),
		}
	}
	for _, cand := range versionedCandidates {
		if _, err := probe.statFn(cand); err == nil {
			return "unix:" + cand, cand
		}
	}

	// 2. Pool SCL Remi untuk versi spesifik bila memakai TCP/socket kustom
	if verNoDot != "" {
		for _, poolConf := range []string{
			fmt.Sprintf("/var/opt/remi/php%s/php-fpm.d/www.conf", verNoDot),
			fmt.Sprintf("/etc/opt/remi/php%s/php-fpm.d/www.conf", verNoDot),
		} {
			if pass, sock := parsePHPPoolListenFile(poolConf, probe.readFn); pass != "" {
				return pass, sock
			}
		}
	}

	// 3. Konfigurasi pool PHP-FPM standar Fedora/RHEL/Arch (/etc/php-fpm.d/www.conf)
	_, debErr := probe.statFn("/etc/php")
	hasDebianPHPLayout := debErr == nil
	if !hasDebianPHPLayout {
		if pass, sock := parsePHPPoolListenFile("/etc/php-fpm.d/www.conf", probe.readFn); pass != "" {
			return pass, sock
		}
	}

	// 4. Socket PHP-FPM generik yang aktif di disk
	for _, cand := range []string{
		"/run/php-fpm/www.sock",
		"/run/php-fpm/php-fpm.sock",
		"/run/php/php-fpm.sock",
	} {
		if _, err := probe.statFn(cand); err == nil {
			return "unix:" + cand, cand
		}
	}
	if matches, err := probe.globFn("/run/php-fpm/*.sock"); err == nil && len(matches) > 0 {
		sort.Strings(matches)
		return "unix:" + matches[0], matches[0]
	}
	if matches, err := probe.globFn("/run/php/*fpm.sock"); err == nil && len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool { return matches[i] > matches[j] })
		return "unix:" + matches[0], matches[0]
	}

	// 5. Fallback bila service PHP-FPM belum aktif
	if pass, sock := parsePHPPoolListenFile("/etc/php-fpm.d/www.conf", probe.readFn); pass != "" {
		return pass, sock
	}
	if _, err := probe.statFn("/etc/php-fpm.d/www.conf"); err == nil {
		return "unix:/run/php-fpm/www.sock", "/run/php-fpm/www.sock"
	}
	fallbackSock := fmt.Sprintf("/run/php/php%s-fpm.sock", ver)
	if ver == "" {
		fallbackSock = "/run/php/php-fpm.sock"
	}
	return "unix:" + fallbackSock, fallbackSock
}

func parsePHPPoolListenFile(confPath string, readFn func(string) ([]byte, error)) (string, string) {
	data, err := readFn(confPath)
	if err != nil {
		return "", ""
	}
	var listenVal string
	inTargetPool := true
	for _, rawLine := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inTargetPool = strings.EqualFold(section, "www") || strings.EqualFold(section, "global")
			continue
		}
		if !inTargetPool {
			continue
		}
		key, val, ok := strings.Cut(trimmed, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "listen") {
			continue
		}
		if idx := strings.IndexAny(val, ";#"); idx >= 0 {
			val = val[:idx]
		}
		if v := strings.TrimSpace(val); v != "" {
			listenVal = v
		}
	}
	if listenVal == "" {
		return "", ""
	}
	if strings.HasPrefix(listenVal, "/") {
		return "unix:" + listenVal, listenVal
	}
	if strings.HasPrefix(listenVal, ":") {
		return "127.0.0.1" + listenVal, ""
	}
	isPortOnly := true
	for _, r := range listenVal {
		if r < '0' || r > '9' {
			isPortOnly = false
			break
		}
	}
	if isPortOnly {
		return "127.0.0.1:" + listenVal, ""
	}
	return listenVal, ""
}

func (m *Manager) addHostsEntry(domain string) {
	m.addHostsEntries(domain)
}

func (m *Manager) addHostsEntries(domains ...string) {
	if m.cfg == nil || m.cfg.HostsFile == "" || len(domains) == 0 {
		return
	}
	content, err := os.ReadFile(m.cfg.HostsFile)
	if err != nil {
		return
	}
	existing := make(map[string]bool)
	for _, l := range strings.Split(string(content), "\n") {
		for _, f := range strings.Fields(l) {
			existing[strings.ToLower(f)] = true
		}
	}

	var missing []string
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && !existing[d] {
			existing[d] = true
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return
	}

	prefix := ""
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		prefix = "\n"
	}
	entry := fmt.Sprintf("%s127.0.0.1 %s # wdev-managed\n", prefix, strings.Join(missing, " "))
	f, err := os.OpenFile(m.cfg.HostsFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		_, _ = f.WriteString(entry)
		return
	}
	if os.Geteuid() != 0 {
		_, _ = m.runPrivilegedStdin(entry, "tee", "-a", m.cfg.HostsFile)
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
			if strings.EqualFold(f, domain) {
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
		_, _ = m.runPrivilegedStdin(string(data), "tee", m.cfg.HostsFile)
	}
}

func (m *Manager) SyncAll() error {
	sites, err := m.ListSites()
	if err != nil {
		return err
	}
	certDir := "/etc/ssl/local-certs"
	_ = m.ensureDir(certDir, 0755)
	for _, s := range sites {
		m.addHostsEntry(s.Domain)
		if s.SSLEnabled {
			certFile := filepath.Join(certDir, s.Domain+".pem")
			keyFile := filepath.Join(certDir, s.Domain+"-key.pem")
			if !m.pathExists(certFile) {
				_ = m.generateSSL(s.Domain, certFile, keyFile)
			}
		}
	}
	if _, err := m.EnsureProxyHTTPSForPHPVhosts(); err != nil {
		return err
	}
	if m.skipNginxExec {
		return nil
	}
	if nginxBin := findNginxBinary(); nginxBin != "" {
		_, _ = m.runPrivileged(nginxBin, "-t")
	}
	return m.reloadOrStartNginx()
}

// RepairEmptyServerNamesInVHosts memeriksa seluruh berkas konfigurasi Nginx di
// sites-available dan memperbaiki direktif "server_name ;" yang kosong/tanpa argumen
// agar pengujian Nginx (nginx -t) dan pembuatan virtual host baru tidak gagal.
func (m *Manager) RepairEmptyServerNamesInVHosts() (int, error) {
	sitesAvail := m.sitesAvailDir()
	entries, err := os.ReadDir(sitesAvail)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	reEmptyDirective := regexp.MustCompile(`(?m)^\s*server_name\s*;\s*$`)
	reZeroArgDirective := regexp.MustCompile(`(?m)(server_name)\s*;`)

	repairedCount := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") ||
			strings.HasSuffix(name, ".disabled") || strings.HasSuffix(name, "~") ||
			strings.HasSuffix(name, ".custom.conf") {
			continue
		}
		confPath := filepath.Join(sitesAvail, name)
		origBytes, readErr := m.readFile(confPath)
		if readErr != nil {
			continue
		}
		text := string(origBytes)

		base := strings.ToLower(strings.TrimSuffix(name, ".conf"))
		base = strings.TrimPrefix(base, "wdev_")

		modified := false
		if reEmptyDirective.MatchString(text) || reZeroArgDirective.MatchString(text) {
			domainName := base
			if ValidateDomain(base) != nil {
				if rec, ok := m.GetSite(base); ok && rec.Domain != "" {
					domainName = rec.Domain
				} else {
					domainName = "_"
				}
			}
			text = reEmptyDirective.ReplaceAllString(text, "    server_name "+domainName+";")
			text = reZeroArgDirective.ReplaceAllString(text, "${1} "+domainName+";")
			modified = true
		}

		if modified {
			if writeErr := m.writeFile(confPath, []byte(text), 0644); writeErr == nil {
				repairedCount++
			}
		}
	}
	return repairedCount, nil
}

// EnsureProxyHTTPSForPHPVhosts memastikan seluruh vhost PHP aktif di sites-available
// meneruskan status HTTPS dari header X-Forwarded-Proto reverse proxy ke PHP-FPM.
func (m *Manager) EnsureProxyHTTPSForPHPVhosts() (int, error) {
	_, _ = m.RepairEmptyServerNamesInVHosts()
	sitesAvail := m.sitesAvailDir()
	entries, err := os.ReadDir(sitesAvail)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	updatedCount := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "default" || name == "default.conf" ||
			name == "000-default" || name == "000-default.conf" ||
			strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".custom.conf") ||
			strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, ".disabled") ||
			strings.HasSuffix(name, "~") {
			continue
		}
		confPath := filepath.Join(sitesAvail, name)
		origBytes, readErr := m.readFile(confPath)
		if readErr != nil {
			continue
		}
		nextText, changedFastCGI := EnsureFastCGIProxyHTTPSInConfig(string(origBytes))
		nextText, changedProxy := EnsureProxyProtoInConfig(nextText)
		if !changedFastCGI && !changedProxy {
			continue
		}
		if writeErr := m.writeFile(confPath, []byte(nextText), 0644); writeErr != nil {
			return updatedCount, writeErr
		}
		if !m.skipNginxExec {
			if nginxBin := findNginxBinary(); nginxBin != "" {
				if out, testErr := m.runPrivileged(nginxBin, "-t"); testErr != nil {
					_ = m.writeFile(confPath, origBytes, 0644)
					return updatedCount, fmt.Errorf("konfigurasi nginx tidak valid setelah memperbarui %s: %s", name, out)
				}
			}
		}
		updatedCount++
	}
	if updatedCount > 0 && !m.skipNginxExec {
		_ = m.reloadOrStartNginx()
	}
	return updatedCount, nil
}

func findNginxBinary() string {
	for _, candidate := range []string{"/usr/sbin/nginx", "/sbin/nginx"} {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	if p, err := exec.LookPath("nginx"); err == nil {
		return p
	}
	return ""
}

func hasIPv6() bool {
	if _, err := os.Stat("/proc/net/if_inet6"); err != nil {
		return false
	}
	data, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6")
	if err == nil && strings.TrimSpace(string(data)) == "1" {
		return false
	}
	return true
}

func (m *Manager) ensureNginxSitesEnabledIncluded() {
	if m.cfg == nil || m.cfg.NginxSitesEnable != "/etc/nginx/sites-enabled" {
		return
	}
	const nginxConfPath = "/etc/nginx/nginx.conf"
	data, err := os.ReadFile(nginxConfPath)
	if err != nil {
		return
	}
	content := string(data)
	if strings.Contains(content, "/etc/nginx/sites-enabled/") {
		return
	}
	target := "include /etc/nginx/conf.d/*.conf;"
	if !strings.Contains(content, target) {
		return
	}
	patched := strings.Replace(content, target, target+"\n    include /etc/nginx/sites-enabled/*;", 1)
	if err := m.writeFile(nginxConfPath, []byte(patched), 0644); err != nil {
		return
	}
	if nginxBin := findNginxBinary(); nginxBin != "" {
		if _, testErr := m.runPrivileged(nginxBin, "-t"); testErr != nil {
			_ = m.writeFile(nginxConfPath, data, 0644)
		}
	}
}

func (m *Manager) reloadOrStartNginx() error {
	if _, err := m.runPrivileged("systemctl", "reload", "nginx"); err == nil {
		return nil
	}
	if exec.Command("systemctl", "is-active", "--quiet", "nginx").Run() != nil {
		_, err := m.runPrivileged("systemctl", "start", "nginx")
		return err
	}
	_, err := m.runPrivileged("systemctl", "reload", "nginx")
	return err
}

func (m *Manager) buildPrivilegedCmd(stdinData, name string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		cmd := exec.Command(name, args...)
		if stdinData != "" {
			cmd.Stdin = strings.NewReader(stdinData)
		}
		return cmd
	}
	pass := ""
	if m.sudoPassFn != nil {
		pass = m.sudoPassFn()
	}
	if pass != "" {
		sudoArgs := append([]string{"-S", "-p", "", name}, args...)
		cmd := exec.Command("sudo", sudoArgs...)
		cmd.Stdin = strings.NewReader(pass + "\n" + stdinData)
		return cmd
	}
	sudoArgs := append([]string{"-n", name}, args...)
	cmd := exec.Command("sudo", sudoArgs...)
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	return cmd
}

func (m *Manager) runPrivileged(name string, args ...string) (string, error) {
	return m.runPrivilegedStdin("", name, args...)
}

func (m *Manager) runPrivilegedStdin(stdinData, name string, args ...string) (string, error) {
	out, err := m.buildPrivilegedCmd(stdinData, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (m *Manager) ensureDir(dir string, mode os.FileMode) error {
	if err := os.MkdirAll(dir, mode); err == nil {
		return nil
	} else if !os.IsPermission(err) || os.Geteuid() == 0 {
		return err
	}
	if out, err := m.runPrivileged("mkdir", "-p", dir); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	_, _ = m.runPrivileged("chmod", fmt.Sprintf("%04o", mode), dir)
	return nil
}

func (m *Manager) writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err == nil {
		return os.Chmod(path, mode)
	} else if !os.IsPermission(err) || os.Geteuid() == 0 {
		return err
	}
	tmpFile, err := os.CreateTemp("", "wdev-site-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	_ = os.Chmod(tmpPath, mode)
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if out, err := m.runPrivileged("install", "-m", fmt.Sprintf("%04o", mode), tmpPath, path); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	return nil
}

func (m *Manager) ensureSymlink(target, link string) error {
	_ = os.Remove(link)
	if err := os.Symlink(target, link); err == nil {
		return nil
	} else if os.Geteuid() == 0 {
		return err
	}
	if out, err := m.runPrivileged("ln", "-sfn", target, link); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	return nil
}

func (m *Manager) removeFile(path string) error {
	if _, lerr := os.Lstat(path); os.IsNotExist(lerr) {
		return nil
	}
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		return nil
	} else if !os.IsPermission(err) || os.Geteuid() == 0 {
		return err
	}
	if out, err := m.runPrivileged("rm", "-f", path); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	return nil
}

func (m *Manager) pathExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	} else if os.IsNotExist(err) {
		return false
	}
	if os.Geteuid() == 0 {
		return false
	}
	_, err := m.runPrivileged("test", "-e", path)
	return err == nil
}

func (m *Manager) readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !os.IsPermission(err) || os.Geteuid() == 0 {
		return nil, err
	}
	out, runErr := m.runPrivileged("cat", path)
	if runErr != nil {
		return nil, fmt.Errorf("%w (%s)", runErr, out)
	}
	return []byte(out), nil
}
