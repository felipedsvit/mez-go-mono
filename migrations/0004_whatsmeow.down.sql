-- 0004_whatsmeow.down.sql
BEGIN;

DROP TABLE IF EXISTS whatsapp_history       CASCADE;
DROP TABLE IF EXISTS whatsapp_session_keys CASCADE;
DROP TABLE IF EXISTS whatsapp_account_state CASCADE;

COMMIT;
