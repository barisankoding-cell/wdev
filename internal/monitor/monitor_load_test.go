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

func TestParseProcStatAndCalculateCoresUsage(t *testing.T) {
	sampleStat1 := `cpu  1000 200 300 5000 100 0 50 20 0 0
cpu0 500 100 150 2500 50 0 25 10 0 0
cpu1 500 100 150 2500 50 0 25 10 0 0
intr 12345
`
	sampleStat2 := `cpu  1100 200 350 5050 100 0 60 20 0 0
cpu0 580 100 170 2520 50 0 30 10 0 0
cpu1 520 100 160 2580 50 0 25 10 0 0
intr 12350
`
	s1 := parseProcStat(sampleStat1)
	if len(s1) != 2 {
		t.Fatalf("expected 2 cores parsed, got %d", len(s1))
	}
	if _, ok := s1[0]; !ok {
		t.Fatalf("expected cpu0 in s1")
	}
	if _, ok := s1[1]; !ok {
		t.Fatalf("expected cpu1 in s1")
	}

	s2 := parseProcStat(sampleStat2)
	if len(s2) != 2 {
		t.Fatalf("expected 2 cores parsed, got %d", len(s2))
	}

	usages := calculateCoresUsage(s1, s2)
	if len(usages) != 2 {
		t.Fatalf("expected 2 core usages calculated, got %d", len(usages))
	}
	if usages[0].Core != 0 || usages[1].Core != 1 {
		t.Fatalf("expected sorted cores 0 and 1, got %+v", usages)
	}
	// Core 0:
	// prev: nonIdle=785, idle=2550, total=3335
	// curr: nonIdle=890, idle=2570, total=3460
	// totalDelta = 125, idleDelta = 20, usedDelta = 105 -> 105/125 = 84%
	if usages[0].Percent != 84 {
		t.Fatalf("expected 84%% on core 0, got %d%%", usages[0].Percent)
	}
	// Core 1:
	// prev: nonIdle=785, idle=2550, total=3335
	// curr: nonIdle=815, idle=2630, total=3445
	// totalDelta = 110, idleDelta = 80, usedDelta = 30 -> 30/110 = 27%
	if usages[1].Percent != 27 {
		t.Fatalf("expected 27%% on core 1, got %d%%", usages[1].Percent)
	}
}
