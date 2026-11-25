package server

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	"wdev/internal/i18n"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/serviceconfig"
	"wdev/internal/store"
)

const (
	externalPortCacheTTL = 45 * time.Second
	portDialTimeout      = 650 * time.Millisecond
	externalHTTPTimeout  = 3500 * time.Millisecond
)

var privateVPNCIDRs = parseCIDRList([]string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
})

// PortScopeDetail merangkum status keterjangkauan satu port pada cakupan jaringan tertentu.
type PortScopeDetail struct {
	Status  string `json:"status"` // "open" | "blocked" | "closed" | "internal" | "na" | "unverified"
	Color   string `json:"color"`  // "green" | "yellow" | "red" | "neutral"
	Label   string `json:"label"`
	Target  string `json:"target,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Checked bool   `json:"checked"`
}

// PortEntry merangkum konfigurasi, binding socket, status Lokal/VPN/VPS, dan rekomendasi satu port wdev.
type PortEntry struct {
	ID             string          `json:"id"`
	Port           int             `json:"port"`
	Protocol       string          `json:"protocol"`
	Service        string          `json:"service"`
	Name           string          `json:"name"`
	Category       string          `json:"category"`
	CategoryLabel  string          `json:"category_label"`
	Installed      bool            `json:"installed"`
	ExpectedScope  string          `json:"expected_scope"` // "public" | "vpn" | "local" | "internal" | "edge_remote"
	BindAddresses  []string        `json:"bind_addresses,omitempty"`
	ConfigHint     string          `json:"config_hint,omitempty"`
	OverallStatus  string          `json:"overall_status"` // "open" | "blocked" | "closed" | "internal" | "not_installed"
	OverallColor   string          `json:"overall_color"`  // "green" | "yellow" | "red" | "neutral"
	OverallLabel   string          `json:"overall_label"`
	Local          PortScopeDetail `json:"local"`
	VPN            PortScopeDetail `json:"vpn"`
	VPS            PortScopeDetail `json:"vps"`
	Description    string          `json:"description"`
	Recommendation string          `json:"recommendation,omitempty"`
}

// PortFirewallInfo merangkum status firewall host (UFW atau firewalld).
type PortFirewallInfo struct {
	Engine          string `json:"engine"` // "ufw" | "firewalld" | "none"
	Active          bool   `json:"active"`
	DefaultIncoming string `json:"default_incoming,omitempty"`
	Summary         string `json:"summary"`
	RulesChecked    bool   `json:"rules_checked"`
	RulesError      string `json:"rules_error,omitempty"`
}

// PortNetworkContext merangkum identitas jaringan host dan mode operasional server.
type PortNetworkContext struct {
	Mode          string           `json:"mode"`
	LocalIP       string           `json:"local_ip"`
	LanIP         string           `json:"lan_ip,omitempty"`
	VPNIP         string           `json:"vpn_ip,omitempty"`
	VPSVPNIP      string           `json:"vps_vpn_ip,omitempty"`
	VPSPublicIP   string           `json:"vps_public_ip,omitempty"`
	NetbirdActive bool             `json:"netbird_active"`
	Firewall      PortFirewallInfo `json:"firewall"`
}

// PortSummaryCounts merangkum jumlah port menurut status warna diagnostik.
type PortSummaryCounts struct {
	Total        int `json:"total"`
	Installed    int `json:"installed"`
	Green        int `json:"green"`
	Yellow       int `json:"yellow"`
	Red          int `json:"red"`
	Neutral      int `json:"neutral"`
	NotInstalled int `json:"not_installed"`
}

// PortStatusReport adalah respons lengkap endpoint GET /api/ports.
type PortStatusReport struct {
	CheckedAt string             `json:"checked_at"`
	Network   PortNetworkContext `json:"network"`
	Summary   PortSummaryCounts  `json:"summary"`
	Ports     []PortEntry        `json:"ports"`
}

type socketBindings struct {
	tcp map[int][]net.IP
	udp map[int][]net.IP
}

func newSocketBindings() socketBindings {
	return socketBindings{
		tcp: make(map[int][]net.IP),
		udp: make(map[int][]net.IP),
	}
}

type fwRule struct {
	proto      string // "tcp" | "udp" | "any"
	portStart  int
	portEnd    int
	allow      bool
	publicFrom bool
	cidrs      []*net.IPNet
}

type firewallSnapshot struct {
	info        PortFirewallInfo
	defaultDeny bool
	rules       []fwRule
}

type fwDecision struct {
	known         bool
	publicAllowed bool
	vpnAllowed    bool
	note          string
}

type portExternalProbeCacheEntry struct {
	reachable bool
	checked   bool
	checkedAt time.Time
}

type portProbeDeps struct {
	readSocketsFn       func() socketBindings
	detectFirewallFn    func(ctx context.Context) firewallSnapshot
	dialTCPFn           func(host string, port int, timeout time.Duration) bool
	probeExternalFn     func(port int, wantIP string, force bool) (reachable bool, checked bool)
	detectPublicIPFn    func() string
	detectSSHPortFn     func() (int, bool)
	hasActiveSSLSitesFn func() bool
	isUnitActiveFn      func(name string) bool
	fileExistsFn        func(path string) bool
	lookPathFn          func(file string) string
}

var (
	globalExtPortCacheMu sync.Mutex
	globalExtPortCache   = make(map[string]portExternalProbeCacheEntry)
)

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	lang := i18n.FromRequest(r)
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, i18n.Pick(lang, "Method not allowed", "Method tidak diizinkan"))
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	report := s.collectPortDiagnostics(r.Context(), refresh, lang)
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) defaultPortProbeDeps() portProbeDeps {
	return portProbeDeps{
		readSocketsFn:       readSystemSocketBindings,
		detectFirewallFn:    detectHostFirewall,
		dialTCPFn:           probePortTCP,
		probeExternalFn:     probeExternalPortCached,
		detectPublicIPFn:    pdns.DetectPublicIP,
		detectSSHPortFn:     detectSSHServerStatus,
		hasActiveSSLSitesFn: s.hasAnySSLSiteConfigured,
		isUnitActiveFn:      monitor.IsSystemdServiceActive,
		fileExistsFn:        portFileExists,
		lookPathFn: func(file string) string {
			p, err := exec.LookPath(file)
			if err != nil {
				return ""
			}
			return p
		},
	}
}

func (s *Server) activePortDeps() portProbeDeps {
	if s.portDeps != nil {
		return *s.portDeps
	}
	return s.defaultPortProbeDeps()
}

type portCandidate struct {
	id            string
	port          int
	protocol      string
	service       string
	name          string
	category      string
	categoryLabel string
	installed     bool
	expectedScope string // "public" | "vpn" | "local" | "internal" | "edge_remote"
	configHint    string
	description   string
	// Kebijakan khusus untuk database/SSH/Nginx/API
	allowPublic   bool
	allowVPN      bool
	preferPrivate bool
	securityAPIGuard bool // port admin API internal/VPN (8081, 8082) yang tidak boleh terbuka ke publik
	edgeTerminatedSSL bool // port 443 di edge mode diterminasi di VPS Edge Gateway
	sslOptionalIdle   bool // port 443 lokal belum ada vhost SSL aktif
	localBindHint     string
	remediationHint   string
}

func (s *Server) collectPortDiagnostics(ctx context.Context, forceRefresh bool, lang string) PortStatusReport {
	loc := i18n.Normalize(lang)
	deps := s.activePortDeps()

	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	var (
		sockets socketBindings
		fw      firewallSnapshot
		wgMeta  sync.WaitGroup
	)
	wgMeta.Add(2)
	go func() {
		defer wgMeta.Done()
		sockets = deps.readSocketsFn()
	}()
	if s.portDeps != nil {
		go func() {
			defer wgMeta.Done()
			fw = deps.detectFirewallFn(probeCtx)
		}()
	} else {
		go func() {
			defer wgMeta.Done()
			fw = detectHostFirewallLocale(probeCtx, loc)
		}()
	}

	mode := "local"
	var pdnsCfg pdns.EdgeConfig
	vpnIP := ""
	if s.pdnsMgr != nil {
		mode = s.pdnsMgr.GetMode()
		pdnsCfg = s.pdnsMgr.LoadConfig()
		vpnIP = strings.TrimSpace(pdnsCfg.LocalNetbirdIP)
		if vpnIP == "" {
			vpnIP = strings.TrimSpace(s.pdnsMgr.GetLocalNetbirdIP())
		}
	}
	if vpnIP == "" {
		vpnIP = detectLocalCGNATFallback()
	}

	lanIP := strings.TrimSpace(pdns.DetectLocalLANIP())
	netbirdActive := vpnIP != "" || deps.isUnitActiveFn("netbird")

	vpsVPNIP := ""
	if mode == "edge" && pdnsCfg.VPSNetbirdIP != "" && !isLoopbackOrSameHost(pdnsCfg.VPSNetbirdIP, vpnIP) {
		vpsVPNIP = strings.TrimSpace(pdnsCfg.VPSNetbirdIP)
	}

	vpsPublicIP := ""
	if mode == "vps" {
		if pdns.IsPublicIPv4String(pdnsCfg.VPSPublicIP) {
			vpsPublicIP = pdnsCfg.VPSPublicIP
		} else if deps.detectPublicIPFn != nil {
			if pub := strings.TrimSpace(deps.detectPublicIPFn()); pdns.IsPublicIPv4String(pub) {
				vpsPublicIP = pub
			}
		}
	} else if mode == "edge" {
		if pdns.IsPublicIPv4String(pdnsCfg.VPSPublicIP) {
			vpsPublicIP = strings.TrimSpace(pdnsCfg.VPSPublicIP)
		}
	}

	wgMeta.Wait()

	netCtx := PortNetworkContext{
		Mode:          mode,
		LocalIP:       "127.0.0.1",
		LanIP:         lanIP,
		VPNIP:         vpnIP,
		VPSVPNIP:      vpsVPNIP,
		VPSPublicIP:   vpsPublicIP,
		NetbirdActive: netbirdActive,
		Firewall:      fw.info,
	}

	candidates := s.buildPortCandidates(probeCtx, deps, netCtx, pdnsCfg, loc)
	entries := make([]PortEntry, len(candidates))

	// Jalankan probe koneksi secara paralel dengan batas wajar agar cepat selesai.
	var wgPorts sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range candidates {
		wgPorts.Add(1)
		go func(idx int) {
			defer wgPorts.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			entries[idx] = evaluatePortCandidateLocale(candidates[idx], netCtx, sockets, fw, deps, forceRefresh, loc)
		}(i)
	}
	wgPorts.Wait()

	summary := PortSummaryCounts{Total: len(entries)}
	for _, e := range entries {
		if !e.Installed {
			summary.NotInstalled++
			summary.Neutral++
			continue
		}
		summary.Installed++
		switch e.OverallColor {
		case "green":
			summary.Green++
		case "yellow":
			summary.Yellow++
		case "red":
			summary.Red++
		default:
			summary.Neutral++
		}
	}

	return PortStatusReport{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Network:   netCtx,
		Summary:   summary,
		Ports:     entries,
	}
}

func (s *Server) buildPortCandidates(
	ctx context.Context,
	deps portProbeDeps,
	netCtx PortNetworkContext,
	pdnsCfg pdns.EdgeConfig,
	lang string,
) []portCandidate {
	panelPort := s.panelPortNumber()
	httpHost := "0.0.0.0"
	if s.cfg != nil && strings.TrimSpace(s.cfg.HTTPHost) != "" {
		httpHost = strings.TrimSpace(s.cfg.HTTPHost)
	}

	panelDomain := ""
	blockDirectIP := false
	if s.authMgr != nil {
		panelDomain, _, blockDirectIP = s.authMgr.PanelDomainConfig()
	}

	panelScope := "public"
	panelAllowPub := true
	panelDesc := i18n.Pick(lang, "wdev control panel web interface & API.", "Antarmuka web & API control panel wdev.")
	panelHint := fmt.Sprintf("host=%s", httpHost)
	if isLoopbackAddrStr(httpHost) {
		panelScope = "local"
		panelAllowPub = false
		panelDesc = i18n.Pick(lang, "wdev panel is configured to listen on localhost (127.0.0.1) only.", "Panel wdev dikonfigurasi hanya listen di localhost (127.0.0.1).")
	} else if blockDirectIP && panelDomain != "" {
		panelScope = "internal"
		panelAllowPub = false
		panelHint = fmt.Sprintf(i18n.Pick(lang, "domain=%s (IP block active)", "domain=%s (blokir IP aktif)"), panelDomain)
		panelDesc = fmt.Sprintf(i18n.Pick(lang,
			"Panel is accessed via official domain (%s) on ports 80/443; direct IP access is blocked by the application.",
			"Panel diakses lewat domain resmi (%s) pada port 80/443; akses IP langsung diblokir aplikasi.",
		), panelDomain)
	} else if netCtx.Mode == "local" {
		panelScope = "vpn"
		panelAllowPub = false
		if panelDomain != "" {
			panelHint = "domain=" + panelDomain
		}
	} else if netCtx.Mode == "edge" {
		panelScope = "vpn"
		panelAllowPub = false
	} else if panelDomain != "" {
		panelHint = "domain=" + panelDomain
		panelDesc = fmt.Sprintf(i18n.Pick(lang,
			"wdev web panel interface (domain %s active on ports 80/443; direct panel port optional).",
			"Antarmuka web panel wdev (domain %s aktif di port 80/443; port direct panel opsional).",
		), panelDomain)
	}

	sshPort, sshInstalled := deps.detectSSHPortFn()
	if sshPort <= 0 {
		sshPort = 22
	}

	nginxInstalled := deps.lookPathFn("nginx") != "" || deps.fileExistsFn("/etc/nginx/nginx.conf") || deps.isUnitActiveFn("nginx")
	hasSSL := deps.hasActiveSSLSitesFn()

	// PostgreSQL
	pgPort := 5432
	pgListen := "localhost"
	pgRemoteMode := "local"
	pgInstalled := deps.lookPathFn("psql") != "" || deps.fileExistsFn("/etc/postgresql") || deps.isUnitActiveFn("postgresql")
	var pgCfg *serviceconfig.PostgresConfig
	if s.svcCfgMgr != nil {
		if cfg, err := s.svcCfgMgr.GetPostgresConfig(ctx, ""); err == nil && cfg != nil {
			pgCfg = cfg
			pgInstalled = true
			if cfg.Port > 0 {
				pgPort = cfg.Port
			}
			if strings.TrimSpace(cfg.ListenAddresses) != "" {
				pgListen = strings.TrimSpace(cfg.ListenAddresses)
			}
			if cfg.RemoteAccessMode != "" {
				pgRemoteMode = cfg.RemoteAccessMode
			}
		}
	}

	pgExpected := "local"
	pgAllowPub := false
	pgAllowVPN := false
	pgPreferPriv := false
	if pgRemoteMode == "all" {
		pgExpected = "public"
		pgAllowPub = true
		pgAllowVPN = true
	} else if pgRemoteMode == "private_vpn" || (!isLoopbackListenList(pgListen) && pgListen != "") {
		pgExpected = "vpn"
		pgAllowVPN = true
		pgPreferPriv = true
	}
	pgHint := fmt.Sprintf("listen_addresses='%s' · mode=%s", pgListen, pgRemoteMode)
	if pgCfg == nil && pgInstalled {
		pgHint = i18n.Pick(lang, "active socket detection", "deteksi socket aktif")
	}

	// Redis / Valkey
	redisPort := 6379
	redisBind := "127.0.0.1 ::1"
	redisInstalled := deps.lookPathFn("redis-server") != "" || deps.lookPathFn("valkey-server") != "" ||
		deps.fileExistsFn("/etc/redis/redis.conf") || deps.fileExistsFn("/etc/valkey/valkey.conf") ||
		deps.isUnitActiveFn("redis-server") || deps.isUnitActiveFn("valkey-server")
	if s.svcCfgMgr != nil {
		if cfg, err := s.svcCfgMgr.GetRedisConfig(ctx); err == nil && cfg != nil {
			redisInstalled = true
			if cfg.Port > 0 {
				redisPort = cfg.Port
			}
			if strings.TrimSpace(cfg.Bind) != "" {
				redisBind = strings.TrimSpace(cfg.Bind)
			}
		}
	}

	redisLocalOnly := isLoopbackBindList(redisBind)
	redisExpected := "local"
	redisAllowPub := false
	redisAllowVPN := false
	redisPreferPriv := false
	if !redisLocalOnly {
		redisExpected = "vpn"
		redisAllowVPN = true
		redisPreferPriv = true
		if netCtx.Mode == "vps" {
			redisExpected = "vpn"
		}
	}

	// PowerDNS & Edge Agent
	pdnsAPIPort := 8081
	if p, err := strconv.Atoi(strings.TrimSpace(pdnsCfg.PDNSPort)); err == nil && p > 0 && p <= 65535 {
		pdnsAPIPort = p
	}
	agentPort := 8082
	if p, err := strconv.Atoi(strings.TrimSpace(pdnsCfg.AgentPort)); err == nil && p > 0 && p <= 65535 {
		agentPort = p
	}
	localPDNSInstalled := pdnsCfg.HasLocalPDNS || deps.lookPathFn("pdns_server") != "" || deps.fileExistsFn("/etc/powerdns/pdns.conf")
	edgeConfigured := netCtx.Mode == "edge" && pdnsCfg.Configured && netCtx.VPSVPNIP != ""
	pdnsInstalled := localPDNSInstalled || edgeConfigured
	agentInstalled := edgeConfigured || deps.isUnitActiveFn("wdev-edge-agent")

	stalwartInstalled := isStalwartInstalled()

	catPanel := i18n.Pick(lang, "Panel & Access", "Panel & Akses")
	catWeb := i18n.Pick(lang, "Web & Proxy", "Web & Proxy")
	catDB := i18n.Pick(lang, "Database & Cache", "Database & Cache")
	catDNS := i18n.Pick(lang, "DNS & Edge Gateway", "DNS & Edge Gateway")
	catMail := i18n.Pick(lang, "Mail Server (Stalwart)", "Mail Server (Stalwart)")

	candidates := []portCandidate{
		{
			id:              "wdev-panel",
			port:            panelPort,
			protocol:        "TCP",
			service:         "wdev",
			name:            "wdev Web Dashboard",
			category:        "system",
			categoryLabel:   catPanel,
			installed:       true,
			expectedScope:   panelScope,
			configHint:      panelHint,
			allowPublic:     panelAllowPub,
			allowVPN:        !isLoopbackAddrStr(httpHost),
			description:     panelDesc,
			remediationHint: i18n.Pick(lang, "Check '--host' flag on the wdev service or firewall rules for the panel port.", "Periksa flag '--host' pada service wdev atau rule firewall untuk port panel."),
		},
		{
			id:              "ssh",
			port:            sshPort,
			protocol:        "TCP",
			service:         "sshd",
			name:            "OpenSSH Server",
			category:        "system",
			categoryLabel:   catPanel,
			installed:       sshInstalled,
			expectedScope:   "public",
			allowPublic:     netCtx.Mode == "vps" || netCtx.Mode == "edge",
			allowVPN:        true,
			description:     i18n.Pick(lang, "Remote SSH terminal & SFTP access to the host server.", "Akses remote SSH terminal & SFTP ke server host."),
			remediationHint: i18n.Pick(lang, "Ensure sshd is running and never block the SSH port when enabling the firewall.", "Pastikan sshd aktif dan jangan pernah menutup port SSH saat mengaktifkan firewall."),
		},
		{
			id:              "nginx-http",
			port:            80,
			protocol:        "TCP",
			service:         "nginx",
			name:            "Nginx HTTP",
			category:        "web",
			categoryLabel:   catWeb,
			installed:       nginxInstalled,
			expectedScope:   "public",
			allowPublic:     netCtx.Mode != "local",
			allowVPN:        true,
			description:     i18n.Pick(lang, "Primary web server for HTTP, Let's Encrypt ACME challenges, and reverse proxy.", "Web server utama untuk HTTP, challenge ACME Let's Encrypt, dan reverse proxy."),
			remediationHint: i18n.Pick(lang, "Open port 80/tcp in the firewall / Security Group so public domains & Let's Encrypt SSL are reachable.", "Buka port 80/tcp di firewall / Security Group agar domain publik & SSL Let's Encrypt dapat diakses."),
		},
		{
			id:                "nginx-https",
			port:              443,
			protocol:          "TCP",
			service:           "nginx",
			name:              "Nginx HTTPS / SSL",
			category:          "web",
			categoryLabel:     catWeb,
			installed:         nginxInstalled,
			expectedScope:     "public",
			allowPublic:       netCtx.Mode != "local",
			allowVPN:          true,
			edgeTerminatedSSL: netCtx.Mode == "edge",
			sslOptionalIdle:   !hasSSL,
			description:       i18n.Pick(lang, "HTTPS (TLS) endpoint for all websites and the wdev panel.", "Endpoint HTTPS (TLS) untuk seluruh website dan panel wdev."),
			remediationHint:   i18n.Pick(lang, "Enable SSL on at least one site and open port 443/tcp in the firewall / Security Group.", "Aktifkan SSL pada minimal satu situs dan buka port 443/tcp pada firewall / Security Group."),
		},
		{
			id:              "postgresql",
			port:            pgPort,
			protocol:        "TCP",
			service:         "postgresql",
			name:            "PostgreSQL Database",
			category:        "database",
			categoryLabel:   catDB,
			installed:       pgInstalled,
			expectedScope:   pgExpected,
			configHint:      pgHint,
			allowPublic:     pgAllowPub,
			allowVPN:        pgAllowVPN,
			preferPrivate:   pgPreferPriv,
			localBindHint:   i18n.Pick(lang, "Currently listening on localhost (127.0.0.1) only. Switch access mode to LAN & VPN in PostgreSQL Settings to connect via NetBird.", "Saat ini hanya listen lokal (127.0.0.1). Ubah mode akses ke LAN & VPN di Pengaturan PostgreSQL jika ingin terhubung via NetBird."),
			description:     i18n.Pick(lang, "PostgreSQL relational database server.", "Server database relasional PostgreSQL."),
			remediationHint: i18n.Pick(lang, "Open Status Menu -> PostgreSQL Config and select 'All Private IPs (LAN & NetBird VPN)' to set listen_addresses='*' and sync firewall rules.", "Buka Menu Status -> Konfigurasi PostgreSQL lalu pilih 'Semua IP Privat (LAN & NetBird VPN)' agar listen_addresses='*' dan firewall tersinkron."),
		},
		{
			id:              "redis",
			port:            redisPort,
			protocol:        "TCP",
			service:         "redis",
			name:            "Redis / Valkey Cache",
			category:        "database",
			categoryLabel:   catDB,
			installed:       redisInstalled,
			expectedScope:   redisExpected,
			configHint:      "bind " + redisBind,
			allowPublic:     redisAllowPub,
			allowVPN:        redisAllowVPN,
			preferPrivate:   redisPreferPriv,
			localBindHint:   i18n.Pick(lang, "Currently bound locally (127.0.0.1). Keep local for security, or add a NetBird IP in Redis Settings for remote access.", "Saat ini bind lokal (127.0.0.1). Pertahankan lokal demi keamanan, atau tambahkan IP NetBird di Pengaturan Redis jika dibutuhkan jarak jauh."),
			description:     i18n.Pick(lang, "Redis/Valkey in-memory cache & message broker.", "In-memory cache & message broker Redis/Valkey."),
			remediationHint: i18n.Pick(lang, "Check the 'bind' parameter and host firewall if Redis/Valkey needs access via NetBird VPN.", "Periksa parameter 'bind' dan firewall host jika Redis/Valkey perlu diakses melalui NetBird VPN."),
		},
	}

	// Tambahkan port upstream aplikasi proxy dari daftar situs Nginx yang dikelola wdev.
	if managedSites := s.listManagedSites(); len(managedSites) > 0 {
		seenSitePorts := make(map[int]bool)
		for _, st := range managedSites {
			if !strings.EqualFold(st.Framework, "proxy") || st.ProxyPort <= 0 {
				continue
			}
			if st.ProxyPort == panelPort || st.ProxyPort == 80 || st.ProxyPort == 443 ||
				st.ProxyPort == pgPort || st.ProxyPort == redisPort || st.ProxyPort == 8080 ||
				st.ProxyPort == pdnsAPIPort || st.ProxyPort == agentPort || seenSitePorts[st.ProxyPort] {
				continue
			}
			seenSitePorts[st.ProxyPort] = true
			candidates = append(candidates, portCandidate{
				id:              fmt.Sprintf("site-proxy-%d", st.ProxyPort),
				port:            st.ProxyPort,
				protocol:        "TCP",
				service:         "app-proxy",
				name:            fmt.Sprintf("Upstream App (%s)", st.Domain),
				category:        "web",
				categoryLabel:   catWeb,
				installed:       true,
				expectedScope:   "internal",
				configHint:      "proxy_pass -> 127.0.0.1:" + strconv.Itoa(st.ProxyPort),
				description:     fmt.Sprintf(i18n.Pick(lang, "Backend application process port for domain %s (proxied by Nginx).", "Port proses aplikasi backend untuk domain %s (diproxy oleh Nginx)."), st.Domain),
				remediationHint: fmt.Sprintf(i18n.Pick(lang, "Ensure the backend app for %s is running and listening on port %d.", "Pastikan aplikasi backend untuk %s sedang berjalan dan listen di port %d."), st.Domain, st.ProxyPort),
			})
		}
	}

	// DNS & Edge Gateway ports
	dnsExpected := "public"
	if netCtx.Mode == "edge" {
		dnsExpected = "edge_remote"
	} else if netCtx.Mode == "local" {
		dnsExpected = "local"
	}
	candidates = append(candidates,
		portCandidate{
			id:              "pdns-dns",
			port:            53,
			protocol:        "TCP/UDP",
			service:         "pdns",
			name:            "PowerDNS Authoritative",
			category:        "dns",
			categoryLabel:   catDNS,
			installed:       pdnsInstalled,
			expectedScope:   dnsExpected,
			configHint:      condStr(netCtx.Mode == "edge", "VPS Edge DNS", "Local PowerDNS"),
			allowPublic:     netCtx.Mode != "local",
			allowVPN:        true,
			description:     i18n.Pick(lang, "Authoritative DNS nameserver (TCP & UDP 53) for PowerDNS-managed domains.", "Nameserver authoritative DNS (TCP & UDP 53) untuk domain yang dikelola PowerDNS."),
			remediationHint: i18n.Pick(lang, "Ensure systemd-resolved does not conflict with port 53 and open port 53 TCP/UDP in the firewall.", "Pastikan systemd-resolved tidak bentrok dengan port 53 dan buka port 53 TCP/UDP pada firewall."),
		},
		portCandidate{
			id:               "pdns-api",
			port:             pdnsAPIPort,
			protocol:         "TCP",
			service:          "pdns-api",
			name:             "PowerDNS HTTP API",
			category:         "dns",
			categoryLabel:    catDNS,
			installed:        pdnsInstalled,
			expectedScope:    condStr(netCtx.Mode == "edge", "edge_remote", "internal"),
			configHint:       condStr(netCtx.Mode == "edge", "NetBird VPN Only", i18n.Pick(lang, "127.0.0.1 Internal", "127.0.0.1 Internal")),
			allowVPN:         netCtx.Mode == "edge",
			securityAPIGuard: true,
			description:      i18n.Pick(lang, "Internal PowerDNS REST API for automated DNS zone and record syncing.", "REST API internal PowerDNS untuk sinkronisasi zone dan record DNS otomatis."),
			remediationHint:  i18n.Pick(lang, "PowerDNS API must be restricted to localhost or the NetBird subnet (100.64.0.0/10).", "API PowerDNS harus dibatasi hanya pada localhost atau subnet NetBird (100.64.0.0/10)."),
		},
		portCandidate{
			id:               "edge-agent",
			port:             agentPort,
			protocol:         "TCP",
			service:          "edge-agent",
			name:             "wdev Edge Gateway Agent",
			category:         "dns",
			categoryLabel:    catDNS,
			installed:        agentInstalled,
			expectedScope:    "edge_remote",
			configHint:       "NetBird VPN Only",
			allowVPN:         true,
			securityAPIGuard: true,
			description:      i18n.Pick(lang, "Orchestration agent on Edge Gateway VPS for reverse proxy & Let's Encrypt SSL sync via NetBird.", "Agen orkestrasi di VPS Edge Gateway untuk sinkronisasi reverse proxy & SSL Let's Encrypt lewat NetBird."),
			remediationHint:  i18n.Pick(lang, "Ensure wdev-edge-agent is active on Edge VPS and allowed only from NetBird subnet 100.64.0.0/10.", "Pastikan wdev-edge-agent aktif di VPS Edge dan hanya diizinkan dari subnet NetBird 100.64.0.0/10."),
		},
	)

	// Mail Server (Stalwart)
	mailDefs := []struct {
		port     int
		id       string
		name     string
		desc     string
		internal bool
	}{
		{25, "mail-smtp", "SMTP Inbound", i18n.Pick(lang, "Server-to-server inbound email delivery (MX) across internet domains.", "Jalur masuk email server-to-server (MX) antar domain internet."), false},
		{465, "mail-smtps", "SMTPS (Implicit TLS)", i18n.Pick(lang, "TLS-encrypted outgoing mail submission directly from mail clients.", "Pengiriman email terenkripsi TLS langsung oleh aplikasi mail client."), false},
		{587, "mail-submission", "SMTP Submission (STARTTLS)", i18n.Pick(lang, "Authenticated mail client submission (Outlook, Thunderbird, Webmail).", "Otentikasi pengiriman email client (Outlook, Thunderbird, Webmail)."), false},
		{993, "mail-imaps", "IMAPS (Mailbox SSL)", i18n.Pick(lang, "Secure inbox fetching & syncing by email clients and SnappyMail.", "Pengambilan & sinkronisasi folder inbox aman oleh klien email & SnappyMail."), false},
		{8080, "mail-admin", "Stalwart Admin UI", i18n.Pick(lang, "Stalwart internal admin web interface (securely proxied via Nginx).", "Antarmuka web admin internal Stalwart (diproxy aman lewat Nginx)."), true},
	}
	for _, m := range mailDefs {
		expected := "public"
		allowPub := netCtx.Mode != "local"
		hint := i18n.Pick(lang, "Public Mail Service", "Layanan Mail Publik")
		if m.internal {
			expected = "internal"
			allowPub = false
			hint = i18n.Pick(lang, "Internal Only (127.0.0.1 Proxy)", "Internal Saja (127.0.0.1 Proxy)")
		}
		rem := i18n.Pick(lang, "Open port in host firewall / VPS Security Group.", "Buka port pada firewall host / Security Group VPS.")
		if m.port == 25 {
			rem = i18n.Pick(lang, "Port 25 is often blocked by ISPs/cloud providers (AWS/GCP/Alibaba/DO). Use an SMTP Relay (Resend/Brevo) if outbound port 25 is blocked.", "Port 25 sering diblokir ISP/cloud (AWS/GCP/Alibaba/DO). Gunakan SMTP Relay (Resend/Brevo) jika outbound port 25 terblokir.")
		} else if m.internal {
			rem = i18n.Pick(lang, "Accessed internally by Nginx reverse proxy; do not expose publicly.", "Diakses secara internal oleh Nginx reverse proxy; jangan diekspos langsung ke publik.")
		}
		candidates = append(candidates, portCandidate{
			id:              m.id,
			port:            m.port,
			protocol:        "TCP",
			service:         "stalwart",
			name:            m.name,
			category:        "mail",
			categoryLabel:   catMail,
			installed:       stalwartInstalled,
			expectedScope:   expected,
			configHint:      hint,
			allowPublic:     allowPub,
			allowVPN:        !m.internal,
			description:     m.desc,
			remediationHint: rem,
		})
	}

	// Urutkan agar rapi per kategori & port
	catOrder := map[string]int{"system": 1, "web": 2, "database": 3, "dns": 4, "mail": 5}
	sort.SliceStable(candidates, func(i, j int) bool {
		ci, cj := catOrder[candidates[i].category], catOrder[candidates[j].category]
		if ci != cj {
			return ci < cj
		}
		return candidates[i].port < candidates[j].port
	})
	return candidates
}

func evaluatePortCandidate(
	c portCandidate,
	netCtx PortNetworkContext,
	sockets socketBindings,
	fw firewallSnapshot,
	deps portProbeDeps,
	forceRefresh bool,
) PortEntry {
	return evaluatePortCandidateLocale(c, netCtx, sockets, fw, deps, forceRefresh, i18n.DefaultLocale)
}

func evaluatePortCandidateLocale(
	c portCandidate,
	netCtx PortNetworkContext,
	sockets socketBindings,
	fw firewallSnapshot,
	deps portProbeDeps,
	forceRefresh bool,
	lang string,
) PortEntry {
	entry := PortEntry{
		ID:            c.id,
		Port:          c.port,
		Protocol:      c.protocol,
		Service:       c.service,
		Name:          c.name,
		Category:      c.category,
		CategoryLabel: c.categoryLabel,
		Installed:     c.installed,
		ExpectedScope: c.expectedScope,
		ConfigHint:    c.configHint,
		Description:   c.description,
	}

	if !c.installed {
		naDetail := PortScopeDetail{
			Status: "na",
			Color:  "neutral",
			Label:  i18n.Pick(lang, "Not Installed", "Belum Terpasang"),
		}
		entry.Local = naDetail
		entry.VPN = naDetail
		entry.VPS = naDetail
		entry.OverallStatus = "not_installed"
		entry.OverallColor = "neutral"
		entry.OverallLabel = i18n.Pick(lang, "Not Installed", "Belum Terpasang")
		return entry
	}

	// Khusus layanan remote VPS Edge Gateway (Mode Edge: PowerDNS 53, PDNS API 8081, Edge Agent 8082)
	if c.expectedScope == "edge_remote" {
		return evaluateEdgeRemotePort(entry, c, netCtx, deps, lang)
	}

	// Analisis socket binding lokal pada host ini
	tcpBinds := sockets.tcp[c.port]
	udpBinds := sockets.udp[c.port]
	entry.BindAddresses = formatBindAddresses(tcpBinds, udpBinds, strings.Contains(c.protocol, "UDP"))

	hasWildcard, hasLoopback, bindsVPN := analyzeBinds(tcpBinds, netCtx.VPNIP)
	udpListening := len(udpBinds) > 0
	tcpSocketListening := len(tcpBinds) > 0

	// 1. Cek Lokal (127.0.0.1:<port>)
	localDial := deps.dialTCPFn("127.0.0.1", c.port, portDialTimeout)
	localListening := localDial || tcpSocketListening || (strings.Contains(c.protocol, "UDP") && udpListening)

	if localListening {
		detailTxt := "127.0.0.1:" + strconv.Itoa(c.port)
		if len(entry.BindAddresses) > 0 {
			detailTxt = "Bind: " + strings.Join(entry.BindAddresses, ", ")
		}
		if strings.Contains(c.protocol, "UDP") && !udpListening {
			entry.Local = PortScopeDetail{
				Status:  "blocked",
				Color:   "yellow",
				Label:   i18n.Pick(lang, "TCP Active · UDP Down", "TCP Aktif · UDP Mati"),
				Target:  "127.0.0.1:" + strconv.Itoa(c.port),
				Detail:  i18n.Pick(lang, "TCP socket is active but UDP 53 is not listening", "Socket TCP aktif tetapi UDP 53 tidak listen"),
				Checked: true,
			}
		} else {
			entry.Local = PortScopeDetail{
				Status:  "open",
				Color:   "green",
				Label:   i18n.Pick(lang, "Listening (Active)", "Listening (Aktif)"),
				Target:  "127.0.0.1:" + strconv.Itoa(c.port),
				Detail:  detailTxt,
				Checked: true,
			}
		}
	} else if c.edgeTerminatedSSL {
		entry.Local = PortScopeDetail{
			Status:  "internal",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Terminated on Edge VPS", "Terminasi di VPS Edge"),
			Target:  "127.0.0.1:" + strconv.Itoa(c.port),
			Detail:  i18n.Pick(lang, "Edge mode terminates TLS on the VPS and forwards HTTP :80 via NetBird", "Mode Edge menerminasi TLS di VPS lalu meneruskan HTTP :80 lewat NetBird"),
			Checked: true,
		}
	} else if c.sslOptionalIdle {
		entry.Local = PortScopeDetail{
			Status:  "na",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Standby (No SSL Yet)", "Siaga (Belum Ada SSL)"),
			Target:  "127.0.0.1:" + strconv.Itoa(c.port),
			Detail:  i18n.Pick(lang, "Nginx will automatically listen on port 443 once at least 1 SSL site is enabled", "Nginx akan otomatis listen di port 443 setelah minimal 1 situs SSL aktif"),
			Checked: true,
		}
	} else {
		entry.Local = PortScopeDetail{
			Status:  "closed",
			Color:   "red",
			Label:   i18n.Pick(lang, "Down (Not Listening)", "Mati (Tidak Listen)"),
			Target:  "127.0.0.1:" + strconv.Itoa(c.port),
			Detail:  i18n.Pick(lang, "No process is listening on port ", "Tidak ada proses yang listen pada port ") + strconv.Itoa(c.port),
			Checked: true,
		}
	}

	fwDec := evaluateFirewallForPortLocale(fw, c.port, "tcp", netCtx.VPNIP, lang)

	// 2. Cek VPN (NetBird wt0 / 100.64.0.0/10)
	vpnTarget := ""
	if netCtx.VPNIP != "" {
		vpnTarget = net.JoinHostPort(netCtx.VPNIP, strconv.Itoa(c.port))
	}
	switch {
	case !localListening:
		if c.edgeTerminatedSSL {
			entry.VPN = PortScopeDetail{
				Status:  "internal",
				Color:   "neutral",
				Label:   i18n.Pick(lang, "Via HTTP :80 Tunnel", "Lewat Tunnel HTTP :80"),
				Target:  vpnTarget,
				Detail:  i18n.Pick(lang, "HTTPS traffic from the VPS is forwarded to local port 80 over NetBird", "Traffic HTTPS dari VPS diteruskan ke port 80 lokal lewat NetBird"),
				Checked: true,
			}
		} else if c.sslOptionalIdle {
			entry.VPN = PortScopeDetail{
				Status: "na",
				Color:  "neutral",
				Label:  i18n.Pick(lang, "Standby (No SSL Yet)", "Siaga (Belum Ada SSL)"),
				Target: vpnTarget,
			}
		} else {
			entry.VPN = PortScopeDetail{
				Status:  "closed",
				Color:   "red",
				Label:   i18n.Pick(lang, "Service Down", "Layanan Mati"),
				Target:  vpnTarget,
				Checked: true,
			}
		}
	case c.expectedScope == "internal":
		entry.VPN = PortScopeDetail{
			Status:  "internal",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Internal Only", "Internal Saja"),
			Target:  vpnTarget,
			Detail:  i18n.Pick(lang, "Only accessed internally by Nginx / host loopback", "Hanya diakses internal oleh Nginx / loopback host"),
			Checked: true,
		}
	case netCtx.VPNIP == "":
		entry.VPN = PortScopeDetail{
			Status:  "na",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "NetBird Inactive", "NetBird Tidak Aktif"),
			Detail:  i18n.Pick(lang, "NetBird VPN interface (wt0) was not found on this host", "Interface VPN NetBird (wt0) tidak ditemukan pada host ini"),
			Checked: false,
		}
	default:
		vpnDial := deps.dialTCPFn(netCtx.VPNIP, c.port, portDialTimeout)
		boundToVPN := hasWildcard || bindsVPN || (len(tcpBinds) == 0 && vpnDial)
		if !boundToVPN || !vpnDial {
			// Layanan hanya bind 127.0.0.1 / ::1
			if !c.allowVPN && (c.expectedScope == "local" || (hasLoopback && !hasWildcard)) {
				entry.VPN = PortScopeDetail{
					Status:  "internal",
					Color:   "neutral",
					Label:   i18n.Pick(lang, "Local Only (127.0.0.1)", "Hanya Lokal (127.0.0.1)"),
					Target:  vpnTarget,
					Detail:  condStr(c.localBindHint != "", c.localBindHint, i18n.Pick(lang, "Socket binds to localhost only; connections to the NetBird IP will be refused.", "Socket hanya bind pada localhost; koneksi ke IP NetBird akan ditolak (Connection refused).")),
					Checked: true,
				}
			} else {
				entry.VPN = PortScopeDetail{
					Status:  "blocked",
					Color:   "yellow",
					Label:   i18n.Pick(lang, "Refused (Bind 127.0.0.1)", "Ditolak (Bind 127.0.0.1)"),
					Target:  vpnTarget,
					Detail:  fmt.Sprintf(i18n.Pick(lang, "Service is active on localhost, but %s refuses connections because it is not listening on wildcard/wt0.", "Layanan aktif di localhost namun %s menolak koneksi karena tidak listen di wildcard/wt0."), vpnTarget),
					Checked: true,
				}
			}
		} else if fwDec.known && !fwDec.vpnAllowed {
			entry.VPN = PortScopeDetail{
				Status:  "blocked",
				Color:   "yellow",
				Label:   i18n.Pick(lang, "Blocked by Host Firewall", "Terblokir Firewall Host"),
				Target:  vpnTarget,
				Detail:  fmt.Sprintf(i18n.Pick(lang, "Socket is listening on %s, but %s blocks access from the VPN subnet (100.64.0.0/10).", "Socket listen di %s, tetapi %s memblokir akses dari subnet VPN (100.64.0.0/10)."), vpnTarget, strings.ToUpper(fw.info.Engine)),
				Checked: true,
			}
		} else {
			detailMsg := fmt.Sprintf(i18n.Pick(lang, "Port active and accepting connections on %s", "Port aktif dan menerima koneksi dari %s"), vpnTarget)
			if fw.info.Active && !fwDec.known {
				detailMsg += i18n.Pick(lang, " (firewall rules unreadable as non-root)", " (rule firewall tidak terbaca non-root)")
			}
			entry.VPN = PortScopeDetail{
				Status:  "open",
				Color:   "green",
				Label:   i18n.Pick(lang, "Open via VPN", "Terbuka via VPN"),
				Target:  vpnTarget,
				Detail:  detailMsg,
				Checked: true,
			}
		}
	}

	// 3. Cek IP VPS / Publik
	vpsTarget := ""
	if netCtx.VPSPublicIP != "" {
		vpsTarget = net.JoinHostPort(netCtx.VPSPublicIP, strconv.Itoa(c.port))
	}

	switch {
	case c.expectedScope == "internal":
		if netCtx.Mode != "local" && netCtx.VPSPublicIP != "" && !c.allowPublic && hasWildcard && fwDec.known && fwDec.publicAllowed && c.securityAPIGuard {
			if netCtx.Mode == "vps" {
				reach, checked := deps.probeExternalFn(c.port, netCtx.VPSPublicIP, forceRefresh)
				if checked && reach {
					entry.VPS = PortScopeDetail{
						Status:  "blocked",
						Color:   "yellow",
						Label:   i18n.Pick(lang, "Publicly Exposed (Risk)", "Terbuka ke Publik (Berisiko)"),
						Target:  vpsTarget,
						Detail:  i18n.Pick(lang, "This internal/API port should be restricted to localhost or NetBird VPN.", "Port internal/API ini seharusnya dibatasi ke localhost atau NetBird VPN."),
						Checked: true,
					}
					break
				}
			}
		}
		entry.VPS = PortScopeDetail{
			Status:  "internal",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Internal Only", "Internal Saja"),
			Target:  vpsTarget,
			Detail:  i18n.Pick(lang, "Does not need direct exposure to the public internet", "Tidak perlu dibuka langsung ke internet publik"),
			Checked: true,
		}

	case netCtx.Mode == "local":
		entry.VPS = PortScopeDetail{
			Status:  "internal",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Local Mode (Non-Public)", "Mode Lokal (Non-Publik)"),
			Detail:  i18n.Pick(lang, "Server is running in Local Standalone mode without a public VPS IP", "Server berjalan di mode Lokal Standalone tanpa IP VPS publik"),
			Checked: true,
		}

	case !localListening && !c.edgeTerminatedSSL:
		if c.sslOptionalIdle {
			entry.VPS = PortScopeDetail{
				Status: "na",
				Color:  "neutral",
				Label:  i18n.Pick(lang, "Standby (No SSL Yet)", "Siaga (Belum Ada SSL)"),
				Target: vpsTarget,
			}
		} else {
			entry.VPS = PortScopeDetail{
				Status:  "closed",
				Color:   "red",
				Label:   i18n.Pick(lang, "Closed (Service Down)", "Tertutup (Layanan Mati)"),
				Target:  vpsTarget,
				Checked: true,
			}
		}

	case c.expectedScope == "local" || (hasLoopback && !hasWildcard && !bindsVPN):
		entry.VPS = PortScopeDetail{
			Status:  "internal",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Local Only (127.0.0.1)", "Hanya Lokal (127.0.0.1)"),
			Target:  vpsTarget,
			Detail:  i18n.Pick(lang, "Service intentionally binds to loopback only so it is not exposed on the VPS IP", "Layanan sengaja hanya bind ke loopback agar tidak terekspos ke IP VPS"),
			Checked: true,
		}

	case c.preferPrivate && !c.allowPublic:
		// Database mode private_vpn: harusnya dibatasi hanya VPN/LAN dan tertutup di IP publik
		if fwDec.known && !fwDec.publicAllowed {
			entry.VPS = PortScopeDetail{
				Status:  "internal",
				Color:   "neutral",
				Label:   i18n.Pick(lang, "Restricted to VPN/LAN", "Dibatasi Khusus VPN/LAN"),
				Target:  vpsTarget,
				Detail:  i18n.Pick(lang, "Firewall blocks public access and only allows private / NetBird networks", "Firewall memblokir akses publik dan hanya mengizinkan jaringan privat / NetBird"),
				Checked: true,
			}
		} else if netCtx.VPSPublicIP != "" {
			reach, checked := probeVPSExternalForMode(netCtx.Mode, c.port, netCtx.VPSPublicIP, forceRefresh, deps)
			if checked && reach {
				entry.VPS = PortScopeDetail{
					Status:  "blocked",
					Color:   "yellow",
					Label:   i18n.Pick(lang, "Publicly Exposed (Check Firewall)", "Terbuka ke Publik (Periksa Firewall)"),
					Target:  vpsTarget,
					Detail:  fmt.Sprintf(i18n.Pick(lang, "Policy is LAN & VPN, yet %s is still reachable externally. Enable UFW / Security Group.", "Kebijakan adalah LAN & VPN, tetapi %s masih dapat dijangkau dari luar. Aktifkan UFW / Security Group."), vpsTarget),
					Checked: true,
				}
			} else if checked {
				entry.VPS = PortScopeDetail{
					Status:  "internal",
					Color:   "neutral",
					Label:   i18n.Pick(lang, "Closed from Public (Safe)", "Tertutup dari Publik (Aman)"),
					Target:  vpsTarget,
					Detail:  i18n.Pick(lang, "Unreachable from the public internet; access is routed via NetBird VPN", "Tidak terjangkau dari internet publik; akses diarahkan lewat VPN NetBird"),
					Checked: true,
				}
			} else {
				entry.VPS = PortScopeDetail{
					Status:  "unverified",
					Color:   "yellow",
					Label:   i18n.Pick(lang, "Unverified", "Belum Terverifikasi"),
					Target:  vpsTarget,
					Detail:  i18n.Pick(lang, "No active host firewall; ensure the cloud Security Group blocks public access to this port", "Tidak ada firewall host aktif; pastikan Security Group cloud memblokir akses publik port ini"),
					Checked: false,
				}
			}
		} else {
			entry.VPS = PortScopeDetail{
				Status: "internal",
				Color:  "neutral",
				Label:  i18n.Pick(lang, "VPN/LAN Only", "Khusus VPN/LAN"),
			}
		}

	default:
		// Port yang diharapkan terbuka ke IP VPS publik (80, 443, 22, 53, Mail 25/465/587/993, dst)
		if fwDec.known && !fwDec.publicAllowed && netCtx.Mode == "vps" {
			entry.VPS = PortScopeDetail{
				Status:  "blocked",
				Color:   "yellow",
				Label:   fmt.Sprintf(i18n.Pick(lang, "Blocked by Firewall (%s)", "Terblokir Firewall (%s)"), strings.ToUpper(fw.info.Engine)),
				Target:  vpsTarget,
				Detail:  fwDec.note,
				Checked: true,
			}
			break
		}
		if netCtx.VPSPublicIP == "" {
			entry.VPS = PortScopeDetail{
				Status:  "unverified",
				Color:   "yellow",
				Label:   i18n.Pick(lang, "VPS IP Unavailable", "IP VPS Belum Tersedia"),
				Detail:  i18n.Pick(lang, "Public VPS IP address is not configured or could not be detected", "Alamat IP VPS publik belum terkonfigurasi atau tidak terdeteksi"),
				Checked: false,
			}
			break
		}
		reach, checked := probeVPSExternalForMode(netCtx.Mode, c.port, netCtx.VPSPublicIP, forceRefresh, deps)
		if !checked {
			entry.VPS = PortScopeDetail{
				Status:  "unverified",
				Color:   "yellow",
				Label:   i18n.Pick(lang, "Unverified", "Belum Terverifikasi"),
				Target:  vpsTarget,
				Detail:  fmt.Sprintf(i18n.Pick(lang, "External connectivity test to %s failed to complete (timeout/network)", "Uji konektivitas eksternal ke %s gagal diselesaikan (timeout/jaringan)"), vpsTarget),
				Checked: false,
			}
		} else if reach {
			entry.VPS = PortScopeDetail{
				Status:  "open",
				Color:   "green",
				Label:   i18n.Pick(lang, "Fully Open", "Terbuka Penuh"),
				Target:  vpsTarget,
				Detail:  i18n.Pick(lang, "Reachable from the internet via VPS IP ", "Terjangkau dari internet melalui IP VPS ") + vpsTarget,
				Checked: true,
			}
		} else {
			entry.VPS = PortScopeDetail{
				Status:  "blocked",
				Color:   "yellow",
				Label:   i18n.Pick(lang, "Blocked by Firewall / ISP", "Terblokir Firewall / ISP"),
				Target:  vpsTarget,
				Detail:  fmt.Sprintf(i18n.Pick(lang, "Service is running on the server, but %s is blocked from the internet (check VPS Firewall / Security Group / ISP)", "Layanan aktif di server, tetapi %s terblokir dari internet (cek Firewall VPS / Security Group / ISP)"), vpsTarget),
				Checked: true,
			}
		}
	}

	finalizeOverallPortStatus(&entry, c, netCtx, lang)
	return entry
}

func evaluateEdgeRemotePort(
	entry PortEntry,
	c portCandidate,
	netCtx PortNetworkContext,
	deps portProbeDeps,
	lang string,
) PortEntry {
	entry.Local = PortScopeDetail{
		Status:  "na",
		Color:   "neutral",
		Label:   i18n.Pick(lang, "On Edge Gateway VPS", "Di VPS Edge Gateway"),
		Detail:  i18n.Pick(lang, "This service runs on the Edge VPS rather than the local machine", "Layanan ini berjalan di VPS Edge, bukan di mesin lokal"),
		Checked: true,
	}

	if netCtx.VPSVPNIP != "" {
		vpnTarget := net.JoinHostPort(netCtx.VPSVPNIP, strconv.Itoa(c.port))
		if deps.dialTCPFn(netCtx.VPSVPNIP, c.port, portDialTimeout) {
			entry.VPN = PortScopeDetail{
				Status:  "open",
				Color:   "green",
				Label:   i18n.Pick(lang, "Connected via NetBird", "Terhubung via NetBird"),
				Target:  vpnTarget,
				Detail:  i18n.Pick(lang, "Reachable over the NetBird tunnel to Edge VPS", "Terjangkau lewat tunnel NetBird ke VPS Edge"),
				Checked: true,
			}
		} else {
			entry.VPN = PortScopeDetail{
				Status:  "closed",
				Color:   "red",
				Label:   i18n.Pick(lang, "VPN Unreachable", "VPN Tidak Terjangkau"),
				Target:  vpnTarget,
				Detail:  fmt.Sprintf(i18n.Pick(lang, "Failed to connect to %s via NetBird", "Gagal terhubung ke %s lewat NetBird"), vpnTarget),
				Checked: true,
			}
		}
	} else {
		entry.VPN = PortScopeDetail{
			Status:  "na",
			Color:   "neutral",
			Label:   i18n.Pick(lang, "Edge VPS Not Connected", "VPS Edge Belum Terhubung"),
			Detail:  i18n.Pick(lang, "Edge VPS NetBird IP is not configured yet", "IP NetBird VPS Edge belum dikonfigurasi"),
			Checked: false,
		}
	}

	if netCtx.VPSPublicIP != "" {
		vpsTarget := net.JoinHostPort(netCtx.VPSPublicIP, strconv.Itoa(c.port))
		reach := deps.dialTCPFn(netCtx.VPSPublicIP, c.port, portDialTimeout)
		if c.securityAPIGuard {
			if reach {
				entry.VPS = PortScopeDetail{
					Status:  "blocked",
					Color:   "yellow",
					Label:   i18n.Pick(lang, "Publicly Exposed (Risk)", "Terbuka ke Publik (Risiko)"),
					Target:  vpsTarget,
					Detail:  i18n.Pick(lang, "This API/Agent port is exposed to the public internet! Restrict it in VPS firewall to subnet 100.64.0.0/10.", "Port API/Agent ini terbuka ke internet publik! Sebaiknya batasi di firewall VPS khusus subnet 100.64.0.0/10."),
					Checked: true,
				}
			} else {
				entry.VPS = PortScopeDetail{
					Status:  "internal",
					Color:   "neutral",
					Label:   i18n.Pick(lang, "Safely Closed (VPN Only)", "Tertutup Aman (Khusus VPN)"),
					Target:  vpsTarget,
					Detail:  i18n.Pick(lang, "Blocked on the public IP and accessible only through NetBird VPN", "Diblokir dari IP publik dan hanya dapat diakses lewat NetBird VPN"),
					Checked: true,
				}
			}
		} else {
			if reach {
				entry.VPS = PortScopeDetail{
					Status:  "open",
					Color:   "green",
					Label:   i18n.Pick(lang, "Fully Open", "Terbuka Penuh"),
					Target:  vpsTarget,
					Detail:  i18n.Pick(lang, "Edge VPS public DNS responds to external connections", "DNS publik VPS Edge merespons koneksi eksternal"),
					Checked: true,
				}
			} else {
				entry.VPS = PortScopeDetail{
					Status:  "blocked",
					Color:   "yellow",
					Label:   i18n.Pick(lang, "Blocked / Down on VPS", "Terblokir / Mati di VPS"),
					Target:  vpsTarget,
					Detail:  fmt.Sprintf(i18n.Pick(lang, "Cannot reach %s externally", "Tidak dapat terhubung ke %s dari luar"), vpsTarget),
					Checked: true,
				}
			}
		}
	} else {
		entry.VPS = PortScopeDetail{
			Status: "na",
			Color:  "neutral",
			Label:  i18n.Pick(lang, "No VPS Public IP", "IP Publik VPS Belum Ada"),
		}
	}

	switch {
	case entry.VPN.Color == "red":
		entry.OverallStatus = "closed"
		entry.OverallColor = "red"
		entry.OverallLabel = i18n.Pick(lang, "Edge VPN Disconnected", "VPN Edge Terputus")
		entry.Recommendation = c.remediationHint
	case entry.VPS.Color == "yellow":
		entry.OverallStatus = "blocked"
		entry.OverallColor = "yellow"
		entry.OverallLabel = entry.VPS.Label
		entry.Recommendation = c.remediationHint
	case entry.VPN.Color == "green" && (entry.VPS.Color == "green" || (c.securityAPIGuard && entry.VPS.Color == "neutral")):
		entry.OverallStatus = "open"
		entry.OverallColor = "green"
		if c.securityAPIGuard {
			entry.OverallLabel = i18n.Pick(lang, "Securely Connected (VPN)", "Terhubung Aman (VPN)")
		} else {
			entry.OverallLabel = i18n.Pick(lang, "Fully Open", "Terbuka Penuh")
		}
	default:
		entry.OverallStatus = "internal"
		entry.OverallColor = "neutral"
		entry.OverallLabel = i18n.Pick(lang, "Awaiting Edge Connection", "Menunggu Koneksi Edge")
	}
	return entry
}

func finalizeOverallPortStatus(entry *PortEntry, c portCandidate, netCtx PortNetworkContext, lang string) {
	hasYellow := entry.Local.Color == "yellow" || entry.VPN.Color == "yellow" || entry.VPS.Color == "yellow"
	hasRed := entry.Local.Color == "red" || (c.edgeTerminatedSSL && entry.VPS.Color == "red")

	switch {
	case hasRed:
		entry.OverallStatus = "closed"
		entry.OverallColor = "red"
		entry.OverallLabel = i18n.Pick(lang, "Down / Closed", "Mati / Tertutup")
		entry.Recommendation = c.remediationHint
	case hasYellow:
		entry.OverallStatus = "blocked"
		entry.OverallColor = "yellow"
		switch {
		case entry.VPN.Color == "yellow":
			entry.OverallLabel = i18n.Pick(lang, "Restricted · VPN Issue", "Terbatas · VPN Terkendala")
			if entry.VPN.Status == "blocked" && strings.Contains(entry.VPN.Label, "Bind") && c.localBindHint != "" {
				entry.Recommendation = c.remediationHint
			} else {
				entry.Recommendation = entry.VPN.Detail
			}
		case entry.VPS.Status == "unverified":
			entry.OverallLabel = i18n.Pick(lang, "Active · Unverified", "Aktif · Belum Terverifikasi")
			entry.Recommendation = c.remediationHint
		default:
			entry.OverallLabel = i18n.Pick(lang, "Open Locally · Publicly Blocked", "Terbuka Lokal · Terblokir Publik")
			entry.Recommendation = c.remediationHint
		}
	case c.expectedScope == "internal":
		entry.OverallStatus = "internal"
		entry.OverallColor = "neutral"
		entry.OverallLabel = i18n.Pick(lang, "Internal Only", "Internal Saja")
	case c.sslOptionalIdle && entry.Local.Status == "na" && !c.edgeTerminatedSSL:
		entry.OverallStatus = "internal"
		entry.OverallColor = "neutral"
		entry.OverallLabel = i18n.Pick(lang, "Standby (No SSL Yet)", "Siaga (Belum Ada SSL)")
	case c.edgeTerminatedSSL && entry.VPS.Color == "green":
		entry.OverallStatus = "open"
		entry.OverallColor = "green"
		entry.OverallLabel = i18n.Pick(lang, "Open via Edge VPS", "Terbuka via VPS Edge")
	case netCtx.Mode != "local" && entry.VPS.Color == "green" && entry.Local.Color == "green":
		entry.OverallStatus = "open"
		entry.OverallColor = "green"
		entry.OverallLabel = i18n.Pick(lang, "Fully Open", "Terbuka Penuh")
	case entry.Local.Color == "green" && entry.VPN.Color == "green":
		entry.OverallStatus = "open"
		entry.OverallColor = "green"
		entry.OverallLabel = i18n.Pick(lang, "Open (Local & VPN)", "Terbuka (Lokal & VPN)")
	case entry.Local.Color == "green":
		entry.OverallStatus = "open"
		entry.OverallColor = "green"
		entry.OverallLabel = i18n.Pick(lang, "Active (Local Only)", "Aktif (Lokal Saja)")
		if c.localBindHint != "" && c.expectedScope == "local" {
			entry.Recommendation = c.localBindHint
		}
	default:
		entry.OverallStatus = "internal"
		entry.OverallColor = "neutral"
		entry.OverallLabel = i18n.Pick(lang, "Internal Only", "Internal Saja")
	}
}

func probeVPSExternalForMode(mode string, port int, publicIP string, force bool, deps portProbeDeps) (reachable bool, checked bool) {
	if publicIP == "" {
		return false, false
	}
	if mode == "edge" {
		ok := deps.dialTCPFn(publicIP, port, 1500*time.Millisecond)
		return ok, true
	}
	return deps.probeExternalFn(port, publicIP, force)
}

func probePortTCP(host string, port int, timeout time.Duration) bool {
	if strings.TrimSpace(host) == "" || port <= 0 || port > 65535 {
		return false
	}
	if timeout <= 0 {
		timeout = portDialTimeout
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func probeExternalPortCached(port int, wantIP string, force bool) (reachable bool, checked bool) {
	if port <= 0 || port > 65535 || !pdns.IsPublicIPv4String(wantIP) {
		return false, false
	}
	cacheKey := fmt.Sprintf("%s:%d", wantIP, port)
	if !force {
		globalExtPortCacheMu.Lock()
		cached, ok := globalExtPortCache[cacheKey]
		globalExtPortCacheMu.Unlock()
		if ok && time.Since(cached.checkedAt) < externalPortCacheTTL {
			return cached.reachable, cached.checked
		}
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: externalHTTPTimeout}).DialContext(ctx, "tcp4", addr)
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{Timeout: externalHTTPTimeout, Transport: transport}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://ifconfig.co/port/%d", port), nil)
	if err != nil {
		return false, false
	}
	req.Header.Set("User-Agent", "wdev-port-check/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var parsed struct {
		IP        string `json:"ip"`
		Port      int    `json:"port"`
		Reachable bool   `json:"reachable"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&parsed); err != nil {
		return false, false
	}
	if wantIP != "" && strings.TrimSpace(parsed.IP) != "" && strings.TrimSpace(parsed.IP) != wantIP {
		return false, false
	}
	checked = true
	reachable = parsed.Reachable

	globalExtPortCacheMu.Lock()
	globalExtPortCache[cacheKey] = portExternalProbeCacheEntry{
		reachable: reachable,
		checked:   checked,
		checkedAt: time.Now(),
	}
	globalExtPortCacheMu.Unlock()
	return reachable, checked
}

