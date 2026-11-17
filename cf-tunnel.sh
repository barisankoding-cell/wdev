#!/bin/bash
# cf-tunnel: manajemen Cloudflare Tunnel multi-akun untuk wdev.
# Dipasang oleh setup_dev.sh; state disimpan per profil di /etc/wdev/cloudflare.
set -uo pipefail

WDEV_ETC="${WDEV_ETC:-/etc/wdev}"
CF_ETC="${WDEV_CF_ETC:-$WDEV_ETC/cloudflare}"
CF_ACCOUNTS="$CF_ETC/accounts"
CF_ROUTES="$CF_ETC/routes"
CF_ACCESS="$CF_ETC/access"
CF_EXPOSURES="${WDEV_EXPOSURES:-$WDEV_ETC/exposures}"
CF_STATE="${WDEV_CLOUDFLARED_STATE:-/etc/cloudflared}"
SYSTEMD_DIR="${WDEV_SYSTEMD_DIR:-/etc/systemd/system}"
CA_POOL="${WDEV_CA_POOL:-/etc/ssl/local-certs/ca/rootCA.pem}"
NGINX_SITES_DIR="${WDEV_NGINX_SITES_DIR:-/etc/nginx/sites-available}"
CLOUDFLARED_OVERRIDE="${WDEV_CLOUDFLARED_BIN:-}"
CF_HELPER_VERSION="1.1"

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
profile_protocol() {
  local proto
  proto=$(profile_get "$1" CF_PROTOCOL)
  printf '%s\n' "${proto:-auto}"
}
profile_cert() { printf '%s/cert.pem\n' "$(profile_state_dir "$1")"; }
routes_file() { printf '%s/%s.conf\n' "$CF_ROUTES" "$1"; }
access_file() { printf '%s/%s.conf\n' "$CF_ACCESS" "$1"; }

quick_state_dir() {
  if [ -n "${WDEV_QUICK_DIR:-}" ]; then
    printf '%s\n' "$WDEV_QUICK_DIR"
  elif is_root; then
    printf '/run/wdev-quick\n'
  else
    printf '%s/wdev-quick-%s\n' "${XDG_RUNTIME_DIR:-/tmp}" "$(id -u)"
  fi
}

check_accounts_readable() {
  if [ -d "$CF_ACCOUNTS" ] && [ ! -r "$CF_ACCOUNTS" ]; then
    warn "Direktori profil tidak dapat dibaca tanpa sudo: $CF_ACCOUNTS"
    say "  Jalankan dengan: sudo cf-tunnel ..."
    return 1
  fi
  return 0
}

valid_protocol() {
  case "${1:-}" in auto|quic|http2) return 0 ;; *) return 1 ;; esac
}

routes_for_profile() {
  local f
  f=$(routes_file "$1")
  [ -f "$f" ] || return 0
  awk -F'\t' 'NF >= 3 && $1 !~ /^#/ && $1 != ""' "$f"
}

route_count() {
  routes_for_profile "$1" | wc -l | tr -d '[:space:]'
}

route_files() {
  [ -d "$CF_ROUTES" ] || return 0
  find "$CF_ROUTES" -maxdepth 1 -type f -name '*.conf' -print 2>/dev/null | sort
}

access_for_profile() {
  local f
  f=$(access_file "$1")
  [ -f "$f" ] || return 0
  awk -F'\t' 'NF >= 3 && $1 !~ /^#/ && $1 != ""' "$f"
}

access_for_host() {
  local p=$1 host=$2 f
  f=$(access_file "$p")
  [ -f "$f" ] || return 1
  awk -F'\t' -v h="$host" 'NF >= 3 && $1 == h { print $2 "\t" $3; exit }' "$f"
}

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

# Usage: host_under_zone <host> <zone> [allow_deep=0]
# Return: 0 valid, 2 multi-label under zone (needs allow_deep=1), 1 outside zone
host_under_zone() {
  local host=$1 zone=$2 allow_deep=${3:-0} rel
  [ -n "$host" ] && [ -n "$zone" ] || return 1
  [ "$host" = "$zone" ] && return 0
  case "$host" in
    *."$zone") ;;
    *) return 1 ;;
  esac
  rel="${host%.$zone}"
  [ -n "$rel" ] || return 1
  if [[ "$rel" == *.* ]]; then
    [ "$allow_deep" = "1" ] && return 0 || return 2
  fi
  return 0
}

service_installed() {
  local unit
  unit=$(profile_unit "$1")
  [ -f "$SYSTEMD_DIR/$unit" ] || systemctl cat "$unit" >/dev/null 2>&1
}

