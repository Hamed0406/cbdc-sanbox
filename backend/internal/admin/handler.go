package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/pkg/response"
)

// adminService is the interface the handler depends on.
// Using an interface lets tests inject a mock without any DB or Redis.
type adminService interface {
	IssueCBDC(ctx context.Context, req IssueRequest, adminID uuid.UUID, idempotencyKey, ip string) (*IssuanceResponse, error)
}

// Handler wires admin HTTP routes.
type Handler struct {
	svc adminService
}

// NewHandler creates a new admin Handler.
func NewHandler(svc adminService) *Handler {
	return &Handler{svc: svc}
}

// Routes registers all admin routes on a chi sub-router.
// Expected to be mounted at /api/v1/admin behind RequireAdmin middleware.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/issue-cbdc", h.issueCBDC)
	return r
}

// issueCBDC handles POST /api/v1/admin/issue-cbdc
// @Summary      Issue CBDC
// @Description  Mints new DD$ into a target wallet. Admin only. Requires X-Idempotency-Key.
// @Tags         admin
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        X-Idempotency-Key  header  string        true  "Unique key to prevent duplicate issuance"
// @Param        body               body    IssueRequest  true  "Issuance details"
// @Success      201  {object}  IssuanceResponse
// @Failure      400  {object}  response.ErrorPayload
// @Failure      401  {object}  response.ErrorPayload
// @Failure      403  {object}  response.ErrorPayload
// @Failure      422  {object}  response.ErrorPayload
// @Router       /admin/issue-cbdc [post]
func (h *Handler) issueCBDC(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())

	// Extract authenticated admin ID from context (set by Authenticate middleware)
	adminIDStr, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Unauthorized(w, requestID)
		return
	}
	adminID, err := uuid.Parse(adminIDStr)
	if err != nil {
		response.Unauthorized(w, requestID)
		return
	}

	// Idempotency key is REQUIRED for issuance — prevents accidental double-mint
	idempotencyKey := r.Header.Get("X-Idempotency-Key")

	var req IssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Invalid JSON body", requestID)
		return
	}

	ip := clientIP(r)

	result, err := h.svc.IssueCBDC(r.Context(), req, adminID, idempotencyKey, ip)
	if err != nil {
		h.handleError(w, err, requestID)
		return
	}

	response.Created(w, result)
}

// handleError maps domain errors to HTTP responses.
func (h *Handler) handleError(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, ErrAmountZero):
		response.UnprocessableEntity(w, "INVALID_AMOUNT", "Amount must be greater than zero.", requestID)
	case errors.Is(err, ErrAmountTooLarge):
		response.UnprocessableEntity(w, "AMOUNT_TOO_LARGE", "Amount exceeds the maximum issuance limit of DD$1,000,000 per action.", requestID)
	case errors.Is(err, ErrReasonTooShort):
		response.UnprocessableEntity(w, "INVALID_REASON", "Reason must be at least 10 characters.", requestID)
	case errors.Is(err, ErrMissingIdempotencyKey):
		response.BadRequest(w, "X-Idempotency-Key header is required", requestID)
	case errors.Is(err, ledger.ErrWalletNotFound):
		response.NotFound(w, "wallet", requestID)
	case errors.Is(err, ledger.ErrWalletFrozen):
		response.UnprocessableEntity(w, "WALLET_FROZEN", "Cannot issue CBDC to a frozen wallet.", requestID)
	default:
		response.InternalError(w, requestID)
	}
}

// clientIP extracts the bare IP address from r.RemoteAddr (strips the port).
// PostgreSQL's inet type rejects "ip:port" strings.
func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
