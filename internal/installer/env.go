package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// envProfilePath adalah satu-satunya file environment yang ditulis wdev.
const envProfilePath = "/etc/profile.d/wdev.sh"

// legacyProfileFiles adalah file PATH lama yang harus dihapus agar tidak dobel.
var legacyProfileFiles = []string{
	"/etc/profile.d/flutter.sh",
	"/etc/profile.d/golang.sh",
	"/etc/profile.d/rust.sh",
	"/etc/profile.d/node.sh",
	"/etc/profile.d/nodejs.sh",
	"/etc/profile.d/java.sh",
	"/etc/profile.d/android.sh",
	"/etc/profile.d/orca-browser.sh",
	"/etc/profile.d/mkcert.sh",
}

// toolLinkSpec memetakan symlink /usr/local/bin ke area kanonik milik wdev.
// Validasi memakai prefix direktori agar versi Node yang aktif (multi-versi)
// tetap sah tanpa harus menunjuk nama folder tertentu.
type toolLinkSpec struct {
	link     string
	prefixes []string // target symlink harus berada di salah satu prefix ini
}

var toolLinkSpecs = []toolLinkSpec{
	{link: "go", prefixes: []string{"/usr/local/go/bin/"}},
	{link: "gofmt", prefixes: []string{"/usr/local/go/bin/"}},
	{link: "node", prefixes: []string{"/usr/local/lib/nodejs/"}},
	{link: "npm", prefixes: []string{"/usr/local/lib/nodejs/"}},
	{link: "npx", prefixes: []string{"/usr/local/lib/nodejs/"}},
	{link: "corepack", prefixes: []string{"/usr/local/lib/nodejs/"}},
	{link: "flutter", prefixes: []string{"/usr/local/flutter/bin/"}},
	{link: "dart", prefixes: []string{"/usr/local/flutter/bin/"}},
	{link: "rustup", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "cargo", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "cargo-clippy", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "cargo-fmt", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "cargo-miri", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "clippy-driver", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rls", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rust-analyzer", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rust-gdb", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rust-gdbgui", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rust-lldb", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rustc", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rustdoc", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "rustfmt", prefixes: []string{"/usr/local/cargo/bin/"}},
	{link: "mold", prefixes: []string{"/usr/local/mold/", "/usr/bin/"}},
}

// canonicalTarget memetakan link -> sumber kanonik di area wdev.
var canonicalMap = map[string]string{
	"go":    "/usr/local/go/bin/go",
	"gofmt": "/usr/local/go/bin/gofmt",
	"mold":  "/usr/bin/mold",
}

func (s toolLinkSpec) allowed(target string) bool {
	for _, p := range s.prefixes {
		if target == p || strings.HasPrefix(target, p) {
			return true
		}
	}
	return false
}

// canonicalTarget mengembalikan target symlink terbaik untuk sebuah tool.
func (s toolLinkSpec) canonicalTarget() string {
	if direct, ok := canonicalMap[s.link]; ok {
		if fileExistsAt(direct) {
			return direct
		}
		return ""
	}
	for _, p := range s.prefixes {
		candidate := filepath.Join(p, s.link)
		if fileExistsAt(candidate) {
			return candidate
		}
		if strings.Contains(p, "/nodejs/") {
			if cur := filepath.Join("/usr/local/lib/nodejs/current/bin", s.link); fileExistsAt(cur) {
				return cur
			}
			matches, _ := filepath.Glob(filepath.Join("/usr/local/lib/nodejs/node-*", "bin", s.link))
			if len(matches) > 0 {
				return matches[len(matches)-1]
			}
		}
		if strings.Contains(p, "/cargo/") {
			matches, _ := filepath.Glob(filepath.Join("/usr/local", "cargo", "bin", s.link))
			if len(matches) > 0 {
				return matches[0]
			}
		}
	}
	return ""
}

func fileExistsAt(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// profileScript menulis PATH tunggal. Semua entri menunjuk direktori kanonik wdev.
func profileScript() string {
	return `# Managed by wdev — SATU-SATUNYA sumber environment toolchain.
# File ini dibuat ulang otomatis oleh: wdev env sync
# Jangan tambahkan file /etc/profile.d/*.sh lain untuk tool yang sama.
export GOROOT="/usr/local/go"
export RUSTUP_HOME="/usr/local/rustup"
export FLUTTER_ROOT="/usr/local/flutter"
export ANDROID_HOME="/usr/local/android-sdk"
export ANDROID_SDK_ROOT="/usr/local/android-sdk"
export NODE_HOME="/usr/local/lib/nodejs/current"
if [ "$(id -u)" -eq 0 ]; then
	export CARGO_HOME="/usr/local/cargo"
	export CAROOT="/etc/ssl/local-certs/ca"
elif [ -n "${HOME:-}" ]; then
	if [ -z "${CARGO_HOME:-}" ] || [ "${CARGO_HOME}" = "/usr/local/cargo" ]; then
		export CARGO_HOME="${HOME}/.cargo"
	fi
	if [ -z "${NPM_CONFIG_PREFIX:-}" ] && [ -z "${npm_config_prefix:-}" ]; then
		export npm_config_prefix="${HOME}/.npm-global"
	fi
	if [ "${CAROOT:-}" = "/etc/ssl/local-certs/ca" ]; then
		unset CAROOT
	fi
fi

# Browser automation Orca ADE (satu-satunya definisi).
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
	for _wdev_b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge /snap/bin/chromium; do
		if [ -x "$_wdev_b" ]; then
			export ORCA_BROWSER_EXECUTABLE="$_wdev_b"
			break
		fi
	done
	unset _wdev_b
fi

_wdev_prepend_path() {
	case ":$PATH:" in
	*":$1:"*) ;;
	*) [ -d "$1" ] && PATH="$1${PATH:+:$PATH}" ;;
	esac
}

_wdev_append_path() {
	case ":$PATH:" in
	*":$1:"*) ;;
	*) [ -d "$1" ] && PATH="${PATH:+$PATH:}$1" ;;
	esac
}

_wdev_prepend_path "/usr/local/bin"
_wdev_prepend_path "/usr/local/go/bin"
_wdev_prepend_path "/usr/local/cargo/bin"
_wdev_prepend_path "/usr/local/flutter/bin"
_wdev_prepend_path "/usr/local/android-sdk/platform-tools"
_wdev_prepend_path "/usr/local/android-sdk/cmdline-tools/latest/bin"
_wdev_prepend_path "/usr/local/lib/nodejs/current/bin"
if [ "$(id -u)" -ne 0 ] && [ -n "${HOME:-}" ]; then
	_wdev_append_path "${HOME}/.cargo/bin"
	_wdev_append_path "${HOME}/.npm-global/bin"
fi
if [ -n "${HOME:-}" ]; then
	_wdev_append_path "${GOPATH:-${HOME}/go}/bin"
fi
export PATH
unset -f _wdev_prepend_path _wdev_append_path
`
}

// SyncToolchainEnv membangun ulang environment PATH secara idempoten:
//  1. tulis /etc/profile.d/wdev.sh (satu-satunya sumber)
//  2. hapus file profile lama yang membuat PATH bercabang
//  3. arahkan ulang symlink /usr/local/bin ke sumber kanonik wdev
func (inst *Installer) SyncToolchainEnv(ctx context.Context, cb LogCallback) error {
	if cb == nil {
		cb = func(string) {}
	}
	cb("==> Menyinkronkan environment toolchain (satu arah dari wdev)...")

	if err := inst.writeSystemFile(ctx, cb, envProfilePath, []byte(profileScript()), "644"); err != nil {
		return fmt.Errorf("gagal menulis %s: %w", envProfilePath, err)
	}
	cb(" ✓ Ditulis: " + envProfilePath)

	removed := 0
	for _, p := range legacyProfileFiles {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := inst.runCmd(ctx, cb, "rm", "-f", p); err == nil {
			cb(" ✓ Dihapus (legacy): " + p)
			removed++
		}
	}
	if removed == 0 {
		cb(" ✓ Tidak ada file profile legacy.")
	}

	fixed := 0
	for _, spec := range toolLinkSpecs {
		linkPath := filepath.Join("/usr/local/bin", spec.link)
		current, err := os.Readlink(linkPath)
		if err == nil && spec.allowed(current) {
			continue
		}
		target := spec.canonicalTarget()
		if target == "" {
			continue
		}
		_ = inst.runCmd(ctx, cb, "ln", "-sfn", target, linkPath)
		if err == nil {
			cb(fmt.Sprintf(" ✓ Diperbaiki: %s -> %s (sebelumnya %s)", linkPath, target, current))
		} else {
			cb(fmt.Sprintf(" ✓ Diselaraskan: %s -> %s", linkPath, target))
		}
		fixed++
	}
	if fixed == 0 {
		cb(" ✓ Semua symlink tool sudah menunjuk area kanonik wdev.")
	}

	user, home := ResolveOrcaUser()
	if changed, err := SyncUserShellEnv(user, home); err != nil {
		cb(fmt.Sprintf("[WARN] Gagal menyelaraskan rc shell user: %v", err))
	} else if len(changed) > 0 {
		cb(fmt.Sprintf(" ✓ RC shell diselaraskan: %s", strings.Join(changed, ", ")))
	} else {
		cb(" ✓ RC shell user sudah memuat blok wdev.")
	}

	if rootChanged, err := syncRootShellEnv(); err != nil {
		cb(fmt.Sprintf("[WARN] Gagal menyelaraskan rc root: %v", err))
	} else if len(rootChanged) > 0 {
		cb(fmt.Sprintf(" ✓ RC root diselaraskan: %s", strings.Join(rootChanged, ", ")))
	}

	cb("✓ Environment toolchain sinkron (satu sumber: " + envProfilePath + ").")
	return nil
}

// ToolchainEnvReport mengembalikan ringkasan kondisi PATH tanpa mengubah apa pun.
func ToolchainEnvReport() []string {
	var lines []string
	if _, err := os.Stat(envProfilePath); err == nil {
		lines = append(lines, " [OK] profile   : "+envProfilePath)
	} else {
		lines = append(lines, " [--] profile   : "+envProfilePath+" belum ada (jalankan: wdev env sync)")
	}

	var legacy []string
	for _, p := range legacyProfileFiles {
		if _, err := os.Stat(p); err == nil {
			legacy = append(legacy, p)
		}
	}
	if len(legacy) > 0 {
		lines = append(lines, " [!] legacy    : "+strings.Join(legacy, ", "))
	} else {
		lines = append(lines, " [OK] legacy    : tidak ada file profile duplikat")
	}

	var drift []string
	for _, spec := range toolLinkSpecs {
		linkPath := filepath.Join("/usr/local/bin", spec.link)
		current, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}
		if !spec.allowed(current) {
			drift = append(drift, spec.link+" -> "+current)
		}
	}
	if len(drift) > 0 {
		lines = append(lines, " [!] drift     : "+strings.Join(drift, ", "))
	} else {
		lines = append(lines, " [OK] symlink   : konsisten dengan sumber kanonik")
	}

	user, home := ResolveOrcaUser()
	lines = append(lines, ShellEnvReport(user, home)...)
	return lines
}

// EffectivePathPreview mengembalikan PATH hasil login shell untuk verifikasi.
func EffectivePathPreview(ctx context.Context) []string {
	cmd := exec.CommandContext(ctx, "bash", "-lc", "echo $PATH")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var entries []string
	for _, p := range strings.Split(strings.TrimSpace(string(out)), ":") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		entries = append(entries, p)
	}
	return entries
}
