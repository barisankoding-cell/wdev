package mail

import (
	"strings"
	"testing"
)

func TestRelayRecordKey(t *testing.T) {
	a := ManagedRelayRecord{Name: "Send.Example.com.", Type: "mx", Content: "feedback-smtp.example.com"}
	b := ManagedRelayRecord{Name: "send.example.com", Type: "MX", Content: "feedback-smtp.example.com"}
	if relayRecordKey(a) != relayRecordKey(b) {
		t.Errorf("expected equal keys, got %q vs %q", relayRecordKey(a), relayRecordKey(b))
	}
	c := ManagedRelayRecord{Name: "send.example.com", Type: "MX", Content: "other.example.com"}
	if relayRecordKey(a) == relayRecordKey(c) {
		t.Error("different content should have different key")
	}
}

func TestRelayDNSReportSummary(t *testing.T) {
	rep := RelayDNSReport{
		Removed: []string{"MX send.example.com"},
		Added:   []string{"CNAME brevo1._domainkey.example.com"},
		Manual:  []string{"pasang DKIM di dashboard"},
	}
	got := rep.summary()
	if got == "" {
		t.Fatal("summary should not be empty")
	}
	for _, want := range []string{"1 record", "dihapus", "dibuat", "manual"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", got, want)
		}
	}
	empty := RelayDNSReport{}
	if empty.summary() != "tidak ada record DNS relay yang dikelola" {
		t.Error("empty report summary mismatch")
	}
}
