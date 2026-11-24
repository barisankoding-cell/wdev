package siteapp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"wdev/internal/config"
	"wdev/internal/runner"
	"wdev/internal/serviceconfig"
	"wdev/internal/site"
	"wdev/internal/store"
)

var unixUsernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}\$?$`)

// AppRuntimeStatus merangkum status live unit systemd milik Managed App VHost.
type AppRuntimeStatus struct {
	ServiceName string `json:"service_name"`
	UnitExists  bool   `json:"unit_exists"`
	ActiveState string `json:"active_state"` // "active" | "activating" | "inactive" | "failed"
	SubState    string `json:"sub_state"`
	Enabled     bool   `json:"enabled"`
	MainPID     int    `json:"main_pid"`
	MemoryBytes int64  `json:"memory_bytes"`
	StartedAt   string `json:"started_at,omitempty"`
}

// sysExec abstraksi perintah sistem terangkat agar dapat diuji dengan mock.
type sysExec interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
	WriteFile(ctx context.Context, path string, data []byte, mode os.FileMode, ownerUser string) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	RemoveFile(ctx context.Context, path string) error
	FileExists(ctx context.Context, path string) (bool, error)
	EnsureDir(ctx context.Context, dir string, mode os.FileMode, ownerUser string) error
}

// Manager mengelola lifecycle unit systemd, file .env (0600), build, dan log
// untuk VHost bertipe Managed App (Node.js, Go, Rust).
type Manager struct {
	cfg            *config.Config
	store          *store.Store
	sudoPassFn     func() string
	systemdDir     string
	allowAnyDir    bool
	skipUserLookup bool
	exec           sysExec
}

// New membuat instance siteapp.Manager.
func New(cfg *config.Config, s *store.Store, sudoPassFn func() string) *Manager {
	if sudoPassFn == nil {
		sudoPassFn = func() string { return "" }
	}
	m := &Manager{
		cfg:        cfg,
		store:      s,
		sudoPassFn: sudoPassFn,
		systemdDir: "/etc/systemd/system",
	}
	m.exec = &realSysExec{mgr: m}
	return m
}

// EnvDir mengembalikan direktori penyimpanan .env terkelola aplikasi (/etc/wdev/apps).
func (m *Manager) EnvDir() string {
	base := "/etc/wdev"
	if m != nil && m.cfg != nil && strings.TrimSpace(m.cfg.WdevEtc) != "" {
		base = strings.TrimSpace(m.cfg.WdevEtc)
	}
	return filepath.Join(base, "apps")
}

// UnitPath mengembalikan path file .service systemd untuk domain tertentu.
func (m *Manager) UnitPath(domain string) string {
	return filepath.Join(m.systemdDir, UnitFilenameForDomain(domain))
}

// EnvPath mengembalikan path file .env terkelola untuk domain tertentu.
func (m *Manager) EnvPath(domain string) string {
	return filepath.Join(m.EnvDir(), ServiceNameForDomain(domain)+".env")
}

// ProjectEnvPath mengembalikan path file .env langsung di dalam folder proyek.
func (m *Manager) ProjectEnvPath(workingDir string) string {
	clean := strings.TrimSpace(workingDir)
	if clean == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(clean), ".env")
}

// IsManagedEnvPath melaporkan apakah envPath berada di direktori .env terkelola wdev (/etc/wdev/apps)
// dan bukan file .env milik proyek pengguna.
func (m *Manager) IsManagedEnvPath(envPath string) bool {
	if strings.TrimSpace(envPath) == "" {
		return false
	}
	return filepath.Clean(filepath.Dir(envPath)) == filepath.Clean(m.EnvDir())
}

// DetectProjectEnvPort membaca nilai PORT dari <workingDir>/.env jika ada dan valid.
func (m *Manager) DetectProjectEnvPort(workingDir string) int {
	pEnv := m.ProjectEnvPath(workingDir)
	if pEnv == "" {
		return 0
	}
	entries, _, err := ReadEnvFile(pEnv)
	if err != nil {
		return 0
	}
	return FindEnvPort(entries)
}

// SetSystemdDirForTest mengganti direktori unit systemd untuk isolasi unit test.
func (m *Manager) SetSystemdDirForTest(dir string) {
	m.systemdDir = dir
}

// SetAllowAnyDirForTest menonaktifkan whitelist root directory untuk unit test t.TempDir().
func (m *Manager) SetAllowAnyDirForTest(allow bool) {
	m.allowAnyDir = allow
	m.skipUserLookup = allow
}

// SetExecForTest mengganti backend eksekutor sistem untuk unit test.
func (m *Manager) SetExecForTest(execImpl sysExec) {
	m.exec = execImpl
}

// DefaultRunUser mengembalikan user non-root standar pemilik proses aplikasi.
func (m *Manager) DefaultRunUser() string {
	u := strings.TrimSpace(config.CurrentUsername())
	if u == "" {
		return "root"
	}
	return u
}

// InspectProject memeriksa folder proyek dan memvalidasi keamanannya.
func (m *Manager) InspectProject(dir string) (*InspectResult, error) {
	if m != nil && m.allowAnyDir {
		clean := filepath.Clean(strings.TrimSpace(dir))
		return inspectDirectoryPath(clean)
	}
	return InspectDirectory(dir)
}

// NormalizeAndValidate memvalidasi dan melengkapi metadata SiteAppRecord.
func (m *Manager) NormalizeAndValidate(rec store.SiteAppRecord) (store.SiteAppRecord, error) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(rec.Domain), "."), ".conf"))
	if err := site.ValidateDomain(domain); err != nil {
		return rec, err
	}
	rec.Domain = domain

	rec.Runtime = strings.ToLower(strings.TrimSpace(rec.Runtime))
	if !store.IsManagedAppFramework(rec.Runtime) {
		return rec, fmt.Errorf("runtime tidak didukung: %q (gunakan nodejs, go, atau rust)", rec.Runtime)
	}

	var cleanDir string
	if m.allowAnyDir {
		cleanDir = filepath.Clean(strings.TrimSpace(rec.WorkingDir))
	} else {
		var err error
		cleanDir, err = site.ValidateRootPath(rec.WorkingDir)
		if err != nil {
			return rec, fmt.Errorf("working_dir tidak valid: %w", err)
		}
	}
	fi, err := os.Stat(cleanDir)
	if err != nil || !fi.IsDir() {
		return rec, fmt.Errorf("direktori proyek tidak ditemukan: %s", cleanDir)
	}
	rec.WorkingDir = cleanDir

	rec.BuildCommand = strings.TrimSpace(rec.BuildCommand)
	if len(rec.BuildCommand) > 1024 || strings.ContainsAny(rec.BuildCommand, "\r\n\x00") {
		return rec, fmt.Errorf("build_command tidak valid")
	}

	rec.StartCommand = strings.TrimSpace(rec.StartCommand)
	if rec.StartCommand == "" {
		return rec, fmt.Errorf("start_command wajib diisi untuk Managed App")
	}
	if len(rec.StartCommand) > 1024 || strings.ContainsAny(rec.StartCommand, "\r\n\x00") {
		return rec, fmt.Errorf("start_command tidak valid")
	}

	if rec.Port <= 0 || rec.Port > 65535 {
		return rec, fmt.Errorf("port aplikasi tidak valid: %d", rec.Port)
	}

	runUser := strings.TrimSpace(rec.RunUser)
	if runUser == "" {
		runUser = m.DefaultRunUser()
	}
	if !unixUsernameRe.MatchString(runUser) {
		return rec, fmt.Errorf("nama user sistem tidak valid: %q", runUser)
	}
	if !m.skipUserLookup {
		if _, err := user.Lookup(runUser); err != nil {
			return rec, fmt.Errorf("user sistem %q tidak ditemukan di OS: %w", runUser, err)
		}
	}
	rec.RunUser = runUser
	rec.ServiceName = ServiceNameForDomain(domain)
	useProjectEnv := rec.UseProjectEnv
	if !useProjectEnv && rec.EnvFile != "" && filepath.Clean(rec.EnvFile) == filepath.Clean(filepath.Join(cleanDir, ".env")) {
		useProjectEnv = true
	}
	rec.UseProjectEnv = useProjectEnv
	if useProjectEnv {
		rec.EnvFile = filepath.Join(cleanDir, ".env")
	} else {
		rec.EnvFile = m.EnvPath(domain)
	}
	return rec, nil
}

func (m *Manager) syncEnvFile(
	ctx context.Context,
	norm store.SiteAppRecord,
	importProjectEnv bool,
	initialEnvRaw string,
) error {
	if norm.UseProjectEnv {
		// Pakai file .env proyek langsung tanpa mengubah permission WorkingDir.
		if strings.TrimSpace(initialEnvRaw) != "" {
			if _, err := ParseEnvContent(initialEnvRaw); err != nil {
				return fmt.Errorf("isi .env tidak valid: %w", err)
			}
			content := EnsurePortInRawEnv(initialEnvRaw, norm.Port)
			return m.exec.WriteFile(ctx, norm.EnvFile, []byte(content), 0600, norm.RunUser)
		}
		existingBytes, readErr := m.exec.ReadFile(ctx, norm.EnvFile)
		if readErr == nil {
			updatedRaw, changed := UpsertPortInRawEnv(string(existingBytes), norm.Port)
			if !changed {
				return nil
			}
			return m.exec.WriteFile(ctx, norm.EnvFile, []byte(updatedRaw), 0600, norm.RunUser)
		}
		if os.IsNotExist(readErr) {
			content := fmt.Sprintf("PORT=%d\n", norm.Port)
			return m.exec.WriteFile(ctx, norm.EnvFile, []byte(content), 0600, norm.RunUser)
		}
		return fmt.Errorf("gagal membaca .env proyek %s: %w", norm.EnvFile, readErr)
	}

	envEntries, err := m.prepareEnvEntries(ctx, norm, importProjectEnv, initialEnvRaw)
	if err != nil {
		return err
	}
	envContent, err := SerializeEnvEntries(envEntries)
	if err != nil {
		return err
	}
	if err := m.exec.EnsureDir(ctx, filepath.Dir(norm.EnvFile), 0711, "root"); err != nil {
		return fmt.Errorf("gagal menyiapkan direktori env %s: %w", filepath.Dir(norm.EnvFile), err)
	}
	if err := m.exec.WriteFile(ctx, norm.EnvFile, []byte(envContent), 0600, norm.RunUser); err != nil {
		return fmt.Errorf("gagal menulis file env %s: %w", norm.EnvFile, err)
	}
	return nil
}

// ProvisionOrSync menulis file .env (atau menautkan .env proyek), file unit systemd,
// melakukan daemon-reload, serta mengelola status enable/start unit.
func (m *Manager) ProvisionOrSync(
	ctx context.Context,
	rec store.SiteAppRecord,
	startNow bool,
	importProjectEnv bool,
	initialEnvRaw string,
) (store.SiteAppRecord, error) {
	norm, err := m.NormalizeAndValidate(rec)
	if err != nil {
		return norm, err
	}

	if err := m.ensurePrivilegeReady(ctx); err != nil {
		return norm, err
	}

	if err := m.syncEnvFile(ctx, norm, importProjectEnv, initialEnvRaw); err != nil {
		return norm, err
	}

	unitContent := RenderSystemdUnit(norm)
	unitPath := m.UnitPath(norm.Domain)
	if err := m.exec.EnsureDir(ctx, filepath.Dir(unitPath), 0755, "root"); err != nil {
		return norm, fmt.Errorf("gagal menyiapkan direktori systemd: %w", err)
	}
	if err := m.exec.WriteFile(ctx, unitPath, []byte(unitContent), 0644, "root"); err != nil {
		return norm, fmt.Errorf("gagal menulis unit systemd %s: %w", unitPath, err)
	}

	if out, err := m.exec.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return norm, fmt.Errorf("systemctl daemon-reload gagal: %w (%s)", err, out)
	}

	unitFile := UnitFilenameForDomain(norm.Domain)
	if norm.AutoStart {
		if out, err := m.exec.Run(ctx, "systemctl", "enable", unitFile); err != nil {
			return norm, fmt.Errorf("systemctl enable %s gagal: %w (%s)", unitFile, err, out)
		}
	} else {
		_, _ = m.exec.Run(ctx, "systemctl", "disable", unitFile)
	}

	if m.store != nil {
		if err := m.store.SaveSiteApp(norm); err != nil {
			return norm, fmt.Errorf("gagal menyimpan metadata aplikasi ke database: %w", err)
		}
	}

	if startNow {
		if out, err := m.exec.Run(ctx, "systemctl", "restart", unitFile); err != nil {
			return norm, fmt.Errorf("gagal menjalankan service %s: %w (%s)", unitFile, err, out)
		}
	}

	return norm, nil
}

// UpdateAndSync memperbarui konfigurasi unit & env aplikasi lalu me-restart hanya jika perlu.
func (m *Manager) UpdateAndSync(ctx context.Context, prev *store.SiteAppRecord, next store.SiteAppRecord) (store.SiteAppRecord, error) {
	norm, err := m.NormalizeAndValidate(next)
	if err != nil {
		return norm, err
	}
	if err := m.ensurePrivilegeReady(ctx); err != nil {
		return norm, err
	}

	serviceChanged := prev == nil ||
		prev.WorkingDir != norm.WorkingDir ||
		prev.StartCommand != norm.StartCommand ||
		prev.Port != norm.Port ||
		prev.RunUser != norm.RunUser ||
		prev.Runtime != norm.Runtime ||
		filepath.Clean(prev.EnvFile) != filepath.Clean(norm.EnvFile)

	if err := m.syncEnvFile(ctx, norm, false, ""); err != nil {
		return norm, err
	}

	unitContent := RenderSystemdUnit(norm)
	unitPath := m.UnitPath(norm.Domain)
	if err := m.exec.EnsureDir(ctx, filepath.Dir(unitPath), 0755, "root"); err != nil {
		return norm, err
	}
	if err := m.exec.WriteFile(ctx, unitPath, []byte(unitContent), 0644, "root"); err != nil {
		return norm, err
	}

	if out, err := m.exec.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return norm, fmt.Errorf("systemctl daemon-reload gagal: %w (%s)", err, out)
	}

	unitFile := UnitFilenameForDomain(norm.Domain)
	if norm.AutoStart {
		if out, err := m.exec.Run(ctx, "systemctl", "enable", unitFile); err != nil {
			return norm, fmt.Errorf("systemctl enable %s gagal: %w (%s)", unitFile, err, out)
		}
	} else {
		_, _ = m.exec.Run(ctx, "systemctl", "disable", unitFile)
	}

	if m.store != nil {
		if err := m.store.SaveSiteApp(norm); err != nil {
			return norm, err
		}
	}

	if serviceChanged {
		if out, err := m.exec.Run(ctx, "systemctl", "restart", unitFile); err != nil {
			return norm, fmt.Errorf("gagal me-restart service %s setelah pembaruan: %w (%s)", unitFile, err, out)
		}
	}

	return norm, nil
}

// RemoveApp menghentikan dan menghapus unit systemd serta file .env terkelola secara aman
// tanpa menghapus folder kode proyek di WorkingDir. Idempoten untuk VHost non-Managed-App.
func (m *Manager) RemoveApp(ctx context.Context, domain string) error {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	if cleanDom == "" {
		return nil
	}

	var rec *store.SiteAppRecord
	if m.store != nil {
		rec, _ = m.store.GetSiteApp(cleanDom)
	}

	unitPath := m.UnitPath(cleanDom)
	envPath := m.EnvPath(cleanDom)
	unitName := UnitFilenameForDomain(cleanDom)
	if rec != nil {
		if strings.TrimSpace(rec.ServiceName) != "" {
			unitName = strings.TrimSuffix(strings.TrimSpace(rec.ServiceName), ".service") + ".service"
			unitPath = filepath.Join(m.systemdDir, unitName)
		}
		if strings.TrimSpace(rec.EnvFile) != "" {
			envPath = strings.TrimSpace(rec.EnvFile)
		}
	}

	unitExists, uErr := m.exec.FileExists(ctx, unitPath)
	if errors.Is(uErr, serviceconfig.ErrSudoRequired) {
		return uErr
	}
	managedEnvPath := m.EnvPath(cleanDom)
	managedEnvExists, meErr := m.exec.FileExists(ctx, managedEnvPath)
	if errors.Is(meErr, serviceconfig.ErrSudoRequired) {
		return meErr
	}

	if rec == nil && !unitExists && !managedEnvExists {
		return nil
	}

	if unitExists {
		if err := m.ensurePrivilegeReady(ctx); err != nil {
			return err
		}
		if _, err := m.exec.Run(ctx, "systemctl", "stop", unitName); err != nil && errors.Is(err, serviceconfig.ErrSudoRequired) {
			return err
		}
		if _, err := m.exec.Run(ctx, "systemctl", "disable", unitName); err != nil && errors.Is(err, serviceconfig.ErrSudoRequired) {
			return err
		}
		if err := m.exec.RemoveFile(ctx, unitPath); err != nil {
			return fmt.Errorf("gagal menghapus unit systemd %s: %w", unitPath, err)
		}
		_, _ = m.exec.Run(ctx, "systemctl", "daemon-reload")
		_, _ = m.exec.Run(ctx, "systemctl", "reset-failed", unitName)
	}

	// Hanya hapus file env terkelola (/etc/wdev/apps/...), JANGAN pernah hapus .env milik proyek pengguna!
	if m.IsManagedEnvPath(envPath) {
		if envExists, _ := m.exec.FileExists(ctx, envPath); envExists {
			if err := m.exec.RemoveFile(ctx, envPath); err != nil {
				return fmt.Errorf("gagal menghapus file env %s: %w", envPath, err)
			}
		}
	}
	if managedEnvExists && filepath.Clean(managedEnvPath) != filepath.Clean(envPath) {
		if err := m.exec.RemoveFile(ctx, managedEnvPath); err != nil {
			return fmt.Errorf("gagal menghapus file env terkelola %s: %w", managedEnvPath, err)
		}
	}

	if m.store != nil {
		if err := m.store.DeleteSiteApp(cleanDom); err != nil {
			return err
		}
	}

	return nil
}

// Action mengontrol service systemd aplikasi: start, stop, restart, enable, disable.
func (m *Manager) Action(ctx context.Context, domain, action string) error {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "start", "stop", "restart", "enable", "disable":
	default:
		return fmt.Errorf("aksi service tidak valid: %q", action)
	}

	if m.store == nil {
		return fmt.Errorf("store tidak tersedia")
	}
	rec, err := m.store.GetSiteApp(cleanDom)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("aplikasi terkelola untuk domain %s tidak ditemukan", cleanDom)
	}

	if err := m.ensurePrivilegeReady(ctx); err != nil {
		return err
	}

	unitName := UnitFilenameForDomain(cleanDom)
	if out, err := m.exec.Run(ctx, "systemctl", action, unitName); err != nil {
		return fmt.Errorf("systemctl %s %s gagal: %w (%s)", action, unitName, err, out)
	}

	if action == "enable" || action == "disable" {
		rec.AutoStart = (action == "enable")
		_ = m.store.SaveSiteApp(*rec)
	}
	return nil
}

// Status mengambil status runtime live unit systemd milik satu domain.
func (m *Manager) Status(ctx context.Context, domain string) AppRuntimeStatus {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	svc := ServiceNameForDomain(cleanDom)
	st := AppRuntimeStatus{
		ServiceName: svc,
		ActiveState: "inactive",
		SubState:    "dead",
	}

	unitFile := svc + ".service"
	if ok, err := m.exec.FileExists(ctx, filepath.Join(m.systemdDir, unitFile)); err == nil {
		st.UnitExists = ok
	}

	out, err := m.exec.Run(
		ctx,
		"systemctl",
		"show",
		unitFile,
		"--no-pager",
		"--property=ActiveState,SubState,UnitFileState,MainPID,MemoryCurrent,ExecMainStartTimestamp",
	)
	if err != nil && strings.TrimSpace(out) == "" {
		return st
	}

	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		k, v := parts[0], parts[1]
		switch k {
		case "ActiveState":
			if v != "" {
				st.ActiveState = v
			}
		case "SubState":
			if v != "" {
				st.SubState = v
			}
		case "UnitFileState":
			st.Enabled = (v == "enabled" || v == "enabled-runtime")
		case "MainPID":
			if pid, pErr := strconv.Atoi(v); pErr == nil && pid > 0 {
				st.MainPID = pid
			}
		case "MemoryCurrent":
			if mem, mErr := strconv.ParseInt(v, 10, 64); mErr == nil && mem > 0 && mem < (1<<60) {
				st.MemoryBytes = mem
			}
		case "ExecMainStartTimestamp":
			if v != "" && v != "n/a" {
				st.StartedAt = v
			}
		}
	}
	return st
}

// BatchStatus mengambil status runtime live untuk banyak domain sekaligus.
func (m *Manager) BatchStatus(ctx context.Context, domains []string) map[string]AppRuntimeStatus {
	out := make(map[string]AppRuntimeStatus, len(domains))
	for _, dom := range domains {
		clean := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(dom), "."), ".conf"))
		if clean == "" {
			continue
		}
		out[clean] = m.Status(ctx, clean)
	}
	return out
}

// GetEnv membaca variabel env aplikasi dari file .env aktif (proyek atau terkelola /etc/wdev/apps).
func (m *Manager) GetEnv(ctx context.Context, domain string) (string, []EnvEntry, string, error) {
	if m.store == nil {
		return "", nil, "", fmt.Errorf("store tidak tersedia")
	}
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	rec, err := m.store.GetSiteApp(cleanDom)
	if err != nil {
		return "", nil, "", err
	}
	if rec == nil {
		return "", nil, "", fmt.Errorf("aplikasi terkelola %s tidak ditemukan", cleanDom)
	}

	envPath := rec.EnvFile
	if envPath == "" {
		envPath = m.EnvPath(cleanDom)
	}
	data, readErr := m.exec.ReadFile(ctx, envPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return envPath, UpsertPortInEnvEntries(nil, rec.Port), fmt.Sprintf("PORT=%d\n", rec.Port), nil
		}
		return envPath, nil, "", readErr
	}
	raw := string(data)
	entries, parseErr := ParseEnvContent(raw)
	if parseErr != nil {
		return envPath, nil, raw, parseErr
	}
	return envPath, entries, raw, nil
}

// SaveEnvResult berisi hasil penyimpanan file .env aplikasi beserta PORT yang terdeteksi.
type SaveEnvResult struct {
	EnvFile      string     `json:"env_file"`
	Entries      []EnvEntry `json:"entries"`
	Raw          string     `json:"raw"`
	ResolvedPort int        `json:"resolved_port,omitempty"`
}

// SaveEnv menyimpan file .env aktif aplikasi (.env proyek atau terkelola) dengan izin 0600
// sambil mempertahankan komentar jika menggunakan mode Raw Text, lalu opsional me-restart service.
func (m *Manager) SaveEnv(ctx context.Context, domain string, entries []EnvEntry, raw string, restart bool) (*SaveEnvResult, error) {
	if m.store == nil {
		return nil, fmt.Errorf("store tidak tersedia")
	}
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	rec, err := m.store.GetSiteApp(cleanDom)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("aplikasi terkelola %s tidak ditemukan", cleanDom)
	}

	var parsed []EnvEntry
	var finalContent string

	if strings.TrimSpace(raw) != "" && len(entries) == 0 {
		parsed, err = ParseEnvContent(raw)
		if err != nil {
			return nil, err
		}
		finalContent = strings.ReplaceAll(raw, "\r\n", "\n")
		if !strings.HasSuffix(finalContent, "\n") {
			finalContent += "\n"
		}
		if !rec.UseProjectEnv && FindEnvPort(parsed) == 0 {
			finalContent = EnsurePortInRawEnv(finalContent, rec.Port)
			parsed, _ = ParseEnvContent(finalContent)
		}
	} else {
		parsed = entries
		if !rec.UseProjectEnv || FindEnvPort(parsed) == 0 {
			parsed = UpsertPortInEnvEntries(parsed, rec.Port)
		}
		finalContent, err = SerializeEnvEntries(parsed)
		if err != nil {
			return nil, err
		}
	}

	if err := m.ensurePrivilegeReady(ctx); err != nil {
		return nil, err
	}
	envPath := rec.EnvFile
	if envPath == "" {
		envPath = m.EnvPath(cleanDom)
	}
	if m.IsManagedEnvPath(envPath) {
		if err := m.exec.EnsureDir(ctx, filepath.Dir(envPath), 0711, "root"); err != nil {
			return nil, err
		}
	}
	if err := m.exec.WriteFile(ctx, envPath, []byte(finalContent), 0600, rec.RunUser); err != nil {
		return nil, err
	}

	if restart {
		unitName := UnitFilenameForDomain(cleanDom)
		if out, err := m.exec.Run(ctx, "systemctl", "restart", unitName); err != nil {
			return nil, fmt.Errorf("env tersimpan, tetapi gagal me-restart %s: %w (%s)", unitName, err, out)
		}
	}

	return &SaveEnvResult{
		EnvFile:      envPath,
		Entries:      parsed,
		Raw:          finalContent,
		ResolvedPort: FindEnvPort(parsed),
	}, nil
}

// StartBuild menjalankan BuildCommand melalui runner secara async sambil streaming output
// dan otomatis me-restart unit systemd jika build berhasil dan restartAfter == true.
func (m *Manager) StartBuild(r *runner.Runner, domain string, restartAfter bool) (int64, error) {
	if r == nil {
		return 0, fmt.Errorf("runner tidak tersedia")
	}
	if m.store == nil {
		return 0, fmt.Errorf("store tidak tersedia")
	}
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	rec, err := m.store.GetSiteApp(cleanDom)
	if err != nil {
		return 0, err
	}
	if rec == nil {
		return 0, fmt.Errorf("aplikasi terkelola %s tidak ditemukan", cleanDom)
	}
	buildCmd := strings.TrimSpace(rec.BuildCommand)
	if buildCmd == "" {
		return 0, fmt.Errorf("aplikasi %s tidak memiliki build_command", cleanDom)
	}

	taskName := fmt.Sprintf("Build %s (%s)", cleanDom, rec.Runtime)
	appCopy := *rec
	return r.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		cb(fmt.Sprintf("== Memulai build %s [%s] di %s ==", appCopy.Domain, appCopy.Runtime, appCopy.WorkingDir))
		cb(fmt.Sprintf("$ %s", appCopy.BuildCommand))

		cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", appCopy.BuildCommand)
		cmd.Dir = appCopy.WorkingDir
		cmd.Env = m.buildTaskEnv(ctx, appCopy)

		if os.Geteuid() == 0 && appCopy.RunUser != "" && appCopy.RunUser != "root" {
			if u, uErr := user.Lookup(appCopy.RunUser); uErr == nil {
				uid, uOk := strconv.Atoi(u.Uid)
				gid, gOk := strconv.Atoi(u.Gid)
				if uOk == nil && gOk == nil {
					cmd.SysProcAttr = &syscall.SysProcAttr{
						Credential: &syscall.Credential{
							Uid: uint32(uid),
							Gid: uint32(gid),
						},
					}
				}
			}
		}

		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw

		if err := cmd.Start(); err != nil {
			_ = pw.Close()
			_ = m.store.UpdateSiteAppBuildStatus(appCopy.Domain, "failed", time.Now())
			return fmt.Errorf("gagal menjalankan proses build: %w", err)
		}

		scanDone := make(chan struct{})
		go func() {
			defer close(scanDone)
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				cb(sc.Text())
			}
		}()

		waitErr := cmd.Wait()
		_ = pw.Close()
		<-scanDone

		if waitErr != nil {
			_ = m.store.UpdateSiteAppBuildStatus(appCopy.Domain, "failed", time.Now())
			return fmt.Errorf("build gagal: %w", waitErr)
		}

		_ = m.store.UpdateSiteAppBuildStatus(appCopy.Domain, "success", time.Now())
		cb("== Build selesai dengan sukses ==")

		if restartAfter {
			unitName := UnitFilenameForDomain(appCopy.Domain)
			cb(fmt.Sprintf("Me-restart service systemd %s...", unitName))
			if out, rErr := m.exec.Run(ctx, "systemctl", "restart", unitName); rErr != nil {
				return fmt.Errorf("build sukses, namun gagal me-restart %s: %w (%s)", unitName, rErr, out)
			}
			cb(fmt.Sprintf("Service %s berjalan dengan build terbaru.", unitName))
		}
		return nil
	})
}

// ReadLogs mengambil snapshot log terbaru dari journalctl untuk unit aplikasi.
func (m *Manager) ReadLogs(ctx context.Context, domain string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	if lines > 2000 {
		lines = 2000
	}
	unitName := UnitFilenameForDomain(domain)
	out, err := m.exec.Run(ctx, "journalctl", "-u", unitName, "--no-pager", "-n", strconv.Itoa(lines), "-o", "short-iso")
	if err != nil && strings.TrimSpace(out) == "" {
		return "", err
	}
	return out, nil
}

// NewLogStreamCmd menyiapkan exec.Cmd untuk streaming SSE log journalctl secara live.
func (m *Manager) NewLogStreamCmd(ctx context.Context, domain string, tail int) *exec.Cmd {
	if tail <= 0 {
		tail = 100
	}
	if tail > 1000 {
		tail = 1000
	}
	unitName := UnitFilenameForDomain(domain)
	return exec.CommandContext(ctx, "journalctl", "-u", unitName, "-f", "--no-pager", "-n", strconv.Itoa(tail), "-o", "short-iso")
}

func (m *Manager) buildTaskEnv(ctx context.Context, rec store.SiteAppRecord) []string {
	pathVal, homeDir := buildToolchainPATH(rec.RunUser)
	envMap := map[string]string{
		"PATH":        pathVal,
		"HOME":        homeDir,
		"PORT":        strconv.Itoa(rec.Port),
		"CARGO_HOME":  "/usr/local/cargo",
		"RUSTUP_HOME": "/usr/local/rustup",
	}

	if data, err := m.exec.ReadFile(ctx, rec.EnvFile); err == nil {
		if entries, pErr := ParseEnvContent(string(data)); pErr == nil {
			for _, e := range entries {
				envMap[e.Key] = e.Value
			}
		}
	}
	envMap["PORT"] = strconv.Itoa(rec.Port)

	var envList []string
	for k, v := range envMap {
		envList = append(envList, k+"="+v)
	}
	return envList
}

func (m *Manager) prepareEnvEntries(ctx context.Context, rec store.SiteAppRecord, importProjectEnv bool, overrideRaw string) ([]EnvEntry, error) {
	var base []EnvEntry

	if strings.TrimSpace(overrideRaw) != "" {
		parsed, err := ParseEnvContent(overrideRaw)
		if err != nil {
			return nil, fmt.Errorf("isi .env awal tidak valid: %w", err)
		}
		base = parsed
	} else if existingBytes, err := m.exec.ReadFile(ctx, rec.EnvFile); err == nil {
		if parsed, pErr := ParseEnvContent(string(existingBytes)); pErr == nil {
			base = parsed
		}
	} else if importProjectEnv {
		if projEntries, _, pErr := ReadEnvFile(filepath.Join(rec.WorkingDir, ".env")); pErr == nil {
			base = projEntries
		}
	}

	return UpsertPortInEnvEntries(base, rec.Port), nil
}

func (m *Manager) ensurePrivilegeReady(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return nil
	}
	// Jika test/custom exec mengganti backend, delegasikan sepenuhnya.
	if _, isReal := m.exec.(*realSysExec); !isReal {
		return nil
	}
	_, err := m.exec.Run(ctx, "true")
	return err
}

// realSysExec mengeksekusi operasi file & systemctl dengan sudo bila proses bukan root.
type realSysExec struct {
	mgr *Manager
}

func (r *realSysExec) buildSudoCmd(ctx context.Context, stdinData, name string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, name, args...)
		if stdinData != "" {
			cmd.Stdin = strings.NewReader(stdinData)
		}
		return cmd
	}
	pass := ""
	if r.mgr != nil && r.mgr.sudoPassFn != nil {
		pass = r.mgr.sudoPassFn()
	}
	if pass != "" {
		sudoArgs := append([]string{"-S", "-p", "", name}, args...)
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
		cmd.Stdin = strings.NewReader(pass + "\n" + stdinData)
		return cmd
	}
	sudoArgs := append([]string{"-n", name}, args...)
	cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	return cmd
}

func isSudoAuthErr(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "a password is required") ||
		strings.Contains(lower, "incorrect password") ||
		strings.Contains(lower, "sorry, try again") ||
		strings.Contains(lower, "no tty present")
}

func (r *realSysExec) Run(ctx context.Context, name string, args ...string) (string, error) {
	// Read-only queries (systemctl show, journalctl) dicoba tanpa sudo dulu bila non-root.
	if os.Geteuid() != 0 && (name == "journalctl" || (name == "systemctl" && len(args) > 0 && args[0] == "show")) {
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err == nil {
			return strings.TrimSpace(buf.String()), nil
		}
	}

	var buf bytes.Buffer
	cmd := r.buildSudoCmd(ctx, "", name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		if isSudoAuthErr(out) {
			return out, serviceconfig.ErrSudoRequired
		}
		return out, err
	}
	return out, nil
}

func (r *realSysExec) FileExists(ctx context.Context, path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	}
	if os.Geteuid() == 0 {
		return false, nil
	}
	_, err := r.Run(ctx, "test", "-e", path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, serviceconfig.ErrSudoRequired) {
		return false, serviceconfig.ErrSudoRequired
	}
	return false, nil
}

func (r *realSysExec) ReadFile(ctx context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if os.IsNotExist(err) || os.Geteuid() == 0 {
		return nil, err
	}
	out, sErr := r.Run(ctx, "cat", path)
	if sErr != nil {
		if strings.Contains(strings.ToLower(out), "no such file") {
			return nil, os.ErrNotExist
		}
		return nil, sErr
	}
	return []byte(out), nil
}

func resolveOwnerIDs(ownerUser string) (string, string) {
	if ownerUser == "" || ownerUser == "root" {
		return "0", "0"
	}
	if u, err := user.Lookup(ownerUser); err == nil {
		return u.Uid, u.Gid
	}
	return ownerUser, ownerUser
}

func (r *realSysExec) EnsureDir(ctx context.Context, dir string, mode os.FileMode, ownerUser string) error {
	if err := os.MkdirAll(dir, mode); err == nil {
		_ = os.Chmod(dir, mode)
		return nil
	} else if !os.IsPermission(err) || os.Geteuid() == 0 {
		return err
	}
	if out, err := r.Run(ctx, "mkdir", "-p", dir); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	if _, err := r.Run(ctx, "chmod", fmt.Sprintf("%04o", mode), dir); err != nil {
		return err
	}
	if ownerUser != "" && ownerUser != "root" {
		uid, gid := resolveOwnerIDs(ownerUser)
		_, _ = r.Run(ctx, "chown", uid+":"+gid, dir)
	}
	return nil
}

func (r *realSysExec) WriteFile(ctx context.Context, dstPath string, data []byte, mode os.FileMode, ownerUser string) error {
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}
		tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), ".wdev-app-*")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()
		if _, wErr := tmpFile.Write(data); wErr != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return wErr
		}
		_ = tmpFile.Close()
		_ = os.Chmod(tmpPath, mode)
		if ownerUser != "" {
			if u, uErr := user.Lookup(ownerUser); uErr == nil {
				uid, uOk := strconv.Atoi(u.Uid)
				gid, gOk := strconv.Atoi(u.Gid)
				if uOk == nil && gOk == nil {
					_ = os.Chown(tmpPath, uid, gid)
				}
			}
		}
		return os.Rename(tmpPath, dstPath)
	}

	if err := os.WriteFile(dstPath, data, mode); err == nil {
		return os.Chmod(dstPath, mode)
	}

	tmpFile, err := os.CreateTemp("", "wdev-app-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, wErr := tmpFile.Write(data); wErr != nil {
		_ = tmpFile.Close()
		return wErr
	}
	_ = tmpFile.Close()
	uid, gid := resolveOwnerIDs(ownerUser)
	if out, err := r.Run(ctx, "install", "-o", uid, "-g", gid, "-m", fmt.Sprintf("%04o", mode), tmpPath, dstPath); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	return nil
}

func (r *realSysExec) RemoveFile(ctx context.Context, path string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		return nil
	} else if os.Geteuid() == 0 {
		return err
	}
	if out, err := r.Run(ctx, "rm", "-f", path); err != nil {
		return fmt.Errorf("%w (%s)", err, out)
	}
	return nil
}
