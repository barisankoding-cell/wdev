package installer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"wdev/internal/config"
)

// OrcaPairInfo mengambil tautan pairing dari CLI resmi Orca (tanpa skrip Node/Bash).
func OrcaPairInfo(ctx context.Context, home string) (pairingURL, webClientURL, endpoint string) {
	cli := orcaRealCLI(home)
	if cli == "" {
		return "", "", ""
	}
	cmd := exec.CommandContext(ctx, cli, "pair", "--json")
	cmd.Env = orcaEnv(home)
	out, err := cmd.Output()
	if err != nil {
		return "", "", ""
	}
	var payload struct {
		PairingURL   string `json:"pairingUrl"`
		WebClientURL string `json:"webClientUrl"`
		Endpoint     string `json:"endpoint"`
		Result       *struct {
			PairingURL   string `json:"pairingUrl"`
			WebClientURL string `json:"webClientUrl"`
			Endpoint     string `json:"endpoint"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", "", ""
	}
	if payload.Result != nil {
		return payload.Result.PairingURL, payload.Result.WebClientURL, payload.Result.Endpoint
	}
	return payload.PairingURL, payload.WebClientURL, payload.Endpoint
}

func orcaEnv(home string) []string {
	env := os.Environ()
	if home != "" {
		env = append(env, "HOME="+home)
	}
	if browser := detectOrcaBrowser(); browser != "" && os.Getenv("ORCA_BROWSER_EXECUTABLE") == "" {
		env = append(env, "ORCA_BROWSER_EXECUTABLE="+browser)
	}
	return env
}

func execOrcaReal(home string, args []string) int {
	cli := orcaRealCLI(home)
	if cli == "" {
		fmt.Fprintln(os.Stderr, "Orca ADE belum terpasang. Jalankan: wdev install orca-headless")
		return 1
	}
	argv := append([]string{cli}, args...)
	if err := syscall.Exec(cli, argv, orcaEnv(home)); err != nil {
		fmt.Fprintf(os.Stderr, "Gagal menjalankan Orca: %v\n", err)
		return 1
	}
	return 0
}

func runOrcaPair(ctx context.Context, home string, args []string) int {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Println("Penggunaan: orca pair [--json] [--mobile] [--rotate]")
			return 0
		}
	}
	cli := orcaRealCLI(home)
	if cli == "" {
		fmt.Fprintln(os.Stderr, "Orca ADE belum terpasang.")
		return 1
	}
	cmd := exec.CommandContext(ctx, cli, append([]string{"pair"}, args...)...)
	cmd.Env = orcaEnv(home)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

func orcaAutostartPath(home string) string {
	return filepath.Join(home, ".config", "autostart", "orca-ide.desktop")
}

func setOrcaAutostart(user, home string, enable bool) error {
	if !enable {
		_ = os.Remove(orcaAutostartPath(home))
		return nil
	}
	ensureOrcaDesktopEntry(user, home, "")
	src := filepath.Join(home, ".local", "share", "applications", "orca-ide.desktop")
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dst := orcaAutostartPath(home)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	chownPath(user, dst)
	return nil
}

func orcaServiceActive(ctx context.Context, user, home string) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "is-active", "--quiet", "orca.service")
	cmd.Env = orcaEnv(home)
	if os.Geteuid() == 0 && user != "" && user != "root" {
		cmd = exec.CommandContext(ctx, "runuser", "-u", user, "--", "systemctl", "--user", "is-active", "--quiet", "orca.service")
		cmd.Env = orcaEnv(home)
	}
	return cmd.Run() == nil
}

// PrintOrcaStatus menampilkan status mode, layanan, versi, dan pairing.
func PrintOrcaStatus(ctx context.Context, home string) {
	user, _ := ResolveOrcaUser()
	mode := readOrcaMode(home)
	label := "HEADLESS SERVER"
	if mode == "desktop" {
		label = "DESKTOP GUI"
	}
	fmt.Println("==================================================")
	fmt.Println(" STATUS OPERASI ORCA ADE")
	fmt.Println("==================================================")
	fmt.Printf("Mode Terkonfigurasi : %s\n", label)
	fmt.Printf("Versi Terpasang     : %s\n", orcaVersionOrUnknown(orcaBinaryVersion(ctx, home, orcaInstallDir(home))))
	if orcaServiceActive(ctx, user, home) {
		fmt.Println("Status Headless Svc : RUNNING (systemd user orca.service)")
	} else {
		fmt.Println("Status Headless Svc : INACTIVE")
	}
	if fi, err := os.Stat(orcaAutostartPath(home)); err == nil && !fi.IsDir() {
		fmt.Println("Desktop Autostart   : Aktif")
	} else {
		fmt.Println("Desktop Autostart   : Nonaktif")
	}
	fmt.Println("==================================================")
	if mode == "headless" {
		if pairing, web, endpoint := OrcaPairInfo(ctx, home); pairing != "" {
			fmt.Println("\nPAIRING:")
			fmt.Printf("Pairing URL : %s\n", pairing)
			if web != "" {
				fmt.Printf("Web Client  : %s\n", web)
			}
			if endpoint != "" {
				fmt.Printf("Endpoint    : %s\n", endpoint)
			}
		}
	}
}

func runOrcaSwitch(ctx context.Context, inst *Installer, user, home string, args []string) int {
	cb := func(line string) { fmt.Println(line) }
	action := ""
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "desktop", "gui":
		if err := inst.SetOrcaMode(ctx, cb, "desktop"); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	case "headless", "server":
		if err := inst.SetOrcaMode(ctx, cb, "headless"); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		PrintOrcaStatus(ctx, home)
		return 0
	case "autostart":
		target := "toggle"
		if len(args) > 1 {
			target = args[1]
		}
		enabled := target == "enable" || target == "en" || target == "on"
		if target == "toggle" {
			_, err := os.Stat(orcaAutostartPath(home))
			enabled = err != nil
		}
		if err := setOrcaAutostart(user, home, enabled); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		if enabled {
			fmt.Println("✓ Autostart Desktop DIAKTIFKAN.")
		} else {
			fmt.Println("✓ Autostart Desktop DINONAKTIFKAN.")
		}
		return 0
	case "status":
		PrintOrcaStatus(ctx, home)
		return 0
	case "help", "--help", "-h":
		fmt.Println("Penggunaan: orca switch [desktop|headless|autostart|status]")
		fmt.Println("\nPerintah:")
		fmt.Println("  desktop, gui        Beralih ke mode Desktop GUI")
		fmt.Println("  headless, server    Beralih ke mode Headless Server")
		fmt.Println("  autostart [en|dis]  Aktifkan/nonaktifkan autostart desktop")
		fmt.Println("  status              Tampilkan status mode, layanan & pairing")
		return 0
	case "":
		return runOrcaSwitchMenu(ctx, inst, user, home)
	default:
		fmt.Printf("Opsi tidak dikenal: %s\n", action)
		fmt.Println("Gunakan: orca switch [desktop|headless|autostart|status]")
		return 1
	}
}

func runOrcaSwitchMenu(ctx context.Context, inst *Installer, user, home string) int {
	fmt.Println("==================================================")
	fmt.Println(" PENGALIH MODE ORCA ADE")
	fmt.Println("==================================================")
	fmt.Println("1. Mode Desktop GUI")
	fmt.Println("2. Mode Headless Server")
	fmt.Println("3. Toggle Desktop Autostart")
	fmt.Println("4. Status lengkap & pairing")
	fmt.Println("5. Keluar")
	fmt.Print("Pilih opsi [1-5]: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return 0
	}
	switch strings.TrimSpace(scanner.Text()) {
	case "1":
		return runOrcaSwitch(ctx, inst, user, home, []string{"desktop"})
	case "2":
		return runOrcaSwitch(ctx, inst, user, home, []string{"headless"})
	case "3":
		return runOrcaSwitch(ctx, inst, user, home, []string{"autostart", "toggle"})
	case "4":
		return runOrcaSwitch(ctx, inst, user, home, []string{"status"})
	}
	return 0
}

// launchOrcaDesktop menghentikan service headless lalu menjalankan GUI Orca.
func launchOrcaDesktop(ctx context.Context, user, home string, args []string) int {
	bin := orcaAppBinary(home)
	if bin == "" {
		fmt.Fprintln(os.Stderr, "Biner Orca Desktop tidak ditemukan. Jalankan: wdev install orca-desktop")
		return 1
	}
	_ = orcaUserSystemctl(ctx, nil, user, home, "stop", "orca.service")

	extra := []string{}
	sandbox := filepath.Join(orcaInstallDir(home), "chrome-sandbox")
	needSandbox := false
	if fi, err := os.Stat(sandbox); err == nil {
		if fi.Mode().Perm() != 0o755 || fi.Sys().(*syscall.Stat_t).Uid != 0 {
			needSandbox = true
		}
	}
	if needSandbox {
		if data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil && strings.TrimSpace(string(data)) == "0" {
			extra = append(extra, "--no-sandbox")
		}
	}

	argv := append([]string{bin}, append(extra, args...)...)
	if err := syscall.Exec(bin, argv, orcaEnv(home)); err != nil {
		fmt.Fprintf(os.Stderr, "Gagal menjalankan Orca Desktop: %v\n", err)
		return 1
	}
	return 0
}

// HandleOrcaProgram menangani pemanggilan biner lewat nama legacy (orca, orca-pair,
// orca-switch, orca-update, orca-desktop, orca-ide) tanpa skrip shell.
func HandleOrcaProgram(argv0 string, args []string) bool {
	base := filepath.Base(argv0)
	switch base {
	case "orca", "orca-ide", "orca-desktop", "orca-pair", "orca-switch", "orca-update":
	default:
		return false
	}

	ctx := context.Background()
	cfg := config.Load()
	inst := New(cfg, nil)
	user, home := ResolveOrcaUser()

	switch base {
	case "orca-ide":
		os.Exit(execOrcaReal(home, args))
	case "orca-update":
		cb := func(line string) { fmt.Println(line) }
		if err := inst.UpdateOrca(ctx, cb); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "orca-pair":
		os.Exit(runOrcaPair(ctx, home, args))
	case "orca-desktop":
		os.Exit(launchOrcaDesktop(ctx, user, home, args))
	case "orca-switch":
		os.Exit(runOrcaSwitch(ctx, inst, user, home, args))
	case "orca":
		if len(args) == 0 {
			os.Exit(execOrcaReal(home, args))
		}
		switch args[0] {
		case "update":
			cb := func(line string) { fmt.Println(line) }
			rest := args[1:]
			if len(rest) > 0 && (rest[0] == "--check" || rest[0] == "-c") {
				PrintOrcaStatus(ctx, home)
				return true
			}
			if err := inst.UpdateOrca(ctx, cb); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		case "switch", "mode":
			os.Exit(runOrcaSwitch(ctx, inst, user, home, args[1:]))
		case "pair":
			os.Exit(runOrcaPair(ctx, home, args[1:]))
		case "desktop", "gui":
			os.Exit(launchOrcaDesktop(ctx, user, home, args[1:]))
		case "server", "headless":
			if len(args) > 1 {
				switch args[1] {
				case "stop":
					_ = orcaUserSystemctl(ctx, nil, user, home, "stop", "orca.service")
					fmt.Println("✓ Layanan Orca Headless dihentikan.")
					return true
				case "status":
					PrintOrcaStatus(ctx, home)
					return true
				}
			}
			os.Exit(runOrcaSwitch(ctx, inst, user, home, []string{"headless"}))
		case "start":
			mode := readOrcaMode(home)
			if mode == "desktop" {
				os.Exit(launchOrcaDesktop(ctx, user, home, args[1:]))
			}
			os.Exit(runOrcaSwitch(ctx, inst, user, home, []string{"headless"}))
		case "stop":
			_ = orcaUserSystemctl(ctx, nil, user, home, "disable", "--now", "orca.service")
			fmt.Println("✓ Layanan Orca dihentikan.")
		case "status":
			PrintOrcaStatus(ctx, home)
		case "open":
			os.Exit(execOrcaReal(home, args))
		default:
			os.Exit(execOrcaReal(home, args))
		}
	}
	return true
}
