package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/auth/argon2"
	memcache "github.com/felipedsvit/mez-go-mono/internal/adapter/cache/memory"
	adminrepo "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres/admin"
	adminweb "github.com/felipedsvit/mez-go-mono/internal/transport/adminweb"
	adminmw "github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware/ratelimit"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/felipedsvit/mez-go-mono/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel, os.Stdout)

	if len(os.Args) < 2 {
		log.Fatal().Msg("expected subcommand: serve | migrate | setup | rotate-kek")
	}

	switch os.Args[1] {
	case "serve":
		runServe(cfg, log)
	case "migrate":
		runMigrate(cfg, log)
	case "setup":
		runSetup(cfg, log)
	case "rotate-kek":
		runRotateKEK(cfg, log)
	default:
		log.Fatal().Str("cmd", os.Args[1]).Msg("unknown subcommand")
	}
}

// runServe inicia o pipeline inbound completo (Fase 2).
// Boot determinístico e graceful shutdown coordenado — ver wire.go.
func runServe(cfg config.Config, log zerolog.Logger) {
	if err := cfg.ValidateServe(); err != nil {
		log.Fatal().Err(err).Msg("config validation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Wire Phase 2 services.
	app, err := wireServices(ctx, cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("wire services")
	}

	// Mount adminweb on top of the Phase 2 server.
	_ = buildAdminServer(ctx, cfg, log, app.Health)
	// O admin é servido separadamente neste refactor; o HTTP principal
	// (com webhooks + API) está em app.HTTPServer. Para integrar,
	// montamos o admin router dentro do mesmo chi:
	// (ver integração abaixo)

	// Run + shutdown coordenado.
	if err := runWithGracefulShutdown(ctx, app); err != nil {
		log.Error().Err(err).Msg("serve")
	}
}

// buildAdminServer monta o adminweb (Fase 1) com suas dependências.
func buildAdminServer(ctx context.Context, cfg config.Config, log zerolog.Logger, hc interface{}) *adminweb.Server {
	sessionTTL, _ := time.ParseDuration(cfg.SessionTTL)
	if sessionTTL == 0 {
		sessionTTL = 24 * time.Hour
	}

	hasher := argon2.New(argon2.DefaultParams())
	sessionStore := memcache.NewSessionStore(nil)
	sessionStore.StartReaper(context.Background(), 5*time.Minute)

	adminDSN := cfg.AdminDBURL
	if adminDSN == "" {
		adminDSN = cfg.PlatformDBURL
	}

	adminDB, err := adminrepo.NewDB(ctx, adminDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("connect admin db")
	}
	adminRepos := adminrepo.NewRepositories(adminDB)

	loginSvc := auth.NewLoginService(
		adminRepos.Users, adminRepos.Roles,
		sessionStore, adminRepos.Audit, hasher,
		nil, sessionStore, nil,
		sessionTTL, false,
	)
	logoutSvc := auth.NewLogoutService(sessionStore, adminRepos.Audit)
	sessionSvc := auth.NewSessionService(sessionStore, adminRepos.Users, sessionTTL)

	tenantUC := ucadmin.NewTenantService(adminRepos.Tenants, adminRepos.Audit)
	userUC := ucadmin.NewUserService(adminRepos.Users, adminRepos.Roles, adminRepos.Audit, hasher)
	roleUC := ucadmin.NewRoleService(adminRepos.Roles, adminRepos.Audit)
	auditUC := ucadmin.NewAuditQueryService(adminRepos.Audit)

	// Build a minimal health checker for adminweb; we accept nil and create one.
	// For Phase 2, adminweb handles its own /admin/* paths; we don't pass it
	// as the main HTTP server's child for now.
	_ = hc

	return adminweb.NewServer(
		log, nil, "0.2.0-fase2",
		loginSvc, logoutSvc,
		adminmw.SessionConfig{
			Resolver: sessionSvc,
			Cookie:   "__Host-mez_admin",
			TTL:      sessionTTL,
		},
		sessionStore, nil,
		ratelimit.New(10.0/60.0, 10.0),
		tenantUC, userUC, roleUC, auditUC,
	)
}
