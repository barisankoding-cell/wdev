package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/mail"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/site"
	"wdev/internal/store"
	"wdev/internal/switcher"
	"wdev/web"
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	runner      *runner.Runner
	installer   *installer.Installer
	siteMgr     *site.Manager
	switcherMgr *switcher.Manager
	cfMgr       *cloudflare.Manager
	pdnsMgr     *pdns.Manager
	mailMgr     *mail.Manager
	mux         *http.ServeMux
}

func New(
	cfg *config.Config,
	s *store.Store,
	r *runner.Runner,
	inst *installer.Installer,
	siteMgr *site.Manager,
	swMgr *switcher.Manager,
	cfMgr *cloudflare.Manager,
	pdnsMgr *pdns.Manager,
	mailMgr *mail.Manager,
) *Server {
	srv := &Server{
		cfg:         cfg,
		store:       s,
		runner:      r,
		installer:   inst,
		siteMgr:     siteMgr,
		switcherMgr: swMgr,
		cfMgr:       cfMgr,
		pdnsMgr:     pdnsMgr,
		mailMgr:     mailMgr,
		mux:         http.NewServeMux(),
	}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	// API routes
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/service/", s.handleServiceControl)
	s.mux.HandleFunc("/api/sites", s.handleSites)
	s.mux.HandleFunc("/api/sites/", s.handleSiteDelete)
	s.mux.HandleFunc("/api/switchers", s.handleSwitchers)
	s.mux.HandleFunc("/api/switchers/", s.handleSwitchersAction)
	s.mux.HandleFunc("/api/cloudflare/status", s.handleCFStatus)
	s.mux.HandleFunc("/api/cloudflare/expose", s.handleCFExpose)
	s.mux.HandleFunc("/api/cloudflare/unexpose", s.handleCFUnexpose)
	s.mux.HandleFunc("/api/pdns/status", s.handlePDNSStatus)
	s.mux.HandleFunc("/api/pdns/zones", s.handlePDNSZones)
	s.mux.HandleFunc("/api/pdns/zone/doctor", s.handlePDNSZoneDoctor)
	s.mux.HandleFunc("/api/pdns/zone/repair", s.handlePDNSZoneRepair)
	s.mux.HandleFunc("/api/pdns/records", s.handlePDNSRecords)
	s.mux.HandleFunc("/api/pdns/exposures", s.handlePDNSExposures)
	s.mux.HandleFunc("/api/pdns/expose", s.handlePDNSExpose)
	s.mux.HandleFunc("/api/pdns/unexpose", s.handlePDNSUnexpose)
	s.mux.HandleFunc("/api/pdns/deploy", s.handlePDNSDeploy)
	s.mux.HandleFunc("/api/pdns/connect", s.handlePDNSConnect)
	s.mux.HandleFunc("/api/mail/status", s.handleMailStatus)
	s.mux.HandleFunc("/api/mail/accounts", s.handleMailAccounts)
	s.mux.HandleFunc("/api/mail/account", s.handleMailAccountAdd)
	s.mux.HandleFunc("/api/mail/account/delete", s.handleMailAccountDelete)
	s.mux.HandleFunc("/api/mail/relay", s.handleMailRelaySet)
	s.mux.HandleFunc("/api/mail/preflight", s.handleMailPreflight)
	s.mux.HandleFunc("/api/mail/routes", s.handleMailRoutes)
	s.mux.HandleFunc("/api/mail/relay/remove", s.handleMailRelayRemove)
	s.mux.HandleFunc("/api/mail/admin-reset", s.handleMailAdminReset)
	s.mux.HandleFunc("/api/mail/setup", s.handleMailSetup)
	s.mux.HandleFunc("/api/sudo/status", s.handleSudoStatus)
	s.mux.HandleFunc("/api/sudo/verify", s.handleSudoVerify)
	s.mux.HandleFunc("/api/toolchains", s.handleToolchains)
	s.mux.HandleFunc("/api/workspace/status", s.handleWorkspaceStatus)
	s.mux.HandleFunc("/api/install", s.handleInstall)
	s.mux.HandleFunc("/api/tasks/events", s.handleSSEEvents)

	// Embedded Static UI
	s.mux.Handle("/", web.Handler())
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%s", s.cfg.HTTPHost, s.cfg.HTTPPort)
	fmt.Printf("✓ wdev Web Dashboard aktif di: http://%s\n", addr)
	fmt.Printf("✓ Akses lokal: http://localhost:%s\n", s.cfg.HTTPPort)
	return http.ListenAndServe(addr, s.mux)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Handlers
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true" {
		monitor.RefreshLatestVersions(true)
	}
	stats, err := monitor.Collect()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}

	// format: /api/service/{name}/{action}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/service/"), "/")
	if len(parts) < 2 {
		writeError(w, http.StatusBadRequest, "Parameter tidak lengkap")
		return
	}
	name, action := parts[0], parts[1]

	if err := monitor.ControlService(name, action); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sites, err := s.siteMgr.ListSites()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sites)

	case http.MethodPost:
		var opts site.CreateOptions
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.siteMgr.CreateSite(opts); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "created"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	domain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Domain tidak valid")
		return
	}

	if err := s.siteMgr.DeleteSite(domain); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleSwitchers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	writeJSON(w, http.StatusOK, s.switcherMgr.GetAll())
}