service_state() {
  local unit st
  unit=$(profile_unit "$1")
  if service_installed "$1"; then
    st=$(systemctl is-active "$unit" 2>/dev/null || true)
    printf '%s\n' "${st:-inactive}"
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
  if [ "$(profile_mode "$p")" = "token" ] && [ -n "$(profile_get "$p" CF_API_TOKEN)" ]; then
    local aid tid resp api_count
    aid=$(ensure_account_id "$p" 2>/dev/null || true)
    tid=$(ensure_tunnel_id "$p" 2>/dev/null || true)
    if [ -n "$aid" ] && [ -n "$tid" ]; then
      CF_PROFILE="$p"
      resp=$(api_request GET "/accounts/${aid}/cfd_tunnel/${tid}/connections" 2>/dev/null || true)
      if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
        api_count=$(printf '%s' "$resp" | jq -r '(.result // []) | length' 2>/dev/null || true)
        if [ -n "$api_count" ] && [ "$api_count" -ge 0 ] 2>/dev/null; then
          printf '%s\n' "$api_count"
          return 0
        fi
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
  local p=$1 host=$2 force=${3:-0} zid tid target resp rid cur data conflicts cid ctype cval
  CF_PROFILE="$p"
  zid=$(ensure_zone_id "$p") || { warn "Zone ID tidak tersedia; DNS '${host}' tidak diubah."; return 1; }
  tid=$(ensure_tunnel_id "$p") || { warn "Tunnel ID tidak tersedia; DNS '${host}' tidak diubah."; return 1; }
  target=$(dns_target "$tid")
  resp=$(api_request GET "/zones/${zid}/dns_records?name=${host}") || {
    warn "Gagal membaca DNS '${host}'."
    return 1
  }
  if ! printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
    warn "Gagal membaca DNS '${host}': $(printf '%s' "$resp" | api_error_text)"
    return 1
  fi
  conflicts=$(printf '%s' "$resp" | jq -r '.result[]? | select(.type == "A" or .type == "AAAA") | [.id, .type, .content] | @tsv')
  if [ -n "$conflicts" ]; then
    if [ "$force" != "1" ]; then
      warn "Record A/AAAA sudah ada untuk '${host}'; CNAME dilewati (gunakan --force untuk mengganti)."
      return 1
    fi
    while IFS=$'\t' read -r cid ctype cval; do
      [ -n "$cid" ] || continue
      if api_request DELETE "/zones/${zid}/dns_records/${cid}" | jq -e '.success == true' >/dev/null 2>&1; then
        ok "Record konflik ${ctype} dihapus: ${host} (${cval})"
      else
        warn "Gagal menghapus record ${ctype} lama untuk '${host}'."
        return 1
      fi
    done <<< "$conflicts"
  fi
  rid=$(printf '%s' "$resp" | jq -r '[.result[]? | select(.type == "CNAME")][0].id // empty')
  data=$(jq -nc --arg n "$host" --arg c "$target" \
    '{type:"CNAME", name:$n, content:$c, proxied:true, ttl:1}')
  if [ -n "$rid" ]; then
    cur=$(printf '%s' "$resp" | jq -r '[.result[]? | select(.type == "CNAME")][0].content // empty')
    if [ "$cur" = "$target" ] && [ "$(printf '%s' "$resp" | jq -r '[.result[]? | select(.type == "CNAME")][0].proxied')" = "true" ]; then
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
  if [ -z "$(profile_get "$p" CF_API_TOKEN)" ]; then
    return 4
  fi
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

dns_hosts_for_profile() {
  local p=$1 host domain path svc
  {
    while IFS=$'\t' read -r host domain; do
      [ -n "$host" ] && printf '%s\n' "$host"
    done < <(exposures_for_profile "$p")
    while IFS=$'\t' read -r host path svc _; do
      [ -n "$host" ] && [ "$host" != "*" ] && printf '%s\n' "$host"
    done < <(routes_for_profile "$p")
  } | sort -u
}

dns_ensure_profile() {
  local p=$1 host rc=0 total=0 tid
  if [ -z "$(profile_get "$p" CF_API_TOKEN)" ]; then
    tid=$(profile_tunnel_id "$p")
    warn "Profil '${p}' tidak punya CF_API_TOKEN. DNS tidak disinkronkan."
    say "  Buat CNAME manual: ${C_BOLD}<host>${C_RESET} -> ${C_BOLD}${tid:-<tunnel-id>}.cfargotunnel.com${C_RESET} (proxied)"
    return 4
  fi
  while IFS= read -r host; do
    [ -n "$host" ] || continue
    total=$((total + 1))
    dns_upsert "$p" "$host" || rc=1
  done < <(dns_hosts_for_profile "$p")
  [ "$total" -gt 0 ] || say "  ${C_DIM}Tidak ada eksposur/route untuk profil '${p}'.${C_RESET}"
  return "$rc"
}

unit_reserved_metrics_ports() {
  local skip_unit=$1 u
  [ -d "$SYSTEMD_DIR" ] || return 0
  for u in "$SYSTEMD_DIR"/cloudflared-*.service; do
    [ -f "$u" ] || continue
    [ "$(basename "$u")" = "$skip_unit" ] && continue
    grep -oP '(?<=--metrics 127\.0\.0\.1:)\d+' "$u" 2>/dev/null || true
  done
}

allocate_metrics_port() {
  local p=$1 idx base port cur_pid holder unit reserved
  unit=$(profile_unit "$p")
  idx=$(profile_list | awk -v p="$p" '$0 == p { print NR }')
  base=$((20260 + ${idx:-1}))
  cur_pid=$(systemctl show -p MainPID --value "$unit" 2>/dev/null || true)
  reserved=$(unit_reserved_metrics_ports "$unit")
  port=$base
  while :; do
    if printf '%s\n' "$reserved" | grep -qxF "$port"; then
      port=$((port + 1))
      continue
    fi
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
  local p=$1 bin dir unit tmp mode port proto extra_flags=""
  bin=$(cloudflared_bin) || return 1
  dir=$(profile_state_dir "$p")
  unit="$SYSTEMD_DIR/$(profile_unit "$p")"
  mode=$(profile_mode "$p")
  proto=$(profile_protocol "$p")
  port=$(allocate_metrics_port "$p")
  [ -n "$proto" ] && [ "$proto" != "auto" ] && extra_flags=" --protocol ${proto}"
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
      printf 'ExecStart=%s --no-autoupdate --metrics 127.0.0.1:%s%s tunnel run\n' "$bin" "$port" "$extra_flags"
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

yaml_str() {
  local s=$1
  s=${s//\'/\'\'}
  printf "'%s'" "$s"
}

emit_yaml_access() {
  local p=$1 host=$2 indent_n=$3 acc team aud auds a
  acc=$(access_for_host "$p" "$host" 2>/dev/null || true)
  [ -n "$acc" ] || return 0
  IFS=$'\t' read -r team aud <<< "$acc"
  [ -n "$team" ] && [ -n "$aud" ] || return 0
  ind "$indent_n" "access:"
  ind $((indent_n + 2)) "required: true"
  ind $((indent_n + 2)) "teamName: $(yaml_str "$team")"
  ind $((indent_n + 2)) "audTag:"
  IFS=',' read -ra auds <<< "$aud"
  for a in "${auds[@]}"; do
    a=$(printf '%s' "$a" | tr -d '[:space:]')
    [ -n "$a" ] && ind $((indent_n + 4)) "- $(yaml_str "$a")"
  done
}

emit_yaml_route() {
  local p=$1 host=$2 path=$3 svc=$4 notls=$5 hh=$6 osn=$7 http2=$8 capool=$9
  local has_or=0 acc=""
  [ "$notls" = "1" ] && has_or=1
  [ -n "$hh" ] && [ "$hh" != "-" ] && has_or=1
  [ -n "$osn" ] && [ "$osn" != "-" ] && has_or=1
  [ "$http2" = "1" ] && has_or=1
  [ -n "$capool" ] && [ "$capool" != "-" ] && has_or=1
  [ "$host" != "*" ] && acc=$(access_for_host "$p" "$host" 2>/dev/null || true)
  [ -n "$acc" ] && has_or=1

  if [ "$host" != "*" ]; then
    ind 2 "- hostname: $(yaml_str "$host")"
    [ -n "$path" ] && [ "$path" != "-" ] && ind 4 "path: $(yaml_str "$path")"
    ind 4 "service: $(yaml_str "$svc")"
  else
    ind 2 "- service: $(yaml_str "$svc")"
    [ -n "$path" ] && [ "$path" != "-" ] && ind 4 "path: $(yaml_str "$path")"
  fi
  if [ "$has_or" -eq 1 ]; then
    ind 4 "originRequest:"
    [ "$notls" = "1" ] && ind 6 "noTLSVerify: true"
    [ -n "$hh" ] && [ "$hh" != "-" ] && ind 6 "httpHostHeader: $(yaml_str "$hh")"
    [ -n "$osn" ] && [ "$osn" != "-" ] && ind 6 "originServerName: $(yaml_str "$osn")"
    [ "$http2" = "1" ] && ind 6 "http2Origin: true"
    [ -n "$capool" ] && [ "$capool" != "-" ] && ind 6 "caPool: $(yaml_str "$capool")"
    [ "$host" != "*" ] && emit_yaml_access "$p" "$host" 6
  fi
}

generate_config() {
  local p=$1 dir config tmp host domain out proto capool_ok=0 warned_ca=0
  local r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool
  dir=$(profile_state_dir "$p")
  config="$dir/config.yml"
  tmp="$dir/.config.yml.tmp.$$"
  proto=$(profile_protocol "$p")
  [ -f "$CA_POOL" ] && capool_ok=1
  mkdir -p "$dir" || return 1
  chmod 700 "$dir"
  {
    printf 'tunnel: %s\n' "$(profile_tunnel_id "$p")"
    printf 'credentials-file: %s/%s.json\n' "$dir" "$(profile_tunnel_id "$p")"
    printf 'protocol: %s\n' "${proto:-auto}"
    printf 'no-autoupdate: true\n'
    printf 'ingress:\n'
    # Emit path-specific custom routes first so catch-all rules do not shadow them
    while IFS=$'\t' read -r r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool; do
      [ -n "$r_host" ] || continue
      [ -n "$r_path" ] && [ "$r_path" != "-" ] || continue
      emit_yaml_route "$p" "$r_host" "$r_path" "$r_svc" "${r_notls:-0}" "${r_hh:--}" "${r_osn:--}" "${r_http2:-0}" "${r_capool:--}"
    done < <(routes_for_profile "$p")
    while IFS=$'\t' read -r r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool; do
      [ -n "$r_host" ] || continue
      [ -n "$r_path" ] && [ "$r_path" != "-" ] && continue
      emit_yaml_route "$p" "$r_host" "-" "$r_svc" "${r_notls:-0}" "${r_hh:--}" "${r_osn:--}" "${r_http2:-0}" "${r_capool:--}"
    done < <(routes_for_profile "$p")
    while IFS=$'\t' read -r host domain; do
      [ -n "$host" ] || continue
      ind 2 "- hostname: $(yaml_str "$host")"
      ind 4 "service: https://127.0.0.1:443"
      ind 4 "originRequest:"
      ind 6 "originServerName: $(yaml_str "$host")"
      if [ "$capool_ok" -eq 1 ]; then
        ind 6 "caPool: $(yaml_str "$CA_POOL")"
      else
        if [ "$warned_ca" -eq 0 ]; then
          warn "CA mkcert ($CA_POOL) belum ada; jalankan 'sudo setup_dev.sh --ssl' agar TLS origin tervalidasi."
          warned_ca=1
        fi
        ind 6 "noTLSVerify: true"
      fi
      ind 6 "connectTimeout: 10s"
      emit_yaml_access "$p" "$host" 6
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

route_rule_json() {
  local p=$1 host=$2 path=$3 svc=$4 notls=$5 hh=$6 osn=$7 http2=$8 capool=$9
  local acc team aud aud_json='[]' or_json='{}'
  [ "$notls" = "1" ] && or_json=$(printf '%s' "$or_json" | jq -c '. + {noTLSVerify: true}')
  [ -n "$hh" ] && [ "$hh" != "-" ] && or_json=$(printf '%s' "$or_json" | jq -c --arg v "$hh" '. + {httpHostHeader: $v}')
  [ -n "$osn" ] && [ "$osn" != "-" ] && or_json=$(printf '%s' "$or_json" | jq -c --arg v "$osn" '. + {originServerName: $v}')
  [ "$http2" = "1" ] && or_json=$(printf '%s' "$or_json" | jq -c '. + {http2Origin: true}')
  [ -n "$capool" ] && [ "$capool" != "-" ] && or_json=$(printf '%s' "$or_json" | jq -c --arg v "$capool" '. + {caPool: $v}')
  if [ "$host" != "*" ]; then
    acc=$(access_for_host "$p" "$host" 2>/dev/null || true)
    if [ -n "$acc" ]; then
      IFS=$'\t' read -r team aud <<< "$acc"
      aud_json=$(printf '%s' "$aud" | tr ',' '\n' | awk 'NF' | jq -Rc . | jq -cs .)
      or_json=$(printf '%s' "$or_json" | jq -c --arg t "$team" --argjson a "$aud_json" \
        '. + {access: {required: true, teamName: $t, audTag: $a}}')
    fi
  fi
  jq -nc \
    --arg h "$host" \
    --arg pa "$path" \
    --arg s "$svc" \
    --argjson or "$or_json" \
    '({service: $s, originRequest: $or}
      + (if $h != "*" then {hostname: $h} else {} end)
      + (if $pa != "" and $pa != "-" then {path: $pa} else {} end))'
}

generate_remote_ingress_json() {
  local p=$1 items="" r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool host domain
  local capool_val="-" notls_val="1"
  [ -f "$CA_POOL" ] && { capool_val="$CA_POOL"; notls_val="0"; }
  while IFS=$'\t' read -r r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool; do
    [ -n "$r_host" ] || continue
    [ -n "$r_path" ] && [ "$r_path" != "-" ] || continue
    items="${items}$(route_rule_json "$p" "$r_host" "$r_path" "$r_svc" "${r_notls:-0}" "${r_hh:--}" "${r_osn:--}" "${r_http2:-0}" "${r_capool:--}")"$'\n'
  done < <(routes_for_profile "$p")
  while IFS=$'\t' read -r r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool; do
    [ -n "$r_host" ] || continue
    [ -n "$r_path" ] && [ "$r_path" != "-" ] && continue
    items="${items}$(route_rule_json "$p" "$r_host" "-" "$r_svc" "${r_notls:-0}" "${r_hh:--}" "${r_osn:--}" "${r_http2:-0}" "${r_capool:--}")"$'\n'
  done < <(routes_for_profile "$p")
  while IFS=$'\t' read -r host domain; do
    [ -n "$host" ] || continue
    items="${items}$(route_rule_json "$p" "$host" "-" "https://127.0.0.1:443" "$notls_val" "-" "$host" "0" "$capool_val")"$'\n'
  done < <(exposures_for_profile "$p")
  items="${items}"'{"service":"http_status:404"}'$'\n'
  printf '%s' "$items" | jq -cs '{config: {ingress: .}}'
}

push_remote_config() {
  local p=$1 aid tid payload resp
  CF_PROFILE="$p"
  [ -n "$(profile_get "$p" CF_API_TOKEN)" ] || {
    err "Sinkronisasi --remote membutuhkan CF_API_TOKEN pada profil '${p}'."
    return 1
  }
  aid=$(ensure_account_id "$p") || { err "Account ID tidak tersedia untuk profil '${p}'."; return 1; }
  tid=$(ensure_tunnel_id "$p") || { err "Tunnel ID tidak tersedia untuk profil '${p}'."; return 1; }
  payload=$(generate_remote_ingress_json "$p") || return 1
  resp=$(api_request PUT "/accounts/${aid}/cfd_tunnel/${tid}/configurations" "$payload") || {
    err "Gagal mengirim konfigurasi remote untuk '${p}'."
    return 1
  }
  if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
    ok "Ingress remote profil '${p}' tersinkron ke Cloudflare API ($(exposure_count "$p") eksposur, $(route_count "$p") route)."
    return 0
  fi
  err "Gagal sinkronisasi ingress remote '${p}': $(printf '%s' "$resp" | api_error_text)"
  return 1
}

sync_profile() {
  local p=$1 remote=${2:-0} mode tid dir config changed=0 total_rules
  mode=$(profile_mode "$p")
  if [ "$mode" != "local" ]; then
    if write_unit "$p"; then
      systemctl daemon-reload 2>/dev/null || true
      changed=1
    fi
    if [ "$changed" -eq 1 ] && service_state "$p" | grep -q '^active$'; then
      systemctl restart "$(profile_unit "$p")" 2>/dev/null \
        && ok "Service '$(profile_unit "$p")' di-restart." \
        || warn "Gagal restart '$(profile_unit "$p")'."
    fi
    if [ "$remote" -eq 1 ]; then
      push_remote_config "$p" || return 1
      return 0
    fi
    warn "Profil '${p}' memakai mode token (remotely-managed); ingress diatur di dashboard (atau jalankan: cf-tunnel sync --remote --profile ${p})."
    return 0
  fi
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
  total_rules=$(( $(exposure_count "$p") + $(route_count "$p") ))
  ok "Ingress profil '${p}' tersinkron (${total_rules} aturan)."
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
  local p="" name cert mode tid aid unit state port ready rc_count resp
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
  mode=$(profile_mode "$p")
  cert=$(profile_cert "$p")
  name=$(profile_tunnel_name "$p")
  if [ "$mode" = "local" ] && [ -f "$cert" ]; then
    "$CF_BIN" --origincert "$cert" tunnel info "$name"
    return $?
  fi
  tid=$(ensure_tunnel_id "$p" 2>/dev/null || profile_tunnel_id "$p")
  aid=$(ensure_account_id "$p" 2>/dev/null || profile_get "$p" CF_ACCOUNT_ID)
  unit=$(profile_unit "$p")
  state=$(service_state "$p")
  say "Profil     : $p"
  say "Mode       : $mode"
  say "Tunnel     : ${name:-"-"} (${tid:-"-"})"
  say "Account ID : ${aid:-"-"}"
  say "Protocol   : $(profile_protocol "$p")"
  say "Service    : $unit ($state)"
  if [ "$state" = "active" ]; then
    port=$(systemctl show -p ExecStart --value "$unit" 2>/dev/null | grep -oP '(?<=--metrics 127\.0\.0\.1:)\d+' || true)
    if [ -n "$port" ]; then
      ready=$(curl -s --max-time 2 "http://127.0.0.1:${port}/ready" 2>/dev/null || true)
      rc_count=$(printf '%s' "$ready" | jq -r '.readyConnections // empty' 2>/dev/null || true)
      [ -n "$rc_count" ] && say "Koneksi    : ${rc_count} readyConnections (metrics :${port})"
    fi
  fi
  if [ -n "$(profile_get "$p" CF_API_TOKEN)" ] && [ -n "$aid" ] && [ -n "$tid" ]; then
    CF_PROFILE="$p"
    resp=$(api_request GET "/accounts/${aid}/cfd_tunnel/${tid}" 2>/dev/null || true)
    if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
      say "CF Status  : $(printf '%s' "$resp" | jq -r '.result.status // "-"')"
      local conns
      conns=$(printf '%s' "$resp" | jq -r '.result.connections[]? | "  - " + (.id // "-") + " (" + (.colo_name // "-") + ", " + (.origin_ip // "-") + ")"')
      if [ -n "$conns" ]; then
        say "Connectors :"
        printf '%s\n' "$conns"
      fi
    fi
  elif [ "$mode" = "local" ]; then
    die "Certifikat origin tidak ada: $cert"
  fi
}

cmd_account_add() {
  local p="" zone="" mode="local" name="" api_token="" tunnel_token="" account_id="" protocol="auto"
  local no_login=0 no_start=0 mode_explicit=0 non_interactive=0
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
      --protocol) protocol=${2:-}; shift 2 ;;
      --protocol=*) protocol=${1#*=}; shift ;;
      --no-login) no_login=1; non_interactive=1; shift ;;
      --no-start) no_start=1; shift ;;
      --non-interactive|--yes|-y) non_interactive=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  [ -n "$p" ] || die "Penggunaan: cf-tunnel account add <profile> [--zone example.com] [--mode local|token]"
  valid_profile_name "$p" || die "Nama profil tidak valid: '$p'."
  profile_exists "$p" && die "Profil '$p' sudah ada."
  case "$mode" in local|token) ;; *) die "Mode harus 'local' atau 'token'." ;; esac
  valid_protocol "$protocol" || die "Protocol harus 'auto', 'quic', atau 'http2'."

  if [ -t 0 ] && [ "$non_interactive" -eq 0 ]; then
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

  mkdir -p "$CF_ACCOUNTS" "$CF_ROUTES" "$CF_ACCESS"
  chmod 700 "$CF_ETC" "$CF_ACCOUNTS" "$CF_ROUTES" "$CF_ACCESS" 2>/dev/null || true
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
      printf 'CF_PROTOCOL=%s\n' "$protocol"
      printf 'CF_API_TOKEN=%s\n' "$api_token"
    } > "$(account_file "$p")"
  )
  chmod 600 "$(account_file "$p")"
  ok "Profil '${p}' dibuat (zone: ${zone}, mode: ${mode}, protocol: ${protocol})."
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
    say "Catatan: ingress mode token diatur di dashboard Cloudflare (atau via: sudo cf-tunnel sync --remote --profile ${p})."
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

