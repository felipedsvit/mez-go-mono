-- 0006_kek_version.down.sql
-- Reverte: remove kek_version e rotation_window_until de channel_credentials.
-- O índice é removido em cascata quando a coluna some.

BEGIN;

DROP INDEX IF EXISTS idx_channel_credentials_kek_version;

ALTER TABLE channel_credentials
    DROP COLUMN IF EXISTS rotation_window_until;

ALTER TABLE channel_credentials
    DROP COLUMN IF EXISTS kek_version;

COMMIT;
