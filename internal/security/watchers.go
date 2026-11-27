package security

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// WatcherStatus merangkum status operasional setiap detektor keamanan.
type WatcherStatus struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Running     bool       `json:"running"`
	Source      string     `json:"source"`
	Available   bool       `json:"available"`
	EventsCount int64      `json:"events_count"`
	LastEventAt *time.Time `json:"last_event_at,omitempty"`
	Note        string     `json:"note,omitempty"`
}

// MonitorStatusReport adalah ringkasan konfigurasi, status firewall, status Nginx, dan status detektor.
type MonitorStatusReport struct {
	Policy   PolicyConfig    `json:"policy"`
	Firewall FirewallStatus  `json:"firewall"`
	Nginx    NginxSyncStatus `json:"nginx"`
	Watchers []WatcherStatus `json:"watchers"`
}

type watcherStats struct {
	running     bool
	source      string
	available   bool
	eventsCount int64
	lastEventAt *time.Time
	note        string
}

// NginxAccessEntry merepresentasikan satu baris log akses Nginx yang telah diparsing.
type NginxAccessEntry struct {
	IP        string
	Method    string
	Path      string
	Status    int
	UserAgent string
}

// ProcNetConnection merepresentasikan satu koneksi TCP dari /proc/net/tcp atau tcp6.
type ProcNetConnection struct {
	LocalPort  int
	RemoteIP   string
	RemotePort int
	State      string
}

var (
	sshFailedRe  = regexp.MustCompile(`(?i)(?:Failed password for (?:invalid user )?|Invalid user |Connection closed by (?:authenticating|invalid) user |Disconnected from (?:authenticating|invalid) user |maximum authentication attempts exceeded for (?:invalid user )?)(\S+)?\s*(?:from\s+)?([0-9a-fA-F:\.]+)(?:\s+port\s+\d+)?`)
	sshPamRe     = regexp.MustCompile(`(?i)pam_unix\(sshd:auth\):\s+authentication failure;.*?\brhost=([0-9a-fA-F:\.]+)(?:.*?\buser=(\S+))?`)
	nginxLogRe   = regexp.MustCompile(`^(\S+)\s+\S+\s+\S+\s+\[[^\]]+\]\s+"([A-Z]+)\s+(\S+)(?:\s+HTTP/[^"]+)?"\s+(\d{3})\s+\S+(?:\s+"[^"]*"\s+"([^"]*)")?`)
	fwSrcRe      = regexp.MustCompile(`\bSRC=([0-9a-fA-F:\.]+)\b`)
	fwDptRe      = regexp.MustCompile(`\bDPT=(\d+)\b`)
	fwProtoRe    = regexp.MustCompile(`\bPROTO=(\S+)\b`)
	mailDovecotRe = regexp.MustCompile(`(?i)\brip=([0-9a-fA-F:\.]+)\b`)
	mailPostfixRe = regexp.MustCompile(`(?i)\[([0-9a-fA-F:\.]+)\]:\s+SASL\b`)
	mailGenericRe = regexp.MustCompile(`(?i)\b(?:remote[-_]ip|client[-_]ip|source[-_]?ip|src|ip|from)[=:\s"]+([0-9a-fA-F:\.]+)`)
	mailUserRe    = regexp.MustCompile(`(?i)\buser(?:name)?[=:\s"<]+([^\s,">]+)`)
)

var scannerUserAgents = []string{
	"sqlmap",
	"nikto",
	"nuclei",
	"zgrab",
	"masscan",
	"nmap",
	"gobuster",
	"dirbuster",
	"dirb/",
	"ffuf",
	"wpscan",
	"acunetix",
	"nessus",
	"openvas",
	"feroxbuster",
	"hydra",
	"httpx",
	"whatweb",
	"censys",
}

var alwaysExploitPathPatterns = []string{
	"../",
	"..\\",
	"%2e%2e",
	"/etc/passwd",
	"/etc/shadow",
	"/proc/self/environ",
	"/vendor/phpunit",
	"/eval-stdin.php",
	"/.aws/credentials",
	"/.git/config",
	"/.git/head",
	"/.env",
	"/.htpasswd",
	"/.dockerenv",
	"/cgi-bin/",
	"/boaform/",
	"/hnap1",
	"/shell.php",
	"/cmd.php",
	"/webshell",
	"/wp-config.php~",
	"/wp-config.bak",
	"/server-status",
}

var queryExploitPatterns = []string{
	"../",
	"..\\",
	"%2e%2e",
	"/etc/passwd",
	"/etc/shadow",
	"phpunit",
	"union select",
	"union+select",
	"union%20select",
	"union/**/select",
	"information_schema",
	"sleep(",
	"benchmark(",
	"waitfor delay",
	"waitfor+delay",
	"waitfor%20delay",
	"' or 1=1",
	"' or '1'='1",
	"%27%20or%20",
	"%27+or+",
	"<script",
	"%3cscript",
	"javascript:",
	"php://filter",
	"php://input",
	"expect://",
	"${jndi:",
	"%24%7bjndi:",
	"() { :;",
}

var decoyWhenMissingPatterns = []string{
	"/wp-login.php",
	"/wp-admin",
	"/xmlrpc.php",
	"/phpmyadmin",
	"/pma",
	"/adminer",
	"/actuator",
	"/telescope",
	"/_profiler",
	"/manager/html",
	"/solr/",
	"/.ds_store",
	"/backup.sql",
	"/dump.sql",
	"/database.sql",
}

