#!/usr/bin/env bash
# ==============================================================================
# setup_orca_headless.sh
# Skrip instalasi & konfigurasi otomatis Orca ADE (Desktop GUI & Headless Server) untuk Linux.
# Mendukung auto-deteksi Lingkungan Desktop (DE) untuk instalasi versi Desktop.
# Dapat dijalankan tanpa akses root (user-space install) maupun dengan sudo.
# ==============================================================================

set -euo pipefail

# Mode instalasi: auto (jika ada DE -> desktop, jika tidak ada -> headless)
# Orca ADE bersifat single-instance, sehingga hanya SALAH SATU yang dipasang/dijalankan.
INSTALL_MODE="auto"
for arg in "$@"; do
    case "$arg" in
        --desktop) INSTALL_MODE="desktop" ;;
        --headless) INSTALL_MODE="headless" ;;
        --both)
            # Kompatibilitas mundur: alihkan ke auto karena single-instance lock
            INSTALL_MODE="auto"
            ;;
        --help|-h)
            echo "Penggunaan: $0 [opsi]"
            echo ""
            echo "Sistem akan otomatis memilih SALAH SATU versi:"
            echo "  - Jika Desktop Environment (DE) terdeteksi -> Pasang versi Desktop GUI"
            echo "  - Jika tanpa Desktop (Headless)            -> Pasang versi Headless Server"
            echo ""
            echo "Opsi:"
            echo "  --desktop     Paksa instal Orca ADE versi Desktop (GUI, Menu, Ikon)"
            echo "  --headless    Paksa instal Orca ADE versi Headless Server (Background Service)"
            echo "  -h, --help    Tampilkan bantuan ini"
            exit 0
            ;;
    esac
done

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

if [ "$INSTALL_MODE" = "auto" ]; then
    if [ -f "$HOME/.config/orca/.mode" ]; then
        saved_m=$(cat "$HOME/.config/orca/.mode" 2>/dev/null | tr -d '[:space:]')
        if [ "$saved_m" = "desktop" ] || [ "$saved_m" = "headless" ]; then
            INSTALL_MODE="$saved_m"
        fi
    fi
    if [ "$INSTALL_MODE" = "auto" ]; then
        if [ "$HAS_DE" -eq 1 ]; then
            INSTALL_MODE="desktop"
        else
            INSTALL_MODE="headless"
        fi
    fi
fi
mkdir -p "$HOME/.config/orca"
echo "$INSTALL_MODE" > "$HOME/.config/orca/.mode"

echo "=================================================="
if [ "$INSTALL_MODE" = "desktop" ]; then
    echo "       MEMULAI INSTALASI ORCA ADE DESKTOP GUI     "
    [ -n "$DETECTED_DE" ] && echo "       Lingkungan Desktop Terdeteksi: $DETECTED_DE"
    echo "       Mode: Desktop GUI Saja (Headless Dimatikan)"
else
    echo "    MEMULAI INSTALASI ORCA ADE HEADLESS SERVER    "
    echo "       Mode: Headless Server Saja (Tanpa Desktop)"
fi
echo "=================================================="

# 1. Deteksi dependensi dasar
MISSING_CMDS=()
for cmd in curl node; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        MISSING_CMDS+=("$cmd")
    fi
done

# Xvfb hanya diwajibkan untuk mode headless
if [ "$INSTALL_MODE" = "headless" ]; then
    if ! command -v Xvfb >/dev/null 2>&1; then
        MISSING_CMDS+=("Xvfb")
    fi
fi

if ! command -v dpkg-deb >/dev/null 2>&1 && ! command -v ar >/dev/null 2>&1; then
    MISSING_CMDS+=("dpkg-deb/ar")
fi

if [ ${#MISSING_CMDS[@]} -gt 0 ]; then
    echo "Error: Perintah/paket berikut diperlukan tetapi belum terpasang: ${MISSING_CMDS[*]}" >&2
    if command -v dnf >/dev/null 2>&1; then
        echo "Silakan pasang terlebih dahulu: sudo dnf install -y curl nodejs xorg-x11-server-Xvfb dpkg" >&2
    else
        echo "Silakan pasang terlebih dahulu: sudo apt install -y curl nodejs xvfb dpkg" >&2
    fi
    exit 1
fi

# Jika mode Desktop, pasang dependensi antarmuka GUI Desktop
if [ "$INSTALL_MODE" = "desktop" ]; then
    echo "[*] Memeriksa & memasang dependensi antarmuka GUI Desktop..."
    if command -v apt-get >/dev/null 2>&1; then
        pkgs=(libgtk-3-0t64 libnotify4 libnss3 libxss1 libxtst6 xdg-utils libsecret-1-0 libgbm1 xdotool xclip)
        if [ "$EUID" -eq 0 ]; then
            apt-get install -y "${pkgs[@]}" 2>/dev/null || apt-get install -y libgtk-3-0 libnotify4 libnss3 libxss1 libxtst6 xdg-utils libsecret-1-0 libgbm1 xdotool xclip 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            sudo apt-get install -y "${pkgs[@]}" 2>/dev/null || sudo apt-get install -y libgtk-3-0 libnotify4 libnss3 libxss1 libxtst6 xdg-utils libsecret-1-0 libgbm1 xdotool xclip 2>/dev/null || true
        fi
    elif command -v dnf >/dev/null 2>&1; then
        pkgs=(gtk3 libnotify nss libXScrnSaver libXtst xdg-utils libsecret mesa-libgbm xdotool xclip)
        if [ "$EUID" -eq 0 ]; then
            dnf install -y "${pkgs[@]}" 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            sudo dnf install -y "${pkgs[@]}" 2>/dev/null || true
        fi
    fi
fi

detect_chromium_browser() {
    local candidates=(
        "${ORCA_BROWSER_EXECUTABLE:-}"
        "/usr/bin/google-chrome"
        "/usr/bin/google-chrome-stable"
        "/usr/bin/chromium"
        "/usr/bin/chromium-browser"
        "/usr/bin/brave-browser"
        "/usr/bin/microsoft-edge-stable"
        "/usr/bin/microsoft-edge"
        "/snap/bin/chromium"
    )
    for b in "${candidates[@]}"; do
        if [ -n "$b" ] && [ -x "$b" ]; then
            echo "$b"
            return 0
        fi
    done

    for name in google-chrome google-chrome-stable chromium chromium-browser brave-browser microsoft-edge-stable microsoft-edge; do
        if command -v "$name" >/dev/null 2>&1; then
            command -v "$name"
            return 0
        fi
    done

    return 1
}

# Auto-detect atau auto-install Chromium browser untuk Orca ADE browser automation
ORCA_BROWSER=""
if ORCA_BROWSER="$(detect_chromium_browser 2>/dev/null)"; then
    echo "[✓] Browser Chromium terdeteksi: $ORCA_BROWSER"
else
    echo "[*] Browser Chromium belum terpasang. Mencoba memasang Chromium secara otomatis..."
    if command -v dnf >/dev/null 2>&1; then
        if [ "$EUID" -eq 0 ]; then
            dnf install -y chromium 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            sudo dnf install -y chromium 2>/dev/null || true
        fi
    elif command -v apt-get >/dev/null 2>&1; then
        if [ "$EUID" -eq 0 ]; then
            apt-get update -y 2>/dev/null || true
            apt-get install -y chromium 2>/dev/null || apt-get install -y chromium-browser 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            sudo apt-get update -y 2>/dev/null || true
            sudo apt-get install -y chromium 2>/dev/null || sudo apt-get install -y chromium-browser 2>/dev/null || true
        fi
    fi
    if ORCA_BROWSER="$(detect_chromium_browser 2>/dev/null)"; then
        echo "[✓] Browser Chromium berhasil dipasang: $ORCA_BROWSER"
    else
        echo "[!] Peringatan: Browser Chromium tidak ditemukan. Browser automation Orca membutuhkan Chromium/Chrome."
    fi
fi
[ -n "$ORCA_BROWSER" ] && export ORCA_BROWSER_EXECUTABLE="$ORCA_BROWSER"

extract_deb() {
    local deb_file="$1"
    local dest_dir="$2"
    mkdir -p "$dest_dir"
    if command -v dpkg-deb >/dev/null 2>&1; then
        dpkg-deb -x "$deb_file" "$dest_dir"
    elif command -v ar >/dev/null 2>&1; then
        local tmp_ar
        tmp_ar="$(mktemp -d /tmp/deb-ar-XXXXXX)"
        (cd "$tmp_ar" && ar -x "$deb_file")
        local data_archive
        data_archive="$(find "$tmp_ar" -name "data.tar.*" | head -n1)"
        if [ -n "$data_archive" ]; then
            tar -xf "$data_archive" -C "$dest_dir"
        else
            echo "Error: data.tar.* tidak ditemukan dalam berkas .deb" >&2
            rm -rf "$tmp_ar"
            return 1
        fi
        rm -rf "$tmp_ar"
    else
        echo "Error: Diperlukan 'dpkg-deb' atau 'ar' untuk mengekstrak paket .deb" >&2
        return 1
    fi
}

# 2. Deteksi arsitektur
ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
case "$ARCH" in
    amd64|x86_64) DEB_ARCH="amd64" ;;
    arm64|aarch64) DEB_ARCH="arm64" ;;
    *)
        echo "Error: Arsitektur '$ARCH' belum didukung." >&2
        exit 1
        ;;
esac
echo "[✓] Arsitektur terdeteksi: $DEB_ARCH"

# 3. Menyiapkan direktori instalasi
INSTALL_DIR="$HOME/.local/lib/orca-ide"
BIN_DIR="$HOME/.local/bin"
SYSTEMD_USER_DIR="$HOME/.config/systemd/user"
TEMP_DIR="$(mktemp -d /tmp/orca-setup-XXXXXX)"

mkdir -p "$INSTALL_DIR" "$BIN_DIR" "$SYSTEMD_USER_DIR"
trap 'rm -rf "$TEMP_DIR"' EXIT

