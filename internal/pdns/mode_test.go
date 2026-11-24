package pdns

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
	"wdev/internal/store"
)

func TestNormalizeMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"local", "local", true},
		{"LOKAL", "local", true},
		{"standalone", "local", true},
		{"edge", "edge", true},
		{"hybrid", "edge", true},
		{"vps", "vps", true},
		{"aaPanel", "vps", true},
		{"", "", false},
		{"cloud", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizeMode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeMode(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDetectModeFromSignals(t *testing.T) {
	cases := []struct {
		name string
		sig  modeSignals
		want string
	}{
		{
			name: "default clean host without netbird or public IP",
			sig:  modeSignals{},
			want: "local",
		},
		{
			name: "explicit local overrides netbird and public nic",
			sig: modeSignals{
				explicitMode:   "local",
				localNetbirdIP: "100.64.0.5",
				hasPublicNIC:   true,
			},
			want: "local",
		},
		{
			name: "edge agent config implies vps",
			sig: modeSignals{
				hasEdgeAgentCfg: true,
			},
			want: "vps",
		},
		{
			name: "local pdns without public nic is local",
			sig: modeSignals{
				hasLocalPDNS:   true,
				localNetbirdIP: "100.64.0.9",
			},
			want: "local",
		},
		{
			name: "local pdns with public nic is vps",
			sig: modeSignals{
				hasLocalPDNS: true,
				hasPublicNIC: true,
			},
			want: "vps",
		},
		{
			name: "remote edge-dns host implies edge",
			sig: modeSignals{
				remoteEdgeHost: "100.64.0.10",
			},
			want: "edge",
		},
		{
			name: "netbird only implies edge",
			sig: modeSignals{
				localNetbirdIP: "100.64.0.11",
			},
			want: "edge",
		},
		{
			name: "public nic only implies vps",
			sig: modeSignals{
				hasPublicNIC: true,
			},
			want: "vps",
		},
		{
			name: "public nic with netbird implies vps (vps with admin vpn)",
			sig: modeSignals{
				hasPublicNIC:   true,
				localNetbirdIP: "10.3.35.254",
			},
			want: "vps",
		},
		{
			name: "floating public ip without netbird implies vps",
			sig: modeSignals{
				hasPublicIP: true,
			},
			want: "vps",
		},
		{
			name: "floating public ip with netbird implies edge client",
			sig: modeSignals{
				hasPublicIP:    true,
				localNetbirdIP: "100.64.0.12",
			},
			want: "edge",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectModeFromSignals(tc.sig); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestManagerLocalConfigAndListExposuresWithoutNetbird(t *testing.T) {
	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mgr := New(&config.Config{WdevEtc: etcDir}, st)
	if mgr.IsModeSet() {
		t.Fatal("expected IsModeSet to be false initially")
	}
	if err := mgr.SetMode("invalid"); err == nil {
		t.Fatal("expected SetMode(invalid) to fail")
	}
	if err := mgr.SetMode("local"); err != nil {
		t.Fatalf("SetMode(local) failed: %v", err)
	}
	if !mgr.IsModeSet() || mgr.GetMode() != "local" {
		t.Fatalf("unexpected mode state: isSet=%v mode=%q", mgr.IsModeSet(), mgr.GetMode())
	}

	confData := `VPS_NETBIRD_IP="127.0.0.1"
VPS_PUBLIC_IP="192.168.10.20"
API_KEY="local-secret-key"
PDNS_PORT="8081"
AGENT_PORT="19999"
`
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confData), 0600); err != nil {
		t.Fatal(err)
	}

	conf := mgr.LoadConfig()
	if conf.Mode != "local" {
		t.Fatalf("LoadConfig().Mode = %q, want local", conf.Mode)
	}
	if !conf.Configured {
		t.Fatal("expected LoadConfig().Configured = true")
	}
	if conf.VPSNetbirdIP != "127.0.0.1" || conf.VPSPublicIP != "192.168.10.20" {
		t.Fatalf("unexpected endpoints in conf: %+v", conf)
	}
	if host := mgr.readRemoteEdgeHost(); host != "" {
		t.Fatalf("expected loopback host not to be treated as remote edge, got %q", host)
	}

	exposures, err := mgr.ListExposures()
	if err != nil {
		t.Fatalf("expected ListExposures in local mode without agent to succeed cleanly, got error: %v", err)
	}
	if len(exposures) != 0 {
		t.Fatalf("expected 0 exposures, got %d", len(exposures))
	}

	// Test VPS mode configuration
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatalf("SetMode(vps) failed: %v", err)
	}
	vpsConf := mgr.LoadConfig()
	if vpsConf.Mode != "vps" {
		t.Fatalf("expected mode=vps, got %q", vpsConf.Mode)
	}
	if vpsConf.VPSNetbirdIP != "127.0.0.1" {
		t.Fatalf("expected loopback VPSNetbirdIP in vps mode, got %q", vpsConf.VPSNetbirdIP)
	}
}

func TestModeSwitchingRoundTripPreservesAndIsolatesConfigs(t *testing.T) {
	restorePub := resetExternalPublicIPCacheForTest(func(context.Context) string { return "" })
	defer restorePub()

	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	availDir := filepath.Join(tmp, "nginx", "sites-available")
	confDDir := filepath.Join(tmp, "nginx", "conf.d")
	for _, d := range []string{etcDir, availDir, confDDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mgr := New(&config.Config{WdevEtc: etcDir, NginxSitesAvail: availDir}, st)

	// 1. Edge mode dengan konfigurasi VPS remote -> tersimpan & realip ditulis.
	if err := mgr.SetMode("edge"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveConfig("100.64.0.10", "203.0.113.50", "edge-key-123", "8081", "8082"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(confDDir, "wdev-edge-realip.conf")); err != nil {
		t.Fatalf("expected wdev-edge-realip.conf in edge mode, got err=%v", err)
	}

	// 2. Pindah ke local (tanpa PDNS lokal) -> edge-dns.conf tidak boleh membocorkan IP Edge remote,
	//    realip dihapus, dan edge-remote.conf tetap aman.
	if err := mgr.SetMode("local"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(confDDir, "wdev-edge-realip.conf")); !os.IsNotExist(err) {
		t.Fatalf("expected wdev-edge-realip.conf removed in local mode, err=%v", err)
	}
	localClean := mgr.LoadConfig()
	if localClean.Configured || localClean.VPSNetbirdIP != "" || localClean.VPSPublicIP != "" || localClean.APIKey != "" {
		t.Fatalf("expected local mode isolated from remote edge conf, got %+v", localClean)
	}

	// 3. Simpan konfigurasi PDNS lokal dalam mode local, lalu pindah ke vps & kembali ke local.
	if err := mgr.SaveConfig("127.0.0.1", "192.168.10.20", "local-key-999", "8081", "8082"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatal(err)
	}
	// Simulasikan local-pdns.conf pernah menyimpan IP publik VPS saat mode vps, lalu balik ke local.
	if err := mgr.SaveConfig("127.0.0.1", "198.51.100.77", "local-key-999", "8081", "8082"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetMode("local"); err != nil {
		t.Fatal(err)
	}
	backToLocal := mgr.LoadConfig()
	if !backToLocal.Configured || backToLocal.VPSNetbirdIP != "127.0.0.1" || backToLocal.APIKey != "local-key-999" {
		t.Fatalf("expected local PDNS profile preserved across vps round-trip, got %+v", backToLocal)
	}
	if IsPublicIPv4String(backToLocal.VPSPublicIP) {
		t.Fatalf("expected public VPS IP cleared when returning to local mode, got %q", backToLocal.VPSPublicIP)
	}

	// 4. Kembali ke edge -> koneksi VPS Edge lama (100.64.0.10 / 203.0.113.50 / edge-key-123) pulih otomatis
	//    dan wdev-edge-realip.conf kembali dibuat.
	if err := mgr.SetMode("edge"); err != nil {
		t.Fatal(err)
	}
	restoredEdge := mgr.LoadConfig()
	if !restoredEdge.Configured || restoredEdge.VPSNetbirdIP != "100.64.0.10" ||
		restoredEdge.VPSPublicIP != "203.0.113.50" || restoredEdge.APIKey != "edge-key-123" {
		t.Fatalf("expected edge profile restored on switch back to edge, got %+v", restoredEdge)
	}
	realIPBytes, err := os.ReadFile(filepath.Join(confDDir, "wdev-edge-realip.conf"))
	if err != nil || !strings.Contains(string(realIPBytes), "set_real_ip_from 100.64.0.10;") {
		t.Fatalf("expected wdev-edge-realip.conf restored in edge mode, err=%v content=%q", err, string(realIPBytes))
	}

	// 5. Jika hanya punya profil lokal (tanpa edge-remote.conf & tanpa edge-dns.conf remote)
	//    lalu pindah ke edge, edge tidak boleh mengira 127.0.0.1 lokal adalah Edge Gateway.
	_ = os.Remove(filepath.Join(etcDir, edgeRemoteConfFile))
	_ = os.Remove(filepath.Join(etcDir, activeDNSConfFile))
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveConfig("127.0.0.1", "198.51.100.77", "local-only-key", "8081", "8082"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetMode("edge"); err != nil {
		t.Fatal(err)
	}
	unconfiguredEdge := mgr.LoadConfig()
	if unconfiguredEdge.Configured || unconfiguredEdge.VPSNetbirdIP == "127.0.0.1" {
		t.Fatalf("expected edge mode without remote profile not to treat local 127.0.0.1 as Edge Gateway, got %+v", unconfiguredEdge)
	}

	// 6. Operasi Edge Agent (ExposeSite, Unexpose, AgentPost) wajib ditolak bila bukan mode edge.
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ExposeSite("app.test", "app.example.com", false); err == nil {
		t.Fatal("expected ExposeSite rejected in vps mode")
	}
	if err := mgr.Unexpose("app.example.com", true); err == nil {
		t.Fatal("expected Unexpose rejected in vps mode")
	}
	if _, err := mgr.AgentPost("/api/v1/proxies", map[string]string{}); err == nil {
		t.Fatal("expected AgentPost rejected in vps mode")
	}
}