var staticAssetSuffixes = []string{
	".css", ".js", ".mjs", ".map", ".png", ".jpg", ".jpeg", ".gif",
	".svg", ".ico", ".webp", ".woff", ".woff2", ".ttf", ".eot",
}

// HasScannerUserAgent mendeteksi User-Agent tool pemindai kerentanan / brute force.
func HasScannerUserAgent(ua string) (string, bool) {
	low := strings.ToLower(strings.TrimSpace(ua))
	if low == "" {
		return "", false
	}
	for _, sig := range scannerUserAgents {
		if strings.Contains(low, sig) {
			return sig, true
		}
	}
	return "", false
}

// IsWebExploitPath mendeteksi path traversal, file rahasia, SQLi, XSS, LFI, Log4Shell, dan RCE payload.
func IsWebExploitPath(rawPath string) bool {
	full := strings.ToLower(strings.TrimSpace(rawPath))
	for _, qpat := range queryExploitPatterns {
		if strings.Contains(full, qpat) {
			return true
		}
	}

	clean := full
	if idx := strings.IndexByte(clean, '?'); idx != -1 {
		clean = clean[:idx]
	}
	for _, pat := range alwaysExploitPathPatterns {
		if strings.Contains(clean, pat) {
			return true
		}
	}
	return false
}

// IsNginxDecoyMissingPath mendeteksi endpoint decoy/admin yang mencurigakan saat menghasilkan 403/404.
func IsNginxDecoyMissingPath(rawPath string) bool {
	clean := strings.ToLower(strings.TrimSpace(rawPath))
	if idx := strings.IndexByte(clean, '?'); idx != -1 {
		clean = clean[:idx]
	}
	for _, pat := range decoyWhenMissingPatterns {
		if strings.HasPrefix(clean, pat) || strings.Contains(clean, pat) {
			return true
		}
	}
	return false
}

func isStaticAssetPath(rawPath string) bool {
	clean := strings.ToLower(strings.TrimSpace(rawPath))
	if idx := strings.IndexByte(clean, '?'); idx != -1 {
		clean = clean[:idx]
	}
	for _, ext := range staticAssetSuffixes {
		if strings.HasSuffix(clean, ext) {
			return true
		}
	}
	return false
}

// ParseSSHLogLine mengekstrak IP penyerang dan username dari baris log OpenSSH / PAM.
func ParseSSHLogLine(line string) (ip string, user string, severe bool, ok bool) {
	if !strings.Contains(line, "sshd") && !strings.Contains(strings.ToLower(line), "authentication failure") && !strings.Contains(line, "Invalid user") && !strings.Contains(line, "Failed password") {
		return "", "", false, false
	}
	if m := sshFailedRe.FindStringSubmatch(line); len(m) >= 3 {
		candidateIP := strings.TrimSpace(m[2])
		if parsed := net.ParseIP(candidateIP); parsed != nil {
			user = strings.TrimSpace(m[1])
			if user == "" || user == "from" || user == "user" {
				user = "unknown"
			}
			severe = strings.Contains(strings.ToLower(line), "maximum authentication attempts exceeded")
			return parsed.String(), user, severe, true
		}
	}
	if m := sshPamRe.FindStringSubmatch(line); len(m) >= 2 {
		candidateIP := strings.TrimSpace(m[1])
		if parsed := net.ParseIP(candidateIP); parsed != nil {
			if len(m) >= 3 && strings.TrimSpace(m[2]) != "" {
				user = strings.TrimSpace(m[2])
			} else {
				user = "unknown"
			}
			return parsed.String(), user, false, true
		}
	}
	return "", "", false, false
}

// ParseNginxAccessLine mem-parsing baris access log Nginx format combined.
func ParseNginxAccessLine(line string) (NginxAccessEntry, bool) {
	m := nginxLogRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) < 5 {
		return NginxAccessEntry{}, false
	}
	ip := net.ParseIP(strings.TrimSpace(m[1]))
	if ip == nil {
		return NginxAccessEntry{}, false
	}
	status, err := strconv.Atoi(m[4])
	if err != nil {
		return NginxAccessEntry{}, false
	}
	ua := ""
	if len(m) >= 6 {
		ua = strings.TrimSpace(m[5])
	}
	return NginxAccessEntry{
		IP:        ip.String(),
		Method:    strings.TrimSpace(m[2]),
		Path:      strings.TrimSpace(m[3]),
		Status:    status,
		UserAgent: ua,
	}, true
}

// ParseFirewallLogLine mengekstrak paket masuk yang diblokir oleh UFW / iptables / nftables dari log kernel.
func ParseFirewallLogLine(line string) (srcIP string, dstPort int, proto string, ok bool) {
	upper := strings.ToUpper(line)
	if !strings.Contains(upper, "SRC=") || !strings.Contains(upper, "DPT=") {
		return "", 0, "", false
	}
	if !strings.Contains(upper, "BLOCK") && !strings.Contains(upper, "DROP") && !strings.Contains(upper, "REJECT") {
		return "", 0, "", false
	}
	// Abaikan log chain OUTPUT (IN= kosong) agar koneksi keluar tidak dianggap port scan masuk
	if strings.Contains(upper, "IN= ") || strings.HasSuffix(upper, "IN=") {
		return "", 0, "", false
	}
	sm := fwSrcRe.FindStringSubmatch(line)
	dm := fwDptRe.FindStringSubmatch(line)
	if len(sm) < 2 || len(dm) < 2 {
		return "", 0, "", false
	}
	parsed := net.ParseIP(strings.TrimSpace(sm[1]))
	if parsed == nil {
		return "", 0, "", false
	}
	port, err := strconv.Atoi(dm[1])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, "", false
	}
	proto = "TCP"
	if pm := fwProtoRe.FindStringSubmatch(line); len(pm) >= 2 {
		proto = strings.ToUpper(strings.TrimSpace(pm[1]))
	}
	return parsed.String(), port, proto, true
}

