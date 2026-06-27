-- 0003_outbox_fks_indexes.down.sql
-- Reverte a migration 0003.

BEGIN;

DROP INDEX IF EXISTS idx_messages_tenant_routed_at;
DROP INDEX IF EXISTS idx_messages_received_created;
DROP INDEX IF EXISTS idx_outbox_tenant_msg;
DROP INDEX IF EXISTS idx_outbox_tenant_status_created;

ALTER TABLE messages DROP COLUMN IF EXISTS notified_at;
ALTER TABLE messages DROP COLUMN IF EXISTS routed_at;

ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS fk_messages_conversation,
    DROP CONSTRAINT IF EXISTS fk_messages_contact;

ALTER TABLE conversations
    DROP CONSTRAINT IF EXISTS fk_conversations_contact;

ALTER TABLE messages
    ADD CONSTRAINT messages_conversation_id_fkey
        FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    ADD CONSTRAINT messages_contact_id_fkey
        FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE;

ALTER TABLE conversations
    ADD CONSTRAINT conversations_contact_id_fkey
        FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE CASCADE;

REVOKE SELECT, INSERT, UPDATE, DELETE ON outbound_events FROM mez_platform;
REVOKE SELECT, UPDATE                     ON messages      FROM mez_platform;

COMMIT;
