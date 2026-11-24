package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectZshTheme(t *testing.T) {
	tmp := t.TempDir()
	rc := filepath.Join(tmp, ".zshrc")
	content := `# comment
export ZSH="$HOME/.oh-my-zsh"
ZSH_THEME="powerlevel10k/powerlevel10k"
plugins=(git)
`
	if err := os.WriteFile(rc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if got := detectZshTheme(rc); got != "powerlevel10k" {
		t.Fatalf("detectZshTheme = %q, want powerlevel10k", got)
	}
}

func TestGetComponentStatusesReportsInstalledAndBundles(t *testing.T) {
	tmp := t.TempDir()
	omzDir := filepath.Join(tmp, ".oh-my-zsh")
	if err := os.MkdirAll(omzDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".zshrc"), []byte("ZSH_THEME=\"agnoster\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	orcaCfg := filepath.Join(tmp, ".config", "orca")
	if err := os.MkdirAll(orcaCfg, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orcaCfg, ".mode"), []byte("desktop\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prevFind := findToolFn
	prevHome := resolveSetupUserHomeFn
	prevOrcaHome := resolveSetupOrcaHomeFn
	prevStat := statExistsFn
	prevDir := dirExistsSetupFn
	prevOrcaVer := detectOrcaVerFn
	defer func() {
		findToolFn = prevFind
		resolveSetupUserHomeFn = prevHome
		resolveSetupOrcaHomeFn = prevOrcaHome
		statExistsFn = prevStat
		dirExistsSetupFn = prevDir
		detectOrcaVerFn = prevOrcaVer
	}()

	findToolFn = func(names ...string) string {
		for _, n := range names {
			switch n {
			case "zsh", "orca", "nginx", "php", "composer", "mkcert":
				return "/bin/true"
			}
		}
		return ""
	}
	resolveSetupUserHomeFn = func() string { return tmp }
	resolveSetupOrcaHomeFn = func() (string, string) { return "testuser", tmp }
	statExistsFn = func(p string) bool { return false }
	dirExistsSetupFn = func(p string) bool { return p == omzDir }
	detectOrcaVerFn = func(ctx context.Context, user, home string) string { return "1.2.3" }

	inst := &Installer{}
	statuses := inst.GetComponentStatuses(context.Background())

	if !statuses["zsh"].Installed || statuses["zsh"].Version != "agnoster" {
		t.Fatalf("unexpected zsh status: %+v", statuses["zsh"])
	}
	if !statuses["orca-desktop"].Installed || statuses["orca-desktop"].Version != "1.2.3" {
		t.Fatalf("unexpected orca-desktop status: %+v", statuses["orca-desktop"])
	}
	if statuses["orca-headless"].Installed {
		t.Fatalf("expected orca-headless false in desktop mode")
	}
	if !statuses["php-stack"].Installed {
		t.Fatalf("expected php-stack true when nginx, php, composer, ssl are all present")
	}
	if statuses["pdns"].Installed {
		t.Fatalf("expected local pdns false when pdns_server binary absent")
	}
	if statuses["docker"].Installed {
		t.Fatalf("expected docker false when docker binary absent")
	}
}
