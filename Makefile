.PHONY: tools build test lint generate openapi-gen migrate-up migrate-down docker docker-compose-up tidy

GO ?= go
BIN := mez-go-mono
MIGRATE_SOURCE := file://migrations

# ==============================================================================
# Tools
# ==============================================================================

tools:
	$(GO) install github.com/a-h/templ/cmd/templ@latest
	$(GO) install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

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

# ==============================================================================
# Code generation
# ==============================================================================

generate: templ-gen openapi-gen

templ-gen:
	@which templ > /dev/null 2>&1 && templ generate || echo "templ not installed; skipping"

openapi-gen:
	@which oapi-codegen > /dev/null 2>&1 && oapi-codegen -generate types,server -package api api/openapi.yaml > api/openapi.gen.go || echo "oapi-codegen not installed; skipping"

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

clean:
	rm -rf bin/
	$(GO) clean -cache
