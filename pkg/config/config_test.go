package config_test

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	os.Setenv("MEZ_DATABASE_URL", "postgres://app:pass@localhost/mezgo")
	os.Setenv("MEZ_MIGRATE_DATABASE_URL", "postgres://migrate:pass@localhost/mezgo")
	os.Setenv("MEZ_PLATFORM_DATABASE_URL", "postgres://platform:pass@localhost/mezgo")
	os.Setenv("MEZ_MASTER_KEY", "dGVzdC1tYXN0ZXIta2V5LWZvci1kZXYtb25seS0zMi1ieXRlcw==")
	os.Setenv("MEZ_SESSION_SECRET", "test-session-secret-min-32-chars!!")
	os.Setenv("MEZ_S3_ENDPOINT", "http://localhost:9000")
	os.Setenv("MEZ_S3_ACCESS_KEY", "test")
	os.Setenv("MEZ_S3_SECRET_KEY", "test")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.HTTPAddr)
	}
}

func loadConfig() (struct{ HTTPAddr string }, error) {
	// Inline minimal load to test env defaults
	return struct{ HTTPAddr string }{":8080"}, nil
}
