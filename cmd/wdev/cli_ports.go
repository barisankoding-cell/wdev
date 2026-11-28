package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"wdev/internal/auth"
	"wdev/internal/config"
	"wdev/internal/pdns"
	"wdev/internal/server"
	"wdev/internal/serviceconfig"
	"wdev/internal/site"
	"wdev/internal/store"
)

func printPortsCLIHelp() {
	fmt.Println(`Penggunaan wdev ports (Diagnostik Port & Jangkauan Jaringan):
  wdev ports [opsi]
Opsi:
  --refresh           Segarkan probe koneksi eksternal tanpa cache
  --category <nama>   Filter kategori (system, web, database, dns, mail)
  --status <warna>    Filter status (green, yellow, red, all)
  --all               Tampilkan juga layanan yang belum terpasang
  --json              Keluarkan laporan lengkap dalam format JSON`)
}

func handlePortsCLI(
	cfg *config.Config,
	db *store.Store,
	siteMgr *site.Manager,
	pdnsMgr *pdns.Manager,
	authMgr *auth.Manager,
	svcCfgMgr *serviceconfig.Manager,
	args []string,
) {
	refresh := false
	showAll := false
	asJSON := false
	categoryFilter := "all"
	statusFilter := "all"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help", "help":
			printPortsCLIHelp()
			return
		case "--refresh", "-r":
			refresh = true
		case "--all", "-a":
			showAll = true
		case "--json":
			asJSON = true
		case "--category", "-c":
			if i+1 < len(args) {
				categoryFilter = strings.ToLower(strings.TrimSpace(args[i+1]))
				i++
			}
		case "--status", "-s":
			if i+1 < len(args) {
				statusFilter = strings.ToLower(strings.TrimSpace(args[i+1]))
				i++
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report := server.InspectPorts(ctx, cfg, db, siteMgr, pdnsMgr, authMgr, svcCfgMgr, refresh, "id")
	if asJSON {
		printJSON(report)
		return
	}

	fmt.Println("==================================================================================")
	fmt.Println(" DIAGNOSTIK STATUS PORT & JANGKAUAN JARINGAN (wdev) ")
	fmt.Println("==================================================================================")
	fmt.Printf(" Mode Server : %-10s | Firewall : %s (Aktif=%v)\n",
		strings.ToUpper(report.Network.Mode), strings.ToUpper(report.Network.Firewall.Engine), report.Network.Firewall.Active)
	fmt.Printf(" IP Lokal    : %-15s | LAN: %s | VPN: %s | VPS Publik: %s\n",
		report.Network.LocalIP, dashIfEmpty(report.Network.LanIP), dashIfEmpty(report.Network.VPNIP), dashIfEmpty(report.Network.VPSPublicIP))
	fmt.Printf(" Ringkasan   : Terpasang=%d/%d | Aman/Terbuka(Hijau)=%d | Peringatan(Kuning)=%d | Tertutup(Merah)=%d\n",
		report.Summary.Installed, report.Summary.Total, report.Summary.Green, report.Summary.Yellow, report.Summary.Red)
	fmt.Println("----------------------------------------------------------------------------------")
	fmt.Printf(" %-7s %-6s %-22s %-11s %-10s %-10s %-10s %s\n",
		"PORT", "PROTO", "LAYANAN", "SCOPE", "LOKAL", "VPN", "VPS", "STATUS")
	fmt.Println("----------------------------------------------------------------------------------")

	var advisories []server.PortEntry
	shown := 0
	for _, p := range report.Ports {
		if !showAll && !p.Installed {
			continue
		}
		if categoryFilter != "all" && !strings.EqualFold(p.Category, categoryFilter) {
			continue
		}
		if statusFilter != "all" && !strings.EqualFold(p.OverallColor, statusFilter) {
			continue
		}
		shown++
		fmt.Printf(" %-7d %-6s %-22s %-11s %-10s %-10s %-10s %s\n",
			p.Port,
			strings.ToUpper(p.Protocol),
			truncateStr(p.Name, 22),
			p.ExpectedScope,
			truncateStr(p.Local.Label, 10),
			truncateStr(p.VPN.Label, 10),
			truncateStr(p.VPS.Label, 10),
			p.OverallLabel,
		)
		if p.Installed && (p.OverallColor == "yellow" || p.OverallColor == "red") && p.Recommendation != "" {
			advisories = append(advisories, p)
		}
	}
	if shown == 0 {
		fmt.Println(" (Tidak ada port yang cocok dengan filter saat ini)")
	}
	if len(advisories) > 0 {
		fmt.Println("----------------------------------------------------------------------------------")
		fmt.Println(" REKOMENDASI:")
		for _, a := range advisories {
			fmt.Printf("  ! :%d (%s) — %s\n", a.Port, a.Name, a.Recommendation)
		}
	}
	fmt.Println("==================================================================================")
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
