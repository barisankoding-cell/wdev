package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/server"
	"wdev/internal/site"
	"wdev/internal/store"
	"wdev/internal/switcher"
	"wdev/internal/tui"
)

const Version = "2.0.0"

func printHelp() {
	fmt.Printf(`wdev v%s — Linux Developer Control Plane

PENGGUNAAN:
  wdev [perintah] [argumen...]

ANTARMUKA:
  wdev serve, wdev web      Jalankan Web UI Dashboard (default: http://localhost:3200)
    --port <port>           Tentukan port HTTP (default: 3200)
    --host <host>           Tentukan host binding (default: 0.0.0.0)
  wdev tui                  Jalankan TUI Terminal Dashboard interaktif

MANAJEMEN LAYANAN & STATUS:
  wdev status, wdev check   Tampilkan status cepat sistem, IP, dan layanan inti
  wdev check-updates, -cu   Periksa ketersediaan pembaruan toolchain & paket sistem
  wdev service <nama> <aksi> Start/Stop/Restart layanan (contoh: wdev service nginx restart)

VIRTUAL HOST (NGINX):
  wdev site list            Tampilkan semua virtual host lokal
  wdev site create <domain> [root] [php-ver] [framework] [proxy-port]
                            Buat virtual host baru dengan SSL otomatis
  wdev site delete <domain> Hapus virtual host, SSL, dan entri /etc/hosts
  wdev site sync            Sinkronkan semua vhost ke /etc/hosts & sertifikat SSL

RUNTIME SWITCHER:
  wdev switch list          Tampilkan versi Node, PHP, PostgreSQL, Java
  wdev switch node <ver>    Ganti versi Node.js aktif (contoh: wdev switch node 22)
  wdev switch php <ver>     Ganti versi PHP aktif (contoh: wdev switch php 8.4)
  wdev switch pg <ver>      Ganti versi cluster PostgreSQL aktif
  wdev switch java <ver>    Ganti versi OpenJDK aktif (contoh: wdev switch java 21)

CLOUDFLARE TUNNEL:
  wdev cf status            Status tunnel, akun, dan exposures aktif
  wdev cf expose <local> <public> [--profile <nama>]
                            Ekspos vhost lokal ke domain publik Cloudflare
  wdev cf unexpose <local>  Hapus eksposur domain

POWERDNS & VPS EDGE:
  wdev pdns status                         Status koneksi VPS Netbird & PowerDNS API
  wdev pdns zone [list]                    Tampilkan daftar authoritative zone
  wdev pdns zone add <nama>                Tambah authoritative zone baru
  wdev pdns zone rm <nama>                 Hapus authoritative zone
  wdev pdns record list <zone>             Tampilkan record DNS dalam zone
  wdev pdns record add <z> <n> <t> <val>   Tambah/update DNS record (TTL opsional)
  wdev pdns record rm <z> <n> <t>          Hapus DNS record
  wdev pdns exposures                      Daftar proxy domain aktif di Edge Gateway
  wdev pdns expose <dom> <port> [--ssl]    Ekspos port ke domain publik via VPS Edge
  wdev pdns expose-site <loc> <pub> [--ssl] Ekspos vhost lokal (PHP/Laravel) via VPS Edge
  wdev pdns unexpose <dom> [--keep-dns]    Hapus eksposur dari Edge Gateway

INSTALASI & UPDATE:
  wdev install <komponen>   Pasang atau perbarui komponen (update, node, php-stack, postgres, dll.)
`, Version)
}

