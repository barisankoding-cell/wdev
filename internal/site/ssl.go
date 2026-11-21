package site

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"wdev/internal/store"
)

var (
	serverNameRe     = regexp.MustCompile(`(?m)server_name\s+([^;]+);`)
	sslCertificateRe = regexp.MustCompile(`(?m)^\s*ssl_certificate\s+([^;\s]+)\s*;`)
)

// ValidateLetsEncryptDomain memastikan domain valid dan merupakan FQDN publik
// (bukan TLD lokal/internal seperti .test atau .local).
func ValidateLetsEncryptDomain(domain string) error {
	clean := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if err := ValidateDomain(clean); err != nil {
		return err
	}
	for _, suffix := range []string{".test", ".local", ".localhost", ".invalid", ".example", ".arpa", ".internal"} {
		if strings.HasSuffix(clean, suffix) {
			return fmt.Errorf("domain lokal (%s) tidak dapat diterbitkan sertifikat Let's Encrypt; gunakan domain publik yang mengarah ke IP VPS", clean)
		}
	}
	return nil
}

func validateLetsEncryptEmail(email string) (string, error) {
	clean := strings.TrimSpace(email)
	if clean == "" {
		return "", nil
	}
	if strings.HasPrefix(clean, "-") || strings.ContainsAny(clean, " \t\r\n\x00\"'<>") {
		return "", fmt.Errorf("alamat email tidak valid")
	}
	at := strings.Index(clean, "@")
	if at <= 0 || at >= len(clean)-1 || !strings.Contains(clean[at+1:], ".") {
		return "", fmt.Errorf("format alamat email tidak valid: %q", clean)
	}
	return clean, nil
}

// DetectSSLProviderFromConfig memeriksa isi berkas konfigurasi Nginx untuk mengetahui
// apakah SSL aktif serta apakah sertifikatnya berasal dari Let's Encrypt atau lokal.
func DetectSSLProviderFromConfig(confText string) (enabled bool, provider string) {
	enabled, provider, _ = DetectSSLInfoFromConfig(confText)
	return enabled, provider
}

// DetectSSLInfoFromConfig mengekstrak status SSL, provider, dan path file sertifikat dari konfigurasi Nginx.
func DetectSSLInfoFromConfig(confText string) (enabled bool, provider, certPath string) {
	m := sslCertificateRe.FindStringSubmatch(confText)
	if len(m) < 2 {
		return false, "", ""
	}
	certPath = strings.Trim(strings.TrimSpace(m[1]), `"'`)
	if strings.Contains(confText, "/etc/letsencrypt/") || strings.Contains(certPath, "/etc/letsencrypt/") {
		return true, "letsencrypt", certPath
	}
	return true, "local", certPath
}

// ParseCertificateExpiry membaca waktu kedaluwarsa (NotAfter) sertifikat X.509 pertama dari konten PEM.
func ParseCertificateExpiry(pemBytes []byte) (time.Time, error) {
	rest := pemBytes
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		return cert.NotAfter.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("sertifikat X.509 tidak ditemukan")
}

func (m *Manager) populateSiteSSLExpiry(s *store.SiteRecord, certPath string) {
	if s == nil || !s.SSLEnabled {
		return
	}
	candidates := make([]string, 0, 3)
	if certPath != "" {
		candidates = append(candidates, certPath)
	}
	if s.SSLProvider == "letsencrypt" {
		candidates = append(candidates, filepath.Join("/etc/letsencrypt/live", s.Domain, "fullchain.pem"))
	} else if s.SSLProvider == "local" {
		candidates = append(candidates, filepath.Join("/etc/ssl/local-certs", s.Domain+".pem"))
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		data, err := m.readFile(p)
		if err != nil {
			continue
		}
		exp, err := ParseCertificateExpiry(data)
		if err != nil {
			continue
		}
		expUTC := exp.UTC()
		daysLeft := int(math.Floor(time.Until(expUTC).Hours() / 24))
		s.SSLExpiresAt = &expUTC
		s.SSLDaysLeft = &daysLeft
		return
	}
}

