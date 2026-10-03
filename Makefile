.PHONY: help test vet lint build dev sync favicon-sync start docker-build docker-up docker-down docker-logs changelog orca-evidence verify test-live test-architecture worker-test

# Go toolchain. Override to point at a specific toolchain, e.g.
# `make test GO=/opt/go/bin/go`, for hosts that do not put `go` on PATH.
GO ?= go

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ── Go ──────────────────────────────────────────────────────────────

test: ## Run Go and worker tests
	$(GO) test ./...
	$(MAKE) -C worker test

vet: ## Run Go vet
	$(GO) vet ./...

test-architecture: ## Run the architecture/import boundary tests
	$(GO) test ./internal/app -run 'TestImportConstraints|TestDutyBoundaries' -count=1

test-live: ## Run the live OrcaRouter acceptance tests (requires ORCAROUTER_API_KEY)
	$(GO) test ./internal/providers/orcarouter/ -run TestLive -count=1 -v

worker-test: ## Run the worker daemon tests
	cd worker && npm test

# ── Frontend ────────────────────────────────────────────────────────

frontend-deps: ## Install frontend dependencies
	cd frontend && npm ci

frontend-lint: ## Lint frontend
	cd frontend && npm run lint

frontend-build: ## Build frontend
	cd frontend && npm run build

sync: frontend-build ## Build frontend and sync embedded assets into Go
	cd frontend && npm run sync

favicon-sync: sync ## Sync favicon suite into the embedded webui static assets
	@echo "synced: frontend/public -> internal/webui/static"
	@ls -1 frontend/public/favicon*.svg frontend/public/apple-touch-icon.svg frontend/public/og-card.svg frontend/public/site.webmanifest 2>/dev/null

# ── Docker ──────────────────────────────────────────────────────────

docker-build: ## Build container image from source
	cd deploy && docker compose up -d --build

docker-up: ## Start service (pull pre-built image)
	cd deploy && docker compose pull && docker compose up -d

docker-down: ## Stop service
	cd deploy && docker compose down

docker-logs: ## Follow service logs
	cd deploy && docker compose logs -f qoder-api-proxy

# ── Aggregate ───────────────────────────────────────────────────────

lint: vet frontend-lint ## Run all linters

verify: ## Run the full local gate: vet, Go + worker tests, frontend build and lint
	$(GO) vet ./...
	$(GO) test ./...
	$(MAKE) -C worker test
	cd frontend && npm install --no-package-lock --no-audit --no-fund && npm run build && npm run lint

orca-evidence: ## Capture the OrcaRouter console evidence screenshots
	GO_BIN="$(GO)" python3 scripts/orca_evidence/test_orcarouter_gui.py

changelog: ## Validate bilingual changelog
	python3 scripts/release-notes.py self-test
	python3 scripts/release-notes.py validate

check: test lint frontend-build changelog ## Run tests, linters, and build frontend

start: ## Start Docker deployment and print a first-run API key
	./scripts/start.sh

dev: ## Run Go server locally (requires .env or env vars)
	$(GO) run ./cmd/server

