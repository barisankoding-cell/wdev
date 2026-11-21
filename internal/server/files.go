package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"wdev/internal/filemanager"
)

func (s *Server) ensureFileManager() *filemanager.Manager {
	if s.fileMgr == nil {
		s.fileMgr = filemanager.New(s.cfg, s.store)
	}
	return s.fileMgr
}

// FileManager mengembalikan manajer berkas server (berguna untuk konfigurasi root & pengujian).
func (s *Server) FileManager() *filemanager.Manager {
	return s.ensureFileManager()
}

func (s *Server) registerFileManagerRoutes() {
	s.mux.HandleFunc("/api/files/roots", s.handleFilesRoots)
	s.mux.HandleFunc("/api/files/list", s.handleFilesList)
	s.mux.HandleFunc("/api/files/search", s.handleFilesSearch)
	s.mux.HandleFunc("/api/files/read", s.handleFilesRead)
	s.mux.HandleFunc("/api/files/raw", s.handleFilesRaw)
	s.mux.HandleFunc("/api/files/download", s.handleFilesDownload)
	s.mux.HandleFunc("/api/files/write", s.handleFilesWrite)
	s.mux.HandleFunc("/api/files/mkdir", s.handleFilesMkdir)
	s.mux.HandleFunc("/api/files/rename", s.handleFilesRename)
	s.mux.HandleFunc("/api/files/copy", s.handleFilesCopy)
	s.mux.HandleFunc("/api/files/move", s.handleFilesMove)
	s.mux.HandleFunc("/api/files/delete", s.handleFilesDelete)
	s.mux.HandleFunc("/api/files/chmod", s.handleFilesChmod)
	s.mux.HandleFunc("/api/files/upload", s.handleFilesUpload)
	s.mux.HandleFunc("/api/files/fetch-url", s.handleFilesFetchURL)
	s.mux.HandleFunc("/api/files/compress", s.handleFilesCompress)
	s.mux.HandleFunc("/api/files/extract", s.handleFilesExtract)
}

func writeFileMgrError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	msg := err.Error()
	if strings.Contains(msg, "akses ditolak") {
		status = http.StatusForbidden
	} else if strings.Contains(strings.ToLower(msg), "no such file") || strings.Contains(msg, "tidak ditemukan") {
		status = http.StatusNotFound
	}
	writeError(w, status, msg)
}

func (s *Server) handleFilesRoots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	writeJSON(w, http.StatusOK, s.ensureFileManager().AllowedRoots())
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	path := r.URL.Query().Get("path")
	hidden := r.URL.Query().Get("hidden") == "1" || r.URL.Query().Get("hidden") == "true"
	res, err := s.ensureFileManager().List(path, hidden)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleFilesSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	path := r.URL.Query().Get("path")
	q := r.URL.Query().Get("q")
	hidden := r.URL.Query().Get("hidden") == "1" || r.URL.Query().Get("hidden") == "true"
	entries, err := s.ensureFileManager().Search(path, q, hidden, 150)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	res, err := s.ensureFileManager().Read(r.URL.Query().Get("path"))
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleFilesRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if err := s.ensureFileManager().ServeRaw(w, r, r.URL.Query().Get("path")); err != nil {
		writeFileMgrError(w, err)
	}
}

func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	paths := r.URL.Query()["paths"]
	if len(paths) == 0 {
		if single := r.URL.Query().Get("path"); single != "" {
			paths = []string{single}
		}
	}
	if err := s.ensureFileManager().StreamDownload(w, r, paths); err != nil {
		writeFileMgrError(w, err)
	}
}

func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid: "+err.Error())
		return
	}
	if err := s.ensureFileManager().Write(payload.Path, payload.Content); err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "path": payload.Path})
}

func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	if err := s.ensureFileManager().Mkdir(payload.Path); err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "created"})
}

func (s *Server) handleFilesRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Path    string `json:"path"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	newPath, err := s.ensureFileManager().Rename(payload.Path, payload.NewName)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "renamed", "path": newPath})
}

func (s *Server) handleFilesCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Sources []string `json:"sources"`
		DestDir string   `json:"dest_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	count, err := s.ensureFileManager().Copy(payload.Sources, payload.DestDir)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "copied", "count": count})
}

func (s *Server) handleFilesMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Sources []string `json:"sources"`
		DestDir string   `json:"dest_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	count, err := s.ensureFileManager().Move(payload.Sources, payload.DestDir)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "moved", "count": count})
}

func (s *Server) handleFilesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	count, err := s.ensureFileManager().Delete(payload.Paths)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": count})
}

func (s *Server) handleFilesChmod(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Paths     []string `json:"paths"`
		Mode      string   `json:"mode"`
		Recursive bool     `json:"recursive"`
		WebOwner  bool     `json:"web_owner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	if err := s.ensureFileManager().Chmod(payload.Paths, payload.Mode, payload.Recursive, payload.WebOwner); err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "Total ukuran upload melebihi batas 256 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "Gagal memproses upload multipart: "+err.Error())
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	destDir := r.FormValue("dir")
	overwrite := r.FormValue("overwrite") != "false"
	relPaths := r.MultipartForm.Value["rel_paths"]
	var files = r.MultipartForm.File["files"]
	if len(files) == 0 {
		files = r.MultipartForm.File["file"]
	}
	entries, skipped, err := s.ensureFileManager().SaveUploadedDetailed(destDir, files, filemanager.UploadOptions{
		RelPaths:  relPaths,
		Overwrite: overwrite,
	})
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "uploaded",
		"count":         len(entries),
		"skipped_count": len(skipped),
		"skipped":       skipped,
		"entries":       entries,
	})
}

func (s *Server) handleFilesFetchURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		URL       string `json:"url"`
		Dir       string `json:"dir"`
		Filename  string `json:"filename"`
		Overwrite *bool  `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	overwrite := true
	if payload.Overwrite != nil {
		overwrite = *payload.Overwrite
	}
	entry, err := s.ensureFileManager().FetchFromURL(r.Context(), payload.URL, payload.Dir, payload.Filename, overwrite)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "downloaded",
		"entry":  entry,
	})
}

func (s *Server) handleFilesCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Paths      []string `json:"paths"`
		OutputPath string   `json:"output_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	entry, err := s.ensureFileManager().Compress(payload.Paths, payload.OutputPath)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "compressed", "entry": entry})
}

func (s *Server) handleFilesExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Path    string `json:"path"`
		DestDir string `json:"dest_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	count, err := s.ensureFileManager().Extract(payload.Path, payload.DestDir)
	if err != nil {
		writeFileMgrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "extracted", "count": count})
}
