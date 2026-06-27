-- 0002_admin_auth.up.sql
-- Admin global: bootstrap owner with Argon2id hash.
-- Single-row table — only one admin global exists at a time, created via /setup wizard.
-- Subsequent /setup calls fail (issue #16 acceptance criterion).

BEGIN;

CREATE TABLE admin_globals (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- /setup wizard is supposed to be available only when 0 admins exist.
-- We don't need RLS on this table (it's a global singleton, not multi-tenant),
-- but we DO need to grant read access to mez_app so the handler can check count.
GRANT SELECT, INSERT ON admin_globals TO mez_app;
GRANT SELECT, INSERT ON admin_globals TO mez_platform;

COMMIT;
