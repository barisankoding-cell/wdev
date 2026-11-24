package cloudflare

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"wdev/internal/store"

	"gopkg.in/yaml.v3"
)

// ParsedTunnelToken memuat isi metadata hasil decode token Cloudflare Zero Trust Tunnel.
type ParsedTunnelToken struct {
	RawToken   string
	AccountTag string
	TunnelID   string
	Secret     string
}

// TokenProfileOptions mendeskripsikan pendaftaran profil berbasis Token Cloudflare Zero Trust.
type TokenProfileOptions struct {
	Name       string `json:"name"`
	Token      string `json:"token"`
	TunnelName string `json:"tunnel_name,omitempty"`
	AutoStart  *bool  `json:"auto_start,omitempty"`
}

// CertProfileOptions mendeskripsikan pendaftaran profil berbasis cert.pem (Locally-Managed Tunnel).
type CertProfileOptions struct {
	Name            string `json:"name"`
	CertPEM         string `json:"cert_pem,omitempty"`
	LocalCertPath   string `json:"local_cert_path,omitempty"`
	CredentialsJSON string `json:"credentials_json,omitempty"`
	TunnelName      string `json:"tunnel_name,omitempty"`
	TunnelID        string `json:"tunnel_id,omitempty"`
	AutoStart       *bool  `json:"auto_start,omitempty"`
}

// LoginSession merepresentasikan sesi login browser Cloudflare OAuth yang terisolasi per profil.
type LoginSession struct {
	ID         string       `json:"session_id"`
	Profile    string       `json:"profile"`
	TunnelName string       `json:"tunnel_name"`
	Status     string       `json:"status"` // "starting" | "waiting_browser" | "completed" | "failed" | "cancelled"
	AuthURL    string       `json:"auth_url,omitempty"`
	Message    string       `json:"message,omitempty"`
	Error      string       `json:"error,omitempty"`
	Result     *ProfileInfo `json:"result,omitempty"`
	ExpiresAt  string       `json:"expires_at"`

	cancel context.CancelFunc
	tmpDir string
}

