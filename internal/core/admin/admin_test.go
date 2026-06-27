package admin

import (
	"testing"
)

func TestAction_Valid(t *testing.T) {
	valid := []Action{
		ActionAuthLoginSuccess, ActionAuthLoginFailure, ActionAuthLogout,
		ActionSetupBootstrap, ActionSetupRebootstrap,
		ActionTenantCreate, ActionTenantUpdate, ActionTenantStatus, ActionTenantList,
		ActionUserCreate, ActionUserStatus, ActionUserRoleAssign, ActionUserRoleRevoke,
		ActionRoleCreate, ActionRolePermissions,
		ActionPlatformAccess,
	}
	for _, a := range valid {
		if !a.Valid() {
			t.Errorf("expected %q to be valid", a)
		}
	}

	invalid := []Action{"", "not.an.action", "x", "AuthLoginSuccess"}
	for _, a := range invalid {
		if a.Valid() {
			t.Errorf("expected %q to be invalid", a)
		}
	}
}

func TestPrincipal_HasPermission(t *testing.T) {
	p := Principal{
		Permissions: map[Permission]struct{}{
			"tenants:read": {},
			"users:create": {},
		},
	}
	if !p.HasPermission("tenants:read") {
		t.Error("expected HasPermission(tenants:read) = true")
	}
	if p.HasPermission("tenants:delete") {
		t.Error("expected HasPermission(tenants:delete) = false")
	}
	emptyP := Principal{}
	if emptyP.HasPermission("tenants:read") {
		t.Error("empty principal should not have any permission")
	}
	nilP := Principal{Permissions: nil}
	if nilP.HasPermission("tenants:read") {
		t.Error("nil permissions map should not have any permission")
	}
}

func TestPrincipal_IsPlatform(t *testing.T) {
	tests := []struct {
		name string
		p    Principal
		want bool
	}{
		{"empty", Principal{}, false},
		{"tenant-only", Principal{Roles: []RoleBinding{{Scope: ScopeTenant, TenantID: "t1"}}}, false},
		{"platform-only", Principal{Roles: []RoleBinding{{Scope: ScopePlatform}}}, true},
		{"mixed", Principal{Roles: []RoleBinding{
			{Scope: ScopeTenant, TenantID: "t1"},
			{Scope: ScopePlatform},
		}}, true},
		// The OLD implementation used strings.Contains(role, "platform") which
		// would match a tenant role whose ID happened to contain "platform".
		// This test ensures IsPlatform uses Scope, not string parsing.
		{"tenant-role-id-contains-platform", Principal{Roles: []RoleBinding{
			{RoleID: "role-platform-tenant-special", Scope: ScopeTenant, TenantID: "t1"},
		}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.IsPlatform(); got != tt.want {
				t.Errorf("IsPlatform() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluate_PlatformScope(t *testing.T) {
	tenantOnly := Principal{
		Permissions: map[Permission]struct{}{"tenants:create": {}},
		Roles:       []RoleBinding{{Scope: ScopeTenant, TenantID: "t1"}},
	}
	platform := Principal{
		Permissions: map[Permission]struct{}{"tenants:create": {}},
		Roles:       []RoleBinding{{Scope: ScopePlatform}},
	}
	noPerm := Principal{
		Roles: []RoleBinding{{Scope: ScopePlatform}},
	}

	if Evaluate(tenantOnly, "tenants:create", ScopePlatform) {
		t.Error("tenant role should NOT do platform actions even with the perm")
	}
	if !Evaluate(platform, "tenants:create", ScopePlatform) {
		t.Error("platform role with perm should do platform actions")
	}
	if Evaluate(noPerm, "tenants:create", ScopePlatform) {
		t.Error("platform role without perm should NOT do platform actions")
	}
}

func TestEvaluate_TenantScope(t *testing.T) {
	platform := Principal{
		Permissions: map[Permission]struct{}{"users:read": {}},
		Roles:       []RoleBinding{{Scope: ScopePlatform}},
	}
	tenantSame := Principal{
		Permissions: map[Permission]struct{}{"users:read": {}},
		Roles:       []RoleBinding{{Scope: ScopeTenant, TenantID: "t1"}},
		TenantID:    "t1",
	}
	tenantOther := Principal{
		Permissions: map[Permission]struct{}{"users:read": {}},
		Roles:       []RoleBinding{{Scope: ScopeTenant, TenantID: "t2"}},
		TenantID:    "t1",
	}
	tenantNoBinding := Principal{
		Permissions: map[Permission]struct{}{"users:read": {}},
		Roles:       []RoleBinding{{Scope: ScopeTenant, TenantID: ""}}, // template
		TenantID:    "t1",
	}
	noPerm := Principal{
		Roles:    []RoleBinding{{Scope: ScopePlatform}},
		TenantID: "t1",
	}

	if !Evaluate(platform, "users:read", ScopeTenant) {
		t.Error("platform role should do tenant actions")
	}
	if !Evaluate(tenantSame, "users:read", ScopeTenant) {
		t.Error("tenant role with matching binding should do tenant actions")
	}
	if Evaluate(tenantOther, "users:read", ScopeTenant) {
		t.Error("tenant role with different binding should NOT do actions on other tenant")
	}
	if !Evaluate(tenantNoBinding, "users:read", ScopeTenant) {
		t.Error("tenant role with empty binding (template) should work for any tenant")
	}
	if Evaluate(noPerm, "users:read", ScopeTenant) {
		t.Error("no perm should NOT do any action")
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  User@Example.COM  ", "user@example.com"},
		{"u@e.co", "u@e.co"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeEmail(tt.in); got != tt.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"user@example.com", true},
		{"u@e.co", true},
		{"user+tag@example.com", true},
		{"", false},
		{"no-at-sign", false},
		{"@example.com", false},
		{"user@", false},
		{"user@example", false},
	}
	for _, tt := range tests {
		if got := ValidEmail(tt.in); got != tt.want {
			t.Errorf("ValidEmail(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestUser_IsActive(t *testing.T) {
	tests := []struct {
		status UserStatus
		want   bool
	}{
		{UserStatusActive, true},
		{UserStatusDisabled, false},
		{UserStatusInvited, false},
		{"garbage", false},
	}
	for _, tt := range tests {
		u := AdminUser{Status: tt.status}
		if got := u.IsActive(); got != tt.want {
			t.Errorf("IsActive(status=%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