func decodeProcHexAddrPort(addrPort string, isIPv6 bool) (string, int, bool) {
	hostHex, portHex, ok := strings.Cut(strings.TrimSpace(addrPort), ":")
	if !ok {
		return "", 0, false
	}
	portVal, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil || portVal <= 0 || portVal > 65535 {
		return "", 0, false
	}
	raw, err := hex.DecodeString(hostHex)
	if err != nil {
		return "", 0, false
	}
	if !isIPv6 {
		if len(raw) != 4 {
			return "", 0, false
		}
		ip := net.IPv4(raw[3], raw[2], raw[1], raw[0])
		return ip.String(), int(portVal), true
	}
	if len(raw) != 16 {
		return "", 0, false
	}
	out := make([]byte, 16)
	for word := 0; word < 4; word++ {
		off := word * 4
		out[off] = raw[off+3]
		out[off+1] = raw[off+2]
		out[off+2] = raw[off+1]
		out[off+3] = raw[off]
	}
	ip := net.IP(out)
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String(), int(portVal), true
	}
	return ip.String(), int(portVal), true
}

// ParseProcNetConnections mengekstrak port listening non-loopback dan koneksi TCP remote
// dari isi /proc/net/tcp atau /proc/net/tcp6.
// Hanya memantau state ESTABLISHED ("01"), SYN_RECV ("03"), dan TIME_WAIT ("06").
func ParseProcNetConnections(content string, isIPv6 bool) (map[int]bool, []ProcNetConnection) {
	listeningPorts := make(map[int]bool)
	var out []ProcNetConnection
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "sl") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		state := strings.ToUpper(fields[3])
		localIP, localPort, okL := decodeProcHexAddrPort(fields[1], isIPv6)
		if !okL {
			continue
		}
		if state == "0A" {
			if parsedL := net.ParseIP(localIP); parsedL == nil || !parsedL.IsLoopback() {
				listeningPorts[localPort] = true
			}
			continue
		}
		if state != "01" && state != "03" && state != "06" {
			continue
		}
		remoteIP, remotePort, okR := decodeProcHexAddrPort(fields[2], isIPv6)
		if !okR || remotePort == 0 {
			continue
		}
		if remoteIP == "0.0.0.0" || remoteIP == "::" {
			continue
		}
		out = append(out, ProcNetConnection{
			LocalPort:  localPort,
			RemoteIP:   remoteIP,
			RemotePort: remotePort,
			State:      state,
		})
	}
	return listeningPorts, out
}

// slidingEventTracker menyimpan timestamp & item unik per IP untuk agregasi serangan.
type slidingEventTracker struct {
	mu       sync.Mutex
	entries  map[string]*ipWindowState
	cooldown map[string]time.Time
}

type ipWindowState struct {
	timestamps []time.Time
	items      map[string]time.Time
}

func newSlidingEventTracker() *slidingEventTracker {
	return &slidingEventTracker{
		entries:  make(map[string]*ipWindowState),
		cooldown: make(map[string]time.Time),
	}
}

func (t *slidingEventTracker) addAndCheck(ip, item string, now time.Time, window, cool time.Duration) (count int, distinct []string, canEmit bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.entries) > 4096 {
		for k, st := range t.entries {
			if len(st.timestamps) == 0 || now.Sub(st.timestamps[len(st.timestamps)-1]) > window {
				delete(t.entries, k)
			}
		}
	}
	if len(t.cooldown) > 4096 {
		for k, ts := range t.cooldown {
			if now.Sub(ts) > cool*2 {
				delete(t.cooldown, k)
			}
		}
	}

	st, ok := t.entries[ip]
	if !ok {
		st = &ipWindowState{items: make(map[string]time.Time)}
		t.entries[ip] = st
	}

	cutoff := now.Add(-window)
	kept := st.timestamps[:0]
	for _, ts := range st.timestamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	st.timestamps = append(kept, now)

	if item != "" {
		st.items[item] = now
	}
	for k, ts := range st.items {
		if !ts.After(cutoff) {
			delete(st.items, k)
		}
	}

	distinct = make([]string, 0, len(st.items))
	for k := range st.items {
		distinct = append(distinct, k)
	}
	sort.Strings(distinct)

	lastEmit, hasCool := t.cooldown[ip]
	canEmit = !hasCool || now.Sub(lastEmit) >= cool
	return len(st.timestamps), distinct, canEmit
}

func (t *slidingEventTracker) markEmitted(ip string, now time.Time) {
	t.mu.Lock()
	t.cooldown[ip] = now
	t.mu.Unlock()
}

// logTailCursor menjaga posisi baca file log dan menangani rotasi log (logrotate).
type logTailCursor struct {
	path   string
	offset int64
	inode  uint64
	dev    uint64
	init   bool
}

