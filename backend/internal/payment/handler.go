package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/ledger"
	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/pkg/response"
)

// paymentService is the interface the handler depends on.
type paymentService interface {
	Send(ctx context.Context, req SendRequest, senderWalletID, userID uuid.UUID, idempotencyKey, ip string) (*SendResponse, error)
	GetTransaction(ctx context.Context, txnID, walletID uuid.UUID) (*TransactionDetail, error)
	ListTransactions(ctx context.Context, walletID uuid.UUID, p ListParams) (*PaymentListResponse, error)
}

// Handler wires payment HTTP routes.
type Handler struct {
	svc paymentService
}

// NewHandler creates a new payment Handler.
func NewHandler(svc paymentService) *Handler {
	return &Handler{svc: svc}
}

// Routes registers all payment routes on a chi sub-router.
// Expected to be mounted at /api/v1/payments behind Authenticate middleware.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/send", h.send)
	r.Get("/", h.list)
	r.Get("/{id}", h.getByID)
	return r
}

// send handles POST /api/v1/payments/send
// @Summary      Send CBDC payment
// @Description  Transfers DD$ from the authenticated user's wallet to another wallet.
// @Tags         payments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        X-Idempotency-Key  header  string       true  "Unique key to prevent duplicate payments"
// @Param        body               body    SendRequest  true  "Payment details"
// @Success      201  {object}  SendResponse
// @Failure      400  {object}  response.ErrorPayload
// @Failure      401  {object}  response.ErrorPayload
// @Failure      422  {object}  response.ErrorPayload
// @Router       /payments/send [post]
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())

	userIDStr, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Unauthorized(w, requestID)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.Unauthorized(w, requestID)
		return
	}

	walletIDStr, ok := middleware.GetWalletID(r.Context())
	if !ok {
		response.Unauthorized(w, requestID)
		return
	}
	senderWalletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		response.Unauthorized(w, requestID)
		return
	}

	idempotencyKey := r.Header.Get("X-Idempotency-Key")

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Invalid JSON body", requestID)
		return
	}

	result, err := h.svc.Send(r.Context(), req, senderWalletID, userID, idempotencyKey, clientIP(r))
	if err != nil {
		h.handleError(w, err, requestID)
		return
	}

	response.Created(w, result)
}

// getByID handles GET /api/v1/payments/{id}
// @Summary      Get transaction by ID
// @Description  Returns a single transaction. The authenticated user must be the sender or receiver.
// @Tags         payments
// @Security     BearerAuth
// @Produce      json
// @Param        id  path  string  true  "Transaction UUID"
// @Success      200  {object}  TransactionDetail
// @Failure      401  {object}  response.ErrorPayload
// @Failure      404  {object}  response.ErrorPayload
// @Router       /payments/{id} [get]
func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())

	walletIDStr, ok := middleware.GetWalletID(r.Context())
	if !ok {
		response.Unauthorized(w, requestID)
		return
	}
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		response.Unauthorized(w, requestID)
		return
	}

	txnIDStr := chi.URLParam(r, "id")
	txnID, err := uuid.Parse(txnIDStr)
	if err != nil {
		response.NotFound(w, "transaction", requestID)
		return
	}

	d, err := h.svc.GetTransaction(r.Context(), txnID, walletID)
	if err != nil {
		h.handleError(w, err, requestID)
		return
	}

	response.OK(w, d)
}

// list handles GET /api/v1/payments/
// @Summary      List transactions
// @Description  Returns paginated payment history for the authenticated user's wallet.
// @Tags         payments
// @Security     BearerAuth
// @Produce      json
// @Param        page    query  int     false  "Page number (default 1)"
// @Param        limit   query  int     false  "Items per page (default 20, max 100)"
// @Param        type    query  string  false  "Filter by type: TRANSFER, ISSUANCE, REFUND"
// @Param        status  query  string  false  "Filter by status: SETTLED, PENDING"
// @Param        from    query  string  false  "Filter from date (RFC3339)"
// @Param        to      query  string  false  "Filter to date (RFC3339)"
// @Success      200  {object}  PaymentListResponse
// @Failure      401  {object}  response.ErrorPayload
// @Router       /payments/ [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetRequestID(r.Context())

	walletIDStr, ok := middleware.GetWalletID(r.Context())
	if !ok {
		response.Unauthorized(w, requestID)
		return
	}
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		response.Unauthorized(w, requestID)
		return
	}

	p, err := parseListParams(r)
	if err != nil {
		response.BadRequest(w, err.Error(), requestID)
		return
	}

	result, err := h.svc.ListTransactions(r.Context(), walletID, p)
	if err != nil {
		response.InternalError(w, requestID)
		return
	}

	response.OK(w, result)
}

// handleError maps domain errors to HTTP status codes.
func (h *Handler) handleError(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, ErrSelfPayment):
		response.UnprocessableEntity(w, "SELF_PAYMENT", "Cannot send payment to your own wallet.", requestID)
	case errors.Is(err, ErrAmountZero):
		response.UnprocessableEntity(w, "INVALID_AMOUNT", "Amount must be greater than zero.", requestID)
	case errors.Is(err, ErrReferenceTooLong):
		response.UnprocessableEntity(w, "REFERENCE_TOO_LONG", "Reference must be 256 characters or fewer.", requestID)
	case errors.Is(err, ErrMissingIdempotencyKey):
		response.BadRequest(w, "X-Idempotency-Key header is required", requestID)
	case errors.Is(err, ErrTransactionNotFound):
		response.NotFound(w, "transaction", requestID)
	case errors.Is(err, ledger.ErrWalletNotFound):
		response.NotFound(w, "wallet", requestID)
	case errors.Is(err, ledger.ErrWalletFrozen):
		response.UnprocessableEntity(w, "WALLET_FROZEN", "One or both wallets are frozen.", requestID)
	case errors.Is(err, ledger.ErrInsufficientFunds):
		response.UnprocessableEntity(w, "INSUFFICIENT_FUNDS", "Insufficient funds for this transfer.", requestID)
	default:
		response.InternalError(w, requestID)
	}
}

// parseListParams extracts and validates pagination + filter query params.
func parseListParams(r *http.Request) (ListParams, error) {
	q := r.URL.Query()

	page := 1
	if s := q.Get("page"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return ListParams{}, errors.New("page must be a positive integer")
		}
		page = n
	}

	limit := 20
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			return ListParams{}, errors.New("limit must be between 1 and 100")
		}
		limit = n
	}

	p := ListParams{
		Page:   page,
		Limit:  limit,
		Type:   q.Get("type"),
		Status: q.Get("status"),
	}

	if s := q.Get("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return ListParams{}, errors.New("from must be a valid RFC3339 datetime")
		}
		p.From = &t
	}
	if s := q.Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return ListParams{}, errors.New("to must be a valid RFC3339 datetime")
		}
		p.To = &t
	}

	return p, nil
}

// clientIP extracts the bare IP address from r.RemoteAddr (strips the port).
func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
