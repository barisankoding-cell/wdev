package pdns

import (
	"os"
	"path/filepath"
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
