# wdev Makefile
# Otomasi development, build, test, run, dan rilis GitHub

SHELL := /bin/bash
BIN_DIR := bin
BINARY := $(BIN_DIR)/wdev
CMD_PKG := ./cmd/wdev
VERSION_PKG := wdev/internal/version

# Ambil versi saat ini dari version.go
CURRENT_VERSION := $(shell grep 'var Version =' internal/version/version.go | cut -d '"' -f 2)

.PHONY: all
all: build

# -----------------------------------------------------------------------------
# Development & Execution
# -----------------------------------------------------------------------------
.PHONY: dev-ui
dev-ui: ## Jalankan frontend Vite dev server (port 5173)
	@echo "==> Menjalankan Frontend SvelteKit Dev Server..."
	cd frontend && pnpm dev

.PHONY: run
run: build ## Build binary dan jalankan wdev serve secara lokal di port 3200
	@echo "==> Menjalankan wdev dashboard di http://localhost:3200 ..."
	./$(BINARY) serve --port 3200 --host 0.0.0.0

.PHONY: dev
dev: run ## Alias untuk make run

# -----------------------------------------------------------------------------
# Build & Test
# -----------------------------------------------------------------------------
.PHONY: build-ui
build-ui: ## Compile frontend SvelteKit ke embedded static assets (web/static)
	@echo "==> Building Frontend SvelteKit..."
	cd frontend && pnpm install --frozen-lockfile && pnpm build

.PHONY: build
build: build-ui ## Build binary wdev dengan embedded UI dan metadata versi
	@echo "==> Building Go binary wdev (v$(CURRENT_VERSION))..."
	@mkdir -p $(BIN_DIR)
	go build -trimpath \
		-ldflags "-s -w -X $(VERSION_PKG).Version=$(CURRENT_VERSION)" \
		-o $(BINARY) $(CMD_PKG)
	@echo "✓ Binary berhasil dibuat: $(BINARY)"

.PHONY: build-go
build-go: ## Build cepat Go binary tanpa rebuild frontend (untuk testing Go)
	@echo "==> Quick build Go binary..."
	@mkdir -p $(BIN_DIR)
	go build -trimpath \
		-ldflags "-s -w -X $(VERSION_PKG).Version=$(CURRENT_VERSION)" \
		-o $(BINARY) $(CMD_PKG)
	@echo "✓ Binary berhasil dibuat: $(BINARY)"

.PHONY: test
test: ## Jalankan semua unit test
	@echo "==> Menjalankan unit test..."
	go test -v ./internal/...

.PHONY: clean
clean: ## Bersihkan direktori build binary dan cache
	@echo "==> Membersihkan direktori build..."
	rm -rf $(BIN_DIR) dist /tmp/wdev_*

# -----------------------------------------------------------------------------
# Release Automation & Changelog
# -----------------------------------------------------------------------------
.PHONY: changelog
changelog: ## Tampilkan changelog dari git commit sejak rilis terakhir
	@chmod +x scripts/changelog.sh
	@scripts/changelog.sh $(CURRENT_VERSION)

.PHONY: release
release: ## Otomatisasi rilis ke GitHub dengan changelog (Contoh: make release VERSION=2.2.0)
	@chmod +x scripts/release.sh scripts/changelog.sh
	@scripts/release.sh $(VERSION)

.PHONY: help
help: ## Tampilkan daftar perintah Makefile
	@echo "wdev Development & Release Automation"
	@echo ""
	@echo "Penggunaan:"
	@echo "  make <target>"
	@echo ""
	@echo "Target yang tersedia:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
