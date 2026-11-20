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
	"wdev/internal/edge"
	"wdev/internal/installer"
	"wdev/internal/mail"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/server"
	"wdev/internal/site"
	"wdev/internal/store"
	"wdev/internal/switcher"
	"wdev/internal/tui"
	"wdev/internal/updater"
	"wdev/internal/version"
)

var Version = version.Version

func printHelp() {
	fmt.Printf(`wdev v%s — Linux Developer Control Plane

PENGGUNAAN:
  wdev [perintah] [argumen...]

ANTARMUKA:
  wdev serve, wdev web      Jalankan Web UI Dashboard (default: http://localhost:3200)
    --port <port>           Tentukan port HTTP (default: 3200)
    --host <host>           Tentukan host binding (default: 0.0.0.0)
  wdev tui                  Jalankan TUI Terminal Dashboard interaktif

MANAJEMEN LAYANAN & SYSTEMD:
  wdev status, wdev check              Tampilkan status cepat sistem, IP, dan layanan inti
  wdev check-updates, -cu              Periksa ketersediaan pembaruan toolchain & paket sistem
  wdev service install [--port 3200]   Pasang wdev sebagai systemd background service (auto-start)
  wdev service uninstall [--purge]     Hapus systemd service wdev dari sistem
  wdev service status [nama]           Tampilkan status layanan (default: wdev)
  wdev service <nama> <aksi>           Start/Stop/Restart layanan (contoh: wdev service nginx restart)

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
  wdev pdns deploy <ip> [--user u] [--port p] [--key path]  Deploy otomatis ke VPS via SSH
  wdev pdns connect <netbird_ip> <pub_ip> <api_key>          Hubungkan manual ke VPS Edge
  wdev pdns exposures                      Daftar proxy domain aktif di Edge Gateway
  wdev pdns expose <dom> <port> [--ssl]    Ekspos port ke domain publik via VPS Edge
  wdev pdns expose-site <loc> <pub> [--ssl] Ekspos vhost lokal (PHP/Laravel) via VPS Edge
  wdev pdns unexpose <dom> [--keep-dns]    Hapus eksposur dari Edge Gateway

MAIL SERVER & RELAY:
  wdev mail status                         Status Stalwart, SnappyMail, dan relay per domain
  wdev mail accounts [domain]              Daftar akun email dikelompokkan per domain
  wdev mail add-account <email> <password> Buat kotak pos / akun email baru
 wdev mail set-relay <domain|global> <provider> Setel relay (resend, brevo, custom, off)
  wdev mail admin-reset <domain>           Reset password admin Stalwart untuk domain
 wdev mail unset-relay <domain> Hapus override relay domain (fallback default)
 wdev mail admin-cred <domain> Tampilkan kredensial admin Stalwart domain

INSTALASI & SYSTEMD SERVICE:
  wdev install [service]               Pasang wdev ke sistem & aktifkan systemd service (auto-start)
  wdev uninstall [--purge]             Hapus systemd service wdev & binary dari sistem
  wdev install <komponen>              Pasang atau perbarui komponen (update, node, php, postgres, dll.)
`, Version)
}

func relayModeLabel(relayOnly bool) string {
	if relayOnly {
		return "relay-only (direct/port 25 nonaktif)"
	}
	return "belum relay-only (periksa konfigurasi relay)"
}

func printZoneChecks(report pdns.ZoneDoctorReport) {
	for _, c := range report.Checks {
		icon := "✓"
		switch c.Status {
		case pdns.CheckWarn:
			icon = "!"
		case pdns.CheckFail:
			icon = "✗"
		}
		fmt.Printf(" %s %-26s %s\n", icon, c.Name, c.Detail)
	}
}

