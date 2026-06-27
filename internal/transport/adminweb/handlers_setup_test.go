package adminweb

import "testing"

func TestIsStrongPassword(t *testing.T) {
	cases := []struct {
		name string
		pwd  string
		want bool
	}{
		// Fracas
		{"empty", "", false},
		{"too_short_8", "Aa1!aaaa", false},
		{"only_lower_12", "abcdefghijkl", false},
		{"no_upper_12", "abcdefgh1jkl!", false},
		{"no_lower_12", "ABCDEFGH1JKL!", false},
		{"no_digit_12", "Abcdefghijkl!", false},
		{"no_symbol_12", "Abcdefghijkl1", false},
		// Fortes
		{"all_classes_12", "Abcdefgh1jkl!", true},
		{"all_classes_16", "MyP@ssw0rd123456!", true},
		{"with_unicode", "MünchenTürkçe123!", true},
		{"very_long", "Abcdefghijklmnopqrstuvwxyz123!@#", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isStrongPassword(c.pwd); got != c.want {
				t.Errorf("isStrongPassword(%q) = %v; want %v", c.pwd, got, c.want)
			}
		})
	}
}

func TestNewCSRFToken(t *testing.T) {
	t1 := newCSRFToken()
	t2 := newCSRFToken()
	if t1 == "" || t2 == "" {
		t.Error("tokens não podem ser vazios")
	}
	if t1 == t2 {
		t.Error("tokens devem ser diferentes entre chamadas")
	}
	// 32 bytes hex = 64 chars
	if len(t1) != 64 {
		t.Errorf("token deve ter 64 chars, got %d", len(t1))
	}
}
