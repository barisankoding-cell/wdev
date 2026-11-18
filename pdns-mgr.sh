#!/usr/bin/env bash
# ==============================================================================
# pdns-mgr: CLI Helper Manajemen PowerDNS & VPS Edge Gateway untuk wdev
# Mengontrol PowerDNS Authoritative & Reverse-Proxy Nginx di VPS via Netbird
# ==============================================================================
set -euo pipefail

WDEV_ETC="${WDEV_ETC:-/etc/wdev}"
CONF_FILE="$WDEV_ETC/edge-dns.conf"

# Styling & Warna
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

is_root() { [ "${EUID:-$(id -u)}" -eq 0 ]; }
need_root() {
  is_root || die "Jalankan perintah ini dengan sudo/root (contoh: sudo pdns-mgr ...)"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Perintah '$1' belum terpasang. Pasang dengan: sudo apt install -y $1 (atau dnf)"
}

# Muat Konfigurasi
load_config() {
  if [ ! -f "$CONF_FILE" ]; then
    die "Konfigurasi belum ada. Jalankan: sudo pdns-mgr connect --vps-ip <IP_NETBIRD_VPS> --public-ip <IP_PUBLIK> --api-key <KEY>"
  fi
  # shellcheck source=/dev/null
  . "$CONF_FILE"
  VPS_NETBIRD_IP="${VPS_NETBIRD_IP:-}"
  VPS_PUBLIC_IP="${VPS_PUBLIC_IP:-}"
  API_KEY="${API_KEY:-}"
  PDNS_PORT="${PDNS_PORT:-8081}"
  AGENT_PORT="${AGENT_PORT:-8082}"

  [ -n "$VPS_NETBIRD_IP" ] || die "VPS_NETBIRD_IP belum disetel di $CONF_FILE"
  [ -n "$API_KEY" ] || die "API_KEY belum disetel di $CONF_FILE"
}

get_local_netbird_ip() {
  local ip=""
  if ip -4 addr show dev wt0 >/dev/null 2>&1; then
    ip=$(ip -4 addr show dev wt0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -n 1)
  fi
  if [ -z "$ip" ]; then
    # Fallback cek netbird status
    if command -v netbird >/dev/null 2>&1; then
      ip=$(netbird status 2>/dev/null | grep -i "NetBird IP" | awk '{print $3}' | cut -d/ -f1 || echo "")
    fi
  fi
  echo "$ip"
}

# Helper Request ke PowerDNS API
pdns_api() {
  local method="$1"
  local path="$2"
  local data="${3:-}"
  local url="http://${VPS_NETBIRD_IP}:${PDNS_PORT}${path}"

  local -a curl_args=(-s -S -X "$method" "$url" -H "X-API-Key: ${API_KEY}" -H "Accept: application/json")
  if [ -n "$data" ]; then
    curl_args+=(-H "Content-Type: application/json" -d "$data")
  fi

  curl "${curl_args[@]}"
}

# Helper Request ke Edge Agent API (Nginx Reverse Proxy)
agent_api() {
  local method="$1"
  local path="$2"
  local data="${3:-}"
  local url="http://${VPS_NETBIRD_IP}:${AGENT_PORT}${path}"

  local -a curl_args=(-s -S -X "$method" "$url" -H "X-API-Key: ${API_KEY}" -H "Accept: application/json")
  if [ -n "$data" ]; then
    curl_args+=(-H "Content-Type: application/json" -d "$data")
  fi

  curl "${curl_args[@]}"
}

# ==============================================================================
# SUBCOMMANDS
# ==============================================================================