func newLogTailCursor(path string, seekEnd bool) *logTailCursor {
	c := &logTailCursor{path: path}
	if seekEnd {
		if info, err := os.Stat(path); err == nil {
			c.offset = info.Size()
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				c.inode = st.Ino
				c.dev = uint64(st.Dev)
			}
			c.init = true
		}
	}
	return c
}

func (c *logTailCursor) readNewLines() []string {
	info, err := os.Stat(c.path)
	if err != nil {
		return nil
	}
	var ino, dev uint64
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		ino = st.Ino
		dev = uint64(st.Dev)
	}
	if !c.init {
		c.offset = info.Size()
		c.inode = ino
		c.dev = dev
		c.init = true
		return nil
	}
	if info.Size() < c.offset || (c.inode != 0 && (ino != c.inode || dev != c.dev)) {
		c.offset = 0
		c.inode = ino
		c.dev = dev
	}
	if info.Size() == c.offset {
		return nil
	}

	f, err := os.Open(c.path)
	if err != nil {
		return nil
	}
	defer f.Close()

	if _, err := f.Seek(c.offset, io.SeekStart); err != nil {
		return nil
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	var lines []string
	var consumed int64
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if strings.HasSuffix(line, "\n") {
				consumed += int64(len(line))
				trimmed := strings.TrimRight(line, "\r\n")
				if trimmed != "" {
					lines = append(lines, trimmed)
				}
			} else if err == io.EOF {
				// Baris belum selesai ditulis (tanpa newline), tunggu polling berikutnya
				break
			}
		}
		if err != nil {
			break
		}
		if len(lines) >= 2000 {
			break
		}
	}
	c.offset += consumed
	return lines
}

// StartWatchers menjalankan seluruh monitor keamanan latar belakang dan menyinkronkan firewall.
func (m *Manager) StartWatchers(parentCtx context.Context) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	m.mu.Lock()
	if m.watcherCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	m.watcherCancel = cancel
	m.mu.Unlock()

	// Sinkronkan blocked IPs dari database ke firewall host dan Nginx L7 secara asinkron
	go func() {
		syncCtx, cxl := context.WithTimeout(ctx, 10*time.Second)
		defer cxl()
		m.SyncBlockedIPsToFirewall(syncCtx)
		_ = m.SyncNginxProtection(syncCtx)
	}()

	m.watcherWG.Add(4)
	go func() {
		defer m.watcherWG.Done()
		m.runSSHWatcher(ctx)
	}()
	go func() {
		defer m.watcherWG.Done()
		m.runNginxWatcher(ctx)
	}()
	go func() {
		defer m.watcherWG.Done()
		m.runMailWatcher(ctx)
	}()
	go func() {
		defer m.watcherWG.Done()
		m.runPortScanWatcher(ctx)
	}()
}

// StopWatchers menghentikan seluruh monitor latar belakang dan timer sinkronisasi dengan rapi.
func (m *Manager) StopWatchers() {
	m.nginxSyncMu.Lock()
	if m.nginxSyncTimer != nil {
		m.nginxSyncTimer.Stop()
		m.nginxSyncTimer = nil
	}
	m.nginxSyncMu.Unlock()

	m.mu.Lock()
	cancel := m.watcherCancel
	m.watcherCancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		m.watcherWG.Wait()
	}
}

func (m *Manager) updateWatcherState(id string, fn func(st *watcherStats)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.watcherStates[id]
	if !ok {
		st = &watcherStats{}
		m.watcherStates[id] = st
	}
	fn(st)
}

func (m *Manager) recordWatcherHit(watcherID string, ctx context.Context, ev Event) {
	blocked, err := m.RecordAttack(ctx, ev)
	if err == nil {
		now := time.Now()
		m.updateWatcherState(watcherID, func(st *watcherStats) {
			st.eventsCount++
			st.lastEventAt = &now
			_ = blocked
		})
	}
}

