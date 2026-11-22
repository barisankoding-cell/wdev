package tui

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/site"
	"wdev/internal/store"
	"wdev/internal/switcher"
)

// ANSI Styling
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
)

type TUI struct {
	cfg     *config.Config
	store   *store.Store
	inst    *installer.Installer
	siteMgr *site.Manager
	swMgr   *switcher.Manager
	cfMgr   *cloudflare.Manager
	pdnsMgr *pdns.Manager
	reader  *bufio.Reader
}

func New(
	cfg *config.Config,
	s *store.Store,
	inst *installer.Installer,
	sm *site.Manager,
	sw *switcher.Manager,
	cf *cloudflare.Manager,
	pdns *pdns.Manager,
) *TUI {
	return &TUI{
		cfg:     cfg,
		store:   s,
		inst:    inst,
		siteMgr: sm,
		swMgr:   sw,
		cfMgr:   cf,
		pdnsMgr: pdns,
		reader:  bufio.NewReader(os.Stdin),
	}
}

func (t *TUI) Clear() {
	fmt.Print("\033[H\033[2J")
}

func (t *TUI) waitEnter() {
	fmt.Printf("\n%sTekan [ENTER] untuk kembali...%s", Dim, Reset)
	_, _ = t.reader.ReadString('\n')
}

