.PHONY: help tools build test lint generate openapi-gen openapi-validate templ-gen migrate-up migrate-down migrate-status docker docker-compose-up docker-compose-down tidy run run-migrate clean

GO ?= go
BIN := mez-go-mono
MIGRATE_SOURCE := file://migrations

# ==============================================================================
# Help (autodocumented)
# ==============================================================================

help:
	@echo "mez-go-mono — make targets:"
	@echo "  tools              install templ, oapi-codegen, golang-migrate"
	@echo "  build              compile single binary"
	@echo "  test               go test -race -shuffle=on"
	@echo "  test-integration   go test -race -shuffle=on -tags=integration"
	@echo "  lint               go vet + golangci-lint"
	@echo "  fmt                go fmt"
	@echo "  generate           templ generate + openapi-gen"
	@echo "  templ-gen          templ generate"
	@echo "  openapi-gen        regenerate api/openapi.gen.go"
	@echo "  openapi-validate   regenerate + fail if diff"
	@echo "  migrate-up         apply migrations (needs MEZ_MIGRATE_DATABASE_URL)"
	@echo "  migrate-down       rollback all migrations"
	@echo "  migrate-status     show current migration version"
	@echo "  docker             build image"
	@echo "  docker-compose-up  start postgres + minio + app"
	@echo "  docker-compose-down stop stack"
	@echo "  tidy               go mod tidy"
	@echo "  run                go run ./cmd/server serve"
	@echo "  run-migrate        go run ./cmd/server migrate up"
	@echo "  clean              rm -rf bin/ + go clean"

# ==============================================================================
# Tools
# ==============================================================================

tools:
	$(GO) install github.com/a-h/templ/cmd/templ@latest
	$(GO) install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

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
	@if which golangci-lint > /dev/null 2>&1; then \
		golangci-lint run ./... ; \
	else \
		echo "golangci-lint not installed; run 'make tools'" ; \
	fi

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

# ==============================================================================
# Code generation
# ==============================================================================

generate: templ-gen openapi-gen

templ-gen:
	@if which templ > /dev/null 2>&1; then \
		templ generate ; \
	else \
		echo "templ not installed; run 'make tools'" ; \
		exit 1 ; \
	fi

openapi-gen:
	@if which oapi-codegen > /dev/null 2>&1; then \
		cd api && oapi-codegen -config oapi-codegen.yaml openapi.yaml > openapi.gen.go ; \
	else \
		echo "oapi-codegen not installed; run 'make tools'" ; \
		exit 1 ; \
	fi

openapi-validate: openapi-gen
	@if [ -n "$$(git ls-files --others --exclude-standard api/openapi.gen.go 2>/dev/null)" ]; then \
		echo "INFO: api/openapi.gen.go is not yet tracked; add and commit it" ; \
	elif [ -n "$$(git status --porcelain api/openapi.gen.go 2>/dev/null)" ]; then \
		echo "ERROR: api/openapi.gen.go is out of date; run 'make openapi-gen' and commit" ; \
		git diff --stat api/openapi.gen.go ; \
		exit 1 ; \
	else \
		echo "openapi.gen.go is in sync" ; \
	fi

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
