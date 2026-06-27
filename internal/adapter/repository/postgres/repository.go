package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// ---- TenantRepo -------------------------------------------------------

type TenantRepo struct {
	pool *pgxpool.Pool
}

func NewTenantRepo(pool *pgxpool.Pool) *TenantRepo {
	return &TenantRepo{pool: pool}
}

func (r *TenantRepo) List(ctx context.Context) ([]domain.Tenant, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT id, name, slug, active, created_at, updated_at FROM tenants ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Active, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tenants rows: %w", err)
	}
	return tenants, nil
}

func (r *TenantRepo) Get(ctx context.Context, id domain.TenantID) (*domain.Tenant, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	var t domain.Tenant
	err := q.QueryRow(ctx,
		`SELECT id, name, slug, active, created_at, updated_at FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.Active, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return &t, nil
}

func (r *TenantRepo) Create(ctx context.Context, t *domain.Tenant) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO tenants (id, name, slug, active, created_at, updated_at) VALUES ($1, $2, $3, $4, NOW(), NOW())`,
		t.ID, t.Name, t.Slug, t.Active,
	)
	if err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	return nil
}

func (r *TenantRepo) Update(ctx context.Context, t *domain.Tenant) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`UPDATE tenants SET name=$1, slug=$2, active=$3, updated_at=NOW() WHERE id=$4`,
		t.Name, t.Slug, t.Active, t.ID,
	)
	if err != nil {
		return fmt.Errorf("update tenant: %w", err)
	}
	return nil
}

func (r *TenantRepo) Delete(ctx context.Context, id domain.TenantID) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	return nil
}

// ---- ContactRepo -------------------------------------------------------

type ContactRepo struct {
	pool *pgxpool.Pool
}

func NewContactRepo(pool *pgxpool.Pool) *ContactRepo {
	return &ContactRepo{pool: pool}
}

func (r *ContactRepo) ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]domain.Contact, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, tenant_id, channel, provider_id, name, avatar_url, created_at, updated_at
		 FROM contacts WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []domain.Contact
	for rows.Next() {
		var c domain.Contact
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Channel, &c.ProviderID, &c.Name, &c.AvatarURL, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list contacts rows: %w", err)
	}
	return contacts, nil
}

func (r *ContactRepo) Get(ctx context.Context, id domain.ContactID) (*domain.Contact, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	var c domain.Contact
	err := q.QueryRow(ctx,
		`SELECT id, tenant_id, channel, provider_id, name, avatar_url, created_at, updated_at
		 FROM contacts WHERE id = $1`, id,
	).Scan(&c.ID, &c.TenantID, &c.Channel, &c.ProviderID, &c.Name, &c.AvatarURL, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("get contact: %w", err)
	}
	return &c, nil
}

func (r *ContactRepo) Upsert(ctx context.Context, c *domain.Contact) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO contacts (id, tenant_id, channel, provider_id, name, avatar_url, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,NOW(),NOW())
		 ON CONFLICT (tenant_id, channel, provider_id) DO UPDATE SET name=$5, avatar_url=$6, updated_at=NOW()`,
		c.ID, c.TenantID, c.Channel, c.ProviderID, c.Name, c.AvatarURL,
	)
	if err != nil {
		return fmt.Errorf("upsert contact: %w", err)
	}
	return nil
}

// ---- ConversationRepo --------------------------------------------------

type ConversationRepo struct {
	pool *pgxpool.Pool
}

func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{pool: pool}
}

func (r *ConversationRepo) ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]domain.Conversation, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, tenant_id, channel, contact_id, status, external_id, assigned_agent, created_at, updated_at
		 FROM conversations WHERE tenant_id = $1 ORDER BY updated_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var convs []domain.Conversation
	for rows.Next() {
		var c domain.Conversation
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Channel, &c.ContactID, &c.Status, &c.ExternalID, &c.AssignedAgent, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		convs = append(convs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list conversations rows: %w", err)
	}
	return convs, nil
}

func (r *ConversationRepo) Get(ctx context.Context, id domain.ConversationID) (*domain.Conversation, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	var c domain.Conversation
	err := q.QueryRow(ctx,
		`SELECT id, tenant_id, channel, contact_id, status, external_id, assigned_agent, created_at, updated_at
		 FROM conversations WHERE id = $1`, id,
	).Scan(&c.ID, &c.TenantID, &c.Channel, &c.ContactID, &c.Status, &c.ExternalID, &c.AssignedAgent, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return &c, nil
}

func (r *ConversationRepo) Upsert(ctx context.Context, c *domain.Conversation) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO conversations (id, tenant_id, channel, contact_id, status, external_id, assigned_agent, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,NOW(),NOW())
		 ON CONFLICT (tenant_id, external_id) WHERE external_id IS NOT NULL
		 DO UPDATE SET status=$5, assigned_agent=$7, updated_at=NOW()`,
		c.ID, c.TenantID, c.Channel, c.ContactID, c.Status, c.ExternalID, c.AssignedAgent,
	)
	if err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}
	return nil
}