func extractVHostDomains(confText, primaryDomain string) []string {
	primaryDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(primaryDomain), "."))
	seen := map[string]bool{primaryDomain: true}
	domains := []string{primaryDomain}

	for _, match := range serverNameRe.FindAllStringSubmatch(confText, -1) {
		for _, tok := range strings.Fields(match[1]) {
			cand := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(tok), "."))
			if cand == "" || seen[cand] || strings.Contains(cand, "*") {
				continue
			}
			if ValidateLetsEncryptDomain(cand) == nil {
				seen[cand] = true
				domains = append(domains, cand)
			}
		}
	}
	return domains
}

func buildCertbotArgs(domains []string, email string) []string {
	args := []string{
		"--nginx",
		"--non-interactive",
		"--agree-tos",
		"--redirect",
		"--expand",
		"--keep-until-expiring",
		"--deploy-hook", "systemctl reload nginx",
	}
	if email != "" {
		args = append(args, "--email", email, "--no-eff-email")
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	for _, d := range domains {
		args = append(args, "-d", d)
	}
	return args
}

// EnableLetsEncrypt menerbitkan dan memasang sertifikat SSL Let's Encrypt ke virtual host Nginx.
func (m *Manager) EnableLetsEncrypt(ctx context.Context, cb func(string), domain, email string) error {
	if cb == nil {
		cb = func(string) {}
	}
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if err := ValidateLetsEncryptDomain(domain); err != nil {
		return err
	}
	cleanEmail, err := validateLetsEncryptEmail(email)
	if err != nil {
		return err
	}

	confPath, confExists := m.findVHostConfigPath(domain)
	if !confExists {
		return fmt.Errorf("virtual host untuk %s tidak ditemukan di %s", domain, m.sitesAvailDir())
	}

	confBytes, err := m.readFile(confPath)
	if err != nil {
		return fmt.Errorf("gagal membaca konfigurasi vhost %s: %w", confPath, err)
	}
	confText := string(confBytes)
	domains := extractVHostDomains(confText, domain)

	// Jika vhost memakai sertifikat lokal/self-signed (/etc/ssl/local-certs/),
	// ubah dulu ke HTTP bersih agar certbot --nginx memasang blok 443 Let's Encrypt dengan rapi.
	if strings.Contains(confText, "/etc/ssl/local-certs/") && filepath.Base(confPath) == domain {
		if rec, ok := m.lookupStoredSite(domain); ok && rec.Framework != "" {
			cb("==> Mengonversi konfigurasi SSL lokal menjadi HTTP bersih sebelum penerbitan Let's Encrypt...")
			var aliases []string
			if len(domains) > 1 {
				aliases = domains[1:]
			}
			if err := m.CreateSite(CreateOptions{
				Domain:        domain,
				ServerAliases: aliases,
				RootPath:      rec.RootPath,
				PHPVersion:    rec.PHPVersion,
				Framework:     rec.Framework,
				ProxyPort:     rec.ProxyPort,
				SSLEnabled:    false,
			}); err != nil {
				return fmt.Errorf("gagal menyiapkan vhost HTTP sebelum Let's Encrypt: %w", err)
			}
		}
	}

	if m.certbotExecFn == nil {
		nginxBin := findNginxBinary()
		if nginxBin == "" {
			return fmt.Errorf("nginx belum terpasang; pasang Nginx terlebih dahulu")
		}
		enablePath := filepath.Join(m.sitesEnableDir(), filepath.Base(confPath))
		if err := m.ensureSymlink(confPath, enablePath); err != nil {
			return fmt.Errorf("gagal mengaktifkan symlink vhost: %w", err)
		}
		if !m.skipNginxExec {
			m.ensureNginxSitesEnabledIncluded()
			if out, err := m.runPrivileged(nginxBin, "-t"); err != nil {
				return fmt.Errorf("konfigurasi nginx tidak valid sebelum certbot: %s", out)
			}
			if err := m.reloadOrStartNginx(); err != nil {
				return fmt.Errorf("layanan nginx tidak aktif: %w", err)
			}
		}

		cb("==> [1/3] Memeriksa instalasi certbot & plugin Nginx...")
		if err := m.ensureCertbotInstalled(ctx, cb); err != nil {
			return err
		}
	}

	certbotBin := "certbot"
	if m.certbotExecFn == nil {
		certbotBin = findCertbotBinary()
		if certbotBin == "" {
			return fmt.Errorf("biner certbot tidak ditemukan setelah instalasi")
		}
	}

	cb(fmt.Sprintf("==> [2/3] Menerbitkan sertifikat SSL Let's Encrypt untuk: %s", strings.Join(domains, ", ")))
	runCertbot := func(doms []string) error {
		args := buildCertbotArgs(doms, cleanEmail)
		if m.certbotExecFn != nil {
			return m.certbotExecFn(ctx, cb, certbotBin, args)
		}
		return m.runPrivilegedStreamCtx(ctx, cb, certbotBin, args...)
	}

	if err := runCertbot(domains); err != nil {
		if len(domains) > 1 {
			cb(fmt.Sprintf("[WARN] Penerbitan dengan alias (%s) gagal (%v); mencoba ulang khusus domain utama %s...", strings.Join(domains[1:], ", "), err, domain))
			if retryErr := runCertbot([]string{domain}); retryErr != nil {
				return fmt.Errorf("certbot gagal menerbitkan sertifikat untuk %s: %w (pastikan DNS A record sudah mengarah ke IP publik VPS dan port 80/443 terbuka)", domain, retryErr)
			}
		} else {
			return fmt.Errorf("certbot gagal menerbitkan sertifikat untuk %s: %w (pastikan DNS A record sudah mengarah ke IP publik VPS dan port 80/443 terbuka)", domain, err)
		}
	}

	cb("==> [3/3] Memverifikasi konfigurasi Nginx & pembaruan otomatis...")
	if m.certbotExecFn == nil && !m.skipNginxExec {
		if nginxBin := findNginxBinary(); nginxBin != "" {
			if out, err := m.runPrivileged(nginxBin, "-t"); err != nil {
				return fmt.Errorf("konfigurasi nginx setelah certbot tidak valid: %s", out)
			}
		}
		if err := m.reloadOrStartNginx(); err != nil {
			return fmt.Errorf("gagal me-reload nginx setelah certbot: %w", err)
		}
		m.enableCertbotRenewTimer(ctx, cb)
	}

	if m.store != nil {
		if err := m.store.SetSiteSSL(domain, true, "letsencrypt"); err != nil {
			return fmt.Errorf("sertifikat terpasang namun gagal memperbarui status di database: %w", err)
		}
	}

	cb(fmt.Sprintf("✓ SSL Let's Encrypt aktif! Situs dapat diakses melalui https://%s", domain))
	return nil
}

func (m *Manager) lookupStoredSite(domain string) (store.SiteRecord, bool) {
	if m.store == nil {
		return store.SiteRecord{}, false
	}
	sites, err := m.store.ListSites()
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

func (m *Manager) findVHostConfigPath(domain string) (string, bool) {
	avail := m.sitesAvailDir()
	for _, name := range []string{domain, domain + ".conf"} {
		p := filepath.Join(avail, name)
		if m.pathExists(p) {
			return p, true
		}
	}
	return "", false
}

func findCertbotBinary() string {
	for _, candidate := range []string{"/usr/bin/certbot", "/usr/local/bin/certbot", "/snap/bin/certbot"} {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	if p, err := exec.LookPath("certbot"); err == nil {
		return p
	}
	return ""
}

func (m *Manager) hasCertbotNginxPlugin(certbotBin string) bool {
	if certbotBin == "" {
		return false
	}
	out, err := m.runPrivileged(certbotBin, "plugins", "--no-eff-email")
	if err != nil {
		out, _ = m.runPrivileged(certbotBin, "plugins")
	}
	return strings.Contains(strings.ToLower(out), "nginx")
}

func (m *Manager) ensureCertbotInstalled(ctx context.Context, cb func(string)) error {
	bin := findCertbotBinary()
	if bin != "" && m.hasCertbotNginxPlugin(bin) {
		cb("  ✓ Certbot dan plugin Nginx sudah tersedia.")
		return nil
	}

	cb("  Memasang certbot + plugin Nginx secara otomatis...")
	family := detectDistroPkgMgr()
	switch family {
	case "apt-get":
		_ = m.runPrivilegedStreamCtx(ctx, cb, "apt-get", "update", "-y")
		if err := m.runPrivilegedStreamCtx(ctx, cb, "apt-get", "install", "-y", "--no-install-recommends", "certbot", "python3-certbot-nginx"); err != nil {
			return fmt.Errorf("gagal memasang certbot & python3-certbot-nginx via apt-get: %w", err)
		}
	case "dnf", "yum":
		if err := m.runPrivilegedStreamCtx(ctx, cb, family, "install", "-y", "certbot", "python3-certbot-nginx"); err != nil {
			return fmt.Errorf("gagal memasang certbot & python3-certbot-nginx via %s: %w", family, err)
		}
	case "pacman":
		if err := m.runPrivilegedStreamCtx(ctx, cb, "pacman", "-S", "--noconfirm", "--needed", "certbot", "certbot-nginx"); err != nil {
			return fmt.Errorf("gagal memasang certbot & certbot-nginx via pacman: %w", err)
		}
	case "zypper":
		if err := m.runPrivilegedStreamCtx(ctx, cb, "zypper", "install", "-y", "certbot", "python3-certbot-nginx"); err != nil {
			return fmt.Errorf("gagal memasang certbot via zypper: %w", err)
		}
	default:
		return fmt.Errorf("certbot belum terpasang dan package manager distro tidak dikenali; pasang certbot dan python3-certbot-nginx secara manual")
	}

	bin = findCertbotBinary()
	if bin == "" {
		return fmt.Errorf("biner certbot masih tidak ditemukan setelah instalasi")
	}
	if !m.hasCertbotNginxPlugin(bin) {
		return fmt.Errorf("plugin certbot-nginx tidak terdeteksi setelah instalasi")
	}
	cb("  ✓ Certbot & plugin Nginx berhasil dipasang.")
	return nil
}

func detectDistroPkgMgr() string {
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		low := strings.ToLower(string(data))
		if strings.Contains(low, "fedora") || strings.Contains(low, "rhel") || strings.Contains(low, "centos") || strings.Contains(low, "rocky") || strings.Contains(low, "alma") {
			if _, err := exec.LookPath("dnf"); err == nil {
				return "dnf"
			}
			return "yum"
		}
		if strings.Contains(low, "arch") || strings.Contains(low, "manjaro") {
			return "pacman"
		}
		if strings.Contains(low, "suse") {
			return "zypper"
		}
		if strings.Contains(low, "debian") || strings.Contains(low, "ubuntu") || strings.Contains(low, "mint") || strings.Contains(low, "pop") {
			return "apt-get"
		}
	}
	for _, pm := range []string{"apt-get", "dnf", "yum", "pacman", "zypper"} {
		if _, err := exec.LookPath(pm); err == nil {
			return pm
		}
	}
	return ""
}

func (m *Manager) enableCertbotRenewTimer(ctx context.Context, cb func(string)) {
	for _, timer := range []string{"certbot.timer", "certbot-renew.timer"} {
		if exec.CommandContext(ctx, "systemctl", "list-unit-files", timer).Run() == nil {
			if _, err := m.runPrivileged("systemctl", "enable", "--now", timer); err == nil {
				cb("  ✓ Auto-renew systemd timer aktif (" + timer + ")")
				return
			}
		}
	}
}

func (m *Manager) runPrivilegedStreamCtx(ctx context.Context, cb func(string), name string, args ...string) error {
	var cmd *exec.Cmd
	var stdinPipe io.WriteCloser

	if os.Geteuid() == 0 {
		cmd = exec.CommandContext(ctx, name, args...)
	} else {
		pass := ""
		if m.sudoPassFn != nil {
			pass = m.sudoPassFn()
		}
		if pass != "" {
			sudoArgs := append([]string{"-S", "-p", "", name}, args...)
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
			var err error
			stdinPipe, err = cmd.StdinPipe()
			if err != nil {
				return err
			}
		} else {
			sudoArgs := append([]string{"-n", name}, args...)
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
		}
	}
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	if stdinPipe != nil {
		pass := m.sudoPassFn()
		go func() {
			defer stdinPipe.Close()
			_, _ = io.WriteString(stdinPipe, pass+"\n")
		}()
	}

	var wg sync.WaitGroup
	wg.Add(2)
	scan := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 16*1024), 512*1024)
		for scanner.Scan() {
			cb(scanner.Text())
		}
	}
	go scan(stdout)
	go scan(stderr)
	wg.Wait()
	return cmd.Wait()
}
