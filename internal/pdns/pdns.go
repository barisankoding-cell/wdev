package pdns

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/config"
	"wdev/internal/edge"
	"wdev/internal/site"
	"wdev/internal/store"
)

type Manager struct {
	cfg     *config.Config
	store   *store.Store
	siteMgr *site.Manager
}

// AddZoneResult merangkum hasil pembuatan zone PowerDNS dan vhost static HTML bawaan.
type AddZoneResult struct {
	Zone         string   `json:"zone"`
	Nameservers  []string `json:"nameservers"`
	ZoneIP       string   `json:"zone_ip"`
	VHostStatus  string   `json:"vhost_status"` // "created", "exists", "skipped", "error"
	VHostCreated bool     `json:"vhost_created"`
	VHostRoot    string   `json:"vhost_root,omitempty"`
	VHostURL     string   `json:"vhost_url,omitempty"`
	EdgeExposed  bool     `json:"edge_exposed,omitempty"`
	VHostMessage string   `json:"vhost_message,omitempty"`
}

type ConnectOptions struct {
	VPSNetbirdIP string `json:"vps_netbird_ip"`
	VPSPublicIP  string `json:"vps_public_ip"`
	APIKey       string `json:"api_key"`
	PDNSPort     string `json:"pdns_port"`
	AgentPort    string `json:"agent_port"`
}

type DeployOptions struct {
	VPSHost    string `json:"host"`
	SSHPort    int    `json:"port"`
	SSHUser    string `json:"user"`
	AuthMethod string `json:"auth_method"` // "password" or "key"
	Password   string `json:"password"`
	KeyPath    string `json:"key_path"`
}

type EdgeConfig struct {
	Configured     bool   `json:"configured"`
	VPSNetbirdIP   string `json:"vps_netbird_ip"`
	VPSPublicIP    string `json:"vps_public_ip"`
	APIKey         string `json:"api_key"`
	HasAPIKey      bool   `json:"has_api_key"`
	PDNSPort       string `json:"pdns_port"`
	AgentPort      string `json:"agent_port"`
	LocalNetbirdIP string `json:"local_netbird_ip"`
	Reachable      bool   `json:"reachable"`
	Mode           string `json:"mode"`
	ModeIsSet      bool   `json:"mode_is_set"`
	HasLocalPDNS   bool   `json:"has_local_pdns"`
}

type ProxyExposure struct {
	Domain     string `json:"domain"`
	Enabled    bool   `json:"enabled"`
	ConfigFile string `json:"config_file"`
	Target     string `json:"target,omitempty"`
	LocalSite  string `json:"local_site,omitempty"`
	SSL        string `json:"ssl,omitempty"`
}

type ZoneInfo struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Serial int64   `json:"serial"`
	URL    string  `json:"url"`
	RRsets []RRSet `json:"rrsets,omitempty"`
}

type RRSet struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	TTL        int      `json:"ttl"`
	ChangeType string   `json:"changetype,omitempty"`
	Records    []Record `json:"records"`
}

type Record struct {
	Content  string `json:"content"`
	Disabled bool   `json:"disabled"`
}

type ExposeOptions struct {
	Mode         string `json:"mode"`          // "site" or "port"
	LocalDomain  string `json:"local_domain"`  // e.g. "appsone.test"
	PublicDomain string `json:"public_domain"` // e.g. "appsone.erposoft.my.id"
	TargetPort   int    `json:"target_port"`   // e.g. 3000
	UseSSL       bool   `json:"use_ssl"`
	WebSocket    bool   `json:"websocket"`
}

func New(cfg *config.Config, s *store.Store) *Manager {
	return &Manager{cfg: cfg, store: s}
}

// SetSiteManager menyetel site.Manager agar penambahan domain PowerDNS otomatis menyiapkan vhost static HTML.
func (m *Manager) SetSiteManager(sm *site.Manager) {
	m.siteMgr = sm
}

// NormalizeMode menormalkan nama mode operasional server ("local", "edge", "vps").
func NormalizeMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "local", "lokal", "standalone":
		return "local", true
	case "edge", "hybrid":
		return "edge", true
	case "vps", "aapanel":
		return "vps", true
	default:
		return "", false
	}
}

func isVirtualOrTunnelIface(name string) bool {
	low := strings.ToLower(name)
	return low == "lo" ||
		low == "wt0" ||
		strings.HasPrefix(low, "wg") ||
		strings.HasPrefix(low, "tun") ||
		strings.HasPrefix(low, "tap") ||
		strings.HasPrefix(low, "docker") ||
		strings.HasPrefix(low, "br-") ||
		strings.HasPrefix(low, "veth") ||
		strings.HasPrefix(low, "virbr") ||
		strings.HasPrefix(low, "cni") ||
		strings.HasPrefix(low, "flannel")
}

func isCGNATIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

// DetectPublicInterfaceIP mengembalikan alamat IPv4 publik pertama yang ditemukan
// langsung pada interface fisik host.
func DetectPublicInterfaceIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || isVirtualOrTunnelIface(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			if isPublicIPv4(ip4) {
				return ip4.String()
			}
		}
	}
	return ""
}

func isPublicIPv4(ip net.IP) bool {
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

// IsPublicIPv4String memeriksa apakah representasi string IPv4 adalah alamat publik yang valid.
func IsPublicIPv4String(ipStr string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ipStr))
	if parsed == nil {
		return false
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return false
	}
	return isPublicIPv4(ip4)
}

// HasPublicInterfaceIP memeriksa apakah mesin ini memiliki IPv4 publik langsung
// pada interface jaringan host (ciri khas VPS publik tanpa NAT).
func HasPublicInterfaceIP() bool {
	return DetectPublicInterfaceIP() != ""
}

var (
	detectExternalPublicIPFn = edge.DetectPublicIP
	externalPublicIPMu       sync.Mutex
	cachedExternalPublicIP   string
	cachedExternalPublicAt   time.Time
	cachedExternalFailAt     time.Time
)

const (
	externalPublicIPTTL     = 5 * time.Minute
	externalPublicFailDelay = 10 * time.Second
)

func detectCachedExternalPublicIP() string {
	externalPublicIPMu.Lock()
	defer externalPublicIPMu.Unlock()

	if cachedExternalPublicIP != "" && time.Since(cachedExternalPublicAt) < externalPublicIPTTL {
		return cachedExternalPublicIP
	}
	if cachedExternalPublicIP == "" && !cachedExternalFailAt.IsZero() && time.Since(cachedExternalFailAt) < externalPublicFailDelay {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	pub := strings.TrimSpace(detectExternalPublicIPFn(ctx))
	if IsPublicIPv4String(pub) {
		cachedExternalPublicIP = pub
		cachedExternalPublicAt = time.Now()
		cachedExternalFailAt = time.Time{}
		return pub
	}
	cachedExternalFailAt = time.Now()
	// Pertahankan IP publik valid terakhir bila lookup sementara gagal setelah TTL habis.
	return cachedExternalPublicIP
}

// DetectPublicIP mendeteksi IPv4 publik baik langsung pada interface jaringan (tanpa NAT)
// maupun floating IP / 1:1 NAT melalui layanan eksternal (di-cache 5 menit).
func DetectPublicIP() string {
	if pub := DetectPublicInterfaceIP(); pub != "" {
		return pub
	}
	return detectCachedExternalPublicIP()
}

// HasPublicIP memeriksa apakah mesin ini memiliki alamat IPv4 publik (baik langsung di NIC maupun Floating IP).
func HasPublicIP() bool {
	return DetectPublicIP() != ""
}

// DetectLocalLANIP mengembalikan IPv4 LAN/host utama (bukan loopback dan bukan NetBird).
func DetectLocalLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	fallback := ""
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || isVirtualOrTunnelIface(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || isCGNATIPv4(ip4) {
				continue
			}
			if ip4.IsPrivate() {
				return ip4.String()
			}
			if fallback == "" {
				fallback = ip4.String()
			}
		}
	}
	return fallback
}