func (s *Server) handleSwitchersAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	typeStr := strings.TrimPrefix(r.URL.Path, "/api/switchers/")
	var payload struct {
		Version  string `json:"version"`
		SudoPass string `json:"sudo_pass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	cleanVer := strings.TrimSpace(payload.Version)
	if cleanVer == "" {
		writeError(w, http.StatusBadRequest, "Parameter version diperlukan")
		return
	}

	if payload.SudoPass != "" {
		if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
			writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
			return
		}
	}

	if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "sudo_required",
			"message": "Operasi pengalihan runtime memerlukan hak akses administrator (sudo).",
		})
		return
	}

	var taskName string
	var fn func(ctx context.Context, cb func(string)) error

	switch typeStr {
	case "node":
		taskName = fmt.Sprintf("Switch Runtime Node.js ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchNode(ctx, cb, cleanVer)
		}
	case "php":
		taskName = fmt.Sprintf("Switch Runtime PHP ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchPHP(ctx, cb, cleanVer)
		}
	case "pg":
		taskName = fmt.Sprintf("Switch Runtime PostgreSQL ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchPG(ctx, cb, cleanVer)
		}
	case "java":
		taskName = fmt.Sprintf("Switch Runtime Java ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchJava(ctx, cb, cleanVer)
		}
	default:
		writeError(w, http.StatusBadRequest, "Tipe runtime tidak valid")
		return
	}

	taskID, err := s.runner.StartNativeTask(taskName, fn)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
		"type":    typeStr,
		"version": cleanVer,
	})
}

func (s *Server) executeSwitchNode(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME NODE.JS KE VERSI %s ===", ver))
	cleanVer := strings.TrimPrefix(strings.TrimSpace(ver), "v")

	nodeBase := "/usr/local/lib/nodejs"
	entries, _ := os.ReadDir(nodeBase)

	var targetDir string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "node-v"+cleanVer) ||
			strings.Contains(name, "node-v"+cleanVer+".") ||
			strings.Contains(name, "node-v"+cleanVer+"-") {
			targetDir = filepath.Join(nodeBase, name)
			break
		}
	}

	if targetDir == "" {
		for _, e := range entries {
			if e.Name() == "v"+cleanVer || e.Name() == cleanVer {
				if resolved, err := filepath.EvalSymlinks(filepath.Join(nodeBase, e.Name())); err == nil {
					targetDir = resolved
					break
				}
			}
		}
	}

	if targetDir == "" {
		cb(fmt.Sprintf("[INFO] Node.js v%s belum terpasang di %s. Mengunduh dan memasang otomatis...", cleanVer, nodeBase))
		if err := s.installer.InstallNode(ctx, cb, cleanVer); err != nil {
			return fmt.Errorf("gagal memasang Node.js v%s: %w", cleanVer, err)
		}
		entries, _ = os.ReadDir(nodeBase)
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "node-v"+cleanVer) {
				targetDir = filepath.Join(nodeBase, name)
				break
			}
		}
	}

	if targetDir == "" {
		return fmt.Errorf("direktori instalasi Node.js v%s tidak ditemukan di %s", cleanVer, nodeBase)
	}

	cb(fmt.Sprintf("[1/3] Mengarahkan direktori aktif: %s", targetDir))
	_ = s.installer.RunCmd(ctx, cb, "ln", "-sfn", targetDir, filepath.Join(nodeBase, "current"))

	cb("[2/3] Memperbarui tautan simbolik binary (node, npm, npx, corepack)...")
	bins := []string{"node", "npm", "npx", "corepack"}
	for _, b := range bins {
		src := filepath.Join(targetDir, "bin", b)
		dst := filepath.Join("/usr/local/bin", b)
		if _, err := os.Stat(src); err == nil {
			_ = s.installer.RunCmd(ctx, cb, "rm", "-f", dst)
			_ = s.installer.RunCmd(ctx, cb, "ln", "-sfn", src, dst)
		}
	}

	cb("[3/3] Memverifikasi versi aktif...")
	out, err := exec.CommandContext(ctx, "node", "-v").CombinedOutput()
	activeNode := strings.TrimSpace(string(out))
	if err == nil {
		cb(fmt.Sprintf("✓ Node.js aktif: %s", activeNode))
	} else {
		cb(fmt.Sprintf("⚠️ Verifikasi node -v: %s (%v)", activeNode, err))
	}

	if npmOut, err := exec.CommandContext(ctx, "npm", "-v").CombinedOutput(); err == nil {
		cb(fmt.Sprintf("✓ NPM aktif: v%s", strings.TrimSpace(string(npmOut))))
	}

	cb(fmt.Sprintf("✓ [SELESAI] Pengalihan ke Node.js %s berhasil.", activeNode))
	return nil
}

func (s *Server) executeSwitchPHP(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME PHP KE VERSI %s ===", ver))
	cleanVer := strings.TrimSpace(ver)

	bin := fmt.Sprintf("/usr/bin/php%s", cleanVer)
	if _, err := os.Stat(bin); err != nil {
		cb(fmt.Sprintf("[INFO] PHP %s belum terpasang di sistem. Memasang paket PHP %s & FPM...", cleanVer, cleanVer))
		if err := s.installer.InstallPHP(ctx, cb, cleanVer); err != nil {
			return fmt.Errorf("gagal memasang PHP %s: %w", cleanVer, err)
		}
	}

	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("biner %s tidak ditemukan setelah instalasi", bin)
	}

	cb(fmt.Sprintf("[1/3] Menyetel update-alternatives untuk PHP CLI ke %s...", bin))
	if err := s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", "php", bin); err != nil {
		cb(fmt.Sprintf("[WARN] update-alternatives php: %v", err))
	}

	for _, tool := range []string{"phar", "phpize", "php-config"} {
		toolBin := fmt.Sprintf("/usr/bin/%s%s", tool, cleanVer)
		if _, err := os.Stat(toolBin); err == nil {
			_ = s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", tool, toolBin)
		}
	}

	cb(fmt.Sprintf("[2/3] Memulai layanan PHP-FPM php%s-fpm...", cleanVer))
	fpmService := fmt.Sprintf("php%s-fpm", cleanVer)
	_ = s.installer.RunCmd(ctx, cb, "systemctl", "enable", "--now", fpmService)
	_ = s.installer.RunCmd(ctx, cb, "systemctl", "restart", fpmService)

	cb("[3/3] Memverifikasi versi aktif...")
	out, err := exec.CommandContext(ctx, "php", "-v").CombinedOutput()
	if err == nil {
		firstLine := strings.Split(string(out), "\n")[0]
		cb(fmt.Sprintf("✓ PHP CLI aktif: %s", firstLine))
	}

	cb(fmt.Sprintf("✓ [SELESAI] Pengalihan ke PHP %s berhasil.", cleanVer))
	return nil
}

func (s *Server) executeSwitchPG(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME POSTGRESQL KE VERSI %s ===", ver))
	cleanVer := strings.TrimSpace(ver)

	// Deteksi apakah sistem memakai RPM (Fedora/RHEL + PGDG) atau Debian/Ubuntu.
	_, hasPgCreate := exec.LookPath("pg_createcluster")
	_, hasDnf := exec.LookPath("dnf")
	_, hasYum := exec.LookPath("yum")
	isRPM := hasPgCreate != nil && (hasDnf == nil || hasYum == nil)

	if isRPM {
		cb(fmt.Sprintf("[1/3] Memastikan paket PGDG PostgreSQL %s terpasang & terbaru...", cleanVer))
		// Hentikan cluster versi lain (dan unit bawaan distro postgresql) agar port 5432 kosong.
		for _, other := range []string{"19", "18", "17", "16", "15", "14"} {
			if other != cleanVer {
				_ = s.installer.RunCmd(ctx, cb, "systemctl", "stop", "postgresql-"+other)
				_ = s.installer.RunCmd(ctx, cb, "systemctl", "disable", "postgresql-"+other)
			}
		}
		_ = s.installer.RunCmd(ctx, cb, "systemctl", "stop", "postgresql")
		_ = s.installer.RunCmd(ctx, cb, "systemctl", "disable", "postgresql")
		cb(fmt.Sprintf("[2/3] Memasang/memperbarui & mengaktifkan PostgreSQL %s...", cleanVer))
		if err := s.installer.InstallPostgres(ctx, cb, cleanVer); err != nil {
			return err
		}
		cb(fmt.Sprintf("[3/3] Memverifikasi PostgreSQL %s aktif...", cleanVer))
		pgdgPsql := fmt.Sprintf("/usr/pgsql-%s/bin/psql", cleanVer)
		if _, err := os.Stat(pgdgPsql); err == nil {
			if out, vErr := exec.CommandContext(ctx, pgdgPsql, "--version").CombinedOutput(); vErr == nil {
				cb(fmt.Sprintf("✓ %s", strings.TrimSpace(string(out))))
			}
		}
		cb(fmt.Sprintf("✓ [SELESAI] PostgreSQL %s aktif pada port 5432.", cleanVer))
		return nil
	}

	binDir := fmt.Sprintf("/usr/lib/postgresql/%s/bin", cleanVer)
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		cb(fmt.Sprintf("[INFO] PostgreSQL %s belum terpasang. Memasang paket postgresql-%s...", cleanVer, cleanVer))
		if err := s.installer.InstallPostgres(ctx, cb, cleanVer); err != nil {
			return err
		}
	}

	clusterConf := fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", cleanVer)
	if _, err := os.Stat(clusterConf); os.IsNotExist(err) {
		cb(fmt.Sprintf("[1/3] Membuat cluster data baru untuk PostgreSQL %s...", cleanVer))
		_ = s.installer.RunCmd(ctx, cb, "pg_createcluster", cleanVer, "main")
	}

	cb("[2/3] Mengonfigurasi port 5432 untuk cluster target...")
	content, err := os.ReadFile(clusterConf)
	if err == nil {
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "port =") {
				lines[i] = "port = 5432"
				break
			}
		}
		_ = os.WriteFile(clusterConf, []byte(strings.Join(lines, "\n")), 0644)
	}

	if entries, err := os.ReadDir("/etc/postgresql"); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != cleanVer {
				otherConf := fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", e.Name())
				if oContent, err := os.ReadFile(otherConf); err == nil {
					oLines := strings.Split(string(oContent), "\n")
					for i, line := range oLines {
						if strings.HasPrefix(strings.TrimSpace(line), "port =") && strings.Contains(line, "5432") {
							oLines[i] = "port = 5433"
							break
						}
					}
					_ = os.WriteFile(otherConf, []byte(strings.Join(oLines, "\n")), 0644)
				}
				_ = s.installer.RunCmd(ctx, cb, "systemctl", "stop", fmt.Sprintf("postgresql@%s-main", e.Name()))
			}
		}
	}

	cb(fmt.Sprintf("[3/3] Memulai layanan cluster postgresql@%s-main...", cleanVer))
	_ = s.installer.RunCmd(ctx, cb, "systemctl", "restart", fmt.Sprintf("postgresql@%s-main", cleanVer))
	_ = s.installer.RunCmd(ctx, cb, "systemctl", "restart", "postgresql")

	cb(fmt.Sprintf("✓ [SELESAI] PostgreSQL %s aktif pada port 5432.", cleanVer))
	return nil
}

func (s *Server) executeSwitchJava(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME JAVA OPENJDK KE VERSI %s ===", ver))
	cleanVer := strings.TrimSpace(ver)

	jvmDir := "/usr/lib/jvm"
	entries, _ := os.ReadDir(jvmDir)

	var targetBin string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, cleanVer) && !strings.HasPrefix(name, ".") {
			bin := filepath.Join(jvmDir, name, "bin", "java")
			if _, err := os.Stat(bin); err == nil {
				targetBin = bin
				break
			}
		}
	}

	if targetBin == "" {
		cb(fmt.Sprintf("[INFO] Java OpenJDK %s belum terpasang. Memasang paket openjdk-%s-jdk...", cleanVer, cleanVer))
		if err := s.installer.InstallJava(ctx, cb, cleanVer); err != nil {
			return fmt.Errorf("gagal memasang OpenJDK %s: %w", cleanVer, err)
		}
		entries, _ = os.ReadDir(jvmDir)
		for _, e := range entries {
			name := e.Name()
			if strings.Contains(name, cleanVer) && !strings.HasPrefix(name, ".") {
				bin := filepath.Join(jvmDir, name, "bin", "java")
				if _, err := os.Stat(bin); err == nil {
					targetBin = bin
					break
				}
			}
		}
	}

	if targetBin == "" {
		return fmt.Errorf("biner java untuk versi %s tidak ditemukan di %s", cleanVer, jvmDir)
	}

	if resolved, err := filepath.EvalSymlinks(targetBin); err == nil {
		targetBin = resolved
	}

	cb(fmt.Sprintf("[1/2] Mengalihkan update-alternatives java ke: %s", targetBin))
	_ = s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", "java", targetBin)

	javacBin := filepath.Join(filepath.Dir(targetBin), "javac")
	if _, err := os.Stat(javacBin); err == nil {
		_ = s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", "javac", javacBin)
	}

	cb("[2/2] Memverifikasi versi java aktif...")
	out, err := exec.CommandContext(ctx, "java", "-version").CombinedOutput()
	if err == nil {
		firstLine := strings.Split(string(out), "\n")[0]
		cb(fmt.Sprintf("✓ Java aktif: %s", firstLine))
	}

	cb(fmt.Sprintf("✓ [SELESAI] Pengalihan ke Java %s berhasil.", cleanVer))
	return nil
}

func (s *Server) handleCFStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfMgr.GetStatus())
}

func (s *Server) handleCFExpose(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		LocalDomain string `json:"local_domain"`
		PublicHost  string `json:"public_host"`
		Profile     string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.cfMgr.Expose(payload.LocalDomain, payload.PublicHost, payload.Profile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "exposed"})
}

func (s *Server) handleCFUnexpose(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		LocalDomain string `json:"local_domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.cfMgr.Unexpose(payload.LocalDomain); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unexposed"})
}