cmd_account_update() {
  local p="" zone="" api_token="" tunnel_token="" account_id="" protocol=""
  local set_zone=0 set_api=0 set_tt=0 set_aid=0 set_proto=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --zone) zone=${2:-}; set_zone=1; shift 2 ;;
      --zone=*) zone=${1#*=}; set_zone=1; shift ;;
      --api-token) api_token=${2:-}; set_api=1; shift 2 ;;
      --api-token=*) api_token=${1#*=}; set_api=1; shift ;;
      --tunnel-token) tunnel_token=${2:-}; set_tt=1; shift 2 ;;
      --tunnel-token=*) tunnel_token=${1#*=}; set_tt=1; shift ;;
      --account-id) account_id=${2:-}; set_aid=1; shift 2 ;;
      --account-id=*) account_id=${1#*=}; set_aid=1; shift ;;
      --protocol) protocol=${2:-}; set_proto=1; shift 2 ;;
      --protocol=*) protocol=${1#*=}; set_proto=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$p" ] && p=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  profile_require "$p"
  if [ "$set_zone" -eq 0 ] && [ "$set_api" -eq 0 ] && [ "$set_tt" -eq 0 ] && [ "$set_aid" -eq 0 ] && [ "$set_proto" -eq 0 ]; then
    if [ -t 0 ]; then
      local cur_z cur_pr
      cur_z=$(profile_zone "$p")
      cur_pr=$(profile_protocol "$p")
      read -rp "Zone baru [Enter tetap: ${cur_z}]: " zone
      [ -n "$zone" ] && set_zone=1
      read -rp "Protocol baru (auto/quic/http2) [Enter tetap: ${cur_pr}]: " protocol
      [ -n "$protocol" ] && set_proto=1
      read -rsp "API Token baru [Enter tetap tidak diubah]: " api_token; echo
      [ -n "$api_token" ] && set_api=1
      if [ "$(profile_mode "$p")" = "token" ]; then
        read -rsp "Tunnel Token baru [Enter tetap tidak diubah]: " tunnel_token; echo
        [ -n "$tunnel_token" ] && set_tt=1
      fi
    else
      die "Penggunaan: cf-tunnel account update <profile> [--zone ...] [--api-token ...] [--tunnel-token ...] [--account-id ...] [--protocol auto|quic|http2]"
    fi
  fi
  if [ "$set_proto" -eq 1 ]; then
    valid_protocol "$protocol" || die "Protocol harus 'auto', 'quic', atau 'http2'."
    profile_set "$p" CF_PROTOCOL "$protocol"
    ok "Protocol profil '${p}' diubah ke '${protocol}'."
  fi
  if [ "$set_zone" -eq 1 ]; then
    [ -n "$zone" ] || die "Zone tidak boleh kosong."
    profile_set "$p" CF_ZONE "$zone"
    profile_set "$p" CF_ZONE_ID ""
    ok "Zone profil '${p}' diubah ke '${zone}'."
  fi
  if [ "$set_aid" -eq 1 ]; then
    profile_set "$p" CF_ACCOUNT_ID "$account_id"
    ok "Account ID profil '${p}' diperbarui."
  fi
  if [ "$set_api" -eq 1 ]; then
    profile_set "$p" CF_API_TOKEN "$api_token"
    ok "API Token profil '${p}' diperbarui."
  fi
  if [ "$set_tt" -eq 1 ]; then
    [ "$(profile_mode "$p")" = "token" ] || die "--tunnel-token hanya berlaku untuk profil mode 'token'."
    tunnel_token=$(printf '%s' "$tunnel_token" | awk '{print $NF}' | tr -d '\r\n"')
    [ -n "$tunnel_token" ] || die "Tunnel Token tidak boleh kosong."
    local envf parsed_tid parsed_aid
    envf=$(profile_env_file "$p")
    ( umask 077; printf 'TUNNEL_TOKEN=%s\n' "$tunnel_token" > "$envf" )
    chmod 600 "$envf"
    parsed_tid=$(printf '%s' "$tunnel_token" | base64 -d 2>/dev/null | jq -r '.t // empty' 2>/dev/null || true)
    parsed_aid=$(printf '%s' "$tunnel_token" | base64 -d 2>/dev/null | jq -r '.a // empty' 2>/dev/null || true)
    [ -n "$parsed_tid" ] && profile_set "$p" CF_TUNNEL_ID "$parsed_tid"
    [ -n "$parsed_aid" ] && profile_set "$p" CF_ACCOUNT_ID "$parsed_aid"
    ok "Tunnel Token profil '${p}' diperbarui."
  fi
  if [ -n "$(profile_get "$p" CF_API_TOKEN)" ] && { [ "$set_zone" -eq 1 ] || [ "$set_api" -eq 1 ]; }; then
    profile_set "$p" CF_ZONE_ID ""
    ensure_zone_id "$p" >/dev/null && ok "Zone ID ter-cache." \
      || warn "Token/zone belum tervalidasi."
  fi
  if [ "$set_proto" -eq 1 ] || [ "$set_tt" -eq 1 ]; then
    require_cloudflared
    sync_profile "$p" || true
  fi
}