func readSystemSocketBindings() socketBindings {
	sb := newSocketBindings()
	for _, spec := range []struct {
		path   string
		isIPv6 bool
		udp    bool
		state  string
	}{
		{"/proc/net/tcp", false, false, "0A"},
		{"/proc/net/tcp6", true, false, "0A"},
		{"/proc/net/udp", false, true, "07"},
		{"/proc/net/udp6", true, true, "07"},
	} {
		data, err := os.ReadFile(spec.path)
		if err != nil {
			continue
		}
		parsed := parseProcNetSockets(string(data), spec.isIPv6, spec.state)
		target := sb.tcp
		if spec.udp {
			target = sb.udp
		}
		for port, ips := range parsed {
			target[port] = appendUniqueIPs(target[port], ips...)
		}
	}
	return sb
}

func parseProcNetSockets(content string, isIPv6 bool, wantStates ...string) map[int][]net.IP {
	res := make(map[int][]net.IP)
	want := make(map[string]bool, len(wantStates))
	for _, st := range wantStates {
		want[strings.ToUpper(st)] = true
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "sl") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if len(want) > 0 && !want[strings.ToUpper(fields[3])] {
			continue
		}
		hostHex, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		portVal, err := strconv.ParseInt(portHex, 16, 32)
		if err != nil || portVal <= 0 || portVal > 65535 {
			continue
		}
		ip := decodeProcHexIP(hostHex, isIPv6)
		if ip == nil {
			continue
		}
		port := int(portVal)
		res[port] = appendUniqueIPs(res[port], ip)
	}
	return res
}

