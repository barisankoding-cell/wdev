package filemanager

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxExtractTotalBytes int64 = 512 << 20 // 512 MiB batas total hasil ekstrak
	maxExtractEntries    int   = 20000
)

func addPathToZip(zw *zip.Writer, srcPath, baseInZip, skipDestPath string) error {
	info, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	if srcPath == skipDestPath {
		return nil
	}
	// Abaikan symlink keluar saat kompresi agar arsip tetap portabel dan aman.
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	slashName := filepath.ToSlash(baseInZip)
	if info.IsDir() {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = strings.TrimSuffix(slashName, "/") + "/"
		header.Method = zip.Deflate
		if _, err := zw.CreateHeader(header); err != nil {
			return err
		}
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := addPathToZip(zw, filepath.Join(srcPath, e.Name()), filepath.Join(baseInZip, e.Name()), skipDestPath); err != nil {
				return err
			}
		}
		return nil
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = slashName
	header.Method = zip.Deflate

	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(writer, f)
	return err
}

// Compress membuat arsip .zip dari daftar berkas atau folder yang dipilih.
func (m *Manager) Compress(paths []string, outputPath string) (FileEntry, error) {
	if len(paths) == 0 {
		return FileEntry{}, fmt.Errorf("pilih minimal satu berkas atau folder untuk dikompres")
	}

	resolvedSources := make([]string, 0, len(paths))
	for _, p := range paths {
		res, err := m.ResolvePath(p, true)
		if err != nil {
			return FileEntry{}, err
		}
		if m.isProtectedBaseRoot(res) {
			return FileEntry{}, fmt.Errorf("direktori akar tidak boleh dikompres utuh")
		}
		resolvedSources = append(resolvedSources, res)
	}

	outTarget := strings.TrimSpace(outputPath)
	if outTarget == "" {
		if len(resolvedSources) == 1 {
			outTarget = resolvedSources[0] + ".zip"
		} else {
			outTarget = filepath.Join(filepath.Dir(resolvedSources[0]), "archive.zip")
		}
	}
	if !strings.HasSuffix(strings.ToLower(outTarget), ".zip") {
		outTarget += ".zip"
	}

	resolvedOut, err := m.ResolvePath(outTarget, false)
	if err != nil {
		return FileEntry{}, err
	}
	if _, err := os.Stat(filepath.Dir(resolvedOut)); err != nil {
		return FileEntry{}, fmt.Errorf("direktori tujuan arsip tidak ditemukan")
	}

	tmpZip, err := os.CreateTemp(filepath.Dir(resolvedOut), ".wdev-zip-*")
	if err != nil {
		return FileEntry{}, fmt.Errorf("gagal membuat arsip sementara: %w", err)
	}
	tmpName := tmpZip.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	zw := zip.NewWriter(tmpZip)
	for _, src := range resolvedSources {
		if err := addPathToZip(zw, src, filepath.Base(src), resolvedOut); err != nil {
			_ = zw.Close()
			_ = tmpZip.Close()
			return FileEntry{}, err
		}
	}
	if err := zw.Close(); err != nil {
		_ = tmpZip.Close()
		return FileEntry{}, err
	}
	if err := tmpZip.Chmod(0644); err != nil {
		_ = tmpZip.Close()
		return FileEntry{}, err
	}
	if err := tmpZip.Close(); err != nil {
		return FileEntry{}, err
	}
	inheritParentNonRootOwnership(tmpName)
	if err := os.Rename(tmpName, resolvedOut); err != nil {
		return FileEntry{}, err
	}
	cleanup = false

	st, err := os.Lstat(resolvedOut)
	if err != nil {
		return FileEntry{}, err
	}
	return m.buildEntry(resolvedOut, st, newUIDGidResolver()), nil
}

func safeArchiveTarget(destDir, memberName string) (string, error) {
	cleaned := strings.ReplaceAll(memberName, "\\", "/")
	if strings.HasPrefix(cleaned, "/") || cleaned == "" {
		return "", fmt.Errorf("entri arsip tidak aman (path absolut): %q", memberName)
	}
	joined := filepath.Clean(filepath.Join(destDir, cleaned))
	if !isWithinRoot(joined, destDir) {
		return "", fmt.Errorf("entri arsip tidak aman (path traversal): %q", memberName)
	}
	return joined, nil
}

// Extract mengekstrak arsip .zip, .tar.gz, .tgz, atau .tar ke direktori tujuan dengan proteksi Zip/Tar Slip.
func (m *Manager) Extract(archivePath, destDir string) (int, error) {
	resolvedArchive, err := m.ResolvePath(archivePath, true)
	if err != nil {
		return 0, err
	}
	archInfo, err := os.Stat(resolvedArchive)
	if err != nil || archInfo.IsDir() {
		return 0, fmt.Errorf("berkas arsip tidak valid")
	}

	if strings.TrimSpace(destDir) == "" {
		destDir = filepath.Dir(resolvedArchive)
	}
	resolvedDest, err := m.ResolvePath(destDir, false)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(resolvedDest, 0755); err != nil {
		return 0, fmt.Errorf("gagal menyiapkan direktori tujuan: %w", err)
	}
	inheritParentNonRootOwnership(resolvedDest)

	lower := strings.ToLower(resolvedArchive)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(resolvedArchive, resolvedDest)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTar(resolvedArchive, resolvedDest, true)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(resolvedArchive, resolvedDest, false)
	default:
		return 0, fmt.Errorf("format arsip belum didukung (gunakan .zip, .tar.gz, .tgz, atau .tar)")
	}
}

