// Package whatsmeow — real_factory.go: factory do *RealClient.
//
// Issue #158 (Fase 9, sub-issue F). Constrói um ClientFactory que usa
// o RealClient (sqlstore + device store) em vez do stub.
//
// Pré-requisitos:
//
//   - Postgres com permissão DDL para criar as tabelas whatsmeow_*.
//   - ffmpeg/cwebp no container (Deployments/Dockerfile já os instala).
//   - MEZ_WHATSMEOW_ENABLED=true e MEZ_WHATSMEOW_DEVICE_DSN=<pgx DSN>.
//
// Uso em cmd/server/wire.go:
//
//	identity := whatsmeow.IdentityFromConfig(cfg.IdentityKind, cfg.IdentityOS)
//	manager.SetClientFactory(identity, whatsmeow.NewRealClientFactory(
//	    RealFactoryConfig{DeviceDSN: cfg.DeviceDSN, Transcoder: transcoder, Log: log},
//	))
package whatsmeow

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// RealFactoryConfig configura NewRealClientFactory.
type RealFactoryConfig struct {
	// DeviceDSN para o session store (sqlstore do whatsmeow).
	DeviceDSN string
	// Transcoder opcional para OGG/Opus + WebP.
	Transcoder MediaTranscoder
	// Log zerolog (a factory envelopa em waLog.Zerolog internamente).
	Log zerolog.Logger
}

// NewRealClientFactory retorna um ClientFactory que cria RealClient.
//
// A factory é idempotente em termos de Identity (Manager.SetClientFactory
// aplica DeviceIdentity.Apply() uma vez antes do primeiro GetOrCreate).
// Mas o whatsmeow requer que store.SetOSInfo seja chamado ANTES de
// qualquer NewClient — por isso, a factory assume que SetClientFactory
// foi chamado antes.
func NewRealClientFactory(cfg RealFactoryConfig) ClientFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (Client, error) {
		tenant := string(tenantID)
		client, err := NewRealClient(ctx, tenant, RealClientConfig{
			DeviceDSN:   cfg.DeviceDSN,
			Transcoder:  cfg.Transcoder,
			WaLog:       nil, // zerolog wraperá internamente
		}, cfg.Log)
		if err != nil {
			return nil, err
		}
		// Connect é assíncrono no whatsmeow (não bloqueia). Caller pode
		// chamar Connect() explicitamente se quiser esperar o socket.
		// Aqui deixamos lazy — o Dispatcher pega Connect no primeiro Send.
		_ = client.Connect(ctx)
		return client, nil
	}
}
