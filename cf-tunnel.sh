#!/bin/bash
# cf-tunnel: manajemen Cloudflare Tunnel multi-akun untuk wdev.
# Dipasang oleh setup_dev.sh; state disimpan per profil di /etc/wdev/cloudflare.
set -uo pipefail

WDEV_ETC="${WDEV_ETC:-/etc/wdev}"
CF_ETC="${WDEV_CF_ETC:-$WDEV_ETC/cloudflare}"
CF_ACCOUNTS="$CF_ETC/accounts"
CF_EXPOSURES="${WDEV_EXPOSURES:-$WDEV_ETC/exposures}"
CF_STATE="${WDEV_CLOUDFLARED_STATE:-/etc/cloudflared}"
SYSTEMD_DIR="${WDEV_SYSTEMD_DIR:-/etc/systemd/system}"
CA_POOL="${WDEV_CA_POOL:-/etc/ssl/local-certs/ca/rootCA.pem}"
CLOUDFLARED_OVERRIDE="${WDEV_CLOUDFLARED_BIN:-}"
CF_HELPER_VERSION="1.0"

C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"
C_DIM="\e[2m"

say() { printf '%b\n' "$*"; }
ok() { printf "${C_GREEN}[OK]${C_RESET} %s\n" "$*"; }
warn() { printf "${C_YELLOW}[!]${C_RESET} %s\n" "$*"; }
err() { printf "${C_RED}[x]${C_RESET} %s\n" "$*" >&2; }
die() { err "$*"; exit 1; }

is_root() { [ "${EUID:-$(id -u)}" -eq 0 ]; }
need_root() {
  is_root || die "Jalankan perintah ini dengan sudo/root."
}

cloudflared_bin() {
  local c
  if [ -n "$CLOUDFLARED_OVERRIDE" ] && [ -x "$CLOUDFLARED_OVERRIDE" ]; then
    printf '%s\n' "$CLOUDFLARED_OVERRIDE"
    return 0
  fi
  if command -v cloudflared >/dev/null 2>&1; then
    command -v cloudflared
    return 0
  fi
  for c in /usr/local/bin/cloudflared /usr/bin/cloudflared; do
    if [ -x "$c" ]; then
      printf '%s\n' "$c"
      return 0
    fi
  done
  return 1
}

require_cloudflared() {
  CF_BIN=$(cloudflared_bin) || die "cloudflared tidak ditemukan. Jalankan: sudo setup_dev.sh --cloudflare"
  export CF_BIN
}

valid_profile_name() {
  [[ "${1:-}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$ ]]
}

account_file() { printf '%s/%s.conf\n' "$CF_ACCOUNTS" "$1"; }
profile_exists() { [ -f "$(account_file "$1")" ]; }

profile_get() {
  local p=$1 k=$2 f
  f=$(account_file "$p")
  [ -f "$f" ] || return 0
  awk -F= -v k="$k" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$f"
}

profile_set() {
  local p=$1 k=$2 v=$3 f tmp
  f=$(account_file "$p")
  [ -f "$f" ] || return 1
  tmp=$(mktemp "${f}.tmp.XXXXXX") || return 1
  chmod 600 "$tmp"
  awk -v k="$k" -v v="$v" '
    BEGIN { done = 0 }
    $0 ~ "^" k "=" { print k "=" v; done = 1; next }
    { print }
    END { if (!done) print k "=" v }
  ' "$f" > "$tmp" || { rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$f"
  chmod 600 "$f"
}

profile_list() {
  [ -d "$CF_ACCOUNTS" ] || return 0
  find "$CF_ACCOUNTS" -maxdepth 1 -type f -name '*.conf' -printf '%f\n' 2>/dev/null \
    | sed 's/\.conf$//' | sort
}

profile_require() {
  local p=${1:-}
  valid_profile_name "$p" || die "Nama profil tidak valid: '$p' (huruf, angka, _, -; maks 32)."
  profile_exists "$p" || die "Profil '$p' belum ada. Tambah dengan: sudo cf-tunnel account add $p"
}

profile_state_dir() { printf '%s/%s\n' "$CF_STATE" "$1"; }
profile_unit() { printf 'cloudflared-%s.service\n' "$1"; }
profile_env_file() { printf '%s/%s.env\n' "$CF_ACCOUNTS" "$1"; }

profile_mode() { profile_get "$1" CF_MODE; }
profile_zone() { profile_get "$1" CF_ZONE; }
profile_tunnel_name() { profile_get "$1" CF_TUNNEL_NAME; }
profile_tunnel_id() { profile_get "$1" CF_TUNNEL_ID; }
profile_cert() { printf '%s/cert.pem\n' "$(profile_state_dir "$1")"; }

exposure_files() {
  [ -d "$CF_EXPOSURES" ] || return 0
  find "$CF_EXPOSURES" -maxdepth 1 -type f -name '*.conf' -print 2>/dev/null | sort
}

exposures_for_profile() {
  local profile=$1 f domain
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    domain=$(basename "$f" .conf)
    awk -v p="$profile" -v d="$domain" 'NF >= 2 && $1 !~ /^#/ && $2 == p { print $1 "\t" d }' "$f"
  done < <(exposure_files)
}

exposures_for_domain() {
  local domain=$1 f
  f="$CF_EXPOSURES/${domain}.conf"
  [ -f "$f" ] || return 0
  awk 'NF >= 2 && $1 !~ /^#/ { print $1 "\t" $2 }' "$f"
}

exposure_count() {
  exposures_for_profile "$1" | wc -l | tr -d '[:space:]'
}

host_under_zone() {
  local host=$1 zone=$2 rel
  [ -n "$host" ] && [ -n "$zone" ] || return 1
  [ "$host" = "$zone" ] && return 0
  case "$host" in
    *."$zone") ;;
    *) return 1 ;;
  esac
  rel="${host%.$zone}"
  [ -n "$rel" ] && [[ "$rel" != *.* ]]
}

service_installed() {
  systemctl cat "$(profile_unit "$1")" >/dev/null 2>&1
}

service_state() {
  local unit
  unit=$(profile_unit "$1")
  if service_installed "$1"; then
    systemctl is-active "$unit" 2>/dev/null || true
  else
    printf 'not-installed\n'
  fi
}

