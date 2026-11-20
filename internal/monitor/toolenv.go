package monitor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// toolchainDirs direktori kanonik wdev, diprioritaskan di depan PATH.
var toolchainDirs = []string{
	"/usr/local/go/bin",
	"/usr/local/cargo/bin",
	"/usr/local/flutter/bin",
	"/usr/local/lib/nodejs/current/bin",
	"/usr/local/android-sdk/platform-tools",
	"/usr/local/android-sdk/cmdline-tools/latest/bin",
	"/usr/local/bin",
}

// toolchainEnvDefaults variabel yang wajib ada agar rustup/go/flutter dikenali.
var toolchainEnvDefaults = [][2]string{
	{"GOROOT", "/usr/local/go"},
	{"CARGO_HOME", "/usr/local/cargo"},
	{"RUSTUP_HOME", "/usr/local/rustup"},
	{"FLUTTER_ROOT", "/usr/local/flutter"},
	{"ANDROID_HOME", "/usr/local/android-sdk"},
	{"ANDROID_SDK_ROOT", "/usr/local/android-sdk"},
	{"NODE_HOME", "/usr/local/lib/nodejs/current"},
}

// toolEnv menyusun environment untuk eksekusi toolchain.
// Ini penting karena wdev.service berjalan tanpa login shell, sehingga variabel
// dari /etc/profile.d/wdev.sh (RUSTUP_HOME dll.) tidak tersedia.
func toolEnv() []string {
	env := os.Environ()

	have := func(key string) bool {
		prefix := key + "="
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}

	for _, kv := range toolchainEnvDefaults {
		if !have(kv[0]) {
			env = append(env, kv[0]+"="+kv[1])
		}
	}

	// Pastikan direktori kanonik ada di PATH (tanpa duplikasi).
	pathVal := os.Getenv("PATH")
	parts := strings.Split(pathVal, ":")
	seen := map[string]bool{}
	for _, p := range parts {
		if p != "" {
			seen[p] = true
		}
	}
	var prepend []string
	for _, d := range toolchainDirs {
		if !seen[d] {
			prepend = append(prepend, d)
			seen[d] = true
		}
	}
	if len(prepend) > 0 {
		joined := strings.Join(append(prepend, parts...), ":")
		env = append(env, "PATH="+joined)
	}
	return env
}

// toolPath mencari executable di direktori kanonik lalu PATH.
func toolPath(name string) string {
	for _, d := range toolchainDirs {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// runToolVersion menjalankan tool dengan environment toolchain lengkap.
func runToolVersion(name string, args ...string) (string, error) {
	bin := toolPath(name)
	if bin == "" {
		return "", exec.ErrNotFound
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = toolEnv()
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
