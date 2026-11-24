package terminal

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"

	"github.com/creack/pty"
)

const (
	DefaultMaxSessions = 16
	DefaultCols        = 120
	DefaultRows        = 32
	DefaultIdleTimeout = 15 * time.Minute
)

// TargetUser mendeskripsikan akun Linux yang dapat dipilih untuk menjalankan shell PTY.
type TargetUser struct {
	Username string `json:"username"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	HomeDir  string `json:"home_dir"`
	Shell    string `json:"shell"`
	IsRoot   bool   `json:"is_root"`
}

// Info merangkum metadata sistem, user target, shell, dan kebijakan keamanan terminal PTY.
type Info struct {
	Hostname             string       `json:"hostname"`
	OSName               string       `json:"os_name"`
	User                 string       `json:"user"`
	DefaultUser          string       `json:"default_user"`
	AvailableUsers       []TargetUser `json:"available_users"`
	IsDaemonRoot         bool         `json:"is_daemon_root"`
	Shell                string       `json:"shell"`
	AvailableShells      []string     `json:"available_shells"`
	HomeDir              string       `json:"home_dir"`
	ServerMode           string       `json:"server_mode,omitempty"`
	ActiveSessions       int          `json:"active_sessions"`
	MaxSessions          int          `json:"max_sessions"`
	IdleTimeoutSeconds   int          `json:"idle_timeout_seconds"`
	IsHTTPS              bool         `json:"is_https"`
	RootUnlocked         bool         `json:"root_unlocked"`
	RootUnlockExpiresAt  *time.Time   `json:"root_unlock_expires_at,omitempty"`
	StepUpAlwaysRequired bool         `json:"step_up_always_required"`
}

// StartOptions mengonfigurasi pembukaan sesi terminal PTY baru.
type StartOptions struct {
	User        string
	Cwd         string
	Shell       string
	ExecBin     string
	ExecArgs    []string
	ExecLabel   string
	Cols        uint16
	Rows        uint16
	IdleTimeout time.Duration // 0 => DefaultIdleTimeout; < 0 => nonaktif
}

// Session merepresentasikan satu proses shell interaktif berbasis PTY.
type Session struct {
	ID          string
	TargetUser  TargetUser
	Cwd         string
	Shell       string
	User        string
	Hostname    string
	StartedAt   time.Time
	IdleTimeout time.Duration

	cmd           *exec.Cmd
	ptmx          *os.File
	pid           int
	doneCh        chan struct{}
	exitCode      int
	exitErr       error
	closeReasonMu sync.Mutex
	closeReason   string
	lastInputNano atomic.Int64
	stopWatchCh   chan struct{}
	closeOnce     sync.Once
	onClose       func()
}

// Manager mengelola siklus hidup sesi PTY interaktif secara aman.
type Manager struct {
	cfg         *config.Config
	store       *store.Store
	maxSessions int

	mu       sync.Mutex
	sessions map[string]*Session
}

// New membuat instance manajer terminal PTY.
func New(cfg *config.Config, s *store.Store) *Manager {
	return &Manager{
		cfg:         cfg,
		store:       s,
		maxSessions: DefaultMaxSessions,
		sessions:    make(map[string]*Session),
	}
}

func randomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func detectOSName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS + "/" + runtime.GOARCH
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			val = strings.Trim(val, `"'`)
			if val != "" {
				return val
			}
		}
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

type passwdEntry struct {
	username string
	uid      uint32
	gid      uint32
	homeDir  string
	shell    string
}

func readPasswdEntries() []passwdEntry {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []passwdEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		uid64, err1 := strconv.ParseUint(fields[2], 10, 32)
		gid64, err2 := strconv.ParseUint(fields[3], 10, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		entries = append(entries, passwdEntry{
			username: fields[0],
			uid:      uint32(uid64),
			gid:      uint32(gid64),
			homeDir:  fields[5],
			shell:    fields[6],
		})
	}
	return entries
}

