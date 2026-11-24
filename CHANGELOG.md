# Changelog

## [v2.8.0] - 2026-11-24

### 🚀 Fitur Baru
- feat(compose): tambahkan pengelolaan Docker Compose dan instalasi Docker (8d3e107)
- feat(cloudflare): dukung pengelolaan tunnel multi-profil (172376f)
- feat: tambahkan diagnostik status port dan jaringan (0bc7dc4)

### 🐛 Perbaikan Bug
- fix: siapkan pnpm otomatis sebelum proses rilis (96a59c6)
- fix: selaraskan environment toolchain dan akses remote PostgreSQL (5a0e0a2)
- fix(installer): benahi deteksi dan kontrol layanan Orca (353a508)
- fix: isolasi konfigurasi server berdasarkan mode operasional (e463c5f)
- fix(security): perketat akses layanan internal dan terminal (a90ffdb)

## [v2.8.0] - 2026-11-24

### 🚀 Fitur Baru
- feat(cloudflare): dukung pengelolaan tunnel multi-profil (172376f)
- feat: tambahkan diagnostik status port dan jaringan (0bc7dc4)

### 🐛 Perbaikan Bug
- fix: selaraskan environment toolchain dan akses remote PostgreSQL (5a0e0a2)
- fix(installer): benahi deteksi dan kontrol layanan Orca (353a508)
- fix: isolasi konfigurasi server berdasarkan mode operasional (e463c5f)
- fix(security): perketat akses layanan internal dan terminal (a90ffdb)

## [v2.7.1] - 2026-11-23

### 🐛 Perbaikan Bug
- fix(orca): paksa keluar Desktop saat menerapkan konfigurasi Commit AI (343b425)
- fix(frontend): benahi fokus menu saat modal terbuka dan metadata halaman (dbe3154)

## [v2.7.0] - 2026-11-22

### 🚀 Fitur Baru
- feat: sinkronkan konfigurasi Commit AI Orca secara otomatis (148b82e)

### 🐛 Perbaikan Bug
- fix: buat modal konfigurasi Commit AI nyaman di layar kecil (f08fcd8)
- fix: perjelas sinkronisasi pengaturan AI Orca Web Client (ef7c3d7)
- fix: sinkronkan konfigurasi Orca Commit AI pada mode headless (d8a0334)

## [v2.6.0] - 2026-11-22

### 🚀 Fitur Baru
- feat: tambah fitur ubah virtual host dan rapikan UI mobile (6f514e6)
- feat: tambahkan paginasi pada daftar layanan dan riwayat (8b8d856)

### 🐛 Perbaikan Bug
- fix: pulihkan server_name kosong dan rapikan pengelolaan DNS (202e9e8)

## [v2.5.1] - 2026-11-22

### 🐛 Perbaikan Bug
- fix: tampilkan IP NetBird hanya saat layanan terpasang (18e5ab2)

## [v2.5.0] - 2026-11-22

### 🚀 Fitur Baru
- feat: perbarui identitas visual dan ringkasan sistem (03137ed)
- feat: tampilkan informasi disk eksternal dan mount tambahan (0ee1fef)
- feat: tingkatkan dashboard monitoring dan alur login (9acd932)
- feat: Tambahkan konfigurasi Orca Commit AI (be16cd2)
- feat: perbaiki domain panel Edge/DNS, modal vhost baru, dan auto-reload pasca-update (1127ae1)
- feat: tampilkan dan akses drive tambahan di `/mnt` (8e6c808)
- feat: optimalkan antarmuka file dan webstack untuk perangkat sentuh (db503d7)

### 🐛 Perbaikan Bug
- fix: teruskan HTTPS dan real IP reverse proxy Edge ke PHP serta amankan upgrade Nginx (348735f)
- fix: Rapikan tata letak tampilan mobile File Manager (6742368)
- fix: perkuat keamanan panel, agent, dan pembaruan edge (5861049)

### 🔧 Pemeliharaan & Tooling
- chore(web): perbarui aset hasil build frontend (d8cdc41)

## [v2.4.0] - 2026-11-21

