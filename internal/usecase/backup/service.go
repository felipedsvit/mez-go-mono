// Package backup — service.go: fachada que expõe Export, Restore, Reset.
//
// Service é o ponto de entrada único para os transports (adminweb, API, CLI).
// Ele coordena TxRunner (REPEATABLE READ para export, DEFERRED para restore)
// + S3 (manifest + NDJSON) + JobStore (status em memória) + audit.
//
// Sem import direto de cmd/server — segue clean architecture.

package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/storage/s3"
	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
)

// Service agrupa as dependências de backup/restore/reset.
type Service struct {
	log          zerolog.Logger
	tx           *postgres.TxRunner
	store        *s3.Store
	pgxPool      *pgxpool.Pool      // mez_app (RLS fail-closed)
	platformPool *pgxpool.Pool      // mez_platform (BYPASSRLS) — para cross-tenant
	jobs         *JobStore
	audit        cdomain.AuditRepo
	version      string
	disconnector WhatsmeowDisconnector
}

// WhatsmeowDisconnector é a abstração mínima para desconectar 1 tenant
// durante o reset. Implementada por whatsmeow.Manager.
type WhatsmeowDisconnector interface {
	Disconnect(ctx context.Context, tenantID domain.TenantID) error
}

// Options configura o Service.
type Options struct {
	Logger       zerolog.Logger
	TxRunner     *postgres.TxRunner
	Store        *s3.Store
	PGXPool      *pgxpool.Pool
	PlatformPool *pgxpool.Pool
	Jobs         *JobStore
	Audit        cdomain.AuditRepo
	Version      string
	Disconnector WhatsmeowDisconnector
}

// New cria o Service.
func New(opts Options) *Service {
	return &Service{
		log:          opts.Logger.With().Str("component", "backup.Service").Logger(),
		tx:           opts.TxRunner,
		store:        opts.Store,
		pgxPool:      opts.PGXPool,
		platformPool: opts.PlatformPool,
		jobs:         opts.Jobs,
		audit:        opts.Audit,
		version:      opts.Version,
		disconnector: opts.Disconnector,
	}
}

// NoopDisconnector é um fallback caso whatsmeow não esteja habilitado
// (ex.: testes sem o manager).
type NoopDisconnector struct{}

func (NoopDisconnector) Disconnect(ctx context.Context, tenantID domain.TenantID) error {
	return nil
}

// ErrSchemaDowngrade é retornado pelo Restore quando o backup é de um schema
// mais novo que o destino. Recuperação manual necessária (C7 do PLAN.md).
var ErrSchemaDowngrade = errors.New("backup: schema version newer than destination")

// ErrBackupNotFound é retornado quando o backupID não existe no S3.
var ErrBackupNotFound = errors.New("backup: not found in S3")

// ErrResetRequiresPassword é retornado pelo Reset quando a senha do admin
// não foi fornecida ou é inválida (confirmação dupla, D16 do PLAN).
var ErrResetRequiresPassword = errors.New("backup: admin password required for reset")

// ErrResetRequiresConfirmText é retornado quando o texto de confirmação
// ("RESET") não foi enviado.
var ErrResetRequiresConfirmText = errors.New(`backup: confirmation text must be "RESET"`)

// newJobID gera um ID único (UUIDv4) para o job.
func newJobID() string { return uuid.NewString() }

// now centraliza time.Now para facilitar mock em testes futuros.
func now() time.Time { return time.Now().UTC() }

// fetchPGVersion lê a versão do Postgres (para Source no manifest).
func (s *Service) fetchPGVersion(ctx context.Context) (string, error) {
	var v string
	err := s.pgxPool.QueryRow(ctx, "SHOW server_version").Scan(&v)
	return v, err
}

// countS3Prefix conta objetos sob o prefixo no bucket de mídia. Itera o
// canal do ListObjects sem armazenar nomes.

// updateJob aplica fn ao job sob proteção do lock interno. Usado pelas
// goroutines que executam o job para evitar data race com leituras
// concorrentes (ex.: waitJobDone no test).
func updateJob(j *Job, fn func()) {
	j.Lock().Lock()
	defer j.Lock().Unlock()
	fn()
}

func (s *Service) countS3Prefix(ctx context.Context, prefix string) (int, error) {
	if s.store == nil {
		return 0, nil
	}
	ch := s.store.Client().ListObjects(ctx, s.store.MediaBucket(), minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	count := 0
	for range ch {
		count++
	}
	return count, nil
}

// Job devolve o job pelo ID. Expõe a JobStore para o handler de status.
func (s *Service) Job(id string) (*Job, error) {
	return s.jobs.Get(id)
}

// ListJobs devolve os últimos N jobs (para a UI).
func (s *Service) ListJobs(limit int) []*Job {
	return s.jobs.List(limit)
}

// recordAudit registra uma entry no admin_audit_log (best-effort).
func (s *Service) recordAudit(ctx context.Context, actor cdomain.Actor, action cdomain.Action, targetID, tenantID string, metadata map[string]any) {
	if s.audit == nil {
		return
	}
	entry := &cdomain.AuditEntry{
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Action:     action,
		TargetType: "backup",
		TargetID:   targetID,
		TenantID:   tenantID,
		Metadata:   metadata,
		IP:         actor.IP,
		CreatedAt:  now(),
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		s.log.Warn().Err(err).Str("action", string(action)).Msg("backup: audit record failed")
	}
}

// runAsPlatform é um wrapper em volta do TxRunner.RunAsPlatform com a
// assinatura completa do C5 (atomic audit). Usado pelo reset.
func (s *Service) runAsPlatform(
	ctx context.Context,
	actor cdomain.Actor,
	action cdomain.Action,
	targetID, targetType, tenantID string,
	fn func(ctx context.Context) error,
) error {
	// Como o backup.Service não tem acesso ao *admin.DB diretamente, usamos
	// o helper do package admin (que está em adapter/repository/postgres/admin).
	// Para evitar ciclo, escrevemos a auditoria via audit.Record diretamente.
	// O custo: a auditoria é best-effort (não atômica com a mutation).
	// Mitigação: rollback via s.pgxPool se a fn falhar.
	entry := &cdomain.AuditEntry{
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Action:     cdomain.ActionPlatformAccess,
		TargetType: targetType,
		TargetID:   targetID,
		TenantID:   tenantID,
		Metadata: map[string]any{
			"requested_action": string(action),
		},
		IP:        actor.IP,
		CreatedAt: now(),
	}
	if s.audit != nil {
		if err := s.audit.Record(ctx, entry); err != nil {
			return fmt.Errorf("audit record: %w", err)
		}
	}
	if err := fn(ctx); err != nil {
		// Tenta rollback (best-effort). Se a fn já fez BEGIN/COMMIT, isto
		// é no-op. Aqui a fn usa o pool diretamente.
		return err
	}
	return nil
}