func (m *Manager) runSSHWatcher(ctx context.Context) {
	candidates := []string{"/var/log/auth.log", "/var/log/secure"}
	var activeFile string
	for _, p := range candidates {
		if f, err := os.Open(p); err == nil {
			_ = f.Close()
			activeFile = p
			break
		}
	}

	if activeFile != "" {
		m.updateWatcherState("ssh", func(st *watcherStats) {
			st.running = true
			st.available = true
			st.source = activeFile
			st.note = "Memantau log autentikasi SSH secara real-time"
		})
		cursor := newLogTailCursor(activeFile, true)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				m.updateWatcherState("ssh", func(st *watcherStats) { st.running = false })
				return
			case <-ticker.C:
				if !m.GetPolicy().SSHMonitorEnabled {
					continue
				}
				for _, line := range cursor.readNewLines() {
					m.ProcessSSHLine(ctx, line, time.Now())
				}
			}
		}
	}

	// Fallback ke journalctl bila /var/log/auth.log maupun /var/log/secure tidak tersedia
	jPath, err := exec.LookPath("journalctl")
	if err != nil {
		m.updateWatcherState("ssh", func(st *watcherStats) {
			st.running = false
			st.available = false
			st.source = "auth.log / journalctl"
			st.note = "Log SSH tidak ditemukan atau memerlukan izin root untuk membaca /var/log/auth.log"
		})
		return
	}

	m.updateWatcherState("ssh", func(st *watcherStats) {
		st.running = true
		st.available = true
		st.source = "journalctl (ssh/sshd)"
		st.note = "Memantau unit systemd ssh/sshd secara real-time"
	})
	defer m.updateWatcherState("ssh", func(st *watcherStats) { st.running = false })

	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, jPath, "-u", "ssh", "-u", "sshd", "-f", "-n", "0", "-o", "cat")
		stdout, pErr := cmd.StdoutPipe()
		if pErr != nil || cmd.Start() != nil {
			m.updateWatcherState("ssh", func(st *watcherStats) {
				st.available = false
				st.note = "Gagal membaca stream journalctl untuk ssh/sshd"
			})
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				continue
			}
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if m.GetPolicy().SSHMonitorEnabled {
				m.ProcessSSHLine(ctx, scanner.Text(), time.Now())
			}
		}
		_ = cmd.Wait()

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// ProcessSSHLine mengevaluasi satu baris log SSH dan mencatat event SSH_BRUTE_FORCE jika melewati ambang batas.
func (m *Manager) ProcessSSHLine(ctx context.Context, line string, now time.Time) {
	ip, user, severe, ok := ParseSSHLogLine(line)
	if !ok || m.IsWhitelisted(ip) {
		return
	}

	count, users, canEmit := m.sshTracker.addAndCheck(ip, user, now, 2*time.Minute, 25*time.Second)
	if !severe && count < 3 {
		return
	}
	if !canEmit && !severe {
		return
	}
	m.sshTracker.markEmitted(ip, now)

	sev := SeverityHigh
	if count >= 10 {
		sev = SeverityCritical
	}

	m.recordWatcherHit("ssh", ctx, Event{
		Timestamp:  now,
		SourceIP:   ip,
		AttackType: AttackSSHBruteForce,
		Severity:   sev,
		Target:     "ssh:22",
		Details:    fmt.Sprintf("SSH brute-force (%d percobaan gagal dalam 2m; user: %s)", count, strings.Join(users, ", ")),
	})
}

func findNginxAccessLogs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		if name != "access.log" && !strings.Contains(name, "access") {
			continue
		}
		full := filepath.Join(dir, name)
		if f, err := os.Open(full); err == nil {
			_ = f.Close()
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return out
}

func (m *Manager) runNginxWatcher(ctx context.Context) {
	const nginxLogDir = "/var/log/nginx"
	cursors := make(map[string]*logTailCursor)

	refreshFiles := func(seekEnd bool) []string {
		files := findNginxAccessLogs(nginxLogDir)
		activeSet := make(map[string]bool, len(files))
		for _, f := range files {
			activeSet[f] = true
			if _, exists := cursors[f]; !exists {
				cursors[f] = newLogTailCursor(f, seekEnd)
			}
		}
		for old := range cursors {
			if !activeSet[old] {
				delete(cursors, old)
			}
		}
		return files
	}

	files := refreshFiles(true)
	m.updateWatcherState("nginx", func(st *watcherStats) {
		st.running = true
		st.available = len(files) > 0
		if len(files) > 0 {
			st.source = fmt.Sprintf("%s (%d file log)", nginxLogDir, len(files))
			st.note = "Memantau access log Nginx vhost untuk bot scanner & probe"
		} else {
			st.source = nginxLogDir
			st.note = "Belum ada access log Nginx yang dapat dibaca"
		}
	})

	pollTicker := time.NewTicker(1 * time.Second)
	scanTicker := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer scanTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.updateWatcherState("nginx", func(st *watcherStats) { st.running = false })
			return
		case <-scanTicker.C:
			files = refreshFiles(true)
			m.updateWatcherState("nginx", func(st *watcherStats) {
				st.available = len(files) > 0
				if len(files) > 0 {
					st.source = fmt.Sprintf("%s (%d file log)", nginxLogDir, len(files))
					st.note = "Memantau access log Nginx vhost untuk bot scanner & probe"
				}
			})
		case <-pollTicker.C:
			if !m.GetPolicy().NginxMonitorEnabled {
				continue
			}
			for path, cursor := range cursors {
				siteName := strings.TrimSuffix(filepath.Base(path), "-access.log")
				siteName = strings.TrimSuffix(siteName, ".log")
				for _, line := range cursor.readNewLines() {
					m.ProcessNginxLine(ctx, siteName, line, time.Now())
				}
			}
		}
	}
}

