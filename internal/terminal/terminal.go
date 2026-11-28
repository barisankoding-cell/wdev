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
	DefaultMaxSessions     = 16
	DefaultCols            = 120
	DefaultRows            = 32
	DefaultIdleTimeout     = 15 * time.Minute
	DefaultDetachTimeout   = 30 * time.Minute
	DefaultIdleWarning     = 60 * time.Second
	DefaultScrollbackBytes = 256 * 1024
)

// TargetUser mendeskripsikan akun Linux yang dapat dipilih untuk menjalankan shell PTY.
type TargetUser = config.SystemUser

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
	DetachTimeoutSeconds int          `json:"detach_timeout_seconds"`
	IsHTTPS              bool         `json:"is_https"`
	RootUnlocked         bool         `json:"root_unlocked"`
	RootUnlockExpiresAt  *time.Time   `json:"root_unlock_expires_at,omitempty"`
	StepUpAlwaysRequired bool         `json:"step_up_always_required"`
	TOTPRequired         bool         `json:"totp_required"`
}

// SessionSnapshot merangkum status sesi PTY yang sedang berjalan atau detached.
type SessionSnapshot struct {
	ID                 string     `json:"session_id"`
	PanelUser          string     `json:"panel_user,omitempty"`
	User               string     `json:"user"`
	IsRoot             bool       `json:"is_root"`
	Shell              string     `json:"shell"`
	Cwd                string     `json:"cwd"`
	Hostname           string     `json:"hostname"`
	ExecLabel          string     `json:"exec_label,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	LastActivityAt     time.Time  `json:"last_activity_at"`
	IdleTimeoutSeconds int        `json:"idle_timeout_seconds"`
	IdleRemainingSec   int        `json:"idle_remaining_seconds"`
	DetachTimeoutSec   int        `json:"detach_timeout_seconds"`
	KeepAlive          bool       `json:"keep_alive"`
	Attached           bool       `json:"attached"`
	DetachedAt         *time.Time `json:"detached_at,omitempty"`
	Exited             bool       `json:"exited"`
	ExitCode           int        `json:"exit_code,omitempty"`
}

// SubscriberEvent merepresentasikan event live dari PTY ke WebSocket yang sedang terpasang.
type SubscriberEvent struct {
	Data        []byte
	IdleWarning *int
	Replaced    bool
	Exited      bool
	ExitCode    int
	CloseReason string
}

type ringBuffer struct {
	data []byte
	pos  int
	full bool
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = DefaultScrollbackBytes
	}
	return &ringBuffer{
		data: make([]byte, capacity),
	}
}

func (rb *ringBuffer) Write(p []byte) {
	if len(p) == 0 || len(rb.data) == 0 {
		return
	}
	capLen := len(rb.data)
	if len(p) >= capLen {
		copy(rb.data, p[len(p)-capLen:])
		rb.pos = 0
		rb.full = true
		return
	}
	first := capLen - rb.pos
	if len(p) <= first {
		copy(rb.data[rb.pos:], p)
		rb.pos += len(p)
		if rb.pos == capLen {
			rb.pos = 0
			rb.full = true
		}
		return
	}
	copy(rb.data[rb.pos:], p[:first])
	rem := len(p) - first
	copy(rb.data[0:], p[first:])
	rb.pos = rem
	rb.full = true
}

func (rb *ringBuffer) Bytes() []byte {
	if !rb.full {
		if rb.pos == 0 {
			return nil
		}
		out := make([]byte, rb.pos)
		copy(out, rb.data[:rb.pos])
		return out
	}
	capLen := len(rb.data)
	out := make([]byte, capLen)
	n := copy(out, rb.data[rb.pos:])
	copy(out[n:], rb.data[:rb.pos])
	// Lewati byte lanjutan UTF-8 terpotong di awal buffer hasil wrap.
	trim := 0
	for trim < 3 && trim < len(out) && (out[trim]&0xC0) == 0x80 {
		trim++
	}
	if trim > 0 {
		return out[trim:]
	}
	return out
}

// StartOptions mengonfigurasi pembukaan sesi terminal PTY baru.
type StartOptions struct {
	PanelUser     string
	User          string
	Cwd           string
	Shell         string
	ExecBin       string
	ExecArgs      []string
	ExecLabel     string
	Cols          uint16
	Rows          uint16
	IdleTimeout   time.Duration // 0 => DefaultIdleTimeout; < 0 => nonaktif
	DetachTimeout time.Duration // 0 => DefaultDetachTimeout; < 0 => nonaktif
	IdleWarning   time.Duration // 0 => otomatis 60s bila IdleTimeout >= 2m; < 0 => nonaktif
	Stream        bool          // true => jalankan read pump & ring buffer untuk attach/reattach WS
	OnExit        func(sess *Session, exitCode int, reason string)
}

type sessionSubscriber struct {
	id uint64
	ch chan SubscriberEvent
}

// Session merepresentasikan satu proses shell interaktif berbasis PTY.
type Session struct {
	ID            string
	PanelUser     string
	TargetUser    TargetUser
	Cwd           string
	Shell         string
	User          string
	Hostname      string
	ExecLabel     string
	StartedAt     time.Time
	IdleTimeout   time.Duration
	DetachTimeout time.Duration
	IdleWarning   time.Duration

	cmd                   *exec.Cmd
	ptmx                  *os.File
	pid                   int
	doneCh                chan struct{}
	exitCode              atomic.Int32
	exitErr               error
	closeReasonMu         sync.Mutex
	closeReason           string
	lastInputNano         atomic.Int64
	lastOutputNano        atomic.Int64
	lastActivityNano      atomic.Int64
	warnedForActivityNano atomic.Int64
	detachedAtNano        atomic.Int64
	keepAlive             atomic.Bool
	stopWatchCh           chan struct{}
	closeOnce             sync.Once
	onClose               func()
	onExit                func(sess *Session, exitCode int, reason string)

	streamOnce   sync.Once
	streamMu     sync.Mutex
	scrollback   *ringBuffer
	subscribers  map[uint64]*sessionSubscriber
	nextSubID    uint64
	streamExited bool
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

func lookupCurrentProcessUser() TargetUser {
	euid := uint32(os.Geteuid())
	egid := uint32(os.Getegid())
	for _, pe := range config.ReadPasswdEntries("/etc/passwd") {
		if pe.UID == euid {
			return TargetUser{
				Username: pe.Username,
				UID:      pe.UID,
				GID:      pe.GID,
				HomeDir:  pe.HomeDir,
				Shell:    pe.Shell,
				IsRoot:   pe.UID == 0,
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

// AvailableUsers mengembalikan daftar user Linux yang aman dipakai untuk PTY.
// Ketika wdev berjalan sebagai root, user non-root interaktif diposisikan paling depan (default aman),
// dan root diletakkan di akhir dengan penanda IsRoot=true.
func (m *Manager) AvailableUsers() []TargetUser {
	current := lookupCurrentProcessUser()
	if os.Geteuid() != 0 {
		return []TargetUser{current}
	}

	users := config.ListSystemUsers(true)
	hasRoot := false
	for _, u := range users {
		if u.IsRoot {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		r := current
		r.IsRoot = true
		users = append(users, r)
	}
	return users
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
		if !config.IsUsableShell(cand) {
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
		Hostname:             hostname,
		OSName:               detectOSName(),
		User:                 defUser.Username,
		DefaultUser:          defUser.Username,
		AvailableUsers:       users,
		IsDaemonRoot:         os.Geteuid() == 0,
		Shell:                shells[0],
		AvailableShells:      shells,
		HomeDir:              defUser.HomeDir,
		ActiveSessions:       active,
		MaxSessions:          maxSess,
		IdleTimeoutSeconds:   int(DefaultIdleTimeout.Seconds()),
		DetachTimeoutSeconds: int(DefaultDetachTimeout.Seconds()),
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
	} else if idleTimeout < 0 {
		idleTimeout = 0
	}

	detachTimeout := opts.DetachTimeout
	if detachTimeout == 0 {
		detachTimeout = DefaultDetachTimeout
	} else if detachTimeout < 0 {
		detachTimeout = 0
	}

	idleWarning := opts.IdleWarning
	if idleWarning == 0 {
		if idleTimeout >= 2*time.Minute {
			idleWarning = DefaultIdleWarning
		}
	} else if idleWarning < 0 {
		idleWarning = 0
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

	now := time.Now()
	sess := &Session{
		ID:            sessionID,
		PanelUser:     strings.TrimSpace(opts.PanelUser),
		TargetUser:    target,
		Cwd:           cwd,
		Shell:         displayShell,
		User:          target.Username,
		Hostname:      hostname,
		ExecLabel:     strings.TrimSpace(opts.ExecLabel),
		StartedAt:     now,
		IdleTimeout:   idleTimeout,
		DetachTimeout: detachTimeout,
		IdleWarning:   idleWarning,
		cmd:           cmd,
		ptmx:          ptmx,
		pid:           cmd.Process.Pid,
		doneCh:        make(chan struct{}),
		stopWatchCh:   make(chan struct{}),
		onExit:        opts.OnExit,
		scrollback:    newRingBuffer(DefaultScrollbackBytes),
		subscribers:   make(map[uint64]*sessionSubscriber),
	}
	nowNano := now.UnixNano()
	sess.lastInputNano.Store(nowNano)
	sess.lastOutputNano.Store(nowNano)
	sess.lastActivityNano.Store(nowNano)
	sess.onClose = func() {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
	}

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	if idleTimeout > 0 || detachTimeout > 0 {
		go sess.watchTimers()
	}

	if opts.Stream {
		sess.ensureStreamPump()
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
		sess.exitCode.Store(int32(code))
		sess.exitErr = waitErr
		sess.onClose()
		if sess.onExit != nil {
			sess.onExit(sess, code, sess.Reason())
		}
		close(sess.doneCh)
	}()

	return sess, fallbackReason, nil
}

// GetSession mengembalikan sesi aktif berdasarkan ID.
func (m *Manager) GetSession(id string) (*Session, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok || sess == nil {
		return nil, false
	}
	return sess, true
}

// ListSessions mengembalikan daftar sesi PTY yang masih berjalan atau detached.
func (m *Manager) ListSessions(panelUser string, matchOwner bool) []SessionSnapshot {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s == nil {
			continue
		}
		if matchOwner && panelUser != "" && s.PanelUser != "" && s.PanelUser != panelUser {
			continue
		}
		list = append(list, s)
	}
	m.mu.Unlock()

	out := make([]SessionSnapshot, 0, len(list))
	for _, s := range list {
		out = append(out, s.Snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// CloseSession menghentikan sesi PTY berdasarkan ID secara eksplisit.
func (m *Manager) CloseSession(id, reason string) bool {
	sess, ok := m.GetSession(id)
	if !ok {
		return false
	}
	_ = sess.CloseWithReason(reason)
	return true
}

func (s *Session) ensureStreamPump() {
	s.streamOnce.Do(func() {
		go s.readLoop()
	})
}

func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, rerr := s.ptmx.Read(buf)
		if n > 0 {
			now := time.Now().UnixNano()
			s.lastOutputNano.Store(now)
			s.lastActivityNano.Store(now)

			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			s.streamMu.Lock()
			s.scrollback.Write(chunk)
			s.broadcastLocked(SubscriberEvent{Data: chunk})
			s.streamMu.Unlock()
		}
		if rerr != nil {
			<-s.doneCh
			s.streamMu.Lock()
			s.streamExited = true
			ev := SubscriberEvent{
				Exited:      true,
				ExitCode:    int(s.exitCode.Load()),
				CloseReason: s.Reason(),
			}
			for id, sub := range s.subscribers {
				select {
				case sub.ch <- ev:
				default:
				}
				close(sub.ch)
				delete(s.subscribers, id)
			}
			s.streamMu.Unlock()
			return
		}
	}
}

func (s *Session) broadcast(ev SubscriberEvent) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.streamExited {
		return
	}
	s.broadcastLocked(ev)
}

func (s *Session) broadcastLocked(ev SubscriberEvent) {
	for id, sub := range s.subscribers {
		select {
		case sub.ch <- ev:
		default:
			close(sub.ch)
			delete(s.subscribers, id)
		}
	}
	if len(s.subscribers) == 0 && !s.streamExited && s.detachedAtNano.Load() == 0 {
		s.detachedAtNano.Store(time.Now().UnixNano())
	}
}

// AppendBanner menyimpan banner awal ke scrollback buffer sesi agar ikut tampil saat reattach.
func (s *Session) AppendBanner(data []byte) {
	if len(data) == 0 {
		return
	}
	s.streamMu.Lock()
	s.scrollback.Write(data)
	s.broadcastLocked(SubscriberEvent{Data: append([]byte(nil), data...)})
	s.streamMu.Unlock()
}

// Subscribe mengambil snapshot scrollback secara atomik dan memasang subscriber stream live.
// Jika sudah ada subscriber aktif sebelumnya, subscriber lama akan diganti secara bersih.
func (s *Session) Subscribe() (snapshot []byte, ch <-chan SubscriberEvent, unsubscribe func(), exited bool, exitCode int, reason string) {
	s.ensureStreamPump()

	s.streamMu.Lock()
	defer s.streamMu.Unlock()

	snap := s.scrollback.Bytes()
	if s.streamExited {
		return snap, nil, func() {}, true, int(s.exitCode.Load()), s.Reason()
	}

	for id, old := range s.subscribers {
		select {
		case old.ch <- SubscriberEvent{Replaced: true}:
		default:
		}
		close(old.ch)
		delete(s.subscribers, id)
	}

	s.nextSubID++
	subID := s.nextSubID
	sub := &sessionSubscriber{
		id: subID,
		ch: make(chan SubscriberEvent, 256),
	}
	s.subscribers[subID] = sub
	s.detachedAtNano.Store(0)

	var unsubOnce sync.Once
	unsub := func() {
		unsubOnce.Do(func() {
			s.streamMu.Lock()
			if cur, ok := s.subscribers[subID]; ok && cur == sub {
				delete(s.subscribers, subID)
				close(sub.ch)
				if len(s.subscribers) == 0 && !s.streamExited {
					s.detachedAtNano.Store(time.Now().UnixNano())
				}
			}
			s.streamMu.Unlock()
		})
	}
	return snap, sub.ch, unsub, false, 0, ""
}

// ExtendIdle mereset timer idle sesi terminal.
func (s *Session) ExtendIdle() int {
	now := time.Now().UnixNano()
	s.lastInputNano.Store(now)
	s.lastActivityNano.Store(now)
	s.warnedForActivityNano.Store(0)
	return int(s.IdleTimeout.Seconds())
}

// SetKeepAlive mengaktifkan atau menonaktifkan mode keep-alive agar sesi tidak tertutup otomatis saat idle.
func (s *Session) SetKeepAlive(enabled bool) {
	s.keepAlive.Store(enabled)
	now := time.Now().UnixNano()
	s.lastActivityNano.Store(now)
	s.warnedForActivityNano.Store(0)
}

// KeepAlive mengembalikan status keep-alive sesi saat ini.
func (s *Session) KeepAlive() bool {
	return s.keepAlive.Load()
}

// Snapshot mengembalikan metadata ringkas status sesi saat ini.
func (s *Session) Snapshot() SessionSnapshot {
	s.streamMu.Lock()
	attached := len(s.subscribers) > 0
	exited := s.streamExited
	s.streamMu.Unlock()

	lastAct := time.Unix(0, s.lastActivityNano.Load())
	remSec := 0
	if s.IdleTimeout > 0 && !s.keepAlive.Load() {
		rem := s.IdleTimeout - time.Since(lastAct)
		if rem > 0 {
			remSec = int(rem.Seconds())
		}
	}
	var detachedAt *time.Time
	if det := s.detachedAtNano.Load(); det > 0 && !attached {
		t := time.Unix(0, det)
		detachedAt = &t
	}
	return SessionSnapshot{
		ID:                 s.ID,
		PanelUser:          s.PanelUser,
		User:               s.User,
		IsRoot:             s.TargetUser.IsRoot,
		Shell:              s.Shell,
		Cwd:                s.Cwd,
		Hostname:           s.Hostname,
		ExecLabel:          s.ExecLabel,
		StartedAt:          s.StartedAt,
		LastActivityAt:     lastAct,
		IdleTimeoutSeconds: int(s.IdleTimeout.Seconds()),
		IdleRemainingSec:   remSec,
		DetachTimeoutSec:   int(s.DetachTimeout.Seconds()),
		KeepAlive:          s.keepAlive.Load(),
		Attached:           attached,
		DetachedAt:         detachedAt,
		Exited:             exited,
		ExitCode:           int(s.exitCode.Load()),
	}
}

func (s *Session) watchTimers() {
	checkEvery := 5 * time.Second
	if s.IdleTimeout > 0 && s.IdleTimeout/4 < checkEvery {
		checkEvery = s.IdleTimeout / 4
	}
	if s.DetachTimeout > 0 && s.DetachTimeout/4 < checkEvery {
		checkEvery = s.DetachTimeout / 4
	}
	if checkEvery < 50*time.Millisecond {
		checkEvery = 50 * time.Millisecond
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
			if s.DetachTimeout > 0 {
				if detNano := s.detachedAtNano.Load(); detNano > 0 {
					if time.Since(time.Unix(0, detNano)) >= s.DetachTimeout {
						_ = s.CloseWithReason("detach_timeout")
						return
					}
				}
			}
			if s.IdleTimeout > 0 && !s.keepAlive.Load() {
				lastAct := s.lastActivityNano.Load()
				elapsed := time.Since(time.Unix(0, lastAct))
				if elapsed >= s.IdleTimeout {
					_ = s.CloseWithReason("idle_timeout")
					return
				}
				if s.IdleWarning > 0 && s.IdleTimeout > s.IdleWarning {
					remaining := s.IdleTimeout - elapsed
					if remaining <= s.IdleWarning && s.warnedForActivityNano.Load() != lastAct {
						s.warnedForActivityNano.Store(lastAct)
						remSec := int(remaining.Seconds())
						if remSec < 1 {
							remSec = 1
						}
						s.broadcast(SubscriberEvent{IdleWarning: &remSec})
					}
				}
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
		now := time.Now().UnixNano()
		s.lastInputNano.Store(now)
		s.lastActivityNano.Store(now)
		s.warnedForActivityNano.Store(0)
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
	return int(s.exitCode.Load()), s.exitErr
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
