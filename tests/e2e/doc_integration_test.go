//go:build integration

// Package e2e — testes E2E com testcontainers (Postgres real).
//
// Estes testes sobem um Postgres 16-alpine via testcontainers, aplicam
// as migrations, seedam um tenant, e executam o pipeline completo:
//
//	Meta webhook → ingestor real → outbox enqueue → relay poll → sender
//
// Requisitos: docker daemon rodando.
//
// Para rodar:
//
//	go test -tags=integration -race ./tests/e2e/...
//
// Se o docker daemon não estiver disponível, os testes são pulados (t.Skipf).
package e2e
