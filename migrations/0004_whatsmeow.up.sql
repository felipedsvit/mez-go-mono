-- 0004_whatsmeow.up.sql
-- Fase 4 — WhatsMeow (canal informal).
-- Consolida 3 tabelas do parent (0011, 0013, 0004) com FORCE RLS (C3) e
-- mez_app sem BYPASSRLS (C4).
--
-- Tabelas:
--   - whatsapp_account_state : warmup state (E6 anti-ban) — 10-day ramp
--   - whatsapp_session_keys  : DEK por JID via LocalSealer (scaffolding V2)
--   - whatsapp_history       : HistorySync paginado (OOM guard)
--
-- A session store real do whatsmeow (sqlstore) é gerenciada pela própria lib
-- (go.mau.fi/whatsmeow/store/sqlstore); as tabelas do sqlstore são criadas
-- on-demand pelo New() com driver "pgx" e a DSN apontando para mez_app.
-- Migration não cria a schema do sqlstore (delegado à lib).

BEGIN;

-- ==========================================================================
-- 1. whatsapp_account_state (warmup + health score + timelock)
-- ==========================================================================

CREATE TABLE IF NOT EXISTS whatsapp_account_state (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    jid             TEXT NOT NULL,        -- ex.: "5511999999999@s.whatsapp.net"
    day_anchor      TIMESTAMPTZ NOT NULL DEFAULT now(),
    day_sent_count  INT NOT NULL DEFAULT 0,
    health_score    INT NOT NULL DEFAULT 100,  -- 0..100
    timelock_until  TIMESTAMPTZ,
    banned_at       TIMESTAMPTZ,           -- setado após N falhas consecutivas
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, jid)
);

-- ==========================================================================
-- 2. whatsapp_session_keys (DEK por JID via LocalSealer — scaffolding V2)
-- ==========================================================================
-- EncryptedStore (V2) usará esta DEK para cifrar colunas sensíveis do
-- sqlstore. Por enquanto é só scaffolding (D13 do pai / C9 do mono);
-- KEK vem do LocalSealer (Fase 7 ativa rotação).

CREATE TABLE IF NOT EXISTS whatsapp_session_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    jid           TEXT NOT NULL,
    wrapped_dek   BYTEA NOT NULL,           -- DEK cifrada pela KEK do tenant
    kek_version   INT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, jid)
);

-- ==========================================================================
-- 3. whatsapp_history (HistorySync paginado — OOM guard)
-- ==========================================================================
-- Limite: 1000 mensagens/tenant no primeiro start (mitigação OOM). Lote
-- de 100 por insert (ver whatsapp_history_repo.go do mono).

CREATE TABLE IF NOT EXISTS whatsapp_history (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    jid         TEXT NOT NULL,             -- conversa no whatsmeow
    msg_id      TEXT NOT NULL,             -- provider_msg_id
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, jid, msg_id)
);

-- ==========================================================================
-- 4. Indexes
-- ==========================================================================

CREATE INDEX IF NOT EXISTS idx_wa_state_tenant ON whatsapp_account_state(tenant_id);
CREATE INDEX IF NOT EXISTS idx_wa_state_banned ON whatsapp_account_state(tenant_id) WHERE banned_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_wa_session_keys_tenant ON whatsapp_session_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_wa_history_tenant_jid ON whatsapp_history(tenant_id, jid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_wa_history_created ON whatsapp_history(created_at DESC);

-- ==========================================================================
-- 5. RLS — FORCE (C3) + mez_app sem BYPASSRLS (C4)
-- ==========================================================================

ALTER TABLE whatsapp_account_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsapp_account_state FORCE  ROW LEVEL SECURITY;

ALTER TABLE whatsapp_session_keys  ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsapp_session_keys  FORCE  ROW LEVEL SECURITY;

ALTER TABLE whatsapp_history       ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsapp_history       FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON whatsapp_account_state
    FOR ALL TO mez_app
    USING      (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON whatsapp_session_keys
    FOR ALL TO mez_app
    USING      (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

CREATE POLICY tenant_isolation ON whatsapp_history
    FOR ALL TO mez_app
    USING      (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);

-- ==========================================================================
-- 6. Grants
-- ==========================================================================
-- mez_app: CRUD normal (com RLS).
-- mez_platform: SELECT/UPDATE cross-tenant para RunAsPlatform (auditado).

GRANT SELECT, INSERT, UPDATE, DELETE ON whatsapp_account_state TO mez_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON whatsapp_session_keys  TO mez_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON whatsapp_history       TO mez_app;

GRANT SELECT, UPDATE                ON whatsapp_account_state TO mez_platform;
GRANT SELECT, UPDATE                ON whatsapp_session_keys  TO mez_platform;
GRANT SELECT, UPDATE                ON whatsapp_history       TO mez_platform;

COMMIT;