connector_count() {
  local p=$1 unit port ready
  unit=$(profile_unit "$p")
  if [ "$(service_state "$p")" = "active" ]; then
    port=$(systemctl show -p ExecStart --value "$unit" 2>/dev/null | grep -oP '(?<=--metrics 127\.0\.0\.1:)\d+' || true)
    if [ -n "$port" ]; then
      ready=$(curl -s --max-time 1 "http://127.0.0.1:${port}/ready" 2>/dev/null || true)
      local rc_count
      rc_count=$(printf '%s' "$ready" | jq -r '.readyConnections // empty' 2>/dev/null || true)
      if [ -n "$rc_count" ] && [ "$rc_count" -ge 0 ] 2>/dev/null; then
        printf '%s\n' "$rc_count"
        return 0
      fi
    fi
  fi
  local cert name out count
  cert=$(profile_cert "$p")
  name=$(profile_tunnel_name "$p")
  [ -f "$cert" ] || { printf '%s\n' "-"; return 0; }
  [ -n "$name" ] || { printf '%s\n' "-"; return 0; }
  out=$(timeout 8 "$CF_BIN" --origincert "$cert" tunnel info --output json "$name" 2>/dev/null || true)
  count=$(printf '%s' "$out" | jq -r '(.connectors // []) | length' 2>/dev/null || true)
  if [ -n "$count" ] && [ "$count" != "null" ] && [ "$count" -ge 0 ] 2>/dev/null; then
    printf '%s\n' "$count"
    return 0
  fi
  out=$(timeout 8 "$CF_BIN" --origincert "$cert" tunnel info "$name" 2>/dev/null || true)
  count=$(printf '%s' "$out" | grep -cE '^[0-9a-f]{8}-[0-9a-f]{4}-' || true)
  printf '%s\n' "${count:-0}"
}

api_request() {
  local method=$1 path=$2 data=${3:-}
  local token cfg resp rc
  token=$(profile_get "$CF_PROFILE" CF_API_TOKEN)
  [ -n "$token" ] || return 3
  cfg=$(mktemp) || return 1
  chmod 600 "$cfg"
  printf 'header = "Authorization: Bearer %s"\nheader = "Content-Type: application/json"\n' "$token" > "$cfg"
  if [ -n "$data" ]; then
    resp=$(curl -sS -m 30 -X "$method" --config "$cfg" --data-binary "$data" \
      "https://api.cloudflare.com/client/v4${path}" 2>&1)
    rc=$?
  else
    resp=$(curl -sS -m 30 -X "$method" --config "$cfg" \
      "https://api.cloudflare.com/client/v4${path}" 2>&1)
    rc=$?
  fi
  rm -f "$cfg"
  printf '%s\n' "$resp"
  return "$rc"
}

api_error_text() {
  jq -r '(.errors // [])[]?.message' 2>/dev/null | paste -sd '; ' - 2>/dev/null || true
}

ensure_zone_id() {
  local p=$1 zid zone resp
  CF_PROFILE="$p"
  zid=$(profile_get "$p" CF_ZONE_ID)
  if [ -n "$zid" ]; then
    printf '%s\n' "$zid"
    return 0
  fi
  zone=$(profile_zone "$p")
  [ -n "$zone" ] || return 1
  resp=$(api_request GET "/zones?name=${zone}") || return 1
  if ! printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
    err "Gagal mengambil zone '${zone}': $(printf '%s' "$resp" | api_error_text)"
    return 1
  fi
  zid=$(printf '%s' "$resp" | jq -r '.result[0].id // empty')
  [ -n "$zid" ] || { err "Zone '${zone}' tidak ditemukan untuk token profil '$p'."; return 1; }
  profile_set "$p" CF_ZONE_ID "$zid"
  printf '%s\n' "$zid"
}

ensure_account_id() {
  local p=$1 aid zone resp
  CF_PROFILE="$p"
  aid=$(profile_get "$p" CF_ACCOUNT_ID)
  if [ -n "$aid" ]; then
    printf '%s\n' "$aid"
    return 0
  fi
  if [ "$(profile_mode "$p")" = "token" ]; then
    local envf tkn
    envf=$(profile_env_file "$p")
    if [ -f "$envf" ]; then
      tkn=$(sed -n 's/^TUNNEL_TOKEN=//p' "$envf" | tr -d '\r\n"')
      aid=$(printf '%s' "$tkn" | base64 -d 2>/dev/null | jq -r '.a // empty' 2>/dev/null || true)
      if [ -n "$aid" ]; then
        profile_set "$p" CF_ACCOUNT_ID "$aid"
        printf '%s\n' "$aid"
        return 0
      fi
    fi
  fi
  zone=$(profile_zone "$p")
  resp=$(api_request GET "/zones?name=${zone}") || return 1
  aid=$(printf '%s' "$resp" | jq -r '.result[0].account.id // empty' 2>/dev/null)
  [ -n "$aid" ] || return 1
  profile_set "$p" CF_ACCOUNT_ID "$aid"
  printf '%s\n' "$aid"
}

ensure_tunnel_id() {
  local p=$1 tid aid name resp
  CF_PROFILE="$p"
  tid=$(profile_tunnel_id "$p")
  if [ -n "$tid" ]; then
    printf '%s\n' "$tid"
    return 0
  fi
  if [ "$(profile_mode "$p")" = "token" ]; then
    local envf tkn
    envf=$(profile_env_file "$p")
    if [ -f "$envf" ]; then
      tkn=$(sed -n 's/^TUNNEL_TOKEN=//p' "$envf" | tr -d '\r\n"')
      tid=$(printf '%s' "$tkn" | base64 -d 2>/dev/null | jq -r '.t // empty' 2>/dev/null || true)
      aid=$(printf '%s' "$tkn" | base64 -d 2>/dev/null | jq -r '.a // empty' 2>/dev/null || true)
      if [ -n "$tid" ]; then
        profile_set "$p" CF_TUNNEL_ID "$tid"
        [ -z "$(profile_get "$p" CF_ACCOUNT_ID)" ] && [ -n "$aid" ] && profile_set "$p" CF_ACCOUNT_ID "$aid"
        printf '%s\n' "$tid"
        return 0
      fi
    fi
  fi
  name=$(profile_tunnel_name "$p")
  aid=$(ensure_account_id "$p") || return 1
  [ -n "$name" ] || return 1
  resp=$(api_request GET "/accounts/${aid}/cfd_tunnel?name=${name}&is_deleted=false") || return 1
  tid=$(printf '%s' "$resp" | jq -r '.result[0].id // empty' 2>/dev/null)
  [ -n "$tid" ] || return 1
  profile_set "$p" CF_TUNNEL_ID "$tid"
  printf '%s\n' "$tid"
}

