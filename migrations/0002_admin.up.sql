-- 0002_admin.up.sql
-- Role-based admin schema (Phase 1 — Auth + Admin).
--
-- Architecture: the control plane is global (admins manage all tenants) but
-- every mutation by an admin must be auditable. RLS is enabled and FORCED on
-- admin_users, admin_roles, admin_role_permissions, admin_user_roles so that
-- a misconfigured application role (mez_app) cannot bypass policies. admin_audit_log
-- is intentionally NOT RLS-protected because:
--   (1) mez_platform needs cross-tenant read+insert for any audit scope.
--   (2) mez_app needs insert for its own login success/failure.
--   (3) REVOKE UPDATE, DELETE on admin_audit_log enforces append-only.
--
-- C3 (FORCE RLS), C5 (RunAsPlatform auditado): see README §8.
-- C9: TenantID is a nullable TEXT FK to tenants(id) so per-tenant actions can
--     be tagged while platform-wide actions use NULL.

BEGIN;

-- UUID PKs per README §9 (so logical restore by id works).
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- -----------------------------------------------------------------------------
-- 1. Tables
-- -----------------------------------------------------------------------------

CREATE TABLE admin_users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    auth_kind     TEXT NOT NULL DEFAULT 'local' CHECK (auth_kind IN ('local', 'oidc')),
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'invited')),
    password_hash TEXT,
    idp_subject   TEXT,
    idp_issuer    TEXT,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- local users must have a password hash; oidc users must have idp_subject+idp_issuer
    CONSTRAINT admin_users_local_has_hash CHECK (
        auth_kind <> 'local' OR password_hash IS NOT NULL
    ),
    CONSTRAINT admin_users_oidc_has_idp CHECK (
        auth_kind <> 'oidc' OR (idp_subject IS NOT NULL AND idp_issuer IS NOT NULL)
    )
);

CREATE INDEX idx_admin_users_email ON admin_users(lower(email));
CREATE INDEX idx_admin_users_idp ON admin_users(idp_issuer, idp_subject) WHERE idp_subject IS NOT NULL;

CREATE TABLE admin_roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scope       TEXT NOT NULL CHECK (scope IN ('platform', 'tenant')),
    tenant_id   TEXT, -- nullable: NULL = platform role; references tenants(id) but FK added after tenants exists
    is_builtin  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, tenant_id, name)
);

CREATE TABLE admin_role_permissions (
    role_id    UUID NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE admin_user_roles (
    user_id   UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    role_id   UUID NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    tenant_id TEXT,
    PRIMARY KEY (user_id, role_id, tenant_id)
);

CREATE INDEX idx_admin_user_roles_user ON admin_user_roles(user_id);
CREATE INDEX idx_admin_user_roles_role ON admin_user_roles(role_id);

-- audit_log: append-only. tenant_id is nullable (NULL = platform-wide action).
CREATE TABLE admin_audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id    UUID REFERENCES admin_users(id) ON DELETE SET NULL,
    actor_email TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    tenant_id   TEXT,
    metadata    JSONB,
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_audit_created ON admin_audit_log(created_at DESC);
CREATE INDEX idx_admin_audit_actor ON admin_audit_log(actor_id);
CREATE INDEX idx_admin_audit_action ON admin_audit_log(action);
CREATE INDEX idx_admin_audit_tenant ON admin_audit_log(tenant_id);

-- -----------------------------------------------------------------------------
-- 2. Seed: 4 built-in roles + ~10 permissions
-- -----------------------------------------------------------------------------
-- Per issue #20: 4 roles (platform-superadmin, platform-admin, tenant-owner,
-- tenant-agent) and ~10 permissions. Permissions follow <resource>:<action>.

INSERT INTO admin_roles (id, name, description, scope, tenant_id, is_builtin) VALUES
    ('00000000-0000-0000-0000-000000000001', 'platform-superadmin', 'Full access to all platform features', 'platform', NULL, true),
    ('00000000-0000-0000-0000-000000000002', 'platform-admin',      'Manage tenants, users, roles (no delete)', 'platform', NULL, true),
    ('00000000-0000-0000-0000-000000000003', 'tenant-owner',        'Manage own tenant: users, channels, settings', 'tenant', NULL, true),
    ('00000000-0000-0000-0000-000000000004', 'tenant-agent',        'Handle conversations and messages', 'tenant', NULL, true)
ON CONFLICT (id) DO NOTHING;

