package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"

	"gopkg.in/yaml.v3"
)

var (
	cfHostRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	cfProfileRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	cfLoginURLRe = regexp.MustCompile(`https://dash\.cloudflare\.com/[^\s"'<>]+`)

	// ErrSudoRequired menandakan operasi sistem membutuhkan password sudo saat wdev tidak berjalan sebagai root.
	ErrSudoRequired = errors.New("diperlukan hak akses administrator (sudo)")
)

const (
	defaultSystemdTemplatePath = "/etc/systemd/system/cloudflared@.service"
	defaultLegacyConfigPath    = "/etc/cloudflared/config.yml"
)

func validateCFHost(host, label string) error {
	if host == "" || len(host) > 253 || !cfHostRe.MatchString(host) {
		return fmt.Errorf("%s tidak valid: %q", label, host)
	}
	return nil
}

func normalizeCFProfile(profile string) string {
	return strings.ToLower(strings.TrimSpace(profile))
}

func validateCFProfile(profile string) error {
	if !cfProfileRe.MatchString(profile) {
		return fmt.Errorf("nama profil Cloudflare tidak valid (gunakan huruf, angka, '-' atau '_'): %q", profile)
	}
	return nil
}

// Manager mengelola instalasi, profil multi-akun, systemd template, dan routing Cloudflare Tunnel.
type Manager struct {
	cfg                 *config.Config
	store               *store.Store
	sudoPassFn          func() string
	systemdTemplatePath string
	legacyConfigPath    string
	apiBaseURL          string
	httpClient          *http.Client

	// Hook untuk unit testing
	lookPathFn       func(string) (string, error)
	runCmdFn         func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	isServiceActiveFn  func(unit string) bool
	isServiceEnabledFn func(unit string) bool
	anyTunnelProcFn    func() bool
	runPrivilegedFn    func(bin string, args ...string) (string, error)
	startLoginCmdFn    func(ctx context.Context, homeDir, binPath string) (io.ReadCloser, func() error, error)

	loginMu sync.Mutex
	loginSessions map[string]*LoginSession
}

// OriginRequestConfig memetakan opsi originRequest untuk setiap ingress rule cloudflared.
type OriginRequestConfig struct {
	HTTPHostHeader string `yaml:"httpHostHeader,omitempty"`
	NoTLSVerify    bool   `yaml:"noTLSVerify,omitempty"`
}

// CloudflaredConfig merepresentasikan file YAML konfigurasi daemon cloudflared per profil.
type CloudflaredConfig struct {
	TunnelID    string        `yaml:"tunnel,omitempty"`
	Credentials string        `yaml:"credentials-file,omitempty"`
	Metrics     string        `yaml:"metrics,omitempty"`
	Ingress     []IngressRule `yaml:"ingress"`
}

// IngressRule merepresentasikan satu entri ingress rule di file YAML cloudflared.
type IngressRule struct {
	Hostname      string               `yaml:"hostname,omitempty"`
	Service       string               `yaml:"service"`
	OriginRequest *OriginRequestConfig `yaml:"originRequest,omitempty"`
}

// ProfileInfo merangkum metadata dan status layanan satu profil Cloudflare Tunnel.
type ProfileInfo struct {
	Name           string `json:"name"`
	AuthMode       string `json:"auth_mode"` // "token" | "cert"
	TunnelName     string `json:"tunnel_name,omitempty"`
	TunnelID       string `json:"tunnel_id,omitempty"`
	AccountTag     string `json:"account_tag,omitempty"`
	HasCert        bool   `json:"has_cert"`
	HasCredentials bool   `json:"has_credentials"`
	HasToken       bool   `json:"has_token"`
	HasAPIToken    bool   `json:"has_api_token"`
	Active         bool   `json:"active"`
	Enabled        bool   `json:"enabled"`
	ServiceUnit    string `json:"service_unit"`
	ConfigPath     string `json:"config_path,omitempty"`
	RouteCount     int    `json:"route_count"`
	CreatedAt      string `json:"created_at,omitempty"`
}

// Exposure merangkum satu pemetaan domain lokal ke domain publik Cloudflare.
type Exposure struct {
	LocalDomain    string `json:"local_domain"`
	PublicHost     string `json:"public_host"`
	Profile        string `json:"profile"`
	ServiceURL     string `json:"service_url,omitempty"`
	HTTPHostHeader string `json:"http_host_header,omitempty"`
	NoTLSVerify    bool   `json:"no_tls_verify,omitempty"`
}