func lookupCurrentProcessUser() TargetUser {
	euid := uint32(os.Geteuid())
	egid := uint32(os.Getegid())
	for _, pe := range readPasswdEntries() {
		if pe.uid == euid {
			return TargetUser{
				Username: pe.username,
				UID:      pe.uid,
				GID:      pe.gid,
				HomeDir:  pe.homeDir,
				Shell:    pe.shell,
				IsRoot:   pe.uid == 0,
			}
		}
	}
	username := os.Getenv("USER")
	homeDir, _ := os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		if u.Username != "" {
			username = u.Username
		}
		if u.HomeDir != "" {
			homeDir = u.HomeDir
		}
	}
	if username == "" {
		if euid == 0 {
			username = "root"
		} else {
			username = strconv.FormatUint(uint64(euid), 10)
		}
	}
	if homeDir == "" {
		homeDir = "/"
	}
	return TargetUser{
		Username: username,
		UID:      euid,
		GID:      egid,
		HomeDir:  homeDir,
		Shell:    os.Getenv("SHELL"),
		IsRoot:   euid == 0,
	}
}

func isUsableShell(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	base := strings.ToLower(filepath.Base(p))
	if base == "nologin" || base == "false" || base == "true" || base == "halt" || base == "sync" || base == "shutdown" {
		return false
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0111 != 0
}

// AvailableUsers mengembalikan daftar user Linux yang aman dipakai untuk PTY.
// Ketika wdev berjalan sebagai root, user non-root interaktif diposisikan paling depan (default aman),
// dan root diletakkan di akhir dengan penanda IsRoot=true.
func (m *Manager) AvailableUsers() []TargetUser {
	current := lookupCurrentProcessUser()
	if os.Geteuid() != 0 {
		return []TargetUser{current}
	}

	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	var nonRoot []TargetUser
	var rootEntry *TargetUser

	for _, pe := range readPasswdEntries() {
		if pe.uid == 0 {
			if rootEntry == nil {
				h := pe.homeDir
				if h == "" {
					h = "/root"
				}
				rootEntry = &TargetUser{
					Username: pe.username,
					UID:      pe.uid,
					GID:      pe.gid,
					HomeDir:  h,
					Shell:    pe.shell,
					IsRoot:   true,
				}
			}
			continue
		}
		// Abaikan akun sistem/nobody; ambil akun manusia interaktif (UID 1000..60000).
		if pe.uid < 1000 || pe.uid > 60000 || pe.username == "nobody" {
			continue
		}
		if !isUsableShell(pe.shell) {
			continue
		}
		home := pe.homeDir
		if home == "" {
			home = "/"
		}
		if st, err := os.Stat(home); err != nil || !st.IsDir() {
			continue
		}
		nonRoot = append(nonRoot, TargetUser{
			Username: pe.username,
			UID:      pe.uid,
			GID:      pe.gid,
			HomeDir:  home,
			Shell:    pe.shell,
			IsRoot:   false,
		})
	}

	sort.SliceStable(nonRoot, func(i, j int) bool {
		if sudoUser != "" {
			if nonRoot[i].Username == sudoUser && nonRoot[j].Username != sudoUser {
				return true
			}
			if nonRoot[j].Username == sudoUser && nonRoot[i].Username != sudoUser {
				return false
			}
		}
		return nonRoot[i].UID < nonRoot[j].UID
	})

	if rootEntry == nil {
		r := current
		r.IsRoot = true
		rootEntry = &r
	}

	out := make([]TargetUser, 0, len(nonRoot)+1)
	out = append(out, nonRoot...)
	out = append(out, *rootEntry)
	return out
}

// ResolveTargetUser memvalidasi dan memilih user Linux target untuk sesi PTY.
func (m *Manager) ResolveTargetUser(requested string) (TargetUser, error) {
	users := m.AvailableUsers()
	req := strings.TrimSpace(requested)
	if req == "" {
		return users[0], nil
	}
	for _, u := range users {
		if u.Username == req {
			return u, nil
		}
	}
	return TargetUser{}, fmt.Errorf("akun OS %q tidak diizinkan atau tidak tersedia untuk terminal", requested)
}

// AvailableShellsForUser mengembalikan daftar shell interaktif dengan prioritas shell milik target user.
func (m *Manager) AvailableShellsForUser(preferredShell string) []string {
	candidates := []string{
		preferredShell,
		os.Getenv("SHELL"),
		"/bin/zsh",
		"/usr/bin/zsh",
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/sh",
	}

	if f, err := os.Open("/etc/shells"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			candidates = append(candidates, line)
		}
		_ = f.Close()
	}

	seenReal := make(map[string]bool)
	seenName := make(map[string]bool)
	var out []string
	for _, cand := range candidates {
		cand = strings.TrimSpace(cand)
		if !isUsableShell(cand) {
			continue
		}
		realPath := cand
		if resolved, err := filepath.EvalSymlinks(cand); err == nil {
			realPath = resolved
		}
		base := filepath.Base(cand)
		if seenReal[realPath] || seenName[base] {
			continue
		}
		seenReal[realPath] = true
		seenName[base] = true
		out = append(out, cand)
	}
	if len(out) == 0 {
		out = append(out, "/bin/sh")
	}
	return out
}

