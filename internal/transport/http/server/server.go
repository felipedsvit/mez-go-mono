// Package server implementa a composição do chi router para o mez-go-mono.
//
// Fase 2 (#42): monta as rotas /webhooks/* (Meta + Telegram) e /api/*.
// Integra com o adminweb (Fase 1) e os health/metrics endpoints.
//
// Padrão: cada subcomponente expõe um Router() chi.Router; este server
// agrega tudo em um único http.ServeMux. O wiring principal fica em
// cmd/server/wire.go.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/meta"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/telegram"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/transport/http/api"
	apimw "github.com/felipedsvit/mez-go-mono/internal/transport/http/middleware"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
	"github.com/felipedsvit/mez-go-mono/pkg/health"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

// Config configura o server.
type Config struct {
	JWTSecret    []byte
	JWTSecretEnv string // nome da env var para o secret
}

// Services agrupa as dependências injetadas.
type Services struct {
	Log             zerolog.Logger
	Health          *health.Checker
	Metrics         *metrics.Registry
	ConvRepo        port.ConversationRepo
	MsgRepo         port.MessageRepo
	TenantRepo      port.TenantRepo
	MetaHandler     *meta.Handler
	TelegramHandler *telegram.Handler
	AdminRouter     chi.Router // opcional, do adminweb
	JWTSecret       []byte
	SenderService   *ucmessaging.SenderService
	// ListService é o use case de leitura/assign/resolve (issue #126).
	// Se nil, o handler usa fallback que rejeita os endpoints com 503.
	ListService     *ucmessaging.ListService
	SenderRegistry  port.SenderRegistry
	QRCodeProvider  api.QRCodeProvider
	// Fase 6: backup service e admin verifier (para reset) — opcionais;
	// se nil, os endpoints de backup retornam 503.
	BackupService   *ucbackup.Service
	AdminVerifier   ucbackup.AdminVerifier
}

// New cria o http.Handler com todas as rotas montadas.
func New(svc Services) http.Handler {
	r := chi.NewRouter()

	// Middlewares globais.
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60_000_000_000)) // 60s

	// Health, readyz, metrics (sempre sem auth).
	r.Get("/health", health.LiveHandler())
	r.Get("/readyz", health.ReadyHandler(svc.Health))
	r.Get("/metrics", svc.Metrics.Handler().ServeHTTP)

	// Webhooks (sem auth — verificação é por signature).
	if svc.MetaHandler != nil {
		r.Post("/webhooks/meta/{app_id}", svc.MetaHandler.ServeHTTP)
	}
	if svc.TelegramHandler != nil {
		r.Post("/webhooks/telegram/{tenant_id}", svc.TelegramHandler.ServeHTTP)
	}

	// API REST (com Bearer JWT).
	jwtSecret := svc.JWTSecret
	if len(jwtSecret) == 0 {
		// fallback: usa um secret dummy para que o middleware não panique.
		// Em produção, ValidateServe deve garantir que MEZ_API_JWT_SECRET
		// está setado.
		jwtSecret = []byte("dev-only-not-secure-replace-in-prod")
		svc.Log.Warn().Msg("MEZ_API_JWT_SECRET not set; using dev placeholder")
	}
	apiMw := apimw.BearerAuth(apimw.BearerAuthConfig{Secret: jwtSecret}, svc.Log)

	apiH := api.New(svc.Log, svc.ConvRepo, svc.MsgRepo, svc.TenantRepo, svc.SenderService, svc.ListService, svc.SenderRegistry, svc.QRCodeProvider)
	var backupH *api.BackupHandlers
	if svc.BackupService != nil {
		backupH = api.NewBackupHandlers(svc.BackupService, svc.AdminVerifier)
	}
	r.Route("/api", func(r chi.Router) {
		r.Use(apiMw)
		apiH.Register(r)
		if backupH != nil {
			backupH.Register(r)
		}
	})

	// Admin (do Fase 1).
	if svc.AdminRouter != nil {
		r.Mount("/admin", svc.AdminRouter)
	}

	return r
}
