package port

import (
	"errors"
	"fmt"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

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
