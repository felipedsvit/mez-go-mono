package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	HTTPAddr string `mapstructure:"http_addr"`
	// HTTPTLSCertFile + HTTPTLSKeyFile (opcional): se ambos setados,
	// o server usa ListenAndServeTLS em vez de ListenAndServe. Issue
	// #151 (Sprint 0B H12): TLS nativo como fallback quando não há
	// proxy reverso na frente. Em prod recomenda-se proxy (Caddy/nginx)
	// mas o binário suporta TLS direto.
	HTTPTLSCertFile string `mapstructure:"http_tls_cert_file"`
	HTTPTLSKeyFile  string `mapstructure:"http_tls_key_file"`
	// HTTPForceHTTPS: se true, middleware retorna 301 em requests HTTP
	// plain quando TLS está ativo (via flag ou proxy). Issue #151.
	HTTPForceHTTPS     bool   `mapstructure:"http_force_https"`
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
	APIJWTSecret       string `mapstructure:"api_jwt_secret"`
	ReconcileBatch     int    `mapstructure:"reconcile_batch"`
	OutboxBatch        int    `mapstructure:"outbox_batch"`
	// MigrateOnBoot: se true, executa `migrate up` antes de subir o
	// pipeline (Fase 8 #99 sub-issue). Substitui o entrypoint.sh
	// (que fazia `migrate up && serve` em 2 processos) por um único
	// binário que se auto-migra. Default true para dev; em prod
	// recomenda-se false e rodar migrations em job separado.
	MigrateOnBoot bool `mapstructure:"migrate_on_boot"`
	// WSAllowedOrigins: lista (CSV) de origens (scheme://host[:port])
	// aceitas no WebSocket upgrade. Issue #129 (C1 audit). Vazio
	// rejeita todas as cross-origin; same-origin passa via Host.
	WSAllowedOrigins string `mapstructure:"ws_allowed_origins"`
	// WSAllowSameOrigin: aceita requests sem Origin (curl, Postman,
	// clientes Go). Default false em production hardening.
	WSAllowSameOrigin bool `mapstructure:"ws_allow_same_origin"`
	// WSTrustedProxy: honra X-Forwarded-Origin/-Proto. Apenas se
	// houver reverse proxy controlado na frente.
	WSTrustedProxy bool `mapstructure:"ws_trusted_proxy"`
	// SessionCookieSecure: flag Secure do cookie de sessão. Issue #131
	// (Sprint 0A C3 audit): prefixo __Host- exige Secure=true (RFC 6265bis).
	// Default true em prod, false em dev (testcontainers sem HTTPS).
	SessionCookieSecure bool `mapstructure:"session_cookie_secure"`
	// Fase 10 (#177): app-level config removido de env vars.
	// Use /admin/settings (system_settings table) em vez de env.
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
	v.SetDefault("reconcile_batch", 100)
	v.SetDefault("outbox_batch", 32)
	v.SetDefault("migrate_on_boot", true) // Fase 8 #99 sub-issue
	v.SetDefault("session_cookie_secure", true) // issue #131 — __Host- exige Secure

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
		// Issue #141 (H3 audit): permission 0600 + no-symlink
		// enforced by ReadKeyFile. KEK = root of envelope encryption.
		data, err := ReadKeyFile(mkf)
		if err != nil {
			return cfg, fmt.Errorf("read master key file: %w", err)
		}
		cfg.MasterKey = data
	}
	if cfg.MasterKey == "" {
		return cfg, fmt.Errorf("MEZ_MASTER_KEY or MEZ_MASTER_KEY_FILE is required")
	}

	return cfg, nil
}

// devAPISecretPlaceholder é o literal que o server.go usava como fallback
// em dev. Bloqueado por ValidateServe: se alguém setar essa string
// propositalmente, é fail-closed. Issue #130 (C2 audit) + #144 (H6).
const devAPISecretPlaceholder = "dev-only-not-secure-replace-in-prod"

// minAPISecretLen é o tamanho mínimo do MEZ_API_JWT_SECRET. 32 bytes =
// 256 bits, compatível com HS256 (RFC 7518 §3.2). Abaixo disso é
// brute-forcável em horas. Issue #144 (H6 audit, DREAD 5.0).
const minAPISecretLen = 32

// ValidateServe checks fields required only by the 'serve' subcommand.
//
// Issue #130 (C2) + #144 (H6): exige MEZ_API_JWT_SECRET com tamanho
// mínimo de 32 bytes e rejeita o literal dev conhecido.
//
// Issue #142 (H6b, Sprint 0B): além de length, exige Shannon entropy
// >= MinEntropyBits (3.5) para rejeitar segredos previsíveis (all-same,
// padrões repetitivos, low-variety).
func (c Config) ValidateServe() error {
	if c.SessionSecret == "" {
		return fmt.Errorf("MEZ_SESSION_SECRET is required for serve")
	}
	if len(c.APIJWTSecret) < minAPISecretLen {
		return fmt.Errorf("MEZ_API_JWT_SECRET must be at least %d bytes (256 bits); got %d", minAPISecretLen, len(c.APIJWTSecret))
	}
	if c.APIJWTSecret == devAPISecretPlaceholder {
		return fmt.Errorf("MEZ_API_JWT_SECRET is set to the dev placeholder literal; replace with a real secret (>= %d bytes)", minAPISecretLen)
	}
	// Issue #142: entropy check adicional
	entropy := ShannonEntropy(c.APIJWTSecret)
	if entropy < MinEntropyBits {
		return fmt.Errorf("MEZ_API_JWT_SECRET entropy too low (%.2f bits < %.2f min); generate with `openssl rand -base64 32`", entropy, MinEntropyBits)
	}
	return nil
}
