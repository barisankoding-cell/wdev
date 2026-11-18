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

    apt-get install -y --no-install-recommends \
      curl jq sqlite3 nginx certbot python3-certbot-nginx python3 pdns-server pdns-backend-sqlite3 ufw || {
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
      curl jq sqlite nginx certbot python3-certbot-nginx python3 pdns pdns-backend-sqlite3 firewalld
    ;;
esac

# Pastikan default vhost Nginx tidak mencoba bind IPv6 jika kernel tidak mendukung
if [ "$HAS_IPV6" -eq 0 ] && [ -f /etc/nginx/sites-available/default ]; then
  sed -i 's/listen \[::\]:80/#listen [::]:80/' /etc/nginx/sites-available/default
  systemctl restart nginx 2>/dev/null || true
fi
ok "Paket perangkat lunak berhasil dipasang."

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

mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
# Pastikan nginx.conf memuat sites-enabled
if ! grep -q "sites-enabled" /etc/nginx/nginx.conf 2>/dev/null; then
  sed -i '/http {/a \    include /etc/nginx/sites-enabled/*;' /etc/nginx/nginx.conf
fi

# Buat daemon Python Edge Agent (Zero-dependency, hanya stdlib)
cat <<'AGENT_EOF' > /usr/local/bin/wdev-edge-agent
#!/usr/bin/env python3
import json
import os
import re
import subprocess
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler

CONFIG_FILE = "/etc/wdev-edge/edge.conf"
API_KEY_FILE = "/etc/wdev-edge/api_key.secret"
NGINX_AVAILABLE = "/etc/nginx/sites-available"
NGINX_ENABLED = "/etc/nginx/sites-enabled"

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

# 7. Konfigurasi Firewall (UFW)
say "${C_BOLD}>>> [7/7] Mengonfigurasi Firewall...${C_RESET}"
if command -v ufw >/dev/null 2>&1; then
  ufw allow 22/tcp comment "SSH" 2>/dev/null || true
  ufw allow 53/tcp comment "PowerDNS" 2>/dev/null || true
  ufw allow 53/udp comment "PowerDNS" 2>/dev/null || true
  ufw allow 80/tcp comment "HTTP Nginx" 2>/dev/null || true
  ufw allow 443/tcp comment "HTTPS Nginx" 2>/dev/null || true
  # Port API hanya dibuka untuk subnet Netbird CGNAT (100.64.0.0/10)
  ufw allow from 100.64.0.0/10 to any port "$PDNS_API_PORT" proto tcp comment "PowerDNS API (Netbird)" 2>/dev/null || true
  ufw allow from 100.64.0.0/10 to any port "$AGENT_PORT" proto tcp comment "wdev Agent (Netbird)" 2>/dev/null || true
  ufw --force enable 2>/dev/null || true
  ok "Firewall UFW dikonfigurasi dengan aman."
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
