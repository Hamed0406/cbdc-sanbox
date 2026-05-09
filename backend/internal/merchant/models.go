// Package merchant handles QR-code-based payment requests for merchant users.
// Flow: merchant creates PaymentRequest → QR code displayed → customer scans
// and pays → webhook fired to merchant's webhook_url.
package merchant

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Domain errors.
var (
	ErrMerchantNotFound        = errors.New("merchant profile not found")
	ErrMerchantAlreadyExists   = errors.New("merchant profile already exists for this user")
	ErrMerchantInactive        = errors.New("merchant account is inactive")
	ErrPaymentRequestNotFound  = errors.New("payment request not found")
	ErrPaymentRequestExpired   = errors.New("payment request has expired")
	ErrPaymentRequestAlreadyPaid = errors.New("payment request already paid")
	ErrPaymentRequestCancelled = errors.New("payment request has been cancelled")
	ErrAmountZero              = errors.New("amount must be greater than zero")
	ErrDescriptionTooLong      = errors.New("description must be 500 characters or fewer")
	ErrReferenceTooLong        = errors.New("reference must be 64 characters or fewer")
	ErrNotMerchant             = errors.New("user does not have merchant role")
	ErrSelfPayment             = errors.New("merchant cannot pay their own payment request")
)

// PaymentRequestStatus mirrors the payment_request_status DB enum.
type PaymentRequestStatus string

const (
	StatusPending   PaymentRequestStatus = "PENDING"
	StatusPaid      PaymentRequestStatus = "PAID"
	StatusExpired   PaymentRequestStatus = "EXPIRED"
	StatusCancelled PaymentRequestStatus = "CANCELLED"
)

// DefaultExpirySeconds is the TTL for new payment requests (15 minutes).
const DefaultExpirySeconds = 900

// Merchant maps to a row in the merchants table.
type Merchant struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	BusinessName   string
	BusinessType   *string
	WebhookURL     *string
	APIKeyPrefix   string  // first 8 chars, safe to display
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PaymentRequest maps to a row in the payment_requests table.
type PaymentRequest struct {
	ID               uuid.UUID
	MerchantID       uuid.UUID
	AmountCents      int64
	Currency         string
	Reference        *string
	Description      *string
	QRPayload        string // full cbdc:// URI
	Status           PaymentRequestStatus
	PaidByWalletID   *uuid.UUID
	TransactionID    *uuid.UUID
	ExpiresAt        time.Time
	CreatedAt        time.Time
	PaidAt           *time.Time
}

// RegisterRequest is the body for POST /api/v1/merchant/register.
type RegisterRequest struct {
	BusinessName string `json:"business_name"`
	BusinessType string `json:"business_type"` // optional
	WebhookURL   string `json:"webhook_url"`   // optional
}

// Validate checks register inputs before hitting the DB.
func (r *RegisterRequest) Validate() error {
	r.BusinessName = strings.TrimSpace(r.BusinessName)
	r.BusinessType = strings.TrimSpace(r.BusinessType)
	r.WebhookURL = strings.TrimSpace(r.WebhookURL)
	if r.BusinessName == "" {
		return errors.New("business_name is required")
	}
	if len(r.BusinessName) > 200 {
		return errors.New("business_name must be 200 characters or fewer")
	}
	return nil
}

// CreatePaymentRequestInput is the body for POST /api/v1/merchant/payment-requests.
type CreatePaymentRequestInput struct {
	AmountCents int64  `json:"amount_cents"`
	Reference   string `json:"reference"`   // merchant's order ID, optional, max 64
	Description string `json:"description"` // customer-facing text, optional, max 500
	ExpirySecs  int    `json:"expiry_seconds"` // 0 = use default (900)
}

// Validate checks create-request inputs.
func (r *CreatePaymentRequestInput) Validate() error {
	r.Reference = strings.TrimSpace(r.Reference)
	r.Description = strings.TrimSpace(r.Description)
	if r.AmountCents <= 0 {
		return ErrAmountZero
	}
	if len(r.Description) > 500 {
		return ErrDescriptionTooLong
	}
	if len(r.Reference) > 64 {
		return ErrReferenceTooLong
	}
	if r.ExpirySecs <= 0 {
		r.ExpirySecs = DefaultExpirySeconds
	}
	return nil
}

// PayQRRequest is the body for POST /api/v1/payments/qr.
type PayQRRequest struct {
	QRPayload string `json:"qr_payload"` // full cbdc:// URI from the QR code
}

// MerchantProfile is the JSON representation of a merchant for GET /profile.
type MerchantProfile struct {
	ID           string  `json:"id"`
	BusinessName string  `json:"business_name"`
	BusinessType *string `json:"business_type"`
	WebhookURL   *string `json:"webhook_url"`
	APIKeyPrefix string  `json:"api_key_prefix"`
	IsActive     bool    `json:"is_active"`
	CreatedAt    string  `json:"created_at"`
}

// PaymentRequestDetail is the full JSON view of a payment request.
type PaymentRequestDetail struct {
	ID           string               `json:"id"`
	MerchantID   string               `json:"merchant_id"`
	BusinessName string               `json:"business_name"`
	AmountCents  int64                `json:"amount_cents"`
	AmountDisplay string              `json:"amount_display"`
	Currency     string               `json:"currency"`
	Reference    *string              `json:"reference"`
	Description  *string              `json:"description"`
	QRPayload    string               `json:"qr_payload"`
	Status       PaymentRequestStatus `json:"status"`
	ExpiresAt    string               `json:"expires_at"`
	CreatedAt    string               `json:"created_at"`
	PaidAt       *string              `json:"paid_at"`
}

// PaymentRequestListResponse wraps paginated payment request history.
type PaymentRequestListResponse struct {
	PaymentRequests []PaymentRequestDetail `json:"payment_requests"`
	Pagination      Pagination             `json:"pagination"`
}

// Pagination metadata.
type Pagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
	Pages int `json:"pages"`
}

// PayQRResponse is returned after a successful QR payment.
type PayQRResponse struct {
	TransactionID  string `json:"transaction_id"`
	AmountCents    int64  `json:"amount_cents"`
	AmountDisplay  string `json:"amount_display"`
	MerchantName   string `json:"merchant_name"`
	Reference      string `json:"reference"`
	NewBalanceCents int64 `json:"new_balance_cents"`
	NewBalanceDisplay string `json:"new_balance_display"`
}
