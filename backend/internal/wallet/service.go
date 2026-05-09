package wallet

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/pkg/currency"
)

// repository defines the data access methods the Service needs.
// Using an interface lets tests inject a mock without a real database.
type repository interface {
	FindByID(ctx context.Context, walletID uuid.UUID) (*Wallet, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) (*Wallet, error)
	GetTransactionHistory(ctx context.Context, walletID uuid.UUID, p ListParams) ([]TransactionRow, int, error)
	Search(ctx context.Context, query string, limit int) ([]WalletSearchResult, error)
}

// Service contains wallet business logic.
type Service struct {
	repo repository
}

// NewService creates a new wallet Service.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// GetWallet returns full wallet details.
// Users can only see their own wallet; admins can see any.
func (s *Service) GetWallet(ctx context.Context, walletID, requesterUserID uuid.UUID, requesterRole string) (*WalletResponse, error) {
	w, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if err := authorize(w, requesterUserID, requesterRole); err != nil {
		return nil, err
	}
	return toWalletResponse(w), nil
}

// GetBalance returns a lightweight balance snapshot.
// More cache-friendly than GetWallet (smaller payload, easier to cache at CDN).
func (s *Service) GetBalance(ctx context.Context, walletID, requesterUserID uuid.UUID, requesterRole string) (*BalanceResponse, error) {
	w, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if err := authorize(w, requesterUserID, requesterRole); err != nil {
		return nil, err
	}
	return &BalanceResponse{
		WalletID:       w.ID.String(),
		BalanceCents:   w.Balance,
		BalanceDisplay: currency.Format(w.Balance, w.Currency),
		IsFrozen:       w.IsFrozen,
		AsOf:           time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// GetTransactions returns paginated transaction history for a wallet.
func (s *Service) GetTransactions(ctx context.Context, walletID, requesterUserID uuid.UUID, requesterRole string, p ListParams) (*TransactionListResponse, error) {
	w, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if err := authorize(w, requesterUserID, requesterRole); err != nil {
		return nil, err
	}

	// Clamp page/limit to sane defaults
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Limit < 1 || p.Limit > 100 {
		p.Limit = 20
	}

	rows, total, err := s.repo.GetTransactionHistory(ctx, walletID, p)
	if err != nil {
		return nil, err
	}

	// Populate AmountDisplay for each row
	for i := range rows {
		rows[i].AmountDisplay = currency.Format(rows[i].AmountCents, "DD$")
	}

	pages := (total + p.Limit - 1) / p.Limit
	if pages == 0 {
		pages = 1
	}

	return &TransactionListResponse{
		Transactions: rows,
		Pagination: Pagination{
			Page:  p.Page,
			Limit: p.Limit,
			Total: total,
			Pages: pages,
		},
	}, nil
}

// SearchWallets returns wallets matching a name or email query.
// Balances are included since this endpoint is authenticated — only logged-in
// users can call it, and seeing a balance next to a name helps confirm the right recipient.
// Admin wallets are excluded: admins don't have balances and shouldn't be payment targets.
func (s *Service) SearchWallets(ctx context.Context, query string) ([]WalletSearchResult, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return []WalletSearchResult{}, nil
	}
	return s.repo.Search(ctx, query, 20)
}

// authorize checks that the requester is allowed to access a wallet.
// Rule: users see only their own wallet; admins see all.
func authorize(w *Wallet, requesterUserID uuid.UUID, requesterRole string) error {
	if requesterRole == "admin" {
		return nil // admins have unrestricted read access
	}
	if w.UserID != requesterUserID {
		return ErrAccessDenied
	}
	return nil
}

// toWalletResponse converts the domain Wallet to the API response DTO.
func toWalletResponse(w *Wallet) *WalletResponse {
	return &WalletResponse{
		ID:             w.ID.String(),
		UserID:         w.UserID.String(),
		Currency:       w.Currency,
		BalanceCents:   w.Balance,
		BalanceDisplay: currency.Format(w.Balance, w.Currency),
		IsFrozen:       w.IsFrozen,
		CreatedAt:      w.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      w.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
