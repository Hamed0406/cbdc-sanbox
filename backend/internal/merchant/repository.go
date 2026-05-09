package merchant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cbdc-simulator/backend/pkg/database"
)

// Repository handles all merchant and payment_request DB operations.
type Repository struct {
	db *database.Pool
}

// NewRepository creates a new merchant Repository.
func NewRepository(db *database.Pool) *Repository {
	return &Repository{db: db}
}

// FindByUserID fetches the merchant profile for a given user, or ErrMerchantNotFound.
func (r *Repository) FindByUserID(ctx context.Context, userID uuid.UUID) (*Merchant, error) {
	m := &Merchant{}
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, business_name, business_type, webhook_url,
		       api_key_prefix, is_active, created_at, updated_at
		FROM merchants
		WHERE user_id = $1
	`, userID).Scan(
		&m.ID, &m.UserID, &m.BusinessName, &m.BusinessType, &m.WebhookURL,
		&m.APIKeyPrefix, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantNotFound
		}
		return nil, fmt.Errorf("find merchant by user_id: %w", err)
	}
	return m, nil
}

// FindByID fetches a merchant by its primary key.
func (r *Repository) FindByID(ctx context.Context, merchantID uuid.UUID) (*Merchant, error) {
	m := &Merchant{}
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, business_name, business_type, webhook_url,
		       api_key_prefix, is_active, created_at, updated_at
		FROM merchants
		WHERE id = $1
	`, merchantID).Scan(
		&m.ID, &m.UserID, &m.BusinessName, &m.BusinessType, &m.WebhookURL,
		&m.APIKeyPrefix, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantNotFound
		}
		return nil, fmt.Errorf("find merchant by id: %w", err)
	}
	return m, nil
}

// CreateMerchantParams holds the values needed to insert a new merchant row.
type CreateMerchantParams struct {
	UserID       uuid.UUID
	BusinessName string
	BusinessType *string
	WebhookURL   *string
	APIKeyHash   string // SHA-256 of the raw API key, stored in DB
	APIKeyPrefix string // first 8 chars of the raw key, safe to show in UI
}

// Create inserts a new merchant profile and returns it.
func (r *Repository) Create(ctx context.Context, p CreateMerchantParams) (*Merchant, error) {
	m := &Merchant{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO merchants (user_id, business_name, business_type, webhook_url, api_key_hash, api_key_prefix)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, business_name, business_type, webhook_url,
		          api_key_prefix, is_active, created_at, updated_at
	`,
		p.UserID, p.BusinessName, p.BusinessType, p.WebhookURL, p.APIKeyHash, p.APIKeyPrefix,
	).Scan(
		&m.ID, &m.UserID, &m.BusinessName, &m.BusinessType, &m.WebhookURL,
		&m.APIKeyPrefix, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, ErrMerchantAlreadyExists
		}
		return nil, fmt.Errorf("create merchant: %w", err)
	}
	return m, nil
}

// CreatePaymentRequestParams holds inputs for inserting a new payment_request row.
type CreatePaymentRequestParams struct {
	MerchantID  uuid.UUID
	AmountCents int64
	Currency    string
	Reference   *string
	Description *string
	QRPayload   string
	ExpiresAt   time.Time
}

// CreatePaymentRequest inserts a new payment request and returns it.
func (r *Repository) CreatePaymentRequest(ctx context.Context, p CreatePaymentRequestParams) (*PaymentRequest, error) {
	pr := &PaymentRequest{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO payment_requests
		    (merchant_id, amount_cents, currency, reference, description, qr_payload, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, merchant_id, amount_cents, currency, reference, description,
		          qr_payload, status, paid_by_wallet_id, transaction_id,
		          expires_at, created_at, paid_at
	`,
		p.MerchantID, p.AmountCents, p.Currency, p.Reference, p.Description, p.QRPayload, p.ExpiresAt,
	).Scan(
		&pr.ID, &pr.MerchantID, &pr.AmountCents, &pr.Currency, &pr.Reference, &pr.Description,
		&pr.QRPayload, &pr.Status, &pr.PaidByWalletID, &pr.TransactionID,
		&pr.ExpiresAt, &pr.CreatedAt, &pr.PaidAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create payment request: %w", err)
	}
	return pr, nil
}

