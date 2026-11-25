package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"wdev/internal/compose"
	"wdev/internal/site"
)

func (s *Server) ensureComposeManager() *compose.Manager {
	if s.composeMgr == nil {
		var sudoFn func() string
		if s.installer != nil {
			sudoFn = s.installer.GetSudoPassword
		}
		s.composeMgr = compose.New(s.cfg, s.store, sudoFn)
	}
	return s.composeMgr
}

func (s *Server) registerComposeRoutes() {
	s.mux.HandleFunc("/api/compose/status", s.handleComposeStatus)
	s.mux.HandleFunc("/api/compose/projects", s.handleComposeProjects)
	s.mux.HandleFunc("/api/compose/projects/", s.handleComposeProjectSub)
	s.mux.HandleFunc("/api/compose/system/df", s.handleComposeSystemDF)
	s.mux.HandleFunc("/api/compose/system/prune", s.handleComposeSystemPrune)
}

func (s *Server) verifyComposeSudoIfNeeded(w http.ResponseWriter, r *http.Request, sudoPass string, eng compose.EngineInfo) bool {
	if sudoPass != "" && s.installer != nil {
		if !s.verifySudoPasswordWithRateLimit(w, r, sudoPass, "Password sudo tidak valid: ") {
			return false
		}
	}
	if os.Geteuid() != 0 && eng.NeedsSudo && s.installer != nil {
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": "Operasi Docker Compose memerlukan hak akses administrator (sudo) atau user di grup docker.",
			})
			return false
		}
	}
	return true
}

func (s *Server) handleComposeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	mgr := s.ensureComposeManager()
	eng := mgr.GetEngineInfo(r.Context())
	projects, err := mgr.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if projects == nil {
		projects = []compose.Project{}
	}

	var running, partial, stopped, warnings int
	for _, p := range projects {
		switch p.Status {
		case "running":
			running++
		case "partial":
			partial++
		default:
			stopped++
		}
		warnings += len(p.SecurityWarnings)
	}

	writeJSON(w, http.StatusOK, compose.StatusReport{
		Engine:    eng,
		Projects:  projects,
		Templates: compose.StarterTemplates(),
		Total:     len(projects),
		Running:   running,
		Partial:   partial,
		Stopped:   stopped,
		Warnings:  warnings,
	})
}

func (s *Server) handleComposeProjects(w http.ResponseWriter, r *http.Request) {
	mgr := s.ensureComposeManager()
	switch r.Method {
	case http.MethodGet:
		projects, err := mgr.ListProjects(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if projects == nil {
			projects = []compose.Project{}
		}
		writeJSON(w, http.StatusOK, projects)

	case http.MethodPost:
		var req compose.CreateProjectOptions
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}
		proj, err := mgr.CreateOrRegisterProject(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp := map[string]any{
			"status":  "created",
			"project": proj,
		}
		if req.StartAfter && s.runner != nil {
			taskName := fmt.Sprintf("Compose Up: %s", proj.Name)
			taskID, terr := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
				return mgr.RunActionTask(ctx, proj.Name, compose.ActionOptions{
					Action:   "up",
					Profiles: req.Profiles,
				}, cb)
			})
			if terr == nil {
				resp["task_id"] = taskID
			}
		}
		writeJSON(w, http.StatusOK, resp)

	default:
		writeMethodNotAllowed(w, r)
	}
}

