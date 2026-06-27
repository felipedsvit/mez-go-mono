package port

import (
	"context"
	"errors"
	"fmt"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// CredentialsResolver resolve credenciais de canal como bytes JSON para um
// par (tenantID, channel). O Keyring (usecase/secrets) é a implementação de
// produção; adapters baseados em env var podem satisfazer a interface em
// dev/test.
//
// O conteúdo dos bytes é um JSON com os campos relevantes para o canal:
//
//	WABA:     {"phone_number_id":"...","access_token":"..."}
//	IG/MSG:   {"page_id":"...","access_token":"..."}
//	Telegram: {"bot_token":"..."}
//
// Factories do SenderRegistry fazem Unmarshal do retorno para a struct
// canal-específica.
type CredentialsResolver interface {
	ResolveCredentials(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) ([]byte, error)
}

// ErrCapabilityUnsupported is returned when a channel does not advertise a
// particular capability.
var ErrCapabilityUnsupported = errors.New("capability not supported by channel")

// Resolver is a simple in-memory capability resolver keyed by channel name.
// Adapters can register their capability sets at startup and look them up
// during message routing.
type Resolver struct {
	providers map[domain.Channel]CapabilitySet
}

// NewResolver returns an empty Resolver.
func NewResolver() *Resolver {
	return &Resolver{
		providers: make(map[domain.Channel]CapabilitySet),
	}
}

// Register associates caps with the given channel.
func (r *Resolver) Register(channel domain.Channel, caps CapabilitySet) {
	r.providers[channel] = caps
}

// Resolve returns the capability set registered for the given channel.
// The second return value is true if a set was found.
func (r *Resolver) Resolve(channel domain.Channel) (CapabilitySet, error) {
	caps, ok := r.providers[channel]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, channel)
	}
	return caps, nil
}

// Supports returns true if the given channel supports the given capability.
func (r *Resolver) Supports(channel domain.Channel, cap Capability) bool {
	caps, err := r.Resolve(channel)
	if err != nil {
		return false
	}
	return caps.Supports(cap)
}

// CapabilitiesWABA é a matriz oficial WABA (WhatsApp Cloud API):
// text, media, reactions, delete, templates, mark_read.
// NÃO tem edit, presence, typing, groups, persistent_menu, calls, payments.
func CapabilitiesWABA() CapabilitySet {
	return CapabilitySet{
		CapText:      true,
		CapMedia:     true,
		CapReactions: true,
		CapDelete:    true,
		CapTemplates: true,
		CapMarkRead:  true,
	}
}

// CapabilitiesInstagram é a matriz IG: text, media, reactions.
// NÃO tem edit, delete, presence, typing, groups, persistent_menu.
func CapabilitiesInstagram() CapabilitySet {
	return CapabilitySet{
		CapText:      true,
		CapMedia:     true,
		CapReactions: true,
	}
}

// CapabilitiesMessenger é a matriz MSG: text, media, reactions, mark_read,
// typing, groups, persistent_menu, presence(parcial via actions).
// NÃO tem edit, presence nativa, payments, calls.
func CapabilitiesMessenger() CapabilitySet {
	return CapabilitySet{
		CapText:           true,
		CapMedia:          true,
		CapReactions:      true,
		CapMarkRead:       true,
		CapTyping:         true,
		CapGroups:         true,
		CapPersistentMenu: true,
	}
}

// CapabilitiesTelegram é a matriz TG: text, media, reactions, edit, delete,
// typing, presence, groups, inline_keyboard, forum, payments, gifts, newsletter.
// Quase tudo que o Bot API permite.
func CapabilitiesTelegram() CapabilitySet {
	return CapabilitySet{
		CapText:           true,
		CapMedia:          true,
		CapReactions:      true,
		CapEdit:           true,
		CapDelete:         true,
		CapTyping:         true,
		CapPresence:       true,
		CapGroups:         true,
		CapInlineKeyboard: true,
		CapForum:          true,
		CapPayments:       true,
		CapGifts:          true,
		CapNewsletter:     true,
	}
}

// CapabilitiesWhatsMeow é a matriz informal (Phase 4): tudo que o
// protocolo permite. Marcado como não-implementado na Fase 3.
func CapabilitiesWhatsMeow() CapabilitySet {
	return CapabilitySet{
		CapText:         true,
		CapMedia:        true,
		CapReactions:    true,
		CapEdit:         true,
		CapDelete:       true,
		CapMarkRead:     true,
		CapTyping:       true,
		CapPresence:     true,
		CapGroups:       true,
		CapCalls:        true,
		CapDisappearing: true,
		CapBlocklist:    true,
	}
}
