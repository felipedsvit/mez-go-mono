package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// TestNewOutboxMessage: factory valida campos obrigatórios.
func TestNewOutboxMessage_Valid(t *testing.T) {
	ob, err := domain.NewOutboxMessage("msg-1", "t", domain.ChannelWABA, "conv-1", "c-1")
	if err != nil {
		t.Fatalf("NewOutboxMessage: %v", err)
	}
	if ob.ID == "" {
		t.Error("ID should be generated")
	}
	if ob.Status != domain.OutboxStatusPending {
		t.Errorf("Status mismatch: %s", ob.Status)
	}
	if ob.MessageID != "msg-1" {
		t.Errorf("MessageID mismatch: %s", ob.MessageID)
	}
}

// TestNewOutboxMessage_Invalid: cobre entradas inválidas.
func TestNewOutboxMessage_Invalid(t *testing.T) {
	cases := []struct {
		name                          string
		msgID, tenant, ch, conv, cont string
	}{
		{"empty message id", "", "t", string(domain.ChannelWABA), "c", "x"},
		{"empty tenant", "m", "", string(domain.ChannelWABA), "c", "x"},
		{"empty channel", "m", "t", "", "c", "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NewOutboxMessage(
				domain.MessageID(c.msgID),
				domain.TenantID(c.tenant),
				domain.Channel(c.ch),
				domain.ConversationID(c.conv),
				domain.ContactID(c.cont),
			)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}

// TestOutboxMessage_FSM: transições válidas e FSM guards.
func TestOutboxMessage_FSM(t *testing.T) {
	ob, _ := domain.NewOutboxMessage("m", "t", domain.ChannelWABA, "c", "x")

	// Pending → Claimed.
	if err := ob.MarkClaimed(); err != nil {
		t.Fatalf("MarkClaimed: %v", err)
	}
	// Idempotente.
	if err := ob.MarkClaimed(); err != nil {
		t.Errorf("re-MarkClaimed should be no-op: %v", err)
	}

	// Claimed → Sent.
	if err := ob.MarkSent(); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}

	// Re-MarkSent é idempotente.
	if err := ob.MarkSent(); err != nil {
		t.Errorf("re-MarkSent should be no-op: %v", err)
	}

	// MarkFailed em estado terminal (Sent) é FSM error.
	if err := ob.MarkFailed(time.Now(), errors.New("late")); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("MarkFailed after Sent should be ErrInvalidTransition, got: %v", err)
	}
}

// TestOutboxMessage_MarkFailedThenDLQ: caminho de retry até DLQ.
func TestOutboxMessage_MarkFailedThenDLQ(t *testing.T) {
	ob, _ := domain.NewOutboxMessage("m", "t", domain.ChannelWABA, "c", "x")
	_ = ob.MarkClaimed()

	next := time.Now().Add(time.Minute)
	if err := ob.MarkFailed(next, errors.New("provider timeout")); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if ob.Status != domain.OutboxStatusFailed {
		t.Errorf("Status should be Failed, got: %s", ob.Status)
	}
	if ob.Attempts != 1 {
		t.Errorf("Attempts should be 1, got: %d", ob.Attempts)
	}

	// Re-falha atualiza attempts.
	if err := ob.MarkFailed(time.Now(), errors.New("another")); err != nil {
		t.Fatalf("re-MarkFailed: %v", err)
	}
	if ob.Attempts != 2 {
		t.Errorf("Attempts should be 2, got: %d", ob.Attempts)
	}

	// Failed → DLQ.
	if err := ob.MarkDLQ(errors.New("max attempts reached")); err != nil {
		t.Fatalf("MarkDLQ: %v", err)
	}
	if ob.Status != domain.OutboxStatusDLQ {
		t.Errorf("Status should be DLQ, got: %s", ob.Status)
	}

	// Re-MarkDLQ é idempotente.
	if err := ob.MarkDLQ(nil); err != nil {
		t.Errorf("re-MarkDLQ should be no-op: %v", err)
	}
}