### 🚀 Fitur Baru
- feat: tambah inspeksi PHP dan deteksi preset konfigurasi (1cb881a)
- feat: Tambahkan deteksi status komponen setup dan preset mode server (3ea96d0)
- feat: Tambahkan pengelolaan eksposur Edge dan pemilih folder (3029985)
- feat(mail): uji keterjangkauan port dari dalam dan luar VPS (be9a56d)
- feat(mail): sinkronkan DNS relay Resend otomatis ke PowerDNS (550cfdf)
- feat: Tambahkan HTTPS langsung untuk panel dengan sertifikat mandiri (9ef99b8)
- feat: dukung pengikatan domain HTTPS untuk panel wdev (6ff6e89)
- feat(terminal): tambahkan keamanan akses dan audit sesi PTY (6ac9117)
- feat: tambahkan terminal PTY dan fitur lanjutan file manager (b75af3c)
- feat: tambahkan File Manager untuk mengelola berkas situs (9ade4ce)
- feat: tampilkan ringkasan status port Stalwart_mail (b308bf0)
- feat: kelola role PostgreSQL dan ubah password akun email (2f2049d)
- feat(mail): tampilkan status aksesibilitas port Stalwart (918ceb5)
- feat: tambah pembuatan subdomain dan sinkronisasi DNS otomatis (cd11c3c)
- feat: otomatis pasang Mail Stack dan tampilkan masa berlaku SSL (ce4ba8f)
- feat(site): tambahkan penerbitan SSL Let's Encrypt untuk vhost VPS (0067278)
- feat: Buat vhost HTML statis otomatis saat menambah zone DNS (2ba29fd)
- feat(mail,ui,cli): dukung setup domain mail dengan SMTP lokal default tanpa wajib relay (94dc2de)
- feat: tingkatkan tampilan metrik sistem di dashboard (43c61ea)
- feat(mail,monitor,ui): perbaiki installer stalwart, persentase load per-core, dan deteksi ip mode vps (7ff6ffc)
- feat(installer): migrasikan database saat beralih versi PostgreSQL (18fd382)
- feat(pdns,mail,setup): dukung instalasi PowerDNS standalone serta mode lokal, edge, dan vps tanpa NetBird (7be023d)

### 🐛 Perbaikan Bug
- fix(site): hapus konfigurasi vhost dengan aman dan dukung sudo (74bedb1)
- fix(mail): sesuaikan pemeriksaan port dengan mode edge dan lokal (fc222bd)
- fix(mail): bedakan status port lokal dan akses publik (a627c82)
- fix: cegah perubahan panel lokal saat layanan sistem aktif (321f46b)
- fix(pdns): validasi dan cache IP publik VPS (ef18946)
- fix(ui): tampilkan indikator proses pada aksi layanan dan pengelolaan data (9900202)
- fix(frontend): pertahankan modal saat konfirmasi ditolak atau gagal (047974a)
- fix(monitor,pdns): dukung deteksi floating ip publik dan cegah fallback ke ip privat (36cd1c0)

### ⚡ Peningkatan & Refactoring
- refactor(ui): rapikan topbar dan tata letak konfigurasi layanan (ed4b406)
- refactor: rapikan aksi tabel ke menu dropdown responsif (31ccf3f)
- refactor(mail): ringkas aksi domain dalam menu dropdown (f59ff04)

### 🔧 Pemeliharaan & Tooling
- chore(release): rilis versi 2.4.0 (e57d600)
- test(mail): dukung nameserver saat membuat zona DNS palsu (0aefd8a)
- test: sesuaikan argumen konstruktor server pada pengujian DNS mail relay (ce1280e)

## [v2.4.0] - 2026-11-21

