package s3

import (
	"errors"
	"testing"
)

func TestWithTenantPrefix_Valid(t *testing.T) {
	cases := []struct {
		tenantID string
		key      string
		want     string
	}{
		{"tenantA", "media/x.png", "tenants/tenantA/media/x.png"},
		{"tenantA", "media/sub/y.png", "tenants/tenantA/media/sub/y.png"},
		{"t1", "x", "tenants/t1/x"},
	}
	for _, c := range cases {
		got, err := WithTenantPrefix(c.tenantID, c.key)
		if err != nil {
			t.Errorf("WithTenantPrefix(%q, %q) err = %v; want nil", c.tenantID, c.key, err)
		}
		if got != c.want {
			t.Errorf("WithTenantPrefix(%q, %q) = %q; want %q", c.tenantID, c.key, got, c.want)
		}
	}
}

func TestWithTenantPrefix_PathTraversal(t *testing.T) {
	cases := []struct {
		name     string
		tenantID string
		key      string
	}{
		{"traversal_dotdot", "tenantA", "../tenantB/media/x.png"},
		{"traversal_middle", "tenantA", "media/../../tenantB/x.png"},
		{"abs_key", "tenantA", "/etc/passwd"},
		{"abs_key_with_prefix", "tenantA", "/tenants/tenantB/x"},
		{"empty_key", "tenantA", ""},
		{"empty_tenant", "", "x.png"},
		{"traversal_only", "tenantA", ".."},
		{"traversal_slash", "tenantA", "../"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := WithTenantPrefix(c.tenantID, c.key)
			if !errors.Is(err, ErrTenantMismatch) {
				t.Errorf("WithTenantPrefix(%q, %q) err = %v; want ErrTenantMismatch", c.tenantID, c.key, err)
			}
		})
	}
}

func TestMustWithTenantPrefix_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustWithTenantPrefix should panic on path traversal")
		}
	}()
	MustWithTenantPrefix("tenantA", "../escape")
}