func (t *TUI) prompt(msg string) string {
	fmt.Printf("%s%s%s", Bold, msg, Reset)
	line, _ := t.reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func (t *TUI) runTask(name string, fn func(ctx context.Context, cb installer.LogCallback) error) {
	fmt.Printf("\n%s%s==> Memulai: %s%s\n\n", Bold, Cyan, name, Reset)
	ctx := context.Background()
	cb := func(line string) {
		fmt.Println(line)
	}
	err := fn(ctx, cb)
	if err != nil {
		fmt.Printf("\n%s%s[FAIL] %s: %v%s\n", Bold, Red, name, err, Reset)
	} else {
		fmt.Printf("\n%s%s[OK] %s selesai dengan sukses!%s\n", Bold, Green, name, Reset)
	}
	t.waitEnter()
}

func (t *TUI) Run() {
	for {
		t.Clear()
		t.renderHeader()
		t.renderSystemSummary()
		t.renderServices()
		t.renderMenu()

		choice := t.prompt("Pilih kategori [0-9]: ")

		switch choice {
		case "1":
			t.menuStatusUpdates()
		case "2":
			t.menuMultiVersion()
		case "3":
			t.menuWebStack()
		case "4":
			t.menuToolchains()
		case "5":
			t.menuWorkspaceShell()
		case "6":
			t.menuCloudflare()
		case "7":
			t.menuPowerDNS()
		case "8":
			t.menuFullInstall()
		case "9":
			t.menuModularInstall()
		case "0", "q", "Q", "exit":
			fmt.Println("\nSampai jumpa!")
			return
		default:
			// Refresh
		}
	}
}

func (t *TUI) renderHeader() {
	fmt.Printf("%s%s========================================================================%s\n", Bold, Blue, Reset)
	fmt.Printf("%s%s   wdev — LINUX DEVELOPER ENVIRONMENT & CONTROL PLANE                  %s\n", Bold, White, Reset)
	fmt.Printf("%s%s========================================================================%s\n", Bold, Blue, Reset)
}

func (t *TUI) renderSystemSummary() {
	stats, err := monitor.Collect()
	if err != nil {
		fmt.Printf("%s[Gagal memuat status sistem]%s\n", Red, Reset)
		return
	}
	if t.pdnsMgr != nil {
		stats.ServerMode = t.pdnsMgr.GetMode()
		cfg := t.pdnsMgr.LoadConfig()
		if cfg.VPSPublicIP != "" && pdns.IsPublicIPv4String(cfg.VPSPublicIP) {
			stats.PublicIP = cfg.VPSPublicIP
		}
	}

	hasNetbird := false
	for _, s := range stats.Services {
		if s.Name == "netbird" && s.Installed {
			hasNetbird = true
			break
		}
	}

	netLabel := "NetBird:"
	netVal := stats.NetbirdIP
	if stats.ServerMode == "vps" || (stats.PublicIP != "" && stats.PublicIP != "-" && stats.PublicIP != "127.0.0.1" && pdns.IsPublicIPv4String(stats.PublicIP)) {
		netLabel = "IP VPS: "
		if stats.PublicIP != "" && pdns.IsPublicIPv4String(stats.PublicIP) {
			netVal = stats.PublicIP
		} else {
			netVal = "-"
		}
	} else if !hasNetbird {
		netLabel = ""
		netVal = ""
	}

	fmt.Printf(" %sOS:%s %-25s  %sUptime:%s %s\n", Bold, Reset, stats.OSName, Bold, Reset, stats.UptimeString)
	fmt.Printf(" %sRAM:%s %d/%d MB (%d%%)       %sDisk:%s   %d/%d GB (%d%%)\n",
		Bold, Reset, stats.MemUsedMB, stats.MemTotalMB, stats.MemPercent,
		Bold, Reset, stats.DiskUsedGB, stats.DiskTotalGB, stats.DiskPercent)
	if netLabel != "" {
		fmt.Printf(" %sLAN IP:%s %-22s  %s%s%s %s\n", Bold, Reset, stats.LanIP, Bold, Reset, netLabel, netVal)
	} else {
		fmt.Printf(" %sLAN IP:%s %s\n", Bold, Reset, stats.LanIP)
	}
	fmt.Printf("%s------------------------------------------------------------------------%s\n", Dim, Reset)
}

func (t *TUI) renderServices() {
	stats, err := monitor.Collect()
	if err != nil {
		return
	}

	fmt.Printf("%s%sSTATUS LAYANAN:%s\n", Bold, Yellow, Reset)
	for _, s := range stats.Services {
		badge := fmt.Sprintf("%s%s● AKTIF%s", Bold, Green, Reset)
		if !s.Active {
			badge = fmt.Sprintf("%s%s○ NONAKTIF%s", Bold, Red, Reset)
		}
		portStr := ""
		if s.Port != "" {
			portStr = fmt.Sprintf("(%s)", s.Port)
		}
		nameWithVer := s.DisplayName
		if s.Version != "" {
			nameWithVer = fmt.Sprintf("%s [%s]", s.DisplayName, s.Version)
		}
		fmt.Printf("  %-34s %-20s %s\n", nameWithVer, portStr, badge)
	}
	fmt.Printf("%s------------------------------------------------------------------------%s\n", Dim, Reset)
}

func (t *TUI) renderMenu() {
	fmt.Printf("%s%sMENU UTAMA (9 KATEGORI):%s\n", Bold, Yellow, Reset)
	fmt.Printf("  %s%s[1]%s 📊 Status & Pembaruan Sistem\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Cek status layanan, cek update versi, perbarui sistem, self-check%s\n", Dim, Reset)
	fmt.Printf("  %s%s[2]%s 🔄 Pengelola Multi-Versi Runtime (Switchers)\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Beralih versi Node.js, PHP CLI/FPM, PostgreSQL & Java OpenJDK%s\n", Dim, Reset)
	fmt.Printf("  %s%s[3]%s 🌐 Web Stack & Domain Lokal (wdev site)\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Kelola Virtual Host, Nginx, PHP-FPM, Local SSL (mkcert)%s\n", Dim, Reset)
	fmt.Printf("  %s%s[4]%s 🛠️  Toolchains, Compilers & SDKs\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Rust & Sccache, Golang, Java, Flutter & Android SDK, Redis%s\n", Dim, Reset)
	fmt.Printf("  %s%s[5]%s 💻 Workspace, ADE & Shell\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Orca ADE (Desktop / Headless), Oh My Zsh & Tema Prompt%s\n", Dim, Reset)
	fmt.Printf("  %s%s[6]%s ☁️  Cloudflare Tunnel & DNS\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Akun, tunnel, eksposur subdomain, DNS, service & diagnostik%s\n", Dim, Reset)
	fmt.Printf("  %s%s[7]%s 🌐 PowerDNS & VPS Edge Gateway (Netbird)\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Authoritative DNS mandiri, domain publik & eksposur port aplikasi%s\n", Dim, Reset)
	fmt.Printf("  %s%s[8]%s 🚀 Instalasi Lengkap (Full Auto Setup)\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Instal seluruh stack developer secara otomatis dari nol (14 langkah)%s\n", Dim, Reset)
	fmt.Printf("  %s%s[9]%s 📦 Instalasi Komponen Modular (19 Item)\n", Bold, Cyan, Reset)
	fmt.Printf("      %s└─ Instal atau perbarui komponen tertentu sesuai kebutuhan%s\n", Dim, Reset)
	fmt.Printf("  %s%s[0]%s 🚪 Keluar\n", Bold, Yellow, Reset)
	fmt.Println()
}

// 1. STATUS & PEMBARUAN SISTEM
func (t *TUI) menuStatusUpdates() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [1] STATUS & PEMBARUAN SISTEM ===%s\n\n", Bold, Blue, Reset)
		fmt.Printf("  %s[1]%s Cek Informasi & Status Detail Layanan\n", Bold, Reset)
		fmt.Printf("  %s[2]%s Cek Ketersediaan Pembaruan Versi Sistem\n", Bold, Reset)
		fmt.Printf("  %s[3]%s Perbarui Seluruh Paket & Toolchain (Update All)\n", Bold, Reset)
		fmt.Printf("  %s[4]%s Validasi Sintaks & Pemeriksaan Sistem (Self-Check)\n", Bold, Reset)
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-4]: ")
		switch c {
		case "1":
			t.Clear()
			t.renderHeader()
			t.renderSystemSummary()
			t.renderServices()
			info := t.swMgr.GetAll()
			fmt.Printf("\n%s%sRUNTIME AKTIF:%s\n", Bold, Yellow, Reset)
			for name, item := range info {
				fmt.Printf("  - %-12s : %s (Tersedia: %s)\n", strings.ToUpper(name), item.ActiveVersion, strings.Join(item.AvailableVersions, ", "))
			}
			t.waitEnter()
		case "2":
			t.runTask("Pemeriksaan Ketersediaan Pembaruan Versi", t.inst.CheckUpdates)
		case "3":
			t.runTask("Perbarui Seluruh Paket & Toolchain", t.inst.RunUpdateAll)
		case "4":
			t.runTask("Validasi & Self-Check Sistem", t.inst.RunSelfCheck)
		case "0", "q", "Q":
			return
		}
	}
}