func decodeProcHexIP(hexStr string, isIPv6 bool) net.IP {
	raw, err := hex.DecodeString(strings.TrimSpace(hexStr))
	if err != nil {
		return nil
	}
	if !isIPv6 && len(raw) == 4 {
		// Linux /proc/net/{tcp,udp} little-endian 32-bit integer -> reverse byte order
		return net.IPv4(raw[3], raw[2], raw[1], raw[0])
	}
	if isIPv6 && len(raw) == 16 {
		// 4 word 32-bit little-endian -> balik setiap blok 4 byte
		ip := make(net.IP, 16)
		for w := 0; w < 16; w += 4 {
			ip[w] = raw[w+3]
			ip[w+1] = raw[w+2]
			ip[w+2] = raw[w+1]
			ip[w+3] = raw[w]
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4
		}
		return ip
	}
	return nil
}

func appendUniqueIPs(dst []net.IP, add ...net.IP) []net.IP {
	for _, ip := range add {
		if ip == nil {
			continue
		}
		exists := false
		for _, cur := range dst {
			if cur.Equal(ip) {
				exists = true
				break
			}
		}
		if !exists {
			dst = append(dst, ip)
		}
	}
	return dst
}

func analyzeBinds(binds []net.IP, vpnIP string) (hasWildcard, hasLoopback, bindsVPN bool) {
	parsedVPN := net.ParseIP(strings.TrimSpace(vpnIP))
	for _, ip := range binds {
		if ip.IsUnspecified() {
			hasWildcard = true
			bindsVPN = true
		} else if ip.IsLoopback() {
			hasLoopback = true
		}
		if parsedVPN != nil && ip.Equal(parsedVPN) {
			bindsVPN = true
		}
	}
	return
}

