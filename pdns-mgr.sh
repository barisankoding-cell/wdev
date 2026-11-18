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

  load_config 2>/dev/null || true

  say "${C_BOLD}${C_CYAN}=== Automated Remote VPS Edge Deployment & Update via Netbird ===${C_RESET}"
  say ""

  if [ -z "$vps_ip" ]; then
    local def_ip="${VPS_NETBIRD_IP:-}"
    if [ -n "$def_ip" ]; then
      read -r -p "Masukkan Netbird IP VPS [default: $def_ip]: " input_ip
      vps_ip="${input_ip:-$def_ip}"
    else
      read -r -p "Masukkan Netbird IP VPS (contoh: 100.64.0.1): " input_ip
      vps_ip="${input_ip:-}"
    fi
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
  if [ "$type" = "TXT" ]; then
    if [[ "$content" != \"* ]] || [[ "$content" != *\" ]]; then
      content="\"$content\""
    fi
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
# MANAJEMEN MAIL SERVER & WEBMAIL (HYBRID LOCAL STORAGE + VPS MX QUEUE)
# ==============================================================================

ensure_local_mail_stack() {
  say "Memeriksa kesiapan Mail Server & Webmail lokal (Stalwart & SnappyMail)..."
  local base_dir="/mnt/f7d8a899-1e8d-4c44-afaf-2f7c69f2cce5/mail-data"
  [ -d "$base_dir" ] || base_dir="/var/mail-data"

  local st_dir="$base_dir/stalwart"
  local sn_dir="$base_dir/snappymail"
  mkdir -p "$st_dir" "$sn_dir" /var/www/snappymail 2>/dev/null || true

  # 1. Symlink /var/lib/stalwart ke partisi lokal berkapasitas besar (382 GB)
  if [ ! -L "/var/lib/stalwart" ]; then
    mkdir -p /var/lib
    ln -sfn "$st_dir" /var/lib/stalwart
  fi

  # 2. Stalwart Server
  if ! command -v stalwart >/dev/null 2>&1 && [ ! -f /usr/local/bin/stalwart ]; then
    say "Mengunduh & menginstal Stalwart Mail Server di mesin lokal..."
    curl --proto '=https' --tlsv1.2 -sSf https://get.stalw.art/install.sh | sh
  fi

  # 3. Stalwart CLI
  if ! command -v stalwart-cli >/dev/null 2>&1 && [ ! -f /usr/local/bin/stalwart-cli ]; then
    say "Mengunduh & menginstal Stalwart CLI..."
    STALWART_CLI_INSTALL_DIR=/usr/local curl --proto '=https' --tlsv1.2 -LsSf https://github.com/stalwartlabs/cli/releases/latest/download/stalwart-cli-installer.sh | sh 2>/dev/null || true
    if [ -f /root/.cargo/bin/stalwart-cli ]; then
      cp /root/.cargo/bin/stalwart-cli /usr/local/bin/stalwart-cli
      chmod +x /usr/local/bin/stalwart-cli
    fi
  fi

  mkdir -p /var/log/stalwart 2>/dev/null || true
  chown stalwart:stalwart /var/log/stalwart 2>/dev/null || true
  chown -R stalwart:stalwart "$st_dir" 2>/dev/null || true
  systemctl enable stalwart 2>/dev/null || true
  systemctl start stalwart 2>/dev/null || true

  # 4. SnappyMail lokal
  if [ ! -f /var/www/snappymail/index.php ]; then
    say "Mengunduh & menginstal SnappyMail Web Client di mesin lokal..."
    local snap_tar
    snap_tar=$(curl -sL https://api.github.com/repos/the-djmaze/snappymail/releases/latest | grep "browser_download_url.*\.tar\.gz" | grep -v "cpanel\|nextcloud" | head -n 1 | cut -d '"' -f 4)
    [ -n "$snap_tar" ] || snap_tar="https://github.com/the-djmaze/snappymail/releases/download/v3.0.1/snappymail-3.0.1.tar.gz"
    curl -sSL "$snap_tar" | tar -xz -C /var/www/snappymail
    chown -R www-data:www-data /var/www/snappymail
    chmod -R 755 /var/www/snappymail
  fi

  # 5. Konfigurasi Vhost Nginx lokal untuk SnappyMail
  local php_sock="/run/php/php8.4-fpm.sock"
  if [ ! -S "$php_sock" ] && [ -d "/run/php" ]; then
    for s in /run/php/*fpm.sock; do
      [ -S "$s" ] && { php_sock="$s"; break; }
    done
  fi

  local local_vhost="/etc/nginx/sites-available/wdev_webmail.conf"
  if [ ! -f "$local_vhost" ]; then
    cat <<EOF > "$local_vhost"
# Managed by wdev (Local SnappyMail Web Client)
server {
    listen 80;
    server_name webmail.* webmail.test;

    root /var/www/snappymail;
    index index.php index.html;
    client_max_body_size 50M;

    location / {
        try_files \$uri \$uri/ /index.php?\$query_string;
    }

    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:${php_sock};
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
    }

    location ~ ^/(data|config)/ {
        deny all;
        return 404;
    }
}
EOF
    ln -sfn "$local_vhost" /etc/nginx/sites-enabled/wdev_webmail.conf
    nginx -t >/dev/null 2>&1 && systemctl reload nginx 2>/dev/null || true
  fi
  if wait_stalwart_ready; then
    ok "Stalwart aktif di ${STALWART_LOCAL_URL} (WebUI admin: /admin/)."
  else
    warn "Stalwart belum merespons di ${STALWART_LOCAL_URL}. Cek: journalctl -u stalwart -n 100"
  fi
  if [ -f /var/www/snappymail/index.php ]; then
    ok "SnappyMail tersedia di /var/www/snappymail."
  else
    warn "SnappyMail belum terpasang dengan benar."
  fi
  ok "Mail Server & Webmail lokal siap digunakan."
}

# ------------------------------------------------------------------------------
# Helper Provisioning Stalwart v1.x (bootstrap, domain, akun admin, kredensial)
# ------------------------------------------------------------------------------
STALWART_ENV_FILE="${STALWART_ENV_FILE:-/etc/stalwart/stalwart.env}"
STALWART_CONFIG_FILE="${STALWART_CONFIG_FILE:-/etc/stalwart/config.json}"
STALWART_LOCAL_URL="${STALWART_LOCAL_URL:-http://127.0.0.1:8080}"
STALWART_DATA_PATH="${STALWART_DATA_PATH:-/var/lib/stalwart/}"
MAIL_ADMIN_CRED_FILE="${MAIL_ADMIN_CRED_FILE:-$WDEV_ETC/mail-admin.cred}"
MAIL_RELAY_CRED_FILE="${MAIL_RELAY_CRED_FILE:-$WDEV_ETC/mail-relay.cred}"
MAIL_RELAY_DOMAINS_CRED_FILE="${MAIL_RELAY_DOMAINS_CRED_FILE:-$WDEV_ETC/mail-relay-domains.cred}"
RESEND_API_BASE="${RESEND_API_BASE:-https://api.resend.com}"
BREVO_API_BASE="${BREVO_API_BASE:-https://api.brevo.com/v3}"

stalwart_cli_path() {
  local p
  p=$(command -v stalwart-cli 2>/dev/null || true)
  [ -n "$p" ] || p="/usr/local/bin/stalwart-cli"
  printf '%s' "$p"
}

stalwart_http_ready() {
  curl -fsS -m 3 -o /dev/null "${STALWART_LOCAL_URL}/admin/" 2>/dev/null
}

wait_stalwart_ready() {
  local _i
  for _i in $(seq 1 30); do
    stalwart_http_ready && return 0
    sleep 1
  done
  return 1
}

stalwart_env_set() {
  local key="$1" val="$2"
  [ -f "$STALWART_ENV_FILE" ] || touch "$STALWART_ENV_FILE"
  if grep -q "^${key}=" "$STALWART_ENV_FILE" 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${val}|" "$STALWART_ENV_FILE"
  else
    printf '\n%s=%s\n' "$key" "$val" >> "$STALWART_ENV_FILE"
  fi
}

stalwart_env_unset() {
  local key="$1"
  [ -f "$STALWART_ENV_FILE" ] && sed -i "/^${key}=/d" "$STALWART_ENV_FILE" || true
}

stalwart_random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
  else
    head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 32
  fi
}

run_stalwart_cli() {
  local _user="$1" _pass="$2"
  shift 2
  STALWART_URL="$STALWART_LOCAL_URL" \
  STALWART_USER="$_user" \
  STALWART_PASSWORD="$_pass" \
    "$(stalwart_cli_path)" "$@"
}

# query --json dapat mengembalikan satu objek, satu array, atau JSON-per-baris.
# Normalisasi selalu menjadi array JSON agar aman diproses jq berikutnya.
stalwart_query_json() {
  local _user="$1" _pass="$2"
  shift 2
  run_stalwart_cli "$_user" "$_pass" "$@" --json 2>/dev/null \
    | jq -s 'if length == 0 then [] elif length == 1 and (.[0] | type) == "array" then .[0] else . end' 2>/dev/null \
    || printf '[]'
}

mail_admin_cred_set() {
  local domain="$1" email="$2" password="$3" tmp
  mkdir -p "$(dirname "$MAIL_ADMIN_CRED_FILE")"
  tmp=$(mktemp)
  if [ -f "$MAIL_ADMIN_CRED_FILE" ] && jq -e . "$MAIL_ADMIN_CRED_FILE" >/dev/null 2>&1; then
    jq --arg d "$domain" --arg e "$email" --arg p "$password" \
      '.[$d] = {email: $e, password: $p, updatedAt: (now | todate)}' \
      "$MAIL_ADMIN_CRED_FILE" > "$tmp"
  else
    jq -n --arg d "$domain" --arg e "$email" --arg p "$password" \
      '{($d): {email: $e, password: $p, updatedAt: (now | todate)}}' > "$tmp"
  fi
  install -m 600 "$tmp" "$MAIL_ADMIN_CRED_FILE"
  rm -f "$tmp"
}

mail_admin_cred_get() {
  local domain="$1" key="$2"
  [ -f "$MAIL_ADMIN_CRED_FILE" ] || return 1
  jq -r --arg d "$domain" --arg k "$key" '.[$d][$k] // empty' "$MAIL_ADMIN_CRED_FILE" 2>/dev/null
}

# Pilih kredensial admin pertama yang masih valid. Output: email<TAB>password
mail_pick_admin_cred() {
  local domain_filter="${1:-}" cand_email cand_pass i
  local -a cred_emails=() cred_passwords=()
  if [ -n "$domain_filter" ] && [ -n "$(mail_admin_cred_get "$domain_filter" email || true)" ]; then
    cred_emails+=("$(mail_admin_cred_get "$domain_filter" email || true)")
    cred_passwords+=("$(mail_admin_cred_get "$domain_filter" password || true)")
  fi
  while IFS=$'\t' read -r cand_email cand_pass; do
    [ -n "$cand_email" ] || continue
    if [ "${#cred_emails[@]}" -gt 0 ] && [ "$cand_email" = "${cred_emails[0]}" ]; then
      continue
    fi
    cred_emails+=("$cand_email")
    cred_passwords+=("$cand_pass")
  done < <(jq -r 'to_entries[] | [.value.email, .value.password] | @tsv' "$MAIL_ADMIN_CRED_FILE" 2>/dev/null || true)

  for i in "${!cred_emails[@]}"; do
    if run_stalwart_cli "${cred_emails[$i]}" "${cred_passwords[$i]}" query Account --fields id --json >/dev/null 2>&1; then
      printf '%s\t%s\n' "${cred_emails[$i]}" "${cred_passwords[$i]}"
      return 0
    fi
  done
  return 1
}

mail_relay_cred_set() {
  local provider="$1" host="$2" port="$3" implicit="$4" username="$5" password="$6" tmp
  mkdir -p "$(dirname "$MAIL_RELAY_CRED_FILE")"
  tmp=$(mktemp)
  jq -n --arg provider "$provider" --arg host "$host" --argjson port "$port" \
    --argjson implicit "$implicit" --arg username "$username" --arg password "$password" '{
      provider: $provider,
      host: $host,
      port: $port,
      implicitTls: $implicit,
      username: $username,
      password: $password,
      updatedAt: (now | todate)
    }' > "$tmp"
  install -m 600 "$tmp" "$MAIL_RELAY_CRED_FILE"
  rm -f "$tmp"
}

mail_relay_domain_cred_set() {
  local domain="$1" provider="$2" host="$3" port="$4" implicit="$5" \
    username="$6" password="$7" route="$8" tmp
  mkdir -p "$(dirname "$MAIL_RELAY_DOMAINS_CRED_FILE")"
  tmp=$(mktemp)
  if [ -f "$MAIL_RELAY_DOMAINS_CRED_FILE" ] && jq -e . "$MAIL_RELAY_DOMAINS_CRED_FILE" >/dev/null 2>&1; then
    jq --arg d "$domain" --arg provider "$provider" --arg host "$host" --argjson port "$port" \
      --argjson implicit "$implicit" --arg username "$username" --arg password "$password" \
      --arg route "$route" '
        .[$d] = {
          provider: $provider,
          host: $host,
          port: $port,
          implicitTls: $implicit,
          username: $username,
          password: $password,
          route: $route,
          updatedAt: (now | todate)
        }' "$MAIL_RELAY_DOMAINS_CRED_FILE" > "$tmp"
  else
    jq -n --arg d "$domain" --arg provider "$provider" --arg host "$host" --argjson port "$port" \
      --argjson implicit "$implicit" --arg username "$username" --arg password "$password" \
      --arg route "$route" '
        {($d): {
          provider: $provider,
          host: $host,
          port: $port,
          implicitTls: $implicit,
          username: $username,
          password: $password,
          route: $route,
          updatedAt: (now | todate)
        }}' > "$tmp"
  fi
  install -m 600 "$tmp" "$MAIL_RELAY_DOMAINS_CRED_FILE"
  rm -f "$tmp"
}

mail_relay_domain_cred_remove() {
  local domain="$1" tmp
  [ -f "$MAIL_RELAY_DOMAINS_CRED_FILE" ] || return 0
  tmp=$(mktemp)
  jq --arg d "$domain" 'del(.[$d])' "$MAIL_RELAY_DOMAINS_CRED_FILE" > "$tmp" 2>/dev/null || printf '{}' > "$tmp"
  install -m 600 "$tmp" "$MAIL_RELAY_DOMAINS_CRED_FILE"
  rm -f "$tmp"
}

stalwart_find_domain_id() {
  local user="$1" pass="$2" domain="$3"
  stalwart_query_json "$user" "$pass" query Domain --where "name=${domain}" --fields id \
    | jq -r '.[0].id // empty' 2>/dev/null || true
}

stalwart_find_admin() {
  local user="$1" pass="$2" did="$3" out
  out=$(stalwart_query_json "$user" "$pass" query Account --where "domainId=${did}" \
    --fields id,emailAddress,domainId,roles)
  if [ "$(printf '%s' "$out" | jq 'length' 2>/dev/null || echo 0)" -eq 0 ]; then
    out=$(stalwart_query_json "$user" "$pass" query Account \
      --fields id,emailAddress,domainId,roles)
  fi
  printf '%s' "$out" | jq -r --arg did "$did" '
    .[] | select((.domainId == $did) and (.roles."@type" == "Admin"))
    | [.id, .emailAddress] | @tsv' 2>/dev/null | head -n 1 || true
}

# Setup awal Stalwart tanpa wizard interaktif. Server menulis config.json dan
# mengembalikan username/secret administrator yang baru dibuat.
ensure_stalwart_bootstrap() {
  local domain="$1" hostname="${2:-mail.$1}"
  if [ -f "$STALWART_CONFIG_FILE" ]; then
    return 0
  fi

  say "${C_CYAN}Stalwart belum dikonfigurasi (bootstrap). Menjalankan setup awal otomatis...${C_RESET}"
  local rec_pass payload out admin_email admin_pass
  rec_pass=$(stalwart_random_secret)
  stalwart_env_set STALWART_RECOVERY_ADMIN "wdev-bootstrap:${rec_pass}"
  systemctl restart stalwart
  if ! wait_stalwart_ready; then
    stalwart_env_unset STALWART_RECOVERY_ADMIN
    die "Stalwart tidak merespons di ${STALWART_LOCAL_URL} saat bootstrap."
  fi

  payload=$(mktemp)
  jq -n --arg d "$domain" --arg h "$hostname" --arg path "$STALWART_DATA_PATH" '{
    defaultDomain: $d,
    defaultHostname: $h,
    dataStore: {"@type": "RocksDb", "path": $path},
    blobStore: {"@type": "Default"},
    searchStore: {"@type": "Default"},
    inMemoryStore: {"@type": "Default"},
    directory: {"@type": "Internal"},
    dnsServer: {"@type": "Manual"},
    requestTlsCertificate: false,
    generateDkimKeys: true
  }' > "$payload"

  out=$(run_stalwart_cli "wdev-bootstrap" "$rec_pass" update Bootstrap --file "$payload" 2>&1 || true)
  rm -f "$payload"

  admin_email=$(printf '%s\n' "$out" | sed -n 's/.*username:[[:space:]]*"\{0,1\}\([^"[:space:]]*\)"\{0,1\}.*/\1/p' | head -n 1)
  admin_pass=$(printf '%s\n' "$out" | sed -n 's/.*secret:[[:space:]]*"\{0,1\}\([^"[:space:]]*\)"\{0,1\}.*/\1/p' | head -n 1)

  if ! printf '%s' "$out" | grep -qi 'Updated Bootstrap' || [ -z "$admin_email" ] || [ -z "$admin_pass" ]; then
    stalwart_env_unset STALWART_RECOVERY_ADMIN
    systemctl restart stalwart 2>/dev/null || true
    die "Setup awal Stalwart gagal. Output: ${out}"
  fi

  mail_admin_cred_set "$domain" "$admin_email" "$admin_pass"
  stalwart_env_unset STALWART_RECOVERY_ADMIN
  systemctl restart stalwart
  wait_stalwart_ready || warn "Stalwart belum merespons setelah restart normal."
  ok "Setup awal Stalwart selesai. Admin: ${admin_email}"
  say "  Kredensial disimpan di ${C_BOLD}${MAIL_ADMIN_CRED_FILE}${C_RESET} (0600)."
}

# Reset password administrator domain lewat recovery admin sementara.
stalwart_reset_admin() {
  local domain="$1"
  [ -n "$domain" ] || die "Domain wajib diisi untuk reset admin."

  if [ ! -f "$STALWART_CONFIG_FILE" ]; then
    ensure_stalwart_bootstrap "$domain" "mail.${domain}"
    return 0
  fi

  local rec_pass did admin_line admin_id admin_email new_pass out cred_json
  rec_pass=$(stalwart_random_secret)
  say "${C_CYAN}Mengaktifkan recovery admin Stalwart sementara...${C_RESET}"
  stalwart_env_set STALWART_RECOVERY_ADMIN "wdev-recovery:${rec_pass}"
  systemctl restart stalwart
  if ! wait_stalwart_ready; then
    stalwart_env_unset STALWART_RECOVERY_ADMIN
    systemctl restart stalwart 2>/dev/null || true
    die "Stalwart tidak merespons saat mengaktifkan recovery mode."
  fi

  did=$(stalwart_find_domain_id "wdev-recovery" "$rec_pass" "$domain")
  if [ -z "$did" ]; then
    out=$(run_stalwart_cli "wdev-recovery" "$rec_pass" create Domain \
      --field "name=${domain}" --field enable=true 2>&1 || true)
    did=$(stalwart_find_domain_id "wdev-recovery" "$rec_pass" "$domain")
  fi
  if [ -z "$did" ]; then
    stalwart_env_unset STALWART_RECOVERY_ADMIN
    systemctl restart stalwart 2>/dev/null || true
    die "Domain '${domain}' tidak ditemukan dan gagal dibuat di Stalwart. Output: ${out:-<kosong>}"
  fi

  admin_line=$(stalwart_find_admin "wdev-recovery" "$rec_pass" "$did")
  admin_id=$(printf '%s' "$admin_line" | cut -f1)
  admin_email=$(printf '%s' "$admin_line" | cut -f2)
  new_pass=$(stalwart_random_secret)
  cred_json=$(jq -nc --arg p "$new_pass" '{"0":{"@type":"Password","secret":$p}}')

  if [ -n "$admin_id" ] && [ -n "$admin_email" ]; then
    out=$(run_stalwart_cli "wdev-recovery" "$rec_pass" update Account "$admin_id" \
      --field "credentials=${cred_json}" 2>&1 || true)
  else
    admin_email="admin@${domain}"
    out=$(run_stalwart_cli "wdev-recovery" "$rec_pass" create Account/User \
      --field name=admin \
      --field "domainId=${did}" \
      --field 'roles={"@type":"Admin"}' \
      --field description='System administrator' \
      --field "credentials=${cred_json}" 2>&1 || true)
    admin_line=$(stalwart_find_admin "wdev-recovery" "$rec_pass" "$did")
    admin_id=$(printf '%s' "$admin_line" | cut -f1)
    admin_email=$(printf '%s' "$admin_line" | cut -f2)
  fi

  stalwart_env_unset STALWART_RECOVERY_ADMIN
  systemctl restart stalwart
  wait_stalwart_ready || warn "Stalwart belum merespons setelah recovery mode dimatikan."

  if [ -z "$admin_email" ] || [ -z "$admin_id" ]; then
    die "Gagal membuat/menemukan akun administrator untuk '${domain}'. Output: ${out}"
  fi

  mail_admin_cred_set "$domain" "$admin_email" "$new_pass"
  ok "Password administrator '${admin_email}' berhasil direset."
}

# Pastikan domain dan akun admin tersedia di Stalwart lokal.
ensure_stalwart_mail_domain() {
  local domain="$1" email pass did admin_line admin_id admin_email new_pass cred_json out
  email=$(mail_admin_cred_get "$domain" email || true)
  pass=$(mail_admin_cred_get "$domain" password || true)

  if [ -n "$email" ] && [ -n "$pass" ] && run_stalwart_cli "$email" "$pass" query Domain --json >/dev/null 2>&1; then
    :
  elif [ -n "$email" ] && [ -n "$pass" ]; then
    warn "Kredensial admin tersimpan untuk '${domain}' tidak valid (mungkin password sudah diganti)."
    warn "Jalankan: ${C_BOLD}sudo pdns-mgr mail admin-reset ${domain}${C_RESET} lalu ulangi mail setup."
    return 1
  else
    stalwart_reset_admin "$domain" || return 1
    email=$(mail_admin_cred_get "$domain" email || true)
    pass=$(mail_admin_cred_get "$domain" password || true)
  fi
  [ -n "$email" ] && [ -n "$pass" ] || return 1

  did=$(stalwart_find_domain_id "$email" "$pass" "$domain")
  if [ -z "$did" ]; then
    say "Mendaftarkan domain '${domain}' di Stalwart..."
    out=$(run_stalwart_cli "$email" "$pass" create Domain --field "name=${domain}" --field enable=true 2>&1 || true)
    did=$(stalwart_find_domain_id "$email" "$pass" "$domain")
  fi
  if [ -z "$did" ]; then
    warn "Gagal mendaftarkan domain '${domain}' di Stalwart. Output: ${out:-<kosong>}"
    return 1
  fi

  admin_line=$(stalwart_find_admin "$email" "$pass" "$did")
  admin_id=$(printf '%s' "$admin_line" | cut -f1)
  admin_email=$(printf '%s' "$admin_line" | cut -f2)
  if [ -z "$admin_id" ]; then
    say "Membuat akun administrator untuk '${domain}'..."
    new_pass=$(stalwart_random_secret)
    cred_json=$(jq -nc --arg p "$new_pass" '{"0":{"@type":"Password","secret":$p}}')
    out=$(run_stalwart_cli "$email" "$pass" create Account/User \
      --field name=admin \
      --field "domainId=${did}" \
      --field 'roles={"@type":"Admin"}' \
      --field description='System administrator' \
      --field "credentials=${cred_json}" 2>&1 || true)
    admin_line=$(stalwart_find_admin "$email" "$pass" "$did")
    admin_id=$(printf '%s' "$admin_line" | cut -f1)
    admin_email=$(printf '%s' "$admin_line" | cut -f2)
    if [ -n "$admin_email" ]; then
      mail_admin_cred_set "$domain" "$admin_email" "$new_pass"
      ok "Akun administrator '${admin_email}' dibuat."
    else
      warn "Gagal membuat akun administrator untuk '${domain}'. Output: ${out:-<kosong>}"
      return 1
    fi
  fi
  return 0
}

ensure_stalwart_public_url() {
  local domain="$1" url="https://mailadmin.${1}" cur
  cur=$(grep -E '^STALWART_PUBLIC_URL=' "$STALWART_ENV_FILE" 2>/dev/null | tail -n 1 | cut -d= -f2- || true)
  [ "$cur" = "$url" ] && return 0
  stalwart_env_set STALWART_PUBLIC_URL "$url"
  systemctl restart stalwart
  wait_stalwart_ready || warn "Stalwart belum merespons setelah memperbarui STALWART_PUBLIC_URL."
  ok "STALWART_PUBLIC_URL diset ke ${url}"
}

# SnappyMail lokal terhubung ke Stalwart via IMAPS 993 / SMTPS 465.
# Sertifikat Stalwart lokal bersifat self-signed, jadi verifikasi cert dimatikan
# khusus untuk koneksi loopback ini.
ensure_snappymail_domain_config() {
  local domain="$1"
  local snap_root="/var/www/snappymail"
  local snap_dom_dir="$snap_root/data/_data_/_default_/domains"
  [ -d "$snap_root" ] || return 0
  mkdir -p "$snap_dom_dir"

  cat > "$snap_dom_dir/${domain}.json" <<'EOF'
{
    "IMAP": {
        "host": "127.0.0.1",
        "port": 993,
        "type": 1,
        "timeout": 300,
        "shortLogin": false,
        "lowerLogin": true,
        "ssl": {
            "verify_peer": false,
            "verify_peer_name": false,
            "allow_self_signed": true,
            "SNI_enabled": true,
            "disable_compression": true,
            "security_level": 1
        }
    },
    "SMTP": {
        "host": "127.0.0.1",
        "port": 465,
        "type": 1,
        "timeout": 60,
        "shortLogin": false,
        "lowerLogin": true,
        "useAuth": true,
        "ssl": {
            "verify_peer": false,
            "verify_peer_name": false,
            "allow_self_signed": true,
            "SNI_enabled": true,
            "disable_compression": true,
            "security_level": 1
        }
    },
    "Sieve": {
        "host": "127.0.0.1",
        "port": 4190,
        "type": 0,
        "timeout": 10,
        "shortLogin": false,
        "lowerLogin": true,
        "enabled": false,
        "ssl": {
            "verify_peer": false,
            "verify_peer_name": false,
            "allow_self_signed": true,
            "SNI_enabled": true,
            "disable_compression": true,
            "security_level": 1
        }
    },
    "whiteList": ""
}
EOF
  chown www-data:www-data "$snap_dom_dir" "$snap_dom_dir/${domain}.json" 2>/dev/null || true
  chmod 700 "$snap_dom_dir" 2>/dev/null || true
  chmod 600 "$snap_dom_dir/${domain}.json" 2>/dev/null || true
}

# Terapkan relay SMTP global ke Stalwart (route Relay + outbound strategy).
# Provider contoh: Resend (smtp.resend.com:465, user "resend", pass API key),
# Brevo, Mailgun, SES, dll.
apply_stalwart_global_relay() {
  need_root
  need_cmd jq
  local provider="$1" host="$2" port="$3" implicit="$4" username="$5" password="$6"
  local creds admin_email admin_pass
  creds=$(mail_pick_admin_cred || true)
  if [ -z "$creds" ]; then
    die "Kredensial admin Stalwart tidak valid. Jalankan: sudo pdns-mgr mail admin-reset <domain>"
  fi
  admin_email=$(printf '%s' "$creds" | cut -f1)
  admin_pass=$(printf '%s' "$creds" | cut -f2)

  local route_json rid route_type out
  route_json=$(stalwart_query_json "$admin_email" "$admin_pass" query MtaRoute)
  rid=$(printf '%s' "$route_json" | jq -r '.[]? | select(.name=="relay") | .id' | head -n 1)
  route_type=$(printf '%s' "$route_json" | jq -r '.[]? | select(.name=="relay") | ."@type"' | head -n 1)
  if [ -n "$rid" ] && [ "$route_type" != "Relay" ]; then
    run_stalwart_cli "$admin_email" "$admin_pass" delete --ids "$rid" MtaRoute >/dev/null 2>&1 || true
    rid=""
  fi

  local payload_file payload_json
  payload_file=$(mktemp)
  chmod 600 "$payload_file"
  payload_json=$(jq -nc --arg host "$host" --argjson port "$port" --argjson implicit "$implicit" \
    --arg user "$username" --arg pass "$password" '
      {host: $host, port: $port, protocol: "smtp", implicitTls: $implicit}
      + (if $user != "" then {authUsername: $user, authSecret: {"@type": "Value", secret: $pass}} else {} end)
    ')

  if [ -n "$rid" ]; then
    printf '%s' "$payload_json" > "$payload_file"
    out=$(run_stalwart_cli "$admin_email" "$admin_pass" update MtaRoute "$rid" --file "$payload_file" 2>&1 || true)
  else
    printf '%s' "$(jq -c --arg name relay '. + {name: $name}' <<< "$payload_json")" > "$payload_file"
    out=$(run_stalwart_cli "$admin_email" "$admin_pass" create MtaRoute/Relay --file "$payload_file" 2>&1 || true)
  fi
  rm -f "$payload_file"
  if ! printf '%s' "$out" | grep -qiE '(Created|Updated) MtaRoute'; then
    die "Gagal membuat route relay: ${out:-<tanpa output>}"
  fi

  local strategy_json new_route applied
  strategy_json=$(run_stalwart_cli "$admin_email" "$admin_pass" get MtaOutboundStrategy --json 2>&1 || true)
  new_route=$(printf '%s' "$strategy_json" | jq -c --arg r "'relay'" '.route.else = $r | .route' 2>/dev/null || true)
  if [ -z "$new_route" ]; then
    die "Gagal membaca MtaOutboundStrategy."
  fi
  out=$(run_stalwart_cli "$admin_email" "$admin_pass" update MtaOutboundStrategy \
    --json "{\"route\":$new_route}" 2>&1 || true)
  if ! printf '%s' "$out" | grep -qi 'Updated MtaOutboundStrategy'; then
    die "Gagal mengaktifkan relay global: ${out:-<tanpa output>}"
  fi
  applied=$(run_stalwart_cli "$admin_email" "$admin_pass" get MtaOutboundStrategy --json 2>/dev/null \
    | jq -r '.route.else // empty' 2>/dev/null || true)
  if [ "$applied" != "'relay'" ]; then
    die "Relay route belum aktif pada MtaOutboundStrategy (nilai: ${applied:-kosong})."
  fi

  say "Merestart Stalwart agar strategy relay langsung aktif..."
  if ! systemctl restart stalwart; then
    die "Relay tersimpan tetapi Stalwart gagal direstart."
  fi
  if ! wait_stalwart_ready; then
    die "Relay tersimpan tetapi Stalwart belum siap setelah restart."
  fi

  mail_relay_cred_set "$provider" "$host" "$port" "$implicit" "$username" "$password"
  ok "Relay outbound global aktif: ${provider} (${host}:${port}, TLS=${implicit})"
}

# Relay khusus domain pengirim (sender_domain) di Stalwart.
apply_stalwart_domain_relay() {
  need_root
  need_cmd jq
  local domain provider host port implicit username password
  domain=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  provider="$2" host="$3" port="$4" implicit="$5" username="$6" password="$7"
  if ! [[ "$domain" =~ ^[A-Za-z0-9._-]+\.[A-Za-z]{2,}$ ]]; then
    die "Domain tidak valid: ${domain}"
  fi
  local route_name="relay_${domain//./_}"
  local creds admin_email admin_pass
  creds=$(mail_pick_admin_cred || true)
  if [ -z "$creds" ]; then
    die "Kredensial admin Stalwart tidak valid. Jalankan: sudo pdns-mgr mail admin-reset <domain>"
  fi
  admin_email=$(printf '%s' "$creds" | cut -f1)
  admin_pass=$(printf '%s' "$creds" | cut -f2)

  local route_json rid route_type out
  route_json=$(stalwart_query_json "$admin_email" "$admin_pass" query MtaRoute)
  rid=$(printf '%s' "$route_json" | jq -r --arg n "$route_name" '.[]? | select(.name==$n) | .id' | head -n 1)
  route_type=$(printf '%s' "$route_json" | jq -r --arg n "$route_name" '.[]? | select(.name==$n) | ."@type"' | head -n 1)
  if [ -n "$rid" ] && [ "$route_type" != "Relay" ]; then
    run_stalwart_cli "$admin_email" "$admin_pass" delete --ids "$rid" MtaRoute >/dev/null 2>&1 || true
    rid=""
  fi

  local payload_file payload_json
  payload_file=$(mktemp)
  chmod 600 "$payload_file"
  payload_json=$(jq -nc --arg host "$host" --argjson port "$port" --argjson implicit "$implicit" \
    --arg user "$username" --arg pass "$password" '
      {host: $host, port: $port, protocol: "smtp", implicitTls: $implicit}
      + (if $user != "" then {authUsername: $user, authSecret: {"@type": "Value", secret: $pass}} else {} end)
    ')
  if [ -n "$rid" ]; then
    printf '%s' "$payload_json" > "$payload_file"
    out=$(run_stalwart_cli "$admin_email" "$admin_pass" update MtaRoute "$rid" --file "$payload_file" 2>&1 || true)
  else
    printf '%s' "$(jq -c --arg name "$route_name" '. + {name: $name}' <<< "$payload_json")" > "$payload_file"
    out=$(run_stalwart_cli "$admin_email" "$admin_pass" create MtaRoute/Relay --file "$payload_file" 2>&1 || true)
  fi
  rm -f "$payload_file"
  if ! printf '%s' "$out" | grep -qiE '(Created|Updated) MtaRoute'; then
    die "Gagal membuat route relay domain: ${out:-<tanpa output>}"
  fi

  local strategy_json cond thenval new_route applied
  cond="sender_domain == '${domain}'"
  thenval="'${route_name}'"
  strategy_json=$(run_stalwart_cli "$admin_email" "$admin_pass" get MtaOutboundStrategy --json 2>&1 || true)
  new_route=$(printf '%s' "$strategy_json" | jq -c --arg cond "$cond" --arg then "$thenval" '
    .route as $r
    | ($r.match // {}) as $m
    | ($m | to_entries | map(.value) | map(select(.if != $cond))) as $branches
    | ($branches + [{"if": $cond, "then": $then}]) as $new
    | ($r | .match = ($new | to_entries | map({key: (.key | tostring), value: .value}) | from_entries))
  ' 2>/dev/null || true)
  if [ -z "$new_route" ]; then
    die "Gagal menyusun strategy relay per-domain."
  fi
  out=$(run_stalwart_cli "$admin_email" "$admin_pass" update MtaOutboundStrategy \
    --json "{\"route\":$new_route}" 2>&1 || true)
  if ! printf '%s' "$out" | grep -qi 'Updated MtaOutboundStrategy'; then
    die "Gagal mengaktifkan relay per-domain: ${out:-<tanpa output>}"
  fi
  applied=$(run_stalwart_cli "$admin_email" "$admin_pass" get MtaOutboundStrategy --json 2>/dev/null \
    | jq -r --arg cond "$cond" '.route.match | to_entries[]? | select(.value.if==$cond) | .value.then' 2>/dev/null | head -n 1 || true)
  if [ "$applied" != "$thenval" ]; then
    die "Branch relay per-domain belum aktif pada strategy."
  fi

  say "Merestart Stalwart agar relay domain aktif..."
  if ! systemctl restart stalwart; then
    die "Relay domain tersimpan tetapi Stalwart gagal direstart."
  fi
  wait_stalwart_ready || die "Stalwart belum siap setelah restart."

  mail_relay_domain_cred_set "$domain" "$provider" "$host" "$port" "$implicit" "$username" "$password" "$route_name"
  ok "Relay per-domain aktif: ${domain} -> ${host}:${port} (${provider})"
}

remove_stalwart_domain_relay() {
  need_root
  need_cmd jq
  local domain
  domain=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  if [ ! -f "$MAIL_RELAY_DOMAINS_CRED_FILE" ] || ! jq -e --arg d "$domain" 'has($d)' "$MAIL_RELAY_DOMAINS_CRED_FILE" >/dev/null 2>&1; then
    warn "Tidak ada relay per-domain untuk '${domain}'."
    return 1
  fi
  local route_name="relay_${domain//./_}"
  local creds admin_email admin_pass
  creds=$(mail_pick_admin_cred || true)
  if [ -z "$creds" ]; then
    die "Kredensial admin Stalwart tidak valid. Jalankan: sudo pdns-mgr mail admin-reset <domain>"
  fi
  admin_email=$(printf '%s' "$creds" | cut -f1)
  admin_pass=$(printf '%s' "$creds" | cut -f2)

  local strategy_json cond new_route out rid
  cond="sender_domain == '${domain}'"
  strategy_json=$(run_stalwart_cli "$admin_email" "$admin_pass" get MtaOutboundStrategy --json 2>&1 || true)
  new_route=$(printf '%s' "$strategy_json" | jq -c --arg cond "$cond" '
    .route as $r
    | ($r.match // {}) as $m
    | ($m | to_entries | map(.value) | map(select(.if != $cond))) as $branches
    | ($r | .match = ($branches | to_entries | map({key: (.key | tostring), value: .value}) | from_entries))
  ' 2>/dev/null || true)
  if [ -z "$new_route" ]; then
    die "Gagal menyusun strategy untuk menghapus relay domain."
  fi
  out=$(run_stalwart_cli "$admin_email" "$admin_pass" update MtaOutboundStrategy \
    --json "{\"route\":$new_route}" 2>&1 || true)
  if ! printf '%s' "$out" | grep -qi 'Updated MtaOutboundStrategy'; then
    die "Gagal menghapus branch relay domain: ${out:-<tanpa output>}"
  fi

  rid=$(stalwart_query_json "$admin_email" "$admin_pass" query MtaRoute --where "name=${route_name}" --fields id \
    | jq -r '.[0].id // empty' 2>/dev/null || true)
  if [ -n "$rid" ]; then
    run_stalwart_cli "$admin_email" "$admin_pass" delete --ids "$rid" MtaRoute >/dev/null 2>&1 || true
  fi

  mail_relay_domain_cred_remove "$domain"
  say "Merestart Stalwart..."
  if ! systemctl restart stalwart; then
    die "Gagal restart Stalwart."
  fi
  wait_stalwart_ready || warn "Stalwart belum siap setelah restart."
  ok "Relay per-domain '${domain}' dihapus."
}

# Request ke Resend API tanpa menaruh API key di argumen proses.
resend_api() {
  local method="$1" path="$2" data="${3:-}" hdr rc
  hdr=$(mktemp)
  chmod 600 "$hdr"
  printf 'Authorization: Bearer %s\nContent-Type: application/json\n' "$RESEND_API_KEY" > "$hdr"
  local -a args=(-sS -X "$method" "${RESEND_API_BASE}${path}" -H "@$hdr")
  [ -n "$data" ] && args+=(-d "$data")
  curl "${args[@]}"
  rc=$?
  rm -f "$hdr"
  return $rc
}

brevo_api() {
  local method="$1" path="$2" data="${3:-}" hdr rc
  hdr=$(mktemp)
  chmod 600 "$hdr"
  printf 'api-key: %s\nContent-Type: application/json\n' "$BREVO_API_KEY" > "$hdr"
  local -a args=(-sS -X "$method" "${BREVO_API_BASE}${path}" -H "@$hdr")
  [ -n "$data" ] && args+=(-d "$data")
  curl "${args[@]}"
  rc=$?
  rm -f "$hdr"
  return $rc
}

# Tulis record DNS dari Resend ke zone PowerDNS secara batch.
pdns_apply_resend_records() {
  local domain="$1" records_json="$2"
  local zone zone_fqdn payload res err_msg
  zone=$(find_matching_zone "$domain")
  zone_fqdn="${zone%.}."
  if ! cmd_zone_names 2>/dev/null | grep -qx "$zone"; then
    die "Zone '${zone}' belum ada di PowerDNS. Tambahkan zone dulu (menu 5 / pdns-mgr zone add)."
  fi

  say "Record DNS dari Resend untuk ${domain}:"
  printf '%s' "$records_json" | jq -r '.[] | "  • \(.record // "-") [\(.type)] \(.name) -> \(.value)"' 2>/dev/null | cut -c1-150 || true

  payload=$(printf '%s' "$records_json" | jq -c --arg domain "$domain" '
    def trail: if endswith(".") then . else . + "." end;
    def fqdn($d; $n):
      (if $n == "" or $n == "@" or $n == $d then $d
       elif ($n | endswith("." + $d)) then $n
       else "\($n).\($d)" end) | trail;
    def rcontent:
      if .type == "MX" then "\(.priority // 10) \(.value | trail)"
      elif .type == "CNAME" then (.value | trail)
      elif .type == "TXT" then (if (.value | startswith("\"")) then .value else "\"\(.value)\"" end)
      else .value end;
    {
      rrsets: (
        [ .[] | {
            name: fqdn($domain; .name),
            type: (.type | ascii_upcase),
            ttl: (if (.ttl | type) == "number" then .ttl else 300 end),
            content: rcontent
          }]
        | group_by([.name, .type])
        | map({
            name: .[0].name,
            type: .[0].type,
            ttl: .[0].ttl,
            changetype: "REPLACE",
            records: map({content: .content, disabled: false})
          })
      )
    }')
  if [ -z "$payload" ]; then
    die "Gagal menyusun payload DNS dari record Resend."
  fi

  res=$(pdns_api PATCH "/api/v1/servers/localhost/zones/${zone_fqdn}" "$payload")
  if [ -n "$res" ]; then
    err_msg=$(printf '%s' "$res" | jq -r '.error // .' 2>/dev/null || printf '%s' "$res")
    die "Gagal menulis record DNS Resend: ${err_msg}"
  fi
  ok "Record DNS Resend berhasil diterapkan ke zone ${zone}."
}

# Daftarkan/ambil domain Resend, tulis record DNS-nya, lalu minta verifikasi.
resend_domain_setup() {
  local domain="$1" list created id domain_json records status msg
  list=$(resend_api GET "/domains")
  id=$(printf '%s' "$list" | jq -r --arg d "$domain" '.data[]? | select(.name==$d) | .id' 2>/dev/null | head -n 1)

  if [ -z "$id" ]; then
    say "Mendaftarkan domain '${domain}' di Resend..."
    created=$(resend_api POST "/domains" "$(jq -nc --arg d "$domain" '{name:$d}')")
    id=$(printf '%s' "$created" | jq -r '.id // empty' 2>/dev/null || true)
    if [ -z "$id" ]; then
      list=$(resend_api GET "/domains")
      id=$(printf '%s' "$list" | jq -r --arg d "$domain" '.data[]? | select(.name==$d) | .id' 2>/dev/null | head -n 1)
    fi
    if [ -z "$id" ]; then
      msg=$(printf '%s' "$created" | jq -r '.message // .' 2>/dev/null || printf '%s' "$created")
      die "Gagal mendaftarkan domain di Resend: ${msg}"
    fi
  fi

  domain_json=$(resend_api GET "/domains/$id")
  status=$(printf '%s' "$domain_json" | jq -r '.status // "unknown"')
  records=$(printf '%s' "$domain_json" | jq -c '.records // []')
  if [ "$(printf '%s' "$records" | jq 'length' 2>/dev/null || echo 0)" -eq 0 ]; then
    msg=$(printf '%s' "$domain_json" | jq -r '.message // .' 2>/dev/null || printf '%s' "$domain_json")
    die "Resend tidak mengembalikan record DNS untuk ${domain}: ${msg}"
  fi

  say "Domain Resend : ${domain} (status: ${status})"
  pdns_apply_resend_records "$domain" "$records"

  say "Meminta verifikasi domain di Resend..."
  resend_api POST "/domains/$id/verify" "" >/dev/null || true
  domain_json=$(resend_api GET "/domains/$id")
  status=$(printf '%s' "$domain_json" | jq -r '.status // "unknown"')
  say "Status verifikasi Resend: ${status} (asynchronous; cek dashboard Resend)."
}

# Tulis record DNS Brevo ke PowerDNS. TXT di apex (brevo-code + SPF) digabung,
# dan DMARC lama tidak ditimpa.
pdns_apply_brevo_records() {
  local domain="$1" records_json="$2"
  local zone zone_fqdn zone_json
  zone=$(find_matching_zone "$domain")
  zone_fqdn="${zone%.}."
  if ! cmd_zone_names 2>/dev/null | grep -qx "$zone"; then
    die "Zone '${zone}' belum ada di PowerDNS. Tambahkan zone dulu (menu 5 / pdns-mgr zone add)."
  fi
  zone_json=$(pdns_api GET "/api/v1/servers/localhost/zones/${zone_fqdn}")
  if printf '%s' "$zone_json" | jq -e '.error' >/dev/null 2>&1; then
    die "Gagal membaca zone ${zone}: $(printf '%s' "$zone_json" | jq -r '.error // .')"
  fi

  say "Record DNS dari Brevo untuk ${domain}:"
  printf '%s' "$records_json" | jq -r '.[] | "  • \(.record // "-") [\(.type)] \(.name) -> \(.value)"' 2>/dev/null | cut -c1-160 || true

  local count i rec type raw_name name content existing ttl records_payload payload res err_msg
  local skipped_dmarc=false
  count=$(printf '%s' "$records_json" | jq 'length' 2>/dev/null || echo 0)
  if [ "$count" -gt 0 ]; then
    for i in $(seq 0 $((count - 1))); do
      rec=$(printf '%s' "$records_json" | jq -c ".[$i]")
      type=$(printf '%s' "$rec" | jq -r '.type // ""' | tr '[:lower:]' '[:upper:]')
      raw_name=$(printf '%s' "$rec" | jq -r '.name // ""')
      content=$(printf '%s' "$rec" | jq -r '.value // ""')
      [ -n "$type" ] && [ -n "$content" ] || continue

      raw_name="${raw_name%.}"
      if [ -z "$raw_name" ] || [ "$raw_name" = "@" ] || [ "$raw_name" = "$domain" ]; then
        name="${domain}."
      elif [[ "$raw_name" == *".${domain}" ]]; then
        name="${raw_name}."
      else
        name="${raw_name}.${domain}."
      fi

      if [ "$type" = "TXT" ]; then
        if [[ "$content" != \"* ]] || [[ "$content" != *\" ]]; then
          content="\"$content\""
        fi
      fi

      existing=$(printf '%s' "$zone_json" | jq -c --arg n "$name" --arg t "$type" \
        '.rrsets[]? | select(.name==$n and .type==$t)' 2>/dev/null | head -n 1)

      if [ "$type" = "TXT" ] && [ "$name" = "_dmarc.${domain}." ] && [ -n "$existing" ] && [ "$existing" != "null" ]; then
        warn "DMARC yang sudah ada untuk ${domain} dipertahankan; DMARC dari Brevo dilewati."
        skipped_dmarc=true
        continue
      fi

      if [ -n "$existing" ] && [ "$existing" != "null" ]; then
        ttl=$(printf '%s' "$existing" | jq -r '.ttl // 300')
        records_payload=$(printf '%s' "$existing" | jq -c --arg c "$content" \
          '[.records[] | .content] + [$c] | unique | map({content: ., disabled: false})')
      else
        ttl=300
        records_payload=$(jq -nc --arg c "$content" '[{content: $c, disabled: false}]')
      fi

      payload=$(jq -nc --arg n "$name" --arg t "$type" --argjson ttl "$ttl" --argjson recs "$records_payload" \
        '{rrsets: [{name: $n, type: $t, ttl: $ttl, changetype: "REPLACE", records: $recs}]}')
      res=$(pdns_api PATCH "/api/v1/servers/localhost/zones/${zone_fqdn}" "$payload")
      if [ -n "$res" ]; then
        err_msg=$(printf '%s' "$res" | jq -r '.error // .' 2>/dev/null || printf '%s' "$res")
        die "Gagal menulis record DNS Brevo (${name} ${type}): ${err_msg}"
      fi
    done
  fi

  ok "Record DNS Brevo berhasil diterapkan ke zone ${zone}."
  [ "$skipped_dmarc" = true ] && warn "DMARC lama dipertahankan; pastikan kebijakannya sesuai."
}

# Daftarkan/ambil domain Brevo, tulis DNS, lalu minta autentikasi.
brevo_domain_setup() {
  local domain="$1" list found domain_json records msg auth verified authenticated
  list=$(brevo_api GET "/senders/domains")
  found=$(printf '%s' "$list" | jq -r --arg d "$domain" '.domains[]? | select(.domain_name==$d) | .domain_name' 2>/dev/null | head -n 1)

  if [ -z "$found" ]; then
    say "Mendaftarkan domain '${domain}' di Brevo..."
    domain_json=$(brevo_api POST "/senders/domains" "$(jq -nc --arg d "$domain" '{name:$d}')")
    if ! printf '%s' "$domain_json" | jq -e '.dns_records' >/dev/null 2>&1; then
      domain_json=$(brevo_api GET "/senders/domains/${domain}")
    fi
  else
    domain_json=$(brevo_api GET "/senders/domains/${domain}")
  fi

  records=$(printf '%s' "$domain_json" | jq -c '
    (.dns_records // {}) as $r
    | (if ($r | type) == "array" then $r
       else ($r | to_entries | map(
         .key as $k | .value |
         {record: (if $k=="dkim_record" then "DKIM"
                   elif $k=="brevo_code" then "BrevoCode"
                   elif $k=="dmarc_record" then "DMARC"
                   else $k end),
          type: .type,
          name: (.host_name // ""),
          value: .value,
          status: (.status // false)}
       )) end)
    | map(select(.value != null and .value != ""))
  ' 2>/dev/null || true)

  if [ -z "$records" ] || [ "$(printf '%s' "$records" | jq 'length' 2>/dev/null || echo 0)" -eq 0 ]; then
    msg=$(printf '%s' "$domain_json" | jq -r '.message // .' 2>/dev/null || printf '%s' "$domain_json")
    die "Brevo tidak mengembalikan record DNS untuk ${domain}: ${msg}"
  fi

  say "Domain Brevo : ${domain}"
  pdns_apply_brevo_records "$domain" "$records"

  say "Meminta autentikasi domain di Brevo..."
  auth=$(brevo_api PUT "/senders/domains/${domain}/authenticate" "")
  msg=$(printf '%s' "$auth" | jq -r '.message // empty' 2>/dev/null || true)
  [ -n "$msg" ] && say "Brevo: ${msg}"

  domain_json=$(brevo_api GET "/senders/domains/${domain}")
  verified=$(printf '%s' "$domain_json" | jq -r '.verified // false')
  authenticated=$(printf '%s' "$domain_json" | jq -r '.authenticated // false')
  say "Status Brevo: verified=${verified}, authenticated=${authenticated} (asynchronous)."
}

cmd_mail_setup() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local domain="${1:-}"
  local use_ssl=true
  local relay_host=""
  local relay_port="587"
  local relay_user=""
  local relay_pass=""
  local per_domain_relay=false

  shift || true
  while [ $# -gt 0 ]; do
    case "$1" in
      --no-ssl) use_ssl=false; shift ;;
      --ssl) use_ssl=true; shift ;;
      --relay-host) relay_host="$2"; shift 2 ;;
      --relay-port) relay_port="$2"; shift 2 ;;
      --relay-user) relay_user="$2"; shift 2 ;;
      --relay-pass) relay_pass="$2"; shift 2 ;;
      --per-domain-relay) per_domain_relay=true; shift ;;
      *) shift ;;
    esac
  done

  if [ -z "$domain" ]; then
    say "Penggunaan: sudo pdns-mgr mail setup <domain> [opsi]"
    say "Opsi:"
    say "  --ssl / --no-ssl                     Aktifkan Let's Encrypt SSL (default: aktif)"
    say "  --relay-host <host>                  Host SMTP Relay (misal: smtp-relay.brevo.com)"
    say "  --relay-port <port>                  Port SMTP Relay (default: 587)"
    say "  --relay-user <username>              Username / API Key SMTP Relay"
    say "  --relay-pass <password>              Password / Secret Key SMTP Relay"
    say "  --per-domain-relay                   Gunakan relay khusus untuk domain ini (bukan global)"
    exit 1
  fi

  local clean_dom="${domain%.}"
  local local_nb
  local_nb=$(get_local_netbird_ip)
  [ -n "$local_nb" ] || die "IP Netbird lokal (wt0) tidak terdeteksi. Pastikan Netbird aktif: 'netbird status'"

  say "${C_CYAN}${C_BOLD}=== Menyiapkan Mail Server & Webmail Lokal untuk '${clean_dom}' ===${C_RESET}"

  # 1. Pastikan mail stack lokal siap (Stalwart & SnappyMail)
  say "${C_CYAN}[1/3] Menyiapkan Mail Stack Lokal (Storage di Harddisk PC Anda)...${C_RESET}"
  ensure_local_mail_stack
  ensure_stalwart_bootstrap "$clean_dom" "mail.${clean_dom}"

  # 2. Cari zone DNS yang cocok dan daftarkan record
  local zone_cand
  zone_cand=$(find_matching_zone "$clean_dom")
  say "${C_CYAN}[2/3] Membuat Standarisasi DNS Record di PowerDNS VPS (Zone: ${zone_cand})...${C_RESET}"

  say "  -> A record: mail.${clean_dom} -> VPS Public IP"
  cmd_record_add "$zone_cand" "mail.${clean_dom}" "A" auto "300" >/dev/null

  say "  -> A record: mx.${clean_dom} -> VPS Public IP (hostname gateway)"
  cmd_record_add "$zone_cand" "mx.${clean_dom}" "A" auto "300" >/dev/null

  say "  -> TXT SPF: mail.${clean_dom} -> \"v=spf1 a ip4:100.64.0.0/10 -all\""
  cmd_record_add "$zone_cand" "mail.${clean_dom}" "TXT" "\"v=spf1 a ip4:100.64.0.0/10 -all\"" "300" >/dev/null

  say "  -> TXT SPF: mx.${clean_dom} -> \"v=spf1 a ip4:100.64.0.0/10 -all\""
  cmd_record_add "$zone_cand" "mx.${clean_dom}" "TXT" "\"v=spf1 a ip4:100.64.0.0/10 -all\"" "300" >/dev/null

  say "  -> A record: webmail.${clean_dom} -> VPS Public IP"
  cmd_record_add "$zone_cand" "webmail.${clean_dom}" "A" auto "300" >/dev/null

  say "  -> A record: mailadmin.${clean_dom} -> VPS Public IP"
  cmd_record_add "$zone_cand" "mailadmin.${clean_dom}" "A" auto "300" >/dev/null

  say "  -> MX record: @ -> 10 mail.${clean_dom}."
  cmd_record_add "$zone_cand" "@" "MX" "10 mail.${clean_dom}." "300" >/dev/null

  say "  -> TXT SPF: @ -> \"v=spf1 mx a:mail.${clean_dom} ~all\""
  cmd_record_add "$zone_cand" "@" "TXT" "\"v=spf1 mx a:mail.${clean_dom} ~all\"" "300" >/dev/null

  say "  -> TXT DMARC: _dmarc -> \"v=DMARC1; p=none; sp=none; rua=mailto:admin@${clean_dom}\""
  cmd_record_add "$zone_cand" "_dmarc" "TXT" "\"v=DMARC1; p=none; sp=none; rua=mailto:admin@${clean_dom}\"" "300" >/dev/null

  ok "Semua DNS Record Mail (A, MX, SPF, DMARC) berhasil dibuat!"

  # 3. Siapkan payload untuk Edge Agent VPS
  say "${C_CYAN}[3/3] Mendaftarkan VPS MX Inbound Gateway (Queue) & Reverse Proxy ke Lokal...${C_RESET}"
  local relay_obj="null"
  if [ -n "$relay_host" ]; then
    local relay_scope="global"
    if [ "$per_domain_relay" = "true" ]; then
      relay_scope="$clean_dom"
    fi
    relay_obj=$(jq -n \
      --arg scope "$relay_scope" \
      --arg host "$relay_host" \
      --arg port "$relay_port" \
      --arg user "$relay_user" \
      --arg pass "$relay_pass" \
      '{
        domain: $scope,
        host: $host,
        port: ($port | tonumber),
        user: $user,
        pass: $pass
      }')
  fi

  local payload
  payload=$(jq -n \
    --arg dom "$clean_dom" \
    --arg target "$local_nb" \
    --argjson ssl "$use_ssl" \
    --argjson relay "$relay_obj" \
    '{
      domain: $dom,
      target_ip: $target,
      ssl: $ssl,
      relay: $relay
    }')

  local res
  res=$(agent_api POST "/api/v1/mail/setup-domain" "$payload")

  if [ "$(echo "$res" | jq -r '.success // empty' 2>/dev/null)" = "true" ] || echo "$res" | grep -qE '"success"[[:space:]]*:[[:space:]]*true'; then
    local admin_url webmail_url mail_host ssl_stat mail_ports_proxy fw_stat listener_stat smtp_starttls_stat
    admin_url=$(echo "$res" | jq -r '.admin_url // empty')
    webmail_url=$(echo "$res" | jq -r '.webmail_url // empty')
    mail_host=$(echo "$res" | jq -r '.mail_host // empty')
    ssl_stat=$(echo "$res" | jq -r '.ssl // "none"')
    mail_ports_proxy=$(echo "$res" | jq -r '.mail_ports_proxy // "not-configured"')
    fw_stat=$(echo "$res" | jq -r '.firewall // "unknown"')
    listener_stat=$(echo "$res" | jq -r '.listeners // {} | to_entries | map("\(.key)=\(if .value then "on" else "off" end)") | join(" ")')
    smtp_starttls_stat=$(echo "$res" | jq -r '.smtp_starttls // false')
    case "$mail_ports_proxy" in
      active-tls) mail_ports_proxy="Aktif + TLS Let's Encrypt (993/465/995)" ;;
      active-passthrough) mail_ports_proxy="Aktif (passthrough, sertifikat lokal self-signed)" ;;
      not-configured|skipped) mail_ports_proxy="Belum dikonfigurasi" ;;
    esac

    [ -n "$admin_url" ] || admin_url="https://mailadmin.${clean_dom}/admin/"
    [[ "$admin_url" == */admin/ ]] || admin_url="${admin_url%/}/admin/"
    [ -n "$webmail_url" ] || webmail_url="https://webmail.${clean_dom}"
    [ -n "$mail_host" ] || mail_host="mail.${clean_dom}"

    # Domain & akun administrator di Stalwart lokal
    local st_ready=false
    if ensure_stalwart_mail_domain "$clean_dom"; then
      st_ready=true
      ensure_stalwart_public_url "$clean_dom"
    else
      warn "Provisioning Stalwart lokal belum selesai. DNS & reverse proxy tetap aktif."
    fi

    # Prekonfigurasi domain di SnappyMail lokal (IMAPS 993 / SMTPS 465)
    ensure_snappymail_domain_config "$clean_dom"

    local st_admin_email st_admin_pass
    st_admin_email=$(mail_admin_cred_get "$clean_dom" email || true)
    st_admin_pass=$(mail_admin_cred_get "$clean_dom" password || true)

    say ""
    echo "================================================================================"
    say "${C_BOLD}${C_GREEN}✓ EMAIL SERVER & WEBMAIL (HYBRID LOCAL STORAGE) BERHASIL AKTIF!${C_RESET}"
    echo "================================================================================"
    say "  Domain Email        : ${C_BOLD}${clean_dom}${C_RESET}"
    say "  Lokasi Data Mailbox : ${C_BOLD}/mnt/f7d8a899-1e8d-4c44-afaf-2f7c69f2cce5/mail-data${C_RESET} (Lokal 382 GB)"
    say "  Webmail Klien       : ${C_BOLD}${C_CYAN}${webmail_url}${C_RESET} (SnappyMail)"
    say "  Panel Admin (WebUI) : ${C_BOLD}${C_CYAN}${admin_url}${C_RESET}"
    say "                        (root domain akan redirect ke /account; gunakan /admin/)"
    if [ "$st_ready" = "true" ] && [ -n "$st_admin_email" ]; then
      say "  Username Admin      : ${C_BOLD}${st_admin_email}${C_RESET}"
      say "  Password Admin      : ${C_BOLD}${st_admin_pass}${C_RESET}"
    elif [ -n "$st_admin_email" ]; then
      say "  Username Admin      : ${C_YELLOW}${st_admin_email} (kredensial tersimpan belum diverifikasi)${C_RESET}"
      say "  Password Admin      : ${C_YELLOW}Jalankan: sudo pdns-mgr mail admin-reset ${clean_dom}${C_RESET}"
    else
      say "  Username Admin      : ${C_YELLOW}Belum tersedia — jalankan mail setup ulang / mail admin-reset${C_RESET}"
    fi
    say "  File Kredensial     : ${C_BOLD}${MAIL_ADMIN_CRED_FILE}${C_RESET} (0600)"
    say "  Mail Server FQDN    : ${C_BOLD}${mail_host}${C_RESET}"
    say "  Status SSL          : ${ssl_stat}"
    say "  Proxy IMAP/SMTPS    : ${mail_ports_proxy}"
    if [ "$smtp_starttls_stat" = "true" ]; then
      say "  STARTTLS SMTP (25)  : Aktif (Let's Encrypt)"
    else
      say "  STARTTLS SMTP (25)  : Belum aktif"
    fi
    say "  Firewall VPS        : ${fw_stat}"
    say "  Listener VPS        : ${listener_stat:-tidak diketahui}"
    say "  VPS MX Queue Gateway: ${C_BOLD}${VPS_PUBLIC_IP}:25${C_RESET} -> Diteruskan ke PC Lokal (${local_nb}:25)"
    echo "--------------------------------------------------------------------------------"
    say "${C_BOLD}Ketahanan saat Mati Listrik (Zero Data Loss):${C_RESET}"
    say "  Jika PC lokal mati listrik, VPS otomatis menampung semua email masuk di antrean"
    say "  (spool queue) hingga 5 hari. Saat listrik nyala, email otomatis masuk ke harddisk."
    echo "--------------------------------------------------------------------------------"
    say "${C_BOLD}Parameter Aplikasi Email (Thunderbird, Outlook, Mobile, Laravel):${C_RESET}"
    say "  • IMAP (Terima)     : ${mail_host} Port 993 (SSL)"
    say "  • SMTP (Kirim)      : ${mail_host} Port 465 (SSL)"
    say "  • POP3              : ${mail_host} Port 995 (SSL)"
    say "  • Username Email    : alamat email lengkap (contoh: user@${clean_dom})"
    say "  • Catatan           : Port 143/587/110 belum diaktifkan; gunakan 993/465/995."
    echo "================================================================================"
    say ""

    if [ -n "$relay_host" ]; then
      local relay_implicit="false"
      case "$relay_port" in
        465|2465) relay_implicit="true" ;;
      esac
      if [ "$per_domain_relay" = "true" ]; then
        warn "Relay per-domain belum didukung untuk penerapan otomatis; menerapkan relay global."
      fi
      say "${C_CYAN}Menerapkan relay outbound global ${relay_host}:${relay_port}...${C_RESET}"
      apply_stalwart_global_relay "custom" "$relay_host" "$relay_port" "$relay_implicit" "$relay_user" "$relay_pass"
    fi
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    die "Gagal menyiapkan Mail Gateway di VPS: $err_msg"
  fi
}

cmd_mail_set_relay() {
  need_root
  need_cmd jq

  local scope="${1:-global}"
  local host="${2:-}"
  local port="${3:-587}"
  local user="${4:-}"
  local pass="${5:-}"

  if [ -z "$host" ]; then
    say "Penggunaan: sudo pdns-mgr mail set-relay <global|domain> <host> [port] [username] [password]"
    say "Contoh Global   : sudo pdns-mgr mail set-relay global smtp-relay.brevo.com 587 user pass"
    say "Contoh Per-Domain: sudo pdns-mgr mail set-relay domainanda.com smtp.resend.com 465 resend apikey"
    say "Resend        : sudo pdns-mgr mail relay-resend domainanda.com --per-domain"
    exit 1
  fi

  scope="${scope%.}"
  local implicit="false"
  case "$port" in
    465|2465) implicit="true" ;;
  esac

  if [ "$scope" != "global" ]; then
    say "Mengonfigurasi relay per-domain '${scope}' -> ${host}:${port}..."
    apply_stalwart_domain_relay "$scope" "custom" "$host" "$port" "$implicit" "$user" "$pass"
    say "Email outbound dari domain '${scope}' sekarang melalui ${host}:${port}."
    return 0
  fi

  say "Mengonfigurasi relay outbound global ${host}:${port}..."
  apply_stalwart_global_relay "custom" "$host" "$port" "$implicit" "$user" "$pass"
  say "Semua email outbound non-lokal sekarang diarahkan via ${host}:${port}."
}

cmd_mail_relay_resend() {
  need_root
  need_cmd jq

  local domain="" use_stdin=false dns_only=false global_mode=false api_key=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --stdin) use_stdin=true; shift ;;
      --dns-only) dns_only=true; shift ;;
      --global) global_mode=true; shift ;;
      --per-domain) global_mode=false; shift ;;
      --domain) domain="${2:-}"; shift 2 ;;
      -*) shift ;;
      *)
        if [ -z "$domain" ] && [[ "$1" == re_* ]]; then
          api_key="$1"
          warn "API key sebaiknya dikirim via --stdin agar tidak terlihat di proses."
        elif [ -z "$domain" ]; then
          domain="$1"
        else
          warn "Argumen diabaikan: $1"
        fi
        shift
        ;;
    esac
  done

  if [ -z "$domain" ]; then
    die "Domain wajib diisi. Resend diverifikasi per domain; jalankan: sudo pdns-mgr mail relay-resend <domain>"
  fi
  if ! [[ "$domain" =~ ^[A-Za-z0-9._-]+\.[A-Za-z]{2,}$ ]]; then
    die "Domain tidak valid: ${domain}"
  fi

  if [ -z "$api_key" ] && [ "$use_stdin" = true ]; then
    IFS= read -r api_key || true
  elif [ -z "$api_key" ] && [ -t 0 ]; then
    printf 'API Key Resend (input tersembunyi): '
    read -r -s api_key || true
    printf '\n'
  elif [ -z "$api_key" ] && [ "$use_stdin" = false ]; then
    IFS= read -r api_key || true
  fi

  if [ -z "$api_key" ]; then
    die "API key Resend kosong."
  fi

  need_cmd curl
  load_config
  local RESEND_API_KEY="$api_key"
  resend_domain_setup "$domain"
  if [ "$dns_only" = true ]; then
    ok "Record DNS Resend untuk '${domain}' selesai. Relay tidak diubah (--dns-only)."
    return 0
  fi

  if [ "$global_mode" = true ]; then
    apply_stalwart_global_relay "resend" "smtp.resend.com" 465 true "resend" "$api_key"
    say ""
    say "Relay global Resend aktif. Domain tanpa route per-domain akan memakai relay ini."
  else
    apply_stalwart_domain_relay "$domain" "resend" "smtp.resend.com" 465 true "resend" "$api_key"
    say ""
    say "Relay per-domain Resend aktif untuk ${domain}."
  fi
  say "Pastikan status domain di dashboard Resend menjadi 'verified'."
  say "Jika belum verified, email keluar akan ditolak sampai DNS terpropagasi."
}

