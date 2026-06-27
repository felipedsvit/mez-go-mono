package messenger

import "github.com/felipedsvit/mez-go-mono/internal/core/port"

// MessengerCapabilities é a matriz MSG: text, media, reactions, mark_read,
// typing, groups, persistent_menu, presence (parcial via actions).
// NÃO tem edit, presence nativa, payments, calls.
//
// Movida de port.CapabilitiesMessenger em #120. Exportada para wire.go.
func MessengerCapabilities() port.CapabilitySet {
	return port.CapabilitySet{
		port.CapText:           true,
		port.CapMedia:          true,
		port.CapReactions:      true,
		port.CapMarkRead:       true,
		port.CapTyping:         true,
		port.CapGroups:         true,
		port.CapPersistentMenu: true,
	}
}