// AvailableShells mengembalikan daftar shell interaktif yang tersedia di host.
func (m *Manager) AvailableShells() []string {
	cur := lookupCurrentProcessUser()
	return m.AvailableShellsForUser(cur.Shell)
}

// ResolveShell memvalidasi shell pilihan pengguna agar hanya shell interaktif sah yang dapat dijalankan.
func (m *Manager) ResolveShell(requested, preferredUserShell string) (string, error) {
	avail := m.AvailableShellsForUser(preferredUserShell)
	req := strings.TrimSpace(requested)
	if req == "" {
		return avail[0], nil
	}
	cleanReq := filepath.Clean(req)
	reqReal := cleanReq
	if resolved, err := filepath.EvalSymlinks(cleanReq); err == nil {
		reqReal = resolved
	}
	for _, sh := range avail {
		shReal := sh
		if resolved, err := filepath.EvalSymlinks(sh); err == nil {
			shReal = resolved
		}
		if cleanReq == sh || reqReal == shReal || (filepath.Base(cleanReq) == filepath.Base(sh) && !strings.Contains(req, "/")) {
			return sh, nil
		}
	}
	return "", fmt.Errorf("shell %q tidak tersedia atau tidak diizinkan", requested)
}

func canTargetTraverseDir(dirPath string, target TargetUser) bool {
	if target.UID == 0 || target.UID == uint32(os.Geteuid()) {
		return true
	}
	resolved := dirPath
	if eval, err := filepath.EvalSymlinks(dirPath); err == nil {
		resolved = eval
	}
	gids := map[uint32]bool{target.GID: true}
	if u, err := user.LookupId(strconv.FormatUint(uint64(target.UID), 10)); err == nil {
		if grpIDs, gerr := u.GroupIds(); gerr == nil {
			for _, g := range grpIDs {
				if parsed, perr := strconv.ParseUint(g, 10, 32); perr == nil {
					gids[uint32(parsed)] = true
				}
			}
		}
	}

	cur := "/"
	checkOne := func(p string) bool {
		info, err := os.Stat(p)
		if err != nil {
			return false
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return true
		}
		perm := info.Mode().Perm()
		if st.Uid == target.UID {
			return perm&0100 != 0
		}
		if gids[st.Gid] {
			return perm&0010 != 0
		}
		return perm&0001 != 0
	}

	if !checkOne(cur) {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(resolved, "/"), "/") {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		if !checkOne(cur) {
			return false
		}
	}
	return true
}