cmd_mail_relay_brevo() {
  need_root
  need_cmd jq

  local domain="" use_stdin=false dns_only=false api_key="" smtp_user="" smtp_key=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --stdin) use_stdin=true; shift ;;
      --dns-only) dns_only=true; shift ;;
      --domain) domain="${2:-}"; shift 2 ;;
      -*) shift ;;
      *)
        if [ -z "$domain" ]; then
          domain="$1"
        else
          warn "Argumen diabaikan: $1"
        fi
        shift
        ;;
    esac
  done

  if [ -z "$domain" ]; then
    die "Domain wajib diisi. Jalankan: sudo pdns-mgr mail relay-brevo <domain>"
  fi
  if ! [[ "$domain" =~ ^[A-Za-z0-9._-]+\.[A-Za-z]{2,}$ ]]; then
    die "Domain tidak valid: ${domain}"
  fi

  if [ "$use_stdin" = true ]; then
    IFS= read -r api_key || true
    IFS= read -r smtp_user || true
    IFS= read -r smtp_key || true
  else
    api_key="${BREVO_API_KEY:-}"
    smtp_user="${BREVO_SMTP_USER:-}"
    smtp_key="${BREVO_SMTP_KEY:-}"
    if [ -z "$api_key" ] && [ -t 0 ]; then
      printf 'API Key Brevo (input tersembunyi): '
      read -r -s api_key || true
      printf '\n'
    fi
    if [ -z "$smtp_user" ] && [ -t 0 ]; then
      printf 'SMTP Login Brevo (contoh: 12345@smtp-brevo.com): '
      read -r smtp_user || true
    fi
    if [ -z "$smtp_key" ] && [ -t 0 ]; then
      printf 'SMTP Key Brevo (input tersembunyi): '
      read -r -s smtp_key || true
      printf '\n'
    fi
  fi

  [ -n "$api_key" ] || die "API key Brevo kosong."
  [ -n "$smtp_user" ] || die "SMTP login Brevo kosong."
  [ -n "$smtp_key" ] || die "SMTP key Brevo kosong."

  need_cmd curl
  load_config
  local BREVO_API_KEY="$api_key"
  brevo_domain_setup "$domain"
  if [ "$dns_only" = true ]; then
    ok "Record DNS Brevo untuk '${domain}' selesai. Relay tidak diubah (--dns-only)."
    return 0
  fi

  apply_stalwart_domain_relay "$domain" "brevo" "smtp-relay.brevo.com" 587 false "$smtp_user" "$smtp_key"
  say ""
  say "Relay per-domain Brevo aktif untuk ${domain} (smtp-relay.brevo.com:587)."
  say "Pastikan status domain di dashboard Brevo menjadi authenticated."
}

