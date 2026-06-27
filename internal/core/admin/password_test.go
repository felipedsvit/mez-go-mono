package admin

import (
	"strings"
	"testing"
)

func TestHashPassword_ValidFormat(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Errorf("hash has wrong PHC prefix: %q", hash)
	}
}

func TestHashPassword_RejectsShort(t *testing.T) {
	_, err := HashPassword("short")
	if err == nil {
		t.Fatal("expected error for short password, got nil")
	}
}

func TestHashPassword_DifferentSalts(t *testing.T) {
	h1, _ := HashPassword("same-password")
	h2, _ := HashPassword("same-password")
	if h1 == h2 {
		t.Error("two hashes of the same password should differ (random salt)")
	}
}

func TestVerifyPassword_Success(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Error("VerifyPassword should return true for correct password")
	}
}

func TestVerifyPassword_Failure(t *testing.T) {
	hash, _ := HashPassword("correct-horse")
	if VerifyPassword(hash, "wrong-horse") {
		t.Error("VerifyPassword should return false for wrong password")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$",
		"$argon2id$v=19$m=65536,t=3,p=2$aaa$bbb$ccc", // 7 parts
		"$argon2i$v=19$m=65536,t=3,p=2$YWFhYQ$YWFhYQ",  // wrong algorithm
		"$argon2id$v=18$m=65536,t=3,p=2$YWFhYQ$YWFhYQ",  // wrong version
	}
	for _, c := range cases {
		if VerifyPassword(c, "anything") {
			t.Errorf("VerifyPassword(%q) should be false", c)
		}
	}
}