// FindPaymentRequestByID fetches a payment request by primary key.
func (r *Repository) FindPaymentRequestByID(ctx context.Context, id uuid.UUID) (*PaymentRequest, error) {
	pr := &PaymentRequest{}
	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, amount_cents, currency, reference, description,
		       qr_payload, status, paid_by_wallet_id, transaction_id,
		       expires_at, created_at, paid_at
		FROM payment_requests
		WHERE id = $1
	`, id).Scan(
		&pr.ID, &pr.MerchantID, &pr.AmountCents, &pr.Currency, &pr.Reference, &pr.Description,
		&pr.QRPayload, &pr.Status, &pr.PaidByWalletID, &pr.TransactionID,
		&pr.ExpiresAt, &pr.CreatedAt, &pr.PaidAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentRequestNotFound
		}
		return nil, fmt.Errorf("find payment request: %w", err)
	}
	return pr, nil
}

// MarkPaid atomically transitions a PENDING payment request to PAID.
// Uses a conditional UPDATE to prevent double-payment races.
func (r *Repository) MarkPaid(ctx context.Context, id, paidByWalletID, transactionID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE payment_requests
		SET status = 'PAID',
		    paid_by_wallet_id = $2,
		    transaction_id = $3,
		    paid_at = NOW()
		WHERE id = $1 AND status = 'PENDING' AND expires_at > NOW()
	`, id, paidByWalletID, transactionID)
	if err != nil {
		return fmt.Errorf("mark payment request paid: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either already paid/cancelled/expired, or the request ID doesn't exist.
		// Re-fetch to return the precise error.
		pr, fetchErr := r.FindPaymentRequestByID(ctx, id)
		if fetchErr != nil {
			return ErrPaymentRequestNotFound
		}
		switch pr.Status {
		case StatusPaid:
			return ErrPaymentRequestAlreadyPaid
		case StatusExpired:
			return ErrPaymentRequestExpired
		case StatusCancelled:
			return ErrPaymentRequestCancelled
		default:
			return ErrPaymentRequestExpired
		}
	}
	return nil
}

// CancelPaymentRequest transitions a PENDING request to CANCELLED.
// Only the owning merchant can cancel their own requests.
func (r *Repository) CancelPaymentRequest(ctx context.Context, id, merchantID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE payment_requests
		SET status = 'CANCELLED'
		WHERE id = $1 AND merchant_id = $2 AND status = 'PENDING'
	`, id, merchantID)
	if err != nil {
		return fmt.Errorf("cancel payment request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentRequestNotFound
	}
	return nil
}

// ListParams holds pagination + filter options for listing payment requests.
type ListParams struct {
	Page   int
	Limit  int
	Status string // optional filter
}

// ListByMerchant returns paginated payment requests for a merchant, newest first.
func (r *Repository) ListByMerchant(ctx context.Context, merchantID uuid.UUID, p ListParams) ([]PaymentRequest, int, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Limit < 1 || p.Limit > 100 {
		p.Limit = 20
	}
	offset := (p.Page - 1) * p.Limit

	args := []any{merchantID}
	statusFilter := ""
	if p.Status != "" {
		args = append(args, p.Status)
		statusFilter = fmt.Sprintf("AND status = $%d::payment_request_status", len(args))
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM payment_requests
		WHERE merchant_id = $1 %s
	`, statusFilter)

	var total int
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count payment requests: %w", err)
	}

	args = append(args, p.Limit, offset)
	query := fmt.Sprintf(`
		SELECT id, merchant_id, amount_cents, currency, reference, description,
		       qr_payload, status, paid_by_wallet_id, transaction_id,
		       expires_at, created_at, paid_at
		FROM payment_requests
		WHERE merchant_id = $1 %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, statusFilter, len(args)-1, len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list payment requests: %w", err)
	}
	defer rows.Close()

	var results []PaymentRequest
	for rows.Next() {
		var pr PaymentRequest
		if err := rows.Scan(
			&pr.ID, &pr.MerchantID, &pr.AmountCents, &pr.Currency, &pr.Reference, &pr.Description,
			&pr.QRPayload, &pr.Status, &pr.PaidByWalletID, &pr.TransactionID,
			&pr.ExpiresAt, &pr.CreatedAt, &pr.PaidAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan payment request: %w", err)
		}
		results = append(results, pr)
	}
	return results, total, rows.Err()
}