cmd_mail_relay_disable() {
  need_root
  need_cmd jq
  local target="${1:-global}"
  target="${target%.}"
  if [ "$target" != "global" ]; then
    remove_stalwart_domain_relay "$target"
    return 0
  fi

  local creds admin_email admin_pass
  creds=$(mail_pick_admin_cred || true)
  if [ -z "$creds" ]; then
    die "Kredensial admin Stalwart tidak valid. Jalankan: sudo pdns-mgr mail admin-reset <domain>"
  fi
  admin_email=$(printf '%s' "$creds" | cut -f1)
  admin_pass=$(printf '%s' "$creds" | cut -f2)

  local strategy_json new_route out rid
  strategy_json=$(run_stalwart_cli "$admin_email" "$admin_pass" get MtaOutboundStrategy --json 2>/dev/null || true)
  new_route=$(printf '%s' "$strategy_json" | jq -c --arg r "'mx'" '.route.else = $r | .route' 2>/dev/null || true)
  if [ -z "$new_route" ]; then
    die "Gagal membaca MtaOutboundStrategy."
  fi
  out=$(run_stalwart_cli "$admin_email" "$admin_pass" update MtaOutboundStrategy \
    --json "{\"route\":$new_route}" 2>&1 || true)
  if ! printf '%s' "$out" | grep -qi 'Updated MtaOutboundStrategy'; then
    die "Gagal menonaktifkan relay: ${out:-<tanpa output>}"
  fi

  rid=$(stalwart_query_json "$admin_email" "$admin_pass" query MtaRoute --where name=relay --fields id \
    | jq -r '.[0].id // empty' 2>/dev/null || true)
  if [ -n "$rid" ]; then
    run_stalwart_cli "$admin_email" "$admin_pass" delete --ids "$rid" MtaRoute >/dev/null 2>&1 || true
  fi

  rm -f "$MAIL_RELAY_CRED_FILE"
  say "Merestart Stalwart agar pengiriman langsung (mx) aktif..."
  if ! systemctl restart stalwart; then
    die "Gagal restart Stalwart."
  fi
  wait_stalwart_ready || warn "Stalwart belum siap setelah restart."
  ok "Relay dinonaktifkan. Email keluar kembali dikirim langsung ke MX tujuan."
}