dns_target() {
  local tid=$1
  printf '%s.cfargotunnel.com\n' "$tid"
}

dns_upsert() {
  local p=$1 host=$2 zid tid target resp rid cur data
  CF_PROFILE="$p"
  zid=$(ensure_zone_id "$p") || { warn "Zone ID tidak tersedia; DNS '${host}' tidak diubah."; return 1; }
  tid=$(ensure_tunnel_id "$p") || { warn "Tunnel ID tidak tersedia; DNS '${host}' tidak diubah."; return 1; }
  target=$(dns_target "$tid")
  resp=$(api_request GET "/zones/${zid}/dns_records?type=CNAME&name=${host}") || {
    warn "Gagal membaca DNS '${host}'."
    return 1
  }
  if ! printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
    warn "Gagal membaca DNS '${host}': $(printf '%s' "$resp" | api_error_text)"
    return 1
  fi
  rid=$(printf '%s' "$resp" | jq -r '.result[0].id // empty')
  data=$(jq -nc --arg n "$host" --arg c "$target" \
    '{type:"CNAME", name:$n, content:$c, proxied:true, ttl:1}')
  if [ -n "$rid" ]; then
    cur=$(printf '%s' "$resp" | jq -r '.result[0].content // empty')
    if [ "$cur" = "$target" ] && [ "$(printf '%s' "$resp" | jq -r '.result[0].proxied')" = "true" ]; then
      say "  ${C_DIM}=>${C_RESET} ${host} sudah CNAME ke ${target}"
      return 0
    fi
    resp=$(api_request PUT "/zones/${zid}/dns_records/${rid}" "$data") || return 1
    if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
      ok "DNS diperbarui: ${host} -> ${target} (proxied)"
      return 0
    fi
    warn "Gagal memperbarui DNS '${host}': $(printf '%s' "$resp" | api_error_text)"
    return 1
  fi
  resp=$(api_request POST "/zones/${zid}/dns_records" "$data") || return 1
  if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
    ok "DNS dibuat: ${host} -> ${target} (proxied)"
    return 0
  fi
  warn "Gagal membuat DNS '${host}': $(printf '%s' "$resp" | api_error_text)"
  return 1
}

dns_delete() {
  local p=$1 host=$2 force=${3:-0}
  CF_PROFILE="$p"
  local zid tid target resp rid cur ids deleted=0
  zid=$(ensure_zone_id "$p") || return 1
  tid=$(profile_tunnel_id "$p")
  target=""
  [ -n "$tid" ] && target=$(dns_target "$tid")
  resp=$(api_request GET "/zones/${zid}/dns_records?type=CNAME&name=${host}") || return 1
  if ! printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
    warn "Gagal membaca DNS '${host}': $(printf '%s' "$resp" | api_error_text)"
    return 1
  fi
  ids=$(printf '%s' "$resp" | jq -r '.result[]? | [.id, .content] | @tsv')
  if [ -z "$ids" ]; then
    say "  ${C_DIM}=>${C_RESET} ${host} tidak punya record CNAME"
    return 0
  fi
  while IFS=$'\t' read -r rid cur; do
    [ -n "$rid" ] || continue
    if [ -n "$target" ] && [ "$cur" != "$target" ] && [ "$force" != "1" ]; then
      warn "Record ${host} mengarah ke '${cur}', bukan '${target}'. Dilewati (gunakan --force untuk hapus)."
      continue
    fi
    resp=$(api_request DELETE "/zones/${zid}/dns_records/${rid}") || continue
    if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
      ok "DNS dihapus: ${host} (${cur})"
      deleted=$((deleted + 1))
    else
      warn "Gagal menghapus DNS '${host}': $(printf '%s' "$resp" | api_error_text)"
    fi
  done <<< "$ids"
  [ "$deleted" -gt 0 ]
}

dns_ensure_profile() {
  local p=$1 host domain rc=0 total=0
  if [ -z "$(profile_get "$p" CF_API_TOKEN)" ]; then
    warn "Profil '${p}' tidak punya CF_API_TOKEN. DNS tidak disinkronkan."
    say "  Buat CNAME manual: ${C_BOLD}<host>${C_RESET} -> ${C_BOLD}<tunnel-id>.cfargotunnel.com${C_RESET} (proxied)"
    return 4
  fi
  while IFS=$'\t' read -r host domain; do
    [ -n "$host" ] || continue
    total=$((total + 1))
    dns_upsert "$p" "$host" || rc=1
  done < <(exposures_for_profile "$p")
  [ "$total" -gt 0 ] || say "  ${C_DIM}Tidak ada eksposur untuk profil '${p}'.${C_RESET}"
  return "$rc"
}

allocate_metrics_port() {
  local p=$1 idx base port cur_pid holder
  idx=$(profile_list | awk -v p="$p" '$0 == p { print NR }')
  base=$((20260 + ${idx:-1}))
  cur_pid=$(systemctl show -p MainPID --value "$(profile_unit "$p")" 2>/dev/null || true)
  port=$base
  while :; do
    holder=$(ss -tulpn 2>/dev/null | grep -E ":${port}\b" || true)
    if [ -z "$holder" ]; then
      echo "$port"
      return 0
    fi
    if [ -n "$cur_pid" ] && [ "$cur_pid" != "0" ] && echo "$holder" | grep -q "pid=${cur_pid},"; then
      echo "$port"
      return 0
    fi
    port=$((port + 1))
  done
}

