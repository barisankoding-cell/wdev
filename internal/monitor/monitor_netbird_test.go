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