// TunnelStatus adalah respons lengkap status Cloudflare Tunnel di Web UI & CLI.
type TunnelStatus struct {
	Installed          bool          `json:"installed"`
	Binary             string        `json:"binary"`
	Active             bool          `json:"active"`
	ServiceUnitReady   bool          `json:"service_unit_ready"`
	Profiles           []string      `json:"profiles"`
	ProfileDetails     []ProfileInfo `json:"profile_details"`
	Exposures          []Exposure    `json:"exposures"`
	DetectedLocalCerts []string      `json:"detected_local_certs,omitempty"`
}

// ExposeOptions mengonfigurasi pembuatan rule ingress Cloudflare untuk vhost atau layanan lokal.
type ExposeOptions struct {
	LocalDomain string `json:"local_domain"`
	PublicHost  string `json:"public_host"`
	Profile     string `json:"profile"`
	ServiceURL  string `json:"service_url,omitempty"`
	NoTLSVerify *bool  `json:"no_tls_verify,omitempty"`
}

// ExposeResult merangkum hasil pembuatan exposure Cloudflare.
type ExposeResult struct {
	Status     string `json:"status"`
	Profile    string `json:"profile"`
	PublicHost string `json:"public_host"`
	ServiceURL string `json:"service_url"`
	DNSWarning string `json:"dns_warning,omitempty"`
}

// UnexposeResult merangkum hasil penghapusan exposure Cloudflare beserta status pembersihan DNS.
type UnexposeResult struct {
	Status     string `json:"status"`
	Profile    string `json:"profile,omitempty"`
	PublicHost string `json:"public_host,omitempty"`
	DNSDeleted bool   `json:"dns_deleted,omitempty"`
	DNSWarning string `json:"dns_warning,omitempty"`
}

// New menginisialisasi cloudflare.Manager dengan konfigurasi dan SQLite store.
func New(cfg *config.Config, s *store.Store) *Manager {
	m := &Manager{
		cfg:                 cfg,
		store:               s,
		systemdTemplatePath: defaultSystemdTemplatePath,
		legacyConfigPath:    defaultLegacyConfigPath,
		apiBaseURL:          "https://api.cloudflare.com/client/v4",
		httpClient:          &http.Client{Timeout: 20 * time.Second},
		loginSessions:       make(map[string]*LoginSession),
	}
	m.lookPathFn = exec.LookPath
	m.runCmdFn = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		return cmd.CombinedOutput()
	}
	m.isServiceActiveFn = func(unit string) bool {
		return exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
	}
	m.isServiceEnabledFn = func(unit string) bool {
		return exec.Command("systemctl", "is-enabled", "--quiet", unit).Run() == nil
	}
	m.anyTunnelProcFn = func() bool {
		return exec.Command("pgrep", "cloudflared").Run() == nil
	}
	m.startLoginCmdFn = defaultStartLoginCmd
	return m
}

// SetSudoPasswordFn menyetel penyedia password sudo saat wdev berjalan sebagai non-root.
func (m *Manager) SetSudoPasswordFn(fn func() string) {
	m.sudoPassFn = fn
}

func (m *Manager) baseDir() string {
	return filepath.Join(m.cfg.WdevEtc, "cloudflare")
}

func (m *Manager) accountsDir() string {
	return filepath.Join(m.baseDir(), "accounts")
}

func (m *Manager) tokensDir() string {
	return filepath.Join(m.baseDir(), "tokens")
}

func (m *Manager) routesDir() string {
	return filepath.Join(m.baseDir(), "routes")
}

func (m *Manager) profilesEnvDir() string {
	return filepath.Join(m.baseDir(), "profiles")
}

func (m *Manager) exposuresDir() string {
	return filepath.Join(m.cfg.WdevEtc, "exposures")
}

func (m *Manager) certFilePath(profile string) string {
	return filepath.Join(m.accountsDir(), profile+".cert")
}

func (m *Manager) credFilePath(profile string) string {
	return filepath.Join(m.accountsDir(), profile+".json")
}

func (m *Manager) tokenFilePath(profile string) string {
	return filepath.Join(m.tokensDir(), profile+".token")
}

func (m *Manager) routeConfigPath(profile string) string {
	return filepath.Join(m.routesDir(), profile+".yml")
}

