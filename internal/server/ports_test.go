package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
	"wdev/internal/pdns"
	"wdev/internal/store"
)

func TestParseProcNetSocketsAndBinds(t *testing.T) {
	// 0100007F:18EB -> 127.0.0.1:6379 (LISTEN = 0A)
	// 00000000:1538 -> 0.0.0.0:5432 (LISTEN = 0A)
	// 189A4464:0C80 -> 100.68.154.24:3200 (LISTEN = 0A)
	// 00000000:0050 -> ESTABLISHED (01) harus diabaikan
	tcp4Sample := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:18EB 00000000:0000 0A 00000000:00000000 00:00000000 00000000   102        0 31325 1 00000000 100 0 0 10 0
   1: 00000000:1538 00000000:0000 0A 00000000:00000000 00:00000000 00000000   988        0 14141 1 00000000 100 0 0 10 0
   2: 189A4464:0C80 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22222 1 00000000 100 0 0 10 0
   3: 00000000:0050 0100007F:9999 01 00000000:00000000 00:00000000 00000000     0        0 33333 1 00000000 100 0 0 10 0
`
	parsed4 := parseProcNetSockets(tcp4Sample, false, "0A")
	if len(parsed4[6379]) != 1 || parsed4[6379][0].String() != "127.0.0.1" {
		t.Fatalf("port 6379 binds = %v, want [127.0.0.1]", parsed4[6379])
	}
	if len(parsed4[5432]) != 1 || parsed4[5432][0].String() != "0.0.0.0" {
		t.Fatalf("port 5432 binds = %v, want [0.0.0.0]", parsed4[5432])
	}
	if len(parsed4[3200]) != 1 || parsed4[3200][0].String() != "100.68.154.24" {
		t.Fatalf("port 3200 binds = %v, want [100.68.154.24]", parsed4[3200])
	}
	if len(parsed4[80]) != 0 {
		t.Fatalf("port 80 ESTABLISHED socket should not be listed as LISTEN: %v", parsed4[80])
	}

	// IPv6: :: port 3200 (0C80) dan ::1 port 6379 (18EB)
	tcp6Sample := `  sl  local_address                         remote_address                        st
   0: 00000000000000000000000000000000:0C80 00000000000000000000000000000000:0000 0A
   1: 00000000000000000000000001000000:18EB 00000000000000000000000000000000:0000 0A
`
	parsed6 := parseProcNetSockets(tcp6Sample, true, "0A")
	hasW, hasL, bindsVPN := analyzeBinds(parsed6[3200], "100.68.154.24")
	if !hasW || !bindsVPN || hasL {
		t.Fatalf("port 3200 [::] analyzeBinds = (wildcard=%v, loopback=%v, vpn=%v), want (true, false, true)", hasW, hasL, bindsVPN)
	}
	hasW6, hasL6, bindsVPN6 := analyzeBinds(parsed6[6379], "100.68.154.24")
	if hasW6 || !hasL6 || bindsVPN6 {
		t.Fatalf("port 6379 [::1] analyzeBinds = (wildcard=%v, loopback=%v, vpn=%v), want (false, true, false)", hasW6, hasL6, bindsVPN6)
	}
}

func TestParseUFWAndFirewalldRules(t *testing.T) {
	ufwOut := `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
80,443/tcp                 ALLOW IN    Anywhere
5432/tcp                   ALLOW IN    100.64.0.0/10              # wdev PostgreSQL (LAN/VPN)
25/tcp                     DENY IN     Anywhere
22/tcp (v6)                ALLOW IN    Anywhere (v6)
`
	ufwSnap := parseUFWStatus(ufwOut, nil)
	if !ufwSnap.info.Active || ufwSnap.info.Engine != "ufw" || ufwSnap.info.DefaultIncoming != "deny" {
		t.Fatalf("unexpected UFW snapshot: %+v", ufwSnap.info)
	}

	d80 := evaluateFirewallForPort(ufwSnap, 80, "tcp", "100.68.154.24")
	if !d80.publicAllowed || !d80.vpnAllowed {
		t.Fatalf("port 80 should be allowed on public and VPN: %+v", d80)
	}
	d5432 := evaluateFirewallForPort(ufwSnap, 5432, "tcp", "100.68.154.24")
	if d5432.publicAllowed || !d5432.vpnAllowed {
		t.Fatalf("port 5432 should allow VPN 100.64.0.0/10 only, block public: %+v", d5432)
	}
	d25 := evaluateFirewallForPort(ufwSnap, 25, "tcp", "100.68.154.24")
	if d25.publicAllowed || d25.vpnAllowed {
		t.Fatalf("port 25 DENY IN Anywhere should block both: %+v", d25)
	}
	d6379 := evaluateFirewallForPort(ufwSnap, 6379, "tcp", "100.68.154.24")
	if d6379.publicAllowed || d6379.vpnAllowed {
		t.Fatalf("port 6379 without allow rule should fall back to default deny: %+v", d6379)
	}

	// Permission denied should mark RulesChecked = false rather than guessing blocks
	ufwNoRoot := parseUFWStatus("ERROR: You need to be root to run this script", errors.New("exit 1"))
	if ufwNoRoot.info.RulesChecked || ufwNoRoot.info.Engine != "ufw" {
		t.Fatalf("expected ufw rules_checked=false on non-root error, got %+v", ufwNoRoot.info)
	}

	// firewalld
	fwdOut := `public (active)
  target: default
  services: http https ssh
  ports: 53/tcp 53/udp
  rich rules:
        rule family="ipv4" source address="100.64.0.0/10" port protocol="tcp" port="5432" accept
`
	fwdSnap := parseFirewalldZone(fwdOut, nil)
	if !fwdSnap.info.Active || fwdSnap.info.Engine != "firewalld" {
		t.Fatalf("unexpected firewalld snapshot: %+v", fwdSnap.info)
	}
	fd5432 := evaluateFirewallForPort(fwdSnap, 5432, "tcp", "100.68.154.24")
	if fd5432.publicAllowed || !fd5432.vpnAllowed {
		t.Fatalf("firewalld 5432 should allow VPN rich rule and block public: %+v", fd5432)
	}
	fd443 := evaluateFirewallForPort(fwdSnap, 443, "tcp", "100.68.154.24")
	if !fd443.publicAllowed || !fd443.vpnAllowed {
		t.Fatalf("firewalld https service should allow port 443: %+v", fd443)
	}
}

func TestParseSSHPortFromConfigs(t *testing.T) {
	if p := parseSSHPortFromConfigs("# Port 22\n"); p != 22 {
		t.Fatalf("default SSH port = %d, want 22", p)
	}
	if p := parseSSHPortFromConfigs("Port 2222\n", "Port 22\n"); p != 2222 {
		t.Fatalf("drop-in SSH port = %d, want 2222", p)
	}
}

func TestEvaluatePortCandidateMatrix(t *testing.T) {
	sb := newSocketBindings()
	sb.tcp[5432] = []net.IP{net.ParseIP("127.0.0.1")} // bind localhost only
	sb.tcp[80] = []net.IP{net.ParseIP("0.0.0.0")}
	sb.tcp[25] = []net.IP{net.ParseIP("0.0.0.0")}
	sb.tcp[8080] = []net.IP{net.ParseIP("127.0.0.1")}

	fw := parseUFWStatus(`Status: active
Default: deny (incoming), allow (outgoing), disabled (routed)
To                         Action      From
--                         ------      ----
80/tcp                     ALLOW IN    Anywhere
25/tcp                     ALLOW IN    Anywhere
`, nil)

	vpsCtx := PortNetworkContext{
		Mode:          "vps",
		LocalIP:       "127.0.0.1",
		VPNIP:         "100.68.154.24",
		VPSPublicIP:   "203.0.113.10",
		NetbirdActive: true,
		Firewall:      fw.info,
	}

	deps := portProbeDeps{
		dialTCPFn: func(host string, port int, timeout time.Duration) bool {
			if port == 443 || port == 6379 {
				return false // not listening
			}
			if port == 5432 || port == 8080 {
				return host == "127.0.0.1" // refuses 100.68.154.24 because bind localhost
			}
			if port == 80 || port == 25 {
				return true
			}
			return false
		},
		probeExternalFn: func(port int, wantIP string, force bool) (bool, bool) {
			if port == 80 {
				return true, true
			}
			if port == 25 {
				return false, true // ISP blocks outbound/inbound 25
			}
			return false, false
		},
	}

	// 1. Port 80 -> Terbuka penuh (Green)
	p80 := evaluatePortCandidate(portCandidate{
		id: "nginx-http", port: 80, protocol: "TCP", service: "nginx", name: "Nginx HTTP",
		category: "web", categoryLabel: "Web & Proxy", installed: true, expectedScope: "public", allowPublic: true, allowVPN: true,
	}, vpsCtx, sb, fw, deps, false)
	if p80.OverallColor != "green" || p80.Local.Color != "green" || p80.VPN.Color != "green" || p80.VPS.Color != "green" {
		t.Fatalf("port 80 expected all green, got %+v", p80)
	}

	// 2. Port 25 -> Listen lokal, terblokir ISP di IP VPS (Yellow)
	p25 := evaluatePortCandidate(portCandidate{
		id: "mail-smtp", port: 25, protocol: "TCP", service: "stalwart", name: "SMTP Inbound",
		category: "mail", categoryLabel: "Mail Server", installed: true, expectedScope: "public", allowPublic: true, allowVPN: true,
	}, vpsCtx, sb, fw, deps, false)
	if p25.OverallColor != "yellow" || p25.Local.Color != "green" || p25.VPS.Color != "yellow" || p25.VPS.Status != "blocked" {
		t.Fatalf("port 25 expected yellow blocked on VPS, got %+v", p25)
	}

	// 3. Port 5432 intended for VPN (`private_vpn`), but bound to 127.0.0.1 -> Yellow VPN refused
	pPGMis := evaluatePortCandidate(portCandidate{
		id: "postgresql", port: 5432, protocol: "TCP", service: "postgresql", name: "PostgreSQL",
		category: "database", categoryLabel: "Database", installed: true, expectedScope: "vpn", allowVPN: true, preferPrivate: true,
	}, vpsCtx, sb, fw, deps, false)
	if pPGMis.OverallColor != "yellow" || pPGMis.Local.Color != "green" || pPGMis.VPN.Color != "yellow" {
		t.Fatalf("port 5432 misconfigured localhost bind expected yellow on VPN, got %+v", pPGMis)
	}

	// 4. Port 5432 bound 0.0.0.0, VPN dial OK, tapi UFW tidak punya rule 5432 -> Yellow UFW blocked!
	sbPGWildcard := newSocketBindings()
	sbPGWildcard.tcp[5432] = []net.IP{net.IPv4zero}
	depsPGWildcard := deps
	depsPGWildcard.dialTCPFn = func(host string, port int, timeout time.Duration) bool {
		return port == 5432
	}
	pPGUFWBlocked := evaluatePortCandidate(portCandidate{
		id: "postgresql", port: 5432, protocol: "TCP", service: "postgresql", name: "PostgreSQL",
		category: "database", categoryLabel: "Database", installed: true, expectedScope: "vpn", allowVPN: true, preferPrivate: true,
	}, vpsCtx, sbPGWildcard, fw, depsPGWildcard, false)
	if pPGUFWBlocked.OverallColor != "yellow" || pPGUFWBlocked.VPN.Status != "blocked" {
		t.Fatalf("port 5432 blocked by UFW expected yellow/blocked on VPN, got %+v", pPGUFWBlocked)
	}

	// 5. Port 6379 dead when installed -> Red
	pRedisDead := evaluatePortCandidate(portCandidate{
		id: "redis", port: 6379, protocol: "TCP", service: "redis", name: "Redis",
		category: "database", categoryLabel: "Database", installed: true, expectedScope: "local",
	}, vpsCtx, sb, fw, deps, false)
	if pRedisDead.OverallColor != "red" || pRedisDead.Local.Color != "red" {
		t.Fatalf("dead installed service expected red, got %+v", pRedisDead)
	}

	// 6. Port 8080 internal only -> Neutral overall, Green local
	p8080 := evaluatePortCandidate(portCandidate{
		id: "mail-admin", port: 8080, protocol: "TCP", service: "stalwart", name: "Stalwart Admin UI",
		category: "mail", categoryLabel: "Mail Server", installed: true, expectedScope: "internal",
	}, vpsCtx, sb, fw, deps, false)
	if p8080.OverallColor != "neutral" || p8080.Local.Color != "green" || p8080.VPS.Status != "internal" {
		t.Fatalf("internal port 8080 expected green local & neutral overall, got %+v", p8080)
	}

	// 7. Port 8082 Edge Agent in edge mode: VPN open, Public closed -> Green overall ("Aman via VPN")
	edgeCtx := PortNetworkContext{
		Mode:        "edge",
		LocalIP:     "127.0.0.1",
		VPNIP:       "100.68.154.24",
		VPSVPNIP:    "100.68.0.10",
		VPSPublicIP: "198.51.100.20",
	}
	depsEdge := portProbeDeps{
		dialTCPFn: func(host string, port int, timeout time.Duration) bool {
			return host == "100.68.0.10" && port == 8082
		},
	}
	pAgent := evaluatePortCandidate(portCandidate{
		id: "edge-agent", port: 8082, protocol: "TCP", service: "edge-agent", name: "wdev Edge Agent",
		category: "dns", categoryLabel: "DNS & Edge Gateway", installed: true, expectedScope: "edge_remote",
		securityAPIGuard: true, allowVPN: true,
	}, edgeCtx, newSocketBindings(), fw, depsEdge, false)
	if pAgent.OverallColor != "green" || pAgent.VPN.Color != "green" || pAgent.VPS.Status != "internal" {
		t.Fatalf("edge agent protected on public IP expected green overall, got %+v", pAgent)
	}
}

func TestHandlePortsEndpointAndEdgeMode(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "ports.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer db.Close()

	// Tambahkan satu proxy site untuk memastikan port proxy kustom ikut terdeteksi
	if err := db.SaveSite(store.SiteRecord{
		Domain:     "app.test",
		RootPath:   "/var/www/app",
		Framework:  "proxy",
		ProxyPort:  4100,
		SSLEnabled: true,
	}); err != nil {
		t.Fatalf("SaveSite: %v", err)
	}

	cfg := &config.Config{
		HTTPHost: "0.0.0.0",
		HTTPPort: "3200",
		WdevEtc:  dir,
	}
	pdnsMgr := pdns.New(cfg, db)
	_ = pdnsMgr.SetMode("local")
	restoreNB := pdns.SetLocalNetbirdIPFn(func() string { return "100.68.154.24" })
	defer restoreNB()

	sb := newSocketBindings()
	sb.tcp[3200] = []net.IP{net.IPv6zero}
	sb.tcp[22] = []net.IP{net.IPv4zero}
	sb.tcp[80] = []net.IP{net.IPv4zero}
	sb.tcp[443] = []net.IP{net.IPv4zero}
	sb.tcp[4100] = []net.IP{net.ParseIP("127.0.0.1")}

	srv := &Server{
		cfg:     cfg,
		store:   db,
		pdnsMgr: pdnsMgr,
		mux:     http.NewServeMux(),
		portDeps: &portProbeDeps{
			readSocketsFn: func() socketBindings { return sb },
			detectFirewallFn: func(ctx context.Context) firewallSnapshot {
				return firewallSnapshot{
					info: PortFirewallInfo{
						Engine:       "none",
						Active:       false,
						RulesChecked: true,
						Summary:      "Tidak ada firewall host aktif",
					},
				}
			},
			dialTCPFn: func(host string, port int, timeout time.Duration) bool {
				return port == 3200 || port == 22 || port == 80 || port == 443 || (port == 4100 && host == "127.0.0.1")
			},
			probeExternalFn: func(port int, wantIP string, force bool) (bool, bool) {
				return false, false
			},
			detectPublicIPFn:    func() string { return "" },
			detectSSHPortFn:     func() (int, bool) { return 22, true },
			hasActiveSSLSitesFn: func() bool { return true },
			isUnitActiveFn: func(name string) bool {
				return name == "nginx" || name == "netbird"
			},
			fileExistsFn: func(p string) bool {
				return strings.Contains(p, "nginx")
			},
			lookPathFn: func(file string) string {
				if file == "nginx" {
					return "/usr/sbin/nginx"
				}
				return ""
			},
		},
	}
	srv.mux.HandleFunc("/api/ports", srv.handlePorts)

	// POST -> 405
	postReq := httptest.NewRequest(http.MethodPost, "/api/ports", nil)
	postRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/ports status = %d, want 405", postRec.Code)
	}

	// GET -> 200
	getReq := httptest.NewRequest(http.MethodGet, "/api/ports?refresh=1", nil)
	getRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/ports status = %d, want 200: %s", getRec.Code, getRec.Body.String())
	}

	var report PortStatusReport
	if err := json.Unmarshal(getRec.Body.Bytes(), &report); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if report.Network.VPNIP != "100.68.154.24" || !report.Network.NetbirdActive {
		t.Fatalf("unexpected network context: %+v", report.Network)
	}

	byID := make(map[string]PortEntry, len(report.Ports))
	for _, p := range report.Ports {
		byID[p.ID] = p
	}
	if p3200, ok := byID["wdev-panel"]; !ok || p3200.OverallColor != "green" || p3200.VPN.Color != "green" {
		t.Fatalf("wdev-panel entry unexpected: %+v", byID["wdev-panel"])
	}
	if pProxy, ok := byID["site-proxy-4100"]; !ok || pProxy.Local.Color != "green" || pProxy.OverallColor != "neutral" {
		t.Fatalf("custom site proxy port 4100 unexpected: %+v", byID["site-proxy-4100"])
	}
	if pPG, ok := byID["postgresql"]; !ok || pPG.Installed || pPG.OverallColor != "neutral" {
		t.Fatalf("uninstalled postgres should be neutral/not_installed: %+v", byID["postgresql"])
	}

	// Uji mode VPS dengan IP publik & DNS UDP+TCP
	_ = pdnsMgr.SetMode("vps")
	_ = pdnsMgr.SaveConfig("127.0.0.1", "203.0.113.50", "local-key", "8081", "8082")
	sbVPS := newSocketBindings()
	sbVPS.tcp[3200] = []net.IP{net.IPv6zero}
	sbVPS.tcp[22] = []net.IP{net.IPv4zero}
	sbVPS.tcp[80] = []net.IP{net.IPv4zero}
	sbVPS.tcp[443] = []net.IP{net.IPv4zero}
	sbVPS.tcp[53] = []net.IP{net.IPv4zero}
	sbVPS.udp[53] = []net.IP{net.IPv4zero}
	srv.portDeps.readSocketsFn = func() socketBindings { return sbVPS }
	srv.portDeps.dialTCPFn = func(host string, port int, timeout time.Duration) bool {
		return port == 3200 || port == 22 || port == 80 || port == 443 || port == 53
	}
	srv.portDeps.probeExternalFn = func(port int, wantIP string, force bool) (bool, bool) {
		if wantIP != "203.0.113.50" {
			return false, false
		}
		if port == 80 || port == 443 || port == 22 || port == 53 {
			return true, true
		}
		if port == 3200 {
			return false, true
		}
		return false, true
	}
	srv.portDeps.fileExistsFn = func(p string) bool {
		return strings.Contains(p, "nginx") || strings.Contains(p, "pdns")
	}

	vpsReq := httptest.NewRequest(http.MethodGet, "/api/ports?refresh=1", nil)
	vpsRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(vpsRec, vpsReq)
	if vpsRec.Code != http.StatusOK {
		t.Fatalf("GET /api/ports in vps mode = %d, want 200", vpsRec.Code)
	}
	var vpsReport PortStatusReport
	if err := json.Unmarshal(vpsRec.Body.Bytes(), &vpsReport); err != nil {
		t.Fatalf("invalid json in vps mode: %v", err)
	}
	if vpsReport.Network.Mode != "vps" || vpsReport.Network.VPSPublicIP != "203.0.113.50" {
		t.Fatalf("unexpected vps network context: %+v", vpsReport.Network)
	}
	vpsByID := make(map[string]PortEntry, len(vpsReport.Ports))
	for _, p := range vpsReport.Ports {
		vpsByID[p.ID] = p
	}
	if dns := vpsByID["pdns-dns"]; !dns.Installed || dns.OverallColor != "green" || dns.VPS.Color != "green" {
		t.Fatalf("pdns-dns expected green in vps mode, got %+v", dns)
	}
	if panel := vpsByID["wdev-panel"]; panel.OverallColor != "yellow" || panel.VPS.Status != "blocked" {
		t.Fatalf("wdev-panel blocked externally expected yellow, got %+v", panel)
	}
}
