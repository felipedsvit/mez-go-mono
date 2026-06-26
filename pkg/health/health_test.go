package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/felipedsvit/mez-go-mono/pkg/health"
)

func TestLiveHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	health.LiveHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestReadyHandler_AllOK(t *testing.T) {
	checker := health.NewChecker()
	checker.Add("test", func(ctx context.Context) error {
		return nil
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	health.ReadyHandler(checker)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestReadyHandler_Failing(t *testing.T) {
	checker := health.NewChecker()
	checker.Add("failing", func(ctx context.Context) error {
		return context.DeadlineExceeded
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	health.ReadyHandler(checker)(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}
