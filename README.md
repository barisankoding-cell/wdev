# wdev — Linux Developer Control Plane & Environment Manager

Aplikasi manajemen lingkungan pengembangan & infrastruktur Linux terpadu dalam satu biner Go mandiri (**Web UI Dashboard**, **TUI Terminal**, dan **CLI**) untuk toolchain multi-versi, web stack lokal, Cloudflare Tunnel, PowerDNS Authoritative, serta Orca ADE. Seluruh automasi berjalan native di dalam biner Go — tanpa skrip `.sh`.

---

## Fitur Utama v2.0 (Go Control Plane)

* 🌐 **Web UI Dashboard Tersemat (`wdev serve`)**: Antarmuka web modern & responsif di `http://localhost:3200` dengan live streaming log terminal via Server-Sent Events (SSE).
* 🖥️ **TUI Terminal Dashboard (`wdev tui`)**: Dashboard terminal interaktif dengan indikator status layanan (🟢 Aktif / 🔴 Nonaktif).
* 🗄️ **Penyimpanan State SQLite**: State profil tunnel, virtual host, dan DNS terintegrasi dan tersinkronisasi otomatis.
* ⚡ **CLI Terpadu**:
  * `wdev status`: Cek cepat status sistem, memori, disk, LAN IP, NetBird IP, dan layanan inti.
  * `wdev site list|create|delete`: Manajemen virtual host Nginx (Laravel, WordPress, CI4/3, Symfony, Proxy) dengan SSL otomatis (`mkcert`).
  * `wdev switch list|node|php|pg`: Pengelola versi runtime aktif.
  * `wdev cf status|expose|unexpose`: Manajemen Cloudflare Tunnel multi-akun & routing ingress.
  * `wdev pdns status|zone|record`: Manajemen PowerDNS & VPS Edge Gateway via Netbird.
  * `wdev install <komponen>`: Pasang atau perbarui komponen modular.

---

## Mulai Cepat

### 1. Instalasi Mandiri dari Biner Release (Stand-Alone)

Biner release `wdev` bersifat **mandiri (self-contained)** — seluruh Web UI tertanam via `go:embed` dan seluruh automasi instalasi/pembaruan dijalankan native oleh biner Go. Tidak ada skrip `.sh` yang perlu diunduh, disalin, atau dijalankan.

Cukup unduh biner rilis dan jalankan perintah install:

```bash
# 1. Berikan izin eksekusi pada biner release wdev
chmod +x wdev

# 2. Pasang wdev ke sistem & aktifkan systemd background service (auto-start)
sudo ./wdev install
```

Setelah instalasi selesai, Web Dashboard langsung aktif di:
👉 **`http://localhost:3200`**

---

### 2. Mengelola Layanan (CLI)

```bash
# Cek status layanan wdev
wdev service status wdev

# Restart / Stop / Start layanan
sudo wdev service restart wdev
sudo wdev service stop wdev
sudo wdev service start wdev

# Lihat log real-time
journalctl -u wdev.service -f
```

---

### 3. Uninstall Layanan & Biner

Untuk menghapus service systemd dan biner `wdev` secara mandiri dari biner:

```bash
# Hapus service systemd dan biner (data konfigurasi di /etc/wdev dipertahankan)
sudo wdev uninstall

# Atau hapus total termasuk seluruh database & konfigurasi
sudo wdev uninstall --purge
```

---

### 4. Instalasi Manual (Pengembang)

```bash
# Build & install biner wdev secara global
go build -ldflags="-s -w" -o wdev ./cmd/wdev
sudo install -m 755 ./wdev /usr/local/bin/wdev

# Pasang systemd service via CLI
sudo wdev install --port 3200

# Atau jalankan secara langsung di terminal (foreground)
wdev serve

# Atau jalankan Terminal UI Interaktif
wdev tui
```

### Mode Interaktif

Tanpa argumen, skrip menampilkan menu utama:

1. Status & Pembaruan Sistem
2. Pengelola Multi-Versi Runtime (Switchers)
3. Web Stack & Domain Lokal (`wdev site`)
4. Toolchains, Compilers & SDKs
5. Workspace, ADE & Shell
6. Cloudflare Tunnel & DNS
7. PowerDNS & VPS Edge Gateway (Netbird)
8. Instalasi Lengkap (Full Auto Setup)
9. Instalasi Komponen Modular (19 Item)

Struktur submenu:

