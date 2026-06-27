package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/auth/argon2"
	adminrepo "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/pkg/config"
)

func runSetup(cfg config.Config, log zerolog.Logger) {
	ctx := context.Background()

	email := os.Getenv("MEZ_SETUP_EMAIL")
	password := os.Getenv("MEZ_SETUP_PASSWORD")

	if email == "" || password == "" {
		fmt.Fprintln(os.Stderr, "MEZ_SETUP_EMAIL and MEZ_SETUP_PASSWORD environment variables required")
		os.Exit(1)
	}

	adminDSN := cfg.AdminDBURL
	if adminDSN == "" {
		adminDSN = cfg.PlatformDBURL
	}

	db, err := adminrepo.NewDB(ctx, adminDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("connect admin db")
	}
	defer db.Close()

	repos := adminrepo.NewRepositories(db)

	_, err = repos.Users.GetByEmail(ctx, email)
	if err == nil {
		log.Info().Str("email", email).Msg("admin user already exists")
		return
	}

	hasher := argon2.New(argon2.DefaultParams())

	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		log.Fatal().Err(err).Msg("hash password")
	}

	user := &admin.AdminUser{
		Email:        email,
		Name:         "Admin",
		AuthKind:     admin.AuthKindLocal,
		Status:       admin.UserStatusActive,
		PasswordHash: hash,
	}

	if err := repos.Users.Insert(ctx, user); err != nil {
		log.Fatal().Err(err).Msg("insert admin user")
	}

	if err := repos.Users.AssignRole(ctx, user.ID, "role-platform-superadmin", ""); err != nil {
		log.Fatal().Err(err).Msg("assign superadmin role")
	}

	log.Info().Str("email", email).Msg("admin user created successfully")
}