# 4. Pastikan ~/.local/bin ada di PATH & ORCA_BROWSER_EXECUTABLE terkonfigurasi
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    export PATH="$BIN_DIR:$PATH"
    for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
        if [ -f "$rc" ] && ! grep -q 'export PATH="\$HOME/\.local/bin:\$PATH"' "$rc"; then
            echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$rc"
            echo "[✓] PATH ditambahkan ke $rc"
        fi
    done
fi

for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
    if [ -f "$rc" ] && ! grep -q 'ORCA_BROWSER_EXECUTABLE' "$rc"; then
        cat << 'EOF' >> "$rc"

# Orca ADE Browser Automation Backend
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
    for _b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge; do
        if [ -x "$_b" ]; then
            export ORCA_BROWSER_EXECUTABLE="$_b"
            break
        fi
    done
    unset _b
fi
EOF
        echo "[✓] ORCA_BROWSER_EXECUTABLE auto-detection ditambahkan ke $rc"
    fi
done

if [ -n "$ORCA_BROWSER" ] && command -v systemctl >/dev/null 2>&1; then
    systemctl --user set-environment ORCA_BROWSER_EXECUTABLE="$ORCA_BROWSER" 2>/dev/null || true
fi

# Buat /etc/profile.d/orca-browser.sh jika memiliki akses root/sudo
if [ "$EUID" -eq 0 ]; then
    cat << 'EOF' > /etc/profile.d/orca-browser.sh
# /etc/profile.d/orca-browser.sh - Set ORCA_BROWSER_EXECUTABLE for Orca ADE
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
    for _b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge; do
        if [ -x "$_b" ]; then
            export ORCA_BROWSER_EXECUTABLE="$_b"
            break
        fi
    done
    unset _b
fi
EOF
    chmod 644 /etc/profile.d/orca-browser.sh
elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    sudo tee /etc/profile.d/orca-browser.sh >/dev/null << 'EOF'
# /etc/profile.d/orca-browser.sh - Set ORCA_BROWSER_EXECUTABLE for Orca ADE
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
    for _b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge; do
        if [ -x "$_b" ]; then
            export ORCA_BROWSER_EXECUTABLE="$_b"
            break
        fi
    done
    unset _b
fi
EOF
    sudo chmod 644 /etc/profile.d/orca-browser.sh
fi

# 5. Mengambil informasi rilis terbaru dari GitHub
echo "[*] Mencari rilis Orca terbaru dari GitHub..."
RELEASE_JSON="$(curl -sL https://api.github.com/repos/stablyai/orca/releases/latest)"
LATEST_TAG="$(echo "$RELEASE_JSON" | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
LATEST_VERSION="$(echo "$LATEST_TAG" | tr -d 'v')"

if [ -z "$LATEST_VERSION" ]; then
    echo "Error: Gagal mengambil informasi rilis dari GitHub." >&2
    exit 1
fi
echo "[✓] Rilis terbaru: $LATEST_VERSION"

CURRENT_INSTALLED=""
if [ -x "$INSTALL_DIR/resources/bin/orca-ide" ]; then
    CURRENT_INSTALLED="$("$INSTALL_DIR/resources/bin/orca-ide" --version 2>&1 | tr -d 'v' | head -n 1 || true)"
fi

if [ -n "$CURRENT_INSTALLED" ] && [ "$CURRENT_INSTALLED" = "$LATEST_VERSION" ]; then
    echo "[✓] Versi $LATEST_VERSION sudah terpasang di $INSTALL_DIR (melewati pengunduhan ulang)."
else
    DOWNLOAD_URL="$(echo "$RELEASE_JSON" | grep -o 'https://[^"]*orca-ide_[^"]*'"${DEB_ARCH}"'\.deb' | head -n 1 || true)"
    if [ -z "$DOWNLOAD_URL" ]; then
        DOWNLOAD_URL="https://github.com/stablyai/orca/releases/download/${LATEST_TAG}/orca-ide_${LATEST_VERSION}_${DEB_ARCH}.deb"
    fi

    # 6. Mengunduh dan mengekstrak paket Orca
    echo "[*] Mengunduh paket: $DOWNLOAD_URL"
    curl -L --progress-bar -o "$TEMP_DIR/orca.deb" "$DOWNLOAD_URL"

    echo "[*] Mengekstrak paket aplikasi..."
    mkdir -p "$TEMP_DIR/extracted"
    extract_deb "$TEMP_DIR/orca.deb" "$TEMP_DIR/extracted"

    echo "[*] Memasang file ke $INSTALL_DIR..."
    rm -rf "$INSTALL_DIR.old"
    if [ -d "$INSTALL_DIR" ]; then
        mv "$INSTALL_DIR" "$INSTALL_DIR.old"
    fi
    mv "$TEMP_DIR/extracted/opt/Orca" "$INSTALL_DIR"
    rm -rf "$INSTALL_DIR.old"

    # Simpan resource desktop & ikon dari paket deb agar persisten
    if [ -d "$TEMP_DIR/extracted/usr/share/icons" ]; then
        mkdir -p "$INSTALL_DIR/resources/icons"
        cp -r "$TEMP_DIR/extracted/usr/share/icons/"* "$INSTALL_DIR/resources/icons/" 2>/dev/null || true
    fi
    if [ -f "$TEMP_DIR/extracted/usr/share/applications/orca-ide.desktop" ]; then
        mkdir -p "$INSTALL_DIR/resources/desktop"
        cp "$TEMP_DIR/extracted/usr/share/applications/orca-ide.desktop" "$INSTALL_DIR/resources/desktop/" 2>/dev/null || true
    fi
fi

echo "[✓] File biner Orca berhasil dipasang."

# 7. Memasang skrip 'orca-pair'
cat << 'EOF' > "$BIN_DIR/orca-pair"
#!/usr/bin/env node
const fs = require("fs");
const path = require("path");
const os = require("os");
const crypto = require("crypto");

const QR_MODULE = path.join(os.homedir(), ".local/lib/orca-ide/resources/node_modules/qrcode");
let QRCode = null;
try {
  QRCode = require(QR_MODULE);
} catch (e) {}

const orcaDir = path.join(os.homedir(), ".config/orca");
const instFile = path.join(orcaDir, "host-installation.json");
const keyFile = path.join(orcaDir, "orca-e2ee-keypair.json");
const devFile = path.join(orcaDir, "orca-devices.json");
const runFile = path.join(orcaDir, "orca-runtime.json");

function getAvailableIPs() {
  const ifaces = os.networkInterfaces();
  const ips = [];
  for (const name in ifaces) {
    for (const iface of ifaces[name]) {
      if (iface.family === "IPv4" && !iface.internal) {
        ips.push({ iface: name, ip: iface.address });
      }
    }
  }
  return ips;
}

function parseArgs() {
  const args = process.argv.slice(2);
  const options = { host: null, port: null, mobile: false, rotate: false, json: false, noQr: false, help: false };
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === "--help" || arg === "-h") options.help = true;
    else if (arg === "--json") options.json = true;
    else if (arg === "--no-qr" || arg === "-n") options.noQr = true;
    else if (arg === "--mobile" || arg === "-m") options.mobile = true;
    else if (arg === "--rotate" || arg === "-r") options.rotate = true;
    else if (arg === "--host" || arg === "-H") options.host = args[++i];
    else if (arg === "--port" || arg === "-p") options.port = parseInt(args[++i], 10);
  }
  return options;
}

function printHelp() {
  console.log(`orca pair - Menampilkan URL dan QR Code pairing Orca Server

Penggunaan:
  orca pair [opsi]
  orca-pair [opsi]

Opsi:
  -H, --host <ip/domain>  Gunakan alamat host/IP tertentu
  -p, --port <nomor>      Gunakan port tertentu (default: sesuai runtime / 6768)
  -m, --mobile            Mode pairing untuk Orca Mobile app (iOS/Android)
  -n, --no-qr             Sembunyikan QR Code (hanya tampilkan URL/teks)
  -r, --rotate            Buat token pairing baru
  --json                  Output format JSON
  -h, --help              Tampilkan bantuan ini
`);
}