write_unit() {
  local p=$1 bin dir unit tmp mode port
  bin=$(cloudflared_bin) || return 1
  dir=$(profile_state_dir "$p")
  unit="$SYSTEMD_DIR/$(profile_unit "$p")"
  mode=$(profile_mode "$p")
  port=$(allocate_metrics_port "$p")
  tmp=$(mktemp) || return 1
  {
    printf '[Unit]\n'
    printf 'Description=Cloudflare Tunnel (profile: %s)\n' "$p"
    printf 'After=network-online.target nginx.service\n'
    printf 'Wants=network-online.target\n\n'
    printf '[Service]\n'
    printf 'Type=notify\n'
    printf 'TimeoutStartSec=0\n'
    if [ "$mode" = "token" ]; then
      printf 'EnvironmentFile=%s\n' "$(profile_env_file "$p")"
      printf 'ExecStart=%s --no-autoupdate --metrics 127.0.0.1:%s tunnel run\n' "$bin" "$port"
    else
      printf 'ExecStart=%s --no-autoupdate --metrics 127.0.0.1:%s --config %s/config.yml tunnel run\n' "$bin" "$port" "$dir"
    fi
    printf 'Restart=always\n'
    printf 'RestartSec=5s\n'
    printf 'NoNewPrivileges=true\n\n'
    printf '[Install]\n'
    printf 'WantedBy=multi-user.target\n'
  } > "$tmp"
  chmod 644 "$tmp"
  if [ -f "$unit" ] && cmp -s "$tmp" "$unit"; then
    rm -f "$tmp"
    return 1
  fi
  mkdir -p "$SYSTEMD_DIR" 2>/dev/null || true
  mv -f "$tmp" "$unit"
  chmod 644 "$unit"
  return 0
}

ind() {
  local n=$1
  shift
  printf '%*s%s\n' "$n" "" "$*"
}

generate_config() {
  local p=$1 dir config tmp host domain out
  dir=$(profile_state_dir "$p")
  config="$dir/config.yml"
  tmp="$dir/.config.yml.tmp.$$"
  mkdir -p "$dir" || return 1
  chmod 700 "$dir"
  {
    printf 'tunnel: %s\n' "$(profile_tunnel_id "$p")"
    printf 'credentials-file: %s/%s.json\n' "$dir" "$(profile_tunnel_id "$p")"
    printf 'protocol: auto\n'
    printf 'no-autoupdate: true\n'
    printf 'ingress:\n'
    while IFS=$'\t' read -r host domain; do
      [ -n "$host" ] || continue
  ind 2 "- hostname: $host"
  ind 4 "service: https://127.0.0.1:443"
  ind 4 "originRequest:"
  ind 6 "originServerName: $host"
  ind 6 "caPool: $CA_POOL"
  ind 6 "connectTimeout: 10s"
 done < <(exposures_for_profile "$p")
  ind 2 "- service: http_status:404"
  } > "$tmp"
  chmod 600 "$tmp"
  if ! out=$("$CF_BIN" --config "$tmp" tunnel ingress validate 2>&1); then
    rm -f "$tmp"
    err "Ingress profil '${p}' tidak valid:"
    printf '%s\n' "$out" | sed 's/^/  /' >&2
    return 1
  fi
  mv -f "$tmp" "$config"
  chmod 644 "$config"
}

sync_profile() {
  local p=$1 mode tid dir config changed=0
  mode=$(profile_mode "$p")
  [ "$mode" = "local" ] || {
    warn "Profil '${p}' memakai mode token (remotely-managed); ingress diatur di dashboard."
    return 0
  }
  tid=$(profile_tunnel_id "$p")
  dir=$(profile_state_dir "$p")
  config="$dir/config.yml"
  if [ -z "$tid" ]; then
    warn "Profil '${p}' belum punya tunnel. Jalankan: sudo cf-tunnel tunnel create --profile $p"
    return 1
  fi
  [ -f "$dir/${tid}.json" ] || {
    warn "Credentials tunnel '${p}' tidak ditemukan: $dir/${tid}.json"
    return 1
  }
  if [ -f "$config" ]; then
    local before
    before=$(sha256sum "$config" | awk '{print $1}')
    generate_config "$p" || return 1
    [ "$(sha256sum "$config" | awk '{print $1}')" = "$before" ] || changed=1
  else
    generate_config "$p" || return 1
    changed=1
  fi
  if write_unit "$p"; then
    systemctl daemon-reload 2>/dev/null || true
    changed=1
  fi
  if [ "$changed" -eq 1 ] && service_state "$p" | grep -q '^active$'; then
    systemctl restart "$(profile_unit "$p")" 2>/dev/null \
      && ok "Service '$(profile_unit "$p")' di-restart." \
      || warn "Gagal restart '$(profile_unit "$p")'."
  fi
  ok "Ingress profil '${p}' tersinkron ($(exposure_count "$p") host)."
}

cmd_login() {
  local p="" force=0 dir cert
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --force) force=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) p=$1; shift ;;
    esac
  done
  need_root
  profile_require "$p"
  require_cloudflared
  dir=$(profile_state_dir "$p")
  cert="$dir/cert.pem"
  mkdir -p "$dir"
  chmod 700 "$dir"
  if [ -f "$cert" ] && [ "$force" -ne 1 ]; then
    say "Certifikat origin sudah ada: $cert (gunakan --force untuk login ulang)"
    return 0
  fi
  say "Membuka login Cloudflare untuk profil '${p}'. Ikuti URL yang muncul."
  ( unset TUNNEL_ORIGIN_CERT; HOME="$dir" "$CF_BIN" tunnel login ) || {
    err "Login Cloudflare gagal."
    return 1
  }
  if [ -f "$dir/.cloudflared/cert.pem" ]; then
    mv -f "$dir/.cloudflared/cert.pem" "$cert"
    rmdir "$dir/.cloudflared" 2>/dev/null || true
  fi
  [ -f "$cert" ] || { err "cert.pem tidak ditemukan setelah login."; return 1; }
  chmod 600 "$cert"
  ok "Certifikat origin disimpan: $cert"
}

