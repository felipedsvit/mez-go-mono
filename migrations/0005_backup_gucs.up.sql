-- 0005_backup_gucs.up.sql
-- Fase 6 (#81): registra custom GUCs necessários para backup/restore.
--
-- O TxRunner usa set_config('mez.tenant_id', ...) em cada tx. Em PG 12-13
-- o parâmetro precisa ser pré-registrado (via ALTER DATABASE ou SET).
-- Em PG 14+ isso é tolerado sem registro, mas mantemos o ALTER para
-- uniformidade e para evitar surpresas em ambientes restritos.
--
-- Também registra mez.iso_level para permitir que use cases futuros
-- possam consultar o isolation level da tx atual.

BEGIN;

-- O nome do banco é capturado dinamicamente via current_database().
DO $$
DECLARE
    dbname text;
BEGIN
    dbname := current_database();
    EXECUTE format('ALTER DATABASE %I SET mez.tenant_id TO %L', dbname, '');
    EXECUTE format('ALTER DATABASE %I SET mez.iso_level TO %L', dbname, 'read_committed');
END
$$;

COMMIT;
