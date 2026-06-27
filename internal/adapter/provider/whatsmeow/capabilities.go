package whatsmeow

import "github.com/felipedsvit/mez-go-mono/internal/core/port"

// WhatsmeowCapabilities é a matriz informal (Fase 4): tudo que o
// protocolo permite. Marcado como não-implementado em algumas
// capabilities (envio retorna ErrNotImplemented — carryover).
//
// Movida de port.CapabilitiesWhatsMeow em #120. Exportada para wire.go.
func WhatsmeowCapabilities() port.CapabilitySet {
	return port.CapabilitySet{
		port.CapText:         true,
		port.CapMedia:        true,
		port.CapReactions:    true,
		port.CapEdit:         true,
		port.CapDelete:       true,
		port.CapMarkRead:     true,
		port.CapTyping:       true,
		port.CapPresence:     true,
		port.CapGroups:       true,
		port.CapCalls:        true,
		port.CapDisappearing: true,
		port.CapBlocklist:    true,
	}
}
