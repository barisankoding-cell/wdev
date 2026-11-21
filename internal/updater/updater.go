package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	RepoSlug    = "barisankoding-cell/wdev"
	BinaryPath  = "/usr/local/bin/wdev"
	EdgePath    = "/usr/local/bin/wdev-edge"
	checksumTag = ".sha256"
)

var (
	cacheMu   sync.Mutex
	cachedRel Release
	cachedAt  time.Time
	cacheTTL  = 15 * time.Minute
)

type Release struct {
	Tag    string  `json:"tag_name"`
	Body   string  `json:"body"`
	Assets []Asset `json:"assets"`
}

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Options struct {
	CheckOnly bool
	Force     bool
	EdgeHost  string // mis. root@203.0.113.10
	EdgeKey   string
	SourceDir string // alternatif: build dari checkout git lokal
}

type Result struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	HasUpdate  bool   `json:"has_update"`
	Installed  bool   `json:"installed"`
	AssetName  string `json:"asset_name,omitempty"`
	Message    string `json:"message,omitempty"`
	EdgeUpdate string `json:"edge_update,omitempty"`
	Changelog  string `json:"changelog,omitempty"`
}

func FetchLatest(ctx context.Context) (Release, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", RepoSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", "wdev-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("gagal menghubungi GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return Release{}, fmt.Errorf("belum ada rilis GitHub publik untuk %s; gunakan: sudo wdev update --source <path-repo>", RepoSlug)
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub API status %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	if strings.TrimSpace(rel.Tag) == "" {
		return Release{}, fmt.Errorf("rilis terbaru tidak memiliki tag")
	}
	return rel, nil
}

// FetchLatestCached mengambil rilis terbaru dengan caching in-memory untuk menghemat kuota API GitHub.
func FetchLatestCached(ctx context.Context, force bool) (Release, error) {
	cacheMu.Lock()
	if !force && !cachedAt.IsZero() && time.Since(cachedAt) < cacheTTL && cachedRel.Tag != "" {
		rel := cachedRel
		cacheMu.Unlock()
		return rel, nil
	}
	cacheMu.Unlock()

	rel, err := FetchLatest(ctx)
	if err != nil {
		cacheMu.Lock()
		defer cacheMu.Unlock()
		if cachedRel.Tag != "" {
			return cachedRel, nil
		}
		return Release{}, err
	}

	cacheMu.Lock()
	cachedRel = rel
	cachedAt = time.Now()
	cacheMu.Unlock()
	return rel, nil
}

func archName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "armv7"
	default:
		return runtime.GOARCH
	}
}

func binaryAssetName() string {
	return "wdev-linux-" + archName()
}

func pickAsset(assets []Asset, name string) (Asset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Asset{}, false
}

// compare membandingkan versi semver sederhana (major.minor.patch).
func compare(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		parts := strings.Split(v, ".")
		out := make([]int, 0, 3)
		for i := 0; i < 3; i++ {
			n := 0
			if i < len(parts) {
				clean := parts[i]
				for j, r := range clean {
					if r < '0' || r > '9' {
						clean = clean[:j]
						break
					}
				}
				n, _ = strconv.Atoi(clean)
			}
			out = append(out, n)
		}
		return out
	}
	x, y := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if x[i] < y[i] {
			return -1
		}
		if x[i] > y[i] {
			return 1
		}
	}
	return 0
}

