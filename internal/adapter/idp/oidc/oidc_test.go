package oidc

import (
	"testing"
)

// TestChallengeFromVerifier_KSpec validates PKCE S256 against the worked
// example from RFC 7636 §4.6:
//
//	verifier:  dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk
//	challenge: E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM
func TestChallengeFromVerifier_KSpec(t *testing.T) {
	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)
	got := ChallengeFromVerifier(verifier)
	if got != challenge {
		t.Errorf("ChallengeFromVerifier:\n  got  = %q\n  want = %q", got, challenge)
	}
}

func TestChallengeFromVerifier_DifferentInputs_DifferentOutputs(t *testing.T) {
	a := ChallengeFromVerifier("verifier-aaaaaaaaaaaaaaaaaaaaaaaaaa")
	b := ChallengeFromVerifier("verifier-bbbbbbbbbbbbbbbbbbbbbbbbb")
	if a == b {
		t.Errorf("expected different challenges for different verifiers")
	}
}

func TestChallengeFromVerifier_SameInput_SameOutput(t *testing.T) {
	v := "deterministic-verifier-1234567890"
	a := ChallengeFromVerifier(v)
	b := ChallengeFromVerifier(v)
	if a != b {
		t.Errorf("expected same output for same input (SHA-256 is deterministic)")
	}
}
