package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/secrets"
	"github.com/felipedsvit/mez-go-mono/pkg/config"
)

// flagsCLI do subcomando rotate-kek.
//
// Suportados:
//
//	--dry-run            não persiste; apenas conta o que seria feito
//	--actor <string>     string para registrar nos audit rows (default: $USER)
//	--json               saída em JSON em vez de texto tabular
//	--timeout <duration> timeout total da operação (default: 30m)
func runRotateKEK(cfg config.Config, log zerolog.Logger) {
	fs := flag.NewFlagSet("rotate-kek", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "não persiste; apenas conta o que seria feito")
	actor := fs.String("actor", "", "actor para audit log (default: operator:$USER)")
	asJSON := fs.Bool("json", false, "saída em JSON em vez de texto")
	timeout := fs.Duration("timeout", 30*time.Minute, "timeout total")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "rotate-kek: %v\n", err)
		os.Exit(2)
	}

	// Resolve actor (default = operator:$USER).
	effectiveActor := *actor
	if effectiveActor == "" {
		u := os.Getenv("USER")
		if u == "" {
			u = "unknown"
		}
		effectiveActor = "operator:" + u
	}

	// Lê KEKs (env var ou _FILE variant).
	oldKEK, err := readKeyFromEnv("MEZ_MASTER_KEY", "MEZ_MASTER_KEY_FILE")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rotate-kek: %v\n", err)
		os.Exit(2)
	}
	newKEK, err := readKeyFromEnv("MEZ_MASTER_KEY_NEW", "MEZ_MASTER_KEY_NEW_FILE")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rotate-kek: %v\n", err)
		os.Exit(2)
	}
	if oldKEK == "" || newKEK == "" {
		fmt.Fprintln(os.Stderr, "rotate-kek: MEZ_MASTER_KEY e MEZ_MASTER_KEY_NEW (ou _FILE) são obrigatórios")
		os.Exit(2)
	}

	// Contexto com timeout.
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Conecta nos pools. rotate-kek NÃO roda dentro de um contexto
	// migrado (precisa de um DB existente), mas checa via Ping.
	appPool, err := postgres.ConnectPool(ctx, cfg.DatabaseURL, 4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rotate-kek: connect app: %v\n", err)
		os.Exit(2)
	}
	defer appPool.Close()

	platformPool, err := postgres.ConnectPool(ctx, cfg.PlatformDBURL, 4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rotate-kek: connect platform: %v\n", err)
		appPool.Close()
		os.Exit(2)
	}
	defer platformPool.Close()

	txRunner := postgres.NewTxRunner(appPool, platformPool, log)
	credsRepo := postgres.NewChannelCredentialsRepo(appPool, platformPool, txRunner)

	// Audit repo: usa o platformPool + admin.AuditRepo.
	auditRepo, err := newAdminAuditRepo(ctx, platformPool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rotate-kek: audit repo: %v\n", err)
		os.Exit(2)
	}

	// Roda a rotação.
	report, rerr := secrets.Rotate(ctx, credsRepo, auditRepo, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEK,
		NewKEKBase64: newKEK,
		DryRun:       *dryRun,
		Actor:        effectiveActor,
		// Sem InvalidateFn aqui: rotate-kek é um processo offline,
		// não tem Keyring cache para invalidar. A próxima leitura do
		// Keyring (após restart) vai usar o wrapped_dek novo.
	})

	// Saída.
	if *asJSON {
		emitJSON(report, rerr)
	} else {
		emitTabular(report, rerr, *dryRun, effectiveActor)
	}

	// Exit code: 0 sucesso total; 1 parcial; 2 total.
	if rerr != nil {
		// Validação ou erro estrutural (ex.: KEK inválida).
		os.Exit(2)
	}
	if len(report.Errors) > 0 {
		os.Exit(1)
	}
}