| Menu | Isi |
|---|---|
| `[1]` | Status layanan, cek update versi, perbarui semua, self-check |
| `[2]` | Multi-versi Node.js, PHP, PostgreSQL, & Java OpenJDK (`wdev switch`) |
| `[3]` | Vhost lokal, SSL (`mkcert`), PHP switch; eksposur publik diarahkan ke `[6]` atau `[7]` |
| `[4]` | Rust, Go, Java, Flutter, Android SDK, Redis, build tools, & menu modular |
| `[5]` | Orca ADE (Desktop GUI / Headless Server), Oh My Zsh & tema prompt |
| `[6]` | Status, eksposur publik, akun, tunnel, sync, service, DNS, quick tunnel, custom routes & CIDR, Access, doctor, install |
| `[7]` | Status VPS Edge, auto-deploy SSH Netbird, kelola zone domain, record DNS (A/CNAME/TXT/MX), ekspos port aplikasi (Node/Go/PHP) |
| `[8]` | Instalasi penuh 14 langkah otomatis (dengan konfirmasi keamanan) |
| `[9]` | Instalasi modular per komponen (19 pilihan komponen terstruktur per kategori) |

### Opsi CLI

| Perintah | Keterangan |
|---|---|
| `wdev serve [--port 3200] [--host 0.0.0.0]` | Jalankan Web UI Dashboard |
| `wdev tui` | Jalankan Terminal UI interaktif |
| `wdev status` | Status layanan, versi, IP akses, dan status Orca |
| `wdev check-updates`, `wdev -cu` | Bandingkan versi terpasang vs terbaru (tanpa instalasi) |
| `wdev install all` | Instalasi lengkap seluruh stack |
| `wdev install update` | Perbarui semua paket yang terpasang |
| `wdev install self-check` | Validasi konfigurasi & lingkungan sistem |
| `wdev install <komponen>` | Instal/perbarui komponen tertentu |
| `wdev uninstall [--purge]` | Hapus service systemd & biner wdev |

Komponen instalasi: `all`, `update`, `check-updates`, `self-check`, `base`, `node`, `rust`, `go`, `java`, `android`, `flutter`, `postgres`, `redis`, `nginx`, `php`, `composer`, `ssl`, `php-stack`, `cloudflare`, `zsh`, `orca-desktop`, `orca-headless`, `orca-update`, `orca-uninstall`.

Pemilih versi saat instalasi (lewat Web UI / TUI / argumen `wdev install`):

| Komponen | Nilai | Default |
|---|---|---|
| Node.js | `20`, `22`, `24` | `22` |
| Java | `17`, `21`, `25` | `21` |
| Android API | `all`, `36`, `35`, `34` | `all` |
| PostgreSQL | `16`, `17`, `18` | `18` |
| PHP | `8.1`, `8.2`, `8.3`, `8.4`, `system` | `8.4` |
| Zsh theme | `powerlevel10k`, `agnoster`, `robbyrussell` | `powerlevel10k` |

Contoh:

```bash
wdev install node
wdev install php
wdev install postgres
wdev site sync
```

### Komponen yang Dipasang & Sumber Resmi

`wdev` memprioritaskan instalasi dari **rilis binary/tarball resmi upstream** serta **repositori resmi vendor** (untuk service daemon) pada keluarga Debian/Ubuntu (`apt-get`) maupun Fedora/RHEL (`dnf`/`yum`), sekaligus menyatukan environment di `/etc/profile.d/wdev.sh` dan membersihkan duplikasi paket distro yang bentrok:

