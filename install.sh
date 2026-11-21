#!/usr/bin/env bash
# ==============================================================================
# wdev Server One-Click Installer (aaPanel Style)
# ==============================================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}==================================================================${NC}"
echo -e "${CYAN}          wdev Linux Developer Control Plane Installer           ${NC}"
echo -e "${CYAN}                 (Standalone VPS Architecture)                   ${NC}"
echo -e "${CYAN}==================================================================${NC}"

# 1. Pastikan dijalankan sebagai root
if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}[ERROR] Script ini harus dijalankan sebagai root:${NC}"
  echo "        sudo bash install.sh"
  exit 1
fi

# 2. Deteksi Arsitektur
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  GOARCH="amd64" ;;
  aarch64) GOARCH="arm64" ;;
  armv7l)  GOARCH="arm" ;;
  *)
    echo -e "${RED}[ERROR] Arsitektur CPU '$ARCH' belum didukung.${NC}"
    exit 1
    ;;
esac

echo -e "${BLUE}>>> [1/4] Memeriksa dependensi sistem...${NC}"
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -y -qq
  apt-get install -y -qq curl tar sqlite3 ca-certificates ufw >/dev/null 2>&1 || true
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y curl tar sqlite ca-certificates >/dev/null 2>&1 || true
fi

echo -e "${BLUE}>>> [2/4] Menyiapkan biner dan direktori wdev...${NC}"
mkdir -p /etc/wdev /var/lib/wdev /var/log/wdev
chmod 700 /etc/wdev

# Jika ada biner lokal di folder saat ini, gunakan biner tersebut
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/wdev" ]; then
  echo "    Menyalin biner dari $SCRIPT_DIR/wdev ke /usr/local/bin/wdev..."
  cp -f "$SCRIPT_DIR/wdev" /usr/local/bin/wdev
  chmod 755 /usr/local/bin/wdev
fi

if [ ! -f "/usr/local/bin/wdev" ]; then
  echo -e "${RED}[ERROR] Biner /usr/local/bin/wdev tidak ditemukan.${NC}"
  echo "        Letakkan biner 'wdev' di folder yang sama dengan script ini."
  exit 1
fi

echo -e "${BLUE}>>> [3/4] Memasang systemd service wdev (port 3200)...${NC}"
if [ ! -f /etc/wdev/wdev.db ]; then
  /usr/local/bin/wdev panel ssl on >/dev/null 2>&1 || true
fi
/usr/local/bin/wdev install service --port 3200

echo -e "${BLUE}>>> [4/4] Memeriksa firewall (UFW)...${NC}"
if command -v ufw >/dev/null 2>&1; then
  status=$(ufw status | head -n 1 || true)
  if [[ "$status" == *"active"* ]]; then
    ufw allow 3200/tcp comment 'wdev panel' >/dev/null 2>&1 || true
    ufw allow 80/tcp comment 'HTTP' >/dev/null 2>&1 || true
    ufw allow 443/tcp comment 'HTTPS' >/dev/null 2>&1 || true
    ufw allow 53 comment 'DNS PowerDNS' >/dev/null 2>&1 || true
    ufw allow 25/tcp comment 'SMTP Inbound' >/dev/null 2>&1 || true
    ufw allow 465/tcp comment 'SMTPS' >/dev/null 2>&1 || true
    ufw allow 587/tcp comment 'Submission' >/dev/null 2>&1 || true
    ufw allow 993/tcp comment 'IMAPS' >/dev/null 2>&1 || true
    echo "    Port 3200, 80, 443, 53, 25, 465, 587, 993 dibuka di UFW."
  fi
fi

echo ""
echo -e "${GREEN}==================================================================${NC}"
echo -e "${GREEN}          PEMASANGAN WDEV PANEL DI VPS BERHASIL!                  ${NC}"
echo -e "${GREEN}==================================================================${NC}"
/usr/local/bin/wdev panel default