// 2. MULTI-VERSI RUNTIME
func (t *TUI) menuMultiVersion() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [2] PENGELOLA MULTI-VERSI RUNTIME ===%s\n\n", Bold, Blue, Reset)
		info := t.swMgr.GetAll()

		node := info["node"]
		fmt.Printf("  %s[1]%s 🟢 Node.js       (Aktif: %s%s%s | Tersedia: %s)\n", Bold, Reset, Green, node.ActiveVersion, Reset, strings.Join(node.AvailableVersions, ", "))
		php := info["php"]
		fmt.Printf("  %s[2]%s 🐘 PHP CLI & FPM (Aktif: %s%s%s | Tersedia: %s)\n", Bold, Reset, Green, php.ActiveVersion, Reset, strings.Join(php.AvailableVersions, ", "))
		pg := info["pg"]
		fmt.Printf("  %s[3]%s 🐘 PostgreSQL    (Aktif: %s%s%s | Tersedia: %s)\n", Bold, Reset, Green, pg.ActiveVersion, Reset, strings.Join(pg.AvailableVersions, ", "))
		java := info["java"]
		fmt.Printf("  %s[4]%s ☕ Java OpenJDK  (Aktif: %s%s%s | Tersedia: %s)\n", Bold, Reset, Green, java.ActiveVersion, Reset, strings.Join(java.AvailableVersions, ", "))
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-4]: ")
		switch c {
		case "1":
			ver := t.prompt("Masukkan versi Node.js (contoh: 20, 22, 24): ")
			if ver != "" {
				if err := t.swMgr.SwitchNode(ver); err != nil {
					fmt.Printf("Gagal ganti versi Node: %v\n", err)
				} else {
					fmt.Printf("✓ Berhasil beralih ke Node.js %s\n", ver)
				}
				t.waitEnter()
			}
		case "2":
			ver := t.prompt("Masukkan versi PHP (contoh: 8.1, 8.2, 8.3, 8.4): ")
			if ver != "" {
				if err := t.swMgr.SwitchPHP(ver); err != nil {
					fmt.Printf("Gagal ganti versi PHP: %v\n", err)
				} else {
					fmt.Printf("✓ Berhasil beralih ke PHP %s\n", ver)
				}
				t.waitEnter()
			}
		case "3":
			ver := t.prompt("Masukkan versi PostgreSQL (contoh: 16, 17, 18): ")
			if ver != "" {
				t.runTask("Switch PostgreSQL "+ver, func(ctx context.Context, cb installer.LogCallback) error {
					return t.inst.SwitchPostgres(ctx, cb, ver)
				})
			}
		case "4":
			ver := t.prompt("Masukkan versi Java OpenJDK (contoh: 17, 21): ")
			if ver != "" {
				if err := t.swMgr.SwitchJava(ver); err != nil {
					fmt.Printf("Gagal ganti versi Java: %v\n", err)
				} else {
					fmt.Printf("✓ Berhasil beralih ke Java OpenJDK %s\n", ver)
				}
				t.waitEnter()
			}
		case "0", "q", "Q":
			return
		}
	}
}

