package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"wdev/internal/serviceconfig"
	"wdev/internal/siteapp"
	"wdev/internal/store"
)

// SiteWithAppRecord menggabungkan SiteRecord dengan metadata Managed App dan status systemd live.
type SiteWithAppRecord struct {
	store.SiteRecord
	App       *store.SiteAppRecord      `json:"app,omitempty"`
	AppStatus *siteapp.AppRuntimeStatus `json:"app_status,omitempty"`
}

type siteAppPayload struct {
	BuildCommand     string `json:"build_command,omitempty"`
	StartCommand     string `json:"start_command,omitempty"`
	RunUser          string `json:"run_user,omitempty"`
	AutoStart        *bool  `json:"auto_start,omitempty"`
	RunBuildNow      *bool  `json:"run_build_now,omitempty"`
	ImportProjectEnv bool   `json:"import_project_env,omitempty"`
	InitialEnvRaw    string `json:"initial_env_raw,omitempty"`
}

func (s *Server) ensureSiteAppManager() *siteapp.Manager {
	if s.siteAppMgr == nil {
		var sudoFn func() string
		if s.installer != nil {
			sudoFn = s.installer.GetSudoPassword
		}
		s.siteAppMgr = siteapp.New(s.cfg, s.store, sudoFn)
		if s.siteMgr != nil {
			mgr := s.siteAppMgr
			s.siteMgr.SetAppCleanupHook(func(domain string) error {
				return mgr.RemoveApp(context.Background(), domain)
			})
		}
	}
	return s.siteAppMgr
}

func (s *Server) enrichSitesWithApps(ctx context.Context, sites []store.SiteRecord) []SiteWithAppRecord {
	out := make([]SiteWithAppRecord, 0, len(sites))
	if len(sites) == 0 {
		return out
	}

	appMap := make(map[string]store.SiteAppRecord)
	if s.store != nil {
		if apps, err := s.store.ListSiteApps(); err == nil {
			for _, a := range apps {
				appMap[strings.ToLower(strings.TrimSpace(a.Domain))] = a
			}
		}
	}

	var managedDomains []string
	for _, st := range sites {
		domKey := strings.ToLower(strings.TrimSpace(st.Domain))
		if store.IsManagedAppFramework(st.Framework) {
			managedDomains = append(managedDomains, domKey)
		} else if _, hasApp := appMap[domKey]; hasApp {
			managedDomains = append(managedDomains, domKey)
		}
	}

	var statusMap map[string]siteapp.AppRuntimeStatus
	if len(managedDomains) > 0 {
		statusMap = s.ensureSiteAppManager().BatchStatus(ctx, managedDomains)
	}

	for _, st := range sites {
		item := SiteWithAppRecord{SiteRecord: st}
		domKey := strings.ToLower(strings.TrimSpace(st.Domain))
		if appRec, ok := appMap[domKey]; ok {
			copyRec := appRec
			item.App = &copyRec
		}
		if stVal, ok := statusMap[domKey]; ok {
			copySt := stVal
			item.AppStatus = &copySt
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) enrichSingleSiteWithApp(ctx context.Context, st store.SiteRecord) SiteWithAppRecord {
	list := s.enrichSitesWithApps(ctx, []store.SiteRecord{st})
	if len(list) > 0 {
		return list[0]
	}
	return SiteWithAppRecord{SiteRecord: st}
}

func (s *Server) reservedSitePortsMap() map[int]string {
	reserved := map[int]string{
		5432: "PostgreSQL",
		6379: "Redis",
		8081: "PowerDNS API",
		8082: "Edge Agent",
	}
	if s.pdnsMgr != nil {
		conf := s.pdnsMgr.LoadConfig()
		if p, err := strconv.Atoi(strings.TrimSpace(conf.PDNSPort)); err == nil && p > 0 {
			reserved[p] = "PowerDNS API"
		}
		if p, err := strconv.Atoi(strings.TrimSpace(conf.AgentPort)); err == nil && p > 0 {
			reserved[p] = "Edge Agent"
		}
	}
	if isStalwartInstalled() {
		reserved[8080] = "Stalwart Mail Admin"
	}
	return reserved
}

func (s *Server) suggestFreeSitePort() (int, error) {
	used := make(map[int]bool)
	if s.siteMgr != nil {
		if sites, err := s.siteMgr.ListSites(); err == nil {
			for _, st := range sites {
				if st.ProxyPort > 0 {
					used[st.ProxyPort] = true
				}
			}
		}
	}
	if s.store != nil {
		if apps, err := s.store.ListSiteApps(); err == nil {
			for _, app := range apps {
				if app.Port > 0 {
					used[app.Port] = true
				}
			}
		}
	}

	reserved := make(map[int]bool)
	for p := range s.reservedSitePortsMap() {
		reserved[p] = true
	}
	if panelPort := s.panelPortNumber(); panelPort > 0 {
		reserved[panelPort] = true
	}
	return siteapp.SuggestFreePort(used, reserved)
}

func (s *Server) handleInspectSiteDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "Path direktori wajib diisi")
		return
	}

	res, err := s.ensureSiteAppManager().InspectProject(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	suggestedPort := res.DetectedPortHint
	if suggestedPort <= 0 || s.validateSiteProxyTarget("nodejs", suggestedPort, "") != nil {
		if freePort, pErr := s.suggestFreeSitePort(); pErr == nil {
			suggestedPort = freePort
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"inspect":        res,
		"suggested_port": suggestedPort,
	})
}

func (s *Server) handleSuggestSitePort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	port, err := s.suggestFreeSitePort()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"port": port})
}

