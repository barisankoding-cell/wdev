package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"wdev/internal/config"
	"wdev/internal/security"
	"wdev/internal/store"
)

func etcDBInUse(cfg *config.Config) bool {
	if cfg == nil || cfg.DBPath == "/etc/wdev/wdev.db" {
		return false
	}
	_, etcStatErr := os.Stat("/etc/wdev/wdev.db")
	return etcStatErr == nil || os.IsPermission(etcStatErr) || exec.Command("systemctl", "is-active", "--quiet", "wdev").Run() == nil
}

func requireSystemRootIfNeeded(cfg *config.Config, cmdPrefix string) {
	if os.Geteuid() != 0 && etcDBInUse(cfg) {
		fmt.Fprintf(os.Stderr, "⚠️ Service sistem wdev aktif menggunakan /etc/wdev/wdev.db.\n")
		fmt.Fprintf(os.Stderr, "   Jalankan dengan sudo agar data yang diakses sesuai sistem aktif: sudo %s\n", cmdPrefix)
		os.Exit(1)
	}
}

func printSecurityCLIHelp() {
	fmt.Println(`Penggunaan wdev security (Pemantauan Serangan & Perlindungan Server):
  wdev security status [--json]
      Tampilkan status kebijakan, firewall host, Nginx L7, statistik serangan, & watchers
  wdev security events [--limit 20] [--type <tipe>] [--severity <lvl>] [--search <q>] [--json]
      Tampilkan riwayat event serangan terbaru
  wdev security blocked [--json]
      Tampilkan daftar alamat IP yang sedang diblokir
  wdev security block <ip> [--reason "..."] [--hours 24 | --minutes N]
      Blokir IP secara manual di database, firewall host, Nginx, & Cloudflare WAF
  wdev security unblock <ip>
      Cabut pemblokiran IP
  wdev security whitelist [list | add <ip/cidr> | rm <ip/cidr>]
      Lihat atau kelola daftar whitelist IP/CIDR tepercaya`)
}