cmd_account_list() {
  local p zone mode name tid unit state proto
  printf "${C_BOLD}%-12s | %-22s | %-6s | %-6s | %-26s | %-12s${C_RESET}\n" \
    "PROFILE" "ZONE" "MODE" "PROTO" "TUNNEL" "SERVICE"
  say "-------------+------------------------+--------+--------+----------------------------+-------------"
  local count=0
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    count=$((count + 1))
    zone=$(profile_zone "$p")
    mode=$(profile_mode "$p")
    proto=$(profile_protocol "$p")
    name=$(profile_tunnel_name "$p")
    tid=$(profile_tunnel_id "$p")
    unit=$(profile_unit "$p")
    state=$(service_state "$p")
    printf "%-12s | %-22s | %-6s | %-6s | %-26s | %-12s\n" \
      "$p" "${zone:--}" "${mode:--}" "${proto:-auto}" "${name:--}${tid:+ [${tid:0:8}]}" "$state"
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
  local exp_c rt_c
  exp_c=$(exposure_count "$p")
  rt_c=$(route_count "$p")
  if [ "$exp_c" -gt 0 ]; then
    die "Profil '$p' masih dipakai ${exp_c} eksposur. Hapus dulu: sudo dev-site unexpose <domain> --profile $p"
  fi
  if [ "$rt_c" -gt 0 ]; then
    die "Profil '$p' masih punya ${rt_c} custom route. Hapus dulu: sudo cf-tunnel route rm --profile $p <host>"
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
  rm -f "$(account_file "$p")" "$(profile_env_file "$p")" "$(routes_file "$p")" "$(access_file "$p")"
  ok "Profil '${p}' dihapus. Record DNS tidak dihapus otomatis."
}

cmd_sync() {
  local p="" all=0 remote=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --all) all=1; shift ;;
      --remote) remote=1; shift ;;
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
      sync_profile "$p" "$remote" || rc=1
    done < <(profile_list)
  else
    profile_require "$p"
    sync_profile "$p" "$remote" || rc=1
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
      dns_upsert "$p" "$host" "$force"
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
  check_accounts_readable || return 1
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
  printf "${C_BOLD}%-10s | %-20s | %-6s | %-10s | %-7s | %-9s | %-6s | %s${C_RESET}\n" \
    "PROFILE" "ZONE" "MODE" "SERVICE" "CONNECT" "EXPOSURES" "ROUTES" "TUNNEL"
  say "-----------+----------------------+--------+------------+---------+-----------+--------+--------------------------"
  local zone mode state conn exp rt name
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
    rt=$(route_count "$p")
    printf "%-10s | %-20s | %-6s | %-10s | %-7s | %-9s | %-6s | %s\n" \
      "$p" "${zone:--}" "${mode:--}" "$state" "$conn" "$exp" "$rt" "${name:--}"
  done
}

cmd_exposures() {
  local host domain profile mode state f
  check_accounts_readable || return 1
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

has_unsafe_field() {
  case "${1:-}" in
    *$'\t'*|*$'\n'*|*$'\r'*) return 0 ;;
    *) return 1 ;;
  esac
}

require_safe_fields() {
  local v
  for v in "$@"; do
    has_unsafe_field "$v" && die "Input mengandung karakter kontrol tidak valid."
  done
}