func download(ctx context.Context, url, dst string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "wdev-updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unduhan status %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func verifyChecksum(path, checksumURL string) error {
	if checksumURL == "" {
		return nil
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(checksumURL)
	if err != nil {
		return fmt.Errorf("gagal mengunduh checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	want := strings.Fields(strings.TrimSpace(string(body)))
	if len(want) == 0 {
		return fmt.Errorf("file checksum kosong")
	}
	expected := strings.ToLower(want[0])

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("checksum tidak cocok: got %s want %s", got, expected)
	}
	return nil
}

func restartService(ctx context.Context, unit string) error {
	if out, err := exec.CommandContext(ctx, "systemctl", "cat", unit).CombinedOutput(); err != nil || len(out) == 0 {
		return nil
	}
	// Menggunakan --no-block agar pemanggilan restart dari dalam proses daemon tidak deadlock
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "--no-block", unit)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restart %s gagal: %w (%s)", unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildFromSource memperbarui dari checkout git lokal lalu build ulang.
func buildFromSource(ctx context.Context, dir, version string, cb func(string)) (Result, error) {
	res := Result{Current: version, Latest: version, Installed: true}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return res, fmt.Errorf("%s bukan checkout repo wdev (go.mod tidak ada)", dir)
	}
	if os.Geteuid() != 0 {
		return res, fmt.Errorf("update dari source butuh root: jalankan ulang dengan sudo")
	}
	steps := [][]string{
		{"git", "-C", dir, "fetch", "--all", "--tags"},
		{"git", "-C", dir, "pull", "--ff-only"},
	}
	for _, cmd := range steps {
		cb("$ " + strings.Join(cmd, " "))
		if out, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			return res, fmt.Errorf("%s gagal: %w (%s)", cmd[0], err, strings.TrimSpace(string(out)))
		}
	}
	tagOut, _ := exec.CommandContext(ctx, "git", "-C", dir, "describe", "--tags", "--abbrev=0").Output()
	tag := strings.TrimPrefix(strings.TrimSpace(string(tagOut)), "v")
	if tag == "" {
		tag = version
	}
	res.Latest = tag

	tmpDir, err := os.MkdirTemp("", "wdev-build-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "wdev")
	cb("Membangun biner...")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath",
		"-ldflags", fmt.Sprintf("-s -w -X wdev/internal/version.Version=%s", tag),
		"-o", tmpBin, "./cmd/wdev")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return res, fmt.Errorf("go build gagal: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	backup := BinaryPath + ".bak"
	if _, err := os.Stat(BinaryPath); err == nil {
		_ = os.Remove(backup)
		if err := os.Rename(BinaryPath, backup); err != nil {
			return res, err
		}
	}
	if err := os.Rename(tmpBin, BinaryPath); err != nil {
		_ = os.Rename(backup, BinaryPath)
		return res, err
	}
	_ = os.Chmod(BinaryPath, 0755)
	cb("✓ Biner diperbarui ke v" + tag)
	if err := restartService(ctx, "wdev.service"); err != nil {
		return res, err
	}
	cb("✓ wdev.service di-restart")
	res.Message = fmt.Sprintf("wdev dibangun ulang dari source (v%s)", tag)
	return res, nil
}

// Run memeriksa dan (bila tidak check-only) memasang rilis terbaru.
func Run(ctx context.Context, current string, opts Options, cb func(string)) (Result, error) {
	if cb == nil {
		cb = func(string) {}
	}
	res := Result{Current: current}
	if strings.TrimSpace(opts.SourceDir) != "" {
		return buildFromSource(ctx, opts.SourceDir, current, cb)
	}
	var rel Release
	var err error
	if opts.Force {
		rel, err = FetchLatest(ctx)
	} else {
		rel, err = FetchLatestCached(ctx, false)
	}
	if err != nil {
		return res, err
	}
	res.Latest = strings.TrimPrefix(strings.TrimSpace(rel.Tag), "v")
	res.HasUpdate = compare(res.Latest, current) > 0
	res.Changelog = rel.Body
	if !res.HasUpdate && !opts.Force {
		res.Message = "wdev sudah versi terbaru"
		cb("✓ " + res.Message)
		return res, nil
	}

	assetName := binaryAssetName()
	asset, ok := pickAsset(rel.Assets, assetName)
	if !ok {
		return res, fmt.Errorf("aset %s tidak ditemukan pada rilis %s (arsitektur %s/%s)", assetName, rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	res.AssetName = asset.Name
	if opts.CheckOnly {
		res.Message = fmt.Sprintf("update tersedia: v%s -> v%s", current, res.Latest)
		cb(res.Message)
		return res, nil
	}

	if runtime.GOOS != "linux" {
		return res, fmt.Errorf("self-update hanya didukung di Linux")
	}
	if os.Geteuid() != 0 {
		return res, fmt.Errorf("self-update butuh root: jalankan ulang dengan sudo")
	}

	cb(fmt.Sprintf("Mengunduh %s (v%s)...", asset.Name, res.Latest))
	tmpDir, err := os.MkdirTemp("", "wdev-update-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "wdev")
	if err := download(ctx, asset.URL, tmpBin); err != nil {
		return res, fmt.Errorf("gagal mengunduh biner: %w", err)
	}
	checksumAsset, hasChecksum := pickAsset(rel.Assets, asset.Name+checksumTag)
	if hasChecksum {
		if err := verifyChecksum(tmpBin, checksumAsset.URL); err != nil {
			return res, err
		}
		cb("✓ Checksum SHA256 cocok")
	} else {
		cb(" [!] Rilis tidak menyertakan checksum; lanjut tanpa verifikasi")
	}

	backup := BinaryPath + ".bak"
	if _, err := os.Stat(BinaryPath); err == nil {
		_ = os.Remove(backup)
		if err := os.Rename(BinaryPath, backup); err != nil {
			return res, fmt.Errorf("gagal membuat backup biner: %w", err)
		}
	}
	if err := os.Rename(tmpBin, BinaryPath); err != nil {
		_ = os.Rename(backup, BinaryPath)
		return res, fmt.Errorf("gagal memasang biner baru: %w", err)
	}
	if err := os.Chmod(BinaryPath, 0755); err != nil {
		return res, err
	}
	res.Installed = true
	cb("✓ Biner wdev diperbarui ke v" + res.Latest)

	if err := restartService(ctx, "wdev.service"); err != nil {
		return res, err
	}
	cb("✓ wdev.service di-restart")

	if strings.TrimSpace(opts.EdgeHost) != "" {
		cb("Memperbarui edge agent di " + opts.EdgeHost + "...")
		if err := updateEdge(ctx, opts); err != nil {
			res.EdgeUpdate = "failed: " + err.Error()
			cb(" [!] Edge agent gagal diperbarui: " + err.Error())
		} else {
			res.EdgeUpdate = "updated"
			cb("✓ Edge agent diperbarui dan di-restart")
		}
	}

	res.Message = fmt.Sprintf("wdev diperbarui ke v%s", res.Latest)
	cb("")
	cb("Langkah migrasi (sekali saja):")
	cb("  1. sudo wdev install mail")
	cb("  2. sudo wdev mail set-relay global <provider> <key>   # bila belum ada relay default")
	cb("  3. sudo wdev mail setup <domain>                       # ulangi untuk tiap domain")
	cb("  4. wdev mail status")
	return res, nil
}

func updateEdge(ctx context.Context, opts Options) error {
	scpArgs := []string{}
	sshArgs := []string{}
	if opts.EdgeKey != "" {
		scpArgs = append(scpArgs, "-i", opts.EdgeKey)
		sshArgs = append(sshArgs, "-i", opts.EdgeKey)
	}
	scpArgs = append(scpArgs, BinaryPath, opts.EdgeHost+":"+EdgePath)
	if out, err := exec.CommandContext(ctx, "scp", scpArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("scp gagal: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	sshArgs = append(sshArgs, opts.EdgeHost, "chmod 0755 "+EdgePath+" && systemctl restart wdev-edge-agent")
	if out, err := exec.CommandContext(ctx, "ssh", sshArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("ssh restart gagal: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
