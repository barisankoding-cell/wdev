# wdev — Linux Developer Environment Installer

Kumpulan skrip Bash untuk menyiapkan lingkungan pengembangan lengkap di Linux dalam satu perintah: toolchain multi-versi, web stack lokal, serta Orca ADE (Desktop GUI / Headless Server).

Dirancang untuk Fedora/RHEL (dnf/yum) dan Debian/Ubuntu (apt). `pacman` dan `zypper` terdeteksi, tetapi alur instalasi paketnya belum diimplementasikan.

## Isi Repo

| Berkas | Fungsi |
|---|---|
| `setup_dev.sh` | Manajer dev environment: menu interaktif + CLI, instalasi/update per komponen, helper multi-versi |
| `cf-tunnel.sh` | Helper manajemen Cloudflare Tunnel multi-akun (dipasang sebagai `cf-tunnel` oleh `setup_dev.sh`) |
| `pdns-mgr.sh` | Helper manajemen PowerDNS Authoritative & VPS Edge Gateway via Netbird (dipasang sebagai `pdns-mgr`) |
| `vps-edge-setup.sh` | Installer mandiri 1-klik untuk VPS Edge Gateway (PowerDNS + SQLite3 + Nginx + Netbird Agent) |
| `setup_orca_headless.sh` | Installer Orca ADE (Desktop GUI atau Headless Server), satu mode aktif per mesin |

## Persyaratan

- Linux x86_64 atau arm64
- Akses `root`/`sudo`
- Koneksi internet (unduhan dari repo distro, GitHub, nodejs.org, getcomposer.org)
- `bash` 4+, `curl`, `git` (dipasang otomatis oleh langkah dependensi dasar)

## Mulai Cepat

```bash
git clone https://github.com/barisankoding-cell/wdev.git
cd wdev
chmod +x setup_dev.sh setup_orca_headless.sh

sudo ./setup_dev.sh            # menu interaktif
sudo ./setup_dev.sh --install  # instalasi penuh (14 langkah)
```

Setelah selesai: `source /etc/profile` atau buka terminal baru.

## `setup_dev.sh`

### Mode Interaktif

Tanpa argumen, skrip menampilkan menu utama:

1. Status & Pembaruan Sistem
2. Pengelola Multi-Versi Runtime (Switchers)
3. Web Stack & Domain Lokal (`dev-site`)
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
| `[2]` | Multi-versi Node.js (`node-switch`), PHP (`php-switch`), PostgreSQL (`pg-switch`), & Java OpenJDK |
| `[3]` | Vhost lokal, SSL (`mkcert`), PHP switch; eksposur publik diarahkan ke `[6]` atau `[7]` |
| `[4]` | Rust, Go, Java, Flutter, Android SDK, Redis, build tools, & menu modular |
| `[5]` | Orca ADE (Desktop GUI / Headless Server), Oh My Zsh & tema prompt |
| `[6]` | Status, eksposur publik, akun, tunnel, sync, service, DNS, quick tunnel, custom routes & CIDR, Access, doctor, install |
| `[7]` | Status VPS Edge, auto-deploy SSH Netbird, kelola zone domain, record DNS (A/CNAME/TXT/MX), ekspos port aplikasi (Node/Go/PHP) |
| `[8]` | Instalasi penuh 14 langkah otomatis (dengan konfirmasi keamanan) |
| `[9]` | Instalasi modular per komponen (19 pilihan komponen terstruktur per kategori) |

### Opsi CLI

| Opsi | Keterangan |
|---|---|
| `-i`, `--install`, `--all` | Instalasi lengkap 14 langkah (termasuk PHP Web Stack & Zsh) |
| `-u`, `--update` | Perbarui semua paket yang terpasang (8 langkah) |
| `-cu`, `--check-update` | Bandingkan versi terpasang vs terbaru (tanpa instalasi) |
| `-c`, `--check` | Tampilkan status layanan, versi, IP akses, Cloudflare, dan status Orca |
| `--self-check` | Validasi sintaks `setup_dev.sh` dan semua helper yang di-generate |
| `--cloudflare` | Install/perbarui `cloudflared` + helper `cf-tunnel` (tanpa membuat akun) |
| `--cf-tunnel <cmd>` | Teruskan ke helper `cf-tunnel` (profile-aware) |
| `--pdns <cmd>` | Teruskan ke helper `pdns-mgr` (status, deploy-vps, zone, record, expose) |
| `-h`, `--help` | Bantuan lengkap |

