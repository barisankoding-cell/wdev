package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	managedBegin = "# >>> wdev managed block >>>"
	managedEnd   = "# <<< wdev managed block <<<"
)

// managedBlock adalah satu-satunya isi environment di rc file user.
// Semua definisi PATH/CA/browser tetap berasal dari /etc/profile.d/wdev.sh.
func managedBlock() string {
	return managedBegin + "\n" +
		"# Dikelola otomatis oleh wdev (wdev env sync). Jangan diedit manual.\n" +
		"if [ -r /etc/profile.d/wdev.sh ]; then\n" +
		"    . /etc/profile.d/wdev.sh\n" +
		"fi\n" +
		managedEnd + "\n"
}

// stripManagedBlock membuang blok wdev lama dari isi file.
func stripManagedBlock(lines []string) []string {
	var out []string
	skipping := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		switch {
		case trimmed == managedBegin:
			skipping = true
			continue
		case trimmed == managedEnd:
			skipping = false
			continue
		}
		if skipping {
			continue
		}
		out = append(out, l)
	}
	return out
}

// stripOrcaBrowserBlock menghapus blok ORCA_BROWSER_EXECUTABLE lama (if..fi).
func stripOrcaBrowserBlock(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		trimmed := strings.TrimSpace(l)
		isOrcaComment := trimmed == "# Orca ADE Browser Automation Backend"
		if !isOrcaComment && !strings.Contains(l, "ORCA_BROWSER_EXECUTABLE") {
			out = append(out, l)
			continue
		}
		// Lewati sampai 'fi' penutup blok if.
		depth := 0
		started := false
		for ; i < len(lines); i++ {
			t := strings.TrimSpace(lines[i])
			if strings.HasPrefix(t, "if ") || strings.HasPrefix(t, "if[") {
				depth++
				started = true
			}
			if started && t == "fi" {
				depth--
				if depth <= 0 {
					break
				}
			}
			if !started && i > 0 && strings.Contains(t, "unset _b") {
				break
			}
		}
	}
	return out
}

// stripLegacyPathLines menghapus export PATH manual dan loop profile.d lama.
func stripLegacyPathLines(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		trimmed := strings.TrimSpace(l)
		if trimmed == "# --- DEV ENVIRONMENT & TOOLCHAIN PATHS ---" {
			continue
		}
		if strings.HasPrefix(trimmed, `export PATH="$HOME/.local/bin:$HOME/bin:/usr/local/bin`) ||
			strings.Contains(trimmed, "/usr/local/cargo/bin:/usr/local/go/bin") {
			continue
		}
		// Sumber PATH eksternal (rustup per-user ~/.cargo/env) -> digantikan wdev.sh.
		if strings.Contains(trimmed, ".cargo/env") || strings.Contains(trimmed, ".cargo\\env") {
			continue
		}
		// Cek loop `for prof in /etc/profile.d/...` SEBELUM filter satu-baris profile.d/*.sh
		// agar body loop dan `done` tidak tertinggal yatim di file rc.
		if strings.HasPrefix(trimmed, "for prof in /etc/profile.d/") {
			for ; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "done" {
					break
				}
			}
			continue
		}
		// Bersihkan sisa loop yatim akibat versi wdev lama: `[ -r "$prof" ] && . "$prof"` + `done`.
		if strings.Contains(trimmed, `"$prof"`) && strings.Contains(trimmed, "[ -r") {
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "done" {
				i++
			}
			continue
		}
		// Referensi ke file profile.d yang sudah dihapus wdev.
		if strings.Contains(trimmed, "/etc/profile.d/") &&
			(strings.Contains(trimmed, "nodejs.sh") || strings.Contains(trimmed, "rust.sh") ||
				strings.Contains(trimmed, "golang.sh") || strings.Contains(trimmed, "flutter.sh") ||
				strings.Contains(trimmed, "orca-browser.sh") || strings.Contains(trimmed, "mkcert.sh")) {
			continue
		}
		if trimmed == "# Muat environment profile dev" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// dedupeExportPath membuang baris export PATH yang menambahkan direktori sama
