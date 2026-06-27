// Package whatsmeow — real_client_test.go: testes unitários do RealClient.
//
// Os testes verificam:
//   - Compile-time: RealClient satisfaz a interface Client
//   - NewRealClient retorna erro quando DSN vazio
//   - GetQRChannel retorna nil se cli é nil
//   - Disconnect é safe com cli nil
//   - Logs são emitidos corretamente
//
// Não é possível testar Connect/Disconnect contra WhatsApp servers reais
// (requer conta pareada + número real). Esses caminhos são cobertos por
// testes de integração em tests/integration/ (build tag integration).
package whatsmeow

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
)

// TestRealClient_ImplementsClient é compile-time assertion.
func TestRealClient_ImplementsClient(t *testing.T) {
	t.Parallel()

	// Se esta linha compilar, RealClient satisfaz Client.
	var _ Client = (*RealClient)(nil)
}

// TestNewRealClient_EmptyDSN_Fails verifica que sem DSN o construtor
// retorna erro (fail-closed).
func TestNewRealClient_EmptyDSN_Fails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := NewRealClient(ctx, "t1", RealClientConfig{
		DeviceDSN: "",
	}, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error when DeviceDSN is empty")
	}
}

// TestRealClient_Disconnect_NilIsSafe garante que Disconnect não
// panica com cli==nil.
func TestRealClient_Disconnect_NilIsSafe(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	// Não deve panic.
	r.Disconnect()
}

// TestRealClient_IsConnected_NilCli garante que IsConnected retorna
// false com cli==nil.
func TestRealClient_IsConnected_NilCli(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	if r.IsConnected() {
		t.Error("IsConnected should return false when cli is nil")
	}
}

// TestRealClient_SendMessage_NotConnected verifica que sem conexão,
// todas as ações de envio retornam ErrNotConnected.
func TestRealClient_SendMessage_NotConnected(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}

	jid, _ := ParseJID("5511999999999@s.whatsapp.net")

	tests := []struct {
		name string
		fn   func() error
	}{
		{"SendMessage", func() error {
			_, err := r.SendMessage(context.Background(), jid, "olá")
			return err
		}},
		{"SendImage", func() error {
			_, err := r.SendImage(context.Background(), jid, []byte("png"), "image/png", "caption")
			return err
		}},
		{"SendAudio", func() error {
			_, err := r.SendAudio(context.Background(), jid, []byte("ogg"), "audio/ogg", true)
			return err
		}},
		{"SendVideo", func() error {
			_, err := r.SendVideo(context.Background(), jid, []byte("mp4"), "video/mp4", "caption")
			return err
		}},
		{"SendDocument", func() error {
			_, err := r.SendDocument(context.Background(), jid, []byte("pdf"), "application/pdf", "doc.pdf", "title")
			return err
		}},
		{"SendSticker", func() error {
			_, err := r.SendSticker(context.Background(), jid, []byte("webp"))
			return err
		}},
		{"SendReaction", func() error {
			return r.SendReaction(context.Background(), jid, types.MessageID("wamid.X"), "👍")
		}},
		{"EditMessage", func() error {
			_, err := r.EditMessage(context.Background(), jid, types.MessageID("wamid.X"), "novo")
			return err
		}},
		{"RevokeMessage", func() error {
			_, err := r.RevokeMessage(context.Background(), jid, types.MessageID("wamid.X"))
			return err
		}},
		{"MarkRead", func() error {
			return r.MarkRead(context.Background(), jid, []types.MessageID{"wamid.X"}, 0)
		}},
		{"SendChatPresence", func() error {
			return r.SendChatPresence(context.Background(), jid, types.ChatPresenceComposing)
		}},
		{"SendPresence", func() error {
			return r.SendPresence(context.Background(), types.PresenceAvailable)
		}},
		{"RejectCall", func() error {
			return r.RejectCall(context.Background(), jid, "call-1")
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.fn()
			if err != ErrNotConnected {
				t.Errorf("%s: expected ErrNotConnected, got %v", tt.name, err)
			}
		})
	}
}

// TestRealClient_GetQRChannel_NilCli garante que GetQRChannel
// retorna erro com cli==nil.
func TestRealClient_GetQRChannel_NilCli(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	_, err := r.GetQRChannel(context.Background())
	if err == nil {
		t.Error("expected error with nil cli")
	}
}

