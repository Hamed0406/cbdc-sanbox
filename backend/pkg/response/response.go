// Package response provides standardised JSON response helpers used by every HTTP handler.
// Centralising this ensures every error looks the same to API consumers,
// and every success is consistently wrapped.
package response

import (
	"encoding/json"
	"net/http"
	"time"
)

// ErrorPayload is the standard error response body returned on every non-2xx response.
// Clients can rely on this structure always being present on errors.
type ErrorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Timestamp string `json:"timestamp"`
}

// envelope wraps the error in a named field for clarity in API responses.
type envelope struct {
	Error *ErrorPayload `json:"error,omitempty"`
}

// JSON writes a JSON response with the given status code and payload.
func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if payload == nil {
		return
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// If we can't encode the response, there's not much we can do —
		// the header is already written. Log it at the handler level.
		http.Error(w, `{"error":{"code":"ENCODE_ERROR","message":"Failed to encode response"}}`, http.StatusInternalServerError)
	}
}

// Error writes a standardised error response.
// requestID is extracted from the request context by middleware and passed here.
func Error(w http.ResponseWriter, status int, code, message, requestID string) {
	JSON(w, status, envelope{
		Error: &ErrorPayload{
			Code:      code,
			Message:   message,
			RequestID: requestID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// NoContent writes a 204 No Content response with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Created writes a 201 Created response.
func Created(w http.ResponseWriter, payload any) {
	JSON(w, http.StatusCreated, payload)
}

// OK writes a 200 OK response.
func OK(w http.ResponseWriter, payload any) {
	JSON(w, http.StatusOK, payload)
}

// Common error shortcuts — these keep handler code concise and consistent.

func BadRequest(w http.ResponseWriter, message, requestID string) {
	Error(w, http.StatusBadRequest, "INVALID_REQUEST", message, requestID)
}

func Unauthorized(w http.ResponseWriter, requestID string) {
	Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required", requestID)
}

func Forbidden(w http.ResponseWriter, requestID string) {
	Error(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to perform this action", requestID)
}

func NotFound(w http.ResponseWriter, resource, requestID string) {
	Error(w, http.StatusNotFound, "NOT_FOUND", resource+" not found", requestID)
}

func Conflict(w http.ResponseWriter, code, message, requestID string) {
	Error(w, http.StatusConflict, code, message, requestID)
}

func UnprocessableEntity(w http.ResponseWriter, code, message, requestID string) {
	Error(w, http.StatusUnprocessableEntity, code, message, requestID)
}

func TooManyRequests(w http.ResponseWriter, retryAfter int, requestID string) {
	w.Header().Set("Retry-After", http.StatusText(retryAfter))
	Error(w, http.StatusTooManyRequests, "RATE_LIMITED",
		"Too many requests. Please slow down.", requestID)
}

func InternalError(w http.ResponseWriter, requestID string) {
	// Never expose internal error details to the client — log them server-side.
	Error(w, http.StatusInternalServerError, "INTERNAL_ERROR",
		"An unexpected error occurred. Please try again.", requestID)
}
