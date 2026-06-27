package instagram

import "github.com/felipedsvit/mez-go-mono/internal/core/port"

// InstagramCapabilities é a matriz IG: text, media, reactions.
// NÃO tem edit, delete, presence, typing, groups, persistent_menu.
//
// Movida de port.CapabilitiesInstagram em #120. Exportada para wire.go.
func InstagramCapabilities() port.CapabilitySet {
	return port.CapabilitySet{
		port.CapText:      true,
		port.CapMedia:     true,
		port.CapReactions: true,
	}
}
