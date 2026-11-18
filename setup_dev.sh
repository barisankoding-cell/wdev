#!/usr/bin/env bash
set -euo pipefail

export COMPOSER_ALLOW_SUPERUSER=1
export COMPOSER_NO_INTERACTION=1

ORIG_ARGS=("$@")

LOG_FILE="/var/log/dev-install.log"
LOCK_FILE="/var/lock/dev-install.lock"
TOTAL_STEPS=8
CURRENT_STEP=0

# Kredensial & akses jaringan dapat dioverride via environment.
# Default aman: layanan hanya mendengarkan di localhost. Buka ke LAN secara eksplisit (--lan).
REDIS_PASSWORD="${REDIS_PASSWORD:-redis}"
PG_SUPERUSER_PASSWORD="${PG_SUPERUSER_PASSWORD:-postgres}"
DEV_LAN_ACCESS="${DEV_LAN_ACCESS:-0}"

# Teruskan ke helper turunan (pg-switch, dev-site, dll.) agar override tidak hilang.
export REDIS_PASSWORD PG_SUPERUSER_PASSWORD DEV_LAN_ACCESS

# Validasi kredensial: tolak nilai tidak aman, jangan diam-diam kembali ke default.
validate_credentials() {
  local allow_default="1"
  [ "$DEV_LAN_ACCESS" = "1" ] && allow_default="0"
  local name value
  for name in PG_SUPERUSER_PASSWORD REDIS_PASSWORD; do
    value="${!name}"
    case "$value" in
      "") echo "Error: ${name} tidak boleh kosong." >&2; return 1 ;;
      *[!A-Za-z0-9_.-]*)
        echo "Error: ${name} hanya boleh berisi huruf, angka, titik, garis bawah, atau tanda hubung." >&2
        return 1 ;;
    esac
    if [ "$allow_default" != "1" ]; then
      case "${name}=${value}" in
        PG_SUPERUSER_PASSWORD=postgres|REDIS_PASSWORD=redis)
          echo "Error: ${name} masih memakai nilai default; wajib diganti saat DEV_LAN_ACCESS=1." >&2
          return 1 ;;
      esac
    fi
  done
  return 0
}

warn_lan_exposure() {
  [ "$DEV_LAN_ACCESS" = "1" ] || return 0
  echo "Peringatan: DEV_LAN_ACCESS=1 - PostgreSQL & Redis terbuka ke LAN/Netbird (bind 0.0.0.0)."
  echo "           Pastikan password non-default dan firewall sudah dikonfigurasi."
}

# --- WARNA GLOBAL (dipakai seluruh fungsi & helper) ---
C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"
C_DIM="\e[2m"

log_run_header() {
 mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null || true
 {
 echo ""
 echo "================================================================"
 echo " RUN: $(date -Is) | argumen: ${ORIG_ARGS[*]:-interaktif}"
 echo "================================================================"
 } >> "$LOG_FILE" 2>/dev/null || true
 # Log bisa memuat keluaran perintah; batasi hanya untuk root.
 chmod 600 "$LOG_FILE" 2>/dev/null || true
}

LOCK_HELD_PID=""

is_lock_owner_alive() {
  local lock_pid=$1
  [ -n "$lock_pid" ] && kill -0 "$lock_pid" 2>/dev/null || return 1
  if [ -r "/proc/$lock_pid/cmdline" ]; then
    tr '\0' ' ' < "/proc/$lock_pid/cmdline" 2>/dev/null | grep -q "setup_dev"
    return $?
  fi
  return 0
}

release_lock() {
  if [ "${LOCK_HELD_PID:-}" = "$$" ]; then
    flock -u 9 2>/dev/null || true
    exec 9>&- 2>/dev/null || true
    rm -f "$LOCK_FILE" 2>/dev/null || true
    LOCK_HELD_PID=""
  fi
}

acquire_lock() {
  command -v flock >/dev/null 2>&1 || return 0
  [ "${LOCK_HELD_PID:-}" = "$$" ] && return 0

  mkdir -p "$(dirname "$LOCK_FILE")" 2>/dev/null || true
  local attempt
  for attempt in 1 2; do
    if ! exec 9>>"$LOCK_FILE" 2>/dev/null; then
      return 0
    fi
    if flock -n 9; then
      : > "$LOCK_FILE" 2>/dev/null || true
      printf '%s\n' "$$" >&9 2>/dev/null || true
      LOCK_HELD_PID="$$"
      return 0
    fi

    local lock_pid=""
    lock_pid=$(head -n1 "$LOCK_FILE" 2>/dev/null | tr -dc '0-9' || true)
    if [ -z "$lock_pid" ]; then
      sleep 0.2
      lock_pid=$(head -n1 "$LOCK_FILE" 2>/dev/null | tr -dc '0-9' || true)
    fi

    if is_lock_owner_alive "$lock_pid"; then
      exec 9>&- 2>/dev/null || true
      echo "Error: instance setup_dev.sh lain sedang berjalan (PID: $lock_pid, lock: $LOCK_FILE)."
      echo "Jika yakin tidak ada proses lain, hapus lock: sudo rm -f $LOCK_FILE"
      exit 1
    fi

    exec 9>&- 2>/dev/null || true
    if [ "$attempt" -eq 1 ] && rm -f "$LOCK_FILE" 2>/dev/null; then
      echo "Catatan: membersihkan lock usang ($LOCK_FILE)..."
      continue
    fi
    echo "Error: tidak dapat mengambil lock ($LOCK_FILE)."
    exit 1
  done
}

ver_ge() {
 # ver_ge A B -> true jika A >= B (perbandingan versi numerik)
 [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

hosts_has_domain() {
 local d="$1"
 awk -v d="$d" '{ for (i = 2; i <= NF; i++) if ($i == d) { found = 1; exit } } END { exit(found ? 0 : 1) }' /etc/hosts 2>/dev/null
}

hosts_add_domain() {
 local d="$1"
 hosts_has_domain "$d" && return 0
 echo "127.0.0.1 ${d}" >> /etc/hosts 2>/dev/null || true
}

hosts_remove_domain() {
 local d="$1" tmp
 tmp=$(mktemp)
 awk -v d="$d" '{ keep = 1; for (i = 2; i <= NF; i++) if ($i == d) { keep = 0; break } if (keep) print }' /etc/hosts > "$tmp" 2>/dev/null || true
 cat "$tmp" > /etc/hosts 2>/dev/null || true
 rm -f "$tmp"
}

# --- STATE EKSPOSUR CLOUDFLARE (dipakai setup_dev.sh, dev-site, cf-tunnel) ---
WDEV_ETC="${WDEV_ETC:-/etc/wdev}"
WDEV_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export WDEV_ETC

wdev_exposures_dir() { printf '%s/exposures\n' "$WDEV_ETC"; }
wdev_accounts_dir() { printf '%s/cloudflare/accounts\n' "$WDEV_ETC"; }
wdev_exposure_file() { printf '%s/exposures/%s.conf\n' "$WDEV_ETC" "$1"; }

wdev_exposure_lines() {
 local f
 f=$(wdev_exposure_file "$1")
 [ -f "$f" ] || return 0
 awk 'NF >= 2 && $1 !~ /^#/ { print $1 "\t" $2 }' "$f"
}

wdev_public_hosts() { wdev_exposure_lines "$1" | cut -f1; }

wdev_host_owner() {
 local host="$1" f domain
 while IFS= read -r f; do
 [ -f "$f" ] || continue
 domain=$(basename "$f" .conf)
 awk -v h="$host" -v d="$domain" 'NF >= 2 && $1 == h { print d "\t" $2; exit }' "$f"
 done < <(find "$(wdev_exposures_dir)" -maxdepth 1 -type f -name '*.conf' -print 2>/dev/null | sort)
}

wdev_profile_list() {
 local d
 d=$(wdev_accounts_dir)
 if [ "$EUID" -ne 0 ] && [ ! -r "$d" ]; then
   sudo find "$d" -maxdepth 1 -type f -name '*.conf' -printf '%f\n' 2>/dev/null | sed 's/\.conf$//' | sort
   return
 fi
 [ -d "$d" ] || return 0
 find "$d" -maxdepth 1 -type f -name '*.conf' -printf '%f\n' 2>/dev/null | sed 's/\.conf$//' | sort
}

wdev_profile_exists() {
 local f="$(wdev_accounts_dir)/$1.conf"
 if [ -f "$f" ]; then
   return 0
 fi
 [ "$EUID" -ne 0 ] && sudo test -f "$f" 2>/dev/null
}

wdev_profile_get() {
 local f
 f="$(wdev_accounts_dir)/$1.conf"
 if [ "$EUID" -ne 0 ] && [ ! -r "$f" ]; then
   sudo awk -F= -v k="$2" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$f" 2>/dev/null
   return
 fi
 [ -f "$f" ] || return 0
 awk -F= -v k="$2" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$f"
}

wdev_profile_zone() { wdev_profile_get "$1" CF_ZONE; }

# Return: 0 valid (apex/1-label atau multi-label jika allow_deep=1), 2 multi-label butuh allow_deep, 1 di luar zone
wdev_host_in_zone() {
 local host="$1" zone="$2" allow_deep="${3:-0}" rel
 [ -n "$host" ] && [ -n "$zone" ] || return 1
 [ "$host" = "$zone" ] && return 0
 case "$host" in *."$zone") ;; *) return 1 ;; esac
 rel="${host%.$zone}"
 [ -n "$rel" ] || return 1
 if [[ "$rel" == *.* ]]; then
 [ "$allow_deep" = "1" ] && return 0 || return 2
 fi
 return 0
}

wdev_apply_server_names() {
 local domain="$1" conf names host tmp indent
 conf="/etc/nginx/sites-available/${domain}.conf"
 [ -f "$conf" ] || return 1
 names="${domain} *.${domain}"
 while IFS= read -r host; do
 [ -n "$host" ] && names="$names $host"
 done < <(wdev_public_hosts "$domain")
 indent=$(printf '%*s' 4 '')
 tmp=$(mktemp)
 if ! awk -v sn="$names" -v ind="$indent" '/^[[:space:]]*server_name[[:space:]]/ { print ind "server_name " sn ";"; next } { print }' "$conf" > "$tmp"; then
 rm -f "$tmp"
 return 1
 fi
 mv -f "$tmp" "$conf"
 chmod 644 "$conf" 2>/dev/null || true
}

wdev_cert_covers_publics() {
 local domain="$1" cert="$2" text host
 [ -f "$cert" ] || return 1
 text=$(openssl x509 -in "$cert" -noout -text 2>/dev/null) || return 1
 while IFS= read -r host; do
 [ -n "$host" ] || continue
 printf '%s' "$text" | grep -q "DNS:${host}" || return 1
 done < <(wdev_public_hosts "$domain")
 return 0
}

wdev_regen_cert() {
 local domain="$1" cert key host
 cert="/etc/ssl/local-certs/${domain}.pem"
 key="/etc/ssl/local-certs/${domain}-key.pem"
 command -v mkcert >/dev/null 2>&1 || return 1
 mkdir -p /etc/ssl/local-certs
 [ -d /etc/ssl/local-certs/ca ] && export CAROOT="/etc/ssl/local-certs/ca"
 local -a sans=("$domain" "*.$domain" "localhost" "127.0.0.1" "::1")
 while IFS= read -r host; do
 [ -n "$host" ] && sans+=("$host")
 done < <(wdev_public_hosts "$domain")
 mkcert -cert-file "$cert" -key-file "$key" "${sans[@]}" >/dev/null 2>&1 || return 1
 chmod 644 "$cert" 2>/dev/null || true
 chmod 600 "$key" 2>/dev/null || true
 command -v chcon >/dev/null 2>&1 && chcon -t cert_t "$cert" "$key" 2>/dev/null || true
 return 0
}

wdev_refresh_site() {
 local domain="$1" cert
 wdev_apply_server_names "$domain" || return 1
 cert="/etc/ssl/local-certs/${domain}.pem"
 if [ ! -f "$cert" ] || ! wdev_cert_covers_publics "$domain" "$cert"; then
 wdev_regen_cert "$domain" || return 1
 fi
 return 0
}

# --- DETEKSI OS & PACKAGE MANAGER ---
OS_ID="unknown"
OS_NAME="Linux"
OS_VERSION_ID=""
PKG_MGR="unknown"
PKG_MGR_NAME="Sistem"
DNF_RO_OPTS=""

detect_os() {
  if [ -f /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_NAME="${NAME:-Linux}"
    OS_VERSION_ID="${VERSION_ID:-}"
  fi

  if command -v dnf >/dev/null 2>&1; then
    PKG_MGR="dnf"
    PKG_MGR_NAME="DNF"
  elif command -v yum >/dev/null 2>&1; then
    PKG_MGR="yum"
    PKG_MGR_NAME="YUM"
  elif command -v apt-get >/dev/null 2>&1; then
    PKG_MGR="apt"
    PKG_MGR_NAME="APT"
  elif command -v pacman >/dev/null 2>&1; then
    PKG_MGR="pacman"
    PKG_MGR_NAME="Pacman"
  elif command -v zypper >/dev/null 2>&1; then
    PKG_MGR="zypper"
    PKG_MGR_NAME="Zypper"
  fi

  # Flag bypass lock untuk query read-only DNF5 (mencegah hang saat PackageKit/proses dnf lain aktif)
  DNF_RO_OPTS=""
  if [ "$PKG_MGR" = "dnf" ]; then
    if command -v dnf5 >/dev/null 2>&1 || dnf --version 2>&1 | grep -q "dnf5"; then
      DNF_RO_OPTS="--skip-file-locks"
    fi
  fi
}
detect_os

# --- DETEKSI DESKTOP ENVIRONMENT (DE) ---
HAS_DE=0
DETECTED_DE=""

detect_desktop_environment() {
  HAS_DE=0
  DETECTED_DE=""

  # 1. Cek variabel sesi aktif
  local de_var="${XDG_CURRENT_DESKTOP:-${DESKTOP_SESSION:-${GDMSESSION:-}}}"
  if [ -n "$de_var" ]; then
    HAS_DE=1
    DETECTED_DE="$de_var"
    return 0
  fi

  # 2. Cek file sesi X11 / Wayland yang terpasang di sistem
  if [ -d "/usr/share/xsessions" ] || [ -d "/usr/share/wayland-sessions" ]; then
    local sess
    sess=$(ls -1 /usr/share/xsessions/*.desktop /usr/share/wayland-sessions/*.desktop 2>/dev/null | head -n1 || true)
    if [ -n "$sess" ]; then
      HAS_DE=1
      local de_name
      de_name=$(basename "$sess" .desktop)
      DETECTED_DE="$de_name"
      return 0
    fi
  fi

  # 3. Cek binary window manager / desktop environment populer
  local de_bins=(
    "gnome-shell:GNOME"
    "plasmashell:KDE"
    "startplasma-x11:KDE"
    "xfce4-session:XFCE"
    "lxsession:LXDE"
    "startlxde:LXDE"
    "lxqt-session:LXQt"
    "mate-session:MATE"
    "cinnamon-session:Cinnamon"
    "openbox:Openbox"
    "sway:Sway"
    "hyprland:Hyprland"
    "i3:i3"
  )

  for item in "${de_bins[@]}"; do
    local bin="${item%%:*}"
    local name="${item##*:}"
    if command -v "$bin" >/dev/null 2>&1; then
      HAS_DE=1
      DETECTED_DE="$name"
      return 0
    fi
  done

  # 4. Cek display manager service
  if systemctl list-unit-files 2>/dev/null | grep -qE "^(gdm|sddm|lightdm|lxdm|xdm|display-manager)\.service"; then
    HAS_DE=1
    DETECTED_DE="Display Manager"
    return 0
  fi

  return 1
}
detect_desktop_environment || true

get_redis_conf() {
  for f in "/etc/redis/redis.conf" "/etc/valkey/valkey.conf" "/etc/redis.conf" "/etc/valkey.conf"; do
    if [ -f "$f" ]; then
      echo "$f"
      return 0
    fi
  done
  echo "/etc/redis/redis.conf"
}

get_redis_service() {
  local unit_list
  unit_list=$(systemctl list-unit-files 2>/dev/null || true)
  if echo "$unit_list" | grep -qE "^valkey\.service[[:space:]]+(enabled|disabled|static|indirect)"; then
    echo "valkey"
  elif echo "$unit_list" | grep -qE "^redis\.service[[:space:]]+(enabled|disabled|static|indirect)"; then
    echo "redis"
  elif echo "$unit_list" | grep -qE "^redis-server\.service[[:space:]]+(enabled|disabled|static|indirect)"; then
    echo "redis-server"
  elif echo "$unit_list" | grep -q "^valkey\.service"; then
    echo "valkey"
  elif echo "$unit_list" | grep -q "^redis\.service"; then
    echo "redis"
  else
    echo "redis-server"
  fi
}

is_redis_running() {
  systemctl is-active --quiet redis 2>/dev/null || \
  systemctl is-active --quiet redis-server 2>/dev/null || \
  systemctl is-active --quiet valkey 2>/dev/null
}

TMP_CLEANUP_FILES=()
cleanup_tmp() {
  for f in "${TMP_CLEANUP_FILES[@]}"; do
    [ -e "$f" ] && rm -rf "$f" 2>/dev/null || true
  done
  release_lock
}
trap cleanup_tmp EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

restore_tty_state() {
  if [ -t 0 ]; then
    stty echo icanon opost onlcr </dev/tty 2>/dev/null || stty echo icanon opost onlcr 2>/dev/null || true
    if command -v python3 >/dev/null 2>&1; then
      python3 -c 'import fcntl, os
for fd in (0, 1, 2):
    try:
        fl = fcntl.fcntl(fd, fcntl.F_GETFL)
        if fl & os.O_NONBLOCK:
            fcntl.fcntl(fd, fcntl.F_SETFL, fl & ~os.O_NONBLOCK)
    except OSError:
        pass' 2>/dev/null || true
    fi
    printf '\e[?25h\e[0m' 2>/dev/null || true
  fi
}

read_menu_input() {
  local __var=$1
  local __prompt=$2
  local __out="/dev/stdout"
  if [ ! -t 1 ] && [ -t 0 ] && [ -w /dev/tty ]; then
    __out="/dev/tty"
  fi
  if [ -t 0 ]; then
    printf '\e[0m\e[?25h' > "$__out" 2>/dev/null || true
  fi
  printf '%s' "$__prompt" > "$__out"
  local __val=""
  if ! IFS= read -r __val; then
    printf '\n' > "$__out" 2>/dev/null || true
    printf -v "$__var" '%s' ""
    return 1
  fi
  printf -v "$__var" '%s' "$__val"
  return 0
}

read_menu_secret() {
  local __var=$1
  local __prompt=$2
  local __out="/dev/stdout"
  if [ ! -t 1 ] && [ -t 0 ] && [ -w /dev/tty ]; then
    __out="/dev/tty"
  fi
  printf '%s' "$__prompt" > "$__out"
  local __val=""
  if [ -t 0 ]; then
    if ! IFS= read -r -s __val; then
      printf '\n' > "$__out" 2>/dev/null || true
      printf -v "$__var" '%s' ""
      return 1
    fi
    printf '\n' > "$__out" 2>/dev/null || true
  else
    if ! IFS= read -r __val; then
      printf '\n' > "$__out" 2>/dev/null || true
      printf -v "$__var" '%s' ""
      return 1
    fi
  fi
  printf -v "$__var" '%s' "$__val"
  return 0
}

draw_progress() {
  local step=$1
  local total=$2
  local msg=$3
  local width=30
  local percent=0
  [ "$total" -gt 0 ] && percent=$(( step * 100 / total ))
  local filled=0
  [ "$total" -gt 0 ] && filled=$(( step * width / total ))
  local empty=$(( width - filled ))
  
  local bar=""
  for ((i=0; i<filled; i++)); do bar="${bar}█"; done
  for ((i=0; i<empty; i++)); do bar="${bar}░"; done
  
  printf "\r[%s] %3d%% | %s\e[K" "$bar" "$percent" "$msg"
}

run_step() {
  local msg=$1
  shift
  draw_progress "$CURRENT_STEP" "$TOTAL_STEPS" "$msg..."
  if ! "$@" < /dev/null >> "$LOG_FILE" 2>&1; then
    restore_tty_state
    echo -e "\n\n[GAGAL] Terjadi kesalahan saat: $msg"
    echo "Periksa log di: $LOG_FILE"
    echo "--- 30 baris terakhir log ---"
    tail -n 30 "$LOG_FILE" 2>/dev/null || true
    echo "-----------------------------"
    exit 1
  fi
  restore_tty_state
  CURRENT_STEP=$((CURRENT_STEP + 1))
  draw_progress "$CURRENT_STEP" "$TOTAL_STEPS" "$msg selesai"
}

check_root() {
  if [ "$EUID" -ne 0 ]; then
    echo "Error: Jalankan opsi ini sebagai root (sudo $0)"
    exit 1
  fi
}

get_github_latest_tag() {
  local repo=$1
  local default_tag=$2
  local tag
  tag=$(github_api_get "https://api.github.com/repos/${repo}/releases/latest" 2>/dev/null | jq -r '.tag_name // empty' 2>/dev/null || true)
  if [[ "$tag" =~ ^v?[0-9] ]]; then
    echo "$tag"
  else
    echo "$default_tag"
  fi
}

github_api_get() {
 local url=$1
 if [ -n "${GITHUB_TOKEN:-}" ]; then
 curl -sSL -m 10 -H "Authorization: Bearer ${GITHUB_TOKEN}" "$url"
 else
 curl -sSL -m 10 "$url"
 fi
}

get_github_asset_digest() {
 local repo=$1 asset=$2
 github_api_get "https://api.github.com/repos/${repo}/releases/latest" 2>/dev/null \
 | jq -r --arg a "$asset" '.assets[] | select(.name == $a) | .digest // empty' 2>/dev/null \
 | sed 's/^sha256://' | head -n1
}

verify_sha256() {
 local file=$1 expected=$2 label=$3
 [ -n "$expected" ] || return 0
 local actual
 actual=$(sha256sum "$file" | awk '{print $1}')
 if [ "${actual,,}" != "${expected,,}" ]; then
 echo "Error: checksum ${label} tidak cocok (harap: $expected, aktual: $actual)."
 return 1
 fi
}

verify_sha1() {
 local file=$1 expected=$2 label=$3
 [ -n "$expected" ] || return 0
 local actual
 actual=$(sha1sum "$file" | awk '{print $1}')
 if [ "$actual" != "$expected" ]; then
 echo "Error: checksum ${label} tidak cocok (harap: $expected, aktual: $actual)."
 return 1
 fi
}

get_component_version() {
  local comp=$1
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  local ORCA_BIN="$TARGET_HOME/.local/bin/orca"

  case $comp in
    "Node.js")
      node -v 2>/dev/null | tr -d 'v' || echo "-"
      ;;
    "Rustc")
      rustc --version 2>/dev/null | awk '{print $2}' || echo "-"
      ;;
    "Cargo")
      cargo --version 2>/dev/null | awk '{print $2}' || echo "-"
      ;;
    "Mold Linker")
      mold --version 2>/dev/null | head -n1 | awk '{print $2}' || echo "-"
      ;;
    "Sccache")
      sccache --version 2>/dev/null | awk '{print $2}' || echo "-"
      ;;
    "Golang")
      go version 2>/dev/null | awk '{print $3}' || echo "-"
      ;;
    "OpenJDK (Java)")
      local jv=""
      if command -v java >/dev/null 2>&1; then
        local real_java=""
        real_java=$(readlink -f "$(command -v java)" 2>/dev/null || true)
        if [[ "$real_java" == /usr/lib/jvm/temurin-* ]]; then
          jv=$(java -version 2>&1 | head -n1 | awk -F '"' '{print $2}' || true)
        else
          local j_major
          j_major=$(java -version 2>&1 | head -n1 | grep -oE '[0-9]+' | head -n1 || true)
          if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
            if [ -n "$j_major" ]; then
              jv=$(rpm -q --qf "%{version}\n" "java-${j_major}-openjdk-devel" 2>/dev/null | grep -v 'not installed' | head -n1 || true)
              [ -z "$jv" ] && jv=$(rpm -q --qf "%{version}\n" "java-${j_major}-openjdk" 2>/dev/null | grep -v 'not installed' | head -n1 || true)
            fi
          fi
          [ -z "$jv" ] && jv=$(java -version 2>&1 | head -n1 | awk -F '"' '{print $2}' || true)
        fi
      else
        if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
          jv=$(rpm -qa "java*openjdk-devel*" --qf "%{version}\n" 2>/dev/null | head -n1 || true)
        fi
        if [ -z "$jv" ]; then
          local t_bin
          t_bin=$(find /usr/lib/jvm -maxdepth 3 -path '/usr/lib/jvm/temurin-*-jdk/bin/java' -type f 2>/dev/null | sort -V | tail -n1 || true)
          [ -n "$t_bin" ] && jv=$("$t_bin" -version 2>&1 | head -n1 | awk -F '"' '{print $2}' || true)
        fi
      fi
      [ -n "$jv" ] && echo "$jv" || echo "-"
      ;;
    "PostgreSQL")
      psql --version 2>/dev/null | awk '{print $3}' || echo "-"
      ;;
    "Redis")
      if command -v redis-server >/dev/null 2>&1; then
        redis-server --version 2>/dev/null | grep -o 'v=[0-9.]*' | cut -d= -f2 || echo "Terpasang"
      elif command -v valkey-server >/dev/null 2>&1; then
        valkey-server --version 2>/dev/null | grep -o 'v=[0-9.]*' | cut -d= -f2 || echo "Terpasang"
      elif is_redis_running; then
        echo "Terpasang (Active)"
      else
        echo "-"
      fi
      ;;
    "Flutter")
      if [ -d "/usr/local/flutter/.git" ]; then
        git -C /usr/local/flutter describe --tags 2>/dev/null || echo "-"
      elif [ -x "/usr/local/flutter/bin/flutter" ]; then
        /usr/local/flutter/bin/flutter --version 2>/dev/null | head -n1 | awk '{print $2}' || echo "-"
      elif command -v flutter >/dev/null 2>&1; then
        flutter --version 2>/dev/null | head -n1 | awk '{print $2}' || echo "-"
      else
        echo "-"
      fi
      ;;
    "Android SDK")
      if [ -d "/usr/local/android-sdk/cmdline-tools/latest" ]; then
        local highest_api
        highest_api=$(ls -1 /usr/local/android-sdk/platforms 2>/dev/null | grep -oE '[0-9]+' | sort -n | tail -n1)
        if [ -n "$highest_api" ]; then
 echo "API ${highest_api}"
        else
          echo "Ready"
        fi
      else
        echo "-"
      fi
      ;;
    "Orca IDE")
      local REAL_ORCA="$TARGET_HOME/.local/lib/orca-ide/resources/bin/orca-ide"
      if [ -x "$REAL_ORCA" ]; then
        "$REAL_ORCA" --version 2>/dev/null | head -n1 | tr -d 'v' | awk '{print $1}' || echo "-"
      elif [ -x "$ORCA_BIN" ]; then
        if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
          sudo -u "$SUDO_USER" "$ORCA_BIN" --version < /dev/null 2>/dev/null | head -n1 | tr -d 'v' | awk '{print $1}' || echo "-"
        else
          "$ORCA_BIN" --version < /dev/null 2>/dev/null | head -n1 | tr -d 'v' | awk '{print $1}' || echo "-"
        fi
 elif command -v orca-ide >/dev/null 2>&1; then
 orca-ide --version 2>/dev/null | head -n1 | tr -d 'v' | awk '{print $1}' || echo "-"
 else
 echo "-"
 fi
 ;;
 "cloudflared")
 local cf_bin
 cf_bin=$(command -v cloudflared 2>/dev/null || true)
 [ -z "$cf_bin" ] && [ -x /usr/local/bin/cloudflared ] && cf_bin=/usr/local/bin/cloudflared
 if [ -n "$cf_bin" ]; then
 "$cf_bin" --version 2>/dev/null | awk '{print $3}'
 else
 echo "-"
 fi
 ;;
 "Nginx")
 local nginx_bin=""
 if command -v nginx >/dev/null 2>&1; then
 nginx_bin=$(command -v nginx)
 elif [ -x /usr/sbin/nginx ]; then
 nginx_bin="/usr/sbin/nginx"
 fi
 if [ -n "$nginx_bin" ]; then
 "$nginx_bin" -v 2>&1 | grep -o 'nginx/[0-9.]*' | cut -d/ -f2 || echo "-"
 else
 echo "-"
 fi
 ;;
    "PHP")
      if command -v php >/dev/null 2>&1; then
        php -v 2>/dev/null | head -n1 | grep -o 'PHP [0-9.]*' | awk '{print $2}' || echo "-"
      else
        echo "-"
      fi
      ;;
    "Composer")
      if command -v composer >/dev/null 2>&1; then
        COMPOSER_ALLOW_SUPERUSER=1 composer --version --no-interaction 2>/dev/null | grep -o 'version [0-9.]*' | awk '{print $2}' || echo "-"
      else
        echo "-"
      fi
      ;;
    "Zsh")
      if command -v zsh >/dev/null 2>&1; then
        zsh --version 2>/dev/null | awk '{print $2}' || echo "Terpasang"
      else
        echo "-"
      fi
      ;;
    "Oh My Zsh")
      if [ -d "$TARGET_HOME/.oh-my-zsh/.git" ]; then
        local c
        c=$(git -C "$TARGET_HOME/.oh-my-zsh" rev-parse --short HEAD 2>/dev/null || true)
        [ -n "$c" ] && echo "$c" || echo "Terpasang"
      elif [ -d "$TARGET_HOME/.oh-my-zsh" ]; then
        echo "Terpasang"
      else
        echo "-"
      fi
      ;;
    *)
      echo "-"
      ;;
  esac
}

get_latest_version() {
  local comp=$1
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  detect_os
  case $comp in
 "Node.js")
 local v="" major="22"
 if command -v node >/dev/null 2>&1; then
   major=$(node -v 2>/dev/null | sed 's/^v//' | cut -d. -f1)
 fi
 # Jika Node.js terpasang via RPM di Fedora/RHEL, prioritaskan repositori DNF agar tidak false positive terhadap nodejs.org
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
   if rpm -q nodejs >/dev/null 2>&1 || rpm -q "nodejs${major}" >/dev/null 2>&1; then
     v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" "nodejs${major}" 2>/dev/null | cut -d- -f1)
     [ -z "$v" ] && v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" nodejs 2>/dev/null | cut -d- -f1)
   fi
 elif [ "$PKG_MGR" = "apt" ]; then
   if dpkg-query -W -f='${Status}' nodejs 2>/dev/null | grep -q "install ok installed"; then
     v=$(apt-cache policy nodejs 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1)
   fi
 fi
 if [ -z "$v" ]; then
   v=$(curl -sSL -m 5 "https://nodejs.org/dist/index.json" 2>/dev/null | jq -r --arg maj "v${major}." '[.[] | select(.version | startswith($maj))][0].version // empty' 2>/dev/null || true)
 fi
 if [ -z "$v" ]; then
   if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
     v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" "nodejs${major}" 2>/dev/null | cut -d- -f1)
     [ -z "$v" ] && v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" nodejs 2>/dev/null | cut -d- -f1)
   else
     v=$(apt-cache policy nodejs 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1)
   fi
 fi
 [ -n "$v" ] && echo "${v#v}" || echo "-"
 ;;
 "Rustc"|"Cargo")
 local v
 v=$(get_github_latest_tag "rust-lang/rust" "-" | sed 's/^rust-//')
 [ -n "$v" ] && echo "${v#v}" || echo "-"
 ;;
    "Mold Linker")
      local v
      if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
        v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" mold 2>/dev/null)
      else
        v=$(apt-cache policy mold 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d+ -f1 | cut -d- -f1)
      fi
 [ -z "$v" ] && v=$(get_github_latest_tag "rui314/mold" "-")
 echo "${v#v}"
 ;;
 "Sccache")
 local v
 v=$(get_github_latest_tag "mozilla/sccache" "-")
 echo "${v#v}"
 ;;
    "Golang")
      local v
      v=$(curl -sSL -m 5 "https://go.dev/dl/?mode=json" 2>/dev/null | jq -r '.[0].version // empty' 2>/dev/null || true)
      [ -n "$v" ] && echo "$v" || echo "-"
      ;;
 "OpenJDK (Java)")
 # Ikuti major yang terpasang; jangan tawarkan upgrade major (mis. 21 -> 25)
 local major="" real_java=""
 if command -v java >/dev/null 2>&1; then
 real_java=$(readlink -f "$(command -v java)" 2>/dev/null || true)
 major=$(java -version 2>&1 | head -n1 | grep -oE '[0-9]+' | head -n1 || true)
 fi
 [ -n "$major" ] || major="${JAVA_VER_OPTION:-21}"
 local v=""
 if [[ "$real_java" != /usr/lib/jvm/temurin-* ]]; then
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
 v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}\n" "java-${major}-openjdk-devel" 2>/dev/null | grep -E '^[0-9]' | head -n1 | cut -d- -f1 || true)
 [ -z "$v" ] && v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}\n" "java-${major}-openjdk" 2>/dev/null | grep -E '^[0-9]' | head -n1 | cut -d- -f1 || true)
 else
 v=$(apt-cache policy "openjdk-${major}-jdk" 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d+ -f1 || true)
 [ "$v" = "(none)" ] && v=""
 fi
 fi
 [ -z "$v" ] && v=$(adoptium_latest_version "$major" 5)
 [ -n "$v" ] && echo "$v" || echo "-"
 ;;
 "PostgreSQL")
 # Ikuti major cluster yang terpasang; jangan tawarkan upgrade major (mis. 16 -> 18)
 local major=""
 if command -v psql >/dev/null 2>&1; then
 major=$(psql --version 2>/dev/null | awk '{print $3}' | cut -d. -f1)
 fi
 [ -n "$major" ] || major="${PG_VER_OPTION:-18}"
 local v=""
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
 if [ "$major" = "18" ]; then
   v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" postgresql-server 2>/dev/null | cut -d- -f1)
 else
   v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" "postgresql${major}-server" 2>/dev/null | cut -d- -f1)
   [ -z "$v" ] && rpm -q "postgresql${major}-server" >/dev/null 2>&1 && v=$(rpm -q --qf "%{version}\n" "postgresql${major}-server" 2>/dev/null | cut -d- -f1)
 fi
 else
 v=$(apt-cache policy "postgresql-${major}" 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1)
 fi
 [ -n "$v" ] && echo "$v" || echo "-"
 ;;
    "Redis")
      local v=""
      if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
        if rpm -q valkey >/dev/null 2>&1 || (! rpm -q redis >/dev/null 2>&1 && command -v valkey-server >/dev/null 2>&1); then
          v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" valkey 2>/dev/null | cut -d: -f2 | cut -d- -f1)
        fi
        [ -z "$v" ] && v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" redis 2>/dev/null | cut -d: -f2 | cut -d- -f1)
        [ -z "$v" ] && v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" valkey 2>/dev/null | cut -d: -f2 | cut -d- -f1)
      else
        v=$(apt-cache policy redis-server 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d: -f2 | cut -d- -f1)
      fi
      [ -n "$v" ] && echo "$v" || echo "-"
      ;;
    "Flutter")
      local v
      v=$(curl -sSL -m 5 "https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json" 2>/dev/null | jq -r '(.current_release.stable as $h | .releases[] | select(.hash == $h) | .version)' 2>/dev/null || true)
      [ -n "$v" ] && [ "$v" != "null" ] && echo "$v" || echo "-"
      ;;
    "Android SDK")
 local sdkmanager_bin="/usr/local/android-sdk/cmdline-tools/latest/bin/sdkmanager"
 local latest_api=""
 if [ -x "$sdkmanager_bin" ] && command -v java >/dev/null 2>&1; then
   # Hanya ambil rilis platform API stabil resmi (bilangan bulat, abaikan prarilis desimal/canary seperti 37.x)
   latest_api=$(timeout 25 "$sdkmanager_bin" --list 2>/dev/null | grep -oE 'platforms[;/]android-[0-9]+[[:space:]]' | tr -d ' ' | sed 's#.*android-##' | sort -n | tail -n1)
 fi
 if [ -n "$latest_api" ]; then
 echo "API ${latest_api}"
 else
 echo "-"
 fi
      ;;
 "Orca IDE")
 local v
 v=$(curl -sSL -m 5 "https://api.github.com/repos/stablyai/orca/releases/latest" 2>/dev/null | jq -r '.tag_name // empty' 2>/dev/null | tr -d 'v' || true)
 [ -n "$v" ] && echo "$v" || echo "-"
 ;;
 "cloudflared")
 local v
 v=$(get_github_latest_tag "cloudflare/cloudflared" "-")
 [ -n "$v" ] && [ "$v" != "-" ] && echo "${v#v}" || echo "-"
 ;;
    "Nginx")
      local v
      if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
        v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" nginx 2>/dev/null | cut -d- -f1)
      else
        v=$(apt-cache policy nginx 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1 | cut -d: -f2)
      fi
 [ -z "$v" ] && v="-"
 echo "$v"
 ;;
 "PHP")
 # Ikuti major.minor yang terpasang (atau opsi --php-ver)
 local v="" cur_pv=""
 if command -v php >/dev/null 2>&1; then
 cur_pv=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;' 2>/dev/null || true)
 fi
 if [ -z "$cur_pv" ]; then
 cur_pv="${PHP_VER_OPTION:-8.4}"
 [ "$cur_pv" = "system" ] && cur_pv=""
 fi
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
 v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" php 2>/dev/null | cut -d- -f1)
 [ -z "$v" ] && [ -n "$cur_pv" ] && v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" "php${cur_pv//./}" 2>/dev/null | cut -d- -f1)
 else
 if [ -n "$cur_pv" ]; then
 v=$(apt-cache policy "php${cur_pv}" 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1 | cut -d: -f2)
 fi
 [ -z "$v" ] && v=$(apt-cache policy php 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1 | cut -d: -f2)
 fi
 [ -z "$v" ] && v="-"
 echo "$v"
 ;;
 "Composer")
 local v
 v=$(curl -sSL -m 5 "https://getcomposer.org/versions" 2>/dev/null | jq -r '.stable[0].version // empty' 2>/dev/null || true)
 [ -n "$v" ] || v="-"
 echo "$v"
 ;;
 "Zsh")
 local v
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
 v=$(dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{version}" zsh 2>/dev/null | cut -d- -f1)
 else
 v=$(apt-cache policy zsh 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d- -f1 | cut -d: -f2)
 fi
 [ -n "$v" ] || v="-"
 echo "$v"
 ;;
    "Oh My Zsh")
      if [ -d "$TARGET_HOME/.oh-my-zsh/.git" ]; then
        local remote_sha
        remote_sha=$(git ls-remote --heads https://github.com/ohmyzsh/ohmyzsh.git master 2>/dev/null | awk '{print substr($1,1,7)}' || true)
        if [ -n "$remote_sha" ]; then
          echo "$remote_sha"
        else
          git -C "$TARGET_HOME/.oh-my-zsh" rev-parse --short HEAD 2>/dev/null || echo "Terpasang"
        fi
      else
        echo "-"
      fi
      ;;
    *)
      echo "-"
      ;;
  esac
}

collect_installed_versions() {
 local -n target_map=$1
 local components=("Node.js" "Rustc" "Cargo" "Mold Linker" "Sccache" "Golang" "OpenJDK (Java)" "PostgreSQL" "Redis" "Nginx" "PHP" "Composer" "Zsh" "Oh My Zsh" "Flutter" "Android SDK" "Orca IDE" "cloudflared")
 local tmp_dir i=0 comp
 tmp_dir=$(mktemp -d)
 for comp in "${components[@]}"; do
 i=$((i + 1))
 ( get_component_version "$comp" < /dev/null > "$tmp_dir/$i" 2>/dev/null ) &
 done
 wait || true
 i=0
 for comp in "${components[@]}"; do
 i=$((i + 1))
 target_map["$comp"]=$(cat "$tmp_dir/$i" 2>/dev/null)
 [ -n "${target_map[$comp]}" ] || target_map["$comp"]="-"
 done
 rm -rf "$tmp_dir"
}

collect_latest_versions() {
 local -n target_map=$1
 local components=("Node.js" "Rustc" "Cargo" "Mold Linker" "Sccache" "Golang" "OpenJDK (Java)" "PostgreSQL" "Redis" "Nginx" "PHP" "Composer" "Zsh" "Oh My Zsh" "Flutter" "Android SDK" "Orca IDE" "cloudflared")
 local tmp_dir i=0 comp
 tmp_dir=$(mktemp -d)
 for comp in "${components[@]}"; do
 i=$((i + 1))
 ( get_latest_version "$comp" < /dev/null > "$tmp_dir/$i" 2>/dev/null ) &
 done
 wait || true
 i=0
 for comp in "${components[@]}"; do
 i=$((i + 1))
 target_map["$comp"]=$(cat "$tmp_dir/$i" 2>/dev/null)
 [ -n "${target_map[$comp]}" ] || target_map["$comp"]="-"
 done
 rm -rf "$tmp_dir"
}

print_version_table() {
  local -n col1_map=$1
  local -n col2_map=$2
  local title="${3:-RINGKASAN PERBANDINGAN VERSI}"
  local col1_label="${4:-Versi Sebelum}"
  local col2_label="${5:-Versi Sesudah}"
  local mode="${6:-update}" # "update" atau "check"

  local c_reset="\e[0m"
  local c_bold="\e[1m"
  local c_green="\e[32m"
  local c_yellow="\e[33m"
  local c_cyan="\e[36m"
  local c_dim="\e[2m"

  echo ""
  echo -e "${c_bold}========================================================================================${c_reset}"
  printf "${c_bold}%-88s${c_reset}\n" "                      $title"
  echo -e "${c_bold}========================================================================================${c_reset}"
  printf "${c_bold} %-18s | %-20s | %-20s | %-20s ${c_reset}\n" "Komponen" "$col1_label" "$col2_label" "Status"
  echo "-------------------+----------------------+----------------------+----------------------"

  local components=("Node.js" "Rustc" "Cargo" "Mold Linker" "Sccache" "Golang" "OpenJDK (Java)" "PostgreSQL" "Redis" "Nginx" "PHP" "Composer" "Zsh" "Oh My Zsh" "Flutter" "Android SDK" "Orca IDE" "cloudflared")

 for comp in "${components[@]}"; do
 local v1="${col1_map[$comp]:--}"
 local v2="${col2_map[$comp]:--}"
 local status="Up-to-date"
 local color="$c_green"
 local numeric_re='^[0-9]+(\.[0-9]+)*$'

 if [ "$v1" = "-" ] && [ "$v2" = "-" ]; then
 status="Belum Terpasang"
 color="$c_dim"
 elif [ "$v1" = "-" ] && [ "$v2" != "-" ]; then
 if [ "$mode" = "check" ]; then
 status="Belum Terpasang"
 color="$c_yellow"
 else
 status="Terpasang Baru"
 color="$c_cyan"
 fi
 elif [ "$v2" = "-" ]; then
 status="Tidak Diketahui"
 color="$c_dim"
 elif [ "$v1" != "$v2" ]; then
 if [ "$mode" = "check" ] && [[ "$v1" =~ $numeric_re ]] && [[ "$v2" =~ $numeric_re ]] && ver_ge "$v1" "$v2"; then
 status="Up-to-date"
 color="$c_green"
 elif [ "$mode" = "check" ]; then
 status="Tersedia Pembaruan"
 color="$c_yellow"
 else
 status="Diperbarui (Updated)"
 color="$c_cyan"
 fi
 else
 status="Up-to-date"
 color="$c_green"
 fi

    printf " %-18s | %-20s | %-20s | ${color}%-20s${c_reset} \n" "$comp" "$v1" "$v2" "$status"
  done

  echo -e "${c_bold}========================================================================================${c_reset}"
  echo ""
}

# --- STEP 1: Dependensi Dasar ---
step_base_deps() {
  detect_os
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    echo "Memperbarui repositori dan memasang dependensi dasar (RPM/DNF)..."
 dnf -y install curl wget git gnupg2 ca-certificates jq tar clang iproute xorg-x11-server-Xvfb \
 unzip zip xz cmake ninja-build pkgconf-pkg-config gtk3-devel xz-devel mesa-demos dpkg gcc gcc-c++ make python3
 else
 export DEBIAN_FRONTEND=noninteractive
 apt-get update -y
 apt-get install -y curl wget git gnupg lsb-release ca-certificates build-essential jq tar clang iproute2 xvfb \
 unzip zip xz-utils cmake ninja-build pkg-config libgtk-3-dev liblzma-dev mesa-utils python3
  fi
}

# --- STEP 2: Node.js (Multi-version via node-switch) ---
NODE_VER_OPTION=""

prompt_node_options() {
  if [ -n "$NODE_VER_OPTION" ]; then
    return 0
  fi

  detect_os
  local TTY_OUT="/dev/stdout"
  local TTY_IN="/dev/stdin"

  if [ -w /dev/tty ] && [ -r /dev/tty ]; then
    TTY_OUT="/dev/tty"
    TTY_IN="/dev/tty"
  elif [ ! -t 0 ]; then
    NODE_VER_OPTION="22"
    return 0
  fi

  echo "" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "       PILIH VERSI DEFAULT NODE.JS        " > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "Komponen ini mendukung multi-versi (20, 22, 24, dll.)." > "$TTY_OUT"
  echo "Pilih versi Node.js yang akan dijadikan sebagai DEFAULT sistem:" > "$TTY_OUT"
  echo "" > "$TTY_OUT"
  echo "1. Node.js 22 LTS (Active LTS - Sangat Direkomendasikan) [Default]" > "$TTY_OUT"
  echo "2. Node.js 20 LTS (Maintenance LTS)" > "$TTY_OUT"
  echo "3. Node.js 24 (Current / Fitur Terbaru)" > "$TTY_OUT"
  echo "------------------------------------------" > "$TTY_OUT"
  echo "Catatan: Anda dapat berganti versi kapan saja via perintah 'node-switch'." > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  printf "%s" "Pilih versi Node.js default [1-3, default: 1]: " > "$TTY_OUT"
  local n_choice=""
  read -r n_choice < "$TTY_IN"
  case "$n_choice" in
    2) NODE_VER_OPTION="20" ;;
    3) NODE_VER_OPTION="24" ;;
    *) NODE_VER_OPTION="22" ;;
  esac
}

install_node_switch_helper() {
  local target_dest="${1:-/usr/local/bin/node-switch}"
  local tmp_dest="${target_dest}.tmp.$$"
  mkdir -p "$(dirname "$target_dest")" 2>/dev/null || true
  cat <<'EOF_NODE_SWITCH' > "$tmp_dest"
#!/bin/bash
set -euo pipefail

# node-switch: CLI helper untuk mengelola & beralih multi-versi Node.js (20, 22, 24, dll.)
# Dibuat otomatis oleh setup_dev.sh

NODE_BASE_DIR="/usr/local/lib/nodejs"
NODE_CURRENT_LINK="$NODE_BASE_DIR/current"
PROFILE_D_FILE="/etc/profile.d/nodejs.sh"

C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"
C_DIM="\e[2m"

# Otomatis pasang/perbarui ke /usr/local/bin jika dieksekusi dengan akses root
if [ "$EUID" -eq 0 ] && [ "$0" != "/usr/local/bin/node-switch" ]; then
  mkdir -p /usr/local/bin 2>/dev/null || true
  cp -f "$0" /usr/local/bin/node-switch 2>/dev/null && chmod 755 /usr/local/bin/node-switch 2>/dev/null || true
fi

require_root() {
  if [ "$EUID" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
      exec sudo "$0" "$@"
    else
      echo -e "${C_RED}Error: Perintah ini memerlukan akses root (sudo node-switch ...)${C_RESET}"
      exit 1
    fi
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64) echo "x64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armhf) echo "armv7l" ;;
    *) echo "x64" ;;
  esac
}

get_fallback_version() {
  local major="$1"
  case "$major" in
    24) echo "24.22.3" ;;
    22) echo "22.23.5" ;;
    20) echo "20.20.2" ;;
    18) echo "18.20.7" ;;
    *) echo "${major}.0.0" ;;
  esac
}

resolve_remote_version() {
  local query="$1"
  local clean="${query#v}"
  local resolved=""

  if [ "$clean" = "lts" ]; then
    resolved=$(curl -sSL -m 5 "https://nodejs.org/dist/index.json" 2>/dev/null | jq -r '[.[] | select(.lts != false)][0].version // empty' 2>/dev/null || true)
    [ -n "$resolved" ] && { echo "${resolved#v}"; return 0; }
    echo "22.23.5"
    return 0
  fi

  if [ "$clean" = "latest" ] || [ "$clean" = "current" ]; then
    resolved=$(curl -sSL -m 5 "https://nodejs.org/dist/index.json" 2>/dev/null | jq -r '.[0].version // empty' 2>/dev/null || true)
    [ -n "$resolved" ] && { echo "${resolved#v}"; return 0; }
    echo "24.22.3"
    return 0
  fi

  # Jika hanya memasukkan angka major (misal: 20, 22, 24)
  if [[ "$clean" =~ ^[0-9]+$ ]]; then
    resolved=$(curl -sSL -m 5 "https://nodejs.org/dist/index.json" 2>/dev/null | jq -r --arg maj "v${clean}." '[.[] | select(.version | startswith($maj))][0].version // empty' 2>/dev/null || true)
    if [ -n "$resolved" ]; then
      echo "${resolved#v}"
      return 0
    fi
    get_fallback_version "$clean"
    return 0
  fi

  # Jika memasukkan versi lengkap (misal: 20.18.0)
  echo "$clean"
}

find_local_version_dir() {
  local query="$1"
  local clean="${query#v}"
  local arch
  arch=$(detect_arch)

  # 1. Cek exact match: node-v${clean}-linux-${arch}
  if [ -d "$NODE_BASE_DIR/node-v${clean}-linux-${arch}" ]; then
    echo "$NODE_BASE_DIR/node-v${clean}-linux-${arch}"
    return 0
  fi

  # 2. Cek major match: node-v${clean}.*-linux-${arch} (ambil versi tertinggi)
  local found
  found=$(find "$NODE_BASE_DIR" -maxdepth 1 -type d -name "node-v${clean}.*-linux-${arch}" 2>/dev/null | sort -V | tail -n1 || true)
  if [ -n "$found" ] && [ -d "$found" ]; then
    echo "$found"
    return 0
  fi

  return 1
}

ensure_environment_profile() {
  cat <<'EOF_ENV' > "$PROFILE_D_FILE"
export NODE_HOME="/usr/local/lib/nodejs/current"
if [ -d "$NODE_HOME/bin" ]; then
  case ":$PATH:" in
    *:"$NODE_HOME/bin":*) ;;
    *) export PATH="$NODE_HOME/bin:$PATH" ;;
  esac
fi
EOF_ENV
  chmod 644 "$PROFILE_D_FILE" 2>/dev/null || true
}

fix_permissions() {
  local target_u="${SUDO_USER:-$USER}"
  if [ -n "$target_u" ] && id "$target_u" >/dev/null 2>&1; then
    chown -R "$target_u:$target_u" "$NODE_BASE_DIR" 2>/dev/null || true
  fi
  chmod -R u+rwX,go+rX "$NODE_BASE_DIR" 2>/dev/null || true
}

sync_system_links() {
  mkdir -p /usr/local/bin

  local bins=("node" "npm" "npx" "corepack")
  for b in "${bins[@]}"; do
    if [ -e "$NODE_CURRENT_LINK/bin/$b" ]; then
      ln -sf "$NODE_CURRENT_LINK/bin/$b" "/usr/local/bin/$b"

      # Tangani jika /usr/bin/$b adalah file binary asli dari sistem
      if [ -f "/usr/bin/$b" ] && [ ! -L "/usr/bin/$b" ]; then
        mv "/usr/bin/$b" "/usr/bin/${b}.orig" 2>/dev/null || true
        ln -sf "/usr/local/bin/$b" "/usr/bin/$b" 2>/dev/null || true
      elif [ -L "/usr/bin/$b" ]; then
        ln -sf "/usr/local/bin/$b" "/usr/bin/$b" 2>/dev/null || true
      fi
    fi
  done
}

download_and_install_node() {
  local full_ver="$1"
  local arch
  arch=$(detect_arch)
  local target_dir="$NODE_BASE_DIR/node-v${full_ver}-linux-${arch}"

  if [ -d "$target_dir" ] && [ -x "$target_dir/bin/node" ]; then
    return 0
  fi

  echo -e "${C_CYAN}Mengunduh Node.js v${full_ver} (${arch})...${C_RESET}"
  local tmp_dir
  tmp_dir=$(mktemp -d)
  local tarball="$tmp_dir/node.tar.xz"
  local url="https://nodejs.org/dist/v${full_ver}/node-v${full_ver}-linux-${arch}.tar.xz"
  local shasum_url="https://nodejs.org/dist/v${full_ver}/SHASUMS256.txt"

  if ! curl -fsSL -m 60 "$url" -o "$tarball"; then
    echo -e "${C_RED}Error: Gagal mengunduh Node.js dari $url${C_RESET}"
    rm -rf "$tmp_dir"
    return 1
  fi

  # Verifikasi Checksum SHA256
  local expected_sha
  expected_sha=$(curl -fsSL -m 10 "$shasum_url" 2>/dev/null | grep "node-v${full_ver}-linux-${arch}\.tar\.xz" | awk '{print $1}' || true)
  if [ -n "$expected_sha" ]; then
    local actual_sha
    actual_sha=$(sha256sum "$tarball" | awk '{print $1}')
    if [ "$actual_sha" != "$expected_sha" ]; then
      echo -e "${C_RED}Error: Checksum SHA256 Node.js tidak cocok!${C_RESET}"
      rm -rf "$tmp_dir"
      return 1
    fi
  fi

  echo -e "${C_CYAN}Mengekstrak ke $target_dir...${C_RESET}"
  mkdir -p "$NODE_BASE_DIR"
  if ! tar -C "$NODE_BASE_DIR" -xJf "$tarball"; then
    echo -e "${C_RED}Error: Gagal mengekstrak tarball Node.js!${C_RESET}"
    rm -rf "$tmp_dir"
    return 1
  fi
  rm -rf "$tmp_dir"

  # Buat symlink versi major untuk kemudahan
  local major
  major=$(echo "$full_ver" | cut -d. -f1)
  ln -sfn "node-v${full_ver}-linux-${arch}" "$NODE_BASE_DIR/v${major}" 2>/dev/null || true

  fix_permissions
}

cmd_list() {
  echo -e "${C_BOLD}============================================================${C_RESET}"
  echo -e "${C_BOLD}                 STATUS MULTI-VERSI NODE.JS                 ${C_RESET}"
  echo -e "${C_BOLD}============================================================${C_RESET}"

  local cur_node="Belum terpasang"
  local cur_npm="-"
  local cur_path="-"

  if command -v node >/dev/null 2>&1; then
    cur_node="$(node -v 2>/dev/null)"
    cur_npm="$(npm -v 2>/dev/null || echo '-')"
    cur_path="$(command -v node)"
    if [ -L "$cur_path" ]; then
      cur_path="${cur_path} -> $(readlink -f "$cur_path" 2>/dev/null || true)"
    fi
  fi

  echo -e "  Node.js Aktif     : ${C_GREEN}${cur_node}${C_RESET}"
  echo -e "  NPM Aktif         : ${C_GREEN}${cur_npm}${C_RESET}"
  echo -e "  Lokasi Binary     : ${C_DIM}${cur_path}${C_RESET}"
  echo -e "------------------------------------------------------------"
  echo -e "  ${C_BOLD}Versi Terpasang di Sistem:${C_RESET}"

  local arch
  arch=$(detect_arch)
  local found_any=0
  local active_v="${cur_node#v}"

  if [ -d "$NODE_BASE_DIR" ]; then
    for dir in "$NODE_BASE_DIR"/node-v*-linux-"$arch"; do
      if [ -d "$dir" ] && [ -x "$dir/bin/node" ]; then
        found_any=1
        local bname
        bname=$(basename "$dir")
        local v_clean
        v_clean=$(echo "$bname" | sed -e "s/^node-v//" -e "s/-linux-${arch}$//")
        local is_active=""
        if [ "$v_clean" = "$active_v" ]; then
          is_active=" ${C_GREEN}(Aktif)${C_RESET}"
          echo -e "    ${C_GREEN}* ${v_clean}${is_active}${C_RESET}"
        else
          echo -e "      ${v_clean}"
        fi
      fi
    done
  fi

  # Cek jika ada versi bawaan paket sistem jika belum termigrasi
  local sys_label="Sistem"
  command -v dnf >/dev/null 2>&1 && sys_label="Sistem RPM/DNF"
  command -v apt-get >/dev/null 2>&1 && sys_label="Sistem APT"

  if [ -f "/usr/bin/node.orig" ] || [ -f "/usr/bin/node.apt-orig" ]; then
    local orig_bin="/usr/bin/node.orig"
    [ -f "$orig_bin" ] || orig_bin="/usr/bin/node.apt-orig"
    local orig_v
    orig_v=$("$orig_bin" -v 2>/dev/null || true)
    echo -e "      ${orig_v#v} ${C_DIM}(Cadangan Paket ${sys_label})${C_RESET}"
  elif [ "$found_any" -eq 0 ] && command -v node >/dev/null 2>&1; then
    echo -e "    ${C_GREEN}* ${active_v} ${C_GREEN}(Aktif - ${sys_label})${C_RESET}"
    found_any=1
  fi

  if [ "$found_any" -eq 0 ]; then
    echo -e "    ${C_DIM}(Belum ada versi Node.js yang terpasang)${C_RESET}"
  fi

  echo -e "------------------------------------------------------------"
  echo -e "  ${C_BOLD}Preset Versi Populer:${C_RESET}"
  echo -e "    - ${C_CYAN}22${C_RESET} : Node.js 22 LTS (Active LTS)"
  echo -e "    - ${C_CYAN}20${C_RESET} : Node.js 20 LTS (Maintenance LTS)"
  echo -e "    - ${C_CYAN}24${C_RESET} : Node.js 24 (Current / Fitur Terbaru)"
  echo -e "============================================================"
  echo -e "Perintah Beralih Versi: ${C_CYAN}node-switch <versi>${C_RESET}"
  echo -e "Contoh:"
  echo -e "  ${C_CYAN}node-switch 20${C_RESET}        -> Beralih ke Node.js 20 LTS"
  echo -e "  ${C_CYAN}node-switch 22${C_RESET}        -> Beralih ke Node.js 22 LTS"
  echo -e "  ${C_CYAN}node-switch 24${C_RESET}        -> Beralih ke Node.js 24 Current"
}

cmd_switch() {
  local req_ver="${1:-}"
  if [ -z "$req_ver" ]; then
    echo -e "${C_RED}Error: Masukkan versi target! (contoh: node-switch 20 atau node-switch 22)${C_RESET}"
    exit 1
  fi

  require_root "$@"

  local arch
  arch=$(detect_arch)
  local target_dir=""

  # 1. Cek apakah sudah terpasang lokal
  target_dir=$(find_local_version_dir "$req_ver" 2>/dev/null || true)

  # 2. Jika belum ada di lokal, unduh versi resmi
  if [ -z "$target_dir" ] || [ ! -x "$target_dir/bin/node" ]; then
    local resolved_ver
    resolved_ver=$(resolve_remote_version "$req_ver")
    download_and_install_node "$resolved_ver"
    target_dir="$NODE_BASE_DIR/node-v${resolved_ver}-linux-${arch}"
  fi

  if [ ! -d "$target_dir" ] || [ ! -x "$target_dir/bin/node" ]; then
    echo -e "${C_RED}Error: Gagal menemukan binary Node.js di $target_dir${C_RESET}"
    exit 1
  fi

  # 3. Alihkan symlink active current
  mkdir -p "$NODE_BASE_DIR"
  ln -sfn "$target_dir" "$NODE_CURRENT_LINK"

  # 4. Sinkronkan symlink global di /usr/local/bin & /usr/bin
  sync_system_links

  # 5. Konfigurasi profile.d
  ensure_environment_profile

  fix_permissions

  local new_ver
  new_ver=$("$NODE_CURRENT_LINK/bin/node" -v 2>/dev/null || echo "v?")
  local new_npm
  new_npm=$("$NODE_CURRENT_LINK/bin/npm" -v 2>/dev/null || echo "?")

  echo -e "${C_GREEN}[✓] Berhasil beralih ke Node.js ${new_ver} (NPM ${new_npm})${C_RESET}"
  echo -e "${C_DIM}    Path: $NODE_CURRENT_LINK/bin/node${C_RESET}"
}

cmd_install() {
  local req_ver="${1:-}"
  if [ -z "$req_ver" ]; then
    echo -e "${C_RED}Error: Masukkan versi yang ingin diinstal! (contoh: node-switch install 20)${C_RESET}"
    exit 1
  fi
  require_root "$@"
  local resolved_ver
  resolved_ver=$(resolve_remote_version "$req_ver")
  download_and_install_node "$resolved_ver"
  echo -e "${C_GREEN}[✓] Node.js v${resolved_ver} berhasil diinstal.${C_RESET}"
  echo -e "Gunakan: ${C_CYAN}node-switch ${resolved_ver}${C_RESET} untuk mengaktifkannya."
}

cmd_remove() {
  local req_ver="${1:-}"
  if [ -z "$req_ver" ]; then
    echo -e "${C_RED}Error: Masukkan versi yang ingin dihapus! (contoh: node-switch remove 20)${C_RESET}"
    exit 1
  fi
  require_root "$@"
  local target_dir
  target_dir=$(find_local_version_dir "$req_ver" 2>/dev/null || true)
  if [ -z "$target_dir" ] || [ ! -d "$target_dir" ]; then
    echo -e "${C_YELLOW}Peringatan: Versi ${req_ver} tidak ditemukan di ${NODE_BASE_DIR}.${C_RESET}"
    exit 0
  fi

  # Cek jika versi yang ingin dihapus sedang aktif
  if [ -L "$NODE_CURRENT_LINK" ] && [ "$(readlink -f "$NODE_CURRENT_LINK")" = "$(readlink -f "$target_dir")" ]; then
    echo -e "${C_RED}Error: Tidak dapat menghapus versi yang sedang aktif! Beralihlah ke versi lain terlebih dahulu.${C_RESET}"
    exit 1
  fi

  rm -rf "$target_dir"
  echo -e "${C_GREEN}[✓] Berhasil menghapus $(basename "$target_dir").${C_RESET}"
}

case "${1:-}" in
  list|ls|status)
    cmd_list
    ;;
  switch)
    shift
    cmd_switch "${1:-}"
    ;;
  install|add)
    shift
    cmd_install "${1:-}"
    ;;
  remove|rm|uninstall)
    shift
    cmd_remove "${1:-}"
    ;;
  help|--help|-h)
    echo "Penggunaan: node-switch <perintah|versi>"
    echo ""
    echo "Perintah:"
    echo "  node-switch [list|status]     Tampilkan status dan daftar versi Node.js"
    echo "  node-switch <20|22|24|ver>    Beralih versi Node.js aktif (otomatis unduh jika belum ada)"
    echo "  node-switch switch <ver>      Beralih ke versi tertentu"
    echo "  node-switch install <ver>     Unduh dan instal versi tertentu tanpa switch"
    echo "  node-switch remove <ver>      Hapus versi yang terpasang"
    echo ""
    echo "Contoh:"
    echo "  node-switch 20"
    echo "  node-switch 22"
    echo "  node-switch 24"
    echo "  node-switch list"
    ;;
  *)
    if [ -n "${1:-}" ]; then
      cmd_switch "$1"
    else
      cmd_list
    fi
    ;;
esac
EOF_NODE_SWITCH
  chmod 755 "$tmp_dest"
  mv -f "$tmp_dest" "$target_dest"

  if [ "$target_dest" = "/usr/local/bin/node-switch" ]; then
    local target_user="${SUDO_USER:-}"
    if [ -n "$target_user" ] && id "$target_user" >/dev/null 2>&1; then
      local u_home
      u_home=$(getent passwd "$target_user" 2>/dev/null | cut -d: -f6)
      if [ -n "$u_home" ] && [ -d "$u_home" ]; then
        mkdir -p "$u_home/.local/bin" 2>/dev/null || true
        cp -f "$target_dest" "$u_home/.local/bin/node-switch" 2>/dev/null || true
        chown "$target_user:$target_user" "$u_home/.local/bin/node-switch" 2>/dev/null || true
        chmod 755 "$u_home/.local/bin/node-switch" 2>/dev/null || true
      fi
    fi
  fi
}

ensure_node_switch_helper() {
  local u_user="${SUDO_USER:-$USER}"
  local u_home
  u_home=$(getent passwd "$u_user" 2>/dev/null | cut -d: -f6)
  [ -z "$u_home" ] && u_home="$HOME"
  local u_dir="$u_home/.local/bin"
  mkdir -p "$u_dir" 2>/dev/null || true
  install_node_switch_helper "$u_dir/node-switch"

  if [ "$EUID" -eq 0 ]; then
    install_node_switch_helper "/usr/local/bin/node-switch"
  fi
}

step_nodejs() {
  detect_os
  prompt_node_options
  local target_node="${NODE_VER_OPTION:-22}"

  echo "Mengonfigurasi Node.js $target_node via node-switch..."
  ensure_node_switch_helper

  local node_bin="/usr/local/bin/node-switch"
  if [ ! -x "$node_bin" ]; then
    install_node_switch_helper "$node_bin"
  fi

  if [ -x "$node_bin" ]; then
    "$node_bin" switch "$target_node"
  else
    echo "Error: gagal memasang helper node-switch."
    return 1
  fi
}

# --- STEP 3: Rust + Mold + Sccache ---
step_rust() {
  detect_os
  local ARCH
  ARCH=$(uname -m)

  # 1. Mold Linker
  if ! command -v mold >/dev/null 2>&1; then
    if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
      dnf -y install mold 2>/dev/null || true
    else
      export DEBIAN_FRONTEND=noninteractive
      apt-get install -y mold 2>/dev/null || true
    fi

    if ! command -v mold >/dev/null 2>&1; then
      local MOLD_VER MOLD_TMP_DIR MOLD_TAR MOLD_ASSET MOLD_SHA
      MOLD_VER=$(get_github_latest_tag "rui314/mold" "v3.0.0")
      MOLD_TMP_DIR=$(mktemp -d)
      TMP_CLEANUP_FILES+=("$MOLD_TMP_DIR")
      MOLD_TAR="$MOLD_TMP_DIR/mold.tar.gz"
      MOLD_ASSET="mold-${MOLD_VER#v}-${ARCH}-linux.tar.gz"
      if ! wget -qO "$MOLD_TAR" "https://github.com/rui314/mold/releases/download/${MOLD_VER}/${MOLD_ASSET}"; then
        rm -rf "$MOLD_TMP_DIR"
        echo "Error: gagal mengunduh mold ${MOLD_VER}."
        return 1
      fi
      MOLD_SHA=$(get_github_asset_digest "rui314/mold" "$MOLD_ASSET")
      if ! verify_sha256 "$MOLD_TAR" "$MOLD_SHA" "mold ${MOLD_VER}"; then
        rm -rf "$MOLD_TMP_DIR"
        return 1
      fi
      if ! tar -tzf "$MOLD_TAR" >/dev/null 2>&1; then
        rm -rf "$MOLD_TMP_DIR"
        echo "Error: arsip mold ${MOLD_VER} rusak atau tidak valid."
        return 1
      fi
      if ! tar --no-same-owner -C /usr/local --strip-components=1 -xzf "$MOLD_TAR"; then
        rm -rf "$MOLD_TMP_DIR"
        echo "Error: gagal mengekstrak mold ${MOLD_VER}."
        return 1
      fi
      rm -rf "$MOLD_TMP_DIR"
    fi
  fi

  # 2. Sccache (Deteksi Dinamis Versi Terbaru GitHub)
  local SCCACHE_VER
  SCCACHE_VER=$(get_github_latest_tag "mozilla/sccache" "v0.19.0")
  [[ "$SCCACHE_VER" != v* ]] && SCCACHE_VER="v${SCCACHE_VER}"
  local SCCACHE_CLEAN="${SCCACHE_VER#v}"

  local NEED_SCCACHE=1
  if command -v sccache >/dev/null 2>&1; then
    local CUR_SCCACHE_VER
    CUR_SCCACHE_VER=$(sccache --version 2>/dev/null | awk '{print $2}')
    if [ "$CUR_SCCACHE_VER" = "$SCCACHE_CLEAN" ]; then
      echo "Sccache sudah versi terbaru ($CUR_SCCACHE_VER)."
      NEED_SCCACHE=0
    fi
  fi

  if [ "$NEED_SCCACHE" -eq 1 ]; then
    echo "Mengunduh Sccache rilis terbaru: $SCCACHE_VER ($ARCH)..."
    local SCCACHE_TAR SCCACHE_DIR SCCACHE_ASSET SCCACHE_SHA SCCACHE_BIN
    SCCACHE_TAR=$(mktemp)
    SCCACHE_DIR=$(mktemp -d)
    TMP_CLEANUP_FILES+=("$SCCACHE_TAR" "$SCCACHE_DIR")
    SCCACHE_ASSET="sccache-${SCCACHE_VER}-${ARCH}-unknown-linux-musl.tar.gz"
    if ! wget -qO "$SCCACHE_TAR" "https://github.com/mozilla/sccache/releases/download/${SCCACHE_VER}/${SCCACHE_ASSET}"; then
      rm -f "$SCCACHE_TAR"
      rm -rf "$SCCACHE_DIR"
      echo "Error: gagal mengunduh sccache ${SCCACHE_VER}."
      return 1
    fi
    SCCACHE_SHA=$(curl -fsSL -m 10 "https://github.com/mozilla/sccache/releases/download/${SCCACHE_VER}/${SCCACHE_ASSET}.sha256" 2>/dev/null | awk '{print $1}' | head -n1 || true)
    [[ "$SCCACHE_SHA" =~ ^[0-9a-fA-F]{64}$ ]] || SCCACHE_SHA=""
    [ -z "$SCCACHE_SHA" ] && SCCACHE_SHA=$(get_github_asset_digest "mozilla/sccache" "$SCCACHE_ASSET")
    if ! verify_sha256 "$SCCACHE_TAR" "$SCCACHE_SHA" "sccache ${SCCACHE_VER}"; then
      rm -f "$SCCACHE_TAR"
      rm -rf "$SCCACHE_DIR"
      return 1
    fi
    if ! tar -tzf "$SCCACHE_TAR" >/dev/null 2>&1; then
      rm -f "$SCCACHE_TAR"
      rm -rf "$SCCACHE_DIR"
      echo "Error: arsip sccache ${SCCACHE_VER} rusak atau tidak valid."
      return 1
    fi
    mkdir -p "$SCCACHE_DIR"
    if ! tar --no-same-owner -C "$SCCACHE_DIR" -xzf "$SCCACHE_TAR"; then
      rm -f "$SCCACHE_TAR"
      rm -rf "$SCCACHE_DIR"
      echo "Error: gagal mengekstrak sccache ${SCCACHE_VER}."
      return 1
    fi
    SCCACHE_BIN=$(find "$SCCACHE_DIR" -type f -name sccache | head -n1 || true)
    if [ -z "$SCCACHE_BIN" ] || ! install -m 755 "$SCCACHE_BIN" /usr/local/bin/sccache; then
      rm -f "$SCCACHE_TAR"
      rm -rf "$SCCACHE_DIR"
      echo "Error: binary sccache tidak ditemukan atau gagal dipasang."
      return 1
    fi
    rm -rf "$SCCACHE_DIR"
    rm -f "$SCCACHE_TAR"
  fi

  # 3. Rustup & Toolchains
  export RUSTUP_HOME=/usr/local/rustup
  export CARGO_HOME=/usr/local/cargo
  export PATH="$CARGO_HOME/bin:$PATH"

  if [ -x "$CARGO_HOME/bin/rustup" ] && [ -x "$CARGO_HOME/bin/rustc" ]; then
    echo "Rustup sudah terpasang. Memperbarui stable toolchain..."
    "$CARGO_HOME/bin/rustup" update stable --no-self-update || true
  else
    local RUSTUP_TRIPLE=""
    case "$(uname -m)" in
      x86_64) RUSTUP_TRIPLE="x86_64-unknown-linux-gnu" ;;
      aarch64|arm64) RUSTUP_TRIPLE="aarch64-unknown-linux-gnu" ;;
    esac
    if [ -z "$RUSTUP_TRIPLE" ]; then
      echo "Error: arsitektur $(uname -m) tidak didukung untuk instalasi rustup otomatis."
      return 1
    fi
    local RUSTUP_INIT_URL="https://static.rust-lang.org/rustup/dist/${RUSTUP_TRIPLE}/rustup-init"
    local RUSTUP_TMP_DIR RUSTUP_BIN RUSTUP_SHA
    # rustup-init membaca argv[0] sebagai nama proxy; nama file wajib "rustup-init"
    RUSTUP_TMP_DIR=$(mktemp -d)
    TMP_CLEANUP_FILES+=("$RUSTUP_TMP_DIR")
    RUSTUP_BIN="$RUSTUP_TMP_DIR/rustup-init"
    if ! curl --proto '=https' --tlsv1.2 -fsSL -m 120 "$RUSTUP_INIT_URL" -o "$RUSTUP_BIN"; then
      rm -rf "$RUSTUP_TMP_DIR"
      echo "Error: gagal mengunduh rustup-init."
      return 1
    fi
    RUSTUP_SHA=$(curl --proto '=https' --tlsv1.2 -fsSL -m 30 "${RUSTUP_INIT_URL}.sha256" 2>/dev/null | awk '{print $1}' | head -n1 || true)
    if [ -z "$RUSTUP_SHA" ] || ! verify_sha256 "$RUSTUP_BIN" "$RUSTUP_SHA" "rustup-init"; then
      rm -rf "$RUSTUP_TMP_DIR"
      echo "Error: checksum rustup-init tidak tersedia atau tidak cocok."
      return 1
    fi
    chmod +x "$RUSTUP_BIN"
    if ! "$RUSTUP_BIN" -y --no-modify-path --profile default; then
      rm -rf "$RUSTUP_TMP_DIR"
      echo "Error: rustup-init gagal memasang Rust toolchain."
      return 1
    fi
    rm -rf "$RUSTUP_TMP_DIR"
  fi

  if [ ! -x "$CARGO_HOME/bin/rustc" ] || [ ! -x "$CARGO_HOME/bin/cargo" ]; then
    echo "Error: rustc/cargo tidak ditemukan di $CARGO_HOME/bin setelah instalasi Rust."
    return 1
  fi

  local TARGET_USER="${SUDO_USER:-$USER}"
  if id "$TARGET_USER" >/dev/null 2>&1; then
    chown -R "$TARGET_USER:$TARGET_USER" "$RUSTUP_HOME" "$CARGO_HOME" 2>/dev/null || true
  fi
  chmod -R u+rwX,go+rX "$RUSTUP_HOME" "$CARGO_HOME" 2>/dev/null || true

  mkdir -p "$CARGO_HOME"
  local SCCACHE_PATH
  SCCACHE_PATH=$(command -v sccache 2>/dev/null || true)
  [ -n "$SCCACHE_PATH" ] || SCCACHE_PATH="/usr/local/bin/sccache"
  local CARGO_CFG="$CARGO_HOME/config.toml"
  if [ ! -f "$CARGO_CFG" ]; then
    cat <<EOF_CARGO > "$CARGO_CFG"
[build]
rustc-wrapper = "$SCCACHE_PATH"

[target.x86_64-unknown-linux-gnu]
linker = "clang"
rustflags = ["-C", "link-arg=-fuse-ld=mold"]

[target.aarch64-unknown-linux-gnu]
linker = "clang"
rustflags = ["-C", "link-arg=-fuse-ld=mold"]
EOF_CARGO
  elif ! grep -q '^[[:space:]]*rustc-wrapper[[:space:]]*=' "$CARGO_CFG"; then
    cp "$CARGO_CFG" "${CARGO_CFG}.bak.$(date +%Y%m%d_%H%M%S)" 2>/dev/null || true
    if grep -q '^\[build\]' "$CARGO_CFG"; then
      sed -i "/^\[build\]/a rustc-wrapper = \"$SCCACHE_PATH\"" "$CARGO_CFG"
    else
      printf '\n[build]\nrustc-wrapper = "%s"\n' "$SCCACHE_PATH" >> "$CARGO_CFG"
    fi
  fi

  cat <<'EOF' > /etc/profile.d/rust.sh
export RUSTUP_HOME=/usr/local/rustup
export CARGO_HOME=/usr/local/cargo
export PATH="$CARGO_HOME/bin:$PATH"
EOF
  chmod +x /etc/profile.d/rust.sh
}

# --- STEP 4: Golang Latest ---
step_golang() {
  local ARCH
  case $(dpkg --print-architecture 2>/dev/null || uname -m) in
    amd64|x86_64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    armhf|armv7l) ARCH="armv6l" ;;
    *) ARCH="amd64" ;;
  esac

  local GO_JSON
  GO_JSON=$(curl -sSL -m 10 "https://go.dev/dl/?mode=json" 2>/dev/null || true)
  if [ -z "$GO_JSON" ]; then
    echo "Peringatan: Gagal menghubungi go.dev untuk memeriksa versi terbaru."
    if command -v go >/dev/null 2>&1; then
      echo "Menggunakan versi Go yang sudah terpasang: $(go version)"
      return 0
    fi
  fi

 local GO_FILE
 GO_FILE=$(echo "$GO_JSON" | jq -r '.[0].files[] | select(.os=="linux" and .arch=="'"$ARCH"'" and .kind=="archive") | .filename' 2>/dev/null | head -n1 || true)
 local GO_SHA
 GO_SHA=$(echo "$GO_JSON" | jq -r --arg f "$GO_FILE" '.[0].files[] | select(.filename == $f) | .sha256 // empty' 2>/dev/null | head -n1 || true)
 local LATEST_VER
 LATEST_VER=$(echo "$GO_JSON" | jq -r '.[0].version' 2>/dev/null || true)

 if [ -z "$GO_FILE" ] || [ "$GO_FILE" = "null" ]; then
 echo "Error: File binary Go untuk arsitektur $ARCH tidak ditemukan."
 return 1
 fi

 # Cek jika versi terpasang sudah yang terbaru
 if command -v /usr/local/go/bin/go >/dev/null 2>&1; then
 local CURRENT_VER
 CURRENT_VER=$(/usr/local/go/bin/go version 2>/dev/null | awk '{print $3}')
 if [ -n "$LATEST_VER" ] && [ "$CURRENT_VER" = "$LATEST_VER" ]; then
 echo "Golang sudah versi terbaru ($CURRENT_VER). Melewati unduhan."
 return 0
 fi
 fi

 echo "Mengunduh Golang versi terbaru: $LATEST_VER ($GO_FILE)..."
 local GO_TMP
 GO_TMP=$(mktemp)
 TMP_CLEANUP_FILES+=("$GO_TMP")
 if ! wget -qO "$GO_TMP" "https://go.dev/dl/${GO_FILE}"; then
   rm -f "$GO_TMP"
   echo "Error: gagal mengunduh Golang $LATEST_VER ($GO_FILE)."
   return 1
 fi

 # Validasi checksum resmi dari go.dev
 if [ -n "$GO_SHA" ] && [ "$GO_SHA" != "null" ]; then
   if ! verify_sha256 "$GO_TMP" "$GO_SHA" "Go ${LATEST_VER}"; then
     rm -f "$GO_TMP"
     return 1
   fi
 fi

 # Validasi integritas arsip sebelum menghapus instalasi lama
 if ! tar -tzf "$GO_TMP" >/dev/null 2>&1; then
 echo "Error: Berkas unduhan Go rusak atau tidak valid!"
 rm -f "$GO_TMP"
 return 1
 fi

 rm -rf /usr/local/go
 if ! tar --no-same-owner -C /usr/local -xzf "$GO_TMP"; then
   rm -f "$GO_TMP"
   echo "Error: gagal mengekstrak Golang $LATEST_VER."
   return 1
 fi
 rm -f "$GO_TMP"

 cat <<'EOF' > /etc/profile.d/golang.sh
export PATH="/usr/local/go/bin:$PATH"
EOF
 chmod +x /etc/profile.d/golang.sh
}

# --- STEP 5: Java (OpenJDK Multi-version) ---
JAVA_VER_OPTION=""

prompt_java_options() {
  if [ -n "$JAVA_VER_OPTION" ]; then
    return 0
  fi

  detect_os
  local TTY_OUT="/dev/stdout"
  local TTY_IN="/dev/stdin"

  if [ -w /dev/tty ] && [ -r /dev/tty ]; then
    TTY_OUT="/dev/tty"
    TTY_IN="/dev/tty"
  elif [ ! -t 0 ]; then
    JAVA_VER_OPTION="21"
    return 0
  fi

  echo "" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "      PILIH VERSI DEFAULT OPENJDK (JAVA)  " > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "Pilih versi OpenJDK yang akan dijadikan sebagai DEFAULT sistem:" > "$TTY_OUT"
  echo "" > "$TTY_OUT"
  echo "1. OpenJDK 21 LTS (Standar Resmi Android SDK & Flutter) [Rekomendasi Default]" > "$TTY_OUT"
  echo "2. OpenJDK 17 LTS (Kompatibilitas Luas)" > "$TTY_OUT"
  echo "3. OpenJDK 25 / Versi Terbaru" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  printf "%s" "Pilih versi Java default [1-3, default: 1]: " > "$TTY_OUT"
  local j_choice=""
  read -r j_choice < "$TTY_IN"
  case "$j_choice" in
    2) JAVA_VER_OPTION="17" ;;
    3) JAVA_VER_OPTION="25" ;;
    *) JAVA_VER_OPTION="21" ;;
  esac
}

adoptium_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "x64" ;;
    aarch64|arm64) echo "aarch64" ;;
    armv7l|armhf) echo "arm" ;;
    *) return 1 ;;
  esac
}

adoptium_latest_version() {
  local major=$1 timeout_sec=${2:-10} api_arch
  api_arch=$(adoptium_arch 2>/dev/null || true)
  [ -n "$api_arch" ] || return 0
  curl -fsSL -m "$timeout_sec" "https://api.adoptium.net/v3/assets/latest/${major}/hotspot?architecture=${api_arch}&image_type=jdk&os=linux&vendor=eclipse" 2>/dev/null \
    | jq -r '.[0].version.openjdk_version // empty' 2>/dev/null \
    | sed 's/[+-].*$//' || true
}

find_java_home_for_major() {
  local major=$1 d=""
  for pattern in "java-${major}-openjdk*" "java-1.${major}.0-openjdk*" "temurin-${major}-jdk*" "java-${major}-temurin*"; do
    while IFS= read -r d; do
      [ -n "$d" ] || continue
      if [ -x "$d/bin/java" ] && [ -x "$d/bin/javac" ]; then
        echo "$d"
        return 0
      fi
    done < <(find /usr/lib/jvm -maxdepth 1 -type d -name "$pattern" 2>/dev/null | sort -V)
  done
  return 1
}

is_distro_jdk_available() {
  local major=$1
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    local pkg="java-${major}-openjdk-devel"
    rpm -q "$pkg" >/dev/null 2>&1 && return 0
    dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{name}\n" "$pkg" 2>/dev/null | grep -qx "$pkg"
    return $?
  elif [ "$PKG_MGR" = "apt" ]; then
    local pkg="openjdk-${major}-jdk"
    dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q "install ok installed" && return 0
    local cand
    cand=$(apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/ {print $2}' || true)
    if [ -z "$cand" ] && ! ls -A /var/lib/apt/lists 2>/dev/null | grep -qvE '^(lock|partial)$'; then
      apt-get update -y >/dev/null 2>&1 || true
      cand=$(apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/ {print $2}' || true)
    fi
    [ -n "$cand" ] && [ "$cand" != "(none)" ]
    return $?
  fi
  return 1
}

install_adoptium_jdk() {
  local major=$1 api_arch
  if ! api_arch=$(adoptium_arch); then
    echo "Error: arsitektur $(uname -m) tidak didukung untuk unduhan Adoptium Temurin."
    return 1
  fi

  local api_url="https://api.adoptium.net/v3/assets/latest/${major}/hotspot?architecture=${api_arch}&image_type=jdk&os=linux&vendor=eclipse"
  local api_json rel_name dl_url dl_sha
  api_json=$(curl -fsSL -m 15 "$api_url" 2>/dev/null || true)
  if [ -z "$api_json" ]; then
    echo "Error: gagal menghubungi Adoptium API untuk Eclipse Temurin OpenJDK ${major}."
    return 1
  fi
  rel_name=$(echo "$api_json" | jq -r '.[0].release_name // empty' 2>/dev/null || true)
  dl_url=$(echo "$api_json" | jq -r '.[0].binary.package.link // empty' 2>/dev/null || true)
  dl_sha=$(echo "$api_json" | jq -r '.[0].binary.package.checksum // empty' 2>/dev/null || true)
  if [ -z "$dl_url" ] || [ -z "$dl_sha" ]; then
    echo "Error: paket Eclipse Temurin OpenJDK ${major} (${api_arch}) tidak ditemukan di Adoptium API."
    return 1
  fi

  local install_dir="/usr/lib/jvm/temurin-${major}-jdk"
  local marker_file="$install_dir/.adoptium-release"
  if [ -x "$install_dir/bin/java" ] && [ -x "$install_dir/bin/javac" ] && [ -n "$rel_name" ] && [ -f "$marker_file" ] && [ "$(cat "$marker_file" 2>/dev/null)" = "$rel_name" ]; then
    echo "Eclipse Temurin OpenJDK ${major} (${rel_name}) sudah versi terbaru."
    return 0
  fi

  echo "Mengunduh Eclipse Temurin OpenJDK ${major} (${rel_name:-latest}, ${api_arch})..."
  local tmp_dir tmp_tar stage_dir backup_dir
  tmp_dir=$(mktemp -d)
  TMP_CLEANUP_FILES+=("$tmp_dir")
  tmp_tar="$tmp_dir/temurin-jdk.tar.gz"
  stage_dir="$tmp_dir/jdk"
  mkdir -p "$stage_dir"

  if ! curl -fsSL -m 300 "$dl_url" -o "$tmp_tar"; then
    rm -rf "$tmp_dir"
    echo "Error: gagal mengunduh Eclipse Temurin OpenJDK ${major}."
    return 1
  fi
  if ! verify_sha256 "$tmp_tar" "$dl_sha" "Temurin OpenJDK ${major}"; then
    rm -rf "$tmp_dir"
    return 1
  fi
  if ! tar -tzf "$tmp_tar" >/dev/null 2>&1; then
    rm -rf "$tmp_dir"
    echo "Error: arsip Eclipse Temurin OpenJDK ${major} rusak atau tidak valid."
    return 1
  fi
  if ! tar --no-same-owner -C "$stage_dir" --strip-components=1 -xzf "$tmp_tar"; then
    rm -rf "$tmp_dir"
    echo "Error: gagal mengekstrak Eclipse Temurin OpenJDK ${major}."
    return 1
  fi
  if [ ! -x "$stage_dir/bin/java" ] || [ ! -x "$stage_dir/bin/javac" ]; then
    rm -rf "$tmp_dir"
    echo "Error: binary java/javac tidak ditemukan di arsip Temurin OpenJDK ${major}."
    return 1
  fi

  mkdir -p /usr/lib/jvm
  backup_dir="${install_dir}.bak.$$"
  if [ -e "$install_dir" ]; then
    mv "$install_dir" "$backup_dir"
  fi
  if mv "$stage_dir" "$install_dir"; then
    rm -rf "$backup_dir"
  else
    [ -e "$backup_dir" ] && mv "$backup_dir" "$install_dir" || true
    rm -rf "$tmp_dir"
    echo "Error: gagal memasang Eclipse Temurin OpenJDK ${major} ke $install_dir."
    return 1
  fi

  printf '%s\n' "$rel_name" > "$marker_file"
  chown -R root:root "$install_dir" 2>/dev/null || true
  chmod -R u+rwX,go+rX "$install_dir" 2>/dev/null || true
  rm -rf "$tmp_dir"
}

register_java_alternatives() {
  local jvm_dir=$1 major=$2
  local prio=$(( major * 1000 ))
  local tool

  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    if command -v alternatives >/dev/null 2>&1; then
      local follower_flag="--slave"
      if alternatives --help 2>&1 | grep -q -- '--follower'; then
        follower_flag="--follower"
      fi

      if ! alternatives --display java 2>/dev/null | grep -Fq "$jvm_dir/bin/java"; then
        local java_followers=()
        [ -x "$jvm_dir/bin/keytool" ] && java_followers+=("$follower_flag" /usr/bin/keytool keytool "$jvm_dir/bin/keytool")
        [ -x "$jvm_dir/bin/rmiregistry" ] && java_followers+=("$follower_flag" /usr/bin/rmiregistry rmiregistry "$jvm_dir/bin/rmiregistry")
        [ -d "$jvm_dir" ] && java_followers+=("$follower_flag" /usr/lib/jvm/jre jre "$jvm_dir")
        if ! alternatives --install /usr/bin/java java "$jvm_dir/bin/java" "$prio" "${java_followers[@]}" 2>/dev/null; then
          alternatives --install /usr/bin/java java "$jvm_dir/bin/java" "$prio" 2>/dev/null || true
        fi
      fi
      alternatives --set java "$jvm_dir/bin/java" 2>/dev/null || true

      if [ -x "$jvm_dir/bin/javac" ]; then
        if ! alternatives --display javac 2>/dev/null | grep -Fq "$jvm_dir/bin/javac"; then
          local javac_followers=()
          for tool in jar jarsigner javadoc javap jcmd jconsole jdb jdeps jinfo jlink jmap jmod jpackage jps jshell jstack jstat serialver; do
            [ -x "$jvm_dir/bin/$tool" ] && javac_followers+=("$follower_flag" "/usr/bin/$tool" "$tool" "$jvm_dir/bin/$tool")
          done
          if ! alternatives --install /usr/bin/javac javac "$jvm_dir/bin/javac" "$prio" "${javac_followers[@]}" 2>/dev/null; then
            alternatives --install /usr/bin/javac javac "$jvm_dir/bin/javac" "$prio" 2>/dev/null || true
          fi
        fi
        alternatives --set javac "$jvm_dir/bin/javac" 2>/dev/null || true
      fi
    fi
  elif command -v update-alternatives >/dev/null 2>&1; then
    if ! update-alternatives --list java 2>/dev/null | grep -Fxq "$jvm_dir/bin/java"; then
      update-alternatives --install /usr/bin/java java "$jvm_dir/bin/java" "$prio" 2>/dev/null || true
    fi
    update-alternatives --set java "$jvm_dir/bin/java" 2>/dev/null || true

    if [ -x "$jvm_dir/bin/javac" ]; then
      if ! update-alternatives --list javac 2>/dev/null | grep -Fxq "$jvm_dir/bin/javac"; then
        update-alternatives --install /usr/bin/javac javac "$jvm_dir/bin/javac" "$prio" 2>/dev/null || true
      fi
      update-alternatives --set javac "$jvm_dir/bin/javac" 2>/dev/null || true
    fi

    for tool in keytool rmiregistry jar jarsigner javadoc javap jcmd jps jshell jlink jdeps; do
      [ -x "$jvm_dir/bin/$tool" ] || continue
      if [ ! -e "/usr/bin/$tool" ] || update-alternatives --list "$tool" >/dev/null 2>&1; then
        if ! update-alternatives --list "$tool" 2>/dev/null | grep -Fxq "$jvm_dir/bin/$tool"; then
          update-alternatives --install "/usr/bin/$tool" "$tool" "$jvm_dir/bin/$tool" "$prio" 2>/dev/null || true
        fi
        update-alternatives --set "$tool" "$jvm_dir/bin/$tool" 2>/dev/null || true
      fi
    done
  fi

  local need_path_export=0
  local cur_java_real="" cur_javac_real=""
  command -v java >/dev/null 2>&1 && cur_java_real=$(readlink -f "$(command -v java)" 2>/dev/null || true)
  command -v javac >/dev/null 2>&1 && cur_javac_real=$(readlink -f "$(command -v javac)" 2>/dev/null || true)
  if [ "$cur_java_real" != "$(readlink -f "$jvm_dir/bin/java" 2>/dev/null || echo "$jvm_dir/bin/java")" ] || \
     [ "$cur_javac_real" != "$(readlink -f "$jvm_dir/bin/javac" 2>/dev/null || echo "$jvm_dir/bin/javac")" ]; then
    need_path_export=1
    export PATH="$jvm_dir/bin:$PATH"
  fi

  export JAVA_HOME="$jvm_dir"
  {
    echo "export JAVA_HOME=\"$jvm_dir\""
    if [ "$need_path_export" -eq 1 ]; then
      echo 'export PATH="$JAVA_HOME/bin:$PATH"'
    fi
  } > /etc/profile.d/java.sh
  chmod 644 /etc/profile.d/java.sh 2>/dev/null || true
}

update_installed_temurin_jdks() {
  local dir j_major
  for dir in /usr/lib/jvm/temurin-*-jdk; do
    [ -d "$dir" ] || continue
    j_major=$(basename "$dir" | sed -n 's/^temurin-\([0-9]*\)-jdk$/\1/p')
    [ -n "$j_major" ] || continue
    install_adoptium_jdk "$j_major" || echo "Peringatan: gagal memeriksa pembaruan Temurin OpenJDK ${j_major}."
  done
}

step_java() {
  detect_os
  prompt_java_options
  local target_java="${JAVA_VER_OPTION:-21}"

  case "$target_java" in
    17|21|25) ;;
    *)
      echo "Error: versi Java '$target_java' tidak didukung. Gunakan 17, 21, atau 25."
      return 1
      ;;
  esac

  if command -v javac >/dev/null 2>&1 && command -v java >/dev/null 2>&1; then
    local cur_jv real_java jvm_dir
    cur_jv=$(java -version 2>&1 | head -n1 | grep -oE '[0-9]+(\.[0-9]+)*' | head -n1 | cut -d. -f1 || true)
    if [ "$cur_jv" = "$target_java" ]; then
      echo "Java $target_java sudah aktif ($(java -version 2>&1 | head -n1)). Memeriksa pembaruan..."
      real_java=$(readlink -f "$(command -v java)" 2>/dev/null || true)
      if [[ "$real_java" == /usr/lib/jvm/temurin-* ]]; then
        install_adoptium_jdk "$target_java" || true
      elif [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
        dnf -y upgrade "java-${target_java}-openjdk-devel" 2>/dev/null || true
      else
        export DEBIAN_FRONTEND=noninteractive
        apt-get install -y --only-upgrade "openjdk-${target_java}-jdk" 2>/dev/null || true
      fi
      jvm_dir=$(find_java_home_for_major "$target_java" 2>/dev/null || true)
      [ -n "$jvm_dir" ] && register_java_alternatives "$jvm_dir" "$target_java"
      return 0
    fi
  fi

  echo "Menginstal & mengonfigurasi OpenJDK $target_java sebagai default..."
  local install_ok=0
  if is_distro_jdk_available "$target_java"; then
    if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
      dnf -y install "java-${target_java}-openjdk-devel" && install_ok=1
    elif [ "$PKG_MGR" = "apt" ]; then
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -y >/dev/null 2>&1 || true
      apt-get install -y "openjdk-${target_java}-jdk" && install_ok=1
    fi
  else
    echo "Paket OpenJDK ${target_java} tidak tersedia di repositori ${PKG_MGR}. Menggunakan Eclipse Temurin (Adoptium)..."
  fi

  if [ "$install_ok" -ne 1 ]; then
    install_adoptium_jdk "$target_java" && install_ok=1 || true
  fi

  if [ "$install_ok" -ne 1 ]; then
    echo "Error: OpenJDK ${target_java} tidak tersedia di repositori ${PKG_MGR} dan unduhan Eclipse Temurin gagal."
    local avail_vers=() v_chk
    for v_chk in 17 21 25; do
      is_distro_jdk_available "$v_chk" && avail_vers+=("$v_chk")
    done
    if [ ${#avail_vers[@]} -gt 0 ]; then
      echo "Versi yang tersedia di repositori ${PKG_MGR}: ${avail_vers[*]} (gunakan --java-ver <versi>), atau periksa koneksi internet untuk Adoptium Temurin."
    else
      echo "Periksa koneksi internet Anda untuk mengunduh Eclipse Temurin dari api.adoptium.net."
    fi
    return 1
  fi

  local jvm_dir
  jvm_dir=$(find_java_home_for_major "$target_java" || true)
  if [ -z "$jvm_dir" ]; then
    echo "Error: direktori JVM JDK untuk OpenJDK ${target_java} tidak ditemukan."
    return 1
  fi

  register_java_alternatives "$jvm_dir" "$target_java"

  local active_jv
  active_jv=$(java -version 2>&1 | head -n1 | grep -oE '[0-9]+(\.[0-9]+)*' | head -n1 | cut -d. -f1 || true)
  if [ "$active_jv" != "$target_java" ]; then
    echo "Error: Java aktif ($active_jv) bukan versi target ($target_java)."
    return 1
  fi
  if ! command -v javac >/dev/null 2>&1; then
    echo "Error: javac tidak tersedia setelah instalasi OpenJDK ${target_java}."
    return 1
  fi
  echo "OpenJDK ${target_java} aktif: $(java -version 2>&1 | head -n1) | $(javac -version 2>&1 | head -n1)"
}

# --- STEP 6: Android SDK & Command Line Tools ---
ANDROID_API_OPTION=""

prompt_android_options() {
  if [ -n "$ANDROID_API_OPTION" ]; then
    return 0
  fi

  local TTY_OUT="/dev/stdout"
  local TTY_IN="/dev/stdin"

  if [ -w /dev/tty ] && [ -r /dev/tty ]; then
    TTY_OUT="/dev/tty"
    TTY_IN="/dev/tty"
  elif [ ! -t 0 ]; then
    ANDROID_API_OPTION="all"
    return 0
  fi

  echo "" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "     PILIH VERSI ANDROID SDK PLATFORM     " > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "1. Rekomendasi Lengkap (API 36 + API 35 + API 34 & Build-Tools) [Default]" > "$TTY_OUT"
  echo "2. Android 16 (API 36 - Preview Terbaru / Baklava)" > "$TTY_OUT"
  echo "3. Android 15 (API 35 - Standar Rilis Stabil Saat Ini)" > "$TTY_OUT"
  echo "4. Android 14 (API 34 - LTS / Kompatibilitas Luas)" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  printf "%s" "Pilih paket platform Android [1-4, default: 1]: " > "$TTY_OUT"
  local a_choice=""
  read -r a_choice < "$TTY_IN"
  case "$a_choice" in
    2) ANDROID_API_OPTION="36" ;;
    3) ANDROID_API_OPTION="35" ;;
    4) ANDROID_API_OPTION="34" ;;
    *) ANDROID_API_OPTION="all" ;;
  esac
}

step_android_sdk() {
  prompt_android_options
  export DEBIAN_FRONTEND=noninteractive

  # Pastikan Java JDK terpasang karena sdkmanager & Android build membutuhkan Java + javac
  if ! command -v java >/dev/null 2>&1 || ! command -v javac >/dev/null 2>&1; then
    echo "Java JDK diperlukan untuk Android SDK. Menginstal OpenJDK ${JAVA_VER_OPTION:-21}..."
    step_java || return 1
  fi

  # Deteksi JAVA_HOME
  local JAVA_BIN
  JAVA_BIN=$(command -v java 2>/dev/null || true)
  if [ -n "$JAVA_BIN" ]; then
    local REAL_JAVA
    REAL_JAVA=$(readlink -f "$JAVA_BIN" 2>/dev/null || true)
    [ -n "$REAL_JAVA" ] && export JAVA_HOME="${REAL_JAVA%/bin/java}"
  fi

  local ANDROID_DIR="/usr/local/android-sdk"
  local CMDLINE_DIR="$ANDROID_DIR/cmdline-tools/latest"

  mkdir -p "$ANDROID_DIR/cmdline-tools"

  if [ ! -x "$CMDLINE_DIR/bin/sdkmanager" ]; then
    echo "Mendeteksi versi Android Command-Line Tools terbaru..."
    local CMDLINE_ZIP="commandlinetools-linux-11076708_latest.zip"
    local DETECTED_ZIP
    DETECTED_ZIP=$(curl -sSL -m 5 "https://developer.android.com/studio" 2>/dev/null | grep -o 'commandlinetools-linux-[0-9_]*latest\.zip' | head -n1 || true)
    [ -n "$DETECTED_ZIP" ] && CMDLINE_ZIP="$DETECTED_ZIP"

 echo "Mengunduh Android Command-Line Tools ($CMDLINE_ZIP)..."
 local TMP_DIR
 TMP_DIR=$(mktemp -d)
 local TMP_ZIP="$TMP_DIR/cmdline-tools.zip"
 local TMP_EXTRACT="$TMP_DIR/extracted"
 TMP_CLEANUP_FILES+=("$TMP_DIR")

 wget -qO "$TMP_ZIP" "https://dl.google.com/android/repository/${CMDLINE_ZIP}"
 local CMDLINE_SHA=""
 CMDLINE_SHA=$(curl -fsSL -m 15 "https://dl.google.com/android/repository/repository2-3.xml" 2>/dev/null | tr -d '\r' | grep -B1 -F "<url>${CMDLINE_ZIP}</url>" | sed -n 's#.*<checksum type="sha1">\([0-9a-f]*\)</checksum>.*#\1#p' | head -n1 || true)
 if ! verify_sha1 "$TMP_ZIP" "$CMDLINE_SHA" "Android cmdline-tools"; then
  rm -rf "$TMP_DIR"
  return 1
 fi
 mkdir -p "$TMP_EXTRACT"
 if ! unzip -q -o "$TMP_ZIP" -d "$TMP_EXTRACT"; then
 echo "Error: gagal mengekstrak Android Command-Line Tools."
 return 1
 fi

 rm -rf "$CMDLINE_DIR"
 if [ -d "$TMP_EXTRACT/cmdline-tools" ]; then
 mv "$TMP_EXTRACT/cmdline-tools" "$CMDLINE_DIR"
 fi
 rm -rf "$TMP_DIR"
 else
 echo "Android Command-Line Tools sudah terpasang di $CMDLINE_DIR."
 fi

  export ANDROID_HOME="$ANDROID_DIR"
  export ANDROID_SDK_ROOT="$ANDROID_DIR"
  export PATH="$CMDLINE_DIR/bin:$ANDROID_DIR/platform-tools:$PATH"

  if [ -x "$CMDLINE_DIR/bin/sdkmanager" ]; then
    echo "Menerima lisensi Android SDK..."
    yes | "$CMDLINE_DIR/bin/sdkmanager" --licenses >/dev/null 2>&1 || true

    local api_pkgs=()
    case "${ANDROID_API_OPTION:-all}" in
      36)
        api_pkgs=("platforms;android-36" "build-tools;36.0.0")
        ;;
      35)
        api_pkgs=("platforms;android-35" "build-tools;35.0.0")
        ;;
      34)
        api_pkgs=("platforms;android-34" "build-tools;34.0.0")
        ;;
      *)
        api_pkgs=("platforms;android-36" "platforms;android-35" "platforms;android-34" "build-tools;36.0.0" "build-tools;35.0.0" "build-tools;34.0.0" "build-tools;28.0.3")
        ;;
    esac

 echo "Menginstal platform-tools, build-tools, dan platform Android (${ANDROID_API_OPTION:-all})..."
 if ! "$CMDLINE_DIR/bin/sdkmanager" "platform-tools" "${api_pkgs[@]}" >>"$LOG_FILE" 2>&1; then
 echo "Error: sdkmanager gagal memasang paket Android (lihat $LOG_FILE)."
 return 1
 fi
 if [ ! -x "$ANDROID_DIR/platform-tools/adb" ]; then
 echo "Error: platform-tools/adb tidak ditemukan setelah instalasi."
 return 1
 fi
 fi

  # Instal aturan udev USB untuk deteksi perangkat Android (adb devices)
  echo "Mengonfigurasi aturan udev Android untuk akses perangkat USB..."
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    dnf -y install android-tools 2>/dev/null || true
  else
    apt-get install -y android-sdk-platform-tools-common 2>/dev/null || true
  fi

  # Tambahkan user ke grup plugdev agar adb devices bisa mendeteksi perangkat fisik tanpa sudo
  groupadd -f plugdev
  local TARGET_USER="${SUDO_USER:-$USER}"
  if [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
    usermod -aG plugdev "$TARGET_USER" 2>/dev/null || true
    chown -R "$TARGET_USER:$TARGET_USER" "$ANDROID_DIR" 2>/dev/null || true
  fi
  udevadm control --reload-rules 2>/dev/null || true
  udevadm trigger 2>/dev/null || true

 # Beri akses baca/eksekusi; tulis hanya untuk owner (user target)
 chmod -R u+rwX,go+rX "$ANDROID_DIR" 2>/dev/null || true
}

# --- STEP 7: Flutter SDK (Stable) ---
step_flutter() {
  local FLUTTER_DIR="/usr/local/flutter"

  # Pastikan git & cmake tersedia
  if ! command -v git >/dev/null 2>&1 || ! command -v cmake >/dev/null 2>&1; then
    echo "Dependensi dasar Flutter belum lengkap. Mempersiapkan dependensi..."
    step_base_deps
  fi

  if [ ! -d "$FLUTTER_DIR/.git" ]; then
    echo "Mengkloning Flutter SDK (channel stable) ke $FLUTTER_DIR..."
    if ! git clone -b stable https://github.com/flutter/flutter.git "$FLUTTER_DIR"; then
      echo "Error: gagal mengkloning Flutter SDK."
      return 1
    fi
  else
    echo "Flutter SDK sudah terpasang di $FLUTTER_DIR."
  fi

 # Git safe directory untuk user non-root (idempoten)
 if ! git config --system --get-all safe.directory 2>/dev/null | grep -qx "$FLUTTER_DIR"; then
 git config --system --add safe.directory "$FLUTTER_DIR" 2>/dev/null || true
 fi

 # Buat file environment profile
 local JAVA_HOME_VAL=""
 if command -v java >/dev/null 2>&1; then
 local REAL_JAVA
 REAL_JAVA=$(readlink -f "$(command -v java)" 2>/dev/null || true)
 [ -n "$REAL_JAVA" ] && JAVA_HOME_VAL="${REAL_JAVA%/bin/java}"
 fi
 cat <<EOF > /etc/profile.d/flutter.sh
export FLUTTER_ROOT="/usr/local/flutter"
export ANDROID_HOME="/usr/local/android-sdk"
export ANDROID_SDK_ROOT="/usr/local/android-sdk"
${JAVA_HOME_VAL:+export JAVA_HOME="$JAVA_HOME_VAL"}
export PATH="\$FLUTTER_ROOT/bin:\$ANDROID_HOME/cmdline-tools/latest/bin:\$ANDROID_HOME/platform-tools:\$PATH"
EOF
 chmod +x /etc/profile.d/flutter.sh

  export PATH="$FLUTTER_DIR/bin:$PATH"

  # Precache Flutter artifacts
  echo "Mengunduh binary engine Flutter (precache)..."
  "$FLUTTER_DIR/bin/flutter" precache --linux --web --android 2>/dev/null || "$FLUTTER_DIR/bin/flutter" precache 2>/dev/null || true

  # Hubungkan Android SDK jika ada
  if [ -d "/usr/local/android-sdk" ]; then
    echo "Mengonfigurasi Android SDK path pada Flutter..."
    "$FLUTTER_DIR/bin/flutter" config --android-sdk /usr/local/android-sdk 2>/dev/null || true
    yes | "$FLUTTER_DIR/bin/flutter" doctor --android-licenses >/dev/null 2>&1 || true
  fi

 # Berikan kepemilikan dan hak akses ke user developer target
 local TARGET_USER="${SUDO_USER:-$USER}"
 if [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
 chown -R "$TARGET_USER:$TARGET_USER" "$FLUTTER_DIR" 2>/dev/null || true
 fi
 chmod -R u+rwX,go+rX "$FLUTTER_DIR" 2>/dev/null || true
}

# --- STEP 8: PostgreSQL Multi-Version (16, 17, 18) ---
PG_VER_OPTION=""

prompt_pg_options() {
  if [ -n "$PG_VER_OPTION" ]; then
    return 0
  fi

  detect_os
  local TTY_OUT="/dev/stdout"
  local TTY_IN="/dev/stdin"

  if [ -w /dev/tty ] && [ -r /dev/tty ]; then
    TTY_OUT="/dev/tty"
    TTY_IN="/dev/tty"
  elif [ ! -t 0 ]; then
    PG_VER_OPTION="18"
    return 0
  fi

  echo "" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "     PILIH VERSI DEFAULT POSTGRESQL       " > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "Komponen ini mendukung multi-versi (16, 17, 18)." > "$TTY_OUT"
  echo "Pilih versi yang akan dijadikan sebagai DEFAULT aktif:" > "$TTY_OUT"
  echo "" > "$TTY_OUT"
  echo "1. PostgreSQL 18 (Rilis Terbaru / Latest) [Rekomendasi Default]" > "$TTY_OUT"
  echo "2. PostgreSQL 17 (Sangat Populer & Stabil)" > "$TTY_OUT"
  echo "3. PostgreSQL 16 (LTS / Kompatibilitas Luas)" > "$TTY_OUT"
  echo "------------------------------------------" > "$TTY_OUT"
  echo "Catatan: Anda dapat berganti versi kapan saja via perintah 'pg-switch'." > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  printf "%s" "Pilih versi PostgreSQL default [1-3, default: 1]: " > "$TTY_OUT"
  local pg_choice=""
  read -r pg_choice < "$TTY_IN"
  case "$pg_choice" in
    2) PG_VER_OPTION="17" ;;
    3) PG_VER_OPTION="16" ;;
    *) PG_VER_OPTION="18" ;;
  esac
}

install_pg_switch_helper() {
  local target_dest="${1:-/usr/local/bin/pg-switch}"
 local tmp_dest="${target_dest}.tmp.$$"
  mkdir -p "$(dirname "$target_dest")" 2>/dev/null || true
 cat <<'EOF_PG_SWITCH' > "$tmp_dest"
#!/bin/bash
set -eo pipefail

# pg-switch: CLI helper untuk mengelola & beralih multi-versi PostgreSQL (16, 17, 18)
# Mendukung migrasi database dua arah (upgrade & downgrade) tanpa kehilangan data.
# Dibuat otomatis oleh setup_dev.sh

# Otomatis pasang/perbarui ke /usr/local/bin jika dieksekusi dengan akses root
if [ "$EUID" -eq 0 ] && [ "$0" != "/usr/local/bin/pg-switch" ]; then
  mkdir -p /usr/local/bin 2>/dev/null || true
  cp -f "$0" /usr/local/bin/pg-switch 2>/dev/null && chmod 755 /usr/local/bin/pg-switch 2>/dev/null || true
fi

C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"
C_DIM="\e[2m"

PG_SUPERUSER_PASSWORD="${PG_SUPERUSER_PASSWORD:-postgres}"
DEV_LAN_ACCESS="${DEV_LAN_ACCESS:-0}"
case "$PG_SUPERUSER_PASSWORD" in
 "") echo "Error: PG_SUPERUSER_PASSWORD tidak boleh kosong." >&2; exit 1 ;;
 *[!A-Za-z0-9_.-]*) echo "Error: PG_SUPERUSER_PASSWORD hanya boleh berisi [A-Za-z0-9_.-]." >&2; exit 1 ;;
esac

run_as_postgres() {
 if command -v runuser >/dev/null 2>&1; then
 runuser -u postgres -- "$@"
 else
 sudo -u postgres "$@"
 fi
}

# Impor dump secara ketat: berhenti pada error pertama dan kembalikan status sebenarnya
psql_import_dump() {
 local dump_file="$1"
 local db="${2:-postgres}"
 gunzip -c "$dump_file" | run_as_postgres psql -v ON_ERROR_STOP=1 -q -d "$db" -f -
}

pg_set_superuser_password() {
 run_as_postgres psql -q -c "ALTER USER postgres WITH PASSWORD '$PG_SUPERUSER_PASSWORD';" >/dev/null 2>&1 || true
}

ensure_dnf_isolated_data_dir() {
 local fallback_ver="${1:-18}"
 # Paket RPM postgresql*-server membuat direktori kosong /var/lib/pgsql/data saat instalasi.
 # Pindahkan hanya jika berisi cluster valid (PG_VERSION); jika kosong hapus agar symlink dapat dibuat.
 if [ -d "/var/lib/pgsql/data" ] && [ ! -L "/var/lib/pgsql/data" ]; then
 if [ -f "/var/lib/pgsql/data/PG_VERSION" ]; then
 local cv
 cv=$(tr -d '[:space:]' < "/var/lib/pgsql/data/PG_VERSION" 2>/dev/null || true)
 [[ "$cv" =~ ^[0-9]+$ ]] || cv="$fallback_ver"
 local dest="/var/lib/pgsql/data-${cv}"
 if [ ! -e "$dest" ]; then
 echo "Mengonversi cluster /var/lib/pgsql/data ke direktori terisolasi data-${cv}..."
 mv /var/lib/pgsql/data "$dest"
 ln -s "data-${cv}" /var/lib/pgsql/data
 chown -h postgres:postgres /var/lib/pgsql/data 2>/dev/null || true
 else
 mv /var/lib/pgsql/data "/var/lib/pgsql/data.bak.$$"
 fi
 elif [ -z "$(ls -A /var/lib/pgsql/data 2>/dev/null)" ]; then
 rmdir /var/lib/pgsql/data
 else
 mv /var/lib/pgsql/data "/var/lib/pgsql/data.uninitialized.$$"
 fi
 fi
}

is_cluster_ready() {
 local pm="$1" target_ver="$2"
 if [ "$pm" = "dnf" ]; then
 [ -L "/var/lib/pgsql/data" ] && \
 [ "$(readlink /var/lib/pgsql/data 2>/dev/null || true)" = "data-${target_ver}" ] && \
 [ -f "/var/lib/pgsql/data-${target_ver}/PG_VERSION" ]
 else
 [ -f "/etc/postgresql/${target_ver}/main/postgresql.conf" ] && \
 [ -f "/var/lib/postgresql/${target_ver}/main/PG_VERSION" ]
 fi
}

detect_pm() {
  if command -v dnf >/dev/null 2>&1; then
    echo "dnf"
  elif command -v apt-get >/dev/null 2>&1; then
    echo "apt"
  else
    echo "unknown"
  fi
}

get_base_dir() {
  local pm
  pm=$(detect_pm)
  if [ "$pm" = "apt" ]; then
    echo "/var/lib/postgresql"
  else
    echo "/var/lib/pgsql"
  fi
}

get_active_version() {
  if command -v psql >/dev/null 2>&1; then
    local v
    v=$(psql --version 2>/dev/null | awk '{print $3}' | cut -d. -f1 || true)
    if [[ "$v" =~ ^[0-9]+$ ]]; then
      echo "$v"
      return 0
    fi
  fi
  local base_dir
  base_dir=$(get_base_dir)
  if [ -f "$base_dir/data/PG_VERSION" ]; then
    cat "$base_dir/data/PG_VERSION" 2>/dev/null | tr -d ' ' || echo "18"
    return 0
  fi
  echo "unknown"
}

cmd_backup() {
  local base_dir
  base_dir=$(get_base_dir)
  local backup_dir="${base_dir}/backups"

  if [ "$EUID" -ne 0 ]; then
    if [ ! -w "$base_dir" ] 2>/dev/null || [ ! -w "$backup_dir" ] 2>/dev/null; then
      echo -e "${C_CYAN}Membuat snapshot cadangan sistem membutuhkan hak akses root...${C_RESET}"
      exec sudo bash "$0" backup "$@"
    fi
  fi

  mkdir -p "$backup_dir" 2>/dev/null || true
  chown postgres:postgres "$backup_dir" 2>/dev/null || true
  chmod 750 "$backup_dir" 2>/dev/null || true

  local active_v
  active_v=$(get_active_version)
  local ts
  ts=$(date +%Y%m%d_%H%M%S)
  local outfile="${backup_dir}/pg_dump_v${active_v}_${ts}.sql.gz"

  echo -e "${C_CYAN}Membuat snapshot cadangan database cluster PG ${active_v}...${C_RESET}"
  
  local dump_ok=0
  if [ "$EUID" -eq 0 ]; then
    if run_as_postgres pg_isready -q 2>/dev/null; then
      if run_as_postgres pg_dumpall --clean --if-exists 2>/dev/null | gzip > "$outfile"; then
        dump_ok=1
      fi
    elif PGPASSWORD="$PG_SUPERUSER_PASSWORD" pg_dumpall -U postgres -h 127.0.0.1 --clean --if-exists 2>/dev/null | gzip > "$outfile"; then
      dump_ok=1
    fi
  else
    if PGPASSWORD="${PGPASSWORD:-postgres}" pg_dumpall -U postgres -h 127.0.0.1 --clean --if-exists 2>/dev/null | gzip > "$outfile"; then
      dump_ok=1
    fi
  fi

  if [ "$dump_ok" -eq 1 ] && [ -s "$outfile" ]; then
    chown postgres:postgres "$outfile" 2>/dev/null || true
    chmod 640 "$outfile" 2>/dev/null || true
    echo -e "${C_GREEN}Snapshot cadangan berhasil disimpan di:${C_RESET} $outfile ($(du -sh "$outfile" 2>/dev/null | awk '{print $1}'))"
    echo "$outfile"
    return 0
  else
    rm -f "$outfile"
    echo -e "${C_YELLOW}Catatan: Database belum berjalan atau belum memiliki tabel tambahan. Tidak ada data yang di-dump.${C_RESET}"
    return 1
  fi
}

cmd_list() {
  local pm
  pm=$(detect_pm)
  local base_dir
  base_dir=$(get_base_dir)
  local active_v
  active_v=$(get_active_version)

  echo -e "${C_BOLD}============================================================${C_RESET}"
  echo -e "${C_BOLD}             PENGELOLA MULTI-VERSI POSTGRESQL               ${C_RESET}"
  echo -e "${C_BOLD}============================================================${C_RESET}"

  local svc_status="Inactive"
  if systemctl is-active --quiet postgresql 2>/dev/null || systemctl is-active --quiet "postgresql@*" 2>/dev/null; then
    svc_status="${C_GREEN}Running (Active)${C_RESET}"
  else
    svc_status="${C_RED}Stopped${C_RESET}"
  fi

  echo -e "  Versi Aktif       : ${C_CYAN}PostgreSQL ${active_v}${C_RESET} ($(command -v psql >/dev/null 2>&1 && psql --version || echo '-'))"
  echo -e "  Status Layanan    : $svc_status"
  echo -e "  Port Default      : 5432"
  echo -e "  Package Manager   : ${pm^^}"
  echo -e "------------------------------------------------------------"
  echo -e "${C_BOLD}STATUS CLUSTER DATA DI DISK:${C_RESET}"
  printf " %-10s | %-16s | %-14s | %-20s\n" "Versi" "Status Cluster" "Ukuran Data" "Direktori"
  echo "------------+------------------+----------------+---------------------"

  for ver in 16 17 18; do
    local c_dir=""
    if [ "$pm" = "apt" ]; then
      c_dir="/var/lib/postgresql/${ver}/main"
    else
      c_dir="/var/lib/pgsql/data-${ver}"
      [ ! -d "$c_dir" ] && [ "$ver" = "$active_v" ] && [ -d "/var/lib/pgsql/data" ] && c_dir="/var/lib/pgsql/data"
    fi

    local status="Belum Dibuat"
    local size="-"
    if [ -f "$c_dir/PG_VERSION" ]; then
      if [ "$ver" = "$active_v" ]; then
        status="${C_GREEN}Aktif (Running)${C_RESET}"
      else
        status="${C_CYAN}Siap (Tersimpan)${C_RESET}"
      fi
      size=$(du -sh "$c_dir" 2>/dev/null | awk '{print $1}' || echo "N/A")
    elif [ "$ver" = "$active_v" ] && (systemctl is-active --quiet postgresql 2>/dev/null || systemctl is-active --quiet "postgresql@*" 2>/dev/null); then
      status="${C_GREEN}Aktif (Running)${C_RESET}"
      size="(Butuh sudo)"
    fi
    printf " %-10s | %-27b | %-14s | %-20s\n" "PG $ver" "$status" "$size" "$c_dir"
  done

  echo -e "------------------------------------------------------------"
  echo -e "${C_BOLD}RIWAYAT SNAPSHOT CADANGAN (BACKUP):${C_RESET}"
  local backup_dir="${base_dir}/backups"
  local count=0
  if [ ! -r "$base_dir" ] && [ "$(id -u)" -ne 0 ]; then
    echo -e "  ${C_YELLOW}(Jalankan 'sudo pg-switch list' untuk melihat rincian backup di ${backup_dir})${C_RESET}"
  elif [ -d "$backup_dir" ]; then
    for b in "$backup_dir"/pg_dump_v*.sql.gz; do
      [ -f "$b" ] || continue
      local b_name
      b_name=$(basename "$b")
      local b_size
      b_size=$(du -sh "$b" 2>/dev/null | awk '{print $1}')
      local b_date
      b_date=$(stat -c "%y" "$b" 2>/dev/null | cut -d. -f1 || echo "-")
      printf "  • %-38s (%s, %s)\n" "$b_name" "$b_size" "$b_date"
      count=$((count + 1))
    done
  fi
  [ "$count" -eq 0 ] && echo "  (Belum ada file backup snapshot)"

  echo -e "------------------------------------------------------------"
  echo -e "Perintah Beralih Versi:"
  echo -e "  ${C_CYAN}pg-switch 18${C_RESET}              -> Ganti ke PostgreSQL 18"
  echo -e "  ${C_CYAN}pg-switch 17 --migrate${C_RESET}    -> Ganti ke PG 17 & migrasikan semua data"
  echo -e "  ${C_CYAN}pg-switch backup${C_RESET}          -> Buat snapshot cadangan sekarang"
}

cmd_restore() {
  if [ "$EUID" -ne 0 ]; then
    echo -e "${C_CYAN}Memulihkan database membutuhkan hak akses administrator (root)...${C_RESET}"
    exec sudo bash "$0" restore "$@"
  fi
  local target_file="${1:-}"
  if [ -z "$target_file" ] || [ ! -f "$target_file" ]; then
    echo -e "${C_RED}Error: Masukkan path file snapshot backup yang valid!${C_RESET}"
    echo "Contoh: pg-switch restore /var/lib/pgsql/backups/pg_dump_v18_20261109_151500.sql.gz"
    exit 1
  fi

  echo -e "${C_YELLOW}PERINGATAN: Memulihkan database dari snapshot akan mengimpor seluruh tabel & basis data.${C_RESET}"
  read -rp "Lanjutkan proses pemulihan? [y/N]: " conf_restore
  if [[ ! "$conf_restore" =~ ^[yY]$ ]]; then
    echo "Dibatalkan."
    exit 0
  fi

  echo -e "${C_CYAN}Memulihkan data dari $target_file...${C_RESET}"
  if ! psql_import_dump "$target_file" postgres; then
  echo -e "${C_RED}Error: pemulihan gagal. Database mungkin dalam keadaan tidak lengkap.${C_RESET}"
  echo "Periksa dump secara manual: gunzip -c '$target_file' | sudo -u postgres psql -v ON_ERROR_STOP=1 -d postgres"
  exit 1
  fi
  pg_set_superuser_password
  echo -e "${C_GREEN}Data berhasil dipulihkan!${C_RESET}"
}

pg_rollback_switch() {
 local pm="$1" target="$2" old_v="$3" old_link="$4"
 echo -e "${C_YELLOW}Rollback: mengembalikan layanan PostgreSQL...${C_RESET}"
 if [ "$pm" = "apt" ]; then
 systemctl stop "postgresql@${target}-main" 2>/dev/null || true
 local rb="$old_v"
 if ! [[ "$rb" =~ ^[0-9]+$ ]] || [ ! -f "/etc/postgresql/${rb}/main/postgresql.conf" ]; then
 rb=$(ls -1d /etc/postgresql/*/main 2>/dev/null | sed 's#/etc/postgresql/##; s#/main$##' | grep -vx "$target" | sort -n | tail -n1)
 fi
 if [ -n "$rb" ] && [ -f "/etc/postgresql/${rb}/main/postgresql.conf" ]; then
 sed -i "s/^port\s*=.*/port = 5432/" "/etc/postgresql/${rb}/main/postgresql.conf" 2>/dev/null || true
 sed -i "s/^port\s*=.*/port = 5433/" "/etc/postgresql/${target}/main/postgresql.conf" 2>/dev/null || true
 update-alternatives --set psql "/usr/lib/postgresql/${rb}/bin/psql" 2>/dev/null || true
 systemctl start "postgresql@${rb}-main" 2>/dev/null || systemctl restart postgresql 2>/dev/null || true
   if run_as_postgres pg_isready -q 2>/dev/null; then
     echo -e "${C_GREEN}Rollback berhasil: PostgreSQL ${rb} aktif kembali di port 5432.${C_RESET}"
   else
     echo -e "${C_RED}Rollback gagal. Jalankan manual: systemctl start postgresql@${rb}-main${C_RESET}"
   fi
   fi
   else
   systemctl stop postgresql 2>/dev/null || true
   if [ -n "$old_link" ] && [ -d "/var/lib/pgsql/${old_link}" ]; then
   ensure_dnf_isolated_data_dir "$old_v"
   [ -L "/var/lib/pgsql/data" ] && rm -f /var/lib/pgsql/data
   ln -sfn "$old_link" /var/lib/pgsql/data
   chown -h postgres:postgres /var/lib/pgsql/data
   systemctl start postgresql 2>/dev/null || true
   if run_as_postgres pg_isready -q 2>/dev/null; then
 echo -e "${C_GREEN}Rollback berhasil: cluster ${old_link} aktif kembali.${C_RESET}"
 else
 echo -e "${C_RED}Rollback butuh tindakan manual: pasang ulang paket PostgreSQL ${old_v}, lalu systemctl start postgresql.${C_RESET}"
 fi
 fi
 fi
}


cmd_switch() {
  if [ "$EUID" -ne 0 ]; then
    echo -e "${C_CYAN}Beralih versi PostgreSQL membutuhkan hak akses administrator (root)...${C_RESET}"
    exec sudo bash "$0" switch "$@"
  fi

  local target_ver="${1:-}"
  shift || true
  local do_migrate=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --migrate|-m) do_migrate=1 ;;
    esac
    shift || true
  done

  if [ "$target_ver" != "16" ] && [ "$target_ver" != "17" ] && [ "$target_ver" != "18" ]; then
    echo -e "${C_RED}Error: Versi target tidak valid: '$target_ver'${C_RESET}"
    echo "Pilihan yang didukung: 16, 17, 18"
    exit 1
  fi

  local pm
  pm=$(detect_pm)
  local base_dir
  base_dir=$(get_base_dir)
  local active_v
  active_v=$(get_active_version)
  old_link=""

  if [ "$target_ver" = "$active_v" ] && is_cluster_ready "$pm" "$target_ver" && \
     (systemctl is-active --quiet postgresql 2>/dev/null || systemctl is-active --quiet "postgresql@*" 2>/dev/null); then
    echo -e "${C_GREEN}PostgreSQL versi $target_ver sudah aktif saat ini.${C_RESET}"
    return 0
  fi

  echo -e "${C_BOLD}============================================================${C_RESET}"
  echo -e "${C_BOLD}       PROSES SWITCH POSTGRESQL $active_v -> $target_ver    ${C_RESET}"
  echo -e "${C_BOLD}============================================================${C_RESET}"

 # 1. Otomatis buat snapshot cadangan data aktif sebelum proses apapun
 local snapshot_file=""
 if systemctl is-active --quiet postgresql 2>/dev/null || systemctl is-active --quiet "postgresql@*" 2>/dev/null; then
 echo "Langkah 1/5: Mengamankan data aktif (Automatic Pre-switch Snapshot)..."
 snapshot_file=$(cmd_backup 2>/dev/null | tail -n1 || true)
 [ ! -f "$snapshot_file" ] && snapshot_file=""
 if [ -z "$snapshot_file" ]; then
 echo -e "${C_RED}Error: snapshot cadangan gagal dibuat; switch dibatalkan agar data aman.${C_RESET}"
 echo "Periksa koneksi PostgreSQL lalu jalankan ulang, atau gunakan 'pg-switch backup'."
 return 1
 fi
 echo "Snapshot: $snapshot_file"
 fi

  # 2. Hentikan service aktif
  echo "Langkah 2/5: Menghentikan layanan PostgreSQL..."
  systemctl stop postgresql 2>/dev/null || true
  systemctl stop "postgresql@*" 2>/dev/null || true

  # 3. Ganti paket biner RPM / APT
  echo "Langkah 3/5: Menyiapkan paket biner PostgreSQL $target_ver ($pm)..."
  if [ "$pm" = "dnf" ]; then
    ensure_dnf_isolated_data_dir "$active_v"

    old_link=$(readlink /var/lib/pgsql/data 2>/dev/null || true)

    # Swap RPM packages
    case "$target_ver" in
      16)
        dnf -y install --setopt=install_weak_deps=False --allowerasing postgresql16-server postgresql16
        ;;
      17)
        dnf -y install --setopt=install_weak_deps=False --allowerasing postgresql17-server postgresql17
        ;;
      18)
        dnf -y install --setopt=install_weak_deps=False --allowerasing postgresql-server postgresql
        ;;
    esac
  else
    # Debian / Ubuntu (APT)
    export DEBIAN_FRONTEND=noninteractive
    install -d /etc/apt/keyrings
    if [ ! -f /etc/apt/keyrings/postgresql.gpg ]; then
      curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /etc/apt/keyrings/postgresql.gpg --yes 2>/dev/null || true
    fi
    local PGDG_LIST="/etc/apt/sources.list.d/pgdg.list"
    local CODENAME
    CODENAME=$(lsb_release -cs 2>/dev/null || grep -oP 'VERSION_CODENAME=\K\w+' /etc/os-release 2>/dev/null || echo "bookworm")
    if [ ! -f "$PGDG_LIST" ] || ! grep -q "pgdg" "$PGDG_LIST"; then
      echo "deb [signed-by=/etc/apt/keyrings/postgresql.gpg] https://apt.postgresql.org/pub/repos/apt ${CODENAME}-pgdg main" > "$PGDG_LIST"
      apt-get update -y 2>/dev/null || true
    fi
    apt-get install -y --no-install-recommends "postgresql-${target_ver}" "postgresql-client-${target_ver}"
    update-alternatives --set psql "/usr/lib/postgresql/${target_ver}/bin/psql" 2>/dev/null || true
  fi

  # 4. Inisialisasi / Arahkan Cluster Data
  echo "Langkah 4/5: Menyiapkan cluster data untuk PostgreSQL $target_ver..."
  local is_new_cluster=0
  local conf_file=""

  if [ "$pm" = "dnf" ]; then
    local target_data="/var/lib/pgsql/data-${target_ver}"
    if [ ! -f "$target_data/PG_VERSION" ]; then
      is_new_cluster=1
      echo "Menginisialisasi cluster baru untuk PostgreSQL $target_ver..."
      if [ -d "$target_data" ] && [ -n "$(ls -A "$target_data" 2>/dev/null)" ]; then
        rm -rf "${target_data:?}/"* "${target_data:?}/".[!.]* 2>/dev/null || true
      fi
      mkdir -p "$target_data"
      chown -R postgres:postgres "$target_data"
      chmod 700 "$target_data"
      run_as_postgres initdb -D "$target_data"
    fi
    command -v chcon >/dev/null 2>&1 && chcon -R -t postgresql_db_t "$target_data" 2>/dev/null || true

    # Hapus direktori kosong /var/lib/pgsql/data yang dibuat RPM sebelum membuat symlink
    ensure_dnf_isolated_data_dir "$target_ver"
    [ -L "/var/lib/pgsql/data" ] && rm -f /var/lib/pgsql/data
    ln -sfn "data-${target_ver}" /var/lib/pgsql/data
    chown -h postgres:postgres /var/lib/pgsql/data
    command -v chcon >/dev/null 2>&1 && chcon -h -t postgresql_db_t /var/lib/pgsql/data 2>/dev/null || true
    conf_file="$target_data/postgresql.conf"
    local hba_file="$target_data/pg_hba.conf"

    # Konfigurasi listen_addresses dan pg_hba.conf
    if [ "$DEV_LAN_ACCESS" = "1" ]; then
      sed -i "s/^#*listen_addresses\s*=.*/listen_addresses = '*'/" "$conf_file" 2>/dev/null || true
    else
      sed -i "s/^#*listen_addresses\s*=.*/listen_addresses = 'localhost'/" "$conf_file" 2>/dev/null || true
    fi
    sed -i -E 's/(host\s+all\s+all\s+127\.0\.0\.1\/32\s+)(ident|trust)/\1scram-sha-256/' "$hba_file" 2>/dev/null || true
    sed -i -E 's/(host\s+all\s+all\s+::1\/128\s+)(ident|trust)/\1scram-sha-256/' "$hba_file" 2>/dev/null || true
    if [ "$DEV_LAN_ACCESS" = "1" ] && ! grep -q "Akses LAN & Netbird" "$hba_file" 2>/dev/null; then
      cat <<'EOF_HBA' >> "$hba_file"

# Akses LAN & Netbird
host    all             all             10.0.0.0/8              scram-sha-256
host    all             all             172.16.0.0/12           scram-sha-256
host    all             all             192.168.0.0/16          scram-sha-256
host    all             all             100.64.0.0/10           scram-sha-256
EOF_HBA
    fi
    chown postgres:postgres "$conf_file" "$hba_file" 2>/dev/null || true
    chmod 600 "$conf_file" "$hba_file" 2>/dev/null || true
  else
    # Debian
    local deb_conf="/etc/postgresql/${target_ver}/main/postgresql.conf"
    local deb_hba="/etc/postgresql/${target_ver}/main/pg_hba.conf"
    if [ ! -f "$deb_conf" ]; then
      is_new_cluster=1
      pg_createcluster "$target_ver" main 2>/dev/null || run_as_postgres initdb -D "/var/lib/postgresql/${target_ver}/main"
    fi
 local alt_port=5433
 for other_c in /etc/postgresql/*/main/postgresql.conf; do
 [ -f "$other_c" ] || continue
 [ "$other_c" = "$deb_conf" ] && continue
 sed -i "s/^port\s*=.*/port = ${alt_port}/" "$other_c" 2>/dev/null || true
 alt_port=$((alt_port + 1))
 done
    sed -i "s/^port\s*=.*/port = 5432/" "$deb_conf" 2>/dev/null || true
    if [ "$DEV_LAN_ACCESS" = "1" ]; then
     sed -i "s/^#*listen_addresses\s*=.*/listen_addresses = '*'/" "$deb_conf" 2>/dev/null || true
    else
     sed -i "s/^#*listen_addresses\s*=.*/listen_addresses = 'localhost'/" "$deb_conf" 2>/dev/null || true
    fi
    if [ -f "$deb_hba" ]; then
      sed -i -E 's/(host\s+all\s+all\s+127\.0\.0\.1\/32\s+)(ident|trust)/\1scram-sha-256/' "$deb_hba" 2>/dev/null || true
      sed -i -E 's/(host\s+all\s+all\s+::1\/128\s+)(ident|trust)/\1scram-sha-256/' "$deb_hba" 2>/dev/null || true
      if [ "$DEV_LAN_ACCESS" = "1" ] && ! grep -q "Akses LAN & Netbird" "$deb_hba" 2>/dev/null; then
        cat <<'EOF_HBA_DEB' >> "$deb_hba"

# Akses LAN & Netbird
host    all             all             10.0.0.0/8              scram-sha-256
host    all             all             172.16.0.0/12           scram-sha-256
host    all             all             192.168.0.0/16          scram-sha-256
host    all             all             100.64.0.0/10           scram-sha-256
EOF_HBA_DEB
      fi
    fi
    conf_file="$deb_conf"
  fi

  # 5. Jalankan Service & Verifikasi
  echo "Langkah 5/5: Menjalankan layanan PostgreSQL $target_ver..."
  if [ "$pm" = "apt" ]; then
    systemctl enable "postgresql@${target_ver}-main" 2>/dev/null || systemctl enable postgresql 2>/dev/null || true
    systemctl restart "postgresql@${target_ver}-main" 2>/dev/null || systemctl restart postgresql 2>/dev/null || true
  else
    systemctl enable postgresql 2>/dev/null || true
    systemctl restart postgresql
  fi

  local ready=0
  for ((i=1; i<=20; i++)); do
    if run_as_postgres pg_isready -q 2>/dev/null; then
      ready=1
      break
    fi
    sleep 1
  done

  if [ "$ready" -ne 1 ]; then
    echo -e "${C_RED}Peringatan: PostgreSQL $target_ver belum merespons dalam 20 detik.${C_RESET}"
    pg_rollback_switch "$pm" "$target_ver" "$active_v" "$old_link"
    return 1
  fi

  # Reset/Pastikan password user postgres
  pg_set_superuser_password

 # 6. Migrasi data otomatis (jika cluster baru atau requested --migrate)
 if [ "$is_new_cluster" -eq 1 ] || [ "$do_migrate" -eq 1 ]; then
 if [ -n "$snapshot_file" ] && [ -f "$snapshot_file" ]; then
 echo -e "${C_CYAN}Memigrasikan basis data & skema dari snapshot cadangan...${C_RESET}"
 if ! psql_import_dump "$snapshot_file" postgres; then
 echo -e "${C_RED}Error: migrasi data gagal. Snapshot tetap tersimpan di: $snapshot_file${C_RESET}"
 echo "Pulihkan manual: sudo pg-switch restore $snapshot_file"
 pg_rollback_switch "$pm" "$target_ver" "$active_v" "$old_link"
 return 1
 fi
 pg_set_superuser_password
 echo -e "${C_GREEN}Migrasi data ke PostgreSQL $target_ver berhasil dilakukan!${C_RESET}"
 fi
 fi

  echo ""
  echo -e "${C_GREEN}${C_BOLD}BERHASIL! PostgreSQL telah beralih ke versi $target_ver.${C_RESET}"
  echo -e "Versi aktif saat ini: ${C_CYAN}$(command -v psql >/dev/null 2>&1 && psql --version || echo "PostgreSQL $target_ver")${C_RESET}"
}

cmd_internal_setup() {
  local target_ver="${1:-18}"
  local pm
  pm=$(detect_pm)
  local base_dir
  base_dir=$(get_base_dir)

  mkdir -p "${base_dir}/backups"
  chown postgres:postgres "${base_dir}/backups" 2>/dev/null || true
  chmod 750 "${base_dir}/backups" 2>/dev/null || true

  if [ "$pm" = "dnf" ]; then
    ensure_dnf_isolated_data_dir "$target_ver"
  fi

  local active_v
  active_v=$(get_active_version)

  if [ "$active_v" != "$target_ver" ] || ! is_cluster_ready "$pm" "$target_ver"; then
    cmd_switch "$target_ver" --migrate
  else
    if [ "$pm" = "apt" ]; then
      systemctl enable "postgresql@${target_ver}-main" 2>/dev/null || systemctl enable postgresql 2>/dev/null || true
      systemctl restart "postgresql@${target_ver}-main" 2>/dev/null || systemctl restart postgresql
    else
      systemctl enable postgresql 2>/dev/null || true
      systemctl restart postgresql
    fi
    if ! run_as_postgres pg_isready -q 2>/dev/null; then
      echo -e "${C_RED}Error: layanan PostgreSQL $target_ver gagal merespons.${C_RESET}"
      return 1
    fi
    pg_set_superuser_password
    echo "PostgreSQL $target_ver sudah aktif dan terkonfigurasi."
  fi
}

case "${1:-}" in
  list|status|"")
    cmd_list
    ;;
  backup)
    cmd_backup
    ;;
  backups)
    cmd_list
    ;;
  restore)
    shift
    cmd_restore "${1:-}"
    ;;
  switch)
    shift
    cmd_switch "$@"
    ;;
  16|17|18)
    cmd_switch "$@"
    ;;
  migrate)
    shift
    cmd_switch "${1:-}" --migrate
    ;;
  internal-setup)
    shift
    cmd_internal_setup "$@"
    ;;
  install)
    if [ "$EUID" -ne 0 ]; then
      echo "Membutuhkan akses root untuk memasang ke /usr/local/bin/pg-switch..."
      exec sudo bash "$0" install
    fi
    mkdir -p /usr/local/bin
    cp -f "$0" /usr/local/bin/pg-switch
    chmod 755 /usr/local/bin/pg-switch
    echo -e "${C_GREEN}Berhasil! pg-switch telah terpasang di /usr/local/bin/pg-switch.${C_RESET}"
    echo "Sekarang Anda dapat menjalankan 'pg-switch' atau 'sudo pg-switch' langsung dari terminal."
    exit 0
    ;;
  help|-h|--help)
    echo "Penggunaan: pg-switch [perintah]"
    echo ""
    echo "Perintah:"
    echo "  pg-switch [list|status]          Tampilkan versi aktif & status cluster"
    echo "  sudo pg-switch <16|17|18>        Beralih ke versi PostgreSQL tertentu"
    echo "  sudo pg-switch switch <ver> -m   Beralih ke versi target & migrasikan semua data"
    echo "  sudo pg-switch backup            Buat snapshot cadangan database cluster"
    echo "  sudo pg-switch restore <file>    Pulihkan database dari file snapshot backup"
    echo "  pg-switch install                Pasang pg-switch ke /usr/local/bin secara global"
    ;;
  *)
    echo "Perintah tidak dikenal: $1"
    echo "Gunakan 'pg-switch help' untuk bantuan."
    exit 1
    ;;
esac
EOF_PG_SWITCH
 chmod 755 "$tmp_dest"
 mv -f "$tmp_dest" "$target_dest"

  if [ "$target_dest" = "/usr/local/bin/pg-switch" ]; then
    local target_user="${SUDO_USER:-$USER}"
    if [ -n "$target_user" ] && [ "$target_user" != "root" ]; then
      local u_home
      u_home=$(getent passwd "$target_user" 2>/dev/null | cut -d: -f6)
      if [ -n "$u_home" ] && [ -d "$u_home/.local/bin" ]; then
        cp -f "$target_dest" "$u_home/.local/bin/pg-switch" 2>/dev/null || true
        chown "$target_user:$target_user" "$u_home/.local/bin/pg-switch" 2>/dev/null || true
        chmod 755 "$u_home/.local/bin/pg-switch" 2>/dev/null || true
      fi
    fi
  fi
}

ensure_pg_switch_helper() {
 local u_user="${SUDO_USER:-$USER}"
 local u_home
 u_home=$(getent passwd "$u_user" 2>/dev/null | cut -d: -f6)
 [ -z "$u_home" ] && u_home="$HOME"
 local u_dir="$u_home/.local/bin"
 mkdir -p "$u_dir" 2>/dev/null || true
 install_pg_switch_helper "$u_dir/pg-switch"

 if [ "$EUID" -eq 0 ]; then
 install_pg_switch_helper "/usr/local/bin/pg-switch"
 fi
}

step_postgres() {
  check_root
  validate_credentials
  warn_lan_exposure
  detect_os
  prompt_pg_options

  local target_ver="${PG_VER_OPTION:-18}"
  echo "Mengonfigurasi PostgreSQL $target_ver..."

  # Pastikan helper pg-switch terpasang
  install_pg_switch_helper

  # Jalankan setup internal via pg-switch
  /usr/local/bin/pg-switch internal-setup "$target_ver"
}

# --- STEP 9: Redis Server ---
step_redis() {
  validate_credentials
  warn_lan_exposure
  detect_os
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    echo "Menginstal Redis / Valkey server (RPM/DNF)..."
    if dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{name}\n" redis 2>/dev/null | grep -qx "redis"; then
      dnf -y install redis || return 1
    elif dnf $DNF_RO_OPTS -y repoquery -q --latest-limit=1 --qf "%{name}\n" valkey-compat-redis 2>/dev/null | grep -qx "valkey-compat-redis"; then
      dnf -y install valkey valkey-compat-redis || return 1
    else
      dnf -y install valkey || return 1
    fi
  else
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y || return 1
    apt-get install -y redis-server redis-tools || return 1
  fi

  local REDIS_CONF
  REDIS_CONF=$(get_redis_conf)

  if [ -f "$REDIS_CONF" ]; then
    local tmp_conf
    tmp_conf=$(mktemp)
    TMP_CLEANUP_FILES+=("$tmp_conf")
    cp "$REDIS_CONF" "$tmp_conf"

    # Bind ke LAN hanya jika DEV_LAN_ACCESS=1 (default); selain itu localhost
    local target_bind="127.0.0.1 -::1"
    [ "$DEV_LAN_ACCESS" = "1" ] && target_bind="0.0.0.0"
    if grep -q "^#* *bind " "$tmp_conf"; then
      sed -i "s/^#* *bind .*/bind ${target_bind}/" "$tmp_conf"
    else
      echo "bind ${target_bind}" >> "$tmp_conf"
    fi

    # Konfigurasi requirepass (dapat dioverride via REDIS_PASSWORD)
    if grep -q "^#* *requirepass " "$tmp_conf"; then
      sed -i "s|^#* *requirepass .*|requirepass ${REDIS_PASSWORD}|" "$tmp_conf"
    else
      echo "requirepass ${REDIS_PASSWORD}" >> "$tmp_conf"
    fi

    # Pastikan protected-mode tetap yes (dengan requirepass, koneksi luar tetap aman dan diizinkan)
    sed -i "s/^#* *protected-mode no/protected-mode yes/" "$tmp_conf"

    # Tulis balik ke inode asli agar owner/group (mis. root:valkey 0640) tetap utuh
    cat "$tmp_conf" > "$REDIS_CONF"
    rm -f "$tmp_conf"
  fi

  local REDIS_SVC
  REDIS_SVC=$(get_redis_service)
  systemctl enable "$REDIS_SVC" 2>/dev/null || true
  if ! systemctl restart "$REDIS_SVC" 2>/dev/null; then
    systemctl restart valkey 2>/dev/null || systemctl restart redis 2>/dev/null || systemctl restart redis-server 2>/dev/null || true
  fi

  # Tunggu redis siap menerima koneksi (mencegah race condition)
  local ready=0
  local CLI_BIN=""
  if command -v redis-cli >/dev/null 2>&1; then
    CLI_BIN="redis-cli"
  elif command -v valkey-cli >/dev/null 2>&1; then
    CLI_BIN="valkey-cli"
  fi

  if [ -n "$CLI_BIN" ]; then
    for ((i=1; i<=15; i++)); do
      if REDISCLI_AUTH="$REDIS_PASSWORD" "$CLI_BIN" ping 2>/dev/null | grep -q "PONG"; then
        ready=1
        break
      fi
      sleep 1
    done
  fi

  if [ "$ready" -ne 1 ]; then
    echo "Error: Redis / Valkey server belum merespons PONG dalam batas waktu 15 detik!"
    systemctl status "$REDIS_SVC" --no-pager 2>/dev/null || true
    return 1
  fi
}

# --- ALUR INSTALASI PHP & WEB STACK ---
setup_repo_nginx() {
  detect_os
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    # Fedora native updates repository sudah menyediakan Nginx Mainline terbaru
    return 0
  fi

  if [ "$PKG_MGR" = "apt" ]; then
    echo "Menyiapkan repository Nginx untuk APT..."
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y curl gnupg2 ca-certificates lsb-release debian-archive-keyring ubuntu-keyring 2>/dev/null || true

    local CODENAME
    CODENAME=$(lsb_release -cs 2>/dev/null || grep -oP 'VERSION_CODENAME=\K\w+' /etc/os-release 2>/dev/null || echo "bookworm")
    local DISTRO_DIR="$OS_ID"
    [ "$DISTRO_DIR" != "ubuntu" ] && DISTRO_DIR="debian"

    if [ ! -f /usr/share/keyrings/nginx-archive-keyring.gpg ]; then
      curl -fsSL https://nginx.org/keys/nginx_signing.key | gpg --dearmor --yes -o /usr/share/keyrings/nginx-archive-keyring.gpg 2>/dev/null || true
    fi

    if [ -f /usr/share/keyrings/nginx-archive-keyring.gpg ]; then
      echo "deb [signed-by=/usr/share/keyrings/nginx-archive-keyring.gpg] https://nginx.org/packages/mainline/${DISTRO_DIR} ${CODENAME} nginx" > /etc/apt/sources.list.d/nginx.list
      apt-get update -y 2>/dev/null || true
    fi
  fi
}

setup_repo_php() {
  detect_os
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    if ! rpm -q remi-release >/dev/null 2>&1; then
      echo "Mengimpor GPG key dan memasang repository Remi PHP untuk Fedora..."
      rpm --import https://rpms.remirepo.net/RPM-GPG-KEY-remi https://rpms.remirepo.net/RPM-GPG-KEY-remi2026 2>/dev/null || true
      local fedora_ver="${OS_VERSION_ID:-}"
      [ -z "$fedora_ver" ] && command -v rpm >/dev/null 2>&1 && fedora_ver=$(rpm -E %fedora 2>/dev/null || true)
      [ -z "$fedora_ver" ] || [ "$fedora_ver" = "%fedora" ] && fedora_ver="45"
      dnf -y install "https://rpms.remirepo.net/fedora/remi-release-${fedora_ver}.rpm" 2>/dev/null || true
    fi
  elif [ "$PKG_MGR" = "apt" ]; then
    export DEBIAN_FRONTEND=noninteractive
    if [ "$OS_ID" = "ubuntu" ]; then
      if ! grep -q "ondrej/php" /etc/apt/sources.list /etc/apt/sources.list.d/* 2>/dev/null; then
        echo "Menambahkan PPA Ondrej PHP..."
        apt-get install -y software-properties-common
        LC_ALL=C.UTF-8 add-apt-repository -y ppa:ondrej/php
        apt-get update -y
      fi
    else
      # Debian
      if [ ! -f /etc/apt/sources.list.d/php.list ]; then
        echo "Menambahkan repository Sury PHP untuk Debian..."
        apt-get install -y apt-transport-https lsb-release ca-certificates curl
        curl -fsSL https://packages.sury.org/php/apt.gpg | gpg --dearmor --yes -o /etc/apt/trusted.gpg.d/php.gpg 2>/dev/null || true
        local SURY_CODENAME
        SURY_CODENAME=$(lsb_release -sc 2>/dev/null || grep -oP 'VERSION_CODENAME=\K\w+' /etc/os-release 2>/dev/null || echo "bookworm")
        echo "deb https://packages.sury.org/php/ ${SURY_CODENAME} main" > /etc/apt/sources.list.d/php.list
        apt-get update -y
      fi
    fi
  fi
}

get_php_fpm_socket() {
  local ver="${1:-}"
  local candidates=()

  if [ -n "$ver" ] && [ "$ver" != "system" ] && [ "$ver" != "default" ]; then
    candidates+=(
      "/run/php/php${ver}-fpm.sock"
      "/run/php-fpm/php${ver}-fpm.sock"
      "/var/run/php/php${ver}-fpm.sock"
    )
  fi

  candidates+=(
    "/run/php-fpm/www.sock"
    "/run/php/php-fpm.sock"
    "/var/run/php-fpm/www.sock"
  )

  local active_v
  active_v=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;' 2>/dev/null || true)
  if [ -n "$active_v" ]; then
    candidates=("/run/php/php${active_v}-fpm.sock" "/run/php-fpm/php${active_v}-fpm.sock" "${candidates[@]}")
  fi

  for s in "${candidates[@]}"; do
    if [ -S "$s" ]; then
      echo "$s"
      return 0
    fi
  done

  if [ -d "/run/php-fpm" ] || [ -f "/etc/fedora-release" ] || [ -f "/etc/redhat-release" ]; then
    echo "/run/php-fpm/www.sock"
  else
    local fallback_v="${ver:-$active_v}"
    [ -z "$fallback_v" ] || [ "$fallback_v" = "system" ] && fallback_v="8.4"
    echo "/run/php/php${fallback_v}-fpm.sock"
  fi
}

install_php_switch_helper() {
  local target_dest="${1:-/usr/local/bin/php-switch}"
  local tmp_switch="${target_dest}.tmp.$$"
 cat <<'EOF_SWITCH' > "$tmp_switch"
#!/bin/bash
set -euo pipefail

# php-switch: CLI helper untuk mengganti versi aktif PHP CLI & PHP-FPM
# Dibuat otomatis oleh setup_dev.sh

# Otomatis pasang/perbarui ke /usr/local/bin jika dieksekusi dengan akses root
if [ "$EUID" -eq 0 ] && [ "$0" != "/usr/local/bin/php-switch" ]; then
  mkdir -p /usr/local/bin 2>/dev/null || true
  cp -f "$0" /usr/local/bin/php-switch 2>/dev/null && chmod 755 /usr/local/bin/php-switch 2>/dev/null || true
fi

C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"

detect_pm() {
  if command -v dnf >/dev/null 2>&1; then
    echo "dnf"
  elif command -v apt-get >/dev/null 2>&1; then
    echo "apt"
  else
    echo "unknown"
  fi
}

cmd_list() {
  echo -e "${C_BOLD}============================================================${C_RESET}"
  echo -e "${C_BOLD}                 STATUS VERSI PHP                           ${C_RESET}"
  echo -e "${C_BOLD}============================================================${C_RESET}"
  if command -v php >/dev/null 2>&1; then
    echo -e "  PHP CLI Aktif     : ${C_GREEN}$(php -v 2>/dev/null | head -n1)${C_RESET}"
  else
    echo -e "  PHP CLI Aktif     : ${C_YELLOW}Belum terpasang${C_RESET}"
  fi

  local PM
  PM=$(detect_pm)
  if [ "$PM" = "dnf" ]; then
    echo -e "  Manajer Modul DNF :"
    local dnf_skip=""
    ( dnf --version 2>&1 | grep -q "dnf5" ) && dnf_skip="--skip-file-locks"
    dnf $dnf_skip -C module list php 2>/dev/null | grep -E 'remi-[0-9]' || dnf $dnf_skip module list php 2>/dev/null | grep -E 'remi-[0-9]' || true
  elif [ "$PM" = "apt" ]; then
    echo -e "  Versi Alternatif  :"
    update-alternatives --list php 2>/dev/null || ls -d /usr/bin/php[0-9].* 2>/dev/null || true
  fi
  echo -e "------------------------------------------------------------"
  echo -e "Ganti versi dengan: ${C_CYAN}sudo php-switch <versi>${C_RESET} (contoh: sudo php-switch 8.3)"
}

cmd_switch() {
  if [ "$EUID" -ne 0 ]; then
    echo -e "${C_RED}Error: Jalankan perintah ini dengan hak akses root (sudo php-switch <versi>)${C_RESET}"
    exit 1
  fi
  local ver="${1:-}"
  if [ -z "$ver" ]; then
    echo -e "${C_RED}Error: Masukkan versi target (contoh: 8.4, 8.3, 8.2, 8.1)!${C_RESET}"
    exit 1
  fi

  local PM
  PM=$(detect_pm)
  echo -e "${C_CYAN}Mengalihkan versi PHP ke ${ver}...${C_RESET}"

  if [ "$PM" = "dnf" ]; then
    dnf -y module reset php 2>/dev/null || true
    dnf -y module enable "php:remi-${ver}"
    dnf -y distro-sync "php*" 2>/dev/null || dnf -y upgrade "php*" 2>/dev/null || true
    systemctl restart php-fpm 2>/dev/null || true
  elif [ "$PM" = "apt" ]; then
    if [ -x "/usr/bin/php${ver}" ]; then
      update-alternatives --set php "/usr/bin/php${ver}" 2>/dev/null || true
      [ -x "/usr/bin/phar${ver}" ] && update-alternatives --set phar "/usr/bin/phar${ver}" 2>/dev/null || true
      [ -x "/usr/bin/phpize${ver}" ] && update-alternatives --set phpize "/usr/bin/phpize${ver}" 2>/dev/null || true
      [ -x "/usr/bin/php-config${ver}" ] && update-alternatives --set php-config "/usr/bin/php-config${ver}" 2>/dev/null || true
      # Jangan hentikan FPM versi lain; socket tiap versi berbeda
      systemctl restart "php${ver}-fpm" 2>/dev/null || systemctl start "php${ver}-fpm" 2>/dev/null || true
    else
      echo -e "${C_RED}Error: Versi PHP ${ver} belum terpasang di /usr/bin/php${ver}!${C_RESET}"
      exit 1
    fi
  fi

  echo -e "${C_GREEN}[✓] Berhasil beralih ke PHP $(php -v 2>/dev/null | head -n1)${C_RESET}"
}

case "${1:-}" in
  list|ls)
    cmd_list
    ;;
  help|--help|-h)
    echo "Penggunaan: sudo php-switch <versi|list>"
    echo "Contoh:"
    echo "  sudo php-switch 8.3"
    echo "  sudo php-switch list"
    ;;
  *)
    if [ -n "${1:-}" ]; then
      cmd_switch "$1"
    else
      cmd_list
    fi
    ;;
esac
EOF_SWITCH
 chmod 755 "$tmp_switch"
 mv -f "$tmp_switch" "$target_dest"
}


# --- DETEKSI BROWSER OTOMATIS & SINKRONISASI KEBIJAKAN LOCAL SSL ---
sync_browser_ssl_policies() {
  local CA_DIR="${1:-/etc/ssl/local-certs/ca}"
  local ROOT_CA="$CA_DIR/rootCA.pem"
  if [ ! -f "$ROOT_CA" ]; then
    return 0
  fi

  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6 || echo "/home/$TARGET_USER")
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  local CA_ESC
  CA_ESC=$(awk '{printf "%s\\n", $0}' "$ROOT_CA")

  local detected_browsers=()

  # 1. CHROMIUM & CHROMIUM-BROWSER (Utama)
  if command -v chromium >/dev/null 2>&1 || command -v chromium-browser >/dev/null 2>&1 || [ -d "/etc/chromium" ] || [ -d "/etc/chromium-browser" ]; then
    for c_dir in "/etc/chromium/policies/managed" "/etc/chromium-browser/policies/managed"; do
      mkdir -p "$c_dir" 2>/dev/null || true
      cat <<EOF_C_POLICY > "$c_dir/local_ca.json" 2>/dev/null || true
{
  "CACertificates": [
    "${CA_ESC}"
  ]
}
EOF_C_POLICY
      chmod 644 "$c_dir/local_ca.json" 2>/dev/null || true
    done
    detected_browsers+=("Chromium")
  fi

  # 2. GOOGLE CHROME
  if command -v google-chrome >/dev/null 2>&1 || command -v google-chrome-stable >/dev/null 2>&1 || [ -d "/opt/google/chrome" ] || [ -d "/etc/opt/chrome" ]; then
    mkdir -p /etc/opt/chrome/policies/managed 2>/dev/null || true
    cat <<EOF_CHROME_POLICY > /etc/opt/chrome/policies/managed/local_ca.json 2>/dev/null || true
{
  "CACertificates": [
    "${CA_ESC}"
  ]
}
EOF_CHROME_POLICY
    chmod 644 /etc/opt/chrome/policies/managed/local_ca.json 2>/dev/null || true
    detected_browsers+=("Google Chrome")
  fi

  # 3. BRAVE BROWSER
  if command -v brave-browser >/dev/null 2>&1 || [ -d "/etc/brave" ]; then
    mkdir -p /etc/brave/policies/managed 2>/dev/null || true
    cat <<EOF_BRAVE_POLICY > /etc/brave/policies/managed/local_ca.json 2>/dev/null || true
{
  "CACertificates": [
    "${CA_ESC}"
  ]
}
EOF_BRAVE_POLICY
    chmod 644 /etc/brave/policies/managed/local_ca.json 2>/dev/null || true
    detected_browsers+=("Brave")
  fi

  # 4. MICROSOFT EDGE
  if command -v microsoft-edge >/dev/null 2>&1 || command -v microsoft-edge-stable >/dev/null 2>&1 || [ -d "/etc/opt/edge" ]; then
    mkdir -p /etc/opt/edge/policies/managed 2>/dev/null || true
    cat <<EOF_EDGE_POLICY > /etc/opt/edge/policies/managed/local_ca.json 2>/dev/null || true
{
  "CACertificates": [
    "${CA_ESC}"
  ]
}
EOF_EDGE_POLICY
    chmod 644 /etc/opt/edge/policies/managed/local_ca.json 2>/dev/null || true
    detected_browsers+=("Microsoft Edge")
  fi

  # 5. MOZILLA FIREFOX / FIREFOX ESR / LIBREWOLF
  local has_ff=0
  if command -v firefox >/dev/null 2>&1 || command -v firefox-esr >/dev/null 2>&1 || command -v librewolf >/dev/null 2>&1 || [ -d "/etc/firefox" ] || [ -d "/etc/firefox-esr" ]; then
    has_ff=1
  fi
  for ff_dir in "/etc/firefox/policies" "/etc/firefox-esr/policies" "/usr/lib/firefox-esr/distribution" "/usr/lib/firefox/distribution" "/usr/lib64/firefox/distribution"; do
    if [ -d "$(dirname "$ff_dir")" ] || [ -d "$ff_dir" ]; then
      mkdir -p "$ff_dir" 2>/dev/null || true
      cat <<EOF_FF_POLICY > "$ff_dir/policies.json" 2>/dev/null || true
{
  "policies": {
    "Certificates": {
      "ImportEnterpriseRoots": true,
      "Install": [
        "${ROOT_CA}"
      ]
    }
  }
}
EOF_FF_POLICY
      chmod 644 "$ff_dir/policies.json" 2>/dev/null || true
    fi
  done

  local TMP_PW
  TMP_PW=$(mktemp 2>/dev/null || echo "/tmp/.nss_empty_pw_$$")
  echo "" > "$TMP_PW" 2>/dev/null || true
  chmod 600 "$TMP_PW" 2>/dev/null || true

  # Inject CA ke profil Firefox aktif via certutil jika tersedia
  if command -v certutil >/dev/null 2>&1; then
    for prof in "$TARGET_HOME"/.mozilla/firefox/* "$TARGET_HOME"/.mozilla/firefox-esr/* /root/.mozilla/firefox/* /root/.mozilla/firefox-esr/*; do
      if [ -d "$prof" ] && ([ -f "$prof/cert9.db" ] || [ -f "$prof/cert8.db" ]); then
        certutil -d "sql:$prof" -A -t "C,," -n "mkcert root CA" -i "$ROOT_CA" -f "$TMP_PW" 2>/dev/null ||         certutil -d "dbm:$prof" -A -t "C,," -n "mkcert root CA" -i "$ROOT_CA" -f "$TMP_PW" 2>/dev/null || true
      fi
    done
  fi
  [ "$has_ff" -eq 1 ] && detected_browsers+=("Mozilla Firefox")

  # 6. NSS SHARED DB (~/.pki/nssdb untuk aplikasi Chromium/Electron)
  for nss_db in "$TARGET_HOME/.pki/nssdb" "/root/.pki/nssdb"; do
    mkdir -p "$nss_db" 2>/dev/null || true
    if command -v certutil >/dev/null 2>&1; then
      if [ ! -f "$nss_db/cert9.db" ] && [ ! -f "$nss_db/cert8.db" ]; then
        certutil -d "sql:$nss_db" -N --empty-password 2>/dev/null || true
      fi
      certutil -d "sql:$nss_db" -A -t "C,," -n "mkcert root CA" -i "$ROOT_CA" -f "$TMP_PW" 2>/dev/null || true
    fi
  done
  rm -f "$TMP_PW" 2>/dev/null || true
  [ -n "$TARGET_USER" ] && chown -R "$TARGET_USER:$TARGET_USER" "$TARGET_HOME/.pki" 2>/dev/null || true

  # 7. SYSTEM TRUST STORE (Agar curl, wget, Node.js, PHP di CLI langsung percaya)
  if [ -d "/usr/local/share/ca-certificates" ] || command -v update-ca-certificates >/dev/null 2>&1; then
    mkdir -p /usr/local/share/ca-certificates 2>/dev/null || true
    cp -f "$ROOT_CA" /usr/local/share/ca-certificates/mkcert-rootCA.crt 2>/dev/null || true
    update-ca-certificates >/dev/null 2>&1 || true
    detected_browsers+=("System Trust Store")
  elif [ -d "/etc/pki/ca-trust/source/anchors" ] || command -v update-ca-trust >/dev/null 2>&1; then
    mkdir -p /etc/pki/ca-trust/source/anchors 2>/dev/null || true
    cp -f "$ROOT_CA" /etc/pki/ca-trust/source/anchors/mkcert-rootCA.crt 2>/dev/null || true
    update-ca-trust extract >/dev/null 2>&1 || true
    detected_browsers+=("System Trust Store")
  fi

  if [ ${#detected_browsers[@]} -gt 0 ]; then
    echo -e "${C_GREEN:-}[✓] Local SSL CA terhubung ke: ${detected_browsers[*]}${C_RESET:-}"
  fi
}

# --- SINKRONISASI OTOMATIS VIRTUAL HOST & /ETC/HOSTS ---
sync_vhosts_and_hosts() {
  local count_added=0
  local count_certs=0
  local CA_DIR="/etc/ssl/local-certs/ca"
  mkdir -p /etc/ssl/local-certs "$CA_DIR" 2>/dev/null || true
  [ -d "$CA_DIR" ] && export CAROOT="$CA_DIR"

  # Pastikan localhost ada di /etc/hosts
  if ! grep -q -E "^[[:space:]]*127\.0\.0\.1[[:space:]]+.*localhost" /etc/hosts 2>/dev/null; then
    echo "127.0.0.1 localhost" >> /etc/hosts 2>/dev/null || true
  fi

  # Pastikan localhost.pem ada
  if [ ! -f /etc/ssl/local-certs/localhost.pem ] && command -v mkcert >/dev/null 2>&1; then
    mkcert -cert-file /etc/ssl/local-certs/localhost.pem -key-file /etc/ssl/local-certs/localhost-key.pem localhost 127.0.0.1 ::1 >/dev/null 2>&1 || true
    chmod 644 /etc/ssl/local-certs/localhost.pem 2>/dev/null || true
    chmod 600 /etc/ssl/local-certs/localhost-key.pem 2>/dev/null || true
  fi

  if [ -d "/etc/nginx/sites-available" ]; then
    for conf in /etc/nginx/sites-available/*.conf; do
      [ -f "$conf" ] || continue
      [[ "$conf" == *.custom.conf ]] && continue
      local domain
      domain=$(basename "$conf" .conf)
      [ -z "$domain" ] && continue

 # 1. Update /etc/hosts jika domain belum ada
 if ! hosts_has_domain "$domain"; then
 hosts_add_domain "$domain"
 echo -e "${C_CYAN:-}[+] Menambahkan ${domain} ke /etc/hosts (127.0.0.1)${C_RESET:-}"
 count_added=$((count_added + 1))
 fi

 # 2. Pastikan SSL cert ada dan valid (termasuk host publik Cloudflare)
 local cert_file="/etc/ssl/local-certs/${domain}.pem"
 local key_file="/etc/ssl/local-certs/${domain}-key.pem"
 if [ ! -f "$cert_file" ] || [ ! -f "$key_file" ] || ! wdev_cert_covers_publics "$domain" "$cert_file"; then
 if command -v mkcert >/dev/null 2>&1; then
 echo -e "${C_CYAN:-}[*] Menghasilkan sertifikat SSL mkcert untuk ${domain}...${C_RESET:-}"
 wdev_regen_cert "$domain" || true
 count_certs=$((count_certs + 1))
 fi
 fi

 # 3. Pastikan server_name memuat host publik hasil expose
 wdev_apply_server_names "$domain" 2>/dev/null || true
 done
 fi

  if [ "$count_added" -gt 0 ] || [ "$count_certs" -gt 0 ]; then
    echo -e "${C_GREEN:-}[✓] Sinkronisasi hosts & SSL selesai: +${count_added} entri hosts, +${count_certs} sertifikat SSL.${C_RESET:-}"
  fi
}

install_cf_tunnel_helper() {
 local target_dest="${1:-/usr/local/bin/cf-tunnel}"
 local src="${WDEV_SCRIPT_DIR}/cf-tunnel.sh"
 local tmp_dest="${target_dest}.tmp.$$"
 if [ ! -f "$src" ]; then
 echo "Error: cf-tunnel.sh tidak ditemukan di ${WDEV_SCRIPT_DIR}." >&2
 return 1
 fi
 mkdir -p "$(dirname "$target_dest")" 2>/dev/null || true
 if [ ! -w "$(dirname "$target_dest")" ] && [ -e "$target_dest" ] && [ ! -w "$target_dest" ]; then
 return 0
 fi
 cp -f "$src" "$tmp_dest" 2>/dev/null || return 1
 chmod 755 "$tmp_dest"
 mv -f "$tmp_dest" "$target_dest"
}

ensure_cf_tunnel_helper() {
 local u_user="${SUDO_USER:-$USER}"
 local u_home u_dir
 u_home=$(getent passwd "$u_user" 2>/dev/null | cut -d: -f6)
 [ -z "$u_home" ] && u_home="$HOME"
 u_dir="$u_home/.local/bin"
 mkdir -p "$u_dir" 2>/dev/null || true
 install_cf_tunnel_helper "$u_dir/cf-tunnel" || true
 if [ "$EUID" -eq 0 ]; then
 install_cf_tunnel_helper "/usr/local/bin/cf-tunnel" || true
 fi
}

install_pdns_mgr_helper() {
  local target_dest="${1:-/usr/local/bin/pdns-mgr}"
  local src="${WDEV_SCRIPT_DIR}/pdns-mgr.sh"
  local tmp_dest="${target_dest}.tmp.$$"
  if [ ! -f "$src" ]; then
    echo "Error: pdns-mgr.sh tidak ditemukan di ${WDEV_SCRIPT_DIR}." >&2
    return 1
  fi
  mkdir -p "$(dirname "$target_dest")" 2>/dev/null || true
  if [ ! -w "$(dirname "$target_dest")" ] && [ -e "$target_dest" ] && [ ! -w "$target_dest" ]; then
    return 0
  fi
  cp -f "$src" "$tmp_dest" 2>/dev/null || return 1
  chmod 755 "$tmp_dest"
  mv -f "$tmp_dest" "$target_dest"
}

ensure_pdns_mgr_helper() {
  local u_user="${SUDO_USER:-$USER}"
  local u_home u_dir
  u_home=$(getent passwd "$u_user" 2>/dev/null | cut -d: -f6)
  [ -z "$u_home" ] && u_home="$HOME"
  u_dir="$u_home/.local/bin"
  mkdir -p "$u_dir" 2>/dev/null || true
  install_pdns_mgr_helper "$u_dir/pdns-mgr" || true
  if [ "$EUID" -eq 0 ]; then
    install_pdns_mgr_helper "/usr/local/bin/pdns-mgr" || true
  fi
}

find_pdns_mgr_bin() {
  for b in /usr/local/bin/pdns-mgr "$HOME/.local/bin/pdns-mgr"; do
    if [ -x "$b" ]; then
      echo "$b"
      return 0
    fi
  done
  command -v pdns-mgr 2>/dev/null || echo "pdns-mgr"
}

install_dev_site_helper() {
  local target_dest="${1:-/usr/local/bin/dev-site}"
 local tmp_dest="${target_dest}.tmp.$$"
  mkdir -p "$(dirname "$target_dest")" 2>/dev/null || true
  if [ ! -w "$(dirname "$target_dest")" ] && [ -e "$target_dest" ] && [ ! -w "$target_dest" ]; then
    return 0
  fi
 cat <<'EOF_DEVSITE' > "$tmp_dest" 2>/dev/null || return 0
#!/bin/bash
set -euo pipefail

# dev-site: CLI helper untuk manajemen virtual host Nginx lokal dengan SSL otomatis (mkcert)
# Dibuat otomatis oleh setup_dev.sh

# Otomatis pasang/perbarui ke /usr/local/bin jika dieksekusi dengan akses root
if [ "$EUID" -eq 0 ] && [ "$0" != "/usr/local/bin/dev-site" ]; then
  mkdir -p /usr/local/bin 2>/dev/null || true
  cp -f "$0" /usr/local/bin/dev-site 2>/dev/null && chmod 755 /usr/local/bin/dev-site 2>/dev/null || true
fi

C_RESET="\e[0m"
C_BOLD="\e[1m"
C_GREEN="\e[32m"
C_YELLOW="\e[33m"
C_RED="\e[31m"
C_CYAN="\e[36m"

WDEV_ETC="${WDEV_ETC:-/etc/wdev}"

validate_domain() {
 local d="${1:-}"
 if [ -z "$d" ] || [ ${#d} -gt 253 ] || [[ "$d" == *..* ]] || [[ ! "$d" =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ ]]; then
 echo -e "${C_RED}Error: domain tidak valid: '$d'. Gunakan huruf, angka, titik, dan strip.${C_RESET}" >&2
 return 1
 fi
}

# @@SHARED_FUNCTIONS@@



grant_web_write() {
 local dir="$1" group=""
 for group in www-data apache nginx; do
 if getent group "$group" >/dev/null 2>&1; then
 chgrp -R "$group" "$dir" 2>/dev/null || true
 chmod -R u+rwX,g+rwX,o+rX "$dir" 2>/dev/null || true
 find "$dir" -type d -exec chmod g+s {} + 2>/dev/null || true
 return 0
 fi
 done
 chmod -R u+rwX,g+rX,o+rX "$dir" 2>/dev/null || true
}

check_root() {
 if [ "$EUID" -ne 0 ]; then
 echo -e "${C_RED}Error: Jalankan perintah ini dengan hak akses root (sudo dev-site ...)${C_RESET}"
 exit 1
 fi
}

find_cf_tunnel_bin() {
 local b
 for b in /usr/local/bin/cf-tunnel "$HOME/.local/bin/cf-tunnel"; do
 if [ -x "$b" ]; then
 echo "$b"
 return 0
 fi
 done
 command -v cf-tunnel 2>/dev/null || return 1
}

validate_public_host() {
 local h="${1:-}"
 if [ -z "$h" ] || [ ${#h} -gt 253 ] || [[ "$h" == *..* ]] || [[ ! "$h" =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ ]]; then
 echo -e "${C_RED}Error: host publik tidak valid: '$h'.${C_RESET}" >&2
 return 1
 fi
}



cmd_create() {
  check_root
  local domain="${1:-}"
  local input_path="${2:-}"
  local php_ver="${3:-}"
  local explicit_type="${4:-auto}"

  if [ -z "$domain" ] || [ -z "$input_path" ]; then
    echo -e "${C_RED}Error: Parameter tidak lengkap!${C_RESET}"
    echo "Penggunaan: sudo dev-site create <domain> <docroot_or_port> [php_ver_or_port] [type]"
    echo "Tipe didukung: auto (default), laravel, wordpress, ci4, ci3, symfony, generic, proxy"
    echo "Contoh PHP  : sudo dev-site create myapp.test /var/www/myapp 8.4"
    echo "Contoh Proxy: sudo dev-site create mygo.test 3221 proxy"
    exit 1
  fi
  validate_domain "$domain" || exit 1

  local proxy_target=""
  # Deteksi mode reverse proxy (Go, Node.js, Python, Rust, Docker, dll.)
  if [ "$explicit_type" = "proxy" ] || [[ "$explicit_type" == proxy:* ]] || [[ "$input_path" =~ ^[0-9]+$ ]] || [[ "$input_path" =~ ^https?:// ]]; then
    explicit_type="proxy"
    if [[ "$4" == proxy:* ]]; then
      proxy_target="http://127.0.0.1:${4#proxy:}"
    elif [[ "$php_ver" =~ ^[0-9]+$ ]]; then
      proxy_target="http://127.0.0.1:$php_ver"
    elif [[ "$input_path" =~ ^[0-9]+$ ]]; then
      proxy_target="http://127.0.0.1:$input_path"
    elif [[ "$input_path" =~ ^https?:// ]]; then
      proxy_target="$input_path"
    else
      proxy_target="http://127.0.0.1:3000"
    fi
  fi

  case "$explicit_type" in
    auto|laravel|wordpress|ci4|ci3|symfony|generic|proxy) ;;
    *)
      echo -e "${C_RED}Error: tipe '$explicit_type' tidak didukung.${C_RESET}"
      echo "Tipe didukung: auto (default), laravel, wordpress, ci4, ci3, symfony, generic, proxy"
      exit 1
      ;;
  esac

  if [ "$explicit_type" = "proxy" ]; then
    if [ ! -d "$input_path" ]; then
      input_path="/var/www/${domain}"
      mkdir -p "$input_path" 2>/dev/null || true
    fi
  else
    # Normalisasi input_path
    if [ ! -d "$input_path" ]; then
      echo -e "${C_YELLOW}Direktori belum ada. Membuat direktori: $input_path${C_RESET}"
      mkdir -p "$input_path"
      local TARGET_USER="${SUDO_USER:-$USER}"
      if [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
        chown -R "$TARGET_USER:$TARGET_USER" "$input_path" 2>/dev/null || true
      fi
    fi
  fi
  input_path=$(cd "$input_path" && pwd)

  local project_root="$input_path"
  local docroot="$input_path"
  local site_type="$explicit_type"
  local parent_dir
  parent_dir=$(dirname "$input_path")

  if [ "$site_type" = "proxy" ]; then
    : # Tetap mode proxy
  elif [ "$site_type" = "auto" ] || [ -z "$site_type" ]; then
    if [ -f "$project_root/artisan" ] || [ -f "$parent_dir/artisan" ]; then
      site_type="laravel"
      if [ -d "$project_root/public" ]; then
        docroot="$project_root/public"
      elif [ -f "$parent_dir/artisan" ] && [ "$(basename "$project_root")" = "public" ]; then
        project_root="$parent_dir"
        docroot="$input_path"
      fi
    elif [ -f "$project_root/spark" ] || [ -f "$parent_dir/spark" ]; then
      site_type="ci4"
      if [ -d "$project_root/public" ]; then
        docroot="$project_root/public"
      elif [ -f "$parent_dir/spark" ] && [ "$(basename "$project_root")" = "public" ]; then
        project_root="$parent_dir"
        docroot="$input_path"
      fi
    elif [ -f "$project_root/wp-config.php" ] || [ -f "$project_root/wp-settings.php" ]; then
      site_type="wordpress"
    elif [ -d "$project_root/application" ] && [ -d "$project_root/system" ]; then
      site_type="ci3"
    elif [ -f "$project_root/bin/console" ] && [ -d "$project_root/public" ]; then
      site_type="symfony"
      docroot="$project_root/public"
    elif [ -d "$project_root/public" ] && [ -f "$project_root/public/index.php" ] && [ ! -f "$project_root/index.php" ]; then
      site_type="generic_public"
      docroot="$project_root/public"
    else
      site_type="generic"
    fi
  else
    case "$site_type" in
      laravel|ci4|symfony)
        if [ -d "$project_root/public" ]; then
          docroot="$project_root/public"
        fi
        ;;
    esac
  fi

  echo -e "${C_CYAN}Tipe Aplikasi     : ${C_BOLD}${site_type^^}${C_RESET}"
  if [ "$site_type" = "proxy" ]; then
    echo -e "${C_CYAN}Proxy Target      : ${C_BOLD}${proxy_target}${C_RESET}"
  else
    echo -e "${C_CYAN}Project Root      : ${project_root}${C_RESET}"
    echo -e "${C_CYAN}Document Root     : ${docroot}${C_RESET}"
  fi

 mkdir -p "$docroot"
 chmod u+rwX,go+rX "$docroot" 2>/dev/null || true
 chmod u+rwX,go+x "$project_root" 2>/dev/null || true

 # Pastikan setiap direktori induk (termasuk /home/user) dapat dilintasi oleh Nginx & PHP-FPM
 local cur_p="$project_root"
 while [ "$cur_p" != "/" ] && [ "$cur_p" != "." ] && [ -n "$cur_p" ]; do
 local perms
 perms=$(stat -c %A "$cur_p" 2>/dev/null || true)
 if [ "${perms:9:1}" != "x" ]; then
 chmod o+x "$cur_p" 2>/dev/null || true
 fi
 cur_p="$(dirname "$cur_p")"
 done

 # Izin khusus Laravel / CI4
 if [ "$site_type" = "laravel" ]; then
 if [ -d "$project_root/storage" ]; then
 echo -e "${C_CYAN}Menyesuaikan izin folder Laravel storage & bootstrap/cache...${C_RESET}"
 grant_web_write "$project_root/storage"
 [ -d "$project_root/bootstrap/cache" ] && grant_web_write "$project_root/bootstrap/cache"
 fi
 elif [ "$site_type" = "ci4" ]; then
 if [ -d "$project_root/writable" ]; then
 grant_web_write "$project_root/writable"
 fi
 fi

  # SELinux support
  if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null)" != "Disabled" ]; then
    chcon -R -t httpd_sys_content_t "$project_root" 2>/dev/null || true
    if [ "$site_type" = "laravel" ] && [ -d "$project_root/storage" ]; then
      chcon -R -t httpd_sys_rw_content_t "$project_root/storage" 2>/dev/null || true
      [ -d "$project_root/bootstrap/cache" ] && chcon -R -t httpd_sys_rw_content_t "$project_root/bootstrap/cache" 2>/dev/null || true
    elif [ "$site_type" = "ci4" ] && [ -d "$project_root/writable" ]; then
      chcon -R -t httpd_sys_rw_content_t "$project_root/writable" 2>/dev/null || true
    fi
  fi

  # Starter index.php jika docroot kosong (hanya untuk PHP/HTML, bukan proxy)
  if [ "$site_type" != "proxy" ] && [ -z "$(ls -A "$docroot" 2>/dev/null)" ]; then
    cat <<EOF_INDEX > "$docroot/index.php"
<?php
echo "<h1>Selamat Datang di {$domain}!</h1>";
echo "<p>Environment: PHP " . PHP_VERSION . " | Tipe: " . htmlspecialchars('${site_type^^}') . " | HTTPS / SSL Local Aktif</p>";
phpinfo();
EOF_INDEX
    local TARGET_USER="${SUDO_USER:-$USER}"
    if [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
      chown "$TARGET_USER:$TARGET_USER" "$docroot/index.php" 2>/dev/null || true
    fi
  fi

  # Direktori SSL
  mkdir -p /etc/ssl/local-certs
  chmod 755 /etc/ssl/local-certs

  if ! command -v mkcert >/dev/null 2>&1; then
    echo -e "${C_RED}Error: mkcert belum terpasang. Jalankan 'sudo setup_dev.sh --ssl' terlebih dahulu.${C_RESET}"
    exit 1
  fi

  echo -e "${C_CYAN}Menghasilkan sertifikat SSL lokal untuk ${domain}...${C_RESET}"
  [ -d /etc/ssl/local-certs/ca ] && export CAROOT="/etc/ssl/local-certs/ca"
  local cert_file="/etc/ssl/local-certs/${domain}.pem"
  local key_file="/etc/ssl/local-certs/${domain}-key.pem"
  mkcert -cert-file "$cert_file" -key-file "$key_file" "$domain" "*.$domain" "localhost" 127.0.0.1 ::1 >/dev/null 2>&1
  chmod 644 "$cert_file"
  chmod 600 "$key_file"
  command -v chcon >/dev/null 2>&1 && chcon -t cert_t "$cert_file" "$key_file" 2>/dev/null || true

  local fpm_sock="none"
  if [ "$site_type" != "proxy" ]; then
    fpm_sock=$(get_socket "$php_ver")
    echo -e "${C_CYAN}Menggunakan PHP-FPM Socket: ${fpm_sock}${C_RESET}"
  else
    echo -e "${C_CYAN}Menggunakan Reverse Proxy: ${C_BOLD}${proxy_target}${C_RESET}"
  fi

  mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled

  local custom_conf="/etc/nginx/sites-available/${domain}.custom.conf"
  if [ ! -f "$custom_conf" ]; then
    cat <<EOF_CUSTOM > "$custom_conf"
# ==============================================================================
# ATURAN KUSTOM NGINX UNTUK: ${domain} (Pengganti .htaccess)
# ==============================================================================
# Anda dapat menambahkan rewrite rules, header kustom, atau lokasi khusus di sini.
# Konfigurasi ini otomatis di-include ke dalam server block ${domain}.
#
# Contoh Rewrite Rule Apache ke Nginx:
# Apache: RewriteRule ^product/([0-9]+)$ product.php?id=\$1 [L]
# Nginx : rewrite ^/product/([0-9]+)$ /product.php?id=\$1 last;
#
EOF_CUSTOM

    if [ -f "$project_root/.nginx.conf" ]; then
      echo "include \"$project_root/.nginx.conf\";" >> "$custom_conf"
    elif [ -f "$project_root/nginx.conf" ]; then
      echo "include \"$project_root/nginx.conf\";" >> "$custom_conf"
    else
      echo "# include \"$project_root/.nginx.conf\";" >> "$custom_conf"
    fi
  fi
  chmod 644 "$custom_conf"

  local conf_avail="/etc/nginx/sites-available/${domain}.conf"
  local conf_enabled="/etc/nginx/sites-enabled/${domain}.conf"

  cat <<EOF_SERVER > "$conf_avail"
# Managed by dev-site helper
# Site Type: ${site_type}
# Project Root: ${project_root}

server {
    listen 80;
    listen [::]:80;
    server_name ${domain} *.${domain};
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name ${domain} *.${domain};

    root ${docroot};
    index index.php index.html index.htm;
    charset utf-8;

    ssl_certificate ${cert_file};
    ssl_certificate_key ${key_file};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    # Batas upload file (128 MB)
    client_max_body_size 128M;

    # Logging
    access_log /var/log/nginx/${domain}.access.log;
    error_log /var/log/nginx/${domain}.error.log;

    # Matikan log untuk file statis umum
    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }
EOF_SERVER

  case "$site_type" in
    laravel)
      cat <<EOF_LARAVEL >> "$conf_avail"

    # --- LARAVEL ROUTING (Pengganti .htaccess public) ---
    location / {
        try_files \$uri \$uri/ /index.php?\$query_string;
    }

    # Proteksi berkas sensitif (.env, .git, .htaccess, composer.json)
    location ~ /\.(?!well-known).* {
        deny all;
    }

    # Proteksi direktori logs
    location ~ ^/storage/logs/ {
        deny all;
    }

    # Eksekusi PHP-FPM dengan FastCGI Path Info
    location ~ \.php$ {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:${fpm_sock};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$realpath_root\$fastcgi_script_name;
        fastcgi_param PATH_INFO \$fastcgi_path_info;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;
    }
EOF_LARAVEL
      ;;

    wordpress)
      cat <<EOF_WP >> "$conf_avail"

    # --- WORDPRESS ROUTING (Pengganti .htaccess) ---
    location / {
        try_files \$uri \$uri/ /index.php?\$args;
    }

    # Proteksi eksekusi PHP di folder uploads
    location ~* ^/wp-content/uploads/.*\.php$ {
        deny all;
    }

    # Proteksi berkas sensitif (.env, .git, .htaccess, wp-config.php)
    location ~ /\.(?!well-known).* {
        deny all;
    }
    location = /wp-config.php {
        deny all;
    }

    # Cache untuk aset statis WordPress
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|webp|woff|woff2|ttf|eot)$ {
        expires 30d;
        add_header Cache-Control "public, no-transform";
        access_log off;
        log_not_found off;
    }

    # Eksekusi PHP-FPM
    location ~ \.php$ {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:${fpm_sock};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        fastcgi_param PATH_INFO \$fastcgi_path_info;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;
    }
EOF_WP
      ;;

    ci4|ci3)
      cat <<EOF_CI >> "$conf_avail"

    # --- CODEIGNITER ROUTING (Pengganti .htaccess) ---
    location / {
        try_files \$uri \$uri/ /index.php?\$query_string;
    }

    # Proteksi direktori internal CodeIgniter (application, system, writable)
    location ~ ^/(application|system|writable)/ {
        deny all;
    }

    # Proteksi berkas sensitif (.env, .git, .htaccess)
    location ~ /\.(?!well-known).* {
        deny all;
    }

    # Eksekusi PHP-FPM dengan dukungan penuh PATH_INFO (/index.php/controller/method)
    location ~ \.php(/|$) {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:${fpm_sock};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$realpath_root\$fastcgi_script_name;
        fastcgi_param PATH_INFO \$fastcgi_path_info;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;
    }
EOF_CI
      ;;

    symfony)
      cat <<EOF_SYMFONY >> "$conf_avail"

    # --- SYMFONY ROUTING (Pengganti .htaccess) ---
    location / {
        try_files \$uri /index.php\$is_args\$args;
    }

    # Proteksi berkas sensitif (.env, .git, .htaccess)
    location ~ /\.(?!well-known).* {
        deny all;
    }

    location ~ \.php$ {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:${fpm_sock};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$realpath_root\$fastcgi_script_name;
        fastcgi_param PATH_INFO \$fastcgi_path_info;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;
    }
EOF_SYMFONY
      ;;

    proxy)
      cat <<EOF_PROXY >> "$conf_avail"

    # --- REVERSE PROXY (Go, Node.js, Python, Rust, Docker, dll.) ---
    location / {
        proxy_pass ${proxy_target};
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
EOF_PROXY
      ;;

    *)
      cat <<EOF_GENERIC >> "$conf_avail"

    # --- UNIVERSAL PHP ROUTING (Pengganti .htaccess RewriteEngine On) ---
    location / {
        try_files \$uri \$uri/ /index.php?\$query_string;
    }

    # Proteksi berkas sensitif (.env, .git, .htaccess, .htpasswd, dll)
    location ~ /\.(?!well-known).* {
        deny all;
    }

    # Eksekusi PHP-FPM dengan dukungan PATH_INFO
    location ~ \.php(/|$) {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:${fpm_sock};
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$realpath_root\$fastcgi_script_name;
        fastcgi_param PATH_INFO \$fastcgi_path_info;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;
    }
EOF_GENERIC
      ;;
  esac

  cat <<EOF_INCLUDE >> "$conf_avail"

    # Sertakan aturan kustom Nginx (Pengganti .htaccess per proyek)
    include ${custom_conf};
}
EOF_INCLUDE

  ln -sf "$conf_avail" "$conf_enabled"

 # Terapkan eksposur Cloudflare yang sudah ada (jika vhost dibuat ulang)
 wdev_apply_server_names "$domain" 2>/dev/null || true
 if [ -f "/etc/ssl/local-certs/${domain}.pem" ] && ! wdev_cert_covers_publics "$domain" "/etc/ssl/local-certs/${domain}.pem"; then
 wdev_regen_cert "$domain" 2>/dev/null || true
 fi

 if ! hosts_has_domain "$domain"; then
 hosts_add_domain "$domain"
    echo -e "${C_CYAN}Menambahkan ${domain} ke /etc/hosts (127.0.0.1)${C_RESET}"
 fi

  if nginx -t >/dev/null 2>&1; then
    systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null || true
    echo ""
    echo -e "${C_BOLD}${C_GREEN}============================================================${C_RESET}"
    echo -e "${C_BOLD}${C_GREEN} [✓] VIRTUAL HOST BERHASIL DIBUAT & DIAKTIFKAN!             ${C_RESET}"
    echo -e "${C_BOLD}${C_GREEN}============================================================${C_RESET}"
    echo -e "  Domain        : ${C_BOLD}https://${domain}${C_RESET}"
    echo -e "  Tipe          : ${site_type^^}"
    echo -e "  Project Root  : ${project_root}"
    echo -e "  Document Root : ${docroot}"
    echo -e "  SSL Cert      : ${cert_file}"
    echo -e "  PHP Socket    : ${fpm_sock}"
    echo -e "  Nginx Config  : ${conf_avail}"
    echo -e "  Custom Rules  : ${custom_conf}"
    echo -e "------------------------------------------------------------"
    echo -e "Buka di browser: ${C_CYAN}https://${domain}${C_RESET}"
    sync_browser_ssl_policies 2>/dev/null || true
  else
    echo -e "${C_RED}Error: Konfigurasi Nginx tidak valid! Membatalkan...${C_RESET}"
    rm -f "$conf_enabled"
    nginx -t
    exit 1
  fi
}

cmd_delete() {
  check_root
  local domain="${1:-}"
  if [ -z "$domain" ]; then
    echo -e "${C_RED}Error: Masukkan domain yang ingin dihapus!${C_RESET}"
    echo "Penggunaan: sudo dev-site delete <domain>"
    exit 1
  fi
 validate_domain "$domain" || exit 1

  local conf_avail="/etc/nginx/sites-available/${domain}.conf"
  local conf_enabled="/etc/nginx/sites-enabled/${domain}.conf"
  local custom_conf="/etc/nginx/sites-available/${domain}.custom.conf"

 local d_root="-"
 if [ -f "$conf_avail" ]; then
 d_root=$(grep -E '^[[:space:]]*root[[:space:]]+' "$conf_avail" | head -n1 | awk '{print $2}' | tr -d ';' || echo "-")
 fi

 # Hapus eksposur Cloudflare lebih dulu agar tidak ada ingress/CNAME menggantung
 if [ -f "$WDEV_ETC/exposures/${domain}.conf" ]; then
 local cf_bin h p
 local -a profs=()
 cf_bin=$(find_cf_tunnel_bin) || true
 while IFS=$'\t' read -r h p; do
 [ -n "$h" ] || continue
 profs+=("$p")
 if [ -n "$cf_bin" ]; then
 "$cf_bin" dns rm --profile "$p" "$h" >/dev/null 2>&1 || true
 fi
 done < <(read_exposures "$domain")
 rm -f "$WDEV_ETC/exposures/${domain}.conf"
 if [ -n "$cf_bin" ]; then
 for p in $(printf '%s\n' ${profs[@]+"${profs[@]}"} | sort -u); do
 [ -n "$p" ] && "$cf_bin" sync --profile "$p" >/dev/null 2>&1 || true
 done
 fi
 echo -e "${C_CYAN}[+] Eksposur Cloudflare untuk ${domain} dihapus.${C_RESET}"
 fi

  rm -f "$conf_enabled" "$conf_avail" "$custom_conf"
  rm -f "/etc/ssl/local-certs/${domain}.pem" "/etc/ssl/local-certs/${domain}-key.pem"

  hosts_remove_domain "$domain"

  nginx -t >/dev/null 2>&1 && (systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null || true)
  echo -e "${C_GREEN}[✓] Virtual host '${domain}', sertifikat SSL, dan entri hosts berhasil dihapus.${C_RESET}"
  if [ "$d_root" != "-" ] && [ -d "$d_root" ]; then
    echo -e "${C_CYAN}    Catatan: Berkas project Anda di '${d_root}' tetap aman (tidak dihapus).${C_RESET}"
  fi
}

cmd_list() {
  echo -e "${C_BOLD}========================================================================================================${C_RESET}"
  echo -e "${C_BOLD}                                DAFTAR VIRTUAL HOST (DEV-SITE)                                          ${C_RESET}"
  echo -e "${C_BOLD}========================================================================================================${C_RESET}"
  printf "${C_BOLD}%-22s | %-9s | %-10s | %-32s | %-16s | %s${C_RESET}\n" "Domain" "Status" "Tipe" "Document Root" "PHP Socket" "Public Host"
  echo "-----------------------+-----------+------------+----------------------------------+------------------+------------------------------"

  local count=0
  if [ -d "/etc/nginx/sites-available" ]; then
    for conf in /etc/nginx/sites-available/*.conf; do
      [ -f "$conf" ] || continue
      [[ "$conf" == *.custom.conf ]] && continue
      local dname
      dname=$(basename "$conf" .conf)
      local is_enabled="Disabled"
      [ -L "/etc/nginx/sites-enabled/${dname}.conf" ] && is_enabled="Enabled"
      local stype="GENERIC"
      stype=$(grep -E '^[[:space:]]*#[[:space:]]*Site Type:' "$conf" | head -n1 | awk '{print $NF}' || echo "GENERIC")
      local d_root
      if grep -q "proxy_pass" "$conf"; then
        d_root=$(grep -E '^[[:space:]]*proxy_pass[[:space:]]+' "$conf" | head -n1 | awk '{print $2}' | tr -d ';' || echo "-")
      else
        d_root=$(grep -E '^[[:space:]]*root[[:space:]]+' "$conf" | head -n1 | awk '{print $2}' | tr -d ';' || echo "-")
      fi
      local f_sock
      f_sock=$(grep -E 'fastcgi_pass unix:' "$conf" | head -n1 | sed -E 's/.*fastcgi_pass unix:([^;]+);.*/\1/' || echo "-")
      local short_sock
      short_sock=$(basename "$f_sock" 2>/dev/null || echo "$f_sock")
      [ "$stype" = "proxy" ] && short_sock="-"
      local public_hosts
      public_hosts=$(wdev_public_hosts "$dname" | paste -sd, - 2>/dev/null || true)
      [ -n "$public_hosts" ] || public_hosts="-"
      printf "%-22s | %-9s | %-10s | %-32s | %-16s | %s\n" "$dname" "$is_enabled" "${stype^^}" "$d_root" "$short_sock" "$public_hosts"
      count=$((count + 1))
    done
  fi

  if [ "$count" -eq 0 ]; then
    echo "  Belum ada virtual host yang dibuat."
  fi
  echo -e "${C_BOLD}========================================================================================================${C_RESET}"
}

cmd_cert() {
  check_root
  local domain="${1:-}"
  if [ -z "$domain" ]; then
    echo -e "${C_RED}Error: Masukkan nama domain!${C_RESET}"
    exit 1
  fi
 validate_domain "$domain" || exit 1
 mkdir -p /etc/ssl/local-certs
 [ -d /etc/ssl/local-certs/ca ] && export CAROOT="/etc/ssl/local-certs/ca"
 local cert_file="/etc/ssl/local-certs/${domain}.pem"
 if ! wdev_regen_cert "$domain"; then
 echo -e "${C_RED}Error: gagal membuat sertifikat untuk ${domain}.${C_RESET}" >&2
 exit 1
 fi
 wdev_apply_server_names "$domain" 2>/dev/null || true

 # Otomatis pastikan domain ada di /etc/hosts (127.0.0.1)
 if ! hosts_has_domain "$domain"; then
 hosts_add_domain "$domain"
 echo -e "${C_CYAN}[+] Menambahkan ${domain} ke /etc/hosts (127.0.0.1)${C_RESET}"
 fi

 sync_browser_ssl_policies
 local pub
 pub=$(wdev_public_hosts "$domain" | paste -sd, - 2>/dev/null || true)
 echo -e "${C_GREEN}[✓] Sertifikat SSL untuk ${domain} diperbarui${pub:+ (SAN: ${pub})}${C_RESET}"
}

cmd_sync() {
  check_root
  echo -e "${C_BOLD}${C_CYAN}============================================================${C_RESET}"
  echo -e "${C_BOLD}${C_CYAN}  SINKRONISASI VHOST, /ETC/HOSTS & LOCAL SSL BROWSER        ${C_RESET}"
  echo -e "${C_BOLD}${C_CYAN}============================================================${C_RESET}"
  sync_vhosts_and_hosts
  sync_browser_ssl_policies

  if nginx -t >/dev/null 2>&1; then
    systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null || true
    echo -e "${C_GREEN}[✓] Konfigurasi Nginx berhasil dimuat ulang.${C_RESET}"
  else
    echo -e "${C_RED}[!] Peringatan: Konfigurasi Nginx memiliki sintaks bermasalah (nginx -t).${C_RESET}"
  fi
}

cmd_custom() {
  local domain="${1:-}"
  if [ -z "$domain" ]; then
    echo -e "${C_RED}Error: Masukkan nama domain!${C_RESET}"
    echo "Contoh: dev-site custom myapp.test"
    exit 1
  fi
 validate_domain "$domain" || exit 1
  local custom_conf="/etc/nginx/sites-available/${domain}.custom.conf"
  if [ ! -f "$custom_conf" ]; then
    echo -e "${C_YELLOW}Berkas custom belum ada. Membuat: $custom_conf...${C_RESET}"
    if [ "$EUID" -ne 0 ]; then
      exec sudo bash "$0" custom "$@"
    fi
    mkdir -p /etc/nginx/sites-available
    cat <<EOF_INIT > "$custom_conf"
# ==============================================================================
# ATURAN KUSTOM NGINX UNTUK: ${domain} (Pengganti .htaccess)
# ==============================================================================
# Tulis aturan rewrite atau konfigurasi Nginx tambahan di sini.
EOF_INIT
    chmod 644 "$custom_conf"
  fi

  if [ -n "${EDITOR:-}" ] && command -v "$EDITOR" >/dev/null 2>&1; then
    "$EDITOR" "$custom_conf"
  elif command -v nano >/dev/null 2>&1; then
    nano "$custom_conf"
  else
    echo -e "${C_BOLD}Berkas aturan kustom: $custom_conf${C_RESET}"
    echo "------------------------------------------------------------"
    cat "$custom_conf"
    echo "------------------------------------------------------------"
    echo "Edit berkas di atas menggunakan: sudo nano $custom_conf"
  fi
}

read_exposures() {
 local f
 f="$WDEV_ETC/exposures/${1}.conf"
 [ -f "$f" ] || return 0
 awk 'NF >= 2 && $1 !~ /^#/ { print $1 "\t" $2 }' "$f"
}

write_exposures() {
 local domain="$1" file tmp
 shift
 file="$WDEV_ETC/exposures/${domain}.conf"
 tmp="${file}.tmp.$$"
 mkdir -p "$WDEV_ETC/exposures"
 if [ "$#" -eq 0 ]; then
 rm -f "$file"
 return 0
 fi
 {
 echo "# Eksposur Cloudflare untuk ${domain} (dikelola dev-site, jangan edit manual)"
 printf '%s\n' "$@"
 } > "$tmp"
 chmod 644 "$tmp"
 mv -f "$tmp" "$file"
}

exposure_profile_exists() {
 [ -f "$WDEV_ETC/cloudflare/accounts/${1}.conf" ]
}

cmd_expose() {
 check_root
 local domain="" public_host="" profile="" keep=0 allow_deep=0 positional=0
 while [ $# -gt 0 ]; do
 case "$1" in
 --profile) profile=${2:-}; shift 2 ;;
 --profile=*) profile=${1#*=}; shift ;;
 --keep-cert) keep=1; shift ;;
 --allow-deep) allow_deep=1; shift ;;
 -*) echo -e "${C_RED}Error: argumen tidak dikenal: $1${C_RESET}" >&2; exit 1 ;;
 *)
 case "$positional" in
 0) domain=$1 ;;
 1) public_host=$1 ;;
 *) echo -e "${C_RED}Error: terlalu banyak argumen: $1${C_RESET}" >&2; exit 1 ;;
 esac
 positional=$((positional + 1))
 shift
 ;;
 esac
 done
 if [ -z "$domain" ] || [ -z "$public_host" ]; then
 echo -e "${C_RED}Error: Parameter tidak lengkap!${C_RESET}"
 echo "Penggunaan: sudo dev-site expose <domain> <public-host> --profile <profil> [--allow-deep]"
 exit 1
 fi
 validate_domain "$domain" || exit 1
 validate_public_host "$public_host" || exit 1
 [ -f "/etc/nginx/sites-available/${domain}.conf" ] || {
 echo -e "${C_RED}Error: vhost '${domain}' tidak ditemukan. Buat dulu: sudo dev-site create ${domain} <path>${C_RESET}" >&2
 exit 1
 }
 if [ -z "$profile" ]; then
 local -a _profs=()
 local p
 while IFS= read -r p; do [ -n "$p" ] && _profs+=("$p"); done < <(find "$WDEV_ETC/cloudflare/accounts" -maxdepth 1 -name '*.conf' -printf '%f\n' 2>/dev/null | sed 's/\.conf$//' | sort)
 if [ "${#_profs[@]}" -eq 1 ]; then
 profile="${_profs[0]}"
 echo "Menggunakan profil satu-satunya: ${profile}"
 else
 echo -e "${C_RED}Error: --profile wajib diisi. Profil tersedia:${C_RESET}"
 for p in "${_profs[@]}"; do echo " - $p"; done
 exit 1
 fi
 fi
 exposure_profile_exists "$profile" || {
 echo -e "${C_RED}Error: profil '${profile}' belum ada. Tambah dengan: sudo cf-tunnel account add ${profile}${C_RESET}" >&2
 exit 1
 }
 local profile_mode
 profile_mode=$(awk -F= '$1 == "CF_MODE" { sub(/^[^=]*=/, ""); print; exit }' "$WDEV_ETC/cloudflare/accounts/${profile}.conf")
 if [ "$profile_mode" = "token" ]; then
   echo -e "${C_CYAN}[i] Profil '${profile}' mode token: vhost Nginx & SSL mkcert akan dikonfigurasi.${C_RESET}"
 fi
 local zone zrc=0
 zone=$(awk -F= '$1 == "CF_ZONE" { sub(/^[^=]*=/, ""); print; exit }' "$WDEV_ETC/cloudflare/accounts/${profile}.conf")
 wdev_host_in_zone "$public_host" "$zone" "$allow_deep" || zrc=$?
 if [ "$zrc" -eq 2 ]; then
 echo -e "${C_RED}Error: host '${public_host}' adalah subdomain bertingkat di bawah '${zone}' (profil ${profile}).${C_RESET}" >&2
 echo "Universal SSL gratis hanya mencakup *.${zone}. Tambahkan --allow-deep jika zone memakai Total TLS / ACM." >&2
 exit 1
 elif [ "$zrc" -ne 0 ]; then
 echo -e "${C_RED}Error: host '${public_host}' bukan bagian dari zone '${zone:-?}' (profil ${profile}).${C_RESET}" >&2
 exit 1
 fi
 if [ "$public_host" = "$zone" ]; then
 echo -e "${C_CYAN}[i] Apex domain '${public_host}' akan memakai CNAME flattening Cloudflare.${C_RESET}"
 elif [ "$allow_deep" -eq 1 ] && [[ "${public_host%.$zone}" == *.* ]]; then
 echo -e "${C_YELLOW}[i] Subdomain bertingkat '${public_host}' diizinkan (--allow-deep); pastikan Total TLS / ACM aktif di Cloudflare.${C_RESET}"
 fi

 # Host publik tidak boleh dipakai site/profil/route lain
 local owner_domain="" owner_profile="" f
 if [ -d "$WDEV_ETC/exposures" ]; then
 while IFS= read -r f; do
 [ -f "$f" ] || continue
 local op
 op=$(awk -v h="$public_host" 'NF >= 2 && $1 == h { print $2; exit }' "$f")
 if [ -n "$op" ]; then
 owner_domain=$(basename "$f" .conf)
 owner_profile="$op"
 break
 fi
 done < <(find "$WDEV_ETC/exposures" -maxdepth 1 -type f -name '*.conf' -print 2>/dev/null | sort)
 fi
 if [ -n "$owner_domain" ]; then
 if [ "$owner_domain" = "$domain" ] && [ "$owner_profile" = "$profile" ]; then
 echo "Eksposur ${public_host} sudah terdaftar untuk ${domain} (profil ${profile})."
 return 0
 fi
 echo -e "${C_RED}Error: host '${public_host}' sudah dipakai oleh '${owner_domain}' (profil ${owner_profile}).${C_RESET}" >&2
 exit 1
 fi
 if [ -d "$WDEV_ETC/cloudflare/routes" ]; then
 local rf rp rh
 while IFS= read -r rf; do
 [ -f "$rf" ] || continue
 rp=$(basename "$rf" .conf)
 rh=$(awk -F'\t' -v h="$public_host" 'NF >= 3 && $1 == h && ($2 == "-" || $2 == "") { print $1; exit }' "$rf")
 if [ -n "$rh" ]; then
 echo -e "${C_RED}Error: host '${public_host}' sudah dipakai custom route di profil '${rp}'. Hapus dulu dengan: sudo cf-tunnel route rm --profile ${rp} ${public_host}${C_RESET}" >&2
 exit 1
 fi
 done < <(find "$WDEV_ETC/cloudflare/routes" -maxdepth 1 -type f -name '*.conf' -print 2>/dev/null | sort)
 fi

 # Host lokal tidak boleh berupa domain Cloudflare aktif
 if [ "$public_host" = "$domain" ]; then
 echo -e "${C_RED}Error: host publik tidak boleh sama dengan domain lokal '${domain}'.${C_RESET}" >&2
 exit 1
 fi

 local -a lines=()
 local h p
 while IFS=$'\t' read -r h p; do
 [ -n "$h" ] && lines+=("${h}	${p}")
 done < <(read_exposures "$domain")
 local prev_count=${#lines[@]}
 lines+=("${public_host}	${profile}")
 write_exposures "$domain" "${lines[@]}"

 rollback_expose() {
 if [ "$prev_count" -eq 0 ]; then
 write_exposures "$domain"
 else
 write_exposures "$domain" "${lines[@]:0:$prev_count}"
 fi
 wdev_apply_server_names "$domain" 2>/dev/null || true
 wdev_regen_cert "$domain" 2>/dev/null || true
 }

 echo -e "${C_CYAN}[*] Memperbarui server_name & sertifikat mkcert untuk ${domain}...${C_RESET}"
 wdev_apply_server_names "$domain" || { rollback_expose; echo -e "${C_RED}Error: gagal memperbarui server_name.${C_RESET}" >&2; exit 1; }
 if [ "$keep" -ne 1 ]; then
 wdev_regen_cert "$domain" || { rollback_expose; echo -e "${C_RED}Error: gagal regenerate sertifikat (mkcert).${C_RESET}" >&2; exit 1; }
 fi

 if nginx -t >/dev/null 2>&1; then
 systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null || true
 else
 rollback_expose
 echo -e "${C_RED}Error: konfigurasi Nginx tidak valid setelah expose; perubahan dibatalkan.${C_RESET}" >&2
 exit 1
 fi

 local cf_bin dns_rc=0
 cf_bin=$(find_cf_tunnel_bin) || true
 if [ -n "$cf_bin" ]; then
 "$cf_bin" sync --profile "$profile" >/dev/null 2>&1 || echo -e "${C_YELLOW}[!] Sinkronisasi tunnel '${profile}' gagal; jalankan: sudo cf-tunnel sync --profile ${profile}${C_RESET}" >&2
 "$cf_bin" dns ensure --profile "$profile" >/dev/null 2>&1 || dns_rc=$?
 if [ "$dns_rc" -eq 4 ]; then
 local tid_hint
 tid_hint=$(awk -F= '$1 == "CF_TUNNEL_ID" { sub(/^[^=]*=/, ""); print; exit }' "$WDEV_ETC/cloudflare/accounts/${profile}.conf")
 echo -e "${C_YELLOW}[i] Profil '${profile}' tidak punya CF_API_TOKEN. Buat CNAME manual:${C_RESET}" >&2
 echo "    ${public_host} -> ${tid_hint:-<tunnel-id>}.cfargotunnel.com (Proxied)" >&2
 elif [ "$dns_rc" -ne 0 ]; then
 echo -e "${C_YELLOW}[!] DNS '${public_host}' belum tentu dibuat; jalankan: sudo cf-tunnel dns add --profile ${profile} ${public_host}${C_RESET}" >&2
 fi
 else
 echo -e "${C_YELLOW}[!] cf-tunnel belum terpasang; ingress & DNS belum disinkronkan.${C_RESET}" >&2
 fi

 echo -e "${C_GREEN}[✓] ${public_host} -> https://${domain} (profil ${profile})${C_RESET}"
 echo "Uji lokal : curl -kI https://${public_host} --resolve ${public_host}:443:127.0.0.1"
 echo "Uji publik : curl -sI https://${public_host} | grep -i cf-ray"
 if [ "$profile_mode" = "token" ]; then
   local has_api
   has_api=$(awk -F= '$1 == "CF_API_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' "$WDEV_ETC/cloudflare/accounts/${profile}.conf")
   echo -e "${C_YELLOW}[!] Langkah mode token:${C_RESET}"
   if [ -n "$has_api" ]; then
     echo "    Opsi 1 (API)       : sudo cf-tunnel sync --remote --profile ${profile}"
   fi
   echo "    Opsi 2 (Dashboard) : Networks > Tunnels > ${profile} > Public Hostname"
   echo "      Public Hostname  : ${public_host}"
   echo "      Service          : HTTPS://127.0.0.1:443"
   echo "      TLS Settings     : No TLS Verify = Enabled"
 fi
}

cmd_unexpose() {
 check_root
 local domain="" public_host="" force=0 positional=0
 while [ $# -gt 0 ]; do
 case "$1" in
 --force|-f) force=1; shift ;;
 -*) echo -e "${C_RED}Error: argumen tidak dikenal: $1${C_RESET}" >&2; exit 1 ;;
 *)
 case "$positional" in
 0) domain=$1 ;;
 1) public_host=$1 ;;
 *) echo -e "${C_RED}Error: terlalu banyak argumen: $1${C_RESET}" >&2; exit 1 ;;
 esac
 positional=$((positional + 1))
 shift
 ;;
 esac
 done
 if [ -z "$domain" ]; then
 echo -e "${C_RED}Error: Masukkan domain!${C_RESET}"
 echo "Penggunaan: sudo dev-site unexpose <domain> [public-host]"
 exit 1
 fi
 validate_domain "$domain" || exit 1

 local -a all=() remaining=()
 local h p profile_of_target=""
 while IFS=$'\t' read -r h p; do
 [ -n "$h" ] || continue
 all+=("${h}	${p}")
 if [ -z "$public_host" ] || [ "$h" = "$public_host" ]; then
 profile_of_target="$p"
 else
 remaining+=("${h}	${p}")
 fi
 done < <(read_exposures "$domain")

 if [ "${#all[@]}" -eq 0 ]; then
 echo "Tidak ada eksposur untuk '${domain}'."
 return 0
 fi
 if [ -n "$public_host" ] && [ "${#all[@]}" -eq "${#remaining[@]}" ]; then
 echo -e "${C_RED}Error: '${public_host}' tidak terdaftar untuk '${domain}'.${C_RESET}" >&2
 exit 1
 fi

 if [ "$force" -ne 1 ]; then
 echo "Eksposur yang akan dihapus:"
 local kept=0
 local e ep
 for e in "${all[@]}"; do
 ep="${e#*$'\t'}"
 if [ -z "$public_host" ] || [ "${e%%$'\t'*}" = "$public_host" ]; then
 echo " - ${e%%$'\t'*} (profil ${ep})"
 kept=$((kept + 1))
 fi
 done
 if [ -t 0 ]; then
 local ans
 read -rp "Hapus ${kept} eksposur (ingress + CNAME + SAN cert)? [y/N]: " ans
 case "$ans" in y|Y|yes) ;; *) echo "Dibatalkan."; return 0 ;; esac
 fi
 fi

 write_exposures "$domain" ${remaining[@]+"${remaining[@]}"}
 wdev_apply_server_names "$domain" 2>/dev/null || true
 wdev_regen_cert "$domain" 2>/dev/null || true

 local cf_bin dns_rm_rc
 cf_bin=$(find_cf_tunnel_bin) || true
 if [ -n "$cf_bin" ]; then
 if [ -n "$public_host" ]; then
 if [ -n "$profile_of_target" ]; then
 dns_rm_rc=0
 "$cf_bin" dns rm --profile "$profile_of_target" "$public_host" >/dev/null 2>&1 || dns_rm_rc=$?
 if [ "$dns_rm_rc" -eq 4 ]; then
 echo -e "${C_YELLOW}[i] Profil '${profile_of_target}' tanpa CF_API_TOKEN; hapus CNAME '${public_host}' manual jika perlu.${C_RESET}"
 elif [ "$dns_rm_rc" -ne 0 ]; then
 echo -e "${C_YELLOW}[!] Record DNS '${public_host}' tidak terhapus otomatis.${C_RESET}"
 fi
 fi
 else
 local e ep eh
 for e in "${all[@]}"; do
 eh="${e%%$'\t'*}"
 ep="${e#*$'\t'}"
 dns_rm_rc=0
 "$cf_bin" dns rm --profile "$ep" "$eh" >/dev/null 2>&1 || dns_rm_rc=$?
 if [ "$dns_rm_rc" -eq 4 ]; then
 echo -e "${C_YELLOW}[i] Profil '${ep}' tanpa CF_API_TOKEN; hapus CNAME '${eh}' manual jika perlu.${C_RESET}"
 fi
 done
 fi
 "$cf_bin" sync --all >/dev/null 2>&1 || true
 fi

 if nginx -t >/dev/null 2>&1; then
 systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null || true
 fi
 echo -e "${C_GREEN}[✓] Eksposur '${domain}'${public_host:+ (${public_host})} dihapus. Virtual host lokal tetap aktif.${C_RESET}"
}

cmd_exposures() {
 local f domain host profile status count=0
 echo -e "${C_BOLD}=================================================================================================${C_RESET}"
 echo -e "${C_BOLD} DAFTAR EKSPOSUR CLOUDFLARE (DEV-SITE) ${C_RESET}"
 echo -e "${C_BOLD}=================================================================================================${C_RESET}"
 printf "${C_BOLD}%-24s | %-40s | %-12s | %s${C_RESET}\n" "Domain" "Public Host" "Profile" "Tunnel"
 echo "-------------------------+------------------------------------------+--------------+------------------"
 if [ -d "$WDEV_ETC/exposures" ]; then
 for f in "$WDEV_ETC/exposures"/*.conf; do
 [ -f "$f" ] || continue
 domain=$(basename "$f" .conf)
 while IFS=$'\t' read -r host profile; do
 [ -n "$host" ] || continue
 status="-"
 if [ -f "$WDEV_ETC/cloudflare/accounts/${profile}.conf" ]; then
 status=$(awk -F= '$1 == "CF_TUNNEL_NAME" { sub(/^[^=]*=/, ""); print; exit }' "$WDEV_ETC/cloudflare/accounts/${profile}.conf")
 fi
 printf "%-24s | %-40s | %-12s | %s\n" "$domain" "$host" "$profile" "${status:--}"
 count=$((count + 1))
 done < <(read_exposures "$domain")
 done
 fi
 if [ "$count" -eq 0 ]; then
 echo " Belum ada eksposur. Gunakan: sudo dev-site expose <domain> <public-host> --profile <profil>"
 fi
 echo -e "${C_BOLD}=================================================================================================${C_RESET}"
}

show_help() {
  echo "Penggunaan: dev-site <perintah> [argumen]"
  echo ""
  echo "Perintah Utama:"
  echo " create <domain> <docroot> [php_ver] [type] Buat virtual host Nginx baru + SSL + aturan framework"
  echo " delete <domain> Hapus virtual host, SSL cert, dan entri hosts"
  echo " list, ls Tampilkan semua virtual host dev-site"
  echo " cert <domain> Buat / perbarui SSL certificate untuk domain"
  echo " expose <domain> <public-host> --profile <p> [--allow-deep] Ekspos vhost ke Cloudflare Tunnel"
  echo " unexpose <domain> [public-host] Hapus eksposur publik (vhost lokal tetap ada)"
  echo " exposures Tampilkan semua eksposur publik per profil"
  echo " sync, update-ssl Sinkronisasi /etc/hosts, SSL certs, & trust store browser"
  echo "  custom <domain>                              Buka/tampilkan berkas aturan kustom (pengganti .htaccess)"
  echo "  help, --help                                 Tampilkan bantuan ini"
  echo ""
  echo "Tipe Framework ([type]):"
  echo "  auto (default)   Otomatis mendeteksi Laravel, WordPress, CodeIgniter, Symfony, atau generic"
  echo "  laravel          Routing Laravel (try_files -> index.php?\$query_string, docroot ke /public)"
  echo "  wordpress        Routing WordPress (try_files -> index.php?\$args, proteksi uploads)"
  echo "  ci4              CodeIgniter 4 (docroot ke /public, proteksi writable)"
  echo "  ci3              CodeIgniter 3 (dukungan PATH_INFO, proteksi application & system)"
  echo "  symfony          Symfony (docroot ke /public, index.php?\$is_args\$args)"
  echo "  generic          Universal PHP (try_files, proteksi .env/.git, FastCGI PATH_INFO)"
  echo "  proxy            Reverse proxy ke port lokal (Go, Node.js, Python, Rust, Docker)"
  echo ""
  echo "Dukungan Pengganti .htaccess:"
  echo "  1. Anda dapat membuat file '.nginx.conf' atau 'nginx.conf' langsung di folder proyek Anda."
  echo "     Aturan di dalamnya akan otomatis dimuat oleh server block Nginx."
  echo "  2. Atau edit file kustom via: sudo dev-site custom <domain>"
  echo ""
  echo "Contoh:"
  echo "  sudo dev-site create my-laravel.test /var/www/my-laravel"
  echo "  sudo dev-site create my-blog.test /var/www/my-blog 8.4 wordpress"
  echo "  sudo dev-site create my-go.test 3221 proxy"
  echo "  sudo dev-site create my-node.test 3000 proxy"
  echo "  sudo dev-site list"
}

case "${1:-}" in
  create)
    shift
    cmd_create "$@"
    ;;
  delete|remove)
    shift
    cmd_delete "$@"
    ;;
  list|ls)
    cmd_list
    ;;
cert)
 shift
 cmd_cert "$@"
 ;;
 expose)
 shift
 cmd_expose "$@"
 ;;
 unexpose)
 shift
 cmd_unexpose "$@"
 ;;
 exposures|exposure-list)
 cmd_exposures
 ;;
 sync|sync-hosts|update-ssl)
    shift
    cmd_sync "$@"
    ;;
  custom|htaccess|rewrite)
    shift
    cmd_custom "$@"
    ;;
  help|--help|-h|"")
    show_help
    ;;
  *)
    echo "Perintah tidak dikenal: $1"
    show_help
    exit 1
    ;;
esac
EOF_DEVSITE
 local shared_file
 shared_file=$(mktemp)
 declare -f hosts_has_domain hosts_add_domain hosts_remove_domain \
 wdev_exposures_dir wdev_accounts_dir wdev_exposure_file wdev_exposure_lines \
 wdev_public_hosts wdev_host_owner wdev_profile_list wdev_profile_exists \
 wdev_profile_get wdev_profile_zone wdev_host_in_zone wdev_apply_server_names \
 wdev_cert_covers_publics wdev_regen_cert wdev_refresh_site \
 get_php_fpm_socket sync_browser_ssl_policies sync_vhosts_and_hosts > "$shared_file"
 printf 'get_socket() { get_php_fpm_socket "$@"; }\n' >> "$shared_file"
 awk -v shared="$shared_file" '
 $0 == "# @@SHARED_FUNCTIONS@@" {
 while ((getline line < shared) > 0) print line
 close(shared); next
 }
 { print }' "$tmp_dest" > "${tmp_dest}.new" && mv -f "${tmp_dest}.new" "$tmp_dest"
 rm -f "$shared_file"

 chmod 755 "$tmp_dest"
 mv -f "$tmp_dest" "$target_dest"
}

ensure_dev_site_helper() {
 local u_user="${SUDO_USER:-$USER}"
 local u_home
 u_home=$(getent passwd "$u_user" 2>/dev/null | cut -d: -f6)
 [ -z "$u_home" ] && u_home="$HOME"
 local u_dir="$u_home/.local/bin"
 mkdir -p "$u_dir" 2>/dev/null || true
 install_dev_site_helper "$u_dir/dev-site"

 if [ "$EUID" -eq 0 ]; then
 install_dev_site_helper "/usr/local/bin/dev-site"
 fi
}

step_nginx() {
  check_root
  detect_os
  echo "Menginstal Nginx (Versi Mainline Terbaru)..."
  setup_repo_nginx

  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    dnf -y install nginx
  else
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y nginx
  fi

  # Struktur direktori virtual hosts
  mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled /var/www /etc/ssl/local-certs
  chmod 755 /etc/ssl/local-certs

  # Pastikan nginx.conf memuat sites-enabled
  if [ -f /etc/nginx/nginx.conf ] && ! grep -q "sites-enabled" /etc/nginx/nginx.conf; then
    if grep -q "include /etc/nginx/conf.d/\*.conf;" /etc/nginx/nginx.conf; then
      sed -i 's|include /etc/nginx/conf.d/\*.conf;|include /etc/nginx/conf.d/*.conf;\n    include /etc/nginx/sites-enabled/*;|' /etc/nginx/nginx.conf
    else
      sed -i '/http {/a \    include /etc/nginx/sites-enabled/*;' /etc/nginx/nginx.conf
    fi
  fi

  # Konfigurasi SELinux boolean jika aktif
  if command -v setsebool >/dev/null 2>&1; then
    echo "Mengonfigurasi SELinux boolean untuk Nginx..."
    setsebool -P httpd_can_network_connect 1 2>/dev/null || true
    setsebool -P httpd_read_user_content 1 2>/dev/null || true
    setsebool -P httpd_enable_homedirs 1 2>/dev/null || true
  fi

  # Pastikan user nginx masuk ke grup www-data / apache agar bisa membaca socket PHP-FPM
  if id nginx >/dev/null 2>&1; then
    getent group www-data >/dev/null 2>&1 && usermod -aG www-data nginx 2>/dev/null || true
    getent group apache >/dev/null 2>&1 && usermod -aG apache nginx 2>/dev/null || true
  fi
  if id apache >/dev/null 2>&1 && getent group nginx >/dev/null 2>&1; then
    usermod -aG nginx apache 2>/dev/null || true
  fi

  # Uji sintaks Nginx dan jalankan service
  nginx -t
  systemctl enable nginx 2>/dev/null || true
  systemctl restart nginx 2>/dev/null || true
  echo "Nginx berhasil dipasang dan berjalan!"
}

PHP_VER_OPTION=""
PHP_EXTS_OPTION=""

prompt_php_options() {
  if [ -n "$PHP_VER_OPTION" ] && [ -n "$PHP_EXTS_OPTION" ]; then
    return 0
  fi

  detect_os
  local TTY_OUT="/dev/stdout"
  local TTY_IN="/dev/stdin"

  if [ -w /dev/tty ] && [ -r /dev/tty ]; then
    TTY_OUT="/dev/tty"
    TTY_IN="/dev/tty"
  elif [ ! -t 0 ]; then
    [ -z "$PHP_VER_OPTION" ] && PHP_VER_OPTION="8.4"
    [ -z "$PHP_EXTS_OPTION" ] && PHP_EXTS_OPTION="full"
    return 0
  fi

  if [ -z "$PHP_VER_OPTION" ]; then
    echo "" > "$TTY_OUT"
    echo "==========================================" > "$TTY_OUT"
    echo "         PILIH VERSI DEFAULT PHP          " > "$TTY_OUT"
    echo "==========================================" > "$TTY_OUT"
    echo "Komponen ini mendukung instalasi multi-versi PHP." > "$TTY_OUT"
    echo "Pilih versi yang akan dijadikan sebagai DEFAULT aktif (CLI & FPM):" > "$TTY_OUT"
    echo "" > "$TTY_OUT"
    echo "1. PHP 8.4 (Modern / Rilis Terbaru) [Rekomendasi Default]" > "$TTY_OUT"
    echo "2. PHP 8.3 (Stabil & Populer)" > "$TTY_OUT"
    echo "3. PHP 8.2 (LTS)" > "$TTY_OUT"
    echo "4. PHP 8.1" > "$TTY_OUT"
    echo "5. Default Sistem ($PKG_MGR native)" > "$TTY_OUT"
    echo "------------------------------------------" > "$TTY_OUT"
    echo "Catatan: Anda dapat berganti versi kapan saja via perintah 'php-switch'." > "$TTY_OUT"
    echo "==========================================" > "$TTY_OUT"
    printf "%s" "Pilih versi PHP default [1-5, default: 1]: " > "$TTY_OUT"
    local v_choice=""
    read -r v_choice < "$TTY_IN"
    case "$v_choice" in
      2) PHP_VER_OPTION="8.3" ;;
      3) PHP_VER_OPTION="8.2" ;;
      4) PHP_VER_OPTION="8.1" ;;
      5) PHP_VER_OPTION="system" ;;
      *) PHP_VER_OPTION="8.4" ;;
    esac
  fi

  if [ -z "$PHP_EXTS_OPTION" ]; then
    echo "" > "$TTY_OUT"
    echo "==========================================" > "$TTY_OUT"
    echo "       PILIH KUMPULAN EKSTENSI PHP        " > "$TTY_OUT"
    echo "==========================================" > "$TTY_OUT"
    echo "1. Lengkap / Web Full Stack (FPM, MySQL, PgSQL, Redis, Mbstring, XML, GD, Intl, Zip, Bcmath, Soap, OPcache) [Default]" > "$TTY_OUT"
    echo "2. Standar / Minimal (FPM, CLI, MySQL, Mbstring, XML, Zip, OPcache)" > "$TTY_OUT"
    echo "3. Kustom (Tentukan daftar ekstensi sendiri)" > "$TTY_OUT"
    echo "==========================================" > "$TTY_OUT"
    printf "%s" "Pilih opsi ekstensi [1-3, default: 1]: " > "$TTY_OUT"
    read -r e_choice < "$TTY_IN"
    case "$e_choice" in
      2) PHP_EXTS_OPTION="minimal" ;;
      3)
        printf "%s" "Masukkan ekstensi yang dipisahkan spasi/koma (contoh: mysql pgsql redis zip gd): " > "$TTY_OUT"
        read -r user_exts < "$TTY_IN"
        PHP_EXTS_OPTION="${user_exts:-full}"
        ;;
      *) PHP_EXTS_OPTION="full" ;;
    esac
  fi
}

step_php() {
  check_root
  detect_os
  prompt_php_options

  local CHOSEN_VER="${PHP_VER_OPTION:-8.4}"
  local CHOSEN_EXTS="${PHP_EXTS_OPTION:-full}"

  echo "Menginstal PHP versi: $CHOSEN_VER (Ekstensi: $CHOSEN_EXTS)..."
  setup_repo_php

  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    if [ "$CHOSEN_VER" != "system" ]; then
      echo "Mengaktifkan modul Remi PHP stream ${CHOSEN_VER}..."
      dnf -y module reset php 2>/dev/null || true
      dnf -y module enable "php:remi-${CHOSEN_VER}" 2>/dev/null || true
    fi

    local pkgs=("php" "php-cli" "php-fpm" "php-common")
    case "$CHOSEN_EXTS" in
      minimal)
        pkgs+=("php-mysqlnd" "php-mbstring" "php-xml" "php-pecl-zip")
        ;;
      full)
        pkgs+=("php-mysqlnd" "php-pgsql" "php-mbstring" "php-xml" "php-pecl-zip" "php-gd" "php-intl" "php-bcmath" "php-soap")
        for r_pkg in php-pecl-redis6 php-pecl-redis; do
          if dnf $DNF_RO_OPTS -C list --available "$r_pkg" >/dev/null 2>&1 || rpm -q "$r_pkg" >/dev/null 2>&1; then
            pkgs+=("$r_pkg")
            break
          fi
        done
        ;;
      *)
        IFS=', ' read -r -a custom_array <<< "$CHOSEN_EXTS"
        for ext in "${custom_array[@]}"; do
          [ -z "$ext" ] && continue
          if [[ "$ext" =~ ^php- ]]; then
            pkgs+=("$ext")
          else
            case "$ext" in
              mysql|mysqli|pdo_mysql) pkgs+=("php-mysqlnd") ;;
              pgsql|pdo_pgsql) pkgs+=("php-pgsql") ;;
              redis)
                for r_pkg in php-pecl-redis6 php-pecl-redis; do
                  if dnf $DNF_RO_OPTS -C list --available "$r_pkg" >/dev/null 2>&1 || rpm -q "$r_pkg" >/dev/null 2>&1; then
                    pkgs+=("$r_pkg")
                    break
                  fi
                done
                ;;
              zip) pkgs+=("php-pecl-zip") ;;
              *) pkgs+=("php-${ext}") ;;
            esac
          fi
        done
        ;;
    esac

    dnf -y install "${pkgs[@]}"

    # Pastikan user nginx & apache dapat mengakses socket PHP-FPM
    if id nginx >/dev/null 2>&1; then
      getent group apache >/dev/null 2>&1 && usermod -aG apache nginx 2>/dev/null || true
      getent group www-data >/dev/null 2>&1 && usermod -aG www-data nginx 2>/dev/null || true
    fi
    if id apache >/dev/null 2>&1 && getent group nginx >/dev/null 2>&1; then
      usermod -aG nginx apache 2>/dev/null || true
    fi

    systemctl enable php-fpm 2>/dev/null || true
    systemctl restart php-fpm 2>/dev/null || true
  else
    # APT (Debian / Ubuntu)
    export DEBIAN_FRONTEND=noninteractive
    local v="$CHOSEN_VER"
    if [ "$v" = "system" ]; then
      v=$(apt-cache policy php 2>/dev/null | awk '/Candidate:/ {print $2}' | cut -d+ -f1 | grep -o '^[0-9]\.[0-9]' || echo "8.4")
    fi

    local pkgs=("php${v}" "php${v}-cli" "php${v}-fpm" "php${v}-common")
    case "$CHOSEN_EXTS" in
      minimal)
        pkgs+=("php${v}-mysql" "php${v}-mbstring" "php${v}-xml" "php${v}-curl" "php${v}-zip" "php${v}-opcache")
        ;;
      full)
        pkgs+=("php${v}-mysql" "php${v}-pgsql" "php${v}-redis" "php${v}-mbstring" "php${v}-xml" "php${v}-curl" "php${v}-zip" "php${v}-gd" "php${v}-intl" "php${v}-bcmath" "php${v}-soap" "php${v}-opcache")
        ;;
      *)
        IFS=', ' read -r -a custom_array <<< "$CHOSEN_EXTS"
        for ext in "${custom_array[@]}"; do
          [ -z "$ext" ] && continue
          if [[ "$ext" =~ ^php ]]; then
            pkgs+=("$ext")
          else
            pkgs+=("php${v}-${ext}")
          fi
        done
        ;;
    esac

    apt-get install -y "${pkgs[@]}"

    # Pastikan user nginx masuk ke grup www-data / apache agar bisa membaca socket PHP-FPM
    if id nginx >/dev/null 2>&1; then
      getent group www-data >/dev/null 2>&1 && usermod -aG www-data nginx 2>/dev/null || true
      getent group apache >/dev/null 2>&1 && usermod -aG apache nginx 2>/dev/null || true
    fi

    systemctl enable "php${v}-fpm" 2>/dev/null || true
    systemctl restart "php${v}-fpm" 2>/dev/null || true
    update-alternatives --set php "/usr/bin/php${v}" 2>/dev/null || true
  fi

  install_php_switch_helper
  echo "PHP $CHOSEN_VER dan ekstensi berhasil dipasang!"
}

step_composer() {
 check_root
 echo "Mengunduh dan memasang Composer terbaru..."
 local COMPOSER_BIN="/usr/local/bin/composer"
 local COMPOSER_TMP
 COMPOSER_TMP=$(mktemp)
 TMP_CLEANUP_FILES+=("$COMPOSER_TMP")
 curl -fsSL https://getcomposer.org/composer-stable.phar -o "$COMPOSER_TMP"
 local COMPOSER_SHA
 COMPOSER_SHA=$(curl -fsSL -m 10 "https://getcomposer.org/download/latest-stable/composer.phar.sha256sum" 2>/dev/null | awk '{print $1}' | head -n1)
 if ! verify_sha256 "$COMPOSER_TMP" "$COMPOSER_SHA" "composer"; then
 rm -f "$COMPOSER_TMP"
 return 1
 fi
 install -m 755 "$COMPOSER_TMP" "$COMPOSER_BIN"
 rm -f "$COMPOSER_TMP"
 if [ -x "$COMPOSER_BIN" ]; then
 echo "Composer berhasil dipasang: $(COMPOSER_ALLOW_SUPERUSER=1 "$COMPOSER_BIN" --version --no-interaction 2>/dev/null | head -n1)"
 else
 echo "Gagal memasang Composer."
 return 1
 fi
}

step_local_ssl() {
  check_root
  detect_os
  echo "Menginstal mkcert dan dependensi NSS tools untuk Local SSL..."
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    dnf -y install mkcert nss-tools 2>/dev/null || true
  else
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y mkcert libnss3-tools 2>/dev/null || true
  fi
 if ! command -v mkcert >/dev/null 2>&1; then
 echo "Mengunduh mkcert binary langsung..."
 local MKCERT_VER="v1.4.4"
 local MKCERT_ARCH
 case "$(uname -m)" in
 x86_64|amd64) MKCERT_ARCH="amd64" ;;
 aarch64|arm64) MKCERT_ARCH="arm64" ;;
 *) echo "Error: arsitektur $(uname -m) belum didukung unduhan mkcert otomatis."; return 1 ;;
 esac
 local MKCERT_TMP
 MKCERT_TMP=$(mktemp)
 if ! curl -fsSL "https://github.com/FiloSottile/mkcert/releases/download/${MKCERT_VER}/mkcert-${MKCERT_VER}-linux-${MKCERT_ARCH}" -o "$MKCERT_TMP"; then
 rm -f "$MKCERT_TMP"
 echo "Error: gagal mengunduh mkcert untuk ${MKCERT_ARCH}."
 return 1
 fi
 local MKCERT_SHA
 MKCERT_SHA=$(get_github_asset_digest "FiloSottile/mkcert" "mkcert-${MKCERT_VER}-linux-${MKCERT_ARCH}")
 if ! verify_sha256 "$MKCERT_TMP" "$MKCERT_SHA" "mkcert ${MKCERT_VER}"; then
  rm -f "$MKCERT_TMP"
  return 1
 fi
 if [ "$(head -c 4 "$MKCERT_TMP" | od -An -tx1 | tr -d '[:space:]')" != "7f454c46" ]; then
  rm -f "$MKCERT_TMP"
  echo "Error: berkas mkcert yang diunduh bukan binary ELF."
  return 1
 fi
 install -m 755 "$MKCERT_TMP" /usr/local/bin/mkcert
 rm -f "$MKCERT_TMP"
 fi

  # Direktori CA bersama & local certs
  local CA_DIR="/etc/ssl/local-certs/ca"
  mkdir -p "$CA_DIR" /etc/ssl/local-certs
  chmod 755 /etc/ssl/local-certs "$CA_DIR"
  command -v chcon >/dev/null 2>&1 && chcon -R -t cert_t /etc/ssl/local-certs 2>/dev/null || true

  # Ekspor CAROOT secara sistemik agar semua user & sudo menggunakan CA yang sama
  export CAROOT="$CA_DIR"
  cat <<'EOF_MKCERT_ENV' > /etc/profile.d/mkcert.sh
# Local SSL shared CA directory
export CAROOT="/etc/ssl/local-certs/ca"
EOF_MKCERT_ENV
  chmod 644 /etc/profile.d/mkcert.sh 2>/dev/null || true

  # Migrasi jika sebelumnya root sudah memiliki CA di /root/.local/share/mkcert
  if [ ! -f "$CA_DIR/rootCA.pem" ] && [ -f "/root/.local/share/mkcert/rootCA.pem" ]; then
    cp -a /root/.local/share/mkcert/* "$CA_DIR/" 2>/dev/null || true
  fi

  echo "Menginstal Certificate Authority (CA) lokal ke sistem..."
  mkcert -install
  chmod 644 "$CA_DIR/rootCA.pem" 2>/dev/null || true
  [ -f "$CA_DIR/rootCA-key.pem" ] && chmod 600 "$CA_DIR/rootCA-key.pem" 2>/dev/null || true

  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6 || echo "/home/$TARGET_USER")

  # Pastikan database NSS Chrome sudah diinisialisasi
  mkdir -p "$TARGET_HOME/.pki/nssdb" /root/.pki/nssdb
  if command -v certutil >/dev/null 2>&1; then
    [ ! -f "$TARGET_HOME/.pki/nssdb/cert9.db" ] && certutil -d "sql:$TARGET_HOME/.pki/nssdb" -N --empty-password 2>/dev/null || true
    [ ! -f "/root/.pki/nssdb/cert9.db" ] && certutil -d "sql:/root/.pki/nssdb" -N --empty-password 2>/dev/null || true
  fi
  [ -n "$TARGET_USER" ] && chown -R "$TARGET_USER:$TARGET_USER" "$TARGET_HOME/.pki" 2>/dev/null || true

  if [ -n "$TARGET_USER" ] && [ "$TARGET_USER" != "root" ]; then
    echo "Menginstal CA mkcert ke user trust store ($TARGET_USER)..."
    sudo -u "$TARGET_USER" env CAROOT="$CA_DIR" mkcert -install 2>/dev/null || true
    if command -v certutil >/dev/null 2>&1 && [ -f "$CA_DIR/rootCA.pem" ]; then
      sudo -u "$TARGET_USER" certutil -d "sql:$TARGET_HOME/.pki/nssdb" -A -t "C,," -n "mkcert root CA" -i "$CA_DIR/rootCA.pem" 2>/dev/null || true
    fi
  fi

  # Pasang kebijakan enterprise Google Chrome agar CA dipercaya secara native oleh browser
 mkdir -p /etc/opt/chrome/policies/managed
 if [ -f "$CA_DIR/rootCA.pem" ]; then
 local CA_ESC
 CA_ESC=$(awk '{printf "%s\\n", $0}' "$CA_DIR/rootCA.pem")
 cat <<EOF_CHROME_POLICY > /etc/opt/chrome/policies/managed/local_ca.json
{
"CACertificates": [
"${CA_ESC}"
]
}
EOF_CHROME_POLICY
 chmod 644 /etc/opt/chrome/policies/managed/local_ca.json 2>/dev/null || true
 fi

  # Hasilkan sertifikat default untuk localhost
  if [ ! -f /etc/ssl/local-certs/localhost.pem ]; then
    echo "Menghasilkan sertifikat SSL default untuk localhost & 127.0.0.1..."
    CAROOT="$CA_DIR" mkcert -cert-file /etc/ssl/local-certs/localhost.pem -key-file /etc/ssl/local-certs/localhost-key.pem localhost 127.0.0.1 ::1 >/dev/null 2>&1 || true
    chmod 644 /etc/ssl/local-certs/localhost.pem 2>/dev/null || true
    chmod 600 /etc/ssl/local-certs/localhost-key.pem 2>/dev/null || true
  fi

  install_dev_site_helper
  install_php_switch_helper
  echo "Local SSL (mkcert) dan CLI dev-site berhasil disiapkan!"
}

step_cloudflare() {
 check_root
 detect_os
 echo "Menyiapkan Cloudflare Tunnel (cloudflared)..."

 if ! command -v cloudflared >/dev/null 2>&1 && [ ! -x /usr/local/bin/cloudflared ]; then
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
 curl -fsSL https://pkg.cloudflare.com/cloudflared.repo -o /etc/yum.repos.d/cloudflared.repo 2>/dev/null || true
 dnf -y install cloudflared 2>/dev/null || true
 elif [ "$PKG_MGR" = "apt" ]; then
 mkdir -p /usr/share/keyrings 2>/dev/null || true
 curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg -o /usr/share/keyrings/cloudflare-main.gpg 2>/dev/null || true
 if [ -s /usr/share/keyrings/cloudflare-main.gpg ]; then
 echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main" > /etc/apt/sources.list.d/cloudflared.list
 apt-get update -y >/dev/null 2>&1 || true
 apt-get install -y cloudflared 2>/dev/null || true
 fi
 fi
 fi

 if ! command -v cloudflared >/dev/null 2>&1 && [ ! -x /usr/local/bin/cloudflared ]; then
 echo "Repo paket tidak tersedia; mengunduh binary cloudflared dari GitHub..."
 local CF_ARCH CF_VER CF_ASSET CF_TMP CF_SHA
 case "$(uname -m)" in
 x86_64|amd64) CF_ARCH="amd64" ;;
 aarch64|arm64) CF_ARCH="arm64" ;;
 *) echo "Error: arsitektur $(uname -m) belum didukung unduhan cloudflared otomatis."; return 1 ;;
 esac
 CF_VER=$(get_github_latest_tag "cloudflare/cloudflared" "-")
 if [ -z "$CF_VER" ] || [ "$CF_VER" = "-" ]; then
 echo "Error: gagal menentukan rilis cloudflared terbaru."
 return 1
 fi
 CF_ASSET="cloudflared-linux-${CF_ARCH}"
 CF_TMP=$(mktemp)
 TMP_CLEANUP_FILES+=("$CF_TMP")
 if ! curl -fsSL "https://github.com/cloudflare/cloudflared/releases/download/${CF_VER}/${CF_ASSET}" -o "$CF_TMP"; then
 rm -f "$CF_TMP"
 echo "Error: gagal mengunduh cloudflared ${CF_VER} (${CF_ARCH})."
 return 1
 fi
 CF_SHA=$(get_github_asset_digest "cloudflare/cloudflared" "$CF_ASSET")
 if ! verify_sha256 "$CF_TMP" "$CF_SHA" "cloudflared ${CF_VER}"; then
 rm -f "$CF_TMP"
 return 1
 fi
 if [ "$(head -c 4 "$CF_TMP" | od -An -tx1 | tr -d '[:space:]')" != "7f454c46" ]; then
 rm -f "$CF_TMP"
 echo "Error: berkas cloudflared yang diunduh bukan binary ELF."
 return 1
 fi
 install -m 755 "$CF_TMP" /usr/local/bin/cloudflared
 rm -f "$CF_TMP"
 fi

 local cf_bin cf_ver
 cf_bin=$(command -v cloudflared 2>/dev/null || true)
 [ -z "$cf_bin" ] && [ -x /usr/local/bin/cloudflared ] && cf_bin=/usr/local/bin/cloudflared
 [ -n "$cf_bin" ] || { echo "Error: cloudflared gagal dipasang."; return 1; }
 cf_ver=$("$cf_bin" --version 2>/dev/null | awk '{print $3}')

 mkdir -p "$WDEV_ETC/cloudflare/accounts" "$WDEV_ETC/cloudflare/routes" "$WDEV_ETC/cloudflare/access" "$WDEV_ETC/exposures" /etc/cloudflared
 chmod 700 "$WDEV_ETC/cloudflare" "$WDEV_ETC/cloudflare/accounts" "$WDEV_ETC/cloudflare/routes" "$WDEV_ETC/cloudflare/access" /etc/cloudflared 2>/dev/null || true
 chmod 755 "$WDEV_ETC" "$WDEV_ETC/exposures" 2>/dev/null || true

 install_cf_tunnel_helper "/usr/local/bin/cf-tunnel" || return 1
 if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
 local u_home
 u_home=$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)
 if [ -n "$u_home" ]; then
 mkdir -p "$u_home/.local/bin"
 install_cf_tunnel_helper "$u_home/.local/bin/cf-tunnel" || true
 fi
 fi
 ensure_dev_site_helper

 echo "cloudflared ${cf_ver:-Terpasang} siap."
 if [ ! -f "/etc/ssl/local-certs/ca/rootCA.pem" ]; then
 echo "Peringatan: CA mkcert belum ada. Jalankan 'sudo $0 --ssl' agar origin HTTPS tervalidasi tunnel."
 fi
 if [ -z "$(wdev_profile_list)" ]; then
 echo "Belum ada akun Cloudflare. Tambah dengan:"
 echo " sudo cf-tunnel account add <profil>"
 echo " sudo dev-site expose <domain> <public-host> --profile <profil>"
 fi
}

step_php_full_stack() {
  check_root
  local own_lock=0
  [ "${LOCK_HELD_PID:-}" != "$$" ] && own_lock=1
  acquire_lock
  prompt_php_options
  echo "============================================================"
  echo "       MEMASANG FULL PHP WEB STACK & LOCAL SSL              "
  echo "============================================================"
  local rc=0
  step_nginx && step_php && step_composer && step_local_ssl || rc=$?
  [ "$own_lock" -eq 1 ] && release_lock
  restore_tty_state
  if [ "$rc" -ne 0 ]; then
    return "$rc"
  fi
  echo -e "\nFull PHP Web Stack berhasil dipasang dan dikonfigurasi!"
}

# --- ALUR INSTALASI OH MY ZSH & PLUGINS/TEMA POPULER ---
ZSH_THEME_OPTION=""

prompt_zsh_theme() {
  if [ -n "$ZSH_THEME_OPTION" ]; then
    return 0
  fi

  local TTY_OUT="/dev/stdout"
  local TTY_IN="/dev/stdin"
  if [ -w /dev/tty ] && [ -r /dev/tty ]; then
    TTY_OUT="/dev/tty"
    TTY_IN="/dev/tty"
  elif [ ! -t 0 ]; then
    ZSH_THEME_OPTION="powerlevel10k"
    return 0
  fi

  echo "" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "       PILIH TEMA OH MY ZSH               " > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  echo "1. Powerlevel10k (Sangat Populer, Modern, Cepat & Status Git Lengkap) [Default]" > "$TTY_OUT"
  echo "2. Agnoster (Gaya Powerline Klasik)" > "$TTY_OUT"
  echo "3. Robbyrussell (Minimalis & Ringan)" > "$TTY_OUT"
  echo "==========================================" > "$TTY_OUT"
  printf "%s" "Pilih tema [1-3, default: 1]: " > "$TTY_OUT"
  read -r t_choice < "$TTY_IN"
  case "$t_choice" in
    2) ZSH_THEME_OPTION="agnoster" ;;
    3) ZSH_THEME_OPTION="robbyrussell" ;;
    *) ZSH_THEME_OPTION="powerlevel10k" ;;
  esac
}

step_zsh() {
  check_root
  detect_os
  prompt_zsh_theme

  echo "Menginstal paket Zsh..."
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    local PKGS=()
    command -v zsh >/dev/null 2>&1 || PKGS+=(zsh)
    command -v git >/dev/null 2>&1 || PKGS+=(git-core)
    command -v curl >/dev/null 2>&1 || PKGS+=(curl)
    command -v fc-cache >/dev/null 2>&1 || PKGS+=(fontconfig)
    if [ ${#PKGS[@]} -gt 0 ]; then
      echo "Mengunduh paket sistem: ${PKGS[*]}..."
      dnf -y install --setopt=install_weak_deps=False "${PKGS[@]}"
    fi
    dnf -y install --setopt=install_weak_deps=False powerline-fonts fira-code-fonts 2>/dev/null || true
  else
    export DEBIAN_FRONTEND=noninteractive
    local PKGS=()
    command -v zsh >/dev/null 2>&1 || PKGS+=(zsh)
    command -v git >/dev/null 2>&1 || PKGS+=(git)
    command -v curl >/dev/null 2>&1 || PKGS+=(curl)
    command -v fc-cache >/dev/null 2>&1 || PKGS+=(fontconfig)
    if [ ${#PKGS[@]} -gt 0 ]; then
      echo "Mengunduh paket sistem: ${PKGS[*]}..."
      apt-get update -y
      apt-get install -y --no-install-recommends "${PKGS[@]}"
    fi
    apt-get install -y --no-install-recommends fonts-powerline fonts-firacode 2>/dev/null || true
  fi

  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  local ZSH_DIR="$TARGET_HOME/.oh-my-zsh"
  local CUSTOM_DIR="$ZSH_DIR/custom"

  clone_git_repo() {
    local repo_url=$1
    local dest_dir=$2
    local label=$3
    if [ -d "$dest_dir/.git" ]; then
      echo "Memperbarui $label..."
      git -c safe.directory="$dest_dir" -C "$dest_dir" pull --rebase 2>/dev/null || true
    else
      echo "Mengunduh $label..."
      rm -rf "$dest_dir"
      git clone --depth=1 --config http.lowSpeedLimit=1000 --config http.lowSpeedTime=30 "$repo_url" "$dest_dir"
    fi
  }

  echo "Memasang Oh My Zsh untuk user: $TARGET_USER..."
  clone_git_repo "https://github.com/ohmyzsh/ohmyzsh.git" "$ZSH_DIR" "Oh My Zsh core"

  # 1. Plugin Populer
  echo "Memasang plugin populer Oh My Zsh..."
  mkdir -p "$CUSTOM_DIR/plugins" "$CUSTOM_DIR/themes"

  clone_git_repo "https://github.com/zsh-users/zsh-autosuggestions.git" "$CUSTOM_DIR/plugins/zsh-autosuggestions" "Plugin zsh-autosuggestions"
  clone_git_repo "https://github.com/zsh-users/zsh-syntax-highlighting.git" "$CUSTOM_DIR/plugins/zsh-syntax-highlighting" "Plugin zsh-syntax-highlighting"
  clone_git_repo "https://github.com/zsh-users/zsh-completions.git" "$CUSTOM_DIR/plugins/zsh-completions" "Plugin zsh-completions"

  # 2. Tema Populer: Powerlevel10k
  clone_git_repo "https://github.com/romkatv/powerlevel10k.git" "$CUSTOM_DIR/themes/powerlevel10k" "Tema Powerlevel10k"

  # 3. Unduh Font MesloLGS NF (Rekomendasi resmi Powerlevel10k) jika belum ada
  local FONT_DIR="$TARGET_HOME/.local/share/fonts"
  mkdir -p "$FONT_DIR"
  if [ ! -f "$FONT_DIR/MesloLGS NF Regular.ttf" ]; then
    echo "Mengunduh MesloLGS Nerd Font untuk ikon tema terminal..."
    local FONT_BASE_URL="https://raw.githubusercontent.com/romkatv/powerlevel10k-media/master"
    for f_name in "MesloLGS%20NF%20Regular.ttf" "MesloLGS%20NF%20Bold.ttf" "MesloLGS%20NF%20Italic.ttf" "MesloLGS%20NF%20Bold%20Italic.ttf"; do
      local decoded_name
      decoded_name=$(echo "$f_name" | sed 's/%20/ /g')
      if [ ! -f "$FONT_DIR/$decoded_name" ]; then
        curl -fsSL --connect-timeout 10 --max-time 60 "$FONT_BASE_URL/$f_name" -o "$FONT_DIR/$decoded_name" 2>/dev/null || true
      fi
    done
    command -v fc-cache >/dev/null 2>&1 && fc-cache -f "$FONT_DIR" 2>/dev/null || true
  fi

  # 4. Siapkan konfigurasi default Powerlevel10k jika dipilih
  if [ "$ZSH_THEME_OPTION" = "powerlevel10k" ] && [ ! -f "$TARGET_HOME/.p10k.zsh" ]; then
    if [ -f "$CUSTOM_DIR/themes/powerlevel10k/config/p10k-rainbow.zsh" ]; then
      cp "$CUSTOM_DIR/themes/powerlevel10k/config/p10k-rainbow.zsh" "$TARGET_HOME/.p10k.zsh"
    fi
  fi

 # 5. Konfigurasi ~/.zshrc
 local ZSHRC="$TARGET_HOME/.zshrc"
 if [ -f "$ZSHRC" ]; then
 local ZSHRC_BAK
 ZSHRC_BAK="${ZSHRC}.bak.$(date +%Y%m%d_%H%M%S)"
 if cp "$ZSHRC" "$ZSHRC_BAK" 2>/dev/null; then
 echo "Backup ~/.zshrc lama: $ZSHRC_BAK"
 fi
 [ -f "${ZSHRC}.pre-ohmyzsh" ] || cp "$ZSHRC" "${ZSHRC}.pre-ohmyzsh" 2>/dev/null || true
 fi

  echo "Mengonfigurasi ~/.zshrc dengan tema '${ZSH_THEME_OPTION}'..."
  local THEME_CONF="ZSH_THEME=\"powerlevel10k/powerlevel10k\""
  case "$ZSH_THEME_OPTION" in
    agnoster) THEME_CONF="ZSH_THEME=\"agnoster\"" ;;
    robbyrussell) THEME_CONF="ZSH_THEME=\"robbyrussell\"" ;;
    *) THEME_CONF="ZSH_THEME=\"powerlevel10k/powerlevel10k\"" ;;
  esac

  cat <<EOF_ZSHRC > "$ZSHRC"
# --- OH MY ZSH CONFIGURATION ---
export ZSH="\$HOME/.oh-my-zsh"

$([ "$ZSH_THEME_OPTION" = "powerlevel10k" ] && cat <<'EOF_P10K_INSTANT'
# Enable Powerlevel10k instant prompt. Should stay close to the top of ~/.zshrc.
if [[ -r "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh" ]]; then
  source "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh"
fi
EOF_P10K_INSTANT
)

# Tema
$THEME_CONF

# Tambahkan fpath completions sebelum oh-my-zsh dimuat
fpath+=(\$ZSH/custom/plugins/zsh-completions/src)

# Plugin Populer
plugins=(
  git
  sudo
  zsh-autosuggestions
  zsh-syntax-highlighting
  zsh-completions
  colored-man-pages
  docker
  npm
)

source \$ZSH/oh-my-zsh.sh

# --- SINKRONISASI ENVIRONMENT DARI ~/.bashrc & ~/.profile ---
# Muat environment variable, custom PATH, dan alias dari ~/.bashrc secara aman tanpa merusak sintaks Zsh
if [ -f "\$HOME/.bashrc" ]; then
  emulate sh -c 'source "\$HOME/.bashrc"' >/dev/null 2>&1 || true
fi
if [ -f "\$HOME/.profile" ] && [ ! -f "\$HOME/.bashrc" ]; then
  emulate sh -c 'source "\$HOME/.profile"' >/dev/null 2>&1 || true
fi

# --- DEV ENVIRONMENT & TOOLCHAIN PATHS ---
export PATH="\$HOME/.local/bin:\$HOME/bin:/usr/local/bin:/usr/local/cargo/bin:/usr/local/go/bin:/usr/local/flutter/bin:/usr/local/android-sdk/cmdline-tools/latest/bin:\$PATH"

# Muat environment profile dev
for prof in /etc/profile.d/rust.sh /etc/profile.d/golang.sh /etc/profile.d/flutter.sh /etc/profile.d/java.sh; do
  [ -r "\$prof" ] && . "\$prof" >/dev/null 2>&1
done

# Otomatis hapus duplikasi pada PATH (fitur bawaan Zsh)
typeset -U PATH path

# Alias Bermanfaat
alias ll='ls -lah'
alias la='ls -A'
alias l='ls -CF'
alias cls='clear'
alias devsite='dev-site'
alias phpswitch='php-switch'
alias pgswitch='pg-switch'
alias nodeswitch='node-switch'

$([ "$ZSH_THEME_OPTION" = "powerlevel10k" ] && cat <<'EOF_P10K_SRC'
# To customize prompt, run `p10k configure` or edit ~/.p10k.zsh.
[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh
EOF_P10K_SRC
)
EOF_ZSHRC

  # 6. Set default login shell ke Zsh
  local ZSH_BIN
  ZSH_BIN=$(command -v zsh || which zsh 2>/dev/null || echo "/bin/zsh")
  if ! grep -Fxq "$ZSH_BIN" /etc/shells 2>/dev/null; then
    echo "$ZSH_BIN" >> /etc/shells
  fi

  if [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
    echo "Mengubah default shell user $TARGET_USER ke $ZSH_BIN..."
    usermod -s "$ZSH_BIN" "$TARGET_USER" 2>/dev/null || chsh -s "$ZSH_BIN" "$TARGET_USER" 2>/dev/null || true
    chown -R "$TARGET_USER:$TARGET_USER" "$ZSH_DIR" "$TARGET_HOME/.zshrc" 2>/dev/null || true
    [ -d "$FONT_DIR" ] && chown -R "$TARGET_USER:$TARGET_USER" "$FONT_DIR" 2>/dev/null || true
    [ -f "$TARGET_HOME/.p10k.zsh" ] && chown "$TARGET_USER:$TARGET_USER" "$TARGET_HOME/.p10k.zsh" 2>/dev/null || true
  fi

  echo "Oh My Zsh beserta plugin dan tema berhasil dipasang!"
  echo "Tip: Buka terminal baru atau ketik 'zsh' untuk langsung menikmati shell baru."
}


# --- ALUR UPDATE / PEMBARUAN ---
apt_upgrade_installed() {
 # Upgrade hanya paket yang benar-benar terpasang; tidak pernah membatalkan seluruh transaksi.
 local pkg failed=()
 local pkgs_to_update=()
 for pkg in "$@"; do
 if dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q "install ok installed"; then
 pkgs_to_update+=("$pkg")
 fi
 done
 if [ ${#pkgs_to_update[@]} -eq 0 ]; then
 echo "Tidak ada paket APT yang relevan terpasang."
 return 0
 fi
 echo "Memperbarui paket APT: ${pkgs_to_update[*]}"
 for pkg in "${pkgs_to_update[@]}"; do
 if ! apt-get install -y --only-upgrade "$pkg" >>"$LOG_FILE" 2>&1; then
 failed+=("$pkg")
 fi
 done
 if [ ${#failed[@]} -gt 0 ]; then
 echo "Peringatan: gagal memperbarui: ${failed[*]} (lihat $LOG_FILE)"
 return 0
 fi
}

dnf_upgrade_installed() {
 local pkg failed=()
 local pkgs_to_update=()
 for pkg in "$@"; do
 if rpm -q "$pkg" >/dev/null 2>&1; then
 pkgs_to_update+=("$pkg")
 fi
 done
 if [ ${#pkgs_to_update[@]} -eq 0 ]; then
 echo "Tidak ada paket RPM yang relevan terpasang."
 return 0
 fi
 echo "Memperbarui paket RPM: ${pkgs_to_update[*]}"
 # Coba upgrade batch terlebih dahulu untuk efisiensi transaksi DNF
 if ! dnf -y upgrade "${pkgs_to_update[@]}" >>"$LOG_FILE" 2>&1; then
 for pkg in "${pkgs_to_update[@]}"; do
 if ! dnf -y upgrade "$pkg" >>"$LOG_FILE" 2>&1; then
 failed+=("$pkg")
 fi
 done
 fi
 if [ ${#failed[@]} -gt 0 ]; then
 echo "Peringatan: gagal memperbarui: ${failed[*]} (lihat $LOG_FILE)"
 fi
}

step_update_system_pkgs() {
 detect_os
 if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
 echo "Memperbarui repositori DNF (makecache)..."
 dnf -y makecache >>"$LOG_FILE" 2>&1 || true
 local extra_rpms=()
 for p in $(rpm -qa --qf "%{name}\n" "nodejs*" "java*openjdk*" "php*" "postgresql*" "valkey*" "redis*" 2>/dev/null); do
 extra_rpms+=("$p")
 done
 dnf_upgrade_installed nodejs nodejs20 nodejs22 nodejs24 mold java-devel \
 java-17-openjdk-devel java-21-openjdk-devel java-25-openjdk-devel \
 postgresql-server postgresql16-server postgresql17-server postgresql \
 redis valkey valkey-compat-redis \
 nginx php php-cli php-fpm php-common mkcert zsh "${extra_rpms[@]}"
 else
 export DEBIAN_FRONTEND=noninteractive
 apt-get update -y >>"$LOG_FILE" 2>&1 || true
 apt_upgrade_installed nodejs default-jdk openjdk-17-jdk openjdk-21-jdk openjdk-25-jdk \
 postgresql-16 postgresql-17 postgresql-18 postgresql-client-16 postgresql-client-17 postgresql-client-18 \
 redis-server mold nginx php-fpm php8.1-fpm php8.2-fpm php8.3-fpm php8.4-fpm mkcert zsh
 fi

 update_installed_temurin_jdks

 if [ -x "/usr/local/bin/composer" ]; then
 echo "Memperbarui Composer self-update..."
 COMPOSER_ALLOW_SUPERUSER=1 /usr/local/bin/composer self-update --no-interaction 2>/dev/null || true
 fi

 local TARGET_USER="${SUDO_USER:-$USER}"
 local TARGET_HOME
 TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
 [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

 if [ -d "$TARGET_HOME/.oh-my-zsh" ]; then
 echo "Memperbarui Oh My Zsh dan plugin/tema kustom..."
 # Repo milik user target: jalankan sebagai user tersebut agar ownership & safe.directory tetap benar
 run_as_target_user env HOME="$TARGET_HOME" git -C "$TARGET_HOME/.oh-my-zsh" pull --rebase 2>/dev/null || true
 if [ -d "$TARGET_HOME/.oh-my-zsh/custom/plugins" ]; then
 for p_dir in "$TARGET_HOME/.oh-my-zsh/custom/plugins/"*; do
 [ -d "$p_dir/.git" ] || continue
 run_as_target_user env HOME="$TARGET_HOME" git -C "$p_dir" pull --rebase 2>/dev/null || true
 done
 fi
 if [ -d "$TARGET_HOME/.oh-my-zsh/custom/themes" ]; then
 for t_dir in "$TARGET_HOME/.oh-my-zsh/custom/themes/"*; do
 [ -d "$t_dir/.git" ] || continue
 run_as_target_user env HOME="$TARGET_HOME" git -C "$t_dir" pull --rebase 2>/dev/null || true
 done
 fi
 fi
}

step_update_orca() {
  ensure_orca_browser_env
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  local ORCA_UPDATE="$TARGET_HOME/.local/bin/orca-update"

  if [ -x "$ORCA_UPDATE" ]; then
    echo "Menjalankan pembaruan Orca ADE..."
    if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
      sudo -u "$SUDO_USER" bash "$ORCA_UPDATE" < /dev/null || true
    else
      bash "$ORCA_UPDATE" < /dev/null || true
    fi
  else
    echo "Orca IDE belum terpasang atau skrip orca-update tidak ditemukan."
  fi
  ensure_orca_browser_env
}

step_update_flutter() {
 local TARGET_USER="${SUDO_USER:-$USER}"
 local TARGET_HOME
 TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
 [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

 if [ -x "/usr/local/flutter/bin/flutter" ]; then
 echo "Memperbarui Flutter SDK ke versi stable terbaru..."
 if ! git config --system --get-all safe.directory 2>/dev/null | grep -qx "/usr/local/flutter"; then
 git config --system --add safe.directory /usr/local/flutter 2>/dev/null || true
 fi
 run_as_target_user env HOME="$TARGET_HOME" /usr/local/flutter/bin/flutter upgrade --force 2>/dev/null || true
 if [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
 chown -R "$TARGET_USER:$TARGET_USER" /usr/local/flutter 2>/dev/null || true
 fi
 chmod -R u+rwX,go+rX /usr/local/flutter 2>/dev/null || true
 fi
 local ANDROID_DIR="/usr/local/android-sdk"
 local ANDROID_BIN="$ANDROID_DIR/cmdline-tools/latest/bin/android"
 local SDKMGR_BIN="$ANDROID_DIR/cmdline-tools/latest/bin/sdkmanager"

 if [ -x "$ANDROID_BIN" ]; then
 echo "Memperbarui paket Android SDK via Android CLI..."
 timeout 60 "$ANDROID_BIN" sdk update --no-downgrade >/dev/null 2>&1 || true
 elif [ -x "$SDKMGR_BIN" ]; then
 echo "Memperbarui paket Android SDK..."
 timeout 120 "$SDKMGR_BIN" --update >/dev/null 2>&1 || true
 fi

 if [ -d "$ANDROID_DIR" ] && [ -n "$TARGET_USER" ] && id "$TARGET_USER" >/dev/null 2>&1; then
 chown -R "$TARGET_USER:$TARGET_USER" "$ANDROID_DIR" 2>/dev/null || true
 chmod -R u+rwX,go+rX "$ANDROID_DIR" 2>/dev/null || true
 fi
}

step_verify_services() {
  ensure_orca_browser_env
  systemctl restart postgresql 2>/dev/null || true
  local REDIS_SVC
  REDIS_SVC=$(get_redis_service)
  systemctl restart "$REDIS_SVC" 2>/dev/null || systemctl restart redis 2>/dev/null || systemctl restart valkey 2>/dev/null || true
  systemctl restart nginx 2>/dev/null || true
  systemctl restart php-fpm 2>/dev/null || true
 for fpm_svc in $(systemctl list-unit-files 'php*-fpm.service' --no-legend 2>/dev/null | awk '{print $1}'); do
 systemctl restart "$fpm_svc" 2>/dev/null || true
 done
 for cf_unit in $(systemctl list-unit-files 'cloudflared-*.service' --no-legend 2>/dev/null | awk '{print $1}'); do
 systemctl restart "$cf_unit" 2>/dev/null || true
 done
 restart_orca_service 2>/dev/null || true
}

step_update_cloudflare() {
 if command -v cloudflared >/dev/null 2>&1 || [ -x /usr/local/bin/cloudflared ]; then
 step_cloudflare
 else
 echo "cloudflared tidak terpasang; pembaruan Cloudflare dilewati."
 fi
 ensure_cf_tunnel_helper
}

step_update_helpers() {
 echo "Memperbarui helper CLI (dev-site, cf-tunnel, php-switch, pg-switch, node-switch)..."
 ensure_dev_site_helper
 ensure_cf_tunnel_helper
 ensure_pg_switch_helper
 ensure_node_switch_helper
 install_php_switch_helper
 install_node_switch_helper
}

run_update() {
  check_root
 acquire_lock
  log_run_header

  # Load env paths
  [ -f /etc/profile.d/rust.sh ] && . /etc/profile.d/rust.sh
  [ -f /etc/profile.d/golang.sh ] && . /etc/profile.d/golang.sh
  [ -f /etc/profile.d/flutter.sh ] && . /etc/profile.d/flutter.sh
  [ -f /etc/profile.d/java.sh ] && . /etc/profile.d/java.sh
  export PATH="/usr/local/cargo/bin:/usr/local/go/bin:/usr/local/flutter/bin:/usr/local/android-sdk/cmdline-tools/latest/bin:$PATH"

  echo "============================================================"
  echo "      MEMERIKSA & MEMPERBARUI SEMUA PAKET KE TERBARU        "
  echo "============================================================"
  echo "Mencatat versi komponen sebelum pembaruan..."
  declare -A VERSIONS_BEFORE
  collect_installed_versions VERSIONS_BEFORE

  CURRENT_STEP=0
  TOTAL_STEPS=8

  run_step "1/8 Memperbarui repository & paket sistem ($PKG_MGR_NAME)" step_update_system_pkgs
  run_step "2/8 Memperbarui Rust toolchain & Sccache" step_rust
  run_step "3/8 Memeriksa & memperbarui Golang" step_golang
  run_step "4/8 Memperbarui Flutter & Android SDK" step_update_flutter
  run_step "5/8 Memperbarui Orca ADE" step_update_orca
  run_step "6/8 Memperbarui helper CLI (dev-site, cf-tunnel, php-switch, pg-switch, node-switch)" step_update_helpers
  run_step "7/8 Memperbarui cloudflared & konfigurasi Cloudflare" step_update_cloudflare
  run_step "8/8 Memverifikasi layanan PostgreSQL, Redis, Nginx, PHP-FPM, Cloudflare & Orca" step_verify_services

  echo -e "\n\nSemua langkah pembaruan selesai!"

  declare -A VERSIONS_AFTER
  collect_installed_versions VERSIONS_AFTER

  print_version_table VERSIONS_BEFORE VERSIONS_AFTER "RINGKASAN PERBANDINGAN PEMBARUAN VERSI" "Versi Sebelum" "Versi Sesudah" "update"

  release_lock
  restore_tty_state
  echo "Muat environment: source /etc/profile"
}

check_updates() {
  detect_os
  # Load env paths
  [ -f /etc/profile.d/rust.sh ] && . /etc/profile.d/rust.sh
  [ -f /etc/profile.d/golang.sh ] && . /etc/profile.d/golang.sh
  [ -f /etc/profile.d/flutter.sh ] && . /etc/profile.d/flutter.sh
  [ -f /etc/profile.d/java.sh ] && . /etc/profile.d/java.sh
  export PATH="/usr/local/cargo/bin:/usr/local/go/bin:/usr/local/flutter/bin:/usr/local/android-sdk/cmdline-tools/latest/bin:$PATH"

  echo "============================================================"
  echo "       MEMERIKSA KETERSEDIAAN PEMBARUAN VERSI TERBARU       "
  echo "============================================================"
  echo "Memeriksa versi terpasang dan rilis terbaru di repositori..."

  declare -A INSTALLED_MAP
  collect_installed_versions INSTALLED_MAP

  declare -A LATEST_MAP
  collect_latest_versions LATEST_MAP

  print_version_table INSTALLED_MAP LATEST_MAP "STATUS KETERSEDIAAN PEMBARUAN VERSI" "Versi Terpasang" "Versi Terbaru" "check"

  # Cek status pembaruan paket sistem operasi (Fedora / DNF & Debian / APT)
  local c_reset="\e[0m" c_bold="\e[1m" c_green="\e[32m" c_yellow="\e[33m" c_dim="\e[2m"
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    echo -e "${c_bold}--- STATUS PEMBARUAN PAKET SISTEM OPERASI (${OS_NAME} / DNF) ---${c_reset}"
    echo "Memeriksa repositori DNF untuk pembaruan paket sistem..."
    local dnf_out="" dnf_code=0 dnf_count=0
    dnf_out=$(dnf $DNF_RO_OPTS check-update 2>&1) || dnf_code=$?
    if [ "$dnf_code" -eq 100 ]; then
      dnf_count=$(echo "$dnf_out" | awk 'BEGIN {c=0} /^[a-zA-Z0-9_-]+\./ {c++} END {print c}')
      echo -e "${c_yellow}[!] Tersedia ${dnf_count} pembaruan paket sistem di repositori DNF.${c_reset}"
      echo "Beberapa paket yang tersedia pembaruan:"
      echo "$dnf_out" | awk '/^[a-zA-Z0-9_-]+\./ { if (cnt < 6) { printf "   • %-32s -> %s\n", $1, $2; cnt++ } }'
      if [ "$dnf_count" -gt 6 ]; then
        echo -e "   ${c_dim}... dan $((dnf_count - 6)) paket lainnya.${c_reset}"
      fi
    elif [ "$dnf_code" -eq 0 ]; then
      echo -e "${c_green}[✓] Seluruh paket sistem operasi ${OS_NAME} sudah Up-to-date.${c_reset}"
    else
      echo -e "${c_dim}Catatan: Tidak dapat memeriksa pembaruan paket OS (kode keluar: ${dnf_code}).${c_reset}"
    fi
  elif [ "$PKG_MGR" = "apt" ]; then
    echo -e "${c_bold}--- STATUS PEMBARUAN PAKET SISTEM OPERASI (${OS_NAME} / APT) ---${c_reset}"
    local apt_count=0
    apt_count=$(apt-get -s upgrade 2>/dev/null | grep -Po '^[0-9]+(?= upgraded)' || true)
    if [ -n "$apt_count" ] && [ "$apt_count" -gt 0 ]; then
      echo -e "${c_yellow}[!] Tersedia ${apt_count} pembaruan paket sistem APT.${c_reset}"
    else
      echo -e "${c_green}[✓] Seluruh paket sistem operasi ${OS_NAME} sudah Up-to-date.${c_reset}"
    fi
  fi
  echo ""
  echo "Tip: Jalankan 'sudo $0 --update' untuk menerapkan pembaruan yang tersedia."
}

run_installation() {
   check_root
  validate_credentials
  warn_lan_exposure
  acquire_lock
  prompt_node_options
  prompt_java_options
  prompt_android_options
  prompt_pg_options
  prompt_php_options
  prompt_zsh_theme
  log_run_header
  CURRENT_STEP=0
  TOTAL_STEPS=14

  run_step "1/14 Mempersiapkan dependensi sistem" step_base_deps
  run_step "2/14 Menginstal Node.js (${NODE_VER_OPTION:-22})" step_nodejs
  run_step "3/14 Menginstal Rust, Mold, & Sccache" step_rust
  run_step "4/14 Menginstal Golang" step_golang
  run_step "5/14 Menginstal OpenJDK (Java ${JAVA_VER_OPTION:-21})" step_java
  run_step "6/14 Menginstal Android SDK & Command Line Tools" step_android_sdk
  run_step "7/14 Menginstal & Mengonfigurasi Flutter SDK" step_flutter
  run_step "8/14 Menginstal & Mengonfigurasi PostgreSQL (${PG_VER_OPTION:-18})" step_postgres
  run_step "9/14 Menginstal & Mengonfigurasi Redis / Valkey Server" step_redis
  run_step "10/14 Menginstal Nginx Web Server (Mainline Terbaru)" step_nginx
  run_step "11/14 Menginstal PHP & PHP-FPM (${PHP_VER_OPTION:-8.4})" step_php
  run_step "12/14 Menginstal Composer" step_composer
  run_step "13/14 Mengonfigurasi Local SSL (mkcert) & Helper dev-site" step_local_ssl
  run_step "14/14 Menginstal Oh My Zsh, Plugin & Tema" step_zsh

  echo -e "\n\nInstalasi selesai!"
  release_lock
  restore_tty_state
  echo "Muat environment: source /etc/profile"
}

run_single_component() {
  local name=$1
  local func=$2
  check_root
  acquire_lock
  case "$func" in
    step_nodejs)
      prompt_node_options
      ;;
    step_java)
      prompt_java_options
      ;;
    step_android_sdk)
      prompt_android_options
      ;;
    step_postgres)
      validate_credentials
      warn_lan_exposure
      prompt_pg_options
      ;;
    step_php|step_php_full_stack)
      prompt_php_options
      ;;
    step_zsh)
      prompt_zsh_theme
      ;;
  esac
  case "$func" in
    step_redis)
      validate_credentials
      warn_lan_exposure
      ;;
  esac
  log_run_header
  CURRENT_STEP=0
  TOTAL_STEPS=1

  run_step "1/1 Menginstal/Memperbarui $name" "$func"
  release_lock
  restore_tty_state

  echo -e "\n\nProses selesai!"
  echo "Muat environment: source /etc/profile"
}

# --- HELPER SYSTEMD USER & PROSES ORCA ADE ---
run_as_target_user() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_UID
  TARGET_UID=$(id -u "$TARGET_USER" 2>/dev/null || echo "1000")
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  local XDG_DIR="/run/user/$TARGET_UID"

  if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
    if command -v runuser >/dev/null 2>&1; then
      runuser -u "$TARGET_USER" -- env "HOME=$TARGET_HOME" "USER=$TARGET_USER" "XDG_RUNTIME_DIR=$XDG_DIR" "DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_DIR/bus" "$@"
    else
      sudo -u "$TARGET_USER" env "HOME=$TARGET_HOME" "USER=$TARGET_USER" "XDG_RUNTIME_DIR=$XDG_DIR" "DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_DIR/bus" "$@"
    fi
  else
    env "XDG_RUNTIME_DIR=$XDG_DIR" "DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_DIR/bus" "$@"
  fi
}

detect_browser() {
  local candidates=(
    "/usr/bin/chromium"
    "/usr/bin/chromium-browser"
    "/snap/bin/chromium"
    "${ORCA_BROWSER_EXECUTABLE:-}"
    "/usr/bin/google-chrome"
    "/usr/bin/google-chrome-stable"
    "/opt/google/chrome/chrome"
    "/usr/bin/brave-browser"
    "/usr/bin/microsoft-edge-stable"
    "/usr/bin/microsoft-edge"
  )
  for b in "${candidates[@]}"; do
    if [ -n "$b" ] && [ -x "$b" ]; then
      echo "$b"
      return 0
    fi
  done

  for name in chromium chromium-browser google-chrome google-chrome-stable brave-browser; do
    if command -v "$name" >/dev/null 2>&1; then
      command -v "$name"
      return 0
    fi
  done

  return 1
}

detect_chrome_browser() {
  detect_browser "$@"
}

install_chromium() {
  detect_os
  echo "Memasang Chromium browser secara otomatis..."
  if [ "$PKG_MGR" = "dnf" ] || [ "$PKG_MGR" = "yum" ]; then
    dnf -y install chromium 2>/dev/null || true
  elif [ "$PKG_MGR" = "apt" ]; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y 2>/dev/null || true
    apt-get install -y chromium 2>/dev/null || apt-get install -y chromium-browser 2>/dev/null || true
  elif [ "$PKG_MGR" = "pacman" ]; then
    pacman -Sy --noconfirm chromium 2>/dev/null || true
  elif [ "$PKG_MGR" = "zypper" ]; then
    zypper --non-interactive in chromium 2>/dev/null || true
  fi
}

is_orca_running() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  local RUNTIME_FILE="$TARGET_HOME/.config/orca/orca-runtime.json"

  # 1. Cek file runtime resmi Orca (dibuat oleh orcad dan memuat PID aktif)
  if [ -f "$RUNTIME_FILE" ]; then
    local RUNTIME_PID
    RUNTIME_PID=$(jq -r '.pid // empty' "$RUNTIME_FILE" 2>/dev/null || true)
    if [ -n "$RUNTIME_PID" ] && kill -0 "$RUNTIME_PID" 2>/dev/null; then
      return 0
    fi
  fi

  # 2. Cek systemd user service via session aktif atau runuser
  if run_as_target_user systemctl --user is-active --quiet orca 2>/dev/null; then
    return 0
  fi

  # 3. Cek systemd system service
  if systemctl is-active --quiet orca 2>/dev/null; then
    return 0
  fi

  # 4. Cek proses biner Orca (Desktop GUI atau server)
  if pgrep -x "orca-ide" >/dev/null 2>&1 || pgrep -f "/orca-ide/orca-ide|orcad-server|daemon-entry|serve-supervisor|orca.*serve" >/dev/null 2>&1; then
    return 0
  fi

  return 1
}

is_orca_desktop_running() {
  pgrep -x "orca-ide" >/dev/null 2>&1 || pgrep -f "/orca-ide/orca-ide" >/dev/null 2>&1
}

is_orca_headless_running() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  if run_as_target_user systemctl --user is-active --quiet orca 2>/dev/null; then
    return 0
  fi
  if systemctl is-active --quiet orca 2>/dev/null; then
    return 0
  fi
  if pgrep -f "orca-ide.*serve|orcad-server|serve-supervisor" >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

get_orca_configured_mode() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  local MODE_FILE="$TARGET_HOME/.config/orca/.mode"

  if [ -f "$MODE_FILE" ]; then
    local m
    m="$(cat "$MODE_FILE" 2>/dev/null | tr -d '[:space:]')"
    if [ "$m" = "desktop" ] || [ "$m" = "headless" ]; then
      echo "$m"
      return 0
    fi
  fi

  detect_desktop_environment || true
  if [ "$HAS_DE" -eq 1 ]; then
    echo "desktop"
  else
    echo "headless"
  fi
}

is_orca_installed() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  [ -x "$TARGET_HOME/.local/bin/orca" ] || \
  [ -x "$TARGET_HOME/.local/bin/orca-ide" ] || \
  [ -x "$TARGET_HOME/.config/orca/cli/bin/orca" ] || \
  [ -x "$TARGET_HOME/.config/orca/cli/bin/orca-ide" ] || \
  [ -x "/usr/local/bin/orca" ] || \
  [ -x "/usr/local/bin/orca-ide" ] || \
  [ -x "$TARGET_HOME/.local/lib/orca-ide/resources/bin/orca-ide" ] || \
  command -v orca >/dev/null 2>&1 || \
  command -v orca-ide >/dev/null 2>&1
}

get_orca_bin() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  for b in \
    "$TARGET_HOME/.local/bin/orca-ide" \
    "$TARGET_HOME/.local/bin/orca" \
    "$TARGET_HOME/.config/orca/cli/bin/orca-ide" \
    "$TARGET_HOME/.config/orca/cli/bin/orca" \
    "/usr/local/bin/orca-ide" \
    "/usr/local/bin/orca" \
    "$TARGET_HOME/.local/lib/orca-ide/resources/bin/orca-ide"; do
    if [ -x "$b" ]; then
      echo "$b"
      return 0
    fi
  done

  if command -v orca-ide >/dev/null 2>&1; then
    command -v orca-ide
    return 0
  fi
  if command -v orca >/dev/null 2>&1; then
    command -v orca
    return 0
  fi
  return 1
}

get_orca_status_label() {
  if is_orca_installed; then
    local cur_m
    cur_m="$(get_orca_configured_mode)"
    echo "Terpasang: $([ "$cur_m" = "desktop" ] && echo "Desktop GUI" || echo "Headless Server")"
  else
    echo "Belum Terpasang"
  fi
}

ensure_orca_browser_env() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  local BROWSER_BIN
  BROWSER_BIN=$(detect_browser 2>/dev/null || echo "")

  if [ -z "$BROWSER_BIN" ]; then
    echo "Chromium belum ditemukan. Memasang Chromium secara otomatis..."
    install_chromium
    BROWSER_BIN=$(detect_browser 2>/dev/null || echo "")
  fi

  if [ -n "$BROWSER_BIN" ]; then
    export ORCA_BROWSER_EXECUTABLE="$BROWSER_BIN"

    local RC_BROWSER_LIST="/usr/bin/chromium /usr/bin/chromium-browser /snap/bin/chromium /usr/bin/google-chrome /usr/bin/google-chrome-stable /opt/google/chrome/chrome /usr/bin/brave-browser /usr/bin/microsoft-edge-stable"

    # 1. Konfigurasi /etc/profile.d/orca-browser.sh
    if [ "$EUID" -eq 0 ]; then
      cat << 'EOF_ORCA_PROF' > /etc/profile.d/orca-browser.sh
# /etc/profile.d/orca-browser.sh - Set ORCA_BROWSER_EXECUTABLE for Orca ADE
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
  for _b in /usr/bin/chromium /usr/bin/chromium-browser /snap/bin/chromium /usr/bin/google-chrome /usr/bin/google-chrome-stable /opt/google/chrome/chrome /usr/bin/brave-browser /usr/bin/microsoft-edge-stable; do
    if [ -x "$_b" ]; then
      export ORCA_BROWSER_EXECUTABLE="$_b"
      break
    fi
  done
  unset _b
fi
EOF_ORCA_PROF
      chmod 644 /etc/profile.d/orca-browser.sh 2>/dev/null || true
    elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
      sudo tee /etc/profile.d/orca-browser.sh >/dev/null << 'EOF_ORCA_PROF'
# /etc/profile.d/orca-browser.sh - Set ORCA_BROWSER_EXECUTABLE for Orca ADE
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
  for _b in /usr/bin/chromium /usr/bin/chromium-browser /snap/bin/chromium /usr/bin/google-chrome /usr/bin/google-chrome-stable /opt/google/chrome/chrome /usr/bin/brave-browser /usr/bin/microsoft-edge-stable; do
    if [ -x "$_b" ]; then
      export ORCA_BROWSER_EXECUTABLE="$_b"
      break
    fi
  done
  unset _b
fi
EOF_ORCA_PROF
      sudo chmod 644 /etc/profile.d/orca-browser.sh 2>/dev/null || true
    fi

    # 2. Tambahkan ke user .bashrc dan .zshrc
    for rc in "$TARGET_HOME/.bashrc" "$TARGET_HOME/.zshrc"; do
      if [ -f "$rc" ]; then
        if grep -q "ORCA_BROWSER_EXECUTABLE" "$rc"; then
          sed -i "s#for _b in .*; do#for _b in $RC_BROWSER_LIST; do#" "$rc" 2>/dev/null || true
        else
          cat << 'EOF_ORCA_RC' >> "$rc"

# Orca ADE Browser Automation Backend
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
  for _b in /usr/bin/chromium /usr/bin/chromium-browser /snap/bin/chromium /usr/bin/google-chrome /usr/bin/google-chrome-stable /opt/google/chrome/chrome /usr/bin/brave-browser /usr/bin/microsoft-edge-stable; do
    if [ -x "$_b" ]; then
      export ORCA_BROWSER_EXECUTABLE="$_b"
      break
    fi
  done
  unset _b
fi
EOF_ORCA_RC
          [ -n "$TARGET_USER" ] && chown "$TARGET_USER:$TARGET_USER" "$rc" 2>/dev/null || true
        fi
      fi
    done

    # 3. Propagasi ke systemd user environment
    run_as_target_user systemctl --user set-environment ORCA_BROWSER_EXECUTABLE="$BROWSER_BIN" 2>/dev/null || true
    if command -v systemctl >/dev/null 2>&1; then
      systemctl --user set-environment ORCA_BROWSER_EXECUTABLE="$BROWSER_BIN" 2>/dev/null || true
    fi

    # 4. Pastikan unit file systemd user/system memiliki ORCA_BROWSER_EXECUTABLE
    local USER_UNIT="$TARGET_HOME/.config/systemd/user/orca.service"
    if [ -f "$USER_UNIT" ]; then
      if grep -q "ORCA_BROWSER_EXECUTABLE" "$USER_UNIT"; then
        sed -i "s#ORCA_BROWSER_EXECUTABLE=[^\"[:space:]]*#ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN#" "$USER_UNIT" 2>/dev/null || true
      else
        sed -i "/\\[Service\\]/a Environment=\"ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN\"" "$USER_UNIT" 2>/dev/null || true
      fi
      run_as_target_user systemctl --user daemon-reload 2>/dev/null || true
    fi

    local SYS_UNIT="/etc/systemd/system/orca.service"
    if [ -f "$SYS_UNIT" ]; then
      if grep -q "ORCA_BROWSER_EXECUTABLE" "$SYS_UNIT"; then
        if [ "$EUID" -eq 0 ]; then
          sed -i "s#ORCA_BROWSER_EXECUTABLE=[^\"[:space:]]*#ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN#" "$SYS_UNIT" 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
          sudo sed -i "s#ORCA_BROWSER_EXECUTABLE=[^\"[:space:]]*#ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN#" "$SYS_UNIT" 2>/dev/null || true
        fi
      else
        if [ "$EUID" -eq 0 ]; then
          sed -i "/\\[Service\\]/a Environment=\"ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN\"" "$SYS_UNIT" 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
          sudo sed -i "/\\[Service\\]/a Environment=\"ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN\"" "$SYS_UNIT" 2>/dev/null || true
        fi
      fi
      systemctl daemon-reload 2>/dev/null || true
    fi

    # 5. Pastikan wrapper orca di ~/.local/bin/orca memuat auto-detect
    local WRAPPER="$TARGET_HOME/.local/bin/orca"
    if [ -f "$WRAPPER" ]; then
      if grep -q "ORCA_BROWSER_EXECUTABLE" "$WRAPPER"; then
        sed -i "s#for _b in .*; do#for _b in $RC_BROWSER_LIST; do#" "$WRAPPER" 2>/dev/null || true
      fi
    fi
  fi
}

start_orca_service() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  ensure_orca_browser_env

  # 1. Coba start via user systemd jika servicenya ada
  if [ -f "$TARGET_HOME/.config/systemd/user/orca.service" ]; then
    run_as_target_user systemctl --user daemon-reload 2>/dev/null || true
    run_as_target_user systemctl --user enable orca.service 2>/dev/null || true
    if run_as_target_user systemctl --user start orca.service 2>/dev/null; then
      echo "Layanan systemd user orca.service berhasil diaktifkan."
      return 0
    fi
  fi

  # 2. Coba start via system systemd jika servicenya ada
  if [ -f "/etc/systemd/system/orca.service" ]; then
    if [ "$EUID" -eq 0 ]; then
      systemctl daemon-reload 2>/dev/null || true
      systemctl enable orca.service 2>/dev/null || true
      if systemctl start orca.service 2>/dev/null; then
        echo "Layanan systemd orca.service berhasil diaktifkan."
        return 0
      fi
    elif command -v sudo >/dev/null 2>&1; then
      sudo systemctl daemon-reload 2>/dev/null || true
      sudo systemctl enable orca.service 2>/dev/null || true
      if sudo systemctl start orca.service 2>/dev/null; then
        echo "Layanan systemd orca.service berhasil diaktifkan."
        return 0
      fi
    fi
  fi

  # 3. Fallback background (nohup)
  local ORCA_BIN
  ORCA_BIN="$(get_orca_bin || true)"
  if [ -n "$ORCA_BIN" ] && [ -x "$ORCA_BIN" ]; then
    local BROWSER_BIN
    BROWSER_BIN=$(detect_chrome_browser 2>/dev/null || echo "")
    [ -n "$BROWSER_BIN" ] && export ORCA_BROWSER_EXECUTABLE="$BROWSER_BIN"
    export ORCA_ALLOW_INSECURE_WEB_PAIRING=1
    export HOME="$TARGET_HOME"
    export USER="$TARGET_USER"
    export ORCA_USER_DATA_PATH="$TARGET_HOME/.config/orca"
    mkdir -p "$TARGET_HOME/.config/orca" 2>/dev/null || true

    local AGENT_PATH="$TARGET_HOME/.local/bin:$TARGET_HOME/.opencode/bin:/usr/local/bin:/usr/bin:/bin"
    [ -d "/usr/local/go/bin" ] && AGENT_PATH="/usr/local/go/bin:$AGENT_PATH"
    [ -d "/usr/local/cargo/bin" ] && AGENT_PATH="/usr/local/cargo/bin:$AGENT_PATH"

    if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
      run_as_target_user env \
        "HOME=$TARGET_HOME" \
        "USER=$TARGET_USER" \
        "PATH=$AGENT_PATH:$PATH" \
        "ORCA_USER_DATA_PATH=$TARGET_HOME/.config/orca" \
        "ORCA_ALLOW_INSECURE_WEB_PAIRING=1" \
        ${BROWSER_BIN:+"ORCA_BROWSER_EXECUTABLE=$BROWSER_BIN"} \
        nohup "$ORCA_BIN" serve --supervise --force < /dev/null > "$TARGET_HOME/.config/orca/orca.log" 2>&1 9>&- &
    else
      PATH="$AGENT_PATH:$PATH" nohup "$ORCA_BIN" serve --supervise --force < /dev/null > "$TARGET_HOME/.config/orca/orca.log" 2>&1 9>&- &
    fi
    echo "Layanan Orca server berhasil dijalankan di background."
    return 0
  fi

  echo "Skrip eksekusi Orca belum terpasang."
  return 1
}

restart_orca_service() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  ensure_orca_browser_env

  # 1. Coba restart via user systemd jika servicenya ada
  if [ -f "$TARGET_HOME/.config/systemd/user/orca.service" ] || run_as_target_user systemctl --user is-active --quiet orca 2>/dev/null; then
    run_as_target_user systemctl --user daemon-reload 2>/dev/null || true
    if run_as_target_user systemctl --user restart orca 2>/dev/null; then
      echo "Layanan systemd user orca.service berhasil direstart."
      return 0
    fi
  fi

  # 2. Coba restart via system systemd
  if systemctl is-active --quiet orca 2>/dev/null || [ -f "/etc/systemd/system/orca.service" ]; then
    if [ "$EUID" -eq 0 ]; then
      systemctl daemon-reload 2>/dev/null || true
      if systemctl restart orca 2>/dev/null; then
        echo "Layanan systemd orca.service berhasil direstart."
        return 0
      fi
    elif command -v sudo >/dev/null 2>&1; then
      sudo systemctl daemon-reload 2>/dev/null || true
      if sudo systemctl restart orca 2>/dev/null; then
        echo "Layanan systemd orca.service berhasil direstart."
        return 0
      fi
    fi
  fi

  # 3. Jika berjalan sebagai proses background biasa (nohup)
  if is_orca_running; then
    pkill -f "orcad-server|daemon-entry|serve-supervisor|orca.*serve" 2>/dev/null || true
    sleep 1
    start_orca_service
    return 0
  fi

  # Fallback: jika belum berjalan, coba jalankan
  start_orca_service
}

stop_orca_service() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  # 1. Coba stop via user systemd
  if [ -f "$TARGET_HOME/.config/systemd/user/orca.service" ] || run_as_target_user systemctl --user is-active --quiet orca 2>/dev/null; then
    run_as_target_user systemctl --user stop orca 2>/dev/null || true
  fi

  # 2. Coba stop via system systemd
  if systemctl is-active --quiet orca 2>/dev/null || [ -f "/etc/systemd/system/orca.service" ]; then
    if [ "$EUID" -eq 0 ]; then
      systemctl stop orca 2>/dev/null || true
    elif command -v sudo >/dev/null 2>&1; then
      sudo systemctl stop orca 2>/dev/null || true
    fi
  fi

  # 3. Hentikan semua proses Orca (Desktop GUI maupun server)
  pkill -u "$TARGET_USER" -x "orca-ide" 2>/dev/null || true
  pkill -u "$TARGET_USER" -f "/orca-ide/orca-ide|orcad-server|daemon-entry|serve-supervisor|orca.*serve" 2>/dev/null || true
  sleep 1
}

# --- MENU CEK INFORMASI & DETAIL STATUS ---
show_info() {
  # Load env paths
  [ -f /etc/profile.d/rust.sh ] && . /etc/profile.d/rust.sh
  [ -f /etc/profile.d/golang.sh ] && . /etc/profile.d/golang.sh
  [ -f /etc/profile.d/flutter.sh ] && . /etc/profile.d/flutter.sh
  [ -f /etc/profile.d/java.sh ] && . /etc/profile.d/java.sh
  export PATH="/usr/local/cargo/bin:/usr/local/go/bin:/usr/local/flutter/bin:/usr/local/android-sdk/cmdline-tools/latest/bin:$PATH"

  # Deteksi user non-root target (jika dijalankan via sudo)
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  echo "============================================================"
  echo "               INFORMASI & STATUS INSTALASI                 "
  echo "============================================================"
  detect_os
  echo "  OS / Distribusi : $OS_NAME ($OS_ID ${OS_VERSION_ID:-})"
  echo "  Package Manager : $PKG_MGR_NAME ($PKG_MGR)"
  echo "------------------------------------------------------------"

  # 1. Node.js & NPM
  if command -v node >/dev/null 2>&1; then
    printf "%-18s: %s (npm: %s)\n" "Node.js" "$(node -v)" "$(npm -v 2>/dev/null || echo '-')"
    if [ -x "/usr/local/bin/node-switch" ]; then
      echo "  Multi-Versi Switch: Terpasang (/usr/local/bin/node-switch, opsi: 20, 22, 24)"
    fi
  else
    printf "%-18s: Belum terinstal\n" "Node.js"
  fi

  # 2. Rust & Cargo
  if command -v rustc >/dev/null 2>&1; then
    printf "%-18s: %s\n" "Rustc" "$(rustc --version)"
    printf "%-18s: %s\n" "Cargo" "$(cargo --version)"
  else
    printf "%-18s: Belum terinstal\n" "Rust"
  fi

  # 3. Mold & Sccache
  if command -v mold >/dev/null 2>&1; then
    printf "%-18s: %s\n" "Mold Linker" "$(mold --version 2>/dev/null | head -n1)"
  else
    printf "%-18s: Belum terinstal\n" "Mold Linker"
  fi

  if command -v sccache >/dev/null 2>&1; then
    printf "%-18s: %s\n" "Sccache" "$(sccache --version 2>/dev/null)"
  else
    printf "%-18s: Belum terinstal\n" "Sccache"
  fi

  # 4. Golang
  if command -v go >/dev/null 2>&1; then
    printf "%-18s: %s\n" "Golang" "$(go version 2>/dev/null)"
  else
    printf "%-18s: Belum terinstal\n" "Golang"
  fi

  # 5. Java
  if command -v java >/dev/null 2>&1; then
    printf "%-18s: %s\n" "Java" "$(java -version 2>&1 | head -n1)"
  else
    printf "%-18s: Belum terinstal\n" "Java"
  fi

  # 6. Flutter & Android SDK
  echo "------------------------------------------------------------"
  echo "STATUS FLUTTER & ANDROID TOOLCHAIN:"
  local FLUTTER_BIN="/usr/local/flutter/bin/flutter"
  if [ -x "$FLUTTER_BIN" ] || command -v flutter >/dev/null 2>&1; then
    [ ! -x "$FLUTTER_BIN" ] && FLUTTER_BIN="$(command -v flutter)"
    local FLUTTER_VER="Terpasang"
    if [ -d "/usr/local/flutter/.git" ]; then
      FLUTTER_VER="Flutter $(git -C /usr/local/flutter describe --tags 2>/dev/null || echo 'Terpasang')"
    fi
    local DART_VER
    DART_VER=$(cat /usr/local/flutter/bin/cache/dart-sdk/version 2>/dev/null || echo "-")
    [ "$DART_VER" != "-" ] && DART_VER="Dart $DART_VER"
    printf "  %-18s: %s\n" "Flutter SDK" "$FLUTTER_VER"
    printf "  %-18s: %s\n" "Dart SDK" "$DART_VER"
  else
    printf "  %-18s: Belum terinstal\n" "Flutter SDK"
  fi

  if [ -d "/usr/local/android-sdk/cmdline-tools/latest" ]; then
    printf "  %-18s: %s\n" "Android SDK Path" "/usr/local/android-sdk"
    local CMDLINE_REV
    CMDLINE_REV=$(grep -E "^Pkg.Revision=" /usr/local/android-sdk/cmdline-tools/latest/source.properties 2>/dev/null | cut -d= -f2 || true)
    [ -n "$CMDLINE_REV" ] && printf "  %-18s: Version %s (Latest)\n" "Command-Line Tools" "$CMDLINE_REV"

    local PLATS
    PLATS=$(ls -1 /usr/local/android-sdk/platforms 2>/dev/null | sort -V | paste -sd, - | sed 's/,/, /g')
    [ -n "$PLATS" ] && printf "  %-18s: %s\n" "Platforms (API)" "$PLATS"

    local BTOOLS
    BTOOLS=$(ls -1 /usr/local/android-sdk/build-tools 2>/dev/null | sort -V | paste -sd, - | sed 's/,/, /g')
    [ -n "$BTOOLS" ] && printf "  %-18s: %s\n" "Build Tools" "$BTOOLS"

    local ADB_BIN="/usr/local/android-sdk/platform-tools/adb"
    [ ! -x "$ADB_BIN" ] && ADB_BIN="$(command -v adb 2>/dev/null || true)"
    if [ -x "$ADB_BIN" ]; then
      printf "  %-18s: %s\n" "Platform Tools" "$("$ADB_BIN" --version 2>/dev/null | head -n1)"
      local ADB_DEVS
      ADB_DEVS=$("$ADB_BIN" devices 2>/dev/null | tail -n +2 | sed '/^$/d' || true)
      if [ -n "$ADB_DEVS" ]; then
        echo "  ADB Devices       :"
        while IFS= read -r dev_line; do
          [ -n "$dev_line" ] && echo "    * $dev_line"
        done <<< "$ADB_DEVS"
      else
        printf "  %-18s: Tidak ada perangkat terhubung (adb devices kosong)\n" "ADB Devices"
      fi
    fi
  else
    printf "  %-18s: Belum terinstal\n" "Android SDK"
  fi

  echo "------------------------------------------------------------"
  echo "STATUS POSTGRESQL:"
  local PG_VER
  PG_VER=$(command -v psql >/dev/null 2>&1 && psql --version 2>/dev/null | awk '{print $3}' || echo "-")

  if systemctl is-active --quiet postgresql 2>/dev/null || systemctl is-active --quiet "postgresql@*" 2>/dev/null; then
    echo "  Status Service    : Running (Active) [PostgreSQL $PG_VER]"
  else
    echo "  Status Service    : Inactive / Not Installed"
  fi

  if [ -x "/usr/local/bin/pg-switch" ]; then
    echo "  Multi-Versi Switch: Terpasang (/usr/local/bin/pg-switch, opsi: 16, 17, 18)"
  fi

  if command -v ss >/dev/null 2>&1; then
    local PG_SOCK
    PG_SOCK=$(ss -tulpn 2>/dev/null | grep ":5432 " || true)
    if [ -n "$PG_SOCK" ]; then
      if echo "$PG_SOCK" | grep -q -E '(0\.0\.0\.0:5432|\*:5432|\[::\]:5432)'; then
        echo "  Port Listening    : 5432 (Terbuka untuk LAN & Netbird)"
      else
        echo "  Port Listening    : 5432 (Lokal 127.0.0.1 saja)"
      fi
    else
      echo "  Port Listening    : Port 5432 tidak aktif"
    fi
  fi

  # Test auth koneksi lokal
  if command -v psql >/dev/null 2>&1; then
    if PGPASSWORD="$PG_SUPERUSER_PASSWORD" psql -U postgres -h 127.0.0.1 -c '\q' >/dev/null 2>&1; then
      echo "  Autentikasi       : OK (user: postgres)"
    else
      echo "  Autentikasi       : Gagal / Belum terkonfigurasi"
    fi
  fi

  echo "------------------------------------------------------------"
  echo "STATUS REDIS / VALKEY SERVER:"

  if is_redis_running; then
    echo "  Status Service    : Running (Active) [$(get_redis_service)]"
  else
    echo "  Status Service    : Inactive / Not Installed"
  fi

  if command -v ss >/dev/null 2>&1; then
    local REDIS_SOCK
    REDIS_SOCK=$(ss -tulpn 2>/dev/null | grep ":6379 " || true)
    if [ -n "$REDIS_SOCK" ]; then
      if echo "$REDIS_SOCK" | grep -q -E '(0\.0\.0\.0:6379|\*:6379|\[::\]:6379)'; then
        echo "  Port Listening    : 6379 (Terbuka untuk LAN & Netbird)"
      else
        echo "  Port Listening    : 6379 (Lokal 127.0.0.1 saja)"
      fi
    else
      echo "  Port Listening    : Port 6379 tidak aktif"
    fi
  fi

  # Test auth koneksi lokal
  local REDIS_CLI_BIN=""
  if command -v redis-cli >/dev/null 2>&1; then
    REDIS_CLI_BIN="redis-cli"
  elif command -v valkey-cli >/dev/null 2>&1; then
    REDIS_CLI_BIN="valkey-cli"
  fi

  if [ -n "$REDIS_CLI_BIN" ]; then
    local REDIS_PING
    REDIS_PING=$(REDISCLI_AUTH="$REDIS_PASSWORD" "$REDIS_CLI_BIN" ping 2>/dev/null || true)
    if [ "$REDIS_PING" = "PONG" ]; then
      echo "  Autentikasi       : OK (requirepass aktif)"
    elif "$REDIS_CLI_BIN" ping 2>/dev/null | grep -q "PONG"; then
      echo "  Autentikasi       : OK (Tanpa Password)"
    else
      echo "  Autentikasi       : Gagal / Belum terkonfigurasi"
    fi
  fi

  echo "------------------------------------------------------------"
  echo "STATUS PHP & WEB SERVER (NGINX + LOCAL SSL):"

 # Nginx
 local NGINX_BIN=""
 if command -v nginx >/dev/null 2>&1; then
 NGINX_BIN=$(command -v nginx)
 elif [ -x /usr/sbin/nginx ]; then
 NGINX_BIN="/usr/sbin/nginx"
 fi
 if [ -n "$NGINX_BIN" ]; then
 local NGINX_VER
 NGINX_VER=$("$NGINX_BIN" -v 2>&1 | awk -F/ '{print $2}')
 local NGINX_STATUS="Inactive"
 systemctl is-active --quiet nginx 2>/dev/null && NGINX_STATUS="Running (Active)"
 printf " %-18s: %s (Status: %s)\n" "Nginx" "${NGINX_VER:-Terpasang}" "$NGINX_STATUS"
 else
 printf " %-18s: Belum terinstal\n" "Nginx"
  fi

  # PHP & Socket
  if command -v php >/dev/null 2>&1; then
    local PHP_VER
    PHP_VER=$(php -v 2>/dev/null | head -n1 | awk '{print $2}')
    local FPM_STATUS="Inactive"
    if systemctl is-active --quiet php-fpm 2>/dev/null || systemctl is-active --quiet 'php*-fpm.service' 2>/dev/null; then
      FPM_STATUS="Running (Active)"
    fi
    local FPM_SOCK
    FPM_SOCK=$(get_php_fpm_socket 2>/dev/null || echo "-")
    printf "  %-18s: %s (FPM: %s)\n" "PHP CLI" "$PHP_VER" "$FPM_STATUS"
    printf "  %-18s: %s\n" "PHP-FPM Socket" "$FPM_SOCK"
  else
    printf "  %-18s: Belum terinstal\n" "PHP"
  fi

  # Composer
  if command -v composer >/dev/null 2>&1; then
    local COMP_VER
    COMP_VER=$(COMPOSER_ALLOW_SUPERUSER=1 composer --version --no-interaction 2>/dev/null | head -n1 | awk '{print $3}')
    printf "  %-18s: %s\n" "Composer" "${COMP_VER:-Terpasang}"
  else
    printf "  %-18s: Belum terinstal\n" "Composer"
  fi

  # Local SSL (mkcert)
  if command -v mkcert >/dev/null 2>&1; then
    local CA_ROOT
    [ -d /etc/ssl/local-certs/ca ] && export CAROOT="/etc/ssl/local-certs/ca"
    CA_ROOT=$(mkcert -CAROOT 2>/dev/null || echo "-")
    printf "  %-18s: Terpasang (CA Root: %s)\n" "Local SSL (mkcert)" "$CA_ROOT"
  else
    printf "  %-18s: Belum terinstal\n" "Local SSL (mkcert)"
  fi

  # Dev-Site Virtual Hosts
  if [ -x "/usr/local/bin/dev-site" ]; then
    local SITE_COUNT=0
    if [ -d "/etc/nginx/sites-available" ]; then
      SITE_COUNT=$(find /etc/nginx/sites-available -type f -name "*.conf" ! -name "*.custom.conf" 2>/dev/null | wc -l)
    fi
 printf " %-18s: Terpasang (/usr/local/bin/dev-site, %d vhosts)\n" "Dev-Site Helper" "$SITE_COUNT"
 fi

 echo "------------------------------------------------------------"
 echo "STATUS CLOUDFLARE TUNNEL:"
 local CF_BIN=""
 if command -v cloudflared >/dev/null 2>&1; then
 CF_BIN=$(command -v cloudflared)
 elif [ -x /usr/local/bin/cloudflared ]; then
 CF_BIN=/usr/local/bin/cloudflared
 fi
 if [ -n "$CF_BIN" ]; then
 printf " %-18s: %s\n" "cloudflared" "$("$CF_BIN" --version 2>/dev/null | head -n1)"
 else
 printf " %-18s: Belum terinstal\n" "cloudflared"
 fi
 if [ -x /usr/local/bin/cf-tunnel ]; then
 if [ "$EUID" -eq 0 ]; then
 /usr/local/bin/cf-tunnel status --all 2>/dev/null || true
 else
 printf " %-18s: Terpasang (jalankan dengan sudo untuk detail profil)\n" "cf-tunnel"
 fi
 else
 printf " %-18s: Belum terinstal (sudo $0 --cloudflare)\n" "cf-tunnel"
 fi

 echo "------------------------------------------------------------"
 echo "STATUS SHELL & OH MY ZSH:"
  if command -v zsh >/dev/null 2>&1; then
    local ZSH_VER
    ZSH_VER=$(zsh --version 2>/dev/null | head -n1)
    local DEF_SHELL
    DEF_SHELL=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f7 || echo "-")
    printf "  %-18s: %s (Shell Login: %s)\n" "Zsh" "$ZSH_VER" "$DEF_SHELL"
  else
    printf "  %-18s: Belum terinstal\n" "Zsh"
  fi

  if [ -d "$TARGET_HOME/.oh-my-zsh" ]; then
    local OMZ_THEME
    OMZ_THEME=$(grep -E '^[[:space:]]*ZSH_THEME=' "$TARGET_HOME/.zshrc" 2>/dev/null | cut -d'"' -f2 || echo "-")
    printf "  %-18s: Terpasang (Tema: %s)\n" "Oh My Zsh" "$OMZ_THEME"
    local PLUG_COUNT=0
    if [ -d "$TARGET_HOME/.oh-my-zsh/custom/plugins" ]; then
      PLUG_COUNT=$(find "$TARGET_HOME/.oh-my-zsh/custom/plugins" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l)
    fi
    printf "  %-18s: %d kustom plugin (autosuggestions, syntax-highlighting, completions)\n" "Plugins" "$PLUG_COUNT"
  else
    printf "  %-18s: Belum terinstal\n" "Oh My Zsh"
  fi

  echo "------------------------------------------------------------"
  detect_desktop_environment || true
  local cur_mode
  cur_mode="$(get_orca_configured_mode)"

  if [ "$cur_mode" = "desktop" ]; then
    echo "STATUS ORCA ADE (DESKTOP GUI):"
    if [ "$HAS_DE" -eq 1 ]; then
      printf "  %-18s: Terdeteksi (%s)\n" "Desktop Env (DE)" "$DETECTED_DE"
    else
      printf "  %-18s: Tidak terdeteksi (Headless)\n" "Desktop Env (DE)"
    fi
    printf "  %-18s: %s\n" "Mode Terpilih" "Desktop GUI (Hanya versi Desktop aktif)"
    local DESKTOP_FILE="$TARGET_HOME/.local/share/applications/orca-ide.desktop"
    [ ! -f "$DESKTOP_FILE" ] && DESKTOP_FILE="/usr/share/applications/orca-ide.desktop"
    if [ -f "$DESKTOP_FILE" ]; then
      printf "  %-18s: Terpasang (%s)\n" "Orca Desktop GUI" "$DESKTOP_FILE"
    else
      printf "  %-18s: Belum terpasang\n" "Orca Desktop GUI"
    fi
  else
    echo "STATUS ORCA ADE (HEADLESS SERVER):"
    if [ "$HAS_DE" -eq 1 ]; then
      printf "  %-18s: Terdeteksi (%s)\n" "Desktop Env (DE)" "$DETECTED_DE"
    else
      printf "  %-18s: Tidak terdeteksi (Headless)\n" "Desktop Env (DE)"
    fi
    printf "  %-18s: %s\n" "Mode Terpilih" "Headless Server (Otomatis aktif di background saat boot)"
  fi
  local ORCA_BIN="$TARGET_HOME/.local/bin/orca"
  local REAL_ORCA="$TARGET_HOME/.local/lib/orca-ide/resources/bin/orca-ide"
  if [ -x "$REAL_ORCA" ] || [ -x "$ORCA_BIN" ] || command -v orca-ide >/dev/null 2>&1; then
    local ORCA_VER
    if [ -x "$REAL_ORCA" ]; then
      ORCA_VER=$("$REAL_ORCA" --version 2>/dev/null | head -n1 | tr -d 'v')
    elif [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ -x "$ORCA_BIN" ]; then
      ORCA_VER=$(sudo -u "$SUDO_USER" "$ORCA_BIN" --version 2>/dev/null | head -n1 | tr -d 'v')
    elif [ -x "$ORCA_BIN" ]; then
      ORCA_VER=$("$ORCA_BIN" --version 2>/dev/null | head -n1 | tr -d 'v')
    else
      ORCA_VER=$(orca-ide --version 2>/dev/null | head -n1 | tr -d 'v' || echo "Terpasang")
    fi
    printf "  %-18s: %s (%s)\n" "Orca IDE CLI" "${ORCA_VER:-Terpasang}" "${REAL_ORCA:-$ORCA_BIN}"
  else
    printf "  %-18s: %s\n" "Orca IDE CLI" "Belum terinstal"
  fi

  if [ "$cur_mode" = "desktop" ]; then
    if is_orca_desktop_running; then
      local GUI_PID
      GUI_PID=$(pgrep -f "/orca-ide/orca-ide" 2>/dev/null | head -n1 || echo "-")
      printf "  %-18s: Running (Active) [PID: %s]\n" "Status Desktop GUI" "$GUI_PID"
    else
      printf "  %-18s: Inactive (Belum dibuka)\n" "Status Desktop GUI"
    fi
    printf "  %-18s: Nonaktif (Sesuai mode desktop)\n" "Headless Service"
    local autostart_status="Nonaktif"
    [ -f "$TARGET_HOME/.config/autostart/orca-ide.desktop" ] && autostart_status="Aktif (~/.config/autostart/orca-ide.desktop)"
    printf "  %-18s: %s\n" "Desktop Autostart" "$autostart_status"
  else
    if is_orca_headless_running; then
      echo "  Status Service    : Running (Active - orca.service)"
    else
      echo "  Status Service    : Inactive / Belum berjalan"
    fi
    local linger_status="Tidak"
    loginctl show-user "$TARGET_USER" 2>/dev/null | grep -q "Linger=yes" && linger_status="Ya (Layanan user aktif saat boot)"
    printf "  %-18s: %s\n" "Systemd Lingering" "$linger_status"
  fi

  local RUNTIME_FILE="$TARGET_HOME/.config/orca/orca-runtime.json"
  if [ -f "$RUNTIME_FILE" ]; then
    local ORCA_PORT
    ORCA_PORT=$(jq -r '.transports[]? | select(.kind=="websocket") | .endpoint' "$RUNTIME_FILE" 2>/dev/null | awk -F: '{print $NF}' | tr -d '/' || true)
    [ -n "$ORCA_PORT" ] && echo "  Port Runtime      : $ORCA_PORT"
  fi
  local BROWSER_BIN
  BROWSER_BIN=$(detect_browser 2>/dev/null || echo "")
  if [ -n "$BROWSER_BIN" ] && [ -x "$BROWSER_BIN" ]; then
    local BROWSER_NAME
    BROWSER_NAME=$(basename "$BROWSER_BIN")
    echo "  Browser Automation: $BROWSER_NAME ($BROWSER_BIN) - Ready"
  else
    echo "  Browser Automation: Belum terpasang (Chromium / Chrome tidak ditemukan)"
  fi
  echo "  Pairing Info      : Jalankan 'orca pair' untuk URL Web & QR Code"

  echo "------------------------------------------------------------"
  echo "ALAMAT IP AKSES (LAN / NETBIRD):"

  local HAS_IP=0
  if command -v ip >/dev/null 2>&1; then
    while read -r iface status ip_cidr extra; do
      [ -z "$iface" ] && continue
      local ip_clean="${ip_cidr%%/*}"
      if [ "$iface" = "wt0" ]; then
        echo "  IP VPN Netbird    : $ip_clean (Interface: wt0)"
        HAS_IP=1
      elif [ "$iface" != "lo" ] && [[ "$iface" != docker* ]] && [[ "$iface" != br-* ]] && [[ "$iface" != veth* ]]; then
        echo "  IP LAN ($iface)   : $ip_clean"
        HAS_IP=1
      fi
    done < <(ip -br -4 addr show scope global 2>/dev/null || true)
  fi

  if [ "$HAS_IP" -eq 0 ]; then
    echo "  IP Akses          : Tidak ditemukan alamat IP aktif"
  fi

  echo "============================================================"
}

# --- SETUP ORCA ADE (DESKTOP ATAU HEADLESS SERVER) ---
setup_orca() {
  local SCRIPT_DIR
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local ORCA_SCRIPT="$SCRIPT_DIR/setup_orca_headless.sh"

  if [ ! -f "$ORCA_SCRIPT" ]; then
    echo "Error: File skrip $ORCA_SCRIPT tidak ditemukan!"
    return 1
  fi

  chmod +x "$ORCA_SCRIPT"

  detect_desktop_environment || true

  local orca_args=("$@")
  local target_mode="auto"
  local arg
  for arg in ${orca_args[@]+"${orca_args[@]}"}; do
    case "$arg" in
      --desktop) target_mode="desktop" ;;
      --headless) target_mode="headless" ;;
    esac
  done
  if [ "$target_mode" = "auto" ]; then
    [ "$HAS_DE" -eq 1 ] && target_mode="desktop" || target_mode="headless"
  fi

  echo ""
  echo "=========================================="
  if [ "$target_mode" = "desktop" ]; then
    echo "       SETUP ORCA ADE (DESKTOP GUI)       "
  else
    echo "    SETUP ORCA ADE HEADLESS SERVER        "
  fi
  echo "=========================================="
  if [ "$target_mode" = "desktop" ]; then
    [ "$HAS_DE" -eq 1 ] && echo "[✓] Lingkungan Desktop terdeteksi: $DETECTED_DE"
    echo "[*] Menginstal Orca ADE versi Desktop GUI & integrasi menu..."
    echo "    (Mode Headless dinonaktifkan agar hanya versi Desktop yang berjalan)."
  else
    if [ "$HAS_DE" -eq 1 ]; then
      echo "[*] Mode Headless Server dipilih (Lingkungan Desktop: $DETECTED_DE)."
    else
      echo "[*] Mode Headless Server (tidak terdeteksi Lingkungan Desktop GUI)."
    fi
    echo "    (Mode Desktop dinonaktifkan agar hanya versi Server yang berjalan)."
  fi

  ensure_orca_browser_env
  if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
    local target_home
    target_home=$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6 || true)
    [ -z "$target_home" ] && target_home="/home/$SUDO_USER"
    echo "Menjalankan instalasi sebagai user: $SUDO_USER..."
    sudo -u "$SUDO_USER" env HOME="$target_home" bash "$ORCA_SCRIPT" ${orca_args[@]+"${orca_args[@]}"}
  elif [ "$EUID" -eq 0 ] && [ -z "${SUDO_USER:-}" ]; then
    echo "Peringatan: Anda menjalankan script sebagai root murni (bukan via sudo user)."
    echo "Orca disarankan diinstal di user reguler."
    bash "$ORCA_SCRIPT" ${orca_args[@]+"${orca_args[@]}"}
  else
    bash "$ORCA_SCRIPT" ${orca_args[@]+"${orca_args[@]}"}
  fi
  ensure_orca_browser_env
  restore_tty_state
}

uninstall_orca() {
  local SCRIPT_DIR ORCA_SCRIPT
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  ORCA_SCRIPT="$SCRIPT_DIR/setup_orca_headless.sh"

  if [ ! -f "$ORCA_SCRIPT" ]; then
    echo "Error: File skrip $ORCA_SCRIPT tidak ditemukan!"
    return 1
  fi

  chmod +x "$ORCA_SCRIPT"
  local u_args=("--uninstall")
  local arg
  for arg in "$@"; do
    case "$arg" in
      --purge|--keep-config) u_args+=("$arg") ;;
    esac
  done

  if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
    local target_home
    target_home=$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6 || true)
    [ -z "$target_home" ] && target_home="/home/$SUDO_USER"
    sudo -u "$SUDO_USER" env HOME="$target_home" bash "$ORCA_SCRIPT" "${u_args[@]}"
  else
    bash "$ORCA_SCRIPT" "${u_args[@]}"
  fi

  # Pastikan berkas sistem juga bersih bila dijalankan lewat sudo ./setup_dev.sh
  if [ "$EUID" -eq 0 ]; then
    systemctl stop orca.service orca-update.service orca-update.timer 2>/dev/null || true
    systemctl disable orca.service orca-update.service orca-update.timer 2>/dev/null || true
    rm -f /etc/systemd/system/orca.service \
          /etc/systemd/system/orca-update.service \
          /etc/systemd/system/orca-update.timer \
          /usr/local/bin/orca-desktop \
          /usr/share/applications/orca-ide.desktop \
          /usr/share/pixmaps/orca-ide.png \
          /usr/share/pixmaps/orca.png \
          /etc/profile.d/orca-browser.sh 2>/dev/null || true
    find /usr/share/icons/hicolor -type f -name "orca-ide.png" -delete 2>/dev/null || true
    systemctl daemon-reload 2>/dev/null || true
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database /usr/share/applications 2>/dev/null || true
    command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
  fi
  restore_tty_state
}

launch_orca_desktop() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  local ORCA_DESKTOP="$TARGET_HOME/.local/bin/orca-desktop"
  [ ! -x "$ORCA_DESKTOP" ] && ORCA_DESKTOP="/usr/local/bin/orca-desktop"

  if [ ! -x "$ORCA_DESKTOP" ]; then
    echo "Orca Desktop belum terpasang atau skrip launcher belum ditemukan."
    echo "Silakan jalankan instalasi Orca ADE terlebih dahulu."
    return 1
  fi

  echo "Menjalankan Orca Desktop GUI..."
  if [ -n "${DISPLAY:-}" ] || [ -n "${WAYLAND_DISPLAY:-}" ]; then
    if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
      sudo -u "$SUDO_USER" nohup "$ORCA_DESKTOP" < /dev/null >/dev/null 2>&1 9>&- &
    else
      nohup "$ORCA_DESKTOP" < /dev/null >/dev/null 2>&1 9>&- &
    fi
    echo "[✓] Orca Desktop berhasil diluncurkan di background layar aktif."
  else
    echo "[!] Sesi terminal saat ini tidak memiliki variabel DISPLAY atau WAYLAND_DISPLAY."
    echo "Anda dapat meluncurkan Orca Desktop langsung dari menu aplikasi DE ($DETECTED_DE)"
    echo "atau jalankan perintah: orca-desktop (di dalam terminal emulator desktop)."
  fi
}

show_orca_pair() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  local ORCA_PAIR=""
  for p in \
    "$TARGET_HOME/.local/bin/orca-pair" \
    "/usr/local/bin/orca-pair" \
    "$TARGET_HOME/.config/orca/cli/bin/orca-pair"; do
    if [ -x "$p" ]; then
      ORCA_PAIR="$p"
      break
    fi
  done

  if [ -n "$ORCA_PAIR" ]; then
    if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
      run_as_target_user "$ORCA_PAIR" "$@"
    else
      "$ORCA_PAIR" "$@"
    fi
    return 0
  fi

  # Fallback gunakan binary / wrapper orca dengan argumen pair
  local ORCA_BIN
  ORCA_BIN="$(get_orca_bin || true)"
  if [ -n "$ORCA_BIN" ]; then
    if [ "$EUID" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
      run_as_target_user "$ORCA_BIN" pair "$@"
    else
      "$ORCA_BIN" pair "$@"
    fi
    return 0
  fi

  echo "Skrip orca-pair belum terpasang."
  echo "Silakan jalankan Setup Orca Server terlebih dahulu."
}

configure_orca_commit_ai() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  local DB_PATH="$TARGET_HOME/.config/orca/profiles/local-default/profile-state.db"

  if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database profil Orca tidak ditemukan di $DB_PATH."
    echo "Pastikan Orca ADE Headless Server sudah terpasang dan service sudah pernah berjalan."
    return 1
  fi

  echo ""
  echo "=================================================="
  echo "   KONFIGURASI DEFAULT GIT COMMIT GENERATOR (AI)  "
  echo "=================================================="
  echo "Pengaturan ini akan disetel sebagai default server & repo,"
  echo "sehingga tiap device baru yang dipairing otomatis menggunakannya."
  echo ""

  # Baca agent aktif saat ini dari database profil jika ada
  local CURRENT_ACTIVE_AGENT=""
  if command -v python3 >/dev/null 2>&1; then
    CURRENT_ACTIVE_AGENT=$(python3 -c "
import sqlite3, json
try:
    c = sqlite3.connect('$DB_PATH')
    r = c.cursor().execute(\"SELECT payload FROM profile_state_documents WHERE domain = 'settings'\").fetchone()
    if r:
        d = json.loads(r[0])
        print(d.get('sourceControlAi', {}).get('agentId') or d.get('commitMessageAi', {}).get('agentId') or '')
except:
    pass
" 2>/dev/null || true)
  fi

  if [ -n "$CURRENT_ACTIVE_AGENT" ]; then
    echo "AI Agent aktif saat ini: $CURRENT_ACTIVE_AGENT"
    echo ""
  fi

  # Cek ketersediaan agent AI di mesin lokal yang didukung Orca ADE
  local HAS_OPENCODE=0
  local HAS_AGY=0
  local HAS_OMP=0
  local HAS_CLAUDE=0
  local HAS_COPILOT=0
  local HAS_CODEX=0
  local HAS_CURSOR=0

  ([ -x "$TARGET_HOME/.opencode/bin/opencode" ] || [ -x "$TARGET_HOME/.local/bin/opencode" ] || command -v opencode >/dev/null 2>&1) && HAS_OPENCODE=1
  ([ -x "$TARGET_HOME/.local/bin/agy" ] || command -v agy >/dev/null 2>&1) && HAS_AGY=1
  ([ -x "$TARGET_HOME/.local/bin/omp" ] || command -v omp >/dev/null 2>&1) && HAS_OMP=1
  command -v claude >/dev/null 2>&1 && HAS_CLAUDE=1
  command -v copilot >/dev/null 2>&1 && HAS_COPILOT=1
  command -v codex >/dev/null 2>&1 && HAS_CODEX=1
  command -v cursor-agent >/dev/null 2>&1 && HAS_CURSOR=1

  # Tentukan opsi default (prioritaskan agent aktif saat ini jika terpasang)
  local DEFAULT_OPT="1"
  if [ "$CURRENT_ACTIVE_AGENT" = "opencode" ] && [ "$HAS_OPENCODE" -eq 1 ]; then
    DEFAULT_OPT="1"
  elif [ "$CURRENT_ACTIVE_AGENT" = "antigravity" ] && [ "$HAS_AGY" -eq 1 ]; then
    DEFAULT_OPT="2"
  elif [ "$CURRENT_ACTIVE_AGENT" = "omp" ] && [ "$HAS_OMP" -eq 1 ]; then
    DEFAULT_OPT="3"
  elif [ "$CURRENT_ACTIVE_AGENT" = "claude" ] && [ "$HAS_CLAUDE" -eq 1 ]; then
    DEFAULT_OPT="4"
  elif [ "$CURRENT_ACTIVE_AGENT" = "copilot" ] && [ "$HAS_COPILOT" -eq 1 ]; then
    DEFAULT_OPT="5"
  elif [ "$HAS_OPENCODE" -eq 1 ]; then
    DEFAULT_OPT="1"
  elif [ "$HAS_AGY" -eq 1 ]; then
    DEFAULT_OPT="2"
  elif [ "$HAS_OMP" -eq 1 ]; then
    DEFAULT_OPT="3"
  elif [ "$HAS_CLAUDE" -eq 1 ]; then
    DEFAULT_OPT="4"
  elif [ "$HAS_COPILOT" -eq 1 ]; then
    DEFAULT_OPT="5"
  fi

  echo "Pilih Default AI Agent untuk Git Commit Generator:"
  echo "1. OpenCode $([ $HAS_OPENCODE -eq 1 ] && echo '[Terpasang]' || echo '[Belum terpasang]')"
  echo "2. Antigravity / AGY $([ $HAS_AGY -eq 1 ] && echo '[Terpasang]' || echo '[Belum terpasang]')"
  echo "3. OMP $([ $HAS_OMP -eq 1 ] && echo '[Terpasang]' || echo '[Belum terpasang]')"
  echo "4. Claude Code $([ $HAS_CLAUDE -eq 1 ] && echo '[Terpasang]' || echo '[Belum terpasang]')"
  echo "5. GitHub Copilot CLI $([ $HAS_COPILOT -eq 1 ] && echo '[Terpasang]' || echo '[Belum terpasang]')"
  echo "6. Custom Agent CLI (ketik nama agent: cursor, codex, kimi, dll.)"
  read_menu_input a_choice "Pilihan [1-6, default: $DEFAULT_OPT]: " || a_choice=""
  [ -z "$a_choice" ] && a_choice="$DEFAULT_OPT"

  local CHOSEN_AGENT="opencode"
  case "$a_choice" in
    1) CHOSEN_AGENT="opencode" ;;
    2) CHOSEN_AGENT="antigravity" ;;
    3) CHOSEN_AGENT="omp" ;;
    4) CHOSEN_AGENT="claude" ;;
    5) CHOSEN_AGENT="copilot" ;;
    6)
      read_menu_input input_agent "Masukkan ID agent (contoh: opencode, antigravity, omp, claude, copilot, cursor): " || input_agent=""
      [ -n "$input_agent" ] && CHOSEN_AGENT="$input_agent"
      ;;
    *)
      case "$DEFAULT_OPT" in
        2) CHOSEN_AGENT="antigravity" ;;
        3) CHOSEN_AGENT="omp" ;;
        4) CHOSEN_AGENT="claude" ;;
        5) CHOSEN_AGENT="copilot" ;;
        *) CHOSEN_AGENT="opencode" ;;
      esac
      ;;
  esac

  echo ""
  echo "Format instruksi commit message default:"
  echo "1. Conventional Commits (Bahasa Indonesia)"
  echo "2. Conventional Commits (English)"
  echo "3. Kustom (ketik sendiri instruksi prompt)"
  read_menu_input p_choice "Pilih format [1-3, default: 1]: " || p_choice=""

  local CHOSEN_PROMPT="Gunakan format Conventional Commits (feat, fix, refactor, docs, chore, test). Judul maksimal 72 karakter. Tulis pesan commit dalam Bahasa Indonesia yang singkat, padat, dan jelas."
  case "$p_choice" in
    2)
      CHOSEN_PROMPT="Follow Conventional Commits (feat, fix, refactor, docs, chore, test). Keep title under 72 characters. Write in clear and concise English."
      ;;
    3)
      read_menu_input custom_p "Masukkan instruksi kustom: " || custom_p=""
      [ -n "$custom_p" ] && CHOSEN_PROMPT="$custom_p"
      ;;
  esac

  echo ""
  echo "Menerapkan konfigurasi ke Orca..."
  echo "  - AI Agent : $CHOSEN_AGENT"
  echo "  - Prompt   : $CHOSEN_PROMPT"

  local cur_mode
  cur_mode="$(get_orca_configured_mode)"

  local WAS_DESKTOP_RUNNING=0
  local WAS_HEADLESS_RUNNING=0
  local WAS_RUNNING=0

  if is_orca_desktop_running; then
    WAS_DESKTOP_RUNNING=1
    WAS_RUNNING=1
  fi
  if is_orca_headless_running; then
    WAS_HEADLESS_RUNNING=1
    WAS_RUNNING=1
  elif is_orca_running; then
    WAS_RUNNING=1
  fi

  if [ "$WAS_RUNNING" -eq 1 ]; then
    echo "Menghentikan Orca sementara agar pembaruan database tersimpan permanen..."
    stop_orca_service
  fi

  if ! command -v python3 >/dev/null 2>&1; then
    echo "Error: python3 tidak ditemukan; konfigurasi AI tidak dapat diterapkan."
    if [ "$WAS_RUNNING" -eq 1 ]; then
      if [ "$cur_mode" = "desktop" ] && [ "$WAS_DESKTOP_RUNNING" -eq 1 ]; then
        launch_orca_desktop
      else
        restart_orca_service
      fi
    fi
    return 1
  fi

  local PY_RC=0
  trap '[ "$WAS_RUNNING" -eq 1 ] && { [ "$cur_mode" = "desktop" ] && [ "$WAS_DESKTOP_RUNNING" -eq 1 ] && launch_orca_desktop 2>/dev/null || restart_orca_service 2>/dev/null; }; exit 130' INT
  if ! run_as_target_user env \
    ORCA_DB_PATH="$DB_PATH" \
    ORCA_AGENT="$CHOSEN_AGENT" \
    ORCA_PROMPT="$CHOSEN_PROMPT" \
    python3 - <<'PYEOF'
import sqlite3, json, hashlib, time, os

db_path = os.environ["ORCA_DB_PATH"]
agent = os.environ["ORCA_AGENT"]
prompt = os.environ["ORCA_PROMPT"]

conn = sqlite3.connect(db_path)
c = conn.cursor()

# Baca revision tertinggi saat ini
c.execute("SELECT value FROM profile_state_meta WHERE key = 'revision'")
row = c.fetchone()
current_rev = int(row[0]) if row and row[0].isdigit() else 0

c.execute("SELECT MAX(revision) FROM profile_state_documents")
row = c.fetchone()
max_doc_rev = row[0] if row and row[0] else 0

new_rev = max(current_rev, max_doc_rev) + 1
now_ms = int(time.time() * 1000)

# 1. Update domain settings (Host level)
c.execute("SELECT payload FROM profile_state_documents WHERE domain = 'settings'")
row = c.fetchone()
if row:
    settings = json.loads(row[0])
    if not isinstance(settings, dict):
        settings = {}
    settings["defaultTuiAgent"] = agent

    if not isinstance(settings.get("commitMessageAi"), dict):
        settings["commitMessageAi"] = {"enabled": True}
    settings["commitMessageAi"]["enabled"] = True
    settings["commitMessageAi"]["agentId"] = agent
    settings["commitMessageAi"]["customPrompt"] = prompt
    settings["commitMessageAi"]["customAgentCommand"] = ""

    if not isinstance(settings.get("sourceControlAi"), dict):
        settings["sourceControlAi"] = {"enabled": True, "actions": {}}
    settings["sourceControlAi"]["enabled"] = True
    settings["sourceControlAi"]["agentId"] = agent

    settings["sourceControlAi"].setdefault("instructionsByOperation", {})
    settings["sourceControlAi"]["instructionsByOperation"]["commitMessage"] = prompt

    settings["sourceControlAi"].setdefault("actions", {})
    settings["sourceControlAi"]["actions"].setdefault("commitMessage", {})
    settings["sourceControlAi"]["actions"]["commitMessage"]["agentId"] = agent
    settings["sourceControlAi"]["actions"]["commitMessage"]["commandInputTemplate"] = "{basePrompt}"

    new_payload = json.dumps(settings)
    chash = hashlib.sha256(new_payload.encode("utf-8")).hexdigest()
    c.execute(
        "UPDATE profile_state_documents SET payload = ?, revision = ?, updated_at = ?, content_hash = ? WHERE domain = 'settings'",
        (new_payload, new_rev, now_ms, chash),
    )

# 2. Update domain repos (Project level override agar device baru langsung mewarisinya)
c.execute("SELECT payload FROM profile_state_documents WHERE domain = 'repos'")
repo_row = c.fetchone()
if repo_row:
    repos = json.loads(repo_row[0])
    if not isinstance(repos, list):
        repos = []
    for repo in repos:
        sc = repo.get("sourceControlAi")
        if not isinstance(sc, dict):
            sc = {}
        sc["enabled"] = True
        sc["agentId"] = agent

        action_overrides = sc.get("actionOverrides")
        if not isinstance(action_overrides, dict):
            action_overrides = {}
        commit_override = action_overrides.get("commitMessage")
        if not isinstance(commit_override, dict):
            commit_override = {}
        commit_override["agentId"] = agent
        commit_override["commandInputTemplate"] = "{basePrompt}"
        action_overrides["commitMessage"] = commit_override
        sc["actionOverrides"] = action_overrides

        actions = sc.get("actions")
        if not isinstance(actions, dict):
            actions = {}
        commit_message = actions.get("commitMessage")
        if not isinstance(commit_message, dict):
            commit_message = {}
        commit_message["agentId"] = agent
        commit_message["commandInputTemplate"] = "{basePrompt}"
        actions["commitMessage"] = commit_message
        sc["actions"] = actions

        instructions = sc.get("instructionsByOperation")
        if not isinstance(instructions, dict):
            instructions = {}
        instructions["commitMessage"] = prompt
        sc["instructionsByOperation"] = instructions
        repo["sourceControlAi"] = sc

        commit_ai = repo.get("commitMessageAi")
        if not isinstance(commit_ai, dict):
            commit_ai = {}
        commit_ai.update({"enabled": True, "agentId": agent, "customPrompt": prompt})
        repo["commitMessageAi"] = commit_ai

    new_r_payload = json.dumps(repos)
    r_hash = hashlib.sha256(new_r_payload.encode("utf-8")).hexdigest()
    c.execute(
        "UPDATE profile_state_documents SET payload = ?, revision = ?, updated_at = ?, content_hash = ? WHERE domain = 'repos'",
        (new_r_payload, new_rev, now_ms, r_hash),
    )

# 3. Sinkronkan profile meta revision
c.execute(
    """
    INSERT INTO profile_state_meta (key, value) VALUES ('revision', ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value
    """,
    (str(new_rev),),
)

conn.commit()
c.execute("PRAGMA wal_checkpoint(TRUNCATE)")
conn.close()
PYEOF
  then
    PY_RC=1
  fi
  trap 'exit 130' INT

  if [ "$cur_mode" = "desktop" ]; then
    if [ "$WAS_DESKTOP_RUNNING" -eq 1 ]; then
      echo "Merestart Orca IDE Desktop (GUI)..."
      launch_orca_desktop
    else
      echo "Orca IDE Desktop sedang tidak aktif. Pengaturan baru akan otomatis dimuat saat aplikasi dibuka."
    fi
  else
    # Mode Headless Server
    if [ "$WAS_RUNNING" -eq 1 ] || [ "$WAS_HEADLESS_RUNNING" -eq 1 ]; then
      echo "Merestart layanan Orca Server..."
      restart_orca_service
    else
      echo "Memulai layanan Orca Server agar konfigurasi baru langsung aktif..."
      start_orca_service
    fi

    # Tunggu beberapa detik agar server siap dan port aktif
    echo "Menunggu Orca Server siap menerima koneksi..."
    local wait_count=0
    while [ "$wait_count" -lt 15 ]; do
      if is_orca_running; then
        break
      fi
      sleep 1
      wait_count=$((wait_count + 1))
    done
    sleep 1

    echo ""
    echo "=================================================="
    echo "       INFORMASI AKSES & PAIRING ORCA SERVER      "
    echo "=================================================="
    show_orca_pair || true
  fi

  if [ "$PY_RC" -ne 0 ]; then
    echo "Error: gagal memperbarui database Orca; layanan sudah dipulihkan."
    return 1
  fi

  echo ""
  echo "[✓] Default Git Commit Generator ($CHOSEN_AGENT) berhasil diperbarui dan tersimpan permanen!"
}

# --- HELPER FORMATTING MENU ---
print_menu_header() {
  local title="$1"
  local width=64
  local pad=$(( (width - ${#title}) / 2 ))
  [ "$pad" -lt 0 ] && pad=0
  echo ""
  echo -e "${C_BOLD}================================================================${C_RESET}"
  printf "${C_BOLD}%*s%s%*s${C_RESET}\n" "$pad" "" "$title" "$pad" ""
  echo -e "${C_BOLD}================================================================${C_RESET}"
}

# --- SUBMENU ORCA ADE (DESKTOP GUI <-> HEADLESS SERVER) ---
menu_orca() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"
  detect_desktop_environment || true

  while true; do
    if ! is_orca_installed; then
      print_menu_header "INSTALASI ORCA ADE"
      echo -e "  ${C_BOLD}Status Instalasi  :${C_RESET} ${C_RED}Belum Terpasang${C_RESET}"
      if [ "$HAS_DE" -eq 1 ]; then
        echo -e "  ${C_BOLD}Lingkungan Sistem :${C_RESET} Desktop Terdeteksi ($DETECTED_DE)"
      else
        echo -e "  ${C_BOLD}Lingkungan Sistem :${C_RESET} Headless (Tanpa Desktop)"
      fi
      echo -e "  --------------------------------------------------------------"
      echo -e "  ${C_CYAN}[1]${C_RESET} Instal Orca ADE - Desktop GUI (Aplikasi Visual & Menu)"
      echo -e "  ${C_CYAN}[2]${C_RESET} Instal Orca ADE - Headless Server (Background Service 24/7 + QR)"
      echo -e "  ${C_CYAN}[3]${C_RESET} Instal Otomatis (Sesuai Deteksi Lingkungan Sistem)"
      echo -e "  ${C_CYAN}[4]${C_RESET} Cek Status Lengkap"
      echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
      echo -e "${C_BOLD}================================================================${C_RESET}"
      read_menu_input orca_opt "Pilih opsi [0-4]: " || break

      case $orca_opt in
        1) setup_orca --desktop ;;
        2) setup_orca --headless ;;
        3) setup_orca ;;
        4) show_info ;;
        0|q|Q) break ;;
        *) echo "Pilihan tidak valid." ;;
      esac
      continue
    fi

    local cur_mode
    cur_mode="$(get_orca_configured_mode)"

    if [ "$cur_mode" = "desktop" ]; then
      print_menu_header "MANAJEMEN ORCA ADE (DESKTOP GUI)"
      if [ "$HAS_DE" -eq 1 ]; then
        echo -e "  ${C_BOLD}Lingkungan Sistem :${C_RESET} Desktop Terdeteksi ($DETECTED_DE)"
      else
        echo -e "  ${C_BOLD}Lingkungan Sistem :${C_RESET} Headless (Tanpa Desktop)"
      fi
      echo -e "  ${C_BOLD}Mode Aktif        :${C_RESET} ${C_GREEN}DESKTOP GUI${C_RESET}"
      if is_orca_desktop_running; then
        local gpid
        gpid=$(pgrep -f "/orca-ide/orca-ide" 2>/dev/null | head -n1 || echo "-")
        echo -e "  ${C_BOLD}Status GUI        :${C_RESET} ${C_GREEN}Running (Active) [PID: $gpid]${C_RESET}"
      else
        echo -e "  ${C_BOLD}Status GUI        :${C_RESET} ${C_YELLOW}Inactive (Belum dibuka)${C_RESET}"
      fi
      local autostart_info="Nonaktif"
      [ -f "$TARGET_HOME/.config/autostart/orca-ide.desktop" ] && autostart_info="Aktif"
      echo -e "  ${C_BOLD}Desktop Autostart :${C_RESET} $autostart_info"
      echo -e "  --------------------------------------------------------------"
      echo -e "  ${C_CYAN}[1]${C_RESET} Buka Orca IDE Desktop (GUI)"
      echo -e "  ${C_CYAN}[2]${C_RESET} Ganti Mode ke Headless Server (Non-GUI / 24/7 di Boot)"
      echo -e "  ${C_CYAN}[3]${C_RESET} Toggle Autostart Desktop [Saat ini: $autostart_info]"
      echo -e "  ${C_CYAN}[4]${C_RESET} Konfigurasi Default Git Commit Generator (AI)"
      echo -e "  ${C_CYAN}[5]${C_RESET} Tutup Orca IDE Desktop (Stop)"
      echo -e "  ${C_CYAN}[6]${C_RESET} Instal Ulang / Update Orca ADE (Desktop GUI)"
      echo -e "  ${C_CYAN}[7]${C_RESET} Cek Status Lengkap"
      echo -e "  ${C_CYAN}[8]${C_RESET} Uninstall Orca ADE"
      echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
      echo -e "${C_BOLD}================================================================${C_RESET}"
      read_menu_input orca_opt "Pilih opsi [0-8]: " || break

      case $orca_opt in
        1) launch_orca_desktop ;;
        2)
          echo ""
          if [ -x "$TARGET_HOME/.local/bin/orca-switch" ]; then
            "$TARGET_HOME/.local/bin/orca-switch" headless
          elif [ -x "/usr/local/bin/orca-switch" ]; then
            /usr/local/bin/orca-switch headless
          else
            stop_orca_service
            setup_orca --headless
          fi
          ;;
        3)
          if [ -x "$TARGET_HOME/.local/bin/orca-switch" ]; then
            "$TARGET_HOME/.local/bin/orca-switch" autostart toggle
          fi
          ;;
        4) configure_orca_commit_ai ;;
        5)
          echo "Menutup Orca IDE Desktop..."
          stop_orca_service
          ;;
        6) setup_orca --desktop ;;
        7)
          if [ -x "$TARGET_HOME/.local/bin/orca-switch" ]; then
            "$TARGET_HOME/.local/bin/orca-switch" status
          else
            show_info
          fi
          ;;
        8) uninstall_orca ;;
        0|q|Q) break ;;
        *) echo "Pilihan tidak valid." ;;
      esac
    else
      # Mode Headless Server
      print_menu_header "MANAJEMEN ORCA ADE (HEADLESS SERVER)"
      if [ "$HAS_DE" -eq 1 ]; then
        echo -e "  ${C_BOLD}Lingkungan Sistem :${C_RESET} Desktop Terdeteksi ($DETECTED_DE)"
      else
        echo -e "  ${C_BOLD}Lingkungan Sistem :${C_RESET} Headless (Tanpa Desktop)"
      fi
      echo -e "  ${C_BOLD}Mode Aktif        :${C_RESET} ${C_GREEN}HEADLESS SERVER (24/7 di Boot)${C_RESET}"
      if is_orca_headless_running; then
        echo -e "  ${C_BOLD}Status Server     :${C_RESET} ${C_GREEN}Running (Active - orca.service)${C_RESET}"
      else
        echo -e "  ${C_BOLD}Status Server     :${C_RESET} ${C_YELLOW}Inactive / Belum berjalan${C_RESET}"
      fi
      local linger_info="Tidak"
      loginctl show-user "${SUDO_USER:-$USER}" 2>/dev/null | grep -q "Linger=yes" && linger_info="Ya (Autostart di Boot)"
      echo -e "  ${C_BOLD}Systemd Lingering :${C_RESET} $linger_info"
      echo -e "  --------------------------------------------------------------"
      echo -e "  ${C_CYAN}[1]${C_RESET} Tampilkan QR Code & Info Pairing (orca pair)"
      echo -e "  ${C_CYAN}[2]${C_RESET} Ganti Mode ke Desktop GUI (Antarmuka Visual)"
      echo -e "  ${C_CYAN}[3]${C_RESET} Start / Restart Layanan Orca Server"
      echo -e "  ${C_CYAN}[4]${C_RESET} Stop Layanan Orca Server"
      echo -e "  ${C_CYAN}[5]${C_RESET} Konfigurasi Default Git Commit Generator (AI)"
      echo -e "  ${C_CYAN}[6]${C_RESET} Instal Ulang / Update Orca ADE (Headless Server)"
      echo -e "  ${C_CYAN}[7]${C_RESET} Cek Status Lengkap"
      echo -e "  ${C_CYAN}[8]${C_RESET} Uninstall Orca ADE"
      echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
      echo -e "${C_BOLD}================================================================${C_RESET}"
      read_menu_input orca_opt "Pilih opsi [0-8]: " || break

      case $orca_opt in
        1) show_orca_pair ;;
        2)
          echo ""
          if [ -x "$TARGET_HOME/.local/bin/orca-switch" ]; then
            "$TARGET_HOME/.local/bin/orca-switch" desktop
          elif [ -x "/usr/local/bin/orca-switch" ]; then
            /usr/local/bin/orca-switch desktop
          else
            stop_orca_service
            setup_orca --desktop
          fi
          ;;
        3)
          if is_orca_running; then
            echo "Merestart layanan Orca Server..."
            restart_orca_service
          else
            echo "Menjalankan layanan Orca Server..."
            start_orca_service
          fi
          ;;
        4)
          echo "Menghentikan layanan Orca Server..."
          stop_orca_service
          ;;
        5) configure_orca_commit_ai ;;
        6) setup_orca --headless ;;
        7)
          if [ -x "$TARGET_HOME/.local/bin/orca-switch" ]; then
            "$TARGET_HOME/.local/bin/orca-switch" status
          else
            show_info
          fi
          ;;
        8) uninstall_orca ;;
        0|q|Q) break ;;
        *) echo "Pilihan tidak valid." ;;
      esac
    fi
  done
}

# --- SUBMENU CLOUDFLARE TUNNEL & DNS ---
cf_tunnel_menu_bin() {
  local b
  for b in /usr/local/bin/cf-tunnel "$HOME/.local/bin/cf-tunnel" "${WDEV_SCRIPT_DIR}/cf-tunnel.sh"; do
    if [ -x "$b" ]; then
      echo "$b"
      return 0
    fi
  done
  command -v cf-tunnel 2>/dev/null || return 1
}

dev_site_menu_bin() {
  local b
  for b in /usr/local/bin/dev-site "$HOME/.local/bin/dev-site"; do
    if [ -x "$b" ]; then
      echo "$b"
      return 0
    fi
  done
  command -v dev-site 2>/dev/null || return 1
}

run_cf_menu() {
  local b rc=0
  b=$(cf_tunnel_menu_bin) || {
    echo "cf-tunnel belum terpasang. Jalankan: sudo $0 --cloudflare"
    return 0
  }
  if [ "$EUID" -eq 0 ]; then
    "$b" "$@" || rc=$?
  else
    sudo "$b" "$@" || rc=$?
  fi
  if [ "$rc" -ne 0 ]; then
    echo "Perintah cf-tunnel gagal (exit ${rc}); menu tetap aktif." >&2
  fi
  return 0
}

menu_cf_pick_profile() {
  local -a profs=()
  local p i idx
  while IFS= read -r p; do
    [ -n "$p" ] && profs+=("$p")
  done < <(wdev_profile_list)
  if [ "${#profs[@]}" -eq 0 ]; then
    echo "Belum ada profil Cloudflare. Tambah dulu lewat menu Akun." >&2
    return 1
  fi
  echo "Profil tersedia:" >&2
  for i in "${!profs[@]}"; do
    printf '  %d. %s\n' "$((i + 1))" "${profs[$i]}" >&2
  done
  if [ "${#profs[@]}" -eq 1 ]; then
    printf '%s' "Pilih nomor atau nama profil [default: 1 (${profs[0]})]: " >&2
    IFS= read -r p || return 1
    [ -z "$p" ] && p="1"
  else
    printf '%s' "Pilih nomor atau nama profil: " >&2
    IFS= read -r p || return 1
  fi
  [ -n "$p" ] || return 1
  if [[ "$p" =~ ^[0-9]+$ ]]; then
    idx=$((p - 1))
    if [ "$idx" -lt 0 ] || [ "$idx" -ge "${#profs[@]}" ]; then
      echo "Nomor profil tidak valid." >&2
      return 1
    fi
    p="${profs[$idx]}"
  fi
  if ! printf '%s\n' "${profs[@]}" | grep -qxF "$p"; then
    echo "Profil '$p' tidak ditemukan." >&2
    return 1
  fi
  printf '%s\n' "$p"
}

menu_cf_account() {
  local p
  while true; do
    print_menu_header "AKUN CLOUDFLARE"
    echo -e "  ${C_CYAN}[1]${C_RESET} Daftar Akun & Status"
    echo -e "  ${C_CYAN}[2]${C_RESET} Tambah Akun Baru"
    echo -e "  ${C_CYAN}[3]${C_RESET} Update Profil / Token / Protocol"
    echo -e "  ${C_CYAN}[4]${C_RESET} Login / Otorisasi Ulang Akun"
    echo -e "  ${C_CYAN}[5]${C_RESET} Hapus Akun"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input a_opt "Pilih opsi [0-5]: " || break
    case "$a_opt" in
      1) run_cf_menu account list ;;
      2)
        read_menu_input p "Nama profil baru (contoh: cf1): " || continue
        [ -n "$p" ] && run_cf_menu account add "$p"
        ;;
      3)
        p=$(menu_cf_pick_profile) || continue
        run_cf_menu account update "$p"
        ;;
      4)
        p=$(menu_cf_pick_profile) || continue
        run_cf_menu login --profile "$p" --force
        ;;
      5)
        p=$(menu_cf_pick_profile) || continue
        run_cf_menu account remove "$p"
        ;;
      0|q|Q) return 0 ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

menu_cf_tunnel() {
  local p
  while true; do
    print_menu_header "TUNNEL CLOUDFLARE"
    echo -e "  ${C_CYAN}[1]${C_RESET} Buat Tunnel Baru"
    echo -e "  ${C_CYAN}[2]${C_RESET} Rotasi / Ganti Tunnel"
    echo -e "  ${C_CYAN}[3]${C_RESET} Info Tunnel & Konektor"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input t_opt "Pilih opsi [0-3]: " || break
    case "$t_opt" in
      1|2)
        p=$(menu_cf_pick_profile) || continue
        if [ "$t_opt" = "1" ]; then
          run_cf_menu tunnel create --profile "$p"
        else
          run_cf_menu tunnel rotate --profile "$p"
        fi
        ;;
      3)
        p=$(menu_cf_pick_profile) || continue
        run_cf_menu tunnel info --profile "$p"
        ;;
      0|q|Q) return 0 ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

menu_cf_service() {
  local action target
  while true; do
    print_menu_header "SERVICE CLOUDFLARED"
    echo -e "  ${C_CYAN}[1]${C_RESET} Start"
    echo -e "  ${C_CYAN}[2]${C_RESET} Stop"
    echo -e "  ${C_CYAN}[3]${C_RESET} Restart"
    echo -e "  ${C_CYAN}[4]${C_RESET} Logs (Journalctl)"
    echo -e "  ${C_CYAN}[5]${C_RESET} Status"
    echo -e "  ${C_CYAN}[6]${C_RESET} Install / Regenerate Unit Systemd"
    echo -e "  ${C_CYAN}[7]${C_RESET} Enable (Autostart di Boot)"
    echo -e "  ${C_CYAN}[8]${C_RESET} Disable (Nonaktifkan Autostart)"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input action "Pilih aksi [0-8]: " || break
    case "$action" in
      0|q|Q) return 0 ;;
      1) action="start" ;;
      2) action="stop" ;;
      3) action="restart" ;;
      4) action="logs" ;;
      5) action="status" ;;
      6) action="install" ;;
      7) action="enable" ;;
      8) action="disable" ;;
      *) echo "Aksi tidak valid."; continue ;;
    esac
    read_menu_input target "Profil (nama profil atau 'all', kosongkan untuk batal): " || continue
    [ -z "$target" ] && continue
    if [ "$target" = "all" ]; then
      run_cf_menu service "$action" --all
    else
      run_cf_menu service "$action" --profile "$target"
    fi
  done
}

menu_cf_dns() {
  local action p host
  while true; do
    print_menu_header "DNS CLOUDFLARE"
    echo -e "  ${C_CYAN}[1]${C_RESET} Daftar CNAME Record"
    echo -e "  ${C_CYAN}[2]${C_RESET} Tambah / Perbarui Host Publik"
    echo -e "  ${C_CYAN}[3]${C_RESET} Hapus Host Publik"
    echo -e "  ${C_CYAN}[4]${C_RESET} Sinkronkan Semua Eksposur Profil"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input action "Pilih aksi [0-4]: " || break
    case "$action" in
      0|q|Q) return 0 ;;
      1|2|3|4) ;;
      *) echo "Aksi tidak valid."; continue ;;
    esac
    p=$(menu_cf_pick_profile) || continue
    case "$action" in
      1) run_cf_menu dns list --profile "$p" ;;
      2)
        read_menu_input host "Host publik (contoh: app-dev.example.com, kosongkan untuk batal): " || continue
        [ -n "$host" ] && run_cf_menu dns add --profile "$p" "$host"
        ;;
      3)
        read_menu_input host "Host publik yang dihapus (kosongkan untuk batal): " || continue
        [ -n "$host" ] && run_cf_menu dns rm --profile "$p" "$host"
        ;;
      4) run_cf_menu dns ensure --profile "$p" ;;
    esac
  done
}

menu_cf_expose() {
  local ds d host p i
  ds=$(dev_site_menu_bin) || { echo "dev-site belum terpasang."; return 1; }
  local -a sites=()
  local conf
  if [ -d /etc/nginx/sites-available ]; then
    for conf in /etc/nginx/sites-available/*.conf; do
      [ -f "$conf" ] || continue
      [[ "$conf" == *.custom.conf ]] && continue
      sites+=("$(basename "$conf" .conf)")
    done
  fi
  if [ "${#sites[@]}" -gt 0 ]; then
    echo "Virtual host lokal tersedia:"
    for i in "${!sites[@]}"; do printf '  %d. %s\n' "$((i + 1))" "${sites[$i]}"; done
  fi
  read_menu_input d "Domain lokal (nomor atau nama, contoh: myapp.test, kosongkan untuk batal): " || return 1
  [ -n "$d" ] || return 1
  if [[ "$d" =~ ^[0-9]+$ ]]; then
    i=$((d - 1))
    if [ "$i" -lt 0 ] || [ "$i" -ge "${#sites[@]}" ]; then
      echo "Nomor vhost tidak valid." >&2
      return 1
    fi
    d="${sites[$i]}"
  fi
  read_menu_input host "Host publik (contoh: app-dev.example.com, kosongkan untuk batal): " || return 1
  [ -n "$host" ] || return 1
  p=$(menu_cf_pick_profile) || return 1
  local zone zrc=0
  local -a extra_flags=()
  zone=$(wdev_profile_zone "$p")
  wdev_host_in_zone "$host" "$zone" 0 || zrc=$?
  if [ "$zrc" -eq 2 ]; then
    echo -e "${C_YELLOW}[!] '${host}' adalah subdomain bertingkat di bawah '${zone}'.${C_RESET}"
    echo "    Universal SSL gratis hanya *.${zone}; host bertingkat butuh Total TLS / ACM di Cloudflare."
    read_menu_input ans_deep "Lanjutkan dengan --allow-deep? [y/N]: " || return 1
    case "$ans_deep" in y|Y|yes) extra_flags+=("--allow-deep") ;; *) return 1 ;; esac
  fi
  if [ "$EUID" -eq 0 ]; then
    "$ds" expose "$d" "$host" --profile "$p" "${extra_flags[@]}" || true
  else
    sudo "$ds" expose "$d" "$host" --profile "$p" "${extra_flags[@]}" || true
  fi
}

menu_cf_unexpose() {
  local ds d host
  ds=$(dev_site_menu_bin) || { echo "dev-site belum terpasang."; return 1; }
  read_menu_input d "Domain lokal (contoh: myapp.test, kosongkan untuk batal): " || return 1
  [ -n "$d" ] || return 1
  read_menu_input host "Host publik (kosongkan untuk semua eksposur domain): " || host=""
  if [ "$EUID" -eq 0 ]; then
    if [ -n "$host" ]; then
      "$ds" unexpose "$d" "$host" || true
    else
      "$ds" unexpose "$d" || true
    fi
  else
    if [ -n "$host" ]; then
      sudo "$ds" unexpose "$d" "$host" || true
    else
      sudo "$ds" unexpose "$d" || true
    fi
  fi
}

menu_cf_exposures() {
  local ds action
  while true; do
    print_menu_header "EKSPOSUR PUBLIK (DEV-SITE)"
    echo -e "  ${C_CYAN}[1]${C_RESET} Daftar Eksposur Aktif"
    echo -e "  ${C_CYAN}[2]${C_RESET} Ekspos Virtual Host ke Internet"
    echo -e "  ${C_CYAN}[3]${C_RESET} Hapus Eksposur Publik"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input action "Pilih opsi [0-3]: " || break
    case "$action" in
      1)
        ds=$(dev_site_menu_bin) || { echo "dev-site belum terpasang."; continue; }
        "$ds" exposures
        ;;
      2) menu_cf_expose ;;
      3) menu_cf_unexpose ;;
      0|q|Q) return 0 ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

menu_cf_quick() {
  local opt target bg_ans label
  while true; do
    print_menu_header "QUICK TUNNEL (TRYCLOUDFLARE.COM)"
    echo -e "  ${C_CYAN}[1]${C_RESET} Mulai Quick Tunnel (Foreground, Ctrl+C berhenti)"
    echo -e "  ${C_CYAN}[2]${C_RESET} Mulai Quick Tunnel (Background)"
    echo -e "  ${C_CYAN}[3]${C_RESET} Status & URL Quick Tunnel Aktif"
    echo -e "  ${C_CYAN}[4]${C_RESET} Lihat Log Quick Tunnel"
    echo -e "  ${C_CYAN}[5]${C_RESET} Hentikan Quick Tunnel"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input opt "Pilih opsi [0-5]: " || break
    case "$opt" in
      1|2)
        read_menu_input target "Target (port mis. 3000, URL http(s)://..., atau domain proxy): " || continue
        [ -n "$target" ] || continue
        local -a q_args=("quick" "$target")
        if [[ "$target" == https://* ]]; then
          read_menu_input bg_ans "Abaikan verifikasi TLS lokal (--no-tls-verify)? [y/N]: " || bg_ans="n"
          [[ "$bg_ans" =~ ^[yY] ]] && q_args+=("--no-tls-verify")
        fi
        if [ "$opt" = "2" ]; then
          read_menu_input label "Label nama (opsional, Enter untuk otomatis): " || label=""
          q_args+=("--bg")
          [ -n "$label" ] && q_args+=("--name" "$label")
        fi
        run_cf_menu "${q_args[@]}"
        ;;
      3) run_cf_menu quick status ;;
      4)
        read_menu_input label "Label Quick Tunnel: " || continue
        [ -n "$label" ] && run_cf_menu quick logs "$label"
        ;;
      5)
        read_menu_input label "Label Quick Tunnel (atau 'all' untuk semua): " || continue
        if [ "$label" = "all" ]; then
          run_cf_menu quick stop --all
        elif [ -n "$label" ]; then
          run_cf_menu quick stop "$label"
        fi
        ;;
      0|q|Q) return 0 ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

menu_cf_routes() {
  local opt p host svc path_val notls_ans cidr comment
  while true; do
    print_menu_header "CUSTOM ROUTES & PRIVATE NETWORK (CIDR)"
    echo -e "  ${C_CYAN}[1]${C_RESET} Daftar Custom Ingress Routes"
    echo -e "  ${C_CYAN}[2]${C_RESET} Tambah Custom Route (HTTP/HTTPS/TCP/SSH/RDP/Unix)"
    echo -e "  ${C_CYAN}[3]${C_RESET} Hapus Custom Route"
    echo -e "  ${C_CYAN}[4]${C_RESET} Daftar Private Network CIDR"
    echo -e "  ${C_CYAN}[5]${C_RESET} Tambah Private Network CIDR"
    echo -e "  ${C_CYAN}[6]${C_RESET} Hapus Private Network CIDR"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input opt "Pilih opsi [0-6]: " || break
    case "$opt" in
      1) run_cf_menu route list --all ;;
      2)
        p=$(menu_cf_pick_profile) || continue
        read_menu_input host "Host publik (mis. api.example.com / ssh.example.com): " || continue
        read_menu_input svc "Service URL (mis. http://127.0.0.1:8080 / ssh://127.0.0.1:22 / tcp://127.0.0.1:5432): " || continue
        [ -n "$host" ] && [ -n "$svc" ] || continue
        read_menu_input path_val "Path regex (opsional, Enter untuk semua): " || path_val=""
        local -a r_args=("route" "add" "--profile" "$p" "$host" "$svc")
        [ -n "$path_val" ] && r_args+=("--path" "$path_val")
        if [[ "$svc" == https://* ]]; then
          read_menu_input notls_ans "No TLS Verify? [y/N]: " || notls_ans="n"
          [[ "$notls_ans" =~ ^[yY] ]] && r_args+=("--no-tls-verify")
        fi
        run_cf_menu "${r_args[@]}"
        ;;
      3)
        p=$(menu_cf_pick_profile) || continue
        read_menu_input host "Host publik route yang dihapus: " || continue
        [ -n "$host" ] && run_cf_menu route rm --profile "$p" "$host"
        ;;
      4)
        p=$(menu_cf_pick_profile) || continue
        run_cf_menu cidr list --profile "$p"
        ;;
      5)
        p=$(menu_cf_pick_profile) || continue
        read_menu_input cidr "CIDR (mis. 10.10.0.0/24 atau 192.168.1.0/24): " || continue
        [ -n "$cidr" ] || continue
        read_menu_input comment "Komentar (opsional): " || comment=""
        if [ -n "$comment" ]; then
          run_cf_menu cidr add --profile "$p" "$cidr" --comment "$comment"
        else
          run_cf_menu cidr add --profile "$p" "$cidr"
        fi
        ;;
      6)
        p=$(menu_cf_pick_profile) || continue
        read_menu_input cidr "CIDR yang dihapus: " || continue
        [ -n "$cidr" ] && run_cf_menu cidr rm --profile "$p" "$cidr"
        ;;
      0|q|Q) return 0 ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

menu_cf_access() {
  local opt p host team aud emails domains del_ans
  while true; do
    print_menu_header "CLOUDFLARE ACCESS (ZERO TRUST)"
    echo -e "  ${C_CYAN}[1]${C_RESET} Daftar Host Terproteksi Access"
    echo -e "  ${C_CYAN}[2]${C_RESET} Lindungi Host dengan Cloudflare Access"
    echo -e "  ${C_CYAN}[3]${C_RESET} Hapus Proteksi Access dari Host"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input opt "Pilih opsi [0-3]: " || break
    case "$opt" in
      1) run_cf_menu access list --all ;;
      2)
        p=$(menu_cf_pick_profile) || continue
        read_menu_input host "Host publik (mis. app.example.com): " || continue
        read_menu_input team "Zero Trust Team Name (<team>.cloudflareaccess.com): " || continue
        [ -n "$host" ] && [ -n "$team" ] || continue
        read_menu_input aud "AUD Tag (kosongkan untuk buat/ambil otomatis via API): " || aud=""
        local -a a_args=("access" "protect" "--profile" "$p" "$host" "--team" "$team")
        if [ -n "$aud" ]; then
          a_args+=("--aud" "$aud")
        else
          read_menu_input emails "Izinkan email (pisahkan koma, opsional): " || emails=""
          read_menu_input domains "Izinkan domain email (pisahkan koma, opsional): " || domains=""
          [ -n "$emails" ] && a_args+=("--allow-emails" "$emails")
          [ -n "$domains" ] && a_args+=("--allow-domains" "$domains")
        fi
        run_cf_menu "${a_args[@]}"
        ;;
      3)
        p=$(menu_cf_pick_profile) || continue
        read_menu_input host "Host publik yang dibuka proteksinya: " || continue
        [ -n "$host" ] || continue
        read_menu_input del_ans "Hapus juga Access App di Cloudflare API? [y/N]: " || del_ans="n"
        if [[ "$del_ans" =~ ^[yY] ]]; then
          run_cf_menu access unprotect --profile "$p" "$host" --delete-app
        else
          run_cf_menu access unprotect --profile "$p" "$host"
        fi
        ;;
      0|q|Q) return 0 ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

menu_cloudflare() {
  while true; do
    print_menu_header "CLOUDFLARE TUNNEL & DNS MANAGER"
    echo -e "  ${C_CYAN}[1]${C_RESET} 📊 Status Akun, Tunnel, Service & Eksposur"
    echo -e "  ${C_CYAN}[2]${C_RESET} 🌐 Eksposur Publik Vhost (dev-site)"
    echo -e "  ${C_CYAN}[3]${C_RESET} 👤 Akun Cloudflare (Daftar / Tambah / Update / Hapus)"
    echo -e "  ${C_CYAN}[4]${C_RESET} 🚇 Tunnel (Buat / Rotasi / Info)"
    echo -e "  ${C_CYAN}[5]${C_RESET} 🔄 Sinkronkan Ingress & DNS Record"
    echo -e "  ${C_CYAN}[6]${C_RESET} ⚙️  Service Cloudflared (Start / Stop / Logs)"
    echo -e "  ${C_CYAN}[7]${C_RESET} 📡 DNS Record (List / Tambah / Hapus / Sync)"
    echo -e "  ${C_CYAN}[8]${C_RESET} ⚡ Quick Tunnel (trycloudflare.com Tanpa Akun)"
    echo -e "  ${C_CYAN}[9]${C_RESET} 🛣️  Custom Routes (HTTP/TCP/SSH/RDP) & CIDR"
    echo -e "  ${C_CYAN}[10]${C_RESET} 🛡️ Cloudflare Access (Proteksi Zero Trust)"
    echo -e "  ${C_CYAN}[11]${C_RESET} 🩺 Diagnostik Konektivitas (Doctor)"
    echo -e "  ${C_CYAN}[12]${C_RESET} 📥 Install / Perbarui cloudflared"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input cf_opt "Pilih opsi [0-12]: " || break

    case $cf_opt in
      1) run_cf_menu status --all ;;
      2) menu_cf_exposures ;;
      3) menu_cf_account ;;
      4) menu_cf_tunnel ;;
      5)
        read_menu_input sync_rem "Push juga konfigurasi remote mode token via API (--remote)? [y/N]: " || sync_rem="n"
        if [[ "$sync_rem" =~ ^[yY] ]]; then
          run_cf_menu sync --all --remote
        else
          run_cf_menu sync --all
        fi
        run_cf_menu dns ensure --all 2>/dev/null || true
        ;;
      6) menu_cf_service ;;
      7) menu_cf_dns ;;
      8) menu_cf_quick ;;
      9) menu_cf_routes ;;
      10) menu_cf_access ;;
      11) run_cf_menu doctor --all ;;
      12) run_single_component "Cloudflare Tunnel (cloudflared)" step_cloudflare ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU POWERDNS & VPS EDGE GATEWAY ---
menu_powerdns() {
  ensure_pdns_mgr_helper
  local pdns_bin
  pdns_bin=$(find_pdns_mgr_bin)

  # Helper memilih domain dari zone aktif PowerDNS
  select_pdns_zone_menu() {
    local prompt_msg="${1:-Pilih nomor domain}"
    local -n _out_dom="$2"
    _out_dom=""

    local -a zones=()
    local z_list
    z_list=$("$pdns_bin" zone names 2>/dev/null || true)
    while IFS= read -r line; do
      [ -n "$line" ] && zones+=("$line")
    done <<< "$z_list"

    if [ "${#zones[@]}" -eq 0 ]; then
      echo -e "${C_YELLOW}[i] Belum ada zone domain yang terdaftar di VPS.${C_RESET}"
      echo -e "Silakan tambahkan domain terlebih dahulu lewat menu [5] (pdns-mgr zone add)."
      return 1
    fi

    echo ""
    echo -e "${C_BOLD}Daftar Domain Aktif di PowerDNS:${C_RESET}"
    for i in "${!zones[@]}"; do
      echo -e "  ${C_CYAN}[$((i + 1))]${C_RESET} ${zones[$i]}"
    done
    echo -e "  ${C_YELLOW}[0]${C_RESET} Batal / Kembali"
    echo ""

    local sel
    if [ "${#zones[@]}" -eq 1 ]; then
      read_menu_input sel "${prompt_msg} [default: 1 (${zones[0]})]: " || return 1
      sel="${sel:-1}"
    else
      read_menu_input sel "${prompt_msg} [1-${#zones[@]}]: " || return 1
    fi

    [ "$sel" = "0" ] && return 1
    [ -z "$sel" ] && return 1

    if [[ "$sel" =~ ^[0-9]+$ ]]; then
      local idx=$((sel - 1))
      if [ "$idx" -ge 0 ] && [ "$idx" -lt "${#zones[@]}" ]; then
        _out_dom="${zones[$idx]}"
        return 0
      fi
    else
      for z in "${zones[@]}"; do
        if [ "$sel" = "$z" ] || [ "${sel%.}" = "$z" ]; then
          _out_dom="$z"
          return 0
        fi
      done
    fi

    echo -e "${C_RED}Pilihan nomor domain tidak valid.${C_RESET}"
    return 1
  }

  # Helper memilih aplikasi yang diekspos di VPS
  select_pdns_proxy_menu() {
    local prompt_msg="${1:-Pilih nomor aplikasi}"
    local -n _out_proxy="$2"
    _out_proxy=""

    local -a proxies=()
    local p_list
    p_list=$("$pdns_bin" proxy-names 2>/dev/null || true)
    while IFS= read -r line; do
      [ -n "$line" ] && proxies+=("$line")
    done <<< "$p_list"

    if [ "${#proxies[@]}" -eq 0 ]; then
      echo -e "${C_YELLOW}[i] Belum ada aplikasi/domain yang sedang diekspos ke VPS.${C_RESET}"
      return 1
    fi

    echo ""
    echo -e "${C_BOLD}Daftar Aplikasi yang Diekspos di VPS:${C_RESET}"
    for i in "${!proxies[@]}"; do
      echo -e "  ${C_CYAN}[$((i + 1))]${C_RESET} ${proxies[$i]}"
    done
    echo -e "  ${C_YELLOW}[0]${C_RESET} Batal / Kembali"
    echo ""

    local sel
    if [ "${#proxies[@]}" -eq 1 ]; then
      read_menu_input sel "${prompt_msg} [default: 1 (${proxies[0]})]: " || return 1
      sel="${sel:-1}"
    else
      read_menu_input sel "${prompt_msg} [1-${#proxies[@]}]: " || return 1
    fi

    [ "$sel" = "0" ] && return 1
    [ -z "$sel" ] && return 1

    if [[ "$sel" =~ ^[0-9]+$ ]]; then
      local idx=$((sel - 1))
      if [ "$idx" -ge 0 ] && [ "$idx" -lt "${#proxies[@]}" ]; then
        _out_proxy="${proxies[$idx]}"
        return 0
      fi
    else
      for p in "${proxies[@]}"; do
        if [ "$sel" = "$p" ]; then
          _out_proxy="$p"
          return 0
        fi
      done
    fi

    echo -e "${C_RED}Pilihan nomor tidak valid.${C_RESET}"
    return 1
  }

  while true; do
    print_menu_header "POWERDNS & VPS EDGE GATEWAY (NETBIRD)"
    echo -e "  ${C_CYAN}[1]${C_RESET} 📊 Status Koneksi & Layanan VPS Edge (pdns-mgr status)"
    echo -e "  ${C_CYAN}[2]${C_RESET} 🚀 Deploy / Update ke VPS via SSH Netbird (deploy-vps / update-vps)"
    echo -e "  ${C_CYAN}[3]${C_RESET} 🔗 Hubungkan Manual ke VPS Edge (pdns-mgr connect)"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[4]${C_RESET} 📋 Daftar Zone Domain Aktif (pdns-mgr zone list)"
    echo -e "  ${C_CYAN}[5]${C_RESET} ➕ Tambah Zone Domain Baru (pdns-mgr zone add)"
    echo -e "  ${C_CYAN}[6]${C_RESET} ➖ Hapus Zone Domain (pdns-mgr zone rm)"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[7]${C_RESET} 📡 Daftar Record DNS suatu Domain (pdns-mgr record list)"
    echo -e "  ${C_CYAN}[8]${C_RESET} ✏️  Tambah / Ubah Record DNS (A / CNAME / TXT / MX)"
    echo -e "  ${C_CYAN}[9]${C_RESET} 🗑️  Hapus Record DNS (pdns-mgr record rm)"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[10]${C_RESET} 🚀 Ekspos Project ke Internet (PHP/Laravel atau Port Node/Go)"
    echo -e "  ${C_CYAN}[11]${C_RESET} 📋 Daftar Aplikasi yang Diekspos di VPS (list-exposures)"
    echo -e "  ${C_CYAN}[12]${C_RESET} 🛑 Hapus Ekspos Aplikasi dari VPS (unexpose)"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[13]${C_RESET} ✉️  Setup Mail Server & Webmail (Standarisasi Template 1-Klik)"
    echo -e "  ${C_CYAN}[14]${C_RESET} 🔑 SMTP Relay Outbound (Resend per-domain / Brevo / Custom)"
    echo -e "  ${C_CYAN}[15]${C_RESET} 👤 Tambah Akun Email Klien (user@domain.com)"
    echo -e "  ${C_CYAN}[16]${C_RESET} 📊 Status Layanan Mail Server & Webmail VPS"
    echo -e "  ${C_CYAN}[17]${C_RESET} 🛡️  Lihat / Reset Kredensial Admin Stalwart (/admin/)"
    echo -e "  ${C_CYAN}[18]${C_RESET} 📧 Daftar Akun Email per Domain (Stalwart lokal)"
    echo -e "  ${C_YELLOW}[0]${C_RESET}  Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input p_opt "Pilih opsi [0-18]: " || break

    case $p_opt in
      1)
        "$pdns_bin" status || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      2)
        "$pdns_bin" deploy-vps || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      3)
        read_menu_input in_vps "Masukkan Netbird IP VPS (contoh: 100.64.0.1): " || continue
        read_menu_input in_pub "Masukkan IP Publik VPS (contoh: 103.150.20.50): " || continue
        read_menu_input in_key "Masukkan Secret API Key PowerDNS: " || continue
        "$pdns_bin" connect --vps-ip "$in_vps" --public-ip "$in_pub" --api-key "$in_key" || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      4)
        "$pdns_bin" zone list || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      5)
        read_menu_input in_dom "Masukkan nama domain (contoh: domainanda.com): " || continue
        [ -n "$in_dom" ] || continue
        "$pdns_bin" zone add "$in_dom" || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      6)
        local in_dom=""
        select_pdns_zone_menu "Pilih nomor domain yang akan dihapus" in_dom || {
          echo ""
          read -r -p "Tekan [Enter] untuk kembali ke menu..." _
          continue
        }
        read_menu_input in_conf "Yakin ingin menghapus zone '$in_dom'? [y/N]: " || continue
        if [[ "$in_conf" =~ ^[yY] ]]; then
          "$pdns_bin" zone rm "$in_dom" || true
        fi
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      7)
        local in_dom=""
        select_pdns_zone_menu "Pilih nomor urutan domain untuk melihat record" in_dom || {
          echo ""
          read -r -p "Tekan [Enter] untuk kembali ke menu..." _
          continue
        }
        echo ""
        "$pdns_bin" record list "$in_dom" || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      8)
        local in_dom=""
        select_pdns_zone_menu "Pilih nomor domain tujuan record" in_dom || {
          echo ""
          read -r -p "Tekan [Enter] untuk kembali ke menu..." _
          continue
        }
        echo -e "Domain terpilih: ${C_BOLD}${in_dom}${C_RESET}"
        read_menu_input in_name "Masukkan host/subdomain (@ untuk apex, api, mail, dll): " || continue
        read_menu_input in_type "Tipe record (A, CNAME, TXT, MX) [default: A]: " || in_type="A"
        in_type="${in_type:-A}"
        read_menu_input in_val "Nilai record (ketik 'auto' untuk IP Publik VPS): " || in_val="auto"
        in_val="${in_val:-auto}"
        "$pdns_bin" record add "$in_dom" "$in_name" "$in_type" "$in_val" || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      9)
        local in_dom=""
        select_pdns_zone_menu "Pilih nomor domain" in_dom || {
          echo ""
          read -r -p "Tekan [Enter] untuk kembali ke menu..." _
          continue
        }
        echo ""
        "$pdns_bin" record list "$in_dom" || true
        echo ""
        read_menu_input in_name "Masukkan host/subdomain yang akan dihapus: " || continue
        read_menu_input in_type "Tipe record (A, CNAME, TXT, MX) [default: A]: " || in_type="A"
        in_type="${in_type:-A}"
        "$pdns_bin" record rm "$in_dom" "$in_name" "$in_type" || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      10)
        echo ""
        echo -e "${C_BOLD}--- WIZARD EKSPOS PROJECT KE INTERNET (VPS EDGE) ---${C_RESET}"
        echo -e "  ${C_CYAN}[1]${C_RESET} 🐘 Web PHP / Laravel / WordPress (Virtual Host Nginx Lokal)"
        echo -e "  ${C_CYAN}[2]${C_RESET} ⚡ Project Berbasis Port (Node.js, Go, Python, Docker, dsb.)"
        echo -e "  ${C_YELLOW}[0]${C_RESET} Batal"
        read_menu_input proj_type "Pilih tipe project [1/2, default: 1]: " || continue
        proj_type="${proj_type:-1}"

        if [ "$proj_type" = "1" ]; then
          # Mode PHP / Laravel / Vhost
          echo ""
          local devsite_bin
          devsite_bin=$(command -v dev-site 2>/dev/null || echo "/usr/local/bin/dev-site")
          if [ -x "$devsite_bin" ]; then
            echo -e "${C_CYAN}Daftar Virtual Host Lokal:${C_RESET}"
            "$devsite_bin" list 2>/dev/null || true
            echo ""
          fi
          read_menu_input in_local "Masukkan domain lokal (contoh: appsone.test): " || continue
          [ -n "$in_local" ] || continue

          # Cek jika vhost lokal belum ada
          if [ ! -f "/etc/nginx/sites-available/${in_local}.conf" ] && [ ! -f "/etc/nginx/sites-available/${in_local}" ]; then
            echo -e "${C_YELLOW}Vhost lokal '${in_local}' belum terdaftar. Menyiapkan vhost lokal:${C_RESET}"
            local def_folder="/var/www/${in_local%.test}"
            read_menu_input in_root "Document root folder [$def_folder]: " || in_root="$def_folder"
            in_root="${in_root:-$def_folder}"
            read_menu_input in_php "Versi PHP [8.4, 8.3, 8.2, 8.1, default: 8.4]: " || in_php="8.4"
            in_php="${in_php:-8.4}"
            "$devsite_bin" create "$in_local" "$in_root" "$in_php" auto || {
              echo "Gagal membuat vhost lokal."
              continue
            }
          fi

          # Pemilihan domain publik di VPS
          local in_pub=""
          local base_dom=""
          if select_pdns_zone_menu "Pilih domain utama untuk aplikasi" base_dom 2>/dev/null && [ -n "$base_dom" ]; then
            echo -e "Domain utama: ${C_BOLD}${base_dom}${C_RESET}"
            read_menu_input in_sub "Masukkan subdomain (contoh: apps, api, dev, atau @ untuk root domain): " || in_sub=""
            if [ -z "$in_sub" ] || [ "$in_sub" = "@" ]; then
              in_pub="$base_dom"
            else
              in_pub="${in_sub}.${base_dom}"
            fi
            echo -e "Domain publik: ${C_GREEN}${in_pub}${C_RESET}"
          else
            read_menu_input in_pub "Masukkan domain publik di VPS (contoh: apps.domainanda.com): " || continue
          fi
          [ -n "$in_pub" ] || continue

          read_menu_input in_ssl "Aktifkan SSL Let's Encrypt (HTTPS)? [Y/n]: " || in_ssl="y"
          in_ssl="${in_ssl:-y}"
          local ssl_flag=""
          [[ "$in_ssl" =~ ^[yY] ]] && ssl_flag="--ssl"

          "$pdns_bin" expose-site "$in_local" "$in_pub" $ssl_flag || true

        elif [ "$proj_type" = "2" ]; then
          # Mode Port (Node / Go / Python)
          echo ""
          local in_pub=""
          local base_dom=""
          if select_pdns_zone_menu "Pilih domain utama untuk aplikasi" base_dom 2>/dev/null && [ -n "$base_dom" ]; then
            echo -e "Domain utama: ${C_BOLD}${base_dom}${C_RESET}"
            read_menu_input in_sub "Masukkan subdomain (contoh: api, ws, dev, atau @ untuk root domain): " || in_sub=""
            if [ -z "$in_sub" ] || [ "$in_sub" = "@" ]; then
              in_pub="$base_dom"
            else
              in_pub="${in_sub}.${base_dom}"
            fi
            echo -e "Domain publik: ${C_GREEN}${in_pub}${C_RESET}"
          else
            read_menu_input in_pub "Masukkan domain publik di VPS (contoh: api.domainanda.com): " || continue
          fi
          [ -n "$in_pub" ] || continue

          read_menu_input in_port "Masukkan port lokal aplikasi Anda (contoh: 3000, 8080): " || continue
          [ -n "$in_port" ] || continue

          read_menu_input in_ssl "Aktifkan SSL Let's Encrypt (HTTPS)? [Y/n]: " || in_ssl="y"
          in_ssl="${in_ssl:-y}"
          local ssl_flag=""
          [[ "$in_ssl" =~ ^[yY] ]] && ssl_flag="--ssl"

          "$pdns_bin" expose "$in_pub" "$in_port" $ssl_flag || true
        fi
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      11)
        "$pdns_bin" list-exposures || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      12)
        local in_dom=""
        select_pdns_proxy_menu "Pilih nomor aplikasi yang ingin di-unexpose" in_dom || {
          echo ""
          read -r -p "Tekan [Enter] untuk kembali ke menu..." _
          continue
        }
        read_menu_input in_conf "Yakin ingin menghapus ekspos proxy '$in_dom' dari VPS? [y/N]: " || continue
        if [[ "$in_conf" =~ ^[yY] ]]; then
          "$pdns_bin" unexpose "$in_dom" || true
        fi
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      13)
        echo ""
        echo -e "${C_BOLD}=== STANDARISASI MAIL SERVER (HYBRID LOCAL STORAGE + VPS QUEUE) ===${C_RESET}"
        echo -e "Arsitektur ini otomatis mengonfigurasi:"
        echo -e "  • ${C_CYAN}Storage Mailbox Lokal${C_RESET} : /mnt/.../mail-data (Data 100% di harddisk lokal Anda)"
        echo -e "  • ${C_CYAN}webmail.<domain>${C_RESET}      : Web Client SnappyMail (Login Klien via Web)"
        echo -e "  • ${C_CYAN}mailadmin.<domain>${C_RESET}    : Panel Web Stalwart Admin (https://mailadmin.<domain>/admin/)"
        echo -e "  • ${C_CYAN}Akun Admin${C_RESET}           : Dibuat otomatis; kredensial disimpan di /etc/wdev/mail-admin.cred"
        echo -e "  • ${C_CYAN}VPS MX Queue Gateway${C_RESET} : Port 25 publik + Store-and-Forward queue (aman saat mati listrik)"
        echo -e "  • DNS Records          : MX 10, SPF TXT, DMARC TXT, dan A Records"
        echo ""

        local m_dom=""
        if ! select_pdns_zone_menu "Pilih domain untuk Mail Server" m_dom 2>/dev/null || [ -z "$m_dom" ]; then
          read_menu_input m_dom "Masukkan nama domain (contoh: domainanda.com): " || continue
        fi
        [ -n "$m_dom" ] || continue

        read_menu_input in_ssl "Aktifkan SSL Let's Encrypt (HTTPS & TLS)? [Y/n]: " || in_ssl="y"
        in_ssl="${in_ssl:-y}"
        local ssl_flag="--ssl"
        [[ ! "$in_ssl" =~ ^[yY] ]] && ssl_flag="--no-ssl"

        echo ""
        echo -e "${C_YELLOW}Catatan Port 25 Outbound:${C_RESET}"
        echo -e "Sebagian besar penyedia VPS memblokir port 25 outbound untuk mencegah spam."
        echo -e "Email inbound (masuk) tetap normal tanpa batas. Untuk kirim email ke Gmail/Yahoo,"
        echo -e "disarankan menggunakan SMTP Relay (Brevo, Resend, Amazon SES, Mailgun, dll)."
        echo ""
        read_menu_input use_relay "Konfigurasi SMTP Relay outbound sekarang? [y/N]: " || use_relay="n"
        local relay_flags=()
        if [[ "$use_relay" =~ ^[yY] ]]; then
          read_menu_input r_host "Host SMTP Relay (contoh: smtp-relay.brevo.com): " || continue
          [ -n "$r_host" ] || continue
          read_menu_input r_port "Port SMTP Relay [default: 587]: " || r_port="587"
          r_port="${r_port:-587}"
          read_menu_input r_user "Username / Login / API Key SMTP Relay: " || r_user=""
          read_menu_secret r_pass "Password / Secret Key SMTP Relay (input tersembunyi): " || r_pass=""

          echo ""
          echo -e "Relay akan diterapkan secara global untuk semua domain."

          relay_flags+=(--relay-host "$r_host" --relay-port "$r_port")
          [ -n "$r_user" ] && relay_flags+=(--relay-user "$r_user")
          [ -n "$r_pass" ] && relay_flags+=(--relay-pass "$r_pass")
        fi

        echo ""
        "$pdns_bin" mail setup "$m_dom" $ssl_flag "${relay_flags[@]}" || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      14)
        echo ""
        echo -e "${C_BOLD}=== SMTP RELAY OUTBOUND ===${C_RESET}"
        echo -e "  [1] Resend per-domain (API key + DNS otomatis ke PowerDNS)"
        echo -e "  [2] Custom global (Brevo, Mailgun, SES, dll)"
        echo -e "  [3] Custom per-domain (pilih domain + host/user/pass)"
        echo -e "  [4] Hapus relay per-domain"
        echo -e "  [5] Nonaktifkan relay global (kembali direct/mx)"
        echo -e "  [0] Batal"
        read_menu_input r_opt "Pilih opsi [0-5]: " || r_opt="0"
        case "$r_opt" in
          1)
            local resend_dom=""
            if ! select_pdns_zone_menu "Pilih domain untuk Resend" resend_dom 2>/dev/null || [ -z "$resend_dom" ]; then
              read_menu_input resend_dom "Masukkan domain (contoh: domainanda.com): " || resend_dom=""
            fi
            [ -n "$resend_dom" ] || { echo -e "${C_YELLOW}Dibatalkan.${C_RESET}"; continue; }

            local resend_key=""
            read_menu_secret resend_key "Masukkan API Key Resend (input tersembunyi): " || resend_key=""
            [ -n "$resend_key" ] || { echo -e "${C_YELLOW}Dibatalkan.${C_RESET}"; continue; }

            printf '%s\n' "$resend_key" | "$pdns_bin" mail relay-resend "$resend_dom" --stdin || true
            ;;
          2)
            read_menu_input r_host "Host SMTP Relay (contoh: smtp-relay.brevo.com): " || continue
            [ -n "$r_host" ] || continue
            read_menu_input r_port "Port SMTP Relay [default: 587]: " || r_port="587"
            r_port="${r_port:-587}"
            read_menu_input r_user "Username / Login SMTP Relay: " || r_user=""
            read_menu_secret r_pass "Password / Secret Key SMTP Relay (input tersembunyi): " || r_pass=""
            "$pdns_bin" mail set-relay global "$r_host" "$r_port" "$r_user" "$r_pass" || true
            ;;
          3)
            local per_dom=""
            if ! select_pdns_zone_menu "Pilih domain untuk relay per-domain" per_dom 2>/dev/null || [ -z "$per_dom" ]; then
              read_menu_input per_dom "Masukkan domain (contoh: domainanda.com): " || per_dom=""
            fi
            [ -n "$per_dom" ] || { echo -e "${C_YELLOW}Dibatalkan.${C_RESET}"; continue; }
            read_menu_input r_host "Host SMTP Relay (contoh: smtp.resend.com): " || continue
            [ -n "$r_host" ] || continue
            read_menu_input r_port "Port SMTP Relay [default: 587]: " || r_port="587"
            r_port="${r_port:-587}"
            read_menu_input r_user "Username / Login SMTP Relay: " || r_user=""
            read_menu_secret r_pass "Password / Secret Key SMTP Relay (input tersembunyi): " || r_pass=""
            "$pdns_bin" mail set-relay "$per_dom" "$r_host" "$r_port" "$r_user" "$r_pass" || true
            ;;
          4)
            local del_dom=""
            if ! select_pdns_zone_menu "Pilih domain yang relay per-domain-nya dihapus" del_dom 2>/dev/null || [ -z "$del_dom" ]; then
              read_menu_input del_dom "Masukkan domain (contoh: domainanda.com): " || del_dom=""
            fi
            [ -n "$del_dom" ] || { echo -e "${C_YELLOW}Dibatalkan.${C_RESET}"; continue; }
            "$pdns_bin" mail relay-disable "$del_dom" || true
            ;;
          5)
            "$pdns_bin" mail relay-disable || true
            ;;
          0|"") ;;
          *) echo "Pilihan tidak valid." ;;
        esac
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      15)
        echo ""
        echo -e "${C_BOLD}=== TAMBAH AKUN EMAIL KLIEN (user@domain.com) ===${C_RESET}"
        local acc_dom=""
        if ! select_pdns_zone_menu "Pilih domain untuk akun email baru" acc_dom 2>/dev/null || [ -z "$acc_dom" ]; then
          read_menu_input acc_dom "Masukkan domain (contoh: domainanda.com): " || continue
        fi
        [ -n "$acc_dom" ] || continue

        read_menu_input in_uname "Masukkan username email (contoh: info, admin, budi): " || continue
        [ -n "$in_uname" ] || continue
        in_uname="${in_uname%@*}"
        local full_email="${in_uname}@${acc_dom}"

        local in_pass="" acc_out="" acc_rc=0
        while true; do
          read_menu_secret in_pass "Masukkan password untuk '${full_email}' [min. 8 karakter, kosongkan untuk batal]: " || in_pass=""
          if [ -z "$in_pass" ]; then
            echo -e "${C_YELLOW}Tambah akun dibatalkan.${C_RESET}"
            break
          fi
          if [ "${#in_pass}" -lt 8 ]; then
            echo -e "${C_RED}Password minimal 8 karakter. Silakan coba lagi.${C_RESET}"
            continue
          fi
          echo ""
          acc_rc=0
          acc_out=$("$pdns_bin" mail add-account "$full_email" "$in_pass" 2>&1) || acc_rc=$?
          printf '%s\n' "$acc_out"
          if [ "$acc_rc" -eq 0 ]; then
            break
          fi
          if [ "$acc_rc" -eq 2 ]; then
            echo -e "${C_YELLOW}Password terlalu lemah. Masukkan password lain (hindari password umum; gabungkan beberapa kata/angka/simbol).${C_RESET}"
            echo ""
            continue
          fi
          echo -e "${C_YELLOW}Gagal menambah akun. Kembali ke menu.${C_RESET}"
          break
        done

        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      16)
        echo ""
        "$pdns_bin" mail status || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      17)
        echo ""
        echo -e "${C_BOLD}=== KREDENSIAL ADMIN STALWART (PANEL /admin/) ===${C_RESET}"
        read_menu_input a_opt "Pilih [1] Lihat kredensial, [2] Reset password admin [1/2, default: 1]: " || a_opt="1"
        a_opt="${a_opt:-1}"
        if [ "$a_opt" = "2" ]; then
          local a_dom=""
          if ! select_pdns_zone_menu "Pilih domain untuk reset admin" a_dom 2>/dev/null || [ -z "$a_dom" ]; then
            read_menu_input a_dom "Masukkan domain (contoh: domainanda.com): " || continue
          fi
          [ -n "$a_dom" ] || continue
          "$pdns_bin" mail admin-reset "$a_dom" || true
        else
          "$pdns_bin" mail admin || true
        fi
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      18)
        echo ""
        local acc_dom=""
        read_menu_input acc_dom "Filter domain (kosongkan untuk semua domain): " || acc_dom=""
        acc_dom="${acc_dom:-}"
        "$pdns_bin" mail accounts ${acc_dom:+"$acc_dom"} || true
        echo ""
        read -r -p "Tekan [Enter] untuk kembali ke menu..." _
        ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU MULTI-VERSI PHP (CLI & FPM) ---
menu_php_switch() {
  while true; do
    local cur_php="Belum terpasang"
    if command -v php >/dev/null 2>&1; then
      cur_php=$(php -r 'echo PHP_MAJOR_VERSION . "." . PHP_MINOR_VERSION;' 2>/dev/null || php -v 2>/dev/null | head -n1 | grep -oE '[0-9]+\.[0-9]+' | head -n1 || echo "Terpasang")
    fi

    print_menu_header "PENGELOLA MULTI-VERSI PHP (CLI & FPM)"
    echo -e "  ${C_BOLD}PHP CLI Aktif  :${C_RESET} ${C_GREEN}${cur_php}${C_RESET}"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[1]${C_RESET} Cek Status & Versi PHP Terpasang (php-switch list)"
    echo -e "  ${C_CYAN}[2]${C_RESET} Beralih ke PHP 8.4"
    echo -e "  ${C_CYAN}[3]${C_RESET} Beralih ke PHP 8.3"
    echo -e "  ${C_CYAN}[4]${C_RESET} Beralih ke PHP 8.2"
    echo -e "  ${C_CYAN}[5]${C_RESET} Beralih ke PHP 8.1"
    echo -e "  ${C_CYAN}[6]${C_RESET} Beralih ke Versi Tertentu (Input Manual)"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input p_opt "Pilih opsi [0-6]: " || break

    case $p_opt in
      1)
        if [ -x "/usr/local/bin/php-switch" ]; then
          php-switch list
        else
          echo "php-switch belum terpasang. Jalankan setup PHP terlebih dahulu."
        fi
        ;;
      2|3|4|5)
        local target_v="8.4"
        [ "$p_opt" = "3" ] && target_v="8.3"
        [ "$p_opt" = "4" ] && target_v="8.2"
        [ "$p_opt" = "5" ] && target_v="8.1"
        if [ -x "/usr/local/bin/php-switch" ]; then
          if [ "$EUID" -eq 0 ]; then
            php-switch "$target_v"
          else
            sudo php-switch "$target_v"
          fi
        else
          echo "php-switch belum terpasang."
        fi
        ;;
      6)
        if [ -x "/usr/local/bin/php-switch" ]; then
          php-switch list
          read_menu_input target_v "Masukkan versi target (contoh: 8.4, 8.3, 8.2, 8.1, kosongkan untuk batal): " || target_v=""
          if [ -n "$target_v" ]; then
            if [ "$EUID" -eq 0 ]; then
              php-switch "$target_v"
            else
              sudo php-switch "$target_v"
            fi
          fi
        else
          echo "php-switch belum terpasang."
        fi
        ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU MULTI-VERSI JAVA OPENJDK ---
menu_java() {
  while true; do
    local cur_jv="Belum terpasang"
    if command -v java >/dev/null 2>&1; then
      cur_jv=$(java -version 2>&1 | head -n1)
    fi

    print_menu_header "PENGELOLA MULTI-VERSI OPENJDK (JAVA)"
    echo -e "  ${C_BOLD}Java Aktif     :${C_RESET} ${C_GREEN}${cur_jv}${C_RESET}"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[1]${C_RESET} Cek Status & Lokasi Java Aktif"
    echo -e "  ${C_CYAN}[2]${C_RESET} Beralih ke OpenJDK 21 LTS (Rekomendasi Default)"
    echo -e "  ${C_CYAN}[3]${C_RESET} Beralih ke OpenJDK 17 LTS (Kompatibilitas Luas)"
    echo -e "  ${C_CYAN}[4]${C_RESET} Beralih ke OpenJDK 25 / Versi Terbaru"
    echo -e "  ${C_CYAN}[5]${C_RESET} Beralih Interaktif via alternatives"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input j_opt "Pilih opsi [0-5]: " || break

    case $j_opt in
      1)
        if command -v java >/dev/null 2>&1; then
          echo ""
          java -version
          echo "Lokasi: $(command -v java)"
          echo "Target binary: $(readlink -f "$(command -v java)" 2>/dev/null || true)"
        else
          echo "Java belum terpasang."
        fi
        ;;
      2)
        JAVA_VER_OPTION="21"
        run_single_component "OpenJDK (Java 21 LTS)" step_java
        ;;
      3)
        JAVA_VER_OPTION="17"
        run_single_component "OpenJDK (Java 17 LTS)" step_java
        ;;
      4)
        JAVA_VER_OPTION="25"
        run_single_component "OpenJDK (Java 25)" step_java
        ;;
      5)
        if command -v update-alternatives >/dev/null 2>&1; then
          if [ "$EUID" -eq 0 ]; then
            update-alternatives --config java || true
          else
            sudo update-alternatives --config java || true
          fi
        elif command -v alternatives >/dev/null 2>&1; then
          if [ "$EUID" -eq 0 ]; then
            alternatives --config java || true
          else
            sudo alternatives --config java || true
          fi
        else
          JAVA_VER_OPTION=""
          prompt_java_options
          run_single_component "OpenJDK (Java $JAVA_VER_OPTION)" step_java
        fi
        ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU PHP WEB STACK & LOCAL SSL ---
menu_php_stack() {
  local TARGET_USER="${SUDO_USER:-$USER}"
  local TARGET_HOME
  TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
  [ -z "$TARGET_HOME" ] && TARGET_HOME="$HOME"

  while true; do
    print_menu_header "WEB STACK & DOMAIN LOKAL (DEV-SITE)"
    echo -e "  ${C_CYAN}[1]${C_RESET} 🚀 Instal Full Web Stack (Nginx + PHP + Composer + SSL)"
    echo -e "  ${C_CYAN}[2]${C_RESET} ➕ Buat Virtual Host Baru (dev-site create)"
    echo -e "  ${C_CYAN}[3]${C_RESET} ➖ Hapus Virtual Host (dev-site delete)"
    echo -e "  ${C_CYAN}[4]${C_RESET} 📋 Daftar Virtual Host Aktif (dev-site list)"
    echo -e "  ${C_CYAN}[5]${C_RESET} 🔄 Sinkronisasi Hosts & SSL Browser (dev-site sync)"
    echo -e "  ${C_CYAN}[6]${C_RESET} 🐘 Kelola Versi PHP CLI & FPM (php-switch)"
    echo -e "  ${C_CYAN}[7]${C_RESET} 🌐 Ekspos Vhost Lokal ke Internet (PowerDNS & VPS Edge)"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    echo -e "  ${C_CYAN}ℹ️  Eksposur publik:${C_RESET} ${C_DIM}Gunakan menu [6] Cloudflare atau [7] PowerDNS VPS Edge${C_RESET}"
    read_menu_input p_opt "Pilih opsi [0-7]: " || break

    case $p_opt in
      1) step_php_full_stack ;;
      2)
        read_menu_input in_dom "Masukkan nama domain (contoh: myapp.test atau mygo.test): " || continue
        read_menu_input in_root "Masukkan document root ATAU port proxy (contoh: /var/www/myapp atau 3221): " || continue
        read_menu_input in_ver "Masukkan versi PHP atau ketik 'proxy' (contoh: 8.4, 8.3, atau proxy): " || in_ver=""
        if [ -n "$in_dom" ] && [ -n "$in_root" ]; then
          if [ -x "/usr/local/bin/dev-site" ]; then
            if [ "$in_ver" = "proxy" ] || [[ "$in_root" =~ ^[0-9]+$ ]]; then
              sudo dev-site create "$in_dom" "$in_root" 8.4 proxy
            else
              sudo dev-site create "$in_dom" "$in_root" "$in_ver"
            fi
          else
            echo "dev-site belum terpasang. Jalankan instalasi Full Web Stack terlebih dahulu."
          fi
        else
          echo "Domain dan Document Root / Port tidak boleh kosong."
        fi
        ;;
      3)
        read_menu_input del_dom "Masukkan nama domain yang ingin dihapus: " || continue
        if [ -n "$del_dom" ]; then
          if [ -x "/usr/local/bin/dev-site" ]; then
            sudo dev-site delete "$del_dom"
          else
            echo "dev-site belum terpasang."
          fi
        fi
        ;;
      4)
        if [ -x "/usr/local/bin/dev-site" ]; then
          dev-site list
        else
          echo "dev-site belum terpasang."
        fi
        ;;
      5)
        if [ -x "/usr/local/bin/dev-site" ]; then
          sudo dev-site sync
        elif [ -x "$TARGET_HOME/.local/bin/dev-site" ]; then
          sudo "$TARGET_HOME/.local/bin/dev-site" sync
        else
          sync_vhosts_and_hosts
          sync_browser_ssl_policies
        fi
        ;;
      6) menu_php_switch ;;
      7) menu_powerdns ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- MENU PENGELOLAAN POSTGRESQL (MULTI-VERSI) ---
menu_postgres() {
  # Jika berjalan sebagai root dan belum ada di /usr/local/bin, pasang otomatis
  if [ "$EUID" -eq 0 ] && [ ! -x "/usr/local/bin/pg-switch" ]; then
    install_pg_switch_helper
  fi

  local pg_bin=""
  if [ -x "/usr/local/bin/pg-switch" ]; then
    pg_bin="/usr/local/bin/pg-switch"
  elif [ -n "${SUDO_USER:-}" ] && [ -x "$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/pg-switch" ]; then
    pg_bin="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/pg-switch"
  elif [ -x "$HOME/.local/bin/pg-switch" ]; then
    pg_bin="$HOME/.local/bin/pg-switch"
  elif command -v pg-switch >/dev/null 2>&1; then
    pg_bin="pg-switch"
  fi

  run_pg_cmd() {
    ensure_pg_switch_helper 
    if [ ! -x "/usr/local/bin/pg-switch" ] && [ -f "$pg_bin" ]; then
      if [ "$EUID" -eq 0 ]; then
        cp -f "$pg_bin" /usr/local/bin/pg-switch 2>/dev/null && chmod 755 /usr/local/bin/pg-switch 2>/dev/null || true
        [ -x "/usr/local/bin/pg-switch" ] && pg_bin="/usr/local/bin/pg-switch"
      else
        sudo bash -c "cp -f '$pg_bin' /usr/local/bin/pg-switch && chmod 755 /usr/local/bin/pg-switch" 2>/dev/null || true
        [ -x "/usr/local/bin/pg-switch" ] && pg_bin="/usr/local/bin/pg-switch"
      fi
    fi

    if [ "$EUID" -eq 0 ]; then
      "$pg_bin" "$@"
    else
      sudo "$pg_bin" "$@"
    fi
  }

  while true; do
    local cur_pg="Belum terpasang"
    if command -v psql >/dev/null 2>&1; then
      cur_pg=$(psql --version 2>/dev/null | head -n1 || echo "Terpasang")
    fi

    print_menu_header "PENGELOLA MULTI-VERSI POSTGRESQL"
    echo -e "  ${C_BOLD}PostgreSQL Aktif :${C_RESET} ${C_GREEN}${cur_pg}${C_RESET}"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[1]${C_RESET} Cek Status, Versi & Cluster PostgreSQL (pg-switch list)"
    echo -e "  ${C_CYAN}[2]${C_RESET} Beralih ke PostgreSQL 18 (Rilis Terbaru)"
    echo -e "  ${C_CYAN}[3]${C_RESET} Beralih ke PostgreSQL 17"
    echo -e "  ${C_CYAN}[4]${C_RESET} Beralih ke PostgreSQL 16"
    echo -e "  ${C_CYAN}[5]${C_RESET} Beralih Versi & Migrasikan Seluruh Data (--migrate)"
    echo -e "  ${C_CYAN}[6]${C_RESET} Buat Snapshot Cadangan Database (Backup)"
    echo -e "  ${C_CYAN}[7]${C_RESET} Pulihkan Database dari Snapshot Cadangan"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input pg_opt "Pilih opsi [0-7]: " || break

    case $pg_opt in
      1)
        if [ -n "$pg_bin" ]; then
          "$pg_bin" list
        else
          echo "pg-switch belum terpasang."
        fi
        ;;
      2) run_pg_cmd switch 18 ;;
      3) run_pg_cmd switch 17 ;;
      4) run_pg_cmd switch 16 ;;
      5)
        read_menu_input target_v "Pilih versi target [16/17/18]: " || continue
        if [ "$target_v" = "16" ] || [ "$target_v" = "17" ] || [ "$target_v" = "18" ]; then
          run_pg_cmd switch "$target_v" --migrate
        else
          echo "Versi target tidak valid."
        fi
        ;;
      6) run_pg_cmd backup ;;
      7)
        if [ -n "$pg_bin" ]; then
          "$pg_bin" list
          read_menu_input b_file "Masukkan path lengkap file backup .sql.gz: " || continue
          [ -n "$b_file" ] && run_pg_cmd restore "$b_file"
        else
          echo "pg-switch belum terpasang."
        fi
        ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- MENU PENGELOLAAN NODE.JS (MULTI-VERSI) ---
menu_nodejs() {
  ensure_node_switch_helper

  local node_bin=""
  if [ -x "/usr/local/bin/node-switch" ]; then
    node_bin="/usr/local/bin/node-switch"
  elif [ -n "${SUDO_USER:-}" ] && [ -x "$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/node-switch" ]; then
    node_bin="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/node-switch"
  elif [ -x "$HOME/.local/bin/node-switch" ]; then
    node_bin="$HOME/.local/bin/node-switch"
  elif command -v node-switch >/dev/null 2>&1; then
    node_bin="node-switch"
  fi

  run_node_cmd() {
    ensure_node_switch_helper
    if [ "$EUID" -eq 0 ]; then
      "$node_bin" "$@"
    else
      sudo "$node_bin" "$@"
    fi
  }

  while true; do
    local cur_node="Belum terpasang"
    if command -v node >/dev/null 2>&1; then
      cur_node=$(node -v 2>/dev/null || echo "Terpasang")
    fi

    print_menu_header "PENGELOLA MULTI-VERSI NODE.JS"
    echo -e "  ${C_BOLD}Node.js Aktif  :${C_RESET} ${C_GREEN}${cur_node}${C_RESET}"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[1]${C_RESET} Cek Status & Versi Node.js Terpasang (node-switch list)"
    echo -e "  ${C_CYAN}[2]${C_RESET} Beralih ke Node.js 22 LTS (Active LTS)"
    echo -e "  ${C_CYAN}[3]${C_RESET} Beralih ke Node.js 20 LTS (Maintenance LTS)"
    echo -e "  ${C_CYAN}[4]${C_RESET} Beralih ke Node.js 24 (Current / Fitur Terbaru)"
    echo -e "  ${C_CYAN}[5]${C_RESET} Pasang / Beralih ke Versi Kustom Lainnya"
    echo -e "  ${C_CYAN}[6]${C_RESET} Hapus Versi Node.js Tertentu"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input n_opt "Pilih opsi [0-6]: " || break

    case $n_opt in
      1)
        if [ -n "$node_bin" ]; then
          "$node_bin" list
        else
          echo "node-switch belum terpasang."
        fi
        ;;
      2) run_node_cmd switch 22 ;;
      3) run_node_cmd switch 20 ;;
      4) run_node_cmd switch 24 ;;
      5)
        read_menu_input custom_v "Masukkan versi target (contoh: 18, 20.18.0, 24): " || continue
        [ -n "$custom_v" ] && run_node_cmd switch "$custom_v"
        ;;
      6)
        if [ -n "$node_bin" ]; then
          "$node_bin" list
          read_menu_input del_v "Masukkan versi yang ingin dihapus (contoh: 20): " || continue
          [ -n "$del_v" ] && run_node_cmd remove "$del_v"
        fi
        ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU STATUS & PEMBARUAN SISTEM ---
menu_status_updates() {
  while true; do
    print_menu_header "STATUS & PEMBARUAN SISTEM"
    echo -e "  ${C_CYAN}[1]${C_RESET} Cek Informasi & Status Detail Layanan"
    echo -e "      ${C_DIM}└─ Port, service systemd, URL akses LAN & Cloudflare${C_RESET}"
    echo -e "  ${C_CYAN}[2]${C_RESET} Cek Ketersediaan Pembaruan Versi"
    echo -e "      ${C_DIM}└─ Tabel perbandingan versi terpasang vs rilis upstream${C_RESET}"
    echo -e "  ${C_CYAN}[3]${C_RESET} Perbarui Seluruh Paket & Toolchain (Update All)"
    echo -e "      ${C_DIM}└─ Update paket distro, runtime, SDK, dan helper wdev${C_RESET}"
    echo -e "  ${C_CYAN}[4]${C_RESET} Validasi Sintaks & Helper (Self-Check)"
    echo -e "      ${C_DIM}└─ Tes bash -n dan shellcheck seluruh generator helper${C_RESET}"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input su_opt "Pilih opsi [0-4]: " || break

    case $su_opt in
      1) show_info ;;
      2) check_updates ;;
      3) run_update ;;
      4) self_check ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU PENGELOLA MULTI-VERSI RUNTIME ---
menu_multi_version() {
  while true; do
    print_menu_header "PENGELOLA MULTI-VERSI RUNTIME"
    echo -e "  ${C_CYAN}[1]${C_RESET} 🟢 Kelola Versi Node.js (node-switch 20 / 22 / 24)"
    echo -e "  ${C_CYAN}[2]${C_RESET} 🐘 Kelola Versi PHP CLI & FPM (php-switch 8.1 - 8.4)"
    echo -e "  ${C_CYAN}[3]${C_RESET} 🐘 Kelola Versi PostgreSQL (pg-switch 16 / 17 / 18)"
    echo -e "  ${C_CYAN}[4]${C_RESET} ☕ Kelola Versi Java OpenJDK (17 / 21 / 25)"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input mv_opt "Pilih opsi [0-4]: " || break

    case $mv_opt in
      1) menu_nodejs ;;
      2) menu_php_switch ;;
      3) menu_postgres ;;
      4) menu_java ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU TOOLCHAINS, COMPILERS & SDKS ---
menu_toolchains() {
  while true; do
    print_menu_header "TOOLCHAINS, COMPILERS & SDKS"
    echo -e "  ${C_CYAN}[1]${C_RESET} 🦀 Rust, Mold Linker & Sccache"
    echo -e "  ${C_CYAN}[2]${C_RESET} 🐹 Golang (Versi Terbaru)"
    echo -e "  ${C_CYAN}[3]${C_RESET} ☕ OpenJDK (Java JDK Multi-versi)"
    echo -e "  ${C_CYAN}[4]${C_RESET} 🎯 Flutter SDK (Channel Stable)"
    echo -e "  ${C_CYAN}[5]${C_RESET} 🤖 Android SDK & Command Line Tools"
    echo -e "  ${C_CYAN}[6]${C_RESET} ⚡ Redis / Valkey Cache Server"
    echo -e "  ${C_CYAN}[7]${C_RESET} 📦 Dependensi Dasar Sistem (Build Tools & Libs)"
    echo -e "  ${C_CYAN}[8]${C_RESET} 📋 Buka Menu Modular Komponen Lengkap (18 Item)"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input tc_opt "Pilih opsi [0-8]: " || break

    case $tc_opt in
      1) run_single_component "Rust, Mold & Sccache" step_rust ;;
      2) run_single_component "Golang" step_golang ;;
      3) run_single_component "OpenJDK (Java)" step_java ;;
      4) run_single_component "Flutter SDK" step_flutter ;;
      5) run_single_component "Android SDK & Command Line Tools" step_android_sdk ;;
      6) run_single_component "Redis / Valkey Server" step_redis ;;
      7) run_single_component "Dependensi Dasar" step_base_deps ;;
      8) menu_custom_install ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SUBMENU WORKSPACE, ADE & SHELL ---
menu_workspace_shell() {
  while true; do
    local orca_status
    orca_status="$(get_orca_status_label)"
    print_menu_header "WORKSPACE, ADE & SHELL"
    echo -e "  ${C_BOLD}Status Orca ADE:${C_RESET} ${C_GREEN}${orca_status}${C_RESET}"
    echo -e "  --------------------------------------------------------------"
    echo -e "  ${C_CYAN}[1]${C_RESET} 🐬 Kelola Orca ADE (Desktop GUI / Headless Server)"
    echo -e "      ${C_DIM}└─ Switch mode, autostart, QR pairing, commit AI & service${C_RESET}"
    echo -e "  ${C_CYAN}[2]${C_RESET} 🚀 Setup / Install Orca ADE Baru"
    echo -e "      ${C_DIM}└─ Pasang mode Desktop GUI atau Headless Server${C_RESET}"
    echo -e "  ${C_CYAN}[3]${C_RESET} 🐚 Oh My Zsh & Terminal Shell"
    echo -e "      ${C_DIM}└─ Instal / perbarui Zsh, plugin populer & konfigurasi${C_RESET}"
    echo -e "  ${C_CYAN}[4]${C_RESET} 🎨 Ganti Tema Oh My Zsh"
    echo -e "      ${C_DIM}└─ Beralih tema Powerlevel10k, Agnoster, atau Robbyrussell${C_RESET}"
    echo -e "  ${C_CYAN}[5]${C_RESET} 🗑️  Uninstall Orca ADE"
    echo -e "      ${C_DIM}└─ Hapus aplikasi Orca, service background & shortcut${C_RESET}"
    echo -e "  ${C_YELLOW}[0]${C_RESET} Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input ws_opt "Pilih opsi [0-5]: " || break

    case $ws_opt in
      1) menu_orca ;;
      2)
        if ! is_orca_installed; then
          menu_orca
        else
          echo ""
          echo "Orca ADE sudah terpasang ($orca_status)."
          read_menu_input r_opt "Pilih target instal ulang: [1] Desktop GUI, [2] Headless Server, [0] Batal: " || continue
          case "$r_opt" in
            1) setup_orca --desktop ;;
            2) setup_orca --headless ;;
            *) ;;
          esac
        fi
        ;;
      3) run_single_component "Oh My Zsh, Plugin & Tema" step_zsh ;;
      4)
        ZSH_THEME_OPTION=""
        prompt_zsh_theme
        run_single_component "Oh My Zsh ($ZSH_THEME_OPTION)" step_zsh
        ;;
      5) uninstall_orca ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- MENU INSTALASI MODULAR (PER KOMPONEN) ---
menu_custom_install() {
  while true; do
    local orca_status
    orca_status="$(get_orca_status_label)"
    print_menu_header "INSTALASI KOMPONEN MODULAR"
    echo -e "  ${C_DIM}Pilih komponen yang ingin diinstal atau diperbarui:${C_RESET}"
    echo -e "  ${C_BOLD}Core & Build Tools:${C_RESET}"
    echo -e "    ${C_CYAN}[1]${C_RESET}  Dependensi Dasar Sistem (Build Tools, curl, git, lib)"
    echo -e "    ${C_CYAN}[2]${C_RESET}  Node.js (Multi-versi via node-switch)"
    echo -e "    ${C_CYAN}[3]${C_RESET}  Rust, Mold Linker & Sccache"
    echo -e "    ${C_CYAN}[4]${C_RESET}  Golang (Versi Terbaru)"
    echo -e "    ${C_CYAN}[5]${C_RESET}  OpenJDK Java (JDK Multi-versi)"
    echo -e "  ${C_BOLD}Mobile & SDKs:${C_RESET}"
    echo -e "    ${C_CYAN}[6]${C_RESET}  Android SDK & Command Line Tools"
    echo -e "    ${C_CYAN}[7]${C_RESET}  Flutter SDK (Channel Stable)"
    echo -e "  ${C_BOLD}Database & Cache:${C_RESET}"
    echo -e "    ${C_CYAN}[8]${C_RESET}  PostgreSQL Server & Konfigurasi (Multi-versi)"
    echo -e "    ${C_CYAN}[9]${C_RESET}  Redis / Valkey Cache Server"
    echo -e "  ${C_BOLD}Web Stack & Networking:${C_RESET}"
    echo -e "    ${C_CYAN}[10]${C_RESET} Nginx Web Server (Mainline Terbaru)"
    echo -e "    ${C_CYAN}[11]${C_RESET} PHP & PHP-FPM (Multi-versi & Ekstensi)"
    echo -e "    ${C_CYAN}[12]${C_RESET} Composer (PHP Package Manager)"
    echo -e "    ${C_CYAN}[13]${C_RESET} Local SSL (mkcert) & Helper dev-site"
    echo -e "    ${C_CYAN}[14]${C_RESET} Full PHP Web Stack (Nginx + PHP + Composer + SSL)"
    echo -e "    ${C_CYAN}[15]${C_RESET} Cloudflare Tunnel & DNS (cloudflared & cf-tunnel)"
    echo -e "    ${C_CYAN}[16]${C_RESET} PowerDNS & VPS Edge Gateway (pdns-mgr & Netbird)"
    echo -e "  ${C_BOLD}Workspace, IDE & Shell:${C_RESET}"
    echo -e "    ${C_CYAN}[17]${C_RESET} Oh My Zsh (Plugin & Tema Populer)"
    echo -e "    ${C_CYAN}[18]${C_RESET} Orca ADE - Desktop GUI (Aplikasi Desktop) [$orca_status]"
    echo -e "    ${C_CYAN}[19]${C_RESET} Orca ADE - Headless Server (Service 24/7) [$orca_status]"
    echo -e "  ${C_YELLOW}[0]${C_RESET}  Kembali ke Menu Utama"
    echo -e "${C_BOLD}================================================================${C_RESET}"
    read_menu_input c_opt "Pilih komponen [0-19]: " || break

    case $c_opt in
      1) run_single_component "Dependensi Dasar" step_base_deps ;;
      2) run_single_component "Node.js (node-switch)" step_nodejs ;;
      3) run_single_component "Rust, Mold & Sccache" step_rust ;;
      4) run_single_component "Golang" step_golang ;;
      5) run_single_component "OpenJDK (Java)" step_java ;;
      6) run_single_component "Android SDK & Command Line Tools" step_android_sdk ;;
      7) run_single_component "Flutter SDK" step_flutter ;;
      8) run_single_component "PostgreSQL" step_postgres ;;
      9) run_single_component "Redis / Valkey Server" step_redis ;;
      10) run_single_component "Nginx (Mainline)" step_nginx ;;
      11) run_single_component "PHP & PHP-FPM" step_php ;;
      12) run_single_component "Composer" step_composer ;;
      13) run_single_component "Local SSL (mkcert)" step_local_ssl ;;
      14) run_single_component "Full PHP Web Stack" step_php_full_stack ;;
      15) run_single_component "Cloudflare Tunnel (cloudflared)" step_cloudflare ;;
      16) menu_powerdns ;;
      17) run_single_component "Oh My Zsh, Plugin & Tema" step_zsh ;;
      18) setup_orca --desktop ;;
      19) setup_orca --headless ;;
      0|q|Q) break ;;
      *) echo "Pilihan tidak valid." ;;
    esac
  done
}

# --- SELF-CHECK (syntax & lint helper yang di-generate) ---
self_check() {
  local tmp_dir rc=0 f
  tmp_dir=$(mktemp -d)
  echo "============================================================"
  echo " SELF-CHECK: setup_dev.sh + helper generated"
  echo "============================================================"
  if bash -n "$0"; then
    echo "[OK] syntax: setup_dev.sh"
  else
    rc=1
  fi
  install_node_switch_helper "$tmp_dir/node-switch" || rc=1
  install_pg_switch_helper "$tmp_dir/pg-switch" || rc=1
  install_php_switch_helper "$tmp_dir/php-switch" || rc=1
  install_dev_site_helper "$tmp_dir/dev-site" || rc=1
  install_cf_tunnel_helper "$tmp_dir/cf-tunnel" || rc=1
  install_pdns_mgr_helper "$tmp_dir/pdns-mgr" || rc=1
  for f in "$tmp_dir/node-switch" "$tmp_dir/pg-switch" "$tmp_dir/php-switch" "$tmp_dir/dev-site" "$tmp_dir/cf-tunnel" "$tmp_dir/pdns-mgr"; do
    if bash -n "$f"; then
      echo "[OK] syntax: $(basename "$f")"
    else
      rc=1
    fi
    if command -v shellcheck >/dev/null 2>&1; then
      if shellcheck -S error "$f"; then
        echo "[OK] shellcheck: $(basename "$f")"
      else
        rc=1
      fi
    else
      echo "[SKIP] shellcheck tidak terpasang untuk $(basename "$f")"
    fi
  done
  rm -rf "$tmp_dir"
  if [ "$rc" -eq 0 ]; then
    echo "Self-check selesai: semua helper valid."
  else
    echo "Self-check menemukan masalah."
  fi
  return "$rc"
}


# --- CLI ARGS HANDLER ---
# Flag akses LAN diproses lebih dulu agar urutan argumen tidak berpengaruh.
_filtered_args=()
for _arg in "$@"; do
  case "$_arg" in
    --lan|--lan-access) DEV_LAN_ACCESS=1 ;;
    --no-lan|--local-only) DEV_LAN_ACCESS=0 ;;
    *) _filtered_args+=("$_arg") ;;
  esac
done
set -- ${_filtered_args[@]+"${_filtered_args[@]}"}
unset _arg

# Parse optional parameter overrides
while [ $# -gt 0 ]; do
  case "$1" in
    --node-ver)
      shift
      [ $# -ge 1 ] || { echo "Error: --node-ver membutuhkan nilai"; exit 1; }
      NODE_VER_OPTION="$1"
      shift
      ;;
    --java-ver)
      shift
      [ $# -ge 1 ] || { echo "Error: --java-ver membutuhkan nilai"; exit 1; }
      JAVA_VER_OPTION="$1"
      shift
      ;;
    --android-api)
      shift
      [ $# -ge 1 ] || { echo "Error: --android-api membutuhkan nilai"; exit 1; }
      ANDROID_API_OPTION="$1"
      shift
      ;;
    --pg-ver)
      shift
      [ $# -ge 1 ] || { echo "Error: --pg-ver membutuhkan nilai"; exit 1; }
      PG_VER_OPTION="$1"
      shift
      ;;
    --php-ver)
      shift
      PHP_VER_OPTION="${1:-}"
      shift
      ;;
    --php-exts)
      shift
      [ $# -ge 1 ] || { echo "Error: --php-exts membutuhkan nilai"; exit 1; }
      PHP_EXTS_OPTION="$1"
      shift
      ;;
    --zsh-theme)
      shift
      [ $# -ge 1 ] || { echo "Error: --zsh-theme membutuhkan nilai"; exit 1; }
      ZSH_THEME_OPTION="$1"
      shift
      ;;
    *)
      break
      ;;
  esac
done

case "${1:-}" in
  --check|-c)
    show_info
    exit 0
    ;;
 --self-check)
 self_check
 exit $?
 ;;
  --check-update|-cu)
    check_updates
    exit 0
    ;;
  --install|-i|--all)
    run_installation
    exit 0
    ;;
  --update|-u)
    run_update
    exit 0
    ;;
  --base)
    run_single_component "Dependensi Dasar" step_base_deps
    exit 0
    ;;
  --node)
    run_single_component "Node.js 22" step_nodejs
    exit 0
    ;;
  --rust)
    run_single_component "Rust, Mold & Sccache" step_rust
    exit 0
    ;;
  --go)
    run_single_component "Golang" step_golang
    exit 0
    ;;
  --java)
    run_single_component "OpenJDK (Java)" step_java
    exit 0
    ;;
  --android)
    run_single_component "Android SDK & Command Line Tools" step_android_sdk
    exit 0
    ;;
  --flutter|-fl)
    run_single_component "Flutter SDK" step_flutter
    exit 0
    ;;
  --postgres|--pg)
    run_single_component "PostgreSQL" step_postgres
    exit 0
    ;;
  --redis)
    run_single_component "Redis / Valkey Server" step_redis
    exit 0
    ;;
  --nginx)
    run_single_component "Nginx (Mainline)" step_nginx
    exit 0
    ;;
  --php)
    run_single_component "PHP & PHP-FPM" step_php
    exit 0
    ;;
  --composer)
    run_single_component "Composer" step_composer
    exit 0
    ;;
  --ssl|--mkcert)
    run_single_component "Local SSL (mkcert)" step_local_ssl
    exit 0
    ;;
 --php-stack|--web-stack)
 run_single_component "Full PHP Web Stack" step_php_full_stack
 exit 0
 ;;
 --cloudflare)
 run_single_component "Cloudflare Tunnel (cloudflared)" step_cloudflare
 exit 0
 ;;
 --cf-tunnel)
 shift
 ensure_cf_tunnel_helper
 cf_bin=""
 if [ -x "/usr/local/bin/cf-tunnel" ]; then
 cf_bin="/usr/local/bin/cf-tunnel"
 elif [ -n "${SUDO_USER:-}" ] && [ -x "$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/cf-tunnel" ]; then
 cf_bin="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/cf-tunnel"
 elif [ -x "$HOME/.local/bin/cf-tunnel" ]; then
 cf_bin="$HOME/.local/bin/cf-tunnel"
 elif command -v cf-tunnel >/dev/null 2>&1; then
 cf_bin="cf-tunnel"
 fi
 if [ -n "$cf_bin" ]; then
 "$cf_bin" "$@"
 else
 echo "Helper cf-tunnel belum terpasang. Jalankan: sudo $0 --cloudflare"
 fi
 exit 0
 ;;
 --pdns|--pdns-mgr)
 shift
 ensure_pdns_mgr_helper
 pdns_bin=$(find_pdns_mgr_bin)
 "$pdns_bin" "$@"
 exit 0
 ;;
  --zsh|--ohmyzsh)
    run_single_component "Oh My Zsh, Plugin & Tema" step_zsh
    exit 0
    ;;
  --sync|--sync-hosts)
    check_root
    sync_vhosts_and_hosts
    sync_browser_ssl_policies
    exit 0
    ;;
  --dev-site)
    shift
 ensure_dev_site_helper 

    dev_bin=""
    if [ "$EUID" -ne 0 ] && [ -x "$HOME/.local/bin/dev-site" ]; then
      dev_bin="$HOME/.local/bin/dev-site"
    elif [ -x "/usr/local/bin/dev-site" ]; then
      dev_bin="/usr/local/bin/dev-site"
    elif [ -n "${SUDO_USER:-}" ] && [ -x "$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/dev-site" ]; then
      dev_bin="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/dev-site"
    elif [ -x "$HOME/.local/bin/dev-site" ]; then
      dev_bin="$HOME/.local/bin/dev-site"
    elif command -v dev-site >/dev/null 2>&1; then
      dev_bin="dev-site"
    fi

    if [ -n "$dev_bin" ]; then
      "$dev_bin" "$@"
    else
      echo "Helper dev-site belum terpasang. Jalankan: sudo $0 --ssl"
    fi
    exit 0
    ;;
  --node-switch|--nodeswitch)
    shift
    ensure_node_switch_helper
    node_bin=""
    if [ -x "/usr/local/bin/node-switch" ]; then
      node_bin="/usr/local/bin/node-switch"
    elif [ -n "${SUDO_USER:-}" ] && [ -x "$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/node-switch" ]; then
      node_bin="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/node-switch"
    elif [ -x "$HOME/.local/bin/node-switch" ]; then
      node_bin="$HOME/.local/bin/node-switch"
    elif command -v node-switch >/dev/null 2>&1; then
      node_bin="node-switch"
    else
      node_bin="/usr/local/bin/node-switch"
    fi
    "$node_bin" "$@"
    exit 0
    ;;
  --php-switch)
    shift
    if [ -x "/usr/local/bin/php-switch" ]; then
      /usr/local/bin/php-switch "$@"
    else
      echo "Helper php-switch belum terpasang. Jalankan: sudo $0 --php"
    fi
    exit 0
    ;;
  --pg-switch)
    shift
 ensure_pg_switch_helper 

    pg_bin=""
    if [ -x "/usr/local/bin/pg-switch" ]; then
      pg_bin="/usr/local/bin/pg-switch"
    elif [ -n "${SUDO_USER:-}" ] && [ -x "$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/pg-switch" ]; then
      pg_bin="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)/.local/bin/pg-switch"
    elif [ -x "$HOME/.local/bin/pg-switch" ]; then
      pg_bin="$HOME/.local/bin/pg-switch"
    elif command -v pg-switch >/dev/null 2>&1; then
      pg_bin="pg-switch"
    fi

    if [ -z "$pg_bin" ]; then
 ensure_pg_switch_helper 
      pg_bin="$HOME/.local/bin/pg-switch"
    fi

    "$pg_bin" "$@"
    exit 0
    ;;
  --orca|-o)
    setup_orca
    exit 0
    ;;
  --orca-desktop)
    setup_orca --desktop
    exit 0
    ;;
  --orca-headless)
    setup_orca --headless
    exit 0
    ;;
  --orca-uninstall)
    shift
    uninstall_orca "$@"
    exit 0
    ;;
  --orca-ai)
    configure_orca_commit_ai
    exit 0
    ;;
  --orca-pair|-p)
    shift
    show_orca_pair "$@"
    exit 0
    ;;
  --orca-switch)
    shift
    orca_switch_bin=""
    if [ -x "$HOME/.local/bin/orca-switch" ]; then
      orca_switch_bin="$HOME/.local/bin/orca-switch"
    elif [ -x "/usr/local/bin/orca-switch" ]; then
      orca_switch_bin="/usr/local/bin/orca-switch"
    fi
    if [ -n "$orca_switch_bin" ]; then
      "$orca_switch_bin" "$@"
    else
      echo "Helper orca-switch belum terpasang. Jalankan setup Orca terlebih dahulu."
    fi
    exit 0
    ;;
  --help|-h)
    echo "Penggunaan: $0 [opsi]"
    echo ""
    echo "Opsi Umum:"
    echo "  -i, --install, --all     Instal semua paket dev environment (termasuk PHP Web Stack & Zsh)"
    echo "  -u, --update             Periksa & perbarui semua paket (tampilkan tabel perbandingan)"
    echo " -cu, --check-update Cek pembaruan dalam major terpasang (Java/PG/PHP), tanpa instal"
    echo "  -c, --check              Cek informasi & detail status layanan"
 echo " --self-check Validasi sintaks & lint helper yang di-generate"
    echo "  -h, --help               Tampilkan bantuan ini"

 echo "Variabel Environment (opsional):"
 echo " REDIS_PASSWORD=... Password Redis (default: redis)"
 echo " PG_SUPERUSER_PASSWORD=... Password user postgres (default: postgres)"
 echo " DEV_LAN_ACCESS=1|0 Buka Redis/PostgreSQL ke LAN & Netbird (default: 0)"
 echo " --lan, --no-lan Aktifkan/matikan akses LAN secara eksplisit"
    echo ""
    echo "Opsi Komponen Core & Mobile:"
    echo "  --base                   Instal dependensi dasar sistem"
    echo "  --node                   Instal/perbarui Node.js (Multi-versi via node-switch)"
    echo "  --node-ver <22|20|24>    Pilih versi default Node.js saat instalasi"
    echo "  --node-switch <cmd|ver>  Jalankan helper node-switch (list, switch, install, remove)"
    echo "  --rust                   Instal/perbarui Rust, Mold, & Sccache"
    echo "  --go                     Instal/perbarui Golang"
    echo "  --java                   Instal/perbarui OpenJDK (Java JDK Multi-versi)"
    echo "  --java-ver <21|17|25>    Pilih versi default OpenJDK Java saat instalasi"
    echo "  --android                Instal/perbarui Android SDK & Command Line Tools"
    echo "  --android-api <all|36..> Pilih versi default Android SDK Platform saat instalasi"
    echo "  --flutter, -fl           Instal/perbarui Flutter SDK (Stable)"
    echo "  --postgres, --pg         Instal & konfigurasi PostgreSQL (Multi-versi)"
    echo "  --pg-ver <18|17|16>      Pilih versi default PostgreSQL saat instalasi"
    echo "  --pg-switch <cmd> ...    Jalankan helper pg-switch (list, switch, backup, restore)"
    echo "  --redis                  Instal & konfigurasi Redis / Valkey Server"
    echo ""
    echo "Opsi PHP & Web Stack (Local SSL):"
    echo "  --nginx                  Instal/perbarui Nginx (Mainline Terbaru)"
    echo "  --php                    Instal/perbarui PHP & PHP-FPM (dinamis multi-versi)"
    echo "  --php-ver <8.4|8.3|...>  Pilih versi default PHP saat instalasi"
 echo " --php-exts <full|min|..> Pilih preset atau daftar ekstensi PHP"
 echo " --dev-site <cmd> ... Jalankan helper dev-site (create, delete, list, expose, sync)"
 echo " --sync, --sync-hosts Sinkronisasi entri /etc/hosts, SSL certs, & trust store semua browser"
 echo " --php-switch <ver> Beralih versi PHP CLI & FPM aktif"
 echo ""
 echo "Opsi Cloudflare Tunnel & DNS:"
 echo " --cloudflare Instal/perbarui cloudflared + helper cf-tunnel"
 echo " --cf-tunnel <cmd> ... Jalankan helper cf-tunnel (account, tunnel, sync, dns, service, doctor)"
 echo " Contoh: sudo $0 --cf-tunnel account add cf1"
 echo " sudo $0 --dev-site expose myapp.test app-dev.example.com --profile cf1"
 echo ""
 echo "Opsi PowerDNS & VPS Edge Gateway (Netbird):"
 echo " --pdns <cmd> ... Jalankan helper pdns-mgr (status, deploy-vps, zone, record, expose)"
 echo " Contoh: sudo $0 --pdns status"
 echo " sudo $0 --pdns expose api.domainanda.com 8080"
 echo ""
    echo "Opsi Shell & Oh My Zsh:"
    echo "  --zsh, --ohmyzsh         Instal Zsh, Oh My Zsh, plugin populer & tema"
    echo "  --zsh-theme <p10k|..>    Pilih tema (powerlevel10k, agnoster, robbyrussell)"
    echo ""
    echo "Opsi Orca ADE:"
    echo "  -o, --orca               Setup Orca ADE (Otomatis: Desktop GUI jika ada DE, Headless jika tanpa DE)"
    echo "  --orca-desktop           Paksa konfigurasi Orca ADE versi Desktop GUI"
    echo "  --orca-headless          Paksa konfigurasi Orca ADE versi Headless Server"
    echo "  --orca-uninstall         Hapus instalasi Orca ADE (tambahkan --purge untuk hapus ~/.config/orca)"
    echo "  --orca-switch <mode>     Beralih mode operasi Orca ADE (desktop / headless / autostart / status)"
    echo "  --orca-ai                Konfigurasi default Git Commit Generator AI di Orca"
    echo "  -p, --orca-pair          Tampilkan QR code & URL pairing Orca Server"
    exit 0
    ;;
  "")
    # Lanjut ke menu interaktif jika tidak ada argumen
    ;;
  *)
    echo "Opsi tidak dikenal: $1"
    echo "Gunakan '$0 --help' untuk melihat daftar opsi yang tersedia."
    exit 1
    ;;
esac

# --- MENU UTAMA ---
while true; do
  print_menu_header "DEV ENVIRONMENT MANAGER"
  echo -e "  ${C_CYAN}[1]${C_RESET} 📊 Status & Pembaruan Sistem"
  echo -e "      ${C_DIM}└─ Cek status layanan, cek update versi, perbarui sistem, self-check${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[2]${C_RESET} 🔄 Pengelola Multi-Versi Runtime (Switchers)"
  echo -e "      ${C_DIM}└─ Beralih versi Node.js, PHP CLI/FPM, PostgreSQL & Java OpenJDK${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[3]${C_RESET} 🌐 Web Stack & Domain Lokal (dev-site)"
  echo -e "      ${C_DIM}└─ Kelola Virtual Host, Nginx, PHP-FPM, Local SSL (mkcert)${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[4]${C_RESET} 🛠️ Toolchains, Compilers & SDKs"
  echo -e "      ${C_DIM}└─ Rust & Sccache, Golang, Java, Flutter & Android SDK, Redis${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[5]${C_RESET} 💻 Workspace, ADE & Shell"
  echo -e "      ${C_DIM}└─ Orca ADE ($(get_orca_status_label)), Oh My Zsh & Tema${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[6]${C_RESET} ☁️  Cloudflare Tunnel & DNS"
  echo -e "      ${C_DIM}└─ Akun, tunnel, eksposur subdomain, DNS, service & diagnostik${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[7]${C_RESET} 🌐 PowerDNS & VPS Edge Gateway (Netbird)"
  echo -e "      ${C_DIM}└─ Authoritative DNS mandiri, domain publik & eksposur port aplikasi${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[8]${C_RESET} 🚀 Instalasi Lengkap (Full Auto Setup)"
  echo -e "      ${C_DIM}└─ Instal seluruh stack developer secara otomatis dari nol (14 langkah)${C_RESET}"
  echo ""
  echo -e "  ${C_CYAN}[9]${C_RESET} 📦 Instalasi Komponen Modular (19 Item)"
  echo -e "      ${C_DIM}└─ Instal atau perbarui komponen tertentu sesuai kebutuhan${C_RESET}"
  echo ""
  echo -e "  ${C_YELLOW}[0]${C_RESET} 🚪 Keluar"
  echo -e "${C_BOLD}================================================================${C_RESET}"
  read_menu_input opt "Pilih kategori [0-9]: " || exit 0

  case $opt in
    1) menu_status_updates ;;
    2) menu_multi_version ;;
    3) menu_php_stack ;;
    4) menu_toolchains ;;
    5) menu_workspace_shell ;;
    6) menu_cloudflare ;;
    7) menu_powerdns ;;
    8)
      echo ""
      read_menu_input confirm_install "Jalankan instalasi otomatis 14 langkah sekarang? [y/N]: " || confirm_install="n"
      if [[ "$confirm_install" =~ ^[yY]([eE][sS])?$ ]]; then
        run_installation
      else
        echo "Instalasi lengkap dibatalkan."
      fi
      ;;
    9) menu_custom_install ;;
    0|q|Q|exit)
      echo "Keluar."
      exit 0
      ;;
    *)
      echo "Pilihan tidak valid."
      ;;
  esac
done
