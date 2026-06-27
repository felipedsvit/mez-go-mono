package oidc

import (
	"context"

	gooidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type Verifier struct {
	verifier *gooidc.IDTokenVerifier
}

type Claims struct {
	Issuer   string `json:"iss"`
	Subject  string `json:"sub"`
	Email    string `json:"email"`
	TenantID string `json:"tenant_id,omitempty"`
}

func NewVerifier(ctx context.Context, issuer, audience string) (*Verifier, error) {
	provider, err := gooidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	verifier := provider.Verifier(&gooidc.Config{
		ClientID: audience,
	})

	return &Verifier{verifier: verifier}, nil
}

func (v *Verifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, err
	}

	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		return Claims{}, err
	}

	if claims.Email == "" {
		return Claims{}, admin.ErrEmailNotVerified
	}

	return claims, nil
}
