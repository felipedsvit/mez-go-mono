// Package e2e contém testes end-to-end do mez-go-mono.
//
// Estes testes montam o pipeline completo (webhook → handler → bus →
// memory sender) e exercitam o caminho HTTP real via httptest.NewServer.
// Não usam testcontainers — toda a infra é in-memory.
//
// Categorias:
//   - E2E_Webhook: full request flow com Meta/Telegram webhooks
//   - E2E_Outbound: bus → sender flow (PublishOutbound → handler → sender.Send)
//   - E2E_BusLifecycle: drain, handler panic recovery, ordem de eventos
//   - E2E_Capability: degradation media→text end-to-end via port.Resolver
//
// Estes testes rodam com `go test ./...` (sem build tag).
package e2e
