package adminweb

import "testing"

func TestSanitizeNext(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Empty / safe defaults
		{"", "/"},
		{"/", "/"},
		{"/admin/", "/admin/"},
		{"/admin/tenants", "/admin/tenants"},
		{"/admin/x?y=z", "/admin/x?y=z"},
		{"/admin/x#frag", "/admin/x#frag"},

		// External URLs
		{"https://evil.com", "/"},
		{"http://evil.com", "/"},
		{"//evil.com", "/"},
		{"//evil.com/admin", "/"},
		{"///triple", "/"},
		{"javascript:alert(1)", "/"},
		{"data:text/html,foo", "/"},

		// Path traversal / backslash
		{"/\\evil.com", "/"},
		{"/admin/../etc", "/admin/../etc"}, // /admin/../etc is internal — allowed
		{"/\\..\\..\\windows", "/"},

		// Control chars
		{"/admin\r\nHost: evil.com", "/"},
		{"/admin\x00.evil", "/"},
	}

	for _, c := range cases {
		got := sanitizeNext(c.in)
		if got != c.want {
			t.Errorf("sanitizeNext(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
