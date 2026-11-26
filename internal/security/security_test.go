package security

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"wdev/internal/store"
)

func TestSecurityManagerStrikesAndAutoBan(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_mgr.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	mgr := New(db)
	mgr.firewallEnabled = false // disable real shell firewall calls during test

	ctx := context.Background()

	// 1. Critical attack should immediately trigger auto-ban for public IP
	attackerIP := "203.0.113.10"
	evCrit := Event{
		SourceIP:   attackerIP,
		AttackType: AttackHoneypot,
		Severity:   SeverityCritical,
		Target:     "/.env",
		Details:    "Scanner bot hit",
	}

	blocked, err := mgr.RecordAttack(ctx, evCrit)
	if err != nil {
		t.Fatalf("RecordAttack failed: %v", err)
	}
	if !blocked {
		t.Fatalf("expected critical attack to immediately block attacker IP")
	}

	isB, rec, err := mgr.IsIPBlocked(attackerIP)
	if err != nil || !isB || rec == nil {
		t.Fatalf("expected %s to be recorded as blocked: isB=%v rec=%+v err=%v", attackerIP, isB, rec, err)
	}

	// 2. High severity attack accumulation (3 strikes + 3 strikes = 6 strikes -> auto-ban)
	attacker2 := "198.51.100.5"
	evHigh := Event{
		SourceIP:   attacker2,
		AttackType: AttackPanelBruteForce,
		Severity:   SeverityHigh,
		Target:     "/api/auth/login",
		Details:    "Invalid password 3x",
	}

	blocked1, _ := mgr.RecordAttack(ctx, evHigh)
	if blocked1 {
		t.Fatalf("first High strike should not block immediately")
	}

	blocked2, _ := mgr.RecordAttack(ctx, evHigh)
	if !blocked2 {
		t.Fatalf("second High strike (total 6 weight) should trigger auto-ban")
	}

	isB2, _, _ := mgr.IsIPBlocked(attacker2)
	if !isB2 {
		t.Fatalf("expected attacker2 to be blocked after 2 high severity events")
	}

	// 3. Unblock IP
	if err := mgr.UnblockIP(ctx, attacker2); err != nil {
		t.Fatalf("UnblockIP failed: %v", err)
	}
	isB2After, _, _ := mgr.IsIPBlocked(attacker2)
	if isB2After {
		t.Fatalf("expected attacker2 to be unblocked")
	}

	// 4. Local IP should never be blocked even on critical attack
	localIP := "127.0.0.1"
	evLocal := Event{
		SourceIP:   localIP,
		AttackType: AttackHoneypot,
		Severity:   SeverityCritical,
		Target:     "/.git/config",
	}
	blockedLocal, _ := mgr.RecordAttack(ctx, evLocal)
	if blockedLocal {
		t.Fatalf("local IP must never be blocked")
	}
	isBLocal, _, _ := mgr.IsIPBlocked(localIP)
	if isBLocal {
		t.Fatalf("local IP in database blocked status should be false")
	}
}

func TestSecurityManagerSubscription(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_sub.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	mgr := New(db)
	mgr.firewallEnabled = false

	ch, unsub := mgr.Subscribe()
	defer unsub()

	ev := Event{
		SourceIP:   "198.51.100.99",
		AttackType: AttackSecurityEntrance,
		Severity:   SeverityMedium,
		Target:     "/login",
		Details:    "Missing security entrance cookie",
	}

	_, err = mgr.RecordAttack(context.Background(), ev)
	if err != nil {
		t.Fatalf("RecordAttack failed: %v", err)
	}

	select {
	case received := <-ch:
		if received.SourceIP != "198.51.100.99" || received.AttackType != AttackSecurityEntrance {
			t.Fatalf("unexpected received event: %+v", received)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for broadcast event")
	}
}

func TestResolveCountryAndLocalCheck(t *testing.T) {
	if !IsPrivateOrLoopback("127.0.0.1") {
		t.Errorf("expected 127.0.0.1 to be private/loopback")
	}
	if !IsPrivateOrLoopback("10.0.0.1") {
		t.Errorf("expected 10.0.0.1 to be private/loopback")
	}
	if !IsPrivateOrLoopback("192.168.1.10") {
		t.Errorf("expected 192.168.1.10 to be private/loopback")
	}
	if !IsPrivateOrLoopback("100.64.0.5") {
		t.Errorf("expected 100.64.0.5 to be CGNAT/NetBird")
	}
	if IsPrivateOrLoopback("8.8.8.8") {
		t.Errorf("expected 8.8.8.8 to be public")
	}

	country := ResolveCountry("127.0.0.1")
	if country != "LAN" {
		t.Errorf("expected LAN for local IP, got %s", country)
	}
}
