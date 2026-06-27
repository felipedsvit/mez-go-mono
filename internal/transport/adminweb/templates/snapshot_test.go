package templates_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

// newBasePage devolve um PageData com campos obrigatórios preenchidos para
// os components que precisam de Principal/CSRFToken/Now.
func newBasePage(title string) templates.PageData {
	return templates.PageData{
		Title:     title,
		Now:       time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
		CSRFToken: "csrf-test-token",
		Principal: admin.Principal{
			UserID:      "u1",
			Email:       "admin@example.com",
			Permissions: map[admin.Permission]struct{}{admin.PermReadTenants: {}, admin.PermReadUsers: {}},
		},
	}
}

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestLogin_RendersTitle(t *testing.T) {
	got := renderComponent(t, templates.Login(newBasePage("Login"), false))
	if !strings.Contains(got, "Login") {
		t.Errorf("expected to contain 'Login', got %q", got[:min(200, len(got))])
	}
	if !strings.Contains(got, "csrf-test-token") {
		t.Errorf("expected to contain CSRF token, got %q", got[:min(200, len(got))])
	}
}

func TestLogin_RendersSSO_WhenOIDCEnabled(t *testing.T) {
	got := renderComponent(t, templates.Login(newBasePage("Login"), true))
	if !strings.Contains(got, "Sign In with SSO") {
		t.Error("expected SSO link when OIDC enabled")
	}
}

func TestDashboard_RendersTilesForPlatformPrincipal(t *testing.T) {
	got := renderComponent(t, templates.Dashboard(newBasePage("Dashboard")))
	if !strings.Contains(got, "Tenants") {
		t.Error("expected Tenants tile")
	}
	if !strings.Contains(got, "Users") {
		t.Error("expected Users tile")
	}
}

func TestAudit_EmptyEntries(t *testing.T) {
	got := renderComponent(t, templates.Audit(templates.AuditData{Page: newBasePage("Audit Log")}))
	if !strings.Contains(got, "No audit entries") {
		t.Error("expected 'No audit entries' message for empty list")
	}
}

func TestAudit_WithEntries(t *testing.T) {
	got := renderComponent(t, templates.Audit(templates.AuditData{
		Page: newBasePage("Audit Log"),
		Entries: []admin.AuditEntry{
			{ID: "a1", ActorEmail: "a@b.c", Action: admin.ActionAuthLoginSuccess, CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
	}))
	if !strings.Contains(got, "a@b.c") {
		t.Error("expected actor email in audit row")
	}
}

func TestTenants_EmptyList(t *testing.T) {
	got := renderComponent(t, templates.Tenants(templates.TenantsData{Page: newBasePage("Tenants")}))
	if !strings.Contains(got, "No tenants yet") {
		t.Error("expected 'No tenants yet' for empty list")
	}
}

func TestTenants_WithRows(t *testing.T) {
	got := renderComponent(t, templates.Tenants(templates.TenantsData{
		Page: newBasePage("Tenants"),
		Tenants: []admin.Tenant{
			{ID: "t1", Name: "Acme", Slug: "acme", Status: admin.TenantActive, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}))
	if !strings.Contains(got, "Acme") {
		t.Error("expected tenant name")
	}
	if !strings.Contains(got, "acme") {
		t.Error("expected tenant slug")
	}
}

func TestRoles_Renders(t *testing.T) {
	got := renderComponent(t, templates.Roles(templates.RolesData{
		Page: newBasePage("Roles"),
		Roles: []admin.Role{
			{ID: "r1", Name: "admin", Scope: admin.ScopePlatform, IsBuiltin: true, Permissions: []admin.Permission{admin.PermReadTenants}},
		},
	}))
	if !strings.Contains(got, "admin") {
		t.Error("expected role name in table")
	}
}

func TestChannels_RendersFiveChannels(t *testing.T) {
	chs := []templates.ChannelRow{
		{ID: "waba", Name: "WhatsApp Business", Implemented: true, Capabilities: []templates.ChannelCapability{"text"}},
		{ID: "whatsmeow", Name: "WhatsApp (informal)", Implemented: true, Capabilities: []templates.ChannelCapability{"text"}},
		{ID: "instagram", Name: "Instagram Direct", Implemented: true, Capabilities: []templates.ChannelCapability{"text"}},
		{ID: "messenger", Name: "Facebook Messenger", Implemented: true, Capabilities: []templates.ChannelCapability{"text"}},
		{ID: "telegram_bot", Name: "Telegram Bot", Implemented: true, Capabilities: []templates.ChannelCapability{"text"}},
	}
	got := renderComponent(t, templates.Channels(templates.ChannelsData{Page: newBasePage("Channels"), TenantID: "t1", Channels: chs}))
	for _, id := range []string{"waba", "whatsmeow", "instagram", "messenger", "telegram_bot"} {
		if !strings.Contains(got, id) {
			t.Errorf("expected channel %q in page", id)
		}
	}
}

func TestInbox_EmptyConversations(t *testing.T) {
	got := renderComponent(t, templates.Inbox(templates.InboxData{Page: newBasePage("Inbox")}))
	if !strings.Contains(got, "No conversations") {
		t.Error("expected 'No conversations' for empty inbox")
	}
}

func TestReset_RendersConfirmationForm(t *testing.T) {
	got := renderComponent(t, templates.Reset(templates.ResetData{Page: newBasePage("Reset"), TenantID: "t1"}))
	if !strings.Contains(got, "RESET") {
		t.Error("expected RESET confirmation text in form")
	}
	if !strings.Contains(got, "csrf-test-token") {
		t.Error("expected CSRF token in form")
	}
}

func TestErrorPage_RendersStatus(t *testing.T) {
	got := renderComponent(t, templates.ErrorPage(templates.ErrorData{
		Page:       newBasePage("Error"),
		StatusCode: 404,
		Title:      "Not Found",
		Message:    "The page you requested was not found.",
		Path:       "/missing",
	}))
	if !strings.Contains(got, "404") {
		t.Error("expected status code 404 in error page")
	}
	if !strings.Contains(got, "/missing") {
		t.Error("expected path in error page")
	}
}

func TestHealth_RendersChecks(t *testing.T) {
	got := renderComponent(t, templates.Health(templates.HealthData{
		Page: newBasePage("Services"),
		Checks: []templates.HealthCheck{
			{Name: "postgres", Status: "ok"},
			{Name: "s3", Status: "down", Detail: "connection refused"},
		},
	}))
	if !strings.Contains(got, "postgres") {
		t.Error("expected postgres check")
	}
	if !strings.Contains(got, "s3") {
		t.Error("expected s3 check")
	}
	if !strings.Contains(got, "connection refused") {
		t.Error("expected detail for down check")
	}
}

func TestTruncate(t *testing.T) {
	if got := templates.Truncate("hello", 10); got != "hello" {
		t.Errorf("Truncate('hello', 10) = %q, want 'hello'", got)
	}
	if got := templates.Truncate("hello world", 5); got != "he..." {
		t.Errorf("Truncate('hello world', 5) = %q, want 'he...'", got)
	}
}

func TestFormatDate_Empty(t *testing.T) {
	if got := templates.FormatDate(time.Time{}, "2006-01-02"); got != "" {
		t.Errorf("FormatDate zero time = %q, want empty", got)
	}
}

func TestFormatDate_NonEmpty(t *testing.T) {
	got := templates.FormatDate(time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC), "2006-01-02")
	if got != "2026-06-27" {
		t.Errorf("FormatDate = %q, want '2026-06-27'", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
