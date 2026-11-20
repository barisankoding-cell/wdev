#!/usr/bin/env bash
# scripts/release.sh - Otomasi rilis wdev ke GitHub dengan Changelog otomatis
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CURRENT_VERSION=$(grep 'var Version =' internal/version/version.go | cut -d '"' -f 2)
NEW_VERSION="${1:-}"

if [ -z "$NEW_VERSION" ]; then
    echo "=================================================================="
    echo "                 wdev AUTOMATED RELEASE ASSISTANT                 "
    echo "=================================================================="
    echo "Versi saat ini : v${CURRENT_VERSION}"
    echo ""
    read -r -p "Masukkan versi rilis baru (contoh: 2.2.0): " INPUT_VER
    NEW_VERSION="$INPUT_VER"
fi

NEW_VERSION="${NEW_VERSION#v}"
TAG="v${NEW_VERSION}"

if [ -z "$NEW_VERSION" ]; then
    echo "Error: Versi rilis tidak boleh kosong." >&2
    exit 1
fi

# Cek apakah tag sudah ada
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "Error: Git tag $TAG sudah ada! Gunakan nomor versi yang lebih tinggi." >&2
    exit 1
fi

# Cek uncommitted changes selain file yang akan kita modifikasi
if [ -n "$(git status --porcelain | grep -v 'CHANGELOG.md\|internal/version/version.go')" ]; then
    echo "Peringatan: Ada perubahan git yang belum di-commit:"
    git status -s
    echo ""
    read -r -p "Apakah Anda ingin melanjutkan dan meng-commit perubahan ini bersama rilis? [y/N]: " PROCEED
    if [[ ! "$PROCEED" =~ ^[Yy]$ ]]; then
        echo "Rilis dibatalkan. Bersihkan atau commit pekerjaan Anda terlebih dahulu."
        exit 1
    fi
fi

echo "=================================================================="
echo "Memulai proses rilis $TAG ..."
echo "=================================================================="

# 1. Update version.go
echo "==> [1/6] Memperbarui internal/version/version.go ke $NEW_VERSION ..."
sed -i "s/var Version = \".*\"/var Version = \"${NEW_VERSION}\"/" internal/version/version.go

# 2. Generate Changelog
echo "==> [2/6] Mengenerate changelog rilis..."
TEMP_NOTES=$(mktemp /tmp/wdev_release_notes.XXXXXX.md)
"$ROOT_DIR/scripts/changelog.sh" "$NEW_VERSION" > "$TEMP_NOTES"

CHANGELOG_FILE="$ROOT_DIR/CHANGELOG.md"
if [ ! -f "$CHANGELOG_FILE" ]; then
    echo "# Changelog wdev" > "$CHANGELOG_FILE"
    echo "" >> "$CHANGELOG_FILE"
fi

TEMP_CHANGELOG=$(mktemp /tmp/wdev_changelog.XXXXXX.md)
head -n 2 "$CHANGELOG_FILE" > "$TEMP_CHANGELOG"
cat "$TEMP_NOTES" >> "$TEMP_CHANGELOG"
tail -n +3 "$CHANGELOG_FILE" >> "$TEMP_CHANGELOG"
mv "$TEMP_CHANGELOG" "$CHANGELOG_FILE"

# 3. Build & Test
echo "==> [3/6] Menjalankan test dan build frontend + binary..."
cd "$ROOT_DIR/frontend"
pnpm install --frozen-lockfile
pnpm build
cd "$ROOT_DIR"

go test ./internal/...

mkdir -p bin
go build -trimpath -ldflags "-s -w -X wdev/internal/version.Version=${NEW_VERSION}" -o bin/wdev ./cmd/wdev

# 4. Commit Release
echo "==> [4/6] Membuat git commit rilis..."
git add -A
git commit -m "chore(release): bump version to ${TAG}" || true

# 5. Tag Release
echo "==> [5/6] Membuat annotated git tag ${TAG}..."
git tag -a "$TAG" -F "$TEMP_NOTES"

# 6. Push & Publish to GitHub
echo "==> [6/6] Mempublikasikan ke GitHub..."
git push origin HEAD
git push origin "$TAG"

# Jika gh CLI terpasang dan authenticated, buat release sekarang
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    echo "==> Mendaftarkan rilis di GitHub via gh CLI..."
    # Buat GitHub release dengan notes yang dihasilkan
    gh release create "$TAG" \
        --title "wdev ${TAG}" \
        --notes-file "$TEMP_NOTES" || echo "Catatan: GitHub Actions akan melanjutkan pembuatan aset release."
fi

rm -f "$TEMP_NOTES"

echo "=================================================================="
echo "✓ SUKSES: Rilis ${TAG} berhasil dipublikasikan ke GitHub!"
echo "GitHub Actions sedang meng-compile release multi-arch binaries."
echo "Repository: https://github.com/barisankoding-cell/wdev/releases/tag/${TAG}"
echo "=================================================================="
