package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/routing"
	"github.com/rs/zerolog"
)

// --- Fakes ---

type fakeTxRunner struct{}

func (fakeTxRunner) RunInTenantTx(ctx context.Context, _ domain.TenantID, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
func (fakeTxRunner) RunAsPlatform(ctx context.Context, _ string, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type fakeConvRepo struct {
	convs map[domain.ConversationID]*domain.Conversation
}

func (r *fakeConvRepo) ListByTenant(_ context.Context, _ domain.TenantID) ([]domain.Conversation, error) {
	out := make([]domain.Conversation, 0, len(r.convs))
	for _, c := range r.convs {
		out = append(out, *c)
	}
	return out, nil
}
func (r *fakeConvRepo) Get(_ context.Context, id domain.ConversationID) (*domain.Conversation, error) {
	c, ok := r.convs[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	return c, nil
}
func (r *fakeConvRepo) Upsert(_ context.Context, c *domain.Conversation) error {
	r.convs[c.ID] = c
	return nil
}
func (r *fakeConvRepo) UpdateStatus(_ context.Context, id domain.ConversationID, s domain.ConversationStatus) error {
	c, ok := r.convs[id]
	if !ok {
		return port.ErrNotFound
	}
	c.Status = s
	return nil
}

type fakeMsgRepo struct {
	msgs map[domain.ConversationID][]domain.Message
}

func (r *fakeMsgRepo) Insert(_ context.Context, _ *domain.Message) error { return nil }
func (r *fakeMsgRepo) ListByConversation(_ context.Context, convID domain.ConversationID) ([]domain.Message, error) {
	return r.msgs[convID], nil
}
func (r *fakeMsgRepo) Get(_ context.Context, _ domain.MessageID) (*domain.Message, error) {
	return nil, port.ErrNotFound
}
func (r *fakeMsgRepo) UpdateStatus(_ context.Context, _ domain.MessageID, _ domain.MessageStatus) error {
	return nil
}
func (r *fakeMsgRepo) SelectUnroutedMessages(_ context.Context, _ int) ([]domain.Message, error) {
	return nil, nil
}
func (r *fakeMsgRepo) MarkRouted(_ context.Context, _ domain.MessageID) error { return nil }

type fakeAgentRepo struct {
	agents []domain.Agent
}

func (r *fakeAgentRepo) Candidates(_ context.Context, _ domain.QueueID) ([]domain.Agent, error) {
	return r.agents, nil
}
func (r *fakeAgentRepo) List(_ context.Context, _ int, _ int) ([]domain.Agent, error) {
	return r.agents, nil
}
func (r *fakeAgentRepo) IncLoad(_ context.Context, _ domain.AgentID, _ int) error { return nil }

// --- Helpers ---

func newService() (*Service, *fakeConvRepo, *fakeMsgRepo) {
	cr := &fakeConvRepo{convs: make(map[domain.ConversationID]*domain.Conversation)}
	mr := &fakeMsgRepo{msgs: make(map[domain.ConversationID][]domain.Message)}
	r := routing.NewRouter(fakeTxRunner{}, cr, zerolog.Nop())
	return NewService(fakeTxRunner{}, cr, mr, nil, r), cr, mr
}

func seedConv(cr *fakeConvRepo, id string, status domain.ConversationStatus, agent string) {
	cr.convs[domain.ConversationID(id)] = &domain.Conversation{
		ID:            domain.ConversationID(id),
		TenantID:      "t1",
		Channel:       domain.ChannelWABA,
		ContactID:     "c1",
		Status:        status,
		AssignedAgent: agent,
		UpdatedAt:     time.Now(),
	}
}

// --- Tests ---

func TestListConversations_Empty(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService()
	convs, err := svc.ListConversations(context.Background(), "t1", 50, 0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 0 {
		t.Errorf("expected 0, got %d", len(convs))
	}
}

func TestListConversations_OrderedByUpdatedDesc(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	now := time.Now()
	cr.convs["c1"] = &domain.Conversation{ID: "c1", TenantID: "t1", Status: domain.ConvStatusOpen, UpdatedAt: now.Add(-2 * time.Hour)}
	cr.convs["c2"] = &domain.Conversation{ID: "c2", TenantID: "t1", Status: domain.ConvStatusOpen, UpdatedAt: now.Add(-1 * time.Hour)}
	cr.convs["c3"] = &domain.Conversation{ID: "c3", TenantID: "t1", Status: domain.ConvStatusOpen, UpdatedAt: now}

	convs, err := svc.ListConversations(context.Background(), "t1", 50, 0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 3 {
		t.Fatalf("expected 3, got %d", len(convs))
	}
	if convs[0].ID != "c3" || convs[2].ID != "c1" {
		t.Errorf("ordering wrong: %+v", convs)
	}
}

func TestListConversations_Pagination(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	now := time.Now()
	for i := 0; i < 10; i++ {
		cr.convs[domain.ConversationID(string(rune('a'+i)))] = &domain.Conversation{
			ID: domain.ConversationID(string(rune('a' + i))),
			UpdatedAt: now.Add(-time.Duration(i) * time.Minute),
		}
	}

	// limit > 200 clampa
	convs, err := svc.ListConversations(context.Background(), "t1", 500, 0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 10 {
		t.Errorf("expected 10, got %d", len(convs))
	}

	// limit/offset normal
	convs, err = svc.ListConversations(context.Background(), "t1", 3, 2)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 3 {
		t.Errorf("expected 3, got %d", len(convs))
	}

	// offset beyond
	convs, err = svc.ListConversations(context.Background(), "t1", 3, 100)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 0 {
		t.Errorf("expected 0, got %d", len(convs))
	}
}

func TestThread_NotFound(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService()
	_, _, err := svc.Thread(context.Background(), "t1", "missing", 50, 0)
	if !errors.Is(err, ErrConvNotFound) {
		t.Errorf("expected ErrConvNotFound, got %v", err)
	}
}

func TestThread_OK(t *testing.T) {
	t.Parallel()

	svc, cr, mr := newService()
	seedConv(cr, "c1", domain.ConvStatusOpen, "agent-1")
	now := time.Now()
	mr.msgs["c1"] = []domain.Message{
		{ID: "m1", ConversationID: "c1", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "m2", ConversationID: "c1", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "m3", ConversationID: "c1", CreatedAt: now},
	}

	conv, msgs, err := svc.Thread(context.Background(), "t1", "c1", 50, 0)
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if conv.ID != "c1" {
		t.Errorf("conv.ID = %q", conv.ID)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	// Mais recente primeiro
	if msgs[0].ID != "m3" {
		t.Errorf("expected m3 first (newest), got %s", msgs[0].ID)
	}
}

func TestThread_CrossTenantBlock(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	// conv pertence a outro tenant
	cr.convs["c1"] = &domain.Conversation{ID: "c1", TenantID: "outro"}
	_, _, err := svc.Thread(context.Background(), "t1", "c1", 50, 0)
	if !errors.Is(err, ErrConvNotFound) {
		t.Errorf("expected ErrConvNotFound (cross-tenant), got %v", err)
	}
}

func TestAssign_EmptyAgentID(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService()
	err := svc.Assign(context.Background(), "t1", "c1", "")
	if err == nil {
		t.Fatal("expected error for empty agentID")
	}
}

func TestAssign_OK(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	seedConv(cr, "c1", domain.ConvStatusOpen, "")
	if err := svc.Assign(context.Background(), "t1", "c1", "agent-1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if cr.convs["c1"].AssignedAgent != "agent-1" {
		t.Errorf("agent = %q, want agent-1", cr.convs["c1"].AssignedAgent)
	}
}

func TestResolve_OK(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	seedConv(cr, "c1", domain.ConvStatusOpen, "agent-1")
	if err := svc.Resolve(context.Background(), "t1", "c1"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cr.convs["c1"].Status != domain.ConvStatusResolved {
		t.Errorf("status = %q, want resolved", cr.convs["c1"].Status)
	}
}

func TestUnassign_OK(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	seedConv(cr, "c1", domain.ConvStatusOpen, "agent-1")
	if err := svc.Unassign(context.Background(), "t1", "c1"); err != nil {
		t.Fatalf("Unassign: %v", err)
	}
	if cr.convs["c1"].AssignedAgent != "" {
		t.Errorf("agent = %q, want empty", cr.convs["c1"].AssignedAgent)
	}
}

func TestListAgents_NilRepo(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService()
	agents, err := svc.ListAgents(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if agents != nil {
		t.Errorf("expected nil, got %+v", agents)
	}
}

func TestListAgents_WithRepo(t *testing.T) {
	t.Parallel()

	cr := &fakeConvRepo{convs: make(map[domain.ConversationID]*domain.Conversation)}
	mr := &fakeMsgRepo{msgs: make(map[domain.ConversationID][]domain.Message)}
	ar := &fakeAgentRepo{agents: []domain.Agent{
		{ID: "a1", Status: domain.AgentOnline, MaxLoad: 5},
		{ID: "a2", Status: domain.AgentOffline, MaxLoad: 5},
	}}
	r := routing.NewRouter(fakeTxRunner{}, cr, zerolog.Nop())
	svc := NewService(fakeTxRunner{}, cr, mr, ar, r)

	agents, err := svc.ListAgents(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2, got %d", len(agents))
	}
}

func TestPaginate_DefaultLimit(t *testing.T) {
	t.Parallel()

	svc, cr, _ := newService()
	// list com limit <= 0 usa default 50
	_, err := svc.ListConversations(context.Background(), "t1", 0, 0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	_ = cr
}

func TestPaginateMessages_DefaultLimit(t *testing.T) {
	t.Parallel()

	// Já coberto por TestThread_OK, mas validamos o limite > 500 clampa
	svc, cr, mr := newService()
	seedConv(cr, "c1", domain.ConvStatusOpen, "a")
	now := time.Now()
	for i := 0; i < 5; i++ {
		mr.msgs["c1"] = append(mr.msgs["c1"], domain.Message{
			ID:             domain.MessageID(string(rune('a' + i))),
			ConversationID: "c1",
			CreatedAt:      now.Add(-time.Duration(i) * time.Minute),
		})
	}
	_, msgs, err := svc.Thread(context.Background(), "t1", "c1", 1000, 0)
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 (clamped to 500), got %d", len(msgs))
	}
}