cmd_mail_add_account() {
  need_root
  need_cmd jq
  load_config

  local email="${1:-}"
  local password="${2:-}"

  if [ -z "$email" ] || [ -z "$password" ]; then
    say "Penggunaan: sudo pdns-mgr mail add-account <email> <password>"
    say "Contoh    : sudo pdns-mgr mail add-account info@domainanda.com Rahasia123!"
    exit 1
  fi

  if [[ "$email" != *@* ]]; then
    die "Format email tidak valid: $email"
  fi

  local user_part="${email%%@*}"
  local domain_part="${email#*@}"
  domain_part="${domain_part%.}"

  if [ -z "$(mail_admin_cred_get "$domain_part" email || true)" ]; then
    die "Kredensial admin '${domain_part}' belum tersedia. Jalankan: sudo pdns-mgr mail admin-reset ${domain_part} (atau sudo pdns-mgr mail setup ${domain_part}) terlebih dahulu."
  fi

  if ! ensure_stalwart_mail_domain "$domain_part"; then
    die "Stalwart belum siap untuk domain '${domain_part}'."
  fi

  local admin_email admin_pass did acc_out acc_id out cred_json action
  admin_email=$(mail_admin_cred_get "$domain_part" email || true)
  admin_pass=$(mail_admin_cred_get "$domain_part" password || true)
  if [ -z "$admin_email" ] || [ -z "$admin_pass" ]; then
    die "Kredensial admin '${domain_part}' tidak ditemukan. Jalankan: sudo pdns-mgr mail admin-reset ${domain_part}"
  fi

  did=$(stalwart_find_domain_id "$admin_email" "$admin_pass" "$domain_part")
  [ -n "$did" ] || die "Domain '${domain_part}' tidak ditemukan di Stalwart."

  acc_out=$(stalwart_query_json "$admin_email" "$admin_pass" query Account --where "domainId=${did}" \
    --fields id,name,domainId)
  acc_id=$(printf '%s' "$acc_out" | jq -r --arg n "$user_part" \
    '.[] | select(.name==$n) | .id' 2>/dev/null | head -n 1)
  if [ -z "$acc_id" ]; then
    acc_out=$(stalwart_query_json "$admin_email" "$admin_pass" query Account \
      --fields id,name,domainId)
    acc_id=$(printf '%s' "$acc_out" | jq -r --arg n "$user_part" --arg did "$did" \
      '.[] | select(.name==$n and .domainId==$did) | .id' 2>/dev/null | head -n 1)
  fi

  cred_json=$(jq -nc --arg p "$password" '{"0":{"@type":"Password","secret":$p}}')
  if [ -n "$acc_id" ]; then
    say "Akun '${email}' sudah ada, memperbarui password..."
    out=$(run_stalwart_cli "$admin_email" "$admin_pass" update Account "$acc_id" \
      --field "credentials=${cred_json}" 2>&1 || true)
    action="diperbarui"
  else
    say "Mendaftarkan akun email '${email}' di Stalwart lokal..."
    out=$(run_stalwart_cli "$admin_email" "$admin_pass" create Account/User \
      --field "name=${user_part}" \
      --field "domainId=${did}" \
      --field "credentials=${cred_json}" 2>&1 || true)
    action="dibuat"
  fi

  if ! printf '%s' "$out" | grep -qiE '(Created|Updated) Account'; then
    if printf '%s' "$out" | grep -qiE 'too weak|password.*(weak|strength)|invalidProperties'; then
      err "Password ditolak oleh kebijakan Stalwart: ${out:-<tanpa output>}"
      err "Gunakan password minimal 8 karakter dan hindari password umum."
      return 2
    fi
    err "Gagal ${action} akun '${email}': ${out:-<tanpa output>}"
    return 1
  fi

  ensure_snappymail_domain_config "$domain_part"

  ok "Akun email '${email}' berhasil ${action} di storage lokal!"
  say ""
  say "  Email         : ${C_BOLD}${email}${C_RESET}"
  say "  IMAP Server   : ${C_BOLD}mail.${domain_part}${C_RESET} (993 SSL)"
  say "  SMTP Server   : ${C_BOLD}mail.${domain_part}${C_RESET} (465 SSL)"
  say "  Webmail Login : ${C_BOLD}${C_CYAN}https://webmail.${domain_part}${C_RESET}"
  say "  Panel Admin   : ${C_BOLD}${C_CYAN}https://mailadmin.${domain_part}/admin/${C_RESET}"
  say ""
}