validate_service_target() {
  local s=${1:-}
  case "$s" in
    http://*|https://*|tcp://*|ssh://*|rdp://*|smb://*|unix://*|bastion|hello_world|http_status:[0-9][0-9][0-9])
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

route_conflict_check() {
  local p=$1 host=$2 path=${3:--} f dom hp op r_host r_path
  [ "$host" = "*" ] && return 0
  if [ "$path" = "-" ]; then
    while IFS= read -r f; do
      [ -f "$f" ] || continue
      dom=$(basename "$f" .conf)
      while IFS=$'\t' read -r hp op; do
        if [ "$hp" = "$host" ]; then
          die "Host '${host}' sudah terdaftar di dev-site expose (${dom}, profil ${op}). Gunakan --path atau unexpose dulu."
        fi
      done < <(exposures_for_domain "$dom")
    done < <(exposure_files)
  fi
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    op=$(basename "$f" .conf)
    [ "$op" = "$p" ] && continue
    while IFS=$'\t' read -r r_host r_path _; do
      if [ "$r_host" = "$host" ]; then
        if [ "$path" = "-" ] || [ "$r_path" = "-" ] || [ "$r_path" = "$path" ]; then
          die "Host '${host}' sudah dipakai custom route di profil '${op}'."
        fi
      fi
    done < <(routes_for_profile "$op")
  done < <(route_files)
}

cmd_route() {
  local action=${1:-list}
  [ $# -gt 0 ] && shift || true
  case "$action" in
    list|ls)
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
      check_accounts_readable || return 1
      local -a targets=()
      if [ "$all" -eq 1 ] || [ -z "$p" ]; then
        while IFS= read -r p; do [ -n "$p" ] && targets+=("$p"); done < <(profile_list)
      else
        profile_require "$p"
        targets=("$p")
      fi
      printf "${C_BOLD}%-10s | %-28s | %-12s | %-28s | %s${C_RESET}\n" \
        "PROFILE" "HOST" "PATH" "SERVICE" "OPTIONS"
      say "-----------+------------------------------+--------------+------------------------------+------------------"
      local t r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool opts count=0
      for t in "${targets[@]}"; do
        while IFS=$'\t' read -r r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool; do
          [ -n "$r_host" ] || continue
          opts=""
          [ "${r_notls:-0}" = "1" ] && opts="${opts}noTLSVerify "
          [ -n "${r_hh:-}" ] && [ "$r_hh" != "-" ] && opts="${opts}host=${r_hh} "
          [ -n "${r_osn:-}" ] && [ "$r_osn" != "-" ] && opts="${opts}sni=${r_osn} "
          [ "${r_http2:-0}" = "1" ] && opts="${opts}http2 "
          [ -n "${r_capool:-}" ] && [ "$r_capool" != "-" ] && opts="${opts}caPool "
          printf "%-10s | %-28s | %-12s | %-28s | %s\n" \
            "$t" "$r_host" "${r_path:--}" "$r_svc" "${opts:--}"
          count=$((count + 1))
        done < <(routes_for_profile "$t")
      done
      [ "$count" -gt 0 ] || say "Belum ada custom route. Tambah: sudo cf-tunnel route add --profile <p> <host> <service>"
      ;;
    add)
      local p="" host="" service="" path="-" notls=0 hh="-" osn="-" http2=0 capool="-"
      local allow_deep=0 skip_zone=0 pos=0
      while [ $# -gt 0 ]; do
        case "$1" in
          --profile) p=${2:-}; shift 2 ;;
          --profile=*) p=${1#*=}; shift ;;
          --path) path=${2:-}; shift 2 ;;
          --path=*) path=${1#*=}; shift ;;
          --no-tls-verify) notls=1; shift ;;
          --host-header) hh=${2:-}; shift 2 ;;
          --host-header=*) hh=${1#*=}; shift ;;
          --origin-server-name) osn=${2:-}; shift 2 ;;
          --origin-server-name=*) osn=${1#*=}; shift ;;
          --ca-pool) capool=${2:-}; shift 2 ;;
          --ca-pool=*) capool=${1#*=}; shift ;;
          --http2) http2=1; shift ;;
          --allow-deep) allow_deep=1; shift ;;
          --skip-zone-check) skip_zone=1; shift ;;
          -*) die "Opsi tidak dikenal: $1" ;;
          *)
            case "$pos" in
              0) host=$1 ;;
              1) service=$1 ;;
              *) die "Argumen tidak dikenal: $1" ;;
            esac
            pos=$((pos + 1))
            shift
            ;;
        esac
      done
      need_root
      profile_require "$p"
      [ -n "$host" ] && [ -n "$service" ] || die "Penggunaan: cf-tunnel route add --profile <p> <host> <service> [--path <regex>] [--no-tls-verify] [--host-header <h>] [--origin-server-name <s>] [--ca-pool <pem>] [--http2] [--allow-deep]"
      require_safe_fields "$host" "$path" "$service" "$hh" "$osn" "$capool"
      validate_service_target "$service" || die "Service tidak valid: '$service'. Gunakan http://, https://, tcp://, ssh://, rdp://, smb://, unix://, bastion, hello_world, atau http_status:NNN."
      [ "$capool" = "-" ] || [ -f "$capool" ] || die "File caPool tidak ditemukan: $capool"
      if [ "$host" != "*" ] && [ "$skip_zone" -ne 1 ]; then
        local zone zrc=0
        zone=$(profile_zone "$p")
        host_under_zone "$host" "$zone" "$allow_deep" || zrc=$?
        if [ "$zrc" -eq 2 ]; then
          die "Host '${host}' adalah subdomain bertingkat di bawah '${zone}' (Universal SSL gratis hanya *.${zone}). Tambahkan --allow-deep jika memakai Total TLS / ACM."
        elif [ "$zrc" -ne 0 ]; then
          die "Host '${host}' bukan bagian dari zone '${zone}' (profil ${p}). Gunakan --skip-zone-check bila perlu."
        fi
        [ "$host" = "$zone" ] && say "${C_CYAN}[i]${C_RESET} Apex domain '${host}' akan menggunakan CNAME flattening Cloudflare."
      fi
      route_conflict_check "$p" "$host" "$path"
      mkdir -p "$CF_ROUTES"
      chmod 700 "$CF_ROUTES"
      local rf tmp line path_note=""
      rf=$(routes_file "$p")
      tmp=$(mktemp) || return 1
      if [ -f "$rf" ]; then
        awk -F'\t' -v h="$host" -v pa="$path" '!($1 == h && $2 == pa)' "$rf" > "$tmp"
      fi
      line="${host}"$'\t'"${path}"$'\t'"${service}"$'\t'"${notls}"$'\t'"${hh}"$'\t'"${osn}"$'\t'"${http2}"$'\t'"${capool}"
      printf '%s\n' "$line" >> "$tmp"
      chmod 600 "$tmp"
      mv -f "$tmp" "$rf"
      [ "$path" != "-" ] && path_note=" (path=${path})"
      ok "Route ditambahkan: ${host}${path_note} -> ${service} (profil ${p})"
      require_cloudflared
      sync_profile "$p" || true
      if [ "$host" != "*" ]; then
        if [ -n "$(profile_get "$p" CF_API_TOKEN)" ]; then
          dns_upsert "$p" "$host" || true
        else
          local tid_hint
          tid_hint=$(profile_tunnel_id "$p")
          say "  Buat CNAME manual: ${host} -> ${tid_hint:-<tunnel-id>}.cfargotunnel.com (proxied)"
        fi
      fi
      ;;
    rm|remove|del)
      local p="" host="" path="" keep_dns=0 pos=0
      while [ $# -gt 0 ]; do
        case "$1" in
          --profile) p=${2:-}; shift 2 ;;
          --profile=*) p=${1#*=}; shift ;;
          --path) path=${2:-}; shift 2 ;;
          --path=*) path=${1#*=}; shift ;;
          --keep-dns) keep_dns=1; shift ;;
          -*) die "Opsi tidak dikenal: $1" ;;
          *)
            case "$pos" in
              0) host=$1 ;;
              *) die "Argumen tidak dikenal: $1" ;;
            esac
            pos=$((pos + 1))
            shift
            ;;
        esac
      done
      need_root
      profile_require "$p"
      [ -n "$host" ] || die "Penggunaan: cf-tunnel route rm --profile <p> <host> [--path <regex>]"
      local rf tmp before after
      rf=$(routes_file "$p")
      [ -f "$rf" ] || die "Profil '${p}' belum punya custom route."
      before=$(wc -l < "$rf" | tr -d '[:space:]')
      tmp=$(mktemp) || return 1
      if [ -n "$path" ]; then
        awk -F'\t' -v h="$host" -v pa="$path" '!($1 == h && $2 == pa)' "$rf" > "$tmp"
      else
        awk -F'\t' -v h="$host" '$1 != h' "$rf" > "$tmp"
      fi
      after=$(wc -l < "$tmp" | tr -d '[:space:]')
      if [ "$before" = "$after" ]; then
        rm -f "$tmp"
        die "Route '${host}' tidak ditemukan di profil '${p}'."
      fi
      if [ "$after" -eq 0 ]; then
        rm -f "$tmp" "$rf"
      else
        chmod 600 "$tmp"
        mv -f "$tmp" "$rf"
      fi
      ok "Route '${host}' dihapus dari profil '${p}'."
      require_cloudflared
      sync_profile "$p" || true
      if [ "$keep_dns" -eq 0 ] && [ "$host" != "*" ]; then
        local still_used=0
        if routes_for_profile "$p" | awk -F'\t' -v h="$host" '$1 == h { found=1 } END { exit !found }'; then
          still_used=1
        fi
        if exposures_for_profile "$p" | awk -F'\t' -v h="$host" '$1 == h { found=1 } END { exit !found }'; then
          still_used=1
        fi
        if [ "$still_used" -eq 0 ] && [ -n "$(profile_get "$p" CF_API_TOKEN)" ]; then
          dns_delete "$p" "$host" || true
        fi
      fi
      ;;
    *) die "Aksi route tidak dikenal: $action (gunakan list|add|rm)" ;;
  esac
}

