package pdns

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

type Manager struct {
	cfg   *config.Config
	store *store.Store
}

type EdgeConfig struct {
	Configured     bool   `json:"configured"`
	VPSNetbirdIP   string `json:"vps_netbird_ip"`
	VPSPublicIP    string `json:"vps_public_ip"`
	APIKey         string `json:"api_key"`
	PDNSPort       string `json:"pdns_port"`
	AgentPort      string `json:"agent_port"`
	LocalNetbirdIP string `json:"local_netbird_ip"`
	Reachable      bool   `json:"reachable"`
}

type ProxyExposure struct {
	Domain     string `json:"domain"`
	Enabled    bool   `json:"enabled"`
	ConfigFile string `json:"config_file"`
	Target     string `json:"target,omitempty"`
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

func (m *Manager) LoadConfig() EdgeConfig {
	confFile := filepath.Join(m.cfg.WdevEtc, "edge-dns.conf")
	data, err := os.ReadFile(confFile)
	if err != nil {
		return EdgeConfig{
			Configured:     false,
			LocalNetbirdIP: m.GetLocalNetbirdIP(),
		}
	}

	conf := EdgeConfig{
		Configured:     true,
		PDNSPort:       "8081",
		AgentPort:      "8082",
		LocalNetbirdIP: m.GetLocalNetbirdIP(),
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
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
			conf.VPSPublicIP = v
		case "API_KEY":
			conf.APIKey = v
		case "PDNS_PORT":
			conf.PDNSPort = v
		case "AGENT_PORT":
			conf.AgentPort = v
		}
	}

	// Check if reachable
	if conf.VPSNetbirdIP != "" && conf.PDNSPort != "" {
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

func (m *Manager) GetLocalNetbirdIP() string {
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

func (m *Manager) ListZones() ([]ZoneInfo, error) {
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return nil, fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
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
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return nil, fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
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

func (m *Manager) AddZone(zoneName string) error {
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
	}

	zoneName = strings.TrimSuffix(strings.TrimSpace(zoneName), ".") + "."

	payload := map[string]any{
		"name":        zoneName,
		"kind":        "Native",
		"nameservers": []string{},
	}
	jsonBytes, _ := json.Marshal(payload)

	client := http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones", conf.VPSNetbirdIP, conf.PDNSPort)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBytes))
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

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API PowerDNS merespons %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (m *Manager) DeleteZone(zoneName string) error {
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
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
	if strings.HasSuffix(name, zClean) {
		return name + "."
	}
	return name + "." + zone
}

func (m *Manager) AddRecord(zone, name, rtype, content string, ttl int) error {
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
	}

	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".") + "."
	fqdn := normalizeRecordName(name, zone)
	rtype = strings.ToUpper(strings.TrimSpace(rtype))

	if ttl <= 0 {
		ttl = 300
	}

	// Format record content according to type
	content = strings.TrimSpace(content)
	if rtype == "TXT" {
		if !strings.HasPrefix(content, "\"") || !strings.HasSuffix(content, "\"") {
			content = `"` + content + `"`
		}
	} else if rtype == "CNAME" {
		if !strings.HasSuffix(content, ".") {
			content = content + "."
		}
	}

	payload := map[string]any{
		"rrsets": []map[string]any{
			{
				"name":       fqdn,
				"type":       rtype,
				"ttl":        ttl,
				"changetype": "REPLACE",
				"records": []map[string]any{
					{
						"content":  content,
						"disabled": false,
					},
				},
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
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
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
	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		return nil, fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
	}

	agentPort := conf.AgentPort
	if agentPort == "" {
		agentPort = "8082"
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

	return res.Proxies, nil
}

func (m *Manager) FindMatchingZone(domain string) (string, error) {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	zones, err := m.ListZones()
	if err != nil {
		parts := strings.Split(cleanDom, ".")
		if len(parts) >= 2 {
			return strings.Join(parts[len(parts)-2:], "."), nil
		}
		return cleanDom, nil
	}

	var candidate string
	for _, z := range zones {
		zClean := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(z.Name), "."))
		if cleanDom == zClean || strings.HasSuffix(cleanDom, "."+zClean) {
			if len(zClean) > len(candidate) {
				candidate = zClean
			}
		}
	}

	if candidate == "" {
		parts := strings.Split(cleanDom, ".")
		if len(parts) >= 2 {
			candidate = strings.Join(parts[len(parts)-2:], ".")
		} else {
			candidate = cleanDom
		}
	}

	return candidate, nil
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

	// 1. Auto-configure DNS Record in PowerDNS if matching zone exists
	zone, _ := m.FindMatchingZone(domain)
	if zone != "" && conf.VPSPublicIP != "" {
		_ = m.AddRecord(zone, domain, "A", conf.VPSPublicIP, 300)
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

func (m *Manager) ExposeSite(localDomain, publicDomain string, useSSL bool) error {
	localDomain = strings.ToLower(strings.TrimSpace(localDomain))
	publicDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(publicDomain), "."))

	if localDomain == "" || publicDomain == "" {
		return fmt.Errorf("domain lokal dan domain publik tidak boleh kosong")
	}

	// Check config file in sites-available
	confPath := filepath.Join("/etc/nginx/sites-available", localDomain+".conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		confPath = filepath.Join("/etc/nginx/sites-available", localDomain)
		if _, err := os.Stat(confPath); os.IsNotExist(err) {
			return fmt.Errorf("konfigurasi vhost lokal untuk '%s' tidak ditemukan di /etc/nginx/sites-available", localDomain)
		}
	}

	// Configure local vhost with alias and reverse proxy headers
	if err := configureLocalVhost(confPath, publicDomain); err != nil {
		return fmt.Errorf("gagal mengonfigurasi vhost lokal: %w", err)
	}

	// Reload nginx
	if err := reloadNginx(); err != nil {
		return fmt.Errorf("gagal me-reload Nginx lokal: %w", err)
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

	// Check if pubDom already present in server_name
	reWord := regexp.MustCompile(`server_name[^;]*\b` + regexp.QuoteMeta(pubDom) + `\b`)
	if !reWord.MatchString(text) {
		// Remove standalone port 80 redirect block if any
		reStandalone := regexp.MustCompile(`(?s)server\s*\{[^{}]*listen\s+80[^{}]*return\s+301\s+https://\$host\$request_uri;\s*\}\s*`)
		text = reStandalone.ReplaceAllString(text, "")

		// Ensure listen 80 is present in the first server block
		if !strings.Contains(text, "listen 80;") {
			reServer := regexp.MustCompile(`(?m)(server\s*\{[ \t]*\n)`)
			text = reServer.ReplaceAllString(text, "${1}    listen 80;\n    listen [::]:80;\n")
		}

		// Add alias to server_name
		reServerName := regexp.MustCompile(`(?m)(server_name\s+[^;]+);`)
		text = reServerName.ReplaceAllString(text, "${1} "+pubDom+";")

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

	return nil
}

func removeLocalVhostAlias(pubDom string) {
	matches, err := filepath.Glob("/etc/nginx/sites-available/*")
	if err != nil {
		return
	}

	for _, file := range matches {
		if strings.HasSuffix(file, ".bak") || strings.HasSuffix(file, "~") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		text := string(data)
		reWord := regexp.MustCompile(`\b` + regexp.QuoteMeta(pubDom) + `\b`)
		if reWord.MatchString(text) {
			newText := reWord.ReplaceAllString(text, "")
			reClean := regexp.MustCompile(`server_name\s+([^;]+);`)
			newText = reClean.ReplaceAllStringFunc(newText, func(match string) string {
				fields := strings.Fields(match)
				return strings.Join(fields, " ")
			})

			_ = writeFileWithSudo(file, []byte(newText))
		}
	}
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
