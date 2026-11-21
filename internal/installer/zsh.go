package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type nerdFontAsset struct {
	FileName string
	URL      string
}

var mesloLGSNerdFonts = []nerdFontAsset{
	{
		FileName: "MesloLGS NF Regular.ttf",
		URL:      "https://github.com/romkatv/powerlevel10k-media/raw/master/MesloLGS%20NF%20Regular.ttf",
	},
	{
		FileName: "MesloLGS NF Bold.ttf",
		URL:      "https://github.com/romkatv/powerlevel10k-media/raw/master/MesloLGS%20NF%20Bold.ttf",
	},
	{
		FileName: "MesloLGS NF Italic.ttf",
		URL:      "https://github.com/romkatv/powerlevel10k-media/raw/master/MesloLGS%20NF%20Italic.ttf",
	},
	{
		FileName: "MesloLGS NF Bold Italic.ttf",
		URL:      "https://github.com/romkatv/powerlevel10k-media/raw/master/MesloLGS%20NF%20Bold%20Italic.ttf",
	},
}

func isNerdFontFileName(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".ttf") && !strings.HasSuffix(lower, ".otf") {
		return false
	}
	return strings.Contains(lower, "meslolgs") ||
		strings.Contains(lower, "nerd") ||
		strings.Contains(lower, "nf ") ||
		strings.Contains(lower, "nf.")
}

// DetectNerdFont memeriksa apakah Nerd Font (mis. MesloLGS NF) sudah tersedia di direktori font user/sistem.
func DetectNerdFont(home string) string {
	var dirs []string
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"))
	}
	dirs = append(dirs, "/usr/local/share/fonts", "/usr/share/fonts")

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		found := ""
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			if isNerdFontFileName(d.Name()) {
				lower := strings.ToLower(d.Name())
				if strings.Contains(lower, "meslolgs") {
					found = "MesloLGS NF"
				} else {
					found = strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
				}
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// ensureMesloLGSNerdFont memasang MesloLGS NF (Nerd Font rekomendasi Powerlevel10k)
// ke ~/.local/share/fonts bila sistem belum memiliki Nerd Font, lalu memperbarui fc-cache.
func (inst *Installer) ensureMesloLGSNerdFont(ctx context.Context, cb LogCallback, user, home string) {
	if home == "" {
		return
	}
	if existing := DetectNerdFont(home); existing != "" {
		// Jika MesloLGS NF sudah ada di user dir, pastikan lengkap 4 varian; jika Nerd Font lain sudah terpasang di sistem, lewati.
		fontsDir := filepath.Join(home, ".local", "share", "fonts")
		if existing != "MesloLGS NF" {
			cb(fmt.Sprintf("✓ Nerd Font sudah terpasang (%s), melewati unduhan.", existing))
			return
		}
		allPresent := true
		for _, f := range mesloLGSNerdFonts {
			if fi, err := os.Stat(filepath.Join(fontsDir, f.FileName)); err != nil || fi.Size() == 0 {
				allPresent = false
				break
			}
		}
		if allPresent {
			cb("✓ Nerd Font MesloLGS NF sudah lengkap terpasang.")
			return
		}
	}

	fontsDir := filepath.Join(home, ".local", "share", "fonts")
	if err := os.MkdirAll(fontsDir, 0o755); err != nil {
		cb(fmt.Sprintf("[WARN] Gagal membuat direktori font %s: %v", fontsDir, err))
		return
	}

	cb("==> Mengunduh & memasang Nerd Font (MesloLGS NF untuk Powerlevel10k)...")
	downloaded := 0
	for _, f := range mesloLGSNerdFonts {
		dst := filepath.Join(fontsDir, f.FileName)
		if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
			continue
		}
		tmp := dst + ".tmp"
		if err := downloadFileCtx(ctx, f.URL, tmp); err != nil {
			_ = os.Remove(tmp)
			cb(fmt.Sprintf("[WARN] Gagal mengunduh %s: %v", f.FileName, err))
			continue
		}
		if err := os.Chmod(tmp, 0o644); err != nil {
			_ = os.Remove(tmp)
			continue
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			cb(fmt.Sprintf("[WARN] Gagal menyimpan %s: %v", f.FileName, err))
			continue
		}
		downloaded++
	}

	chownPath(user, fontsDir)
	if downloaded > 0 {
		if fcBin, err := exec.LookPath("fc-cache"); err == nil {
			_ = runAsUser(ctx, cb, user, home, fcBin, "-f", fontsDir)
		}
		cb(fmt.Sprintf("✓ Nerd Font MesloLGS NF (%d file) berhasil dipasang di %s.", downloaded, fontsDir))
	}
}

func resolveZshThemeValue(theme string) string {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "", "powerlevel10k", "p10k", "powerlevel10k/powerlevel10k":
		return "powerlevel10k/powerlevel10k"
	case "agnoster":
		return "agnoster"
	case "robbyrussell":
		return "robbyrussell"
	default:
		// Tolak nilai kustom bebas agar tidak terjadi injeksi shell ke ~/.zshrc
		return "powerlevel10k/powerlevel10k"
	}
}

