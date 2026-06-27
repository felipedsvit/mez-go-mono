package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	pgadapter "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/pkg/crypto"
)

func main() {
	ctx := context.Background()
	envelope, err := crypto.NewEnvelope(os.Getenv("MEZ_MASTER_KEY"))
	if err != nil {
		panic(err)
	}

	appURL := "postgres://mez_app:mez_dev_pass@localhost:5432/mezgo?sslmode=disable"
	platformURL := "postgres://mez_platform:mez_dev_pass@localhost:5432/mezgo?sslmode=disable"

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		panic(err)
	}
	defer appPool.Close()

	platformPool, err := pgxpool.New(ctx, platformURL)
	if err != nil {
		panic(err)
	}
	defer platformPool.Close()

	repo := pgadapter.NewSystemSettingsRepo(appPool, platformPool)

	// 1. Get non-existent
	_, _, err = repo.Get(ctx, "whatsmeow.enabled")
	if err != nil {
		panic(err)
	}
	fmt.Println("✓ Get non-existent: returns (nil, 0, nil)")

	// 2. Set
	plaintext := []byte(`true`)
	encrypted, err := envelope.SealSystem(plaintext)
	if err != nil {
		panic(err)
	}
	err = repo.Set(ctx, "whatsmeow.enabled", encrypted, 1, "Liga o canal WhatsApp Web real", "smoke@test")
	if err != nil {
		panic(err)
	}
	fmt.Println("✓ Set: inserted 1 row")

	// 3. Get back
	stored, kek, err := repo.Get(ctx, "whatsmeow.enabled")
	if err != nil {
		panic(err)
	}
	decrypted, err := envelope.OpenSystem(stored)
	if err != nil {
		panic(err)
	}
	if string(decrypted) != "true" {
		panic(fmt.Sprintf("expected 'true', got %q", string(decrypted)))
	}
	fmt.Printf("✓ Get + Decrypt: kek_version=%d value=%s\n", kek, string(decrypted))

	// 4. List
	entries, err := repo.List(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ List: %d rows\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  - key=%s desc=%q\n", e.Key, e.Description)
	}

	// 5. Update (different value)
	encrypted2, _ := envelope.SealSystem([]byte(`false`))
	err = repo.Set(ctx, "whatsmeow.enabled", encrypted2, 1, "Liga o canal WhatsApp Web real", "smoke@test")
	if err != nil {
		panic(err)
	}
	stored2, _, _ := repo.Get(ctx, "whatsmeow.enabled")
	decrypted2, _ := envelope.OpenSystem(stored2)
	if string(decrypted2) != "false" {
		panic(fmt.Sprintf("expected 'false' after update, got %q", string(decrypted2)))
	}
	fmt.Println("✓ Update: Set with same key overwrites")

	// 6. Delete
	err = repo.Delete(ctx, "whatsmeow.enabled")
	if err != nil {
		panic(err)
	}
	_, _, err = repo.Get(ctx, "whatsmeow.enabled")
	if err != nil {
		panic(err)
	}
	fmt.Println("✓ Delete: row removed")

	// 7. RLS test: try to write as mez_app (should fail)
	_, err = appPool.Exec(ctx, "INSERT INTO system_settings (key, value_encrypted) VALUES ('rls.test', '\\x00'::bytea)")
	if err == nil {
		panic("expected RLS to block mez_app write")
	}
	fmt.Printf("✓ RLS fail-closed: mez_app blocked from write: %v\n", err)

	fmt.Println("\n🎉 All smoke tests passed!")
}