async function main() {
  const options = parseArgs();
  if (options.help) return printHelp();

  if (!fs.existsSync(orcaDir) || !fs.existsSync(instFile) || !fs.existsSync(keyFile)) {
    console.error("Error: Konfigurasi Orca belum ada di ~/.config/orca.");
    console.error("Jalankan 'orca-ide serve' terlebih dahulu.");
    process.exit(1);
  }

  const instId = JSON.parse(fs.readFileSync(instFile, "utf8")).installationId;
  const publicKeyB64 = JSON.parse(fs.readFileSync(keyFile, "utf8")).publicKeyB64;

  let devices = [];
  if (fs.existsSync(devFile)) {
    try { devices = JSON.parse(fs.readFileSync(devFile, "utf8")); } catch (e) { devices = []; }
  }

  let isRunning = false;
  let port = options.port || 6768;
  if (fs.existsSync(runFile)) {
    try {
      const runData = JSON.parse(fs.readFileSync(runFile, "utf8"));
      if (runData.pid) {
        try { process.kill(runData.pid, 0); isRunning = true; } catch (e) { isRunning = false; }
      }
      if (!options.port && runData.transports) {
        const ws = runData.transports.find(t => t.kind === "websocket");
        if (ws) port = parseInt(new URL(ws.endpoint).port || 6768, 10);
      }
    } catch (e) {}
  }
  if (!isRunning) {
    try {
      const r1 = require("child_process").spawnSync("systemctl", ["is-active", "--quiet", "orca"]);
      if (r1.status === 0) isRunning = true;
    } catch (e) {}
  }
  if (!isRunning) {
    try {
      const r2 = require("child_process").spawnSync("systemctl", ["--user", "is-active", "--quiet", "orca"]);
      if (r2.status === 0) isRunning = true;
    } catch (e) {}
  }

  const netIPs = getAvailableIPs();
  let selectedHost = options.host;
  if (!selectedHost) {
    const ts = netIPs.find(n => n.iface.startsWith("wt") || n.iface.startsWith("tailscale") || n.ip.startsWith("100."));
    if (ts) selectedHost = ts.ip;
    else if (netIPs.length > 0) selectedHost = netIPs[0].ip;
    else selectedHost = "127.0.0.1";
  }

  const targetScope = options.mobile ? "mobile" : "runtime";
  let dev = null;
  if (!options.rotate) {
    dev = devices.find(d => d.scope === targetScope && d.lastSeenAt === 0 && d.token);
  }

  if (!dev) {
    dev = {
      deviceId: crypto.randomUUID(),
      token: crypto.randomBytes(24).toString("hex"),
      name: `${options.mobile ? "Mobile" : "CLI"} ${new Date().toLocaleDateString()}`,
      scope: targetScope,
      pairedAt: Date.now(),
      lastSeenAt: 0,
      pairingReach: "network"
    };
    devices.push(dev);
    fs.writeFileSync(devFile, JSON.stringify(devices, null, 2), { mode: 0o600 });
  }

  let machineBinding = undefined;
  if (targetScope === "runtime") {
    try {
      const machineId = fs.readFileSync("/etc/machine-id", "utf8").trim();
      machineBinding = crypto.createHmac("sha256", instId)
        .update(`${machineId}\0${orcaDir}`)
        .digest("base64url");
    } catch (e) {}
  }

  function buildPairing(host) {
    const ep = `ws://${host}:${port}`;
    const pl = {
      v: 2,
      endpoint: ep,
      deviceToken: dev.token,
      publicKeyB64: publicKeyB64,
      pairedDeviceId: dev.deviceId,
      scope: targetScope,
      ...(targetScope === "runtime" && machineBinding ? {
        hostDescriptor: {
          installationId: instId,
          machineBinding: machineBinding
        }
      } : {})
    };
    const c = Buffer.from(JSON.stringify(pl)).toString("base64url");
    const pUrl = `orca://pair?code=${c}`;
    const wUrl = `http://${host}:${port}/web-index.html#pairing=${encodeURIComponent(pUrl)}`;
    return { host, endpoint: ep, pairingUrl: pUrl, webClientUrl: wUrl, payload: pl };
  }

  const primaryPairing = buildPairing(selectedHost);

  if (options.json) {
    const allPairs = {};
    for (const item of netIPs) {
      allPairs[item.iface] = buildPairing(item.ip);
    }
    console.log(JSON.stringify({
      serverRunning: isRunning,
      scope: targetScope,
      host: selectedHost,
      port: port,
      endpoint: primaryPairing.endpoint,
      pairingUrl: primaryPairing.pairingUrl,
      webClientUrl: primaryPairing.webClientUrl,
      payload: primaryPairing.payload,
      allInterfaces: allPairs,
      availableNetworkIPs: netIPs
    }, null, 2));
    return;
  }

  console.log("==================================================");
  console.log(" ORCA SERVER PAIRING INFO ");
  console.log("==================================================");
  console.log(`Status Server : ${isRunning ? "ACTIVE (Berjalan)" : "INACTIVE (Mati/Belum distart)"}`);
  console.log(`Target Scope  : ${targetScope.toUpperCase()}`);
  console.log(`Active Host   : ${selectedHost}`);
  console.log(`Port          : ${port}`);
  if (netIPs.length > 0) {
    console.log("IP Jaringan   : " + netIPs.map(n => `${n.iface}: ${n.ip}`).join(", "));
  }
  console.log("--------------------------------------------------");
  console.log(`Web Client URL (Host: ${selectedHost}):`);
  console.log(primaryPairing.webClientUrl);
  console.log("--------------------------------------------------");
  console.log(`Pairing URL (Host: ${selectedHost}):`);
  console.log(primaryPairing.pairingUrl);

  if (!options.host && netIPs.length > 1) {
    console.log("--------------------------------------------------");
    console.log("Alternatif Web Client URL per Interface Jaringan:");
    for (const net of netIPs) {
      if (net.ip !== selectedHost) {
        const alt = buildPairing(net.ip);
        console.log(`[${net.iface} - ${net.ip}]:`);
        console.log(alt.webClientUrl);
      }
    }
    console.log("\nTips: Jika mengakses dari LAN lokal (Wi-Fi sama), gunakan URL interface 'ens18'.");
    console.log("Jika mengakses dari jaringan NetBird/VPN, gunakan URL interface 'wt0'.");
    console.log("Untuk ubah host aktif QR: orca pair -H <IP>");
  }

  if (QRCode && !options.noQr) {
    console.log("--------------------------------------------------");
    try {
      let qrDev = dev;
      if (targetScope !== "mobile") {
        qrDev = devices.find(d => d.scope === "mobile" && d.lastSeenAt === 0 && d.token);
        if (!qrDev) {
          qrDev = {
            deviceId: crypto.randomUUID(),
            token: crypto.randomBytes(24).toString("hex"),
            name: `Mobile ${new Date().toLocaleDateString()}`,
            scope: "mobile",
            pairedAt: Date.now(),
            lastSeenAt: 0,
            pairingReach: "network"
          };
          devices.push(qrDev);
          fs.writeFileSync(devFile, JSON.stringify(devices, null, 2), { mode: 0o600 });
        }
      }

      const qrPayload = {
        v: 2,
        endpoint: `ws://${selectedHost}:${port}`,
        deviceToken: qrDev.token,
        publicKeyB64: publicKeyB64,
        pairedDeviceId: qrDev.deviceId,
        scope: "mobile"
      };
      const qrCode = Buffer.from(JSON.stringify(qrPayload)).toString("base64url");
      const qrPairingUrl = `orca://pair?code=${qrCode}`;

      const qrStr = await new Promise((resolve, reject) => {
        QRCode.toString(qrPairingUrl, {
          type: "terminal",
          small: true,
          errorCorrectionLevel: "L",
          margin: 1
        }, (err, str) => {
          if (err) reject(err);
          else resolve(str);
        });
      });
      console.log(`QR Code Ringkas (Host: ${selectedHost} - scan dengan Orca Mobile / HP):`);
      console.log(qrStr);
    } catch (e) {}
  }

  if (!isRunning) {
    console.log("Catatan: Server belum berjalan. Periksa status dengan: systemctl status orca");
  }
}

main().catch(err => {
  console.error("Error:", err.message);
  process.exit(1);
});
EOF
chmod +x "$BIN_DIR/orca-pair"
echo "[✓] Skrip orca-pair terpasang."

# 8. Memasang skrip 'orca-update'
cat << 'EOF' > "$BIN_DIR/orca-update"
#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$HOME/.local/lib/orca-ide"
TEMP_DIR="/tmp/orca-update-$$"

ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
case "$ARCH" in
    amd64|x86_64) DEB_ARCH="amd64" ;;
    arm64|aarch64) DEB_ARCH="arm64" ;;
    *) echo "Arsitektur tidak didukung: $ARCH" >&2; exit 1 ;;
esac

CURRENT_VERSION=""
if [ -x "$INSTALL_DIR/resources/bin/orca-ide" ]; then
    CURRENT_VERSION="$("$INSTALL_DIR/resources/bin/orca-ide" --version 2>&1 | tr -d 'v' | head -n 1)"
fi

CHECK_ONLY=false
FORCE=false

for arg in "$@"; do
    case "$arg" in
        --check|-c) CHECK_ONLY=true ;;
        --force|-f) FORCE=true ;;
        --help|-h)
            echo "Penggunaan: orca update [opsi]"
            echo ""
            echo "Opsi:"
            echo "  -c, --check    Hanya cek pembaruan tanpa mengunduh"
            echo "  -f, --force    Paksa update ulang meskipun versi sama"
            echo "  -h, --help     Tampilkan bantuan ini"
            exit 0
            ;;
    esac
done

echo "Memeriksa rilis terbaru dari GitHub..."
RELEASE_JSON="$(curl -sL https://api.github.com/repos/stablyai/orca/releases/latest)"
LATEST_TAG="$(echo "$RELEASE_JSON" | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
LATEST_VERSION="$(echo "$LATEST_TAG" | tr -d 'v')"

if [ -z "$LATEST_VERSION" ]; then
    echo "Gagal mendapatkan informasi rilis terbaru dari GitHub." >&2
    exit 1
fi

echo "Versi terpasang : ${CURRENT_VERSION:-tidak diketahui}"
echo "Versi terbaru   : $LATEST_VERSION"

if [ "$CURRENT_VERSION" = "$LATEST_VERSION" ] && [ "$FORCE" = false ]; then
    echo "Orca sudah berada di versi paling mutakhir."
    exit 0
fi

if [ "$CHECK_ONLY" = true ]; then
    if [ "$CURRENT_VERSION" != "$LATEST_VERSION" ]; then
        echo "Tersedia pembaruan baru: $CURRENT_VERSION -> $LATEST_VERSION"
        echo "Jalankan 'orca update' untuk mengunduh dan memasang."
    fi
    exit 0
fi

