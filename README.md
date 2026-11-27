# wdev — Linux Developer Control Plane & Environment Manager

[![Version](https://img.shields.io/badge/version-v2.12.0-blue.svg)](CHANGELOG.md)
[![Go](https://img.shields.io/badge/Go-1.27+-00ADD8.svg)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`wdev` adalah aplikasi control plane manajemen lingkungan pengembangan dan infrastruktur server Linux terpadu dalam satu biner Go mandiri (**Web UI Dashboard**, **TUI Terminal**, **CLI**, dan **VPS Edge Agent**). Antarmuka web dibangun dengan SvelteKit 5 + Tailwind CSS v4 + `shadcn-svelte` dan disematkan langsung ke dalam biner via `go:embed`.

Seluruh automasi runtime, provisioning, installer toolchain, virtual host, DNS, mail stack, dan panel keamanan berjalan native di dalam biner Go — menggantikan kumpulan skrip `.sh` wrapper lama (`node-switch`, `php-switch`, `pg-switch`, `dev-site`, `cf-tunnel`, `pdns-mgr`, `orca-*`).

---

## Arsitektur 3-Mode Server (`wdev mode`)

`wdev` mendukung tiga mode operasi yang dapat diubah kapan saja melalui Web UI maupun CLI (`wdev mode set <local|edge|vps>`):

| Mode | Deskripsi | Penggunaan Utama |
|---|---|---|
| **`local`** | **Lokal Standalone** — Tanpa NetBird maupun VPS Edge Gateway. Virtual host memakai `/etc/hosts` lokal, SSL `mkcert`/self-signed, serta PowerDNS lokal opsional (`127.0.0.1:8081`). | Workstation / laptop developer |
| **`edge`** | **Hybrid Edge Gateway (NetBird)** — Mesin lokal terhubung ke VPS Edge Gateway melalui mesh VPN **NetBird** (`100.64.0.0/10`) dan `wdev-edge-agent` untuk mengekspos situs, port, DNS, dan mail ke publik. | Developer lokal + VPS reverse proxy |
| **`vps`** | **Full VPS Standalone (aaPanel Style)** — Nginx, PowerDNS Authoritative, Mail Server (Stalwart + SnappyMail), dan SSL Let's Encrypt (`certbot`) berjalan langsung di VPS publik tanpa NetBird. | Hosting server / VPS produksi mandiri |

---

## Fitur Utama (`v2.12.0`)

### 1. Web UI Dashboard Tersemat (`wdev serve`)
Antarmuka web modern dan responsif (dukungan bahasa **Inggris & Indonesia**, serta optimasi layar sentuh/mobile) di port default `3200`, dilengkapi live streaming log task via Server-Sent Events (SSE), tombol *restart* sistem dari panel, serta pengecekan dan pembaruan rilis GitHub otomatis dengan banner changelog dan auto-reload pasca-update:

* **Status & Update (`/`)**: Pemantauan CPU total & per-core, load average (`1m`/`5m`/`15m`), RAM & Swap, Disk utama (`/`) serta drive eksternal / mount tambahan di `/mnt`, alamat IP (LAN, NetBird, IP publik VPS), status layanan inti (termasuk status *starting* daemon Docker/layanan), dan updater biner `wdev`.
* **Web Stack, VHost & Managed Apps (`/webstack`)**:
 * Manajemen virtual host Nginx (`laravel`, `wordpress`, `ci4`, `ci3`, `symfony`, `static`, `proxy`) beserta folder picker, ubah konfigurasi VHost, paginasi, dan sinkronisasi record A PowerDNS / eksposur Edge otomatis.
 * **Managed App (`nodejs`, `go`, `rust`)**: Jalankan aplikasi backend langsung di balik Nginx reverse proxy sebagai unit `systemd` terkelola dengan konfigurasi `build_command`, `start_command`, pilihan user Linux, sinkronisasi port `.env` proyek/terkelola, live log stream, serta metrik PID & penggunaan RAM.
 * Penerbitan SSL otomatis (`mkcert` lokal, Let's Encrypt via `certbot` untuk VPS, atau SSL Edge Gateway) lengkap dengan informasi masa berlaku sertifikat.
 * **Konfigurasi & Inspeksi Layanan Inti**: Pengaturan parameter dan preset otomatis untuk **Nginx**, **PHP-FPM** (termasuk inspeksi runtime/ekstensi), **PostgreSQL** (termasuk pembuatan database dan manajemen role), **Redis / Valkey** (inspeksi keyspace, statistik hit-rate, dan flush DB), serta **Docker** beserta penampil log unit systemd.
* **Docker & Compose v5 (`/compose`)**: Pengelolaan proyek Docker Compose v5 (register folder atau buat dari template `nginx-static`, `postgres-redis`, `app-postgres-redis`, `wordpress`, `blank`), kontrol siklus hidup (`up`, `down`, `start`, `stop`, `restart`, `pull`, `build`, `run-job`), editor `compose.yaml` & `.env`, live logs, `df`/`prune`, peringatan port sensitif, eksposur ke VHost Nginx SSL, serta instalasi Docker Engine & pra-konfigurasi daemon.
* **Multi-Versi Runtime (`/switchers`)**: Berpindah versi aktif **Node.js**, **PHP CLI/FPM**, **PostgreSQL** (lengkap dengan migrasi cluster database aman antar versi mayor), dan **OpenJDK Java**.
* **Toolchains & SDKs (`/toolchains`)**: Ringkasan toolchain terpasang, pemeriksaan pembaruan, serta sinkronisasi & audit duplikat `PATH` (`/etc/profile.d/wdev.sh`).
* **Workspace & ADE (`/workspace`)**: Manajemen **Orca ADE** (Desktop GUI / Headless Server, autostart, menu aksi terkelompok, pengaturan pairing **QR Code** dengan mode `auto`/`local`/`vpn`/`custom`, pembaruan versi) dan konfigurasi **Orca Commit AI** (pemilihan AI agent, argumen CLI agent, model discovery, thinking level, preset prompt Conventional Commits / Gitmoji, serta sinkronisasi otomatis ke database Orca).
* **Status Port & Jaringan (`/ports`)**: Diagnostik status & keterjangkauan port layanan di cakupan **Lokal**, **VPN NetBird**, dan **VPS Publik**, ringkasan status firewall (`UFW`/`firewalld`), serta detail jangkauan jaringan per port.
* **Keamanan & Pemantauan Serangan (`/security`)**: IDS/IPS dan perlindungan server yang memantau brute-force panel/sudo/SSH/mail, *honeypot decoy traps*, *web scanner*, dan *port scan*; auto-ban & blokir IP manual dengan sinkronisasi ke firewall host (`UFW`/`firewalld`/`nftables`), blocklist Nginx L7 & Cloudflare Real-IP, sinkronisasi Cloudflare WAF, whitelist CIDR, retensi log, serta alert webhook.
* **File Manager (`/files`)**: Penjelajah berkas terpadu untuk Web Root (`/var/www`), direktori VHost, direktori Home, drive tambahan di `/mnt`, serta root kustom (`WDEV_FM_ROOTS`). Mendukung editor teks bawaan (hingga 4 MiB), preview gambar, pencarian file, upload (hingga 256 MiB), fetch URL remote, `chmod`, kompresi & ekstraksi arsip (`.zip`, `.tar`, `.gz`, `.tgz`).
* **Terminal SSH / PTY (`/terminal`)**: Web terminal interaktif berbasis `xterm.js` + WebSocket PTY multi-sesi (maksimal 16 sesi, idle timeout 15 menit), pemilih user Linux & shell target, pengamanan *step-up root unlock* (berlaku 10 menit), serta audit log sesi PTY.
* **Domain, DNS & Edge (`/powerdns`)**, **Cloudflare Tunnel (`/cloudflare`)** (multi-profil dengan mode `token` & `cert`, manajemen zona & DNS via API Token, auto-naming profil, dan overwrite exposure), **Mail Server (`/mail`)**, serta **Setup & Komponen (`/setup`)** dengan preset instalasi sesuai mode server dan instalasi batch/modular.

### 2. Keamanan Panel (`aaPanel Security Architecture`)
* **Security Entrance**: Path akses rahasia acak (`/<entrance>`); permintaan tanpa cookie entrance yang valid otomatis menerima halaman `404 Not Found`.
* **Autentikasi & Anti-Brute-Force**: Hashing kata sandi PBKDF2-SHA256 (`210.000` iterasi), cookie sesi `HttpOnly` bertoken hash SHA-256, proteksi CSRF (`Origin`/`Referer`/`Sec-Fetch-Site` serta dukungan reverse proxy tepercaya), header keamanan HTTP (`CSP`, `X-Frame-Options: DENY`, `HSTS`), serta lockout 15 menit setelah 5 kali percobaan gagal.
* **Force HTTPS Port Panel (`wdev panel ssl on|off`)**: Melayani HTTP dan HTTPS di port panel yang sama menggunakan multiplexer TLS (`tls_mux`) dengan sertifikat ECDSA self-signed dan pengalihan otomatis HTTP ke HTTPS.
* **Pengikatan Domain Panel (`wdev panel domain`)**: Ikat domain khusus (mis. `panel.domainanda.com`) via reverse proxy Nginx dengan SSL `letsencrypt`, `edge`, `local`, `external`, atau `none`, serta opsi **blokir akses langsung via IP** (`wdev panel block-ip on|off`).

### 3. TUI Terminal (`wdev tui`) & Penyimpanan SQLite
* **TUI Terminal Dashboard (`wdev tui`)**: Menu interaktif 10 kategori di terminal (termasuk pengelolaan Docker & Compose v5) dengan ringkasan sistem dan indikator status layanan.
* **State Terpadu SQLite (`modernc.org/sqlite`)**: Menyimpan state situs, Managed App, proyek Compose, profil tunnel, pengaturan panel, audit terminal, event keamanan/IP terblokir, dan riwayat task pada `/etc/wdev/wdev.db` (atau `~/.config/wdev/wdev.db` untuk pengguna non-root).

---

## Mulai Cepat

### 1. Instalasi Mandiri dari Biner Release

Unduh biner rilis untuk arsitektur Anda dari [GitHub Releases](https://github.com/barisankoding-cell/wdev/releases) (`wdev-linux-amd64`, `wdev-linux-arm64`, atau `wdev-linux-armv7`):

```bash
chmod +x wdev-linux-amd64
sudo ./wdev-linux-amd64 install
```

Atau bila ingin memasang paket VPS lengkap ala aaPanel (membuat direktori sistem, mengaktifkan `panel ssl on`, memasang `wdev.service` di port `3200`, membuka port UFW `3200, 80, 443, 53, 25, 465, 587, 993`, lalu menampilkan kredensial panel):

```bash
# Letakkan biner 'wdev' di direktori yang sama dengan install.sh (atau /usr/local/bin/wdev)
chmod +x wdev install.sh
sudo bash install.sh
```

Lihat URL akses, Security Entrance, dan kredensial login panel aktif kapan saja:

```bash
sudo wdev panel default
```

### 2. Pembaruan Mandiri (`wdev update`)

Untuk memperbarui biner `wdev` ke versi terbaru tanpa unduh manual:

```bash
sudo wdev update --check      # cek rilis terbaru di GitHub
sudo wdev update              # unduh biner, verifikasi SHA256, backup wdev.bak, & restart service
sudo wdev update --edge root@<IP-VPS> --edge-key ~/.ssh/id_ed25519
sudo wdev update --source ~/projects/wdev   # build langsung dari repositori source lokal
```

`wdev update` memverifikasi checksum `.sha256`, menyimpan cadangan biner lama di `/usr/local/bin/wdev.bak`, dan me-restart `wdev.service` secara otomatis. Pembaruan juga dapat dipicu langsung dari banner Web UI Dashboard.

### 3. Mengelola Layanan (`wdev service`)

```bash
# Cek status service wdev atau layanan lain (nginx, postgresql, stalwart, dll.)
wdev service status wdev
wdev service status nginx

# Start / Stop / Restart / Reload layanan
sudo wdev service restart wdev
sudo wdev service stop wdev
sudo wdev service start wdev

# Lihat log real-time systemd
journalctl -u wdev.service -f
```

### 4. Uninstall Layanan & Biner

```bash
# Hapus service systemd dan biner wdev (database & konfigurasi di /etc/wdev dipertahankan)
sudo wdev uninstall

# Atau hapus total termasuk database & seluruh konfigurasi
sudo wdev uninstall --purge
```

### 5. Build Manual dari Source (Pengembang)

Prasyarat: Go `1.27+`, Node.js `22+`, dan `pnpm`.

```bash
# Build frontend SvelteKit + biner Go lengkap ke bin/wdev (via Makefile)
make build

# Atau build manual langkah demi langkah
cd frontend && pnpm install --frozen-lockfile && pnpm build && cd ..
go build -trimpath -ldflags="-s -w" -o bin/wdev ./cmd/wdev
sudo install -m 755 ./bin/wdev /usr/local/bin/wdev

# Pasang systemd service
sudo wdev install --port 3200

# Atau jalankan langsung di foreground / TUI
wdev serve
wdev tui
```

Target `Makefile` yang tersedia:

| Target | Keterangan |
|---|---|
| `make build` | Build frontend SvelteKit (`web/static`) dan biner Go ke `bin/wdev` |
| `make build-go` | Build cepat biner Go tanpa rebuild frontend |
| `make dev-ui` | Jalankan SvelteKit Vite dev server (port `5173`) |
| `make run` / `make dev` | Build lalu jalankan `./bin/wdev serve --port 3200 --host 0.0.0.0` |
| `make test` | Jalankan unit test (`go test -v ./internal/...`) |
| `make clean` | Bersihkan `bin/`, `dist/`, dan file sementara |
| `make next-version` | Deteksi versi SemVer berikutnya dari `version.go`, tag, dan Conventional Commits |
| `make changelog` | Tampilkan pratinjau changelog sejak tag rilis terakhir |
| `make release-dry` | Simulasi deteksi versi dan pratinjau changelog tanpa commit/tag/push |
| `make release` | Otomasi rilis lengkap ke GitHub |

---

## Referensi CLI `wdev`

### Antarmuka, Status & Layanan
| Perintah | Keterangan |
|---|---|
| `wdev serve [--port 3200] [--host 0.0.0.0] [--tls-cert ...] [--tls-key ...]` | Jalankan Web UI Dashboard (`alias: wdev web, wdev dashboard`) |
| `wdev tui` | Jalankan Terminal UI interaktif (10 kategori menu) |
| `wdev status` / `wdev check` / `wdev -c` | Ringkasan OS, uptime, load per-core, RAM, disk, IP, layanan inti, dan runtime |
| `wdev check-updates` / `wdev -cu` | Bandingkan versi toolchain & paket OS terpasang vs terbaru |
| `wdev mode [get \| set <local\|edge\|vps>]` | Lihat atau ubah arsitektur mode server |
| `wdev service <install\|uninstall\|status\|start\|stop\|restart\|reload>` | Kelola unit `wdev.service` dan layanan systemd lain |
| `wdev install [service]` | Pasang biner & systemd service `wdev` (auto-start) |
| `wdev uninstall [--purge]` | Hapus systemd service dan biner `wdev` |
| `wdev version` / `-v` | Tampilkan versi `wdev` |

### Keamanan Panel & Domain (`wdev panel`)
```bash
sudo wdev panel default                          # tampilkan URL akses, entrance, username & password
sudo wdev panel ssl on                           # aktifkan Force HTTPS di port panel (self-signed ECDSA)
sudo wdev panel domain panel.example.com         # ikat domain HTTPS ke panel (default: letsencrypt, block IP)
sudo wdev panel domain panel.example.com --ssl edge --allow-ip
sudo wdev panel domain off                       # lepas domain panel & aktifkan kembali akses IP
sudo wdev panel block-ip on|off                  # blokir atau izinkan akses langsung via nomor IP
sudo wdev panel entrance rahasia123              # atur path security entrance (atau 'off')
sudo wdev panel username adminbaru               # ubah username panel
sudo wdev panel password '<sandi_baru>'          # ubah kata sandi panel
sudo wdev panel auth on|off                      # aktifkan / nonaktifkan proteksi login
sudo wdev panel reset                            # reset kredensial & security entrance ke nilai acak baru
```

### Virtual Host, Runtime Switcher & Environment
```bash
# Virtual Host Nginx
sudo wdev site list
sudo wdev site create myapp.test /var/www/myapp 8.4 laravel
sudo wdev site create mygo.test "" "" proxy 3221
sudo wdev site ssl example.com --email admin@example.com   # terbitkan SSL Let's Encrypt (VPS)
sudo wdev site sync
sudo wdev site delete myapp.test

# Multi-Versi Runtime Switcher
wdev switch list
wdev switch node 22
sudo wdev switch php 8.4
sudo wdev switch pg 18          # mendukung migrasi cluster database antar versi mayor
sudo wdev switch java 21

# Environment PATH & Audit Duplikat
wdev env status
sudo wdev env sync
sudo wdev install audit
sudo wdev install audit-fix

# Docker & Compose v5
wdev compose list
wdev compose add myapp /var/www/myapp --template app-postgres-redis --up
wdev compose ps myapp
wdev compose up myapp
wdev compose logs myapp --tail 200
wdev compose df
```

### Padanan Perintah Helper Lama
| Helper lama | Perintah Go |
|---|---|
| `node-switch` | `wdev switch node <versi>` |
| `php-switch` | `wdev switch php <versi>` |
| `pg-switch` | `wdev switch pg <versi>` |
| `dev-site` | `wdev site list\|create\|ssl\|delete\|sync` |
| `cf-tunnel` | `wdev cf status\|expose\|unexpose` |
| `pdns-mgr` | `wdev pdns ...` dan `wdev mail ...` |
| `orca-switch` | `wdev orca switch <desktop\|headless\|status\|autostart>` |
| `orca-update` | `wdev orca update` (atau `wdev install orca-update`) |
| `orca-pair` | `wdev orca pair` |
| `orca-desktop` | `wdev orca desktop` |

---

## Komponen Instalasi, Preset & Sumber Resmi

### Komponen `wdev install`
```bash
wdev install all                 # Full Auto Setup (14 langkah + pre/post-flight audit PATH)
wdev install update              # Perbarui paket OS (apt/dnf), composer, rustup, flutter & unit Nginx/wdev
wdev install check-updates       # Cek pembaruan komponen dan simulasi upgrade paket sistem
wdev install self-check          # Validasi biner toolchain, sintaks Nginx, dan cache permission
wdev install <komponen> [versi]  # Instal/perbarui komponen modular tertentu
```

Komponen modular yang didukung:
`base`, `node`, `go`, `rust`, `java`, `nginx`, `php`, `composer`, `ssl`, `postgres`, `redis`, `php-stack`, `docker`, `android`, `flutter`, `netbird`, `pdns`, `mail`, `cloudflare`, `zsh`, `orca-desktop`, `orca-headless`, `orca-update`, `orca-uninstall`.

Pilihan versi komponen:

| Komponen | Nilai Didukung | Default |
|---|---|---|
| Node.js | `20`, `22`, `24` | `22` |
| Java OpenJDK | `17`, `21`, `25` | `21` |
| PostgreSQL | `16`, `17`, `18` | `18` (Web UI) / `16` (CLI `install postgres` tanpa argumen & Full Auto Setup) |
| PHP & PHP-FPM | `8.1`, `8.2`, `8.3`, `8.4` | `8.4` |
| Zsh theme | `powerlevel10k`, `agnoster`, `robbyrussell` | `powerlevel10k` |

Di Web UI (`/setup`), tersedia juga **Preset Instalasi per Mode** (instalasi batch terkurasi):
* **Mode `local`**: *Workstation Development Lengkap*, *Web Stack & Database Lokal*, *Web Lokal + Cloudflare Tunnel*, *Mobile & Cross-Platform*.
* **Mode `edge`**: *Edge Node Web & Database*, *Edge Workstation Developer + Tunnel*, *Edge Web Ringan (Tanpa DB)*, *Mobile & Cross-Platform*.
* **Mode `vps`**: *Full VPS Hosting (Web + DB + DNS + Mail)*, *Produksi VPS (Web + Database)*, *DNS Authoritative & Mail Server VPS*, *Web Server VPS Ringan*.

### Sumber Resmi & Jalur Kanonik

`wdev` memprioritaskan **rilis binary/tarball resmi upstream** serta **repositori resmi vendor** pada keluarga Debian/Ubuntu (`apt-get`) maupun Fedora/RHEL (`dnf`/`yum`), menyatukan environment di `/etc/profile.d/wdev.sh`, dan membersihkan duplikasi paket distro yang bentrok:

| Komponen | Sumber & Jalur Kanonik |
|---|---|
| Dependensi dasar | `apt-get` / `dnf` (`gcc`, `clang`, `cmake`, `make`, `git`, `jq`, `curl`, dll.) |
| Node.js | Tarball resmi `nodejs.org/dist/` di `/usr/local/lib/nodejs` (`current` + symlink `/usr/local/bin`) |
| Rust & Mold | `rustup` resmi di `/usr/local/cargo` & `/usr/local/rustup` + linker `mold` dari rilis GitHub `rui314/mold` di `/usr/local/mold` |
| Golang | Tarball resmi `go.dev/dl/` di `/usr/local/go` |
| OpenJDK Java | Tarball resmi **Eclipse Adoptium (Temurin)** di `/usr/lib/jvm/java-<ver>-temurin` + `update-alternatives` / `alternatives` |
| Android SDK | Command line tools resmi Google di `/usr/local/android-sdk` (API 34, 35, platform-tools) |
| Flutter | Repository resmi `flutter/flutter` (channel `stable`) di `/usr/local/flutter` |
| PostgreSQL | Repo resmi **PGDG** (`apt.postgresql.org` / `download.postgresql.org`) + fallback repo sistem; mendukung migrasi cluster antar versi mayor |
| Redis / Valkey | Repo resmi **`packages.redis.io`** (atau `valkey` pada Fedora/Debian) |
| Nginx | Repo resmi **`nginx.org/packages/mainline`** (fallback otomatis ke repo sistem) + `/etc/nginx/sites-enabled/*` |
| PHP & PHP-FPM | Repo resmi **Ondřej PPA** (Ubuntu), **Sury** (Debian), dan **Remi** (Fedora/RHEL) untuk `8.1`–`8.4` |
| Composer | PHAR resmi `getcomposer.org/download/latest-stable/composer.phar` di `/usr/local/bin/composer` |
| Local SSL | Rilis GitHub resmi `FiloSottile/mkcert` di `/usr/local/bin/mkcert` + sertifikat di `/etc/ssl/local-certs` |
| Cloudflared | Rilis GitHub resmi `cloudflare/cloudflared` di `/usr/local/bin/cloudflared` |
| Docker & Compose | Repo resmi Docker (`download.docker.com` — Docker Engine + Docker Compose v5 plugin) atau paket distro |
| NetBird | Repo resmi **`pkgs.netbird.io`** (service `netbird`) |
| PowerDNS | Paket `pdns-server` + `pdns-backend-sqlite3` (`127.0.0.1:8081` API lokal atau `8081` NetBird pada VPS Edge) |
| Mail Stack | Rilis resmi **Stalwart Mail Server** (`/usr/local/bin/stalwart`, `stalwart-cli`) + **SnappyMail** (`/var/www/snappymail`) |
| Zsh & Font | Paket `zsh` + Oh My Zsh, `zsh-autosuggestions`, `zsh-syntax-highlighting`, `zsh-completions`, `powerlevel10k`, & Nerd Font (`MesloLGS NF`) |
| Orca ADE | Rilis GitHub resmi `stablyai/orca`: ekstrak `.deb` ke `~/.local/lib/orca-ide` (Debian/Ubuntu) atau AppImage/tarball server (Fedora/RHEL) |

---

## Orca ADE & Orca Commit AI

Installer dan pembaruan Orca ADE ditangani langsung oleh biner Go `wdev` tanpa skrip Bash. Pada Debian/Ubuntu, paket `.deb` resmi (`orca-ide_<versi>_<arch>.deb`) diekstrak ke ruang user (`~/.local/lib/orca-ide`) sehingga tidak bentrok dengan `dpkg`/`apt`.

```bash
wdev orca update            # perbarui ke rilis resmi terbaru
wdev install orca-desktop   # pasang / perbarui mode Desktop GUI
wdev install orca-headless  # pasang / perbarui mode Headless Server
wdev install orca-uninstall # hapus Orca ADE
wdev orca pair              # tampilkan URL pairing + web client
wdev orca status            # cek mode, versi, layanan, & pairing
wdev orca switch headless   # beralih ke mode headless 24/7
wdev orca switch desktop    # kembali ke Desktop GUI
```

| Mode | Perilaku |
|---|---|
| Desktop | Entri menu `orca-ide.desktop`, autostart opsional (`wdev orca switch autostart en`) |
| Headless | Unit `orca.service` (systemd user, `Restart=always`) + `orca-update.timer` harian + `loginctl enable-linger` |

**Konfigurasi Orca Commit AI** tersedia langsung di Web UI (halaman **Workspace & ADE**):
* Mendeteksi otomatis agent AI yang terpasang di sistem (`opencode`, `antigravity`/`agy`, `claude`, `codex`, `copilot`, `cursor-agent`, `omp`, `pi`, `kimi`, `opencode2`, `jcode`, `dsh`, atau `custom`).
* Pemilihan model per agent (dengan discovery model), *thinking level*, serta preset instruksi prompt (*Conventional Commits Indonesia*, *Conventional Commits English*, *Conventional + Ringkasan*, *Gitmoji + Conventional*, atau *Bawaan Orca*).

---

## Cloudflare Tunnel & DNS (Multi-Akun)

Setiap akun Cloudflare dikelola sebagai satu profil terpisah (satu proses `cloudflared` + satu unit systemd + satu set kredensial):

```text
/etc/wdev/cloudflare/accounts/<profil>.conf   # zone, mode, protocol, tunnel id
/etc/wdev/cloudflare/accounts/<profil>.env    # TUNNEL_TOKEN untuk mode token (0600)
/etc/wdev/cloudflare/routes/<profil>.conf     # custom ingress routes (HTTP/TCP/SSH/RDP/Unix)
/etc/wdev/cloudflare/access/<profil>.conf     # aturan JWT Cloudflare Access per host
/etc/wdev/exposures/<domain>.conf             # public-host <TAB> profil
/etc/cloudflared/<profil>/config.yml          # generated oleh wdev
/etc/cloudflared/<profil>/<uuid>.json         # credentials tunnel (0600)
/etc/cloudflared/<profil>/cert.pem            # origin cert hasil login (0600)
/etc/systemd/system/cloudflared-<profil>.service
```

```bash
sudo wdev install cloudflare
wdev cf status
wdev cf profile add-token cf1 <TUNNEL_TOKEN> --api-token <CF_API_TOKEN>
wdev cf zones --profile cf1
wdev cf expose myapp.test app-dev.example.com --profile cf1
wdev cf expose myapp.test example.com --profile cf1 --overwrite # apex domain
wdev cf unexpose myapp.test
```

* `expose` memvalidasi host terhadap zone profil, menulis state eksposur, meregenerasi `server_name` Nginx, menerbitkan sertifikat `mkcert` dengan SAN host publik, menyinkronkan ingress `cloudflared`, lalu membuat CNAME proxied ke `<uuid>.cfargotunnel.com`.
* Batasan Cloudflare: Universal SSL gratis hanya mencakup `example.com` dan `*.example.com` (subdomain bertingkat butuh Total TLS/ACM); body upload Free/Pro ~100 MB; timeout origin ~100 detik.

---

## PowerDNS, Standalone VPS & Edge Gateway

PowerDNS Authoritative dapat dijalankan secara **standalone langsung di VPS/lokal** (`wdev install pdns`) maupun sebagai **VPS Edge Gateway** via NetBird (`wdev pdns deploy`).

```bash
# Mode VPS Standalone (tanpa NetBird)
sudo wdev install pdns
sudo wdev pdns zone add domainanda.com

# Mode Hybrid Edge Gateway (dari mesin lokal ke VPS via SSH + NetBird)
wdev pdns deploy <ip-vps> --user root --key ~/.ssh/id_ed25519
wdev pdns connect <netbird_ip> <public_ip> <api_key>
wdev pdns status
wdev pdns zone add domainanda.com
wdev pdns expose api.domainanda.com 8080 --ssl
wdev pdns expose-site myapp.test myapp.domainanda.com --ssl
wdev pdns unexpose myapp.domainanda.com
```

### Fitur Zone & Diagnosa PowerDNS
* `wdev pdns zone add <domain>` memvalidasi FQDN, membuat zone `Native` dengan `ns1.<domain>` dan `ns2.<domain>`, membuat glue A ke IP publik VPS, mengaktifkan `SOA-EDIT-API` / `SOA-EDIT=INCEPTION-INCREMENT`, memverifikasi SOA + NS (rollback bila gagal), serta otomatis membuat **vhost HTML statis default** untuk domain tersebut bila belum ada.
* `wdev pdns zone doctor <domain>` memeriksa ketersediaan zone, NS `ns1`/`ns2`, glue A, delegasi NS dari resolver publik, serial SOA authoritative, dan keterjangkauan port 53.
* `wdev pdns zone repair <domain>` memperbaiki SOA, NS, glue A, dan metadata serial pada zone bermasalah tanpa menyentuh record lain.
* Penambahan record via `wdev pdns record add <zone> <name> <type> <val> [ttl]` bersifat *append* (tidak menimpa record lain dengan nama/tipe yang sama).
* Pada VPS Edge Gateway (`wdev edge-agent provision --yes`), PowerDNS API (`8081`) dan Edge Agent (`8082`) hanya dibuka untuk subnet NetBird `100.64.0.0/10`.

---

## Mail Server (Stalwart + SnappyMail)

`wdev` mengelola **Stalwart Mail Server** dan webmail **SnappyMail** pada mode **`vps`**, **`edge`**, maupun **`local`**:

* **Mode `vps`**: Stalwart, Nginx, PowerDNS, dan Let's Encrypt berjalan langsung di VPS publik.
* **Mode `edge`**: Mail server berjalan di mesin lokal dan dipublikasikan melalui VPS Edge Gateway lewat **NetBird** (`100.64.0.0/10`) — Nginx stream TLS (`465`/`587`/`993`/`143`/`995`/`110`), Postfix queue (`25`), serta proxy HTTPS `webmail.<domain>` & `mailadmin.<domain>`.
* **Mode `local`**: Mail stack berjalan lokal memakai `/etc/hosts` dan SSL lokal (`mkcert`/self-signed) untuk pengujian/dev.

### Outbound SMTP: Lokal Default & Relay Eksternal Opsional

* **Default**: Jika belum ada relay eksternal yang disetel, outbound memakai **SMTP lokal bawaan Stalwart (`mx` / direct)**.
* **Relay Eksternal (Global atau Per-Domain)**: Mendukung `resend`, `brevo`, atau `custom` SMTP relay. Relay global disimpan di `MtaOutboundStrategy.route.else`; override per domain memakai cabang `sender_domain == '<domain>'`. Gunakan `local` untuk kembali ke SMTP lokal Stalwart, atau `off` (`unset-relay`) untuk menghapus override domain.

```bash
# Instalasi & setup domain mail
sudo wdev install mail
sudo wdev mail setup domainanda.com
sudo wdev mail setup domainanda.com --relay resend --relay-key <API_KEY>

# Manajemen akun email
wdev mail accounts [domain]
wdev mail add-account halo@domainanda.com '<password_kuat>'
wdev mail change-password halo@domainanda.com '<password_baru>'
wdev mail admin-cred domainanda.com
wdev mail admin-reset domainanda.com

# Manajemen outbound relay & sinkronisasi DNS relay
wdev mail set-relay global brevo <SMTP_KEY>
wdev mail set-relay domainanda.com resend <RESEND_API_KEY>   # otomatis sync DNS Resend ke PowerDNS
wdev mail set-relay domainanda.com local                    # gunakan SMTP lokal Stalwart
wdev mail unset-relay domainanda.com                        # hapus override domain
wdev mail relay-dns domainanda.com list|add|remove ...

# Status, perbaikan, dan penghapusan domain
wdev mail status                       # cek status service, port lokal & eksternal (25/465/587/993/8080)
wdev mail repair-domain domainanda.com # perbaiki zone NS/SOA, A/MX/SPF/DMARC, vhost, & routing relay
wdev mail delete-domain domainanda.com [--keep-dns]
```

DNS inbound yang dibuat otomatis oleh `wdev mail setup`:

| Name | Type | Isi |
|---|---|---|
| `mail.<domain>` | A | IP publik VPS (atau IP lokal pada mode `local`) |
| `mx.<domain>` | A | IP publik VPS |
| `webmail.<domain>` | A | IP publik VPS |
| `mailadmin.<domain>` | A | IP publik VPS |
| `@` | MX | `10 mail.<domain>.` |
| `@` | TXT | `v=spf1 mx ~all` (SMTP lokal) atau `v=spf1 -all` (relay eksternal) |
| `_dmarc` | TXT | `v=DMARC1; p=none; sp=none; rua=mailto:dmarc@<domain>` |

> Catatan pengiriman internet publik: untuk pengiriman direct/inbound yang optimal di VPS, pastikan **PTR/rDNS** IP publik VPS menunjuk ke `mail.<domain>` dan port `25` tidak diblokir penyedia VPS. Penyimpanan data mail menggunakan `WDEV_MAIL_DATA` (default `/var/lib/wdev-mail`).

---

## Environment, PATH & Audit Duplikat

`wdev` menyatukan konfigurasi `PATH` dan variabel toolchain ke satu sumber di `/etc/profile.d/wdev.sh` (`GOROOT`, `CARGO_HOME`, `RUSTUP_HOME`, `NODE_HOME`, `FLUTTER_ROOT`, `ANDROID_HOME`, `CAROOT`, dll.).

```bash
wdev env status        # cek sumber PATH, file legacy, dan drift symlink
sudo wdev env sync     # bangun ulang /etc/profile.d/wdev.sh & rapikan ~/.bashrc / ~/.zshrc
wdev install audit     # deteksi biner duplikat, paket distro bentrok, atau double PATH
wdev install audit-fix # hapus duplikat otomatis dan selaraskan symlink kanonik
```

---

## Variabel Environment Konfigurasi

| Variabel | Default | Keterangan |
|---|---|---|
| `WDEV_ETC` | `/etc/wdev` (root) / `~/.config/wdev` | Direktori konfigurasi & kredensial `wdev` |
| `WDEV_DB` | `<WDEV_ETC>/wdev.db` | Lokasi database SQLite state `wdev` |
| `WDEV_PORT` | `3200` | Port HTTP/HTTPS Web Dashboard |
| `WDEV_HOST` | `0.0.0.0` | Host binding server panel |
| `WDEV_WWW_ROOT` | `/var/www` | Direktori dasar web root untuk vhost baru |
| `WDEV_TLS_CERT` / `WDEV_TLS_KEY` | — | Path sertifikat & private key TLS kustom untuk `wdev serve` |
| `WDEV_MAIL_DATA` | `/var/lib/wdev-mail` | Direktori penyimpanan data Stalwart & SnappyMail |
| `WDEV_FM_ROOTS` | — | Root tambahan untuk File Manager (dipisahkan `:`) |

---

## Distribusi & Rilis (Maintainer)

Rilis multi-arsitektur (`wdev-linux-amd64`, `wdev-linux-arm64`, `wdev-linux-armv7` beserta checksum `.sha256`) dibangun otomatis oleh [`.github/workflows/release.yml`](.github/workflows/release.yml) setiap kali tag `v*` di-push.

Gunakan asisten rilis otomatis melalui `Makefile`:

```bash
make next-version   # lihat deteksi versi SemVer otomatis dari Conventional Commits
make release-dry    # pratinjau versi & changelog tanpa mengubah git
make release        # update version.go, CHANGELOG.md, test, build, commit, tag, & push ke GitHub
```

Atau rilis manual via git tag:

```bash
git add -A && git commit -m "chore(release): bump version to v2.12.1"
git tag v2.12.1
git push origin master --tags
```

---

## Struktur Repo

```text
wdev/
├── cmd/
│ └── wdev/
│ └── main.go # Entrypoint CLI, launcher symlink Orca, & dispatch subperintah
├── frontend/ # Aplikasi Web UI (SvelteKit 5, Tailwind CSS v4, shadcn-svelte, xterm.js)
│ └── src/
│ ├── lib/ # API client, stores, components, modals, types, & i18n (en/id)
│ └── routes/ # Halaman dashboard (webstack, compose, ports, security, switchers, files, terminal, mail, pdns, setup, dll.)
├── internal/
│ ├── auth/ # Security Entrance, PBKDF2, session, rate limit, panel domain & SSL
│ ├── cloudflare/ # Multi-akun Cloudflare Tunnel & DNS (cert/token & API Token)
│ ├── compose/ # Manajer Docker Engine & Docker Compose v5
│ ├── config/ # Konfigurasi global, env vars, & deteksi user sistem
│ ├── edge/ # Provisioner & daemon agent VPS Edge Gateway
│ ├── filemanager/ # Backend File Manager, arsip, fetch URL, drive /mnt
│ ├── i18n/ # Lokalisasi respons backend (en/id)
│ ├── installer/ # Installer toolchain, repo resmi, audit PATH, Orca & Commit AI, PostgreSQL switch
│ ├── mail/ # Stalwart Mail Server, SnappyMail, outbound relay & DNS sync
│ ├── monitor/ # Telemetri CPU/RAM/Disk/Mount /mnt & status layanan
│ ├── pdns/ # PowerDNS Authoritative, mode server, zone doctor/repair
│ ├── runner/ # Task runner asinkron + log streaming SSE
│ ├── security/ # IDS/IPS watchers (SSH/Nginx/Mail/PortScan/Honeypot), firewall, blocklist & webhook
│ ├── server/ # HTTP API server, TLS mux, WebSocket PTY, port diagnostics, CSRF & security middleware
│ ├── serviceconfig/ # Inspeksi, preset, & konfigurasi Nginx, PHP-FPM, PostgreSQL, Redis, Docker
│ ├── site/ # Virtual host Nginx, mkcert, & Let's Encrypt
│ ├── siteapp/ # Manajer Managed App (Node.js/Go/Rust via systemd & .env)
│ ├── store/ # Penyimpanan state SQLite (modernc.org/sqlite)
│ ├── switcher/ # Pengelola multi-versi Node.js, PHP, PostgreSQL, Java
│ ├── terminal/ # Manajemen sesi PTY interaktif & audit
│ ├── tui/ # Terminal UI interaktif (10 kategori)
│ ├── updater/ # Auto-updater biner dari GitHub Releases / source lokal
│ └── version/ # Metadata versi rilis
├── scripts/                        # Otomasi SemVer, changelog, & release (next-version.sh, changelog.sh, release.sh)
├── web/                            # Asset hasil build frontend yang di-embed via go:embed
├── .github/workflows/release.yml   # CI/CD build multi-arch & GitHub Release
├── CHANGELOG.md
├── install.sh                      # One-click installer VPS (aaPanel style)
├── LICENSE                         # MIT License
├── Makefile                        # Target build, test, dev, & release
└── README.md
```

---

## Catatan Keamanan

* Perintah sistem pada biner `wdev` memerlukan hak akses `root` / `sudo` untuk menulis ke `/usr/local`, `/etc`, dan `/var`.
* **Keamanan Panel**: Selalu pertahankan Security Entrance (`wdev panel entrance`), proteksi login (`wdev panel auth on`), serta aktifkan HTTPS (`wdev panel ssl on` atau `wdev panel domain <domain>`) sebelum mengekspos panel ke jaringan publik.
* **Terminal PTY**: Sesi shell root melalui Web UI dilindungi oleh *step-up authentication* dengan masa berlaku izin sementara (10 menit) dan pencatatan audit.
* **Database & Cache**: Default Redis/PostgreSQL mendengarkan di localhost; jangan membuka akses eksternal tanpa kata sandi kuat dan pembatasan firewall.
* **Kredensial & Rahasia**: Kredensial Cloudflare, API key PowerDNS/Edge (`/etc/wdev-edge/api_key.secret`), kredensial mail (`/etc/wdev/mail-*.cred`), dan file `.env` disimpan dengan izin `0600` dan tidak boleh di-commit ke Git.
* **Local CA `mkcert`**: Hanya untuk pengembangan lokal; jangan gunakan sertifikat `mkcert` di lingkungan produksi publik.
* Jangan menambahkan host publik yang diekspos lewat Cloudflare Tunnel ke `/etc/hosts` lokal karena akan memotong jalur tunnel.

---

## Lisensi

Proyek ini dilisensikan di bawah **[MIT License](LICENSE)** — Copyright (c) 2026 `barisankoding-cell`.
