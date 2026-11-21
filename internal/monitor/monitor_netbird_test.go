package monitor

import "testing"

func TestBuildNetbirdServiceStatusUsesIndependentComponent(t *testing.T) {
	st := buildNetbirdServiceStatus(true, "v0.83.1", true, "0.83.2")
	if st.Name != "netbird" {
		t.Fatalf("Name = %q, want netbird", st.Name)
	}
	if st.InstallComponent != "netbird" {
		t.Fatalf("InstallComponent = %q, want netbird (must not require pdns)", st.InstallComponent)
	}
	if !st.HasUpdate {
		t.Fatalf("expected HasUpdate=true for v0.83.1 -> 0.83.2")
	}

	upToDate := buildNetbirdServiceStatus(true, "v0.83.2", true, "0.83.2")
	if upToDate.HasUpdate {
		t.Fatalf("expected HasUpdate=false when versions match")
	}
}

func TestBuildPDNSStatusLoopbackIsLocalNotRemoteEdge(t *testing.T) {
	st := buildPDNSStatusFromEdgeConf("127.0.0.1", "8081", "v4.9.1", false)
	if st.IsRemote {
		t.Fatalf("loopback edge-dns.conf should not mark IsRemote=true")
	}
	if st.DisplayName != "PowerDNS Authoritative" {
		t.Fatalf("DisplayName = %q, want PowerDNS Authoritative", st.DisplayName)
	}
	if st.StatusText != "Stopped (Local)" {
		t.Fatalf("StatusText = %q, want Stopped (Local)", st.StatusText)
	}

	remote := buildPDNSStatusFromEdgeConf("100.64.0.10", "8081", "v4.9.1", true)
	if !remote.IsRemote || !remote.Active || remote.DisplayName != "PowerDNS Authoritative (VPS Edge)" {
		t.Fatalf("unexpected remote edge pdns status: %+v", remote)
	}
}