// ParseTunnelToken memvalidasi dan mengekstrak token base64 Cloudflare Zero Trust.
// Mendukung input token mentah ataupun salinan perintah `cloudflared service install <token>`.
func ParseTunnelToken(input string) (*ParsedTunnelToken, error) {
	raw := strings.TrimSpace(input)
	raw = strings.Trim(raw, "\"'`")
	if raw == "" {
		return nil, errors.New("tunnel token tidak boleh kosong")
	}

	candidate := raw
	if strings.ContainsAny(raw, " \t\r\n") {
		fields := strings.Fields(raw)
		found := ""
		for i, f := range fields {
			if f == "--token" && i+1 < len(fields) {
				found = fields[i+1]
				break
			}
		}
		if found == "" {
			for i := len(fields) - 1; i >= 0; i-- {
				if strings.HasPrefix(fields[i], "eyJ") || len(fields[i]) > 32 {
					found = fields[i]
					break
				}
			}
		}
		if found != "" {
			candidate = strings.Trim(found, "\"'`")
		}
	}

	var decoded []byte
	var err error
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err = enc.DecodeString(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, errors.New("format tunnel token Cloudflare tidak valid (bukan base64 JSON)")
	}

	var payload struct {
		AccountTag string `json:"a"`
		TunnelID   string `json:"t"`
		Secret     string `json:"s"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, errors.New("isi tunnel token Cloudflare tidak valid (gagal parse JSON)")
	}
	if strings.TrimSpace(payload.TunnelID) == "" || strings.TrimSpace(payload.Secret) == "" {
		return nil, errors.New("tunnel token Cloudflare tidak lengkap (field tunnel ID / secret kosong)")
	}

	return &ParsedTunnelToken{
		RawToken:   candidate,
		AccountTag: strings.TrimSpace(payload.AccountTag),
		TunnelID:   strings.TrimSpace(payload.TunnelID),
		Secret:     strings.TrimSpace(payload.Secret),
	}, nil
}

// RegisterTokenProfile mendaftarkan profil multi-akun menggunakan Tunnel Token dari Zero Trust Dashboard.
func (m *Manager) RegisterTokenProfile(opts TokenProfileOptions) (*ProfileInfo, error) {
	name := normalizeCFProfile(opts.Name)
	if name == "" {
		name = "default"
	}
	if err := validateCFProfile(name); err != nil {
		return nil, err
	}

	parsed, err := ParseTunnelToken(opts.Token)
	if err != nil {
		return nil, err
	}
	if err := m.ensureDirs(); err != nil {
		return nil, err
	}

	tokenPath := m.tokenFilePath(name)
	if err := m.writeConfigFile(tokenPath, []byte(parsed.RawToken+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("gagal menyimpan file token: %w", err)
	}

	tunnelName := strings.TrimSpace(opts.TunnelName)
	if tunnelName == "" {
		tunnelName = "cf-" + name
	}

	info := ProfileInfo{
		Name:       name,
		AuthMode:   "token",
		TunnelName: tunnelName,
		TunnelID:   parsed.TunnelID,
		AccountTag: parsed.AccountTag,
		HasToken:   true,
	}

	if err := m.writeProfileRunnerFiles(info); err != nil {
		return nil, err
	}

	if m.store != nil {
		if err := m.store.SaveCFProfile(store.CFProfile{
			Name:       name,
			AuthMode:   "token",
			TunnelName: tunnelName,
			TunnelID:   parsed.TunnelID,
			AccountTag: parsed.AccountTag,
		}); err != nil {
			return nil, fmt.Errorf("gagal menyimpan profil ke database: %w", err)
		}
	}

	autoStart := true
	if opts.AutoStart != nil {
		autoStart = *opts.AutoStart
	}
	if autoStart {
		if err := m.EnsureSystemdTemplate(); err != nil {
			return nil, err
		}
		unit := profileServiceUnit(name)
		if out, err := m.runPrivileged("systemctl", "enable", "--now", unit); err != nil {
			return nil, fmt.Errorf("profil tersimpan, namun gagal mengaktifkan service %s: %w (%s)", unit, err, out)
		}
		// Restart memastikan token baru dimuat jika profil diperbarui
		_, _ = m.runPrivileged("systemctl", "restart", unit)
	}

	st := m.GetStatus()
	for _, d := range st.ProfileDetails {
		if d.Name == name {
			cp := d
			return &cp, nil
		}
	}
	return &info, nil
}

// RegisterCertProfile mendaftarkan profil multi-akun berbasis cert.pem (Locally-Managed Tunnel).
// Jika credentials_json tidak disertakan dan biner cloudflared tersedia, wdev otomatis membuat tunnel baru.
func (m *Manager) RegisterCertProfile(opts CertProfileOptions) (*ProfileInfo, error) {
	name := normalizeCFProfile(opts.Name)
	if name == "" {
		name = "default"
	}
	if err := validateCFProfile(name); err != nil {
		return nil, err
	}

	certText := strings.TrimSpace(opts.CertPEM)
	if certText == "" && strings.TrimSpace(opts.LocalCertPath) != "" {
		allowed := m.detectSystemCertFiles()
		cleanLocal := filepath.Clean(strings.TrimSpace(opts.LocalCertPath))
		match := false
		for _, p := range allowed {
			if p == cleanLocal {
				match = true
				break
			}
		}
		if !match {
			return nil, fmt.Errorf("path cert lokal tidak termasuk lokasi cert.pem yang terdeteksi: %s", cleanLocal)
		}
		raw, err := os.ReadFile(cleanLocal)
		if err != nil {
			return nil, fmt.Errorf("gagal membaca %s: %w", cleanLocal, err)
		}
		certText = strings.TrimSpace(string(raw))
	}

	hasCert := certText != ""
	if hasCert {
		if !strings.Contains(certText, "-----BEGIN") ||
			(!strings.Contains(certText, "ARGO TUNNEL TOKEN") && !strings.Contains(certText, "PRIVATE KEY")) {
			return nil, errors.New("isi cert.pem tidak valid (harus berisi blok PEM CERTIFICATE / ARGO TUNNEL TOKEN Cloudflare)")
		}
	}

	credText := strings.TrimSpace(opts.CredentialsJSON)
	if !hasCert && credText == "" {
		return nil, errors.New("minimal cert.pem atau credentials JSON tunnel wajib disediakan")
	}

	if err := m.ensureDirs(); err != nil {
		return nil, err
	}

	certPath := m.certFilePath(name)
	credPath := m.credFilePath(name)
	cfgPath := m.routeConfigPath(name)

	if hasCert {
		if err := m.writeConfigFile(certPath, []byte(certText+"\n"), 0600); err != nil {
			return nil, fmt.Errorf("gagal menyimpan %s: %w", certPath, err)
		}
	}

	tunnelName := strings.TrimSpace(opts.TunnelName)
	if tunnelName == "" {
		hostName, _ := os.Hostname()
		if hostName == "" {
			hostName = "wdev"
		}
		tunnelName = strings.ToLower(fmt.Sprintf("%s-%s", strings.Split(hostName, ".")[0], name))
		if !cfProfileRe.MatchString(tunnelName) {
			tunnelName = "wdev-" + name
		}
	}

	tunnelID := strings.TrimSpace(opts.TunnelID)
	accountTag := ""

	if credText != "" {
		tid, tag, err := parseCredentialsJSONBytes([]byte(credText))
		if err != nil {
			return nil, err
		}
		if tunnelID == "" {
			tunnelID = tid
		}
		accountTag = tag
		if err := m.writeConfigFile(credPath, []byte(credText+"\n"), 0600); err != nil {
			return nil, fmt.Errorf("gagal menyimpan credentials JSON: %w", err)
		}
	} else {
		// Buat tunnel otomatis lewat cloudflared tunnel create --origincert ...
		bin, err := m.lookPathFn("cloudflared")
		if err != nil || bin == "" {
			return nil, errors.New("biner cloudflared belum terpasang untuk membuat tunnel otomatis")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		out, cErr := m.runCmdFn(ctx, nil, bin, "tunnel", "--origincert", certPath, "create", "--cred-file", credPath, tunnelName)
		cancel()
		if cErr != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = cErr.Error()
			}
			return nil, fmt.Errorf("gagal membuat tunnel %q di Cloudflare (%s). Jika nama tunnel sudah ada, gunakan nama lain atau sertakan credentials JSON", tunnelName, msg)
		}
		if fileExists(credPath) {
			tid, tag, err := parseCredentialsJSONFile(credPath)
			if err == nil {
				if tunnelID == "" {
					tunnelID = tid
				}
				accountTag = tag
			}
		}
	}

	// Buat atau perbarui routes/<profile>.yml agar selalu memiliki tunnel + credentials-file
	var cfg CloudflaredConfig
	if existing, err := os.ReadFile(cfgPath); err == nil {
		_ = yaml.Unmarshal(existing, &cfg)
	}
	if tunnelID != "" {
		cfg.TunnelID = tunnelID
	} else {
		cfg.TunnelID = tunnelName
	}
	if fileExists(credPath) {
		cfg.Credentials = credPath
	}
	if cfg.Metrics == "" {
		cfg.Metrics = "127.0.0.1:0"
	}
	if len(cfg.Ingress) == 0 {
		cfg.Ingress = []IngressRule{{Service: "http_status:404"}}
	}
	if yamlBytes, err := yaml.Marshal(&cfg); err == nil {
		if err := m.writeConfigFile(cfgPath, yamlBytes, 0644); err != nil {
			return nil, fmt.Errorf("gagal menyimpan config tunnel %s: %w", cfgPath, err)
		}
	}

	info := ProfileInfo{
		Name:           name,
		AuthMode:       "cert",
		TunnelName:     tunnelName,
		TunnelID:       tunnelID,
		AccountTag:     accountTag,
		HasCert:        hasCert || fileExists(certPath),
		HasCredentials: fileExists(credPath),
		ConfigPath:     cfgPath,
	}

	if err := m.writeProfileRunnerFiles(info); err != nil {
		return nil, err
	}

	if m.store != nil {
		if err := m.store.SaveCFProfile(store.CFProfile{
			Name:            name,
			AuthMode:        "cert",
			TunnelName:      tunnelName,
			TunnelID:        tunnelID,
			AccountTag:      accountTag,
			CredentialsPath: credPath,
			ConfigPath:      cfgPath,
		}); err != nil {
			return nil, fmt.Errorf("gagal menyimpan profil ke database: %w", err)
		}
	}

	autoStart := true
	if opts.AutoStart != nil {
		autoStart = *opts.AutoStart
	}
	if autoStart && info.HasCredentials {
		if err := m.EnsureSystemdTemplate(); err != nil {
			return nil, err
		}
		unit := profileServiceUnit(name)
		if out, err := m.runPrivileged("systemctl", "enable", "--now", unit); err != nil {
			return nil, fmt.Errorf("profil tersimpan, namun gagal menjalankan service %s: %w (%s)", unit, err, out)
		}
		_, _ = m.runPrivileged("systemctl", "restart", unit)
	}

	st := m.GetStatus()
	for _, d := range st.ProfileDetails {
		if d.Name == name {
			cp := d
			return &cp, nil
		}
	}
	return &info, nil
}

// StartBrowserLogin memulai proses `cloudflared tunnel login` terisolasi per profil
// dan mengembalikan URL otorisasi Cloudflare untuk dibuka di browser pengguna.
func (m *Manager) StartBrowserLogin(profile, tunnelName string) (*LoginSession, error) {
	name := normalizeCFProfile(profile)
	if name == "" {
		name = "default"
	}
	if err := validateCFProfile(name); err != nil {
		return nil, err
	}
	bin, err := m.lookPathFn("cloudflared")
	if err != nil || bin == "" {
		return nil, errors.New("biner cloudflared belum terpasang; pasang terlebih dahulu dari menu Setup")
	}

	// Batalkan sesi login sebelumnya untuk profil yang sama jika masih berjalan
	m.loginMu.Lock()
	for id, sess := range m.loginSessions {
		if sess.Profile == name && (sess.Status == "starting" || sess.Status == "waiting_browser") {
			if sess.cancel != nil {
				sess.cancel()
			}
			if sess.tmpDir != "" {
				_ = os.RemoveAll(sess.tmpDir)
			}
			sess.Status = "cancelled"
			delete(m.loginSessions, id)
		}
	}
	m.loginMu.Unlock()

	tmpHome, err := os.MkdirTemp("", "wdev-cf-login-*")
	if err != nil {
		return nil, fmt.Errorf("gagal membuat direktori login sementara: %w", err)
	}
	_ = os.Chmod(tmpHome, 0700)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	pipe, waitFn, err := m.startLoginCmdFn(ctx, tmpHome, bin)
	if err != nil {
		cancel()
		_ = os.RemoveAll(tmpHome)
		return nil, fmt.Errorf("gagal menjalankan cloudflared tunnel login: %w", err)
	}

	sessID := newSessionID()
	sess := &LoginSession{
		ID:         sessID,
		Profile:    name,
		TunnelName: strings.TrimSpace(tunnelName),
		Status:     "starting",
		Message:    "Menyiapkan URL otorisasi Cloudflare...",
		ExpiresAt:  time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		cancel:     cancel,
		tmpDir:     tmpHome,
	}

	m.loginMu.Lock()
	m.loginSessions[sessID] = sess
	m.loginMu.Unlock()

	urlReadyCh := make(chan struct{})
	var closeOnce sync.Once
	markReady := func() {
		closeOnce.Do(func() { close(urlReadyCh) })
	}

	go func() {
		defer cancel()
		defer os.RemoveAll(tmpHome)

		var logBuf strings.Builder
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			logBuf.WriteString(line)
			logBuf.WriteByte('\n')
			if match := cfLoginURLRe.FindString(line); match != "" {
				m.loginMu.Lock()
				if cur, ok := m.loginSessions[sessID]; ok {
					cur.AuthURL = match
					if cur.Status == "starting" {
						cur.Status = "waiting_browser"
						cur.Message = "Buka tautan otorisasi di browser lalu pilih domain zona Cloudflare Anda."
					}
				}
				m.loginMu.Unlock()
				markReady()
			}
		}
		_ = pipe.Close()

		waitErr := waitFn()
		certPath := filepath.Join(tmpHome, ".cloudflared", "cert.pem")
		certBytes, readErr := os.ReadFile(certPath)

		m.loginMu.Lock()
		cur, ok := m.loginSessions[sessID]
		if !ok {
			m.loginMu.Unlock()
			markReady()
			return
		}
		if cur.Status == "cancelled" {
			m.loginMu.Unlock()
			markReady()
			return
		}
		m.loginMu.Unlock()

		if waitErr != nil || readErr != nil || len(strings.TrimSpace(string(certBytes))) == 0 {
			errMsg := strings.TrimSpace(logBuf.String())
			if waitErr != nil {
				if errMsg == "" {
					errMsg = waitErr.Error()
				}
			} else if readErr != nil {
				errMsg = fmt.Sprintf("cert.pem tidak ditemukan setelah login: %v", readErr)
			}
			m.loginMu.Lock()
			cur.Status = "failed"
			cur.Error = errMsg
			cur.Message = "Otorisasi browser Cloudflare gagal atau kedaluwarsa."
			m.loginMu.Unlock()
			markReady()
			return
		}

		res, regErr := m.RegisterCertProfile(CertProfileOptions{
			Name:       name,
			CertPEM:    string(certBytes),
			TunnelName: sess.TunnelName,
		})

		m.loginMu.Lock()
		if regErr != nil {
			cur.Status = "failed"
			cur.Error = regErr.Error()
			cur.Message = "Login berhasil, namun gagal membuat tunnel profil."
		} else {
			cur.Status = "completed"
			cur.Result = res
			cur.Message = fmt.Sprintf("Profil '%s' berhasil terdaftar dan aktif.", name)
		}
		m.loginMu.Unlock()
		markReady()
	}()

	select {
	case <-urlReadyCh:
	case <-time.After(6 * time.Second):
	}

	return m.GetLoginSession(sessID)
}

// GetLoginSession mengembalikan snapshot status sesi login browser Cloudflare.
func (m *Manager) GetLoginSession(sessionID string) (*LoginSession, error) {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()

	sess, ok := m.loginSessions[strings.TrimSpace(sessionID)]
	if !ok {
		return nil, errors.New("sesi login Cloudflare tidak ditemukan")
	}
	cp := *sess
	cp.cancel = nil
	cp.tmpDir = ""
	return &cp, nil
}

// CancelLoginSession membatalkan sesi login browser Cloudflare yang sedang menunggu.
func (m *Manager) CancelLoginSession(sessionID string) error {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()

	sess, ok := m.loginSessions[strings.TrimSpace(sessionID)]
	if !ok {
		return nil
	}
	sess.Status = "cancelled"
	sess.Message = "Sesi login dibatalkan."
	if sess.cancel != nil {
		sess.cancel()
	}
	if sess.tmpDir != "" {
		_ = os.RemoveAll(sess.tmpDir)
	}
	return nil
}

// ControlProfile menjalankan aksi systemd pada layanan `cloudflared@<profile>.service`.
func (m *Manager) ControlProfile(profile, action string) error {
	name := normalizeCFProfile(profile)
	if err := validateCFProfile(name); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start", "stop", "restart", "enable", "disable":
	default:
		return fmt.Errorf("aksi service tidak valid: %s", action)
	}

	profiles := m.collectProfileMap()
	info, exists := profiles[name]
	if !exists {
		return fmt.Errorf("profil Cloudflare %q tidak ditemukan", name)
	}

	act := strings.ToLower(strings.TrimSpace(action))
	if act == "start" || act == "restart" || act == "enable" {
		if err := m.writeProfileRunnerFiles(info); err != nil {
			return err
		}
		if err := m.EnsureSystemdTemplate(); err != nil {
			return err
		}
	}

	unit := profileServiceUnit(name)
	if out, err := m.runPrivileged("systemctl", act, unit); err != nil {
		return fmt.Errorf("gagal %s %s: %w (%s)", act, unit, err, out)
	}
	return nil
}

// GetProfileLogs mengembalikan log journald terakhir untuk unit `cloudflared@<profile>.service`.
func (m *Manager) GetProfileLogs(profile string, lines int) (string, error) {
	name := normalizeCFProfile(profile)
	if err := validateCFProfile(name); err != nil {
		return "", err
	}
	if lines <= 0 || lines > 200 {
		lines = 50
	}
	unit := profileServiceUnit(name)
	out, err := m.runPrivileged("journalctl", "-u", unit, "-n", fmt.Sprintf("%d", lines), "--no-pager")
	if err != nil && out == "" {
		return "", fmt.Errorf("gagal membaca log %s: %w", unit, err)
	}
	return out, nil
}

// DeleteProfile menghapus profil Cloudflare beserta token/cert/env/routes milik profil tersebut.
func (m *Manager) DeleteProfile(profile string) error {
	name := normalizeCFProfile(profile)
	if err := validateCFProfile(name); err != nil {
		return err
	}

	unit := profileServiceUnit(name)
	_, _ = m.runPrivileged("systemctl", "disable", "--now", unit)

	// Bersihkan file penanda exposure milik profil ini
	if m.store != nil {
		if routes, err := m.store.ListCFRoutes(name); err == nil {
			for _, r := range routes {
				if r.LocalDomain != "" {
					_ = os.Remove(filepath.Join(m.exposuresDir(), r.LocalDomain))
				}
			}
		}
		_ = m.store.DeleteCFRoutesByProfile(name)
		_ = m.store.DeleteCFProfile(name)
	}

	for _, p := range []string{
		m.profileEnvPath(name),
		m.tokenFilePath(name),
		m.certFilePath(name),
		m.credFilePath(name),
		m.routeConfigPath(name),
	} {
		if _, err := os.Stat(p); err == nil {
			if rmErr := os.Remove(p); rmErr != nil {
				_, _ = m.runPrivileged("rm", "-f", p)
			}
		}
	}

	return nil
}

// EnsureSystemdTemplate memasang unit template systemd `/etc/systemd/system/cloudflared@.service`.
func (m *Manager) EnsureSystemdTemplate() error {
	bin, err := m.lookPathFn("cloudflared")
	if err != nil || strings.TrimSpace(bin) == "" {
		bin = "/usr/local/bin/cloudflared"
	}

	unit := fmt.Sprintf(`[Unit]
Description=Cloudflare Tunnel (wdev profile: %%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%s/%%i.env
ExecStart=%s $CLOUDFLARED_ARGS
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, m.profilesEnvDir(), bin)

	if existing, err := os.ReadFile(m.systemdTemplatePath); err == nil && string(existing) == unit {
		return nil
	}

	if err := m.writeConfigFile(m.systemdTemplatePath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("gagal menulis unit systemd %s: %w", m.systemdTemplatePath, err)
	}
	if _, err := m.runPrivileged("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("gagal memuat ulang systemd daemon: %w", err)
	}
	return nil
}

func (m *Manager) writeProfileRunnerFiles(info ProfileInfo) error {
	if err := m.ensureDirs(); err != nil {
		return err
	}

	var argsLine string
	if info.AuthMode == "token" || fileExists(m.tokenFilePath(info.Name)) {
		argsLine = fmt.Sprintf("tunnel --no-autoupdate --metrics 127.0.0.1:0 run --token-file %s", m.tokenFilePath(info.Name))
	} else {
		cfgPath := m.routeConfigPath(info.Name)
		if !fileExists(cfgPath) {
			cfg := CloudflaredConfig{
				TunnelID:    info.TunnelID,
				Credentials: m.credFilePath(info.Name),
				Metrics:     "127.0.0.1:0",
				Ingress:     []IngressRule{{Service: "http_status:404"}},
			}
			if cfg.TunnelID == "" {
				cfg.TunnelID = info.TunnelName
			}
			if data, err := yaml.Marshal(&cfg); err == nil {
				_ = m.writeConfigFile(cfgPath, data, 0644)
			}
		}
		parts := []string{"tunnel", "--no-autoupdate", "--metrics", "127.0.0.1:0", "--config", cfgPath}
		if cert := m.certFilePath(info.Name); fileExists(cert) {
			parts = append(parts, "--origincert", cert)
		}
		parts = append(parts, "run")
		argsLine = strings.Join(parts, " ")
	}

	envContent := fmt.Sprintf("CLOUDFLARED_ARGS=%q\n", argsLine)
	return m.writeConfigFile(m.profileEnvPath(info.Name), []byte(envContent), 0600)
}

func (m *Manager) detectSystemCertFiles() []string {
	var candidates []string
	seen := make(map[string]bool)
	addIfCert := func(p string) {
		clean := filepath.Clean(p)
		if seen[clean] || !fileExists(clean) {
			return
		}
		if raw, err := os.ReadFile(clean); err == nil {
			if strings.Contains(string(raw), "ARGO TUNNEL TOKEN") || strings.Contains(string(raw), "PRIVATE KEY") {
				seen[clean] = true
				candidates = append(candidates, clean)
			}
		}
	}

	addIfCert("/etc/cloudflared/cert.pem")
	addIfCert("/root/.cloudflared/cert.pem")
	if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
		addIfCert(filepath.Join("/home", sudoUser, ".cloudflared", "cert.pem"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		addIfCert(filepath.Join(home, ".cloudflared", "cert.pem"))
	}
	return candidates
}

func defaultStartLoginCmd(ctx context.Context, homeDir, binPath string) (io.ReadCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, binPath, "tunnel", "login")
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "TUNNEL_ORIGIN_CERT=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "HOME="+homeDir)
	cmd.Env = env

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return nil, nil, err
	}
	errCh := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		_ = pw.Close()
		errCh <- waitErr
	}()
	return pr, func() error {
		return <-errCh
	}, nil
}

func newSessionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