// 3. WEB STACK & DOMAIN LOKAL
func (t *TUI) menuWebStack() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [3] WEB STACK & DOMAIN LOKAL (DEV-SITE) ===%s\n\n", Bold, Blue, Reset)
		fmt.Printf("  %s[1]%s 🚀 Instal Full Web Stack (Nginx + PHP + Composer + SSL)\n", Bold, Reset)
		fmt.Printf("  %s[2]%s ➕ Buat Virtual Host Baru (wdev site create)\n", Bold, Reset)
		fmt.Printf("  %s[3]%s ➖ Hapus Virtual Host (wdev site delete)\n", Bold, Reset)
		fmt.Printf("  %s[4]%s 📋 Daftar Virtual Host Aktif (wdev site list)\n", Bold, Reset)
		fmt.Printf("  %s[5]%s 🔄 Sinkronisasi Hosts & SSL (wdev site sync)\n", Bold, Reset)
		fmt.Printf("  %s[6]%s 🐘 Kelola Versi PHP CLI & FPM (wdev switch php)\n", Bold, Reset)
		fmt.Printf("  %s[7]%s 🌐 Ekspos Vhost Lokal ke Internet (Cloudflare / PowerDNS)\n", Bold, Reset)
		fmt.Printf("  %s[8]%s 🔒 Terbitkan SSL Let's Encrypt pada VHost (wdev site ssl)\n", Bold, Reset)
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-8]: ")
		switch c {
		case "1":
			t.runTask("Full PHP Web Stack", t.inst.InstallPhpStack)
		case "2":
			dom := t.prompt("Masukkan nama domain (contoh: myapp.test): ")
			root := t.prompt("Masukkan document root atau port (contoh: /var/www/myapp atau 3000): ")
			ver := t.prompt("Versi PHP [8.4/8.3/proxy]: ")
			fw := t.prompt("Framework [laravel/wordpress/ci4/generic/static/proxy]: ")
			if dom != "" {
				err := t.siteMgr.CreateSite(site.CreateOptions{
					Domain:     dom,
					RootPath:   root,
					PHPVersion: ver,
					Framework:  fw,
					SSLEnabled: true,
				})
				if err != nil {
					fmt.Printf("Gagal membuat vhost: %v\n", err)
				} else {
					fmt.Printf("✓ Virtual host %s berhasil dibuat dengan SSL otomatis.\n", dom)
				}
				t.waitEnter()
			}
		case "3":
			dom := t.prompt("Masukkan domain yang ingin dihapus: ")
			if dom != "" {
				err := t.siteMgr.DeleteSite(dom)
				if err != nil {
					fmt.Printf("Gagal menghapus vhost: %v\n", err)
				} else {
					fmt.Printf("✓ Virtual host %s dihapus.\n", dom)
				}
				t.waitEnter()
			}
		case "4":
			t.Clear()
			sites, _ := t.siteMgr.ListSites()
			fmt.Printf("%-24s %-12s %-12s %-28s %s\n", "DOMAIN", "FRAMEWORK", "RUNTIME", "SSL", "ROOT / TARGET")
			fmt.Println(strings.Repeat("-", 92))
			for _, s := range sites {
				ssl := "HTTP"
				if s.SSLEnabled {
					ssl = "SSL"
					if s.SSLProvider == "letsencrypt" {
						ssl = "SSL (LE)"
					}
					if s.SSLExpiresAt != nil {
						expDate := s.SSLExpiresAt.Local().Format("2006-01-02")
						if s.SSLDaysLeft != nil && *s.SSLDaysLeft >= 0 {
							ssl += fmt.Sprintf(" s/d %s (%dh)", expDate, *s.SSLDaysLeft)
						} else {
							ssl += fmt.Sprintf(" exp %s", expDate)
						}
					}
				}
				runtimeText := "PHP " + s.PHPVersion
				if s.Framework == "static" {
					runtimeText = "Static HTML"
				} else if s.Framework == "proxy" {
					runtimeText = fmt.Sprintf("Port :%d", s.ProxyPort)
				}
				target := s.RootPath
				if s.Framework == "proxy" {
					target = fmt.Sprintf("Port :%d", s.ProxyPort)
				}
				fmt.Printf("%-24s %-12s %-12s %-28s %s\n", s.Domain, strings.ToUpper(s.Framework), runtimeText, ssl, target)
			}
			t.waitEnter()
		case "5":
			if err := t.siteMgr.SyncAll(); err != nil {
				fmt.Printf("Gagal sinkronisasi: %v\n", err)
			} else {
				fmt.Println("✓ Sinkronisasi /etc/hosts, sertifikat SSL, dan reload Nginx berhasil!")
			}
			t.waitEnter()
		case "6":
			ver := t.prompt("Pilih versi PHP aktif [8.1, 8.2, 8.3, 8.4]: ")
			if ver != "" {
				_ = t.swMgr.SwitchPHP(ver)
			}
		case "7":
			fmt.Println("Gunakan Menu [6] Cloudflare Tunnel atau Menu [7] PowerDNS VPS Edge untuk eksposur publik.")
			t.waitEnter()
		case "8":
			dom := t.prompt("Masukkan domain virtual host (contoh: domainanda.com): ")
			if dom != "" {
				email := t.prompt("Email notifikasi Let's Encrypt (opsional, Enter untuk lewati): ")
				t.runTask("SSL Let's Encrypt ("+dom+")", func(ctx context.Context, cb installer.LogCallback) error {
					return t.siteMgr.EnableLetsEncrypt(ctx, cb, dom, email)
				})
			}
		case "0", "q", "Q":
			return
		}
	}
}

