package installer

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
	"sync"
	"time"
	"wdev/internal/i18n"
)

// ToolchainStatus merepresentasikan informasi status instalasi & rilis dari suatu SDK/Toolchain.
type ToolchainStatus struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Category         string `json:"category"`
	Description      string `json:"description"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version"`
	ExtraInfo        string `json:"extra_info,omitempty"`
	LatestVersion    string `json:"latest_version"`
	HasUpdate        bool   `json:"has_update"`
	InstallComponent string `json:"install_component"`
}

type toolchainCache struct {
	mu        sync.RWMutex
	checkedAt time.Time
	latest    map[string]string
}

var globalToolchainCache = &toolchainCache{
	latest: make(map[string]string),
}

// toolCmd mengeksekusi perintah dengan environment toolchain lengkap (CARGO_HOME, RUSTUP_HOME, dsb).
func toolCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	env := os.Environ()

	hasCargo := false
	hasRustup := false
	for _, e := range env {
		if strings.HasPrefix(e, "CARGO_HOME=") {
			hasCargo = true
		}
		if strings.HasPrefix(e, "RUSTUP_HOME=") {
			hasRustup = true
		}
	}
	if !hasCargo && dirExists("/usr/local/cargo") {
		env = append(env, "CARGO_HOME=/usr/local/cargo")
	}
	if !hasRustup && dirExists("/usr/local/rustup") {
		env = append(env, "RUSTUP_HOME=/usr/local/rustup")
	}
	cmd.Env = env
	return cmd
}

// findTool mencari executable dengan memprioritaskan direktori instalasi resmi wdev sebelum PATH sistem.
func findTool(names ...string) string {
	for _, name := range names {
		// 1. Prioritaskan lokasi instalasi resmi wdev (/usr/local/...)
		canonical := []string{
			filepath.Join("/usr/local/bin", name),
			filepath.Join("/usr/local/go/bin", name),
			filepath.Join("/usr/local/cargo/bin", name),
			filepath.Join("/usr/local/flutter/bin", name),
			filepath.Join("/usr/local/android-sdk/cmdline-tools/latest/bin", name),
			filepath.Join("/usr/local/android-sdk/platform-tools", name),
			filepath.Join("/usr/local/lib/nodejs/current/bin", name),
		}
		for _, p := range canonical {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}

		// 2. Fallback ke PATH dan lokasi sistem standar
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		systemDirs := []string{
			filepath.Join("/usr/bin", name),
			filepath.Join("/usr/sbin", name),
			filepath.Join("/sbin", name),
			filepath.Join("/bin", name),
		}
		if _, home := ResolveTargetUserAndHome(); home != "" {
			systemDirs = append(systemDirs,
				filepath.Join(home, ".cargo/bin", name),
				filepath.Join(home, ".local/bin", name),
			)
		}
		for _, p := range systemDirs {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func getAptCandidates(ctx context.Context, pkgs ...string) map[string]string {
	res := make(map[string]string)
	for _, p := range pkgs {
		res[p] = "-"
	}
	if len(pkgs) == 0 {
		return res
	}

	args := append([]string{"policy"}, pkgs...)
	out, err := exec.CommandContext(ctx, "apt-cache", args...).CombinedOutput()
	if err != nil {
		return res
	}

	lines := strings.Split(string(out), "\n")
	currentPkg := ""
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
			pkg := strings.TrimSuffix(trimmed, ":")
			if _, ok := res[pkg]; ok {
				currentPkg = pkg
			} else {
				currentPkg = ""
			}
			continue
		}
		if currentPkg != "" && strings.HasPrefix(trimmed, "Candidate:") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				cand := parts[1]
				cand = strings.Split(cand, "+")[0]
				cand = strings.Split(cand, "~")[0]
				cand = strings.Split(cand, "-")[0]
				cand = strings.TrimPrefix(cand, "5:")
				res[currentPkg] = cand
			}
			currentPkg = ""
		}
	}
	return res
}

func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "rust-")
	v = strings.TrimPrefix(v, "go")
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "version ")
	return v
}