| Komponen | Sumber & Jalur Kanonik |
|---|---|
| Dependensi dasar | `apt-get` / `dnf` (`gcc`, `clang`, `cmake`, `make`, `git`, `jq`, `curl`, dll.) |
| Node.js | Tarball resmi `nodejs.org/dist/` di `/usr/local/lib/nodejs` (`current` + `/usr/local/bin`), switch via `wdev switch node` / Web UI |
| Rust & Mold | `rustup` resmi di `/usr/local/cargo` & `/usr/local/rustup` + linker `mold` rilis GitHub `rui314/mold` di `/usr/local/mold` |
| Golang | Tarball resmi `go.dev/dl/` di `/usr/local/go` |
| OpenJDK Java | Tarball resmi **Eclipse Adoptium (Temurin)** di `/usr/lib/jvm/java-<ver>-temurin` + `update-alternatives` / `alternatives` |
| Android SDK | Command line tools resmi Google di `/usr/local/android-sdk` |
| Flutter | Repository resmi `flutter/flutter` (channel `stable`) di `/usr/local/flutter` |
| PostgreSQL | Repo resmi **PGDG** (`apt.postgresql.org` / `download.postgresql.org`) + fallback repo sistem; multi-versi via `wdev switch pg` / Web UI |
| Redis / Valkey | Repo resmi **`packages.redis.io`** (atau `valkey` pada Fedora) |
| Nginx | Repo resmi **`nginx.org/packages/mainline`** (fallback otomatis ke repo sistem bila versi distro belum tersedia) + `/etc/nginx/sites-enabled/*` |
| PHP & PHP-FPM | Repo resmi **Ondřej PPA** (Ubuntu), **Sury** (Debian), dan **Remi** (Fedora/RHEL) untuk `8.1`–`8.4` |
| Composer | Installer resmi `getcomposer.org` (`self-update` saat dijalankan ulang) di `/usr/local/bin/composer` |
| Local SSL | Rilis GitHub resmi `FiloSottile/mkcert` di `/usr/local/bin/mkcert` + CA lokal di `/etc/ssl/local-certs` |
| Cloudflared | Rilis GitHub resmi `cloudflare/cloudflared` di `/usr/local/bin/cloudflared` |
| Zsh | Paket `zsh` + Oh My Zsh, `zsh-autosuggestions`, `zsh-syntax-highlighting`, dan `powerlevel10k` (auto `git pull` saat update) |
| Orca ADE | Rilis GitHub resmi `stablyai/orca`: paket `.deb` `orca-ide_<versi>_<arch>.deb` diekstrak ke `~/.local/lib/orca-ide` (Desktop GUI & Headless Server) dengan deteksi/pembaruan versi di Web UI & CLI |

> **Auto Setup Idempoten (`wdev install all` / tombol Full Auto Setup):** Ketika dijalankan ulang, setiap langkah memeriksa versi/instalasi yang sudah ada terlebih dahulu—melewati unduhan bila sudah pada versi terbaru, memperbarui bila ada rilis lebih baru, serta menjaga `initdb`, layanan `systemd`, dan `/etc/profile.d/wdev.sh` tetap sinkron.

### Perintah CLI `wdev`

```bash
wdev status                          # Status sistem, layanan inti & runtime aktif
wdev install <komponen>              # all, update, check-updates, self-check, base, node, rust, go, java, android, flutter, postgres, redis, nginx, php, composer, ssl, php-stack, cloudflare, zsh, orca-desktop, orca-headless
wdev switch <list|node|php|pg|java>  # Pengelola versi runtime aktif
wdev site <list|create|delete>       # Virtual host Nginx + SSL + /etc/hosts
wdev cf <status|expose|unexpose>     # Cloudflare Tunnel multi-akun
wdev pdns <status|zone|record|...>   # PowerDNS Authoritative & VPS Edge Gateway
```

### Padanan Perintah (tanpa skrip shell)

Seluruh helper lama kini menjadi subperintah biner `wdev`:

| Helper lama | Perintah Go |
|---|---|
| `node-switch` | `wdev switch node <versi>` |
| `php-switch` | `wdev switch php <versi>` |
| `pg-switch` | `wdev switch pg <versi>` |
| `dev-site` | `wdev site list\|create\|delete\|sync` |
| `cf-tunnel` | `wdev cf status\|expose\|unexpose` |
| `pdns-mgr` | `wdev pdns ...` (termasuk `wdev mail ...`) |
| `orca-switch` | `wdev orca switch <desktop\|headless\|status\|autostart>` |
| `orca-update` | `wdev orca update` (atau `wdev install orca-update`) |
| `orca-pair` | `wdev orca pair` |
| `orca-desktop` | `wdev orca desktop` |

Contoh:

```bash
wdev switch node 20
sudo wdev switch php 8.3
sudo wdev site create myapp.test /var/www/myapp 8.4 laravel
sudo wdev site create mygo.test 3221 proxy
sudo wdev site list
sudo wdev site delete myapp.test

sudo wdev install cloudflare
sudo wdev cf expose myapp.test myapp.example.com --profile cf1
sudo wdev cf status

sudo wdev pdns status
sudo wdev pdns zone add domainanda.com
sudo wdev pdns expose api.domainanda.com 8080 --ssl
sudo wdev pdns expose-site myapp.test myapp.domainanda.com --ssl
sudo wdev pdns unexpose myapp.domainanda.com
```

