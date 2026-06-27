package admin

import "context"

type OIDCState struct {
	State         string `json:"state"`
	CodeVerifier  string `json:"code_verifier"`
	RedirectAfter string `json:"redirect_after"`
}

type StateStore interface {
	SaveState(ctx context.Context, key string, state OIDCState, ttl int) error
	LoadState(ctx context.Context, key string) (OIDCState, error)
	DeleteState(ctx context.Context, key string) error
}