type modeSignals struct {
	explicitMode    string
	remoteEdgeHost  string
	hasLocalPDNS    bool
	hasEdgeAgentCfg bool
	localNetbirdIP  string
	hasPublicNIC    bool
	hasPublicIP     bool
}

func detectModeFromSignals(sig modeSignals) string {
	if norm, ok := NormalizeMode(sig.explicitMode); ok {
		return norm
	}
	// 1. Jika ada edge.conf VPS, ini jelas VPS agent.
	if sig.hasEdgeAgentCfg {
		return "vps"
	}
	// 2. Jika NIC memiliki IP publik langsung (ciri khas VPS publik tanpa NAT), ini PASTI adalah VPS standalone!
	// Pengecekan ini harus didahulukan sebelum NetBird, karena VPS publik juga sering menginstal NetBird untuk VPN admin.
	if sig.hasPublicNIC {
		return "vps"
	}
	// 3. Jika mesin memiliki IP publik (termasuk Floating IP / 1:1 NAT) tanpa tunnel NetBird dan tanpa remote edge host,
	// maka ini adalah Full VPS standalone!
	if sig.hasPublicIP && sig.localNetbirdIP == "" && sig.remoteEdgeHost == "" {
		return "vps"
	}
	if sig.hasLocalPDNS {
		return "local"
	}
	// 4. Jika edge-dns.conf mengarah ke host remote (bukan loopback), ini client Edge.
	if sig.remoteEdgeHost != "" {
		return "edge"
	}
	// 5. Jika NetBird aktif tanpa IP publik host, arahkan ke Edge Gateway.
	if sig.localNetbirdIP != "" {
		return "edge"
	}
	// 6. Default aman: mode Lokal standalone.
	return "local"
}

func (m *Manager) readEdgeDNSField(key string) string {
	candidates := []string{filepath.Join(m.cfg.WdevEtc, "edge-dns.conf")}
	if m.cfg.WdevEtc != "/etc/wdev" {
		candidates = append(candidates, "/etc/wdev/edge-dns.conf")
	}
	prefix := key + "="
	for _, confFile := range candidates {
		data, err := os.ReadFile(confFile)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, prefix) {
				v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), "\"")
				if v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func (m *Manager) readEdgeDNSFilePublicIP() string {
	return m.readEdgeDNSField("VPS_PUBLIC_IP")
}

func (m *Manager) readRemoteEdgeHost() string {
	v := m.readEdgeDNSField("VPS_NETBIRD_IP")
	if v != "" && v != "127.0.0.1" && v != "localhost" && v != "::1" && v != "0.0.0.0" {
		return v
	}
	return ""
}

// GetMode mengembalikan mode server: "local" (standalone lokal), "edge" (hybrid NetBird), atau "vps" (aaPanel style).
func (m *Manager) GetMode() string {
	explicit := ""
	if m.store != nil {
		if val, err := m.store.GetSetting("server_mode"); err == nil {
			explicit = val
		}
	}
	_, pdnsConfErr := os.Stat("/etc/powerdns/pdns.conf")
	_, edgeConfErr := os.Stat("/etc/wdev-edge/edge.conf")
	hasPubNIC := HasPublicInterfaceIP()
	hasPubIP := hasPubNIC
	if !hasPubIP {
		hasPubIP = HasPublicIP()
	}
	return detectModeFromSignals(modeSignals{
		explicitMode:    explicit,
		remoteEdgeHost:  m.readRemoteEdgeHost(),
		hasLocalPDNS:    pdnsConfErr == nil,
		hasEdgeAgentCfg: edgeConfErr == nil,
		localNetbirdIP:  m.GetLocalNetbirdIP(),
		hasPublicNIC:    hasPubNIC,
		hasPublicIP:     hasPubIP,
	})
}

// IsModeSet mengecek apakah user sudah memilih mode secara eksplisit.
func (m *Manager) IsModeSet() bool {
	if m.store == nil {
		return false
	}
	val, err := m.store.GetSetting("server_mode")
	if err != nil {
		return false
	}
	_, ok := NormalizeMode(val)
	return ok
}

// SetMode mengubah mode antara "local", "edge", dan "vps".
func (m *Manager) SetMode(mode string) error {
	norm, ok := NormalizeMode(mode)
	if !ok {
		return fmt.Errorf("mode tidak valid: %s (harus 'local', 'edge', atau 'vps')", mode)
	}
	if err := m.store.SetSetting("server_mode", norm); err != nil {
		return err
	}
	if norm == "vps" || norm == "local" {
		m.ensureLocalPDNSConfig(norm)
	}
	return nil
}

func (m *Manager) readLocalPDNSFileSettings() (apiKey, pdnsPort string) {
	pdnsPort = "8081"
	if data, err := os.ReadFile("/etc/powerdns/pdns.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "api-key=") {
				apiKey = strings.TrimSpace(strings.TrimPrefix(l, "api-key="))
			}
			if strings.HasPrefix(l, "webserver-port=") {
				if p := strings.TrimSpace(strings.TrimPrefix(l, "webserver-port=")); p != "" {
					pdnsPort = p
				}
			}
		}
	}
	return apiKey, pdnsPort
}

func (m *Manager) ensureLocalPDNSConfig(mode string) {
	apiKey := ""
	pubIP := ""
	pdnsPort := "8081"
	agentPort := "8082"

	// Baca dari /etc/wdev-edge/edge.conf jika ada
	for _, p := range []string{"/etc/wdev-edge/edge.conf", filepath.Join(m.cfg.WdevEtc, "edge.conf")} {
		if data, err := os.ReadFile(p); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
				if len(parts) == 2 {
					k := parts[0]
					v := strings.Trim(strings.TrimSpace(parts[1]), "\"")
					switch k {
					case "API_KEY":
						apiKey = v
					case "PUBLIC_IP":
						pubIP = v
					case "PDNS_API_PORT", "PDNS_PORT":
						if v != "" {
							pdnsPort = v
						}
					case "AGENT_PORT":
						if v != "" {
							agentPort = v
						}
					}
				}
			}
			break
		}
	}

	// Baca dari /etc/powerdns/pdns.conf jika belum ada API key
	if apiKey == "" {
		k, p := m.readLocalPDNSFileSettings()
		apiKey = k
		if p != "" {
			pdnsPort = p
		}
	}

	if mode == "vps" {
		if !IsPublicIPv4String(pubIP) {
			pubIP = DetectPublicIP()
		}
		if !IsPublicIPv4String(pubIP) {
			pubIP = ""
		}
	} else if pubIP == "" || !IsPublicIPv4String(pubIP) {
		pubIP = ""
	}

	if apiKey != "" {
		_ = m.SaveConfig("127.0.0.1", pubIP, apiKey, pdnsPort, agentPort)
	}
}