func extractZip(archivePath, destDir string) (int, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return 0, fmt.Errorf("gagal membuka arsip ZIP: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > maxExtractEntries {
		return 0, fmt.Errorf("jumlah entri arsip melebihi batas (%d)", maxExtractEntries)
	}

	var totalWritten int64
	count := 0
	for _, f := range zr.File {
		target, err := safeArchiveTarget(destDir, f.Name)
		if err != nil {
			return count, err
		}
		if target == destDir {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return count, fmt.Errorf("entri symlink di dalam arsip tidak diizinkan demi keamanan: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return count, err
			}
			inheritParentNonRootOwnership(target)
			count++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return count, err
		}
		inheritParentNonRootOwnership(filepath.Dir(target))

		rc, err := f.Open()
		if err != nil {
			return count, err
		}
		perm := f.Mode().Perm()
		if perm == 0 {
			perm = 0644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err != nil {
			_ = rc.Close()
			return count, err
		}

		remaining := maxExtractTotalBytes - totalWritten + 1
		n, copyErr := io.Copy(out, io.LimitReader(rc, remaining))
		_ = rc.Close()
		closeErr := out.Close()
		totalWritten += n
		if copyErr != nil {
			return count, copyErr
		}
		if closeErr != nil {
			return count, closeErr
		}
		if totalWritten > maxExtractTotalBytes {
			return count, fmt.Errorf("ukuran total ekstrak melebihi batas 512 MiB")
		}
		inheritParentNonRootOwnership(target)
		count++
	}
	return count, nil
}

func extractTar(archivePath, destDir string, useGzip bool) (int, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var reader io.Reader = f
	if useGzip {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return 0, fmt.Errorf("gagal membaca gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	tr := tar.NewReader(reader)
	var totalWritten int64
	count := 0

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("gagal membaca tar: %w", err)
		}
		count++
		if count > maxExtractEntries {
			return count, fmt.Errorf("jumlah entri arsip melebihi batas (%d)", maxExtractEntries)
		}
		if hdr.Name == "." || hdr.Name == "./" {
			continue
		}
		target, err := safeArchiveTarget(destDir, hdr.Name)
		if err != nil {
			return count, err
		}
		if target == destDir {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return count, err
			}
			inheritParentNonRootOwnership(target)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return count, err
			}
			inheritParentNonRootOwnership(filepath.Dir(target))
			perm := os.FileMode(hdr.Mode).Perm()
			if perm == 0 {
				perm = 0644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
			if err != nil {
				return count, err
			}
			remaining := maxExtractTotalBytes - totalWritten + 1
			n, copyErr := io.Copy(out, io.LimitReader(tr, remaining))
			closeErr := out.Close()
			totalWritten += n
			if copyErr != nil {
				return count, copyErr
			}
			if closeErr != nil {
				return count, closeErr
			}
			if totalWritten > maxExtractTotalBytes {
				return count, fmt.Errorf("ukuran total ekstrak melebihi batas 512 MiB")
			}
			inheritParentNonRootOwnership(target)
		case tar.TypeSymlink, tar.TypeLink:
			return count, fmt.Errorf("entri link di dalam arsip tidak diizinkan demi keamanan: %q", hdr.Name)
		case tar.TypeXHeader, tar.TypeXGlobalHeader:
			continue
		}
	}
	return count, nil
}

// StreamDownload mengunduh satu berkas langsung atau meng-stream arsip .zip bila memilih folder/multi-berkas.
func (m *Manager) StreamDownload(w http.ResponseWriter, r *http.Request, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("tidak ada berkas yang dipilih untuk diunduh")
	}

	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		rp, err := m.ResolvePath(p, true)
		if err != nil {
			return err
		}
		resolved = append(resolved, rp)
	}

	if len(resolved) == 1 {
		info, err := os.Stat(resolved[0])
		if err != nil {
			return err
		}
		if !info.IsDir() {
			f, err := os.Open(resolved[0])
			if err != nil {
				return err
			}
			defer f.Close()
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeHeaderFilename(filepath.Base(resolved[0]))))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeContent(w, r, filepath.Base(resolved[0]), info.ModTime(), f)
			return nil
		}
	}

	zipName := fmt.Sprintf("wdev-files-%s.zip", time.Now().Format("20060102-150405"))
	if len(resolved) == 1 {
		zipName = sanitizeHeaderFilename(filepath.Base(resolved[0])) + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	zw := zip.NewWriter(w)
	for _, src := range resolved {
		_ = addPathToZip(zw, src, filepath.Base(src), "")
	}
	return zw.Close()
}

func sanitizeHeaderFilename(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '"' || r == '\r' || r == '\n' || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	if clean == "" {
		return "download"
	}
	return clean
}