cmd_deploy_vps() {
  need_root
  need_cmd ssh
  need_cmd scp
  need_cmd curl
  need_cmd jq

  local vps_ip="${1:-}"
  local ssh_user="${2:-root}"
  local ssh_pass=""
  local SCRIPT_DIR
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local vps_setup_script="$SCRIPT_DIR/vps-edge-setup.sh"

  if [ ! -f "$vps_setup_script" ]; then
    for p in "/mnt/f7d8a899-1e8d-4c44-afaf-2f7c69f2cce5/projects/wdev/vps-edge-setup.sh" "$HOME/wdev/vps-edge-setup.sh" "/etc/wdev/vps-edge-setup.sh"; do
      if [ -f "$p" ]; then
        vps_setup_script="$p"
        break
      fi
    done
  fi

  [ -f "$vps_setup_script" ] || die "Berkas vps-edge-setup.sh tidak ditemukan!"

  say "${C_BOLD}${C_CYAN}=== Automated Remote VPS Edge Deployment via Netbird ===${C_RESET}"
  say ""

  if [ -z "$vps_ip" ]; then
    read -r -p "Masukkan Netbird IP VPS (contoh: 100.64.0.1): " input_ip
    vps_ip="${input_ip:-}"
    [ -n "$vps_ip" ] || die "IP Netbird VPS tidak boleh kosong."
  fi

  say "Menguji jalur koneksi Netbird ke $vps_ip..."
  if ! ping -c 2 -W 3 "$vps_ip" >/dev/null 2>&1; then
    die "Tidak dapat melakukan ping ke $vps_ip via Netbird. Pastikan Netbird di VPS aktif."
  fi
  ok "Jalur Netbird terhubung ($vps_ip)."

  read -r -p "Username SSH VPS [$ssh_user]: " input_user
  ssh_user="${input_user:-$ssh_user}"

  say "Metode autentikasi SSH:"
  say " 1) Password SSH (Interaktif)"
  say " 2) SSH Key (~/.ssh/id_rsa / id_ed25519)"
  read -r -p "Pilihan [1/2, default: 1]: " auth_choice
  auth_choice="${auth_choice:-1}"

  local ssh_cmd=("ssh" "-o" "StrictHostKeyChecking=no" "-o" "ConnectTimeout=10")
  local scp_cmd=("scp" "-o" "StrictHostKeyChecking=no" "-o" "ConnectTimeout=10")

  if [ "$auth_choice" = "1" ]; then
    need_cmd sshpass
    read -r -s -p "Masukkan Password SSH untuk ${ssh_user}@${vps_ip}: " ssh_pass
    echo ""
    [ -n "$ssh_pass" ] || die "Password tidak boleh kosong."
    ssh_cmd=("sshpass" "-p" "$ssh_pass" "${ssh_cmd[@]}")
    scp_cmd=("sshpass" "-p" "$ssh_pass" "${scp_cmd[@]}")
  fi

  say "Menguji login SSH ke ${ssh_user}@${vps_ip}..."
  if ! "${ssh_cmd[@]}" "${ssh_user}@${vps_ip}" "echo 'SSH_OK'" >/dev/null 2>&1; then
    die "Gagal login SSH ke ${ssh_user}@${vps_ip}. Periksa username/password atau port SSH VPS."
  fi
  ok "Autentikasi SSH berhasil."

  say "Mengunggah vps-edge-setup.sh ke VPS..."
  "${scp_cmd[@]}" "$vps_setup_script" "${ssh_user}@${vps_ip}:/tmp/vps-edge-setup.sh" || die "Gagal mengunggah script ke VPS."
  "${ssh_cmd[@]}" "${ssh_user}@${vps_ip}" "chmod +x /tmp/vps-edge-setup.sh"

  say ""
  say "${C_BOLD}>>> Memulai instalasi otomatis di VPS...${C_RESET}"
  say "----------------------------------------------------------------------"
  "${ssh_cmd[@]}" -t "${ssh_user}@${vps_ip}" "sudo /tmp/vps-edge-setup.sh"
  say "----------------------------------------------------------------------"

  say "Mengambil kredensial otomatis dari VPS..."
  local vps_conf
  vps_conf=$("${ssh_cmd[@]}" "${ssh_user}@${vps_ip}" "sudo cat /etc/wdev-edge/edge.conf 2>/dev/null" || true)

  if [ -z "$vps_conf" ]; then
    die "Gagal membaca /etc/wdev-edge/edge.conf di VPS. Apakah instalasi berhasil?"
  fi

  local pub_ip="" key="" p_port="8081" a_port="8082"
  pub_ip=$(echo "$vps_conf" | awk -F= '$1=="PUBLIC_IP"{print $2}')
  key=$(echo "$vps_conf" | awk -F= '$1=="API_KEY"{print $2}')
  p_port=$(echo "$vps_conf" | awk -F= '$1=="PDNS_API_PORT"{print $2}')
  a_port=$(echo "$vps_conf" | awk -F= '$1=="AGENT_PORT"{print $2}')

  say "Menghubungkan pdns-mgr lokal ke VPS..."
  cmd_connect --vps-ip "$vps_ip" --public-ip "$pub_ip" --api-key "$key" --pdns-port "${p_port:-8081}" --agent-port "${a_port:-8082}"

  say ""
  ok "Deployment & Pairing Selesai! Mesin lokal Anda sekarang siap mengelola domain & expose aplikasi."
}

cmd_connect() {
  need_root
  need_cmd curl
  need_cmd jq

  local vps_ip="" pub_ip="" key="" p_port="8081" a_port="8082"

  while [ $# -gt 0 ]; do
    case "$1" in
      --vps-ip) vps_ip="$2"; shift 2 ;;
      --public-ip) pub_ip="$2"; shift 2 ;;
      --api-key) key="$2"; shift 2 ;;
      --pdns-port) p_port="$2"; shift 2 ;;
      --agent-port) a_port="$2"; shift 2 ;;
      *) die "Opsi tidak dikenal: $1" ;;
    esac
  done

  if [ -z "$vps_ip" ] || [ -z "$key" ]; then
    say "Penggunaan: sudo pdns-mgr connect --vps-ip <IP_NETBIRD_VPS> --public-ip <IP_PUBLIK> --api-key <KEY>"
    exit 1
  fi

  say "Menguji koneksi ke PowerDNS API di http://${vps_ip}:${p_port}..."
  local test_pdns
  test_pdns=$(curl -s -m 5 -H "X-API-Key: $key" "http://${vps_ip}:${p_port}/api/v1/servers/localhost" 2>&1 || true)
  if ! echo "$test_pdns" | grep -q '"id":' 2>/dev/null; then
    err "Gagal menghubungi PowerDNS API di http://${vps_ip}:${p_port}!"
    say "Detail error: $test_pdns"
    say "Pastikan Netbird terhubung dan firewall VPS mengizinkan port $p_port."
    exit 1
  fi
  ok "PowerDNS API terverifikasi dan aktif."

  say "Menguji koneksi ke Edge Proxy Agent di http://${vps_ip}:${a_port}..."
  local test_agent
  test_agent=$(curl -s -m 5 -H "X-API-Key: $key" "http://${vps_ip}:${a_port}/api/v1/ping" 2>&1 || true)
  if ! echo "$test_agent" | grep -q '"healthy"' 2>/dev/null; then
    warn "Edge Agent di port $a_port belum merespons (opsional untuk proxy)."
  else
    ok "Edge Proxy Agent terverifikasi."
  fi

  mkdir -p "$WDEV_ETC"
  cat <<EOF > "$CONF_FILE"