func (m *Manager) profileEnvPath(profile string) string {
	return filepath.Join(m.profilesEnvDir(), profile+".env")
}

func profileServiceUnit(profile string) string {
	return fmt.Sprintf("cloudflared@%s.service", profile)
}

func (m *Manager) ensureDirs() error {
	for _, dir := range []string{
		m.baseDir(),
		m.accountsDir(),
		m.tokensDir(),
		m.routesDir(),
		m.profilesEnvDir(),
		m.exposuresDir(),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			if os.Geteuid() != 0 {
				if _, runErr := m.runPrivileged("mkdir", "-p", dir); runErr != nil {
					return runErr
				}
				continue
			}
			return err
		}
	}
	_ = os.Chmod(m.accountsDir(), 0700)
	_ = os.Chmod(m.tokensDir(), 0700)
	_ = os.Chmod(m.profilesEnvDir(), 0700)
	return nil
}

// GetStatus mengembalikan status menyeluruh biner, profil multi-akun, dan eksposur aktif.
func (m *Manager) GetStatus() TunnelStatus {
	bin, err := m.lookPathFn("cloudflared")
	installed := err == nil && bin != ""

	unitReady := fileExists(m.systemdTemplatePath)
	profilesMap := m.collectProfileMap()
	exposures := m.collectExposures()

	routeCounts := make(map[string]int)
	for _, exp := range exposures {
		routeCounts[exp.Profile]++
	}

	profileNames := make([]string, 0, len(profilesMap))
	for name := range profilesMap {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)

	profileDetails := make([]ProfileInfo, 0, len(profileNames))
	anyActive := false
	for _, name := range profileNames {
		p := profilesMap[name]
		p.RouteCount = routeCounts[name]
		p.ServiceUnit = profileServiceUnit(name)
		p.Active = m.isServiceActiveFn(p.ServiceUnit)
		p.Enabled = m.isServiceEnabledFn(p.ServiceUnit)
		if p.Active {
			anyActive = true
		}
		profileDetails = append(profileDetails, p)
	}

	if installed && !anyActive {
		anyActive = m.isServiceActiveFn("cloudflared") || m.anyTunnelProcFn()
	}

	if exposures == nil {
		exposures = []Exposure{}
	}

	return TunnelStatus{
		Installed:          installed,
		Binary:             bin,
		Active:             anyActive,
		ServiceUnitReady:   unitReady,
		Profiles:           profileNames,
		ProfileDetails:     profileDetails,
		Exposures:          exposures,
		DetectedLocalCerts: m.detectSystemCertFiles(),
	}
}

func (m *Manager) collectProfileMap() map[string]ProfileInfo {
	out := make(map[string]ProfileInfo)

	if m.store != nil {
		if dbProfiles, err := m.store.ListCFProfiles(); err == nil {
			for _, rec := range dbProfiles {
				name := normalizeCFProfile(rec.Name)
				if name == "" {
					continue
				}
				authMode := rec.AuthMode
				if authMode == "" {
					authMode = "cert"
				}
				info := ProfileInfo{
					Name:       name,
					AuthMode:   authMode,
					TunnelName: rec.TunnelName,
					TunnelID:   rec.TunnelID,
					AccountTag: rec.AccountTag,
					ConfigPath: rec.ConfigPath,
					CreatedAt:  rec.CreatedAt.UTC().Format(time.RFC3339),
				}
				info.HasCert = fileExists(m.certFilePath(name))
				info.HasCredentials = fileExists(m.credFilePath(name)) || (rec.CredentialsPath != "" && fileExists(rec.CredentialsPath))
				info.HasToken = fileExists(m.tokenFilePath(name))
				if info.HasToken && authMode == "cert" && !info.HasCert && !info.HasCredentials {
					info.AuthMode = "token"
				}
				out[name] = info
			}
		}
	}

	// Gabungkan profil legacy dari disk jika belum tercatat di DB
	if entries, err := os.ReadDir(m.accountsDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || (!strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".cert")) {
				continue
			}
			name := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".json"), ".cert")
			name = normalizeCFProfile(name)
			if name == "" {
				continue
			}
			info, exists := out[name]
			if !exists {
				info = ProfileInfo{Name: name, AuthMode: "cert"}
			}
			if strings.HasSuffix(e.Name(), ".cert") {
				info.HasCert = true
			}
			if strings.HasSuffix(e.Name(), ".json") {
				info.HasCredentials = true
				if tid, tag, err := parseCredentialsJSONFile(filepath.Join(m.accountsDir(), e.Name())); err == nil {
					if info.TunnelID == "" {
						info.TunnelID = tid
					}
					if info.AccountTag == "" {
						info.AccountTag = tag
					}
				}
			}
			if fileExists(m.routeConfigPath(name)) && info.ConfigPath == "" {
				info.ConfigPath = m.routeConfigPath(name)
			}
			out[name] = info
		}
	}

	if entries, err := os.ReadDir(m.tokensDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".token") {
				continue
			}
			name := normalizeCFProfile(strings.TrimSuffix(e.Name(), ".token"))
			if name == "" {
				continue
			}
			info, exists := out[name]
			if !exists {
				info = ProfileInfo{Name: name, AuthMode: "token"}
			}
			info.HasToken = true
			if raw, rErr := os.ReadFile(filepath.Join(m.tokensDir(), e.Name())); rErr == nil {
				if parsed, pErr := ParseTunnelToken(string(raw)); pErr == nil {
					if info.TunnelID == "" {
						info.TunnelID = parsed.TunnelID
					}
					if info.AccountTag == "" {
						info.AccountTag = parsed.AccountTag
					}
				}
			}
			out[name] = info
		}
	}

	for name, info := range out {
		info.HasAPIToken = fileExists(m.apiTokenFilePath(name))
		out[name] = info
	}

	return out
}