cmd_quick() {
  local sub=${1:-}
  case "$sub" in
    help|-h|--help)
      say "Penggunaan: cf-tunnel quick <port|url|vhost-proxy> [--no-tls-verify] [--ca-pool <pem>] [--bg] [--name <label>]"
      say "            cf-tunnel quick <status|logs [label]|stop [label|--all]>"
      return 0
      ;;
    status|ls)
      shift || true
      local qdir f label pid url log state pub
      qdir=$(quick_state_dir)
      printf "${C_BOLD}%-16s | %-8s | %-8s | %-30s | %s${C_RESET}\n" "LABEL" "STATUS" "PID" "TARGET" "PUBLIC URL"
      say "-----------------+----------+----------+--------------------------------+----------------------------------------"
      local count=0
      if [ -d "$qdir" ]; then
        for f in "$qdir"/*.pid; do
          [ -f "$f" ] || continue
          count=$((count + 1))
          label=$(basename "$f" .pid)
          pid=$(cat "$f" 2>/dev/null || true)
          url=$(cat "$qdir/${label}.target" 2>/dev/null || echo "-")
          log="$qdir/${label}.log"
          if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            state="running"
          else
            state="stopped"
          fi
          pub=$(grep -oE 'https://[a-zA-Z0-9-]+\.trycloudflare\.com' "$log" 2>/dev/null | head -n1 || true)
          printf "%-16s | %-8s | %-8s | %-30s | %s\n" "$label" "$state" "${pid:--}" "$url" "${pub:--}"
        done
      fi
      [ "$count" -gt 0 ] || say "Belum ada Quick Tunnel background."
      return 0
      ;;
    logs)
      shift || true
      local label=${1:-default} qdir log
      qdir=$(quick_state_dir)
      log="$qdir/${label}.log"
      [ -f "$log" ] || die "Log Quick Tunnel '${label}' tidak ditemukan ($log)."
      cat "$log"
      return 0
      ;;
    stop)
      shift || true
      local target=${1:-} all=0 qdir f label pid stopped=0
      [ "$target" = "--all" ] && { all=1; target=""; }
      qdir=$(quick_state_dir)
      [ -d "$qdir" ] || { say "Tidak ada Quick Tunnel berjalan."; return 0; }
      for f in "$qdir"/*.pid; do
        [ -f "$f" ] || continue
        label=$(basename "$f" .pid)
        if [ "$all" -eq 0 ] && [ -n "$target" ] && [ "$label" != "$target" ]; then
          continue
        fi
        pid=$(cat "$f" 2>/dev/null || true)
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
          kill "$pid" 2>/dev/null || true
          ok "Quick Tunnel '${label}' (PID ${pid}) dihentikan."
        else
          say "Quick Tunnel '${label}' sudah berhenti."
        fi
        rm -f "$f" "$qdir/${label}.target"
        stopped=$((stopped + 1))
        [ "$all" -eq 0 ] && [ -z "$target" ] && break
      done
      [ "$stopped" -gt 0 ] || say "Quick Tunnel '${target:-default}' tidak ditemukan."
      return 0
      ;;
  esac

  local target="" notls=0 bg=0 label="" capool=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --no-tls-verify) notls=1; shift ;;
      --bg|--background) bg=1; shift ;;
      --name|--label) label=${2:-}; shift 2 ;;
      --name=*|--label=*) label=${1#*=}; shift ;;
      --ca-pool) capool=${2:-}; shift 2 ;;
      --ca-pool=*) capool=${1#*=}; shift ;;
      -h|--help)
        say "Penggunaan: cf-tunnel quick <port|url|vhost-proxy> [--no-tls-verify] [--ca-pool <pem>] [--bg] [--name <label>]"
        say "            cf-tunnel quick <status|logs [label]|stop [label|--all]>"
        return 0
        ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$target" ] && target=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  [ -n "$target" ] || die "Masukkan target port, URL, atau domain vhost proxy (contoh: cf-tunnel quick 3000)."
  require_cloudflared

  local url=""
  if [[ "$target" =~ ^[0-9]+$ ]]; then
    url="http://127.0.0.1:${target}"
    [ -n "$label" ] || label="port-${target}"
  elif [[ "$target" =~ ^https?:// ]]; then
    url="$target"
    [ -n "$label" ] || label="quick-$(printf '%s' "$target" | tr -cs 'a-zA-Z0-9' '-' | sed 's/^-//;s/-$//' | cut -c1-20)"
  else
    local vconf="$NGINX_SITES_DIR/${target}.conf"
    if [ -f "$vconf" ]; then
      if grep -q "proxy_pass" "$vconf"; then
        url=$(awk '/^[[:space:]]*proxy_pass[[:space:]]+/ { gsub(/;$/, "", $2); print $2; exit }' "$vconf")
        [ -n "$label" ] || label=$(printf '%s' "$target" | tr -cs 'a-zA-Z0-9' '-' | cut -c1-24)
        say "Menggunakan upstream proxy dari ${target}: ${url}"
      else
        die "Vhost '${target}' adalah PHP/Nginx non-proxy. Quick Tunnel (trycloudflare.com) tidak dapat mengirim Host header kustom; gunakan 'sudo dev-site expose ${target} <host> --profile <p>' atau jalankan quick tunnel ke port langsung."
      fi
    else
      die "Target tidak dikenal: '$target'. Masukkan nomor port (mis. 3000), URL http(s)://..., atau domain dev-site proxy."
    fi
  fi

  local -a args=("tunnel" "--no-autoupdate" "--url" "$url")
  [ "$notls" -eq 1 ] && args+=("--no-tls-verify")
  if [ -n "$capool" ]; then
    [ -f "$capool" ] || die "File caPool tidak ditemukan: $capool"
    args+=("--origin-ca-pool" "$capool")
  fi

  if [ "$bg" -eq 0 ]; then
    say "Menjalankan Quick Tunnel ke ${url} (Ctrl+C untuk berhenti)..."
    "$CF_BIN" "${args[@]}"
    return $?
  fi

  valid_profile_name "$label" || label="quick-$(date +%s)"
  local qdir pidfile logfile targetfile pid pub="" i=0
  qdir=$(quick_state_dir)
  mkdir -p "$qdir"
  chmod 700 "$qdir"
  pidfile="$qdir/${label}.pid"
  logfile="$qdir/${label}.log"
  targetfile="$qdir/${label}.target"
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile" 2>/dev/null || true)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      die "Quick Tunnel '${label}' sudah aktif (PID ${pid}). Hentikan dulu: cf-tunnel quick stop ${label}"
    fi
  fi
  printf '%s\n' "$url" > "$targetfile"
  nohup "$CF_BIN" "${args[@]}" > "$logfile" 2>&1 < /dev/null &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  while [ "$i" -lt 40 ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      err "Quick Tunnel gagal dijalankan:"
      tail -n 15 "$logfile" >&2 || true
      rm -f "$pidfile"
      return 1
    fi
    pub=$(grep -oE 'https://[a-zA-Z0-9-]+\.trycloudflare\.com' "$logfile" 2>/dev/null | head -n1 || true)
    if [ -n "$pub" ]; then
      break
    fi
    sleep 0.25
    i=$((i + 1))
  done
  ok "Quick Tunnel aktif di background (label: ${label}, PID: ${pid})"
  say "  Target     : $url"
  say "  Public URL : ${pub:-"(menunggu URL di log: cf-tunnel quick logs ${label})"}"
  say "  Stop       : cf-tunnel quick stop ${label}"
}

valid_cidr() {
  local c=${1:-} ip prefix o1 o2 o3 o4
  [[ "$c" == */* ]] || return 1
  ip=${c%/*}
  prefix=${c#*/}
  [[ "$prefix" =~ ^[0-9]+$ ]] || return 1
  if [[ "$ip" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]]; then
    o1=${BASH_REMATCH[1]} o2=${BASH_REMATCH[2]} o3=${BASH_REMATCH[3]} o4=${BASH_REMATCH[4]}
    [ "$o1" -le 255 ] && [ "$o2" -le 255 ] && [ "$o3" -le 255 ] && [ "$o4" -le 255 ] && [ "$prefix" -le 32 ]
    return $?
  elif [[ "$ip" == *:* ]] && [[ "$ip" =~ ^[0-9a-fA-F:]+$ ]] && [ "$prefix" -le 128 ]; then
    return 0
  fi
  return 1
}

cmd_cidr() {
  local action=${1:-list}
  [ $# -gt 0 ] && shift || true
  local p="" cidr="" vnet="" comment="" all=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --vnet) vnet=${2:-}; shift 2 ;;
      --vnet=*) vnet=${1#*=}; shift ;;
      --comment) comment=${2:-}; shift 2 ;;
      --comment=*) comment=${1#*=}; shift ;;
      --all) all=1; shift ;;
      -*) die "Opsi tidak dikenal: $1" ;;
      *) [ -z "$cidr" ] && cidr=$1 || die "Argumen tidak dikenal: $1"; shift ;;
    esac
  done
  need_root
  profile_require "$p"
  require_cloudflared
  CF_PROFILE="$p"
  local mode cert tid aid resp
  mode=$(profile_mode "$p")
  cert=$(profile_cert "$p")
  tid=$(ensure_tunnel_id "$p" 2>/dev/null || profile_tunnel_id "$p")
  aid=$(ensure_account_id "$p" 2>/dev/null || profile_get "$p" CF_ACCOUNT_ID)

  case "$action" in
    list|show|ls)
      if [ -n "$(profile_get "$p" CF_API_TOKEN)" ] && [ -n "$aid" ]; then
        resp=$(api_request GET "/accounts/${aid}/teamnet/routes?is_deleted=false") || die "Gagal membaca CIDR routes."
        printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1 \
          || die "Gagal membaca CIDR routes: $(printf '%s' "$resp" | api_error_text)"
        printf "${C_BOLD}%-36s | %-20s | %-20s | %s${C_RESET}\n" "ROUTE ID" "CIDR" "TUNNEL" "COMMENT"
        say "-------------------------------------+----------------------+----------------------+------------------"
        if [ "$all" -eq 1 ] || [ -z "$tid" ]; then
          printf '%s' "$resp" | jq -r '.result[]? | [.id, .network, (.tunnel_name // .tunnel_id // "-"), (.comment // "-")] | @tsv' \
            | awk -F'\t' '{ printf "%-36s | %-20s | %-20s | %s\n", $1, $2, $3, $4 }'
        else
          printf '%s' "$resp" | jq -r --arg tid "$tid" '.result[]? | select(.tunnel_id == $tid) | [.id, .network, (.tunnel_name // .tunnel_id // "-"), (.comment // "-")] | @tsv' \
            | awk -F'\t' '{ printf "%-36s | %-20s | %-20s | %s\n", $1, $2, $3, $4 }'
        fi
      elif [ "$mode" = "local" ] && [ -f "$cert" ]; then
        "$CF_BIN" --origincert "$cert" tunnel route ip show
      else
        die "Menampilkan route CIDR membutuhkan cert.pem (mode local) atau CF_API_TOKEN + Account ID."
      fi
      ;;
    add)
      valid_cidr "$cidr" || die "CIDR tidak valid: '$cidr' (contoh: 10.0.0.0/24 atau 192.168.1.0/24)."
      [ -n "$tid" ] || die "Tunnel ID profil '${p}' belum tersedia."
      if [ -n "$(profile_get "$p" CF_API_TOKEN)" ] && [ -n "$aid" ]; then
        local data
        data=$(jq -nc --arg t "$tid" --arg n "$cidr" --arg v "$vnet" --arg c "$comment" \
          '({tunnel_id: $t, network: $n}
            + (if $v != "" then {virtual_network_id: $v} else {} end)
            + (if $c != "" then {comment: $c} else {} end))')
        resp=$(api_request POST "/accounts/${aid}/teamnet/routes" "$data") || die "Gagal menambah CIDR."
        if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
          ok "CIDR route ditambahkan: ${cidr} -> tunnel ${tid}"
        else
          die "Gagal menambah CIDR: $(printf '%s' "$resp" | api_error_text)"
        fi
      elif [ "$mode" = "local" ] && [ -f "$cert" ]; then
        local -a ip_args=("--origincert" "$cert" "tunnel" "route" "ip" "add")
        [ -n "$vnet" ] && ip_args+=("--vnet" "$vnet")
        ip_args+=("$cidr" "$tid")
        "$CF_BIN" "${ip_args[@]}"
      else
        die "Menambah CIDR membutuhkan cert.pem (mode local) atau CF_API_TOKEN + Account ID."
      fi
      ;;
    rm|remove|del)
      valid_cidr "$cidr" || die "CIDR tidak valid: '$cidr'."
      [ -n "$(profile_get "$p" CF_API_TOKEN)" ] || die "Menghapus route CIDR membutuhkan CF_API_TOKEN (izin Cloudflare One Networks / Tunnel Write)."
      [ -n "$aid" ] || die "Account ID tidak tersedia."
      resp=$(api_request GET "/accounts/${aid}/teamnet/routes?is_deleted=false") || die "Gagal membaca CIDR routes."
      printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1 \
        || die "Gagal membaca CIDR routes: $(printf '%s' "$resp" | api_error_text)"
      local rids rid deleted=0
      rids=$(printf '%s' "$resp" | jq -r --arg n "$cidr" --arg tid "${tid:-}" \
        '.result[]? | select(.network == $n and ($tid == "" or .tunnel_id == $tid)) | .id')
      [ -n "$rids" ] || die "Route CIDR '${cidr}' tidak ditemukan pada profil '${p}'."
      while IFS= read -r rid; do
        [ -n "$rid" ] || continue
        resp=$(api_request DELETE "/accounts/${aid}/teamnet/routes/${rid}") || continue
        if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
          ok "CIDR route dihapus: ${cidr} (id: ${rid})"
          deleted=$((deleted + 1))
        else
          warn "Gagal menghapus route '${rid}': $(printf '%s' "$resp" | api_error_text)"
        fi
      done <<< "$rids"
      [ "$deleted" -gt 0 ] || return 1
      ;;
    *) die "Aksi cidr tidak dikenal: $action (gunakan list|add|rm)" ;;
  esac
}