# wdev Edge & PowerDNS Configuration
VPS_NETBIRD_IP="$vps_ip"
VPS_PUBLIC_IP="$pub_ip"
API_KEY="$key"
PDNS_PORT="$p_port"
AGENT_PORT="$a_port"
EOF
  chmod 644 "$CONF_FILE"
  ok "Konfigurasi berhasil disimpan ke $CONF_FILE"
}

cmd_status() {
  need_cmd curl
  need_cmd jq
  load_config

  local local_nb
  local_nb=$(get_local_netbird_ip)

  say "${C_BOLD}=== Status wdev PowerDNS & Edge Gateway ===${C_RESET}"
  say "IP Netbird Lokal   : ${local_nb:-${C_RED}Belum Aktif${C_RESET}}"
  say "IP Netbird VPS     : ${VPS_NETBIRD_IP}"
  say "IP Publik VPS      : ${VPS_PUBLIC_IP:-${C_YELLOW}Belum Diisi${C_RESET}}"
  say "Port PowerDNS API  : ${PDNS_PORT}"
  say "Port Edge Agent    : ${AGENT_PORT}"

  printf "Koneksi PowerDNS   : "
  local p_res
  p_res=$(pdns_api GET "/api/v1/servers/localhost" || echo "")
  if echo "$p_res" | grep -q '"id":'; then
    local ver
    ver=$(echo "$p_res" | jq -r '.version // "unknown"')
    printf "${C_GREEN}[ONLINE]${C_RESET} (PowerDNS v%s)\n" "$ver"
  else
    printf "${C_RED}[OFFLINE / ERROR]${C_RESET}\n"
  fi

  printf "Koneksi Edge Agent : "
  local a_res
  a_res=$(agent_api GET "/api/v1/ping" || echo "")
  if echo "$a_res" | grep -q '"healthy"'; then
    printf "${C_GREEN}[ONLINE]${C_RESET}\n"
  else
    printf "${C_YELLOW}[OFFLINE / TIDAK AKTIF]${C_RESET}\n"
  fi
}

cmd_zone_add() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  local ns1="${2:-ns1.$domain}"
  local ns2="${3:-ns2.$domain}"

  [ -n "$domain" ] || die "Penggunaan: sudo pdns-mgr zone add <nama_domain> [ns1] [ns2]"

  # Normalisasi nama domain
  domain="${domain%.}."
  ns1="${ns1%.}."
  ns2="${ns2%.}."

  say "Membuat zone Authoritative untuk '${domain}' di VPS..."

  local pub_ip="$VPS_PUBLIC_IP"
  if [ -z "$pub_ip" ]; then
    warn "IP Publik VPS belum disetel di config."
    read -r -p "Masukkan IP Publik VPS: " pub_ip
  fi

  # Payload pembuatan zone standar PowerDNS (nameservers akan otomatis membuat NS records)
  local payload
  payload=$(jq -n \
    --arg name "$domain" \
    --arg ns1 "$ns1" \
    --arg ns2 "$ns2" \
    --arg ip "$pub_ip" \
    '{
      name: $name,
      kind: "Native",
      nameservers: [$ns1, $ns2],
      rrsets: [
        {
          name: $name,
          type: "SOA",
          ttl: 3600,
          records: [
            {
              content: ($ns1 + " hostmaster." + $name + " 1 10800 3600 604800 3600"),
              disabled: false
            }
          ]
        },
        {
          name: $ns1,
          type: "A",
          ttl: 3600,
          records: [ { content: $ip, disabled: false } ]
        },
        {
          name: $ns2,
          type: "A",
          ttl: 3600,
          records: [ { content: $ip, disabled: false } ]
        },
        {
          name: $name,
          type: "A",
          ttl: 300,
          records: [ { content: $ip, disabled: false } ]
        }
      ]
    }')

  local res
  res=$(pdns_api POST "/api/v1/servers/localhost/zones" "$payload")

  if echo "$res" | grep -q '"id":'; then
    ok "Zone '${domain}' berhasil dibuat!"
    say ""
    say "${C_YELLOW}PENTING: PENGATURAN DI SISI REGISTRAR DOMAIN:${C_RESET}"
    say "1. Buat Glue Record (Child Nameserver) di registrar:"
    say "   - ${ns1%.} -> ${pub_ip}"
    say "   - ${ns2%.} -> ${pub_ip}"
    say "2. Ganti Nameserver utama domain Anda menjadi:"
    say "   - Nameserver 1: ${ns1%.}"
    say "   - Nameserver 2: ${ns2%.}"
    say ""
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    die "Gagal membuat zone: $err_msg"
  fi
}

