// Package port — testes do fallback de capability (D7).
//
// Cobre:
//   - ResolveMessage: canal suporta → mensagem inalterada, degraded=false
//   - ResolveMessage: canal não suporta media mas suporta text → fallback media→text, degraded=true
//   - ResolveMessage: canal sem capability alguma → erro ErrCapabilityUnsupported
//   - ResolveMessage: canal não registrado → erro
//   - degradeMediaToText: preserva URL mesmo com body vazio
//   - RequiredCapabilityForType: mapeamento MessageType → Capability
package port_test

import (
	"errors"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// testChannel é um canal de mentira só usado pelos testes de fallback.
// Não existe em produção — serve para isolar "canal com capability parcial".
const testChannel domain.Channel = "test-channel"

func newMsg(t domain.MessageType, body string, meta map[string]any) domain.Message {
	return domain.Message{
		Type:     t,
		Body:     body,
		Metadata: meta,
	}
}

func TestResolveMessage_SupportedCapability_NoDegradation(t *testing.T) {
	t.Parallel()

	r := port.NewResolver()
	r.Register(domain.ChannelWABA, port.CapabilitySet{port.CapText: true, port.CapMedia: true})

	msg := newMsg(domain.MessageTypeText, "hello", nil)
	out, degraded, err := r.ResolveMessage(domain.ChannelWABA, msg)
	if err != nil {
		t.Fatalf("ResolveMessage: %v", err)
	}
	if degraded {
		t.Error("expected degraded=false when channel supports text")
	}
	if out.Type != domain.MessageTypeText {
		t.Errorf("type = %q, want text (unchanged)", out.Type)
	}
	if out.Body != "hello" {
		t.Errorf("body = %q, want unchanged", out.Body)
	}
}

func TestResolveMessage_MediaFallback_ToText(t *testing.T) {
	t.Parallel()

	r := port.NewResolver()
	// Canal que NÃO tem media mas tem text (ex.: scenario custom).
	r.Register(testChannel, port.CapabilitySet{port.CapText: true})

	msg := newMsg(domain.MessageTypeImage, "look at this", map[string]any{
		"url": "https://example.com/cat.jpg",
	})
	out, degraded, err := r.ResolveMessage(testChannel, msg)
	if err != nil {
		t.Fatalf("ResolveMessage: %v", err)
	}
	if !degraded {
		t.Error("expected degraded=true (media→text fallback)")
	}
	if out.Type != domain.MessageTypeText {
		t.Errorf("type = %q, want text (degraded)", out.Type)
	}
	if out.Body == "" {
		t.Error("body should not be empty after fallback")
	}
	// Body + URL concatenados.
	if !contains(out.Body, "look at this") || !contains(out.Body, "https://example.com/cat.jpg") {
		t.Errorf("body = %q, want contains caption + URL", out.Body)
	}
}

func TestResolveMessage_MediaFallback_PreservesURL_WhenBodyEmpty(t *testing.T) {
	t.Parallel()

	r := port.NewResolver()
	r.Register(testChannel, port.CapabilitySet{port.CapText: true})

	msg := newMsg(domain.MessageTypeImage, "", map[string]any{
		"url": "https://example.com/cat.jpg",
	})
	out, degraded, err := r.ResolveMessage(testChannel, msg)
	if err != nil {
		t.Fatalf("ResolveMessage: %v", err)
	}
	if !degraded {
		t.Error("expected degraded=true")
	}
	if out.Body != "https://example.com/cat.jpg" {
		t.Errorf("body = %q, want URL only", out.Body)
	}
}

func TestResolveMessage_UnsupportedCapability_ReturnsError(t *testing.T) {
	t.Parallel()

	r := port.NewResolver()
	// Canal sem nenhuma capability relevante para image.
	r.Register(testChannel, port.CapabilitySet{port.CapTemplates: true})

	msg := newMsg(domain.MessageTypeImage, "x", nil)
	_, _, err := r.ResolveMessage(testChannel, msg)
	if err == nil {
		t.Fatal("expected error when channel lacks required capability")
	}
	if !errors.Is(err, port.ErrCapabilityUnsupported) {
		t.Errorf("err = %v, want wraps ErrCapabilityUnsupported", err)
	}
}

func TestResolveMessage_UnregisteredChannel_ReturnsError(t *testing.T) {
	t.Parallel()

	r := port.NewResolver()
	_, _, err := r.ResolveMessage(domain.ChannelWABA, newMsg(domain.MessageTypeText, "hi", nil))
	if !errors.Is(err, port.ErrCapabilityUnsupported) {
		t.Errorf("err = %v, want wraps ErrCapabilityUnsupported", err)
	}
}

func TestResolveMessage_ReactionType_DefaultCapability(t *testing.T) {
	t.Parallel()

	// Tipos não-mapeados caem no default CapText via requiredCapabilityForType.
	r := port.NewResolver()
	r.Register(testChannel, port.CapabilitySet{port.CapText: true})

	msg := newMsg(domain.MessageTypeSystem, "system msg", nil)
	_, degraded, err := r.ResolveMessage(testChannel, msg)
	if err != nil {
		t.Fatalf("ResolveMessage: %v", err)
	}
	if degraded {
		t.Error("system messages should not be degraded when channel supports text")
	}
}

func TestRequiredCapabilityForType_Mapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ  domain.MessageType
		want port.Capability
	}{
		{domain.MessageTypeText, port.CapText},
		{domain.MessageTypeImage, port.CapMedia},
		{domain.MessageTypeAudio, port.CapMedia},
		{domain.MessageTypeVideo, port.CapMedia},
		{domain.MessageTypeDocument, port.CapMedia},
		{domain.MessageTypeSticker, port.CapMedia},
		{domain.MessageTypeLocation, port.CapMedia},
		{domain.MessageTypeButton, port.CapMedia},
		{domain.MessageTypeTemplate, port.CapMedia},
		{domain.MessageTypeReaction, port.CapText},
		{domain.MessageTypeSystem, port.CapText},
	}
	for _, c := range cases {
		if got := port.RequiredCapabilityForType(c.typ); got != c.want {
			t.Errorf("RequiredCapabilityForType(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestCapabilitySet_SupportsAndAdd(t *testing.T) {
	t.Parallel()

	// nil set: Supports é false para qualquer capability.
	var nilSet port.CapabilitySet
	if nilSet.Supports(port.CapText) {
		t.Error("nil CapabilitySet should NOT support anything")
	}
	// Add em nil set é no-op (receiver é nil map; writes panicariam).
	// Por isso, Add em nil é defensivo e não faz nada.
	nilSet.Add(port.CapText)
	if nilSet.Supports(port.CapText) {
		t.Error("Add on nil set should be no-op (set remains nil)")
	}

	cs := port.CapabilitySet{port.CapText: true}
	if !cs.Supports(port.CapText) {
		t.Error("explicitly true should support")
	}
	if cs.Supports(port.CapMedia) {
		t.Error("missing key should not support")
	}
	cs.Add(port.CapMedia)
	if !cs.Supports(port.CapMedia) {
		t.Error("Add should add the capability to a non-nil set")
	}
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