DOWNLOAD_URL="$(echo "$RELEASE_JSON" | grep -o 'https://[^"]*orca-ide_[^"]*'"${DEB_ARCH}"'\.deb' | head -n 1 || true)"
if [ -z "$DOWNLOAD_URL" ]; then
    DOWNLOAD_URL="https://github.com/stablyai/orca/releases/download/${LATEST_TAG}/orca-ide_${LATEST_VERSION}_${DEB_ARCH}.deb"
fi

echo "Mengunduh pembaruan dari: $DOWNLOAD_URL"
mkdir -p "$TEMP_DIR"
trap 'rm -rf "$TEMP_DIR"' EXIT

curl -L -o "$TEMP_DIR/orca.deb" "$DOWNLOAD_URL"

extract_deb() {
    local deb_file="\$1"
    local dest_dir="\$2"
    mkdir -p "\$dest_dir"
    if command -v dpkg-deb >/dev/null 2>&1; then
        dpkg-deb -x "\$deb_file" "\$dest_dir"
    elif command -v ar >/dev/null 2>&1; then
        local tmp_ar
        tmp_ar="\$(mktemp -d /tmp/deb-ar-XXXXXX)"
        (cd "\$tmp_ar" && ar -x "\$deb_file")
        local data_archive
        data_archive="\$(find "\$tmp_ar" -name "data.tar.*" | head -n1)"
        if [ -n "\$data_archive" ]; then
            tar -xf "\$data_archive" -C "\$dest_dir"
        else
            echo "Error: data.tar.* tidak ditemukan dalam berkas .deb" >&2
            rm -rf "\$tmp_ar"
            return 1
        fi
        rm -rf "\$tmp_ar"
    else
        echo "Error: Diperlukan 'dpkg-deb' atau 'ar' untuk mengekstrak paket .deb" >&2
        return 1
    fi
}

echo "Mengekstrak paket..."
mkdir -p "$TEMP_DIR/extracted"
extract_deb "$TEMP_DIR/orca.deb" "$TEMP_DIR/extracted"

echo "Menerapkan pembaruan ke $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
rm -rf "$INSTALL_DIR.old"
if [ -d "$INSTALL_DIR" ]; then
  mv "$INSTALL_DIR" "$INSTALL_DIR.old"
fi
mv "$TEMP_DIR/extracted/opt/Orca" "$INSTALL_DIR"
rm -rf "$INSTALL_DIR.old"

NEW_VERSION="$("$INSTALL_DIR/resources/bin/orca-ide" --version 2>&1 | tr -d 'v' | head -n 1)"
echo "Berhasil diperbarui ke versi $NEW_VERSION."

if systemctl --user is-active --quiet orca 2>/dev/null; then
    echo "Merestart service systemd user orca..."
    systemctl --user restart orca
    echo "Service orca berhasil direstart."
elif systemctl is-active --quiet orca 2>/dev/null; then
    echo "Merestart service systemd orca..."
    sudo systemctl restart orca 2>/dev/null || systemctl restart orca 2>/dev/null || true
    echo "Service orca berhasil direstart."
fi
EOF
chmod +x "$BIN_DIR/orca-update"
echo "[✓] Skrip orca-update terpasang."

# 8.5 Memasang skrip 'orca-switch' (Pengalih Mode Desktop GUI <-> Headless Server)
cat << 'EOF' > "$BIN_DIR/orca-switch"
#!/usr/bin/env bash
# ==============================================================================
# orca-switch - Pengalih Mode Operasi Orca ADE (Desktop GUI <-> Headless Server)
# Memungkinkan pengguna beralih antara Mode Desktop GUI dan Mode Headless Server
# secara eksklusif (hanya salah satu yang berjalan), bahkan saat DE terpasang.
# ==============================================================================

set -euo pipefail

TARGET_USER="${SUDO_USER:-$(id -un)}"
TARGET_HOME=$(getent passwd "$TARGET_USER" 2>/dev/null | cut -d: -f6)
[ -z "$TARGET_HOME" ] && TARGET_HOME="${HOME:-/home/$TARGET_USER}"

CONFIG_DIR="$TARGET_HOME/.config/orca"
MODE_FILE="$CONFIG_DIR/.mode"
BIN_DIR="$TARGET_HOME/.local/bin"
SYSTEMD_USER_DIR="$TARGET_HOME/.config/systemd/user"
AUTOSTART_DIR="$TARGET_HOME/.config/autostart"
APPS_DIR="$TARGET_HOME/.local/share/applications"

mkdir -p "$CONFIG_DIR" "$BIN_DIR" "$AUTOSTART_DIR"

detect_browser() {
    if [ -n "${ORCA_BROWSER_EXECUTABLE:-}" ] && [ -x "$ORCA_BROWSER_EXECUTABLE" ]; then
        echo "$ORCA_BROWSER_EXECUTABLE"
        return 0
    fi
    for b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge /snap/bin/chromium; do
        if [ -x "$b" ]; then
            echo "$b"
            return 0
        fi
    done
    echo "/usr/bin/chromium"
}

get_current_mode() {
    if [ -f "$MODE_FILE" ]; then
        local m
        m="$(cat "$MODE_FILE" 2>/dev/null | tr -d '[:space:]')"
        if [ "$m" = "desktop" ] || [ "$m" = "headless" ]; then
            echo "$m"
            return 0
        fi
    fi

    # Fallback jika belum terekam
    if pgrep -x "orca-ide" >/dev/null 2>&1 || pgrep -f "/orca-ide/orca-ide" >/dev/null 2>&1; then
        echo "desktop"
    elif systemctl --user is-active --quiet orca 2>/dev/null || pgrep -f "orca-ide.*serve|orcad-server" >/dev/null 2>&1; then
        echo "headless"
    else
        # Default berbasis ketersediaan DE
        if [ -n "${XDG_CURRENT_DESKTOP:-${DESKTOP_SESSION:-}}" ] || [ -d "/usr/share/xsessions" ] || command -v lxsession >/dev/null 2>&1; then
            echo "desktop"
        else
            echo "headless"
        fi
    fi
}

switch_to_desktop() {
    echo "=================================================="
    echo "    BERALIH KE MODE ORCA ADE DESKTOP GUI          "
    echo "=================================================="
    echo "[*] Menghentikan Headless Server jika sedang berjalan..."

    # 1. Hentikan dan nonaktifkan systemd service headless
    systemctl --user stop orca.service 2>/dev/null || true
    systemctl --user disable orca.service 2>/dev/null || true
    if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        sudo systemctl stop orca.service 2>/dev/null || true
        sudo systemctl disable orca.service 2>/dev/null || true
    fi
    pkill -f "orca-ide.*serve|orcad-server" 2>/dev/null || true
    sleep 1

    # 2. Pastikan file mode tercatat 'desktop'
    echo "desktop" > "$MODE_FILE"

    # 3. Pastikan desktop launcher dan icon tersedia
    if [ ! -f "$APPS_DIR/orca-ide.desktop" ] && [ -f /usr/share/applications/orca-ide.desktop ]; then
        cp /usr/share/applications/orca-ide.desktop "$APPS_DIR/orca-ide.desktop" 2>/dev/null || true
    fi

    echo ""
    echo "[✓] Berhasil beralih ke Mode DESKTOP GUI!"
    echo "  - Headless background service telah dimatikan & dinonaktifkan."
    echo "  - Orca ADE kini beroperasi murni melalui antarmuka Desktop GUI."
    echo ""
    echo "Cara Membuka Orca Desktop:"
    echo "  1. Dari Menu Aplikasi Desktop -> Development -> Orca IDE"
    echo "  2. Dari terminal: orca-desktop (atau 'orca desktop' / 'orca start')"
    echo ""

    if [ -n "${DISPLAY:-}" ] || [ -n "${WAYLAND_DISPLAY:-}" ]; then
        if [ "${1:-}" = "--launch" ] || [ "${1:-}" = "-l" ]; then
            echo "[*] Meluncurkan Orca Desktop GUI..."
            nohup "$BIN_DIR/orca-desktop" >/dev/null 2>&1 &
            echo "[✓] Orca Desktop berhasil diluncurkan di layar aktif."
        fi
    fi
}

switch_to_headless() {
    echo "=================================================="
    echo "    BERALIH KE MODE ORCA ADE HEADLESS SERVER      "
    echo "=================================================="
    echo "[*] Menghentikan Orca Desktop GUI jika sedang berjalan..."

    # 1. Tutup proses Desktop GUI
    pkill -x "orca-ide" 2>/dev/null || true
    pkill -f "/orca-ide/orca-ide" 2>/dev/null || true
    sleep 1

    # 2. Hapus autostart desktop agar tidak membuka window saat login
    rm -f "$AUTOSTART_DIR/orca-ide.desktop" 2>/dev/null || true

    # 3. Aktifkan linger agar systemd user service hidup saat boot tanpa perlu login
    if command -v loginctl >/dev/null 2>&1; then
        echo "[*] Mengaktifkan systemd lingering untuk user '$TARGET_USER' (autostart saat boot)..."
        if [ "$EUID" -eq 0 ]; then
            loginctl enable-linger "$TARGET_USER" 2>/dev/null || true
        elif command -v sudo >/dev/null 2>&1; then
            sudo loginctl enable-linger "$TARGET_USER" 2>/dev/null || true
        fi
    fi

    # 4. Buat / perbarui unit systemd user service
    local browser_bin
    browser_bin="$(detect_browser)"
    mkdir -p "$SYSTEMD_USER_DIR"
    cat << SVCEOF > "$SYSTEMD_USER_DIR/orca.service"
[Unit]
Description=Orca ADE Headless Server
After=network.target

[Service]
Type=simple
ExecStart=$BIN_DIR/orca-ide serve --supervise --force
Restart=always
RestartSec=5
Environment="PATH=$BIN_DIR:/usr/local/bin:/usr/bin:/bin"
Environment="ORCA_ALLOW_INSECURE_WEB_PAIRING=1"
Environment="ORCA_BROWSER_EXECUTABLE=$browser_bin"

[Install]
WantedBy=default.target
SVCEOF

    cat << SVCEOF > "$SYSTEMD_USER_DIR/orca-update.service"
[Unit]
Description=Check and apply Orca ADE updates
After=network.target

[Service]
Type=oneshot
ExecStart=$BIN_DIR/orca-update
SVCEOF

    cat << SVCEOF > "$SYSTEMD_USER_DIR/orca-update.timer"
[Unit]
Description=Daily update check for Orca ADE

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=1h

[Install]
WantedBy=timers.target
SVCEOF

    # 5. Reload dan nyalakan service
    echo "[*] Mengaktifkan dan menjalankan orca.service di latar belakang..."
    systemctl --user daemon-reload 2>/dev/null || true
    systemctl --user enable orca.service 2>/dev/null || true
    systemctl --user restart orca.service 2>/dev/null || true
    systemctl --user enable orca-update.timer 2>/dev/null || true
    systemctl --user restart orca-update.timer 2>/dev/null || true

    # 6. Catat mode aktif
    echo "headless" > "$MODE_FILE"

    echo ""
    echo "[✓] Berhasil beralih ke Mode HEADLESS SERVER!"
    echo "  - Orca Desktop GUI telah ditutup."
    echo "  - Layanan 'orca.service' aktif dan otomatis berjalan saat Linux restart (tanpa perlu login)."
    echo "  - Siap diakses via Web Client atau pairing Orca Mobile."
    echo ""

    # Tunggu runtime file sebentar lalu tampilkan pairing info
    sleep 2
    if [ -x "$BIN_DIR/orca-pair" ]; then
        "$BIN_DIR/orca-pair" || true
    fi
}

toggle_autostart() {
    local target="${1:-toggle}"
    local desktop_src="$APPS_DIR/orca-ide.desktop"
    [ ! -f "$desktop_src" ] && desktop_src="/usr/share/applications/orca-ide.desktop"
    local autostart_file="$AUTOSTART_DIR/orca-ide.desktop"

    if [ "$target" = "enable" ] || ([ "$target" = "toggle" ] && [ ! -f "$autostart_file" ]); then
        if [ -f "$desktop_src" ]; then
            cp "$desktop_src" "$autostart_file"
            echo "[✓] Autostart Desktop DIAKTIFKAN."
            echo "    Orca Desktop GUI akan otomatis terbuka setiap kali sesi desktop/RDP dimulai."
        else
            echo "Error: File $desktop_src tidak ditemukan."
            return 1
        fi
    else
        rm -f "$autostart_file"
        echo "[✓] Autostart Desktop DINONAKTIFKAN."
        echo "    Orca Desktop GUI tidak akan terbuka otomatis saat sesi desktop dimulai."
    fi
}

show_status() {
    local cur_mode
    cur_mode="$(get_current_mode)"
    echo "=================================================="
    echo "           STATUS OPERASI ORCA ADE                "
    echo "=================================================="
    echo "Mode Terkonfigurasi : $([ "$cur_mode" = "desktop" ] && echo "DESKTOP GUI" || echo "HEADLESS SERVER")"
    
    local de_detected="Tidak (Headless)"
    if [ -n "${XDG_CURRENT_DESKTOP:-${DESKTOP_SESSION:-}}" ]; then
        de_detected="Ya (${XDG_CURRENT_DESKTOP:-$DESKTOP_SESSION})"
    elif [ -d "/usr/share/xsessions" ]; then
        de_detected="Ya ($(ls -1 /usr/share/xsessions/*.desktop 2>/dev/null | head -n1 | xargs -n1 basename 2>/dev/null || echo 'X11 Session'))"
    fi
    echo "Desktop Environment : $de_detected"

    if pgrep -x "orca-ide" >/dev/null 2>&1 || pgrep -f "/orca-ide/orca-ide" >/dev/null 2>&1; then
        local gpid
        gpid=$(pgrep -f "/orca-ide/orca-ide" 2>/dev/null | head -n1 || echo "-")
        echo "Status Desktop GUI  : RUNNING (Active, PID: $gpid)"
    else
        echo "Status Desktop GUI  : INACTIVE"
    fi

    if systemctl --user is-active --quiet orca 2>/dev/null; then
        echo "Status Headless Svc : RUNNING (Active - systemd user orca.service)"
    elif pgrep -f "orca-ide.*serve|orcad-server" >/dev/null 2>&1; then
        echo "Status Headless Svc : RUNNING (Background process)"
    else
        echo "Status Headless Svc : INACTIVE"
    fi

    local autostart_status="Nonaktif"
    [ -f "$AUTOSTART_DIR/orca-ide.desktop" ] && autostart_status="Aktif (~/.config/autostart/orca-ide.desktop)"
    echo "Desktop Autostart   : $autostart_status"

    local linger_status="Tidak"
    if command -v loginctl >/dev/null 2>&1; then
        loginctl show-user "$TARGET_USER" 2>/dev/null | grep -q "Linger=yes" && linger_status="Ya (Layanan user aktif saat boot)"
    fi
    echo "Systemd Lingering   : $linger_status"
    echo "=================================================="
}

interactive_menu() {
    while true; do
        local cur_mode
        cur_mode="$(get_current_mode)"
        echo ""
        echo "=================================================="
        echo "       PENGALIH MODE ORCA ADE (ORCA-SWITCH)       "
        echo "=================================================="
        echo "Mode Aktif Saat Ini : $([ "$cur_mode" = "desktop" ] && echo "[ DESKTOP GUI ]" || echo "[ HEADLESS SERVER ]")"
        echo "--------------------------------------------------"
        echo "1. Beralih ke Mode Desktop GUI"
        echo "   (Menutup background service, mengaktifkan antarmuka visual IDE)"
        echo "2. Beralih ke Mode Headless Server"
        echo "   (Menutup GUI, mengaktifkan orca.service 24/7 di boot tanpa login)"
        echo "3. Toggle Desktop Autostart $([ -f "$AUTOSTART_DIR/orca-ide.desktop" ] && echo '[Saat ini: AKTIF]' || echo '[Saat ini: NONAKTIF]')"
        echo "4. Cek Status Lengkap & Informasi Pairing"
        echo "5. Keluar"
        echo "=================================================="
        read -rp "Pilih opsi [1-5]: " s_opt

        case "$s_opt" in
            1)
                switch_to_desktop
                break
                ;;
            2)
                switch_to_headless
                break
                ;;
            3)
                toggle_autostart toggle
                ;;
            4)
                show_status
                if [ "$cur_mode" = "headless" ] && [ -x "$BIN_DIR/orca-pair" ]; then
                    "$BIN_DIR/orca-pair" || true
                fi
                ;;
            5)
                break
                ;;
            *)
                echo "Pilihan tidak valid."
                ;;
        esac
    done
}