-- Permission catalog: 14 keys covering Phase 1 + future channels
INSERT INTO admin_role_permissions (role_id, permission) VALUES
    -- platform-superadmin: all
    ('00000000-0000-0000-0000-000000000001', 'tenants:read'),
    ('00000000-0000-0000-0000-000000000001', 'tenants:create'),
    ('00000000-0000-0000-0000-000000000001', 'tenants:update'),
    ('00000000-0000-0000-0000-000000000001', 'tenants:delete'),
    ('00000000-0000-0000-0000-000000000001', 'users:read'),
    ('00000000-0000-0000-0000-000000000001', 'users:create'),
    ('00000000-0000-0000-0000-000000000001', 'users:update'),
    ('00000000-0000-0000-0000-000000000001', 'users:delete'),
    ('00000000-0000-0000-0000-000000000001', 'roles:read'),
    ('00000000-0000-0000-0000-000000000001', 'roles:update'),
    ('00000000-0000-0000-0000-000000000001', 'audit:read'),
    ('00000000-0000-0000-0000-000000000001', 'channels:manage'),
    -- platform-admin: no tenants:delete, no users:delete
    ('00000000-0000-0000-0000-000000000002', 'tenants:read'),
    ('00000000-0000-0000-0000-000000000002', 'tenants:create'),
    ('00000000-0000-0000-0000-000000000002', 'tenants:update'),
    ('00000000-0000-0000-0000-000000000002', 'users:read'),
    ('00000000-0000-0000-0000-000000000002', 'users:create'),
    ('00000000-0000-0000-0000-000000000002', 'users:update'),
    ('00000000-0000-0000-0000-000000000002', 'roles:read'),
    ('00000000-0000-0000-0000-000000000002', 'audit:read'),
    ('00000000-0000-0000-0000-000000000002', 'channels:manage'),
    -- tenant-owner: tenant-scoped
    ('00000000-0000-0000-0000-000000000003', 'users:read'),
    ('00000000-0000-0000-0000-000000000003', 'users:create'),
    ('00000000-0000-0000-0000-000000000003', 'users:update'),
    ('00000000-0000-0000-0000-000000000003', 'channels:manage'),
    -- tenant-agent: read inbox only
    ('00000000-0000-0000-0000-000000000004', 'inbox:read'),
    ('00000000-0000-0000-0000-000000000004', 'inbox:write')
ON CONFLICT (role_id, permission) DO NOTHING;

-- -----------------------------------------------------------------------------
-- 3. RLS (C3/C4) — force on tables; audit_log exempt but append-only
-- -----------------------------------------------------------------------------

ALTER TABLE admin_users            ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_users            FORCE  ROW LEVEL SECURITY;

ALTER TABLE admin_roles            ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_roles            FORCE  ROW LEVEL SECURITY;

ALTER TABLE admin_role_permissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_role_permissions FORCE  ROW LEVEL SECURITY;

ALTER TABLE admin_user_roles       ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_user_roles       FORCE  ROW LEVEL SECURITY;

-- Policies: read access for mez_app (used by LoginLocal to validate creds);
-- write access ONLY via mez_platform (RunAsPlatform wrapper).
-- admin_audit_log: NO RLS — mez_platform reads/writes freely; mez_app can insert only.

CREATE POLICY admin_users_read ON admin_users
    FOR SELECT TO mez_app
    USING (true);

CREATE POLICY admin_users_write ON admin_users
    FOR ALL TO mez_app
    USING (false) WITH CHECK (false);

CREATE POLICY admin_users_platform_all ON admin_users
    FOR ALL TO mez_platform
    USING (true) WITH CHECK (true);

CREATE POLICY admin_roles_read ON admin_roles
    FOR SELECT TO mez_app
    USING (true);

CREATE POLICY admin_roles_write ON admin_roles
    FOR ALL TO mez_app
    USING (false) WITH CHECK (false);

CREATE POLICY admin_roles_platform_all ON admin_roles
    FOR ALL TO mez_platform
    USING (true) WITH CHECK (true);

CREATE POLICY admin_role_permissions_read ON admin_role_permissions
    FOR SELECT TO mez_app
    USING (true);

CREATE POLICY admin_role_permissions_write ON admin_role_permissions
    FOR ALL TO mez_app
    USING (false) WITH CHECK (false);

CREATE POLICY admin_role_permissions_platform_all ON admin_role_permissions
    FOR ALL TO mez_platform
    USING (true) WITH CHECK (true);

CREATE POLICY admin_user_roles_read ON admin_user_roles
    FOR SELECT TO mez_app
    USING (true);

CREATE POLICY admin_user_roles_write ON admin_user_roles
    FOR ALL TO mez_app
    USING (false) WITH CHECK (false);

CREATE POLICY admin_user_roles_platform_all ON admin_user_roles
    FOR ALL TO mez_platform
    USING (true) WITH CHECK (true);

-- -----------------------------------------------------------------------------
-- 4. Grants
-- -----------------------------------------------------------------------------

-- mez_app: read everything (used by LoginService to validate creds + look up roles)
GRANT SELECT ON admin_users            TO mez_app;
GRANT SELECT ON admin_roles            TO mez_app;
GRANT SELECT ON admin_role_permissions TO mez_app;
GRANT SELECT ON admin_user_roles       TO mez_app;
GRANT INSERT ON admin_audit_log        TO mez_app;
GRANT USAGE  ON ALL SEQUENCES IN SCHEMA public TO mez_app;

-- mez_platform: full control plane access (used by RunAsPlatform)
GRANT SELECT, INSERT, UPDATE, DELETE ON admin_users            TO mez_platform;
GRANT SELECT, INSERT, UPDATE, DELETE ON admin_roles            TO mez_platform;
GRANT SELECT, INSERT, UPDATE, DELETE ON admin_role_permissions TO mez_platform;
GRANT SELECT, INSERT, UPDATE, DELETE ON admin_user_roles       TO mez_platform;
GRANT SELECT, INSERT                  ON admin_audit_log        TO mez_platform;
GRANT USAGE  ON ALL SEQUENCES IN SCHEMA public TO mez_platform;

-- Defense in depth (C5): audit_log is append-only for both roles
REVOKE UPDATE, DELETE ON admin_audit_log FROM mez_app;
REVOKE UPDATE, DELETE ON admin_audit_log FROM mez_platform;

COMMIT;
