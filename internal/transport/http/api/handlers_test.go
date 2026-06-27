package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

type fakeConvRepo struct {
	mu    sync.Mutex
	store map[string]domain.Conversation
}

func newFakeConvRepo() *fakeConvRepo {
	return &fakeConvRepo{store: make(map[string]domain.Conversation)}
}
func (f *fakeConvRepo) Upsert(_ context.Context, c *domain.Conversation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[string(c.ID)] = *c
	return nil
}
func (f *fakeConvRepo) ListByTenant(_ context.Context, _ domain.TenantID) ([]domain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Conversation, 0, len(f.store))
	for _, c := range f.store {
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeConvRepo) Get(_ context.Context, id domain.ConversationID) (*domain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.store[string(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &c, nil
}
func (f *fakeConvRepo) UpdateStatus(_ context.Context, id domain.ConversationID, s domain.ConversationStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.store[string(id)]
	if !ok {
		return errors.New("not found")
	}
	c.Status = s
	f.store[string(id)] = c
	return nil
}

type fakeMsgRepo struct{}

func (f *fakeMsgRepo) Insert(_ context.Context, _ *domain.Message) error { return nil }
func (f *fakeMsgRepo) ListByConversation(_ context.Context, _ domain.ConversationID) ([]domain.Message, error) {
	return nil, nil
}
func (f *fakeMsgRepo) Get(_ context.Context, _ domain.MessageID) (*domain.Message, error) {
	return nil, errors.New("not found")
}
func (f *fakeMsgRepo) UpdateStatus(_ context.Context, _ domain.MessageID, _ domain.MessageStatus) error {
	return nil
}
func (f *fakeMsgRepo) SelectUnroutedMessages(_ context.Context, _ int) ([]domain.Message, error) {
	return nil, nil
}
func (f *fakeMsgRepo) MarkRouted(_ context.Context, _ domain.MessageID) error { return nil }

type fakeTenantRepo struct{}

func (f *fakeTenantRepo) List(_ context.Context) ([]domain.Tenant, error) { return nil, nil }
func (f *fakeTenantRepo) Get(_ context.Context, _ domain.TenantID) (*domain.Tenant, error) {
	return nil, errors.New("not found")
}
func (f *fakeTenantRepo) Create(_ context.Context, _ *domain.Tenant) error  { return nil }
func (f *fakeTenantRepo) Update(_ context.Context, _ *domain.Tenant) error  { return nil }
func (f *fakeTenantRepo) Delete(_ context.Context, _ domain.TenantID) error { return nil }

func newListSvc(convRepo port.ConversationRepo, msgRepo port.MessageRepo) *ucmessaging.ListService {
	return ucmessaging.NewListService(convRepo, msgRepo, zerolog.Nop())
}

func newRouter(h *Handlers) *chi.Mux {
	r := chi.NewRouter()
	h.Register(r)
	return r
}

func TestListConversations_RequiresTenant(t *testing.T) {
	h := New(zerolog.Nop(), newFakeConvRepo(), &fakeMsgRepo{}, &fakeTenantRepo{}, nil, nil, nil, nil, nil)
	r := newRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/conversations", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestListConversations_OK(t *testing.T) {
	convRepo := newFakeConvRepo()
	convRepo.store["c1"] = domain.Conversation{
		ID:        "c1",
		Channel:   domain.ChannelWABA,
		ContactID: "co1",
		Status:    domain.ConvStatusOpen,
	}

	listSvc := newListSvc(convRepo, &fakeMsgRepo{})
	h := New(zerolog.Nop(), convRepo, &fakeMsgRepo{}, &fakeTenantRepo{}, nil, listSvc, nil, nil, nil)
	r := newRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/conversations", nil)
	req = req.WithContext(ContextWithTenant(req.Context(), "tenant-1"))
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if total, ok := body["total"].(float64); !ok || total != 1 {
		t.Errorf("total = %v, want 1", body["total"])
	}
}

func TestPostMessage_RequiresFields(t *testing.T) {
	// Fase 3: POST /api/messages é real; sem sender service retorna 503,
	// com body inválido retorna 400.
	h := New(zerolog.Nop(), newFakeConvRepo(), &fakeMsgRepo{}, &fakeTenantRepo{}, nil, nil, nil, nil, nil)
	r := newRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithTenant(req.Context(), "tenant-1"))
	r.ServeHTTP(rec, req)

	// sem sender: 503; sem campos: 400 — sem body válido: 400 ou 503.
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 400 or 503", rec.Code)
	}
}

func TestListMessages_RequiresConversationID(t *testing.T) {
	h := New(zerolog.Nop(), newFakeConvRepo(), &fakeMsgRepo{}, &fakeTenantRepo{}, nil, nil, nil, nil, nil)
	r := newRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages", nil)
	req = req.WithContext(ContextWithTenant(req.Context(), "tenant-1"))
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestConversationResolve_UpdatesStatus(t *testing.T) {
	convRepo := newFakeConvRepo()
	convRepo.store["c1"] = domain.Conversation{
		ID: "c1", Channel: domain.ChannelWABA, ContactID: "co1", Status: domain.ConvStatusOpen,
	}

	listSvc := newListSvc(convRepo, &fakeMsgRepo{})
	h := New(zerolog.Nop(), convRepo, &fakeMsgRepo{}, &fakeTenantRepo{}, nil, listSvc, nil, nil, nil)
	r := newRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/conversations/c1/resolve", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "c1")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ContextWithTenant(ctx, "tenant-1"))
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (got %s)", rec.Code, http.StatusText(rec.Code))
	}
	if convRepo.store["c1"].Status != domain.ConvStatusResolved {
		t.Errorf("status = %q, want resolved", convRepo.store["c1"].Status)
	}
}

func TestChannelHealth_RequiresTenant(t *testing.T) {
	// Fase 3: health agora exige tenant no contexto.
	h := New(zerolog.Nop(), newFakeConvRepo(), &fakeMsgRepo{}, &fakeTenantRepo{}, nil, nil, nil, nil, nil)
	r := newRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/channels/waba/health", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "waba")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (no tenant)", rec.Code)
	}
}