func (s *Server) writeSudoRequiredIfNeeded(w http.ResponseWriter, err error, msg string) bool {
	if errors.Is(err, serviceconfig.ErrSudoRequired) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "sudo_required",
			"message": msg,
		})
		return true
	}
	if s.installer != nil && os.Geteuid() != 0 {
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": msg,
			})
			return true
		}
	}
	return false
}

func (s *Server) handleSiteAppAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var req struct {
		Domain   string `json:"domain"`
		Action   string `json:"action"`
		SudoPass string `json:"sudo_pass,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	if req.SudoPass != "" {
		if !s.verifySudoPasswordWithRateLimit(w, r, req.SudoPass, "Password sudo tidak valid: ") {
			return
		}
	}

	mgr := s.ensureSiteAppManager()
	if err := mgr.Action(r.Context(), req.Domain, req.Action); err != nil {
		if s.writeSudoRequiredIfNeeded(w, err, "Mengontrol service systemd aplikasi memerlukan hak akses administrator (sudo).") {
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	st := mgr.Status(r.Context(), req.Domain)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"app_status": st,
	})
}

func (s *Server) handleSiteAppBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var req struct {
		Domain   string `json:"domain"`
		Restart  *bool  `json:"restart,omitempty"`
		SudoPass string `json:"sudo_pass,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	if req.SudoPass != "" {
		if !s.verifySudoPasswordWithRateLimit(w, r, req.SudoPass, "Password sudo tidak valid: ") {
			return
		}
	}

	restartAfter := true
	if req.Restart != nil {
		restartAfter = *req.Restart
	}
	if restartAfter && os.Geteuid() != 0 && s.installer != nil {
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": "Me-restart service systemd setelah build memerlukan hak akses administrator (sudo).",
			})
			return
		}
	}

	taskID, err := s.ensureSiteAppManager().StartBuild(s.runner, req.Domain, restartAfter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
	})
}

func (s *Server) handleSiteAppEnv(w http.ResponseWriter, r *http.Request) {
	mgr := s.ensureSiteAppManager()

	switch r.Method {
	case http.MethodGet:
		domain := strings.TrimSpace(r.URL.Query().Get("domain"))
		if domain == "" {
			writeError(w, http.StatusBadRequest, "Parameter domain wajib diisi")
			return
		}
		entries, raw, err := mgr.GetEnv(r.Context(), domain)
		if err != nil {
			if s.writeSudoRequiredIfNeeded(w, err, "Membaca file .env terkelola (0600) memerlukan hak akses administrator (sudo).") {
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"domain":   domain,
			"env_file": mgr.EnvPath(domain),
			"entries":  entries,
			"raw":      raw,
		})

	case http.MethodPut, http.MethodPost:
		var req struct {
			Domain   string             `json:"domain"`
			Entries  []siteapp.EnvEntry `json:"entries,omitempty"`
			Raw      string             `json:"raw,omitempty"`
			Restart  bool               `json:"restart"`
			SudoPass string             `json:"sudo_pass,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}
		if strings.TrimSpace(req.Domain) == "" {
			writeError(w, http.StatusBadRequest, "Domain wajib diisi")
			return
		}
		if req.SudoPass != "" {
			if !s.verifySudoPasswordWithRateLimit(w, r, req.SudoPass, "Password sudo tidak valid: ") {
				return
			}
		}

		entries, raw, err := mgr.SaveEnv(r.Context(), req.Domain, req.Entries, req.Raw, req.Restart)
		if err != nil {
			if s.writeSudoRequiredIfNeeded(w, err, "Menyimpan file .env terkelola memerlukan hak akses administrator (sudo).") {
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "saved",
			"entries":    entries,
			"raw":        raw,
			"restarted":  req.Restart,
			"app_status": mgr.Status(r.Context(), req.Domain),
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleSiteAppLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Parameter domain wajib diisi")
		return
	}
	lines := 200
	if rawLines := strings.TrimSpace(r.URL.Query().Get("lines")); rawLines != "" {
		if n, err := strconv.Atoi(rawLines); err == nil && n > 0 {
			lines = n
		}
	}
	logs, err := s.ensureSiteAppManager().ReadLogs(r.Context(), domain, lines)
	if err != nil {
		if s.writeSudoRequiredIfNeeded(w, err, "Membaca log journalctl memerlukan hak akses administrator (sudo).") {
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":       domain,
		"service_name": siteapp.ServiceNameForDomain(domain),
		"logs":         logs,
	})
}

func (s *Server) handleSiteAppLogsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Parameter domain wajib diisi")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming log tidak didukung")
		return
	}

	tail := 100
	if rawTail := strings.TrimSpace(r.URL.Query().Get("tail")); rawTail != "" {
		if n, err := strconv.Atoi(rawTail); err == nil && n > 0 {
			tail = n
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeEvent := func(typ, msg string) {
		b, _ := json.Marshal(map[string]string{"type": typ, "message": msg})
		fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}

	unitName := siteapp.UnitFilenameForDomain(domain)
	writeEvent("start", "Terhubung ke live log systemd "+unitName)

	cmd := s.ensureSiteAppManager().NewLogStreamCmd(r.Context(), domain, tail)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		writeEvent("error", "Gagal memulai journalctl stream: "+err.Error())
		writeEvent("finish", "Stream log berakhir")
		return
	}

	lineCh := make(chan string, 128)
	errCh := make(chan error, 1)
	go func() {
		defer close(lineCh)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 32*1024), 512*1024)
		for sc.Scan() {
			select {
			case lineCh <- sc.Text():
			case <-r.Context().Done():
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		errCh <- err
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-lineCh:
			if !ok {
				select {
				case err := <-errCh:
					if err != nil && r.Context().Err() == nil {
						writeEvent("error", err.Error())
					}
				default:
				}
				writeEvent("finish", "Stream log berakhir")
				return
			}
			writeEvent("log", line)
		}
	}
}
