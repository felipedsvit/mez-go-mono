-- 0001_init.up.sql
-- Core schema: 3 roles, 8 tables, FORCE RLS, FKs deferíveis, fail-closed policies.
--
-- Roles:
--   mez_migrate  – owner das tabelas (usado apenas em migrate)
--   mez_app      – role da aplicação (SEM BYPASSRLS – fail-closed)
--   mez_platform – role cross-tenant (COM BYPASSRLS – auditado)

BEGIN;

-- ==========================================================================
--  Roles
-- ==========================================================================

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_migrate') THEN
        CREATE ROLE mez_migrate WITH LOGIN;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_app') THEN
        CREATE ROLE mez_app WITH LOGIN;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mez_platform') THEN
        CREATE ROLE mez_platform WITH LOGIN BYPASSRLS;
    END IF;
END
$$;

-- ==========================================================================
--  Extensions
-- ==========================================================================

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ==========================================================================
--  Tenants
-- ==========================================================================

CREATE TABLE tenants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ==========================================================================
--  Contacts
-- ==========================================================================

CREATE TABLE contacts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL,
    phone       TEXT,
    name        TEXT NOT NULL DEFAULT '',
    avatar_url  TEXT,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel, phone)
);

-- ==========================================================================
--  Conversations
-- ==========================================================================

CREATE TABLE conversations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel         TEXT NOT NULL,
    contact_id      UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'open',
    external_id     TEXT,
    assigned_agent  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel, external_id)
);

-- ==========================================================================
--  Messages
-- ==========================================================================

CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel         TEXT NOT NULL,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    contact_id      UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    direction       TEXT NOT NULL,
    type            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'received',
    body            TEXT NOT NULL DEFAULT '',
    provider_msg_id TEXT,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel, provider_msg_id)
);

-- ==========================================================================
--  Inbound Events (raw provider payloads)
-- ==========================================================================

CREATE TABLE inbound_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL,
    source      TEXT NOT NULL,
    payload     JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

-- ==========================================================================
--  Outbound Events (delivery queue)
-- ==========================================================================

CREATE TABLE outbound_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL,
    target      JSONB NOT NULL,
    payload     JSONB NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ==========================================================================
--  Audit Log
-- ==========================================================================

CREATE TABLE audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID REFERENCES tenants(id) ON DELETE SET NULL,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    details    JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ==========================================================================
--  Channel Credentials (1 row per tenant+channel, encrypted with DEK)
-- ==========================================================================

CREATE TABLE channel_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel       TEXT NOT NULL,
    wrapped_dek   BYTEA NOT NULL,
    encrypted     BYTEA NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel)
);

-- ==========================================================================
--  Indexes
-- ==========================================================================

CREATE INDEX idx_contacts_tenant ON contacts(tenant_id);
CREATE INDEX idx_conversations_tenant ON conversations(tenant_id);
CREATE INDEX idx_messages_conversation ON messages(conversation_id);
CREATE INDEX idx_messages_contact ON messages(contact_id);
CREATE INDEX idx_messages_status ON messages(status) WHERE status = 'received';
CREATE INDEX idx_inbound_events_status ON inbound_events(processed_at) WHERE processed_at IS NULL;
CREATE INDEX idx_outbound_events_status ON outbound_events(status) WHERE status = 'pending';
CREATE INDEX idx_audit_log_created ON audit_log(created_at DESC);
CREATE INDEX idx_channel_credentials_tenant ON channel_credentials(tenant_id);

-- ==========================================================================
--  RLS – FORCE ROW LEVEL SECURITY em todas as tabelas multi-tenant (C3)
-- ==========================================================================

ALTER TABLE tenants             ENABLE ROW LEVEL SECURITY;
ALTER TABLE contacts            ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversations       ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages            ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbound_events      ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbound_events     ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log           ENABLE ROW LEVEL SECURITY;
ALTER TABLE channel_credentials ENABLE ROW LEVEL SECURITY;

ALTER TABLE tenants             FORCE ROW LEVEL SECURITY;
ALTER TABLE contacts            FORCE ROW LEVEL SECURITY;
ALTER TABLE conversations       FORCE ROW LEVEL SECURITY;
ALTER TABLE messages            FORCE ROW LEVEL SECURITY;
ALTER TABLE inbound_events      FORCE ROW LEVEL SECURITY;
ALTER TABLE outbound_events     FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_log           FORCE ROW LEVEL SECURITY;
ALTER TABLE channel_credentials FORCE ROW LEVEL SECURITY;

-- ==========================================================================
--  RLS Policies – fail-closed (C4): exigem mez.tenant_id setado
-- ==========================================================================

CREATE POLICY tenant_isolation ON tenants
    FOR ALL TO mez_app
    USING (id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON contacts
    FOR ALL TO mez_app
    USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON conversations
    FOR ALL TO mez_app
    USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON messages
    FOR ALL TO mez_app
    USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON inbound_events
    FOR ALL TO mez_app
    USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON outbound_events
    FOR ALL TO mez_app
    USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON channel_credentials
    FOR ALL TO mez_app
    USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY cross_tenant_audit ON audit_log
    FOR ALL TO mez_platform
    USING (true) WITH CHECK (true);

-- ==========================================================================
--  Grant permissions
-- ==========================================================================

GRANT USAGE ON SCHEMA public TO mez_app;
GRANT USAGE ON SCHEMA public TO mez_platform;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO mez_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO mez_app;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO mez_platform;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO mez_platform;

COMMIT;