cmd_access() {
  local sub=${1:-list}
  [ $# -gt 0 ] && shift || true
  case "$sub" in
    tcp|ssh|rdp|smb|curl|token)
      require_cloudflared
      "$CF_BIN" access "$sub" "$@"
      return $?
      ;;
    list|ls)
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
      check_accounts_readable || return 1
      local -a targets=()
      if [ "$all" -eq 1 ] || [ -z "$p" ]; then
        while IFS= read -r p; do [ -n "$p" ] && targets+=("$p"); done < <(profile_list)
      else
        profile_require "$p"
        targets=("$p")
      fi
      printf "${C_BOLD}%-12s | %-30s | %-20s | %s${C_RESET}\n" "PROFILE" "HOST" "TEAM" "AUD TAG"
      say "-------------+--------------------------------+----------------------+--------------------------------"
      local t h tm aud count=0
      for t in "${targets[@]}"; do
        while IFS=$'\t' read -r h tm aud; do
          [ -n "$h" ] || continue
          printf "%-12s | %-30s | %-20s | %s\n" "$t" "$h" "$tm" "$aud"
          count=$((count + 1))
        done < <(access_for_profile "$t")
      done
      [ "$count" -gt 0 ] || say "Belum ada aturan Cloudflare Access lokal. Tambah: sudo cf-tunnel access protect --profile <p> <host> --team <team>"
      ;;
    protect)
      local p="" host="" team="" aud="" app_name="" allow_emails="" allow_domains="" session_dur="24h"
      while [ $# -gt 0 ]; do
        case "$1" in
          --profile) p=${2:-}; shift 2 ;;
          --profile=*) p=${1#*=}; shift ;;
          --team|--team-name) team=${2:-}; shift 2 ;;
          --team=*|--team-name=*) team=${1#*=}; shift ;;
          --aud|--aud-tag) aud=${2:-}; shift 2 ;;
          --aud=*|--aud-tag=*) aud=${1#*=}; shift ;;
          --name) app_name=${2:-}; shift 2 ;;
          --name=*) app_name=${1#*=}; shift ;;
          --allow-emails) allow_emails=${2:-}; shift 2 ;;
          --allow-emails=*) allow_emails=${1#*=}; shift ;;
          --allow-domains) allow_domains=${2:-}; shift 2 ;;
          --allow-domains=*) allow_domains=${1#*=}; shift ;;
          --session) session_dur=${2:-}; shift 2 ;;
          --session=*) session_dur=${1#*=}; shift ;;
          -*) die "Opsi tidak dikenal: $1" ;;
          *) [ -z "$host" ] && host=$1 || die "Argumen tidak dikenal: $1"; shift ;;
        esac
      done
      need_root
      profile_require "$p"
      [ -n "$team" ] || team=$(profile_get "$p" CF_ACCESS_TEAM)
      [ -n "$host" ] && [ -n "$team" ] || die "Penggunaan: cf-tunnel access protect --profile <p> <host> --team <teamName> [--aud <audTag>] [--allow-emails <csv>] [--allow-domains <csv>]"
      require_safe_fields "$host" "$team" "$aud"
      profile_set "$p" CF_ACCESS_TEAM "$team"

      if ! exposures_for_profile "$p" | awk -F'\t' -v h="$host" '$1 == h { found=1 } END { exit !found }' \
        && ! routes_for_profile "$p" | awk -F'\t' -v h="$host" '$1 == h { found=1 } END { exit !found }'; then
        warn "Host '${host}' belum terdaftar di eksposur atau custom route profil '${p}'."
      fi

      if [ -z "$aud" ]; then
        CF_PROFILE="$p"
        [ -n "$(profile_get "$p" CF_API_TOKEN)" ] || die "Tanpa --aud, dibutuhkan CF_API_TOKEN untuk membuat/membaca Access App via API."
        local aid resp app_id
        aid=$(ensure_account_id "$p") || die "Account ID tidak tersedia."
        resp=$(api_request GET "/accounts/${aid}/access/apps") || die "Gagal membaca Access Apps."
        printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1 \
          || die "Gagal membaca Access Apps: $(printf '%s' "$resp" | api_error_text)"
        app_id=$(printf '%s' "$resp" | jq -r --arg d "$host" '[.result[]? | select(.domain == $d)][0].id // empty')
        aud=$(printf '%s' "$resp" | jq -r --arg d "$host" '[.result[]? | select(.domain == $d)][0].aud // empty')
        if [ -z "$aud" ]; then
          [ -n "$app_name" ] || app_name="wdev: ${host}"
          local app_data
          app_data=$(jq -nc --arg n "$app_name" --arg d "$host" --arg s "$session_dur" \
            '{name: $n, domain: $d, type: "self_hosted", session_duration: $s}')
          resp=$(api_request POST "/accounts/${aid}/access/apps" "$app_data") || die "Gagal membuat Access App."
          if ! printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
            die "Gagal membuat Access App: $(printf '%s' "$resp" | api_error_text)"
          fi
          app_id=$(printf '%s' "$resp" | jq -r '.result.id // empty')
          aud=$(printf '%s' "$resp" | jq -r '.result.aud // empty')
          [ -n "$aud" ] || die "Cloudflare API tidak mengembalikan AUD tag."
          ok "Access App dibuat untuk '${host}' (AUD: ${aud:0:16}...)."
        else
          ok "Menggunakan Access App yang sudah ada untuk '${host}'."
        fi
        if [ -n "$app_id" ] && { [ -n "$allow_emails" ] || [ -n "$allow_domains" ]; }; then
          local inc_json='[]' item
          if [ -n "$allow_emails" ]; then
            IFS=',' read -ra _em <<< "$allow_emails"
            for item in "${_em[@]}"; do
              item=$(printf '%s' "$item" | tr -d '[:space:]')
              [ -n "$item" ] && inc_json=$(printf '%s' "$inc_json" | jq -c --arg e "$item" '. + [{email: {email: $e}}]')
            done
          fi
          if [ -n "$allow_domains" ]; then
            IFS=',' read -ra _dm <<< "$allow_domains"
            for item in "${_dm[@]}"; do
              item=$(printf '%s' "$item" | tr -d '[:space:]')
              [ -n "$item" ] && inc_json=$(printf '%s' "$inc_json" | jq -c --arg d "$item" '. + [{email_domain: {domain: $d}}]')
            done
          fi
          local pol_data
          pol_data=$(jq -nc --argjson inc "$inc_json" '{name: "wdev allow", decision: "allow", include: $inc}')
          resp=$(api_request POST "/accounts/${aid}/access/apps/${app_id}/policies" "$pol_data") || warn "Gagal membuat policy Access."
          if printf '%s' "$resp" | jq -e '.success == true' >/dev/null 2>&1; then
            ok "Policy Access 'allow' ditambahkan."
          else
            warn "Policy Access gagal dibuat: $(printf '%s' "$resp" | api_error_text)"
          fi
        fi
      fi

      mkdir -p "$CF_ACCESS"
      chmod 700 "$CF_ACCESS"
      local af tmp
      af=$(access_file "$p")
      tmp=$(mktemp) || return 1
      [ -f "$af" ] && awk -F'\t' -v h="$host" '$1 != h' "$af" > "$tmp"
      printf '%s\t%s\t%s\n' "$host" "$team" "$aud" >> "$tmp"
      chmod 600 "$tmp"
      mv -f "$tmp" "$af"
      ok "Proteksi Access JWT disimpan untuk '${host}' (team: ${team})."
      require_cloudflared
      sync_profile "$p" || true
      ;;
    unprotect)
      local p="" host="" delete_app=0
      while [ $# -gt 0 ]; do
        case "$1" in
          --profile) p=${2:-}; shift 2 ;;
          --profile=*) p=${1#*=}; shift ;;
          --delete-app) delete_app=1; shift ;;
          -*) die "Opsi tidak dikenal: $1" ;;
          *) [ -z "$host" ] && host=$1 || die "Argumen tidak dikenal: $1"; shift ;;
        esac
      done
      need_root
      profile_require "$p"
      [ -n "$host" ] || die "Penggunaan: cf-tunnel access unprotect --profile <p> <host> [--delete-app]"
      local af tmp before after
      af=$(access_file "$p")
      if [ -f "$af" ]; then
        before=$(wc -l < "$af" | tr -d '[:space:]')
        tmp=$(mktemp) || return 1
        awk -F'\t' -v h="$host" '$1 != h' "$af" > "$tmp"
        after=$(wc -l < "$tmp" | tr -d '[:space:]')
        if [ "$after" -eq 0 ]; then
          rm -f "$tmp" "$af"
        else
          chmod 600 "$tmp"
          mv -f "$tmp" "$af"
        fi
        [ "$before" != "$after" ] && ok "Proteksi Access lokal '${host}' dihapus." || warn "'${host}' tidak ada di daftar Access lokal."
      else
        warn "Profil '${p}' belum punya konfigurasi Access lokal."
      fi
      if [ "$delete_app" -eq 1 ] && [ -n "$(profile_get "$p" CF_API_TOKEN)" ]; then
        CF_PROFILE="$p"
        local aid resp app_id
        aid=$(ensure_account_id "$p" 2>/dev/null || true)
        if [ -n "$aid" ]; then
          resp=$(api_request GET "/accounts/${aid}/access/apps" 2>/dev/null || true)
          app_id=$(printf '%s' "$resp" | jq -r --arg d "$host" '[.result[]? | select(.domain == $d)][0].id // empty')
          if [ -n "$app_id" ]; then
            if api_request DELETE "/accounts/${aid}/access/apps/${app_id}" | jq -e '.success == true' >/dev/null 2>&1; then
              ok "Access App '${host}' dihapus dari Cloudflare."
            else
              warn "Gagal menghapus Access App '${host}'."
            fi
          fi
        fi
      fi
      require_cloudflared
      sync_profile "$p" || true
      ;;
    *) die "Aksi access tidak dikenal: $sub (gunakan tcp|ssh|rdp|smb|curl|token|list|protect|unprotect)" ;;
  esac
}

