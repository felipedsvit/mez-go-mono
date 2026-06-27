// Package whatsmeow — identity.go: DeviceIdentity (E1 anti-ban).
//
// Mascara o OS/browser no payload de conexão (anti-ban E1,
// mez-go/internal/adapter/provider/whatsmeow/identity.go). Default:
// Chrome + "Mac OS"; configurável via MEZ_WHATSmeow_IDENTITY.
package whatsmeow

import (
	"context"

	waCompanionReg "go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// DeviceIdentity emula um navegador comercial no payload de conexão.
type DeviceIdentity struct {
	OSName   string
	Platform waCompanionReg.DeviceProps_PlatformType
}

// IdentityFromConfig traduz o kind ("chrome"|"edge"|"none") para um
// DeviceIdentity. kind vazio devolve (nil): mantém a firma whatsmeow padrão.
func IdentityFromConfig(kind, osName string) *DeviceIdentity {
	if osName == "" {
		osName = "Mac OS"
	}
	switch kind {
	case "", "none":
		return nil
	case "edge":
		return &DeviceIdentity{OSName: osName, Platform: waCompanionReg.DeviceProps_EDGE}
	default: // "chrome" e qualquer valor desconhecido caem no mais comum.
		return &DeviceIdentity{OSName: osName, Platform: waCompanionReg.DeviceProps_CHROME}
	}
}

// Apply ajusta os metadados globais do store antes do Connect. Idempotente.
func (d *DeviceIdentity) Apply(logger waLog.Logger) {
	if d == nil {
		return
	}
	name := d.OSName
	if name == "" {
		name = "Mac OS"
	}
	store.SetOSInfo(name, store.GetWAVersion())
	store.DeviceProps.PlatformType = d.Platform.Enum()
	if logger != nil {
		_ = context.Background()
	}
}
