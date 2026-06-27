package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type Client struct {
	provider *gooidc.Provider
	verifier *gooidc.IDTokenVerifier
	oauth    *oauth2.Config
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}

	scopes := cfg.Scopes
	if scopes == nil {
		scopes = []string{gooidc.ScopeOpenID, "profile", "email"}
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	verifier := provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})

	c := &Client{
		provider: provider,
		verifier: verifier,
		oauth:    oauthCfg,
	}

	return c, nil
}

func (c *Client) AuthCodeURL(state, codeChallenge string) string {
	return c.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// AuthCodeURLWithNonce inclui o nonce no AuthCodeURL. IdP deve ecoar o
// mesmo nonce no claim `nonce` do ID-token; verificação no callback
// via gooidc.VerifyNonce. Issue #147 (H2 audit, Sprint 0B).
func (c *Client) AuthCodeURLWithNonce(state, codeChallenge, nonce string) string {
	return c.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

func (c *Client) Exchange(ctx context.Context, code, codeVerifier string) (string, error) {
	token, err := c.oauth.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return "", err
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", admin.ErrStateMismatch
	}

	return rawIDToken, nil
}

func (c *Client) VerifyIDToken(ctx context.Context, rawIDToken string) (admin.UserInfo, error) {
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return admin.UserInfo{}, err
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return admin.UserInfo{}, err
	}

	return admin.UserInfo{
		Issuer:        idToken.Issuer,
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
	}, nil
}

func ChallengeFromVerifier(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