### 🚀 Fitur Baru
- feat: tambah inspeksi PHP dan deteksi preset konfigurasi (1cb881a)
- feat: Tambahkan deteksi status komponen setup dan preset mode server (3ea96d0)
- feat: Tambahkan pengelolaan eksposur Edge dan pemilih folder (3029985)
- feat(mail): uji keterjangkauan port dari dalam dan luar VPS (be9a56d)
- feat(mail): sinkronkan DNS relay Resend otomatis ke PowerDNS (550cfdf)
- feat: Tambahkan HTTPS langsung untuk panel dengan sertifikat mandiri (9ef99b8)
- feat: dukung pengikatan domain HTTPS untuk panel wdev (6ff6e89)
- feat(terminal): tambahkan keamanan akses dan audit sesi PTY (6ac9117)
- feat: tambahkan terminal PTY dan fitur lanjutan file manager (b75af3c)
- feat: tambahkan File Manager untuk mengelola berkas situs (9ade4ce)
- feat: tampilkan ringkasan status port Stalwart_mail (b308bf0)
- feat: kelola role PostgreSQL dan ubah password akun email (2f2049d)
- feat(mail): tampilkan status aksesibilitas port Stalwart (918ceb5)
- feat: tambah pembuatan subdomain dan sinkronisasi DNS otomatis (cd11c3c)
- feat: otomatis pasang Mail Stack dan tampilkan masa berlaku SSL (ce4ba8f)
- feat(site): tambahkan penerbitan SSL Let's Encrypt untuk vhost VPS (0067278)
- feat: Buat vhost HTML statis otomatis saat menambah zone DNS (2ba29fd)
- feat(mail,ui,cli): dukung setup domain mail dengan SMTP lokal default tanpa wajib relay (94dc2de)
- feat: tingkatkan tampilan metrik sistem di dashboard (43c61ea)
- feat(mail,monitor,ui): perbaiki installer stalwart, persentase load per-core, dan deteksi ip mode vps (7ff6ffc)
- feat(installer): migrasikan database saat beralih versi PostgreSQL (18fd382)
- feat(pdns,mail,setup): dukung instalasi PowerDNS standalone serta mode lokal, edge, dan vps tanpa NetBird (7be023d)

### 🐛 Perbaikan Bug
- fix(site): hapus konfigurasi vhost dengan aman dan dukung sudo (74bedb1)
- fix(mail): sesuaikan pemeriksaan port dengan mode edge dan lokal (fc222bd)
- fix(mail): bedakan status port lokal dan akses publik (a627c82)
- fix: cegah perubahan panel lokal saat layanan sistem aktif (321f46b)
- fix(pdns): validasi dan cache IP publik VPS (ef18946)
- fix(ui): tampilkan indikator proses pada aksi layanan dan pengelolaan data (9900202)
- fix(frontend): pertahankan modal saat konfirmasi ditolak atau gagal (047974a)
- fix(monitor,pdns): dukung deteksi floating ip publik dan cegah fallback ke ip privat (36cd1c0)

### ⚡ Peningkatan & Refactoring
- refactor(ui): rapikan topbar dan tata letak konfigurasi layanan (ed4b406)
- refactor: rapikan aksi tabel ke menu dropdown responsif (31ccf3f)
- refactor(mail): ringkas aksi domain dalam menu dropdown (f59ff04)

### 🔧 Pemeliharaan & Tooling
- test(mail): dukung nameserver saat membuat zona DNS palsu (0aefd8a)
- test: sesuaikan argumen konstruktor server pada pengujian DNS mail relay (ce1280e)

## [v2.4.0] - 2026-11-21

### 🚀 Fitur Baru
- feat: tambah inspeksi PHP dan deteksi preset konfigurasi (1cb881a)
- feat: Tambahkan deteksi status komponen setup dan preset mode server (3ea96d0)
- feat: Tambahkan pengelolaan eksposur Edge dan pemilih folder (3029985)
- feat(mail): uji keterjangkauan port dari dalam dan luar VPS (be9a56d)
- feat(mail): sinkronkan DNS relay Resend otomatis ke PowerDNS (550cfdf)
- feat: Tambahkan HTTPS langsung untuk panel dengan sertifikat mandiri (9ef99b8)
- feat: dukung pengikatan domain HTTPS untuk panel wdev (6ff6e89)
- feat(terminal): tambahkan keamanan akses dan audit sesi PTY (6ac9117)
- feat: tambahkan terminal PTY dan fitur lanjutan file manager (b75af3c)
- feat: tambahkan File Manager untuk mengelola berkas situs (9ade4ce)
- feat: tampilkan ringkasan status port Stalwart_mail (b308bf0)
- feat: kelola role PostgreSQL dan ubah password akun email (2f2049d)
- feat(mail): tampilkan status aksesibilitas port Stalwart (918ceb5)
- feat: tambah pembuatan subdomain dan sinkronisasi DNS otomatis (cd11c3c)
- feat: otomatis pasang Mail Stack dan tampilkan masa berlaku SSL (ce4ba8f)
- feat(site): tambahkan penerbitan SSL Let's Encrypt untuk vhost VPS (0067278)
- feat: Buat vhost HTML statis otomatis saat menambah zone DNS (2ba29fd)
- feat(mail,ui,cli): dukung setup domain mail dengan SMTP lokal default tanpa wajib relay (94dc2de)
- feat: tingkatkan tampilan metrik sistem di dashboard (43c61ea)
- feat(mail,monitor,ui): perbaiki installer stalwart, persentase load per-core, dan deteksi ip mode vps (7ff6ffc)
- feat(installer): migrasikan database saat beralih versi PostgreSQL (18fd382)
- feat(pdns,mail,setup): dukung instalasi PowerDNS standalone serta mode lokal, edge, dan vps tanpa NetBird (7be023d)

