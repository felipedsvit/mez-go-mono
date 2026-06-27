package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/transport/http/api"
	"github.com/rs/zerolog"
)

// makeToken cria um JWT HS256 para testes.
func makeToken(t *testing.T, secret []byte, claims Claims) string {
	t.Helper()
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payloadJSON, _ := json.Marshal(claims)
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signing := header + "." + payload
	mac := hmac256Sum(secret, []byte(signing))
	sig := base64.RawURLEncoding.EncodeToString(mac)
	return signing + "." + sig
}

func hmac256Sum(secret, data []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return mac.Sum(nil)
}

// futureExp devolve um exp = now + 1h (token válido).
func futureExp() int64 {
	return time.Now().Add(time.Hour).Unix()
}

func TestBearerAuth_NoHeader(t *testing.T) {
	mw := BearerAuth(BearerAuthConfig{Secret: []byte("s")}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_InvalidToken(t *testing.T) {
	mw := BearerAuth(BearerAuthConfig{Secret: []byte("s")}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	token := makeToken(t, secret, Claims{TenantID: "t1", Exp: futureExp()})

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	var gotTenant string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, _ := api.TenantFromContext(r.Context())
		gotTenant = string(t)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotTenant != "t1" {
		t.Errorf("tenant = %q, want t1", gotTenant)
	}
}

func TestBearerAuth_BadScheme(t *testing.T) {
	mw := BearerAuth(BearerAuthConfig{Secret: []byte("s")}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Basic abc")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_MissingTenantClaim(t *testing.T) {
	secret := []byte("s")
	token := makeToken(t, secret, Claims{Exp: futureExp()}) // sem tenant_id

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (missing tenant_id)", rec.Code)
	}
}

func TestBearerAuth_RejectsExpiredToken(t *testing.T) {
	secret := []byte("s")
	token := makeToken(t, secret, Claims{
		TenantID: "t1",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(), // expirado há 1h
	})

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (expired)", rec.Code)
	}
}

func TestBearerAuth_RejectsMissingExp(t *testing.T) {
	secret := []byte("s")
	token := makeToken(t, secret, Claims{TenantID: "t1"}) // sem exp

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (missing exp)", rec.Code)
	}
}

func TestBearerAuth_RejectsExpZero(t *testing.T) {
	secret := []byte("s")
	token := makeToken(t, secret, Claims{TenantID: "t1", Exp: 0}) // exp=0 (1970)

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (exp=0)", rec.Code)
	}
}

func TestBearerAuth_RejectsNbfFuture(t *testing.T) {
	secret := []byte("s")
	token := makeToken(t, secret, Claims{
		TenantID: "t1",
		Exp:      futureExp(),
		Nbf:      time.Now().Add(10 * time.Minute).Unix(), // válido só em 10min
	})

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (nbf future)", rec.Code)
	}
}

func TestBearerAuth_InjectsActor(t *testing.T) {
	secret := []byte("s")
	token := makeToken(t, secret, Claims{
		TenantID: "t1",
		Exp:      futureExp(),
		Sub:      "user-42",
		Email:    "admin@example.com",
	})

	mw := BearerAuth(BearerAuthConfig{Secret: secret}, zerolog.Nop())
	var gotActor api.Actor
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActor, _ = api.ActorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotActor.ID != "user-42" {
		t.Errorf("actor.ID = %q, want user-42", gotActor.ID)
	}
	if gotActor.Email != "admin@example.com" {
		t.Errorf("actor.Email = %q, want admin@example.com", gotActor.Email)
	}
}

func TestParseAndValidateJWT_BadSignature(t *testing.T) {
	secret := []byte("good")
	token := makeToken(t, secret, Claims{TenantID: "t1", Exp: futureExp()})

	// Mude 1 char do token.
	tampered := token[:len(token)-2] + "AA"

	_, _, err := parseAndValidateJWT(tampered, secret)
	if err == nil {
		t.Error("expected error for tampered signature")
	}
}

func TestParseAndValidateJWT_AlgNone(t *testing.T) {
	// JWT com alg=none (ataque conhecido).
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"tenant_id":"t1","exp":9999999999}`))
	token := header + "." + payload + "."

	_, _, err := parseAndValidateJWT(token, []byte("s"))
	if err == nil {
		t.Error("expected error for alg=none")
	}
}

func TestContextWithTenant(t *testing.T) {
	ctx := api.ContextWithTenant(context.Background(), "tenant-X")
	t2, ok := api.TenantFromContext(ctx)
	if !ok {
		t.Fatal("not found")
	}
	if t2 != "tenant-X" {
		t.Errorf("got %q, want tenant-X", t2)
	}
}