// ResolveStartDir memvalidasi working directory awal untuk user target dan memberi fallback yang aman.
func (m *Manager) ResolveStartDir(requested string, target TargetUser) (string, string) {
	fallback := target.HomeDir
	if info, err := os.Stat(fallback); err != nil || !info.IsDir() || !canTargetTraverseDir(fallback, target) {
		if m.cfg != nil && m.cfg.WebRootBase != "" {
			if wi, err := os.Stat(m.cfg.WebRootBase); err == nil && wi.IsDir() && canTargetTraverseDir(m.cfg.WebRootBase, target) {
				fallback = m.cfg.WebRootBase
			} else {
				fallback = "/"
			}
		} else {
			fallback = "/"
		}
	}

	req := strings.TrimSpace(strings.ReplaceAll(requested, "\x00", ""))
	if req == "" {
		return fallback, ""
	}
	clean := filepath.Clean(req)
	if !filepath.IsAbs(clean) {
		return fallback, fmt.Sprintf("Path %q bukan path absolut, dialihkan ke %s", req, fallback)
	}
	info, err := os.Stat(clean)
	if err != nil {
		return fallback, fmt.Sprintf("Direktori %q tidak ditemukan, dialihkan ke %s", clean, fallback)
	}
	targetDir := clean
	if !info.IsDir() {
		parent := filepath.Dir(clean)
		pi, perr := os.Stat(parent)
		if perr != nil || !pi.IsDir() {
			return fallback, fmt.Sprintf("Target %q bukan direktori, dialihkan ke %s", clean, fallback)
		}
		targetDir = parent
	}
	if !canTargetTraverseDir(targetDir, target) {
		return fallback, fmt.Sprintf("Akun %s tidak memiliki izin masuk ke %q, dialihkan ke %s", target.Username, targetDir, fallback)
	}
	return targetDir, ""
}

// SystemInfo mengembalikan ringkasan sistem, daftar user, shell, dan jumlah sesi aktif.
func (m *Manager) SystemInfo() Info {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}
	users := m.AvailableUsers()
	defUser := users[0]
	shells := m.AvailableShellsForUser(defUser.Shell)

	m.mu.Lock()
	active := len(m.sessions)
	maxSess := m.maxSessions
	m.mu.Unlock()

	return Info{
		Hostname:           hostname,
		OSName:             detectOSName(),
		User:               defUser.Username,
		DefaultUser:        defUser.Username,
		AvailableUsers:     users,
		IsDaemonRoot:       os.Geteuid() == 0,
		Shell:              shells[0],
		AvailableShells:    shells,
		HomeDir:            defUser.HomeDir,
		ActiveSessions:     active,
		MaxSessions:        maxSess,
		IdleTimeoutSeconds: int(DefaultIdleTimeout.Seconds()),
	}
}

func shellArgs(shellPath string) []string {
	base := strings.ToLower(filepath.Base(shellPath))
	switch base {
	case "bash", "zsh", "fish":
		return []string{"-l"}
	default:
		return []string{"-i"}
	}
}

