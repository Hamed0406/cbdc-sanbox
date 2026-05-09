package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	rdb "github.com/cbdc-simulator/backend/pkg/redis"
)

// redisChannel is the single Redis pub/sub channel for all WebSocket events.
// All backend instances publish here; each instance's subscriber routes to its
// locally-connected clients. Single channel avoids per-wallet subscriptions at scale.
const redisChannel = "cbdc:ws:events"

// sendBufferSize is the number of events buffered per client before dropping.
// 32 is generous for a payment app — typical bursts are 1-3 events.
const sendBufferSize = 32

// client represents one active WebSocket connection.
type client struct {
	walletID uuid.UUID
	send     chan []byte // outbound message queue; closed by hub when client is removed
}

// Hub manages all active WebSocket connections grouped by walletID (room).
// It satisfies the Publisher interface.
type Hub struct {
	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*client]struct{} // walletID → connected clients

	register   chan *client
	unregister chan *client

	// redis is optional. When nil, events are delivered locally only (useful in tests).
	// When set, all events go through Redis so every backend instance delivers them —
	// this means the hub's own subscriber also delivers self-published events, which
	// is intentional and avoids a separate local-delivery code path.
	redis *rdb.Client
}

// NewHub creates a Hub. Pass nil for redis in unit tests.
func NewHub(redis *rdb.Client) *Hub {
	return &Hub{
		rooms:      make(map[uuid.UUID]map[*client]struct{}),
		register:   make(chan *client, 16),
		unregister: make(chan *client, 16),
		redis:      redis,
	}
}

// Run starts the hub's event loop. It must be called in a goroutine and runs
// until ctx is cancelled. It also launches the Redis subscriber if Redis is set.
func (h *Hub) Run(ctx context.Context) {
	if h.redis != nil {
		go h.runRedisSubscriber(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case c := <-h.register:
			h.mu.Lock()
			if h.rooms[c.walletID] == nil {
				h.rooms[c.walletID] = make(map[*client]struct{})
			}
			h.rooms[c.walletID][c] = struct{}{}
			h.mu.Unlock()
			slog.Debug("websocket: client registered", "wallet_id", c.walletID)
		case c := <-h.unregister:
			h.mu.Lock()
			if room, ok := h.rooms[c.walletID]; ok {
				if _, exists := room[c]; exists {
					delete(room, c)
					close(c.send) // signals writePump to exit
					if len(room) == 0 {
						delete(h.rooms, c.walletID)
					}
				}
			}
			h.mu.Unlock()
			slog.Debug("websocket: client unregistered", "wallet_id", c.walletID)
		}
	}
}

// Publish sends an event to all clients connected to the given wallet.
//
// When Redis is configured:
//   - The event is published to Redis; the hub's own subscriber delivers it locally.
//   - This ensures every backend instance (not just the one that processed the payment)
//     delivers the event to its connected clients.
//   - Falls back to local-only delivery if Redis is unavailable.
//
// When Redis is nil (tests): delivers directly to local clients.
func (h *Hub) Publish(ctx context.Context, walletID uuid.UUID, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if h.redis != nil {
		if pubErr := h.redis.Publish(ctx, redisChannel, string(data)); pubErr != nil {
			// Redis unavailable — fall back to local delivery so the payment notification
			// still reaches clients connected to this instance.
			slog.Warn("websocket: redis publish failed, delivering locally",
				"wallet_id", walletID, "error", pubErr)
			h.broadcast(walletID, data)
		}
		// On success: do NOT also broadcast locally — the Redis subscriber will do it.
		return nil
	}

	h.broadcast(walletID, data)
	return nil
}

// broadcast delivers raw JSON to every client in the given wallet's room.
// Client pointers are copied inside the read lock, then sends happen outside it —
// this prevents a race with the Run goroutine deleting entries during unregister.
func (h *Hub) broadcast(walletID uuid.UUID, data []byte) {
	h.mu.RLock()
	var targets []*client
	for c := range h.rooms[walletID] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			// Client's buffer is full — drop rather than block the caller.
			// The client can re-sync its balance/history via REST on reconnect.
			slog.Warn("websocket: dropped event, client buffer full", "wallet_id", walletID)
		}
	}
}

// runRedisSubscriber listens on redisChannel and routes events published by any
// backend instance to locally-connected clients.
func (h *Hub) runRedisSubscriber(ctx context.Context) {
	pubsub := h.redis.Subscribe(ctx, redisChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				slog.Warn("websocket: Redis pub/sub channel closed")
				return
			}
			var event Event
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				slog.Warn("websocket: invalid event payload from Redis", "error", err)
				continue
			}
			walletID, err := uuid.Parse(event.WalletID)
			if err != nil {
				slog.Warn("websocket: invalid wallet_id in Redis event", "wallet_id", event.WalletID)
				continue
			}
			h.broadcast(walletID, []byte(msg.Payload))
		}
	}
}

// ConnectedCount returns the total number of active WebSocket connections.
// Used for metrics and health checks.
func (h *Hub) ConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, room := range h.rooms {
		total += len(room)
	}
	return total
}