cmd_zone_list() {
  need_cmd curl
  need_cmd jq
  load_config

  local res
  res=$(pdns_api GET "/api/v1/servers/localhost/zones")
  printf "${C_BOLD}%-30s %-10s %-10s %-15s${C_RESET}\n" "ZONE / DOMAIN" "KIND" "SERIAL" "RECORDS"
  echo "----------------------------------------------------------------------"
  echo "$res" | jq -r '.[] | "\(.name) \(.kind) \(.serial) \(.edited_serial // "-")"' | while read -r name kind serial edited; do
    printf "%-30s %-10s %-10s %-15s\n" "$name" "$kind" "$serial" "$edited"
  done
}

cmd_zone_names() {
  need_cmd curl
  need_cmd jq
  load_config

  local res
  res=$(pdns_api GET "/api/v1/servers/localhost/zones" 2>/dev/null || echo "[]")
  echo "$res" | jq -r '.[].name // empty' 2>/dev/null | sed 's/\.$//'
}

cmd_proxy_names() {
  need_cmd curl
  need_cmd jq
  load_config

  local res
  res=$(agent_api GET "/api/v1/proxies" 2>/dev/null || echo "{}")
  echo "$res" | jq -r '.proxies[]?.domain // empty' 2>/dev/null
}

cmd_zone_rm() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  if [ -z "$domain" ]; then
    local -a zones=()
    while IFS= read -r z; do [ -n "$z" ] && zones+=("$z"); done < <(cmd_zone_names)
    if [ "${#zones[@]}" -eq 0 ]; then
      die "Belum ada zone domain terdaftar di PowerDNS."
    fi
    say "${C_CYAN}Pilih zone domain yang akan dihapus:${C_RESET}"
    for i in "${!zones[@]}"; do
      say "  [$((i + 1))] ${zones[$i]}"
    done
    say "  [0] Batal"
    read -r -p "Pilih nomor [1-${#zones[@]}]: " sel
    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le "${#zones[@]}" ]; then
      domain="${zones[$((sel - 1))]}"
    else
      die "Dibatalkan atau pilihan tidak valid."
    fi
  fi
  domain="${domain%.}."

  say "Menghapus zone '${domain}'..."
  local res
  res=$(pdns_api DELETE "/api/v1/servers/localhost/zones/${domain}")
  if [ -z "$res" ]; then
    ok "Zone '${domain}' berhasil dihapus."
  else
    say "$res"
  fi
}

cmd_record_add() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  local name="${2:-}"
  local type="${3:-A}"
  local content="${4:-auto}"
  local ttl="${5:-300}"

  if [ -z "$domain" ] || [ -z "$name" ]; then
    say "Penggunaan: sudo pdns-mgr record add <domain> <subdomain|@> <TYPE> [CONTENT|auto] [TTL]"
    say "Contoh 1: sudo pdns-mgr record add domain.com api A auto"
    say "Contoh 2: sudo pdns-mgr record add domain.com @ TXT \"v=spf1 ~all\""
    exit 1
  fi

  domain="${domain%.}."
  local d_clean="${domain%.}"
  local fqdn
  if [ "$name" = "@" ] || [ "$name" = "" ]; then
    fqdn="$domain"
  else
    name="${name%.}"
    if [[ "$name" == *"$d_clean" ]] || [[ "$name" == *"$domain" ]]; then
      fqdn="${name%.}."
    else
      fqdn="${name}.${domain}"
    fi
  fi

  type=$(echo "$type" | tr '[:lower:]' '[:upper:]')

  if [ "$content" = "auto" ]; then
    [ -n "$VPS_PUBLIC_IP" ] || die "VPS_PUBLIC_IP belum ada di config. Berikan IP secara spesifik."
    content="$VPS_PUBLIC_IP"
  fi

  # Format content jika CNAME / MX
  if [ "$type" = "CNAME" ] && [[ "$content" != *.* ]]; then
    content="${content}.${domain}"
  fi
  if [ "$type" = "CNAME" ] && [[ "$content" != *. ]]; then
    content="${content}."
  fi

  say "Menambahkan/Mengubah record ${type}: ${fqdn} -> ${content} (TTL: ${ttl})..."

  local payload
  payload=$(jq -n \
    --arg name "$fqdn" \
    --arg type "$type" \
    --arg ttl "$ttl" \
    --arg content "$content" \
    '{
      rrsets: [
        {
          name: $name,
          type: $type,
          ttl: ($ttl | tonumber),
          changetype: "REPLACE",
          records: [
            { content: $content, disabled: false }
          ]
        }
      ]
    }')

  local res
  res=$(pdns_api PATCH "/api/v1/servers/localhost/zones/${domain}" "$payload")
  if [ -z "$res" ]; then
    ok "Record ${fqdn} (${type}) berhasil diperbarui!"
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    die "Gagal memperbarui record: $err_msg"
  fi
}