# --- CLI DISPATCHER ---
action="${1:-}"
case "$action" in
    desktop|gui)
        shift
        switch_to_desktop "$@"
        ;;
    headless|server)
        shift
        switch_to_headless "$@"
        ;;
    autostart)
        shift
        toggle_autostart "${1:-toggle}"
        ;;
    status)
        show_status
        ;;
    help|--help|-h)
        echo "Penggunaan: orca switch [desktop|headless|autostart|status]"
        echo ""
        echo "Perintah:"
        echo "  desktop, gui       Beralih ke mode Desktop GUI (matikan server background)"
        echo "  headless, server   Beralih ke mode Headless Server (matikan GUI, aktifkan orca.service di boot)"
        echo "  autostart [en|dis] Aktifkan / nonaktifkan autostart Orca saat desktop dibuka"
        echo "  status             Tampilkan status mode, proses, dan layanan aktif"
        echo "  (tanpa argumen)    Buka menu interaktif pengalih mode"
        ;;
    *)
        if [ -n "$action" ]; then
            echo "Opsi tidak dikenal: $action"
            echo "Jalankan 'orca switch --help' untuk bantuan."
            exit 1
        fi
        interactive_menu
        ;;
esac
EOF
chmod +x "$BIN_DIR/orca-switch"
if [ "$EUID" -eq 0 ]; then
    cp "$BIN_DIR/orca-switch" /usr/local/bin/orca-switch 2>/dev/null || true
    chmod +x /usr/local/bin/orca-switch 2>/dev/null || true
elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    sudo cp "$BIN_DIR/orca-switch" /usr/local/bin/orca-switch 2>/dev/null || true
    sudo chmod +x /usr/local/bin/orca-switch 2>/dev/null || true
fi
echo "[✓] Skrip orca-switch terpasang."

# 9. Memasang wrapper 'orca' dan 'orca-ide'
cat << 'EOF' > "$BIN_DIR/orca"
#!/usr/bin/env bash
set -euo pipefail

REAL_ORCA="$HOME/.local/lib/orca-ide/resources/bin/orca-ide"
ORCA_PAIR="$HOME/.local/bin/orca-pair"
ORCA_UPDATE="$HOME/.local/bin/orca-update"
ORCA_DESKTOP="$HOME/.local/bin/orca-desktop"
ORCA_SWITCH="$HOME/.local/bin/orca-switch"

# Auto-detect Chromium browser untuk backend Browser Automation Orca ADE
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
    for _b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge /snap/bin/chromium; do
        if [ -x "$_b" ]; then
            export ORCA_BROWSER_EXECUTABLE="$_b"
            break
        fi
    done
    if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
        for _name in google-chrome google-chrome-stable chromium chromium-browser brave-browser microsoft-edge-stable; do
            if command -v "$_name" >/dev/null 2>&1; then
                export ORCA_BROWSER_EXECUTABLE="$(command -v "$_name")"
                break
            fi
        done
    fi
    unset _b _name
fi

has_display() {
    [ -n "${DISPLAY:-}" ] || [ -n "${WAYLAND_DISPLAY:-}" ]
}

stop_all_orca() {
    local stopped=0
    if pgrep -x "orca-ide" >/dev/null 2>&1 || pgrep -f "/orca-ide/orca-ide" >/dev/null 2>&1; then
        echo "[*] Menghentikan proses Orca Desktop GUI..."
        pkill -x "orca-ide" 2>/dev/null || true
        pkill -f "/orca-ide/orca-ide" 2>/dev/null || true
        stopped=1
    fi
    if systemctl --user is-active --quiet orca 2>/dev/null; then
        echo "[*] Menghentikan service systemd user orca..."
        systemctl --user stop orca.service 2>/dev/null || true
        stopped=1
    fi
    if command -v sudo >/dev/null 2>&1 && sudo systemctl is-active --quiet orca 2>/dev/null; then
        echo "[*] Menghentikan service systemd system orca..."
        sudo systemctl stop orca.service 2>/dev/null || true
        stopped=1
    fi
    if pgrep -f "orca-ide.*serve|orcad-server" >/dev/null 2>&1; then
        echo "[*] Menghentikan proses orca serve background..."
        pkill -f "orca-ide.*serve|orcad-server" 2>/dev/null || true
        stopped=1
    fi
    if [ "$stopped" -eq 1 ]; then
        echo "[✓] Semua proses Orca ADE berhasil dihentikan."
    else
        echo "[i] Tidak ada proses Orca ADE yang sedang berjalan."
    fi
}

