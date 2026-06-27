package waba

import "github.com/felipedsvit/mez-go-mono/internal/core/port"

// wabaCapabilities é a matriz oficial WABA (WhatsApp Cloud API):
// text, media, reactions, delete, templates, mark_read.
// NÃO tem edit, presence, typing, groups, persistent_menu, calls, payments.
//
// Movida de port.CapabilitiesWABA em #120 — capability matrix é
// responsabilidade do adapter. Exportada para que wire.go construa o
// port.Resolver sem precisar instanciar um Adapter.
func WABACapabilities() port.CapabilitySet {
	return port.CapabilitySet{
		port.CapText:      true,
		port.CapMedia:     true,
		port.CapReactions: true,
		port.CapDelete:    true,
		port.CapTemplates: true,
		port.CapMarkRead:  true,
	}
}