func handleSecurityCLI(cfg *config.Config, db *store.Store, args []string) {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}
	if sub == "help" || sub == "-h" || sub == "--help" {
		printSecurityCLIHelp()
		return
	}

	requireSystemRootIfNeeded(cfg, "wdev security "+sub+" ...")

	secMgr := security.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch sub {
	case "status":
		asJSON := hasFlag(args, "--json")
		report := secMgr.GetMonitorStatus(ctx)
		stats, _ := secMgr.GetStats()
		if asJSON {
			printJSON(map[string]any{
				"status": report,
				"stats":  stats,
			})
			return
		}
		printSecurityStatusReport(report, stats)

	case "events", "logs", "list":
		limit := 20
		attackType := ""
		severity := ""
		search := ""
		asJSON := false
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--json":
				asJSON = true
			case "--limit", "-n":
				if i+1 < len(args) {
					if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
						limit = n
					}
					i++
				}
			case "--type":
				if i+1 < len(args) {
					attackType = args[i+1]
					i++
				}
			case "--severity":
				if i+1 < len(args) {
					severity = strings.ToUpper(args[i+1])
					i++
				}
			case "--search", "-q":
				if i+1 < len(args) {
					search = args[i+1]
					i++
				}
			}
		}
		events, total, err := secMgr.ListEvents(limit, 0, attackType, severity, search)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if asJSON {
			printJSON(map[string]any{"events": events, "total": total})
			return
		}
		fmt.Printf("=== RIWAYAT EVENT KEAMANAN (menampilkan %d dari %d) ===\n", len(events), total)
		if len(events) == 0 {
			fmt.Println("Belum ada event serangan yang tercatat.")
			return
		}
		fmt.Printf("%-19s %-9s %-23s %-18s %-12s %s\n", "WAKTU", "SEVERITY", "TIPE", "IP (NEGARA)", "AKSI", "TARGET / DETAIL")
		fmt.Println(strings.Repeat("-", 105))
		for _, ev := range events {
			ipLabel := ev.SourceIP
			if ev.CountryCode != "" {
				ipLabel = fmt.Sprintf("%s (%s)", ev.SourceIP, ev.CountryCode)
			}
			targetDetail := ev.Target
			if ev.Details != "" {
				targetDetail = targetDetail + " — " + ev.Details
			}
			if len(targetDetail) > 48 {
				targetDetail = targetDetail[:45] + "..."
			}
			fmt.Printf("%-19s %-9s %-23s %-18s %-12s %s\n",
				ev.Timestamp.Local().Format("2006-01-02 15:04:05"),
				ev.Severity,
				ev.AttackType,
				ipLabel,
				ev.ActionTaken,
				targetDetail,
			)
		}

	case "blocked":
		asJSON := hasFlag(args, "--json")
		list, err := secMgr.ListBlockedIPs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if asJSON {
			printJSON(map[string]any{"blocked_ips": list})
			return
		}
		fmt.Printf("=== DAFTAR IP TERBLOKIR AKTIF (%d) ===\n", len(list))
		if len(list) == 0 {
			fmt.Println("Tidak ada IP yang sedang diblokir.")
			return
		}
		fmt.Printf("%-20s %-19s %-19s %s\n", "ALAMAT IP", "DIBLOKIR SEJAK", "KADALUWARSA", "ALASAN")
		fmt.Println(strings.Repeat("-", 92))
		for _, rec := range list {
			expStr := "Permanen"
			if rec.ExpiresAt != nil && !rec.ExpiresAt.IsZero() {
				expStr = rec.ExpiresAt.Local().Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-20s %-19s %-19s %s\n",
				rec.IP,
				rec.BlockedAt.Local().Format("2006-01-02 15:04:05"),
				expStr,
				rec.Reason,
			)
		}

	case "block":
		if len(args) < 1 {
			fmt.Println("Penggunaan: wdev security block <ip> [--reason \"...\"] [--hours 24 | --minutes N]")
			os.Exit(1)
		}
		ip := strings.TrimSpace(args[0])
		if parsed := net.ParseIP(ip); parsed == nil {
			fmt.Fprintf(os.Stderr, "Error: format IP %q tidak valid\n", ip)
			os.Exit(1)
		} else {
			ip = parsed.String()
		}
		reason := "Manual block via CLI"
		dur := 24 * time.Hour
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--reason", "-r":
				if i+1 < len(args) {
					reason = args[i+1]
					i++
				}
			case "--hours":
				if i+1 < len(args) {
					if h, err := strconv.Atoi(args[i+1]); err == nil && h >= 0 {
						dur = time.Duration(h) * time.Hour
					}
					i++
				}
			case "--minutes":
				if i+1 < len(args) {
					if m, err := strconv.Atoi(args[i+1]); err == nil && m >= 0 {
						dur = time.Duration(m) * time.Minute
					}
					i++
				}
			}
		}
		if err := secMgr.BlockIP(ctx, ip, reason, dur); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ IP %s berhasil diblokir (%s).\n", ip, reason)

	case "unblock":
		if len(args) < 1 {
			fmt.Println("Penggunaan: wdev security unblock <ip>")
			os.Exit(1)
		}
		ip := strings.TrimSpace(args[0])
		if err := secMgr.UnblockIP(ctx, ip); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Pemblokiran IP %s dicabut.\n", ip)

	case "whitelist":
		action := "list"
		if len(args) > 0 {
			action = strings.ToLower(strings.TrimSpace(args[0]))
		}
		policy := secMgr.GetPolicy()
		switch action {
		case "list", "ls":
			fmt.Printf("Whitelist IP/CIDR (%d):\n", len(policy.Whitelist))
			if len(policy.Whitelist) == 0 {
				fmt.Println("  (Kosong — loopback & subnet VPN privat selalu dikecualikan otomatis)")
				return
			}
			for _, w := range policy.Whitelist {
				fmt.Println(" - " + w)
			}
		case "add":
			if len(args) < 2 {
				fmt.Println("Penggunaan: wdev security whitelist add <ip/cidr>")
				os.Exit(1)
			}
			norm, err := security.NormalizeIPOrCIDR(args[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			for _, existing := range policy.Whitelist {
				if existing == norm {
					fmt.Printf("• %s sudah ada di whitelist.\n", norm)
					return
				}
			}
			policy.Whitelist = append(policy.Whitelist, norm)
			if _, err := secMgr.UpdatePolicy(ctx, policy); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ %s ditambahkan ke whitelist.\n", norm)
		case "rm", "remove", "delete":
			if len(args) < 2 {
				fmt.Println("Penggunaan: wdev security whitelist rm <ip/cidr>")
				os.Exit(1)
			}
			norm, err := security.NormalizeIPOrCIDR(args[1])
			if err != nil {
				norm = strings.TrimSpace(args[1])
			}
			next := make([]string, 0, len(policy.Whitelist))
			removed := false
			for _, existing := range policy.Whitelist {
				if existing == norm {
					removed = true
					continue
				}
				next = append(next, existing)
			}
			if !removed {
				fmt.Fprintf(os.Stderr, "Entri %s tidak ditemukan di whitelist.\n", norm)
				os.Exit(1)
			}
			policy.Whitelist = next
			if _, err := secMgr.UpdatePolicy(ctx, policy); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ %s dihapus dari whitelist.\n", norm)
		default:
			fmt.Println("Penggunaan: wdev security whitelist [list|add <ip/cidr>|rm <ip/cidr>]")
			os.Exit(1)
		}

	default:
		fmt.Printf("Sub-perintah security tidak dikenal: %s\n\n", sub)
		printSecurityCLIHelp()
		os.Exit(1)
	}
}