func formatBindAddresses(tcpBinds, udpBinds []net.IP, includeUDP bool) []string {
	var all []net.IP
	all = appendUniqueIPs(all, tcpBinds...)
	if includeUDP {
		all = appendUniqueIPs(all, udpBinds...)
	}
	if len(all) == 0 {
		return nil
	}
	out := make([]string, 0, len(all))
	for _, ip := range all {
		if ip.Equal(net.IPv6zero) {
			out = append(out, "[::]")
		} else if ip.Equal(net.IPv6loopback) {
			out = append(out, "[::1]")
		} else {
			out = append(out, ip.String())
		}
	}
	sort.Strings(out)
	return out
}

func detectHostFirewall(ctx context.Context) firewallSnapshot {
	return detectHostFirewallLocale(ctx, i18n.DefaultLocale)
}

func detectHostFirewallLocale(ctx context.Context, lang string) firewallSnapshot {
	runFW := func(bin string, args ...string) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
		defer cancel()
		out, err := exec.CommandContext(cctx, bin, args...).CombinedOutput()
		if err != nil && os.Geteuid() != 0 {
			if sudoPath, sErr := exec.LookPath("sudo"); sErr == nil {
				sArgs := append([]string{"-n", bin}, args...)
				if out2, err2 := exec.CommandContext(cctx, sudoPath, sArgs...).CombinedOutput(); err2 == nil {
					return string(out2), nil
				}
			}
		}
		return string(out), err
	}

	if ufwBin, err := exec.LookPath("ufw"); err == nil {
		out, err := runFW(ufwBin, "status", "verbose")
		snap := parseUFWStatusLocale(out, err, lang)
		if snap.info.Engine == "ufw" {
			return snap
		}
	}

	if fwdBin, err := exec.LookPath("firewall-cmd"); err == nil {
		stateOut, sErr := runFW(fwdBin, "--state")
		if sErr == nil && strings.EqualFold(strings.TrimSpace(stateOut), "running") {
			listOut, lErr := runFW(fwdBin, "--list-all")
			return parseFirewalldZoneLocale(listOut, lErr, lang)
		}
		if sErr != nil && os.Geteuid() != 0 {
			return firewallSnapshot{
				info: PortFirewallInfo{
					Engine:       "firewalld",
					Active:       false,
					RulesChecked: false,
					Summary:      i18n.Pick(lang, "firewalld installed (root access required to read status)", "firewalld terpasang (butuh hak root untuk membaca status)"),
					RulesError:   strings.TrimSpace(stateOut),
				},
			}
		}
	}

	return firewallSnapshot{
		info: PortFirewallInfo{
			Engine:       "none",
			Active:       false,
			RulesChecked: true,
			Summary:      i18n.Pick(lang, "No active host firewall (UFW/firewalld)", "Tidak ada firewall host (UFW/firewalld) yang aktif"),
		},
	}
}