cmd_tunnel_create() {
  local p="" force=0 name="" out="" tid="" creds="" dir="" cert=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --name) name=${2:-}; shift 2 ;;
      --force) force=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  profile_require "$p"
  [ "$(profile_mode "$p")" = "local" ] || die "Profil '$p' memakai mode token; tunnel dibuat di dashboard."
  require_cloudflared
  dir=$(profile_state_dir "$p")
  cert="$dir/cert.pem"
  [ -f "$cert" ] || die "cert.pem belum ada. Jalankan: sudo cf-tunnel login --profile $p"
  if [ -n "$(profile_tunnel_id "$p")" ] && [ "$force" -ne 1 ]; then
    die "Tunnel sudah ada untuk profil '$p'. Gunakan 'tunnel rotate --profile $p' untuk mengganti."
  fi
  [ -n "$name" ] || name=$(profile_tunnel_name "$p")
  [ -n "$name" ] || name="wdev-$p"
  if [ "$force" -eq 1 ]; then
    local backup="$dir/backup-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$backup"
    cp -f "$dir/config.yml" "$dir"/*.json "$backup"/ 2>/dev/null || true
    warn "Backup state lama: $backup"
  fi
  say "Membuat tunnel '${name}' untuk profil '${p}'..."
  out=$(HOME="$dir" "$CF_BIN" --origincert "$cert" tunnel create \
    --credentials-file "$dir/credentials.json" "$name" 2>&1) || {
    printf '%s\n' "$out" | sed 's/^/  /' >&2
    err "Pembuatan tunnel gagal."
    return 1
  }
  tid=$(printf '%s\n' "$out" | sed -n 's/.*with id \([0-9a-f-]\{36\}\).*/\1/p' | head -n1)
  if [ -z "$tid" ]; then
    tid=$(HOME="$dir" "$CF_BIN" --origincert "$cert" tunnel list --name "$name" --output json 2>/dev/null \
      | jq -r '.[0].id // empty' 2>/dev/null || true)
  fi
  [ -n "$tid" ] || { err "Tidak dapat menentukan UUID tunnel dari output cloudflared."; return 1; }
  creds="$dir/${tid}.json"
  if [ -f "$dir/credentials.json" ]; then
    mv -f "$dir/credentials.json" "$creds"
  elif [ -f "$dir/.cloudflared/${tid}.json" ]; then
    mv -f "$dir/.cloudflared/${tid}.json" "$creds"
  fi
  [ -f "$creds" ] || { err "Credentials tunnel tidak ditemukan."; return 1; }
  chmod 600 "$creds"
  profile_set "$p" CF_TUNNEL_ID "$tid"
  profile_set "$p" CF_TUNNEL_NAME "$name"
  ok "Tunnel '${name}' siap (UUID: ${tid})."
  generate_config "$p" || return 1
  write_unit "$p" || true
  sync_profile "$p"
  systemctl daemon-reload 2>/dev/null || true
  systemctl enable --now "$(profile_unit "$p")" >/dev/null 2>&1 \
    && ok "Service '$(profile_unit "$p")' aktif." \
    || warn "Service belum aktif; periksa: systemctl status $(profile_unit "$p")"
}

cmd_tunnel_info() {
  local p="" name cert
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  profile_require "$p"
  require_cloudflared
  cert=$(profile_cert "$p")
  [ -f "$cert" ] || die "Certifikat origin tidak ada: $cert"
  name=$(profile_tunnel_name "$p")
  "$CF_BIN" --origincert "$cert" tunnel info "$name"
}

cmd_account_add() {
local p="" zone="" mode="local" name="" api_token="" tunnel_token="" account_id=""
 local no_login=0 no_start=0 mode_explicit=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --zone) zone=${2:-}; shift 2 ;;
      --zone=*) zone=${1#*=}; shift ;;
      --mode) mode=${2:-}; shift 2; mode_explicit=1 ;;
      --mode=*) mode=${1#*=}; mode_explicit=1; shift ;;
      --tunnel-name|--name) name=${2:-}; shift 2 ;;
      --tunnel-name=*|--name=*) name=${1#*=}; shift ;;
      --api-token) api_token=${2:-}; shift 2 ;;
      --api-token=*) api_token=${1#*=}; shift ;;
      --tunnel-token) tunnel_token=${2:-}; shift 2 ;;
      --tunnel-token=*) tunnel_token=${1#*=}; shift ;;
      --account-id) account_id=${2:-}; shift 2 ;;
      --account-id=*) account_id=${1#*=}; shift ;;
      --no-login) no_login=1; shift ;;
      --no-start) no_start=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  [ -n "$p" ] || die "Penggunaan: cf-tunnel account add <profile> [--zone example.com] [--mode local|token]"
  valid_profile_name "$p" || die "Nama profil tidak valid: '$p'."
  profile_exists "$p" && die "Profil '$p' sudah ada."
  case "$mode" in local|token) ;; *) die "Mode harus 'local' atau 'token'." ;; esac

 if [ -t 0 ]; then
 [ -n "$zone" ] || read -rp "Zone Cloudflare (contoh: example.com): " zone
 if [ "$mode_explicit" -eq 0 ] && [ -z "$tunnel_token" ]; then
 read -rp "Mode tunnel [local/token] (default: local): " in_mode
 case "$in_mode" in token|local) mode="$in_mode" ;; "") ;; *) die "Mode harus 'local' atau 'token'." ;; esac
 fi
 if [ -z "$api_token" ]; then
 read -rsp "API Token (opsional, scope Zone:DNS:Edit + Zone:Read): " api_token; echo
 fi
 fi
  [ -n "$zone" ] || die "Zone wajib diisi (--zone)."
  [ -n "$name" ] || name="wdev-$p"
  if [ "$mode" = "token" ] && [ -z "$tunnel_token" ]; then
    [ -t 0 ] && read -rsp "Tunnel Token dari dashboard: " tunnel_token && echo
    [ -n "$tunnel_token" ] || die "Mode token membutuhkan --tunnel-token."
  fi

  if [ -n "$tunnel_token" ]; then
    tunnel_token=$(printf '%s' "$tunnel_token" | awk '{print $NF}' | tr -d '\r\n"')
  fi

  local parsed_tid="" parsed_aid=""
  if [ "$mode" = "token" ] && [ -n "$tunnel_token" ]; then
    parsed_tid=$(printf '%s' "$tunnel_token" | base64 -d 2>/dev/null | jq -r '.t // empty' 2>/dev/null || true)
    parsed_aid=$(printf '%s' "$tunnel_token" | base64 -d 2>/dev/null | jq -r '.a // empty' 2>/dev/null || true)
    [ -z "$account_id" ] && [ -n "$parsed_aid" ] && account_id="$parsed_aid"
  fi

  mkdir -p "$CF_ACCOUNTS"
  chmod 700 "$CF_ETC" "$CF_ACCOUNTS" 2>/dev/null || true
  (
    umask 077
    {
      printf 'CF_PROFILE=%s\n' "$p"
      printf 'CF_ZONE=%s\n' "$zone"
      printf 'CF_MODE=%s\n' "$mode"
      printf 'CF_ACCOUNT_ID=%s\n' "$account_id"
      printf 'CF_TUNNEL_NAME=%s\n' "$name"
      printf 'CF_TUNNEL_ID=%s\n' "${parsed_tid:-}"
      printf 'CF_ZONE_ID=\n'
      printf 'CF_API_TOKEN=%s\n' "$api_token"
    } > "$(account_file "$p")"
  )
  chmod 600 "$(account_file "$p")"
  ok "Profil '${p}' dibuat (zone: ${zone}, mode: ${mode})."
  [ -n "$parsed_tid" ] && ok "Tunnel ID terdeteksi: ${parsed_tid}"

  if [ -n "$api_token" ]; then
    ensure_zone_id "$p" >/dev/null && ok "Zone ID ter-cache." \
      || warn "Token/zone belum tervalidasi; DNS otomatis mungkin gagal."
  fi

  if [ "$mode" = "token" ]; then
    local envf
    envf=$(profile_env_file "$p")
    ( umask 077; printf 'TUNNEL_TOKEN=%s\n' "$tunnel_token" > "$envf" )
    chmod 600 "$envf"
    ensure_tunnel_id "$p" >/dev/null 2>&1 || warn "Tunnel ID belum diketahui; DNS otomatis dinonaktifkan sampai API token/account ID tersedia."
    write_unit "$p" || true
    systemctl daemon-reload 2>/dev/null || true
    if [ "$no_start" -eq 0 ]; then
      systemctl enable --now "$(profile_unit "$p")" >/dev/null 2>&1 \
        && ok "Service '$(profile_unit "$p")' aktif." \
        || warn "Service gagal dijalankan: $(profile_unit "$p")"
    fi
    say "Catatan: ingress mode token diatur di dashboard Cloudflare (Public Hostname -> https://127.0.0.1:443)."
    return 0
  fi

  if [ "$no_login" -eq 0 ]; then
    [ -f "$(profile_cert "$p")" ] || cmd_login --profile "$p" || return 1
    [ -n "$(profile_tunnel_id "$p")" ] || cmd_tunnel_create --profile "$p" --name "$name" || return 1
    if [ "$no_start" -eq 0 ]; then
      systemctl enable --now "$(profile_unit "$p")" >/dev/null 2>&1 \
        && ok "Service '$(profile_unit "$p")' aktif." \
        || warn "Service gagal dijalankan: $(profile_unit "$p")"
    fi
    sync_profile "$p" || true
    [ -n "$(profile_get "$p" CF_API_TOKEN)" ] && dns_ensure_profile "$p" || true
  else
    say "Login dilewati. Lanjutkan dengan:"
    say "  sudo cf-tunnel login --profile ${p}"
    say "  sudo cf-tunnel tunnel create --profile ${p}"
  fi
}

