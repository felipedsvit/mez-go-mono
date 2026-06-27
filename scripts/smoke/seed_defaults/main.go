package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	pgadapter "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/settings"
	"github.com/felipedsvit/mez-go-mono/pkg/crypto"
)

func main() {
	ctx := context.Background()
	envelope, err := crypto.NewEnvelope(os.Getenv("MEZ_MASTER_KEY"))
	if err != nil {
		panic(err)
	}

	appPool, _ := pgxpool.New(ctx, "postgres://mez_app:mez_dev_pass@localhost:5432/mezgo?sslmode=disable")
	defer appPool.Close()
	platformPool, _ := pgxpool.New(ctx, "postgres://mez_platform:mez_dev_pass@localhost:5432/mezgo?sslmode=disable")
	defer platformPool.Close()

	repo := pgadapter.NewSystemSettingsRepo(appPool, platformPool)
	sealer := settings.NewEnvelopeSealer(envelope)
	log := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	svc := settings.NewService(repo, sealer, 1, log)

	err = svc.SeedDefaults(ctx, "smoke@test")
	if err != nil {
		panic(err)
	}

	entries, err := svc.List(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ SeedDefaults created %d rows:\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  - %-30s = %-10s  kek=%d  desc=%q\n", e.Key, e.Value, e.KekVersion, e.Description)
	}
}
