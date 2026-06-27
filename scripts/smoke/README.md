# Smoke tests

Smoke tests against a real Postgres DB to validate the migration and
end-to-end flows that go beyond unit tests. **Not** run by `make test`
because they require a running DB.

## Prerequisites

```bash
# Start a local Postgres (any way works; docker example below)
docker run -d --name mez-postgres-test -p 5432:5432 \
  -e POSTGRES_USER=mez_migrate \
  -e POSTGRES_PASSWORD=mez_dev_pass \
  -e POSTGRES_DB=mezgo \
  postgres:16-alpine

# Initialize roles
docker exec mez-postgres-test psql -v ON_ERROR_STOP=1 -U mez_migrate -d mezgo <<EOSQL
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_app') THEN
        CREATE ROLE mez_app WITH LOGIN INHERIT PASSWORD 'mez_dev_pass';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_platform') THEN
        CREATE ROLE mez_platform WITH LOGIN INHERIT BYPASSRLS PASSWORD 'mez_dev_pass';
    END IF;
END \$\$;
GRANT ALL ON SCHEMA public TO mez_app;
GRANT ALL ON SCHEMA public TO mez_platform;
ALTER ROLE mez_app WITH PASSWORD 'mez_dev_pass';
ALTER ROLE mez_platform WITH PASSWORD 'mez_dev_pass';
EOSQL

# Apply all migrations
MEZ_MIGRATE_DATABASE_URL="postgres://mez_migrate:mez_dev_pass@localhost:5432/mezgo?sslmode=disable" \
    make migrate-up
```

## Running

```bash
export MEZ_MASTER_KEY="$(openssl rand -base64 32)"

# 1. Validate the SystemSettingsRepo against the real DB
go run ./scripts/smoke/system_settings

# 2. End-to-end settings.Service test (Get/Set/Watch/List/SeedDefaults/InvalidateCache)
go run ./scripts/smoke/settings_service

# 3. Seed 8 Fase 10 defaults into an empty DB
go run ./scripts/smoke/seed_defaults
```

## What each test validates

| Script | What it asserts |
|---|---|
| `system_settings` | RLS fail-closed (mez_app blocked from writes), Get/Set/List/Delete cycle, `Envelope.SealSystem` round-trip |
| `settings_service` | Full Service API: defaults, Set+Get, Watch notifications, List, SeedDefaults idempotency, InvalidateCache |
| `seed_defaults` | `SeedDefaults` populates 8 known defaults (whatsmeow, ffmpeg, bus, reconcile) on an empty DB |
