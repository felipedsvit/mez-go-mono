#!/bin/bash
set -e

# Create roles and databases needed for the application.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- Create roles if they don't exist (migration 0001 will also ensure this)
    DO \$\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_app') THEN
            CREATE ROLE mez_app WITH LOGIN INHERIT PASSWORD 'mez_dev_pass';
        END IF;
        IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_platform') THEN
            CREATE ROLE mez_platform WITH LOGIN INHERIT BYPASSRLS PASSWORD 'mez_dev_pass';
        END IF;
    END
    \$\$;

    -- Grant connect on the database
    GRANT CONNECT ON DATABASE mezgo TO mez_app;
    GRANT CONNECT ON DATABASE mezgo TO mez_platform;
    GRANT CREATE ON DATABASE mezgo TO mez_app;

    -- Create schema if not exists
    GRANT ALL ON SCHEMA public TO mez_app;
    GRANT ALL ON SCHEMA public TO mez_platform;
EOSQL
