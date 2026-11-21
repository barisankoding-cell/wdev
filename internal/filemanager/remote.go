package filemanager

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func parseRemoteURL(rawURL string, allowPrivate bool) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("URL tidak boleh kosong")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("URL tidak valid: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("hanya URL http:// atau https:// yang diizinkan")
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("host URL tidak valid")
	}
	if !allowPrivate {
		if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedIP(ip) {
			return nil, fmt.Errorf("akses ke alamat jaringan lokal/privat tidak diizinkan")
		}
	}
	return u, nil
}

func newSafeRemoteHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip != nil && isBlockedIP(ip) {
				return fmt.Errorf("koneksi ke alamat lokal/privat (%s) ditolak", host)
			}
			return nil
		},
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   3 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return fmt.Errorf("terlalu banyak pengalihan URL (redirect)")
			}
			_, err := parseRemoteURL(req.URL.String(), false)
			return err
		},
	}
}

// SetRemoteHTTPClient memungkinkan injeksi klien HTTP (untuk pengujian unit).
func (m *Manager) SetRemoteHTTPClient(client *http.Client) {
	m.remoteClient = client
}

func inferRemoteFilename(explicitName string, resp *http.Response, reqURL *url.URL) string {
	if strings.TrimSpace(explicitName) != "" {
		return explicitName
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn := strings.TrimSpace(params["filename"]); fn != "" {
				return filepath.Base(strings.ReplaceAll(fn, "\\", "/"))
			}
		}
	}
	base := path.Base(reqURL.Path)
	if base != "" && base != "/" && base != "." {
		return base
	}
	return "downloaded-file"
}

// FetchFromURL mengunduh berkas langsung dari URL publik ke direktori tujuan secara aman.
func (m *Manager) FetchFromURL(ctx context.Context, rawURL, destDir, fileName string, overwrite bool) (FileEntry, error) {
	parsedURL, err := parseRemoteURL(rawURL, m.remoteClient != nil)
	if err != nil {
		return FileEntry{}, err
	}

	resolvedDir, err := m.ResolvePath(destDir, true)
	if err != nil {
		return FileEntry{}, err
	}
	info, err := os.Stat(resolvedDir)
	if err != nil || !info.IsDir() {
		return FileEntry{}, fmt.Errorf("direktori tujuan tidak valid")
	}

	client := m.remoteClient
	if client == nil {
		client = newSafeRemoteHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return FileEntry{}, fmt.Errorf("gagal menyiapkan permintaan unduh: %w", err)
	}
	req.Header.Set("User-Agent", "wdev-filemanager/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return FileEntry{}, fmt.Errorf("gagal mengunduh dari URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FileEntry{}, fmt.Errorf("server mengembalikan HTTP %d saat mengunduh URL", resp.StatusCode)
	}
	if resp.ContentLength > MaxUploadBytes {
		return FileEntry{}, fmt.Errorf("ukuran berkas (%d byte) melebihi batas maksimum 256 MiB", resp.ContentLength)
	}

	candidateName := inferRemoteFilename(fileName, resp, resp.Request.URL)
	cleanName, err := validateBaseName(candidateName)
	if err != nil {
		return FileEntry{}, fmt.Errorf("nama berkas tujuan tidak valid: %w", err)
	}

	dstPath, err := m.ResolvePath(filepath.Join(resolvedDir, cleanName), false)
	if err != nil {
		return FileEntry{}, err
	}
	if !overwrite {
		if _, statErr := os.Lstat(dstPath); statErr == nil {
			return FileEntry{}, fmt.Errorf("berkas '%s' sudah ada dan opsi timpa tidak diaktifkan", cleanName)
		}
	}

	tmp, err := os.CreateTemp(resolvedDir, ".wdev-fetch-*")
	if err != nil {
		return FileEntry{}, fmt.Errorf("gagal menyiapkan berkas sementara: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	n, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, MaxUploadBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return FileEntry{}, fmt.Errorf("gagal menyimpan unduhan URL: %w", copyErr)
	}
	if closeErr != nil {
		return FileEntry{}, fmt.Errorf("gagal menutup berkas unduhan: %w", closeErr)
	}
	if n > MaxUploadBytes {
		return FileEntry{}, fmt.Errorf("ukuran unduhan melebihi batas maksimum 256 MiB")
	}

	_ = os.Chmod(tmpName, 0644)
	inheritParentNonRootOwnership(tmpName)
	if err := os.Rename(tmpName, dstPath); err != nil {
		return FileEntry{}, fmt.Errorf("gagal menempatkan berkas unduhan: %w", err)
	}
	cleanup = false

	st, err := os.Lstat(dstPath)
	if err != nil {
		return FileEntry{}, err
	}
	return m.buildEntry(dstPath, st, newUIDGidResolver()), nil
}
