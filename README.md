# wdev — Linux Developer Environment Installer

Kumpulan skrip Bash untuk menyiapkan lingkungan pengembangan lengkap di Linux dalam satu perintah: toolchain multi-versi, web stack lokal, serta Orca ADE (Desktop GUI / Headless Server).

Dirancang untuk Fedora/RHEL (dnf/yum) dan Debian/Ubuntu (apt). `pacman` dan `zypper` terdeteksi, tetapi alur instalasi paketnya belum diimplementasikan.

## Isi Repo

| Berkas | Fungsi |
|---|---|
| `setup_dev.sh` | Manajer dev environment: menu interaktif + CLI, instalasi/update per komponen, helper multi-versi |
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
6. Instalasi Lengkap (Full Auto Setup)

### Opsi CLI

| Opsi | Keterangan |
|---|---|
| `-i`, `--install`, `--all` | Instalasi lengkap 14 langkah (termasuk PHP Web Stack & Zsh) |
| `-u`, `--update` | Perbarui semua paket yang terpasang (7 langkah) |
| `-cu`, `--check-update` | Bandingkan versi terpasang vs terbaru (tanpa instalasi) |
| `-c`, `--check` | Tampilkan status layanan, versi, IP akses, dan status Orca |
| `--self-check` | Validasi sintaks `setup_dev.sh` dan semua helper yang di-generate |
| `-h`, `--help` | Bantuan lengkap |

Opsi per komponen: `--base`, `--node`, `--rust`, `--go`, `--java`, `--android`, `--flutter`/`-fl`, `--postgres`/`--pg`, `--redis`, `--nginx`, `--php`, `--composer`, `--php-stack`/`--web-stack`, `--ssl`/`--mkcert`, `--zsh`/`--ohmyzsh`, `--sync`/`--sync-hosts`.

Opsi akses jaringan: `--lan`/`--lan-access` membuka PostgreSQL & Redis ke LAN/Netbird (mewajibkan password non-default), `--no-lan`/`--local-only` mengunci ke localhost (default).

Opsi Orca ADE dan passthrough helper:

| Opsi | Keterangan |
|---|---|
| `-o`, `--orca` | Setup Orca ADE (otomatis pilih desktop/headless) |
| `--orca-desktop`, `--orca-headless` | Paksa mode Orca tertentu |
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

Dipasang ke `/usr/local/bin`; `node-switch`, `pg-switch`, dan `dev-site` juga disalin ke `~/.local/bin` milik `SUDO_USER`:

| Perintah | Fungsi |
|---|---|
| `node-switch [list\|<ver>\|install\|remove]` | Kelola versi Node.js |
| `pg-switch [list\|<ver>\|switch\|backup\|restore]` | Kelola cluster PostgreSQL dan migrasi data |
| `php-switch <ver\|list>` | Ganti versi PHP CLI & PHP-FPM aktif |
| `dev-site create\|delete\|list\|cert\|sync\|custom` | Virtual host Nginx + SSL + entri `/etc/hosts` + deteksi framework (Laravel, WordPress, CI3/CI4, Symfony, generic) |
| `orca-switch [desktop\|headless\|autostart\|status]` | Ganti mode Orca ADE |

Contoh:

```bash
node-switch 20
sudo php-switch 8.3
sudo dev-site create myapp.test /var/www/myapp 8.4 laravel
sudo dev-site list
sudo dev-site delete myapp.test
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

## Struktur Repo

```
wdev/
├── setup_dev.sh
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
- Jangan commit kredensial, token, atau berkas `.env`; gunakan `.gitignore` yang disertakan.
- Review skrip sebelum menjalankannya sebagai root.

## Lisensi

Belum ada lisensi yang disertakan pada repo ini.
