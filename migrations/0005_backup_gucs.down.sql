-- 0005_backup_gucs.down.sql
BEGIN;

DO $$
DECLARE
    dbname text;
BEGIN
    dbname := current_database();
    EXECUTE format('ALTER DATABASE %I RESET mez.tenant_id', dbname);
    EXECUTE format('ALTER DATABASE %I RESET mez.iso_level', dbname);
END
$$;

COMMIT;