func defaultZshrcContent(theme string) string {
	themeVal := resolveZshThemeValue(theme)
	instantPrompt := ""
	p10kSource := ""
	if themeVal == "powerlevel10k/powerlevel10k" {
		instantPrompt = `# Enable Powerlevel10k instant prompt. Should stay close to the top of ~/.zshrc.
# Initialization code that may require console input (password prompts, [y/n]
# confirmations, etc.) must go above this block; everything else may go below.
if [[ -r "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh" ]]; then
  source "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh"
fi

`
		p10kSource = `
# To customize prompt, run ` + "`p10k configure`" + ` or edit ~/.p10k.zsh.
[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh
`
	}

	return fmt.Sprintf(`# --- OH MY ZSH CONFIGURATION ---
export ZSH="$HOME/.oh-my-zsh"

%s# Tema
ZSH_THEME="%s"

# Hindari output konsol saat inisialisasi Zsh (mencegah warning P10k instant prompt)
ZSH_DISABLE_COMPFIX="true"
zstyle ':omz:update' mode disabled

# Tambahkan fpath completions sebelum oh-my-zsh dimuat
fpath+=($ZSH/custom/plugins/zsh-completions/src)

# Plugin Populer
plugins=(
  git
  sudo
  zsh-autosuggestions
  zsh-syntax-highlighting
  zsh-completions
  colored-man-pages
  docker
  npm
)

source $ZSH/oh-my-zsh.sh

# --- SINKRONISASI ENVIRONMENT DARI ~/.bashrc & ~/.profile ---
# Muat environment variable secara senyap agar tidak memicu output konsol pada instant prompt
if [ -f "$HOME/.bashrc" ]; then
  emulate sh -c 'source "$HOME/.bashrc"' >/dev/null 2>&1 || true
fi
if [ -f "$HOME/.profile" ] && [ ! -f "$HOME/.bashrc" ]; then
  emulate sh -c 'source "$HOME/.profile"' >/dev/null 2>&1 || true
fi

# Otomatis hapus duplikasi pada PATH (fitur bawaan Zsh)
typeset -U PATH path

# Alias Bermanfaat
alias ll='ls -lah'
alias la='ls -A'
alias l='ls -CF'
alias cls='clear'
alias devsite='dev-site'
alias phpswitch='php-switch'
alias pgswitch='pg-switch'
%s`, instantPrompt, themeVal, p10kSource)
}

// reconcileZshrc memastikan ~/.zshrc yang sudah ada bebas dari sisa loop yatim,
// mengatur ZSH_THEME sesuai pilihan, dan mencegah output konsol saat inisialisasi
// (ZSH_DISABLE_COMPFIX="true" & zstyle ':omz:update' mode disabled) sehingga
// Powerlevel10k instant prompt tidak memunculkan warning saat install ulang.
func reconcileZshrc(existing, theme string) string {
	if strings.TrimSpace(existing) == "" {
		return defaultZshrcContent(theme)
	}

	themeVal := resolveZshThemeValue(theme)
	lines := strings.Split(strings.TrimRight(existing, "\n"), "\n")
	lines = stripLegacyPathLines(lines)

	hasDisableCompfix := false
	hasOmzUpdateDisabled := false
	hasTheme := false
	sourceOmzIdx := -1

	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "ZSH_THEME=") {
			lines[i] = fmt.Sprintf(`ZSH_THEME="%s"`, themeVal)
			hasTheme = true
		}
		if strings.HasPrefix(trimmed, "ZSH_DISABLE_COMPFIX=") {
			lines[i] = `ZSH_DISABLE_COMPFIX="true"`
			hasDisableCompfix = true
		}
		if strings.Contains(trimmed, "zstyle ':omz:update' mode") {
			lines[i] = `zstyle ':omz:update' mode disabled`
			hasOmzUpdateDisabled = true
		}
		if sourceOmzIdx < 0 && strings.Contains(trimmed, "oh-my-zsh.sh") && strings.Contains(trimmed, "source") {
			sourceOmzIdx = i
		}
	}

	var preOmzAdditions []string
	if !hasTheme {
		preOmzAdditions = append(preOmzAdditions, fmt.Sprintf(`ZSH_THEME="%s"`, themeVal))
	}
	if !hasDisableCompfix {
		preOmzAdditions = append(preOmzAdditions, `ZSH_DISABLE_COMPFIX="true"`)
	}
	if !hasOmzUpdateDisabled {
		preOmzAdditions = append(preOmzAdditions, `zstyle ':omz:update' mode disabled`)
	}

	if len(preOmzAdditions) > 0 {
		if sourceOmzIdx >= 0 {
			updated := make([]string, 0, len(lines)+len(preOmzAdditions))
			updated = append(updated, lines[:sourceOmzIdx]...)
			updated = append(updated, preOmzAdditions...)
			updated = append(updated, lines[sourceOmzIdx:]...)
			lines = updated
		} else {
			lines = append(preOmzAdditions, lines...)
		}
	}

	result := strings.Join(lines, "\n") + "\n"
	if themeVal == "powerlevel10k/powerlevel10k" && !strings.Contains(result, "p10k-instant-prompt-") {
		preamble := `# Enable Powerlevel10k instant prompt. Should stay close to the top of ~/.zshrc.
if [[ -r "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh" ]]; then
  source "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh"
fi

`
		result = preamble + result
	}
	return result
}
