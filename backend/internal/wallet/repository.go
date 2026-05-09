package wallet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cbdc-simulator/backend/pkg/database"
)

// Repository handles wallet read queries.
// Balance writes go through internal/ledger — never here.
type Repository struct {
	db *database.Pool
}

// NewRepository creates a new wallet Repository.
func NewRepository(db *database.Pool) *Repository {
	return &Repository{db: db}
}

// FindByID fetches a wallet by its UUID.
func (r *Repository) FindByID(ctx context.Context, walletID uuid.UUID) (*Wallet, error) {
	w := &Wallet{}
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, currency, balance, is_frozen,
		       frozen_reason, frozen_by, frozen_at, created_at, updated_at
		FROM wallets
		WHERE id = $1 AND deleted_at IS NULL
	`, walletID).Scan(
		&w.ID, &w.UserID, &w.Currency, &w.Balance, &w.IsFrozen,
		&w.FrozenReason, &w.FrozenBy, &w.FrozenAt, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("find wallet by id: %w", err)
	}
	return w, nil
}

// FindByUserID fetches the primary DD$ wallet for a user.
func (r *Repository) FindByUserID(ctx context.Context, userID uuid.UUID) (*Wallet, error) {
	w := &Wallet{}
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, currency, balance, is_frozen,
		       frozen_reason, frozen_by, frozen_at, created_at, updated_at
		FROM wallets
		WHERE user_id = $1 AND currency = 'DD$' AND deleted_at IS NULL
		LIMIT 1
	`, userID).Scan(
		&w.ID, &w.UserID, &w.Currency, &w.Balance, &w.IsFrozen,
		&w.FrozenReason, &w.FrozenBy, &w.FrozenAt, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("find wallet by user: %w", err)
	}
	return w, nil
}

// GetTransactionHistory returns paginated transaction history for a wallet.
// Each row includes the direction (DEBIT/CREDIT) and counterparty display name.
//
// WHY LEFT JOIN to users?
// ISSUANCE transactions have sender_wallet_id = NULL (central bank),
// so we use LEFT JOIN and return "CBDC System" for the counterparty name.
func (r *Repository) GetTransactionHistory(ctx context.Context, walletID uuid.UUID, p ListParams) ([]TransactionRow, int, error) {
	// ── Build WHERE clause dynamically ───────────────────────────────────────
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

	// ── Count total matching rows (for pagination) ────────────────────────────
	var total int
	if err := r.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM transactions t WHERE "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count transactions: %w", err)
	}

	// ── Fetch page ────────────────────────────────────────────────────────────
	offset := (p.Page - 1) * p.Limit
	args = append(args, p.Limit, offset)
	limitArg := len(args) - 1
	offsetArg := len(args)

	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT
			t.id,
			t.type,
			t.status,
			-- Direction from this wallet's perspective
			CASE WHEN t.sender_wallet_id = $1 THEN 'DEBIT' ELSE 'CREDIT' END AS direction,
			-- Counterparty name: opposite side's user full_name; NULL for system issuance
			CASE
				WHEN t.sender_wallet_id = $1 THEN receiver_user.full_name
				WHEN t.receiver_wallet_id = $1 THEN
					CASE WHEN sender_user.full_name IS NOT NULL THEN sender_user.full_name
					     ELSE 'CBDC System'
					END
				ELSE NULL
			END AS counterparty_name,
			-- Counterparty wallet ID
			CASE
				WHEN t.sender_wallet_id = $1 THEN t.receiver_wallet_id::text
				ELSE t.sender_wallet_id::text
			END AS counterparty_wallet_id,
			t.amount_cents,
			t.reference,
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
		return nil, 0, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()

	var result []TransactionRow
	for rows.Next() {
		var row TransactionRow
		var counterpartyWalletStr *string
		// Scan timestamps into time.Time — the DB returns timestamptz, not strings.
		// We format to RFC3339 after scanning so the JSON output is consistent.
		var createdAt time.Time
		var settledAt *time.Time
		if err := rows.Scan(
			&row.ID, &row.Type, &row.Status, &row.Direction,
			&row.CounterpartyName, &counterpartyWalletStr,
			&row.AmountCents, &row.Reference,
			&createdAt, &settledAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan transaction row: %w", err)
		}
		row.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if settledAt != nil {
			s := settledAt.UTC().Format(time.RFC3339)
			row.SettledAt = &s
		}
		if counterpartyWalletStr != nil {
			row.CounterpartyWallet = counterpartyWalletStr
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("transaction rows error: %w", err)
	}

	if result == nil {
		result = []TransactionRow{} // return [] not null in JSON
	}
	return result, total, nil
}
