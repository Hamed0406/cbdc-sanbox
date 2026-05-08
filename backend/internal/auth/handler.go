package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/cbdc-simulator/backend/pkg/response"
	"github.com/go-chi/chi/v5"
)

// Handler wires auth HTTP routes to the Service.
type Handler struct {
	svc            *Service
	refreshTTL     time.Duration
	secureCookies  bool // true in production (HTTPS); false in dev (HTTP)
}

// NewHandler creates a new auth Handler.
// secureCookies should be true in production so cookies require HTTPS.
func NewHandler(svc *Service, refreshTTL time.Duration, secureCookies bool) *Handler {
	return &Handler{svc: svc, refreshTTL: refreshTTL, secureCookies: secureCookies}
}

// Routes registers all auth routes on a chi.Router sub-tree.
// Expected to be mounted at /api/v1/auth.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/logout", h.logout)
	r.Post("/refresh", h.refresh)
	return r
}

// register handles POST /api/v1/auth/register
// @Summary      Register a new user
// @Description  Creates a user account and a DD$ wallet, returns JWT tokens
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body body RegisterRequest true "Registration details"
// @Success      201  {object} AuthResponse
// @Failure      400  {object} response.ErrorPayload
// @Failure      409  {object} response.ErrorPayload "Email already registered"
// @Failure      422  {object} response.ErrorPayload "Validation error"
// @Router       /auth/register [post]
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	requestID := chimiddleware.GetReqID(r.Context())

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Invalid JSON body", requestID)
		return
	}

	ip := clientIP(r)
	userAgent := r.UserAgent()

	authResp, refreshToken, err := h.svc.Register(r.Context(), req, ip, userAgent)
	if err != nil {
		h.handleServiceError(w, err, requestID)
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)
	response.Created(w, authResp)
}

// login handles POST /api/v1/auth/login
// @Summary      Login
// @Description  Authenticates a user and returns JWT tokens
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body body LoginRequest true "Credentials"
// @Success      200  {object} AuthResponse
// @Failure      401  {object} response.ErrorPayload
// @Failure      423  {object} response.ErrorPayload "Account locked"
// @Router       /auth/login [post]
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	requestID := chimiddleware.GetReqID(r.Context())

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Return same error as invalid credentials — don't reveal why it failed
		response.Unauthorized(w, requestID)
		return
	}

	ip := clientIP(r)
	userAgent := r.UserAgent()

	authResp, refreshToken, err := h.svc.Login(r.Context(), req, ip, userAgent)
	if err != nil {
		h.handleServiceError(w, err, requestID)
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)
	response.OK(w, authResp)
}

// logout handles POST /api/v1/auth/logout
// @Summary      Logout
// @Description  Revokes the current refresh token session
// @Tags         auth
// @Security     BearerAuth
// @Success      204  "No content"
// @Failure      401  {object} response.ErrorPayload
// @Router       /auth/logout [post]
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	// Read refresh token from HttpOnly cookie
	cookie, err := r.Cookie(refreshTokenCookieName)
	if err != nil || cookie.Value == "" {
		// No cookie = already logged out or never logged in
		// Clear cookie just in case and return success (idempotent logout)
		h.clearRefreshTokenCookie(w)
		response.NoContent(w)
		return
	}

	ip := clientIP(r)

	if err := h.svc.Logout(r.Context(), cookie.Value, nil, ip); err != nil {
		// Logout failure is non-critical — clear cookie anyway
		// The session will expire naturally; partial revocation is acceptable
	}

	h.clearRefreshTokenCookie(w)
	response.NoContent(w)
}

// refresh handles POST /api/v1/auth/refresh
// @Summary      Refresh access token
// @Description  Uses the HttpOnly refresh token cookie to issue a new access token
// @Tags         auth
// @Produce      json
// @Success      200  {object} RefreshResponse
// @Failure      401  {object} response.ErrorPayload
// @Router       /auth/refresh [post]
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	requestID := chimiddleware.GetReqID(r.Context())

	cookie, err := r.Cookie(refreshTokenCookieName)
	if err != nil || cookie.Value == "" {
		response.Unauthorized(w, requestID)
		return
	}

	ip := clientIP(r)
	userAgent := r.UserAgent()

	refreshResp, newRefreshToken, err := h.svc.RefreshToken(r.Context(), cookie.Value, ip, userAgent)
	if err != nil {
		// Token invalid or revoked — clear the cookie
		h.clearRefreshTokenCookie(w)
		response.Unauthorized(w, requestID)
		return
	}

	// Set the rotated refresh token cookie
	h.setRefreshTokenCookie(w, newRefreshToken)
	response.OK(w, refreshResp)
}

// ── Cookie helpers ────────────────────────────────────────────────────────────

// setRefreshTokenCookie writes the refresh token as an HttpOnly cookie.
// HttpOnly = JavaScript cannot read this cookie (XSS protection).
// SameSite=Strict = cookie not sent on cross-site requests (CSRF protection).
// Path=/api/v1/auth = cookie only sent to auth endpoints (minimises exposure).
func (h *Handler) setRefreshTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteStrictMode,
		Path:     "/api/v1/auth",
		MaxAge:   int(h.refreshTTL.Seconds()),
	})
}

// clearRefreshTokenCookie removes the refresh token cookie on logout.
// MaxAge=-1 instructs the browser to immediately delete the cookie.
func (h *Handler) clearRefreshTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteStrictMode,
		Path:     "/api/v1/auth",
		MaxAge:   -1,
	})
}

// handleServiceError maps domain errors to HTTP responses.
// Using typed errors here (not string matching) makes the mapping explicit
// and exhaustive — adding a new domain error without handling it is a compile warning.
func (h *Handler) handleServiceError(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, ErrEmailTaken):
		response.Conflict(w, "EMAIL_TAKEN", "This email address is already registered", requestID)
	case errors.Is(err, ErrInvalidCredentials):
		response.Unauthorized(w, requestID)
	case errors.Is(err, ErrAccountLocked):
		response.Error(w, http.StatusLocked, "ACCOUNT_LOCKED",
			"Account temporarily locked due to too many failed attempts. Try again in 30 minutes.", requestID)
	case errors.Is(err, ErrAccountInactive):
		response.Error(w, http.StatusForbidden, "ACCOUNT_INACTIVE",
			"This account has been deactivated.", requestID)
	case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrTokenRevoked):
		response.Unauthorized(w, requestID)
	case errors.Is(err, ErrWeakPassword):
		response.UnprocessableEntity(w, "WEAK_PASSWORD",
			"Password must be 10-72 characters and include uppercase, number, and special character.", requestID)
	case errors.Is(err, ErrInvalidEmail):
		response.UnprocessableEntity(w, "INVALID_EMAIL", "Email address format is invalid.", requestID)
	case errors.Is(err, ErrNameTooShort):
		response.UnprocessableEntity(w, "INVALID_NAME", "Full name must be at least 2 characters.", requestID)
	default:
		// Unknown error — log it server-side, return generic 500 to client.
		// Never expose internal error details (DB errors, stack traces) to clients.
		slog.Error("unhandled auth service error", "error", err, "request_id", requestID)
		response.InternalError(w, requestID)
	}
}

// clientIP extracts the IP address from r.RemoteAddr, stripping the port.
// r.RemoteAddr is "ip:port" (e.g. "192.168.1.1:54321"), but PostgreSQL's
// inet type only accepts bare IP addresses without a port component.
func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // fall back to raw value if parsing fails
	}
	return ip
}