cmd_record_rm() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  local name="${2:-}"
  local type="${3:-A}"

  [ -n "$domain" ] && [ -n "$name" ] || die "Penggunaan: sudo pdns-mgr record rm <domain> <name|@> <TYPE>"

  domain="${domain%.}."
  local d_clean="${domain%.}"
  local fqdn
  if [ "$name" = "@" ] || [ "$name" = "" ]; then
    fqdn="$domain"
  else
    name="${name%.}"
    if [[ "$name" == *"$d_clean" ]] || [[ "$name" == *"$domain" ]]; then
      fqdn="${name%.}."
    else
      fqdn="${name}.${domain}"
    fi
  fi

  type=$(echo "$type" | tr '[:lower:]' '[:upper:]')
  say "Menghapus record ${type} untuk ${fqdn}..."

  local payload
  payload=$(jq -n \
    --arg name "$fqdn" \
    --arg type "$type" \
    '{
      rrsets: [
        {
          name: $name,
          type: $type,
          changetype: "DELETE"
        }
      ]
    }')

  local res
  res=$(pdns_api PATCH "/api/v1/servers/localhost/zones/${domain}" "$payload")
  if [ -z "$res" ]; then
    ok "Record ${fqdn} (${type}) berhasil dihapus."
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    die "Gagal menghapus record: $err_msg"
  fi
}

cmd_record_list() {
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  if [ -z "$domain" ]; then
    local -a zones=()
    while IFS= read -r z; do [ -n "$z" ] && zones+=("$z"); done < <(cmd_zone_names)
    if [ "${#zones[@]}" -eq 1 ]; then
      domain="${zones[0]}"
      say "${C_YELLOW}[i] Memilih satu-satunya zone aktif: ${domain}${C_RESET}"
    elif [ "${#zones[@]}" -gt 1 ]; then
      say "${C_CYAN}Daftar Domain Aktif:${C_RESET}"
      for i in "${!zones[@]}"; do
        say "  [$((i + 1))] ${zones[$i]}"
      done
      say "  [0] Batal"
      read -r -p "Pilih nomor domain [1-${#zones[@]}]: " sel
      if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le "${#zones[@]}" ]; then
        domain="${zones[$((sel - 1))]}"
      else
        die "Pilihan tidak valid atau dibatalkan."
      fi
    else
      die "Belum ada zone domain terdaftar di PowerDNS. Tambahkan dulu: pdns-mgr zone add <domain>"
    fi
  fi
  domain="${domain%.}."

  local res
  res=$(pdns_api GET "/api/v1/servers/localhost/zones/${domain}")

  say "${C_BOLD}Daftar Record untuk Zone: ${domain}${C_RESET}"
  printf "${C_BOLD}%-35s %-8s %-6s %-30s${C_RESET}\n" "NAME" "TYPE" "TTL" "CONTENT"
  echo "----------------------------------------------------------------------------------"

  echo "$res" | jq -r '.rrsets[] | .name as $n | .type as $t | .ttl as $ttl | .records[] | "\($n) \($t) \($ttl) \(.content)"' | while read -r name type ttl content; do
    printf "%-35s %-8s %-6s %-30s\n" "$name" "$type" "$ttl" "$content"
  done
}

# ==============================================================================
# HELPER ZONE MATCHING & ONE-CLICK EXPOSE
# ==============================================================================

find_matching_zone() {
  local target="${1:-}"
  local clean_target="${target%.}"
  local zone_cand=""

  local zones_json
  zones_json=$(pdns_api GET "/api/v1/servers/localhost/zones" 2>/dev/null || echo "[]")
  while IFS= read -r z; do
    [ -n "$z" ] || continue
    local z_clean="${z%.}"
    if [ "$clean_target" = "$z_clean" ] || [[ "$clean_target" == *".${z_clean}" ]]; then
      if [ "${#z_clean}" -gt "${#zone_cand}" ]; then
        zone_cand="$z_clean"
      fi
    fi
  done < <(echo "$zones_json" | jq -r '.[].name' 2>/dev/null)

  if [ -z "$zone_cand" ]; then
    zone_cand=$(echo "$clean_target" | awk -F. '{if (NF>1) print $(NF-1)"."$NF; else print $0}')
  fi

  echo "$zone_cand"
}