// readKeyFromEnv lê a chave de env var direta ou via _FILE. Retorna
// string vazia se nenhuma das duas estiver setada (caller decide se é erro).
//
// Issue #141 (H3 audit): quando vem de _FILE, valida permissão 0600
// e rejeita symlink via config.ReadKeyFile.
func readKeyFromEnv(env, envFile string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return strings.TrimSpace(v), nil
	}
	if path := os.Getenv(envFile); path != "" {
		return config.ReadKeyFile(path)
	}
	return "", nil
}

// adminAuditRepoAdapter é o adapter mínimo que satisfaz secrets.AuditRepository
// usando o pool platformPool. Mantido local ao cmd/server para evitar
// expor mais um tipo no package admin.
type adminAuditRepoAdapter struct {
	pool *pgxpool.Pool
}

func (a *adminAuditRepoAdapter) Record(ctx context.Context, e *admin.AuditEntry) error {
	if e.ID == "" {
		e.ID = randomID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	_, err := a.pool.Exec(ctx,
		`INSERT INTO admin_audit_log (id, actor_id, actor_email, action, target_type, target_id, tenant_id, metadata, ip, user_agent, created_at)
		 VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), '', $10)`,
		e.ID, string(e.ActorID), e.ActorEmail, string(e.Action),
		e.TargetType, e.TargetID, e.TenantID, e.Metadata, e.IP, e.CreatedAt,
	)
	return err
}

// newAdminAuditRepo cria o adapter. Erro se admin_audit_log não existir
// (não aplicamos migrations 0002 — confiamos que o operador rodou `serve`
// alguma vez ou fez migrate up).
func newAdminAuditRepo(ctx context.Context, pool *pgxpool.Pool) (*adminAuditRepoAdapter, error) {
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &adminAuditRepoAdapter{pool: pool}, nil
}

// randomID gera um UUID v4. Encapsulado para import local.
func randomID() string {
	return uuid.NewString()
}

// emitTabular imprime o relatório em formato tabwriter. Usado quando
// --json não é passado.
func emitTabular(rpt secrets.Report, err error, dryRun bool, actor string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "rotate-kek report\n")
	fmt.Fprintf(w, "  actor       \t%s\n", actor)
	fmt.Fprintf(w, "  dry-run     \t%v\n", dryRun)
	fmt.Fprintf(w, "  tenants     \t%d\n", rpt.Tenants)
	fmt.Fprintf(w, "  channels    \t%d\n", rpt.Channels)
	fmt.Fprintf(w, "  old version \t%d\n", rpt.OldVersion)
	fmt.Fprintf(w, "  new version \t%d\n", rpt.NewVersion)
	fmt.Fprintf(w, "  duration    \t%d ms\n", rpt.DurationMs)
	if err != nil {
		fmt.Fprintf(w, "  ERROR       \t%v\n", err)
	}
	if len(rpt.Errors) > 0 {
		fmt.Fprintf(w, "\n  per-row errors (%d):\n", len(rpt.Errors))
		fmt.Fprintf(w, "    TENANT\tCHANNEL\tOP\tERROR\n")
		for _, e := range rpt.Errors {
			fmt.Fprintf(w, "    %s\t%s\t%s\t%v\n", e.TenantID, e.Channel, e.Op, e.Err)
		}
	}
	_ = w.Flush()
}

// emitJSON serializa report + error (se houver) em JSON.
func emitJSON(rpt secrets.Report, err error) {
	out := map[string]any{
		"tenants":      rpt.Tenants,
		"channels":     rpt.Channels,
		"old_version":  rpt.OldVersion,
		"new_version":  rpt.NewVersion,
		"duration_ms":  rpt.DurationMs,
		"dry_run":      rpt.DryRun,
		"started_at":   rpt.StartedAt,
		"row_errors":   rpt.Errors,
	}
	if err != nil {
		out["fatal_error"] = err.Error()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// Erros sentinela (placeholder — para import de errors não ficar orfão).
var (
	_ = errors.New
	_ = domain.TenantID("")
	_ = pgxpool.Pool{}
)