func main() {
	cfg := config.Load()

	db, err := store.New(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Peringatan: SQLite store gagal diinisialisasi: %v\n", err)
	} else {
		defer db.Close()
		// Initial sync from existing disk configs
		db.SyncFromDisk(cfg.NginxSitesAvail, cfg.WdevEtc+"/exposures")
	}

	runMgr := runner.New(db)
	inst := installer.New(cfg, db)
	siteMgr := site.New(cfg, db)
	swMgr := switcher.New()
	cfMgr := cloudflare.New(cfg, db)
	pdnsMgr := pdns.New(cfg, db)

	args := os.Args[1:]
	if len(args) == 0 {
		// If interactive terminal, launch TUI by default
		t := tui.New(cfg, db, inst, siteMgr, swMgr, cfMgr, pdnsMgr)
		t.Run()
		return
	}

	switch args[0] {
	case "serve", "web", "dashboard":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		portFlag := fs.String("port", cfg.HTTPPort, "Port HTTP untuk Web Dashboard")
		hostFlag := fs.String("host", cfg.HTTPHost, "Host binding")
		_ = fs.Parse(args[1:])

		cfg.HTTPPort = *portFlag
		cfg.HTTPHost = *hostFlag

		srv := server.New(cfg, db, runMgr, inst, siteMgr, swMgr, cfMgr, pdnsMgr)
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error menjalankan server: %v\n", err)
			os.Exit(1)
		}

	case "tui":
		t := tui.New(cfg, db, inst, siteMgr, swMgr, cfMgr, pdnsMgr)
		t.Run()

	case "status", "check", "-c":
		stats, err := monitor.Collect()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Gagal memuat status: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("=== wdev Status Sistem ===\n")
		fmt.Printf("OS:         %s\n", stats.OSName)
		fmt.Printf("Uptime:     %s\n", stats.UptimeString)
		fmt.Printf("Load Avg:   %s\n", stats.LoadAvg)
		fmt.Printf("RAM:        %d / %d MB (%d%%)\n", stats.MemUsedMB, stats.MemTotalMB, stats.MemPercent)
		fmt.Printf("Disk (/):   %d / %d GB (%d%%)\n", stats.DiskUsedGB, stats.DiskTotalGB, stats.DiskPercent)
		fmt.Printf("LAN IP:     %s\n", stats.LanIP)
		fmt.Printf("NetBird IP: %s\n", stats.NetbirdIP)
		fmt.Println("\nLayanan Inti:")
		for _, s := range stats.Services {
			status := "NONAKTIF"
			if s.Active {
				status = "AKTIF"
			}
			fmt.Printf("  - %-24s : %s\n", s.DisplayName, status)
		}
		fmt.Println("\nRuntime Aktif:")
		for k, v := range stats.Runtimes {
			fmt.Printf("  - %-12s : %s\n", strings.ToUpper(k), v)
		}

	case "check-updates", "check-update", "updates", "-cu":
		ctx := context.Background()
		cb := func(line string) {
			fmt.Println(line)
		}
		if err := inst.CheckUpdates(ctx, cb); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "service":
		if len(args) < 3 {
			fmt.Println("Penggunaan: wdev service <nama> <start|stop|restart|reload>")
			os.Exit(1)
		}
		name, action := args[1], args[2]
		if err := monitor.ControlService(name, action); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Layanan %s berhasil di-%s\n", name, action)

	case "site":
		if len(args) < 2 {
			fmt.Println("Penggunaan: wdev site <list|create|delete>")
			os.Exit(1)
		}
		switch args[1] {
		case "list", "ls":
			sites, err := siteMgr.ListSites()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if len(sites) == 0 {
				fmt.Println("Belum ada virtual host.")
				return
			}
			fmt.Printf("%-24s %-12s %-10s %-8s %s\n", "DOMAIN", "FRAMEWORK", "RUNTIME", "SSL", "ROOT / TARGET")
			fmt.Println(strings.Repeat("-", 70))
			for _, s := range sites {
				sslText := "HTTP"
				if s.SSLEnabled {
					sslText = "SSL"
				}
				target := s.RootPath
				if s.Framework == "proxy" {
					target = fmt.Sprintf("Port :%d", s.ProxyPort)
				}
				fmt.Printf("%-24s %-12s %-10s %-8s %s\n", s.Domain, strings.ToUpper(s.Framework), "PHP "+s.PHPVersion, sslText, target)
			}

		case "create", "add":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev site create <domain> [root_path] [php_ver] [framework] [proxy_port]")
				os.Exit(1)
			}
			domain := args[2]
			rootPath := ""
			if len(args) >= 4 {
				rootPath = args[3]
			}
			phpVer := "8.4"
			if len(args) >= 5 {
				phpVer = args[4]
			}
			fw := "laravel"
			if len(args) >= 6 {
				fw = args[5]
			}
			proxyPort := 0
			if len(args) >= 7 {
				proxyPort, _ = strconv.Atoi(args[6])
			}

			err := siteMgr.CreateSite(site.CreateOptions{
				Domain:     domain,
				RootPath:   rootPath,
				PHPVersion: phpVer,
				Framework:  fw,
				ProxyPort:  proxyPort,
				SSLEnabled: true,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Gagal membuat site: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Virtual host %s berhasil dibuat dengan SSL otomatis!\n", domain)

		case "delete", "rm":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev site delete <domain>")
				os.Exit(1)
			}
			domain := args[2]
			if err := siteMgr.DeleteSite(domain); err != nil {
				fmt.Fprintf(os.Stderr, "Gagal menghapus site: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Virtual host %s berhasil dihapus.\n", domain)

		case "sync":
			if err := siteMgr.SyncAll(); err != nil {
				fmt.Fprintf(os.Stderr, "Gagal sinkronisasi site: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓ Seluruh virtual host berhasil disinkronkan ke /etc/hosts dan sertifikat SSL.")
		}

	case "switch":
		if len(args) < 2 || args[1] == "list" {
			info := swMgr.GetAll()
			fmt.Println("=== Multi-Versi Runtime ===")
			for _, item := range info {
				fmt.Printf("%s:\n  Versi Aktif: %s\n  Tersedia:    %s\n", item.Name, item.ActiveVersion, strings.Join(item.AvailableVersions, ", "))
			}
			return
		}

		if len(args) < 3 {
			fmt.Println("Penggunaan: wdev switch <node|php|pg|java> <versi>")
			os.Exit(1)
		}

		target, ver := args[1], args[2]
		var err error
		switch target {
		case "node":
			err = swMgr.SwitchNode(ver)
		case "php":
			err = swMgr.SwitchPHP(ver)
		case "pg":
			err = swMgr.SwitchPG(ver)
		case "java":
			err = swMgr.SwitchJava(ver)
		default:
			fmt.Fprintf(os.Stderr, "Target switcher tidak dikenal: %s\n", target)
			os.Exit(1)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Gagal ganti versi: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Berhasil mengganti %s ke versi %s\n", target, ver)

	case "cf":
		if len(args) < 2 || args[1] == "status" {
			st := cfMgr.GetStatus()
			fmt.Printf("Cloudflare Tunnel: ")
			if st.Active {
				fmt.Println("AKTIF")
			} else {
				fmt.Println("NONAKTIF")
			}
			fmt.Printf("Profil: %s\n", strings.Join(st.Profiles, ", "))
			fmt.Println("Exposures:")
			for _, exp := range st.Exposures {
				fmt.Printf("  - %s -> https://%s (%s)\n", exp.LocalDomain, exp.PublicHost, exp.Profile)
			}
			return
		}
		switch args[1] {
		case "expose":
			if len(args) < 4 {
				fmt.Println("Penggunaan: wdev cf expose <local_domain> <public_host> [--profile <nama>]")
				os.Exit(1)
			}
			profile := ""
			for i := 4; i < len(args); i++ {
				if args[i] == "--profile" && i+1 < len(args) {
					profile = args[i+1]
					break
				}
			}
			if err := cfMgr.Expose(args[2], args[3], profile); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ %s berhasil diekspos ke https://%s\n", args[2], args[3])

		case "unexpose":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev cf unexpose <local_domain>")
				os.Exit(1)
			}
			if err := cfMgr.Unexpose(args[2]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Exposure untuk %s dihapus.\n", args[2])
		}

	case "pdns":
		if len(args) < 2 || args[1] == "status" {
			conf := pdnsMgr.LoadConfig()
			fmt.Printf("PowerDNS & VPS Edge Gateway:\n")
			fmt.Printf("  Terkonfigurasi : %v\n", conf.Configured)
			fmt.Printf("  VPS NetBird IP : %s\n", conf.VPSNetbirdIP)
			fmt.Printf("  VPS Public IP  : %s\n", conf.VPSPublicIP)
			fmt.Printf("  NetBird Lokal  : %s\n", conf.LocalNetbirdIP)
			fmt.Printf("  Status Koneksi : ")
			if conf.Reachable {
				fmt.Println("TERHUBUNG")
			} else {
				fmt.Println("TIDAK DAPAT DIHUBUNGI")
			}
			return
		}
		switch args[1] {
		case "zone":
			if len(args) >= 4 && (args[2] == "add" || args[2] == "create") {
				if err := pdnsMgr.AddZone(args[3]); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("✓ Zone %s berhasil ditambahkan.\n", args[3])
			} else if len(args) >= 4 && (args[2] == "rm" || args[2] == "delete" || args[2] == "del") {
				if err := pdnsMgr.DeleteZone(args[3]); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("✓ Zone %s berhasil dihapus.\n", args[3])
			} else {
				zones, err := pdnsMgr.ListZones()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Println("Zone Authoritative DNS:")
				for _, z := range zones {
					fmt.Printf("  - %-25s (Serial: %d)\n", z.Name, z.Serial)
				}
			}

		case "record":
			if len(args) < 3 {
				fmt.Println("Penggunaan:")
				fmt.Println("  wdev pdns record list <zone>")
				fmt.Println("  wdev pdns record add <zone> <name> <type> <content> [ttl]")
				fmt.Println("  wdev pdns record rm <zone> <name> <type>")
				return
			}
			switch args[2] {
			case "list":
				if len(args) < 4 {
					fmt.Println("Penggunaan: wdev pdns record list <zone>")
					os.Exit(1)
				}
				zoneInfo, err := pdnsMgr.GetZone(args[3])
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("Record DNS pada Zone '%s':\n", zoneInfo.Name)
				fmt.Printf("  %-32s %-8s %-6s %s\n", "NAME", "TYPE", "TTL", "CONTENT")
				for _, rr := range zoneInfo.RRsets {
					for _, rec := range rr.Records {
						fmt.Printf("  %-32s %-8s %-6d %s\n", rr.Name, rr.Type, rr.TTL, rec.Content)
					}
				}
			case "add", "set":
				if len(args) < 7 {
					fmt.Println("Penggunaan: wdev pdns record add <zone> <name> <type> <content> [ttl]")
					os.Exit(1)
				}
				ttl := 300
				if len(args) >= 8 {
					_, _ = fmt.Sscanf(args[7], "%d", &ttl)
				}
				if err := pdnsMgr.AddRecord(args[3], args[4], args[5], args[6], ttl); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("✓ Record %s (%s) berhasil ditambahkan pada zone %s.\n", args[4], args[5], args[3])
			case "rm", "delete", "del":
				if len(args) < 6 {
					fmt.Println("Penggunaan: wdev pdns record rm <zone> <name> <type>")
					os.Exit(1)
				}
				if err := pdnsMgr.DeleteRecord(args[3], args[4], args[5]); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("✓ Record %s (%s) dihapus dari zone %s.\n", args[4], args[5], args[3])
			}

		case "exposures", "list-exposures", "proxies":
			proxies, err := pdnsMgr.ListExposures()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if len(proxies) == 0 {
				fmt.Println("Belum ada eksposur / proxy aktif di VPS Edge Gateway.")
				return
			}
			fmt.Println("Proxy Domain Aktif di VPS Edge Gateway:")
			fmt.Printf("  %-35s %-10s %s\n", "DOMAIN", "STATUS", "CONFIG FILE")
			for _, p := range proxies {
				status := "AKTIF"
				if !p.Enabled {
					status = "NONAKTIF"
				}
				fmt.Printf("  %-35s %-10s %s\n", p.Domain, status, p.ConfigFile)
			}

		case "expose":
			if len(args) < 4 {
				fmt.Println("Penggunaan: wdev pdns expose <domain> <port> [--ssl]")
				os.Exit(1)
			}
			domain := args[2]
			var port int
			if _, err := fmt.Sscanf(args[3], "%d", &port); err != nil || port <= 0 {
				fmt.Fprintf(os.Stderr, "Error: port tidak valid: %s\n", args[3])
				os.Exit(1)
			}
			useSSL := false
			for _, arg := range args[4:] {
				if arg == "--ssl" {
					useSSL = true
				}
			}
			fmt.Printf("Mengekspos port %d ke domain %s via VPS Edge Gateway...\n", port, domain)
			if err := pdnsMgr.Expose(domain, port, useSSL, true); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			proto := "http"
			if useSSL {
				proto = "https"
			}
			fmt.Printf("✓ Berhasil! Aplikasi dapat diakses di: %s://%s\n", proto, domain)

		case "expose-site":
			if len(args) < 4 {
				fmt.Println("Penggunaan: wdev pdns expose-site <local_domain> <public_domain> [--ssl]")
				os.Exit(1)
			}
			localDom := args[2]
			pubDom := args[3]
			useSSL := false
			for _, arg := range args[4:] {
				if arg == "--ssl" {
					useSSL = true
				}
			}
			fmt.Printf("Mengekspos vhost lokal '%s' ke domain publik '%s'...\n", localDom, pubDom)
			if err := pdnsMgr.ExposeSite(localDom, pubDom, useSSL); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			proto := "http"
			if useSSL {
				proto = "https"
			}
			fmt.Printf("✓ Berhasil! Vhost lokal dapat diakses di: %s://%s\n", proto, pubDom)

		case "unexpose":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev pdns unexpose <domain> [--keep-dns]")
				os.Exit(1)
			}
			domain := args[2]
			keepDNS := false
			for _, arg := range args[3:] {
				if arg == "--keep-dns" {
					keepDNS = true
				}
			}
			fmt.Printf("Menghapus eksposur untuk '%s'...\n", domain)
			if err := pdnsMgr.Unexpose(domain, keepDNS); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Eksposur untuk '%s' berhasil dihapus.\n", domain)

		default:
			fmt.Printf("Subperintah pdns tidak dikenal: %s\n", args[1])
			fmt.Println("Jalankan 'wdev help' untuk daftar perintah lengkap.")
		}

	case "install":
		if len(args) < 2 {
			fmt.Println("Penggunaan: wdev install <komponen|update>")
			fmt.Println("Komponen tersedia:")
			fmt.Println("  all, update, self-check, base, node, rust, go, java, android,")
			fmt.Println("  flutter, postgres, redis, nginx, php, composer, ssl, php-stack,")
			fmt.Println("  cloudflare, zsh, orca-desktop, orca-headless, orca-uninstall")
			os.Exit(1)
		}
		comp := strings.TrimPrefix(args[1], "--")
		var fn func(ctx context.Context, cb func(string)) error
		taskName := "Instal " + comp

		switch comp {
		case "all", "full", "full-install":
			taskName = "Full Auto Setup (14 Langkah)"
			fn = inst.RunFullInstall
		case "update", "run-update":
			taskName = "Perbarui Seluruh Paket & Toolchain"
			fn = inst.RunUpdateAll
		case "check-updates", "check_updates", "updates", "cu":
			taskName = "Pemeriksaan Ketersediaan Pembaruan"
			fn = inst.CheckUpdates
		case "self-check":
			taskName = "Validasi & Self-Check Sistem"
			fn = inst.RunSelfCheck
		case "base":
			taskName = "Dependensi Dasar Sistem"
			fn = inst.InstallBase
		case "node":
			taskName = "Node.js LTS"
			fn = func(ctx context.Context, cb func(string)) error {
				ver := "22"
				if len(args) >= 3 {
					ver = args[2]
				}
				return inst.InstallNode(ctx, cb, ver)
			}
		case "rust":
			taskName = "Rust Toolchain & Mold"
			fn = inst.InstallRust
		case "go", "golang":
			taskName = "Golang"
			fn = inst.InstallGolang
		case "java":
			taskName = "OpenJDK Java"
			fn = func(ctx context.Context, cb func(string)) error {
				ver := "21"
				if len(args) >= 3 {
					ver = args[2]
				}
				return inst.InstallJava(ctx, cb, ver)
			}
		case "android":
			taskName = "Android SDK & Tools"
			fn = inst.InstallAndroidSDK
		case "flutter":
			taskName = "Flutter SDK"
			fn = inst.InstallFlutter
		case "postgres", "pg":
			taskName = "PostgreSQL Server"
			fn = func(ctx context.Context, cb func(string)) error {
				ver := "16"
				if len(args) >= 3 {
					ver = args[2]
				}
				return inst.InstallPostgres(ctx, cb, ver)
			}
		case "redis":
			taskName = "Redis Cache Server"
			fn = inst.InstallRedis
		case "nginx":
			taskName = "Nginx Web Server"
			fn = inst.InstallNginx
		case "php":
			taskName = "PHP & PHP-FPM"
			fn = func(ctx context.Context, cb func(string)) error {
				ver := "8.4"
				if len(args) >= 3 {
					ver = args[2]
				}
				return inst.InstallPHP(ctx, cb, ver)
			}
		case "composer":
			taskName = "Composer Package Manager"
			fn = inst.InstallComposer
		case "ssl", "mkcert":
			taskName = "Local SSL (mkcert)"
			fn = inst.InstallMkcert
		case "php-stack", "web-stack":
			taskName = "Full PHP Web Stack"
			fn = inst.InstallPhpStack
		case "cloudflare":
			taskName = "Cloudflare Tunnel (cloudflared)"
			fn = inst.InstallCloudflared
		case "zsh":
			taskName = "Oh My Zsh"
			fn = func(ctx context.Context, cb func(string)) error {
				theme := "powerlevel10k"
				if len(args) >= 3 {
					theme = args[2]
				}
				return inst.InstallZsh(ctx, cb, theme)
			}
		case "orca", "orca-desktop":
			taskName = "Orca ADE (Desktop GUI)"
			fn = func(ctx context.Context, cb func(string)) error {
				return inst.InstallOrca(ctx, cb, "desktop")
			}
		case "orca-headless":
			taskName = "Orca ADE (Headless Server)"
			fn = func(ctx context.Context, cb func(string)) error {
				return inst.InstallOrca(ctx, cb, "headless")
			}
		case "orca-uninstall":
			taskName = "Uninstall Orca ADE"
			fn = func(ctx context.Context, cb func(string)) error {
				return inst.UninstallOrca(ctx, cb, false)
			}
		default:
			fmt.Fprintf(os.Stderr, "Komponen tidak dikenal: %s\n", comp)
			os.Exit(1)
		}

		fmt.Printf("==> Menjalankan: %s\n\n", taskName)
		ctx := context.Background()
		cb := func(line string) {
			fmt.Println(line)
		}
		err := fn(ctx, cb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[ERROR] %s gagal: %v\n", taskName, err)
			os.Exit(1)
		}
		fmt.Printf("\n✓ %s selesai dengan sukses!\n", taskName)

	case "-v", "--version", "version":
		fmt.Printf("wdev version %s\n", Version)

	case "-h", "--help", "help":
		printHelp()

	default:
		fmt.Printf("Perintah tidak dikenal: %s\n\n", args[0])
		printHelp()
		os.Exit(1)
	}
}