func (m *Manager) LoadConfig() EdgeConfig {
	mode := m.GetMode()
	isSet := m.IsModeSet()
	hasLocalPDNS := false
	if _, err := os.Stat("/etc/powerdns/pdns.conf"); err == nil {
		hasLocalPDNS = true
	} else if _, err := os.Stat("/etc/wdev-edge/edge.conf"); err == nil {
		hasLocalPDNS = true
	}

	confFile := filepath.Join(m.cfg.WdevEtc, "edge-dns.conf")
	data, err := os.ReadFile(confFile)
	if err != nil && m.cfg.WdevEtc != "/etc/wdev" {
		data, err = os.ReadFile("/etc/wdev/edge-dns.conf")
	}

	conf := EdgeConfig{
		Configured:     false,
		Mode:           mode,
		ModeIsSet:      isSet,
		HasLocalPDNS:   hasLocalPDNS,
		PDNSPort:       "8081",
		AgentPort:      "8082",
		LocalNetbirdIP: m.GetLocalNetbirdIP(),
	}

	parseConf := func(raw []byte) {
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			switch k {
			case "VPS_NETBIRD_IP":
				conf.VPSNetbirdIP = v
			case "VPS_PUBLIC_IP":
				if v != "" {
					conf.VPSPublicIP = v
				}
			case "API_KEY":
				conf.APIKey = v
			case "PDNS_PORT":
				if v != "" {
					conf.PDNSPort = v
				}
			case "AGENT_PORT":
				if v != "" {
					conf.AgentPort = v
				}
			}
		}
	}

	if err == nil {
		conf.Configured = true
		parseConf(data)
	}

	// Pada mode VPS atau Lokal dengan PowerDNS lokal, target API selalu 127.0.0.1 tanpa NetBird.
	if mode == "vps" || (mode == "local" && hasLocalPDNS) {
		if hasLocalPDNS && (!conf.Configured || conf.APIKey == "" || (conf.VPSNetbirdIP != "127.0.0.1" && conf.VPSNetbirdIP != "localhost")) {
			m.ensureLocalPDNSConfig(mode)
			if d, e := os.ReadFile(confFile); e == nil {
				conf.Configured = true
				parseConf(d)
			} else if m.cfg.WdevEtc != "/etc/wdev" {
				if d, e := os.ReadFile("/etc/wdev/edge-dns.conf"); e == nil {
					conf.Configured = true
					parseConf(d)
				}
			}
		}
		if hasLocalPDNS && conf.APIKey == "" {
			if k, p := m.readLocalPDNSFileSettings(); k != "" {
				conf.APIKey = k
				conf.PDNSPort = p
				conf.Configured = true
			}
		}
		if hasLocalPDNS || mode == "vps" {
			if conf.VPSNetbirdIP == "" || conf.VPSNetbirdIP == "-" || conf.VPSNetbirdIP == "0.0.0.0" || hasLocalPDNS {
				conf.VPSNetbirdIP = "127.0.0.1"
			}
		}
		if mode == "vps" {
			// Mode Full VPS tidak boleh memakai IP privat/CGNAT/loopback sebagai target record publik.
			if !IsPublicIPv4String(conf.VPSPublicIP) {
				conf.VPSPublicIP = ""
			}
			if conf.VPSPublicIP == "" {
				if pub := DetectPublicIP(); pub != "" {
					conf.VPSPublicIP = pub
				}
			}
		}
		if mode == "local" && hasLocalPDNS && conf.VPSPublicIP == "" {
			conf.VPSPublicIP = "127.0.0.1"
		}
		if conf.APIKey != "" && conf.VPSNetbirdIP != "" {
			conf.Configured = true
		}
	}

	// Check if reachable
	conf.HasAPIKey = conf.APIKey != ""
	if conf.VPSNetbirdIP != "" && conf.PDNSPort != "" && conf.APIKey != "" {
		client := http.Client{Timeout: 1500 * time.Millisecond}
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%s/api/v1/servers/localhost", conf.VPSNetbirdIP, conf.PDNSPort), nil)
		if err == nil {
			req.Header.Set("X-API-Key", conf.APIKey)
			resp, err := client.Do(req)
			if err == nil {
				conf.Reachable = (resp.StatusCode == 200 || resp.StatusCode == 401 || resp.StatusCode == 403)
				_ = resp.Body.Close()
			}
		}
	}

	return conf
}

var detectLocalNetbirdIPFn = detectLocalNetbirdIP

func (m *Manager) GetLocalNetbirdIP() string {
	return detectLocalNetbirdIPFn()
}

// SetLocalNetbirdIPFn mengganti detektor IP NetBird lokal (untuk pengujian) dan mengembalikan fungsi pemulih.
func SetLocalNetbirdIPFn(fn func() string) func() {
	prev := detectLocalNetbirdIPFn
	detectLocalNetbirdIPFn = fn
	return func() {
		detectLocalNetbirdIPFn = prev
	}
}

func detectLocalNetbirdIP() string {
	// 1. Try wt0 interface
	if iface, err := net.InterfaceByName("wt0"); err == nil {
		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
					if ip4 := ipnet.IP.To4(); ip4 != nil {
						return ip4.String()
					}
				}
			}
		}
	}

	// 2. Try all network interfaces for 100.x.x.x
	if ifaces, err := net.Interfaces(); err == nil {
		for _, ifi := range ifaces {
			if addrs, err := ifi.Addrs(); err == nil {
				for _, addr := range addrs {
					if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
						if ip4 := ipnet.IP.To4(); ip4 != nil && strings.HasPrefix(ip4.String(), "100.") {
							return ip4.String()
						}
					}
				}
			}
		}
	}

	// 3. Fallback: netbird status command
	if out, err := exec.Command("netbird", "status").Output(); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), "netbird ip") {
				fields := strings.Fields(line)
				for _, f := range fields {
					ip := strings.Split(f, "/")[0]
					if net.ParseIP(ip) != nil {
						return ip
					}
				}
			}
		}
	}

	return ""
}

func (m *Manager) requirePDNSConfig() (EdgeConfig, error) {
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		if conf.Mode == "edge" {
			return conf, fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
		}
		return conf, fmt.Errorf("PowerDNS belum terpasang atau belum dikonfigurasi")
	}
	return conf, nil
}

func (m *Manager) ListZones() ([]ZoneInfo, error) {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		return nil, err
	}

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones", conf.VPSNetbirdIP, conf.PDNSPort)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", conf.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke PowerDNS API gagal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, string(body))
	}

	var zones []ZoneInfo
	if err := json.NewDecoder(resp.Body).Decode(&zones); err != nil {
		return nil, err
	}

	return zones, nil
}

func (m *Manager) GetZone(zoneName string) (*ZoneInfo, error) {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		return nil, err
	}

	zoneName = strings.TrimSuffix(strings.TrimSpace(zoneName), ".") + "."

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones/%s", conf.VPSNetbirdIP, conf.PDNSPort, zoneName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", conf.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil data zone PowerDNS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, string(body))
	}

	var zone ZoneInfo
	if err := json.NewDecoder(resp.Body).Decode(&zone); err != nil {
		return nil, err
	}

	return &zone, nil
}

// NormalizeZoneName membersihkan dan memvalidasi nama zone authoritative.
func NormalizeZoneName(zoneName string) (string, error) {
	z := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zoneName), "."))
	if z == "" {
		return "", fmt.Errorf("nama zone wajib diisi")
	}
	if len(z) > 253 || !strings.Contains(z, ".") || strings.Contains(z, "..") {
		return "", fmt.Errorf("nama zone tidak valid: %q", zoneName)
	}
	for _, label := range strings.Split(z, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("nama zone tidak valid: %q", zoneName)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", fmt.Errorf("nama zone tidak valid: %q", zoneName)
			}
		}
	}
	return z, nil
}

func resolveZoneTargetIP(conf EdgeConfig) string {
	zoneIP := strings.TrimSpace(conf.VPSPublicIP)
	switch conf.Mode {
	case "local":
		if zoneIP == "" {
			zoneIP = DetectLocalLANIP()
			if zoneIP == "" {
				zoneIP = "127.0.0.1"
			}
		}
	case "vps":
		if !IsPublicIPv4String(zoneIP) {
			zoneIP = DetectPublicIP()
		}
		if !IsPublicIPv4String(zoneIP) {
			zoneIP = ""
		}
	}
	return zoneIP
}

// AddZone membuat zone Native lengkap dengan NS, glue A, apex/www A, dan SOA-EDIT-API
// agar zone langsung valid dan serial otomatis naik saat record berubah.
func (m *Manager) AddZone(zoneName string) error {
	_, _, err := m.addZoneCore(zoneName)
	return err
}