// ProcessNginxLine mengevaluasi satu baris access log Nginx dan mencatat WEB_SCANNER / HONEYPOT_PROBE.
func (m *Manager) ProcessNginxLine(ctx context.Context, siteLabel, line string, now time.Time) {
	entry, ok := ParseNginxAccessLine(line)
	if !ok || m.IsWhitelisted(entry.IP) {
		return
	}

	target := entry.Path
	if siteLabel != "" && siteLabel != "access" {
		target = siteLabel + entry.Path
	}

	// 1. User-Agent scanner eksplisit (sqlmap, nikto, nuclei, dll.)
	if sig, isScanner := HasScannerUserAgent(entry.UserAgent); isScanner {
		_, _, canEmit := m.nginxTracker.addAndCheck(entry.IP+"|ua", entry.Path, now, 60*time.Second, 30*time.Second)
		if canEmit {
			m.nginxTracker.markEmitted(entry.IP+"|ua", now)
			m.recordWatcherHit("nginx", ctx, Event{
				Timestamp:  now,
				SourceIP:   entry.IP,
				AttackType: AttackWebScanner,
				Severity:   SeverityHigh,
				Target:     target,
				Details:    fmt.Sprintf("Scanner bot terdeteksi di Nginx (signature: %s, status: %d, UA: %s)", sig, entry.Status, entry.UserAgent),
			})
		}
		return
	}

	// 2. Path eksploitasi / credential leak (/.env, /.git/, ../, phpunit, /etc/passwd)
	if IsWebExploitPath(entry.Path) {
		_, _, canEmit := m.nginxTracker.addAndCheck(entry.IP+"|exploit", entry.Path, now, 60*time.Second, 20*time.Second)
		if canEmit {
			m.nginxTracker.markEmitted(entry.IP+"|exploit", now)
			m.recordWatcherHit("nginx", ctx, Event{
				Timestamp:  now,
				SourceIP:   entry.IP,
				AttackType: AttackHoneypot,
				Severity:   SeverityCritical,
				Target:     target,
				Details:    fmt.Sprintf("Probe file sensitif/eksploit di Nginx (status %d, UA: %s)", entry.Status, entry.UserAgent),
			})
		}
		return
	}

	// 3. Endpoint admin/CMS decoy yang benar-benar tidak ada (403/404) agar WordPress/PMA asli tidak false positive
	if (entry.Status == 403 || entry.Status == 404) && IsNginxDecoyMissingPath(entry.Path) {
		_, _, canEmit := m.nginxTracker.addAndCheck(entry.IP+"|decoy", entry.Path, now, 60*time.Second, 20*time.Second)
		if canEmit {
			m.nginxTracker.markEmitted(entry.IP+"|decoy", now)
			m.recordWatcherHit("nginx", ctx, Event{
				Timestamp:  now,
				SourceIP:   entry.IP,
				AttackType: AttackWebScanner,
				Severity:   SeverityHigh,
				Target:     target,
				Details:    fmt.Sprintf("Pemindaian path CMS/admin yang tidak ada di Nginx (%s -> HTTP %d)", entry.Path, entry.Status),
			})
		}
		return
	}

	// 4. Burst 400/401/403/404 pada non-static paths (>= 15 path unik dalam 60 detik)
	if (entry.Status == 400 || entry.Status == 401 || entry.Status == 403 || entry.Status == 404) && !isStaticAssetPath(entry.Path) {
		count, distinct, canEmit := m.nginxTracker.addAndCheck(entry.IP+"|404", entry.Path, now, 60*time.Second, 45*time.Second)
		if len(distinct) >= 15 && canEmit {
			m.nginxTracker.markEmitted(entry.IP+"|404", now)
			m.recordWatcherHit("nginx", ctx, Event{
				Timestamp:  now,
				SourceIP:   entry.IP,
				AttackType: AttackWebScanner,
				Severity:   SeverityHigh,
				Target:     target,
				Details:    fmt.Sprintf("Burst directory/endpoint scan di Nginx (%d error HTTP pada %d path unik dalam 60d)", count, len(distinct)),
			})
		}
	}
}

func (m *Manager) runPortScanWatcher(ctx context.Context) {
	var fwCursors []*logTailCursor
	var fwSources []string
	for _, p := range []string{"/var/log/ufw.log", "/var/log/kern.log", "/var/log/messages"} {
		if f, err := os.Open(p); err == nil {
			_ = f.Close()
			fwCursors = append(fwCursors, newLogTailCursor(p, true))
			fwSources = append(fwSources, filepath.Base(p))
		}
	}

	var jWG sync.WaitGroup
	defer jWG.Wait()

	var journalKernelSource string
	if len(fwCursors) == 0 {
		if jPath, jErr := exec.LookPath("journalctl"); jErr == nil {
			journalKernelSource = "journalctl -k"
			jWG.Add(1)
			go func() {
				defer jWG.Done()
				for {
					if ctx.Err() != nil {
						return
					}
					cmd := exec.CommandContext(ctx, jPath, "-k", "-f", "-n", "0", "-o", "cat")
					stdout, pErr := cmd.StdoutPipe()
					if pErr != nil || cmd.Start() != nil {
						select {
						case <-ctx.Done():
							return
						case <-time.After(15 * time.Second):
							continue
						}
					}
					scanner := bufio.NewScanner(stdout)
					for scanner.Scan() {
						if m.GetPolicy().PortScanMonitorEnabled {
							m.ProcessFirewallLogLine(ctx, scanner.Text(), time.Now())
						}
					}
					_ = cmd.Wait()
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
				}
			}()
		}
	}

	procReadable := false
	if _, err := os.Stat("/proc/net/tcp"); err == nil {
		procReadable = true
	}

	srcLabel := "/proc/net/tcp"
	if len(fwSources) > 0 {
		srcLabel = fmt.Sprintf("/proc/net/tcp + %s", strings.Join(fwSources, ", "))
	} else if journalKernelSource != "" {
		srcLabel = "/proc/net/tcp + journalctl -k"
	}

	m.updateWatcherState("portscan", func(st *watcherStats) {
		st.running = procReadable || len(fwCursors) > 0 || journalKernelSource != ""
		st.available = procReadable || len(fwCursors) > 0 || journalKernelSource != ""
		st.source = srcLabel
		st.note = "Mendeteksi multi-port probing & SYN flood lewat socket kernel dan log firewall"
	})

	procTicker := time.NewTicker(3 * time.Second)
	logTicker := time.NewTicker(1 * time.Second)
	defer procTicker.Stop()
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.updateWatcherState("portscan", func(st *watcherStats) { st.running = false })
			return
		case <-logTicker.C:
			if !m.GetPolicy().PortScanMonitorEnabled {
				continue
			}
			for _, cur := range fwCursors {
				for _, line := range cur.readNewLines() {
					m.ProcessFirewallLogLine(ctx, line, time.Now())
				}
			}
		case <-procTicker.C:
			if !m.GetPolicy().PortScanMonitorEnabled || !procReadable {
				continue
			}
			listening := make(map[int]bool)
			var conns []ProcNetConnection
			if b4, err := os.ReadFile("/proc/net/tcp"); err == nil {
				lp4, c4 := ParseProcNetConnections(string(b4), false)
				for p := range lp4 {
					listening[p] = true
				}
				conns = append(conns, c4...)
			}
			if b6, err := os.ReadFile("/proc/net/tcp6"); err == nil {
				lp6, c6 := ParseProcNetConnections(string(b6), true)
				for p := range lp6 {
					listening[p] = true
				}
				conns = append(conns, c6...)
			}
			if len(conns) > 0 {
				m.ProcessProcNetSnapshot(ctx, listening, conns, time.Now())
			}
		}
	}
}

