package telegram_bot

import "github.com/felipedsvit/mez-go-mono/internal/core/port"

// TelegramCapabilities é a matriz TG: text, media, reactions, edit, delete,
// typing, presence, groups, inline_keyboard, forum, gifts, newsletter.
// Pagamentos é gated por WithPayments(true) (default off).
//
// Movida de port.CapabilitiesTelegram em #120. Exportada para wire.go.
func TelegramCapabilities() port.CapabilitySet {
	return port.CapabilitySet{
		port.CapText:           true,
		port.CapMedia:          true,
		port.CapReactions:      true,
		port.CapEdit:           true,
		port.CapDelete:         true,
		port.CapTyping:         true,
		port.CapPresence:       true,
		port.CapGroups:         true,
		port.CapInlineKeyboard: true,
		port.CapForum:          true,
		port.CapGifts:          true,
		port.CapNewsletter:     true,
	}
}