// 4. TOOLCHAINS, COMPILERS & SDKS
func (t *TUI) menuToolchains() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [4] TOOLCHAINS, COMPILERS & SDKS ===%s\n\n", Bold, Blue, Reset)
		fmt.Printf("  %s[1]%s 🦀 Rust, Mold Linker & Sccache\n", Bold, Reset)
		fmt.Printf("  %s[2]%s 🐹 Golang (Versi Terbaru)\n", Bold, Reset)
		fmt.Printf("  %s[3]%s ☕ OpenJDK (Java JDK 21)\n", Bold, Reset)
		fmt.Printf("  %s[4]%s 🎯 Flutter SDK (Channel Stable)\n", Bold, Reset)
		fmt.Printf("  %s[5]%s 🤖 Android SDK & Command Line Tools\n", Bold, Reset)
		fmt.Printf("  %s[6]%s ⚡ Redis / Valkey Cache Server\n", Bold, Reset)
		fmt.Printf("  %s[7]%s 📦 Dependensi Dasar Sistem (Build Tools & Libs)\n", Bold, Reset)
		fmt.Printf("  %s[8]%s 📋 Buka Menu Modular Komponen Lengkap (19 Item)\n", Bold, Reset)
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-8]: ")
		switch c {
		case "1":
			t.runTask("Rust & Mold Linker", t.inst.InstallRust)
		case "2":
			t.runTask("Golang", t.inst.InstallGolang)
		case "3":
			t.runTask("Java OpenJDK 21", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallJava(ctx, cb, "21")
			})
		case "4":
			t.runTask("Flutter SDK", t.inst.InstallFlutter)
		case "5":
			t.runTask("Android SDK & Tools", t.inst.InstallAndroidSDK)
		case "6":
			t.runTask("Redis Cache Server", t.inst.InstallRedis)
		case "7":
			t.runTask("Dependensi Dasar Sistem", t.inst.InstallBase)
		case "8":
			t.menuModularInstall()
		case "0", "q", "Q":
			return
		}
	}
}

// 5. WORKSPACE, ADE & SHELL
func (t *TUI) menuWorkspaceShell() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [5] WORKSPACE, ADE & SHELL ===%s\n\n", Bold, Blue, Reset)
		fmt.Printf("  %s[1]%s 🐬 Status & Kontrol Layanan Orca ADE\n", Bold, Reset)
		fmt.Printf("  %s[2]%s 🚀 Setup / Install Orca ADE (Desktop GUI)\n", Bold, Reset)
		fmt.Printf("  %s[3]%s 🚀 Setup / Install Orca ADE (Headless Server)\n", Bold, Reset)
		fmt.Printf("  %s[4]%s 🐚 Oh My Zsh & Terminal Shell\n", Bold, Reset)
		fmt.Printf("  %s[5]%s 🎨 Ganti Tema Oh My Zsh (Powerlevel10k / Agnoster / Robbyrussell)\n", Bold, Reset)
		fmt.Printf("  %s[6]%s 🗑️  Uninstall Orca ADE\n", Bold, Reset)
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-6]: ")
		switch c {
		case "1":
			st := monitor.GetServiceStatus("orca")
			statusStr := fmt.Sprintf("%sNONAKTIF%s", Red, Reset)
			if st.Active {
				statusStr = fmt.Sprintf("%sAKTIF%s", Green, Reset)
			}
			fmt.Printf("\nStatus Layanan Orca: %s\n", statusStr)
			act := t.prompt("Pilih tindakan [start / stop / restart / kembali]: ")
			if act == "start" || act == "stop" || act == "restart" {
				if err := monitor.ControlService("orca", act); err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("✓ Layanan orca berhasil di-%s\n", act)
				}
				t.waitEnter()
			}
		case "2":
			t.runTask("Instal Orca ADE (Desktop GUI)", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallOrca(ctx, cb, "desktop")
			})
		case "3":
			t.runTask("Instal Orca ADE (Headless Server)", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallOrca(ctx, cb, "headless")
			})
		case "4":
			t.runTask("Oh My Zsh", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallZsh(ctx, cb, "powerlevel10k")
			})
		case "5":
			theme := t.prompt("Pilih tema [powerlevel10k / agnoster / robbyrussell]: ")
			if theme != "" {
				t.runTask("Ganti Tema Zsh ("+theme+")", func(ctx context.Context, cb installer.LogCallback) error {
					return t.inst.InstallZsh(ctx, cb, theme)
				})
			}
		case "6":
			t.runTask("Uninstall Orca ADE", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.UninstallOrca(ctx, cb, false)
			})
		case "0", "q", "Q":
			return
		}
	}
}