func parseUFWStatus(output string, cmdErr error) firewallSnapshot {
	return parseUFWStatusLocale(output, cmdErr, i18n.DefaultLocale)
}

func parseUFWStatusLocale(output string, cmdErr error, lang string) firewallSnapshot {
	low := strings.ToLower(output)
	if cmdErr != nil && (strings.Contains(low, "need to be root") || strings.Contains(low, "permission denied")) {
		return firewallSnapshot{
			info: PortFirewallInfo{
				Engine:       "ufw",
				Active:       false,
				RulesChecked: false,
				Summary:      i18n.Pick(lang, "UFW installed, but root access is required to inspect rules", "UFW terpasang, tetapi status rule membutuhkan hak akses root"),
				RulesError:   "permission_denied",
			},
		}
	}
	if strings.Contains(low, "status: inactive") {
		return firewallSnapshot{
			info: PortFirewallInfo{
				Engine:       "ufw",
				Active:       false,
				RulesChecked: true,
				Summary:      i18n.Pick(lang, "UFW installed (Inactive)", "UFW terpasang (Nonaktif)"),
			},
		}
	}
	if !strings.Contains(low, "status: active") {
		return firewallSnapshot{info: PortFirewallInfo{Engine: "none", RulesChecked: true}}
	}

	defaultIncoming := "deny"
	defaultDeny := true
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "default:") {
			for _, part := range strings.Split(strings.TrimPrefix(strings.ToLower(trimmed), "default:"), ",") {
				p := strings.TrimSpace(part)
				if strings.Contains(p, "(incoming)") {
					policy := strings.Fields(p)[0]
					defaultIncoming = policy
					defaultDeny = policy == "deny" || policy == "reject" || policy == "disabled"
				}
			}
		}
	}

	var rules []fwRule
	inTable := false
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "--") {
			inTable = true
			continue
		}
		if !inTable || line == "" {
			continue
		}
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		line = strings.ReplaceAll(line, "(v6)", "")
		parsedRules := parseUFWRuleLine(line)
		rules = append(rules, parsedRules...)
	}

	return firewallSnapshot{
		info: PortFirewallInfo{
			Engine:          "ufw",
			Active:          true,
			DefaultIncoming: defaultIncoming,
			RulesChecked:    true,
			Summary:         fmt.Sprintf(i18n.Pick(lang, "UFW active (default incoming: %s)", "UFW aktif (default incoming: %s)"), defaultIncoming),
		},
		defaultDeny: defaultDeny,
		rules:       rules,
	}
}