## Orca ADE (Debian/Ubuntu)

Installer & pembaruan Orca ADE ditangani langsung oleh biner Go `wdev` — tanpa
skrip Bash. Paket resmi yang dipakai adalah `orca-ide_<versi>_<arch>.deb` dari
rilis GitHub `stablyai/orca`, lalu diekstrak ke ruang user
(`~/.local/lib/orca-ide`) sehingga tidak memerlukan `dpkg`/`apt` dan tidak
bertabrakan dengan paket sistem.

```bash
wdev orca update            # perbarui ke rilis resmi terbaru
wdev install orca-desktop   # pasang / perbarui mode Desktop GUI
wdev install orca-headless  # pasang / perbarui mode Headless Server
wdev install orca-uninstall # hapus Orca ADE
```

Dari Web UI, tombol **Perbarui ke vX.Y.Z** pada kartu Orca ADE memanggil alur
yang sama. Proses update: unduh `.deb` resmi -> ekstrak dengan `dpkg-deb`
(fallback `ar` + `tar`) -> ganti direktori aplikasi secara atomik -> aktifkan
kembali layanan sesuai mode -> segarkan cache versi.

Orca bersifat single-instance: hanya satu mode yang dipasang/dijalankan.

| Mode | Perilaku |
|---|---|
| Desktop | Entri menu `orca-ide.desktop`, autostart opsional (`wdev orca autostart en`) |
| Headless | Unit `orca.service` (systemd user, `Restart=always`) + `orca-update.timer` harian yang memanggil `wdev install orca-update`, `loginctl enable-linger` agar tetap jalan setelah boot tanpa login |

Lokasi:

- Aplikasi: `~/.local/lib/orca-ide`
- Launcher: `~/.local/bin/{orca,orca-ide,orca-desktop,orca-pair,orca-switch,orca-update}` — semuanya **symlink ke biner `wdev`** (kecuali `orca-ide` yang menunjuk CLI resmi Orca), bukan skrip shell
- Konfigurasi & state: `~/.config/orca` (`.mode`, `orca-runtime.json`, keypair pairing)
- Unit systemd user: `~/.config/systemd/user`

Dependensi: `dpkg-deb` (atau `ar` + `tar`) untuk mengekstrak paket `.deb`;
Chromium/Chrome untuk Browser Automation (dideteksi otomatis).

Pairing & mode:

```bash
wdev orca pair             # URL pairing + web client (via CLI resmi Orca)
wdev orca status           # mode, versi, layanan, pairing
wdev orca switch headless  # server 24/7
wdev orca switch desktop   # kembali ke GUI
```

Perintah lama (`orca pair`, `orca switch status`, `orca update`, `orca-desktop`)
tetap berfungsi karena merupakan symlink ke biner `wdev` yang sama.

## Cloudflare Tunnel & DNS (Multi-Akun)

Satu site lokal cukup diekspos lewat satu tunnel. Setiap akun Cloudflare = satu profil; satu profil = satu proses `cloudflared` + satu unit systemd + satu set credentials.

```
/etc/wdev/cloudflare/accounts/<profil>.conf   # zone, mode, protocol, tunnel id
/etc/wdev/cloudflare/accounts/<profil>.env    # TUNNEL_TOKEN untuk mode token (0600)
/etc/wdev/cloudflare/routes/<profil>.conf     # custom ingress routes (HTTP/TCP/SSH/RDP/Unix)
/etc/wdev/cloudflare/access/<profil>.conf     # aturan JWT Cloudflare Access per host
/etc/wdev/exposures/<domain>.conf             # public-host <TAB> profil (sumber kebenaran)
/etc/cloudflared/<profil>/config.yml          # generated oleh cf-tunnel sync
/etc/cloudflared/<profil>/<uuid>.json         # credentials tunnel (0600)
/etc/cloudflared/<profil>/cert.pem            # origin cert hasil login (0600)
/etc/systemd/system/cloudflared-<profil>.service
```

Aturan:

- `accounts/*.conf`, `routes/*.conf`, `access/*.conf`, dan `exposures/*.conf` dikelola `wdev`/Web UI, jangan diedit manual.
- `config.yml`, unit systemd, dan `server_name` Nginx bersifat generated.
- Satu host publik (non-path) hanya boleh dimiliki satu site/route dan satu profil.