cmd_account_list() {
  local p zone mode name tid unit state
  printf "${C_BOLD}%-14s | %-24s | %-6s | %-28s | %-14s${C_RESET}\n" \
    "PROFILE" "ZONE" "MODE" "TUNNEL" "SERVICE"
 say "---------------+--------------------------+--------+------------------------------+---------------"
 local count=0
 while IFS= read -r p; do
 [ -n "$p" ] || continue
 count=$((count + 1))
 zone=$(profile_zone "$p")
 mode=$(profile_mode "$p")
 name=$(profile_tunnel_name "$p")
 tid=$(profile_tunnel_id "$p")
 unit=$(profile_unit "$p")
 state=$(service_state "$p")
 printf "%-14s | %-24s | %-6s | %-28s | %-14s\n" \
 "$p" "${zone:--}" "${mode:--}" "${name:--}${tid:+ [${tid:0:8}]}" "$state"
 done < <(profile_list)
 [ "$count" -gt 0 ] || say "Belum ada profil Cloudflare. Tambah: sudo cf-tunnel account add <profil>"
}

cmd_account_remove() {
  local p="" yes=0 dir unit
  while [ $# -gt 0 ]; do
    case "$1" in
      --yes|-y) yes=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  profile_require "$p"
  local count
  count=$(exposure_count "$p")
  if [ "$count" -gt 0 ]; then
    die "Profil '$p' masih dipakai ${count} eksposur. Hapus dulu: sudo dev-site unexpose <domain> --profile $p"
  fi
  if [ "$yes" -ne 1 ]; then
    [ -t 0 ] || die "Gunakan --yes untuk menghapus tanpa konfirmasi."
    read -rp "Hapus profil '${p}' beserta service dan credentials? [y/N]: " ans
    case "$ans" in y|Y|yes) ;; *) say "Dibatalkan."; return 0 ;; esac
  fi
  unit=$(profile_unit "$p")
  systemctl disable --now "$unit" >/dev/null 2>&1 || true
  rm -f "$SYSTEMD_DIR/$unit"
  systemctl daemon-reload 2>/dev/null || true
  dir=$(profile_state_dir "$p")
  rm -rf "$dir"
  rm -f "$(account_file "$p")" "$(profile_env_file "$p")"
  ok "Profil '${p}' dihapus. Record DNS tidak dihapus otomatis."
}

cmd_sync() {
  local p="" all=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --all) all=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  require_cloudflared
  local rc=0
  if [ "$all" -eq 1 ] || [ -z "$p" ]; then
    while IFS= read -r p; do
      [ -n "$p" ] || continue
      sync_profile "$p" || rc=1
    done < <(profile_list)
  else
    profile_require "$p"
    sync_profile "$p" || rc=1
  fi
  return "$rc"
}