cmd_expose() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  local port_target="${2:-}"
  local use_ssl=false

  shift 2 || true
  while [ $# -gt 0 ]; do
    case "$1" in
      --ssl) use_ssl=true; shift ;;
      *) shift ;;
    esac
  done

  if [ -z "$domain" ] || [ -z "$port_target" ]; then
    say "Penggunaan: sudo pdns-mgr expose <public_domain> <port_or_target> [--ssl]"
    say "Contoh Node : sudo pdns-mgr expose app.domainanda.com 3000 --ssl"
    say "Contoh Go   : sudo pdns-mgr expose api.domainanda.com 8080"
    exit 1
  fi

  local local_nb
  local_nb=$(get_local_netbird_ip)
  [ -n "$local_nb" ] || die "IP Netbird lokal (wt0) tidak terdeteksi. Pastikan Netbird aktif: 'netbird status'"

  say "${C_CYAN}[1/3] Menyiapkan DNS Record di PowerDNS VPS...${C_RESET}"
  local clean_dom="${domain%.}"
  local zone_cand
  zone_cand=$(find_matching_zone "$clean_dom")

  cmd_record_add "$zone_cand" "$clean_dom" "A" auto "300" || {
    warn "Gagal update otomatis ke zone '$zone_cand'. Pastikan zone domain sudah dibuat (pdns-mgr zone add)."
  }

  say "${C_CYAN}[2/3] Mendaftarkan Reverse Proxy di Nginx VPS via Edge Agent...${C_RESET}"
  local payload
  payload=$(jq -n \
    --arg dom "$clean_dom" \
    --arg target "$local_nb" \
    --arg port "$port_target" \
    --argjson ssl "$use_ssl" \
    '{
      domain: $dom,
      target_ip: $target,
      target_port: ($port | tonumber),
      ssl: $ssl,
      websocket: true
    }')

  local res
  res=$(agent_api POST "/api/v1/proxies" "$payload")

  if [ "$(echo "$res" | jq -r '.success // empty' 2>/dev/null)" = "true" ] || echo "$res" | grep -qE '"success"[[:space:]]*:[[:space:]]*true'; then
    local ssl_stat
    ssl_stat=$(echo "$res" | jq -r '.ssl // "none"')
    ok "Aplikasi berhasil diekspos ke internet!"
    say ""
    say "  Domain Publik : ${C_BOLD}http://$clean_dom${C_RESET} $([ "$ssl_stat" = "active" ] && echo "(HTTPS Aktif: https://$clean_dom)")"
    say "  Tujuan Lokal  : ${C_BOLD}http://$local_nb:$port_target${C_RESET} (via Netbird)"
    say "  Status SSL    : ${ssl_stat}"
    say ""
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    die "Gagal mendaftarkan reverse proxy di VPS: $err_msg"
  fi
}

cmd_expose_site() {
  need_root
  need_cmd curl
  need_cmd jq
  need_cmd python3
  load_config

  local local_dom="${1:-}"
  local pub_dom="${2:-}"
  local use_ssl=false

  shift 2 || true
  while [ $# -gt 0 ]; do
    case "$1" in
      --ssl) use_ssl=true; shift ;;
      *) shift ;;
    esac
  done

  if [ -z "$local_dom" ] || [ -z "$pub_dom" ]; then
    say "Penggunaan: sudo pdns-mgr expose-site <local_domain> <public_domain> [--ssl]"
    say "Contoh: sudo pdns-mgr expose-site appsone.test appsone.domainanda.com --ssl"
    exit 1
  fi

  local conf="/etc/nginx/sites-available/${local_dom}.conf"
  if [ ! -f "$conf" ]; then
    if [ -f "/etc/nginx/sites-available/${local_dom}" ]; then
      conf="/etc/nginx/sites-available/${local_dom}"
    else
      die "Konfigurasi vhost lokal untuk '${local_dom}' tidak ditemukan. Buat dulu: sudo dev-site create ${local_dom} <folder> [php_ver]"
    fi
  fi

  say "${C_CYAN}[1/2] Mengonfigurasi vhost Nginx lokal (${local_dom}) untuk '${pub_dom}'...${C_RESET}"
  python3 -c '
import sys, re

conf_path = sys.argv[1]
pub_dom = sys.argv[2]

with open(conf_path, "r") as f:
    text = f.read()

# 1. Cek jika pub_dom sudah ada di server_name
if not re.search(r"server_name[^;]*\b" + re.escape(pub_dom) + r"\b", text):
    # Hapus standalone port 80 redirect block jika ada
    text = re.sub(r"server\s*\{[^{}]*listen\s+80[^{}]*return\s+301\s+https://\$host\$request_uri;\s*\}\s*", "", text)

    # Pastikan listen 80 ada di server block utama
    if "listen 80;" not in text:
        text = re.sub(r"(server\s*\{[ \t]*\n)", r"\1    listen 80;\n    listen [::]:80;\n", text, count=1)

    # Tambahkan alias ke server_name
    text = re.sub(r"(server_name\s+[^;]+);", rf"\1 {pub_dom};", text, count=1)

    # Tambahkan aturan redirect kondisional & fe_https jika belum ada
    if "$redirect_to_https" not in text:
        proxy_rules = """
    # Handle HTTP to HTTPS redirect for direct non-proxy connections
    set $redirect_to_https 0;
    if ($scheme = "http") {
        set $redirect_to_https 1;
    }
    if ($http_x_forwarded_proto = "https") {
        set $redirect_to_https 0;
    }
    if ($redirect_to_https = 1) {
        return 301 https://$host$request_uri;
    }

    # Pass HTTPS flag to PHP-FPM when proxied behind Edge Gateway SSL
    set $fe_https $https;
    if ($http_x_forwarded_proto = "https") {
        set $fe_https on;
    }
"""
        text = re.sub(r"(server_name\s+[^;]+;\n)", rf"\1{proxy_rules}", text, count=1)

    # Tambahkan fastcgi_param HTTPS $fe_https ke PHP block
    if ("location ~ \\.php$" in text or "location ~ \\.php" in text) and "fastcgi_param HTTPS" not in text:
        text = re.sub(
            r"(include\s+fastcgi_params;\n)",
            r"\1        fastcgi_param HTTPS $fe_https if_not_empty;\n",
            text,
            count=1
        )

    with open(conf_path, "w") as f:
        f.write(text)
