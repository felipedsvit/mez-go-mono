// Package port — sender.go define as portas outbound do mez-go-mono.
//
// Esta porta substitui a interface local em usecase/outbox/relay.go
// (Fase 2) por uma forma mais rica que suporta:
//
//   - Action enum (D6): reaction, edit, revoke, mark_read, typing, presence.
//   - OutboundRequest: tudo o que um adapter precisa para entregar a mensagem
//     ou executar a ação, sem precisar re-pescar no DB.
//   - Sender interface: Send + Capabilities + Channel.
//   - SenderRegistry per-tenant: factories lazy, cache com TTL.
//   - SenderFactory: lazy init por (tenant, channel).
package port

import (
	"context"
	"errors"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// Action é o tipo da ação outbound (D6). action vazio = "send" (mensagem nova).
type Action string

const (
	// ActionSend é o default; significa entrega de mensagem nova. Não precisa
	// ser setado explicitamente — SenderService.Send usa este por padrão.
	ActionSend Action = ""

	// ActionReaction adiciona/remove uma reação a uma mensagem entregue.
	// Requer metadata.target_provider_id e metadata.emoji.
	ActionReaction Action = "reaction"

	// ActionEdit edita uma mensagem entregue (somente canais que suportam).
	// Requer metadata.target_provider_id e metadata.new_body.
	ActionEdit Action = "edit"

	// ActionRevoke revoga (delete) uma mensagem entregue. Requer
	// metadata.target_provider_id.
	ActionRevoke Action = "revoke"

	// ActionMarkRead marca a mensagem do peer como lida. Requer
	// metadata.target_provider_id.
	ActionMarkRead Action = "mark_read"

	// ActionTyping indica que o agente está digitando. metadata.state ∈
	// {"on", "off"}.
	ActionTyping Action = "typing"

	// ActionPresence publica estado de presença (whatsmeow/canais informais).
	// metadata.state ∈ {"available", "unavailable"}.
	ActionPresence Action = "presence"
)

// OutboundRequest é o pedido canônico de entrega outbound. O relay
// reconstrói este struct a partir do outbox row e chama Sender.Send.
type OutboundRequest struct {
	// Identidade (sempre presente).
	TenantID       domain.TenantID
	Channel        domain.Channel
	MessageID      domain.MessageID
	ConversationID domain.ConversationID
	ContactID      domain.ContactID

	// Peer é o identificador do destinatário no canal (wa_id para WABA,
	// igsid para Instagram, psid para Messenger, chat_id para Telegram).
	PeerID string

	// Tipo de mensagem (text/image/audio/video/document/sticker/location/
	// button/template/reaction/system). Vazio quando Action != "".
	Type domain.MessageType

	// Conteúdo.
	Body     string
	Metadata map[string]any

	// Action roteia para doAction quando != "". Default (vazio) → SendMessage.
	Action Action

	// Alvo da ação (target_provider_id). Usado por Reaction/Edit/Revoke/MarkRead.
	TargetProviderID string

	// Reação.
	ReactionEmoji string

	// Edição.
	NewBody string

	// MarkRead/Presence/Typing state.
	State string
}

// Sender é a porta outbound. Cada adapter (WABA/IG/MSG/TG) implementa.
//
// Send é a única chamada. O relay processa cada outbox row através daqui.
// Action vazio = entrega de mensagem nova. Action != "" = ação de canal.
type Sender interface {
	// Send entrega uma mensagem ou executa uma ação. Retorna o provider
	// message ID (wamid, mid, etc.) em caso de sucesso.
	Send(ctx context.Context, req OutboundRequest) (providerMsgID string, err error)

	// Capabilities retorna o set de capabilities suportadas por este sender.
	Capabilities() CapabilitySet

	// Channel retorna o canal lógico deste sender.
	Channel() domain.Channel
}

// ErrSenderNotImplemented é o sentinel para "sender não registrado" (legado
// do NoopSender da Fase 2). Mantido para retro-compatibilidade.
var ErrSenderNotImplemented = errors.New("sender não registrado (fase 3)")

// SenderFactory cria um Sender para um tenant específico. Usado pela
// registry para lazy init.
type SenderFactory func(ctx context.Context, tenantID domain.TenantID) (Sender, error)

// SenderRegistry mantém a relação (channel) → factory. A registry cria
// Senders per-tenant sob demanda (lazy) e cachea por TTL.
//
// Concurrency: Get e Register são seguros para uso concorrente.
type SenderRegistry interface {
	// Get retorna o Sender para (tenant, channel). Cria on-demand na primeira
	// chamada. Retorna ErrSenderNotRegistered se o channel não tem factory.
	Get(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) (Sender, error)

	// Register associa uma factory a um channel. Sobrescreve a anterior.
	Register(channel domain.Channel, factory SenderFactory)

	// Channels retorna os channels com factory registrada.
	Channels() []domain.Channel

	// Health verifica a saúde de todos os channels registrados para um tenant.
	Health(ctx context.Context, tenantID domain.TenantID) map[domain.Channel]error
}
