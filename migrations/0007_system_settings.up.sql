-- 0007_system_settings.up.sql
-- Fase 10 (#177): migração de app-level config de env vars para DB.
--
-- system_settings substitui as env vars MEZ_WHATSMEOW_* e futuras
-- app-level configs. Valores são cifrados com a master KEK
-- (AES-256-GCM direto, sem DEK/tenant — se chama "system" porque é
-- platform-wide, não por tenant).
--
-- Decisão arquitetural: NÃO usar env vars para app config (issue #177).
-- O único env var que sobra é MEZ_DATABASE_URL (bootstrap mínimo
-- para abrir a conexão com o DB) + master key (que pode vir de
-- arquivo, vault, ou env).
--
-- Acesso:
--   - mez_app: SELECT apenas (para o app ler config no boot)
--   - mez_platform: SELECT, INSERT, UPDATE, DELETE (admin)
--   - Audit log: admin_audit_log com action='setting.update'
--
-- Defaults são cifrados com a KEK no momento do seed (handled pelo
-- código Go na primeira inicialização, não na migration). A migration
-- cria a tabela vazia; o seed dos defaults é responsabilidade do
-- settings.Service.SeedDefaults().

BEGIN;

-- ==========================================================================
-- 1. Tabela system_settings
-- ==========================================================================

CREATE TABLE IF NOT EXISTS system_settings (
    key              TEXT PRIMARY KEY,
    value_encrypted  BYTEA NOT NULL,           -- AES-256-GCM(KEK, value_json)
    kek_version      INT  NOT NULL DEFAULT 1,  -- bumped em rotação de KEK
    description      TEXT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT                      -- actor email (mez_platform)
);

-- ==========================================================================
-- 2. Indexes
-- ==========================================================================

CREATE INDEX IF NOT EXISTS idx_settings_updated_at ON system_settings(updated_at DESC);

-- ==========================================================================
-- 3. RLS — mez_app lê, mez_platform escreve (C3+C4 fail-closed)
-- ==========================================================================

ALTER TABLE system_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE system_settings FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_app_read ON system_settings
    FOR SELECT TO mez_app
    USING (true);  -- app role lê todas as settings (não há multi-tenant aqui)

CREATE POLICY platform_full_access ON system_settings
    FOR ALL TO mez_platform
    USING      (true)
    WITH CHECK (true);

-- mez_migrate: precisa de acesso total para rodar migrations.
-- Como mez_migrate usa SECURITY DEFINER no padrão do projeto, não precisa
-- de policy explícita. Mas adicionamos para garantir.
CREATE POLICY migrate_full_access ON system_settings
    FOR ALL TO mez_migrate
    USING      (true)
    WITH CHECK (true);

-- ==========================================================================
-- 4. Grants
-- ==========================================================================

GRANT SELECT ON system_settings TO mez_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON system_settings TO mez_platform;
GRANT SELECT, INSERT, UPDATE, DELETE ON system_settings TO mez_migrate;

-- ==========================================================================
-- 5. Audit log extension (opcional)
-- ==========================================================================
-- As mudanças em system_settings são auditadas via admin_audit_log.
-- O código Go (settings.Service.Set) escreve o registro com
-- action='setting.update' e metadata={key, old_value_hash, new_value_hash}.

COMMIT;