// AddZoneWithVhost membuat zone di PowerDNS sekaligus menyiapkan vhost Nginx
// dengan template HTML statis default agar domain dapat langsung diakses.
func (m *Manager) AddZoneWithVhost(zoneName string) (AddZoneResult, error) {
	zone, zoneIP, err := m.addZoneCore(zoneName)
	if err != nil {
		return AddZoneResult{}, err
	}

	res := AddZoneResult{
		Zone:        zone,
		Nameservers: []string{"ns1." + zone, "ns2." + zone},
		ZoneIP:      zoneIP,
		VHostStatus: "skipped",
	}

	if m.siteMgr == nil {
		return res, nil
	}

	vh, vhErr := m.siteMgr.EnsureDomainStaticVhost(zone)
	res.VHostStatus = vh.Status
	res.VHostCreated = vh.Created
	res.VHostRoot = vh.RootPath
	res.VHostURL = vh.URL
	res.VHostMessage = vh.Message
	if vhErr != nil {
		res.VHostStatus = "error"
		res.VHostMessage = vhErr.Error()
		return res, nil
	}

	if vh.Created && m.LoadConfig().Mode == "edge" {
		if m.GetLocalNetbirdIP() != "" {
			if expErr := m.ExposeSite(zone, zone, false); expErr != nil {
				res.VHostMessage = fmt.Sprintf("VHost static HTML lokal dibuat di %s, namun ekspos otomatis ke VPS Edge gagal: %v", vh.RootPath, expErr)
			} else {
				res.EdgeExposed = true
				res.VHostMessage = fmt.Sprintf("VHost static HTML dibuat di %s dan diekspos ke VPS Edge (%s)", vh.RootPath, vh.URL)
			}
		} else {
			res.VHostMessage = fmt.Sprintf("VHost static HTML lokal dibuat di %s; aktifkan NetBird lalu ekspos ke VPS Edge agar dapat diakses publik.", vh.RootPath)
		}
	}

	return res, nil
}

func (m *Manager) addZoneCore(zoneName string) (string, string, error) {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		return "", "", err
	}
	zone, err := NormalizeZoneName(zoneName)
	if err != nil {
		return "", "", err
	}
	zoneIP := resolveZoneTargetIP(conf)
	if zoneIP == "" {
		return "", "", fmt.Errorf("IP server untuk NS/glue zone belum tercatat; atur VPS_PUBLIC_IP atau jalankan 'wdev pdns connect'")
	}

	ns1 := "ns1." + zone
	ns2 := "ns2." + zone
	payload := map[string]any{
		"name":         zone + ".",
		"kind":         "Native",
		"nameservers":  []string{ns1 + ".", ns2 + "."},
		"soa_edit_api": "INCEPTION-INCREMENT",
		"api_rectify":  true,
	}
	jsonBytes, _ := json.Marshal(payload)

	client := http.Client{Timeout: 6 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones", conf.VPSNetbirdIP, conf.PDNSPort)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("X-API-Key", conf.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("gagal memanggil API PowerDNS: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusConflict {
		return "", "", fmt.Errorf("zone %s sudah ada di PowerDNS", zone)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	rollback := func() { _ = m.DeleteZone(zone) }

	// PowerDNS membuat SOA dari default-soa-content
	// (a.misconfigured.dns.server.invalid, serial 0). Tulis SOA eksplisit agar
	// zone langsung valid.
	soa := fmt.Sprintf("%s. hostmaster.%s. %d 10800 3600 604800 3600", ns1, zone, time.Now().Unix())
	if err := m.patchRecords(zone, "@", "SOA", []string{soa}, 3600); err != nil {
		rollback()
		return "", "", fmt.Errorf("SOA gagal ditulis; zone dibatalkan: %w", err)
	}

	// Glue A untuk nameserver in-zone agar delegasi bisa di-resolve.
	if err := m.patchRecords(zone, ns1, "A", []string{zoneIP}, 300); err != nil {
		rollback()
		return "", "", fmt.Errorf("glue %s gagal; zone dibatalkan: %w", ns1, err)
	}
	if err := m.patchRecords(zone, ns2, "A", []string{zoneIP}, 300); err != nil {
		rollback()
		return "", "", fmt.Errorf("glue %s gagal; zone dibatalkan: %w", ns2, err)
	}
	// Record A apex (@) dan www agar domain web langsung mengarah ke IP server.
	if err := m.patchRecords(zone, "@", "A", []string{zoneIP}, 300); err != nil {
		rollback()
		return "", "", fmt.Errorf("record A apex %s gagal; zone dibatalkan: %w", zone, err)
	}
	if !strings.HasPrefix(zone, "www.") {
		if err := m.patchRecords(zone, "www", "A", []string{zoneIP}, 300); err != nil {
			rollback()
			return "", "", fmt.Errorf("record A www.%s gagal; zone dibatalkan: %w", zone, err)
		}
	}
	if err := m.setZoneMetadata(zone, "SOA-EDIT", []string{"INCEPTION-INCREMENT"}); err != nil {
		rollback()
		return "", "", fmt.Errorf("metadata SOA-EDIT gagal; zone dibatalkan: %w", err)
	}

	// Verifikasi zone benar-benar lengkap.
	info, err := m.GetZone(zone)
	if err != nil {
		rollback()
		return "", "", fmt.Errorf("zone gagal diverifikasi; dibatalkan: %w", err)
	}
	hasSOA, hasNS := false, false
	for _, rr := range info.RRsets {
		switch strings.ToUpper(rr.Type) {
		case "SOA":
			if len(rr.Records) > 0 {
				if parsed := parseSOAContent(rr.Records[0].Content); parsed.Primary != "" && !strings.Contains(parsed.Primary, "misconfigured") {
					hasSOA = true
				}
			}
		case "NS":
			if len(rr.Records) > 0 {
				hasNS = true
			}
		}
	}
	if !hasSOA || !hasNS {
		rollback()
		return "", "", fmt.Errorf("zone %s tidak lengkap (SOA=%v NS=%v); dibatalkan", zone, hasSOA, hasNS)
	}
	return zone, zoneIP, nil
}

func (m *Manager) DeleteZone(zoneName string) error {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		return err
	}

	zoneName = strings.TrimSuffix(strings.TrimSpace(zoneName), ".") + "."

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones/%s", conf.VPSNetbirdIP, conf.PDNSPort, zoneName)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", conf.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal menghapus zone PowerDNS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func normalizeRecordName(name, zone string) string {
	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".") + "."
	name = strings.TrimSpace(name)
	if name == "" || name == "@" {
		return zone
	}
	name = strings.TrimSuffix(name, ".")
	zClean := strings.TrimSuffix(zone, ".")
	if name == zClean || strings.HasSuffix(name, "."+zClean) {
		return name + "."
	}
	return name + "." + zone
}

func (m *Manager) AddRecord(zone, name, rtype, content string, ttl int) error {
	return m.patchRecords(zone, name, rtype, []string{content}, ttl)
}

// LookupRecords mengembalikan isi RRset yang sudah ada pada name/type tertentu.
func (m *Manager) LookupRecords(zone, name, rtype string) ([]string, error) {
	zi, err := m.GetZone(zone)
	if err != nil {
		return nil, err
	}
	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".") + "."
	fqdn := normalizeRecordName(name, zone)
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	var res []string
	for _, rr := range zi.RRsets {
		if rr.Name != fqdn || !strings.EqualFold(rr.Type, rtype) {
			continue
		}
		for _, rec := range rr.Records {
			if !rec.Disabled {
				res = append(res, rec.Content)
			}
		}
	}
	return res, nil
}

// ReplaceRecords menimpa RRset dengan daftar isi tertentu (dipakai saat hanya
// sebagian nilai pada RRset yang perlu dihapus).
func (m *Manager) ReplaceRecords(zone, name, rtype string, contents []string, ttl int) error {
	if len(contents) == 0 {
		return m.DeleteRecord(zone, name, rtype)
	}
	return m.patchRecords(zone, name, rtype, contents, ttl)
}