cmd="${1:-}"
case "$cmd" in
    switch|mode)
        shift
        exec "$ORCA_SWITCH" "$@"
        ;;
    autostart)
        shift
        exec "$ORCA_SWITCH" autostart "$@"
        ;;
    status)
        shift
        "$ORCA_SWITCH" status
        echo ""
        echo "Detail Runtime:"
        exec "$REAL_ORCA" status "$@"
        ;;
    pair)
        shift
        exec "$ORCA_PAIR" "$@"
        ;;
    update)
        shift
        exec "$ORCA_UPDATE" "$@"
        ;;
    desktop|gui)
        shift
        exec "$ORCA_DESKTOP" "$@"
        ;;
    server|headless)
        shift
        subcmd="${1:-}"
        case "$subcmd" in
            start)
                if pgrep -x "orca-ide" >/dev/null 2>&1; then
                    echo "[*] Menghentikan Orca Desktop GUI sebelum menyalakan Headless Server..."
                    pkill -x "orca-ide" 2>/dev/null || true
                    sleep 1
                fi
                if systemctl --user list-unit-files orca.service >/dev/null 2>&1; then
                    systemctl --user start orca.service
                    echo "[✓] Layanan Orca Headless Server (systemd user) dijalankan."
                elif [ -f /etc/systemd/system/orca.service ]; then
                    sudo systemctl start orca.service 2>/dev/null || systemctl start orca.service
                    echo "[✓] Layanan Orca Headless Server (system) dijalankan."
                else
                    nohup "$REAL_ORCA" serve --supervise --force > "$HOME/.config/orca/orca.log" 2>&1 &
                    echo "[✓] Orca Headless Server dijalankan di background."
                fi
                ;;
            stop)
                systemctl --user stop orca.service 2>/dev/null || true
                sudo systemctl stop orca.service 2>/dev/null || true
                pkill -f "orca-ide.*serve|orcad-server" 2>/dev/null || true
                echo "[✓] Layanan Orca Headless Server dihentikan."
                ;;
            status)
                if systemctl --user list-unit-files orca.service >/dev/null 2>&1; then
                    systemctl --user status orca.service
                else
                    exec "$REAL_ORCA" status
                fi
                ;;
            *)
                exec "$REAL_ORCA" serve "$@"
                ;;
        esac
        ;;
    start)
        shift
        if has_display && [ -x "$ORCA_DESKTOP" ]; then
            echo "[*] Menjalankan Orca ADE versi Desktop (GUI)..."
            exec "$ORCA_DESKTOP" "$@"
        else
            echo "[*] Sesi tanpa display. Menjalankan Orca ADE versi Headless Server..."
            if systemctl --user list-unit-files orca.service >/dev/null 2>&1; then
                systemctl --user start orca.service
            else
                nohup "$REAL_ORCA" serve --supervise --force > "$HOME/.config/orca/orca.log" 2>&1 &
            fi
        fi
        ;;
    stop)
        stop_all_orca
        ;;
    open)
        shift
        if has_display && [ -x "$ORCA_DESKTOP" ]; then
            exec "$ORCA_DESKTOP" "$@"
        else
            exec "$REAL_ORCA" open "$@"
        fi
        ;;
    *)
        exec "$REAL_ORCA" "$@"
        ;;
esac
EOF
chmod +x "$BIN_DIR/orca"
ln -sf "$BIN_DIR/orca" "$BIN_DIR/orca-ide"

# Pastikan juga terpasang di ~/.config/orca/cli/bin/ (direktori yang di-inject Orca ADE ke terminal internal)
ORCA_CONFIG_CLI_DIR="$HOME/.config/orca/cli/bin"
mkdir -p "$ORCA_CONFIG_CLI_DIR"
rm -f "$ORCA_CONFIG_CLI_DIR/orca" "$ORCA_CONFIG_CLI_DIR/orca-ide"
ln -sf "$BIN_DIR/orca" "$ORCA_CONFIG_CLI_DIR/orca"
ln -sf "$BIN_DIR/orca" "$ORCA_CONFIG_CLI_DIR/orca-ide"

echo "[✓] Wrapper 'orca' dan 'orca-ide' terpasang di $BIN_DIR dan $ORCA_CONFIG_CLI_DIR."

# 10. Konfigurasi Integrasi Desktop (jika mode Desktop / Both atau DE terdeteksi)
setup_desktop_integration() {
    echo "[*] Mengonfigurasi integrasi desktop (Shortcut Menu, Ikon & Launcher)..."

    local APPS_DIR="$HOME/.local/share/applications"
    local ICONS_DIR="$HOME/.local/share/icons/hicolor"
    local PIXMAPS_DIR="$HOME/.local/share/pixmaps"
    mkdir -p "$APPS_DIR" "$ICONS_DIR" "$PIXMAPS_DIR"

    # 1. Pasang ikon di berbagai ukuran resolusi
    local icon_src_hicolor="$INSTALL_DIR/resources/icons/hicolor"
    local fallback_icon="$INSTALL_DIR/resources/app.asar.unpacked/out/renderer/assets/icon-DcVXyfru.png"
    local highres_icon="$INSTALL_DIR/resources/app.asar.unpacked/out/renderer/assets/orca-blue-CWdjK-Ki.png"

    if [ -d "$icon_src_hicolor" ]; then
        cp -r "$icon_src_hicolor/"* "$ICONS_DIR/" 2>/dev/null || true
    fi

    # Pastikan setiap folder ukuran apps memiliki icon
    for sz in 16x16 24x24 32x32 48x48 64x64 128x128 256x256 512x512; do
        mkdir -p "$ICONS_DIR/$sz/apps"
        if [ ! -f "$ICONS_DIR/$sz/apps/orca-ide.png" ]; then
            if [ -f "$fallback_icon" ]; then
                cp "$fallback_icon" "$ICONS_DIR/$sz/apps/orca-ide.png" 2>/dev/null || true
            elif [ -f "$highres_icon" ]; then
                cp "$highres_icon" "$ICONS_DIR/$sz/apps/orca-ide.png" 2>/dev/null || true
            fi
        fi
    done

    # Pixmaps fallback
    if [ -f "$fallback_icon" ]; then
        cp "$fallback_icon" "$PIXMAPS_DIR/orca-ide.png" 2>/dev/null || true
        cp "$fallback_icon" "$PIXMAPS_DIR/orca.png" 2>/dev/null || true
    elif [ -f "$highres_icon" ]; then
        cp "$highres_icon" "$PIXMAPS_DIR/orca-ide.png" 2>/dev/null || true
        cp "$highres_icon" "$PIXMAPS_DIR/orca.png" 2>/dev/null || true
    fi

    local CAN_WRITE_SYSTEM=0
    if [ "$EUID" -eq 0 ]; then
        CAN_WRITE_SYSTEM=1
    elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        CAN_WRITE_SYSTEM=2
    fi

    if [ "$CAN_WRITE_SYSTEM" -eq 1 ]; then
        mkdir -p /usr/share/icons/hicolor /usr/share/pixmaps
        cp -r "$ICONS_DIR/"* /usr/share/icons/hicolor/ 2>/dev/null || true
        cp "$PIXMAPS_DIR/orca-ide.png" /usr/share/pixmaps/ 2>/dev/null || true
        cp "$PIXMAPS_DIR/orca.png" /usr/share/pixmaps/ 2>/dev/null || true
    elif [ "$CAN_WRITE_SYSTEM" -eq 2 ]; then
        sudo mkdir -p /usr/share/icons/hicolor /usr/share/pixmaps 2>/dev/null || true
        sudo cp -r "$ICONS_DIR/"* /usr/share/icons/hicolor/ 2>/dev/null || true
        sudo cp "$PIXMAPS_DIR/orca-ide.png" /usr/share/pixmaps/ 2>/dev/null || true
        sudo cp "$PIXMAPS_DIR/orca.png" /usr/share/pixmaps/ 2>/dev/null || true
    fi

    # 2. Pasang Launcher Script 'orca-desktop'
    cat << 'EOF' > "$BIN_DIR/orca-desktop"
#!/usr/bin/env bash
# ==============================================================================
# orca-desktop - Launcher untuk Orca ADE Desktop GUI
# ==============================================================================
set -euo pipefail

TARGET_HOME="${HOME:-/home/$(id -un)}"
INSTALL_DIR="$TARGET_HOME/.local/lib/orca-ide"
APP_BIN="$INSTALL_DIR/orca-ide"

if [ ! -x "$APP_BIN" ]; then
    for candidate in /opt/Orca/orca-ide /opt/orca-ide/orca-ide; do
        if [ -x "$candidate" ]; then
            APP_BIN="$candidate"
            INSTALL_DIR="$(dirname "$candidate")"
            break
        fi
    done
fi

if [ ! -x "$APP_BIN" ]; then
    echo "Error: Biner Orca Desktop tidak ditemukan di $APP_BIN" >&2
    exit 1
fi

# Auto-detect Chromium browser untuk automation
if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ] || [ ! -x "${ORCA_BROWSER_EXECUTABLE:-}" ]; then
    for _b in /usr/bin/google-chrome /usr/bin/google-chrome-stable /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/brave-browser /usr/bin/microsoft-edge-stable /usr/bin/microsoft-edge /snap/bin/chromium; do
        if [ -x "$_b" ]; then
            export ORCA_BROWSER_EXECUTABLE="$_b"
            break
        fi
    done
    unset _b
fi

# orcad menggunakan single-instance lock pada ~/.config/orca.
# Jika headless server aktif di background, hentikan sementara agar desktop GUI bisa terbuka.
if systemctl --user is-active --quiet orca 2>/dev/null; then
    echo "[*] Menghentikan orca.service (user) agar antarmuka Desktop GUI bisa dibuka..."
    systemctl --user stop orca.service 2>/dev/null || true
    sleep 1
