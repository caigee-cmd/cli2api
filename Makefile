.PHONY: help test vet lint build dev sync favicon-sync start docker-build docker-up docker-down docker-logs changelog

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ── Go ──────────────────────────────────────────────────────────────

test: ## Run Go and worker tests
	go test ./...
	$(MAKE) -C worker test

vet: ## Run Go vet
	go vet ./...

# ── Frontend ────────────────────────────────────────────────────────

frontend-deps: ## Install frontend dependencies
	cd frontend && npm ci

frontend-lint: ## Lint frontend
	cd frontend && npm run lint

frontend-build: ## Build frontend
	cd frontend && npm run build

sync: frontend-build ## Build frontend and sync embedded assets into Go
	cd frontend && npm run sync

favicon-sync: sync ## Sync favicon suite and OG card to docs/assets/ for README relative links
	cp frontend/public/og-card.svg docs/assets/og-card.svg
	@echo "synced: frontend/public/og-card.svg -> docs/assets/og-card.svg"
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

changelog: ## Validate bilingual changelog
	python3 scripts/release-notes.py self-test
	python3 scripts/release-notes.py validate

check: test lint frontend-build changelog ## Run tests, linters, and build frontend

start: ## Start Docker deployment and print a first-run API key
	./scripts/start.sh

dev: ## Run Go server locally (requires .env or env vars)
	go run ./cmd/server