// 6. CLOUDFLARE TUNNEL & DNS
func (t *TUI) menuCloudflare() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [6] CLOUDFLARE TUNNEL & DNS MANAGER ===%s\n\n", Bold, Blue, Reset)
		fmt.Printf("  %s[1]%s 📊 Status Tunnel & Daftar Eksposur\n", Bold, Reset)
		fmt.Printf("  %s[2]%s 🌐 Ekspos Vhost Lokal ke Domain Cloudflare (Expose)\n", Bold, Reset)
		fmt.Printf("  %s[3]%s ❌ Hapus Eksposur Domain (Unexpose)\n", Bold, Reset)
		fmt.Printf("  %s[4]%s ⚙️  Kontrol Layanan cloudflared (Start / Stop / Restart)\n", Bold, Reset)
		fmt.Printf("  %s[5]%s 🩺 Diagnostik Koneksi Cloudflare\n", Bold, Reset)
		fmt.Printf("  %s[6]%s 📥 Install / Perbarui cloudflared\n", Bold, Reset)
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-6]: ")
		switch c {
		case "1":
			t.Clear()
			st := t.cfMgr.GetStatus()
			activeBadge := fmt.Sprintf("%sAKTIF%s", Green, Reset)
			if !st.Active {
				activeBadge = fmt.Sprintf("%sNONAKTIF%s", Red, Reset)
			}
			fmt.Printf("Cloudflare Tunnel Service: %s\n", activeBadge)
			fmt.Printf("Profil Akun: %s\n\n", strings.Join(st.Profiles, ", "))
			fmt.Printf("%-24s %-32s %s\n", "LOCAL DOMAIN", "PUBLIC HOST", "PROFILE")
			fmt.Println(strings.Repeat("-", 70))
			for _, exp := range st.Exposures {
				fmt.Printf("%-24s %-32s %s\n", exp.LocalDomain, exp.PublicHost, exp.Profile)
			}
			t.waitEnter()
		case "2":
			local := t.prompt("Domain lokal (contoh: myapp.test): ")
			pub := t.prompt("Domain publik Cloudflare (contoh: app.domainanda.com): ")
			prof := t.prompt("Nama profil [opsional, default utama]: ")
			if local != "" && pub != "" {
				if err := t.cfMgr.Expose(local, pub, prof); err != nil {
					fmt.Printf("Gagal mengekspos domain: %v\n", err)
				} else {
					fmt.Printf("✓ %s berhasil diekspos ke https://%s\n", local, pub)
				}
				t.waitEnter()
			}
		case "3":
			local := t.prompt("Domain lokal yang ingin dihapus dari eksposur: ")
			if local != "" {
				if err := t.cfMgr.Unexpose(local); err != nil {
					fmt.Printf("Gagal unexpose: %v\n", err)
				} else {
					fmt.Printf("✓ Eksposur untuk %s berhasil dihapus.\n", local)
				}
				t.waitEnter()
			}
		case "4":
			act := t.prompt("Tindakan [start/stop/restart/reload]: ")
			if act != "" {
				if err := monitor.ControlService("cloudflared", act); err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("✓ cloudflared berhasil di-%s\n", act)
				}
				t.waitEnter()
			}
		case "5":
			fmt.Println("Memeriksa konektivitas Cloudflare...")
			client := http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get("https://1.1.1.1")
			if err != nil {
				fmt.Printf("[FAIL] Cloudflare Edge tidak dapat dijangkau: %v\n", err)
			} else {
				resp.Body.Close()
				fmt.Println("[OK] Konektivitas Cloudflare Edge 1.1.1.1 prima.")
			}
			t.waitEnter()
		case "6":
			t.runTask("Install Cloudflared", t.inst.InstallCloudflared)
		case "0", "q", "Q":
			return
		}
	}
}