// berulang kali (mis. $HOME/.local/bin dari beberapa installer berbeda).
func dedupeExportPath(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "export PATH=") {
			if isLocalBinExport(trimmed) {
				if seen["localbin"] {
					continue
				}
				seen["localbin"] = true
			}
			if seen[trimmed] {
				continue
			}
			seen[trimmed] = true
		}
		out = append(out, l)
	}
	return out
}

// isLocalBinExport mendeteksi export PATH yang hanya menambahkan ~/.local/bin
// (baik ditulis sebagai $HOME/.local/bin maupun /home/<user>/.local/bin).
func isLocalBinExport(line string) bool {
	line = strings.TrimSuffix(strings.TrimSpace(line), ";")
	if !strings.HasPrefix(line, "export PATH=") || !strings.HasSuffix(line, `:$PATH"`) {
		return false
	}
	prefix := strings.TrimSuffix(strings.TrimPrefix(line, "export PATH="), `:$PATH"`)
	prefix = strings.Trim(prefix, `"`)
	return strings.HasSuffix(prefix, "/.local/bin")
}

// appendManagedBlock menaruh blok wdev di akhir file.
func appendManagedBlock(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, "")
	lines = append(lines, strings.Split(strings.TrimRight(managedBlock(), "\n"), "\n")...)
	return append(lines, "")
}

// syncRootShellEnv ikut merapikan rc root karena wdev.service berjalan sebagai root.
func syncRootShellEnv() ([]string, error) {
	if os.Geteuid() != 0 {
		return nil, nil
	}
	return SyncUserShellEnv("root", "/root")
}

// SyncUserShellEnv menyatukan path environment pada rc shell user.
func SyncUserShellEnv(user, home string) ([]string, error) {
	if home == "" {
		return nil, fmt.Errorf("home direktori user tidak diketahui")
	}
	rcFiles := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
	}

	bashrcExists := false
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		bashrcExists = true
	}

	var changed []string
	for _, path := range rcFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		lines = stripManagedBlock(lines)
		lines = stripOrcaBrowserBlock(lines)
		lines = stripLegacyPathLines(lines)
		lines = dedupeExportPath(lines)
		// .profile sudah memuat .bashrc; blok wdev cukup sekali.
		if filepath.Base(path) == ".profile" && bashrcExists {
			// .profile sudah memuat .bashrc: buang PATH lokal duplikat.
			var filtered []string
			for _, l := range lines {
				if isLocalBinExport(l) {
					continue
				}
				filtered = append(filtered, l)
			}
			lines = filtered
		} else {
			lines = appendManagedBlock(lines)
		}

		updated := strings.Join(lines, "\n") + "\n"
		if updated == string(data) {
			continue
		}
		info, err := os.Stat(path)
		mode := os.FileMode(0644)
		if err == nil {
			mode = info.Mode()
		}
		if err := os.WriteFile(path, []byte(updated), mode); err != nil {
			return changed, err
		}
		chownPath(user, path)
		changed = append(changed, path)
	}
	return changed, nil
}

// ShellEnvReport melaporkan kondisi rc shell user (tanpa mengubah apa pun).
func ShellEnvReport(user, home string) []string {
	var lines []string
	for _, name := range []string{".bashrc", ".zshrc", ".profile"} {
		path := filepath.Join(home, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		state := " [OK] "
		note := "memuat wdev.sh"
		if !strings.Contains(content, managedBegin) {
			if name == ".profile" {
				// .profile memang tidak butuh blok bila sudah memuat .bashrc.
				note = "memuat .bashrc (tidak perlu blok terpisah)"
			} else {
				state = " [!]  "
				note = "belum ada blok wdev"
			}
		}
		if strings.Contains(content, "ORCA_BROWSER_EXECUTABLE") {
			state = " [!]  "
			note = "masih ada blok ORCA_BROWSER lama"
		}
		lines = append(lines, fmt.Sprintf("%s%-10s: %s", state, name, note))
	}
	return lines
}