### 🐛 Perbaikan Bug
- fix(site): hapus konfigurasi vhost dengan aman dan dukung sudo (74bedb1)
- fix(mail): sesuaikan pemeriksaan port dengan mode edge dan lokal (fc222bd)
- fix(mail): bedakan status port lokal dan akses publik (a627c82)
- fix: cegah perubahan panel lokal saat layanan sistem aktif (321f46b)
- fix(pdns): validasi dan cache IP publik VPS (ef18946)
- fix(ui): tampilkan indikator proses pada aksi layanan dan pengelolaan data (9900202)
- fix(frontend): pertahankan modal saat konfirmasi ditolak atau gagal (047974a)
- fix(monitor,pdns): dukung deteksi floating ip publik dan cegah fallback ke ip privat (36cd1c0)

### ⚡ Peningkatan & Refactoring
- refactor(ui): rapikan topbar dan tata letak konfigurasi layanan (ed4b406)
- refactor: rapikan aksi tabel ke menu dropdown responsif (31ccf3f)
- refactor(mail): ringkas aksi domain dalam menu dropdown (f59ff04)

### 🔧 Pemeliharaan & Tooling
- test(mail): dukung nameserver saat membuat zona DNS palsu (0aefd8a)
- test: sesuaikan argumen konstruktor server pada pengujian DNS mail relay (ce1280e)

## [v2.3.0] - 2026-11-21

### 🚀 Fitur Baru
- feat(setup,server): rapikan installer modular dengan preset hosting & development serta instalasi batch (100bdc5)

### 🐛 Perbaikan Bug
- fix(updater): stage biner di direktori target untuk atasi error cross-device rename (797ea62)

### 🔧 Pemeliharaan & Tooling
- chore(release): tambah deteksi versi SemVer otomatis pada make release dan changelog (19bad6d)

## [v2.2.1] - 2026-11-21

### 🚀 Fitur Baru
- feat(netbird,installer): pisahkan instalasi dan update NetBird mandiri tanpa PowerDNS (da3e3d2)
- feat(updater): auto-check rilis GitHub, cache in-memory, banner notifikasi dan changelog di dashboard (760005b)

### 🐛 Perbaikan Bug
- fix(security): perkuat autentikasi, validasi input, izin file, dan proteksi HTTP panel (0e29aeb)
- fix(terminal,sse,store): pulihkan log live SSE, snapshot tugas aktif, dan scan riwayat SQLite (fc78a19)

### 🔧 Pemeliharaan & Tooling
- chore(gitignore): perluas ignore artefak build, test, dan rilis (c8badfb)

## [v2.2.0] - 2026-11-21

### 🚀 Fitur Baru
- feat(auth,mode): tambah security entrance aaPanel dan dual-mode server (Full VPS vs Edge) (4e5f7c9)
- feat(ui,server): migrasi dashboard ke SvelteKit + shadcn-svelte dan upgrade API control plane (1ff7e7e)
- feat(dashboard,installer): tambah modal config layanan, perbaikan update Orca, dan Zsh/Nerd Font (6c95c85)
- feat(mail): hapus/perbaiki domain dan kelola DNS relay otomatis (a35116c)