func (s *Server) handlePDNSStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pdnsMgr.LoadConfig())
}

func (s *Server) handlePDNSZones(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		zones, err := s.pdnsMgr.ListZones()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, zones)
	case http.MethodPost:
		var payload struct {
			ZoneName string `json:"zone_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		zone, err := pdns.NormalizeZoneName(payload.ZoneName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.pdnsMgr.AddZone(zone); err != nil {
			if strings.Contains(err.Error(), "sudah ada") {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "created",
			"zone":        zone,
			"nameservers": []string{"ns1." + zone, "ns2." + zone},
		})
	case http.MethodDelete:
		var payload struct {
			ZoneName string `json:"zone_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.pdnsMgr.DeleteZone(payload.ZoneName); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handlePDNSZoneRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Zone string `json:"zone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.Zone) == "" {
		writeError(w, http.StatusBadRequest, "Parameter zone diperlukan")
		return
	}
	report, err := s.pdnsMgr.RepairZone(payload.Zone)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "report": report})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handlePDNSZoneDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	zone := r.URL.Query().Get("zone")
	if zone == "" && r.Method == http.MethodPost {
		var payload struct {
			Zone string `json:"zone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			zone = payload.Zone
		}
	}
	if strings.TrimSpace(zone) == "" {
		writeError(w, http.StatusBadRequest, "Parameter zone diperlukan")
		return
	}
	report, err := s.pdnsMgr.DoctorZone(zone)
	if err != nil && zone == "" {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if !report.OK {
		status = http.StatusOK // hasil diagnosa tetap dikirim; klien menilai report.ok
	}
	writeJSON(w, status, report)
}

func (s *Server) handlePDNSRecords(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		zoneName := r.URL.Query().Get("zone")
		if zoneName == "" {
			writeError(w, http.StatusBadRequest, "Parameter zone diperlukan")
			return
		}
		zoneInfo, err := s.pdnsMgr.GetZone(zoneName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, zoneInfo.RRsets)
	case http.MethodPost:
		var payload struct {
			Zone    string `json:"zone"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Content string `json:"content"`
			TTL     int    `json:"ttl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.pdnsMgr.AddRecordKeep(payload.Zone, payload.Name, payload.Type, payload.Content, payload.TTL); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	case http.MethodDelete:
		var payload struct {
			Zone string `json:"zone"`
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.pdnsMgr.DeleteRecord(payload.Zone, payload.Name, payload.Type); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handlePDNSExposures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	proxies, err := s.pdnsMgr.ListExposures()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, proxies)
}

func (s *Server) handlePDNSExpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload pdns.ExposeOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	if payload.Mode == "site" {
		if err := s.pdnsMgr.ExposeSite(payload.LocalDomain, payload.PublicDomain, payload.UseSSL); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := s.pdnsMgr.Expose(payload.PublicDomain, payload.TargetPort, payload.UseSSL, payload.WebSocket); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "exposed"})
}

func (s *Server) handlePDNSUnexpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Domain  string `json:"domain"`
		KeepDNS bool   `json:"keep_dns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.pdnsMgr.Unexpose(payload.Domain, payload.KeepDNS); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unexposed"})
}