// 7. POWERDNS AUTHORITATIVE & EDGE GATEWAY
func (t *TUI) menuPowerDNS() {
	for {
		t.Clear()
		conf := t.pdnsMgr.LoadConfig()
		fmt.Printf("%s%s=== [7] POWERDNS AUTHORITATIVE (%s) ===%s\n\n", Bold, Blue, strings.ToUpper(conf.Mode), Reset)
		fmt.Printf("  %s[1]%s 📊 Status PowerDNS & API\n", Bold, Reset)
		fmt.Printf("  %s[2]%s 📋 Daftar Zone Domain DNS\n", Bold, Reset)
		fmt.Printf("  %s[3]%s ➕ Tambah Zone Domain Baru\n", Bold, Reset)
		fmt.Printf("  %s[4]%s 📦 Pasang PowerDNS Lokal Standalone (Tanpa NetBird)\n", Bold, Reset)
		fmt.Printf("  %s[0]%s Kembali ke Menu Utama\n\n", Bold, Reset)

		c := t.prompt("Pilih opsi [0-4]: ")
		switch c {
		case "1":
			t.Clear()
			conf := t.pdnsMgr.LoadConfig()
			reachStr := fmt.Sprintf("%sONLINE%s", Green, Reset)
			if !conf.Reachable {
				reachStr = fmt.Sprintf("%sOFFLINE%s", Red, Reset)
			}
			fmt.Println("=== Status PowerDNS ===")
			fmt.Printf("  Mode Server    : %s\n", strings.ToUpper(conf.Mode))
			fmt.Printf("  Terkonfigurasi : %v\n", conf.Configured)
			fmt.Printf("  Target API     : %s:%s\n", conf.VPSNetbirdIP, conf.PDNSPort)
			fmt.Printf("  Public/Host IP : %s\n", conf.VPSPublicIP)
			fmt.Printf("  Status API     : %s\n", reachStr)
			t.waitEnter()
		case "2":
			t.Clear()
			zones, err := t.pdnsMgr.ListZones()
			if err != nil {
				fmt.Printf("Gagal memuat zone: %v\n", err)
			} else {
				fmt.Printf("%-28s %-12s %s\n", "ZONE DOMAIN", "SERIAL", "KIND")
				fmt.Println(strings.Repeat("-", 60))
				for _, z := range zones {
					fmt.Printf("%-28s %-12d %s\n", z.Name, z.Serial, z.Kind)
				}
			}
			t.waitEnter()
		case "3":
			z := t.prompt("Masukkan nama domain zone (contoh: domainanda.com): ")
			if z != "" {
				res, err := t.pdnsMgr.AddZoneWithVhost(z)
				if err != nil {
					fmt.Printf("Gagal menambah zone: %v\n", err)
				} else {
					fmt.Printf("✓ Zone domain %s berhasil ditambahkan ke PowerDNS (IP: %s).\n", res.Zone, res.ZoneIP)
					if res.VHostCreated {
						fmt.Printf("✓ VHost static HTML default dibuat di %s (%s).\n", res.VHostRoot, res.VHostURL)
					} else if res.VHostMessage != "" {
						fmt.Printf("! Catatan VHost: %s\n", res.VHostMessage)
					}
				}
				t.waitEnter()
			}
		case "4":
			t.runTask("PowerDNS Authoritative Server", t.inst.InstallPowerDNS)
		case "0", "q", "Q":
			return
		}
	}
}

// 8. FULL AUTO SETUP
func (t *TUI) menuFullInstall() {
	t.Clear()
	fmt.Printf("%s%s=== [8] INSTALASI LENGKAP (FULL AUTO SETUP) ===%s\n\n", Bold, Yellow, Reset)
	fmt.Println("Instalasi otomatis 14 langkah mencakup:")
	fmt.Println("  1. Dependensi Dasar (Build tools, cmake, clang, jq, dll.)")
	fmt.Println("  2. Node.js LTS (multi-versi via wdev switch node)")
	fmt.Println("  3. Rust Toolchain + Mold Linker + Sccache")
	fmt.Println("  4. Golang (Rilis stabil terbaru)")
	fmt.Println("  5. OpenJDK Java (JDK 21)")
	fmt.Println("  6. Android SDK & CLI Tools")
	fmt.Println("  7. Flutter SDK (Stable)")
	fmt.Println("  8. PostgreSQL Server (multi-versi via wdev switch pg)")
	fmt.Println("  9. Redis / Valkey Cache Server")
	fmt.Println("  10. Nginx Mainline Web Server")
	fmt.Println("  11. PHP & PHP-FPM (Multi-versi 8.1 - 8.4)")
	fmt.Println("  12. Composer Package Manager")
	fmt.Println("  13. Local SSL (mkcert CA lokal)")
	fmt.Println("  14. Oh My Zsh (Plugin & Tema Powerlevel10k)")
	fmt.Println()

	conf := t.prompt("Jalankan instalasi otomatis 14 langkah sekarang? [y/N]: ")
	if strings.ToLower(conf) == "y" || strings.ToLower(conf) == "yes" {
		t.runTask("Full Auto Setup (14 Langkah)", t.inst.RunFullInstall)
	} else {
		fmt.Println("Instalasi lengkap dibatalkan.")
		t.waitEnter()
	}
}