cmd_dns() {
  local action=${1:-}
  [ -n "$action" ] || die "Penggunaan: cf-tunnel dns <list|add|rm|ensure> --profile <p> [host]"
  shift || true
  local p="" host="" force=0 all=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --force) force=1; shift ;;
      --all) all=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$host" ] && host=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  if [ "$all" -eq 1 ]; then
    [ "$action" = "ensure" ] || die "Opsi --all hanya untuk 'dns ensure'."
    local rc=0 pp
    while IFS= read -r pp; do
      [ -n "$pp" ] || continue
      dns_ensure_profile "$pp" || rc=1
    done < <(profile_list)
    return "$rc"
  fi
  profile_require "$p"
  CF_PROFILE="$p"
  case "$action" in
    ensure)
      dns_ensure_profile "$p"
      ;;
    list)
      local zid resp
      zid=$(ensure_zone_id "$p") || die "Zone ID tidak tersedia."
      resp=$(api_request GET "/zones/${zid}/dns_records?type=CNAME") || die "Gagal membaca DNS."
      printf '%s' "$resp" | jq -r '.result[]? | [.name, .content, (if .proxied then "proxied" else "dns-only" end)] | @tsv' \
        | awk -F'\t' '{ printf "%-40s %-46s %s\n", $1, $2, $3 }'
      ;;
    add)
      [ -n "$host" ] || die "Host wajib diisi."
      dns_upsert "$p" "$host"
      ;;
    rm)
      [ -n "$host" ] || die "Host wajib diisi."
      dns_delete "$p" "$host" "$force"
      ;;
    *) die "Aksi DNS tidak dikenal: $action" ;;
  esac
}

cmd_service() {
  local action=${1:-}
  [ -n "$action" ] || die "Penggunaan: cf-tunnel service <start|stop|restart|status|logs|install|enable|disable> [--profile p|--all]"
  shift || true
  local p="" all=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --all) all=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  local -a targets=()
  if [ "$all" -eq 1 ]; then
    while IFS= read -r p; do [ -n "$p" ] && targets+=("$p"); done < <(profile_list)
  else
    profile_require "$p"
    targets=("$p")
  fi
  [ "${#targets[@]}" -gt 0 ] || die "Belum ada profil Cloudflare."
  local t unit rc=0
  for t in "${targets[@]}"; do
    unit=$(profile_unit "$t")
    case "$action" in
      install)
        write_unit "$t" && ok "Unit '$unit' diperbarui." || say "Unit '$unit' tidak berubah."
        systemctl daemon-reload 2>/dev/null || true
        ;;
      start)
        systemctl start "$unit" && ok "Started: $unit" || rc=1
        ;;
      stop)
        systemctl stop "$unit" && ok "Stopped: $unit" || rc=1
        ;;
      restart)
        systemctl daemon-reload 2>/dev/null || true
        systemctl restart "$unit" && ok "Restarted: $unit" || rc=1
        ;;
      enable)
        systemctl enable --now "$unit" && ok "Enabled: $unit" || rc=1
        ;;
      disable)
        systemctl disable --now "$unit" && ok "Disabled: $unit" || rc=1
        ;;
      status)
        systemctl status "$unit" --no-pager -l || rc=1
        ;;
      logs)
        journalctl -u "$unit" -n 100 --no-pager || rc=1
        ;;
      *) die "Aksi service tidak dikenal: $action" ;;
    esac
  done
  return "$rc"
}

cmd_status() {
  local p="" all=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --all) all=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  require_cloudflared
  local -a targets=()
  if [ "$all" -eq 1 ] || [ -z "$p" ]; then
    while IFS= read -r p; do [ -n "$p" ] && targets+=("$p"); done < <(profile_list)
  else
    profile_require "$p"
    targets=("$p")
  fi
  if [ "${#targets[@]}" -eq 0 ]; then
    warn "Belum ada profil Cloudflare. Tambah: sudo cf-tunnel account add <profile>"
    return 0
  fi
  printf "${C_BOLD}%-10s | %-22s | %-6s | %-10s | %-9s | %-9s | %s${C_RESET}\n" \
    "PROFILE" "ZONE" "MODE" "SERVICE" "CONNECT" "EXPOSURES" "TUNNEL"
  say "-----------+------------------------+--------+------------+-----------+-----------+--------------------------"
  local zone mode state conn exp name
  for p in "${targets[@]}"; do
    zone=$(profile_zone "$p")
    mode=$(profile_mode "$p")
    state=$(service_state "$p")
    conn="-"
    name=$(profile_tunnel_name "$p")
    if [ "$state" = "active" ]; then
      conn=$(connector_count "$p")
    fi
    exp=$(exposure_count "$p")
    printf "%-10s | %-22s | %-6s | %-10s | %-9s | %-9s | %s\n" \
      "$p" "${zone:--}" "${mode:--}" "$state" "$conn" "$exp" "${name:--}"
  done
}

cmd_exposures() {
  local host domain profile mode state
  printf "${C_BOLD}%-24s | %-38s | %-10s | %-9s${C_RESET}\n" "DOMAIN" "PUBLIC HOST" "PROFILE" "SERVICE"
  say "-------------------------+----------------------------------------+------------+----------"
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    domain=$(basename "$f" .conf)
    while IFS=$'\t' read -r host profile; do
      [ -n "$host" ] || continue
      mode=$(profile_mode "$profile")
      state=$(service_state "$profile")
      printf "%-24s | %-38s | %-10s | %-9s\n" "$domain" "$host" "${profile:-?}" "${state:-?}"
    done < <(exposures_for_domain "$domain")
  done < <(exposure_files)
}