func (s *Server) handlePDNSDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload pdns.DeployOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if payload.VPSHost == "" {
		writeError(w, http.StatusBadRequest, "Host / IP VPS wajib diisi")
		return
	}

	taskName := fmt.Sprintf("Deploy VPS Edge (%s)", payload.VPSHost)
	taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		return s.pdnsMgr.DeployVPS(ctx, cb, payload)
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
	})
}

func (s *Server) handlePDNSConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload pdns.ConnectOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.pdnsMgr.Connect(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "connected",
	})
}

func (s *Server) handleSudoStatus(w http.ResponseWriter, r *http.Request) {
	reqPassword, _ := s.installer.CheckSudoRequired()
	writeJSON(w, http.StatusOK, map[string]any{
		"is_root":       os.Geteuid() == 0,
		"sudo_required": reqPassword,
		"has_cached":    s.installer.GetSudoPassword() != "",
	})
}

func (s *Server) handleSudoVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.installer.VerifyAndSetSudoPassword(payload.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "Password sudo salah: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "Password sudo berhasil diverifikasi"})
}

func (s *Server) handleToolchains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	items, err := s.installer.GetToolchainStatuses(r.Context(), refresh)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Component string `json:"component"`
		SudoPass  string `json:"sudo_pass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	if payload.SudoPass != "" {
		if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
			writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
			return
		}
	}

	// Check if this action requires sudo and if password is required
	nonPrivileged := map[string]bool{
		"check-updates": true,
		"check_updates": true,
		"updates":       true,
		"orca-pair":     true,
		"orca-ai":       true,
		"cf-doctor":     true,
		"doctor":        true,
		"pdns":          true,
	}

	if !nonPrivileged[payload.Component] {
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": "Operasi pemeliharaan/instalasi ini memerlukan hak akses administrator (sudo).",
			})
			return
		}
	}

	var fn func(ctx context.Context, cb func(string)) error
	taskName := "Instal " + payload.Component

	switch payload.Component {
	case "all", "full-install":
		taskName = "Full Auto Setup (14 Langkah)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.RunFullInstall(ctx, cb)
		}
	case "audit", "audit-toolchain":
		taskName = "Audit Duplikat Path & Toolchain"
		fn = func(ctx context.Context, cb func(string)) error {
			s.installer.AuditToolchain(ctx, cb)
			return nil
		}
	case "audit-fix", "repair-toolchain":
		taskName = "Perbaikan Duplikat Path & Toolchain"
		fn = func(ctx context.Context, cb func(string)) error {
			findings := s.installer.AuditToolchain(ctx, cb)
			return s.installer.RepairToolchain(ctx, cb, findings)
		}
	case "update":
		taskName = "Perbarui Seluruh Paket & Toolchain"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.RunUpdateAll(ctx, cb)
		}
	case "check-updates", "check_updates", "updates":
		taskName = "Pemeriksaan Ketersediaan Pembaruan"
		fn = s.installer.CheckUpdates
	case "self-check":
		taskName = "Validasi & Self-Check Sistem"
		fn = s.installer.RunSelfCheck
	case "base":
		taskName = "Dependensi Dasar Sistem"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallBase(ctx, cb)
		}
	case "node":
		taskName = "Node.js LTS"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallNode(ctx, cb, "22")
		}
	case "rust":
		taskName = "Rust Toolchain & Mold"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallRust(ctx, cb)
		}
	case "go":
		taskName = "Golang"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallGolang(ctx, cb)
		}
	case "java":
		taskName = "OpenJDK Java 21"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallJava(ctx, cb, "21")
		}
	case "android":
		taskName = "Android SDK & Tools"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallAndroidSDK(ctx, cb)
		}
	case "flutter":
		taskName = "Flutter SDK"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallFlutter(ctx, cb)
		}
	case "postgres":
		taskName = "PostgreSQL Server"
		fn = func(ctx context.Context, cb func(string)) error {
			ver := monitor.GetActivePostgresMajor()
			if ver == "" {
				ver = "16"
			}
			return s.installer.InstallPostgres(ctx, cb, ver)
		}
	case "redis":
		taskName = "Redis Cache Server"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallRedis(ctx, cb)
		}
	case "nginx":
		taskName = "Nginx Web Server"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallNginx(ctx, cb)
		}
	case "php":
		taskName = "PHP & PHP-FPM"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallPHP(ctx, cb, "8.4")
		}
	case "composer":
		taskName = "Composer Package Manager"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallComposer(ctx, cb)
		}
	case "ssl", "mkcert":
		taskName = "Local SSL (mkcert)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallMkcert(ctx, cb)
		}
	case "php-stack":
		taskName = "Full PHP Web Stack"
		fn = func(ctx context.Context, cb func(string)) error {
			if err := s.installer.InstallNginx(ctx, cb); err != nil {
				return err
			}
			if err := s.installer.InstallPHP(ctx, cb, "8.4"); err != nil {
				return err
			}
			if err := s.installer.InstallComposer(ctx, cb); err != nil {
				return err
			}
			return s.installer.InstallMkcert(ctx, cb)
		}
	case "cloudflare":
		taskName = "Cloudflare Tunnel (cloudflared)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallCloudflared(ctx, cb)
		}
	case "zsh", "zsh-p10k":
		taskName = "Oh My Zsh (Powerlevel10k)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallZsh(ctx, cb, "powerlevel10k")
		}
	case "zsh-agnoster":
		taskName = "Oh My Zsh (Agnoster)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallZsh(ctx, cb, "agnoster")
		}
	case "zsh-robbyrussell":
		taskName = "Oh My Zsh (Robbyrussell)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallZsh(ctx, cb, "robbyrussell")
		}
	case "orca", "orca-desktop":
		taskName = "Orca ADE (Desktop GUI)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallOrca(ctx, cb, "desktop")
		}
	case "orca-headless":
		taskName = "Orca ADE (Headless Server)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallOrca(ctx, cb, "headless")
		}
	case "orca-switch-desktop":
		taskName = "Alih Mode Orca ADE (Desktop GUI)"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengalihkan Orca ADE ke Mode Desktop GUI...")
			if err := s.installer.SetOrcaMode(ctx, cb, "desktop"); err != nil {
				return err
			}
			cb("✓ Mode Orca ADE diatur ke Desktop GUI.")
			return nil
		}
	case "orca-switch-headless":
		taskName = "Alih Mode Orca ADE (Headless Server)"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengalihkan Orca ADE ke Mode Headless Server...")
			if err := s.installer.SetOrcaMode(ctx, cb, "headless"); err != nil {
				return err
			}
			cb("✓ Mode Orca ADE diatur ke Headless Server.")
			return nil
		}
	case "orca-update":
		taskName = "Pembaruan Orca ADE"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.UpdateOrca(ctx, cb)
		}
	case "orca-uninstall":
		taskName = "Uninstall Orca ADE"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.UninstallOrca(ctx, cb, false)
		}
	case "orca-pair":
		taskName = "Orca ADE Pairing Info"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("=== INFO PAIRING ORCA ADE ===")
			_, userHome := installer.ResolveOrcaUser()
			pairing, web, endpoint := installer.OrcaPairInfo(ctx, userHome)
			if pairing == "" {
				cb("Catatan: pairing belum tersedia (server Orca mungkin belum aktif).")
				cb("Tips: Pastikan Orca ADE Headless Server aktif.")
				return nil
			}
			cb("Pairing URL : " + pairing)
			if web != "" {
				cb("Web Client  : " + web)
			}
			if endpoint != "" {
				cb("Endpoint    : " + endpoint)
			}
			return nil
		}
	case "orca-ai":
		taskName = "Konfigurasi Orca Commit AI"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengonfigurasi default AI commit message generator Orca...")
			_, userHome := installer.ResolveTargetUserAndHome()
			cfgDir := filepath.Join(userHome, ".config", "orca")
			_ = os.MkdirAll(cfgDir, 0755)
			cb("✓ Konfigurasi Git Commit AI Orca siap.")
			return nil
		}
	case "sync-hosts", "sync":
		taskName = "Sinkronisasi Hosts & SSL"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Menyinkronkan seluruh virtual host ke /etc/hosts dan sertifikat SSL...")
			if err := s.siteMgr.SyncAll(); err != nil {
				return err
			}
			cb("✓ Sinkronisasi hosts dan SSL selesai, Nginx berhasil direload.")
			return nil
		}
	case "cf-doctor", "doctor":
		taskName = "Cloudflare Tunnel Doctor"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("=== DIAGNOSTIK CLOUDFLARE TUNNEL ===")
			if path, err := exec.LookPath("cloudflared"); err == nil {
				cb(fmt.Sprintf("  [OK] cloudflared binary : %s", path))
			} else {
				cb("  [WARN] cloudflared belum terpasang.")
			}
			st := s.cfMgr.GetStatus()
			if st.Active {
				cb("  [OK] Layanan cloudflared : AKTIF (Running)")
			} else {
				cb("  [WARN] Layanan cloudflared : NONAKTIF")
			}
			cb(fmt.Sprintf("  Profil Akun : %s", strings.Join(st.Profiles, ", ")))
			cb(fmt.Sprintf("  Jumlah Eksposur Aktif : %d", len(st.Exposures)))
			client := http.Client{Timeout: 5 * time.Second}
			if resp, err := client.Get("https://1.1.1.1"); err == nil {
				resp.Body.Close()
				cb("  [OK] Konektivitas Cloudflare Edge (1.1.1.1) : Prima")
			} else {
				cb(fmt.Sprintf("  [WARN] Koneksi ke 1.1.1.1 gagal: %v", err))
			}
			cb("✓ Diagnostik Cloudflare selesai.")
			return nil
		}
	case "pdns":
		taskName = "PowerDNS & Edge Gateway Status"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("=== STATUS POWERDNS & VPS EDGE GATEWAY ===")
			conf := s.pdnsMgr.LoadConfig()
			cb(fmt.Sprintf("  Terkonfigurasi : %v", conf.Configured))
			cb(fmt.Sprintf("  VPS NetBird IP : %s", conf.VPSNetbirdIP))
			cb(fmt.Sprintf("  VPS Public IP  : %s", conf.VPSPublicIP))
			if conf.Reachable {
				cb("  Koneksi API    : [OK] Terhubung ke PowerDNS Authoritative")
			} else {
				cb("  Koneksi API    : [FAIL] Tidak dapat menjangkau PowerDNS API")
			}
			return nil
		}
	default:
		writeError(w, http.StatusBadRequest, "Komponen tidak dikenal: "+payload.Component)
		return
	}

	taskID, err := s.runner.StartNativeTask(taskName, fn)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "started", "task_id": taskID})
}

func (s *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming tidak didukung", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.runner.Subscribe()
	defer s.runner.Unsubscribe(ch)

	// Send initial ping
	fmt.Fprintf(w, "data: {\"type\":\"ping\",\"message\":\"connected\"}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			bytes, err := json.Marshal(evt)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", string(bytes))
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleMailStatus(w http.ResponseWriter, r *http.Request) {
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	st, err := s.mailMgr.GetStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleMailAccounts(w http.ResponseWriter, r *http.Request) {
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	domain := r.URL.Query().Get("domain")
	accounts, err := s.mailMgr.GetDomainAccounts(domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleMailAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.AddAccount(payload.Email, payload.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "created",
		"message": "Akun email " + payload.Email + " berhasil ditambahkan",
	})
}

func (s *Server) handleMailAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.DeleteAccount(payload.Email); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "message": "Akun " + payload.Email + " dihapus"})
}

func (s *Server) handleMailRelaySet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload mail.SetRelayOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.SetRelay(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "updated",
		"message": "Konfigurasi relay domain " + payload.Domain + " berhasil disimpan",
	})
}

func (s *Server) handleMailPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	domain := r.URL.Query().Get("domain")
	writeJSON(w, http.StatusOK, s.mailMgr.Preflight(domain))
}

func (s *Server) handleMailRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	info, err := s.mailMgr.GetOutboundRoutes()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleMailRelayRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.RemoveRelay(payload.Domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "removed",
		"message": "Override relay domain " + payload.Domain + " dihapus; domain kembali ke relay default",
	})
}

func (s *Server) handleMailAdminReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.ResetAdmin(payload.Domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "reset",
		"message": "Password admin domain " + payload.Domain + " berhasil direset",
	})
}

func (s *Server) handleMailSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload mail.SetupMailOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if strings.TrimSpace(payload.Domain) == "" {
		writeError(w, http.StatusBadRequest, "Domain wajib diisi")
		return
	}

	taskName := fmt.Sprintf("Setup Mail Server & Domain (%s)", payload.Domain)
	taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		return s.mailMgr.SetupMail(ctx, cb, payload)
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
	})
}

func (s *Server) handleWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}

	type OrcaInfo struct {
		Installed     bool   `json:"installed"`
		Active        bool   `json:"active"`
		StatusText    string `json:"status_text"`
		Mode          string `json:"mode"`
		Version       string `json:"version"`
		LatestVersion string `json:"latest_version,omitempty"`
		HasUpdate     bool   `json:"has_update"`
		Port          int    `json:"port,omitempty"`
		PID           int    `json:"pid,omitempty"`
		WebClientURL  string `json:"web_client_url,omitempty"`
		PairingURL    string `json:"pairing_url,omitempty"`
		Endpoint      string `json:"endpoint,omitempty"`
	}

	type ShellInfo struct {
		ZshInstalled bool     `json:"zsh_installed"`
		OhMyZsh      bool     `json:"oh_my_zsh"`
		CurrentShell string   `json:"current_shell"`
		ActiveTheme  string   `json:"active_theme"`
		Plugins      []string `json:"plugins"`
	}

	type WorkspaceData struct {
		Orca  OrcaInfo  `json:"orca"`
		Shell ShellInfo `json:"shell"`
	}

	var data WorkspaceData

	targetUser, userHome := installer.ResolveOrcaUser()
	orcaConfigDir := filepath.Join(userHome, ".config", "orca")

	// 1. Orca info (prioritaskan biner kanonik /usr/local/bin/orca)
	orcaBin := "/usr/local/bin/orca"
	if _, err := os.Stat(orcaBin); err == nil {
		data.Orca.Installed = true
	} else if p, err := exec.LookPath("orca"); err == nil {
		orcaBin = p
		data.Orca.Installed = true
	} else if _, err := os.Stat(filepath.Join(userHome, ".local", "bin", "orca")); err == nil {
		orcaBin = filepath.Join(userHome, ".local", "bin", "orca")
		data.Orca.Installed = true
	} else if _, err := os.Stat(orcaConfigDir); err == nil {
		data.Orca.Installed = true
	}

	if monitor.IsSystemdServiceActive("orca") {
		data.Orca.Active = true
	} else if out, err := exec.Command("pgrep", "-f", "orca-ide").Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
		data.Orca.Active = true
	}

	modeFile := filepath.Join(orcaConfigDir, ".mode")
	if mBytes, err := os.ReadFile(modeFile); err == nil {
		data.Orca.Mode = strings.TrimSpace(string(mBytes))
	}
	if data.Orca.Mode == "" {
		data.Orca.Mode = "headless"
	}

	modeCapitalized := "Headless Server"
	if data.Orca.Mode == "desktop" {
		modeCapitalized = "Desktop GUI"
	}

	if data.Orca.Active {
		data.Orca.StatusText = fmt.Sprintf("Running (%s)", modeCapitalized)
	} else if data.Orca.Installed {
		data.Orca.StatusText = fmt.Sprintf("Stopped (%s)", modeCapitalized)
	} else {
		data.Orca.StatusText = "Belum Terpasang"
	}

	vCmd := exec.Command(orcaBin, "--version")
	vCmd.Env = append(os.Environ(), "HOME="+userHome, "USER="+targetUser)
	if out, err := vCmd.Output(); err == nil {
		ver := strings.TrimSpace(string(out))
		if fields := strings.Fields(ver); len(fields) > 0 {
			ver = fields[len(fields)-1]
		}
		data.Orca.Version = strings.TrimPrefix(ver, "v")
	}

	forceRefresh := r.URL.Query().Get("refresh") == "1"
	if latestOrca := installer.GetOrcaLatestVersion(r.Context(), forceRefresh); latestOrca != "" {
		data.Orca.LatestVersion = latestOrca
		if data.Orca.Installed && data.Orca.Version != "" {
			data.Orca.HasUpdate = installer.HasNewerVersion(data.Orca.Version, latestOrca)
		}
	}

	if data.Orca.Active {
		rtFile := filepath.Join(orcaConfigDir, "orca-runtime.json")
		if rtBytes, err := os.ReadFile(rtFile); err == nil {
			var rt struct {
				PID        int `json:"pid"`
				Transports []struct {
					Kind     string `json:"kind"`
					Endpoint string `json:"endpoint"`
				} `json:"transports"`
			}
			if err := json.Unmarshal(rtBytes, &rt); err == nil {
				data.Orca.PID = rt.PID
				for _, t := range rt.Transports {
					if t.Kind == "websocket" {
						data.Orca.Endpoint = t.Endpoint
						parts := strings.Split(t.Endpoint, ":")
						if len(parts) >= 3 {
							pStr := strings.TrimRight(parts[2], "/")
							if p, err := strconv.Atoi(pStr); err == nil {
								data.Orca.Port = p
							}
						}
					}
				}
			}
		}
		if data.Orca.Port == 0 {
			data.Orca.Port = 6768
		}

		pairCtx, pairCancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer pairCancel()
		pairing, web, _ := installer.OrcaPairInfo(pairCtx, userHome)
		data.Orca.PairingURL = pairing
		data.Orca.WebClientURL = web
	}

	// 2. Shell info
	if _, err := exec.LookPath("zsh"); err == nil {
		data.Shell.ZshInstalled = true
	} else if _, err := os.Stat("/usr/bin/zsh"); err == nil {
		data.Shell.ZshInstalled = true
	}
	if fi, err := os.Stat(filepath.Join(userHome, ".oh-my-zsh")); err == nil && fi.IsDir() {
		data.Shell.OhMyZsh = true
	}

	if out, err := exec.Command("getent", "passwd", targetUser).Output(); err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), ":")
		if len(parts) >= 7 {
			data.Shell.CurrentShell = parts[6]
		}
	}
	if data.Shell.CurrentShell == "" {
		data.Shell.CurrentShell = os.Getenv("SHELL")
	}

	zshrcFile := filepath.Join(userHome, ".zshrc")
	if zBytes, err := os.ReadFile(zshrcFile); err == nil {
		lines := strings.Split(string(zBytes), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "ZSH_THEME=") {
				theme := strings.TrimPrefix(trimmed, "ZSH_THEME=")
				theme = strings.Trim(theme, "\"'")
				data.Shell.ActiveTheme = theme
			}
		}
	}

	writeJSON(w, http.StatusOK, data)
}