func (m *Manager) collectExposures() []Exposure {
	byLocal := make(map[string]Exposure)

	if m.store != nil {
		if routes, err := m.store.ListCFRoutes(""); err == nil {
			for _, r := range routes {
				key := strings.ToLower(strings.TrimSpace(r.LocalDomain))
				if key == "" {
					key = strings.ToLower(strings.TrimSpace(r.Hostname))
				}
				if key == "" {
					continue
				}
				prof := normalizeCFProfile(r.ProfileName)
				if prof == "" {
					prof = "default"
				}
				byLocal[key] = Exposure{
					LocalDomain:    r.LocalDomain,
					PublicHost:     r.Hostname,
					Profile:        prof,
					ServiceURL:     r.ServiceURL,
					HTTPHostHeader: r.HTTPHostHeader,
					NoTLSVerify:    r.NoTLSVerify,
				}
			}
		}
	}

	// Gabungkan penanda legacy di /etc/wdev/exposures jika belum masuk cf_routes DB
	if entries, err := os.ReadDir(m.exposuresDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			localDomain := strings.ToLower(strings.TrimSpace(e.Name()))
			content, err := os.ReadFile(filepath.Join(m.exposuresDir(), e.Name()))
			if err != nil {
				continue
			}
			publicHost := strings.TrimSpace(string(content))
			if publicHost == "" {
				continue
			}
			if _, ok := byLocal[localDomain]; !ok {
				byLocal[localDomain] = Exposure{
					LocalDomain: localDomain,
					PublicHost:  publicHost,
					Profile:     "default",
				}
			}
		}
	}

	out := make([]Exposure, 0, len(byLocal))
	for _, exp := range byLocal {
		out = append(out, exp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		return out[i].LocalDomain < out[j].LocalDomain
	})
	return out
}

// Expose menambahkan aturan ingress Cloudflare untuk domain lokal ke domain publik pada profil tertentu.
func (m *Manager) Expose(localDomain, publicHost, profile string) error {
	_, err := m.ExposeWithOptions(ExposeOptions{
		LocalDomain: localDomain,
		PublicHost:  publicHost,
		Profile:     profile,
	})
	return err
}

func resolveExposeTarget(opts ExposeOptions, localDomain string) (serviceURL, httpHostHeader string, noTLSVerify bool, err error) {
	serviceURL = strings.TrimSpace(opts.ServiceURL)
	httpHostHeader = localDomain
	if serviceURL == "" {
		serviceURL = "http://127.0.0.1:80"
		if fileExists(fmt.Sprintf("/etc/ssl/local-certs/%s.pem", localDomain)) {
			serviceURL = "https://127.0.0.1:443"
			noTLSVerify = true
		}
	} else {
		parsed, pErr := url.Parse(serviceURL)
		if pErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return "", "", false, fmt.Errorf("service_url tidak valid (harus http:// atau https://): %q", serviceURL)
		}
		if opts.NoTLSVerify != nil {
			noTLSVerify = *opts.NoTLSVerify
		} else if parsed.Scheme == "https" && (strings.HasPrefix(parsed.Host, "127.0.0.1") || strings.HasPrefix(parsed.Host, "localhost")) {
			noTLSVerify = true
		}
	}
	if opts.NoTLSVerify != nil {
		noTLSVerify = *opts.NoTLSVerify
	}
	return serviceURL, httpHostHeader, noTLSVerify, nil
}

