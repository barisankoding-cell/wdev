#!/usr/bin/env bash
# ==============================================================================
# vps-edge-setup.sh — Installer VPS Edge Gateway untuk wdev
# Komponen: PowerDNS Authoritative + SQLite3 + Nginx Reverse Proxy + Edge Agent
# Terintegrasi dengan Netbird (WireGuard Mesh)
# ==============================================================================
set -euo pipefail

# Warna Output
C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"
C_DIM="\e[2m"

say()  { printf '%b\n' "$*"; }
ok()   { printf "${C_GREEN}[OK]${C_RESET} %b\n" "$*"; }
warn() { printf "${C_YELLOW}[!]${C_RESET} %b\n" "$*"; }
err()  { printf "${C_RED}[x]${C_RESET} %b\n" "$*" >&2; }
die()  { err "$*"; exit 1; }

# Validasi Root
if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  die "Skrip ini harus dijalankan dengan hak akses root (sudo $0)"
fi

CONFIG_DIR="/etc/wdev-edge"
PDNS_CONF="/etc/powerdns/pdns.conf"
PDNS_DB_DIR="/var/lib/powerdns"
PDNS_DB_FILE="$PDNS_DB_DIR/pdns.sqlite3"
AGENT_PORT="8082"
PDNS_API_PORT="8081"

say "${C_BOLD}${C_CYAN}"
say "=================================================================="
say "   wdev VPS Edge Gateway Installer (PowerDNS + Nginx + Netbird)   "
say "=================================================================="
say "${C_RESET}"

# 1. Deteksi Distro Linux
say "${C_BOLD}>>> [1/7] Mendeteksi Sistem Operasi & Dependensi Dasar...${C_RESET}"
OS_ID=""
if [ -f /etc/os-release ]; then
  # shellcheck source=/dev/null
  . /etc/os-release
  OS_ID="${ID:-linux}"
else
  die "Berkas /etc/os-release tidak ditemukan."
fi

case "$OS_ID" in
  debian|ubuntu|pop|linuxmint|dietpi|fedora|rhel|centos|almalinux|rocky)
    ;;
  *)
    die "Distro Linux '$OS_ID' saat ini belum didukung secara otomatis."
    ;;
esac

# 2. Deteksi Netbird, IPv6 & IP Publik VPS
say "${C_BOLD}>>> [2/7] Memeriksa Antarmuka Netbird (wt0), IPv6 & IP Publik VPS...${C_RESET}"

# Cek Dukungan IPv6 pada Kernel VPS
HAS_IPV6=0
if [ -f /proc/net/if_inet6 ] && [ "$(sysctl -n net.ipv6.conf.all.disable_ipv6 2>/dev/null || echo 1)" != "1" ]; then
  HAS_IPV6=1
  ok "Dukungan IPv6: ${C_GREEN}Aktif${C_RESET}"
else
  warn "Dukungan IPv6: ${C_YELLOW}Nonaktif / Tidak didukung kernel VPS${C_RESET} (Mode IPv4-only)"
fi

# Cek IP Publik
PUBLIC_IP=$(curl -4s --max-time 5 https://ifconfig.me || curl -4s --max-time 5 https://api.ipify.org || echo "")
if [ -z "$PUBLIC_IP" ]; then
  warn "Gagal mendeteksi IP Publik secara otomatis."
  read -r -p "Masukkan IP Publik VPS ini secara manual: " PUBLIC_IP
  [ -n "$PUBLIC_IP" ] || die "IP Publik diperlukan."
fi
ok "IP Publik VPS: ${C_BOLD}${PUBLIC_IP}${C_RESET}"

# Cek Antarmuka Netbird (wt0)
NETBIRD_IP=""
if ip -4 addr show dev wt0 >/dev/null 2>&1; then
  NETBIRD_IP=$(ip -4 addr show dev wt0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n 1)
fi

if [ -z "$NETBIRD_IP" ]; then
  warn "Antarmuka Netbird ('wt0') belum aktif atau belum terpasang di VPS ini."
  say "Netbird diperlukan agar VPS dan mesin lokal wdev terhubung dalam jaringan privat aman."
  say "Pilihan:"
  say " 1) Pasang Netbird sekarang (menggunakan skrip resmi Netbird)"
  say " 2) Lanjutkan sementara dengan IP 127.0.0.1 (Netbird dipasang belakangan)"
  read -r -p "Pilihan Anda [1/2]: " nb_choice
  if [ "$nb_choice" = "1" ]; then
    say "Memasang Netbird..."
    curl -fsSL https://pkgs.netbird.io/install.sh | sh || true
    say "Silakan masukkan Setup Key dari dashboard Netbird Anda:"
    read -r -p "Setup Key: " nb_key
    if [ -n "$nb_key" ]; then
      netbird up --setup-key "$nb_key" || true
      sleep 3
      NETBIRD_IP=$(ip -4 addr show dev wt0 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n 1 || echo "")
    fi
  fi
fi

if [ -z "$NETBIRD_IP" ]; then
  warn "Netbird belum aktif. Menggunakan fallback '0.0.0.0' untuk listener internal."
  NETBIRD_IP="0.0.0.0"
else
  ok "Netbird IP VPS: ${C_BOLD}${NETBIRD_IP}${C_RESET} (interface wt0)"
fi

# 3. Instalasi Paket
say "${C_BOLD}>>> [3/7] Menginstal Paket PowerDNS, Nginx, Certbot, SQLite3...${C_RESET}"
case "$OS_ID" in
  debian|ubuntu|pop|linuxmint|dietpi)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y

    # Siapkan direktori nginx dan tangani IPv6 jika dinonaktifkan di VPS
    mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
    if [ "$HAS_IPV6" -eq 0 ]; then
      if [ -f /etc/nginx/sites-available/default ]; then
        sed -i 's/listen \[::\]:80/#listen [::]:80/' /etc/nginx/sites-available/default
      fi
    fi
    dpkg --configure -a 2>/dev/null || true

    echo "postfix postfix/mailname string mail.wdev.local" | debconf-set-selections 2>/dev/null || true
    echo "postfix postfix/main_mailer_type string 'Internet Site'" | debconf-set-selections 2>/dev/null || true

    apt-get install -y --no-install-recommends \
      curl jq sqlite3 nginx certbot python3-certbot-nginx python3 pdns-server pdns-backend-sqlite3 ufw \
      rsyslog \
      libnginx-mod-stream postfix || {
        warn "Memperbaiki konfigurasi paket Nginx untuk mode IPv4-only..."
        if [ -f /etc/nginx/sites-available/default ]; then
          sed -i 's/listen \[::\]:80/#listen [::]:80/' /etc/nginx/sites-available/default
        fi
        dpkg --configure -a
        apt-get install -f -y
      }
    ;;
  fedora|rhel|centos|almalinux|rocky)
    dnf install -y \
      curl jq sqlite nginx certbot python3-certbot-nginx python3 pdns pdns-backend-sqlite3 firewalld rsyslog
    dnf install -y nginx-mod-stream 2>/dev/null || true
    ;;
