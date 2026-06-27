package admin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
)

type UserInfo struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

type IdP interface {
	AuthCodeURL(state, codeChallenge string) string
	// AuthCodeURLWithNonce inclui o nonce. IdP ecoa no claim nonce do
	// ID-token. Issue #147 (H2 audit).
	AuthCodeURLWithNonce(state, codeChallenge, nonce string) string
	Exchange(ctx context.Context, code, codeVerifier string) (string, error)
	VerifyIDToken(ctx context.Context, rawIDToken string) (UserInfo, error)
}

func ChallengeFromVerifier(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
