package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// runHub starts the hub in a background goroutine and returns a cancel func.
func runHub(t *testing.T) (*Hub, context.CancelFunc) {
	t.Helper()
	hub := NewHub(nil) // no Redis — local delivery only
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	// Give the event loop goroutine a moment to start.
	time.Sleep(5 * time.Millisecond)
	return hub, cancel
}

// newRegisteredClient creates a client, sends it to the hub's register channel,
// and waits for the hub event loop to process it.
func newRegisteredClient(t *testing.T, hub *Hub, walletID uuid.UUID) *client {
	t.Helper()
	c := &client{
		walletID: walletID,
		send:     make(chan []byte, sendBufferSize),
	}
	hub.register <- c
	time.Sleep(5 * time.Millisecond) // let hub event loop process
	return c
}

// drainEvent reads one event from the client's send channel with a short timeout.
func drainEvent(t *testing.T, c *client) Event {
	t.Helper()
	select {
	case data := <-c.send:
		var e Event
		if err := json.Unmarshal(data, &e); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		return e
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

// noEvent asserts that no event arrives on the client's send channel.
func noEvent(t *testing.T, c *client) {
	t.Helper()
	select {
	case msg, ok := <-c.send:
		if ok {
			t.Errorf("unexpected event received: %s", msg)
		}
	case <-time.After(30 * time.Millisecond):
		// expected — nothing arrived
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHub_Register(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	walletID := uuid.New()
	c := newRegisteredClient(t, hub, walletID)

	hub.mu.RLock()
	_, ok := hub.rooms[walletID][c]
	hub.mu.RUnlock()

	if !ok {
		t.Fatal("client not found in room after registration")
	}
}

func TestHub_Unregister_RemovesClientAndRoom(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	walletID := uuid.New()
	c := newRegisteredClient(t, hub, walletID)

	hub.unregister <- c
	time.Sleep(10 * time.Millisecond)

	hub.mu.RLock()
	_, roomExists := hub.rooms[walletID]
	hub.mu.RUnlock()

	if roomExists {
		t.Fatal("room should be deleted when last client leaves")
	}

	// Confirm the send channel was closed.
	select {
	case _, ok := <-c.send:
		if ok {
			t.Fatal("send channel should be closed after unregister")
		}
	default:
		t.Fatal("send channel should be closed, not empty")
	}
}

func TestHub_Unregister_KeepsRoomWhenOtherClientsRemain(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	walletID := uuid.New()
	c1 := newRegisteredClient(t, hub, walletID)
	c2 := newRegisteredClient(t, hub, walletID)

	hub.unregister <- c1
	time.Sleep(10 * time.Millisecond)

	hub.mu.RLock()
	room := hub.rooms[walletID]
	hub.mu.RUnlock()

	if _, ok := room[c2]; !ok {
		t.Fatal("c2 should still be in the room")
	}
	if _, ok := room[c1]; ok {
		t.Fatal("c1 should have been removed")
	}
}

func TestHub_Publish_DeliversToCorrectWallet(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	walletID := uuid.New()
	c := newRegisteredClient(t, hub, walletID)

	event := Event{
		Type:      TypePaymentReceived,
		WalletID:  walletID.String(),
		Payload:   PaymentEventPayload{TransactionID: "txn-1", AmountCents: 500},
		Timestamp: time.Now(),
	}

	if err := hub.Publish(context.Background(), walletID, event); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := drainEvent(t, c)
	if got.Type != TypePaymentReceived {
		t.Errorf("event type = %q, want %q", got.Type, TypePaymentReceived)
	}
}

func TestHub_Publish_DoesNotDeliverToOtherWallet(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	sender := uuid.New()
	receiver := uuid.New()

	senderClient := newRegisteredClient(t, hub, sender)
	receiverClient := newRegisteredClient(t, hub, receiver)

	// Publish only to the receiver.
	event := Event{Type: TypePaymentReceived, WalletID: receiver.String(), Timestamp: time.Now()}
	hub.Publish(context.Background(), receiver, event)

	// Receiver gets the event.
	drainEvent(t, receiverClient)

	// Sender must NOT get it.
	noEvent(t, senderClient)
}

func TestHub_Publish_DeliversToAllClientsInRoom(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	walletID := uuid.New()
	c1 := newRegisteredClient(t, hub, walletID)
	c2 := newRegisteredClient(t, hub, walletID)

	event := Event{Type: TypePaymentReceived, WalletID: walletID.String(), Timestamp: time.Now()}
	hub.Publish(context.Background(), walletID, event)

	drainEvent(t, c1)
	drainEvent(t, c2)
}

func TestHub_Publish_NoClientsNoError(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	// Publish to a wallet with no connected clients — should not panic or error.
	event := Event{Type: TypePaymentReceived, WalletID: uuid.NewString(), Timestamp: time.Now()}
	if err := hub.Publish(context.Background(), uuid.New(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHub_Publish_FullBufferDropsGracefully(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	walletID := uuid.New()
	c := newRegisteredClient(t, hub, walletID)

	// Fill the send buffer completely.
	for i := 0; i < sendBufferSize; i++ {
		c.send <- []byte(`{"type":"payment.received"}`)
	}

	// One more publish should drop (not block or panic).
	event := Event{Type: TypePaymentReceived, WalletID: walletID.String(), Timestamp: time.Now()}
	hub.broadcast(walletID, mustMarshal(t, event))
	// If we reach here the hub didn't deadlock — test passes.
}

func TestHub_ConnectedCount(t *testing.T) {
	hub, cancel := runHub(t)
	defer cancel()

	if n := hub.ConnectedCount(); n != 0 {
		t.Fatalf("ConnectedCount = %d before any connections", n)
	}

	w1, w2 := uuid.New(), uuid.New()
	newRegisteredClient(t, hub, w1)
	newRegisteredClient(t, hub, w1)
	newRegisteredClient(t, hub, w2)

	if n := hub.ConnectedCount(); n != 3 {
		t.Fatalf("ConnectedCount = %d, want 3", n)
	}
}

// mustMarshal marshals v to JSON or fails the test.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