func printSecurityStatusReport(report security.MonitorStatusReport, stats store.SecurityStatsRecord) {
	fmt.Println("==================================================================")
	fmt.Println(" STATUS KEAMANAN & PERLINDUNGAN SERVER (wdev) ")
	fmt.Println("==================================================================")
	fmt.Printf(" Auto-Ban           : %v (Threshold: %d strike, Durasi: %dj / Kritis: %dj)\n",
		report.Policy.AutoBanEnabled, report.Policy.StrikeThreshold, report.Policy.BanDurationHours, report.Policy.CriticalBanHours)
	fmt.Printf(" Firewall Host      : %s (Aktif: %v, Sync: %v) — %s\n",
		strings.ToUpper(report.Firewall.Engine), report.Firewall.Active, report.Firewall.SyncEnabled, report.Firewall.Summary)
	fmt.Printf(" Nginx L7 Blocklist : Tersedia=%v, Sync=%v, Real-IP=%s (%d entri)\n",
		report.Nginx.Available, report.Nginx.BlocklistSync, report.Nginx.RealIPMode, report.Nginx.BlockedEntries)
	fmt.Printf(" Whitelist IP/CIDR  : %d entri\n", len(report.Policy.Whitelist))
	fmt.Printf(" Statistik Serangan : Hari ini=%d | 7 Hari=%d | IP Diblokir=%d | Critical=%d | High=%d\n",
		stats.TotalToday, stats.Total7Days, stats.TotalBlocked, stats.CriticalCount, stats.HighCount)
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf(" %-28s %-9s %-10s %s\n", "WATCHER", "ENABLED", "AVAILABLE", "SUMBER")
	for _, w := range report.Watchers {
		fmt.Printf(" %-28s %-9v %-10v %s\n", w.Name, w.Enabled, w.Available, w.Source)
	}
	fmt.Println("==================================================================")
}

func hasFlag(args []string, flagName string) bool {
	for _, a := range args {
		if a == flagName {
			return true
		}
	}
	return false
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