elif systemctl is-active --quiet orca 2>/dev/null; then
    echo "[*] Menghentikan orca.service (system) agar antarmuka Desktop GUI bisa dibuka..."
    sudo systemctl stop orca.service 2>/dev/null || systemctl stop orca.service 2>/dev/null || true
    sleep 1
elif pgrep -f "orca-ide.*serve|orcad-server" >/dev/null 2>&1; then
    echo "[*] Menghentikan proses orca serve background agar antarmuka Desktop GUI bisa dibuka..."
    pkill -f "orca-ide.*serve|orcad-server" 2>/dev/null || true
    sleep 1
fi

EXTRA_ARGS=()
# Fallback sandbox jika unprivileged userns dibatasi dan sandbox bukan SUID root
SANDBOX_BIN="$INSTALL_DIR/chrome-sandbox"
if [ -f "$SANDBOX_BIN" ]; then
    PERMS=$(stat -c "%a" "$SANDBOX_BIN" 2>/dev/null || stat -f "%OLp" "$SANDBOX_BIN" 2>/dev/null || echo "0")
    OWNER=$(stat -c "%u" "$SANDBOX_BIN" 2>/dev/null || echo "1000")
    if [ "$PERMS" != "4755" ] || [ "$OWNER" != "0" ]; then
        if [ -f /proc/sys/kernel/unprivileged_userns_clone ] && [ "$(cat /proc/sys/kernel/unprivileged_userns_clone 2>/dev/null)" = "0" ]; then
            EXTRA_ARGS+=("--no-sandbox")
        fi
    fi
fi

exec "$APP_BIN" "${EXTRA_ARGS[@]}" "$@"
EOF
    chmod +x "$BIN_DIR/orca-desktop"

    if [ "$CAN_WRITE_SYSTEM" -eq 1 ]; then
        cp "$BIN_DIR/orca-desktop" /usr/local/bin/orca-desktop 2>/dev/null || true
        chmod +x /usr/local/bin/orca-desktop 2>/dev/null || true
    elif [ "$CAN_WRITE_SYSTEM" -eq 2 ]; then
        sudo cp "$BIN_DIR/orca-desktop" /usr/local/bin/orca-desktop 2>/dev/null || true
        sudo chmod +x /usr/local/bin/orca-desktop 2>/dev/null || true
    fi

    # 3. Buat file .desktop
    cat << EOF > "$APPS_DIR/orca-ide.desktop"
[Desktop Entry]
Name=Orca IDE
Comment=Next-gen IDE for parallel agentic development
GenericName=Integrated Development Environment
Exec=$BIN_DIR/orca-desktop %U
Icon=orca-ide
Type=Application
StartupNotify=true
StartupWMClass=orca
Terminal=false
Categories=Development;IDE;Utility;TextEditor;
MimeType=text/markdown;text/csv;text/tab-separated-values;text/plain;x-scheme-handler/orca;
Keywords=orca;ide;editor;code;ai;agent;
Actions=new-empty-window;

[Desktop Action new-empty-window]
Name=New Empty Window
Exec=$BIN_DIR/orca-desktop --new-window %U
EOF
    chmod 644 "$APPS_DIR/orca-ide.desktop"

    if [ "$CAN_WRITE_SYSTEM" -eq 1 ]; then
        mkdir -p /usr/share/applications
        cp "$APPS_DIR/orca-ide.desktop" /usr/share/applications/orca-ide.desktop 2>/dev/null || true
        chmod 644 /usr/share/applications/orca-ide.desktop 2>/dev/null || true
    elif [ "$CAN_WRITE_SYSTEM" -eq 2 ]; then
        sudo mkdir -p /usr/share/applications 2>/dev/null || true
        sudo cp "$APPS_DIR/orca-ide.desktop" /usr/share/applications/orca-ide.desktop 2>/dev/null || true
        sudo chmod 644 /usr/share/applications/orca-ide.desktop 2>/dev/null || true
    fi

    # 4. Refresh Desktop & Icon Database
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$APPS_DIR" 2>/dev/null || true
    if [ "$CAN_WRITE_SYSTEM" -ne 0 ] && command -v update-desktop-database >/dev/null 2>&1; then
        if [ "$CAN_WRITE_SYSTEM" -eq 1 ]; then
            update-desktop-database /usr/share/applications 2>/dev/null || true
        else
            sudo update-desktop-database /usr/share/applications 2>/dev/null || true
        fi
    fi

    command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t "$ICONS_DIR" 2>/dev/null || true
    if [ "$CAN_WRITE_SYSTEM" -ne 0 ] && command -v gtk-update-icon-cache >/dev/null 2>&1; then
        if [ "$CAN_WRITE_SYSTEM" -eq 1 ]; then
            gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
        else
            sudo gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
        fi
    fi

    command -v xdg-mime >/dev/null 2>&1 && xdg-mime default orca-ide.desktop x-scheme-handler/orca 2>/dev/null || true

    # Perbaiki permission chrome-sandbox jika punya sudo/root
    if [ "$CAN_WRITE_SYSTEM" -eq 1 ]; then
        chown root:root "$INSTALL_DIR/chrome-sandbox" 2>/dev/null || true
        chmod 4755 "$INSTALL_DIR/chrome-sandbox" 2>/dev/null || true
    elif [ "$CAN_WRITE_SYSTEM" -eq 2 ]; then
        sudo chown root:root "$INSTALL_DIR/chrome-sandbox" 2>/dev/null || true
        sudo chmod 4755 "$INSTALL_DIR/chrome-sandbox" 2>/dev/null || true
    fi

    echo "[✓] Integrasi Desktop selesai. Orca IDE telah ditambahkan ke menu aplikasi."
}

if [ "$INSTALL_MODE" = "desktop" ]; then
    setup_desktop_integration
else
    # Mode Headless Server murni: bersihkan shortcut menu desktop jika ada
    rm -f "$HOME/.local/share/applications/orca-ide.desktop" 2>/dev/null || true
    if [ "$EUID" -eq 0 ]; then
        rm -f /usr/share/applications/orca-ide.desktop 2>/dev/null || true
    elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        sudo rm -f /usr/share/applications/orca-ide.desktop 2>/dev/null || true
    fi
fi

# 11. Konfigurasi Service untuk Orca ADE (Hanya salah satu: Desktop GUI atau Headless Server)
USE_USER_SYSTEMD=false
if systemctl --user daemon-reload >/dev/null 2>&1; then
    USE_USER_SYSTEMD=true
fi

SYSTEMD_USER_OK=0
[ "$USE_USER_SYSTEMD" = true ] && SYSTEMD_USER_OK=1

CAN_SUDO=0
([ "$EUID" -eq 0 ] || (command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null)) && CAN_SUDO=1

if [ "$INSTALL_MODE" = "headless" ]; then
    echo "[*] Mengonfigurasi Orca ADE versi Headless Server (Background Service)..."
    if [ "$USE_USER_SYSTEMD" = true ]; then
        echo "[*] Mengonfigurasi systemd user service..."
        mkdir -p "$SYSTEMD_USER_DIR"
        cat << EOF > "$SYSTEMD_USER_DIR/orca.service"
[Unit]
Description=Orca ADE Headless Server
After=network.target

[Service]
Type=simple
ExecStart=$BIN_DIR/orca-ide serve --supervise --force
Restart=always
RestartSec=5
Environment="PATH=$BIN_DIR:/usr/local/bin:/usr/bin:/bin"
Environment="ORCA_ALLOW_INSECURE_WEB_PAIRING=1"
Environment="ORCA_BROWSER_EXECUTABLE=${ORCA_BROWSER:-/usr/bin/google-chrome}"

[Install]
WantedBy=default.target
EOF

        cat << EOF > "$SYSTEMD_USER_DIR/orca-update.service"
[Unit]
Description=Check and apply Orca ADE updates
After=network.target

[Service]
Type=oneshot
ExecStart=$BIN_DIR/orca-update
EOF

        cat << EOF > "$SYSTEMD_USER_DIR/orca-update.timer"
[Unit]
Description=Daily update check for Orca ADE

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=1h

[Install]
WantedBy=timers.target
EOF

        systemctl --user daemon-reload
        echo "[*] Mengaktifkan dan merestart service systemd user..."
        systemctl --user enable orca.service
        systemctl --user restart orca.service
        systemctl --user enable orca-update.timer
        systemctl --user restart orca-update.timer
    elif [ "$CAN_SUDO" -eq 1 ]; then
        CURRENT_USER="$(id -un)"
        USER_HOME="$HOME"
        echo "[*] Mengonfigurasi systemd system service (/etc/systemd/system/orca.service)..."
        
        sudo tee /etc/systemd/system/orca.service >/dev/null << EOF
[Unit]
Description=Orca ADE Headless Server
After=network.target

[Service]
Type=simple
User=$CURRENT_USER
Group=$CURRENT_USER
WorkingDirectory=$USER_HOME
ExecStart=$BIN_DIR/orca-ide serve --supervise --force
Restart=always
RestartSec=5
Environment="PATH=$BIN_DIR:/usr/local/bin:/usr/bin:/bin"
Environment="HOME=$USER_HOME"
Environment="ORCA_ALLOW_INSECURE_WEB_PAIRING=1"
Environment="ORCA_BROWSER_EXECUTABLE=${ORCA_BROWSER:-/usr/bin/google-chrome}"

[Install]
WantedBy=multi-user.target
EOF

        sudo tee /etc/systemd/system/orca-update.service >/dev/null << EOF
[Unit]
Description=Check and apply Orca ADE updates
After=network.target

[Service]
Type=oneshot
User=$CURRENT_USER
ExecStart=$BIN_DIR/orca-update
EOF

        sudo tee /etc/systemd/system/orca-update.timer >/dev/null << EOF
[Unit]
Description=Daily update check for Orca ADE

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=1h

