-- 0007_system_settings.down.sql
-- Reverte a Fase 10 (#177) — remoção de system_settings.

BEGIN;

DROP TABLE IF EXISTS system_settings;

COMMIT;