func (r *ConversationRepo) UpdateStatus(ctx context.Context, id domain.ConversationID, status domain.ConversationStatus) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`UPDATE conversations SET status=$1, updated_at=NOW() WHERE id=$2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update conversation status: %w", err)
	}
	return nil
}

// ---- MessageRepo -------------------------------------------------------

type MessageRepo struct {
	pool *pgxpool.Pool
}

func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo {
	return &MessageRepo{pool: pool}
}

func (r *MessageRepo) Insert(ctx context.Context, m *domain.Message) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO messages (id, tenant_id, channel, conversation_id, contact_id, direction, type, status, body, provider_msg_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
		 ON CONFLICT (tenant_id, provider_msg_id) WHERE provider_msg_id IS NOT NULL DO NOTHING`,
		m.ID, m.TenantID, m.Channel, m.ConversationID, m.ContactID, m.Direction, m.Type, m.Status, m.Body, m.ProviderMsgID,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func (r *MessageRepo) Get(ctx context.Context, id domain.MessageID) (*domain.Message, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	var m domain.Message
	err := q.QueryRow(ctx,
		`SELECT id, tenant_id, channel, conversation_id, contact_id, direction, type, status, body, provider_msg_id, created_at, updated_at
		 FROM messages WHERE id = $1`, id,
	).Scan(&m.ID, &m.TenantID, &m.Channel, &m.ConversationID, &m.ContactID, &m.Direction, &m.Type, &m.Status, &m.Body, &m.ProviderMsgID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("get message: %w", err)
	}
	return &m, nil
}

func (r *MessageRepo) ListByConversation(ctx context.Context, conversationID domain.ConversationID) ([]domain.Message, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, tenant_id, channel, conversation_id, contact_id, direction, type, status, body, provider_msg_id, created_at, updated_at
		 FROM messages WHERE conversation_id = $1 ORDER BY created_at`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Channel, &m.ConversationID, &m.ContactID, &m.Direction, &m.Type, &m.Status, &m.Body, &m.ProviderMsgID, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages rows: %w", err)
	}
	return msgs, nil
}

func (r *MessageRepo) UpdateStatus(ctx context.Context, id domain.MessageID, status domain.MessageStatus) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	_, err := q.Exec(ctx,
		`UPDATE messages SET status=$1, updated_at=NOW() WHERE id=$2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	return nil
}

func (r *MessageRepo) SelectUnroutedMessages(ctx context.Context, batchSize int) ([]domain.Message, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, tenant_id, channel, conversation_id, contact_id, direction, type, status, body, provider_msg_id, created_at, updated_at
		 FROM messages WHERE status = 'received'
		 ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select unrouted: %w", err)
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Channel, &m.ConversationID, &m.ContactID, &m.Direction, &m.Type, &m.Status, &m.Body, &m.ProviderMsgID, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select unrouted rows: %w", err)
	}
	return msgs, nil
}

func (r *MessageRepo) MarkRouted(ctx context.Context, id domain.MessageID) error {
	return r.UpdateStatus(ctx, id, domain.MessageStatusRouted)
}