### Instalasi

```bash
sudo wdev install cloudflare   # pasang/update cloudflared
wdev cf status                 # status tunnel, profil, dan eksposur aktif
```

Profil tunnel, ingress, dan unit systemd dikelola dari Web UI Dashboard
(tab **Cloudflare**) atau lewat API `wdev`. Tidak ada helper Bash terpisah.

- `local`: ingress dikelola file lokal (`/etc/cloudflared/<profil>/config.yml`), butuh `cert.pem` hasil `cloudflared tunnel login`.
- `token`: tunnel dijalankan dengan `TUNNEL_TOKEN` di `accounts/<profil>.env` (0600); ingress diatur di dashboard Cloudflare.

### Ekspos subdomain

```bash
wdev cf expose myapp.test app-dev.example.com --profile cf1
wdev cf expose myapp.test example.com --profile cf1   # apex domain
wdev cf unexpose myapp.test
wdev cf status
```

`expose` memvalidasi host terhadap zone profil, menulis state eksposur,
meregenerasi `server_name` Nginx, menerbitkan sertifikat `mkcert` dengan SAN
host publik, menyinkronkan ingress cloudflared, lalu membuat CNAME proxied ke
`<uuid>.cfargotunnel.com`.

### Operasional

```bash
wdev cf status
sudo systemctl status cloudflared-<profil>
sudo journalctl -u cloudflared-<profil> -f
sudo cloudflared tunnel list          # passthrough langsung ke binary resmi
```

### Batasan Platform Cloudflare

| Batasan | Dampak |
|---|---|
| Universal SSL gratis hanya `example.com` dan `*.example.com` | Subdomain bertingkat (`foo.dev.example.com`) butuh Total TLS/ACM di Cloudflare agar TLS publik valid |
| Tunnel harus di akun pemilik zone | Zone akun B tidak bisa memakai tunnel akun A; buat profil terpisah |
| Body request Cloudflare Free/Pro ~100 MB | `client_max_body_size 128M` tidak tercapai lewat tunnel |
| Timeout origin ~100 detik | Request panjang kena 524 |
| Cache Cloudflare | Aset dev bisa tersaji lama; kirim `Cache-Control: no-store` atau Cache Rule bypass |

## PowerDNS & VPS Edge Gateway (Native Go)

Deployment VPS sepenuhnya native Go — biner `wdev` yang sama diunggah ke VPS lalu
menjalankan provisioning sendiri (tanpa `vps-edge-setup.sh`, tanpa Python agent).

```bash
wdev pdns deploy <ip-vps> --user root --key ~/.ssh/id_ed25519
wdev pdns status
wdev pdns zone add domainanda.com
wdev pdns expose api.domainanda.com 8080 --ssl
```

Yang diprovision di VPS: PowerDNS Authoritative + SQLite3 + REST API, Nginx
(sites & stream), certbot, Postfix gateway, firewall, dan service
`wdev-edge-agent` (`wdev edge-agent serve`) yang mengelola reverse proxy,
sertifikat, serta TCP stream mail.

Provisioning bersifat destruktif dan **wajib konfirmasi**:

```bash
wdev edge-agent provision --help   # lihat opsi
wdev edge-agent provision --yes    # jalankan di VPS (root)
```

`wdev pdns deploy` memanggil `--yes` secara otomatis karena sudah melalui
konfirmasi di mesin lokal. IP publik yang dideteksi dibatasi IPv4 agar record
DNS `A` valid. API key PowerDNS dibuat sekali, disimpan di
`/etc/wdev-edge/api_key.secret` (0600), dan ditulis ke `edge.conf`.

Agent diekspos di port `8082` (API key di `/etc/wdev-edge/api_key.secret`);
PowerDNS API di port `8081`. Keduanya hanya dibuka untuk subnet NetBird
`100.64.0.0/10`.

## Environment & PATH (Satu Sumber)

PATH dan variabel toolchain hanya dihasilkan oleh wdev — tidak ada file
`/etc/profile.d/*.sh` lain atau blok PATH manual di rc yang ikut campur.

```bash
wdev env status   # cek sumber PATH, file legacy, dan drift symlink
wdev env sync     # bangun ulang /etc/profile.d/wdev.sh + rapikan rc shell
```

### Deteksi Duplikat Otomatis

Full Auto Setup (`wdev install all`) menjalankan audit **sebelum** (pre-flight)
dan **sesudah** (post-flight) 14 langkah instalasi:

