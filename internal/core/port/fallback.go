// Package port — fallback.go: degradação graciosa de mensagens quando o
// canal não suporta a capability requerida (D7 — capability negotiation).
//
// Exemplo clássico: canal não suporta CapMedia mas suporta CapText. Em vez
// de falhar opacamente no provider, degradamos a mensagem para texto
// (com URL/caption).
package port

import (
	"fmt"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// Message é um alias para evitar o import cíclico em assinatura local.
// Mantido apenas para clareza: sempre usamos domain.Message.
type Message = domain.Message

// requiredCapabilityForType mapeia MessageType → Capability mínima requerida.
func requiredCapabilityForType(t domain.MessageType) Capability {
	switch t {
	case domain.MessageTypeText:
		return CapText
	case domain.MessageTypeImage, domain.MessageTypeAudio, domain.MessageTypeVideo,
		domain.MessageTypeDocument, domain.MessageTypeSticker, domain.MessageTypeLocation,
		domain.MessageTypeButton, domain.MessageTypeTemplate:
		return CapMedia
	default:
		return CapText
	}
}

// ResolveMessage aplica a negociação de capability para uma mensagem.
//
// Comportamento:
//   - Se o canal suporta a capability requerida, retorna a mensagem inalterada.
//   - Se o canal não suporta mas tem fallback (media→text), retorna a
//     mensagem degradada + degraded=true.
//   - Caso contrário, retorna ErrCapabilityUnsupported.
//
// Esta função NÃO muta a mensagem original.
func (r *Resolver) ResolveMessage(channel domain.Channel, msg domain.Message) (Message, bool, error) {
	caps, err := r.Resolve(channel)
	if err != nil {
		return msg, false, err
	}

	need := requiredCapabilityForType(msg.Type)
	if caps.Supports(need) {
		return msg, false, nil
	}

	// Fallback media → text.
	if need == CapMedia && caps.Supports(CapText) {
		return degradeMediaToText(msg), true, nil
	}

	return msg, false, fmt.Errorf("%w: canal=%s capability=%s", ErrCapabilityUnsupported, channel, need)
}

// degradeMediaToText converte uma mensagem de mídia em texto, preservando
// URL/caption quando disponíveis.
func degradeMediaToText(msg domain.Message) Message {
	msg.Type = domain.MessageTypeText
	if url, ok := msg.Metadata["url"].(string); ok && url != "" {
		if msg.Body != "" {
			msg.Body = msg.Body + "\n" + url
		} else {
			msg.Body = url
		}
	}
	return msg
}

// RequiredCapabilityForType exporta o mapeamento MessageType → Capability
// para uso em outros pacotes (ex.: validação na API).
func RequiredCapabilityForType(t domain.MessageType) Capability {
	return requiredCapabilityForType(t)
}
