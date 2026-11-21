package filemanager

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"wdev/internal/config"
	"wdev/internal/store"
)

const (
	MaxReadBytes   int64 = 4 << 20   // 4 MiB untuk editor teks
	MaxUploadBytes int64 = 256 << 20 // 256 MiB total body upload
)

var (
	textExtensions = map[string]bool{
		".txt": true, ".md": true, ".markdown": true, ".html": true, ".htm": true,
		".css": true, ".scss": true, ".sass": true, ".less": true,
		".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".mts": true, ".jsx": true, ".tsx": true, ".svelte": true, ".vue": true,
		".json": true, ".jsonc": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true, ".svg": true,
		".php": true, ".phtml": true, ".py": true, ".go": true, ".rs": true, ".rb": true, ".java": true, ".kt": true, ".c": true, ".h": true, ".cpp": true,
		".sh": true, ".bash": true, ".zsh": true, ".env": true, ".ini": true, ".conf": true, ".cfg": true, ".cnf": true,
		".sql": true, ".htaccess": true, ".gitignore": true, ".gitattributes": true, ".editorconfig": true, ".lock": true, ".log": true,
		".csv": true, ".tsv": true, ".service": true, ".example": true,
	}
	imageExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true, ".ico": true, ".bmp": true,
	}
	archiveExtensions = map[string]bool{
		".zip": true, ".tar": true, ".gz": true, ".tgz": true,
	}
)

// RootInfo mendeskripsikan satu direktori akar yang dapat dijelajahi File Manager.
type RootInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Kind   string `json:"kind"` // "webroot", "vhost", "home", "custom"
	Domain string `json:"domain,omitempty"`
}

// FileEntry menyimpan metadata satu berkas atau direktori.
type FileEntry struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	IsDir         bool      `json:"is_dir"`
	IsSymlink     bool      `json:"is_symlink,omitempty"`
	SymlinkTarget string    `json:"symlink_target,omitempty"`
	Size          int64     `json:"size"`
	Mode          string    `json:"mode"`
	PermText      string    `json:"perm_text"`
	Owner         string    `json:"owner"`
	Group         string    `json:"group"`
	ModTime       time.Time `json:"mod_time"`
	Ext           string    `json:"ext,omitempty"`
	Editable      bool      `json:"editable"`
	IsImage       bool      `json:"is_image"`
	IsArchive     bool      `json:"is_archive"`
}

// ListResult adalah respons daftar isi direktori beserta akar yang tersedia.
type ListResult struct {
	Path       string      `json:"path"`
	Parent     string      `json:"parent,omitempty"`
	CanGoUp    bool        `json:"can_go_up"`
	Roots      []RootInfo  `json:"roots"`
	Entries    []FileEntry `json:"entries"`
	TotalDirs  int         `json:"total_dirs"`
	TotalFiles int         `json:"total_files"`
	TotalSize  int64       `json:"total_size"`
}

