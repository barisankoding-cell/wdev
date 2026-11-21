#!/usr/bin/env bash
# scripts/release.sh - Otomasi rilis wdev ke GitHub dengan Changelog otomatis
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

NEXT_SCRIPT="$ROOT_DIR/scripts/next-version.sh"
chmod +x "$NEXT_SCRIPT" "$ROOT_DIR/scripts/changelog.sh"

INPUT_ARG="${1:-}"
AUTO_APPROVE="${AUTO:-${YES:-0}}"
DRY_RUN="${DRY_RUN:-0}"

if [ "$INPUT_ARG" = "--dry-run" ] || [ "$INPUT_ARG" = "-n" ]; then
    DRY_RUN=1
    INPUT_ARG=""
fi

resolve_version_meta() {
    local mode="${1:-auto}"
    local meta
    meta=$("$NEXT_SCRIPT" --explain "$mode")
    FILE_VERSION=$(printf '%s\n' "$meta" | sed -n 's/^FILE_VERSION=//p')
    PREV_TAG=$(printf '%s\n' "$meta" | sed -n 's/^PREV_TAG=//p')
    BASE_VERSION=$(printf '%s\n' "$meta" | sed -n 's/^BASE_VERSION=//p')
    COMMIT_COUNT=$(printf '%s\n' "$meta" | sed -n 's/^COMMIT_COUNT=//p')
    FEAT_COUNT=$(printf '%s\n' "$meta" | sed -n 's/^FEAT_COUNT=//p')
    FIX_COUNT=$(printf '%s\n' "$meta" | sed -n 's/^FIX_COUNT=//p')
    BREAKING_COUNT=$(printf '%s\n' "$meta" | sed -n 's/^BREAKING_COUNT=//p')
    BUMP_TYPE=$(printf '%s\n' "$meta" | sed -n 's/^BUMP_TYPE=//p')
    REASON=$(printf '%s\n' "$meta" | sed -n 's/^REASON=//p')
    NEW_VERSION=$(printf '%s\n' "$meta" | sed -n 's/^NEXT_VERSION=//p')
}

resolve_version_meta "${INPUT_ARG:-auto}"

if [ -z "$INPUT_ARG" ]; then
    echo "=================================================================="
    echo "                 wdev AUTOMATED RELEASE ASSISTANT                 "
    echo "=================================================================="
    echo "Versi version.go : v${FILE_VERSION}"
    echo "Tag rilis terakhir: ${PREV_TAG:-<belum ada>}"
    echo "Basis versi      : v${BASE_VERSION}"
    echo "Analisis commit  : ${COMMIT_COUNT} commit (${FEAT_COUNT} feat, ${FIX_COUNT} fix, ${BREAKING_COUNT} breaking)"
    echo "Deteksi otomatis : v${NEW_VERSION} [${BUMP_TYPE}] (${REASON})"
    echo ""
    if [ "$AUTO_APPROVE" != "1" ] && [ "$DRY_RUN" != "1" ] && [ -t 0 ]; then
        read -r -p "Gunakan v${NEW_VERSION}? [Enter=Ya / ketik versi/major/minor/patch / n=batal]: " USER_CHOICE
        USER_CHOICE="${USER_CHOICE// /}"
        case "$USER_CHOICE" in
            ""|y|Y|yes|YES)
                ;;
            n|N|no|NO)
                echo "Rilis dibatalkan."
                exit 0
                ;;
            *)
                resolve_version_meta "$USER_CHOICE"
                ;;
        esac
    fi
fi

NEW_VERSION="${NEW_VERSION#v}"
TAG="v${NEW_VERSION}"

if [ -z "$NEW_VERSION" ] || [[ ! "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Versi rilis '${NEW_VERSION}' tidak valid (harus SemVer X.Y.Z)." >&2
    exit 1
fi

# Cek apakah tag sudah ada
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "Error: Git tag $TAG sudah ada! Gunakan nomor versi yang lebih tinggi." >&2
    exit 1
fi

if [ "$DRY_RUN" = "1" ]; then
    echo "=================================================================="
    echo "[DRY-RUN] Simulasi rilis $TAG (${BUMP_TYPE}: ${REASON})"
    echo "=================================================================="
    "$ROOT_DIR/scripts/changelog.sh" "$NEW_VERSION"
    exit 0
fi

# Cek uncommitted changes selain file yang akan kita modifikasi
DIRTY_FILES=$(git status --porcelain | grep -v 'CHANGELOG.md\|internal/version/version.go' || true)
if [ -n "$DIRTY_FILES" ]; then
    echo "Peringatan: Ada perubahan git yang belum di-commit:"
    git status -s
    echo ""
    if [ "$AUTO_APPROVE" = "1" ] || [ ! -t 0 ]; then
        echo "Error: Working tree kotor. Commit perubahan dulu atau jalankan secara interaktif." >&2
        exit 1
    fi
    read -r -p "Apakah Anda ingin melanjutkan dan meng-commit perubahan ini bersama rilis? [y/N]: " PROCEED
    if [[ ! "$PROCEED" =~ ^[Yy]$ ]]; then
        echo "Rilis dibatalkan. Bersihkan atau commit pekerjaan Anda terlebih dahulu."
        exit 1
    fi
fi

echo "=================================================================="
echo "Memulai proses rilis $TAG (${BUMP_TYPE}) ..."
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
chmod 644 "$TEMP_CHANGELOG"
mv "$TEMP_CHANGELOG" "$CHANGELOG_FILE"

# 3. Build & Test
echo "==> [3/6] Menjalankan test dan build frontend + binary..."
cd "$ROOT_DIR/frontend"
pnpm install --frozen-lockfile
pnpm build
cd "$ROOT_DIR"

go test ./...

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