func buildTerminalEnv(userName, homeDir, shellPath, cwd string, isRoot bool) []string {
	overrides := map[string]string{
		"TERM":          "xterm-256color",
		"COLORTERM":     "truecolor",
		"HOME":          homeDir,
		"USER":          userName,
		"LOGNAME":       userName,
		"SHELL":         shellPath,
		"PWD":           cwd,
		"WDEV_TERMINAL": "1",
	}
	// wdev.service membawa CARGO_HOME=/usr/local/cargo & CAROOT=/etc/ssl/local-certs/ca
	// (milik root); untuk sesi non-root arahkan tool user ke $HOME agar tidak Permission denied.
	if !isRoot && homeDir != "" {
		overrides["CARGO_HOME"] = filepath.Join(homeDir, ".cargo")
		if os.Getenv("NPM_CONFIG_PREFIX") == "" && os.Getenv("npm_config_prefix") == "" {
			overrides["npm_config_prefix"] = filepath.Join(homeDir, ".npm-global")
		}
	}

	baseEnv := os.Environ()
	merged := make([]string, 0, len(baseEnv)+len(overrides)+2)
	seen := make(map[string]bool)
	hasLang := false
	hasPath := false

	for _, kv := range baseEnv {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		k := kv[:idx]
		if !isRoot && kv == "CAROOT=/etc/ssl/local-certs/ca" {
			continue
		}
		if k == "LANG" {
			hasLang = true
		}
		if k == "PATH" {
			hasPath = true
		}
		if val, ok := overrides[k]; ok {
			merged = append(merged, k+"="+val)
			seen[k] = true
			continue
		}
		merged = append(merged, kv)
		seen[k] = true
	}

	for k, v := range overrides {
		if !seen[k] {
			merged = append(merged, k+"="+v)
		}
	}
	if !hasLang {
		merged = append(merged, "LANG=C.UTF-8")
	}
	if !hasPath {
		merged = append(merged, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return merged
}

func clampDimensions(cols, rows uint16) (uint16, uint16) {
	if cols < 20 || cols > 400 {
		cols = DefaultCols
	}
	if rows < 5 || rows > 200 {
		rows = DefaultRows
	}
	return cols, rows
}

func lookupSupplementaryGroups(uid uint32, primaryGID uint32) []uint32 {
	var groups []uint32
	if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
		if grpIDs, gerr := u.GroupIds(); gerr == nil {
			for _, gs := range grpIDs {
				if parsed, perr := strconv.ParseUint(gs, 10, 32); perr == nil {
					groups = append(groups, uint32(parsed))
				}
			}
		}
	}
	if len(groups) == 0 {
		groups = []uint32{primaryGID}
	}
	return groups
}

// StartSession meluncurkan proses shell interaktif di dalam Linux PTY baru dengan isolasi privilege & idle timeout.
func (m *Manager) StartSession(opts StartOptions) (*Session, string, error) {
	target, err := m.ResolveTargetUser(opts.User)
	if err != nil {
		return nil, "", err
	}
	isExecMode := strings.TrimSpace(opts.ExecBin) != "" && len(opts.ExecArgs) > 0
	shellPath := ""
	if isExecMode {
		resolvedBin, lerr := exec.LookPath(strings.TrimSpace(opts.ExecBin))
		if lerr != nil {
			return nil, "", fmt.Errorf("perintah exec %q tidak ditemukan: %w", opts.ExecBin, lerr)
		}
		shellPath = resolvedBin
	} else {
		shellPath, err = m.ResolveShell(opts.Shell, target.Shell)
		if err != nil {
			return nil, "", err
		}
	}
	cwd, fallbackReason := m.ResolveStartDir(opts.Cwd, target)
	cols, rows := clampDimensions(opts.Cols, opts.Rows)

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = DefaultIdleTimeout
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	sessionID := randomID()

	m.mu.Lock()
	if len(m.sessions) >= m.maxSessions {
		m.mu.Unlock()
		return nil, "", fmt.Errorf("batas maksimum sesi terminal (%d) tercapai", m.maxSessions)
	}
	m.sessions[sessionID] = nil
	m.mu.Unlock()

	ptmx, tty, err := pty.Open()
	if err != nil {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return nil, "", fmt.Errorf("gagal membuka Linux PTY: %w", err)
	}

	if szErr := pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows}); szErr != nil {
		_ = tty.Close()
		_ = ptmx.Close()
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return nil, "", fmt.Errorf("gagal mengatur ukuran PTY: %w", szErr)
	}

	sysAttr := &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}

	// Turunkan hak akses ke akun non-root secara aman bila wdev berjalan sebagai root dan user memilih non-root.
	if os.Geteuid() == 0 && target.UID != 0 {
		_ = os.Chown(tty.Name(), int(target.UID), int(target.GID))
		_ = tty.Chmod(0620)
		sysAttr.Credential = &syscall.Credential{
			Uid:    target.UID,
			Gid:    target.GID,
			Groups: lookupSupplementaryGroups(target.UID, target.GID),
		}
	}

	var cmd *exec.Cmd
	displayShell := shellPath
	if isExecMode {
		cmd = exec.Command(shellPath, opts.ExecArgs...)
		if strings.TrimSpace(opts.ExecLabel) != "" {
			displayShell = strings.TrimSpace(opts.ExecLabel)
		}
	} else {
		cmd = exec.Command(shellPath, shellArgs(shellPath)...)
	}
	cmd.Dir = cwd
	cmd.Env = buildTerminalEnv(target.Username, target.HomeDir, shellPath, cwd, target.IsRoot)
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	cmd.SysProcAttr = sysAttr

	if startErr := cmd.Start(); startErr != nil {
		_ = tty.Close()
		_ = ptmx.Close()
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return nil, "", fmt.Errorf("gagal memulai proses shell PTY: %w", startErr)
	}
	_ = tty.Close()

	sess := &Session{
		ID:          sessionID,
		TargetUser:  target,
		Cwd:         cwd,
		Shell:       displayShell,
		User:        target.Username,
		Hostname:    hostname,
		StartedAt:   time.Now(),
		IdleTimeout: idleTimeout,
		cmd:         cmd,
		ptmx:        ptmx,
		pid:         cmd.Process.Pid,
		doneCh:      make(chan struct{}),
		stopWatchCh: make(chan struct{}),
	}
	sess.lastInputNano.Store(time.Now().UnixNano())
	sess.onClose = func() {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
	}

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	if idleTimeout > 0 {
		go sess.watchIdle(idleTimeout)
	}

	go func() {
		waitErr := cmd.Wait()
		code := 0
		if waitErr != nil {
			code = 1
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				code = exitErr.ExitCode()
			}
		} else if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		sess.exitCode = code
		sess.exitErr = waitErr
		sess.onClose()
		close(sess.doneCh)
	}()

	return sess, fallbackReason, nil
}

