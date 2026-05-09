package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/google/uuid"

	ws "github.com/cbdc-simulator/backend/internal/websocket"
	"github.com/cbdc-simulator/backend/pkg/token"
)

// ── Fake auth ─────────────────────────────────────────────────────────────────

type mockAuth struct {
	claims *token.Claims
	err    error
}

func (m *mockAuth) ValidateAccessToken(_ string) (*token.Claims, error) {
	return m.claims, m.err
}

func validClaims(walletID uuid.UUID) *token.Claims {
	return &token.Claims{
		UserID:   uuid.NewString(),
		Role:     "user",
		WalletID: walletID.String(),
	}
}

// ── Test server helpers ───────────────────────────────────────────────────────

// newTestServer creates an httptest.Server running the WS handler.
func newTestServer(t *testing.T, auth *mockAuth) (*httptest.Server, *ws.Hub) {
	t.Helper()
	hub := ws.NewHub(nil) // no Redis
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)
	time.Sleep(5 * time.Millisecond)

	h := ws.NewHandler(hub, auth)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	t.Cleanup(srv.Close)
	return srv, hub
}

// wsURL converts an httptest server URL to a ws:// URL.
func wsURL(srv *httptest.Server, token string) string {
	u := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws"
	if token != "" {
		u += "?token=" + token
	}
	return u
}

// dial connects to the WS server; returns the connection or fails the test.
func dial(t *testing.T, url string) *gws.Conn {
	t.Helper()
	dialer := gws.DefaultDialer
	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial failed: %v (HTTP %d)", err, resp.StatusCode)
		}
		t.Fatalf("websocket dial failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// dialExpectFail connects and expects an HTTP error (not a WS upgrade).
func dialExpectFail(t *testing.T, url string, wantStatus int) {
	t.Helper()
	_, resp, err := gws.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("expected dial to fail, but it succeeded")
	}
	if resp == nil {
		t.Fatalf("expected HTTP response on failure, got nil (error: %v)", err)
	}
	if resp.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d", resp.StatusCode, wantStatus)
	}
}

// readEvent reads one JSON event from the WebSocket connection.
func readEvent(t *testing.T, conn *gws.Conn) ws.Event {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	var e ws.Event
	if err := json.Unmarshal(msg, &e); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	return e
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHandler_RejectsMissingToken(t *testing.T) {
	auth := &mockAuth{err: fmt.Errorf("no token")}
	srv, _ := newTestServer(t, auth)
	dialExpectFail(t, wsURL(srv, ""), http.StatusUnauthorized)
}

func TestHandler_RejectsInvalidToken(t *testing.T) {
	auth := &mockAuth{err: fmt.Errorf("invalid token")}
	srv, _ := newTestServer(t, auth)
	dialExpectFail(t, wsURL(srv, "bad-token"), http.StatusUnauthorized)
}

func TestHandler_AcceptsValidToken_QueryParam(t *testing.T) {
	walletID := uuid.New()
	auth := &mockAuth{claims: validClaims(walletID)}
	srv, _ := newTestServer(t, auth)

	conn := dial(t, wsURL(srv, "valid-token"))
	// Successful upgrade — ping/pong should work.
	conn.WriteMessage(gws.PingMessage, nil)
}

func TestHandler_AcceptsValidToken_Header(t *testing.T) {
	walletID := uuid.New()
	auth := &mockAuth{claims: validClaims(walletID)}
	srv, _ := newTestServer(t, auth)

	header := http.Header{"Authorization": []string{"Bearer valid-token"}}
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws"
	conn, _, err := gws.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial with header: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
}

func TestHandler_ReceivesEvent(t *testing.T) {
	walletID := uuid.New()
	auth := &mockAuth{claims: validClaims(walletID)}
	srv, hub := newTestServer(t, auth)

	conn := dial(t, wsURL(srv, "valid-token"))
	// Give the hub time to register the new client.
	time.Sleep(20 * time.Millisecond)

	event := ws.Event{
		Type:      ws.TypePaymentReceived,
		WalletID:  walletID.String(),
		Payload:   ws.PaymentEventPayload{TransactionID: "txn-abc", AmountCents: 1000},
		Timestamp: time.Now(),
	}
	if err := hub.Publish(context.Background(), walletID, event); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := readEvent(t, conn)
	if got.Type != ws.TypePaymentReceived {
		t.Errorf("event type = %q, want %q", got.Type, ws.TypePaymentReceived)
	}
	if got.WalletID != walletID.String() {
		t.Errorf("event wallet_id = %q, want %q", got.WalletID, walletID.String())
	}
}

func TestHandler_EventNotDeliveredToOtherWallet(t *testing.T) {
	walletA := uuid.New()
	walletB := uuid.New()

	authA := &mockAuth{claims: validClaims(walletA)}
	srv, hub := newTestServer(t, authA)

	// Connect a client for walletA.
	connA := dial(t, wsURL(srv, "token-a"))
	time.Sleep(20 * time.Millisecond)

	// Publish an event for walletB (not walletA).
	event := ws.Event{
		Type:      ws.TypePaymentReceived,
		WalletID:  walletB.String(),
		Timestamp: time.Now(),
	}
	hub.Publish(context.Background(), walletB, event)

	// walletA's connection should receive nothing.
	connA.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	_, _, err := connA.ReadMessage()
	if err == nil {
		t.Fatal("walletA should not have received walletB's event")
	}
	// Deadline exceeded or close is expected — any error means no event, which is correct.
}

func TestHandler_DisconnectUnregistersClient(t *testing.T) {
	walletID := uuid.New()
	auth := &mockAuth{claims: validClaims(walletID)}
	srv, hub := newTestServer(t, auth)

	conn := dial(t, wsURL(srv, "valid-token"))
	time.Sleep(20 * time.Millisecond)

	if n := hub.ConnectedCount(); n != 1 {
		t.Fatalf("ConnectedCount = %d before disconnect, want 1", n)
	}

	conn.Close()
	time.Sleep(50 * time.Millisecond)

	if n := hub.ConnectedCount(); n != 0 {
		t.Fatalf("ConnectedCount = %d after disconnect, want 0", n)
	}
}