var ufwActionSplitRe = regexp.MustCompile(`^(.*?)\s+(ALLOW IN|ALLOW|DENY IN|DENY|REJECT IN|REJECT|LIMIT IN|LIMIT)\s+(.+)$`)

func parseUFWRuleLine(line string) []fwRule {
	m := ufwActionSplitRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) != 4 {
		return nil
	}
	toPart := strings.TrimSpace(m[1])
	actionPart := strings.ToUpper(strings.TrimSpace(m[2]))
	fromPart := strings.TrimSpace(m[3])

	allow := strings.HasPrefix(actionPart, "ALLOW") || strings.HasPrefix(actionPart, "LIMIT")
	publicFrom := false
	var cidrs []*net.IPNet
	for _, f := range strings.Fields(fromPart) {
		fl := strings.ToLower(f)
		if fl == "anywhere" || fl == "0.0.0.0/0" || fl == "::/0" {
			publicFrom = true
			continue
		}
		if cidr := parseSingleHostOrCIDR(f); cidr != nil {
			cidrs = append(cidrs, cidr)
		}
	}

	portSpecs := expandUFWPortToken(toPart)
	rules := make([]fwRule, 0, len(portSpecs))
	for _, spec := range portSpecs {
		rules = append(rules, fwRule{
			proto:      spec.proto,
			portStart:  spec.start,
			portEnd:    spec.end,
			allow:      allow,
			publicFrom: publicFrom,
			cidrs:      cidrs,
		})
	}
	return rules
}