// ExposeWithOptions menambahkan atau memperbarui rute ingress Cloudflare Tunnel secara menyeluruh per profil.
func (m *Manager) ExposeWithOptions(opts ExposeOptions) (*ExposeResult, error) {
	localDomain := strings.ToLower(strings.TrimSpace(opts.LocalDomain))
	publicHost := strings.ToLower(strings.TrimSpace(opts.PublicHost))
	profile := normalizeCFProfile(opts.Profile)
	if profile == "" {
		profile = "default"
	}

	if err := validateCFHost(localDomain, "local_domain"); err != nil {
		return nil, err
	}
	if err := validateCFHost(publicHost, "public_host"); err != nil {
		return nil, err
	}
	if err := validateCFProfile(profile); err != nil {
		return nil, err
	}

	profilesMap := m.collectProfileMap()
	profInfo, profExists := profilesMap[profile]
	apiToken := m.readProfileAPIToken(profile)

	if profExists && profInfo.AuthMode == "token" && apiToken == "" {
		return nil, fmt.Errorf("profil %q menggunakan mode Token (Remotely-Managed); kelola Public Hostname di Cloudflare Zero Trust Dashboard, pasang Cloudflare API Token (Zone:Read, DNS:Edit, Cloudflare Tunnel:Edit), atau gunakan profil mode cert", profile)
	}

	legacyExists := fileExists(m.legacyConfigPath)
	if !profExists && !legacyExists {
		return nil, fmt.Errorf("profil Cloudflare %q belum terdaftar. Daftarkan profil akun terlebih dahulu di menu Cloudflare Tunnel atau via 'wdev cf profile ...'", profile)
	}

	if err := m.ensureDirs(); err != nil {
		return nil, fmt.Errorf("gagal menyiapkan direktori cloudflare: %w", err)
	}

	serviceURL, httpHostHeader, noTLSVerify, err := resolveExposeTarget(opts, localDomain)
	if err != nil {
		return nil, err
	}

	// Mode Token dengan Cloudflare API Token: kelola ingress remote & DNS langsung via Cloudflare REST API
	if profExists && profInfo.AuthMode == "token" {
		if profInfo.AccountTag == "" || profInfo.TunnelID == "" {
			return nil, fmt.Errorf("profil token %q tidak memiliki metadata Account ID / Tunnel ID untuk pembaruan via API", profile)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := m.upsertRemoteTunnelIngress(ctx, apiToken, profInfo.AccountTag, profInfo.TunnelID, publicHost, serviceURL, httpHostHeader, noTLSVerify); err != nil {
			return nil, fmt.Errorf("gagal memperbarui ingress tunnel via Cloudflare API: %w", err)
		}

		_ = os.WriteFile(filepath.Join(m.exposuresDir(), localDomain), []byte(publicHost+"\n"), 0644)
		if m.store != nil {
			_ = m.store.SaveCFRoute(store.CFRoute{
				ProfileName:    profile,
				LocalDomain:    localDomain,
				Hostname:       publicHost,
				ServiceURL:     serviceURL,
				HTTPHostHeader: httpHostHeader,
				NoTLSVerify:    noTLSVerify,
			})
		}

		dnsWarning := ""
		if dnsErr := m.upsertCFDNSRecord(ctx, apiToken, publicHost, profInfo.TunnelID+".cfargotunnel.com"); dnsErr != nil {
			dnsWarning = fmt.Sprintf("Rute tunnel remote disimpan, namun sinkronisasi DNS CNAME via API gagal: %s", dnsErr.Error())
		}
		return &ExposeResult{
			Status:     "exposed",
			Profile:    profile,
			PublicHost: publicHost,
			ServiceURL: serviceURL,
			DNSWarning: dnsWarning,
		}, nil
	}

	targetConfig := m.legacyConfigPath
	if profExists {
		targetConfig = m.routeConfigPath(profile)
	}

	var cfg CloudflaredConfig
	if data, err := os.ReadFile(targetConfig); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}
	if profExists {
		if cfg.TunnelID == "" && profInfo.TunnelID != "" {
			cfg.TunnelID = profInfo.TunnelID
		}
		if cfg.TunnelID == "" && profInfo.TunnelName != "" {
			cfg.TunnelID = profInfo.TunnelName
		}
		if cfg.Credentials == "" && fileExists(m.credFilePath(profile)) {
			cfg.Credentials = m.credFilePath(profile)
		}
		if cfg.Metrics == "" {
			cfg.Metrics = "127.0.0.1:0"
		}
	}

	originReq := &OriginRequestConfig{
		HTTPHostHeader: httpHostHeader,
		NoTLSVerify:    noTLSVerify,
	}
	newRule := IngressRule{
		Hostname:      publicHost,
		Service:       serviceURL,
		OriginRequest: originReq,
	}

	var newRules []IngressRule
	found := false
	for _, rule := range cfg.Ingress {
		if strings.EqualFold(rule.Hostname, publicHost) {
			newRules = append(newRules, newRule)
			found = true
		} else if rule.Service != "http_status:404" && strings.TrimSpace(rule.Service) != "" {
			newRules = append(newRules, rule)
		}
	}
	if !found {
		newRules = append(newRules, newRule)
	}
	newRules = append(newRules, IngressRule{Service: "http_status:404"})
	cfg.Ingress = newRules

	outData, err := yaml.Marshal(&cfg)
	if err != nil {
		return nil, fmt.Errorf("gagal menyusun YAML konfigurasi tunnel: %w", err)
	}
	if err := m.writeConfigFile(targetConfig, outData, 0644); err != nil {
		return nil, fmt.Errorf("gagal menulis konfigurasi tunnel (%s): %w", targetConfig, err)
	}

	// Tulis penanda legacy /etc/wdev/exposures agar SyncFromDisk & site.DeleteSite tetap sinkron
	_ = os.WriteFile(filepath.Join(m.exposuresDir(), localDomain), []byte(publicHost+"\n"), 0644)

	if m.store != nil {
		_ = m.store.SaveCFRoute(store.CFRoute{
			ProfileName:    profile,
			LocalDomain:    localDomain,
			Hostname:       publicHost,
			ServiceURL:     serviceURL,
			HTTPHostHeader: httpHostHeader,
			NoTLSVerify:    noTLSVerify,
		})
	}

	dnsWarning := ""
	certPath := m.certFilePath(profile)
	if apiToken != "" && profInfo.TunnelID != "" {
		// Prioritaskan Cloudflare API Token agar domain lintas zone langsung masuk ke zone yang tepat
		// (mencegah bug CLI `cloudflared route dns` yang menempelkan zone asal cert.pem ke domain lain).
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		apiErr := m.upsertCFDNSRecord(ctx, apiToken, publicHost, profInfo.TunnelID+".cfargotunnel.com")
		cancel()
		if apiErr != nil {
			dnsWarning = fmt.Sprintf("Rute lokal disimpan, namun sinkronisasi DNS CNAME Cloudflare via API gagal: %s", apiErr.Error())
		}
	} else if bin, err := m.lookPathFn("cloudflared"); err == nil && bin != "" {
		tunnelTarget := profInfo.TunnelID
		if tunnelTarget == "" {
			tunnelTarget = profInfo.TunnelName
		}
		if tunnelTarget == "" {
			tunnelTarget = profile
		}

		args := []string{"tunnel"}
		if fileExists(certPath) {
			args = append(args, "--origincert", certPath)
		}
		args = append(args, "route", "dns", "--overwrite-dns", tunnelTarget, publicHost)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, dnsErr := m.runCmdFn(ctx, nil, bin, args...)
		cancel()
		if dnsErr != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = dnsErr.Error()
			}
			dnsWarning = fmt.Sprintf("Rute lokal disimpan, namun sinkronisasi DNS CNAME Cloudflare gagal: %s", msg)
		}
	}

	// Pastikan env & systemd template terpasang dan restart layanan profil jika ada
	if profExists {
		_ = m.writeProfileRunnerFiles(profInfo)
		unit := profileServiceUnit(profile)
		if fileExists(m.systemdTemplatePath) {
			if m.isServiceActiveFn(unit) {
				_, _ = m.runPrivileged("systemctl", "restart", unit)
			} else if profInfo.HasCredentials {
				_, _ = m.runPrivileged("systemctl", "enable", "--now", unit)
			}
		}
	} else if m.isServiceActiveFn("cloudflared") {
		_, _ = m.runPrivileged("systemctl", "restart", "cloudflared")
	}

	return &ExposeResult{
		Status:     "exposed",
		Profile:    profile,
		PublicHost: publicHost,
		ServiceURL: serviceURL,
		DNSWarning: dnsWarning,
	}, nil
}