// 9. INSTALASI KOMPONEN MODULAR (20 ITEM)
func (t *TUI) menuModularInstall() {
	for {
		t.Clear()
		fmt.Printf("%s%s=== [9] INSTALASI KOMPONEN MODULAR (20 ITEM) ===%s\n\n", Bold, Blue, Reset)
		fmt.Printf("%sCore & Build Tools:%s\n", Bold, Reset)
		fmt.Printf("  %s[1]%s  Dependensi Dasar Sistem (Build Tools, curl, git, lib)\n", Cyan, Reset)
		fmt.Printf("  %s[2]%s  Node.js (multi-versi via wdev switch node)\n", Cyan, Reset)
		fmt.Printf("  %s[3]%s  Rust, Mold Linker & Sccache\n", Cyan, Reset)
		fmt.Printf("  %s[4]%s  Golang (Versi Terbaru)\n", Cyan, Reset)
		fmt.Printf("  %s[5]%s  OpenJDK Java (JDK Multi-versi)\n", Cyan, Reset)
		fmt.Printf("%sMobile & SDKs:%s\n", Bold, Reset)
		fmt.Printf("  %s[6]%s  Android SDK & Command Line Tools\n", Cyan, Reset)
		fmt.Printf("  %s[7]%s  Flutter SDK (Channel Stable)\n", Cyan, Reset)
		fmt.Printf("%sDatabase & Cache:%s\n", Bold, Reset)
		fmt.Printf("  %s[8]%s  PostgreSQL Server & Konfigurasi (Multi-versi)\n", Cyan, Reset)
		fmt.Printf("  %s[9]%s  Redis / Valkey Cache Server\n", Cyan, Reset)
		fmt.Printf("%sWeb Stack & Networking:%s\n", Bold, Reset)
		fmt.Printf("  %s[10]%s Nginx Web Server (Mainline Terbaru)\n", Cyan, Reset)
		fmt.Printf("  %s[11]%s PHP & PHP-FPM (Multi-versi & Ekstensi)\n", Cyan, Reset)
		fmt.Printf("  %s[12]%s Composer (PHP Package Manager)\n", Cyan, Reset)
		fmt.Printf("  %s[13]%s Local SSL (mkcert) & helper wdev site\n", Cyan, Reset)
		fmt.Printf("  %s[14]%s Full PHP Web Stack (Nginx + PHP + Composer + SSL)\n", Cyan, Reset)
		fmt.Printf("  %s[15]%s Cloudflare Tunnel & DNS (cloudflared via wdev cf)\n", Cyan, Reset)
		fmt.Printf("  %s[16]%s NetBird Mesh VPN (Install / Update Mandiri)\n", Cyan, Reset)
		fmt.Printf("  %s[17]%s PowerDNS Authoritative Server (Standalone)\n", Cyan, Reset)
		fmt.Printf("  %s[18]%s Mail Server Stack (Stalwart + SnappyMail)\n", Cyan, Reset)
		fmt.Printf("%sWorkspace, IDE & Shell:%s\n", Bold, Reset)
		fmt.Printf("  %s[19]%s Oh My Zsh (Plugin & Tema Populer)\n", Cyan, Reset)
		fmt.Printf("  %s[20]%s Orca ADE - Desktop GUI (Aplikasi Desktop)\n", Cyan, Reset)
		fmt.Printf("  %s[21]%s Orca ADE - Headless Server (Service 24/7)\n", Cyan, Reset)
		fmt.Printf("  %s[0]%s  Kembali ke Menu Utama\n\n", Yellow, Reset)

		c := t.prompt("Pilih komponen [0-21]: ")
		switch c {
		case "1":
			t.runTask("Dependensi Dasar Sistem", t.inst.InstallBase)
		case "2":
			t.runTask("Node.js LTS", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallNode(ctx, cb, "22")
			})
		case "3":
			t.runTask("Rust & Mold Linker", t.inst.InstallRust)
		case "4":
			t.runTask("Golang", t.inst.InstallGolang)
		case "5":
			t.runTask("Java OpenJDK 21", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallJava(ctx, cb, "21")
			})
		case "6":
			t.runTask("Android SDK & Tools", t.inst.InstallAndroidSDK)
		case "7":
			t.runTask("Flutter SDK", t.inst.InstallFlutter)
		case "8":
			t.runTask("PostgreSQL 16", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallPostgres(ctx, cb, "16")
			})
		case "9":
			t.runTask("Redis Cache Server", t.inst.InstallRedis)
		case "10":
			t.runTask("Nginx Web Server", t.inst.InstallNginx)
		case "11":
			t.runTask("PHP & PHP-FPM", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallPHP(ctx, cb, "8.4")
			})
		case "12":
			t.runTask("Composer", t.inst.InstallComposer)
		case "13":
			t.runTask("Local SSL (mkcert)", t.inst.InstallMkcert)
		case "14":
			t.runTask("Full PHP Web Stack", t.inst.InstallPhpStack)
		case "15":
			t.runTask("Cloudflare Tunnel (cloudflared)", t.inst.InstallCloudflared)
		case "16":
			t.runTask("NetBird Mesh VPN", t.inst.InstallNetbird)
		case "17":
			t.runTask("PowerDNS Authoritative Server", t.inst.InstallPowerDNS)
		case "18":
			t.runTask("Mail Server Stack", t.inst.InstallMailStack)
		case "19":
			t.runTask("Oh My Zsh", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallZsh(ctx, cb, "powerlevel10k")
			})
		case "20":
			t.runTask("Orca ADE (Desktop GUI)", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallOrca(ctx, cb, "desktop")
			})
		case "21":
			t.runTask("Orca ADE (Headless Server)", func(ctx context.Context, cb installer.LogCallback) error {
				return t.inst.InstallOrca(ctx, cb, "headless")
			})
		case "0", "q", "Q":
			return
		}
	}
}