func (s *Session) watchIdle(timeout time.Duration) {
	checkEvery := timeout / 4
	if checkEvery < 50*time.Millisecond {
		checkEvery = 50 * time.Millisecond
	}
	if checkEvery > 15*time.Second {
		checkEvery = 15 * time.Second
	}
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopWatchCh:
			return
		case <-s.doneCh:
			return
		case <-ticker.C:
			last := time.Unix(0, s.lastInputNano.Load())
			if time.Since(last) >= timeout {
				_ = s.CloseWithReason("idle_timeout")
				return
			}
		}
	}
}

// Read membaca byte mentah dari PTY master.
func (s *Session) Read(p []byte) (int, error) {
	return s.ptmx.Read(p)
}

// Write menulis byte input dari pengguna ke PTY master dan mereset idle timer.
func (s *Session) Write(p []byte) (int, error) {
	if len(p) > 0 {
		s.lastInputNano.Store(time.Now().UnixNano())
	}
	return s.ptmx.Write(p)
}

// Resize mengubah ukuran jendela PTY sesuai kolom & baris xterm.
func (s *Session) Resize(cols, rows uint16) error {
	c, r := clampDimensions(cols, rows)
	return pty.Setsize(s.ptmx, &pty.Winsize{
		Cols: c,
		Rows: r,
	})
}

// Wait menunggu proses shell berhenti dan mengembalikan exit code.
func (s *Session) Wait() (int, error) {
	<-s.doneCh
	return s.exitCode, s.exitErr
}

// Reason mengembalikan alasan penutupan khusus bila sesi diputus paksa (mis. "idle_timeout").
func (s *Session) Reason() string {
	s.closeReasonMu.Lock()
	defer s.closeReasonMu.Unlock()
	return s.closeReason
}

// CloseWithReason menutup PTY sambil mencatat alasan penutupan.
func (s *Session) CloseWithReason(reason string) error {
	if reason != "" {
		s.closeReasonMu.Lock()
		if s.closeReason == "" {
			s.closeReason = reason
		}
		s.closeReasonMu.Unlock()
	}
	return s.Close()
}

// Close menutup PTY dan menghentikan grup proses shell dengan aman.
func (s *Session) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.stopWatchCh)
		if s.ptmx != nil {
			closeErr = s.ptmx.Close()
		}
		if s.pid > 0 {
			_ = syscall.Kill(-s.pid, syscall.SIGHUP)
			select {
			case <-s.doneCh:
			case <-time.After(250 * time.Millisecond):
				_ = syscall.Kill(-s.pid, syscall.SIGKILL)
			}
		}
		if s.onClose != nil {
			s.onClose()
		}
	})
	return closeErr
}
