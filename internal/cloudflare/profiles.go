package cloudflare

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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

// ParsedOriginCert memuat metadata yang disematkan di blok ARGO TUNNEL TOKEN pada cert.pem.
type ParsedOriginCert struct {
	ZoneID    string `json:"zoneID"`
	AccountID string `json:"accountID"`
	APIToken  string `json:"apiToken"`
}

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
	APIToken   string `json:"api_token,omitempty"`
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
	APIToken        string `json:"api_token,omitempty"`
	AutoStart       *bool  `json:"auto_start,omitempty"`
}

// DeleteProfileOptions mengonfigurasi penghapusan profil termasuk penghapusan tunnel/DNS remote di Cloudflare.
type DeleteProfileOptions struct {
	DeleteRemote bool `json:"delete_remote,omitempty"`
	DeleteDNS    bool `json:"delete_dns,omitempty"`
}

// DeleteProfileResult merangkum hasil penghapusan profil Cloudflare.
type DeleteProfileResult struct {
	Status          string   `json:"status"`
	RemoteDeleted   bool     `json:"remote_deleted,omitempty"`
	DNSDeletedCount int      `json:"dns_deleted_count,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
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

	cancel   context.CancelFunc
	tmpDir   string
	apiToken string
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

func parseOriginCert(certPEM string) *ParsedOriginCert {
	rest := []byte(strings.TrimSpace(certPEM))
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "ARGO TUNNEL TOKEN" {
			continue
		}
		raw := bytes.TrimSpace(block.Bytes)
		if len(raw) == 0 {
			continue
		}
		var out ParsedOriginCert
		if err := json.Unmarshal(raw, &out); err != nil {
			for _, enc := range []*base64.Encoding{
				base64.StdEncoding,
				base64.RawStdEncoding,
				base64.URLEncoding,
				base64.RawURLEncoding,
			} {
				if dec, dErr := enc.DecodeString(string(raw)); dErr == nil {
					if jErr := json.Unmarshal(bytes.TrimSpace(dec), &out); jErr == nil {
						err = nil
						break
					}
				}
			}
			if err != nil {
				continue
			}
		}
		out.ZoneID = strings.TrimSpace(out.ZoneID)
		out.AccountID = strings.TrimSpace(out.AccountID)
		out.APIToken = strings.TrimSpace(out.APIToken)
		if out.ZoneID != "" || out.AccountID != "" || out.APIToken != "" {
			return &out
		}
	}
	return nil
}

func sanitizeAutoProfileName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	// Cloudflare sering menamai akun personal sebagai "<email>'s Account" atau "<nama>'s Account"
	for _, suffix := range []string{"'s account", "’s account"} {
		if strings.HasSuffix(s, suffix) {
			trimmed := strings.TrimSpace(strings.TrimSuffix(s, suffix))
			if trimmed != "" {
				s = trimmed
			}
			break
		}
	}

	var b strings.Builder
	b.Grow(len(s))
	lastHyphen := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case r == '_':
			if b.Len() > 0 {
				b.WriteRune('_')
				lastHyphen = false
			}
		default:
			if b.Len() > 0 && !lastHyphen {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
	}

	clean := strings.Trim(b.String(), "-_")
	if len(clean) > 64 {
		clean = strings.TrimRight(clean[:64], "-_")
	}
	if !cfProfileRe.MatchString(clean) {
		return ""
	}
	return clean
}

func ensureUniqueProfileName(base string, existing map[string]ProfileInfo) string {
	clean := sanitizeAutoProfileName(base)
	if clean == "" {
		if len(existing) == 0 {
			return "default"
		}
		for i := 2; ; i++ {
			cand := fmt.Sprintf("akun-%d", i)
			if _, ok := existing[cand]; !ok {
				return cand
			}
		}
	}
	if _, ok := existing[clean]; !ok {
		return clean
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		prefix := clean
		maxPrefix := 64 - len(suffix)
		if len(prefix) > maxPrefix {
			prefix = strings.TrimRight(prefix[:maxPrefix], "-_")
		}
		if prefix == "" {
			prefix = "akun"
		}
		cand := prefix + suffix
		if _, ok := existing[cand]; !ok {
			return cand
		}
	}
}

type autoProfileHints struct {
	APIToken   string
	CertToken  string
	AccountID  string
	ZoneID     string
	TunnelID   string
	TunnelName string
}

func (m *Manager) resolveAutoProfileName(hints autoProfileHints) (profileName string, tunnelName string) {
	var tokens []string
	apiTok := strings.TrimSpace(hints.APIToken)
	if apiTok != "" {
		tokens = append(tokens, apiTok)
	}
	if certTok := strings.TrimSpace(hints.CertToken); certTok != "" && certTok != apiTok {
		tokens = append(tokens, certTok)
	}

	var merged cfIdentityMetadata
	if len(tokens) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, tok := range tokens {
			meta := m.fetchIdentityMetadata(ctx, tok, hints.AccountID, hints.ZoneID, hints.TunnelID)
			if merged.Email == "" {
				merged.Email = meta.Email
			}
			if merged.AccountName == "" {
				merged.AccountName = meta.AccountName
			}
			if merged.UserHandle == "" {
				merged.UserHandle = meta.UserHandle
			}
			if merged.ZoneName == "" {
				merged.ZoneName = meta.ZoneName
			}
			if merged.TunnelName == "" {
				merged.TunnelName = meta.TunnelName
			}
			if merged.Email != "" || merged.AccountName != "" {
				break
			}
		}
	}

	resolvedTunnel := strings.TrimSpace(hints.TunnelName)
	if resolvedTunnel == "" {
		resolvedTunnel = merged.TunnelName
	}

	candidates := []string{
		merged.Email,
		merged.AccountName,
		merged.UserHandle,
		merged.ZoneName,
		resolvedTunnel,
	}
	base := ""
	for _, cand := range candidates {
		if clean := sanitizeAutoProfileName(cand); clean != "" {
			base = clean
			break
		}
	}

	existing := m.collectProfileMap()
	return ensureUniqueProfileName(base, existing), resolvedTunnel
}

// RegisterTokenProfile mendaftarkan profil multi-akun menggunakan Tunnel Token dari Zero Trust Dashboard.
func (m *Manager) RegisterTokenProfile(opts TokenProfileOptions) (*ProfileInfo, error) {
	parsed, err := ParseTunnelToken(opts.Token)
	if err != nil {
		return nil, err
	}
	cleanAPITok, err := normalizeAPIToken(opts.APIToken)
	if err != nil {
		return nil, err
	}

	name := normalizeCFProfile(opts.Name)
	var fetchedTunnelName string
	if name != "" {
		if err := validateCFProfile(name); err != nil {
			return nil, err
		}
	} else {
		name, fetchedTunnelName = m.resolveAutoProfileName(autoProfileHints{
			APIToken:   cleanAPITok,
			AccountID:  parsed.AccountTag,
			TunnelID:   parsed.TunnelID,
			TunnelName: strings.TrimSpace(opts.TunnelName),
		})
	}
	if err := m.ensureDirs(); err != nil {
		return nil, err
	}

	tokenPath := m.tokenFilePath(name)
	if err := m.writeConfigFile(tokenPath, []byte(parsed.RawToken+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("gagal menyimpan file token: %w", err)
	}
	if cleanAPITok != "" {
		if err := m.saveProfileAPIToken(name, cleanAPITok); err != nil {
			return nil, fmt.Errorf("gagal menyimpan Cloudflare API Token: %w", err)
		}
	}

	tunnelName := strings.TrimSpace(opts.TunnelName)
	if tunnelName == "" && fetchedTunnelName != "" {
		tunnelName = fetchedTunnelName
	}
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
	cleanAPITok, err := normalizeAPIToken(opts.APIToken)
	if err != nil {
		return nil, err
	}

	var credTunnelID, credAccountTag string
	if credText != "" {
		tid, tag, err := parseCredentialsJSONBytes([]byte(credText))
		if err != nil {
			return nil, err
		}
		credTunnelID = tid
		credAccountTag = tag
	}

	certMeta := parseOriginCert(certText)

	name := normalizeCFProfile(opts.Name)
	if name != "" {
		if err := validateCFProfile(name); err != nil {
			return nil, err
		}
	} else {
		accID := credAccountTag
		if accID == "" && certMeta != nil {
			accID = certMeta.AccountID
		}
		zoneID := ""
		certTok := ""
		if certMeta != nil {
			zoneID = certMeta.ZoneID
			certTok = certMeta.APIToken
		}
		tid := strings.TrimSpace(opts.TunnelID)
		if tid == "" {
			tid = credTunnelID
		}
		name, _ = m.resolveAutoProfileName(autoProfileHints{
			APIToken:   cleanAPITok,
			CertToken:  certTok,
			AccountID:  accID,
			ZoneID:     zoneID,
			TunnelID:   tid,
			TunnelName: strings.TrimSpace(opts.TunnelName),
		})
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
	if cleanAPITok != "" {
		if err := m.saveProfileAPIToken(name, cleanAPITok); err != nil {
			return nil, fmt.Errorf("gagal menyimpan Cloudflare API Token: %w", err)
		}
	}

	tunnelName := strings.TrimSpace(opts.TunnelName)
	if tunnelName == "" {
		hostName, _ := os.Hostname()
		if hostName == "" {
			hostName = "wdev"
		}
		hostPrefix := sanitizeAutoProfileName(strings.Split(hostName, ".")[0])
		if hostPrefix == "" {
			hostPrefix = "wdev"
		}
		tunnelName = hostPrefix + "-" + name
		if len(tunnelName) > 64 {
			tunnelName = strings.TrimRight(tunnelName[:64], "-_")
		}
	}

	tunnelID := strings.TrimSpace(opts.TunnelID)
	accountTag := ""

	if credText != "" {
		if tunnelID == "" {
			tunnelID = credTunnelID
		}
		accountTag = credAccountTag
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
	if accountTag == "" && certMeta != nil {
		accountTag = certMeta.AccountID
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
	return m.StartBrowserLoginWithOptions(profile, tunnelName, "")
}

// StartBrowserLoginWithOptions memulai proses `cloudflared tunnel login` terisolasi per profil dengan API Token opsional.
func (m *Manager) StartBrowserLoginWithOptions(profile, tunnelName, apiToken string) (*LoginSession, error) {
	name := normalizeCFProfile(profile)
	if name != "" {
		if err := validateCFProfile(name); err != nil {
			return nil, err
		}
	}
	cleanAPITok, err := normalizeAPIToken(apiToken)
	if err != nil {
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
		apiToken:   cleanAPITok,
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
			APIToken:   sess.apiToken,
		})

		m.loginMu.Lock()
		if regErr != nil {
			cur.Status = "failed"
			cur.Error = regErr.Error()
			cur.Message = "Login berhasil, namun gagal membuat tunnel profil."
		} else {
			resolvedName := name
			if res != nil && res.Name != "" {
				resolvedName = res.Name
			}
			cur.Profile = resolvedName
			cur.Status = "completed"
			cur.Result = res
			cur.Message = fmt.Sprintf("Profil '%s' berhasil terdaftar dan aktif.", resolvedName)
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

// DeleteProfile menghapus profil Cloudflare beserta token/cert/env/routes lokal milik profil tersebut.
func (m *Manager) DeleteProfile(profile string) error {
	_, err := m.DeleteProfileWithOptions(profile, DeleteProfileOptions{})
	return err
}

// DeleteProfileWithOptions menghapus profil Cloudflare lokal dan opsional menghapus tunnel/DNS CNAME di server Cloudflare.
func (m *Manager) DeleteProfileWithOptions(profile string, opts DeleteProfileOptions) (*DeleteProfileResult, error) {
	name := normalizeCFProfile(profile)
	if err := validateCFProfile(name); err != nil {
		return nil, err
	}

	profiles := m.collectProfileMap()
	profInfo, profExists := profiles[name]
	apiToken := m.readProfileAPIToken(name)

	var routes []store.CFRoute
	if opts.DeleteDNS && m.store != nil {
		if rList, err := m.store.ListCFRoutes(name); err == nil {
			routes = rList
		}
	}

	unit := profileServiceUnit(name)
	_, _ = m.runPrivileged("systemctl", "disable", "--now", unit)

	res := &DeleteProfileResult{Status: "deleted"}

	if opts.DeleteRemote {
		if !profExists {
			return nil, fmt.Errorf("profil Cloudflare %q tidak ditemukan", name)
		}
		if err := m.deleteRemoteTunnelForProfile(profInfo, apiToken); err != nil {
			return nil, err
		}
		res.RemoteDeleted = true
	}

	if opts.DeleteDNS && len(routes) > 0 {
		if apiToken == "" {
			res.Warnings = append(res.Warnings, "Record DNS CNAME tidak dapat dihapus otomatis karena profil belum memiliki Cloudflare API Token.")
		} else {
			targetCNAME := ""
			if profInfo.TunnelID != "" {
				targetCNAME = profInfo.TunnelID + ".cfargotunnel.com"
			}
			for _, r := range routes {
				host := strings.TrimSpace(r.Hostname)
				if host == "" {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				deleted, dnsErr := m.deleteCFDNSRecordByHost(ctx, apiToken, host, targetCNAME)
				cancel()
				if dnsErr != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("Gagal menghapus DNS CNAME %s: %s", host, dnsErr.Error()))
				} else if deleted {
					res.DNSDeletedCount++
				}
			}
		}
	}

	// Bersihkan file penanda exposure milik profil ini
	if m.store != nil {
		if routes == nil {
			routes, _ = m.store.ListCFRoutes(name)
		}
		for _, r := range routes {
			if r.LocalDomain != "" {
				_ = os.Remove(filepath.Join(m.exposuresDir(), r.LocalDomain))
			}
		}
		_ = m.store.DeleteCFRoutesByProfile(name)
		_ = m.store.DeleteCFProfile(name)
	}

	for _, p := range []string{
		m.profileEnvPath(name),
		m.tokenFilePath(name),
		m.apiTokenFilePath(name),
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

	return res, nil
}

func (m *Manager) deleteRemoteTunnelForProfile(profInfo ProfileInfo, apiToken string) error {
	tunnelTarget := strings.TrimSpace(profInfo.TunnelID)
	if tunnelTarget == "" {
		tunnelTarget = strings.TrimSpace(profInfo.TunnelName)
	}

	if profInfo.AuthMode != "token" {
		certPath := m.certFilePath(profInfo.Name)
		if fileExists(certPath) && tunnelTarget != "" {
			bin, err := m.lookPathFn("cloudflared")
			if err != nil || bin == "" {
				if apiToken != "" && profInfo.AccountTag != "" && profInfo.TunnelID != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
					defer cancel()
					return m.deleteRemoteTunnelViaAPI(ctx, apiToken, profInfo.AccountTag, profInfo.TunnelID)
				}
				return errors.New("biner cloudflared belum terpasang untuk menghapus tunnel di server Cloudflare")
			}
			ctxClean, cancelClean := context.WithTimeout(context.Background(), 15*time.Second)
			_, _ = m.runCmdFn(ctxClean, nil, bin, "tunnel", "--origincert", certPath, "cleanup", tunnelTarget)
			cancelClean()

			ctxDel, cancelDel := context.WithTimeout(context.Background(), 25*time.Second)
			out, delErr := m.runCmdFn(ctxDel, nil, bin, "tunnel", "--origincert", certPath, "delete", "-f", tunnelTarget)
			cancelDel()
			if delErr == nil {
				return nil
			}
			msg := strings.TrimSpace(string(out))
			if strings.Contains(strings.ToLower(msg), "not found") {
				return nil
			}
			if apiToken != "" && profInfo.AccountTag != "" && profInfo.TunnelID != "" {
				ctxAPI, cancelAPI := context.WithTimeout(context.Background(), 25*time.Second)
				apiErr := m.deleteRemoteTunnelViaAPI(ctxAPI, apiToken, profInfo.AccountTag, profInfo.TunnelID)
				cancelAPI()
				if apiErr == nil {
					return nil
				}
			}
			if msg == "" {
				msg = delErr.Error()
			}
			return fmt.Errorf("gagal menghapus tunnel %q di server Cloudflare: %s", tunnelTarget, msg)
		}
	}

	if apiToken != "" && profInfo.AccountTag != "" && profInfo.TunnelID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := m.deleteRemoteTunnelViaAPI(ctx, apiToken, profInfo.AccountTag, profInfo.TunnelID); err != nil {
			return fmt.Errorf("gagal menghapus tunnel via Cloudflare API: %w", err)
		}
		return nil
	}

	if profInfo.AuthMode == "token" {
		return fmt.Errorf("profil mode Token memerlukan Cloudflare API Token (izin Cloudflare Tunnel:Edit) untuk menghapus tunnel di server Cloudflare")
	}
	return fmt.Errorf("tidak dapat menghapus tunnel di server Cloudflare: cert.pem atau Cloudflare API Token tidak tersedia untuk profil %q", profInfo.Name)
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
