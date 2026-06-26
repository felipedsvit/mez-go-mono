BEGIN;

DROP TABLE IF EXISTS audit_log CASCADE;
DROP TABLE IF EXISTS channel_credentials CASCADE;
DROP TABLE IF EXISTS outbound_events CASCADE;
DROP TABLE IF EXISTS inbound_events CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS conversations CASCADE;
DROP TABLE IF EXISTS contacts CASCADE;
DROP TABLE IF EXISTS tenants CASCADE;

DROP POLICY IF EXISTS tenant_isolation ON tenants;
DROP POLICY IF EXISTS tenant_isolation ON contacts;
DROP POLICY IF EXISTS tenant_isolation ON conversations;
DROP POLICY IF EXISTS tenant_isolation ON messages;
DROP POLICY IF EXISTS tenant_isolation ON inbound_events;
DROP POLICY IF EXISTS tenant_isolation ON outbound_events;
DROP POLICY IF EXISTS tenant_isolation ON channel_credentials;
DROP POLICY IF EXISTS cross_tenant_audit ON audit_log;

REVOKE ALL ON ALL TABLES IN SCHEMA public FROM mez_app;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM mez_platform;
REVOKE USAGE ON SCHEMA public FROM mez_app;
REVOKE USAGE ON SCHEMA public FROM mez_platform;

DROP ROLE IF EXISTS mez_platform;
DROP ROLE IF EXISTS mez_app;
DROP ROLE IF EXISTS mez_migrate;

COMMIT;