cmd_doctor() {
  local p="" all=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --all) all=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  local -a targets=()
  if [ "$all" -eq 1 ] || [ -z "$p" ]; then
    while IFS= read -r p; do [ -n "$p" ] && targets+=("$p"); done < <(profile_list)
  else
    profile_require "$p"
    targets=("$p")
  fi
  local failures=0
  say "============================================================"
  say " DIAGNOSTIK CLOUDFLARE TUNNEL"
  say "============================================================"
  require_cloudflared || { err "cloudflared tidak ditemukan"; return 1; }
  ok "cloudflared: $("$CF_BIN" --version 2>/dev/null | head -n1)"
  [ "${#targets[@]}" -gt 0 ] || { warn "Belum ada profil."; return 0; }

  local t zone mode cert dir tid unit state conn host domain
  for t in "${targets[@]}"; do
    say ""
    say "${C_BOLD}Profil: ${t}${C_RESET}"
    zone=$(profile_zone "$t")
    mode=$(profile_mode "$t")
    [ -n "$zone" ] && ok "zone: $zone" || { err "zone kosong"; failures=$((failures + 1)); }
    if [ "$(stat -c %a "$(account_file "$t")" 2>/dev/null)" = "600" ]; then
      ok "permission account.conf: 600"
    else
      warn "permission account.conf bukan 600"
    fi
    if [ "$mode" = "local" ]; then
      cert=$(profile_cert "$t")
      dir=$(profile_state_dir "$t")
      tid=$(profile_tunnel_id "$t")
      [ -f "$cert" ] && ok "cert.pem ada" || { err "cert.pem tidak ada"; failures=$((failures + 1)); }
      [ -n "$tid" ] && [ -f "$dir/${tid}.json" ] && ok "credentials tunnel ada" \
        || { err "credentials tunnel tidak ada"; failures=$((failures + 1)); }
      if [ -f "$dir/config.yml" ]; then
        if out=$("$CF_BIN" --config "$dir/config.yml" tunnel ingress validate 2>&1); then
          ok "ingress valid"
        else
          err "ingress tidak valid: $(printf '%s' "$out" | tail -n1)"
          failures=$((failures + 1))
        fi
      else
        err "config.yml belum dibuat (jalankan: cf-tunnel sync --profile $t)"
        failures=$((failures + 1))
      fi
    else
      local envf
      envf=$(profile_env_file "$t")
      [ -f "$envf" ] && ok "EnvironmentFile ada" || { err "EnvironmentFile tidak ada"; failures=$((failures + 1)); }
    fi
    unit=$(profile_unit "$t")
    if service_installed "$t"; then
      state=$(service_state "$t")
      if [ "$state" = "active" ]; then
        ok "service aktif: $unit"
        conn=$(connector_count "$t")
        if [ "${conn:-0}" != "-" ] && [ "${conn:-0}" -gt 0 ] 2>/dev/null; then
          ok "connector aktif: $conn"
        else
          warn "connector belum terlihat (mode: $mode)"
        fi
      else
        err "service tidak aktif: $state"
        failures=$((failures + 1))
      fi
    else
      err "unit belum terpasang: $unit"
      failures=$((failures + 1))
    fi
    while IFS=$'\t' read -r host domain; do
      [ -n "$host" ] || continue
      if curl -sS -m 8 -H 'accept: application/dns-json' \
        "https://cloudflare-dns.com/dns-query?name=${host}&type=A" 2>/dev/null \
        | jq -e '.Answer | length > 0' >/dev/null 2>&1; then
        ok "DNS publik: $host"
      else
        warn "DNS publik belum menjawab: $host"
      fi
      local code
      code=$(curl -sk -m 10 --resolve "${host}:443:127.0.0.1" -o /dev/null \
        -w '%{http_code}' "https://${host}/" 2>/dev/null || true)
      case "$code" in
        200|204|301|302|303|307|308|401|403) ok "origin lokal: $host (HTTP $code)" ;;
        *) warn "origin lokal: $host (HTTP ${code:-timeout})" ;;
      esac
    done < <(exposures_for_profile "$t")
  done
  say ""
  if [ "$failures" -eq 0 ]; then
    ok "Diagnostik selesai: tidak ada kegagalan."
    return 0
  fi
  err "Diagnostik selesai: ${failures} kegagalan."
  return 1
}

show_help() {
  cat <<'EOF'
Penggunaan: cf-tunnel <perintah> [opsi]

Perintah:
  account add <profile>        Tambah akun Cloudflare (login, tunnel, service)
  account list                Tampilkan profil, zone, tunnel, dan status service
  account remove <profile>    Hapus profil (harus tanpa eksposur)
  login --profile <p>         Login origin cert Cloudflare untuk profil
  tunnel create --profile <p> Buat tunnel + credentials + service
  tunnel rotate --profile <p> Ganti tunnel (buat baru, DNS diarahkan ulang)
  tunnel info --profile <p>   Detail connector tunnel
  sync [--profile <p>|--all]  Regenerate ingress, validasi, restart service
  dns list --profile <p>      Daftar CNAME di zone profil
  dns add --profile <p> <host>     Buat/perbarui CNAME ke tunnel
  dns rm --profile <p> <host>      Hapus CNAME (--force untuk target berbeda)
  dns ensure --profile <p>    Sinkronkan CNAME semua eksposur profil
  service <aksi> [--profile <p>|--all]
                               start|stop|restart|status|logs|install|enable|disable
  status [--all]              Tabel profil, connector, dan jumlah eksposur
  exposures                   Tabel domain lokal -> host publik -> profil
  doctor [--all]              Diagnostik config, service, DNS, dan origin lokal
  help                        Tampilkan bantuan ini

Contoh:
  sudo cf-tunnel account add cf1
  sudo cf-tunnel account add cf2 --zone example.net --mode local
  sudo cf-tunnel sync --all
  sudo cf-tunnel status --all
  sudo cf-tunnel dns ensure --profile cf1
  sudo cf-tunnel service logs --profile cf1
EOF
}

main() {
  local cmd=${1:-help}
  [ $# -gt 0 ] && shift || true
  case "$cmd" in
    account)
      local sub=${1:-list}
      [ $# -gt 0 ] && shift || true
      case "$sub" in
        add) cmd_account_add "$@" ;;
        list|ls) need_root; cmd_account_list ;;
        remove|rm) cmd_account_remove "$@" ;;
        *) die "Perintah account tidak dikenal: $sub" ;;
      esac
      ;;
    login) cmd_login "$@" ;;
    tunnel)
      local sub=${1:-}
      [ $# -gt 0 ] && shift || true
      case "$sub" in
        create) cmd_tunnel_create "$@" ;;
        rotate) cmd_tunnel_create "$@" --force ;;
        info|status) cmd_tunnel_info "$@" ;;
        *) die "Perintah tunnel tidak dikenal: $sub" ;;
      esac
      ;;
    sync|sync-ingress) cmd_sync "$@" ;;
    dns) cmd_dns "$@" ;;
    service) cmd_service "$@" ;;
    status|list) cmd_status "$@" ;;
    exposures|expose-list) cmd_exposures ;;
    doctor) cmd_doctor "$@" ;;
    version|--version|-v) say "cf-tunnel ${CF_HELPER_VERSION}" ;;
    help|--help|-h|"") show_help ;;
    *) die "Perintah tidak dikenal: $cmd (lihat: cf-tunnel help)" ;;
  esac
}

main "$@"
