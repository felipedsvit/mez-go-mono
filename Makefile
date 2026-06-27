.PHONY: tools build test lint generate openapi-gen openapi-validate migrate-up migrate-down docker docker-compose-up tidy govulncheck rotate-kek templ-gen templ-check smoke

GO ?= go
BIN := mez-go-mono
MIGRATE_SOURCE := file://migrations
export PATH := $(HOME)/go/bin:$(PATH)

# ==============================================================================
# Tools
# ==============================================================================

tools:
	$(GO) install github.com/a-h/templ/cmd/templ@latest
	$(GO) install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@v1.1.4

# ==============================================================================
# Build
# ==============================================================================

build:
	$(GO) mod tidy
	$(GO) build -ldflags="-s -w" -trimpath -o bin/$(BIN) ./cmd/server

build-race:
	$(GO) build -race -o bin/$(BIN)-race ./cmd/server

# ==============================================================================
# Test
# ==============================================================================

test:
	$(GO) test -race -shuffle=on -count=1 ./...

test-verbose:
	$(GO) test -race -shuffle=on -v -count=1 ./...

test-integration:
	$(GO) test -race -shuffle=on -count=1 -tags=integration ./...

# ==============================================================================
# Lint
# ==============================================================================

lint:
	$(GO) vet ./...
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed; skipping"

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

# govulncheck roda análise de vulnerabilidades conhecidas sobre o módulo.
# Fase 7 #93: alvo do CI. `make tools` instala govulncheck v1.1.4.
# Exit code 0 = sem vulns; 3 = vulns encontradas; 1 = erro de execução.
# Em dev pode-se ignorar com `make govulncheck || true`.
govulncheck:
	@which govulncheck > /dev/null 2>&1 || $(GO) install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	govulncheck ./...

# ==============================================================================
# Code generation
# ==============================================================================

generate: templ-gen openapi-gen

# templ-gen roda `templ generate` em todos os .templ do repo. Falha
# (não skip) se templ não estiver instalado — CI exige o tool.
templ-gen:
	@which templ > /dev/null 2>&1 || (echo "templ not installed; run 'make tools'"; exit 1)
	templ generate

# templ-check: roda `templ generate` e falha se houver drift em arquivos
# *_templ.go (regenera _templ.go, diff contra o commit). Espelha
# `openapi-validate` para a parte templ.
templ-check: templ-gen
	@git diff --exit-code '*.templ.go' '**/*_templ.go' > /dev/null || (echo "templ drift detectado — rode 'make templ-gen' e commit"; exit 1)

openapi-gen:
	@which oapi-codegen > /dev/null 2>&1 && oapi-codegen -generate types,chi-server -package api api/openapi.yaml > api/openapi.gen.go || echo "oapi-codegen not installed; skipping"

# openapi-validate roda `openapi-gen` e falha se o arquivo gerado mudou.
# Fase 7 #93: garante que o commit inclui o .gen.go sincronizado com o spec.
# Exit 0 = spec já estava em sync; 1 = drift detectado (re-commit .gen.go).
openapi-validate: openapi-gen
	@git diff --exit-code api/openapi.gen.go > /dev/null || (echo "openapi.gen.go drift detectado — rode 'make openapi-gen' e commit" && exit 1)

# ==============================================================================
# Database migrations
# ==============================================================================

migrate-up:
	migrate -source $(MIGRATE_SOURCE) -database "$(MEZ_MIGRATE_DATABASE_URL)" up

migrate-down:
	migrate -source $(MIGRATE_SOURCE) -database "$(MEZ_MIGRATE_DATABASE_URL)" down -all

migrate-status:
	migrate -source $(MIGRATE_SOURCE) -database "$(MEZ_MIGRATE_DATABASE_URL)" version

# ==============================================================================
# Docker
# ==============================================================================

docker:
	docker build -f deployments/Dockerfile -t mez-go-mono:dev .

docker-compose-up:
	docker compose -f deployments/docker-compose.yml up -d --build

docker-compose-down:
	docker compose -f deployments/docker-compose.yml down -v

docker-compose-logs:
	docker compose -f deployments/docker-compose.yml logs -f

# ==============================================================================
# Development helpers
# ==============================================================================

tidy:
	$(GO) mod tidy

run:
	$(GO) run ./cmd/server serve

run-migrate:
	$(GO) run ./cmd/server migrate up

# rotate-kek invoca o subcomando CLI (Fase 7 #92). Requer MEZ_MASTER_KEY
# e MEZ_MASTER_KEY_NEW (ou _FILE) no env. Para dry-run, use:
#   make rotate-kek ARGS="--dry-run"
rotate-kek:
	$(GO) run ./cmd/server rotate-kek --actor "operator:$${USER:-unknown}" $(ARGS)

clean:
	rm -rf bin/
	$(GO) clean -cache

# ==============================================================================
# Smoke tests (require real Postgres — see scripts/smoke/README.md)
# ==============================================================================

# smoke roda os 3 smoke tests contra um Postgres local. Requer:
#   - MEZ_MASTER_KEY no env (32 bytes base64)
#   - Postgres acessível em localhost:5432 com mez_migrate/mez_app/mez_platform
#   - Migrations aplicadas (make migrate-up)
# Use scripts/smoke/README.md para subir o ambiente.
smoke:
	@if [ -z "$$MEZ_MASTER_KEY" ]; then echo "MEZ_MASTER_KEY required"; exit 1; fi
	@echo "===== 1/3 system_settings repo ====="
	$(GO) run ./scripts/smoke/system_settings
	@echo
	@echo "===== 2/3 settings_service end-to-end ====="
	$(GO) run ./scripts/smoke/settings_service
	@echo
	@echo "===== 3/3 seed_defaults ====="
	$(GO) run ./scripts/smoke/seed_defaults