// AddRecordKeep menambah record tanpa menghapus record lain pada name/type yang
// sama (dipakai untuk TXT multi-value seperti verifikasi SPF).
func (m *Manager) AddRecordKeep(zone, name, rtype, content string, ttl int) error {
	existing, err := m.LookupRecords(zone, name, rtype)
	if err != nil {
		return err
	}
	return m.patchRecords(zone, name, rtype, mergeRecordContents(existing, content), ttl)
}

func mergeRecordContents(existing []string, content string) []string {
	want := strings.TrimSpace(content)
	for _, e := range existing {
		if strings.TrimSpace(e) == want {
			return existing
		}
	}
	return append(existing, content)
}

func formatTXTRecordContent(raw string) string {
	c := strings.TrimSpace(raw)
	if c == "" {
		return `""`
	}
	// Jika sudah berbentuk multi-chunk "... ..." dan tiap chunk <= 255 byte, pertahankan.
	if strings.HasPrefix(c, `"`) && strings.HasSuffix(c, `"`) && strings.Contains(c, `" "`) {
		inner := c
		parts := strings.Split(inner[1:len(inner)-1], `" "`)
		validChunks := len(parts) > 0
		for _, p := range parts {
			if len(p) > 255 {
				validChunks = false
				break
			}
		}
		if validChunks {
			return c
		}
	}
	if strings.HasPrefix(c, `"`) && strings.HasSuffix(c, `"`) && len(c) >= 2 {
		c = c[1 : len(c)-1]
	}
	if len(c) <= 255 {
		return `"` + c + `"`
	}
	// PowerDNS API menolak string TXT tunggal > 255 byte; pecah per <= 255 byte sesuai RFC 1035.
	var chunks []string
	for len(c) > 0 {
		n := 255
		if len(c) < n {
			n = len(c)
		}
		chunks = append(chunks, `"`+c[:n]+`"`)
		c = c[n:]
	}
	return strings.Join(chunks, " ")
}

func formatMXRecordContent(raw string) string {
	c := strings.TrimSpace(raw)
	if c == "" {
		return c
	}
	fields := strings.Fields(c)
	if len(fields) == 1 {
		target := strings.TrimSuffix(fields[0], ".") + "."
		return "10 " + target
	}
	fields[len(fields)-1] = strings.TrimSuffix(fields[len(fields)-1], ".") + "."
	return strings.Join(fields, " ")
}

func (m *Manager) patchRecords(zone, name, rtype string, contents []string, ttl int) error {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		return err
	}

	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".") + "."
	fqdn := normalizeRecordName(name, zone)
	rtype = strings.ToUpper(strings.TrimSpace(rtype))

	if ttl <= 0 {
		ttl = 300
	}

	records := make([]map[string]any, 0, len(contents))
	for _, c := range contents {
		c = strings.TrimSpace(c)
		if rtype == "TXT" {
			c = formatTXTRecordContent(c)
		} else if rtype == "CNAME" {
			if !strings.HasSuffix(c, ".") {
				c = c + "."
			}
		} else if rtype == "MX" {
			c = formatMXRecordContent(c)
		}
		records = append(records, map[string]any{"content": c, "disabled": false})
	}
	payload := map[string]any{
		"rrsets": []map[string]any{
			{
				"name": fqdn, "type": rtype, "ttl": ttl, "changetype": "REPLACE", "records": records,
			},
		},
	}
	jsonBytes, _ := json.Marshal(payload)

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones/%s", conf.VPSNetbirdIP, conf.PDNSPort, zone)
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", conf.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal memanggil API PowerDNS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (m *Manager) DeleteRecord(zone, name, rtype string) error {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		return err
	}

	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".") + "."
	fqdn := normalizeRecordName(name, zone)
	rtype = strings.ToUpper(strings.TrimSpace(rtype))

	payload := map[string]any{
		"rrsets": []map[string]any{
			{
				"name":       fqdn,
				"type":       rtype,
				"changetype": "DELETE",
			},
		},
	}
	jsonBytes, _ := json.Marshal(payload)

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones/%s", conf.VPSNetbirdIP, conf.PDNSPort, zone)
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", conf.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal memanggil API PowerDNS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (m *Manager) ListExposures() ([]ProxyExposure, error) {
	conf, err := m.requirePDNSConfig()
	if err != nil {
		if conf.Mode != "edge" {
			return []ProxyExposure{}, nil
		}
		return nil, err
	}

	agentPort := conf.AgentPort
	if agentPort == "" {
		agentPort = "8082"
	}
	if conf.Mode != "edge" {
		conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(conf.VPSNetbirdIP, agentPort), 500*time.Millisecond)
		if dialErr != nil {
			return []ProxyExposure{}, nil
		}
		_ = conn.Close()
	}

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/proxies", conf.VPSNetbirdIP, agentPort)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", conf.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal menghubungi VPS Edge Agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("VPS Edge Agent merespons %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		Proxies []ProxyExposure `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	// Enrich each proxy with local site matching and target information
	sites, _ := m.store.ListSites()
	for i := range res.Proxies {
		p := &res.Proxies[i]
		p.SSL = "Aktif (HTTPS)"

		// Check local sites from store
		matchedSite := ""
		matchedFw := ""
		for _, s := range sites {
			if strings.EqualFold(s.ExposedTo, p.Domain) {
				matchedSite = s.Domain
				matchedFw = s.Framework
				break
			}
		}

		// Also check /etc/nginx/sites-available directly
		if matchedSite == "" {
			if matches, err := filepath.Glob("/etc/nginx/sites-available/*"); err == nil {
				for _, f := range matches {
					if strings.HasSuffix(f, ".custom.conf") || strings.HasSuffix(f, ".bak") || strings.HasSuffix(f, "~") {
						continue
					}
					data, err := os.ReadFile(f)
					if err != nil {
						continue
					}
					content := string(data)
					if strings.Contains(content, p.Domain) {
						base := filepath.Base(f)
						matchedSite = strings.TrimSuffix(base, ".conf")
						if strings.Contains(content, "laravel") {
							matchedFw = "laravel"
						} else if strings.Contains(content, "wordpress") {
							matchedFw = "wordpress"
						}
						break
					}
				}
			}
		}

		if matchedSite != "" {
			p.LocalSite = matchedSite
			p.Target = fmt.Sprintf("VHost Lokal (%s - %s)", matchedSite, strings.ToUpper(matchedFw))
		} else if strings.HasPrefix(p.Domain, "mailadmin.") {
			p.Target = "Stalwart Mail Admin UI (:80 / :8080)"
		} else if strings.HasPrefix(p.Domain, "webmail.") {
			p.Target = "SnappyMail Client (:80)"
		} else if strings.HasPrefix(p.Domain, "mail.") {
			p.Target = "Postfix Inbound Mail Host (:25)"
		} else {
			p.Target = "Proxy Port VPS Edge"
		}
	}

	return res.Proxies, nil
}

func (m *Manager) FindMatchingZone(domain string) (string, error) {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if cleanDom == "" {
		return "", fmt.Errorf("domain kosong")
	}
	zones, err := m.ListZones()
	if err != nil {
		return "", fmt.Errorf("gagal membaca daftar zone PowerDNS: %w", err)
	}
	candidate := matchZone(cleanDom, zones)
	if candidate == "" {
		return "", fmt.Errorf("zone DNS untuk '%s' tidak ditemukan; tambahkan zone di PowerDNS terlebih dahulu", cleanDom)
	}
	return candidate, nil
}

// matchZone memilih zone terpanjang yang menjadi suffix domain.
func matchZone(cleanDom string, zones []ZoneInfo) string {
	var candidate string
	for _, z := range zones {
		zClean := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(z.Name), "."))
		if cleanDom == zClean || strings.HasSuffix(cleanDom, "."+zClean) {
			if len(zClean) > len(candidate) {
				candidate = zClean
			}
		}
	}
	return candidate
}