func main() {
	// Launcher legacy (orca, orca-pair, orca-switch, orca-update, orca-desktop,
	// orca-ide) kini diarahkan ke biner Go ini tanpa skrip shell.
	if installer.HandleOrcaProgram(os.Args[0], os.Args[1:]) {
		return
	}

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
	mailMgr := mail.New(cfg, db, pdnsMgr)

	args := os.Args[1:]
	if len(args) == 0 {
		// If interactive terminal, launch TUI by default
		t := tui.New(cfg, db, inst, siteMgr, swMgr, cfMgr, pdnsMgr)
		t.Run()
		return
	}

	switch args[0] {
	case "orca":
		installer.HandleOrcaProgram("orca", args[1:])
		return
	case "env":
		sub := "status"
		if len(args) > 1 {
			sub = args[1]
		}
		switch sub {
		case "status":
			fmt.Println("=== STATUS ENVIRONMENT TOOLCHAIN (PATH) ===")
			for _, line := range installer.ToolchainEnvReport() {
				fmt.Println(line)
			}
			if entries := installer.EffectivePathPreview(context.Background()); len(entries) > 0 {
				fmt.Println("\nPATH efektif (login shell, unik):")
				for _, p := range entries {
					fmt.Println("  " + p)
				}
			}
		case "sync":
			cb := func(line string) { fmt.Println(line) }
			if err := inst.SyncToolchainEnv(context.Background(), cb); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Println("Penggunaan: wdev env <status|sync>")
			os.Exit(1)
		}
		return
	case "edge-agent":
		sub := "serve"
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			sub = args[1]
		}
		cb := func(line string) { fmt.Println(line) }
		switch sub {
		case "serve":
			addr := "0.0.0.0:" + edge.DefaultAgentPort
			for i := 1; i < len(args); i++ {
				if args[i] == "--addr" && i+1 < len(args) {
					addr = args[i+1]
					i++
				}
			}
			if err := edge.RunAgent(addr, cb); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		case "provision":
			opts := edge.ProvisionOptions{}
			confirmed := false
			for i := 1; i < len(args); i++ {
				switch args[i] {
				case "--help", "-h":
					fmt.Println("Penggunaan: wdev edge-agent provision --yes [opsi]")
					fmt.Println("")
					fmt.Println("Memasang PowerDNS + Nginx + agent Go di VPS ini (butuh root).")
					fmt.Println("Operasi ini mengubah sistem: paket, port 53, Nginx, firewall, systemd.")
					fmt.Println("")
					fmt.Println("Opsi:")
					fmt.Println("  --yes              konfirmasi menjalankan provisioning (wajib)")
					fmt.Println("  --public-ip <ip>   paksa IPv4 publik VPS")
					fmt.Println("  --netbird-ip <ip>  paksa IP Netbird (wt0)")
					fmt.Println("  --api-key <key>    pakai API key PowerDNS tertentu")
					fmt.Println("  --pdns-port <n>    port REST API PowerDNS (default 8081)")
					fmt.Println("  --agent-port <n>   port agent edge (default 8082)")
					return
				case "--yes", "-y":
					confirmed = true
				case "--public-ip":
					if i+1 < len(args) {
						opts.PublicIP = args[i+1]
						i++
					}
				case "--netbird-ip":
					if i+1 < len(args) {
						opts.NetbirdIP = args[i+1]
						i++
					}
				case "--api-key":
					if i+1 < len(args) {
						opts.APIKey = args[i+1]
						i++
					}
				case "--pdns-port":
					if i+1 < len(args) {
						opts.PDNSPort = args[i+1]
						i++
					}
				case "--agent-port":
					if i+1 < len(args) {
						opts.AgentPort = args[i+1]
						i++
					}
				}
			}
			if !confirmed {
				fmt.Fprintln(os.Stderr, "Menolak menjalankan provisioning tanpa konfirmasi.")
				fmt.Fprintln(os.Stderr, "Gunakan: wdev edge-agent provision --yes  (atau lewat: wdev pdns deploy <ip>)")
				os.Exit(2)
			}
			if err := edge.Provision(context.Background(), cb, opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Println("Penggunaan: wdev edge-agent <serve|provision> [opsi]")
			os.Exit(1)
		}
		return
	case "serve", "web", "dashboard":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		portFlag := fs.String("port", cfg.HTTPPort, "Port HTTP untuk Web Dashboard")
		hostFlag := fs.String("host", cfg.HTTPHost, "Host binding")
		_ = fs.Parse(args[1:])

		cfg.HTTPPort = *portFlag
		cfg.HTTPHost = *hostFlag

		srv := server.New(cfg, db, runMgr, inst, siteMgr, swMgr, cfMgr, pdnsMgr, mailMgr)
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
			verStr := ""
			if s.Version != "" {
				verStr = " (" + s.Version + ")"
			}
			fmt.Printf("  - %-34s : %s%s\n", s.DisplayName, status, verStr)
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
		if len(args) < 2 {
			fmt.Println("Penggunaan: wdev service <install|uninstall|status|start|stop|restart|reload> [nama/opsi]")
			os.Exit(1)
		}
		sub := args[1]
		switch sub {
		case "install":
			port := cfg.HTTPPort
			host := cfg.HTTPHost
			for i := 2; i < len(args); i++ {
				if args[i] == "--port" && i+1 < len(args) {
					port = args[i+1]
					i++
				} else if args[i] == "--host" && i+1 < len(args) {
					host = args[i+1]
					i++
				}
			}
			ctx := context.Background()
			cb := func(line string) { fmt.Println(line) }
			if err := inst.InstallWdevService(ctx, cb, port, host); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		case "uninstall":
			purge := false
			for i := 2; i < len(args); i++ {
				if args[i] == "--purge" || args[i] == "-p" {
					purge = true
				}
			}
			ctx := context.Background()
			cb := func(line string) { fmt.Println(line) }
			if err := inst.UninstallWdevService(ctx, cb, purge); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		case "status":
			name := "wdev"
			if len(args) >= 3 {
				name = args[2]
			}
			if name == "wdev" || name == "wdev.service" {
				st, err := inst.GetWdevServiceStatus()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Println(st)
			} else {
				st := monitor.GetServiceStatus(name)
				activeStr := "Nonaktif"
				if st.Active {
					activeStr = "Aktif (Running)"
				}
				fmt.Printf("Layanan: %s (%s)\nStatus : %s\nVersi  : %s\nPort   : %s\n", st.DisplayName, st.Name, activeStr, st.Version, st.Port)
			}
		case "start", "stop", "restart", "reload":
			if len(args) < 3 {
				fmt.Printf("Penggunaan: wdev service %s <nama_layanan>\n", sub)
				os.Exit(1)
			}
			name := args[2]
			if err := monitor.ControlService(name, sub); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Layanan %s berhasil di-%s\n", name, sub)
		default:
			// Backward compatibility: wdev service <nama> <action>
			if len(args) >= 3 {
				name, action := args[1], args[2]
				if err := monitor.ControlService(name, action); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("✓ Layanan %s berhasil di-%s\n", name, action)
			} else {
				fmt.Printf("Perintah service tidak dikenal: %s\n", sub)
				fmt.Println("Gunakan: wdev service <install|uninstall|status|start|stop|restart|reload>")
				os.Exit(1)
			}
		}

	case "install-service":
		port := cfg.HTTPPort
		host := cfg.HTTPHost
		for i := 1; i < len(args); i++ {
			if args[i] == "--port" && i+1 < len(args) {
				port = args[i+1]
				i++
			} else if args[i] == "--host" && i+1 < len(args) {
				host = args[i+1]
				i++
			}
		}
		ctx := context.Background()
		cb := func(line string) { fmt.Println(line) }
		if err := inst.InstallWdevService(ctx, cb, port, host); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "uninstall-service":
		purge := false
		for i := 1; i < len(args); i++ {
			if args[i] == "--purge" || args[i] == "-p" {
				purge = true
			}
		}
		ctx := context.Background()
		cb := func(line string) { fmt.Println(line) }
		if err := inst.UninstallWdevService(ctx, cb, purge); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

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
			if len(args) == 3 && (args[2] == "doctor" || args[2] == "repair") {
				fmt.Println("Penggunaan: wdev pdns zone " + args[2] + " <domain>")
				os.Exit(1)
			}
			if len(args) >= 4 && args[2] == "doctor" {
				report, err := pdnsMgr.DoctorZone(args[3])
				for _, c := range report.Checks {
					icon := "✓"
					switch c.Status {
					case pdns.CheckWarn:
						icon = "!"
					case pdns.CheckFail:
						icon = "✗"
					}
					fmt.Printf(" %s %-26s %s\n", icon, c.Name, c.Detail)
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				if !report.OK {
					os.Exit(1)
				}
				fmt.Println("✓ Zone doctor selesai tanpa kegagalan kritis.")
			} else if len(args) >= 4 && args[2] == "repair" {
				report, err := pdnsMgr.RepairZone(args[3])
				printZoneChecks(report)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				if !report.OK {
					os.Exit(1)
				}
				fmt.Println("✓ Zone diperbaiki dan lolos doctor.")
			} else if len(args) >= 4 && (args[2] == "add" || args[2] == "create") {
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
				if err := pdnsMgr.AddRecordKeep(args[3], args[4], args[5], args[6], ttl); err != nil {
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

		case "connect":
			if len(args) < 5 {
				fmt.Println("Penggunaan: wdev pdns connect <vps_netbird_ip> <vps_public_ip> <api_key> [pdns_port] [agent_port]")
				os.Exit(1)
			}
			pPort := "8081"
			if len(args) >= 6 {
				pPort = args[5]
			}
			aPort := "8082"
			if len(args) >= 7 {
				aPort = args[6]
			}
			opts := pdns.ConnectOptions{
				VPSNetbirdIP: args[2],
				VPSPublicIP:  args[3],
				APIKey:       args[4],
				PDNSPort:     pPort,
				AgentPort:    aPort,
			}
			fmt.Printf("Menghubungkan ke PowerDNS VPS di %s...\n", opts.VPSNetbirdIP)
			if err := pdnsMgr.Connect(opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓ Berhasil terhubung ke VPS Edge Gateway! Konfigurasi tersimpan.")

		case "deploy", "deploy-vps":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev pdns deploy <host_vps> [--user root] [--port 22] [--password pass] [--key path]")
				os.Exit(1)
			}
			opts := pdns.DeployOptions{
				VPSHost:    args[2],
				SSHPort:    22,
				SSHUser:    "root",
				AuthMethod: "password",
			}
			for i := 3; i < len(args); i++ {
				switch args[i] {
				case "--user", "-u":
					if i+1 < len(args) {
						opts.SSHUser = args[i+1]
						i++
					}
				case "--port", "-p":
					if i+1 < len(args) {
						var port int
						fmt.Sscanf(args[i+1], "%d", &port)
						opts.SSHPort = port
						i++
					}
				case "--password", "--pass":
					if i+1 < len(args) {
						opts.Password = args[i+1]
						opts.AuthMethod = "password"
						i++
					}
				case "--key", "-i":
					if i+1 < len(args) {
						opts.KeyPath = args[i+1]
						opts.AuthMethod = "key"
						i++
					}
				}
			}
			if opts.AuthMethod == "password" && opts.Password == "" {
				fmt.Print("Masukkan Password SSH: ")
				var pass string
				fmt.Scanln(&pass)
				opts.Password = pass
			}
			ctx := context.Background()
			cb := func(line string) {
				fmt.Println(line)
			}
			if err := pdnsMgr.DeployVPS(ctx, cb, opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

		default:
			fmt.Printf("Subperintah pdns tidak dikenal: %s\n", args[1])
			fmt.Println("Jalankan 'wdev help' untuk daftar perintah lengkap.")
		}

	case "mail":
		if len(args) < 2 {
			fmt.Println("Penggunaan: wdev mail [setup|status|accounts|add-account|delete-account|set-relay|unset-relay|admin-reset|admin-cred]")
			os.Exit(1)
		}
		switch args[1] {
		case "setup":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev mail setup <domain> [--no-ssl] [--relay <provider>] [--relay-key <key>] [--relay-host <host>] [--relay-port <port>] [--relay-user <user>] [--relay-pass <pass>]")
				os.Exit(1)
			}
			opts := mail.SetupMailOptions{Domain: args[2], SSL: true}
			for n := 3; n < len(args); n++ {
				switch args[n] {
				case "--no-ssl":
					opts.SSL = false
				case "--relay":
					if n+1 < len(args) {
						opts.RelayProv = args[n+1]
						n++
					}
				case "--relay-key":
					if n+1 < len(args) {
						opts.RelayPass = args[n+1]
						n++
					}
				case "--relay-host":
					if n+1 < len(args) {
						opts.RelayHost = args[n+1]
						n++
					}
				case "--relay-port":
					if n+1 < len(args) {
						opts.RelayPort, _ = strconv.Atoi(args[n+1])
						n++
					}
				case "--relay-user":
					if n+1 < len(args) {
						opts.RelayUser = args[n+1]
						n++
					}
				case "--relay-pass":
					if n+1 < len(args) {
						opts.RelayPass = args[n+1]
						n++
					}
				}
			}
			if err := mailMgr.SetupMail(context.Background(), func(line string) { fmt.Println(line) }, opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return

		case "status":
			st, err := mailMgr.GetStatus()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("=== Status Mail Server & Relay ===")
			fmt.Printf("Stalwart Mail Server: %v\n", st.StalwartActive)
			fmt.Printf("SnappyMail Webmail:   %v\n", st.SnappyMailActive)
			fmt.Printf("Free Storage:         %s\n", st.StorageFree)
			fmt.Printf("Mode Outbound: %s\n", relayModeLabel(st.RelayOnly))
			if st.GlobalRelay != nil {
				fmt.Printf("Global Relay:         %s (%s:%d)\n", st.GlobalRelay.Provider, st.GlobalRelay.Host, st.GlobalRelay.Port)
			} else {
				fmt.Printf("Global Relay:         (belum dikonfigurasi)\n")
			}
			fmt.Println("\nRelay per Domain:")
			if len(st.DomainRelays) == 0 {
				fmt.Println("  (Belum ada konfigurasi domain relay)")
			} else {
				for dom, r := range st.DomainRelays {
					fmt.Printf("  - %s: %s via %s:%d (User: %s)\n", dom, strings.ToUpper(r.Provider), r.Host, r.Port, r.Username)
				}
			}
			fmt.Println("\nDomain Terdaftar:")
			for _, d := range st.Domains {
				fmt.Printf("  - %s (Admin: %s, Webmail: %s)\n", d.Domain, d.AdminURL, d.Webmail)
			}

		case "accounts", "list":
			filter := ""
			if len(args) >= 3 {
				filter = args[2]
			}
			accs, err := mailMgr.GetDomainAccounts(filter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			for _, d := range accs {
				relayStr := "Relay Default"
				if d.Relay != nil {
					relayStr = fmt.Sprintf("%s (%s:%d)", strings.ToUpper(d.Relay.Provider), d.Relay.Host, d.Relay.Port)
				}
				fmt.Printf("\n=== Domain: %s (Relay: %s) ===\n", d.Domain, relayStr)
				fmt.Printf("Webmail: %s | Admin: %s\n", d.Webmail, d.AdminURL)
				if len(d.Accounts) == 0 {
					fmt.Println("  (Tidak ada akun)")
				} else {
					for _, a := range d.Accounts {
						role := "User"
						if a.IsAdmin {
							role = "Admin"
						}
						fmt.Printf("  - %s [%s]\n", a.Email, role)
					}
				}
			}

		case "add-account":
			if len(args) < 4 {
				fmt.Println("Penggunaan: wdev mail add-account <email> <password>")
				os.Exit(1)
			}
			if err := mailMgr.AddAccount(args[2], args[3]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Akun %s berhasil dibuat!\n", args[2])

		case "relay-remove", "unset-relay":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev mail unset-relay <domain>")
				os.Exit(1)
			}
			if err := mailMgr.RemoveRelay(args[2]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Override relay domain %s dihapus; memakai relay default.\n", args[2])

		case "set-relay":
			if len(args) < 4 {
				fmt.Println("Penggunaan: wdev mail set-relay <domain|global> <provider: resend|brevo|custom|off> [host] [port] [user] [pass]")
				os.Exit(1)
			}
			dom := args[2]
			prov := strings.ToLower(args[3])
			opts := mail.SetRelayOptions{Domain: dom, Provider: prov}
			if prov == "resend" || prov == "brevo" {
				if len(args) >= 5 {
					opts.Password = args[4]
				} else {
					fmt.Printf("Masukkan API Key / Password untuk %s: ", prov)
					var p string
					fmt.Scanln(&p)
					opts.Password = p
				}
			} else if prov == "custom" {
				if len(args) >= 8 {
					opts.Host = args[4]
					opts.Port, _ = strconv.Atoi(args[5])
					opts.Username = args[6]
					opts.Password = args[7]
				} else {
					fmt.Println("Untuk custom relay: wdev mail set-relay <domain> custom <host> <port> <user> <pass>")
					os.Exit(1)
				}
			}
			if err := mailMgr.SetRelay(opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Relay untuk domain %s berhasil disetel ke %s!\n", dom, prov)

		case "admin-cred":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev mail admin-cred <domain>")
				os.Exit(1)
			}
			email, pass, err := mailMgr.AdminCredential(args[2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Domain: %s\nUsername: %s\nPassword: %s\n", args[2], email, pass)

		case "admin-reset":
			if len(args) < 3 {
				fmt.Println("Penggunaan: wdev mail admin-reset <domain>")
				os.Exit(1)
			}
			if err := mailMgr.ResetAdmin(args[2]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Password admin untuk domain %s berhasil direset!\n", args[2])

		default:
			fmt.Printf("Subperintah mail tidak dikenal: %s\n", args[1])
			fmt.Println("Pilihan: status, accounts, add-account, set-relay, unset-relay, admin-reset")
		}

	case "install":
		// Jika dipanggil tanpa argumen atau dengan "service" / "self",
		// jalankan instalasi biner & systemd service wdev secara mandiri!
		if len(args) < 2 || args[1] == "service" || args[1] == "self" {
			port := cfg.HTTPPort
			host := cfg.HTTPHost
			for i := 1; i < len(args); i++ {
				if args[i] == "--port" && i+1 < len(args) {
					port = args[i+1]
					i++
				} else if args[i] == "--host" && i+1 < len(args) {
					host = args[i+1]
					i++
				}
			}
			ctx := context.Background()
			cb := func(line string) { fmt.Println(line) }
			if err := inst.InstallWdevService(ctx, cb, port, host); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
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
		case "audit", "audit-toolchain":
			taskName = "Audit Duplikat Path & Toolchain"
			fn = func(ctx context.Context, cb func(string)) error {
				findings := inst.AuditToolchain(ctx, cb)
				for _, f := range findings {
					if f.Severity != installer.SeverityOK {
						cb("→ perbaiki otomatis: wdev install audit-fix")
						break
					}
				}
				return nil
			}
		case "audit-fix", "repair-toolchain":
			taskName = "Perbaikan Duplikat Path & Toolchain"
			fn = func(ctx context.Context, cb func(string)) error {
				findings := inst.AuditToolchain(ctx, cb)
				return inst.RepairToolchain(ctx, cb, findings)
			}
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
		case "mail", "mail-stack":
			taskName = "Mail Stack (Stalwart + SnappyMail)"
			fn = inst.InstallMailStack
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
		case "orca-update":
			taskName = "Pembaruan Orca ADE"
			fn = func(ctx context.Context, cb func(string)) error {
				return inst.UpdateOrca(ctx, cb)
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

	case "uninstall":
		purge := false
		for i := 1; i < len(args); i++ {
			if args[i] == "--purge" || args[i] == "-p" {
				purge = true
			}
		}
		ctx := context.Background()
		cb := func(line string) { fmt.Println(line) }
		if err := inst.UninstallWdevService(ctx, cb, purge); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "update", "self-update", "upgrade":
		opts := updater.Options{}
		for n := 1; n < len(args); n++ {
			switch args[n] {
			case "--check", "-c":
				opts.CheckOnly = true
			case "--force", "-f":
				opts.Force = true
			case "--edge":
				if n+1 < len(args) {
					opts.EdgeHost = args[n+1]
					n++
				}
			case "--edge-key":
				if n+1 < len(args) {
					opts.EdgeKey = args[n+1]
					n++
				}
			case "--source":
				if n+1 < len(args) {
					opts.SourceDir = args[n+1]
					n++
				}
			}
		}
		res, err := updater.Run(context.Background(), Version, opts, func(line string) { fmt.Println(line) })
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if opts.CheckOnly {
			if res.HasUpdate {
				fmt.Printf("Update tersedia: v%s -> v%s\n", res.Current, res.Latest)
				fmt.Println("Jalankan: sudo wdev update")
			} else {
				fmt.Printf("wdev v%s sudah terbaru\n", res.Current)
			}
		}

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