[Install]
WantedBy=timers.target
EOF

        sudo systemctl daemon-reload
        echo "[*] Mengaktifkan dan merestart service systemd system..."
        sudo systemctl enable orca.service
        sudo systemctl restart orca.service
        sudo systemctl enable orca-update.timer
        sudo systemctl restart orca-update.timer
    else
        echo "[!] Systemd user session dan akses sudo tidak tersedia. Menjalankan di background..."
        pkill -f "orca-ide serve" 2>/dev/null || true
        [ -n "$ORCA_BROWSER" ] && export ORCA_BROWSER_EXECUTABLE="$ORCA_BROWSER"
        nohup "$BIN_DIR/orca-ide" serve --supervise --force > "$USER_HOME/.config/orca/orca.log" 2>&1 &
    fi
else
    # INSTALL_MODE == "desktop"
    echo "[*] Mode Desktop: Orca ADE disetel ke versi Desktop GUI murni."
    echo "    Layanan Headless Server (orca.service) dinonaktifkan & dibersihkan agar tidak bentrok."

    if [ "$USE_USER_SYSTEMD" = true ]; then
        systemctl --user stop orca.service 2>/dev/null || true
        systemctl --user disable orca.service 2>/dev/null || true
        rm -f "$SYSTEMD_USER_DIR/orca.service" 2>/dev/null || true
        
        # Tetap sediakan auto-update timer
        mkdir -p "$SYSTEMD_USER_DIR"
        cat << EOF > "$SYSTEMD_USER_DIR/orca-update.service"
[Unit]
Description=Check and apply Orca ADE updates
After=network.target

[Service]
Type=oneshot
ExecStart=$BIN_DIR/orca-update
EOF

        cat << EOF > "$SYSTEMD_USER_DIR/orca-update.timer"
[Unit]
Description=Daily update check for Orca ADE

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=1h

[Install]
WantedBy=timers.target
EOF

        systemctl --user daemon-reload 2>/dev/null || true
        systemctl --user enable orca-update.timer 2>/dev/null || true
        systemctl --user restart orca-update.timer 2>/dev/null || true
    fi

    if [ "$CAN_SUDO" -eq 1 ]; then
        sudo systemctl stop orca.service 2>/dev/null || true
        sudo systemctl disable orca.service 2>/dev/null || true
        sudo rm -f /etc/systemd/system/orca.service 2>/dev/null || true
        sudo systemctl daemon-reload 2>/dev/null || true
    fi
fi

if [ "$INSTALL_MODE" = "headless" ]; then
    # 12. Menunggu runtime server aktif
    echo "[*] Menunggu runtime server aktif..."
    for i in {1..20}; do
        if [ -f "$HOME/.config/orca/orca-runtime.json" ]; then
            break
        fi
        sleep 1
    done
fi

# 13. Konfigurasi otomatis default Git Commit Generator jika agent lokal tersedia
DB_FILE="$HOME/.config/orca/profiles/local-default/profile-state.db"
if [ -f "$DB_FILE" ] && command -v python3 >/dev/null 2>&1; then
    AUTO_AGENT=""
    if [ -x "$HOME/.opencode/bin/opencode" ] || command -v opencode >/dev/null 2>&1; then
        AUTO_AGENT="opencode"
    elif [ -x "$HOME/.local/bin/agy" ] || command -v agy >/dev/null 2>&1; then
        AUTO_AGENT="antigravity"
    fi

    if [ -n "$AUTO_AGENT" ]; then
        echo "[*] Mengonfigurasi default AI Agent untuk Git Commit Generator ($AUTO_AGENT)..."
        # Hentikan service sementara agar WAL ter-flush dan data in-memory tidak menimpa database
        WAS_SERVICE_ACTIVE=0
        if [ "$SYSTEMD_USER_OK" -eq 1 ] && systemctl --user is-active --quiet orca 2>/dev/null; then
            WAS_SERVICE_ACTIVE=1
            systemctl --user stop orca.service 2>/dev/null || true
        elif [ "$CAN_SUDO" -eq 1 ] && (systemctl is-active --quiet orca 2>/dev/null || sudo systemctl is-active --quiet orca 2>/dev/null); then
            WAS_SERVICE_ACTIVE=1
            sudo systemctl stop orca.service 2>/dev/null || systemctl stop orca.service 2>/dev/null || true
        elif pgrep -f "orca-ide.*serve|orcad-server" >/dev/null 2>&1; then
            WAS_SERVICE_ACTIVE=1
            pkill -f "orca-ide.*serve|orcad-server" 2>/dev/null || true
        fi
        sleep 1

        python3 -c "
import sqlite3, json, sys, hashlib, time
db = sys.argv[1]
agent = sys.argv[2]
prompt = 'Gunakan format Conventional Commits (feat, fix, refactor, docs, chore, test). Judul maksimal 72 karakter. Tulis pesan commit dalam Bahasa Indonesia yang singkat, padat, dan jelas.'
try:
    conn = sqlite3.connect(db)
    c = conn.cursor()
    c.execute(\"SELECT value FROM profile_state_meta WHERE key = 'revision'\")
    r = c.fetchone()
    cur_rev = int(r[0]) if r and r[0].isdigit() else 0
    c.execute(\"SELECT MAX(revision) FROM profile_state_documents\")
    r = c.fetchone()
    max_rev = r[0] if r and r[0] else 0
    new_rev = max(cur_rev, max_rev) + 1
    now_ms = int(time.time() * 1000)

    # 1. Update domain settings
    c.execute(\"SELECT payload FROM profile_state_documents WHERE domain = 'settings'\")
    r = c.fetchone()
    if r:
        s = json.loads(r[0])
        s['defaultTuiAgent'] = agent

        if 'commitMessageAi' not in s or not isinstance(s['commitMessageAi'], dict):
            s['commitMessageAi'] = {'enabled': True}
        s['commitMessageAi']['enabled'] = True
        s['commitMessageAi']['agentId'] = agent
        s['commitMessageAi']['customPrompt'] = prompt
        s['commitMessageAi']['customAgentCommand'] = ''

        if 'sourceControlAi' not in s or not isinstance(s['sourceControlAi'], dict):
            s['sourceControlAi'] = {'actions': {}, 'enabled': True}
        s['sourceControlAi']['enabled'] = True
        s['sourceControlAi']['agentId'] = agent
        if 'instructionsByOperation' not in s['sourceControlAi']:
            s['sourceControlAi']['instructionsByOperation'] = {}
        s['sourceControlAi']['instructionsByOperation']['commitMessage'] = prompt
        if 'actions' not in s['sourceControlAi']:
            s['sourceControlAi']['actions'] = {}
        if 'commitMessage' not in s['sourceControlAi']['actions']:
            s['sourceControlAi']['actions']['commitMessage'] = {}
        s['sourceControlAi']['actions']['commitMessage']['agentId'] = agent
        s['sourceControlAi']['actions']['commitMessage']['commandInputTemplate'] = '{basePrompt}'

        new_payload = json.dumps(s)
        chash = hashlib.sha256(new_payload.encode('utf-8')).hexdigest()
        c.execute(\"UPDATE profile_state_documents SET payload = ?, revision = ?, updated_at = ?, content_hash = ? WHERE domain = 'settings'\", (new_payload, new_rev, now_ms, chash))

    # 2. Update domain repos
    c.execute(\"SELECT payload FROM profile_state_documents WHERE domain = 'repos'\")
    r_row = c.fetchone()
    if r_row:
        repos = json.loads(r_row[0])
        for repo in repos:
            repo['sourceControlAi'] = {
                'enabled': True,
                'agentId': agent,
                'actions': {'commitMessage': {'agentId': agent, 'commandInputTemplate': '{basePrompt}'}},
                'instructionsByOperation': {'commitMessage': prompt}
            }
            repo['commitMessageAi'] = {'enabled': True, 'agentId': agent, 'customPrompt': prompt}
        new_r_payload = json.dumps(repos)
        r_hash = hashlib.sha256(new_r_payload.encode('utf-8')).hexdigest()
        c.execute(\"UPDATE profile_state_documents SET payload = ?, revision = ?, updated_at = ?, content_hash = ? WHERE domain = 'repos'\", (new_r_payload, new_rev, now_ms, r_hash))

    # 3. Update meta revision
    c.execute(\"INSERT INTO profile_state_meta (key, value) VALUES ('revision', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value\", (str(new_rev),))
    conn.commit()
    c.execute(\"PRAGMA wal_checkpoint(TRUNCATE)\")
    conn.close()
except Exception:
    pass
" "$DB_FILE" "$AUTO_AGENT" 2>/dev/null || true

        # Start kembali service hanya jika mode headless
        if [ "$INSTALL_MODE" = "headless" ]; then
            if [ "$SYSTEMD_USER_OK" -eq 1 ]; then
                systemctl --user start orca.service 2>/dev/null || true
            elif [ "$CAN_SUDO" -eq 1 ]; then
                sudo systemctl start orca.service 2>/dev/null || true
            else
                nohup "$BIN_DIR/orca-ide" serve --supervise --force > "$USER_HOME/.config/orca/orca.log" 2>&1 &
            fi
            sleep 2
        fi
    fi
fi

echo ""
echo "=================================================="
echo "    INSTALASI ORCA ADE BERHASIL SELESAI           "
echo "=================================================="
echo ""

# 14. Tampilkan informasi ringkasan atau pairing sesuai mode aktif
if [ "$INSTALL_MODE" = "headless" ]; then
    echo "[✓] Orca ADE versi Headless Server berhasil dikonfigurasi & berjalan."
    echo "  - Status: Mode Headless Server murni (Background Service aktif)."
    echo "  - Service: orca.service (Active)"
    echo "  - Informasi pairing untuk Web Client & Mobile App:"
    echo ""
    "$BIN_DIR/orca" pair || true
else
    echo "[✓] Orca ADE versi Desktop GUI berhasil terpasang!"
    echo "  - Status: Mode Desktop GUI murni (Headless Server dinonaktifkan agar tidak bentrok)."
    echo "  - Buka melalui Menu Aplikasi Desktop ($DETECTED_DE) -> Development -> Orca IDE"
    echo "  - Atau jalankan di terminal: orca-desktop (atau 'orca desktop' / 'orca start')"
    echo "  - Cek status aplikasi: orca status"
    echo ""
fi