// Unexpose menghapus aturan ingress Cloudflare berdasarkan domain lokal (dan memperbarui config profil/DB).
func (m *Manager) Unexpose(localDomain string) error {
	return m.UnexposeWithProfile(localDomain, "")
}

// UnexposeWithProfile menghapus exposure berdasarkan domain lokal dan profil opsional.
func (m *Manager) UnexposeWithProfile(localDomain, profile string) error {
	_, err := m.UnexposeWithResult(localDomain, profile)
	return err
}

// UnexposeWithResult menghapus exposure beserta aturan ingress lokal/remote dan opsional membersihkan DNS CNAME via API Token.
func (m *Manager) UnexposeWithResult(localDomain, profile string) (*UnexposeResult, error) {
	localDomain = strings.ToLower(strings.TrimSpace(localDomain))
	if err := validateCFHost(localDomain, "local_domain"); err != nil {
		return nil, err
	}
	cleanProfile := normalizeCFProfile(profile)

	publicHost := ""
	routeProfile := cleanProfile
	if m.store != nil {
		if r, found, err := m.store.GetCFRouteByLocalDomain(localDomain); err == nil && found {
			publicHost = strings.ToLower(strings.TrimSpace(r.Hostname))
			if routeProfile == "" {
				routeProfile = normalizeCFProfile(r.ProfileName)
			}
		}
	}

	expFile := filepath.Join(m.exposuresDir(), localDomain)
	if publicHost == "" {
		if data, err := os.ReadFile(expFile); err == nil {
			publicHost = strings.ToLower(strings.TrimSpace(string(data)))
		}
	}

	profilesMap := m.collectProfileMap()
	profInfo, profExists := profilesMap[routeProfile]
	apiToken := ""
	if routeProfile != "" {
		apiToken = m.readProfileAPIToken(routeProfile)
	}

	// Jika profil mode token & punya API token, perbarui config ingress remote terlebih dahulu
	if profExists && profInfo.AuthMode == "token" && apiToken != "" && profInfo.AccountTag != "" && profInfo.TunnelID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		remErr := m.removeRemoteTunnelIngress(ctx, apiToken, profInfo.AccountTag, profInfo.TunnelID, publicHost, localDomain)
		cancel()
		if remErr != nil {
			return nil, fmt.Errorf("gagal menghapus rute ingress di Cloudflare API: %w", remErr)
		}
	}

	if m.store != nil {
		_ = m.store.DeleteCFRouteByLocalDomain(localDomain)
	}
	_ = os.Remove(expFile)

	// Kumpulkan seluruh file konfigurasi YAML yang perlu dibersihkan
	var yamlTargets []string
	if routeProfile != "" {
		yamlTargets = append(yamlTargets, m.routeConfigPath(routeProfile))
	} else {
		if entries, err := os.ReadDir(m.routesDir()); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
					yamlTargets = append(yamlTargets, filepath.Join(m.routesDir(), e.Name()))
				}
			}
		}
	}
	if fileExists(m.legacyConfigPath) {
		yamlTargets = append(yamlTargets, m.legacyConfigPath)
	}

	var changedProfiles []string
	for _, configFile := range yamlTargets {
		data, err := os.ReadFile(configFile)
		if err != nil {
			continue
		}
		var cfg CloudflaredConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}
		var newRules []IngressRule
		removed := false
		for _, r := range cfg.Ingress {
			matchHost := publicHost != "" && strings.EqualFold(r.Hostname, publicHost)
			matchHeader := r.OriginRequest != nil && strings.EqualFold(r.OriginRequest.HTTPHostHeader, localDomain)
			if matchHost || matchHeader {
				removed = true
				continue
			}
			if r.Service == "http_status:404" {
				continue
			}
			if strings.TrimSpace(r.Service) != "" {
				newRules = append(newRules, r)
			}
		}
		if !removed {
			continue
		}
		newRules = append(newRules, IngressRule{Service: "http_status:404"})
		cfg.Ingress = newRules
		if outData, err := yaml.Marshal(&cfg); err == nil {
			_ = m.writeConfigFile(configFile, outData, 0644)
		}
		if strings.HasPrefix(configFile, m.routesDir()) && strings.HasSuffix(configFile, ".yml") {
			pName := strings.TrimSuffix(filepath.Base(configFile), ".yml")
			changedProfiles = append(changedProfiles, pName)
		}
	}

	for _, prof := range changedProfiles {
		unit := profileServiceUnit(prof)
		if m.isServiceActiveFn(unit) {
			_, _ = m.runPrivileged("systemctl", "restart", unit)
		}
	}
	if m.isServiceActiveFn("cloudflared") {
		_, _ = m.runPrivileged("systemctl", "restart", "cloudflared")
	}

	res := &UnexposeResult{
		Status:     "unexposed",
		Profile:    routeProfile,
		PublicHost: publicHost,
	}
	if apiToken != "" && publicHost != "" {
		targetCNAME := ""
		if profExists && profInfo.TunnelID != "" {
			targetCNAME = profInfo.TunnelID + ".cfargotunnel.com"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		deleted, dnsErr := m.deleteCFDNSRecordByHost(ctx, apiToken, publicHost, targetCNAME)
		cancel()
		res.DNSDeleted = deleted
		if dnsErr != nil {
			res.DNSWarning = fmt.Sprintf("Exposure dihapus, namun pembersihan DNS CNAME di Cloudflare gagal: %s", dnsErr.Error())
		}
	}

	return res, nil
}

