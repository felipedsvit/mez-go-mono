package secrets

import (
	"context"
	"os"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func TestEnvMetaSecrets_LoadEmpty(t *testing.T) {
	os.Unsetenv("MEZ_META_APP_SECRETS")
	s, err := NewEnvMetaSecrets()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	_, err = s.ResolveMetaSecret(context.Background(), "t1", domain.ChannelWABA, "app1")
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestEnvMetaSecrets_LoadAndResolve(t *testing.T) {
	os.Setenv("MEZ_META_APP_SECRETS", `[{"app_id":"app1","tenant_id":"t1","channel":"waba","secret":"shh"}]`)
	defer os.Unsetenv("MEZ_META_APP_SECRETS")

	s, err := NewEnvMetaSecrets()
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	secret, err := s.ResolveMetaSecret(context.Background(), "t1", domain.ChannelWABA, "app1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(secret) != "shh" {
		t.Errorf("got %q, want shh", string(secret))
	}

	ch, tenant, err := s.ResolveChannel("app1")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if ch != domain.ChannelWABA {
		t.Errorf("channel = %q, want waba", ch)
	}
	if tenant != "t1" {
		t.Errorf("tenant = %q, want t1", tenant)
	}
}

func TestEnvTelegramSecrets_LoadAndResolve(t *testing.T) {
	os.Setenv("MEZ_TELEGRAM_SECRETS", "t1=secret1\nt2=secret2")
	defer os.Unsetenv("MEZ_TELEGRAM_SECRETS")

	s := NewEnvTelegramSecrets()

	got, err := s.ResolveTelegramSecret(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "secret1" {
		t.Errorf("got %q, want secret1", got)
	}

	_, err = s.ResolveTelegramSecret(context.Background(), "t3")
	if err == nil {
		t.Error("expected error for unknown tenant")
	}
}
