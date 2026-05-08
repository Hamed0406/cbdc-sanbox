package wallet

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/pkg/response"
)

// walletService is the interface the handler depends on.
// Using an interface (not *Service) lets tests inject a mock without any DB.
type walletService interface {
	GetWallet(ctx context.Context, walletID, requesterUserID uuid.UUID, requesterRole string) (*WalletResponse, error)
	GetBalance(ctx context.Context, walletID, requesterUserID uuid.UUID, requesterRole string) (*BalanceResponse, error)
	GetTransactions(ctx context.Context, walletID, requesterUserID uuid.UUID, requesterRole string, p ListParams) (*TransactionListResponse, error)
}

// Handler wires wallet HTTP routes.
type Handler struct {
	svc walletService
}

// NewHandler creates a new wallet Handler.
func NewHandler(svc walletService) *Handler {
	return &Handler{svc: svc}
}

// Routes registers all wallet routes on a chi sub-router.
// Expected to be mounted at /api/v1/wallets.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{id}", h.getWallet)
	r.Get("/{id}/balance", h.getBalance)
	r.Get("/{id}/transactions", h.getTransactions)
	return r
}

// getWallet handles GET /api/v1/wallets/{id}
func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())
	walletID, ok := parseWalletID(w, r, requestID)
	if !ok {
		return
	}

	requesterID, requesterRole, ok := requesterFromContext(w, r, requestID)
	if !ok {
		return
	}

	wallet, err := h.svc.GetWallet(r.Context(), walletID, requesterID, requesterRole)
	if err != nil {
		h.handleError(w, err, requestID)
		return
	}

	response.OK(w, wallet)
}

// getBalance handles GET /api/v1/wallets/{id}/balance
func (h *Handler) getBalance(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())
	walletID, ok := parseWalletID(w, r, requestID)
	if !ok {
		return
	}

	requesterID, requesterRole, ok := requesterFromContext(w, r, requestID)
	if !ok {
		return
	}

	bal, err := h.svc.GetBalance(r.Context(), walletID, requesterID, requesterRole)
	if err != nil {
		h.handleError(w, err, requestID)
		return
	}

	response.OK(w, bal)
}

// getTransactions handles GET /api/v1/wallets/{id}/transactions
func (h *Handler) getTransactions(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())
	walletID, ok := parseWalletID(w, r, requestID)
	if !ok {
		return
	}

	requesterID, requesterRole, ok := requesterFromContext(w, r, requestID)
	if !ok {
		return
	}

	params, ok := parseListParams(w, r, requestID)
	if !ok {
		return
	}

	list, err := h.svc.GetTransactions(r.Context(), walletID, requesterID, requesterRole, params)
	if err != nil {
		h.handleError(w, err, requestID)
		return
	}

	response.OK(w, list)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// parseWalletID extracts and validates the {id} URL param.
func parseWalletID(w http.ResponseWriter, r *http.Request, requestID string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "id")
	id, err := uuid.Parse(raw)
	if err != nil {
		response.BadRequest(w, "Invalid wallet ID format", requestID)
		return uuid.Nil, false
	}
	return id, true
}

// requesterFromContext extracts the authenticated user's ID and role from context.
// These are injected by the Authenticate middleware.
func requesterFromContext(w http.ResponseWriter, r *http.Request, requestID string) (uuid.UUID, string, bool) {
	userIDStr, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Unauthorized(w, requestID)
		return uuid.Nil, "", false
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.Unauthorized(w, requestID)
		return uuid.Nil, "", false
	}
	role, _ := middleware.GetUserRole(r.Context())
	return userID, role, true
}

// parseListParams reads and validates query parameters for the transaction list.
func parseListParams(w http.ResponseWriter, r *http.Request, requestID string) (ListParams, bool) {
	p := ListParams{Page: 1, Limit: 20}
	q := r.URL.Query()

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			response.BadRequest(w, "page must be a positive integer", requestID)
			return p, false
		}
		p.Page = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			response.BadRequest(w, "limit must be between 1 and 100", requestID)
			return p, false
		}
		p.Limit = n
	}

	p.Type = q.Get("type")
	p.Status = q.Get("status")

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.BadRequest(w, "from must be ISO8601 format (e.g. 2026-01-01T00:00:00Z)", requestID)
			return p, false
		}
		p.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.BadRequest(w, "to must be ISO8601 format (e.g. 2026-12-31T23:59:59Z)", requestID)
			return p, false
		}
		p.To = &t
	}

	return p, true
}

// handleError maps domain errors to HTTP responses.
func (h *Handler) handleError(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, ErrWalletNotFound):
		response.NotFound(w, "wallet", requestID)
	case errors.Is(err, ErrAccessDenied):
		response.Forbidden(w, requestID)
	default:
		response.InternalError(w, requestID)
	}
}