func (m *Manager) writeConfigFile(dst string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err == nil {
		if err := os.WriteFile(dst, data, mode); err == nil {
			return nil
		}
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("gagal menulis %s sebagai root", dst)
	}
	return m.writePrivilegedFile(dst, data, mode)
}

func (m *Manager) writePrivilegedFile(dst string, data []byte, mode os.FileMode) error {
	tmpFile, err := os.CreateTemp("", "wdev-cf-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	_ = tmpFile.Close()
	defer os.Remove(tmpPath)

	modeStr := fmt.Sprintf("%o", mode)
	if _, err := m.runPrivileged("mkdir", "-p", filepath.Dir(dst)); err != nil {
		return err
	}
	if _, err := m.runPrivileged("install", "-m", modeStr, tmpPath, dst); err != nil {
		return err
	}
	return nil
}

func (m *Manager) runPrivileged(bin string, args ...string) (string, error) {
	if m.runPrivilegedFn != nil {
		return m.runPrivilegedFn(bin, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if os.Geteuid() == 0 {
		out, err := m.runCmdFn(ctx, nil, bin, args...)
		return strings.TrimSpace(string(out)), err
	}

	sudoArgs := append([]string{"-n", bin}, args...)
	cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
	if out, err := cmd.CombinedOutput(); err == nil {
		return strings.TrimSpace(string(out)), nil
	} else if m.sudoPassFn != nil {
		if pass := m.sudoPassFn(); pass != "" {
			sArgs := append([]string{"-S", "-p", "", bin}, args...)
			pCmd := exec.CommandContext(ctx, "sudo", sArgs...)
			pCmd.Stdin = strings.NewReader(pass + "\n")
			pOut, pErr := pCmd.CombinedOutput()
			return strings.TrimSpace(string(pOut)), pErr
		}
		return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", ErrSudoRequired, strings.TrimSpace(string(out)))
	} else {
		return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", ErrSudoRequired, strings.TrimSpace(string(out)))
	}
}

func fileExists(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func parseCredentialsJSONFile(path string) (tunnelID, accountTag string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	return parseCredentialsJSONBytes(data)
}

func parseCredentialsJSONBytes(data []byte) (tunnelID, accountTag string, err error) {
	var parsed struct {
		AccountTag   string `json:"AccountTag"`
		TunnelSecret string `json:"TunnelSecret"`
		TunnelID     string `json:"TunnelID"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", "", fmt.Errorf("format credentials JSON tunnel tidak valid: %w", err)
	}
	if strings.TrimSpace(parsed.TunnelID) == "" || strings.TrimSpace(parsed.TunnelSecret) == "" {
		return "", "", errors.New("credentials JSON harus memiliki field TunnelID dan TunnelSecret")
	}
	return strings.TrimSpace(parsed.TunnelID), strings.TrimSpace(parsed.AccountTag), nil
}
