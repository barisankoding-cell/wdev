package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSecurityEventsAndStats(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "sec_test.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	now := time.Now()
	ev1 := SecurityEventRecord{
		Timestamp:   now,
		SourceIP:    "1.2.3.4",
		CountryCode: "ID",
		AttackType:  "PANEL_BRUTE_FORCE",
		Severity:    "HIGH",
		Target:      "/api/auth/login",
		ActionTaken: "RATE_LIMITED",
		Details:     "Invalid password 5x",
	}
	id1, err := db.SaveSecurityEvent(ev1)
	if err != nil {
		t.Fatalf("SaveSecurityEvent failed: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive ID, got %d", id1)
	}

	ev2 := SecurityEventRecord{
		Timestamp:   now,
		SourceIP:    "5.6.7.8",
		CountryCode: "US",
		AttackType:  "HONEYPOT_PROBE",
		Severity:    "CRITICAL",
		Target:      "/.env",
		ActionTaken: "BLOCKED",
		Details:     "Scanner probe",
	}
	id2, err := db.SaveSecurityEvent(ev2)
	if err != nil {
		t.Fatalf("SaveSecurityEvent 2 failed: %v", err)
	}
	if id2 <= id1 {
		t.Fatalf("expected id2 > id1, got id2=%d id1=%d", id2, id1)
	}

	// Test List
	events, total, err := db.ListSecurityEvents(10, 0, "", "", "")
	if err != nil {
		t.Fatalf("ListSecurityEvents failed: %v", err)
	}
	if total != 2 || len(events) != 2 {
		t.Fatalf("expected 2 events, got total=%d len=%d", total, len(events))
	}
	if events[0].ID != id2 {
		t.Fatalf("expected newest event first, got ID=%d", events[0].ID)
	}

	// Test Filter by attack type
	filtered, totalF, err := db.ListSecurityEvents(10, 0, "HONEYPOT_PROBE", "", "")
	if err != nil {
		t.Fatalf("ListSecurityEvents with filter failed: %v", err)
	}
	if totalF != 1 || len(filtered) != 1 || filtered[0].SourceIP != "5.6.7.8" {
		t.Fatalf("unexpected filtered events: %+v", filtered)
	}

	// Test Filter by search
	searchRes, totalS, err := db.ListSecurityEvents(10, 0, "", "", "1.2.3.4")
	if err != nil {
		t.Fatalf("ListSecurityEvents with search failed: %v", err)
	}
	if totalS != 1 || len(searchRes) != 1 || searchRes[0].SourceIP != "1.2.3.4" {
		t.Fatalf("unexpected search result: %+v", searchRes)
	}

	// Test Blocked IP
	exp := now.Add(1 * time.Hour)
	err = db.SaveBlockedIP(BlockedIPRecord{
		IP:        "5.6.7.8",
		Reason:    "Honeypot hit",
		BlockedAt: now,
		ExpiresAt: &exp,
	})
	if err != nil {
		t.Fatalf("SaveBlockedIP failed: %v", err)
	}

	blocked, rec, err := db.IsIPBlocked("5.6.7.8")
	if err != nil {
		t.Fatalf("IsIPBlocked failed: %v", err)
	}
	if !blocked || rec == nil || rec.Reason != "Honeypot hit" {
		t.Fatalf("expected 5.6.7.8 to be blocked, got %v (%+v)", blocked, rec)
	}

	blockedList, err := db.ListBlockedIPs()
	if err != nil {
		t.Fatalf("ListBlockedIPs failed: %v", err)
	}
	if len(blockedList) != 1 || blockedList[0].IP != "5.6.7.8" {
		t.Fatalf("unexpected blocked list: %+v", blockedList)
	}

	// Test Stats
	stats, err := db.GetSecurityStats()
	if err != nil {
		t.Fatalf("GetSecurityStats failed: %v", err)
	}
	if stats.TotalToday != 2 {
		t.Fatalf("expected TotalToday=2, got %d", stats.TotalToday)
	}
	if stats.TotalBlocked != 1 {
		t.Fatalf("expected TotalBlocked=1, got %d", stats.TotalBlocked)
	}
	if stats.CriticalCount != 1 {
		t.Fatalf("expected CriticalCount=1, got %d", stats.CriticalCount)
	}
	if stats.HighCount != 1 {
		t.Fatalf("expected HighCount=1, got %d", stats.HighCount)
	}

	// Test Unblock
	if err := db.DeleteBlockedIP("5.6.7.8"); err != nil {
		t.Fatalf("DeleteBlockedIP failed: %v", err)
	}
	blocked, _, _ = db.IsIPBlocked("5.6.7.8")
	if blocked {
		t.Fatalf("expected 5.6.7.8 to be unblocked")
	}

	// Test Clear All Events
	if err := db.ClearAllSecurityEvents(); err != nil {
		t.Fatalf("ClearAllSecurityEvents failed: %v", err)
	}
	eventsAfter, totalAfter, _ := db.ListSecurityEvents(10, 0, "", "", "")
	if totalAfter != 0 || len(eventsAfter) != 0 {
		t.Fatalf("expected 0 events after clear, got %d", totalAfter)
	}
}
