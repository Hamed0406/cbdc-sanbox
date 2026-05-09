package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cbdc-simulator/backend/pkg/currency"
	"github.com/cbdc-simulator/backend/pkg/database"
)

// Repository handles payment-related DB queries.
// All writes go through internal/ledger — this package is read-only.
type Repository struct {
	db *database.Pool
}

// NewRepository creates a new payment Repository.
func NewRepository(db *database.Pool) *Repository {
	return &Repository{db: db}
}

// GetByID fetches a single transaction, verifying the given wallet is a party.
// Returns ErrTransactionNotFound if the transaction doesn't exist or the wallet
// is not the sender or receiver (intentionally indistinguishable to prevent enumeration).
func (r *Repository) GetByID(ctx context.Context, txnID, walletID uuid.UUID) (*TransactionDetail, error) {
	var d TransactionDetail
	var senderStr, receiverStr *string
	var createdAt time.Time
	var settledAt *time.Time

	err := r.db.QueryRow(ctx, `
		SELECT
			t.id::text,
			t.type::text,
			t.status::text,
			CASE WHEN t.sender_wallet_id = $2 THEN 'DEBIT' ELSE 'CREDIT' END AS direction,
			t.sender_wallet_id::text,
			t.receiver_wallet_id::text,
			CASE
				WHEN t.sender_wallet_id = $2 THEN receiver_user.full_name
				WHEN t.receiver_wallet_id = $2 THEN
					CASE WHEN sender_user.full_name IS NOT NULL THEN sender_user.full_name
					     ELSE 'CBDC System'
					END
				ELSE NULL
			END AS counterparty_name,
			t.amount_cents,
			t.fee_cents,
			t.reference,
			t.signature,
			t.created_at,
			t.settled_at
		FROM transactions t
		LEFT JOIN wallets sw ON sw.id = t.sender_wallet_id
		LEFT JOIN users sender_user ON sender_user.id = sw.user_id
		LEFT JOIN wallets rw ON rw.id = t.receiver_wallet_id
		LEFT JOIN users receiver_user ON receiver_user.id = rw.user_id
		WHERE t.id = $1
		  AND (t.sender_wallet_id = $2 OR t.receiver_wallet_id = $2)
	`, txnID, walletID).Scan(
		&d.ID, &d.Type, &d.Status, &d.Direction,
		&senderStr, &receiverStr,
		&d.CounterpartyName,
		&d.AmountCents, &d.FeeCents, &d.Reference, &d.Signature,
		&createdAt, &settledAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("get payment by id: %w", err)
	}

	d.SenderWalletID = senderStr
	d.ReceiverWalletID = receiverStr
	d.AmountDisplay = currency.Format(d.AmountCents, "DD$")
	d.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if settledAt != nil {
		s := settledAt.UTC().Format(time.RFC3339)
		d.SettledAt = &s
	}

	return &d, nil
}

// List returns paginated transactions involving the given wallet.
func (r *Repository) List(ctx context.Context, walletID uuid.UUID, p ListParams) ([]TransactionDetail, int, error) {
	args := []any{walletID}
	conditions := []string{"(t.sender_wallet_id = $1 OR t.receiver_wallet_id = $1)", "t.status != 'FAILED'"}

	if p.Type != "" {
		args = append(args, p.Type)
		conditions = append(conditions, fmt.Sprintf("t.type = $%d", len(args)))
	}
	if p.Status != "" {
		args = append(args, p.Status)
		conditions = append(conditions, fmt.Sprintf("t.status = $%d", len(args)))
	}
	if p.From != nil {
		args = append(args, *p.From)
		conditions = append(conditions, fmt.Sprintf("t.created_at >= $%d", len(args)))
	}
	if p.To != nil {
		args = append(args, *p.To)
		conditions = append(conditions, fmt.Sprintf("t.created_at <= $%d", len(args)))
	}

	where := strings.Join(conditions, " AND ")

	var total int
	if err := r.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM transactions t WHERE "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count payments: %w", err)
	}

	offset := (p.Page - 1) * p.Limit
	args = append(args, p.Limit, offset)
	limitArg := len(args) - 1
	offsetArg := len(args)

	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT
			t.id::text,
			t.type::text,
			t.status::text,
			CASE WHEN t.sender_wallet_id = $1 THEN 'DEBIT' ELSE 'CREDIT' END AS direction,
			t.sender_wallet_id::text,
			t.receiver_wallet_id::text,
			CASE
				WHEN t.sender_wallet_id = $1 THEN receiver_user.full_name
				WHEN t.receiver_wallet_id = $1 THEN
					CASE WHEN sender_user.full_name IS NOT NULL THEN sender_user.full_name
					     ELSE 'CBDC System'
					END
				ELSE NULL
			END AS counterparty_name,
			t.amount_cents,
			t.fee_cents,
			t.reference,
			t.signature,
			t.created_at,
			t.settled_at
		FROM transactions t
		LEFT JOIN wallets sw ON sw.id = t.sender_wallet_id
		LEFT JOIN users sender_user ON sender_user.id = sw.user_id
		LEFT JOIN wallets rw ON rw.id = t.receiver_wallet_id
		LEFT JOIN users receiver_user ON receiver_user.id = rw.user_id
		WHERE %s
		ORDER BY t.created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, limitArg, offsetArg), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()

	var result []TransactionDetail
	for rows.Next() {
		var d TransactionDetail
		var senderStr, receiverStr *string
		var createdAt time.Time
		var settledAt *time.Time

		if err := rows.Scan(
			&d.ID, &d.Type, &d.Status, &d.Direction,
			&senderStr, &receiverStr,
			&d.CounterpartyName,
			&d.AmountCents, &d.FeeCents, &d.Reference, &d.Signature,
			&createdAt, &settledAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan payment row: %w", err)
		}

		d.SenderWalletID = senderStr
		d.ReceiverWalletID = receiverStr
		d.AmountDisplay = currency.Format(d.AmountCents, "DD$")
		d.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if settledAt != nil {
			s := settledAt.UTC().Format(time.RFC3339)
			d.SettledAt = &s
		}

		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("payment rows error: %w", err)
	}

	if result == nil {
		result = []TransactionDetail{}
	}
	return result, total, nil
}