esac

# Pastikan default vhost Nginx tidak mencoba bind IPv6 jika kernel tidak mendukung
if [ "$HAS_IPV6" -eq 0 ] && [ -f /etc/nginx/sites-available/default ]; then
  sed -i 's/listen \[::\]:80/#listen [::]:80/' /etc/nginx/sites-available/default
  systemctl restart nginx 2>/dev/null || true
fi
ok "Paket perangkat lunak berhasil dipasang."
systemctl enable --now rsyslog 2>/dev/null || true

# 4. Tangani Konflik Port 53 (systemd-resolved, bind9, dnsmasq, dll.)
say "${C_BOLD}>>> [4/7] Memastikan Port 53 Bebas untuk PowerDNS...${C_RESET}"

# Hentikan service DNS resolver yang mungkin menyandera port 53
for s in systemd-resolved bind9 named dnsmasq; do
  if systemctl is-active --quiet "$s" 2>/dev/null; then
    say "Menghentikan dan menonaktifkan '$s' agar port 53 tidak bentrok..."
    systemctl stop "$s" 2>/dev/null || true
    systemctl disable "$s" 2>/dev/null || true
  fi
done

# Pastikan DNS upstream VPS tetap dapat melakukan query ke internet
if [ -L /etc/resolv.conf ] || [ -f /etc/resolv.conf ]; then
  rm -f /etc/resolv.conf
  echo "nameserver 1.1.1.1" > /etc/resolv.conf
  echo "nameserver 8.8.8.8" >> /etc/resolv.conf
fi
ok "Port 53 siap digunakan oleh PowerDNS."

# 5. Konfigurasi PowerDNS & Database SQLite3
say "${C_BOLD}>>> [5/7] Mengonfigurasi PowerDNS Authoritative Server & REST API...${C_RESET}"
mkdir -p "$PDNS_DB_DIR" "$CONFIG_DIR" /etc/powerdns/pdns.d
chmod 775 "$PDNS_DB_DIR"
chown -R pdns:pdns "$PDNS_DB_DIR" 2>/dev/null || true

# Generate atau muat API Key
API_KEY_FILE="$CONFIG_DIR/api_key.secret"
if [ -f "$API_KEY_FILE" ]; then
  PDNS_API_KEY=$(cat "$API_KEY_FILE")
else
  PDNS_API_KEY=$(openssl rand -hex 24)
  echo "$PDNS_API_KEY" > "$API_KEY_FILE"
  chmod 600 "$API_KEY_FILE"
fi

# Inisialisasi Skema SQLite3 jika belum ada
if [ ! -f "$PDNS_DB_FILE" ] || [ ! -s "$PDNS_DB_FILE" ]; then
  say "Menginisialisasi skema database PowerDNS SQLite3..."

  # Cek jika ada berkas skema resmi dari paket pdns-backend-sqlite3
  OFFICIAL_SCHEMA="/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql"
  if [ -f "$OFFICIAL_SCHEMA" ]; then
    sqlite3 "$PDNS_DB_FILE" < "$OFFICIAL_SCHEMA"
  else
    sqlite3 "$PDNS_DB_FILE" <<'EOF'
PRAGMA foreign_keys = 1;