func (m *Manager) Expose(domain string, targetPort int, useSSL bool, websocket bool) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return fmt.Errorf("domain publik tidak boleh kosong")
	}
	if targetPort <= 0 || targetPort > 65535 {
		return fmt.Errorf("port target tidak valid: %d", targetPort)
	}

	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
	}

	localIP := m.GetLocalNetbirdIP()
	if localIP == "" {
		return fmt.Errorf("IP Netbird lokal (wt0) tidak terdeteksi. Pastikan Netbird aktif.")
	}

	// 1. Auto-configure DNS Record di PowerDNS bila zone dikelola di sini.
	// Zone yang tidak ada (DNS di provider lain) dilewati; kegagalan tulis pada
	// zone yang ada harus terlihat, jangan silent.
	if zone, zoneErr := m.FindMatchingZone(domain); zoneErr == nil && conf.VPSPublicIP != "" {
		if err := m.AddRecord(zone, domain, "A", conf.VPSPublicIP, 300); err != nil {
			return fmt.Errorf("gagal membuat A record %s di zone %s: %w", domain, zone, err)
		}
	}

	// 2. Call VPS Edge Agent
	agentPort := conf.AgentPort
	if agentPort == "" {
		agentPort = "8082"
	}

	payload := map[string]any{
		"domain":      domain,
		"target_ip":   localIP,
		"target_port": targetPort,
		"ssl":         useSSL,
		"websocket":   websocket,
	}
	jsonBytes, _ := json.Marshal(payload)

	client := http.Client{Timeout: 45 * time.Second} // Allow time for certbot if useSSL=true
	url := fmt.Sprintf("http://%s:%s/api/v1/proxies", conf.VPSNetbirdIP, agentPort)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", conf.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal menghubungi VPS Edge Agent: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		_ = json.Unmarshal(bodyBytes, &errResp)
		if errResp.Error != "" {
			if errResp.Details != "" {
				return fmt.Errorf("%s: %s", errResp.Error, errResp.Details)
			}
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("VPS Edge Agent merespons status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

var (
	pdnsHostRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	sshUserRe  = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,63}$`)
)

func validatePortString(raw, label string) (string, error) {
	p, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("%s tidak valid: %q", label, raw)
	}
	return strconv.Itoa(p), nil
}

func (m *Manager) ExposeSite(localDomain, publicDomain string, useSSL bool) error {
	localDomain = strings.ToLower(strings.TrimSpace(localDomain))
	publicDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(publicDomain), "."))

	if localDomain == "" || publicDomain == "" {
		return fmt.Errorf("domain lokal dan domain publik tidak boleh kosong")
	}
	if !pdnsHostRe.MatchString(localDomain) || strings.Contains(localDomain, "..") {
		return fmt.Errorf("domain lokal tidak valid: %q", localDomain)
	}
	if _, err := NormalizeZoneName(publicDomain); err != nil {
		return fmt.Errorf("domain publik tidak valid: %w", err)
	}

	// Check config file in sites-available
	sitesAvailDir := "/etc/nginx/sites-available"
	if m.cfg != nil && strings.TrimSpace(m.cfg.NginxSitesAvail) != "" {
		sitesAvailDir = strings.TrimSpace(m.cfg.NginxSitesAvail)
	}
	confPath := filepath.Join(sitesAvailDir, localDomain+".conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		confPath = filepath.Join(sitesAvailDir, localDomain)
		if _, err := os.Stat(confPath); os.IsNotExist(err) {
			return fmt.Errorf("konfigurasi vhost lokal untuk '%s' tidak ditemukan di %s", localDomain, sitesAvailDir)
		}
	}

	// Configure local vhost with alias and reverse proxy headers
	if err := configureLocalVhost(confPath, publicDomain); err != nil {
		return fmt.Errorf("gagal mengonfigurasi vhost lokal: %w", err)
	}

	// Reload nginx when managing system Nginx directory
	if sitesAvailDir == "/etc/nginx/sites-available" {
		if err := reloadNginx(); err != nil {
			return fmt.Errorf("gagal me-reload Nginx lokal: %w", err)
		}
	}

	// Update store with exposed_to
	if m.store != nil {
		_ = m.store.SetSiteExposedTo(localDomain, publicDomain)
	}

	// Expose via port 80 to Edge Gateway
	return m.Expose(publicDomain, 80, useSSL, true)
}

func configureLocalVhost(confPath, pubDom string) error {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	text := string(data)

	// Check if pubDom already present as an exact whitespace-delimited token in server_name
	reServerName := regexp.MustCompile(`(?m)server_name\s+([^;]+);`)
	matches := reServerName.FindAllStringSubmatch(text, -1)
	alreadyPresent := false
	for _, m := range matches {
		for _, name := range strings.Fields(m[1]) {
			if strings.EqualFold(name, pubDom) {
				alreadyPresent = true
				break
			}
		}
		if alreadyPresent {
			break
		}
	}

	if !alreadyPresent {
		// Remove standalone port 80 redirect block if any
		reStandalone := regexp.MustCompile(`(?s)server\s*\{[^{}]*listen\s+80[^{}]*return\s+301\s+https://\$host\$request_uri;\s*\}\s*`)
		text = reStandalone.ReplaceAllString(text, "")

		// Ensure listen 80 is present in the first server block
		if !strings.Contains(text, "listen 80;") {
			reServer := regexp.MustCompile(`(?m)(server\s*\{[ \t]*\n)`)
			text = reServer.ReplaceAllString(text, "${1}    listen 80;\n    listen [::]:80;\n")
		}

		// Add alias to the first server_name directive
		added := false
		text = reServerName.ReplaceAllStringFunc(text, func(match string) string {
			if !added {
				added = true
				idx := strings.LastIndex(match, ";")
				if idx != -1 {
					return match[:idx] + " " + pubDom + ";"
				}
			}
			return match
		})

		// Add conditional https redirect & fe_https variable if not already present
		if !strings.Contains(text, "$redirect_to_https") {
			proxyRules := `
    # Handle HTTP to HTTPS redirect for direct non-proxy connections
    set $redirect_to_https 0;
    if ($scheme = "http") {
        set $redirect_to_https 1;
    }
    if ($http_x_forwarded_proto = "https") {
        set $redirect_to_https 0;
    }
    if ($redirect_to_https = 1) {
        return 301 https://$host$request_uri;
    }

    # Pass HTTPS flag to PHP-FPM when proxied behind Edge Gateway SSL
    set $fe_https $https;
    if ($http_x_forwarded_proto = "https") {
        set $fe_https on;
    }
`
			reSNLine := regexp.MustCompile(`(?m)(server_name\s+[^;]+;\n)`)
			text = reSNLine.ReplaceAllString(text, "${1}"+proxyRules)
		}

		// Add fastcgi_param HTTPS $fe_https to PHP block if missing
		if strings.Contains(text, `location ~ \.php`) && !strings.Contains(text, "fastcgi_param HTTPS") {
			reFastcgi := regexp.MustCompile(`(?m)(include\s+fastcgi_params;\n)`)
			text = reFastcgi.ReplaceAllString(text, "${1}        fastcgi_param HTTPS $fe_https if_not_empty;\n")
		}

		// Write with sudo if needed
		backup := data
		if err := writeFileWithSudo(confPath, []byte(text)); err != nil {
			return err
		}

		// Test configuration
		if err := testNginx(); err != nil {
			_ = writeFileWithSudo(confPath, backup)
			return err
		}
	}

	return nil
}

func (m *Manager) Unexpose(domain string, keepDNS bool) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return fmt.Errorf("domain tidak boleh kosong")
	}

	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
	}

	agentPort := conf.AgentPort
	if agentPort == "" {
		agentPort = "8082"
	}

	// 1. Remove proxy from VPS Edge Agent
	client := http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/proxies/%s", conf.VPSNetbirdIP, agentPort, domain)
	req, err := http.NewRequest("DELETE", url, nil)
	if err == nil {
		req.Header.Set("X-API-Key", conf.APIKey)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}

	// 2. Remove DNS records from PowerDNS if !keepDNS
	if !keepDNS {
		zone, _ := m.FindMatchingZone(domain)
		if zone != "" && domain != zone {
			_ = m.DeleteRecord(zone, domain, "A")
			_ = m.DeleteRecord(zone, domain, "CNAME")
		}
	}

	// 3. Remove alias from local Nginx vhosts
	removeLocalVhostAlias(domain)
	_ = reloadNginx()

	// 4. Clear store exposed_to
	_ = m.store.ClearSiteExposedTo(domain)

	return nil
}