Opsi per komponen: `--base`, `--node`, `--rust`, `--go`, `--java`, `--android`, `--flutter`/`-fl`, `--postgres`/`--pg`, `--redis`, `--nginx`, `--php`, `--composer`, `--php-stack`/`--web-stack`, `--ssl`/`--mkcert`, `--zsh`/`--ohmyzsh`, `--sync`/`--sync-hosts`.

Opsi akses jaringan: `--lan`/`--lan-access` membuka PostgreSQL & Redis ke LAN/Netbird (mewajibkan password non-default), `--no-lan`/`--local-only` mengunci ke localhost (default).

Opsi Orca ADE dan passthrough helper:

| Opsi | Keterangan |
|---|---|
| `-o`, `--orca` | Setup Orca ADE (otomatis pilih desktop/headless) |
| `--orca-desktop`, `--orca-headless` | Paksa mode Orca tertentu |
| `--orca-uninstall [--purge\|--keep-config]` | Hapus instalasi Orca ADE |
| `--orca-switch <mode>` | Teruskan ke `orca-switch` |
| `--orca-ai` | Konfigurasi default Git Commit Generator AI |
| `-p`, `--orca-pair` | Tampilkan QR code & URL pairing |
| `--dev-site`, `--node-switch`, `--php-switch`, `--pg-switch` | Teruskan ke helper terkait |

Pemilih versi saat instalasi:

| Opsi | Nilai | Default |
|---|---|---|
| `--node-ver` | `20`, `22`, `24` | `22` |
| `--java-ver` | `17`, `21`, `25` | `21` |
| `--android-api` | `all`, `36`, `35`, `34` | `all` |
| `--pg-ver` | `16`, `17`, `18` | `18` |
| `--php-ver` | `8.1`, `8.2`, `8.3`, `8.4`, `system` | `8.4` |
| `--php-exts` | `full`, `minimal`, daftar kustom | `full` |
| `--zsh-theme` | `powerlevel10k`, `agnoster`, `robbyrussell` | `powerlevel10k` |

Contoh:

```bash
sudo ./setup_dev.sh --node --node-ver 20
sudo ./setup_dev.sh --php --php-ver 8.3 --php-exts full
sudo ./setup_dev.sh --postgres --pg-ver 17
sudo ./setup_dev.sh --sync        # sinkronkan /etc/hosts, SSL, trust store
```

### Komponen yang Dipasang

| Komponen | Detail |
|---|---|
| Dependensi dasar | Build tools, clang, cmake, ninja, Xvfb, jq, GTK dev, dll. |
| Node.js | Multi-versi (`20`/`22`/`24`) di `/usr/local/lib/nodejs`, switch via `node-switch` |
| Rust | Toolchain + `mold` linker + `sccache`, `CARGO_HOME=/usr/local/cargo` |
| Golang | Rilis stabil terbaru di `/usr/local/go` |
| OpenJDK | Multi-versi `17`/`21`/`25` |
| Android SDK | Command line tools di `/usr/local/android-sdk` |
| Flutter | Stable channel di `/usr/local/flutter` |
| PostgreSQL | Multi-versi `16`/`17`/`18`, migrasi data via `pg-switch` |
| Redis/Valkey | Server cache lokal |
| Nginx | Mainline terbaru |
| PHP | Multi-versi `8.1`–`8.4` + PHP-FPM, preset ekstensi |
| Composer | Versi stabil terbaru |
| Local SSL | `mkcert` + CA lokal di `/etc/ssl/local-certs` |
| Zsh | Oh My Zsh, plugin populer, dan tema |
| Orca ADE | Integrasi pemasangan/pairing; memanggil `setup_orca_headless.sh` |

### Helper CLI

Dipasang ke `/usr/local/bin`; `node-switch`, `pg-switch`, `dev-site`, dan `cf-tunnel` juga disalin ke `~/.local/bin` milik `SUDO_USER`:

| Perintah | Fungsi |
|---|---|
| `node-switch [list\|<ver>\|install\|remove]` | Kelola versi Node.js |
| `pg-switch [list\|<ver>\|switch\|backup\|restore]` | Kelola cluster PostgreSQL dan migrasi data |
| `php-switch <ver\|list>` | Ganti versi PHP CLI & PHP-FPM aktif |
| `dev-site create\|delete\|list\|cert\|sync\|custom` | Virtual host Nginx + SSL + entri `/etc/hosts` + deteksi framework (Laravel, WordPress, CI3/CI4, Symfony, generic, proxy) |
| `dev-site expose\|unexpose\|exposures` | Ekspos vhost ke Cloudflare Tunnel, kelola host publik per profil |
| `cf-tunnel account\|tunnel\|route\|quick\|cidr\|access\|sync\|dns\|service\|status\|doctor\|raw` | Manajemen multi-akun Cloudflare: tunnel, ingress multi-protokol, quick tunnel, CIDR, Access, DNS, systemd, diagnostik |
| `pdns-mgr deploy-vps\|connect\|status\|zone\|record\|expose\|expose-site\|unexpose\|list-exposures` | Manajemen PowerDNS & VPS Edge Gateway via Netbird: authoritative DNS mandiri, SSL otomatis, dan auto-cleanup DNS saat unexpose |
| `orca-switch [desktop\|headless\|autostart\|status]` | Ganti mode Orca ADE |

Contoh:

```bash
node-switch 20
sudo php-switch 8.3
sudo dev-site create myapp.test /var/www/myapp 8.4 laravel
sudo dev-site create mygo.test 3221 proxy
sudo dev-site list
sudo dev-site delete myapp.test

sudo ./setup_dev.sh --cloudflare
sudo cf-tunnel account add cf1
sudo dev-site expose myapp.test myapp.example.com --profile cf1
sudo dev-site expose mygo.test api.example.com --profile cf1
sudo cf-tunnel status --all

# Manajemen PowerDNS & VPS Edge Gateway (Netbird)
sudo pdns-mgr status
sudo pdns-mgr zone add domainanda.com
sudo pdns-mgr expose api.domainanda.com 8080 --ssl
sudo pdns-mgr expose-site myapp.test myapp.domainanda.com --ssl
sudo pdns-mgr unexpose myapp.domainanda.com   # otomatis hapus proxy VPS, record DNS subdomain, & alias lokal
```

### Variabel Environment

| Variabel | Default | Keterangan |
|---|---|---|
| `REDIS_PASSWORD` | `redis` | Password Redis/Valkey |
| `PG_SUPERUSER_PASSWORD` | `postgres` | Password superuser PostgreSQL |
| `DEV_LAN_ACCESS` | `0` | `1` = buka Redis/PostgreSQL ke LAN & Netbird, `0` = lokal saja |

Akses LAN juga bisa diaktifkan lewat flag `--lan`, atau dimatikan eksplisit lewat `--no-lan`.

Nilai password hanya boleh berisi `[A-Za-z0-9_.-]`. Nilai tidak valid akan **ditampilkan sebagai error** (tidak lagi diganti diam-diam ke default). Saat akses LAN aktif (`--lan`), password default `postgres`/`redis` ditolak.

### Log & Lock

- Log: `/var/log/dev-install.log`
- Lock: `/var/lock/dev-install.lock` (mencegah dua proses bersamaan; hapus manual bila proses sebelumnya mati mendadak)

## `setup_orca_headless.sh`

Installer Orca ADE (mengunduh rilis `.deb` dari GitHub `stablyai/orca` dan mengekstraknya ke ruang user).

```bash
./setup_orca_headless.sh              # auto: desktop bila DE terdeteksi, headless bila tidak
./setup_orca_headless.sh --desktop    # paksa Desktop GUI
./setup_orca_headless.sh --headless   # paksa Headless Server
./setup_orca_headless.sh --uninstall  # hapus Orca ADE (tambahkan --purge untuk hapus ~/.config/orca)
./setup_orca_headless.sh --help
```

Orca bersifat single-instance: hanya satu mode yang dipasang/dijalankan.

| Mode | Perilaku |
|---|---|
| Desktop | Launcher `orca-desktop`, entri menu, autostart opsional |
| Headless | Unit `orca.service` (systemd user, `Restart=always`) + `orca-update.timer` harian, `loginctl enable-linger` agar tetap jalan setelah boot tanpa login |

Lokasi:

- Aplikasi: `~/.local/lib/orca-ide`
- Binari/helper: `~/.local/bin` (`orca`, `orca-ide`, `orca-desktop`, `orca-pair`, `orca-update`, `orca-switch`)
- Konfigurasi & state: `~/.config/orca` (`.mode`, `orca-runtime.json`, keypair pairing)
- Unit systemd user: `~/.config/systemd/user`

Dependensi: `curl`, `node`, `dpkg-deb` atau `ar`; `Xvfb` untuk headless; Chromium/Chrome untuk Browser Automation (dipasang otomatis bila memungkinkan).

Pairing:

```bash
orca pair            # URL Web/WebSocket + QR code
orca switch status   # status mode, proses, layanan, IP akses
orca switch headless # pindah ke server 24/7
```

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

- `accounts/*.conf`, `routes/*.conf`, `access/*.conf`, dan `exposures/*.conf` dikelola CLI, jangan diedit manual.
- `config.yml`, unit systemd, dan `server_name` Nginx bersifat generated.
- Satu host publik (non-path) hanya boleh dimiliki satu site/route dan satu profil.

### Instalasi

```bash
sudo ./setup_dev.sh --cloudflare        # cloudflared + helper cf-tunnel (tanpa akun)
sudo ./setup_dev.sh --cf-tunnel account add cf1
sudo ./setup_dev.sh --cf-tunnel account add cf2 --zone example.net
```

`account add` melakukan login origin cert, membuat tunnel, menulis credentials, membuat config + unit `cloudflared-<profil>`, lalu mengaktifkan service. Pilihan yang tersedia: `--mode local|token`, `--protocol auto|quic|http2`, `--zone`, `--api-token`, `--tunnel-token`, `--account-id`, `--no-login`, `--no-start`. Profil yang sudah ada dapat diperbarui via `cf-tunnel account update <profil>`.

- `local` (default): ingress dikelola file lokal (`config.yml`), butuh `cert.pem` dari `tunnel login`.
- `token`: tunnel dijalankan dengan `TUNNEL_TOKEN` di `accounts/<profil>.env` (0600). Ingress dapat diatur di dashboard Cloudflare atau disinkronkan via API dengan `sudo cf-tunnel sync --remote --profile <profil>` (membutuhkan `CF_API_TOKEN` dengan izin Tunnel Write).

### Ekspos subdomain & Custom Routes

```bash
sudo dev-site expose myapp.test app-dev.example.com --profile cf1
sudo dev-site expose myapp.test example.com --profile cf1                  # apex domain (CNAME flattening)
sudo dev-site expose api.test v1.dev.example.com --profile cf1 --allow-deep # butuh Total TLS / ACM
sudo dev-site exposures
sudo dev-site unexpose myapp.test app-dev.example.com
```

`expose` mendukung profil mode `local` maupun `token`: validasi host terhadap zone profil, cek konflik host antar site/route, tulis state eksposur, regenerasi `server_name`, regenerasi cert `mkcert` dengan SAN host publik, jalankan `cf-tunnel sync` (untuk mode `token`, menampilkan opsi `cf-tunnel sync --remote` dan langkah dashboard), lalu membuat CNAME proxied ke `<uuid>.cfargotunnel.com`. `delete` dan `unexpose` membersihkan DNS + ingress + SAN, vhost lokal tetap aman.

Untuk layanan non-Nginx, multi-protokol, routing path, Quick Tunnel, Private Network (CIDR), dan Cloudflare Access:

```bash
# Quick Tunnel (tanpa akun & domain, menghasilkan URL *.trycloudflare.com)
cf-tunnel quick 3000
cf-tunnel quick mygo.test --bg --name goapp
cf-tunnel quick status && cf-tunnel quick stop goapp

# Custom Ingress Routes (HTTP/HTTPS/TCP/SSH/RDP/SMB/Unix + path routing)
sudo cf-tunnel route add --profile cf1 api.example.com http://127.0.0.1:8080 --path '/api/.*'
sudo cf-tunnel route add --profile cf1 ssh.example.com ssh://127.0.0.1:22
sudo cf-tunnel route add --profile cf1 db.example.com tcp://127.0.0.1:5432
sudo cf-tunnel route list --all

# Private Network / WARP CIDR Routing
sudo cf-tunnel cidr add --profile cf1 10.10.0.0/24 --comment "LAN dev"
sudo cf-tunnel cidr list --profile cf1

# Cloudflare Access (Zero Trust App + validasi JWT di tunnel & client proxy)
sudo cf-tunnel access protect --profile cf1 app-dev.example.com --team myteam --allow-domains example.com
cf-tunnel access ssh --hostname ssh.example.com

# Passthrough langsung ke binary cloudflared dengan context profil
sudo cf-tunnel raw --profile cf1 tunnel list
```