type fwPortSpec struct {
	proto string
	start int
	end   int
}

func expandUFWPortToken(token string) []fwPortSpec {
	t := strings.TrimSpace(strings.ToLower(token))
	switch t {
	case "openssh", "ssh":
		return []fwPortSpec{{proto: "tcp", start: 22, end: 22}}
	case "nginx http", "www", "http":
		return []fwPortSpec{{proto: "tcp", start: 80, end: 80}}
	case "nginx https", "https":
		return []fwPortSpec{{proto: "tcp", start: 443, end: 443}}
	case "nginx full":
		return []fwPortSpec{{proto: "tcp", start: 80, end: 80}, {proto: "tcp", start: 443, end: 443}}
	case "dns":
		return []fwPortSpec{{proto: "any", start: 53, end: 53}}
	case "smtp":
		return []fwPortSpec{{proto: "tcp", start: 25, end: 25}}
	}

	proto := "any"
	portPart := t
	if p, pr, ok := strings.Cut(t, "/"); ok {
		portPart = strings.TrimSpace(p)
		proto = strings.TrimSpace(pr)
	}
	if portPart == "anywhere" || portPart == "any" {
		return []fwPortSpec{{proto: proto, start: 1, end: 65535}}
	}
	var specs []fwPortSpec
	for _, seg := range strings.Split(portPart, ",") {
		seg = strings.TrimSpace(seg)
		if a, b, ok := strings.Cut(seg, ":"); ok {
			p1, err1 := strconv.Atoi(strings.TrimSpace(a))
			p2, err2 := strconv.Atoi(strings.TrimSpace(b))
			if err1 == nil && err2 == nil && p1 > 0 && p2 >= p1 && p2 <= 65535 {
				specs = append(specs, fwPortSpec{proto: proto, start: p1, end: p2})
			}
			continue
		}
		if p, err := strconv.Atoi(seg); err == nil && p > 0 && p <= 65535 {
			specs = append(specs, fwPortSpec{proto: proto, start: p, end: p})
		}
	}
	return specs
}

var (
	fwdRichAddrRe  = regexp.MustCompile(`source\s+address="([^"]+)"`)
	fwdRichProtoRe = regexp.MustCompile(`protocol="([^"]+)"`)
	fwdRichPortRe  = regexp.MustCompile(`port="(\d+)(?:-(\d+))?"`)
)

func parseFirewalldZone(output string, cmdErr error) firewallSnapshot {
	return parseFirewalldZoneLocale(output, cmdErr, i18n.DefaultLocale)
}