// ParseMailLogLine mengekstrak IP sumber dan user dari log gagal autentikasi Stalwart, Postfix, atau Dovecot.
func ParseMailLogLine(line string) (ip string, user string, ok bool) {
	low := strings.ToLower(line)
	if !strings.Contains(low, "auth failed") &&
		!strings.Contains(low, "authentication failed") &&
		!strings.Contains(low, "authentication failure") &&
		!strings.Contains(low, "invalid credentials") &&
		!strings.Contains(low, "failed login") {
		return "", "", false
	}

	var candidateIP string
	for _, re := range []*regexp.Regexp{mailDovecotRe, mailPostfixRe, mailGenericRe} {
		if m := re.FindStringSubmatch(line); len(m) >= 2 {
			candidateIP = strings.TrimSpace(strings.Trim(m[1], `"'[]`))
			if parsed := net.ParseIP(candidateIP); parsed != nil {
				ip = parsed.String()
				break
			}
		}
	}
	if ip == "" {
		return "", "", false
	}

	if um := mailUserRe.FindStringSubmatch(line); len(um) >= 2 {
		user = strings.TrimSpace(strings.Trim(um[1], `"'<>`))
	}
	if user == "" {
		user = "unknown"
	}
	return ip, user, true
}

// ProcessMailLine mengevaluasi satu baris log mail service dan mencatat MAIL_BRUTE_FORCE bila melewati ambang batas.
func (m *Manager) ProcessMailLine(ctx context.Context, line string, now time.Time) {
	ip, user, ok := ParseMailLogLine(line)
	if !ok || m.IsWhitelisted(ip) {
		return
	}

	count, users, canEmit := m.mailTracker.addAndCheck(ip, user, now, 2*time.Minute, 30*time.Second)
	if count < 3 || !canEmit {
		return
	}
	m.mailTracker.markEmitted(ip, now)

	sev := SeverityHigh
	if count >= 10 {
		sev = SeverityCritical
	}

	m.recordWatcherHit("mail", ctx, Event{
		Timestamp:  now,
		SourceIP:   ip,
		AttackType: AttackMailBruteForce,
		Severity:   sev,
		Target:     "mail:smtp/imap",
		Details:    fmt.Sprintf("Mail brute-force auth (%d gagal dalam 2m; user: %s)", count, strings.Join(users, ", ")),
	})
}

func detectMailServicePresence() bool {
	for _, p := range []string{"/etc/stalwart", "/etc/stalwart-mail", "/opt/stalwart-mail", "/etc/postfix", "/etc/dovecot", "/usr/sbin/postfix", "/usr/sbin/dovecot"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	for _, unit := range []string{"stalwart", "stalwart-mail", "postfix", "dovecot"} {
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			return true
		}
	}
	return false
}