### Operasional

```bash
sudo cf-tunnel status --all           # profil, zone, service, connector, eksposur, route
sudo cf-tunnel account list
sudo cf-tunnel account update cf1 --protocol http2
sudo cf-tunnel sync --all             # regenerate ingress + validasi + restart bila berubah
sudo cf-tunnel sync --remote --profile cf2  # push ingress ke API untuk mode token
sudo cf-tunnel tunnel info --profile cf1    # didukung untuk mode local maupun token
sudo cf-tunnel tunnel rotate --profile cf1
sudo cf-tunnel dns ensure --profile cf1
sudo cf-tunnel service restart --profile cf1
sudo cf-tunnel doctor --all           # cert, credentials, ingress, DNS, origin lokal & route
sudo cf-tunnel account remove cf2 --yes
```

Tanpa `CF_API_TOKEN`, DNS tidak diubah otomatis; `cf-tunnel` mencetak CNAME yang harus dibuat manual. Token butuh scope `Zone:DNS:Edit` + `Zone:Read` (opsional `Cloudflare Tunnel:Edit`, `Cloudflare One Networks:Edit`, `Access: Apps and Policies:Edit` untuk sync remote/CIDR/Access otomatis).

### Batasan Platform Cloudflare

| Batasan | Dampak |
|---|---|
| Universal SSL gratis hanya `example.com` dan `*.example.com` | Subdomain bertingkat (`foo.dev.example.com`) butuh flag `--allow-deep` dan Total TLS/ACM di Cloudflare agar TLS publik valid |
| Tunnel harus di akun pemilik zone | Zone akun B tidak bisa memakai tunnel akun A; buat profil terpisah |
| Body request Cloudflare Free/Pro ~100 MB | `client_max_body_size 128M` tidak tercapai lewat tunnel |
| Timeout origin ~100 detik | Request panjang kena 524 |
| Cache Cloudflare | Aset dev bisa tersaji lama; kirim `Cache-Control: no-store` atau Cache Rule bypass |
| Quick Tunnel (`trycloudflare.com`) | Hanya untuk URL/port langsung atau vhost `proxy`; tidak mendukung override `Host` header ke vhost PHP Nginx |

## Struktur Repo

```
wdev/
├── setup_dev.sh
├── cf-tunnel.sh
├── setup_orca_headless.sh
├── README.md
└── .gitignore
```

## Catatan Keamanan

- Skrip membutuhkan `sudo` dan menulis ke `/usr/local`, `/etc`, serta `/var`.
- Default `DEV_LAN_ACCESS=0`: Redis/PostgreSQL hanya mendengarkan di localhost. Akses LAN harus dibuka eksplisit dengan `--lan` dan mewajibkan password non-default.
- Default password Redis/PostgreSQL bersifat publik. Ganti lewat variabel environment bila layanan akan diakses dari luar mesin.
- Log instalasi (`/var/log/dev-install.log`) dibuat dengan mode `600` dan tidak menampilkan nilai password.
- CA `mkcert` ditambahkan ke trust store sistem dan browser; hanya untuk pengembangan lokal.
- Kredensial Cloudflare (`accounts/*.conf`, `cert.pem`, `*.json`, `.env`) disimpan mode `0600` di bawah `/etc/wdev` dan `/etc/cloudflared`; jangan di-commit.
- Gunakan API token Cloudflare ber-scope minimal (`Zone:DNS:Edit`, `Zone:Read`), bukan Global API Key.
- Tunnel mengekspos vhost ke internet. Aktifkan Cloudflare Access (`sudo cf-tunnel access protect ...`) untuk site yang bukan demo statis; `dev-site expose` tidak memasang Access otomatis.
- Jangan menambahkan host publik ke `/etc/hosts`; itu memotong jalur tunnel di mesin lokal.
- Jangan commit kredensial, token, atau berkas `.env`; gunakan `.gitignore` yang disertakan.
- Review skrip sebelum menjalankannya sebagai root.

## Lisensi

Belum ada lisensi yang disertakan pada repo ini.
