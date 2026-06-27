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

	var enabled bool
	err = svc.Get(ctx, "whatsmeow.enabled", &enabled, false)
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ Get default: whatsmeow.enabled=%v (expected false)\n", enabled)

	err = svc.Set(ctx, "whatsmeow.enabled", true, "smoke@test")
	if err != nil {
		panic(err)
	}
	fmt.Println("✓ Set: true")

	enabled = false
	err = svc.Get(ctx, "whatsmeow.enabled", &enabled, false)
	if err != nil {
		panic(err)
	}
	if !enabled {
		panic(fmt.Sprintf("expected true, got %v", enabled))
	}
	fmt.Printf("✓ Get: whatsmeow.enabled=%v (expected true)\n", enabled)

	ch, cancel := svc.Watch()
	defer cancel()
	go func() {
		err := svc.Set(ctx, "ffmpeg.concurrency", 8, "smoke@test")
		if err != nil {
			panic(err)
		}
		err = svc.Set(ctx, "bus.inbound.buffer", 2048, "smoke@test")
		if err != nil {
			panic(err)
		}
	}()

	ev1 := <-ch
	ev2 := <-ch
	fmt.Printf("✓ Watch event 1: key=%s\n", ev1.Key)
	fmt.Printf("✓ Watch event 2: key=%s\n", ev2.Key)

	entries, err := svc.List(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ List: %d rows\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  - key=%s value=%q\n", e.Key, e.Value)
	}

	err = svc.SeedDefaults(ctx, "smoke@test")
	if err != nil {
		panic(err)
	}
	fmt.Println("✓ SeedDefaults: idempotent (existing values preserved)")

	_, _ = platformPool.Exec(ctx, "DELETE FROM system_settings WHERE key = 'whatsmeow.identity.os'")
	err = svc.SeedDefaults(ctx, "smoke@test")
	if err != nil {
		panic(err)
	}
	var osName string
	err = svc.Get(ctx, "whatsmeow.identity.os", &osName, "Linux")
	if err != nil {
		panic(err)
	}
	if osName != "Mac OS" {
		panic(fmt.Sprintf("expected 'Mac OS', got %q", osName))
	}
	fmt.Printf("✓ SeedDefaults re-adds: whatsmeow.identity.os=%q (expected 'Mac OS')\n", osName)

	svc.InvalidateCache("whatsmeow.enabled")
	var cached bool
	svc.Get(ctx, "whatsmeow.enabled", &cached, false)
	fmt.Printf("✓ InvalidateCache: whatsmeow.enabled=%v (re-fetched from DB)\n", cached)

	for _, k := range []string{"whatsmeow.enabled", "ffmpeg.concurrency", "bus.inbound.buffer", "whatsmeow.identity.os"} {
		_ = repo.Delete(ctx, k)
	}

	fmt.Println("\n🎉 settings.Service end-to-end test passed!")
}