func (m *Manager) runMailWatcher(ctx context.Context) {
	var paths []string
	for _, p := range []string{"/var/log/mail.log", "/var/log/maillog"} {
		if f, err := os.Open(p); err == nil {
			_ = f.Close()
			paths = append(paths, p)
		}
	}
	if matches, err := filepath.Glob("/var/log/stalwart*/*.log"); err == nil {
		for _, p := range matches {
			if f, err := os.Open(p); err == nil {
				_ = f.Close()
				paths = append(paths, p)
			}
		}
	}

	if len(paths) > 0 {
		cursors := make([]*logTailCursor, 0, len(paths))
		names := make([]string, 0, len(paths))
		for _, p := range paths {
			cursors = append(cursors, newLogTailCursor(p, true))
			names = append(names, filepath.Base(p))
		}
		m.updateWatcherState("mail", func(st *watcherStats) {
			st.running = true
			st.available = true
			st.source = strings.Join(names, ", ")
			st.note = "Memantau brute-force SMTP/IMAP/JMAP pada Stalwart, Postfix, & Dovecot"
		})
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.updateWatcherState("mail", func(st *watcherStats) { st.running = false })
				return
			case <-ticker.C:
				if !m.GetPolicy().MailMonitorEnabled {
					continue
				}
				for _, cur := range cursors {
					for _, line := range cur.readNewLines() {
						m.ProcessMailLine(ctx, line, time.Now())
					}
				}
			}
		}
	}

	jPath, jErr := exec.LookPath("journalctl")
	if jErr != nil || !detectMailServicePresence() {
		m.updateWatcherState("mail", func(st *watcherStats) {
			st.running = false
			st.available = false
			st.source = "mail.log / stalwart / journalctl"
			st.note = "Layanan mail atau file log mail tidak terdeteksi di host ini"
		})
		return
	}

	m.updateWatcherState("mail", func(st *watcherStats) {
		st.running = true
		st.available = true
		st.source = "journalctl (stalwart/postfix/dovecot)"
		st.note = "Memantau brute-force SMTP/IMAP/JMAP via journalctl"
	})
	defer m.updateWatcherState("mail", func(st *watcherStats) { st.running = false })

	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, jPath, "-u", "stalwart", "-u", "stalwart-mail", "-u", "postfix", "-u", "dovecot", "-f", "-n", "0", "-o", "cat")
		stdout, pErr := cmd.StdoutPipe()
		if pErr != nil || cmd.Start() != nil {
			m.updateWatcherState("mail", func(st *watcherStats) {
				st.available = false
				st.note = "Gagal membaca stream journalctl untuk layanan mail"
			})
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				continue
			}
		}
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if m.GetPolicy().MailMonitorEnabled {
				m.ProcessMailLine(ctx, scanner.Text(), time.Now())
			}
		}
		_ = cmd.Wait()
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// ProcessFirewallLogLine mengevaluasi baris log kernel drop (UFW/iptables) untuk mendeteksi PORT_SCAN.
func (m *Manager) ProcessFirewallLogLine(ctx context.Context, line string, now time.Time) {
	ip, port, proto, ok := ParseFirewallLogLine(line)
	if !ok || m.IsWhitelisted(ip) {
		return
	}

	count, ports, canEmit := m.portTracker.addAndCheck(ip+"|fw", strconv.Itoa(port), now, 60*time.Second, 60*time.Second)
	if len(ports) >= 5 && canEmit {
		m.portTracker.markEmitted(ip+"|fw", now)
		sample := ports
		if len(sample) > 8 {
			sample = sample[:8]
		}
		m.recordWatcherHit("portscan", ctx, Event{
			Timestamp:  now,
			SourceIP:   ip,
			AttackType: AttackPortScan,
			Severity:   SeverityHigh,
			Target:     "ports:" + strings.Join(sample, ","),
			Details:    fmt.Sprintf("Pemindaian port terdeteksi di firewall log (%s: %d paket diblokir pada %d port berbeda dalam 60d)", proto, count, len(ports)),
		})
	}
}

// ProcessProcNetSnapshot memeriksa snapshot /proc/net/tcp untuk mendeteksi multi-port connect scan
// atau SYN flood yang menyasar port listening server (mengabaikan port sumber ephemeral outbound).
func (m *Manager) ProcessProcNetSnapshot(ctx context.Context, listeningPorts map[int]bool, conns []ProcNetConnection, now time.Time) {
	synCountByIP := make(map[string]int)
	portsByIP := make(map[string]map[int]struct{})

	for _, c := range conns {
		if m.IsWhitelisted(c.RemoteIP) {
			continue
		}
		if c.State == "03" {
			synCountByIP[c.RemoteIP]++
		} else if !listeningPorts[c.LocalPort] {
			// Abaikan koneksi outbound yang memakai port sumber ephemeral lokal
			continue
		}
		set, ok := portsByIP[c.RemoteIP]
		if !ok {
			set = make(map[int]struct{})
			portsByIP[c.RemoteIP] = set
		}
		set[c.LocalPort] = struct{}{}
	}

	for ip, synCnt := range synCountByIP {
		if synCnt >= 12 {
			_, _, canEmit := m.portTracker.addAndCheck(ip+"|syn", "syn", now, 60*time.Second, 60*time.Second)
			if canEmit {
				m.portTracker.markEmitted(ip+"|syn", now)
				m.recordWatcherHit("portscan", ctx, Event{
					Timestamp:  now,
					SourceIP:   ip,
					AttackType: AttackPortScan,
					Severity:   SeverityCritical,
					Target:     "tcp:syn_recv",
					Details:    fmt.Sprintf("SYN scan / flood terdeteksi (%d soket SYN_RECV bersamaan)", synCnt),
				})
			}
		}
	}

	for ip, pset := range portsByIP {
		var distinctPorts []string
		var canEmit bool
		for port := range pset {
			_, distinctPorts, canEmit = m.portTracker.addAndCheck(ip+"|proc", strconv.Itoa(port), now, 60*time.Second, 60*time.Second)
		}
		if len(distinctPorts) >= 5 && canEmit {
			m.portTracker.markEmitted(ip+"|proc", now)
			sample := distinctPorts
			if len(sample) > 8 {
				sample = sample[:8]
			}
			m.recordWatcherHit("portscan", ctx, Event{
				Timestamp:  now,
				SourceIP:   ip,
				AttackType: AttackPortScan,
				Severity:   SeverityHigh,
				Target:     "ports:" + strings.Join(sample, ","),
				Details:    fmt.Sprintf("Multi-port TCP probing terdeteksi di host (%d port lokal diakses dalam 60d)", len(distinctPorts)),
			})
		}
	}
}
