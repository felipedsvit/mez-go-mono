package port_test

import (
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

func TestResolver_RegisterAndResolve(t *testing.T) {
	r := port.NewResolver()
	caps := port.CapabilitySet{
		port.CapText:   true,
		port.CapMedia:  true,
		port.CapDelete: true,
	}
	r.Register(domain.ChannelWABA, caps)

	resolved, err := r.Resolve(domain.ChannelWABA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resolved.Supports(port.CapText) {
		t.Error("WABA should support text")
	}
	if !resolved.Supports(port.CapDelete) {
		t.Error("WABA should support delete")
	}
}

func TestResolver_UnsupportedChannel(t *testing.T) {
	r := port.NewResolver()
	_, err := r.Resolve(domain.ChannelTGBot)
	if err == nil {
		t.Fatal("expected error for unregistered channel")
	}
}

func TestResolver_Supports(t *testing.T) {
	r := port.NewResolver()
	caps := port.CapabilitySet{port.CapText: true}
	r.Register(domain.ChannelTGBot, caps)

	if !r.Supports(domain.ChannelTGBot, port.CapText) {
		t.Error("should support text")
	}
	if r.Supports(domain.ChannelTGBot, port.CapMedia) {
		t.Error("should NOT support media")
	}
}
