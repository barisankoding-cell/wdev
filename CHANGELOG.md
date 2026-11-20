# Changelog

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