func removeLocalVhostAlias(pubDom string) {
	matches, err := filepath.Glob("/etc/nginx/sites-available/*")
	if err != nil {
		return
	}

	reServerName := regexp.MustCompile(`(?m)server_name\s+([^;]+);`)

	for _, file := range matches {
		if strings.HasSuffix(file, ".bak") || strings.HasSuffix(file, "~") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		text := string(data)

		modified := false
		newText := reServerName.ReplaceAllStringFunc(text, func(match string) string {
			sub := reServerName.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			fields := strings.Fields(sub[1])
			var kept []string
			hasRemoved := false
			for _, f := range fields {
				if strings.EqualFold(f, pubDom) {
					hasRemoved = true
					modified = true
				} else {
					kept = append(kept, f)
				}
			}
			if hasRemoved {
				return "server_name " + strings.Join(kept, " ") + ";"
			}
			return match
		})

		if modified {
			_ = writeFileWithSudo(file, []byte(newText))
		}
	}
}

func writeSecretFileWithSudo(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0600); err == nil {
		_ = os.Chmod(path, 0600)
		return nil
	}
	if os.Geteuid() != 0 {
		tmpFile, err := os.CreateTemp("", "wdev-secret-*")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)
		_ = os.Chmod(tmpPath, 0600)
		if _, err := tmpFile.Write(content); err != nil {
			tmpFile.Close()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			return err
		}
		cmd := exec.Command("sudo", "-n", "install", "-m", "0600", tmpPath, path)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("sudo install gagal: %w (%s)", err, string(out))
		}
		return nil
	}
	return os.WriteFile(path, content, 0600)
}

func writeFileWithSudo(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0644); err == nil {
		return nil
	}
	if os.Geteuid() != 0 {
		cmd := exec.Command("sudo", "-n", "tee", path)
		cmd.Stdin = bytes.NewReader(content)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("sudo tee gagal: %w (%s)", err, string(out))
		}
		return nil
	}
	return os.WriteFile(path, content, 0644)
}

func testNginx() error {
	var cmd *exec.Cmd
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "-n", "nginx", "-t")
	} else {
		cmd = exec.Command("nginx", "-t")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("konfigurasi nginx tidak valid: %s", string(out))
	}
	return nil
}

func reloadNginx() error {
	var cmd *exec.Cmd
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "-n", "systemctl", "reload", "nginx")
	} else {
		cmd = exec.Command("systemctl", "reload", "nginx")
	}
	return cmd.Run()
}

func (m *Manager) SaveConfig(vpsNetbirdIP, vpsPublicIP, apiKey, pdnsPort, agentPort string) error {
	if pdnsPort == "" {
		pdnsPort = "8081"
	}
	if agentPort == "" {
		agentPort = "8082"
	}
	content := fmt.Sprintf(`# wdev Edge & PowerDNS Configuration
VPS_NETBIRD_IP="%s"
VPS_PUBLIC_IP="%s"
API_KEY="%s"
PDNS_PORT="%s"
AGENT_PORT="%s"
`, vpsNetbirdIP, vpsPublicIP, apiKey, pdnsPort, agentPort)

	targets := []string{m.cfg.WdevEtc}
	if m.cfg.WdevEtc != "/etc/wdev" {
		targets = append(targets, "/etc/wdev")
	}

	var writeErr error
	for _, dir := range targets {
		if os.Geteuid() != 0 {
			_ = exec.Command("sudo", "-n", "mkdir", "-p", dir).Run()
		} else {
			_ = os.MkdirAll(dir, 0700)
		}
		confFile := filepath.Join(dir, "edge-dns.conf")
		if err := writeSecretFileWithSudo(confFile, []byte(content)); err != nil {
			writeErr = err
		}
	}
	return writeErr
}

func (m *Manager) Connect(opts ConnectOptions) error {
	opts.VPSNetbirdIP = strings.TrimSpace(opts.VPSNetbirdIP)
	opts.VPSPublicIP = strings.TrimSpace(opts.VPSPublicIP)
	opts.APIKey = strings.TrimSpace(opts.APIKey)
	if opts.VPSNetbirdIP == "" || opts.APIKey == "" {
		return fmt.Errorf("IP NetBird VPS dan API Key wajib diisi")
	}
	parsedNB := net.ParseIP(opts.VPSNetbirdIP)
	if parsedNB == nil {
		return fmt.Errorf("IP NetBird VPS tidak valid: %q", opts.VPSNetbirdIP)
	}
	opts.VPSNetbirdIP = parsedNB.String()
	if opts.VPSPublicIP != "" {
		parsedPub := net.ParseIP(opts.VPSPublicIP)
		if parsedPub == nil {
			return fmt.Errorf("IP Publik VPS tidak valid: %q", opts.VPSPublicIP)
		}
		opts.VPSPublicIP = parsedPub.String()
	}
	if strings.ContainsAny(opts.APIKey, "\r\n\x00\"'") {
		return fmt.Errorf("API Key mengandung karakter tidak valid")
	}
	if opts.PDNSPort == "" {
		opts.PDNSPort = "8081"
	}
	var err error
	if opts.PDNSPort, err = validatePortString(opts.PDNSPort, "PDNSPort"); err != nil {
		return err
	}
	if opts.AgentPort == "" {
		opts.AgentPort = "8082"
	}
	if opts.AgentPort, err = validatePortString(opts.AgentPort, "AgentPort"); err != nil {
		return err
	}

	client := http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost", opts.VPSNetbirdIP, opts.PDNSPort)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", opts.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("tidak dapat menghubungi PowerDNS di %s:%s: %w", opts.VPSNetbirdIP, opts.PDNSPort, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("PowerDNS API Key salah atau ditolak (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PowerDNS API merespons status HTTP %d", resp.StatusCode)
	}

	return m.SaveConfig(opts.VPSNetbirdIP, opts.VPSPublicIP, opts.APIKey, opts.PDNSPort, opts.AgentPort)
}

