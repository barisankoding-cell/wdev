#!/usr/bin/env bash
# scripts/next-version.sh - Deteksi otomatis versi SemVer berikutnya dari version.go, git tag, dan Conventional Commits
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

EXPLAIN=false
MODE_ARG=""

for arg in "$@"; do
    case "$arg" in
        --explain|-e)
            EXPLAIN=true
            ;;
        *)
            if [ -z "$MODE_ARG" ] && [ -n "$arg" ]; then
                MODE_ARG="$arg"
            fi
            ;;
    esac
done

MODE_ARG="${MODE_ARG:-${BUMP:-auto}}"
MODE_ARG="${MODE_ARG#v}"

is_semver() {
    [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

semver_gt() {
    local a="$1" b="$2"
    if [ "$a" = "$b" ]; then
        return 1
    fi
    local top
    top=$(printf '%s\n%s\n' "$a" "$b" | sort -V | tail -n 1)
    [ "$top" = "$a" ]
}

# 1. Baca versi saat ini dari internal/version/version.go
FILE_VERSION=""
if [ -f "$ROOT_DIR/internal/version/version.go" ]; then
    FILE_VERSION=$(grep -E 'var Version =' "$ROOT_DIR/internal/version/version.go" | head -n 1 | cut -d '"' -f 2 | sed 's/^v//' || true)
fi
if ! is_semver "$FILE_VERSION"; then
    FILE_VERSION="0.0.0"
fi

# 2. Ambil tag SemVer terbaru yang terjangkau di branch dan tertinggi di seluruh repo
PREV_TAG=$(git describe --tags --abbrev=0 --match "v[0-9]*.[0-9]*.[0-9]*" 2>/dev/null || echo "")
HIGHEST_TAG=$(git tag -l "v[0-9]*.[0-9]*.[0-9]*" 2>/dev/null | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1 || echo "")

BASE_VERSION="$FILE_VERSION"
if [ -n "$HIGHEST_TAG" ] && semver_gt "$HIGHEST_TAG" "$BASE_VERSION"; then
    BASE_VERSION="$HIGHEST_TAG"
fi
if [ -n "$PREV_TAG" ]; then
    PREV_TAG_CLEAN="${PREV_TAG#v}"
    if is_semver "$PREV_TAG_CLEAN" && semver_gt "$PREV_TAG_CLEAN" "$BASE_VERSION"; then
        BASE_VERSION="$PREV_TAG_CLEAN"
    fi
fi

# 3. Analisis commit sejak PREV_TAG untuk Conventional Commits
if [ -n "$PREV_TAG" ]; then
    RANGE="${PREV_TAG}..HEAD"
    RANGE_LABEL="$PREV_TAG"
else
    RANGE="HEAD"
    RANGE_LABEL="awal repositori"
fi

COMMIT_COUNT=$(git rev-list --count "$RANGE" 2>/dev/null || echo "0")
DIRTY_COUNT=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')

BREAKING_COUNT=0
FEAT_COUNT=0
FIX_COUNT=0

if [ "$COMMIT_COUNT" -gt 0 ]; then
    BREAKING_MATCHES=$(git log "$RANGE" -E --grep='^[a-zA-Z0-9_-]+(\([^)]*\))?!:|BREAKING[ -]CHANGE:' --format='%H' 2>/dev/null || true)
    if [ -n "$BREAKING_MATCHES" ]; then
        BREAKING_COUNT=$(printf '%s\n' "$BREAKING_MATCHES" | grep -c . || true)
    fi

    FEAT_MATCHES=$(git log "$RANGE" -E --grep='^feat(\([^)]*\))?(!)?:' --format='%H' 2>/dev/null || true)
    if [ -n "$FEAT_MATCHES" ]; then
        FEAT_COUNT=$(printf '%s\n' "$FEAT_MATCHES" | grep -c . || true)
    fi

    FIX_MATCHES=$(git log "$RANGE" -E --grep='^fix(\([^)]*\))?(!)?:' --format='%H' 2>/dev/null || true)
    if [ -n "$FIX_MATCHES" ]; then
        FIX_COUNT=$(printf '%s\n' "$FIX_MATCHES" | grep -c . || true)
    fi
fi

# 4. Tentukan bump & versi berikutnya
IFS='.' read -r BASE_MAJOR BASE_MINOR BASE_PATCH <<< "$BASE_VERSION"
BUMP_TYPE=""
REASON=""
NEXT_VERSION=""

bump_semver() {
    local kind="$1"
    local maj="$BASE_MAJOR" min="$BASE_MINOR" pat="$BASE_PATCH"
    case "$kind" in
        major)
            maj=$((maj + 1))
            min=0
            pat=0
            ;;
        minor)
            min=$((min + 1))
            pat=0
            ;;
        patch)
            pat=$((pat + 1))
            ;;
    esac
    # Hindari tabrakan dengan tag yang sudah ada
    local candidate="${maj}.${min}.${pat}"
    while git rev-parse "v${candidate}" >/dev/null 2>&1; do
        pat=$((pat + 1))
        candidate="${maj}.${min}.${pat}"
    done
    echo "$candidate"
}