cmd_raw() {
  local p=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --profile) p=${2:-}; shift 2 ;;
      --profile=*) p=${1#*=}; shift ;;
      --) shift; break ;;
      *) break ;;
    esac
  done
  require_cloudflared
  if [ -z "$p" ]; then
    "$CF_BIN" "$@"
    return $?
  fi
  need_root
  profile_require "$p"
  local mode dir cert envf tkn
  mode=$(profile_mode "$p")
  dir=$(profile_state_dir "$p")
  cert=$(profile_cert "$p")
  if [ "$mode" = "token" ]; then
    envf=$(profile_env_file "$p")
    [ -f "$envf" ] || die "EnvironmentFile tidak ada: $envf"
    tkn=$(sed -n 's/^TUNNEL_TOKEN=//p' "$envf" | tr -d '\r\n"')
    TUNNEL_TOKEN="$tkn" "$CF_BIN" "$@"
  else
    if [ -f "$cert" ]; then
      HOME="$dir" "$CF_BIN" --origincert "$cert" "$@"
    else
      HOME="$dir" "$CF_BIN" "$@"
    fi
  fi
}

doctor_check_dns() {
  local host=$1
  if curl -sS -m 8 -H 'accept: application/dns-json' \
    "https://cloudflare-dns.com/dns-query?name=${host}&type=A" 2>/dev/null \
    | jq -e '.Answer | length > 0' >/dev/null 2>&1; then
    ok "DNS publik: $host"
  else
    warn "DNS publik belum menjawab: $host"
  fi
}

doctor_check_tcp() {
  local label=$1 addr=$2 h port
  h=${addr%%:*}
  port=${addr##*:}
  if [ -z "$h" ] || [ -z "$port" ]; then
    warn "origin lokal: $label ($addr format tidak valid)"
    return 0
  fi
  if timeout 3 bash -c "exec 3<>/dev/tcp/${h}/${port}" 2>/dev/null; then
    ok "origin TCP: $label ($addr)"
  else
    warn "origin TCP tidak merespons: $label ($addr)"
  fi
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
  local r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool
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
    if [ "$(exposure_count "$t")" -gt 0 ] && [ ! -f "$CA_POOL" ]; then
      warn "CA mkcert ($CA_POOL) belum ada; jalankan 'sudo setup_dev.sh --ssl'"
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
      doctor_check_dns "$host"
      local code
      code=$(curl -sk -m 10 --resolve "${host}:443:127.0.0.1" -o /dev/null \
        -w '%{http_code}' "https://${host}/" 2>/dev/null || true)
      case "$code" in
        200|204|301|302|303|307|308|401|403) ok "origin lokal: $host (HTTP $code)" ;;
        *) warn "origin lokal: $host (HTTP ${code:-timeout})" ;;
      esac
    done < <(exposures_for_profile "$t")
    while IFS=$'\t' read -r r_host r_path r_svc r_notls r_hh r_osn r_http2 r_capool; do
      [ -n "$r_host" ] || continue
      [ "$r_host" != "*" ] && doctor_check_dns "$r_host"
      case "$r_svc" in
        http://*|https://*)
          local rcode
          local -a curl_opts=("-sk" "-m" "10")
          [ -n "${r_hh:-}" ] && [ "$r_hh" != "-" ] && curl_opts+=("-H" "Host: $r_hh")
          rcode=$(curl "${curl_opts[@]}" -o /dev/null -w '%{http_code}' "$r_svc" 2>/dev/null || true)
          case "$rcode" in
            200|204|301|302|303|307|308|401|403|404) ok "origin route: $r_host -> $r_svc (HTTP $rcode)" ;;
            *) warn "origin route: $r_host -> $r_svc (HTTP ${rcode:-timeout})" ;;
          esac
          ;;
        tcp://*|ssh://*|rdp://*|smb://*)
          doctor_check_tcp "$r_host" "${r_svc#*://}"
          ;;
        unix://*)
          local spath=${r_svc#unix://}
          [ -S "$spath" ] && ok "origin unix socket: $spath" || warn "unix socket tidak ditemukan: $spath"
          ;;
        bastion|hello_world|http_status:*)
          ok "origin built-in: $r_host -> $r_svc"
          ;;
      esac
    done < <(routes_for_profile "$t")
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

Perintah Akun & Tunnel:
  account add <profile> [--zone Z] [--mode local|token] [--protocol auto|quic|http2]
  account update <profile> [--zone Z] [--api-token T] [--tunnel-token T] [--protocol P]
  account list                Tampilkan profil, zone, tunnel, dan status service
  account remove <profile>    Hapus profil (harus tanpa eksposur & custom route)
  login --profile <p>         Login origin cert Cloudflare untuk profil
  tunnel create --profile <p> Buat tunnel + credentials + service
  tunnel rotate --profile <p> Ganti tunnel (buat baru, DNS diarahkan ulang)
  tunnel info --profile <p>   Detail connector tunnel (mode local & token)
  sync [--profile <p>|--all] [--remote]
                              Regenerate ingress, validasi, restart service (--remote untuk mode token)

Perintah Routing, DNS & Quick Tunnel:
  route list [--profile <p>|--all]
  route add --profile <p> <host> <service> [--path R] [--no-tls-verify] [--host-header H] [--origin-server-name S] [--ca-pool P] [--http2] [--allow-deep]
  route rm --profile <p> <host> [--path R]
  quick <port|url|vhost-proxy> [--no-tls-verify] [--bg] [--name <label>]
  quick status | logs [label] | stop [label|--all]
  cidr list|add|rm --profile <p> [cidr] [--vnet ID] [--comment TEXT]
  dns list|add|rm|ensure --profile <p> [host] [--force] [--all]

Perintah Cloudflare Access, Service & Diagnostik:
  access protect --profile <p> <host> --team <team> [--aud <tag>] [--allow-emails ...] [--allow-domains ...]
  access unprotect --profile <p> <host> [--delete-app]
  access list [--profile <p>|--all]
  access tcp|ssh|rdp|smb|curl|token <opsi cloudflared>
  service <aksi> [--profile <p>|--all]
                              start|stop|restart|status|logs|install|enable|disable
  status [--all]              Tabel profil, connector, eksposur, dan route
  exposures                   Tabel domain lokal -> host publik -> profil
  doctor [--all]              Diagnostik config, service, DNS, dan origin lokal
  raw [--profile <p>] <args>  Passthrough langsung ke binary cloudflared

Contoh:
  sudo cf-tunnel account add cf1
  sudo cf-tunnel route add --profile cf1 api.example.com http://127.0.0.1:8080
  sudo cf-tunnel route add --profile cf1 ssh.example.com ssh://127.0.0.1:22
  cf-tunnel quick 3000
  sudo cf-tunnel cidr add --profile cf1 10.10.0.0/24
  sudo cf-tunnel access protect --profile cf1 app.example.com --team myteam --allow-domains example.com
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
        update|set) cmd_account_update "$@" ;;
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
    route|routes) cmd_route "$@" ;;
    quick) cmd_quick "$@" ;;
    cidr|net) cmd_cidr "$@" ;;
    access) cmd_access "$@" ;;
    raw|exec) cmd_raw "$@" ;;
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
