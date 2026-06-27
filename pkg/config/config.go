package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	HTTPAddr           string `mapstructure:"http_addr"`
	DatabaseURL        string `mapstructure:"database_url"`
	MigrateDBURL       string `mapstructure:"migrate_database_url"`
	PlatformDBURL      string `mapstructure:"platform_database_url"`
	MasterKey          string `mapstructure:"master_key"`
	MasterKeyFile      string `mapstructure:"master_key_file"`
	S3Endpoint         string `mapstructure:"s3_endpoint"`
	S3Bucket           string `mapstructure:"s3_bucket"`
	S3BackupBucket     string `mapstructure:"s3_backup_bucket"`
	S3AccessKey        string `mapstructure:"s3_access_key"`
	S3SecretKey        string `mapstructure:"s3_secret_key"`
	OIDCIssuer         string `mapstructure:"oidc_issuer"`
	OIDCClientID       string `mapstructure:"oidc_client_id"`
	OIDCClientSecret   string `mapstructure:"oidc_client_secret"`
	OIDCRedirectURL    string `mapstructure:"oidc_redirect_url"`
	SessionSecret      string `mapstructure:"session_secret"`
	SessionTTL         string `mapstructure:"session_ttl"`
	AdminDBURL         string `mapstructure:"admin_database_url"`
	BusInboundBuf      int    `mapstructure:"bus_inbound_buffer"`
	BusOutboundBuf     int    `mapstructure:"bus_outbound_buffer"`
	ReconcileInterval  string `mapstructure:"reconcile_interval"`
	OutboxPollInterval string `mapstructure:"outbox_poll_interval"`
	MaxActiveTenants   int    `mapstructure:"max_active_tenants"`
	FFmpegConcurrency  int    `mapstructure:"ffmpeg_concurrency"`
	LogLevel           string `mapstructure:"log_level"`
	MetricsAddr        string `mapstructure:"metrics_addr"`
}

func Load() (Config, error) {
	v := viper.New()
	v.SetEnvPrefix("MEZ")
	v.AutomaticEnv()

	v.SetDefault("http_addr", ":8080")
	v.SetDefault("bus_inbound_buffer", 1024)
	v.SetDefault("bus_outbound_buffer", 1024)
	v.SetDefault("reconcile_interval", "30s")
	v.SetDefault("outbox_poll_interval", "5s")
	v.SetDefault("max_active_tenants", 100)
	v.SetDefault("ffmpeg_concurrency", 4)
	v.SetDefault("log_level", "info")
	v.SetDefault("metrics_addr", ":9090")
	v.SetDefault("s3_bucket", "mezgo-media")
	v.SetDefault("s3_backup_bucket", "mezgo-backups")
	v.SetDefault("session_ttl", "24h")

	cfg := Config{}
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("MEZ_DATABASE_URL is required")
	}
	if cfg.MigrateDBURL == "" {
		return cfg, fmt.Errorf("MEZ_MIGRATE_DATABASE_URL is required")
	}
	if cfg.PlatformDBURL == "" {
		return cfg, fmt.Errorf("MEZ_PLATFORM_DATABASE_URL is required")
	}

	mk := cfg.MasterKey
	mkf := cfg.MasterKeyFile
	if mk == "" && mkf != "" {
		data, err := os.ReadFile(mkf)
		if err != nil {
			return cfg, fmt.Errorf("read master key file: %w", err)
		}
		cfg.MasterKey = strings.TrimSpace(string(data))
	}
	if cfg.MasterKey == "" {
		return cfg, fmt.Errorf("MEZ_MASTER_KEY or MEZ_MASTER_KEY_FILE is required")
	}

	return cfg, nil
}

// ValidateServe checks fields required only by the 'serve' subcommand.
func (c Config) ValidateServe() error {
	if c.SessionSecret == "" {
		return fmt.Errorf("MEZ_SESSION_SECRET is required for serve")
	}
	return nil
}
