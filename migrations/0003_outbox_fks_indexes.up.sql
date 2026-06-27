-- 0003_outbox_fks_indexes.up.sql
-- Phase 2 groundwork (issues #34, #35):
--   - C6: tornar FKs internas DEFERRABLE INITIALLY DEFERRED.
--     Restore topológico (Fase 6) depende disto. Por enquanto é groundwork;
--     nenhuma query existente viola a ordem porque inserts sempre respeitam
--     a topologia contacts → conversations → messages.
--   - D3: índices outbox para poll de fallback e dedup.
--   - C1: índices reconciler (status='received' já tem índice parcial em 0001).
--   - Estados inbound: routed_at, notified_at em messages.

BEGIN;

-- ==========================================================================
-- 1. FKs deferíveis (C6 groundwork)
-- ==========================================================================
-- Importante: como as FKs já foram criadas em 0001, precisamos dropar e
-- recriar com DEFERRABLE. Como as tabelas têm dados em testes, isso é
-- aceitável; em produção é uma migração pequena.
--
-- Estratégia: dropar FK por nome, recriar com DEFERRABLE INITIALLY DEFERRED.
-- Os nomes gerados pelo Postgres são <tablename>_<fkcol>_fkey por padrão,
-- mas em 0001 não demos nome explícito — então o nome gerado segue o padrão
-- do Postgres 16: fk_messages_conversation, fk_messages_contact, etc.

ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_conversation_id_fkey,
    DROP CONSTRAINT IF EXISTS messages_contact_id_fkey;

ALTER TABLE conversations
    DROP CONSTRAINT IF EXISTS conversations_contact_id_fkey;

ALTER TABLE messages
    ADD CONSTRAINT fk_messages_conversation
        FOREIGN KEY (conversation_id) REFERENCES conversations(id)
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT fk_messages_contact
        FOREIGN KEY (contact_id) REFERENCES contacts(id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE conversations
    ADD CONSTRAINT fk_conversations_contact
        FOREIGN KEY (contact_id) REFERENCES contacts(id)
        DEFERRABLE INITIALLY DEFERRED;

-- ==========================================================================
-- 2. Colunas routed_at / notified_at em messages
-- ==========================================================================

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS routed_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS notified_at TIMESTAMPTZ;

-- ==========================================================================
-- 3. Índices outbox (D3)
-- ==========================================================================
-- O relay poll precisa de scan eficiente por (status, created_at); o claim
-- usa FOR UPDATE SKIP LOCKED que se beneficia do índice em (status).

CREATE INDEX IF NOT EXISTS idx_outbox_tenant_status_created
    ON outbound_events (tenant_id, status, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_outbox_tenant_msg
    ON outbound_events (tenant_id, (payload->>'message_id'))
    WHERE payload ? 'message_id';

-- ==========================================================================
-- 4. Índices reconciler (C1)
-- ==========================================================================
-- O reconciler varre messages WHERE status='received' e marca como 'routed'.
-- 0001 já tem idx_messages_status (partial). Adicionamos um índice composto
-- para suportar SELECT ... FOR UPDATE SKIP LOCKED ordenado por created_at.

CREATE INDEX IF NOT EXISTS idx_messages_received_created
    ON messages (created_at)
    WHERE status = 'received';

CREATE INDEX IF NOT EXISTS idx_messages_tenant_routed_at
    ON messages (tenant_id, routed_at)
    WHERE routed_at IS NOT NULL;

-- ==========================================================================
-- 5. Grant outbox/inbound a mez_platform (RunAsPlatform itera cross-tenant)
-- ==========================================================================
-- mez_app já tem grants de 0001. mez_platform precisa para o relay iterar
-- por todos os tenants.

GRANT SELECT, INSERT, UPDATE, DELETE ON outbound_events TO mez_platform;
GRANT SELECT, UPDATE                     ON messages     TO mez_platform;

COMMIT;
