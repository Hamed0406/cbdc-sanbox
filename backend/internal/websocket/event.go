// Package websocket implements the real-time event hub for live payment notifications.
// Clients connect via GET /api/v1/ws with a JWT (header or ?token= query param).
// The server pushes events; clients never send messages beyond WebSocket control frames.
package websocket

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Event type constants — used as the Type field in every outbound Event.
const (
	TypePaymentSent      = "payment.sent"
	TypePaymentReceived  = "payment.received"
	TypeIssuanceReceived = "issuance.received"
)

// Event is the envelope delivered to WebSocket clients.
// WalletID identifies which wallet this event belongs to (used for routing).
type Event struct {
	Type      string    `json:"type"`
	WalletID  string    `json:"wallet_id"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

// PaymentEventPayload is the Payload for payment.sent and payment.received events.
// NewBalance* fields allow the client to update its local balance without a REST call.
type PaymentEventPayload struct {
	TransactionID     string  `json:"transaction_id"`
	Direction         string  `json:"direction"` // DEBIT or CREDIT relative to wallet owner
	AmountCents       int64   `json:"amount_cents"`
	AmountDisplay     string  `json:"amount_display"`
	CounterpartyName  *string `json:"counterparty_name"`
	Reference         *string `json:"reference"`
	NewBalanceCents   int64   `json:"new_balance_cents"`
	NewBalanceDisplay string  `json:"new_balance_display"`
}

// IssuanceEventPayload is the Payload for issuance.received events.
type IssuanceEventPayload struct {
	TransactionID     string `json:"transaction_id"`
	AmountCents       int64  `json:"amount_cents"`
	AmountDisplay     string `json:"amount_display"`
	Reason            string `json:"reason"`
	NewBalanceCents   int64  `json:"new_balance_cents"`
	NewBalanceDisplay string `json:"new_balance_display"`
}

// Publisher is the interface payment and admin services use to push events
// to connected WebSocket clients. Keeping it here (not in a shared pkg) avoids
// a circular import: payment/admin → websocket → (nothing in payment/admin).
type Publisher interface {
	Publish(ctx context.Context, walletID uuid.UUID, event Event) error
}