cmd_mail_admin_show() {
  need_root
  need_cmd jq
  if [ ! -f "$MAIL_ADMIN_CRED_FILE" ]; then
    warn "Belum ada kredensial admin tersimpan di ${MAIL_ADMIN_CRED_FILE}."
    say "Jalankan: ${C_BOLD}sudo pdns-mgr mail setup <domain>${C_RESET} atau ${C_BOLD}sudo pdns-mgr mail admin-reset <domain>${C_RESET}"
    return 1
  fi
  echo "================================================================================"
  say "${C_BOLD}KREDENSIAL ADMIN STALWART (PANEL ADMIN MAIL)${C_RESET}"
  echo "================================================================================"
  jq -r 'to_entries[] | .key as $d | .value |
    "  Domain      : \($d)\n  URL Admin   : https://mailadmin.\($d)/admin/\n  Username    : \(.email)\n  Password    : \(.password)\n  Diperbarui : \(.updatedAt // "-")\n"' "$MAIL_ADMIN_CRED_FILE"
  say "  File        : ${MAIL_ADMIN_CRED_FILE} (0600)"
  echo "================================================================================"
}

cmd_mail_admin_reset() {
  need_root
  need_cmd jq
  local domain="${1:-}"
  if [ -z "$domain" ]; then
    say "Penggunaan: sudo pdns-mgr mail admin-reset <domain>"
    say "Contoh    : sudo pdns-mgr mail admin-reset domainanda.com"
    exit 1
  fi
  domain="${domain%.}"
  stalwart_reset_admin "$domain"
  local email pass
  email=$(mail_admin_cred_get "$domain" email || true)
  pass=$(mail_admin_cred_get "$domain" password || true)
  say ""
  echo "================================================================================"
  say "${C_BOLD}${C_GREEN}✓ AKSES ADMIN STALWART BERHASIL DIRESET${C_RESET}"
  echo "================================================================================"
  say "  URL Admin   : ${C_BOLD}${C_CYAN}https://mailadmin.${domain}/admin/${C_RESET}"
  say "  Username    : ${C_BOLD}${email}${C_RESET}"
  say "  Password    : ${C_BOLD}${pass}${C_RESET}"
  say "  File        : ${MAIL_ADMIN_CRED_FILE} (0600)"
  echo "================================================================================"
}

