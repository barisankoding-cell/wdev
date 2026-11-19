package installer

import (
	"context"
	"testing"
	"time"
)

func TestToolchains(t *testing.T) {
	inst := &Installer{
		isDebian: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Initial call (refresh)
	start := time.Now()
	statuses, err := inst.GetToolchainStatuses(ctx, true)
	if err != nil {
		t.Fatalf("GetToolchainStatuses error: %v", err)
	}
	t.Logf("Initial fetch took %v", time.Since(start))

	if len(statuses) != 8 {
		t.Fatalf("expected 8 toolchain items, got %d", len(statuses))
	}

	// 2. Cached call
	startCached := time.Now()
	cachedStatuses, err := inst.GetToolchainStatuses(ctx, false)
	if err != nil {
		t.Fatalf("Cached GetToolchainStatuses error: %v", err)
	}
	cachedDuration := time.Since(startCached)
	t.Logf("Cached fetch took %v", cachedDuration)

	if cachedDuration > 500*time.Millisecond {
		t.Errorf("cached call took too long: %v", cachedDuration)
	}

	for _, s := range cachedStatuses {
		t.Logf("[%s] %s | Installed: %v (%s) | Latest: %s | HasUpdate: %v | Extra: %s",
			s.ID, s.Name, s.Installed, s.InstalledVersion, s.LatestVersion, s.HasUpdate, s.ExtraInfo)
	}
}
