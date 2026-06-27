package config

import (
	"os"
	"testing"
)

func TestIsFeatureEnabled(t *testing.T) {
	// t.Setenv com valor vazio não é o mesmo que unset; usamos
	// os.Unsetenv para o caso "unset".
	t.Setenv("MEZ_FEATURE_TEST_BOOL", "true")
	ReloadFeatures()
	if !IsFeatureEnabled("test_bool", false) {
		t.Fatal("expected true with env=true, default=false")
	}

	t.Setenv("MEZ_FEATURE_TEST_BOOL", "false")
	ReloadFeatures()
	if IsFeatureEnabled("test_bool", true) {
		t.Fatal("expected false with env=false, default=true")
	}

	// Caso "realmente unset": limpa o cache + unsetenv
	ReloadFeatures()
	os.Unsetenv("MEZ_FEATURE_TEST_BOOL")
	if !IsFeatureEnabled("test_bool_unset", true) {
		t.Fatal("expected default=true when unset")
	}
	ReloadFeatures()
	if IsFeatureEnabled("test_bool_unset2", false) {
		t.Fatal("expected default=false when unset")
	}
}

func TestIsFeatureEnabledFor_PctRollout(t *testing.T) {
	t.Setenv("MEZ_FEATURE_ROLLOUT_B_PCT", "50")
	ReloadFeatures()

	// Conta quantos subjects caem dentro do bucket 0-49
	hits := 0
	for i := 0; i < 1000; i++ {
		subject := "user:" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if IsFeatureEnabledFor("rollout_b", false, subject) {
			hits++
		}
	}
	// Espera ~50% (com tolerância de 10%)
	if hits < 400 || hits > 600 {
		t.Fatalf("rollout 50%% esperado ~500 hits em 1000; got %d", hits)
	}
}

func TestIsFeatureEnabledFor_Deterministic(t *testing.T) {
	t.Setenv("MEZ_FEATURE_ROLLOUT_C_PCT", "50")
	ReloadFeatures()

	// Mesmo subject → mesmo resultado em chamadas repetidas
	first := IsFeatureEnabledFor("rollout_c", false, "user:42")
	for i := 0; i < 100; i++ {
		if IsFeatureEnabledFor("rollout_c", false, "user:42") != first {
			t.Fatal("rollout não é determinístico para mesmo subject")
		}
	}
}

func TestReadFeaturePct(t *testing.T) {
	// Convenção: name="rollout_a" → MEZ_FEATURE_ROLLOUT_A_PCT
	t.Setenv("MEZ_FEATURE_ROLLOUT_A_PCT", "75")
	ReloadFeatures()
	if got := ReadFeaturePct("rollout_a", -1); got != 75 {
		t.Fatalf("expected 75, got %d", got)
	}

	t.Setenv("MEZ_FEATURE_ROLLOUT_A_PCT", "invalid")
	ReloadFeatures()
	if got := ReadFeaturePct("rollout_a", -1); got != -1 {
		t.Fatalf("expected -1 (default) para valor inválido, got %d", got)
	}

	// Valor fora de range (101) cai no default
	t.Setenv("MEZ_FEATURE_ROLLOUT_A_PCT", "101")
	ReloadFeatures()
	if got := ReadFeaturePct("rollout_a", -1); got != -1 {
		t.Fatalf("expected -1 (default) para pct fora de range, got %d", got)
	}
}

func TestStableHash(t *testing.T) {
	a := stableHash("user:42")
	b := stableHash("user:42")
	if a != b {
		t.Fatal("stableHash não é determinístico")
	}
	c := stableHash("user:43")
	if a == c {
		t.Fatal("stableHash colidiu para subjects diferentes")
	}
	// Verificar que não panica com strings longas ou vazias
	_ = stableHash("")
	_ = stableHash("user:with:colons:and-dashes_and.dots/1234567890")
}
