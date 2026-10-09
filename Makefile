# Equinox command interface. See CLAUDE.md § Commands.

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

# golangci-lint: the official release binary, checked against the release
# checksums (building it from source takes longer than the CI job timeout).
GOLANGCI_LINT_URL := https://github.com/golangci/golangci-lint/releases/download/$(GOLANGCI_LINT_VERSION)
$(STAMPS)/golangci-lint-$(GOLANGCI_LINT_VERSION):
	@mkdir -p $(BIN) $(STAMPS)
	v=$(GOLANGCI_LINT_VERSION:v%=%); pkg=golangci-lint-$$v-$$(go env GOOS)-$$(go env GOARCH); \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	curl -fsSL -o "$$tmp/$$pkg.tar.gz" "$(GOLANGCI_LINT_URL)/$$pkg.tar.gz"; \
	curl -fsSL -o "$$tmp/sums.txt" "$(GOLANGCI_LINT_URL)/golangci-lint-$$v-checksums.txt"; \
	sum=sha256sum; command -v sha256sum >/dev/null || sum="shasum -a 256"; \
	(cd "$$tmp" && grep " $$pkg.tar.gz\$$" sums.txt | $$sum -c -); \
	tar -xzf "$$tmp/$$pkg.tar.gz" -C "$$tmp"; \
	install -m 0755 "$$tmp/$$pkg/golangci-lint" $(GOLANGCI_LINT)
	@rm -f $(STAMPS)/golangci-lint-* && touch $@
$(STAMPS)/goose-$(GOOSE_VERSION):
	$(call go-install,github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION),goose)
$(STAMPS)/govulncheck-$(GOVULNCHECK_VERSION):
	$(call go-install,golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION),govulncheck)
$(STAMPS)/gitleaks-$(GITLEAKS_VERSION):
	$(call go-install,github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION),gitleaks)

.PHONY: help setup db-up db-down migrate seed dev dev-fixtures test test-security e2e \
        cover lint typecheck scan secrets vuln check eval llm-eval-live smoke-live deploy deploy-staging

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
	go test -race -timeout 20m ./...

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

eval: ## Golden-set precision and recall per type, rules only (reads testdata/golden/labels.yaml)
	go run ./cmd/equinox eval

llm-eval-live: ## Live LLM eval (manual, admin only, never CI)
	@if [ -d internal/llm ]; then go test -tags llmlive ./internal/llm/...; \
	else echo "llm-eval-live: not implemented yet (ticket M1-11)"; fi

smoke-live: ## Live smoke test against the live URL (M4-05)
	bash scripts/smoke-live.sh

# Cloud Run deploys (M1-02, D-033). Resources come from scripts/gcp-bootstrap.sh.
GCP_PROJECT ?= peak6-equinox-510816
GCP_REGION  ?= us-central1
SERVICE      := equinox
RUN_SA       := equinox-run@$(GCP_PROJECT).iam.gserviceaccount.com
SQL_INSTANCE := $(GCP_PROJECT):$(GCP_REGION):equinox-pg
IMAGE        := $(GCP_REGION)-docker.pkg.dev/$(GCP_PROJECT)/equinox/$(SERVICE)
DEPLOY_FLAGS := --project=$(GCP_PROJECT) --region=$(GCP_REGION) --source=. --image=$(IMAGE) \
                --service-account=$(RUN_SA) --add-cloudsql-instances=$(SQL_INSTANCE) \
                --allow-unauthenticated --max-instances=2 --quiet
APP_SECRETS  := ADMIN_PASSWORD=equinox-admin-password:latest
# The Anthropic key secret starts with no version (scripts/gcp-bootstrap.sh);
# it is mounted only once the human has added the real key.
ANTHROPIC_SECRET = $$(gcloud secrets versions list equinox-anthropic-key --project=$(GCP_PROJECT) \
                   --filter=state=enabled --limit=1 --format='value(name)' 2>/dev/null | grep -q . \
                   && echo ,ANTHROPIC_API_KEY=equinox-anthropic-key:latest)

# deploy also moves traffic back to the latest revision: a --no-traffic
# staging deploy pins traffic to a fixed revision, and later deploys keep that pin.
deploy: ## Deploy prod to Cloud Run (needs gcloud credentials)
	gcloud run deploy $(SERVICE) $(DEPLOY_FLAGS) \
	  --set-env-vars=APP_ENV=prod,LLM_ENABLED=false,ADMIN_USER=admin \
	  --set-secrets=DATABASE_URL=equinox-database-url:latest,$(APP_SECRETS)$(ANTHROPIC_SECRET)
	gcloud run services update-traffic $(SERVICE) --project=$(GCP_PROJECT) --region=$(GCP_REGION) --to-latest --quiet

deploy-staging: ## Deploy a no-traffic revision tagged staging (needs gcloud credentials)
	gcloud run deploy $(SERVICE) $(DEPLOY_FLAGS) --tag=staging --no-traffic \
	  --set-env-vars=APP_ENV=staging,LLM_ENABLED=false,ADMIN_USER=admin \
	  --set-secrets=DATABASE_URL=equinox-staging-database-url:latest,$(APP_SECRETS)$(ANTHROPIC_SECRET)

review-window-on: ## Set min-instances=1 for the review window (M4-04, D-101)
	bash scripts/review-window.sh on

review-window-off: ## Revert to min-instances=0 after the review window (M4-04, D-101)
	bash scripts/review-window.sh off

review-window-status: ## Show current min-instances setting (M4-04, D-101)
	bash scripts/review-window.sh status
