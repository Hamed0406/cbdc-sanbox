package websocket

import (
	"log/slog"
	"net/http"
	"time"

	gws "github.com/gorilla/websocket"

	"github.com/cbdc-simulator/backend/pkg/token"
)

// WebSocket timing constants.
// pongWait: how long to wait for a pong before closing the connection.
// pingPeriod must be shorter than pongWait so the ping arrives before timeout.
const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10 // 54s — ensures ping arrives before pong deadline
	maxMsgSize = 512                  // bytes; clients only send pong frames
)

// tokenValidator is the minimum interface needed to verify a JWT access token.
// auth.Service satisfies this without the websocket package importing auth.
type tokenValidator interface {
	ValidateAccessToken(tokenString string) (*token.Claims, error)
}

var upgrader = gws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin validates the WebSocket upgrade request's Origin header.
	// We accept connections from any origin here because the JWT provides
	// the actual authentication — a stolen origin header gains nothing without a valid token.
	// In production, restrict this to the configured ALLOWED_ORIGIN.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler handles WebSocket upgrade requests.
type Handler struct {
	hub  *Hub
	auth tokenValidator
}

// NewHandler creates a WebSocket Handler.
func NewHandler(hub *Hub, auth tokenValidator) *Handler {
	return &Handler{hub: hub, auth: auth}
}

// ServeWS upgrades the HTTP connection to WebSocket, validates the JWT,
// registers the client with the hub, then runs the read and write pumps.
//
// Token lookup order:
//  1. Authorization: Bearer <token> header — for non-browser clients (curl, mobile SDKs)
//  2. ?token=<jwt> query param — for browsers using the native WebSocket API,
//     which cannot set custom headers on the initial upgrade request
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	tokenStr := extractToken(r)
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := h.auth.ValidateAccessToken(tokenStr)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes its own error response on failure; just log.
		slog.Warn("websocket: upgrade failed", "error", err)
		return
	}

	c := &client{
		send: make(chan []byte, sendBufferSize),
	}

	// walletID is already validated as a UUID in the JWT claim by auth.Service.
	// A parse failure here means a malformed token slipped through — close immediately.
	if err := c.walletID.UnmarshalText([]byte(claims.WalletID)); err != nil {
		slog.Error("websocket: malformed wallet_id in JWT claim",
			"wallet_id", claims.WalletID, "user_id", claims.UserID)
		conn.Close()
		return
	}

	h.hub.register <- c
	slog.Info("websocket: client connected", "wallet_id", c.walletID, "user_id", claims.UserID)

	// Write pump runs in its own goroutine; read pump runs in the current goroutine.
	// When the read pump returns (disconnect/error), it unregisters the client, which
	// closes c.send, which causes the write pump to exit cleanly.
	go h.writePump(conn, c)
	h.readPump(conn, c)
}

// readPump processes incoming WebSocket frames.
// Its main job is to detect disconnects and handle pong frames that extend the deadline.
// We don't process client messages — the WebSocket connection is server-push only.
func (h *Handler) readPump(conn *gws.Conn, c *client) {
	defer func() {
		h.hub.unregister <- c
		conn.Close()
	}()

	conn.SetReadLimit(maxMsgSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	// Each pong from the client resets the read deadline, keeping the connection alive.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		// We discard client messages; the loop only serves to detect EOF/disconnect.
		if _, _, err := conn.ReadMessage(); err != nil {
			if gws.IsUnexpectedCloseError(err, gws.CloseGoingAway, gws.CloseAbnormalClosure) {
				slog.Warn("websocket: unexpected close", "wallet_id", c.walletID, "error", err)
			}
			return
		}
	}
}

// writePump drains the client's send channel and writes messages to the WebSocket.
// It sends periodic pings so the read pump can detect stale connections.
func (h *Handler) writePump(conn *gws.Conn, c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel — send a clean close frame and exit.
				conn.WriteMessage(gws.CloseMessage, []byte{})
				return
			}
			if err := conn.WriteMessage(gws.TextMessage, msg); err != nil {
				slog.Warn("websocket: write error", "wallet_id", c.walletID, "error", err)
				return
			}
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(gws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// extractToken returns the JWT string from the Authorization header or ?token= query param.
func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return r.URL.Query().Get("token")
}
