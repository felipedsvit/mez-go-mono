package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_Generates(t *testing.T) {
	called := false
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		got = RequestIDFromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	RequestID(next).ServeHTTP(rr, req)

	if !called {
		t.Fatal("next não chamado")
	}
	if got == "" {
		t.Fatal("request_id vazio")
	}
	if len(got) != 32 {
		t.Errorf("request_id deve ter 32 chars hex, got %d", len(got))
	}
	if rr.Header().Get("X-Request-ID") != got {
		t.Error("X-Request-ID header deve bater com context value")
	}
}

func TestRequestID_AcceptsValid(t *testing.T) {
	provided := "abc-1234-def0-9876-fedcba987654"
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", provided)
	rr := httptest.NewRecorder()
	RequestID(next).ServeHTTP(rr, req)

	if got != provided {
		t.Errorf("request_id do header não foi reusado: got %q want %q", got, provided)
	}
}

func TestRequestID_RejectsInvalid(t *testing.T) {
	cases := []string{
		"has spaces",
		"has/slashes",
		"../../../etc/passwd",
		strings.Repeat("a", 100),
		"semicolon;injection",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			var got string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = RequestIDFromContext(r.Context())
			})
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Request-ID", c)
			rr := httptest.NewRecorder()
			RequestID(next).ServeHTTP(rr, req)
			if got == c {
				t.Errorf("request_id inválido %q foi aceito", c)
			}
		})
	}
}

func TestWriteError_NoLeak(t *testing.T) {
	sensitiveErr := &testErr{msg: "pq: relation 'mez.admin_users' does not exist"}
	ctx := context.WithValue(context.Background(), reqIDKey{}, "test-12345")
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	WriteError(rr, req, http.StatusInternalServerError, CodeInternal, sensitiveErr)

	body := rr.Body.String()
	if strings.Contains(body, "pq: relation") {
		t.Errorf("body vazou info do erro: %q", body)
	}
	if strings.Contains(body, "mez.admin_users") {
		t.Errorf("body vazou nome de tabela: %q", body)
	}
	if !strings.Contains(body, `"request_id":"test-12345"`) {
		t.Errorf("body deve ter request_id, got: %q", body)
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status esperado 500, got %d", rr.Code)
	}
}

func TestWriteError_4xx(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	rr := httptest.NewRecorder()
	WriteError(rr, req, http.StatusNotFound, CodeNotFound, nil)
	body := rr.Body.String()
	if !strings.Contains(body, "not found") {
		t.Errorf("body esperado 'not found', got %q", body)
	}
	if rr.Code != http.StatusNotFound {
		t.Errorf("status esperado 404, got %d", rr.Code)
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