' "$conf" "$pub_dom"

  if /usr/sbin/nginx -t >/dev/null 2>&1 || nginx -t >/dev/null 2>&1; then
    systemctl reload nginx 2>/dev/null || true
    ok "Konfigurasi vhost lokal diperbarui dan Nginx di-reload."
  else
    warn "Uji konfigurasi Nginx lokal gagal setelah memperbarui vhost."
  fi

  say "${C_CYAN}[2/2] Mendaftarkan rute reverse proxy di VPS (Port 80 Nginx Lokal)...${C_RESET}"
  if [ "$use_ssl" = true ]; then
    cmd_expose "$pub_dom" 80 --ssl
  else
    cmd_expose "$pub_dom" 80
  fi
}

cmd_unexpose() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  local keep_dns=false

  shift || true
  while [ $# -gt 0 ]; do
    case "$1" in
      --keep-dns) keep_dns=true; shift ;;
      *) shift ;;
    esac
  done

  if [ -z "$domain" ]; then
    local -a proxies=()
    while IFS= read -r p; do [ -n "$p" ] && proxies+=("$p"); done < <(cmd_proxy_names)
    if [ "${#proxies[@]}" -eq 0 ]; then
      die "Belum ada aplikasi atau proxy yang diekspos di VPS."
    fi
    say "${C_CYAN}Pilih aplikasi / domain yang akan di-unexpose:${C_RESET}"
    for i in "${!proxies[@]}"; do
      say "  [$((i + 1))] ${proxies[$i]}"
    done
    say "  [0] Batal"
    read -r -p "Pilih nomor [1-${#proxies[@]}]: " sel
    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le "${#proxies[@]}" ]; then
      domain="${proxies[$((sel - 1))]}"
    else
      die "Pilihan tidak valid atau dibatalkan."
    fi
  fi

  local clean_dom="${domain%.}"

  say "${C_CYAN}[1/3] Menghapus konfigurasi reverse proxy '${clean_dom}' dari VPS...${C_RESET}"
  local res
  res=$(agent_api DELETE "/api/v1/proxies/${clean_dom}")

  if [ "$(echo "$res" | jq -r '.success // empty' 2>/dev/null)" = "true" ] || echo "$res" | grep -qE '"success"[[:space:]]*:[[:space:]]*true'; then
    ok "Proxy '${clean_dom}' berhasil dihapus dari Nginx VPS."
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    warn "Respon VPS: $err_msg"
  fi

  # 2. Hapus DNS Record di PowerDNS (jika bukan --keep-dns)
  if [ "$keep_dns" = false ]; then
    say "${C_CYAN}[2/3] Menghapus DNS record '${clean_dom}' di PowerDNS VPS...${C_RESET}"
    local zone_cand
    zone_cand=$(find_matching_zone "$clean_dom")
    if [ -n "$zone_cand" ]; then
      if [ "$clean_dom" != "$zone_cand" ]; then
        # Subdomain: hapus record A dan CNAME
        cmd_record_rm "$zone_cand" "$clean_dom" "A" >/dev/null 2>&1 || true
        cmd_record_rm "$zone_cand" "$clean_dom" "CNAME" >/dev/null 2>&1 || true
        ok "DNS Record untuk subdomain '${clean_dom}' berhasil dihapus dari zone '${zone_cand}'."
      else
        say "${C_YELLOW}[i] Domain '${clean_dom}' adalah zone apex domain utama; DNS record apex dipertahankan.${C_RESET}"
      fi
    else
      warn "Zone domain untuk '${clean_dom}' tidak ditemukan di PowerDNS."
    fi
  else
    say "${C_CYAN}[2/3] DNS Record dipertahankan (--keep-dns).${C_RESET}"
  fi

  # 3. Lepaskan alias dari Nginx lokal jika ada
  say "${C_CYAN}[3/3] Memeriksa konfigurasi Nginx lokal...${C_RESET}"
  local cleaned_vhost=false
  for conf in /etc/nginx/sites-available/*.conf /etc/nginx/sites-available/*; do
    [ -f "$conf" ] || continue
    if grep -q -w "${clean_dom}" "$conf" 2>/dev/null; then
      sed -i -E "s/[ \t]*\b${clean_dom}\b//g" "$conf"
      cleaned_vhost=true
      ok "Alias '${clean_dom}' dilepas dari $(basename "$conf")."
    fi
  done
  if [ "$cleaned_vhost" = true ]; then
    if /usr/sbin/nginx -t >/dev/null 2>&1 || nginx -t >/dev/null 2>&1; then
      systemctl reload nginx 2>/dev/null || true
      ok "Nginx lokal di-reload."
    fi
  else
    say "[i] Tidak ada alias lokal yang perlu dibersihkan."
  fi

  ok "Selesai: eksposur '${clean_dom}' telah sepenuhnya dihapus (Proxy VPS, DNS Record, & Alias Lokal)."
}

cmd_list_exposures() {
  need_cmd curl
  need_cmd jq
  load_config

  say "${C_BOLD}=== Daftar Domain / Port yang Diekspos ke VPS ===${C_RESET}"
  local res
  res=$(agent_api GET "/api/v1/proxies")

  local count
  count=$(echo "$res" | jq -r '.proxies | length' 2>/dev/null || echo 0)

  if [ "$count" -eq 0 ] 2>/dev/null || [ -z "$count" ]; then
    say "${C_YELLOW}[i] Belum ada domain atau aplikasi yang diekspos ke VPS.${C_RESET}"
    say "Gunakan: sudo pdns-mgr expose <domain> <port> [--ssl] atau opsi [10] di menu setup_dev.sh"
    return 0
  fi

  printf "${C_BOLD}%-35s %-12s %-30s${C_RESET}\n" "DOMAIN" "STATUS" "CONFIG FILE"
  echo "--------------------------------------------------------------------------------"
  echo "$res" | jq -r '.proxies[]? | "\(.domain) \(.enabled) \(.config_file)"' | while read -r dom en cfg; do
    if [ "$en" = "true" ]; then
      printf "%-35s ${C_GREEN}%-12s${C_RESET} %-30s\n" "$dom" "Active" "$cfg"
    else
      printf "%-35s ${C_RED}%-12s${C_RESET} %-30s\n" "$dom" "Disabled" "$cfg"
    fi
  done
  echo "--------------------------------------------------------------------------------"
  say "Total: ${count} domain/aplikasi aktif di reverse proxy VPS."
}

# ==============================================================================
# MENU BANTUAN
# ==============================================================================

cmd_help() {
  say "${C_BOLD}${C_CYAN}pdns-mgr — Pengelola PowerDNS & VPS Edge Gateway untuk wdev${C_RESET}"
  say ""
  say "Penggunaan: sudo pdns-mgr <perintah> [argumen]"
  say ""
  say "${C_BOLD}Setup & Otomasi VPS:${C_RESET}"
  say "  deploy-vps [IP_NETBIRD] [USER]                         Pasang & setup otomatis ke VPS via SSH Netbird"
  say "  connect --vps-ip <IP> --public-ip <IP> --api-key <KEY>  Hubungkan manual ke VPS Edge"
  say "  status                                                 Cek status koneksi & server"
  say ""
  say "${C_BOLD}Manajemen Zone Domain:${C_RESET}"
  say "  zone add <domain> [ns1] [ns2]     Inisialisasi zone DNS domain baru"
  say "  zone list                         Daftar semua zone domain aktif"
  say "  zone rm <domain>                  Hapus zone domain"
  say ""
  say "${C_BOLD}Manajemen DNS Record:${C_RESET}"
  say "  record add <domain> <subdomain|@> <TYPE> [VAL|auto] [TTL] Tambah/ubah record"
  say "  record rm <domain> <subdomain|@> <TYPE>                   Hapus record"
  say "  record list <domain>                                      Lihat daftar record"
  say ""
  say "${C_BOLD}Eksposur Aplikasi Port & Web (Node.js, Go, Python, PHP, Laravel):${C_RESET}"
  say "  expose <domain> <local_port> [--ssl]                   Otomatis DNS + Nginx reverse proxy VPS"
  say "  expose-site <local_domain> <pub_domain> [--ssl]        Ekspos vhost lokal (Laravel/PHP) ke VPS"
  say "  unexpose <domain>                                      Hapus proxy domain dari VPS"
  say "  list-exposures                                         Lihat daftar proxy aktif di VPS"
  say ""
}

# Dispatcher Utama
case "${1:-help}" in
  deploy-vps|install-vps) shift; cmd_deploy_vps "$@" ;;
  connect|setup) shift; cmd_connect "$@" ;;
  status) shift; cmd_status "$@" ;;
  zone)
    shift
    sub="${1:-list}"
    shift || true
    case "$sub" in
      add) cmd_zone_add "$@" ;;
      rm|delete) cmd_zone_rm "$@" ;;
      list) cmd_zone_list "$@" ;;
      names|raw) cmd_zone_names "$@" ;;
      *) die "Subperintah zone tidak dikenal: $sub" ;;
    esac
    ;;
  record)
    shift
    sub="${1:-list}"
    shift || true
    case "$sub" in
      add) cmd_record_add "$@" ;;
      rm|delete) cmd_record_rm "$@" ;;
      list) cmd_record_list "$@" ;;
      *) die "Subperintah record tidak dikenal: $sub" ;;
    esac
    ;;
  expose) shift; cmd_expose "$@" ;;
  expose-site) shift; cmd_expose_site "$@" ;;
  unexpose) shift; cmd_unexpose "$@" ;;
  list-exposures|exposed|proxies) shift; cmd_list_exposures "$@" ;;
  proxy-names|proxies-raw) shift; cmd_proxy_names "$@" ;;
  -h|--help|help) cmd_help ;;
  *) die "Perintah tidak dikenal: $1. Jalankan 'pdns-mgr help' untuk melihat opsi." ;;
esac