// AgentPost performs an authenticated POST request to the VPS Edge Agent.
func (m *Manager) AgentPost(path string, payload any) ([]byte, error) {
	conf := m.LoadConfig()
	if conf.VPSNetbirdIP == "" {
		return nil, fmt.Errorf("VPS Edge Gateway belum terhubung (IP NetBird VPS kosong)")
	}
	agentPort := conf.AgentPort
	if agentPort == "" {
		agentPort = "3100"
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	client := http.Client{Timeout: 60 * time.Second}
	url := fmt.Sprintf("http://%s:%s%s", conf.VPSNetbirdIP, agentPort, path)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", conf.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal menghubungi VPS Edge Agent: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		_ = json.Unmarshal(bodyBytes, &errResp)
		if errResp.Error != "" {
			if errResp.Details != "" {
				return nil, fmt.Errorf("%s: %s", errResp.Error, errResp.Details)
			}
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("VPS Edge Agent error %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return bodyBytes, nil
}

func (m *Manager) DeployVPS(ctx context.Context, cb func(string), opts DeployOptions) error {
	opts.VPSHost = strings.TrimSpace(opts.VPSHost)
	if opts.VPSHost == "" {
		return fmt.Errorf("host / IP VPS tidak boleh kosong")
	}
	if net.ParseIP(opts.VPSHost) == nil && !pdnsHostRe.MatchString(opts.VPSHost) {
		return fmt.Errorf("host / IP VPS tidak valid: %q", opts.VPSHost)
	}
	if opts.SSHPort == 0 {
		opts.SSHPort = 22
	}
	if opts.SSHPort < 1 || opts.SSHPort > 65535 {
		return fmt.Errorf("port SSH tidak valid: %d", opts.SSHPort)
	}
	opts.SSHUser = strings.TrimSpace(opts.SSHUser)
	if opts.SSHUser == "" {
		opts.SSHUser = "root"
	}
	if !sshUserRe.MatchString(opts.SSHUser) {
		return fmt.Errorf("username SSH tidak valid: %q", opts.SSHUser)
	}
	if opts.AuthMethod == "" {
		opts.AuthMethod = "password"
	}
	if opts.AuthMethod != "password" && opts.AuthMethod != "key" {
		return fmt.Errorf("metode autentikasi SSH tidak valid: %q", opts.AuthMethod)
	}
	if opts.AuthMethod == "key" && opts.KeyPath != "" {
		opts.KeyPath = strings.TrimSpace(opts.KeyPath)
		if strings.HasPrefix(opts.KeyPath, "-") || strings.ContainsAny(opts.KeyPath, "\r\n\x00") {
			return fmt.Errorf("path SSH key tidak valid")
		}
		if fi, err := os.Stat(opts.KeyPath); err != nil || fi.IsDir() {
			return fmt.Errorf("file SSH key tidak ditemukan: %s", opts.KeyPath)
		}
	}

	cb(fmt.Sprintf("=== MEMULAI DEPLOYMENT VPS EDGE GATEWAY (%s) ===", opts.VPSHost))

	buildSSH := func(remoteCmd string) *exec.Cmd {
		args := []string{
			"-p", fmt.Sprintf("%d", opts.SSHPort),
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "ConnectTimeout=15",
		}
		if opts.AuthMethod == "key" && opts.KeyPath != "" {
			args = append(args, "-i", opts.KeyPath)
		}
		target := fmt.Sprintf("%s@%s", opts.SSHUser, opts.VPSHost)
		args = append(args, "--", target, remoteCmd)

		if opts.AuthMethod == "password" {
			// Kirim password lewat env SSHPASS (-e) agar tidak tampil di ps / /proc/<pid>/cmdline
			cmd := exec.CommandContext(ctx, "sshpass", append([]string{"-e", "ssh"}, args...)...)
			cmd.Env = append(os.Environ(), "SSHPASS="+opts.Password)
			return cmd
		}
		return exec.CommandContext(ctx, "ssh", args...)
	}

	// 1. Uji koneksi SSH
	cb(fmt.Sprintf("[1/4] Menguji koneksi SSH ke %s@%s:%d...", opts.SSHUser, opts.VPSHost, opts.SSHPort))
	testCmd := buildSSH("echo SSH_OK")
	testOut, err := testCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(testOut), "SSH_OK") {
		return fmt.Errorf("gagal autentikasi SSH ke %s@%s: %s (periksa IP, Port, Username, atau Password)", opts.SSHUser, opts.VPSHost, strings.TrimSpace(string(testOut)))
	}
	cb("  ✓ Autentikasi SSH berhasil terhubung!")

	// 2. Unggah biner wdev (Go) ke VPS lalu jalankan provisioning native.
	cb("[2/4] Memeriksa arsitektur & mengunggah biner wdev...")
	archCmd := buildSSH("uname -m")
	if archOut, err := archCmd.Output(); err == nil {
		remote := strings.TrimSpace(string(archOut))
		mapped := remote
		switch remote {
		case "x86_64":
			mapped = "amd64"
		case "aarch64", "arm64":
			mapped = "arm64"
		}
		if mapped != runtime.GOARCH {
			return fmt.Errorf("arsitektur VPS (%s) berbeda dengan biner wdev lokal (%s); jalankan wdev dari mesin dengan arsitektur yang sama", remote, runtime.GOARCH)
		}
	}
	uploadCmd := buildSSH(`tmp=$(mktemp /tmp/wdev-edge-bin.XXXXXX) && cat > "$tmp" && chmod 755 "$tmp" && mv -f "$tmp" /usr/local/bin/wdev`)
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("gagal menemukan biner wdev: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	binFile, err := os.Open(exePath)
	if err != nil {
		return fmt.Errorf("gagal membuka biner wdev: %w", err)
	}
	defer binFile.Close()
	uploadCmd.Stdin = binFile
	if out, err := uploadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gagal mengunggah biner wdev ke VPS: %s (%w)", string(out), err)
	}
	cb(" ✓ Biner wdev berhasil diunggah.")

	// 3. Jalankan provisioning native di VPS (tanpa skrip shell).
	cb("[3/4] Menjalankan provisioning VPS Edge Gateway (native Go)...")
	provisionRun := "/usr/local/bin/wdev edge-agent provision --yes"
	if opts.SSHUser != "root" {
		provisionRun = "sudo " + provisionRun
	}
	provisionCmd := buildSSH(provisionRun)
	stdout, err := provisionCmd.StdoutPipe()
	if err != nil {
		return err
	}
	provisionCmd.Stderr = provisionCmd.Stdout
	if err := provisionCmd.Start(); err != nil {
		return fmt.Errorf("gagal menjalankan provisioning di VPS: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		cb(scanner.Text())
	}
	if err := provisionCmd.Wait(); err != nil {
		return fmt.Errorf("provisioning di VPS gagal: %w", err)
	}
	cb(" ✓ Provisioning VPS selesai.")

	// 4. Ambil kredensial dari /etc/wdev-edge/edge.conf
	cb("[4/4] Mengambil kredensial dari VPS & mengonfigurasi mesin lokal...")
	catCmd := buildSSH("sudo cat /etc/wdev-edge/edge.conf 2>/dev/null")
	credOut, err := catCmd.Output()
	if err != nil || len(credOut) == 0 {
		return fmt.Errorf("gagal membaca /etc/wdev-edge/edge.conf di VPS: %w", err)
	}

	pubIP := ""
	apiKey := ""
	pdnsPort := "8081"
	agentPort := "8082"
	vpsNetbird := opts.VPSHost

	credScanner := bufio.NewScanner(bytes.NewReader(credOut))
	for credScanner.Scan() {
		l := strings.TrimSpace(credScanner.Text())
		if strings.HasPrefix(l, "#") || !strings.Contains(l, "=") {
			continue
		}
		parts := strings.SplitN(l, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch k {
		case "PUBLIC_IP":
			pubIP = v
		case "API_KEY":
			apiKey = v
		case "PDNS_API_PORT":
			pdnsPort = v
		case "AGENT_PORT":
			agentPort = v
		case "NETBIRD_IP":
			if v != "" {
				vpsNetbird = v
			}
		}
	}

	if apiKey == "" {
		return fmt.Errorf("tidak dapat menemukan API_KEY di /etc/wdev-edge/edge.conf")
	}

	cb(fmt.Sprintf("  -> NetBird IP : %s", vpsNetbird))
	cb(fmt.Sprintf("  -> Public IP  : %s", pubIP))
	cb(fmt.Sprintf("  -> PDNS Port  : %s", pdnsPort))
	cb(fmt.Sprintf("  -> Agent Port : %s", agentPort))

	if err := m.SaveConfig(vpsNetbird, pubIP, apiKey, pdnsPort, agentPort); err != nil {
		return fmt.Errorf("gagal menyimpan konfigurasi lokal: %w", err)
	}
	cb("  ✓ Konfigurasi /etc/wdev/edge-dns.conf lokal berhasil diperbarui.")

	cb("=== DEPLOYMENT VPS EDGE GATEWAY SELESAI DENGAN SUKSES ===")
	return nil
}