```bash
wdev install audit        # deteksi duplikat path / double PATH
wdev install audit-fix    # perbaiki otomatis
```

Yang dideteksi:
- biner tool yang punya lebih dari satu realpath (mis. `/usr/local/bin/node` + `/usr/bin/node`)
- salinan asing di `/usr/local/bin` yang bukan symlink ke area kanonik
- paket distro yang menghasilkan biner duplikat (`nodejs`, `golang-go`, `rustc`, `mkcert`, `composer`, `cloudflared`, dll.)
- file `/etc/profile.d/*.sh` legacy di luar `wdev.sh`
- direktori toolchain kedua di home (`.cargo`, `.rustup`, `.nvm`, `.pyenv`)
- entri `_wdev_prepend_path` ganda di profil/rc

Setiap langkah instalasi komponen tunggal (`node`, `rust`, `go`, `flutter`, `composer`)
juga memanggil `GuardToolchainColumns` untuk memperingatkan duplikat lebih awal.
Perbaikan otomatis menghapus paket distro duplikat, membuang salinan asing, lalu
membangun ulang PATH tunggal. Symlink ke target identik **tidak** dianggap duplikat.

`wdev env sync` bersifat idempoten:
- menulis ulang `/etc/profile.d/wdev.sh` (GOROOT, CARGO_HOME, NODE_HOME, CAROOT, FLUTTER/ANDROID, ORCA_BROWSER)
- menghapus file profile lama (`nodejs.sh`, `rust.sh`, `golang.sh`, `flutter.sh`, `orca-browser.sh`, `mkcert.sh`, dll.)
- mengarahkan symlink `/usr/local/bin/*` ke sumber kanonik (`/usr/local/go`, `/usr/local/cargo`, `/usr/local/lib/nodejs/<versi>`)
- menaruh blok terkelola di `~/.bashrc`/`~/.zshrc` yang hanya me-load `wdev.sh`, dan membuang blok PATH duplikat

## Struktur Repo

```
wdev/
├── cmd/
│   └── wdev/
│       └── main.go
├── internal/
│   ├── config/
│ ├── edge/
│ │ ├── agent.go
│ │ └── provision.go
│   ├── installer/
│   │   ├── installer.go
│   │   ├── official_repos.go
│ │ ├── orca.go
│ │ ├── orca_runtime.go
│   │   ├── service.go
│   │   └── toolchains.go
│   ├── mail/
│   ├── monitor/
│   ├── pdns/
│   ├── runner/
│   ├── server/
│   ├── site/
│   ├── store/
│   ├── switcher/
│   └── tui/
├── web/
│   ├── embed.go
│   └── static/
├── README.md
└── .gitignore
```

## Catatan Keamanan

- Perintah pada biner `wdev` membutuhkan `sudo` untuk menulis ke `/usr/local`, `/etc`, serta `/var`.
- Default `DEV_LAN_ACCESS=0`: Redis/PostgreSQL hanya mendengarkan di localhost. Akses LAN harus dibuka eksplisit dengan `--lan` dan mewajibkan password non-default.
- Default password Redis/PostgreSQL bersifat publik. Ganti lewat variabel environment bila layanan akan diakses dari luar mesin.
- Log instalasi (`/var/log/dev-install.log`) dibuat dengan mode `600` dan tidak menampilkan nilai password.
- CA `mkcert` ditambahkan ke trust store sistem dan browser; hanya untuk pengembangan lokal.
- Kredensial Cloudflare (`accounts/*.conf`, `cert.pem`, `*.json`, `.env`) disimpan mode `0600` di bawah `/etc/wdev` dan `/etc/cloudflared`; jangan di-commit.
- Gunakan API token Cloudflare ber-scope minimal (`Zone:DNS:Edit`, `Zone:Read`), bukan Global API Key.
- Tunnel mengekspos vhost ke internet. Pasang Cloudflare Access dari dashboard Zero Trust untuk site yang bukan demo statis.
- Jangan menambahkan host publik ke `/etc/hosts`; itu memotong jalur tunnel di mesin lokal.
- Jangan commit kredensial, token, atau berkas `.env`; gunakan `.gitignore` yang disertakan.
- Review perubahan konfigurasi sebelum menjalankan perintah `wdev` sebagai root.

## Lisensi

Belum ada lisensi yang disertakan pada repo ini.
