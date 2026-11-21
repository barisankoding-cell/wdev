#!/usr/bin/env bash
# scripts/changelog.sh - Mengenerate changelog Markdown dari git commit
set -euo pipefail

TARGET_VERSION="${1:-}"
PREV_TAG=$(git describe --tags --abbrev=0 --match "v[0-9]*.[0-9]*.[0-9]*" 2>/dev/null || echo "")

if [ -n "$PREV_TAG" ]; then
    RANGE="${PREV_TAG}..HEAD"
    TITLE_RANGE="sejak ${PREV_TAG}"
else
    RANGE="HEAD"
    TITLE_RANGE="semua commit"
fi

if [ -n "$TARGET_VERSION" ]; then
    CLEAN_VER="${TARGET_VERSION#v}"
    echo "## [v${CLEAN_VER}] - $(date +'%Y-%m-%d')"
    echo ""
fi

# Kumpulkan commit berdasarkan tipe Conventional Commits
FEATS=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^feat" || true)
FIXES=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^fix" || true)
PERFS=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^\(perf\|refactor\)" || true)
CHORES=$(git log "$RANGE" --pretty=format:"- %s (%h)" --grep="^\(chore\|build\|ci\|docs\|test\)" || true)
OTHERS=$(git log "$RANGE" --pretty=format:"- %s (%h)" --invert-grep --grep="^\(feat\|fix\|perf\|refactor\|chore\|build\|ci\|docs\|test\)" || true)

HAS_CONTENT=false

if [ -n "$FEATS" ]; then
    echo "### 🚀 Fitur Baru"
    echo "$FEATS"
    echo ""
    HAS_CONTENT=true
fi

if [ -n "$FIXES" ]; then
    echo "### 🐛 Perbaikan Bug"
    echo "$FIXES"
    echo ""
    HAS_CONTENT=true
fi

if [ -n "$PERFS" ]; then
    echo "### ⚡ Peningkatan & Refactoring"
    echo "$PERFS"
    echo ""
    HAS_CONTENT=true
fi

if [ -n "$CHORES" ]; then
    echo "### 🔧 Pemeliharaan & Tooling"
    echo "$CHORES"
    echo ""
    HAS_CONTENT=true
fi

if [ -n "$OTHERS" ]; then
    echo "### 📝 Perubahan Lainnya"
    echo "$OTHERS"
    echo ""
    HAS_CONTENT=true
fi

if [ "$HAS_CONTENT" = false ]; then
    echo "- Pembaruan rutin dan perbaikan sistem."
    echo ""
fi
