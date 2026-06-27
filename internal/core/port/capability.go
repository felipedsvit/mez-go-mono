package port

import "github.com/felipedsvit/mez-go-mono/internal/core/domain"

// Capability is a coarse, channel-independent feature such as "text" or "media".
type Capability string

const (
	CapText           Capability = "text"
	CapMedia          Capability = "media"
	CapReactions      Capability = "reactions"
	CapEdit           Capability = "edit"
	CapDelete         Capability = "delete"
	CapTemplates      Capability = "templates"
	CapGroups         Capability = "groups"
	CapPresence       Capability = "presence"
	CapMarkRead       Capability = "mark_read"
	CapHandover       Capability = "handover"
	CapPersistentMenu Capability = "persistent_menu"
	CapStoryReply     Capability = "story_reply"
	CapCalls          Capability = "calls"
	CapDisappearing   Capability = "disappearing"
	CapBlocklist      Capability = "blocklist"
	CapInlineKeyboard Capability = "inline_keyboard"
	CapTyping         Capability = "typing"
	CapNewsletter     Capability = "newsletter"
	CapForum          Capability = "forum"
	CapPayments       Capability = "payments"
	CapGifts          Capability = "gifts"
)

// CapabilitySet represents the set of capabilities supported by a channel.
// Implementations should be safe for concurrent reads.
type CapabilitySet map[Capability]bool

// Supports returns true if the set contains c.
func (cs CapabilitySet) Supports(c Capability) bool {
	if cs == nil {
		return false
	}
	return cs[c]
}

// Add inserts c into the set, making it supported.
func (cs CapabilitySet) Add(c Capability) {
	if cs == nil {
		return
	}
	cs[c] = true
}

// CapabilityResolver takes a channel name and returns the set of capabilities
// it supports, or ErrCapabilityUnsupported when the channel is unknown.
type CapabilityResolver interface {
	Resolve(channel domain.Channel) (CapabilitySet, error)
}