func compareVersions(v1, v2 string) int {
	v1Clean := cleanVersion(v1)
	v2Clean := cleanVersion(v2)
	if v1Clean == v2Clean {
		return 0
	}
	parts1 := strings.Split(strings.Split(v1Clean, "-")[0], ".")
	parts2 := strings.Split(strings.Split(v2Clean, "-")[0], ".")
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			n1, _ = strconv.Atoi(strings.TrimRight(parts1[i], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"))
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(strings.TrimRight(parts2[i], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"))
		}
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}
	return 0
}

func hasNewerVersion(installed, latest string) bool {
	instClean := cleanVersion(installed)
	latClean := cleanVersion(latest)
	if instClean == "" || latClean == "" || latClean == "-" {
		return false
	}
	if instClean == latClean {
		return false
	}
	// Bila versi terpasang sudah mengandung atau diawali versi rilis terbaru
	if strings.HasPrefix(instClean, latClean) || strings.Contains(instClean, latClean) {
		return false
	}
	return compareVersions(instClean, latClean) < 0
}

func (inst *Installer) fetchLatestVersions(ctx context.Context) map[string]string {
	latest := make(map[string]string)
	client := &http.Client{Timeout: 3 * time.Second}

	var wg sync.WaitGroup
	var mu sync.Mutex

	setLatest := func(key, val string) {
		if val == "" {
			return
		}
		mu.Lock()
		latest[key] = val
		mu.Unlock()
	}

	// 1. Golang
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://go.dev/dl/?mode=json", nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "wdev-control-plane")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var releases []struct {
				Version string `json:"version"`
				Stable  bool   `json:"stable"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil {
				for _, r := range releases {
					if r.Stable {
						setLatest("go", r.Version)
						break
					}
				}
			}
		}
	}()

	// 2. Node.js (Cocokkan dengan major yang terpasang atau rilis LTS terbaru)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://nodejs.org/dist/index.json", nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "wdev-control-plane")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var releases []struct {
				Version string `json:"version"`
				LTS     any    `json:"lts"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil {
				curMajor := ""
				if nodeBin := findTool("node"); nodeBin != "" {
					if out, err := toolCmd(ctx, nodeBin, "-v").CombinedOutput(); err == nil {
						v := strings.TrimSpace(string(out))
						if strings.HasPrefix(v, "v") {
							curMajor = strings.Split(v, ".")[0] + "."
						}
					}
				}
				chosen := ""
				if curMajor != "" {
					for _, r := range releases {
						if strings.HasPrefix(r.Version, curMajor) {
							chosen = r.Version
							break
						}
					}
				}
				if chosen == "" {
					for _, r := range releases {
						if r.LTS != false && r.LTS != nil {
							chosen = r.Version
							break
						}
					}
				}
				if chosen != "" {
					setLatest("node", chosen)
				}
			}
		}
	}()

	// 3. Rust
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/rust-lang/rust/releases/latest", nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "wdev-control-plane")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var rel struct {
				TagName string `json:"tag_name"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil && rel.TagName != "" {
				ver := strings.TrimPrefix(rel.TagName, "rust-")
				ver = strings.TrimPrefix(ver, "v")
				setLatest("rust", ver)
			}
		}
	}()

	// 4. Flutter
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json", nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "wdev-control-plane")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var data struct {
				Releases []struct {
					Version string `json:"version"`
					Channel string `json:"channel"`
				} `json:"releases"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				for _, r := range data.Releases {
					if r.Channel == "stable" {
						setLatest("flutter", r.Version)
						break
					}
				}
			}
		}
	}()

	// 5. OpenJDK Java (Eclipse Adoptium API) & Redis (APT/DNF)
	wg.Add(1)
	go func() {
		defer wg.Done()
		javaMajor := "21"
		if javaBin := findTool("java"); javaBin != "" {
			if out, err := toolCmd(ctx, javaBin, "-version").CombinedOutput(); err == nil {
				line := strings.Split(string(out), "\n")[0]
				if start := strings.Index(line, "\""); start != -1 {
					if end := strings.LastIndex(line, "\""); end > start {
						if m := strings.Split(line[start+1:end], ".")[0]; m != "" {
							javaMajor = m
						}
					}
				}
			}
		}
		if _, verStr, err := fetchAdoptiumLatest(ctx, javaMajor); err == nil && verStr != "" {
			setLatest("java", verStr)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if inst.isDebian {
			cands := getAptCandidates(ctx, "redis", "redis-server")
			if cand, ok := cands["redis"]; ok && cand != "-" {
				setLatest("redis", cand)
			} else if cand, ok := cands["redis-server"]; ok && cand != "-" {
				setLatest("redis", cand)
			}
		} else if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
			if cand := dnfRepoQueryLatest(ctx, "redis"); cand != "-" {
				setLatest("redis", cand)
			}
		}
	}()

	// 6. Android SDK & Base
	setLatest("android", "API 36 (Tools 23.0)")
	setLatest("base", "Up-to-date")

	wg.Wait()
	return latest
}

