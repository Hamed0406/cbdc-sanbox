package merchant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cbdc-simulator/backend/internal/middleware"
	"github.com/cbdc-simulator/backend/pkg/response"
)

// merchantService is the interface the HTTP handler depends on.
type merchantService interface {
	Register(ctx context.Context, userID uuid.UUID, req *RegisterRequest) (*MerchantProfile, string, error)
	GetProfile(ctx context.Context, userID uuid.UUID) (*MerchantProfile, error)
	CreatePaymentRequest(ctx context.Context, userID uuid.UUID, input *CreatePaymentRequestInput) (*PaymentRequestDetail, error)
	GetPaymentRequest(ctx context.Context, userID uuid.UUID, requestID uuid.UUID) (*PaymentRequestDetail, error)
	ListPaymentRequests(ctx context.Context, userID uuid.UUID, p ListParams) (*PaymentRequestListResponse, error)
	CancelPaymentRequest(ctx context.Context, userID uuid.UUID, requestID uuid.UUID) error
	PayViaQR(ctx context.Context, payerUserID uuid.UUID, payerWalletID uuid.UUID, idempotencyKey string, req *PayQRRequest) (*PayQRResponse, error)
}

// Handler wires merchant HTTP routes.
type Handler struct {
	svc merchantService
}

// NewHandler creates a new merchant Handler.
func NewHandler(svc merchantService) *Handler {
	return &Handler{svc: svc}
}

// MerchantRoutes returns routes requiring RequireMerchant middleware (mounted at /merchant).
func (h *Handler) MerchantRoutes() chi.Router {
	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Get("/profile", h.getProfile)
	r.Post("/payment-requests", h.createPaymentRequest)
	r.Get("/payment-requests", h.listPaymentRequests)
	r.Get("/payment-requests/{id}", h.getPaymentRequest)
	r.Delete("/payment-requests/{id}", h.cancelPaymentRequest)
	return r
}

// QRPayRoute returns the single QR-pay route for customer wallets (any authenticated user).
func (h *Handler) QRPayRoute() http.HandlerFunc {
	return h.payViaQR
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid JSON body", rid)
		return
	}
	if err := req.Validate(); err != nil {
		response.UnprocessableEntity(w, "VALIDATION_ERROR", err.Error(), rid)
		return
	}

	profile, rawKey, err := h.svc.Register(r.Context(), userID, &req)
	if err != nil {
		switch {
		case errors.Is(err, ErrMerchantAlreadyExists):
			response.Conflict(w, "ALREADY_EXISTS", err.Error(), rid)
		default:
			response.InternalError(w, rid)
		}
		return
	}

	// Return the raw API key ONLY once — it won't be shown again (only hash is stored).
	response.Created(w, map[string]any{
		"merchant": profile,
		"api_key":  rawKey,
		"warning":  "Store this API key securely — it will not be shown again.",
	})
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	profile, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrMerchantNotFound) {
			response.NotFound(w, "merchant profile", rid)
			return
		}
		response.InternalError(w, rid)
		return
	}

	response.OK(w, profile)
}

func (h *Handler) createPaymentRequest(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	var input CreatePaymentRequestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid JSON body", rid)
		return
	}
	if err := input.Validate(); err != nil {
		response.UnprocessableEntity(w, "VALIDATION_ERROR", err.Error(), rid)
		return
	}

	detail, err := h.svc.CreatePaymentRequest(r.Context(), userID, &input)
	if err != nil {
		switch {
		case errors.Is(err, ErrMerchantNotFound):
			response.NotFound(w, "merchant profile", rid)
		case errors.Is(err, ErrMerchantInactive):
			response.Forbidden(w, rid)
		default:
			response.InternalError(w, rid)
		}
		return
	}

	response.Created(w, detail)
}

func (h *Handler) listPaymentRequests(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	status := r.URL.Query().Get("status")

	result, err := h.svc.ListPaymentRequests(r.Context(), userID, ListParams{
		Page:   page,
		Limit:  limit,
		Status: status,
	})
	if err != nil {
		if errors.Is(err, ErrMerchantNotFound) {
			response.NotFound(w, "merchant profile", rid)
			return
		}
		response.InternalError(w, rid)
		return
	}

	response.OK(w, result)
}

func (h *Handler) getPaymentRequest(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	reqID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid payment request ID", rid)
		return
	}

	detail, err := h.svc.GetPaymentRequest(r.Context(), userID, reqID)
	if err != nil {
		if errors.Is(err, ErrPaymentRequestNotFound) || errors.Is(err, ErrMerchantNotFound) {
			response.NotFound(w, "payment request", rid)
			return
		}
		response.InternalError(w, rid)
		return
	}

	response.OK(w, detail)
}

func (h *Handler) cancelPaymentRequest(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	reqID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid payment request ID", rid)
		return
	}

	if err := h.svc.CancelPaymentRequest(r.Context(), userID, reqID); err != nil {
		if errors.Is(err, ErrPaymentRequestNotFound) || errors.Is(err, ErrMerchantNotFound) {
			response.NotFound(w, "payment request", rid)
			return
		}
		response.InternalError(w, rid)
		return
	}

	response.NoContent(w)
}

func (h *Handler) payViaQR(w http.ResponseWriter, r *http.Request) {
	rid := middleware.GetRequestID(r.Context())

	userID, ok := parseUserID(r, w, rid)
	if !ok {
		return
	}

	walletIDStr, ok := middleware.GetWalletID(r.Context())
	if !ok {
		response.Unauthorized(w, rid)
		return
	}
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		response.Unauthorized(w, rid)
		return
	}

	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	if idempotencyKey == "" {
		response.UnprocessableEntity(w, "MISSING_IDEMPOTENCY_KEY", "X-Idempotency-Key header is required", rid)
		return
	}

	var req PayQRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid JSON body", rid)
		return
	}
	if req.QRPayload == "" {
		response.UnprocessableEntity(w, "VALIDATION_ERROR", "qr_payload is required", rid)
		return
	}

	result, err := h.svc.PayViaQR(r.Context(), userID, walletID, idempotencyKey, &req)
	if err != nil {
		switch {
		case errors.Is(err, ErrPaymentRequestNotFound) || errors.Is(err, ErrMerchantNotFound):
			response.NotFound(w, "payment request", rid)
		case errors.Is(err, ErrPaymentRequestAlreadyPaid):
			response.Conflict(w, "ALREADY_PAID", err.Error(), rid)
		case errors.Is(err, ErrPaymentRequestExpired):
			response.UnprocessableEntity(w, "EXPIRED", err.Error(), rid)
		case errors.Is(err, ErrPaymentRequestCancelled):
			response.UnprocessableEntity(w, "CANCELLED", err.Error(), rid)
		case errors.Is(err, ErrSelfPayment):
			response.UnprocessableEntity(w, "SELF_PAYMENT", err.Error(), rid)
		case errors.Is(err, ErrMerchantInactive):
			response.UnprocessableEntity(w, "MERCHANT_INACTIVE", err.Error(), rid)
		default:
			response.InternalError(w, rid)
		}
		return
	}

	response.Created(w, result)
}

// parseUserID extracts and parses the authenticated user ID from context.
func parseUserID(r *http.Request, w http.ResponseWriter, rid string) (uuid.UUID, bool) {
	userIDStr, ok := middleware.GetUserID(r.Context())
	if !ok {
		response.Unauthorized(w, rid)
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.Unauthorized(w, rid)
		return uuid.Nil, false
	}
	return userID, true
}
