package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SetupComponentStatus merangkum ketersediaan komponen modular pada host.
type SetupComponentStatus struct {
	Component string `json:"component"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

var (
	findToolFn               = findTool
	resolveSetupUserHomeFn   = func() string { _, home := ResolveTargetUserAndHome(); return home }
	resolveSetupOrcaHomeFn   = func() (string, string) { return ResolveOrcaUser() }
	statExistsFn             = func(p string) bool { _, err := os.Stat(p); return err == nil }
	dirExistsSetupFn         = dirExists
	detectOrcaVerFn          = DetectOrcaInstalledVersion
)

// GetComponentStatuses mendeteksi komponen modular yang sudah terpasang secara lokal tanpa koneksi jaringan eksternal.
func (inst *Installer) GetComponentStatuses(ctx context.Context) map[string]SetupComponentStatus {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	res := make(map[string]SetupComponentStatus, 21)
	var mu sync.Mutex
	set := func(comp string, installed bool, version string) {
		mu.Lock()
		res[comp] = SetupComponentStatus{
			Component: comp,
			Installed: installed,
			Version:   strings.TrimSpace(version),
		}
		mu.Unlock()
	}

	var wg sync.WaitGroup
	runProbe := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	// 1. Base dev tools
	runProbe(func() {
		gccBin := findToolFn("gcc")
		makeBin := findToolFn("make")
		cmakeBin := findToolFn("cmake")
		installed := gccBin != "" && makeBin != "" && cmakeBin != ""
		ver := ""
		if installed && gccBin != "" {
			if out, err := toolCmd(probeCtx, gccBin, "-dumpversion").CombinedOutput(); err == nil {
				v := strings.TrimSpace(string(out))
				if v != "" {
					ver = "GCC " + v
				}
			}
		}
		set("base", installed, ver)
	})

	// 2. Node.js
	runProbe(func() {
		bin := findToolFn("node")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "-v").CombinedOutput(); err == nil {
				ver = strings.TrimSpace(string(out))
			}
		}
		set("node", bin != "", ver)
	})

	// 3. Golang
	runProbe(func() {
		bin := findToolFn("go")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 3 {
					ver = strings.TrimPrefix(fields[2], "go")
				}
			}
		}
		set("go", bin != "", ver)
	})

	// 4. Rust
	runProbe(func() {
		bin := findToolFn("rustc")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "--version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 2 {
					ver = fields[1]
				}
			}
		}
		set("rust", bin != "", ver)
	})

	// 5. OpenJDK Java
	runProbe(func() {
		bin := findToolFn("java")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "-version").CombinedOutput(); err == nil {
				for _, line := range strings.Split(string(out), "\n") {
					if q1 := strings.Index(line, "\""); q1 >= 0 {
						if q2 := strings.Index(line[q1+1:], "\""); q2 > 0 {
							ver = line[q1+1 : q1+1+q2]
							break
						}
					}
				}
			}
		}
		set("java", bin != "", ver)
	})

	// 6. Nginx
	runProbe(func() {
		bin := findToolFn("nginx")
		installed := bin != "" || statExistsFn("/usr/sbin/nginx") || statExistsFn("/usr/bin/nginx")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "-v").CombinedOutput(); err == nil {
				txt := strings.TrimSpace(string(out))
				if idx := strings.Index(txt, "nginx/"); idx >= 0 {
					ver = strings.Fields(txt[idx+6:])[0]
				}
			}
		}
		set("nginx", installed, ver)
	})

	// 7. PHP & PHP-FPM
	runProbe(func() {
		bin := findToolFn("php")
		installed := bin != "" || statExistsFn("/usr/sbin/php-fpm") || statExistsFn("/usr/sbin/php-fpm8.4")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;").CombinedOutput(); err == nil {
				v := strings.TrimSpace(string(out))
				if strings.Count(v, ".") == 1 && len(v) <= 6 {
					ver = v
				}
			}
		}
		set("php", installed, ver)
	})

	// 8. Composer
	runProbe(func() {
		bin := findToolFn("composer")
		installed := bin != "" || statExistsFn("/usr/local/bin/composer")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "--version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 3 && fields[0] == "Composer" {
					ver = fields[2]
				}
			}
		}
		set("composer", installed, ver)
	})

	// 9. Local SSL (mkcert)
	runProbe(func() {
		bin := findToolFn("mkcert")
		installed := bin != "" || statExistsFn("/usr/local/bin/mkcert")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "-version").CombinedOutput(); err == nil {
				ver = strings.TrimSpace(string(out))
			}
		}
		set("ssl", installed, ver)
	})

	// 10. PostgreSQL
	runProbe(func() {
		bin := findToolFn("psql", "postgres")
		installed := bin != "" || dirExistsSetupFn("/usr/lib/postgresql")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "--version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 3 {
					ver = strings.Split(fields[2], ".")[0]
				}
			}
		}
		set("postgres", installed, ver)
	})

	// 11. Redis / Valkey
	runProbe(func() {
		bin := findToolFn("redis-server", "valkey-server", "redis-cli", "valkey-cli")
		installed := bin != ""
		ver := ""
		if cli := findToolFn("redis-server", "valkey-server", "redis-cli", "valkey-cli"); cli != "" {
			if out, err := toolCmd(probeCtx, cli, "--version").CombinedOutput(); err == nil {
				for _, f := range strings.Fields(string(out)) {
					if strings.HasPrefix(f, "v=") {
						ver = strings.TrimPrefix(f, "v=")
						break
					}
				}
			}
		}
		set("redis", installed, ver)
	})

	// 12. Android SDK
	runProbe(func() {
		installed := dirExistsSetupFn("/usr/local/android-sdk") ||
			findToolFn("sdkmanager") != "" ||
			findToolFn("adb") != ""
		ver := ""
		if statExistsFn("/usr/local/android-sdk/platform-tools/adb") {
			ver = "SDK Ready"
		}
		set("android", installed, ver)
	})

	// 13. Flutter SDK
	runProbe(func() {
		bin := findToolFn("flutter")
		installed := bin != "" || dirExistsSetupFn("/usr/local/flutter")
		ver := ""
		if data, err := os.ReadFile("/usr/local/flutter/version"); err == nil {
			ver = strings.TrimSpace(string(data))
		}
		set("flutter", installed, ver)
	})

	// 14. Mail Server (Stalwart)
	runProbe(func() {
		bin := findToolFn("stalwart", "stalwart-cli")
		installed := bin != "" ||
			statExistsFn("/usr/local/bin/stalwart") ||
			statExistsFn("/etc/systemd/system/stalwart.service")
		set("mail", installed, "")
	})

	// 15. Cloudflare Tunnel
	runProbe(func() {
		bin := findToolFn("cloudflared")
		installed := bin != "" || statExistsFn("/usr/local/bin/cloudflared") || statExistsFn("/usr/bin/cloudflared")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "--version").CombinedOutput(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 3 {
					ver = fields[2]
				}
			}
		}
		set("cloudflare", installed, ver)
	})

	// 16. NetBird Mesh VPN
	runProbe(func() {
		bin := findToolFn("netbird")
		installed := bin != "" || statExistsFn("/usr/bin/netbird") || statExistsFn("/usr/local/bin/netbird")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "version").CombinedOutput(); err == nil {
				ver = strings.TrimSpace(string(out))
			}
		}
		set("netbird", installed, ver)
	})

	// 17. PowerDNS Authoritative (khusus instalasi daemon lokal, bukan remote Edge)
	runProbe(func() {
		bin := findToolFn("pdns_server")
		installed := bin != "" || statExistsFn("/usr/sbin/pdns_server") || statExistsFn("/etc/powerdns/pdns.conf")
		set("pdns", installed, "")
	})

	// 18. Oh My Zsh
	runProbe(func() {
		home := resolveSetupUserHomeFn()
		omzDir := ""
		if home != "" {
			omzDir = filepath.Join(home, ".oh-my-zsh")
		}
		installed := findToolFn("zsh") != "" && omzDir != "" && dirExistsSetupFn(omzDir)
		theme := ""
		if installed {
			theme = detectZshTheme(filepath.Join(home, ".zshrc"))
		}
		set("zsh", installed, theme)
	})

	// 19 & 20. Orca ADE (Desktop vs Headless)
	runProbe(func() {
		targetUser, userHome := resolveSetupOrcaHomeFn()
		installed := false
		for _, p := range []string{
			"/usr/local/lib/orca-ide/orca.AppImage",
			"/opt/Orca/resources/bin/orca-ide",
			"/opt/Orca/orca-ide",
			"/opt/orca-ide/resources/bin/orca-ide",
		} {
			if statExistsFn(p) {
				installed = true
				break
			}
		}
		if !installed && findOrcaServerBinary() != "" {
			installed = true
		}
		if !installed && userHome != "" {
			for _, p := range []string{
				filepath.Join(userHome, ".local", "lib", "orca-ide", "resources", "bin", "orca-ide"),
				filepath.Join(userHome, ".local", "lib", "orca-ide", "orca-ide"),
			} {
				if statExistsFn(p) {
					installed = true
					break
				}
			}
		}
		if !installed {
			if bin := findToolFn("orca-ide", "orca"); bin != "" && !isWdevBinaryLink(bin) {
				installed = true
			}
		}

		if !installed {
			set("orca-desktop", false, "")
			set("orca-headless", false, "")
			return
		}

		mode := "headless"
		if userHome != "" {
			if b, err := os.ReadFile(filepath.Join(userHome, ".config", "orca", ".mode")); err == nil {
				if m := strings.TrimSpace(string(b)); m != "" {
					mode = m
				}
			}
		}
		ver := detectOrcaVerFn(probeCtx, targetUser, userHome)
		set("orca-desktop", mode == "desktop", ver)
		set("orca-headless", mode != "desktop", ver)
	})

	// 21. Docker Engine & Compose v5
	runProbe(func() {
		bin := findToolFn("docker")
		installed := bin != "" || statExistsFn("/usr/bin/docker") || statExistsFn("/usr/local/bin/docker")
		ver := ""
		if bin != "" {
			if out, err := toolCmd(probeCtx, bin, "compose", "version", "--short").CombinedOutput(); err == nil {
				cVer := strings.TrimSpace(string(out))
				if cVer != "" {
					ver = "Compose v" + strings.TrimPrefix(cVer, "v")
				}
			}
			if ver == "" {
				if out, err := toolCmd(probeCtx, bin, "--version").CombinedOutput(); err == nil {
					fields := strings.Fields(string(out))
					if len(fields) >= 3 {
						ver = "v" + strings.TrimSuffix(strings.TrimPrefix(fields[2], "v"), ",")
					}
				}
			}
		}
		set("docker", installed, ver)
	})

	wg.Wait()

	// 21. Bundel PHP Web Stack
	phpStackInstalled := res["nginx"].Installed && res["php"].Installed && res["composer"].Installed && res["ssl"].Installed
	set("php-stack", phpStackInstalled, "")

	return res
}

func detectZshTheme(zshrcPath string) string {
	data, err := os.ReadFile(zshrcPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ZSH_THEME=") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "ZSH_THEME="))
			val = strings.Trim(val, `"'`)
			if strings.Contains(val, "powerlevel10k") {
				return "powerlevel10k"
			}
			return val
		}
	}
	return ""
}