### 🐛 Perbaikan Bug
- fix(mail,ui): domain mail selalu tampil, modal tambah email, dan rapikan tombol ganda (435e2e3)

### 🔧 Pemeliharaan & Tooling
- chore(dev): tambah Makefile, scripts rilis & changelog otomatis, dan workflow GitHub Actions (36bd7b9)

## v2.1.1

### Added
- `wdev pdns zone doctor <domain>`: diagnosa zone PowerDNS (NS, glue A,
  delegasi publik, SOA primary/serial, port 53) dengan exit code non-zero
  saat ada kegagalan kritis.
- `wdev pdns zone repair <domain>`: menulis ulang SOA/NS/glue dan metadata
  `SOA-EDIT` untuk zone lama yang SOA-nya `a.misconfigured.dns.server.invalid`
  atau serial `0`. Record lain tidak diubah.
- Tombol **Doctor** dan **Repair** di tabel zone Web UI, plus endpoint
  `GET /api/pdns/zone/doctor` dan `POST /api/pdns/zone/repair`.
- DNS client native (UDP/TCP, parser compression) tanpa dependensi eksternal.

### Fixed
- `wdev pdns zone add` sekarang menulis SOA eksplisit (`ns1.<domain>`,
  serial baru) sehingga zone tidak lagi dibuat dengan SOA misconfigured.
- Zone baru langsung mendapat NS, glue A ke IP publik VPS, dan
  `SOA-EDIT=INCEPTION-INCREMENT`/`api_rectify`.
- Metadata `SOA-EDIT-API` yang read-only di PowerDNS 4.9 tidak lagi membuat
  repair gagal (fallback ke `SOA-EDIT`).
- Zone baru di-rollback bila SOA/NS/glue/metadata gagal ditulis.
- `FindMatchingZone` tidak lagi menebak nama zone; error jelas bila zone
  belum ada.
- Tambah record via CLI/Web UI bersifat append, bukan menimpa record lain
  dengan nama/tipe yang sama.
- `Expose` melaporkan kegagalan penulisan A record, tidak silent.

## v2.1.0

Rilis ini berfokus pada mail server relay-only, multi-domain, dan distribusi update.

### Added
- `wdev update` / `wdev self-update` untuk memasang biner rilis GitHub terbaru,
  verifikasi SHA256, backup biner lama, restart `wdev.service`, dan opsi
  `--edge root@vps --edge-key ~/.ssh/id_ed25519` untuk memperbarui edge agent.
- `wdev mail setup <domain>` untuk setup/repair domain dari CLI.
- `wdev install mail` untuk memasang Stalwart, stalwart-cli, SnappyMail, serta
  nginx/PHP-FPM bila belum ada.
- Preflight setup domain (`/api/mail/preflight`) dan panel blocker di Web UI.
- Tombol hapus akun email (khusus non-admin).

### Changed
- Mail outbound sepenuhnya relay-only. `direct`/`mx` ditolak di API, CLI, dan UI.
- Relay default global (`route.else`) dengan override per domain; match route
  disimpan terurut dan diverifikasi setelah update.
- `mailadmin.<domain>` diproxy ke Stalwart `:8080`; vhost `webmail.*` diganti
  vhost per domain.
- Stream mail mencakup 465/587/993/143/995/110; TLS memakai sertifikat SAN
  gabungan semua domain.
- Postfix: `myhostname` stabil dan `mynetworks` hanya IP NetBird mesin lokal.
- Data mail tidak lagi hardcoded (`WDEV_MAIL_DATA`, default `/var/lib/wdev-mail`).
- DNS inbound memakai IP publik VPS; MX/SPF/DMARC yang sudah ada tidak ditimpa.

### Fixed
- A record `"auto"` yang membuat MX tidak resolve.
- Panel admin `mailadmin` yang mengarah ke port salah.
- SPF `ip4:100.64.0.0/10` dan DKIM yang tidak pernah dipublikasikan.
- Race/silent error pada update `MtaOutboundStrategy`.
- Provision VPS yang mematikan Stalwart pada deploy all-in-one.

### Migration
```bash
sudo wdev update
sudo wdev install mail
sudo wdev mail set-relay global <provider> <key>
sudo wdev mail setup <domain>
wdev mail status
```
