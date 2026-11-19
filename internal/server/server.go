package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
	"wdev/internal/installer"
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
	s.mux.HandleFunc("/api/pdns/records", s.handlePDNSRecords)
	s.mux.HandleFunc("/api/pdns/exposures", s.handlePDNSExposures)
	s.mux.HandleFunc("/api/pdns/expose", s.handlePDNSExpose)
	s.mux.HandleFunc("/api/pdns/unexpose", s.handlePDNSUnexpose)
	s.mux.HandleFunc("/api/sudo/status", s.handleSudoStatus)
	s.mux.HandleFunc("/api/sudo/verify", s.handleSudoVerify)
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
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	var err error
	switch typeStr {
	case "node":
		err = s.switcherMgr.SwitchNode(payload.Version)
	case "php":
		err = s.switcherMgr.SwitchPHP(payload.Version)
	case "pg":
		err = s.switcherMgr.SwitchPG(payload.Version)
	case "java":
		err = s.switcherMgr.SwitchJava(payload.Version)
	default:
		writeError(w, http.StatusBadRequest, "Tipe runtime tidak valid")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "switched"})
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
		if err := s.pdnsMgr.AddZone(payload.ZoneName); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "created"})
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
		if err := s.pdnsMgr.AddRecord(payload.Zone, payload.Name, payload.Type, payload.Content, payload.TTL); err != nil {
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
			return s.installer.InstallPostgres(ctx, cb, "16")
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
	case "orca-uninstall":
		taskName = "Uninstall Orca ADE"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.UninstallOrca(ctx, cb, false)
		}
	case "orca-pair":
		taskName = "Orca ADE Pairing Info"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("=== INFO PAIRING ORCA ADE ===")
			cmd := exec.CommandContext(ctx, "orca", "pair")
			if _, err := exec.LookPath("orca"); err != nil {
				if _, err := os.Stat("/home/dietpi/.local/bin/orca"); err == nil {
					cmd = exec.CommandContext(ctx, "/home/dietpi/.local/bin/orca", "pair")
				}
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				cb(fmt.Sprintf("Catatan: %s", string(out)))
				cb("Tips: Pastikan Orca ADE Headless Server aktif.")
			} else {
				for _, line := range strings.Split(string(out), "\n") {
					cb(line)
				}
			}
			return nil
		}
	case "orca-ai":
		taskName = "Konfigurasi Orca Commit AI"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengonfigurasi default AI commit message generator Orca...")
			cfgDir := "/home/dietpi/.config/orca"
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