// FileContent adalah isi berkas teks untuk editor bawaan.
type FileContent struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Content string    `json:"content"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
}

// Manager mengelola operasi File Manager dengan batasan direktori yang aman.
type Manager struct {
	cfg           *config.Config
	store         *store.Store
	overrideRoots []string
	remoteClient  *http.Client
}

// New membuat instance File Manager yang terikat ke root web & vhost wdev.
func New(cfg *config.Config, s *store.Store) *Manager {
	return &Manager{cfg: cfg, store: s}
}

// SetRoots membatasi direktori akar ke daftar tertentu (terutama untuk pengujian unit).
func (m *Manager) SetRoots(roots ...string) {
	m.overrideRoots = append([]string(nil), roots...)
}

func (m *Manager) webRootBase() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.WebRootBase) != "" {
		return filepath.Clean(m.cfg.WebRootBase)
	}
	return "/var/www"
}

// AllowedRoots mengembalikan seluruh root direktori yang diizinkan beserta label pintasan VHost.
func (m *Manager) AllowedRoots() []RootInfo {
	var roots []RootInfo
	seen := make(map[string]bool)
	addRoot := func(raw, name, kind, domain string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		clean := filepath.Clean(raw)
		if !filepath.IsAbs(clean) || clean == "/" {
			return
		}
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			clean = resolved
		}
		if seen[clean] {
			return
		}
		seen[clean] = true
		if name == "" {
			name = filepath.Base(clean)
		}
		roots = append(roots, RootInfo{
			Name:   name,
			Path:   clean,
			Kind:   kind,
			Domain: domain,
		})
	}

	if len(m.overrideRoots) > 0 {
		for _, r := range m.overrideRoots {
			addRoot(r, "", "custom", "")
		}
		return roots
	}

	webBase := m.webRootBase()
	if _, err := os.Stat(webBase); os.IsNotExist(err) {
		_ = os.MkdirAll(webBase, 0755)
	}
	addRoot(webBase, "Web Root ("+webBase+")", "webroot", "")

	if m.store != nil {
		if sites, err := m.store.ListSites(); err == nil {
			for _, s := range sites {
				if strings.EqualFold(s.Framework, "proxy") || strings.TrimSpace(s.RootPath) == "" {
					continue
				}
				addRoot(s.RootPath, s.Domain, "vhost", s.Domain)
			}
		}
	}

	if info, err := os.Stat("/home"); err == nil && info.IsDir() {
		addRoot("/home", "Home (/home)", "home", "")
	}
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" && homeDir != "/" {
		if info, err := os.Stat(homeDir); err == nil && info.IsDir() {
			addRoot(homeDir, "Home ("+filepath.Base(homeDir)+")", "home", "")
		}
	}
	if extra := strings.TrimSpace(os.Getenv("WDEV_FM_ROOTS")); extra != "" {
		for _, item := range strings.Split(extra, ":") {
			addRoot(item, "", "custom", "")
		}
	}

	return roots
}

func isWithinRoot(target, root string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolveThroughExistingAncestor menyelesaikan symlink dari ancestor terdalam yang ada,
// sehingga symlink keluar root terdeteksi bahkan saat target belum dibuat.
func resolveThroughExistingAncestor(cleanPath string) (string, error) {
	curr := cleanPath
	var tail []string
	for {
		if _, err := os.Lstat(curr); err == nil {
			resolved, evalErr := filepath.EvalSymlinks(curr)
			if evalErr != nil {
				// Bila leaf-nya broken symlink, evaluasi parent-nya agar target symlink tetap terperiksa.
				if len(tail) == 0 && curr != "/" {
					parentResolved, pErr := resolveThroughExistingAncestor(filepath.Dir(curr))
					if pErr != nil {
						return "", pErr
					}
					return filepath.Join(parentResolved, filepath.Base(curr)), nil
				}
				return "", evalErr
			}
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return "", fmt.Errorf("direktori induk tidak ditemukan")
		}
		tail = append(tail, filepath.Base(curr))
		curr = parent
	}
}

// ResolvePath memvalidasi dan menormalkan path agar berada di dalam direktori akar yang diizinkan.
func (m *Manager) ResolvePath(rawPath string, mustExist bool) (string, error) {
	roots := m.AllowedRoots()
	if len(roots) == 0 {
		return "", fmt.Errorf("tidak ada direktori akar File Manager yang tersedia")
	}

	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		for _, r := range roots {
			if st, err := os.Stat(r.Path); err == nil && st.IsDir() {
				return r.Path, nil
			}
		}
		return roots[0].Path, nil
	}

	if strings.ContainsRune(trimmed, '\x00') {
		return "", fmt.Errorf("path mengandung karakter tidak valid")
	}

	clean := filepath.Clean(trimmed)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("path harus berupa path absolut")
	}

	var resolved string
	if mustExist {
		if _, err := os.Lstat(clean); err != nil {
			return "", err
		}
		r, err := resolveThroughExistingAncestor(clean)
		if err != nil {
			return "", err
		}
		resolved = r
	} else {
		r, err := resolveThroughExistingAncestor(clean)
		if err != nil {
			return "", err
		}
		resolved = r
	}

	for _, root := range roots {
		if isWithinRoot(resolved, root.Path) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("akses ditolak: path '%s' berada di luar direktori yang diizinkan", clean)
}

func (m *Manager) isProtectedBaseRoot(resolved string) bool {
	for _, r := range m.AllowedRoots() {
		if r.Path == resolved {
			return true
		}
	}
	return resolved == "/"
}

type uidGidResolver struct {
	users  map[uint32]string
	groups map[uint32]string
}

func newUIDGidResolver() *uidGidResolver {
	return &uidGidResolver{
		users:  make(map[uint32]string),
		groups: make(map[uint32]string),
	}
}

func (r *uidGidResolver) ownerName(uid uint32) string {
	if v, ok := r.users[uid]; ok {
		return v
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil && u.Username != "" {
		name = u.Username
	}
	r.users[uid] = name
	return name
}

func (r *uidGidResolver) groupName(gid uint32) string {
	if v, ok := r.groups[gid]; ok {
		return v
	}
	name := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(name); err == nil && g.Name != "" {
		name = g.Name
	}
	r.groups[gid] = name
	return name
}

func isEditableName(name string, size int64) bool {
	if size > MaxReadBytes {
		return false
	}
	lower := strings.ToLower(name)
	if ext := strings.ToLower(filepath.Ext(lower)); ext != "" {
		if textExtensions[ext] {
			return true
		}
		if imageExtensions[ext] || archiveExtensions[ext] {
			return false
		}
	}
	base := filepath.Base(lower)
	switch base {
	case ".env", ".htaccess", "dockerfile", "makefile", "caddyfile", "procfile", "readme", "license":
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	return size <= 512*1024 && filepath.Ext(base) == ""
}

func (m *Manager) buildEntry(fullPath string, info os.FileInfo, res *uidGidResolver) FileEntry {
	name := filepath.Base(fullPath)
	isSymlink := info.Mode()&os.ModeSymlink != 0
	isDir := info.IsDir()
	symlinkTarget := ""
	size := info.Size()
	mode := info.Mode()

	if isSymlink {
		if t, err := os.Readlink(fullPath); err == nil {
			symlinkTarget = t
		}
		if targetInfo, err := os.Stat(fullPath); err == nil {
			isDir = targetInfo.IsDir()
			size = targetInfo.Size()
			mode = targetInfo.Mode()
		}
	}

	ext := ""
	if !isDir {
		ext = strings.ToLower(filepath.Ext(name))
	}

	owner, group := "-", "-"
	if st, ok := info.Sys().(*syscall.Stat_t); ok && res != nil {
		owner = res.ownerName(st.Uid)
		group = res.groupName(st.Gid)
	}

	lowerName := strings.ToLower(name)
	isArch := !isDir && (archiveExtensions[ext] || strings.HasSuffix(lowerName, ".tar.gz") || strings.HasSuffix(lowerName, ".tgz"))

	return FileEntry{
		Name:          name,
		Path:          fullPath,
		IsDir:         isDir,
		IsSymlink:     isSymlink,
		SymlinkTarget: symlinkTarget,
		Size:          size,
		Mode:          fmt.Sprintf("%04o", mode.Perm()),
		PermText:      info.Mode().String(),
		Owner:         owner,
		Group:         group,
		ModTime:       info.ModTime().UTC(),
		Ext:           ext,
		Editable:      !isDir && isEditableName(name, size),
		IsImage:       !isDir && imageExtensions[ext],
		IsArchive:     isArch,
	}
}

// List membaca isi direktori yang diizinkan dan menyusun daftar berkas serta pintasan akar.
func (m *Manager) List(dirPath string, showHidden bool) (ListResult, error) {
	resolved, err := m.ResolvePath(dirPath, true)
	if err != nil {
		return ListResult{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return ListResult{}, err
	}
	if !info.IsDir() {
		return ListResult{}, fmt.Errorf("'%s' bukan direktori", resolved)
	}

	dirEntries, err := os.ReadDir(resolved)
	if err != nil {
		return ListResult{}, fmt.Errorf("gagal membaca direktori: %w", err)
	}

	uidResolver := newUIDGidResolver()
	entries := make([]FileEntry, 0, len(dirEntries))
	var totalDirs, totalFiles int
	var totalSize int64

	for _, de := range dirEntries {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(resolved, name)
		lstatInfo, lErr := os.Lstat(fullPath)
		if lErr != nil {
			continue
		}
		entry := m.buildEntry(fullPath, lstatInfo, uidResolver)
		if entry.IsDir {
			totalDirs++
		} else {
			totalFiles++
			totalSize += entry.Size
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := filepath.Dir(resolved)
	canGoUp := false
	if parent != resolved {
		if _, pErr := m.ResolvePath(parent, true); pErr == nil {
			canGoUp = true
		} else {
			parent = ""
		}
	} else {
		parent = ""
	}

	return ListResult{
		Path:       resolved,
		Parent:     parent,
		CanGoUp:    canGoUp,
		Roots:      m.AllowedRoots(),
		Entries:    entries,
		TotalDirs:  totalDirs,
		TotalFiles: totalFiles,
		TotalSize:  totalSize,
	}, nil
}

// Search mencari berkas/direktori secara rekursif di dalam direktori yang diizinkan.
func (m *Manager) Search(dirPath, query string, showHidden bool, maxResults int) ([]FileEntry, error) {
	resolved, err := m.ResolvePath(dirPath, true)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return []FileEntry{}, nil
	}
	if maxResults <= 0 || maxResults > 200 {
		maxResults = 100
	}

	uidResolver := newUIDGidResolver()
	results := make([]FileEntry, 0, 16)
	visited := 0
	const maxVisited = 12000

	var errLimit = errors.New("limit reached")
	walkErr := filepath.WalkDir(resolved, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == resolved {
			return nil
		}
		visited++
		if visited > maxVisited {
			return errLimit
		}
		name := d.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Hindari traversal ke symlink luar root & direktori dependency besar saat pencarian cepat.
		if d.Type()&os.ModeSymlink != 0 {
			if _, resErr := m.ResolvePath(path, true); resErr != nil {
				return nil
			}
		}
		if strings.Contains(strings.ToLower(name), q) {
			if lstatInfo, err := os.Lstat(path); err == nil {
				results = append(results, m.buildEntry(path, lstatInfo, uidResolver))
				if len(results) >= maxResults {
					return errLimit
				}
			}
		}
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errLimit) {
		return nil, walkErr
	}
	return results, nil
}

// Read membaca isi berkas teks UTF-8 untuk editor panel.
func (m *Manager) Read(filePath string) (FileContent, error) {
	resolved, err := m.ResolvePath(filePath, true)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, fmt.Errorf("direktori tidak dapat dibuka sebagai teks")
	}
	if info.Size() > MaxReadBytes {
		return FileContent{}, fmt.Errorf("ukuran berkas (%d byte) melebihi batas editor (4 MiB); silakan unduh berkas", info.Size())
	}

	f, err := os.Open(resolved)
	if err != nil {
		return FileContent{}, fmt.Errorf("gagal membuka berkas: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxReadBytes+1))
	if err != nil {
		return FileContent{}, fmt.Errorf("gagal membaca berkas: %w", err)
	}
	if int64(len(data)) > MaxReadBytes {
		return FileContent{}, fmt.Errorf("ukuran berkas melebihi batas editor (4 MiB)")
	}
	if len(data) > 0 {
		if bytes.IndexByte(data, 0) != -1 || !utf8.Valid(data) {
			return FileContent{}, fmt.Errorf("berkas biner tidak dapat diedit di editor teks (gunakan Unduh atau Pratinjau Gambar)")
		}
	}

	return FileContent{
		Path:    resolved,
		Name:    filepath.Base(resolved),
		Content: string(data),
		Size:    info.Size(),
		Mode:    fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime: info.ModTime().UTC(),
	}, nil
}

func inheritParentNonRootOwnership(targetPath string) {
	if os.Geteuid() != 0 {
		return
	}
	parentInfo, err := os.Stat(filepath.Dir(targetPath))
	if err != nil {
		return
	}
	if st, ok := parentInfo.Sys().(*syscall.Stat_t); ok && st.Uid != 0 {
		_ = os.Lchown(targetPath, int(st.Uid), int(st.Gid))
	}
}

// Write menulis isi teks ke berkas yang sudah ada atau baru di dalam root yang diizinkan.
func (m *Manager) Write(filePath, content string) error {
	resolved, err := m.ResolvePath(filePath, false)
	if err != nil {
		return err
	}
	if m.isProtectedBaseRoot(resolved) {
		return fmt.Errorf("direktori akar tidak dapat ditimpa sebagai berkas")
	}
	if int64(len(content)) > MaxReadBytes {
		return fmt.Errorf("ukuran konten melebihi batas editor (4 MiB)")
	}

	parentDir := filepath.Dir(resolved)
	pInfo, err := os.Stat(parentDir)
	if err != nil || !pInfo.IsDir() {
		return fmt.Errorf("direktori induk '%s' tidak ditemukan", parentDir)
	}

	perm := os.FileMode(0644)
	isNew := true
	if st, err := os.Lstat(resolved); err == nil {
		if st.IsDir() {
			return fmt.Errorf("'%s' adalah direktori", resolved)
		}
		perm = st.Mode().Perm()
		isNew = false
	}

	// Tulis ke file sementara di direktori yang sama lalu rename secara atomik.
	tmp, err := os.CreateTemp(parentDir, ".wdev-edit-*")
	if err != nil {
		return fmt.Errorf("gagal menulis berkas: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if isNew {
		inheritParentNonRootOwnership(tmpName)
	} else if existingInfo, statErr := os.Lstat(resolved); statErr == nil {
		if st, ok := existingInfo.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
			_ = os.Lchown(tmpName, int(st.Uid), int(st.Gid))
		}
	}
	if err := os.Rename(tmpName, resolved); err != nil {
		return fmt.Errorf("gagal menyimpan berkas: %w", err)
	}
	cleanup = false
	return nil
}

// Mkdir membuat direktori baru di dalam path yang diizinkan.
func (m *Manager) Mkdir(dirPath string) error {
	resolved, err := m.ResolvePath(dirPath, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(resolved); err == nil {
		return fmt.Errorf("'%s' sudah ada", filepath.Base(resolved))
	}
	parentInfo, err := os.Stat(filepath.Dir(resolved))
	if err != nil || !parentInfo.IsDir() {
		return fmt.Errorf("direktori induk tidak ditemukan")
	}
	if err := os.Mkdir(resolved, 0755); err != nil {
		return fmt.Errorf("gagal membuat folder: %w", err)
	}
	inheritParentNonRootOwnership(resolved)
	return nil
}

func validateBaseName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "." || trimmed == ".." || strings.ContainsAny(trimmed, "/\\\x00") {
		return "", fmt.Errorf("nama berkas/folder tidak valid")
	}
	return trimmed, nil
}

// Rename mengganti nama berkas atau folder di direktori yang sama.
func (m *Manager) Rename(oldPath, newName string) (string, error) {
	resolvedOld, err := m.ResolvePath(oldPath, true)
	if err != nil {
		return "", err
	}
	if m.isProtectedBaseRoot(resolvedOld) {
		return "", fmt.Errorf("direktori akar tidak boleh diubah namanya")
	}
	cleanName, err := validateBaseName(newName)
	if err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(resolvedOld), cleanName)
	resolvedNew, err := m.ResolvePath(target, false)
	if err != nil {
		return "", err
	}
	if resolvedNew == resolvedOld {
		return resolvedNew, nil
	}
	if _, err := os.Lstat(resolvedNew); err == nil {
		return "", fmt.Errorf("tujuan '%s' sudah ada", cleanName)
	}
	if err := os.Rename(resolvedOld, resolvedNew); err != nil {
		return "", fmt.Errorf("gagal mengganti nama: %w", err)
	}
	return resolvedNew, nil
}

// Delete menghapus berkas atau folder secara aman (menolak root utama).
func (m *Manager) Delete(paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, fmt.Errorf("tidak ada berkas/folder yang dipilih")
	}
	var resolvedTargets []string
	for _, p := range paths {
		resolved, err := m.ResolvePath(p, true)
		if err != nil {
			return 0, err
		}
		if m.isProtectedBaseRoot(resolved) {
			return 0, fmt.Errorf("direktori akar sistem '%s' tidak boleh dihapus", resolved)
		}
		resolvedTargets = append(resolvedTargets, resolved)
	}

	deleted := 0
	for _, target := range resolvedTargets {
		if err := os.RemoveAll(target); err != nil {
			return deleted, fmt.Errorf("gagal menghapus '%s': %w", filepath.Base(target), err)
		}
		deleted++
	}
	return deleted, nil
}

func copyFileContent(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	inheritParentNonRootOwnership(dst)
	return nil
}

func copyRecursive(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.IsDir() {
		return copyFileContent(src, dst, info.Mode())
	}

	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	inheritParentNonRootOwnership(dst)
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyRecursive(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) validateCopyMoveBatch(sources []string, destDir string) ([]string, string, error) {
	if len(sources) == 0 {
		return nil, "", fmt.Errorf("pilih minimal satu berkas atau folder")
	}
	resolvedDest, err := m.ResolvePath(destDir, true)
	if err != nil {
		return nil, "", err
	}
	destInfo, err := os.Stat(resolvedDest)
	if err != nil || !destInfo.IsDir() {
		return nil, "", fmt.Errorf("direktori tujuan tidak valid")
	}

	resolvedSources := make([]string, 0, len(sources))
	for _, rawSrc := range sources {
		src, err := m.ResolvePath(rawSrc, true)
		if err != nil {
			return nil, "", err
		}
		if m.isProtectedBaseRoot(src) {
			return nil, "", fmt.Errorf("direktori akar '%s' tidak dapat disalin atau dipindahkan", src)
		}
		if isWithinRoot(resolvedDest, src) {
			return nil, "", fmt.Errorf("tujuan tidak boleh berada di dalam folder sumber '%s'", filepath.Base(src))
		}
		dstItem := filepath.Join(resolvedDest, filepath.Base(src))
		if dstItem == src {
			return nil, "", fmt.Errorf("sumber dan tujuan sama untuk '%s'", filepath.Base(src))
		}
		if _, err := os.Lstat(dstItem); err == nil {
			return nil, "", fmt.Errorf("'%s' sudah ada di folder tujuan", filepath.Base(src))
		}
		resolvedSources = append(resolvedSources, src)
	}
	return resolvedSources, resolvedDest, nil
}

// Copy menyalin satu atau lebih berkas/folder ke direktori tujuan.
func (m *Manager) Copy(sources []string, destDir string) (int, error) {
	srcs, dst, err := m.validateCopyMoveBatch(sources, destDir)
	if err != nil {
		return 0, err
	}
	copied := 0
	for _, s := range srcs {
		if err := copyRecursive(s, filepath.Join(dst, filepath.Base(s))); err != nil {
			return copied, fmt.Errorf("gagal menyalin '%s': %w", filepath.Base(s), err)
		}
		copied++
	}
	return copied, nil
}

// Move memindahkan satu atau lebih berkas/folder ke direktori tujuan.
func (m *Manager) Move(sources []string, destDir string) (int, error) {
	srcs, dst, err := m.validateCopyMoveBatch(sources, destDir)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, s := range srcs {
		target := filepath.Join(dst, filepath.Base(s))
		if err := os.Rename(s, target); err != nil {
			// Fallback antar filesystem/mountpoint: copy lalu hapus sumber.
			if copyErr := copyRecursive(s, target); copyErr != nil {
				return moved, fmt.Errorf("gagal memindahkan '%s': %w", filepath.Base(s), err)
			}
			_ = os.RemoveAll(s)
		}
		moved++
	}
	return moved, nil
}

func detectWebUIDGID() (int, int, bool) {
	for _, name := range []string{"www-data", "nginx"} {
		if u, err := user.Lookup(name); err == nil {
			uid, errU := strconv.Atoi(u.Uid)
			gid, errG := strconv.Atoi(u.Gid)
			if errU == nil && errG == nil {
				return uid, gid, true
			}
		}
	}
	return 0, 0, false
}

// Chmod mengubah izin (permission oktal) dan opsional pemilik web pada satu atau lebih path.
func (m *Manager) Chmod(paths []string, modeStr string, recursive bool, webOwner bool) error {
	if len(paths) == 0 {
		return fmt.Errorf("tidak ada berkas/folder yang dipilih")
	}
	cleanMode := strings.TrimSpace(modeStr)
	if len(cleanMode) < 3 || len(cleanMode) > 4 {
		return fmt.Errorf("mode permission harus berupa 3 atau 4 digit oktal (mis. 755 atau 644)")
	}
	parsed, err := strconv.ParseUint(cleanMode, 8, 32)
	if err != nil || parsed > 07777 {
		return fmt.Errorf("mode permission oktal tidak valid: %q", modeStr)
	}
	targetMode := os.FileMode(parsed)

	var webUID, webGID int
	hasWebOwner := false
	if webOwner && os.Geteuid() == 0 {
		webUID, webGID, hasWebOwner = detectWebUIDGID()
	}

	for _, p := range paths {
		resolved, err := m.ResolvePath(p, true)
		if err != nil {
			return err
		}
		if recursive {
			if err := filepath.WalkDir(resolved, func(sub string, d os.DirEntry, wErr error) error {
				if wErr != nil {
					return wErr
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if chErr := os.Chmod(sub, targetMode); chErr != nil {
					return chErr
				}
				if hasWebOwner {
					_ = os.Lchown(sub, webUID, webGID)
				}
				return nil
			}); err != nil {
				return fmt.Errorf("gagal mengatur permission '%s': %w", filepath.Base(resolved), err)
			}
		} else {
			if err := os.Chmod(resolved, targetMode); err != nil {
				return fmt.Errorf("gagal mengatur permission '%s': %w", filepath.Base(resolved), err)
			}
			if hasWebOwner {
				_ = os.Lchown(resolved, webUID, webGID)
			}
		}
	}
	return nil
}

// UploadOptions mengatur perilaku penyimpanan upload berkas maupun folder bertingkat.
type UploadOptions struct {
	RelPaths  []string
	Overwrite bool
}

func sanitizeUploadRelPath(rawRel string) (string, error) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(rawRel, "\\", "/"))
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return "", fmt.Errorf("path berkas upload kosong")
	}
	parts := strings.Split(cleaned, "/")
	validParts := make([]string, 0, len(parts))
	for _, seg := range parts {
		cleanSeg, err := validateBaseName(seg)
		if err != nil {
			return "", err
		}
		validParts = append(validParts, cleanSeg)
	}
	return filepath.Join(validParts...), nil
}

// SaveUploaded menyimpan daftar berkas multipart upload ke direktori tujuan secara aman.
func (m *Manager) SaveUploaded(destDir string, files []*multipart.FileHeader) ([]FileEntry, error) {
	entries, _, err := m.SaveUploadedDetailed(destDir, files, UploadOptions{Overwrite: true})
	return entries, err
}

// SaveUploadedDetailed menyimpan berkas upload dengan dukungan struktur subfolder (rel_paths) dan kontrol overwrite.
func (m *Manager) SaveUploadedDetailed(destDir string, files []*multipart.FileHeader, opts UploadOptions) ([]FileEntry, []string, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("tidak ada berkas yang diunggah")
	}
	resolvedDir, err := m.ResolvePath(destDir, true)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(resolvedDir)
	if err != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("direktori tujuan tidak valid")
	}

	uidResolver := newUIDGidResolver()
	saved := make([]FileEntry, 0, len(files))
	var skipped []string

	for idx, fh := range files {
		rawRel := ""
		if idx < len(opts.RelPaths) {
			rawRel = opts.RelPaths[idx]
		}
		if strings.TrimSpace(rawRel) == "" {
			rawRel = filepath.Base(strings.ReplaceAll(fh.Filename, "\\", "/"))
		}
		relPath, err := sanitizeUploadRelPath(rawRel)
		if err != nil {
			return saved, skipped, fmt.Errorf("nama berkas upload tidak valid (%s): %w", fh.Filename, err)
		}

		dstPath, err := m.ResolvePath(filepath.Join(resolvedDir, relPath), false)
		if err != nil {
			return saved, skipped, err
		}
		if !isWithinRoot(dstPath, resolvedDir) || dstPath == resolvedDir {
			return saved, skipped, fmt.Errorf("tujuan upload keluar dari direktori target")
		}
		if !opts.Overwrite {
			if _, statErr := os.Lstat(dstPath); statErr == nil {
				skipped = append(skipped, filepath.ToSlash(relPath))
				continue
			}
		}

		parentDir := filepath.Dir(dstPath)
		if mkErr := os.MkdirAll(parentDir, 0755); mkErr != nil {
			return saved, skipped, fmt.Errorf("gagal membuat subfolder '%s': %w", filepath.Dir(relPath), mkErr)
		}
		inheritParentNonRootOwnership(parentDir)

		srcFile, err := fh.Open()
		if err != nil {
			return saved, skipped, fmt.Errorf("gagal membuka stream upload '%s': %w", relPath, err)
		}

		outFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			_ = srcFile.Close()
			return saved, skipped, fmt.Errorf("gagal menulis '%s': %w", relPath, err)
		}

		_, copyErr := io.Copy(outFile, srcFile)
		_ = srcFile.Close()
		closeErr := outFile.Close()
		if copyErr != nil {
			_ = os.Remove(dstPath)
			return saved, skipped, fmt.Errorf("gagal menyimpan '%s': %w", relPath, copyErr)
		}
		if closeErr != nil {
			return saved, skipped, fmt.Errorf("gagal menutup '%s': %w", relPath, closeErr)
		}
		inheritParentNonRootOwnership(dstPath)

		if st, sErr := os.Lstat(dstPath); sErr == nil {
			saved = append(saved, m.buildEntry(dstPath, st, uidResolver))
		}
	}
	return saved, skipped, nil
}

// ServeRaw mengirimkan berkas statis (misal gambar) secara aman dengan header isolasi CSP.
func (m *Manager) ServeRaw(w http.ResponseWriter, r *http.Request, filePath string) error {
	resolved, err := m.ResolvePath(filePath, true)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("direktori tidak dapat dipratinjau langsung")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return err
	}
	defer f.Close()

	// Paksa sandbox CSP agar berkas HTML/SVG tidak dapat menjalankan skrip pada origin panel.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(resolved), info.ModTime(), f)
	return nil
}
