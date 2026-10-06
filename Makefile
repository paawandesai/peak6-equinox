# Equinox command interface. See CLAUDE.md § Commands.
# Only M1-02 adds targets here (deploy, deploy-staging).

# Tool versions (Tier 1; logged in docs/log.md). Bump here only.
GOLANGCI_LINT_VERSION := v2.14.0
GOOSE_VERSION         := v3.28.0
GOVULNCHECK_VERSION   := v1.8.0
GITLEAKS_VERSION      := v8.30.1

# Local env: .env is optional and gitignored. Its variables are exported to every recipe.
-include .env
export

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# Build tools with the same toolchain as the project (go.mod), so golangci-lint
# can analyse code at that Go version.
GO_TOOLCHAIN := $(shell awk '$$1 == "toolchain" {print $$2}' go.mod)

BIN    := $(CURDIR)/bin
STAMPS := $(BIN)/.stamps

GOLANGCI_LINT := $(BIN)/golangci-lint
GOOSE         := $(BIN)/goose
GOVULNCHECK   := $(BIN)/govulncheck
GITLEAKS      := $(BIN)/gitleaks

# Each tool is (re)installed when its pinned version changes.
TOOL_STAMPS := $(STAMPS)/golangci-lint-$(GOLANGCI_LINT_VERSION) \
               $(STAMPS)/goose-$(GOOSE_VERSION) \
               $(STAMPS)/govulncheck-$(GOVULNCHECK_VERSION) \
               $(STAMPS)/gitleaks-$(GITLEAKS_VERSION)

define go-install
	@mkdir -p $(BIN) $(STAMPS)
	GOBIN=$(BIN) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install $(1)
	@rm -f $(STAMPS)/$(2)-* && touch $@
endef

$(STAMPS)/golangci-lint-$(GOLANGCI_LINT_VERSION):
	$(call go-install,github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION),golangci-lint)
$(STAMPS)/goose-$(GOOSE_VERSION):
	$(call go-install,github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION),goose)
$(STAMPS)/govulncheck-$(GOVULNCHECK_VERSION):
	$(call go-install,golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION),govulncheck)
$(STAMPS)/gitleaks-$(GITLEAKS_VERSION):
	$(call go-install,github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION),gitleaks)

.PHONY: help setup db-up db-down migrate seed dev dev-fixtures test test-security e2e \
        cover lint typecheck scan secrets vuln check llm-eval-live

help: ## List targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

setup: $(TOOL_STAMPS) ## Download modules and install pinned tools into ./bin
	go mod download

db-up: ## Start local Postgres 16 (port 5432) and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data volume is kept)
	docker compose down

migrate: ## Run database migrations
	go run ./cmd/equinox migrate

seed: ## Load one offline fixture round
	go run ./cmd/equinox seed

dev: ## Run the app on http://localhost:$${PORT:-8080}
	go run ./cmd/equinox serve

dev-fixtures: ## Serve recorded venue fixtures on port 8090
	go run ./cmd/equinox fixture-server

test: ## Run all tests with the race detector (needs make db-up)
	go test -race ./...

test-security: ## Run abuse-case tests only
	go test ./tests/security/...

e2e: ## Run the end-to-end smoke test
	@if [ -d tests/e2e ]; then go test -tags e2e ./tests/e2e/...; \
	else echo "e2e: not implemented yet (ticket M1-10)"; fi

cover: ## Fail if statement coverage over ./internal/... is below 80%
	bash scripts/check-coverage.sh

lint: $(STAMPS)/golangci-lint-$(GOLANGCI_LINT_VERSION) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

typecheck: ## Build and vet everything
	go build ./... && go vet ./...

secrets: $(STAMPS)/gitleaks-$(GITLEAKS_VERSION) ## Scan git history for secrets
	$(GITLEAKS) git --redact --no-banner --config .gitleaks.toml .

vuln: $(STAMPS)/govulncheck-$(GOVULNCHECK_VERSION) ## Check dependencies for known vulnerabilities
	$(GOVULNCHECK) ./...

scan: secrets vuln ## gitleaks + govulncheck

check: typecheck lint test cover scan ## Everything CI runs; use before every push

llm-eval-live: ## Live LLM eval (manual, admin only, never CI)
	@if [ -d internal/llm ]; then go test -tags llmlive ./internal/llm/...; \
	else echo "llm-eval-live: not implemented yet (ticket M1-11)"; fi
