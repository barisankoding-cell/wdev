package monitor

import "testing"

func TestParseLoadAvgPercent(t *testing.T) {
	summary, l1, l5, l15, p1, p5, p15 := parseLoadAvg("2.00 1.00 0.50 1/200 1234", 8)
	if summary != "2.00, 1.00, 0.50" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if l1 != 2.00 || l5 != 1.00 || l15 != 0.50 {
		t.Fatalf("unexpected floats: %v %v %v", l1, l5, l15)
	}
	if p1 != 25 || p5 != 13 || p15 != 6 {
		t.Fatalf("unexpected percentages on 8 cores: p1=%d p5=%d p15=%d", p1, p5, p15)
	}

	// Verify >100% overload is reported without artificial truncation.
	_, _, _, _, overPct, _, _ := parseLoadAvg("6.00 4.00 2.00 3/100 99", 4)
	if overPct != 150 {
		t.Fatalf("expected 150%% load for 6.00 on 4 cores, got %d%%", overPct)
	}

	// Verify zero-core fallback uses 1 core safely.
	_, _, _, _, zeroCorePct, _, _ := parseLoadAvg("0.50 0.50 0.50", 0)
	if zeroCorePct != 50 {
		t.Fatalf("expected 50%% fallback with 0 cores, got %d%%", zeroCorePct)
	}
}