func parseFirewalldZoneLocale(output string, cmdErr error, lang string) firewallSnapshot {
	if cmdErr != nil {
		return firewallSnapshot{
			info: PortFirewallInfo{
				Engine:       "firewalld",
				Active:       true,
				RulesChecked: false,
				Summary:      i18n.Pick(lang, "firewalld active (failed to read rules)", "firewalld aktif (rule gagal dibaca)"),
				RulesError:   strings.TrimSpace(cmdErr.Error()),
			},
			defaultDeny: true,
		}
	}
	defaultIncoming := "deny"
	defaultDeny := true
	var rules []fwRule
	inRich := false

	svcPorts := map[string][]fwPortSpec{
		"ssh":             {{proto: "tcp", start: 22, end: 22}},
		"http":            {{proto: "tcp", start: 80, end: 80}},
		"https":           {{proto: "tcp", start: 443, end: 443}},
		"dns":             {{proto: "any", start: 53, end: 53}},
		"smtp":            {{proto: "tcp", start: 25, end: 25}},
		"smtps":           {{proto: "tcp", start: 465, end: 465}},
		"smtp-submission": {{proto: "tcp", start: 587, end: 587}},
		"imaps":           {{proto: "tcp", start: 993, end: 993}},
		"postgresql":      {{proto: "tcp", start: 5432, end: 5432}},
		"redis":           {{proto: "tcp", start: 6379, end: 6379}},
	}

	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "target:") {
			inRich = false
			target := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "target:")))
			if target == "accept" {
				defaultIncoming = "accept"
				defaultDeny = false
			} else {
				defaultIncoming = target
				defaultDeny = true
			}
			continue
		}
		if strings.HasPrefix(line, "services:") {
			inRich = false
			for _, svc := range strings.Fields(strings.TrimPrefix(line, "services:")) {
				for _, spec := range svcPorts[strings.ToLower(svc)] {
					rules = append(rules, fwRule{
						proto: spec.proto, portStart: spec.start, portEnd: spec.end,
						allow: true, publicFrom: true,
					})
				}
			}
			continue
		}
		if strings.HasPrefix(line, "ports:") {
			inRich = false
			for _, tok := range strings.Fields(strings.TrimPrefix(line, "ports:")) {
				tokNormalized := strings.ReplaceAll(tok, "-", ":")
				for _, spec := range expandUFWPortToken(tokNormalized) {
					rules = append(rules, fwRule{
						proto: spec.proto, portStart: spec.start, portEnd: spec.end,
						allow: true, publicFrom: true,
					})
				}
			}
			continue
		}
		if strings.HasPrefix(line, "rich rules:") {
			inRich = true
			continue
		}
		if inRich && strings.HasPrefix(line, "rule ") {
			allow := strings.Contains(line, " accept")
			deny := strings.Contains(line, " drop") || strings.Contains(line, " reject")
			if !allow && !deny {
				continue
			}
			pm := fwdRichPortRe.FindStringSubmatch(line)
			if len(pm) < 2 {
				continue
			}
			p1, _ := strconv.Atoi(pm[1])
			p2 := p1
			if len(pm) >= 3 && pm[2] != "" {
				if v, err := strconv.Atoi(pm[2]); err == nil {
					p2 = v
				}
			}
			proto := "any"
			if pr := fwdRichProtoRe.FindStringSubmatch(line); len(pr) == 2 {
				proto = strings.ToLower(pr[1])
			}
			var cidrs []*net.IPNet
			pub := true
			if am := fwdRichAddrRe.FindStringSubmatch(line); len(am) == 2 {
				if cidr := parseSingleHostOrCIDR(am[1]); cidr != nil {
					cidrs = append(cidrs, cidr)
					pub = false
				}
			}
			rules = append(rules, fwRule{
				proto: proto, portStart: p1, portEnd: p2,
				allow: allow, publicFrom: pub, cidrs: cidrs,
			})
		} else if strings.Contains(line, ":") {
			inRich = false
		}
	}

	return firewallSnapshot{
		info: PortFirewallInfo{
			Engine:          "firewalld",
			Active:          true,
			DefaultIncoming: defaultIncoming,
			RulesChecked:    true,
			Summary:         fmt.Sprintf(i18n.Pick(lang, "firewalld active (target: %s)", "firewalld aktif (target: %s)"), defaultIncoming),
		},
		defaultDeny: defaultDeny,
		rules:       rules,
	}
}

func evaluateFirewallForPort(fw firewallSnapshot, port int, proto, vpnIP string) fwDecision {
	return evaluateFirewallForPortLocale(fw, port, proto, vpnIP, i18n.DefaultLocale)
}

func evaluateFirewallForPortLocale(fw firewallSnapshot, port int, proto, vpnIP, lang string) fwDecision {
	if !fw.info.RulesChecked {
		return fwDecision{known: false, publicAllowed: true, vpnAllowed: true}
	}
	if !fw.info.Active {
		return fwDecision{known: true, publicAllowed: true, vpnAllowed: true}
	}

	decidedPub, pubAllowed := false, !fw.defaultDeny
	decidedVPN, vpnAllowed := false, !fw.defaultDeny
	parsedVPN := net.ParseIP(strings.TrimSpace(vpnIP))

	for _, r := range fw.rules {
		if r.proto != "any" && !strings.EqualFold(r.proto, proto) {
			continue
		}
		if port < r.portStart || port > r.portEnd {
			continue
		}
		if r.publicFrom {
			if !decidedPub {
				pubAllowed = r.allow
				decidedPub = true
			}
			if !decidedVPN {
				vpnAllowed = r.allow
				decidedVPN = true
			}
			continue
		}
		if !decidedVPN && ruleMatchesVPNCIDR(r.cidrs, parsedVPN) {
			vpnAllowed = r.allow
			decidedVPN = true
		}
	}

	note := ""
	if !pubAllowed {
		note = fmt.Sprintf(i18n.Pick(lang,
			"No public allow rule for port %d/%s in %s (default incoming: %s)",
			"Tidak ada rule izin publik untuk port %d/%s pada %s (default incoming: %s)",
		), port, proto, strings.ToUpper(fw.info.Engine), fw.info.DefaultIncoming)
	}
	return fwDecision{
		known:         true,
		publicAllowed: pubAllowed,
		vpnAllowed:    vpnAllowed,
		note:          note,
	}
}

func ruleMatchesVPNCIDR(cidrs []*net.IPNet, vpnIP net.IP) bool {
	for _, c := range cidrs {
		if vpnIP != nil && c.Contains(vpnIP) {
			return true
		}
		// Match CGNAT NetBird range (100.64.0.0/10) jika IP NetBird belum terdeteksi
		if c.String() == "100.64.0.0/10" {
			return true
		}
	}
	return false
}

func parseSingleHostOrCIDR(raw string) *net.IPNet {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	if _, ipnet, err := net.ParseCIDR(s); err == nil {
		return ipnet
	}
	if ip := net.ParseIP(s); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
	}
	return nil
}

func parseCIDRList(raw []string) []*net.IPNet {
	var out []*net.IPNet
	for _, item := range raw {
		if c := parseSingleHostOrCIDR(item); c != nil {
			out = append(out, c)
		}
	}
	return out
}

func detectSSHServerStatus() (int, bool) {
	var paths []string
	if matches, err := filepath.Glob("/etc/ssh/sshd_config.d/*.conf"); err == nil {
		sort.Strings(matches)
		paths = append(paths, matches...)
	}
	paths = append(paths, "/etc/ssh/sshd_config")
	contents := make([]string, 0, len(paths))
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			contents = append(contents, string(data))
		}
	}
	port := parseSSHPortFromConfigs(contents...)
	installed := portFileExists("/usr/sbin/sshd") ||
		portFileExists("/etc/ssh/sshd_config") ||
		monitor.IsSystemdServiceActive("ssh") ||
		monitor.IsSystemdServiceActive("sshd")
	return port, installed
}

func parseSSHPortFromConfigs(contents ...string) int {
	port := 22
	foundFirst := false
	for _, raw := range contents {
		for _, line := range strings.Split(raw, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(trimmed), "match ") {
				break
			}
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && strings.EqualFold(fields[0], "port") {
				if p, err := strconv.Atoi(fields[1]); err == nil && p > 0 && p <= 65535 && !foundFirst {
					port = p
					foundFirst = true
				}
			}
		}
	}
	return port
}

func (s *Server) listManagedSites() []store.SiteRecord {
	if s.siteMgr != nil {
		if sites, err := s.siteMgr.ListSites(); err == nil {
			return sites
		}
	}
	if s.store != nil {
		if sites, err := s.store.ListSites(); err == nil {
			return sites
		}
	}
	return nil
}

func (s *Server) hasAnySSLSiteConfigured() bool {
	for _, st := range s.listManagedSites() {
		if st.SSLEnabled {
			return true
		}
	}
	if s.cfg != nil && s.cfg.NginxSitesEnable != "" {
		if entries, err := os.ReadDir(s.cfg.NginxSitesEnable); err == nil {
			for _, e := range entries {
				data, err := os.ReadFile(filepath.Join(s.cfg.NginxSitesEnable, e.Name()))
				if err != nil {
					continue
				}
				if strings.Contains(string(data), "443 ssl") || strings.Contains(string(data), "443 ") {
					return true
				}
			}
		}
	}
	return false
}

func detectLocalCGNATFallback() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
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
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			for _, cidr := range privateVPNCIDRs {
				if cidr.String() == "100.64.0.0/10" && cidr.Contains(ip4) {
					return ip4.String()
				}
			}
		}
	}
	return ""
}

func isLoopbackAddrStr(addr string) bool {
	s := strings.ToLower(strings.TrimSpace(addr))
	return s == "127.0.0.1" || s == "localhost" || s == "::1" || s == "[::1]"
}

func isLoopbackListenList(listenAddrs string) bool {
	tokens := strings.FieldsFunc(listenAddrs, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(tokens) == 0 {
		return true
	}
	for _, tok := range tokens {
		if !isLoopbackAddrStr(tok) {
			return false
		}
	}
	return true
}

func isLoopbackBindList(bind string) bool {
	tokens := strings.Fields(strings.TrimSpace(bind))
	if len(tokens) == 0 {
		return true
	}
	for _, tok := range tokens {
		clean := strings.TrimPrefix(tok, "-")
		if !isLoopbackAddrStr(clean) {
			return false
		}
	}
	return true
}

func isLoopbackOrSameHost(targetIP, localVPNIP string) bool {
	t := strings.TrimSpace(targetIP)
	return isLoopbackAddrStr(t) || (localVPNIP != "" && t == localVPNIP)
}

func portFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func condStr(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// Keep unused store import check avoided: reference store.SiteRecord cleanly via s.siteMgr.ListSites()
var _ = store.SiteRecord{}