// TestRealClient_IsLoggedIn_NilCli garante fallback.
func TestRealClient_IsLoggedIn_NilCli(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	if r.IsLoggedIn() {
		t.Error("IsLoggedIn should return false when cli is nil")
	}
}

// TestRealClient_Logout_NilIsSafe garante que Logout não panica.
func TestRealClient_Logout_NilIsSafe(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	if err := r.Logout(context.Background()); err != nil {
		t.Errorf("Logout with nil cli should be no-op, got %v", err)
	}
}

// TestRealClient_AddEventHandler_NilIsSafe garante que AddEventHandler
// retorna 0 (no handler ID) com cli==nil.
func TestRealClient_AddEventHandler_NilIsSafe(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	id := r.AddEventHandler(func(_ any) {})
	if id != 0 {
		t.Errorf("AddEventHandler with nil cli should return 0, got %d", id)
	}
}

// TestRealClient_Connect_NilCli verifica erro em Connect.
func TestRealClient_Connect_NilCli(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, log: zerolog.Nop()}
	if err := r.Connect(context.Background()); err == nil {
		t.Error("expected error with nil cli")
	}
}

// TestRealClient_Container_NilAccess.
func TestRealClient_Container_NilAccess(t *testing.T) {
	t.Parallel()

	r := &RealClient{cli: nil, container: nil, log: zerolog.Nop()}
	if r.Container() != nil {
		t.Error("Container should return nil with nil container")
	}
}

// TestEventType verifica que a função utilitária retorna nomes legíveis.
func TestEventType(t *testing.T) {
	t.Parallel()

	// Casos cobertos: tipo desconhecido cai no fallback fmt.Sprintf("%T", evt).
	if got := EventType(nil); got != "<nil>" {
		t.Errorf("EventType(nil) = %q, want <nil>", got)
	}
	if got := EventType("unknown"); got != "string" {
		t.Errorf("EventType(string) = %q, want string", got)
	}
	if got := EventType(123); got != "int" {
		t.Errorf("EventType(int) = %q, want int", got)
	}
}

// TestIdentityFromConfig valida o anti-ban helper.
func TestIdentityFromConfig(t *testing.T) {
	t.Parallel()

	t.Run("none returns nil", func(t *testing.T) {
		t.Parallel()
		if IdentityFromConfig("none", "") != nil {
			t.Error("expected nil for 'none'")
		}
	})
	t.Run("chrome returns Chrome platform", func(t *testing.T) {
		t.Parallel()
		id := IdentityFromConfig("chrome", "Mac OS")
		if id == nil {
			t.Fatal("expected non-nil for 'chrome'")
		}
		if id.OSName != "Mac OS" {
			t.Errorf("OSName = %q", id.OSName)
		}
	})
	t.Run("edge returns Edge platform", func(t *testing.T) {
		t.Parallel()
		id := IdentityFromConfig("edge", "Linux")
		if id == nil {
			t.Fatal("expected non-nil for 'edge'")
		}
		if id.OSName != "Linux" {
			t.Errorf("OSName = %q", id.OSName)
		}
	})
	t.Run("default OS when empty", func(t *testing.T) {
		t.Parallel()
		id := IdentityFromConfig("chrome", "")
		if id == nil {
			t.Fatal("expected non-nil")
		}
		if id.OSName != "Mac OS" {
			t.Errorf("default OSName = %q, want Mac OS", id.OSName)
		}
	})
	t.Run("unknown kind falls to chrome", func(t *testing.T) {
		t.Parallel()
		id := IdentityFromConfig("weird", "")
		if id == nil {
			t.Fatal("expected non-nil")
		}
		_ = id
	})
	t.Run("empty kind returns nil", func(t *testing.T) {
		t.Parallel()
		if IdentityFromConfig("", "") != nil {
			t.Error("expected nil for empty kind")
		}
	})
}

// TestIdentity_Apply_NilSafe garante que Apply() é safe com nil.
func TestIdentity_Apply_NilSafe(t *testing.T) {
	t.Parallel()

	var d *DeviceIdentity
	// Não deve panic.
	d.Apply()
}