CREATE TABLE IF NOT EXISTS domains (
  id                    INTEGER PRIMARY KEY,
  name                  VARCHAR(255) NOT NULL COLLATE NOCASE,
  master                VARCHAR(128) DEFAULT NULL,
  last_check            INTEGER DEFAULT NULL,
  type                  VARCHAR(8) NOT NULL,
  notified_serial       INTEGER DEFAULT NULL,
  account               VARCHAR(40) DEFAULT NULL,
  options               VARCHAR(65535) DEFAULT NULL,
  catalog               VARCHAR(255) DEFAULT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS name_index ON domains(name);
CREATE INDEX IF NOT EXISTS catalog_idx ON domains(catalog);

CREATE TABLE IF NOT EXISTS records (
  id                    INTEGER PRIMARY KEY,
  domain_id             INTEGER DEFAULT NULL,
  name                  VARCHAR(255) DEFAULT NULL,
  type                  VARCHAR(10) DEFAULT NULL,
  content               VARCHAR(65535) DEFAULT NULL,
  ttl                   INTEGER DEFAULT NULL,
  prio                  INTEGER DEFAULT NULL,
  disabled              BOOLEAN DEFAULT 0,
  ordername             VARCHAR(255),
  auth                  BOOL DEFAULT 1,
  FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS records_lookup_idx ON records(name, type);
CREATE INDEX IF NOT EXISTS records_lookup_id_idx ON records(domain_id, name, type);
CREATE INDEX IF NOT EXISTS records_order_idx ON records(domain_id, ordername);

CREATE TABLE IF NOT EXISTS supermasters (
  ip                    VARCHAR(64) NOT NULL,
  nameserver            VARCHAR(255) NOT NULL COLLATE NOCASE,
  account               VARCHAR(40) NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS ip_nameserver_pk ON supermasters(ip, nameserver);

CREATE TABLE IF NOT EXISTS comments (
  id                    INTEGER PRIMARY KEY,
  domain_id             INTEGER NOT NULL,
  name                  VARCHAR(255) NOT NULL,
  type                  VARCHAR(10) NOT NULL,
  modified_at           INT NOT NULL,
  account               VARCHAR(40) DEFAULT NULL,
  comment               VARCHAR(65535) NOT NULL,
  FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS comments_idx ON comments(domain_id, name, type);
CREATE INDEX IF NOT EXISTS comments_order_idx ON comments (domain_id, modified_at);

CREATE TABLE IF NOT EXISTS domainmetadata (
 id                     INTEGER PRIMARY KEY,
 domain_id              INT NOT NULL,
 kind                   VARCHAR(32) COLLATE NOCASE,
 content                TEXT,
 FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS domainmetaidindex ON domainmetadata(domain_id);

CREATE TABLE IF NOT EXISTS cryptokeys (
 id                     INTEGER PRIMARY KEY,
 domain_id              INT NOT NULL,
 flags                  INT NOT NULL,
 active                 BOOL,
 published              BOOL DEFAULT 1,
 content                TEXT,
 FOREIGN KEY(domain_id) REFERENCES domains(id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS domainidindex ON cryptokeys(domain_id);

CREATE TABLE IF NOT EXISTS tsigkeys (
 id                     INTEGER PRIMARY KEY,
 name                   VARCHAR(255) COLLATE NOCASE,
 algorithm              VARCHAR(50) COLLATE NOCASE,
 secret                 VARCHAR(255)
);

CREATE UNIQUE INDEX IF NOT EXISTS namealgoindex ON tsigkeys(name, algorithm);
EOF
  fi
  ok "Skema database SQLite3 berhasil dibuat."
fi

# Migrasi otomatis jika database sebelumnya dibuat dengan skema lama (tambah kolom options & catalog)
if [ -f "$PDNS_DB_FILE" ]; then
  sqlite3 "$PDNS_DB_FILE" "ALTER TABLE domains ADD COLUMN options VARCHAR(65535) DEFAULT NULL;" 2>/dev/null || true
  sqlite3 "$PDNS_DB_FILE" "ALTER TABLE domains ADD COLUMN catalog VARCHAR(255) DEFAULT NULL;" 2>/dev/null || true
  sqlite3 "$PDNS_DB_FILE" "CREATE INDEX IF NOT EXISTS catalog_idx ON domains(catalog);" 2>/dev/null || true
fi

# Tulis pdns.conf
PDNS_LOCAL_ADDR="0.0.0.0"
if [ "$HAS_IPV6" -eq 1 ]; then
  PDNS_LOCAL_ADDR="0.0.0.0, ::"
fi

cat <<EOF > "$PDNS_CONF"
# PowerDNS Configuration - wdev VPS Edge Gateway
include-dir=/etc/powerdns/pdns.d

setuid=pdns
setgid=pdns

local-address=$PDNS_LOCAL_ADDR
local-port=53

# Backend SQLite3
launch=gsqlite3
gsqlite3-database=$PDNS_DB_FILE
gsqlite3-pragma-synchronous=1
gsqlite3-pragma-foreign-keys=1

# Built-in REST API
api=yes
api-key=$PDNS_API_KEY
webserver=yes
webserver-address=0.0.0.0
webserver-port=$PDNS_API_PORT
webserver-allow-from=127.0.0.1,100.64.0.0/10,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16

# Security Hardening
version-string=anonymous
security-poll-suffix=
EOF

# Pastikan permission aman dan dapat dibaca oleh user pdns
mkdir -p /etc/powerdns/pdns.d
chown root:pdns "$PDNS_CONF" 2>/dev/null || true
chmod 640 "$PDNS_CONF"
chown -R pdns:pdns "$PDNS_DB_DIR" 2>/dev/null || true
chown pdns:pdns "$PDNS_DB_FILE" 2>/dev/null || true
chmod 664 "$PDNS_DB_FILE"

systemctl enable pdns
if ! systemctl restart pdns; then
  err "PowerDNS gagal start! Menampilkan log kegagalan sistem (journalctl):"
  journalctl -u pdns.service -n 30 --no-pager
  exit 1
fi
ok "PowerDNS berhasil diaktifkan dan berjalan pada Port 53 (DNS) & Port $PDNS_API_PORT (API)."

# 6. Pasang wdev Edge Agent (Layanan Manajemen Proxy Otomatis)
say "${C_BOLD}>>> [6/7] Memasang wdev Edge Reverse-Proxy Agent...${C_RESET}"

mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled /etc/nginx/streams-enabled
# Pastikan nginx.conf memuat sites-enabled (HTTP) dan streams-enabled (TCP mail proxy)
if ! grep -q "sites-enabled" /etc/nginx/nginx.conf 2>/dev/null; then
  sed -i '/http {/a \    include /etc/nginx/sites-enabled/*;' /etc/nginx/nginx.conf
fi
if ! grep -q "streams-enabled" /etc/nginx/nginx.conf 2>/dev/null; then
  printf '\n# Managed by wdev-edge: TCP stream proxy untuk port mail\ninclude /etc/nginx/streams-enabled/*.conf;\n' >> /etc/nginx/nginx.conf
fi

# Pastikan nginx reload setelah certbot memperpanjang sertifikat, termasuk
# sertifikat yang dipakai oleh stream proxy IMAPS/SMTPS/POP3S.
mkdir -p /etc/letsencrypt/renewal-hooks/deploy
cat <<'EOF' > /etc/letsencrypt/renewal-hooks/deploy/wdev-reload-nginx.sh
#!/bin/sh
systemctl reload nginx >/dev/null 2>&1 || true
EOF
chmod 755 /etc/letsencrypt/renewal-hooks/deploy/wdev-reload-nginx.sh

# Buat daemon Python Edge Agent (Zero-dependency, hanya stdlib)
cat <<'AGENT_EOF' > /usr/local/bin/wdev-edge-agent
#!/usr/bin/env python3
import json
import os
import re
import shutil
import subprocess
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler

CONFIG_FILE = "/etc/wdev-edge/edge.conf"
API_KEY_FILE = "/etc/wdev-edge/api_key.secret"
NGINX_AVAILABLE = "/etc/nginx/sites-available"
NGINX_ENABLED = "/etc/nginx/sites-enabled"
NGINX_STREAMS_DIR = "/etc/nginx/streams-enabled"
NGINX_CONF = "/etc/nginx/nginx.conf"
MAIL_STREAM_CONF = os.path.join(NGINX_STREAMS_DIR, "wdev_mail.conf")
MAIL_TARGET_FILE = "/etc/wdev-edge/mail_target.json"
MAIL_DOMAINS_FILE = "/etc/wdev-edge/mail_domains.json"
MAIL_RELAYS_FILE = "/etc/wdev-edge/mail_relays.json"

def get_api_key():
    if os.path.exists(API_KEY_FILE):
        with open(API_KEY_FILE, "r") as f:
            return f.read().strip()
    return ""

def is_valid_domain(domain):
    return re.match(r'^(?!-)[A-Za-z0-9-]{1,63}(?<!-)(\.[A-Za-z0-9-]{1,63})*\.[A-Za-z]{2,6}$', domain) is not None

def has_ipv6():
    try:
        import socket
        s = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
        s.close()
        return True
    except OSError:
        return False

def ensure_postfix_installed(mail_host=""):
    # Stalwart lama di VPS tidak dipakai (mailbox ada di mesin lokal) dan
    # berebut port 25 dengan Postfix.
    subprocess.run(["systemctl", "disable", "--now", "stalwart"], check=False)
    if not os.path.exists("/usr/sbin/postfix"):
        subprocess.run(["sh", "-c", "echo 'postfix postfix/mailname string mail.wdev.local' | debconf-set-selections && echo 'postfix postfix/main_mailer_type string \"Internet Site\"' | debconf-set-selections && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends postfix 2>/dev/null || true"], check=False)
    os.makedirs("/etc/postfix", exist_ok=True)
    for f in ["/etc/postfix/relay_domains", "/etc/postfix/transport"]:
        if not os.path.exists(f):
            open(f, "a").close()
            subprocess.run(["postmap", f], check=False)
    # Jangan biarkan domain yang di-relay dianggap sebagai domain lokal VPS.
    # Gunakan hostname gateway yang berbeda dari Stalwart lokal (mail.<domain>)
    # agar Postfix tidak mendeteksi loop "greeted me with my own hostname".
    base_domain = mail_host[5:] if mail_host.startswith("mail.") else mail_host
    myhost = f"mx.{base_domain}" if base_domain else "mail.wdev.local"
    subprocess.run(["postconf", "-e", f"myhostname={myhost}"], check=False)
    subprocess.run(["postconf", "-e", f"smtp_helo_name={myhost}"], check=False)
    subprocess.run(["postconf", "-e", "mydestination=localhost.localdomain, localhost"], check=False)
    subprocess.run(["postconf", "-e", "relay_domains = hash:/etc/postfix/relay_domains"], check=False)
    subprocess.run(["postconf", "-e", "transport_maps = hash:/etc/postfix/transport"], check=False)
    # Relay ke Stalwart lokal tidak memakai SASL; gunakan TLS oportunistik tanpa verifikasi.
    subprocess.run(["postconf", "-e", "smtp_sasl_auth_enable=no"], check=False)
    subprocess.run(["postconf", "-e", "smtp_tls_security_level=may"], check=False)
    subprocess.run(["postconf", "-e", "smtp_tls_verify_cert=no"], check=False)
    subprocess.run(["postconf", "-e", "mynetworks = 127.0.0.0/8 [::1]/128 100.64.0.0/10"], check=False)
    subprocess.run(["postconf", "-e", "inet_interfaces = all"], check=False)
    subprocess.run(["postconf", "-e", "maximal_queue_lifetime = 5d"], check=False)
    subprocess.run(["postconf", "-e", "bounce_queue_lifetime = 5d"], check=False)
    subprocess.run(["systemctl", "enable", "postfix"], check=False)
    subprocess.run(["systemctl", "restart", "postfix"], check=False)

def mail_tcp_ports():
    return [25, 465, 587, 993, 143, 995, 110]

def is_listening(port):
    import socket
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.settimeout(1)
        return s.connect_ex(("127.0.0.1", port)) == 0

def mail_listener_status():
    return {str(p): is_listening(p) for p in mail_tcp_ports()}

def get_firewall_status():
    parts = []
    ufw = shutil.which("ufw")
    if ufw:
        st = subprocess.run([ufw, "status"], capture_output=True, text=True)
        parts.append("ufw=" + ("active" if "Status: active" in st.stdout else "inactive"))
    fw = shutil.which("firewall-cmd")
    if fw:
        st = subprocess.run([fw, "--state"], capture_output=True, text=True)
        parts.append("firewalld=" + (st.stdout.strip() or "unknown"))
    return "; ".join(parts) if parts else "none"

def ensure_mail_firewall():
    results = []
    ports = mail_tcp_ports()
    ufw = shutil.which("ufw")
    if ufw:
        status = subprocess.run([ufw, "status"], capture_output=True, text=True)
        if "Status: active" in status.stdout:
            for spec in ["22/tcp", "80/tcp", "443/tcp", "53/tcp", "53/udp"]:
                subprocess.run([ufw, "allow", spec], check=False)
            for port in ports:
                subprocess.run([ufw, "allow", f"{port}/tcp"], check=False)
            subprocess.run([ufw, "reload"], check=False)
            results.append("ufw aktif: port web, DNS, SSH, mail diizinkan")
        else:
            results.append("ufw terpasang tetapi tidak aktif")
    fw = shutil.which("firewall-cmd")
    if fw:
        state = subprocess.run([fw, "--state"], capture_output=True, text=True).stdout.strip()
        if state == "running":
            for spec in ["22/tcp", "80/tcp", "443/tcp", "53/tcp", "53/udp"]:
                subprocess.run([fw, "--permanent", "--add-port", spec], check=False)
            for port in ports:
                subprocess.run([fw, "--permanent", "--add-port", f"{port}/tcp"], check=False)
            subprocess.run([fw, "--reload"], check=False)
            results.append("firewalld aktif: port web, DNS, SSH, mail diizinkan")
        else:
            results.append("firewalld tidak berjalan")
    if not results:
        results.append("tidak ada firewall host yang terdeteksi")
    return "; ".join(results)

def ensure_nginx_stream_support():
    os.makedirs(NGINX_STREAMS_DIR, exist_ok=True)
    try:
        with open(NGINX_CONF, "r") as f:
            conf = f.read()
        if "streams-enabled" not in conf:
            with open(NGINX_CONF, "a") as f:
                f.write("\n# Managed by wdev-edge-agent\ninclude /etc/nginx/streams-enabled/*.conf;\n")
    except Exception:
        pass

    has_stream = False
    ver = subprocess.run(["nginx", "-V"], capture_output=True, text=True)
    if "with-stream" in (ver.stderr + ver.stdout):
        has_stream = True
    if not has_stream and os.path.isdir("/etc/nginx/modules-enabled"):
        has_stream = any("stream" in name for name in os.listdir("/etc/nginx/modules-enabled"))
    if not has_stream:
        if os.path.exists("/usr/bin/apt-get"):
            subprocess.run(["apt-get", "install", "-y", "libnginx-mod-stream"],
                           capture_output=True, text=True, check=False)
            return True
        elif os.path.exists("/usr/bin/dnf"):
            subprocess.run(["dnf", "install", "-y", "nginx-mod-stream"],
                           capture_output=True, text=True, check=False)
            return True
    return False

def find_letsencrypt_cert(hostname):
    base = "/etc/letsencrypt/live"
    if not hostname or not os.path.isdir(base):
        return None
    target = hostname.lower()
    for name in os.listdir(base):
        cert = os.path.join(base, name, "fullchain.pem")
        key = os.path.join(base, name, "privkey.pem")
        if not (os.path.exists(cert) and os.path.exists(key)):
            continue
        res = subprocess.run(["openssl", "x509", "-in", cert, "-noout", "-text"],
                             capture_output=True, text=True)
        names = [n.lower() for n in re.findall(r"DNS:([^,\s]+)", res.stdout, re.IGNORECASE)]
        if target in names:
            return cert, key
        for n in names:
            if n.startswith("*.") and target.endswith(n[1:]):
                return cert, key
    return None

def build_mail_stream_conf(target_ip, cert_info=None):
    def server_block(port, timeout):
        if cert_info:
            cert, key = cert_info
            return f"""    server {{
        listen {port} ssl;
        ssl_certificate {cert};
        ssl_certificate_key {key};
        ssl_protocols TLSv1.2 TLSv1.3;
        proxy_pass {target_ip}:{port};
        proxy_ssl on;
        proxy_ssl_verify off;
        proxy_connect_timeout 10s;
        proxy_timeout {timeout};
    }}"""
        return f"""    server {{
        listen {port};
        proxy_pass {target_ip}:{port};
        proxy_connect_timeout 10s;
        proxy_timeout {timeout};
    }}"""

    servers = "\n".join([
        server_block(993, "24h"),
        server_block(465, "5m"),
        server_block(995, "24h"),
    ])
    mode = "TLS Let's Encrypt" if cert_info else "TCP passthrough"
    header = f"# Managed by wdev-edge-agent: {mode} port mail -> Stalwart lokal ({target_ip})\n"
    return header + "stream {\n" + servers + "\n}\n"

def apply_mail_stream_conf(conf_text, module_installed):
    with open(MAIL_STREAM_CONF, "w") as f:
        f.write(conf_text)

    test = subprocess.run(["nginx", "-t"], capture_output=True, text=True)
    if test.returncode != 0:
        try:
            os.remove(MAIL_STREAM_CONF)
        except OSError:
            pass
        detail = (test.stderr or test.stdout).strip().splitlines()
        return "failed: " + (detail[-1] if detail else "nginx -t failed")

    reload_action = "restart" if module_installed else "reload"
    reload_res = subprocess.run(["systemctl", reload_action, "nginx"], capture_output=True, text=True)
    if reload_res.returncode != 0:
        try:
            os.remove(MAIL_STREAM_CONF)
        except OSError:
            pass
        return "failed: " + (reload_res.stderr.strip()[:200] or f"nginx {reload_action} failed")
    return None

def write_mail_stream(target_ip, mail_host=""):
    if not target_ip:
        return "skipped"
    module_installed = ensure_nginx_stream_support()
    os.makedirs(NGINX_STREAMS_DIR, exist_ok=True)

    cert_info = find_letsencrypt_cert(mail_host)
    if cert_info:
        err = apply_mail_stream_conf(build_mail_stream_conf(target_ip, cert_info), module_installed)
        if err is None:
            os.makedirs(os.path.dirname(MAIL_TARGET_FILE), exist_ok=True)
            with open(MAIL_TARGET_FILE, "w") as f:
                json.dump({"target_ip": target_ip, "mail_host": mail_host, "mode": "tls"}, f)
            return "active-tls"

    err = apply_mail_stream_conf(build_mail_stream_conf(target_ip, None), module_installed)
    if err is None:
        os.makedirs(os.path.dirname(MAIL_TARGET_FILE), exist_ok=True)
        with open(MAIL_TARGET_FILE, "w") as f:
            json.dump({"target_ip": target_ip, "mail_host": mail_host, "mode": "passthrough"}, f)
        return "active-passthrough"
    return err

def restore_mail_stream():
    if not os.path.exists(MAIL_TARGET_FILE):
        return
    try:
        with open(MAIL_TARGET_FILE, "r") as f:
            data = json.load(f)
        target_ip = data.get("target_ip", "")
        mail_host = data.get("mail_host", "")
        if target_ip:
            write_mail_stream(target_ip, mail_host)
    except Exception:
        pass

def configure_postfix_tls(mail_host):
    cert_info = find_letsencrypt_cert(mail_host)
    if not cert_info:
        return False
    cert, key = cert_info
    settings = {
        "smtpd_tls_cert_file": cert,
        "smtpd_tls_key_file": key,
        "smtpd_tls_security_level": "may",
        "smtp_tls_security_level": "may",
    }
    for key_name, value in settings.items():
        subprocess.run(["postconf", "-e", f"{key_name}={value}"], check=False)
    subprocess.run(["systemctl", "reload", "postfix"], check=False)
    return True

def postfix_starttls_status():
    try:
        level = subprocess.run(["postconf", "-h", "smtpd_tls_security_level"],
                               capture_output=True, text=True).stdout.strip()
        cert = subprocess.run(["postconf", "-h", "smtpd_tls_cert_file"],
                              capture_output=True, text=True).stdout.strip()
        return level in ("may", "encrypt") and bool(cert) and os.path.exists(cert)
    except Exception:
        return False

def load_mail_domains():
    if os.path.exists(MAIL_DOMAINS_FILE):
        try:
            with open(MAIL_DOMAINS_FILE, "r") as f:
                return json.load(f)
        except Exception:
            pass
    return []

def save_mail_domains(domains):
    os.makedirs(os.path.dirname(MAIL_DOMAINS_FILE), exist_ok=True)
    with open(MAIL_DOMAINS_FILE, "w") as f:
        json.dump(domains, f, indent=2)

def load_mail_relays():
    if os.path.exists(MAIL_RELAYS_FILE):
        try:
            with open(MAIL_RELAYS_FILE, "r") as f:
                return json.load(f)
        except Exception:
            pass
    return {}

def save_mail_relays(relays):
    os.makedirs(os.path.dirname(MAIL_RELAYS_FILE), exist_ok=True)
    with open(MAIL_RELAYS_FILE, "w") as f:
        json.dump(relays, f, indent=2)

class EdgeHandler(BaseHTTPRequestHandler):
    def send_json(self, status, data):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data, indent=2).encode("utf-8"))

    def check_auth(self):
        auth_header = self.headers.get("X-API-Key", "")
        expected_key = get_api_key()
        if not expected_key or auth_header != expected_key:
            self.send_json(401, {"error": "Unauthorized: Invalid or missing X-API-Key"})
            return False
        return True

    def do_GET(self):
        if not self.check_auth():
            return

        if self.path == "/api/v1/ping":
            self.send_json(200, {
                "status": "healthy",
                "service": "wdev-edge-agent",
                "nginx": subprocess.run(["systemctl", "is-active", "nginx"], capture_output=True, text=True).stdout.strip(),
                "pdns": subprocess.run(["systemctl", "is-active", "pdns"], capture_output=True, text=True).stdout.strip()
            })
        elif self.path == "/api/v1/proxies":
            proxies = []
            if os.path.exists(NGINX_AVAILABLE):
                for f in os.listdir(NGINX_AVAILABLE):
                    if f.startswith("wdev_") and f.endswith(".conf"):
                        domain = f[5:-5]
                        enabled = os.path.exists(os.path.join(NGINX_ENABLED, f))
                        proxies.append({"domain": domain, "enabled": enabled, "config_file": f})
            self.send_json(200, {"proxies": proxies})
        elif self.path == "/api/v1/mail/status":
            pf_active = subprocess.run(["systemctl", "is-active", "postfix"], capture_output=True, text=True).stdout.strip() == "active"
            st_active = subprocess.run(["systemctl", "is-active", "stalwart"], capture_output=True, text=True).stdout.strip() == "active"
            q_res = subprocess.run(["mailq"], capture_output=True, text=True)
            q_empty = "Mail queue is empty" in q_res.stdout
            mail_tls = False
            if os.path.exists(MAIL_STREAM_CONF):
                try:
                    with open(MAIL_STREAM_CONF, "r") as f:
                        mail_tls = "ssl_certificate" in f.read()
                except Exception:
                    pass
            listeners = mail_listener_status()
            proxy_listening = all(listeners.get(str(p)) for p in (993, 465, 995))
            self.send_json(200, {
                "postfix_active": pf_active,
                "stalwart_active": st_active,
                "mail_ports_proxy": os.path.exists(MAIL_STREAM_CONF),
                "mail_ports_tls": mail_tls,
                "mail_ports_listening": proxy_listening,
                "smtp_starttls": postfix_starttls_status(),
                "firewall": get_firewall_status(),
                "listeners": listeners,
                "queue_empty": q_empty,
                "queue_summary": q_res.stdout.strip().splitlines()[-1] if q_res.stdout.strip() else "Empty",
                "domains": load_mail_domains(),
                "relays": load_mail_relays()
            })
        elif self.path == "/api/v1/mail/queue":
            q_res = subprocess.run(["mailq"], capture_output=True, text=True)
            q_lines = q_res.stdout.strip().splitlines()
            count = 0
            if "Mail queue is empty" not in q_res.stdout and q_lines:
                count = len([l for l in q_lines if l and not l.startswith("-") and not l.startswith(" ")])
            self.send_json(200, {
                "queue_count": count,
                "output": q_res.stdout.strip()
            })
        else:
            self.send_json(404, {"error": "Endpoint not found"})

    def do_POST(self):
        if not self.check_auth():
            return

        if self.path == "/api/v1/proxies":
            content_len = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_len)
            try:
                data = json.loads(body.decode("utf-8"))
            except Exception:
                self.send_json(400, {"error": "Invalid JSON body"})
                return

            domain = data.get("domain", "").strip().lower()
            target_ip = data.get("target_ip", "").strip()
            target_port = int(data.get("target_port", 80))
            use_ssl = bool(data.get("ssl", False))
            ws = bool(data.get("websocket", True))

            if not is_valid_domain(domain):
                self.send_json(400, {"error": f"Invalid domain: {domain}"})
                return
            if not target_ip:
                self.send_json(400, {"error": "target_ip is required"})
                return

            conf_path = os.path.join(NGINX_AVAILABLE, f"wdev_{domain}.conf")
            link_path = os.path.join(NGINX_ENABLED, f"wdev_{domain}.conf")

            listen_v6 = "    listen [::]:80;\n" if has_ipv6() else ""
            nginx_block = f"""# Managed by wdev-edge-agent
server {{
    listen 80;
{listen_v6}    server_name {domain};

    location / {{
        proxy_pass http://{target_ip}:{target_port};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
"""
            if ws:
                nginx_block += """        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
"""
            nginx_block += """    }
}
"""
            with open(conf_path, "w") as f:
                f.write(nginx_block)

            if not os.path.exists(link_path):
                try:
                    os.symlink(conf_path, link_path)
                except OSError:
                    pass

            test_res = subprocess.run(["nginx", "-t"], capture_output=True, text=True)
            if test_res.returncode != 0:
                if os.path.exists(link_path): os.remove(link_path)
                if os.path.exists(conf_path): os.remove(conf_path)
                self.send_json(500, {"error": "Nginx config test failed", "details": test_res.stderr})
                return

            subprocess.run(["systemctl", "reload", "nginx"])

            # Certbot SSL issuance (opsional jika diminta)
            ssl_status = "none"
            if use_ssl:
                cert_res = subprocess.run([
                    "certbot", "--nginx", "-d", domain, "--non-interactive",
                    "--agree-tos", "--register-unsafely-without-email", "--redirect"
                ], capture_output=True, text=True)
                if cert_res.returncode == 0:
                    ssl_status = "active"
                else:
                    ssl_status = f"failed: {cert_res.stderr[:200]}"

            self.send_json(200, {
                "success": True,
                "domain": domain,
                "target": f"http://{target_ip}:{target_port}",
                "ssl": ssl_status
            })

        elif self.path == "/api/v1/mail/setup-domain":
            content_len = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_len)
            try:
                data = json.loads(body.decode("utf-8"))
            except Exception:
                self.send_json(400, {"error": "Invalid JSON body"})
                return

            domain = data.get("domain", "").strip().lower()
            if not is_valid_domain(domain):
                self.send_json(400, {"error": f"Invalid domain: {domain}"})
                return

            target_ip = data.get("target_ip", "").strip()
            use_ssl = bool(data.get("ssl", True))
            relay_conf = data.get("relay")

            if not target_ip or target_ip == "127.0.0.1":
                self.send_json(400, {
                    "error": "target_ip mesin lokal (NetBird) wajib diisi. "
                             "VPS hanya gateway; mailbox dan webmail dilayani mesin lokal."
                })
                return

            fw_status = ensure_mail_firewall()

            admin_sub = f"mailadmin.{domain}"
            webmail_sub = f"webmail.{domain}"
            mail_host = f"mail.{domain}"
            listen_v6 = "    listen [::]:80;\n" if has_ipv6() else ""

            # 1. Konfigurasi Postfix Inbound Store-and-Forward Relay jika target_ip (Lokal) disediakan
            if target_ip and target_ip != "127.0.0.1":
                ensure_postfix_installed(mail_host)
                relay_doms_file = "/etc/postfix/relay_domains"
                transport_file = "/etc/postfix/transport"

                lines = []
                if os.path.exists(relay_doms_file):
                    with open(relay_doms_file, "r") as rf:
                        lines = [l.strip() for l in rf if l.strip()]
                dom_entry = f"{domain} OK"
                if dom_entry not in lines and domain not in lines:
                    lines.append(dom_entry)
                    with open(relay_doms_file, "w") as rf:
                        rf.write("\n".join(lines) + "\n")
                    subprocess.run(["postmap", relay_doms_file], check=False)

                t_lines = []
                if os.path.exists(transport_file):
                    with open(transport_file, "r") as tf:
                        t_lines = [l.strip() for l in tf if l.strip()]
                route_entry = f"{domain} smtp:[{target_ip}]:25"
                t_lines = [l for l in t_lines if not l.startswith(f"{domain} ")]
                t_lines.append(route_entry)
                with open(transport_file, "w") as tf:
                    tf.write("\n".join(t_lines) + "\n")
                subprocess.run(["postmap", transport_file], check=False)
                subprocess.run(["systemctl", "reload", "postfix"], check=False)

            # 2. Nginx config for mailadmin.<domain> (Stalwart Admin Web UI)
            admin_target = f"http://{target_ip}:8080" if target_ip else "http://127.0.0.1:8080"
            admin_conf = os.path.join(NGINX_AVAILABLE, f"wdev_{admin_sub}.conf")
            admin_link = os.path.join(NGINX_ENABLED, f"wdev_{admin_sub}.conf")
            admin_block = f"""# Managed by wdev-edge-agent (Mail Admin)
server {{
    listen 80;
{listen_v6}    server_name {admin_sub};

    location / {{
        proxy_pass {admin_target};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }}
}}
"""
            with open(admin_conf, "w") as f: f.write(admin_block)
            if not os.path.exists(admin_link):
                try: os.symlink(admin_conf, admin_link)
                except OSError: pass

            # 3. Nginx config for webmail.<domain>
            webmail_conf = os.path.join(NGINX_AVAILABLE, f"wdev_{webmail_sub}.conf")
            webmail_link = os.path.join(NGINX_ENABLED, f"wdev_{webmail_sub}.conf")
            webmail_block = f"""# Managed by wdev-edge-agent (Webmail Client via Netbird)
server {{
    listen 80;
{listen_v6}    server_name {webmail_sub};
    client_max_body_size 50M;

    location / {{
        proxy_pass http://{target_ip}:80;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }}
}}
"""
            with open(webmail_conf, "w") as f: f.write(webmail_block)
            if not os.path.exists(webmail_link):
                try: os.symlink(webmail_conf, webmail_link)
                except OSError: pass

            # Dummy Nginx config for mail.<domain>
            mail_conf = os.path.join(NGINX_AVAILABLE, f"wdev_{mail_host}.conf")
            mail_link = os.path.join(NGINX_ENABLED, f"wdev_{mail_host}.conf")
            mail_block = f"""# Managed by wdev-edge-agent (Mail Host verification)
server {{
    listen 80;
{listen_v6}    server_name {mail_host};
    location / {{
        return 200 "mail server host ok";
    }}
}}
"""
            with open(mail_conf, "w") as f: f.write(mail_block)
            if not os.path.exists(mail_link):
                try: os.symlink(mail_conf, mail_link)
                except OSError: pass

            subprocess.run(["nginx", "-t"], check=False)
            subprocess.run(["systemctl", "reload", "nginx"], check=False)

            ssl_status = "none"
            if use_ssl:
                cert_res = subprocess.run([
                    "certbot", "--nginx", "-d", admin_sub, "-d", webmail_sub, "-d", mail_host,
                    "--non-interactive", "--agree-tos", "--register-unsafely-without-email", "--redirect"
                ], capture_output=True, text=True)
                if cert_res.returncode == 0:
                    ssl_status = "active"
                else:
                    ssl_status = f"partial: {cert_res.stderr[:100]}"

            # Proxy TCP port mail setelah sertifikat tersedia agar IMAPS/SMTPS/POP3S
            # memakai sertifikat Let's Encrypt yang valid di sisi VPS.
            smtp_starttls = configure_postfix_tls(mail_host)
            mail_ports_status = "not-configured"
            if target_ip and target_ip != "127.0.0.1":
                stream_tls_host = mail_host if use_ssl else ""
                mail_ports_status = write_mail_stream(target_ip, stream_tls_host)

            domains = load_mail_domains()
            if domain not in domains:
                domains.append(domain)
                save_mail_domains(domains)

            if relay_conf and relay_conf.get("host"):
                relays = load_mail_relays()
                target_scope = relay_conf.get("domain", "global")
                relays[target_scope] = {
                    "host": relay_conf.get("host"),
                    "port": int(relay_conf.get("port", 587)),
                    "user": relay_conf.get("user", ""),
                    "pass": relay_conf.get("pass", "")
                }
                save_mail_relays(relays)

            listeners = mail_listener_status()
            self.send_json(200, {
                "success": True,
                "domain": domain,
                "admin_url": f"https://{admin_sub}/admin/" if ssl_status == "active" else f"http://{admin_sub}/admin/",
                "webmail_url": f"https://{webmail_sub}" if ssl_status == "active" else f"http://{webmail_sub}",
                "mail_host": mail_host,
                "mail_ports_proxy": mail_ports_status,
                "mail_ports_listening": all(listeners.get(str(p)) for p in (993, 465, 995)),
                "smtp_starttls": smtp_starttls,
                "firewall": fw_status,
                "listeners": listeners,
                "ssl": ssl_status
            })

        elif self.path == "/api/v1/mail/open-ports":
            fw_status = ensure_mail_firewall()
            self.send_json(200, {
                "success": True,
                "firewall": fw_status,
                "listeners": mail_listener_status()
            })

        elif self.path == "/api/v1/mail/set-relay":
            content_len = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_len)
            try:
                data = json.loads(body.decode("utf-8"))
            except Exception:
                self.send_json(400, {"error": "Invalid JSON body"})
                return

            target_scope = data.get("domain", "global").strip().lower()
            host = data.get("host", "").strip()
            port = int(data.get("port", 587))
            user = data.get("user", "").strip()
            pwd = data.get("pass", "").strip()

            if not host:
                self.send_json(400, {"error": "Relay host is required"})
                return

            relays = load_mail_relays()
            relays[target_scope] = {
                "host": host,
                "port": port,
                "user": user,
                "pass": pwd
            }
            save_mail_relays(relays)

            self.send_json(200, {
                "success": True,
                "message": f"SMTP Relay configured for '{target_scope}' ({host}:{port})"
            })

        elif self.path == "/api/v1/mail/account":
            # Akun email berada di Stalwart pada mesin lokal (bukan di VPS).
            self.send_json(501, {
                "error": "Akun email dikelola di Stalwart pada mesin lokal. "
                         "Gunakan: sudo pdns-mgr mail add-account <email> <password>"
            })
            return
        elif self.path == "/api/v1/mail/flush-queue":
            subprocess.run(["postfix", "flush"], check=False)
            self.send_json(200, {
                "success": True,
                "message": "Mail queue flush triggered successfully."
            })
        else:
            self.send_json(404, {"error": "Endpoint not found"})

    def do_DELETE(self):
        if not self.check_auth():
            return

        m = re.match(r'^/api/v1/proxies/([A-Za-z0-9.-]+)$', self.path)
        if m:
            domain = m.group(1).lower()
            conf_path = os.path.join(NGINX_AVAILABLE, f"wdev_{domain}.conf")
            link_path = os.path.join(NGINX_ENABLED, f"wdev_{domain}.conf")

            removed = False
            if os.path.exists(link_path):
                os.remove(link_path)
                removed = True
            if os.path.exists(conf_path):
                os.remove(conf_path)
                removed = True

            if removed:
                subprocess.run(["nginx", "-t"])
                subprocess.run(["systemctl", "reload", "nginx"])
                self.send_json(200, {"success": True, "message": f"Proxy for {domain} removed."})
            else:
                self.send_json(404, {"error": f"Proxy for {domain} not found."})
        else:
            self.send_json(404, {"error": "Invalid DELETE path"})

def main():
    restore_mail_stream()
    server = HTTPServer(("0.0.0.0", 8082), EdgeHandler)
    print("wdev-edge-agent listening on 0.0.0.0:8082")
    server.serve_forever()

if __name__ == "__main__":
    main()
AGENT_EOF

chmod 755 /usr/local/bin/wdev-edge-agent

# Systemd Service untuk wdev-edge-agent
cat <<'EOF' > /etc/systemd/system/wdev-edge-agent.service
[Unit]
Description=wdev Edge Reverse-Proxy Agent
After=network.target nginx.service

[Service]
Type=simple
ExecStart=/usr/bin/python3 /usr/local/bin/wdev-edge-agent
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable wdev-edge-agent
systemctl restart wdev-edge-agent
ok "wdev-edge-agent aktif pada Port $AGENT_PORT."

# 6.1 VPS hanya gateway (Postfix + Nginx). Mailbox & webmail dilayani
# Stalwart/SnappyMail di mesin lokal, jadi tidak ada yang perlu diunduh di VPS.
say "${C_BOLD}>>> [6.1/7] Membersihkan komponen mail yang tidak dipakai di VPS...${C_RESET}"
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files stalwart.service >/dev/null 2>&1; then
  say "Menonaktifkan Stalwart lama di VPS (port 25 dialihkan ke Postfix gateway)..."
  systemctl disable --now stalwart 2>/dev/null || true
fi
if [ -d /var/www/snappymail ]; then
  say "Menghapus SnappyMail di VPS (webmail dilayani dari mesin lokal)..."
  rm -rf /var/www/snappymail
fi
rm -f /tmp/snappymail.zip

# 6.2 Pastikan domain mail yang di-relay tidak dianggap domain lokal VPS.
say "${C_BOLD}>>> [6.2/7] Menyelaraskan Postfix (domain relay bukan domain lokal)...${C_RESET}"
if command -v postconf >/dev/null 2>&1; then
  MAIL_DOMAIN_FIRST=""
  if [ -f /etc/wdev-edge/mail_domains.json ] && command -v jq >/dev/null 2>&1; then
    MAIL_DOMAIN_FIRST=$(jq -r '.[0] // empty' /etc/wdev-edge/mail_domains.json 2>/dev/null || true)
  fi
  if [ -n "$MAIL_DOMAIN_FIRST" ]; then
    postconf -e "myhostname=mx.${MAIL_DOMAIN_FIRST}" 2>/dev/null || true
    postconf -e "smtp_helo_name=mx.${MAIL_DOMAIN_FIRST}" 2>/dev/null || true
  fi
  postconf -e "mydestination=localhost.localdomain, localhost" 2>/dev/null || true
  postconf -e "relay_domains = hash:/etc/postfix/relay_domains" 2>/dev/null || true
  postconf -e "transport_maps = hash:/etc/postfix/transport" 2>/dev/null || true
  postconf -e "smtp_sasl_auth_enable=no" 2>/dev/null || true
  postconf -e "smtp_tls_security_level=may" 2>/dev/null || true
  postconf -e "smtp_tls_verify_cert=no" 2>/dev/null || true
  systemctl reload postfix 2>/dev/null || true
  ok "Postfix: myhostname/mydestination diselaraskan (domain mail tidak dianggap lokal)."
  if [ -n "$MAIL_DOMAIN_FIRST" ]; then
    TRANSPORT_TARGET=$(postmap -q "$MAIL_DOMAIN_FIRST" hash:/etc/postfix/transport 2>/dev/null || true)
    if [ -n "$TRANSPORT_TARGET" ]; then
      say "  Transport ${MAIL_DOMAIN_FIRST} -> ${TRANSPORT_TARGET}"
    else
      warn "Transport map untuk ${MAIL_DOMAIN_FIRST} belum ada; jalankan: sudo pdns-mgr mail setup ${MAIL_DOMAIN_FIRST}"
    fi
  fi
fi

# 7. Konfigurasi Firewall (UFW / firewalld)
say "${C_BOLD}>>> [7/7] Mengonfigurasi Firewall (port web, DNS, SSH, dan mail)...${C_RESET}"
FIREWALL_CONFIGURED=false
if command -v ufw >/dev/null 2>&1; then
  ufw allow 22/tcp comment "SSH" 2>/dev/null || true
  ufw allow 53/tcp comment "PowerDNS" 2>/dev/null || true
  ufw allow 53/udp comment "PowerDNS" 2>/dev/null || true
  ufw allow 80/tcp comment "HTTP Nginx" 2>/dev/null || true
  ufw allow 443/tcp comment "HTTPS Nginx" 2>/dev/null || true
  # Port Mail Server (Inbound SMTP, SMTPS, Submission, IMAPS, IMAP, POP3)
  ufw allow 25/tcp comment "SMTP Inbound" 2>/dev/null || true
  ufw allow 465/tcp comment "SMTPS SSL" 2>/dev/null || true
  ufw allow 587/tcp comment "SMTP Submission" 2>/dev/null || true
  ufw allow 993/tcp comment "IMAPS SSL" 2>/dev/null || true
  ufw allow 143/tcp comment "IMAP STARTTLS" 2>/dev/null || true
  ufw allow 995/tcp comment "POP3S SSL" 2>/dev/null || true
  ufw allow 110/tcp comment "POP3 STARTTLS" 2>/dev/null || true
  # Port API hanya dibuka untuk subnet Netbird CGNAT (100.64.0.0/10)
  ufw allow from 100.64.0.0/10 to any port "$PDNS_API_PORT" proto tcp comment "PowerDNS API (Netbird)" 2>/dev/null || true
  ufw allow from 100.64.0.0/10 to any port "$AGENT_PORT" proto tcp comment "wdev Agent (Netbird)" 2>/dev/null || true
  ufw --force enable 2>/dev/null || true
  ufw reload 2>/dev/null || true
  ok "Firewall UFW: port 22,53,80,443,25,465,587,993,143,995,110 dibuka."
  FIREWALL_CONFIGURED=true
fi
if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  for p in 22/tcp 53/tcp 53/udp 80/tcp 443/tcp 25/tcp 465/tcp 587/tcp 993/tcp 143/tcp 995/tcp 110/tcp; do
    firewall-cmd --permanent --add-port="$p" >/dev/null 2>&1 || true
  done
  firewall-cmd --permanent --add-rich-rule="rule family=\"ipv4\" source address=\"100.64.0.0/10\" port port=\"${PDNS_API_PORT}\" protocol=\"tcp\" accept" >/dev/null 2>&1 || true
  firewall-cmd --permanent --add-rich-rule="rule family=\"ipv4\" source address=\"100.64.0.0/10\" port port=\"${AGENT_PORT}\" protocol=\"tcp\" accept" >/dev/null 2>&1 || true
  firewall-cmd --reload >/dev/null 2>&1 || true
  ok "Firewall firewalld: port web, DNS, SSH, dan mail dibuka."
  FIREWALL_CONFIGURED=true
fi
if [ "$FIREWALL_CONFIGURED" = false ]; then
  warn "UFW/firewalld tidak aktif. Pastikan port 25,465,587,993,143,995,110 dibuka di firewall penyedia VPS."
fi

# Simpan metadata konfigurasi VPS
cat <<EOF > "$CONFIG_DIR/edge.conf"
PUBLIC_IP=$PUBLIC_IP
NETBIRD_IP=$NETBIRD_IP
PDNS_API_PORT=$PDNS_API_PORT
AGENT_PORT=$AGENT_PORT
API_KEY=$PDNS_API_KEY
EOF
chmod 600 "$CONFIG_DIR/edge.conf"

say ""
say "${C_BOLD}${C_GREEN}=================================================================="
say "   Pemasangan VPS Edge Gateway Berhasil!   "
say "==================================================================${C_RESET}"
say "IP Publik VPS     : ${C_BOLD}${PUBLIC_IP}${C_RESET}"
say "Netbird IP VPS    : ${C_BOLD}${NETBIRD_IP}${C_RESET}"
say "PowerDNS API Port : ${C_BOLD}${PDNS_API_PORT}${C_RESET}"
say "Edge Agent Port   : ${C_BOLD}${AGENT_PORT}${C_RESET}"
say "Secret API Key    : ${C_BOLD}${PDNS_API_KEY}${C_RESET}"
say ""
say "${C_YELLOW}LANGKAH BERIKUTNYA DI MESIN LOKAL (wdev):${C_RESET}"
say "Jalankan perintah ini di laptop/server lokal Anda untuk menghubungkan:"
say ""
say "${C_CYAN}sudo pdns-mgr connect --vps-ip ${NETBIRD_IP} --public-ip ${PUBLIC_IP} --api-key ${PDNS_API_KEY}${C_RESET}"
say ""