case "$MODE_ARG" in
    ""|auto)
        # Jika version.go sudah dinaikkan manual melebihi tag tertinggi dan tag belum dibuat, gunakan versi tersebut
        if [ -n "$HIGHEST_TAG" ] && semver_gt "$FILE_VERSION" "$HIGHEST_TAG" && ! git rev-parse "v${FILE_VERSION}" >/dev/null 2>&1; then
            BUMP_TYPE="manual-file"
            NEXT_VERSION="$FILE_VERSION"
            REASON="internal/version/version.go (v${FILE_VERSION}) sudah lebih tinggi dari tag terakhir (v${HIGHEST_TAG})"
        elif [ "$BREAKING_COUNT" -gt 0 ]; then
            BUMP_TYPE="major"
            NEXT_VERSION=$(bump_semver major)
            REASON="ditemukan ${BREAKING_COUNT} breaking change sejak ${RANGE_LABEL}"
        elif [ "$FEAT_COUNT" -gt 0 ]; then
            BUMP_TYPE="minor"
            NEXT_VERSION=$(bump_semver minor)
            REASON="ditemukan ${FEAT_COUNT} commit feat sejak ${RANGE_LABEL}"
        else
            BUMP_TYPE="patch"
            NEXT_VERSION=$(bump_semver patch)
            if [ "$FIX_COUNT" -gt 0 ]; then
                REASON="ditemukan ${FIX_COUNT} commit fix (${COMMIT_COUNT} total commit) sejak ${RANGE_LABEL}"
            elif [ "$COMMIT_COUNT" -gt 0 ]; then
                REASON="ditemukan ${COMMIT_COUNT} commit non-feat sejak ${RANGE_LABEL}"
            elif [ "$DIRTY_COUNT" -gt 0 ]; then
                REASON="belum ada commit baru, terdapat ${DIRTY_COUNT} perubahan lokal belum di-commit"
            else
                REASON="belum ada commit baru sejak ${RANGE_LABEL} (patch fallback)"
            fi
        fi
        ;;
    major|minor|patch)
        BUMP_TYPE="$MODE_ARG"
        NEXT_VERSION=$(bump_semver "$MODE_ARG")
        REASON="dipilih mode ${MODE_ARG} bump dari v${BASE_VERSION}"
        ;;
    *)
        if ! is_semver "$MODE_ARG"; then
            echo "Error: Format versi '${MODE_ARG}' tidak valid (gunakan SemVer X.Y.Z, atau auto/patch/minor/major)." >&2
            exit 1
        fi
        BUMP_TYPE="explicit"
        NEXT_VERSION="$MODE_ARG"
        REASON="ditetapkan manual (v${NEXT_VERSION})"
        ;;
esac

if [ "$EXPLAIN" = true ]; then
    echo "FILE_VERSION=${FILE_VERSION}"
    echo "PREV_TAG=${PREV_TAG}"
    echo "HIGHEST_TAG=${HIGHEST_TAG}"
    echo "BASE_VERSION=${BASE_VERSION}"
    echo "COMMIT_COUNT=${COMMIT_COUNT}"
    echo "FEAT_COUNT=${FEAT_COUNT}"
    echo "FIX_COUNT=${FIX_COUNT}"
    echo "BREAKING_COUNT=${BREAKING_COUNT}"
    echo "DIRTY_COUNT=${DIRTY_COUNT}"
    echo "BUMP_TYPE=${BUMP_TYPE}"
    echo "REASON=${REASON}"
    echo "NEXT_VERSION=${NEXT_VERSION}"
else
    echo "$NEXT_VERSION"
fi