cmd_mail_accounts() {
  need_root
  need_cmd jq
  local domain_filter="${1:-}"
  domain_filter="${domain_filter%.}"

  if [ ! -f "$MAIL_ADMIN_CRED_FILE" ]; then
    warn "Belum ada kredensial admin tersimpan di ${MAIL_ADMIN_CRED_FILE}."
    say "Jalankan: ${C_BOLD}sudo pdns-mgr mail admin-reset <domain>${C_RESET} atau ${C_BOLD}sudo pdns-mgr mail setup <domain>${C_RESET} terlebih dahulu."
    return 1
  fi

  local admin_email="" admin_pass="" cand_email cand_pass i
  local -a cred_emails=() cred_passwords=()
  if [ -n "$domain_filter" ] && [ -n "$(mail_admin_cred_get "$domain_filter" email || true)" ]; then
    cred_emails+=("$(mail_admin_cred_get "$domain_filter" email || true)")
    cred_passwords+=("$(mail_admin_cred_get "$domain_filter" password || true)")
  fi
  while IFS=$'\t' read -r cand_email cand_pass; do
    [ -n "$cand_email" ] || continue
    if [ "${#cred_emails[@]}" -gt 0 ] && [ "$cand_email" = "${cred_emails[0]}" ]; then
      continue
    fi
    cred_emails+=("$cand_email")
    cred_passwords+=("$cand_pass")
  done < <(jq -r 'to_entries[] | [.value.email, .value.password] | @tsv' "$MAIL_ADMIN_CRED_FILE" 2>/dev/null || true)

  if [ "${#cred_emails[@]}" -gt 0 ]; then
    for i in "${!cred_emails[@]}"; do
      if run_stalwart_cli "${cred_emails[$i]}" "${cred_passwords[$i]}" query Account --fields id --json >/dev/null 2>&1; then
        admin_email="${cred_emails[$i]}"
        admin_pass="${cred_passwords[$i]}"
        break
      fi
    done
  fi
  if [ -z "$admin_email" ] || [ -z "$admin_pass" ]; then
    die "Kredensial admin tersimpan tidak valid. Jalankan: sudo pdns-mgr mail admin-reset ${domain_filter:-<domain>}"
  fi

  local accounts_json domains_json
  accounts_json=$(stalwart_query_json "$admin_email" "$admin_pass" query Account \
    --fields id,name,emailAddress,domainId,roles)
  domains_json=$(stalwart_query_json "$admin_email" "$admin_pass" query Domain \
    --fields id,name,enable)
  [ -n "$accounts_json" ] || accounts_json="[]"
  [ -n "$domains_json" ] || domains_json="[]"

  echo "================================================================================"
  say "${C_BOLD}AKUN EMAIL TERDAFTAR — STALWART LOKAL${C_RESET}"
  echo "================================================================================"
  jq -r -n --argjson accounts "$accounts_json" --argjson domains "$domains_json" --arg filter "$domain_filter" '
    def list: if type == "array" then . elif . == null then [] else [.] end;
    ($domains | list) as $d
    | ($accounts | list) as $a
    | ($d | map(. as $dom | {
          name: $dom.name,
          enable: ($dom.enable // true),
          accounts: ($a | map(select(.domainId == $dom.id)))
        }))
    | (if $filter != "" then map(select(.name == $filter)) else . end)
    | sort_by(.name)
    | if length == 0 then
        "Tidak ada domain yang cocok dengan filter: \($filter)"
      else
        (.[] as $dom |
          "Domain: \($dom.name)  (\($dom.accounts | length) akun)" +
          (if $dom.enable == false then "  [DISABLED]" else "" end),
          "  Admin : https://mailadmin.\($dom.name)/admin/",
          (if ($dom.accounts | length) == 0 then
             "  (belum ada akun)"
           else
             ($dom.accounts | sort_by(.emailAddress)[] |
               "  • \(.emailAddress // (.name + "@" + $dom.name))" +
               (if (.roles."@type" // "") == "Admin" then "  [Administrator]" else "" end))
           end),
          ""),
        "Total: \(map(.accounts | length) | add // 0) akun di \(length) domain"
      end
  '
  echo "================================================================================"
}

cmd_mail_open_ports() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  say "Membuka dan memverifikasi port mail di firewall VPS..."
  local res
  res=$(agent_api POST "/api/v1/mail/open-ports" "{}")
  if [ "$(echo "$res" | jq -r '.success // empty' 2>/dev/null)" = "true" ]; then
    local fw listeners
    fw=$(echo "$res" | jq -r '.firewall // "unknown"')
    listeners=$(echo "$res" | jq -r '.listeners // {} | to_entries | map("\(.key)=\(if .value then "on" else "off" end)") | join(" ")')
    ok "Firewall VPS : ${fw}"
    say "Listener VPS : ${listeners:-tidak diketahui}"
    say ""
    say "Jika port 25/465/587/993/143/995/110 masih tertutup dari internet,"
    say "buka juga port tersebut di firewall/security group panel penyedia VPS."
  else
    local err_msg
    err_msg=$(echo "$res" | jq -r '.error // .')
    die "Gagal membuka port mail di VPS: ${err_msg}"
  fi
}

cmd_mail_queue() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  say "Memeriksa antrean email (mail queue) di VPS Gateway..."
  local res
  res=$(agent_api GET "/api/v1/mail/queue")
  local count out
  count=$(echo "$res" | jq -r '.queue_count // 0')
  out=$(echo "$res" | jq -r '.output // "Mail queue is empty"')

  echo "================================================================================"
  say "${C_BOLD}STATUS ANTREAN EMAIL VPS (STORE-AND-FORWARD QUEUE)${C_RESET}"
  echo "================================================================================"
  say "Jumlah Email Tertahan di Antrean VPS: ${C_BOLD}${count}${C_RESET}"
  say ""
  say "$out"
  echo "================================================================================"
  if [ "$count" -gt 0 ]; then
    say "Tips: Jalankan 'sudo pdns-mgr mail flush-queue' untuk segera mengirimkan email tertahan ke PC lokal."
  fi
}

cmd_mail_flush_queue() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  say "Memerintahkan VPS untuk segera mengirimkan (flush) semua antrean email ke PC lokal..."
  local res
  res=$(agent_api POST "/api/v1/mail/flush-queue" "{}")
  local msg
  msg=$(echo "$res" | jq -r '.message // "Flush command sent."')
  ok "$msg"
}

cmd_mail_status() {
  need_root
  need_cmd curl
  need_cmd jq
  load_config

  local local_st_active=false
  systemctl is-active stalwart >/dev/null 2>&1 && local_st_active=true
  local local_sn_installed=false
  [ -f /var/www/snappymail/index.php ] && local_sn_installed=true

  local free_space
  free_space=$(df -h /mnt/f7d8a899-1e8d-4c44-afaf-2f7c69f2cce5 2>/dev/null | awk 'NR==2 {print $4 " (Tersedia dari " $2 ")"}')
  [ -n "$free_space" ] || free_space=$(df -h / | awk 'NR==2 {print $4}')

  local res
  res=$(agent_api GET "/api/v1/mail/status" 2>/dev/null || echo "{}")
  local vps_pf_active vps_q_empty vps_q_summary vps_mail_ports vps_mail_tls vps_mail_listening vps_firewall vps_listeners vps_smtp_starttls
  vps_pf_active=$(echo "$res" | jq -r '.postfix_active // false')
  vps_q_empty=$(echo "$res" | jq -r '.queue_empty // true')
  vps_q_summary=$(echo "$res" | jq -r '.queue_summary // "Empty"')
  vps_mail_ports=$(echo "$res" | jq -r '.mail_ports_proxy // false')
  vps_mail_tls=$(echo "$res" | jq -r '.mail_ports_tls // false')
  vps_mail_listening=$(echo "$res" | jq -r '.mail_ports_listening // "unknown"')
  vps_firewall=$(echo "$res" | jq -r '.firewall // "unknown"')
  vps_listeners=$(echo "$res" | jq -r '.listeners // {} | to_entries | map("\(.key)=\(if .value then "on" else "off" end)") | join(" ")')
  vps_smtp_starttls=$(echo "$res" | jq -r '.smtp_starttls // false')

  echo "================================================================================"
  say "${C_BOLD}STATUS MAIL SERVER LOKAL & VPS EDGE GATEWAY${C_RESET}"
  echo "================================================================================"
  say "${C_BOLD}[MESIN LOKAL DIETPI (Storage Utama)]${C_RESET}"
  if [ "$local_st_active" = "true" ]; then
    say "  • Stalwart Mail Server   : ${C_GREEN}Active / Running (Port 8080/Admin, 25/SMTP, 993/IMAP)${C_RESET}"
  else
    say "  • Stalwart Mail Server   : ${C_RED}Inactive / Not Running${C_RESET}"
  fi
  if [ "$local_sn_installed" = "true" ]; then
    say "  • SnappyMail Web Client  : ${C_GREEN}Installed (/var/www/snappymail)${C_RESET}"
  else
    say "  • SnappyMail Web Client  : ${C_YELLOW}Not Installed${C_RESET}"
  fi
  if [ ! -f "$STALWART_CONFIG_FILE" ]; then
    say "  • Setup Stalwart         : ${C_YELLOW}Bootstrap (belum dikonfigurasi)${C_RESET}"
  else
    say "  • Setup Stalwart         : ${C_GREEN}Configured (${STALWART_CONFIG_FILE})${C_RESET}"
  fi
  if [ -f "$MAIL_ADMIN_CRED_FILE" ]; then
    say "  • Kredensial Admin       : ${C_GREEN}Tersimpan di ${MAIL_ADMIN_CRED_FILE} (0600)${C_RESET}"
    say "  • URL Panel Admin        : ${C_CYAN}https://mailadmin.<domain>/admin/${C_RESET}"
  else
    say "  • Kredensial Admin       : ${C_YELLOW}Belum ada. Jalankan: pdns-mgr mail admin-reset <domain>${C_RESET}"
  fi
  local relay_state="tidak diketahui" cred_pair r_email r_pass r_strategy r_else r_info relay_provider
  cred_pair=$(mail_pick_admin_cred || true)
  if [ -n "$cred_pair" ]; then
    r_email=$(printf '%s' "$cred_pair" | cut -f1)
    r_pass=$(printf '%s' "$cred_pair" | cut -f2)
    r_strategy=$(run_stalwart_cli "$r_email" "$r_pass" get MtaOutboundStrategy --json 2>/dev/null || true)
    r_else=$(printf '%s' "$r_strategy" | jq -r '.route.else // empty' 2>/dev/null || true)
    if [ "$r_else" = "'relay'" ]; then
      r_info=$(stalwart_query_json "$r_email" "$r_pass" query MtaRoute --fields name,host,port \
        | jq -r '.[]? | select(.name=="relay") | "\(.host):\(.port)"' 2>/dev/null | head -n 1)
      relay_provider=$(jq -r '.provider // empty' "$MAIL_RELAY_CRED_FILE" 2>/dev/null || true)
      relay_state="relay -> ${r_info:-?}"
      [ -n "$relay_provider" ] && relay_state="${relay_state} (${relay_provider})"
    elif [ -n "$r_else" ]; then
      relay_state="direct (mx)"
    fi
  fi
  say "  • Relay Outbound         : ${relay_state}"
  if [ -f "$MAIL_RELAY_DOMAINS_CRED_FILE" ]; then
    local relay_domains
    relay_domains=$(jq -r 'to_entries[] | "\(.key) -> \(.value.host):\(.value.port) (\(.value.provider))"' \
      "$MAIL_RELAY_DOMAINS_CRED_FILE" 2>/dev/null || true)
    if [ -n "$relay_domains" ]; then
      say "  • Relay Per-Domain       :"
      while IFS= read -r relay_line; do
        [ -n "$relay_line" ] && say "      - ${relay_line}"
      done <<< "$relay_domains"
    else
      say "  • Relay Per-Domain       : belum ada"
    fi
  else
    say "  • Relay Per-Domain       : belum ada"
  fi
  say "  • Kapasitas Harddisk Mail: ${C_BOLD}${C_CYAN}${free_space}${C_RESET}"
  echo "--------------------------------------------------------------------------------"
  say "${C_BOLD}[VPS CLOUD GATEWAY (Penampung Antrean saat Mati Listrik)]${C_RESET}"
  if [ "$vps_pf_active" = "true" ]; then
    say "  • Postfix Inbound Gateway: ${C_GREEN}Active / Listening Port 25${C_RESET}"
  else
    say "  • Postfix Inbound Gateway: ${C_YELLOW}Checking / Standby${C_RESET}"
  fi
  if [ "$vps_mail_ports" = "true" ]; then
    if [ "$vps_mail_tls" = "true" ]; then
      if [ "$vps_mail_listening" = "false" ]; then
        say "  • Proxy IMAP/SMTPS       : ${C_YELLOW}TLS siap, tetapi port belum listen${C_RESET}"
      else
        say "  • Proxy IMAP/SMTPS       : ${C_GREEN}Active + TLS Let's Encrypt (993/465/995)${C_RESET}"
      fi
    else
      say "  • Proxy IMAP/SMTPS       : ${C_GREEN}Active (passthrough; sertifikat lokal self-signed)${C_RESET}"
    fi
  else
    say "  • Proxy IMAP/SMTPS       : ${C_YELLOW}Belum aktif — redeploy VPS lalu mail setup lagi${C_RESET}"
  fi
  say "  • Firewall VPS           : ${vps_firewall}"
  say "  • Listener VPS           : ${vps_listeners:-tidak diketahui}"
  if [ "$vps_smtp_starttls" = "true" ]; then
    say "  • STARTTLS SMTP (25)     : ${C_GREEN}Active + TLS Let's Encrypt${C_RESET}"
  else
    say "  • STARTTLS SMTP (25)     : ${C_YELLOW}Belum aktif${C_RESET}"
  fi
  if [ "$vps_q_empty" = "true" ]; then
    say "  • Status Antrean Spool   : ${C_GREEN}0 Email (Bersih / Terkirim semua ke PC lokal)${C_RESET}"
  else
    say "  • Status Antrean Spool   : ${C_YELLOW}${vps_q_summary}${C_RESET}"
  fi
  echo "--------------------------------------------------------------------------------"
  say "${C_BOLD}Domain Terdaftar di Edge Gateway:${C_RESET}"
  local dom_count
  dom_count=$(echo "$res" | jq -r '.domains // [] | length')
  if [ "$dom_count" -eq 0 ]; then
    say "  (Belum ada domain email yang dikonfigurasi)"
  else
    echo "$res" | jq -r '.domains[]?' | while read -r d; do
      printf "  • %-30s -> Webmail: https://webmail.%-25s Admin: https://mailadmin.%s/admin/\n" "$d" "$d" "$d"
    done
  fi
  echo "================================================================================"
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
  say "${C_BOLD}Mail Server & Webmail (Hybrid Local Storage + VPS MX Queue):${C_RESET}"
  say "  mail setup <domain> [opsi...]                          Setup DNS + Stalwart & SnappyMail lokal + VPS relay"
  say "  mail set-relay <global|domain> <host> [port] [usr] [pwd] Relay SMTP global atau per-domain (Brevo, Mailgun, SES, dll)"
  say "  mail relay-resend <domain> [--stdin]                   Resend per-domain + record DNS otomatis"
  say "  mail relay-brevo <domain> [--stdin]                    Brevo per-domain + record DNS otomatis"
  say "  mail relay-disable [domain]                            Nonaktifkan relay global atau per-domain"
  say "  mail add-account <email> <password>                    Tambah/ubah akun email klien di storage lokal"
  say "  mail accounts [domain]                                 Daftar akun email per domain di Stalwart lokal"
  say "  mail open-ports                                        Buka/verifikasi port mail di firewall VPS"
  say "  mail admin                                             Lihat kredensial admin Stalwart (email + password)"
  say "  mail admin-reset <domain>                              Reset password admin Stalwart lewat recovery mode"
  say "  mail queue                                             Cek antrean email di VPS (saat PC mati listrik)"
  say "  mail flush-queue                                       Kirim segera email tertahan di VPS ke PC lokal"
  say "  mail status                                            Cek status Stalwart lokal, disk, & VPS gateway"
  say ""
  say "${C_DIM}Catatan: panel admin Stalwart ada di https://mailadmin.<domain>/admin/ (bukan root domain).${C_RESET}"
  say ""
}

# Dispatcher Utama
case "${1:-help}" in
  deploy-vps|install-vps|update-vps) shift; cmd_deploy_vps "$@" ;;
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
  mail)
    shift
    sub="${1:-status}"
    shift || true
    case "$sub" in
      setup) cmd_mail_setup "$@" ;;
      set-relay|relay) cmd_mail_set_relay "$@" ;;
      relay-resend|resend) cmd_mail_relay_resend "$@" ;;
      relay-brevo|brevo) cmd_mail_relay_brevo "$@" ;;
      relay-disable|relay-off) cmd_mail_relay_disable "$@" ;;
      add-account|account|user) cmd_mail_add_account "$@" ;;
      accounts|list-accounts|users) cmd_mail_accounts "$@" ;;
      open-ports|ports|firewall) cmd_mail_open_ports "$@" ;;
      admin|admin-show|credentials) cmd_mail_admin_show "$@" ;;
      admin-reset|reset-admin) cmd_mail_admin_reset "$@" ;;
      queue|mailq) cmd_mail_queue "$@" ;;
      flush-queue|flush) cmd_mail_flush_queue "$@" ;;
      status|info) cmd_mail_status "$@" ;;
      *) die "Subperintah mail tidak dikenal: $sub. Pilihan: setup, set-relay, relay-resend, relay-brevo, relay-disable, add-account, accounts, open-ports, admin, admin-reset, queue, flush-queue, status" ;;
    esac
    ;;
  -h|--help|help) cmd_help ;;
  *) die "Perintah tidak dikenal: $1. Jalankan 'pdns-mgr help' untuk melihat opsi." ;;
esac