func (s *Server) handleComposeProjectSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/compose/projects/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		writeError(w, http.StatusBadRequest, "Nama proyek Compose wajib diisi")
		return
	}

	parts := strings.Split(rest, "/")
	projName := parts[0]
	if _, err := compose.ValidateProjectName(projName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	mgr := s.ensureComposeManager()

	// /api/compose/projects/{name}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			proj, err := mgr.GetProject(r.Context(), projName)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, proj)
		case http.MethodDelete:
			runDown := r.URL.Query().Get("down") == "1" || r.URL.Query().Get("down") == "true"
			if runDown {
				eng := mgr.GetEngineInfo(r.Context())
				if !s.verifyComposeSudoIfNeeded(w, r, "", eng) {
					return
				}
			}
			if err := mgr.DeleteProject(r.Context(), projName, runDown); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{
				"status":  "deleted",
				"message": "Proyek Compose " + projName + " dihapus dari daftar kelola wdev",
			})
		default:
			writeMethodNotAllowed(w, r)
		}
		return
	}

	sub := parts[1]
	switch sub {
	case "action":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}
		var payload struct {
			compose.ActionOptions
			SudoPass string `json:"sudo_pass,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload aksi Compose tidak valid")
			return
		}
		proj, err := mgr.GetProject(r.Context(), projName)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if _, err := mgr.BuildComposeArgs(proj, payload.ActionOptions); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		eng := mgr.GetEngineInfo(r.Context())
		if !s.verifyComposeSudoIfNeeded(w, r, payload.SudoPass, eng) {
			return
		}
		if s.runner == nil {
			writeError(w, http.StatusServiceUnavailable, "Task runner tidak tersedia")
			return
		}
		label := fmt.Sprintf("Compose %s: %s", strings.ToUpper(payload.Action), proj.Name)
		if payload.Service != "" {
			label = fmt.Sprintf("Compose %s: %s/%s", strings.ToUpper(payload.Action), proj.Name, payload.Service)
		}
		taskID, err := s.runner.StartNativeTask(label, func(ctx context.Context, cb func(string)) error {
			return mgr.RunActionTask(ctx, proj.Name, payload.ActionOptions, cb)
		})
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "started",
			"task_id": taskID,
		})

	case "files":
		switch r.Method {
		case http.MethodGet:
			files, err := mgr.GetProjectFiles(r.Context(), projName)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, files)
		case http.MethodPut, http.MethodPost:
			var payload struct {
				ComposeContent string `json:"compose_content"`
				EnvContent     string `json:"env_content"`
				UpdateEnv      bool   `json:"update_env"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, "Payload berkas tidak valid")
				return
			}
			files, err := mgr.SaveProjectFiles(r.Context(), projName, payload.ComposeContent, payload.EnvContent, payload.UpdateEnv)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "saved",
				"files":  files,
			})
		default:
			writeMethodNotAllowed(w, r)
		}

	case "config":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}
		raw, err := mgr.RenderResolvedConfig(r.Context(), projName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"project": projName,
			"config":  raw,
		})

	case "logs":
		if len(parts) == 3 && parts[2] == "stream" {
			s.handleComposeLogsStream(w, r, projName)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, r)
			return
		}
		svc := strings.TrimSpace(r.URL.Query().Get("service"))
		tail := 200
		if rawTail := strings.TrimSpace(r.URL.Query().Get("tail")); rawTail != "" {
			if n, err := strconv.Atoi(rawTail); err == nil && n > 0 {
				tail = n
			}
		}
		out, err := mgr.GetLogs(r.Context(), projName, svc, tail)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"project": projName,
			"service": svc,
			"logs":    out,
		})

	case "expose":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}
		if s.siteMgr == nil {
			writeError(w, http.StatusServiceUnavailable, "Site manager tidak tersedia")
			return
		}
		var req struct {
			Domain     string `json:"domain"`
			ProxyPort  int    `json:"proxy_port"`
			SSLEnabled *bool  `json:"ssl_enabled,omitempty"`
			ExposeEdge *bool  `json:"expose_edge,omitempty"`
			EdgeDomain string `json:"edge_domain,omitempty"`
			EdgeSSL    *bool  `json:"edge_ssl,omitempty"`
			AddHosts   *bool  `json:"add_hosts,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}
		proj, err := mgr.GetProject(r.Context(), projName)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		sslEnabled := true
		if req.SSLEnabled != nil {
			sslEnabled = *req.SSLEnabled
		}
		resp, statusCode, err := s.createSiteWithModeSync(siteCreatePayload{
			CreateOptions: site.CreateOptions{
				Domain:     req.Domain,
				RootPath:   proj.DirPath,
				Framework:  "proxy",
				ProxyPort:  req.ProxyPort,
				SSLEnabled: sslEnabled,
			},
			ExposeEdge: req.ExposeEdge,
			EdgeDomain: req.EdgeDomain,
			EdgeSSL:    req.EdgeSSL,
			AddHosts:   req.AddHosts,
		})
		if err != nil {
			writeError(w, statusCode, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)

	default:
		writeError(w, http.StatusNotFound, "Endpoint Compose tidak ditemukan")
	}
}

func (s *Server) handleComposeLogsStream(w http.ResponseWriter, r *http.Request, projName string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming log tidak didukung")
		return
	}
	svc := strings.TrimSpace(r.URL.Query().Get("service"))
	tail := 120
	if rawTail := strings.TrimSpace(r.URL.Query().Get("tail")); rawTail != "" {
		if n, err := strconv.Atoi(rawTail); err == nil && n > 0 {
			tail = n
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lineCh := make(chan string, 128)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.ensureComposeManager().StreamLogs(r.Context(), projName, svc, tail, func(line string) {
			select {
			case lineCh <- line:
			case <-r.Context().Done():
			}
		})
		close(lineCh)
	}()

	writeEvent := func(typ, msg string) {
		b, _ := json.Marshal(map[string]string{"type": typ, "message": msg})
		fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}

	writeEvent("start", "Terhubung ke live log Compose "+projName)

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

func (s *Server) handleComposeSystemDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	df, err := s.ensureComposeManager().GetDiskUsage(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, df)
}

func (s *Server) handleComposeSystemPrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	var payload struct {
		Target   string `json:"target"`
		SudoPass string `json:"sudo_pass,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	mgr := s.ensureComposeManager()
	eng := mgr.GetEngineInfo(r.Context())
	if !s.verifyComposeSudoIfNeeded(w, r, payload.SudoPass, eng) {
		return
	}
	if s.runner == nil {
		writeError(w, http.StatusServiceUnavailable, "Task runner tidak tersedia")
		return
	}
	target := strings.TrimSpace(payload.Target)
	if target == "" {
		target = "images"
	}
	taskID, err := s.runner.StartNativeTask("Docker Prune: "+target, func(ctx context.Context, cb func(string)) error {
		return mgr.Prune(ctx, target, cb)
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