// GetToolchainStatuses mengumpulkan status instalasi lokal dan versi rilis terbaru dari semua toolchain & SDK (English default).
func (inst *Installer) GetToolchainStatuses(ctx context.Context, forceRefresh bool) ([]ToolchainStatus, error) {
	return inst.GetToolchainStatusesLocale(ctx, forceRefresh, i18n.DefaultLocale)
}

// GetToolchainStatusesLocale mengumpulkan status instalasi lokal dan versi rilis terbaru dengan label terlokalisasi.
func (inst *Installer) GetToolchainStatusesLocale(ctx context.Context, forceRefresh bool, lang string) ([]ToolchainStatus, error) {
	loc := i18n.Normalize(lang)
	// Ambil atau perbarui cache versi remote
	globalToolchainCache.mu.Lock()
	needFetch := forceRefresh || globalToolchainCache.checkedAt.IsZero() || time.Since(globalToolchainCache.checkedAt) > 10*time.Minute
	if needFetch {
		fetched := inst.fetchLatestVersions(ctx)
		for k, v := range fetched {
			globalToolchainCache.latest[k] = v
		}
		globalToolchainCache.checkedAt = time.Now()
	}
	latestMap := make(map[string]string)
	for k, v := range globalToolchainCache.latest {
		latestMap[k] = v
	}
	globalToolchainCache.mu.Unlock()

	items := make([]ToolchainStatus, 8)
	var localWg sync.WaitGroup
	localWg.Add(8)

	// 1. Rust, Mold & Sccache
	go func() {
		defer localWg.Done()
		rustcBin := findTool("rustc")
		installed := rustcBin != ""
		installedVer := ""
		extra := ""

		if !installed {
			if matches, _ := filepath.Glob("/usr/local/rustup/toolchains/*/bin/rustc"); len(matches) > 0 {
				rustcBin = matches[0]
				installed = true
			}
		}

		if installed {
			if out, err := toolCmd(ctx, rustcBin, "-V").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 2 {
					installedVer = fields[1]
				}
			}
			if installedVer == "" {
				if matches, _ := filepath.Glob("/usr/local/rustup/toolchains/*/bin/rustc"); len(matches) > 0 {
					if out, err := toolCmd(ctx, matches[0], "--version").CombinedOutput(); err == nil {
						fields := strings.Fields(string(out))
						if len(fields) >= 2 {
							installedVer = fields[1]
						}
					}
				}
			}

			var extras []string
			if moldBin := findTool("mold"); moldBin != "" {
				if out, err := toolCmd(ctx, moldBin, "--version").CombinedOutput(); err == nil {
					fields := strings.Fields(string(out))
					if len(fields) >= 2 {
						extras = append(extras, "Mold "+fields[1])
					}
				}
			}
			if sccacheBin := findTool("sccache"); sccacheBin != "" {
				if out, err := toolCmd(ctx, sccacheBin, "--version").CombinedOutput(); err == nil {
					fields := strings.Fields(string(out))
					if len(fields) >= 2 {
						extras = append(extras, "Sccache "+fields[1])
					}
				}
			}
			if len(extras) > 0 {
				extra = strings.Join(extras, " • ")
			}
		}
		latestVer := latestMap["rust"]
		if latestVer == "" {
			latestVer = "-"
		}
		items[0] = ToolchainStatus{
			ID:               "rust",
			Name:             "🦀 Rust, Mold Linker & Sccache",
			Category:         "Systems & Toolchain",
			Description:      i18n.Pick(loc, "Modern Rust compiler toolchain with ultrafast Mold linker and Sccache compiler cache.", "Toolchain compiler Rust modern dengan linker super cepat Mold dan compiler cache Sccache."),
			Installed:        installed && installedVer != "",
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        installed && installedVer != "" && hasNewerVersion(installedVer, latestVer),
			InstallComponent: "rust",
		}
	}()

	// 2. Golang
	go func() {
		defer localWg.Done()
		goBin := findTool("go")
		installed := goBin != ""
		installedVer := ""
		extra := ""
		if installed {
			if out, err := toolCmd(ctx, goBin, "version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 3 {
					installedVer = fields[2]
				}
			}
			if goEnv, err := toolCmd(ctx, goBin, "env", "GOROOT").CombinedOutput(); err == nil {
				groot := strings.TrimSpace(string(goEnv))
				if groot != "" {
					extra = "GOROOT: " + groot
				}
			}
		}
		latestVer := latestMap["go"]
		if latestVer == "" {
			latestVer = "-"
		}
		items[1] = ToolchainStatus{
			ID:               "go",
			Name:             i18n.Pick(loc, "🐹 Golang (Official Go Release)", "🐹 Golang (Go Rilis Resmi)"),
			Category:         "Backend & CLI",
			Description:      i18n.Pick(loc, "Latest official Go language compiler and toolchain in /usr/local/go.", "Compiler dan toolchain bahasa Go resmi terbaru di /usr/local/go."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        installed && hasNewerVersion(installedVer, latestVer),
			InstallComponent: "go",
		}
	}()

	// 3. Node.js LTS
	go func() {
		defer localWg.Done()
		nodeBin := findTool("node")
		installed := nodeBin != ""
		installedVer := ""
		extra := ""
		if installed {
			if out, err := toolCmd(ctx, nodeBin, "-v").CombinedOutput(); err == nil {
				installedVer = strings.TrimSpace(string(out))
			}
			if npmBin := findTool("npm"); npmBin != "" {
				if out, err := toolCmd(ctx, npmBin, "-v").CombinedOutput(); err == nil {
					extra = "NPM v" + strings.TrimSpace(string(out))
				}
			}
		}
		latestVer := latestMap["node"]
		if latestVer == "" {
			latestVer = "-"
		}
		items[2] = ToolchainStatus{
			ID:               "node",
			Name:             "🟢 Node.js LTS & NPM",
			Category:         "Web & Fullstack",
			Description:      i18n.Pick(loc, "Modern V8 LTS JavaScript runtime with npm package manager in /usr/local/lib/nodejs.", "Runtime JavaScript v8 modern LTS dengan package manager npm di /usr/local/lib/nodejs."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        installed && hasNewerVersion(installedVer, latestVer),
			InstallComponent: "node",
		}
	}()

	// 4. OpenJDK Java
	go func() {
		defer localWg.Done()
		javaBin := findTool("java")
		installed := javaBin != ""
		installedVer := ""
		extra := ""
		if installed {
			if out, err := toolCmd(ctx, javaBin, "-version").CombinedOutput(); err == nil {
				lines := strings.Split(string(out), "\n")
				if len(lines) > 0 {
					line := strings.TrimSpace(lines[0])
					if idx := strings.Index(line, "\""); idx != -1 {
						end := strings.LastIndex(line, "\"")
						if end > idx {
							installedVer = line[idx+1 : end]
						}
					}
					if installedVer == "" {
						installedVer = line
					}
				}
			}
			if entries, err := os.ReadDir("/usr/lib/jvm"); err == nil {
				var jdks []string
				for _, e := range entries {
					if e.IsDir() && strings.HasPrefix(e.Name(), "java-") {
						parts := strings.Split(e.Name(), "-")
						if len(parts) >= 2 {
							jdks = append(jdks, "JDK "+parts[1])
						}
					}
				}
				if len(jdks) > 0 {
					extra = strings.Join(jdks, ", ")
				}
			}
		}
		latestVer := latestMap["java"]
		if latestVer == "" {
			latestVer = "-"
		}
		items[3] = ToolchainStatus{
			ID:               "java",
			Name:             i18n.Pick(loc, "☕ OpenJDK Java (Multi-version JDK)", "☕ OpenJDK Java (JDK Multi-versi)"),
			Category:         "JVM & Enterprise",
			Description:      i18n.Pick(loc, "Multi-version OpenJDK Java Development Kit (JDK 17, 21 LTS, and 25).", "Java Development Kit OpenJDK multi-versi (JDK 17, 21 LTS, dan 25)."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        installed && hasNewerVersion(installedVer, latestVer),
			InstallComponent: "java",
		}
	}()

	// 5. Flutter SDK
	go func() {
		defer localWg.Done()
		flutterBin := findTool("flutter")
		installed := flutterBin != "" || dirExists("/usr/local/flutter")
		installedVer := ""
		extra := ""
		if installed {
			if out, err := toolCmd(ctx, "git", "-C", "/usr/local/flutter", "describe", "--tags").CombinedOutput(); err == nil {
				installedVer = strings.TrimSpace(string(out))
			} else if flutterBin != "" {
				if out, err := toolCmd(ctx, flutterBin, "--version").CombinedOutput(); err == nil {
					lines := strings.Split(string(out), "\n")
					if len(lines) > 0 {
						fields := strings.Fields(lines[0])
						if len(fields) >= 2 {
							installedVer = fields[1]
						}
					}
				}
			}
			if dartBin := findTool("dart"); dartBin != "" {
				if out, err := toolCmd(ctx, dartBin, "--version").CombinedOutput(); err == nil {
					fields := strings.Fields(string(out))
					for i, f := range fields {
						if f == "version:" && i+1 < len(fields) {
							extra = "Dart " + fields[i+1]
							break
						}
					}
				}
			}
			if extra == "" {
				extra = "Channel stable"
			} else {
				extra = "Channel stable • " + extra
			}
		}
		latestVer := latestMap["flutter"]
		if latestVer == "" {
			latestVer = "-"
		}
		items[4] = ToolchainStatus{
			ID:               "flutter",
			Name:             "🎯 Flutter SDK (Channel Stable)",
			Category:         "Mobile & Multiplatform",
			Description:      i18n.Pick(loc, "Multiplatform UI framework for Android, Web, and Desktop apps in /usr/local/flutter.", "Framework UI aplikasi multiplatform Android, Web, dan Desktop di /usr/local/flutter."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        installed && hasNewerVersion(installedVer, latestVer),
			InstallComponent: "flutter",
		}
	}()

	// 6. Android SDK & CLI Tools
	go func() {
		defer localWg.Done()
		sdkDir := "/usr/local/android-sdk"
		installed := dirExists(filepath.Join(sdkDir, "cmdline-tools")) || findTool("sdkmanager") != ""
		installedVer := ""
		extra := ""
		if installed {
			propsPath := filepath.Join(sdkDir, "cmdline-tools", "latest", "source.properties")
			toolsVer := ""
			if data, err := os.ReadFile(propsPath); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, "Pkg.Revision=") {
						toolsVer = strings.TrimPrefix(line, "Pkg.Revision=")
						break
					}
				}
			}
			var platforms []string
			if entries, err := os.ReadDir(filepath.Join(sdkDir, "platforms")); err == nil {
				for _, e := range entries {
					if e.IsDir() && strings.HasPrefix(e.Name(), "android-") {
						platforms = append(platforms, strings.TrimPrefix(e.Name(), "android-"))
					}
				}
			}
			if toolsVer != "" {
				installedVer = "Tools v" + toolsVer
			} else {
				installedVer = i18n.Pick(loc, "Installed", "Terpasang")
			}
			if len(platforms) > 0 {
				extra = fmt.Sprintf("API: %s", strings.Join(platforms, ", "))
			}
		}
		latestVer := latestMap["android"]
		if latestVer == "" {
			latestVer = "API 36 (Tools 23.0)"
		}
		items[5] = ToolchainStatus{
			ID:               "android",
			Name:             "🤖 Android SDK & CLI Tools",
			Category:         "Mobile & Native",
			Description:      i18n.Pick(loc, "Command Line Tools, Platform Tools, and SDK Platform APIs in /usr/local/android-sdk.", "Command Line Tools, Platform Tools, dan SDK Platform API di /usr/local/android-sdk."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        false,
			InstallComponent: "android",
		}
	}()

	// 7. Redis Cache
	go func() {
		defer localWg.Done()
		redisBin := findTool("redis-server")
		installed := redisBin != ""
		installedVer := ""
		extra := "Port 6379 • In-memory cache"
		if installed {
			if out, err := toolCmd(ctx, redisBin, "-v").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				for _, f := range fields {
					if strings.HasPrefix(f, "v=") {
						installedVer = strings.TrimPrefix(f, "v=")
						break
					}
				}
			}
		}
		latestVer := latestMap["redis"]
		if latestVer == "" {
			latestVer = "-"
		}
		items[6] = ToolchainStatus{
			ID:               "redis",
			Name:             "⚡ Redis / Valkey Cache Server",
			Category:         "Database & Cache",
			Description:      i18n.Pick(loc, "High-speed local in-memory cache server on port 6379.", "Server in-memory cache lokal berkecepatan tinggi port 6379."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        installed && hasNewerVersion(installedVer, latestVer),
			InstallComponent: "redis",
		}
	}()

	// 8. Base Dev Tools
	go func() {
		defer localWg.Done()
		gccBin := findTool("gcc")
		makeBin := findTool("make")
		cmakeBin := findTool("cmake")
		installed := gccBin != "" && makeBin != "" && cmakeBin != ""
		installedVer := ""
		extra := "GCC, CMake, Make, Clang, Jq, Curl"
		if installed {
			var details []string
			if gccBin != "" {
				if out, err := toolCmd(ctx, gccBin, "-dumpfullversion").CombinedOutput(); err == nil {
					details = append(details, "GCC "+strings.TrimSpace(string(out)))
				} else if out, err := toolCmd(ctx, gccBin, "-dumpversion").CombinedOutput(); err == nil {
					details = append(details, "GCC "+strings.TrimSpace(string(out)))
				}
			}
			if cmakeBin != "" {
				if out, err := toolCmd(ctx, cmakeBin, "--version").CombinedOutput(); err == nil {
					fields := strings.Fields(string(out))
					if len(fields) >= 3 {
						details = append(details, "CMake "+fields[2])
					}
				}
			}
			if len(details) > 0 {
				installedVer = strings.Join(details, " • ")
			} else {
				installedVer = i18n.Pick(loc, "Installed (Complete)", "Terpasang (Lengkap)")
			}
		}
		latestVer := latestMap["base"]
		if latestVer == "" {
			latestVer = "Up-to-date"
		}
		items[7] = ToolchainStatus{
			ID:               "base",
			Name:             i18n.Pick(loc, "📦 Core System Dependencies", "📦 Dependensi Dasar Sistem"),
			Category:         "Core Build Tools",
			Description:      i18n.Pick(loc, "Essential build tools, clang, cmake, ninja, curl, git, jq, and distro dev libraries.", "Build tools esensial, clang, cmake, ninja, curl, git, jq, dan pustaka dev distro."),
			Installed:        installed,
			InstalledVersion: installedVer,
			ExtraInfo:        extra,
			LatestVersion:    latestVer,
			HasUpdate:        false,
			InstallComponent: "base",
		}
	}()

	localWg.Wait()
	return items, nil
}
