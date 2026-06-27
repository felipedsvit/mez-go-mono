package admin

import "context"

type OIDCState struct {
	State         string `json:"state"`
	CodeVerifier  string `json:"code_verifier"`
	RedirectAfter string `json:"redirect_after"`
	// Nonce (issue #147 H2 audit, Sprint 0B): gerado em StartOIDC, incluído
	// no AuthCodeURL, validado em LoginOIDC contra o claim nonce do
	// ID-token. Bloqueia replay de ID-token capturado.
	Nonce         string `json:"nonce,omitempty"`
}

type StateStore interface {
	SaveState(ctx context.Context, key string, state OIDCState, ttl int) error
	LoadState(ctx context.Context, key string) (OIDCState, error)
	DeleteState(ctx context.Context, key string) error
}
