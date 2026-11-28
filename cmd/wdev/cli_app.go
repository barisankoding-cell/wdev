package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"wdev/internal/config"
	"wdev/internal/siteapp"
	"wdev/internal/store"
)

func printAppCLIHelp() {
	fmt.Println(`Penggunaan wdev app (Pengelola Managed App Node.js / Go / Rust via systemd):
  wdev app list [--json]                   Daftar seluruh Managed App dan status live systemd
  wdev app status <domain> [--json]        Tampilkan detail konfigurasi, PID, RAM & unit systemd
  wdev app start|stop|restart <domain>     Kendalikan service systemd aplikasi
  wdev app build <domain> [--no-restart]   Jalankan build_command dan restart service
  wdev app logs <domain> [--lines 100] [-f|--follow]
                                           Lihat atau ikuti log journalctl aplikasi
  wdev app env <domain> [list | get <KEY> | set <KEY>=<VAL> | unset <KEY>] [--restart]
                                           Lihat atau kelola environment variable aplikasi`)
}

func handleAppCLI(cfg *config.Config, db *store.Store, siteAppMgr *siteapp.Manager, args []string) {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}
	if sub == "help" || sub == "-h" || sub == "--help" {
		printAppCLIHelp()
		return
	}

	requireSystemRootIfNeeded(cfg, "wdev app "+sub+" ...")

	ctx := context.Background()
	switch sub {
	case "list", "ls":
		asJSON := hasFlag(args, "--json")
		apps, err := db.ListSiteApps()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		domains := make([]string, 0, len(apps))
		for _, a := range apps {
			domains = append(domains, a.Domain)
		}
		statusMap := siteAppMgr.BatchStatus(ctx, domains)
		if asJSON {
			type item struct {
				App    store.SiteAppRecord       `json:"app"`
				Status siteapp.AppRuntimeStatus `json:"status"`
			}
			out := make([]item, 0, len(apps))
			for _, a := range apps {
				out = append(out, item{App: a, Status: statusMap[strings.ToLower(a.Domain)]})
			}
			printJSON(out)
			return
		}
		if len(apps) == 0 {
			fmt.Println("Belum ada Managed App (Node.js / Go / Rust) terdaftar.")
			return
		}
		fmt.Printf("%-24s %-8s %-7s %-12s %-11s %-8s %-10s %s\n",
			"DOMAIN", "RUNTIME", "PORT", "USER", "STATUS", "PID", "RAM", "WORKING DIR")
		fmt.Println(strings.Repeat("-", 104))
		for _, a := range apps {
			st := statusMap[strings.ToLower(a.Domain)]
			stateStr := st.ActiveState
			if stateStr == "" {
				stateStr = "inactive"
			}
			pidStr := "-"
			if st.MainPID > 0 {
				pidStr = strconv.Itoa(st.MainPID)
			}
			ramStr := "-"
			if st.MemoryBytes > 0 {
				ramStr = formatBytesCompact(st.MemoryBytes)
			}
			fmt.Printf("%-24s %-8s %-7d %-12s %-11s %-8s %-10s %s\n",
				truncateStr(a.Domain, 24),
				a.Runtime,
				a.Port,
				truncateStr(a.RunUser, 12),
				stateStr,
				pidStr,
				ramStr,
				a.WorkingDir,
			)
		}

	case "status", "info":
		if len(args) < 1 {
			fmt.Println("Penggunaan: wdev app status <domain> [--json]")
			os.Exit(1)
		}
		domain := args[0]
		asJSON := hasFlag(args[1:], "--json")
		rec, err := db.GetSiteApp(domain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if rec == nil {
			fmt.Fprintf(os.Stderr, "Managed App %q tidak ditemukan.\n", domain)
			os.Exit(1)
		}
		st := siteAppMgr.Status(ctx, rec.Domain)
		if asJSON {
			printJSON(map[string]any{"app": rec, "status": st})
			return
		}
		fmt.Printf("=== MANAGED APP: %s ===\n", rec.Domain)
		fmt.Printf(" Runtime      : %s\n", rec.Runtime)
		fmt.Printf(" Service Unit : %s (Active: %s/%s, Enabled: %v)\n", st.ServiceName, st.ActiveState, st.SubState, st.Enabled)
		if st.MainPID > 0 {
			fmt.Printf(" Process      : PID %d | RAM %s\n", st.MainPID, formatBytesCompact(st.MemoryBytes))
		}
		fmt.Printf(" Port Lokal   : :%d | Run User: %s\n", rec.Port, rec.RunUser)
		fmt.Printf(" Working Dir  : %s\n", rec.WorkingDir)
		fmt.Printf(" Env File     : %s\n", rec.EnvFile)
		if rec.BuildCommand != "" {
			fmt.Printf(" Build Cmd    : %s\n", rec.BuildCommand)
		}
		fmt.Printf(" Start Cmd    : %s\n", rec.StartCommand)
		if rec.LastBuildStatus != "" {
			at := ""
			if rec.LastBuildAt != nil {
				at = " (" + rec.LastBuildAt.Local().Format("2006-01-02 15:04:05") + ")"
			}
			fmt.Printf(" Build Terakhir: %s%s\n", rec.LastBuildStatus, at)
		}

	case "start", "stop", "restart":
		if len(args) < 1 {
			fmt.Printf("Penggunaan: wdev app %s <domain>\n", sub)
			os.Exit(1)
		}
		domain := args[0]
		if err := siteAppMgr.Action(ctx, domain, sub); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Managed App %s berhasil di-%s.\n", domain, sub)

	case "build":
		if len(args) < 1 {
			fmt.Println("Penggunaan: wdev app build <domain> [--no-restart]")
			os.Exit(1)
		}
		domain := args[0]
		noRestart := hasFlag(args[1:], "--no-restart")
		if err := siteAppMgr.RunBuild(ctx, domain, !noRestart, func(line string) {
			fmt.Println(line)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "logs":
		if len(args) < 1 {
			fmt.Println("Penggunaan: wdev app logs <domain> [--lines 100] [-f|--follow]")
			os.Exit(1)
		}
		domain := args[0]
		lines := 100
		follow := false
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "-f", "--follow":
				follow = true
			case "--lines", "-n":
				if i+1 < len(args) {
					if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
						lines = n
					}
					i++
				}
			}
		}
		if follow {
			cmd := siteAppMgr.NewLogStreamCmd(ctx, domain, lines)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		out, err := siteAppMgr.ReadLogs(ctx, domain, lines)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(out)
		if !strings.HasSuffix(out, "\n") && out != "" {
			fmt.Println()
		}

	case "env":
		if len(args) < 1 {
			fmt.Println("Penggunaan: wdev app env <domain> [list|get <KEY>|set <KEY>=<VAL>|unset <KEY>] [--restart]")
			os.Exit(1)
		}
		domain := args[0]
		action := "list"
		rest := args[1:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			action = strings.ToLower(strings.TrimSpace(rest[0]))
			rest = rest[1:]
		}
		restart := hasFlag(rest, "--restart")

		envPath, entries, raw, err := siteAppMgr.GetEnv(ctx, domain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		switch action {
		case "list", "ls":
			fmt.Printf("# File: %s\n", envPath)
			fmt.Print(raw)
			if !strings.HasSuffix(raw, "\n") && raw != "" {
				fmt.Println()
			}
		case "get":
			if len(rest) < 1 {
				fmt.Println("Penggunaan: wdev app env <domain> get <KEY>")
				os.Exit(1)
			}
			key := strings.TrimSpace(rest[0])
			for _, e := range entries {
				if e.Key == key {
					fmt.Println(e.Value)
					return
				}
			}
			fmt.Fprintf(os.Stderr, "Key %q tidak ditemukan di %s.\n", key, envPath)
			os.Exit(1)
		case "set":
			if len(rest) < 1 || !strings.Contains(rest[0], "=") {
				fmt.Println("Penggunaan: wdev app env <domain> set KEY=VALUE [--restart]")
				os.Exit(1)
			}
			kv := strings.SplitN(rest[0], "=", 2)
			key, val := strings.TrimSpace(kv[0]), kv[1]
			if key == "" {
				fmt.Fprintln(os.Stderr, "Error: nama key tidak boleh kosong")
				os.Exit(1)
			}
			updated := false
			for i := range entries {
				if entries[i].Key == key {
					entries[i].Value = val
					updated = true
					break
				}
			}
			if !updated {
				entries = append(entries, siteapp.EnvEntry{Key: key, Value: val})
			}
			res, err := siteAppMgr.SaveEnv(ctx, domain, entries, "", restart)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ %s=%s disimpan di %s (Port aktif: %d).\n", key, val, res.EnvFile, res.ResolvedPort)
		case "unset", "rm", "del":
			if len(rest) < 1 {
				fmt.Println("Penggunaan: wdev app env <domain> unset <KEY> [--restart]")
				os.Exit(1)
			}
			key := strings.TrimSpace(rest[0])
			next := make([]siteapp.EnvEntry, 0, len(entries))
			found := false
			for _, e := range entries {
				if e.Key == key {
					found = true
					continue
				}
				next = append(next, e)
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Key %q tidak ditemukan di %s.\n", key, envPath)
				os.Exit(1)
			}
			res, err := siteAppMgr.SaveEnv(ctx, domain, next, "", restart)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Key %s dihapus dari %s.\n", key, res.EnvFile)
		default:
			fmt.Printf("Aksi env tidak dikenal: %s\n", action)
			os.Exit(1)
		}

	default:
		fmt.Printf("Sub-perintah app tidak dikenal: %s\n\n", sub)
		printAppCLIHelp()
		os.Exit(1)
	}
}

func formatBytesCompact(b int64) string {
	if b <= 0 {
		return "0 B"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Unused time reference avoided via direct formatting
var _ = time.Second
