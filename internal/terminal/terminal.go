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
	"strconv"
	"strings"
	"sync"
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
)

// Info merangkum metadata sistem & shell untuk sesi terminal PTY.
type Info struct {
	Hostname        string   `json:"hostname"`
	OSName          string   `json:"os_name"`
	User            string   `json:"user"`
	Shell           string   `json:"shell"`
	AvailableShells []string `json:"available_shells"`
	HomeDir         string   `json:"home_dir"`
	ServerMode      string   `json:"server_mode,omitempty"`
	ActiveSessions  int      `json:"active_sessions"`
	MaxSessions     int      `json:"max_sessions"`
}

// StartOptions mengonfigurasi pembukaan sesi terminal PTY baru.
type StartOptions struct {
	Cwd   string
	Shell string
	Cols  uint16
	Rows  uint16
}

// Session merepresentasikan satu proses shell interaktif berbasis PTY.
type Session struct {
	ID        string
	Cwd       string
	Shell     string
	User      string
	Hostname  string
	StartedAt time.Time

	cmd       *exec.Cmd
	ptmx      *os.File
	pid       int
	doneCh    chan struct{}
	exitCode  int
	exitErr   error
	closeOnce sync.Once
	onClose   func()
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

func lookupPasswdUser() (username, homeDir, loginShell string) {
	uidStr := strconv.Itoa(os.Getuid())
	if f, err := os.Open("/etc/passwd"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, ":")
			if len(fields) >= 7 && fields[2] == uidStr {
				username = fields[0]
				homeDir = fields[5]
				loginShell = fields[6]
				break
			}
		}
	}
	if u, err := user.Current(); err == nil {
		if username == "" {
			username = u.Username
		}
		if homeDir == "" {
			homeDir = u.HomeDir
		}
	}
	if username == "" {
		username = os.Getenv("USER")
	}
	if username == "" {
		username = "root"
	}
	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			homeDir = h
		} else {
			homeDir = "/"
		}
	}
	return username, homeDir, loginShell
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

// AvailableShells mengembalikan daftar shell interaktif yang tersedia di host.
func (m *Manager) AvailableShells() []string {
	_, _, passwdShell := lookupPasswdUser()
	candidates := []string{
		os.Getenv("SHELL"),
		passwdShell,
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

// ResolveShell memvalidasi shell pilihan pengguna agar hanya shell interaktif sah yang dapat dijalankan.
func (m *Manager) ResolveShell(requested string) (string, error) {
	avail := m.AvailableShells()
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
		if cleanReq == sh || reqReal == shReal || filepath.Base(cleanReq) == filepath.Base(sh) && !strings.Contains(req, "/") {
			return sh, nil
		}
	}
	return "", fmt.Errorf("shell %q tidak tersedia atau tidak diizinkan", requested)
}

// ResolveStartDir memvalidasi working directory awal dan memberikan fallback yang aman.
func (m *Manager) ResolveStartDir(requested string) (string, string) {
	_, homeDir, _ := lookupPasswdUser()
	fallback := homeDir
	if info, err := os.Stat(fallback); err != nil || !info.IsDir() {
		if m.cfg != nil && m.cfg.WebRootBase != "" {
			if wi, err := os.Stat(m.cfg.WebRootBase); err == nil && wi.IsDir() {
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
	if !info.IsDir() {
		parent := filepath.Dir(clean)
		if pi, err := os.Stat(parent); err == nil && pi.IsDir() {
			return parent, ""
		}
		return fallback, fmt.Sprintf("Target %q bukan direktori, dialihkan ke %s", clean, fallback)
	}
	return clean, ""
}

// SystemInfo mengembalikan ringkasan sistem, user, shell, dan jumlah sesi aktif.
func (m *Manager) SystemInfo() Info {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}
	userName, homeDir, _ := lookupPasswdUser()
	shells := m.AvailableShells()

	m.mu.Lock()
	active := len(m.sessions)
	maxSess := m.maxSessions
	m.mu.Unlock()

	return Info{
		Hostname:        hostname,
		OSName:          detectOSName(),
		User:            userName,
		Shell:           shells[0],
		AvailableShells: shells,
		HomeDir:         homeDir,
		ActiveSessions:  active,
		MaxSessions:     maxSess,
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

func buildTerminalEnv(userName, homeDir, shellPath, cwd string) []string {
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

// StartSession meluncurkan proses shell interaktif di dalam Linux PTY baru.
func (m *Manager) StartSession(opts StartOptions) (*Session, string, error) {
	shellPath, err := m.ResolveShell(opts.Shell)
	if err != nil {
		return nil, "", err
	}
	cwd, fallbackReason := m.ResolveStartDir(opts.Cwd)
	cols, rows := clampDimensions(opts.Cols, opts.Rows)

	userName, homeDir, _ := lookupPasswdUser()
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
	// Reserve slot sebelum StartWithSize agar tidak race melewati maxSessions.
	m.sessions[sessionID] = nil
	m.mu.Unlock()

	cmd := exec.Command(shellPath, shellArgs(shellPath)...)
	cmd.Dir = cwd
	cmd.Env = buildTerminalEnv(userName, homeDir, shellPath, cwd)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return nil, "", fmt.Errorf("gagal memulai PTY terminal: %w", err)
	}

	sess := &Session{
		ID:        sessionID,
		Cwd:       cwd,
		Shell:     shellPath,
		User:      userName,
		Hostname:  hostname,
		StartedAt: time.Now(),
		cmd:       cmd,
		ptmx:      ptmx,
		pid:       cmd.Process.Pid,
		doneCh:    make(chan struct{}),
	}
	sess.onClose = func() {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
	}

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

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

// Read membaca byte mentah dari PTY master.
func (s *Session) Read(p []byte) (int, error) {
	return s.ptmx.Read(p)
}

// Write menulis byte input dari pengguna ke PTY master.
func (s *Session) Write(p []byte) (int, error) {
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

// Close menutup PTY dan menghentikan grup proses shell dengan aman.
func (s *Session) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
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
