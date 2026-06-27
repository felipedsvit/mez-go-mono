package adminweb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
)

// fakeConvRepo implementa ConversationLister para testes.
type fakeConvRepo struct {
	convs []domain.Conversation
}

func (f *fakeConvRepo) ListByTenant(_ context.Context, _ domain.TenantID) ([]domain.Conversation, error) {
	return f.convs, nil
}

// fakeMsgRepo implementa MessageLister.
type fakeMsgRepo struct {
	msgs []domain.Message
}

func (f *fakeMsgRepo) ListByConversation(_ context.Context, _ domain.ConversationID) ([]domain.Message, error) {
	return f.msgs, nil
}

// fakeSender implementam SenderService.
type fakeSender struct {
	called int
}

func (f *fakeSender) Send(_ context.Context, _ ucmessaging.SendRequest) (domain.Message, error) {
	f.called++
	return domain.Message{ID: "m1", Status: domain.MessageStatusNotified}, nil
}

func newTestRoutes() (*Routes, *fakeSender) {
	log := zerolog.Nop()
	tenant := "tenant-1"
	convRepo := &fakeConvRepo{convs: []domain.Conversation{
		{ID: "c1", Channel: domain.ChannelWABA, Status: domain.ConvStatusOpen},
	}}
	msgRepo := &fakeMsgRepo{msgs: []domain.Message{
		{ID: "m1", Body: "olá", Direction: domain.DirectionInbound},
	}}
	sender := &fakeSender{}
	tenantCtx := func(_ context.Context) (domain.TenantID, bool) { return domain.TenantID(tenant), true }
	app := NewAppHandlers(convRepo, msgRepo, sender, tenantCtx, log)
	admin := NewAdminHandlers(log, nil)
	return &Routes{App: app, Admin: admin, Log: log}, sender
}

func TestRoutes_Inbox(t *testing.T) {
	routes, _ := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/conversations", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRoutes_Thread(t *testing.T) {
	routes, _ := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/conversations/c1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "c1")
	req = req.WithContext(req.Context())
	req = req.WithContext(contextWithChiCtx(req.Context(), rctx))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRoutes_SendMessage(t *testing.T) {
	routes, sender := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	form := "body=olá&channel=waba&contact_id=co1"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/conversations/c1/messages",
		strReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "c1")
	req = req.WithContext(contextWithChiCtx(req.Context(), rctx))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (SeeOther)", rec.Code)
	}
	if sender.called != 1 {
		t.Errorf("sender called %d, want 1", sender.called)
	}
}

func TestRoutes_Services(t *testing.T) {
	routes, _ := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/services", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRoutes_Channels(t *testing.T) {
	routes, _ := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/tenants/t1/channels", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "t1")
	req = req.WithContext(contextWithChiCtx(req.Context(), rctx))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRoutes_QRCode(t *testing.T) {
	routes, _ := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/qrcode", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRoutes_Agents(t *testing.T) {
	routes, _ := newTestRoutes()
	r := chi.NewRouter()
	routes.Register(r)

	// GET
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/tenants/t1/agents", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "t1")
	req = req.WithContext(contextWithChiCtx(req.Context(), rctx))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET status = %d, want 200", rec.Code)
	}

	// POST
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/admin/tenants/t1/agents",
		strReader("email=agente@example.com"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "t1")
	req2 = req2.WithContext(contextWithChiCtx(req2.Context(), rctx2))
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusSeeOther {
		t.Errorf("POST status = %d, want 303", rec2.Code)
	}
}

// helpers

type stringReaderImpl struct {
	s string
	i int
}

func (r *stringReaderImpl) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func strReader(s string) *stringReaderImpl { return &stringReaderImpl{s: s} }

func contextWithChiCtx(ctx context.Context, rctx *chi.Context) context.Context {
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

// jsonTestPayload garante uso do import (removível).
var _ = json.Marshal
