// Package whatsmeow é o adapter do canal WhatsApp informal (whatsmeow).
// Conexão persistente (1 client/tenant) com dispatcher bounded, AutoReconnect,
// QR pairing, e suporte a mídia transcodificada.
//
// Regra de ouro (C10 + decisão do pai): *whatsmeow.Client NÃO é thread-safe.
// Toda chamada ao client passa pelo Dispatcher per-tenant (single goroutine).
// Panic um tenant NÃO derruba o processo (recover por goroutine).
//
// Fase 4 — escopo do build verde: estrutura arquitetural completa (Manager,
// Dispatcher, Adapter, Actions, Reconnect, Identity, Events, Registry) com
// stub do *whatsmeow.Client. A integração real com a lib whatsmeow é drop-in
// via interface `Client` abaixo (production deployment requer session pareada
// e ffmpeg/cwebp no container; fora do escopo do CI verde).
package whatsmeow

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow/types"
)

// MessageID é um alias para types.MessageID (string) — usado nos
// signatures do Client para D6 actions (reaction/edit/revoke/mark_read).
type MessageID = types.MessageID

// Client é a interface mínima do *whatsmeow.Client que o adapter consome.
// Existe para isolar testes do SDK real e para permitir um stub funcional
// no build verde (Fase 4). A produção substitui o stub pelo whatsmeow.Client
// real sem mudar a forma do adapter.
type Client interface {
	// Connect abre o socket e (se necessário) inicia o flow de QR.
	Connect(ctx context.Context) error
	// Disconnect encerra o socket. Deve ser chamado no shutdown.
	Disconnect()

	// IsConnected retorna true se o socket está conectado.
	IsConnected() bool

	// SendMessage envia mensagem de texto e devolve o wamid.
	SendMessage(ctx context.Context, to types.JID, text string) (string, error)

	// SendImage envia imagem (bytes JPEG/PNG) com caption opcional.
	SendImage(ctx context.Context, to types.JID, data []byte, mime, caption string) (string, error)

	// SendAudio envia áudio (PTT/voice) — bytes já transcodificados em OGG/Opus.
	SendAudio(ctx context.Context, to types.JID, data []byte, mime string, ptt bool) (string, error)

	// SendDocument envia documento.
	SendDocument(ctx context.Context, to types.JID, data []byte, mime, filename, caption string) (string, error)

	// SendSticker envia sticker (WebP).
	SendSticker(ctx context.Context, to types.JID, data []byte) (string, error)

	// SendVideo envia vídeo.
	SendVideo(ctx context.Context, to types.JID, data []byte, mime, caption string) (string, error)

	// D6 actions (reação, edit, revoke, mark_read, typing, presence).
	SendReaction(ctx context.Context, chat types.JID, msgID types.MessageID, emoji string) error
	EditMessage(ctx context.Context, chat types.JID, msgID types.MessageID, newText string) (bool, error)
	RevokeMessage(ctx context.Context, chat types.JID, msgID types.MessageID) (bool, error)
	MarkRead(ctx context.Context, chat types.JID, msgIDs []types.MessageID, timestamp int64) error
	SendChatPresence(ctx context.Context, chat types.JID, state types.ChatPresence) error
	SendPresence(ctx context.Context, state types.Presence) error

	// RejectCall rejeita uma chamada recebida (mez é bot, não atende).
	RejectCall(ctx context.Context, from types.JID, callID string) error

	// GetQRChannel retorna o canal de QR pairing (nil se já conectado).
	GetQRChannel(ctx context.Context) (<-chan QRCodeEvent, error)

	// AddEventHandler registra o handler de eventos do whatsmeow.
	AddEventHandler(handler EventHandler) uint32

	// Logout sinaliza logout (forçado pelo celular ou app).
	Logout(ctx context.Context) error
}

// EventHandler é o tipo do handler registrado no AddEventHandler.
// Encapsulamos o tipo do whatsmeow em `any` para desacoplar (testes
// passam mock sem importar a lib).
type EventHandler func(evt any)

// QRCodeEvent é o par QR gerado pelo whatsmeow durante pareamento.
type QRCodeEvent struct {
	Code  string // string base64-friendly para o frontend
	Event string // "code" | "success" | "timeout" | "err-browser"
}

// ErrNotConnected é retornado quando uma chamada é feita sem sessão ativa.
var ErrNotConnected = errors.New("whatsmeow: client não conectado")

// ErrNotImplemented é retornado por ações deferred (calls, groups, etc).
var ErrNotImplemented = errors.New("whatsmeow: ação não implementada (carryover)")

// ParseJID converte string JID ("5511999999999@s.whatsapp.net") em types.JID.
// Helper usado por Manager/Adapter; funciona com a lib real e o stub.
func ParseJID(s string) (types.JID, error) {
	jid, err := types.ParseJID(s)
	if err != nil {
		return types.JID{}, err
	}
	return jid, nil
}
