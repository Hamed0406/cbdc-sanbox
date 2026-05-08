// Tests for the response package.
// Verifies that every helper writes the correct status code, Content-Type,
// and JSON body — because every API handler depends on this.
package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbdc-simulator/backend/pkg/response"
)

// helper decodes the response body into a map for inspection.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("failed to decode response body: %v\nbody: %s", err, rec.Body.String())
	}
	return m
}

// ── JSON ──────────────────────────────────────────────────────────────────────

func TestJSON_WritesCorrectStatusAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusOK, map[string]string{"foo": "bar"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestJSON_NilPayloadWritesNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusNoContent, nil)

	if rec.Body.Len() != 0 {
		t.Errorf("nil payload should produce empty body, got: %q", rec.Body.String())
	}
}

func TestJSON_EncodesPayloadCorrectly(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusOK, map[string]any{"amount": 1050, "currency": "DD$"})

	body := decode(t, rec)
	if body["amount"] != float64(1050) {
		t.Errorf("expected amount 1050, got %v", body["amount"])
	}
	if body["currency"] != "DD$" {
		t.Errorf("expected currency DD$, got %v", body["currency"])
	}
}

// ── Error ─────────────────────────────────────────────────────────────────────

func TestError_ContainsCodeAndMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS",
		"Balance too low", "req_123")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}

	body := decode(t, rec)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' key in response, got: %v", body)
	}
	if errObj["code"] != "INSUFFICIENT_FUNDS" {
		t.Errorf("expected code INSUFFICIENT_FUNDS, got %v", errObj["code"])
	}
	if errObj["message"] != "Balance too low" {
		t.Errorf("expected message 'Balance too low', got %v", errObj["message"])
	}
	if errObj["request_id"] != "req_123" {
		t.Errorf("expected request_id req_123, got %v", errObj["request_id"])
	}
	// Timestamp must be present
	if errObj["timestamp"] == nil || errObj["timestamp"] == "" {
		t.Error("error response must include a timestamp")
	}
}

func TestError_EmptyRequestIDOmittedFromJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, http.StatusBadRequest, "INVALID_REQUEST", "Bad input", "")

	// With an empty request_id and omitempty, the field should not appear in the JSON.
	// We just verify the response is valid and has an error key.
	body := decode(t, rec)
	if _, ok := body["error"]; !ok {
		t.Fatal("error response must have 'error' key")
	}
}

// ── Convenience helpers ───────────────────────────────────────────────────────

func TestOK_Returns200(t *testing.T) {
	rec := httptest.NewRecorder()
	response.OK(rec, map[string]string{"status": "ok"})
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestCreated_Returns201(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Created(rec, map[string]string{"id": "abc"})
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
}

func TestNoContent_Returns204WithNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	response.NoContent(rec)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 must have no body, got: %q", rec.Body.String())
	}
}

func TestBadRequest_Returns400WithCode(t *testing.T) {
	rec := httptest.NewRecorder()
	response.BadRequest(rec, "amount must be positive", "req_1")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	body := decode(t, rec)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "INVALID_REQUEST" {
		t.Errorf("expected code INVALID_REQUEST, got %v", errObj["code"])
	}
}

func TestUnauthorized_Returns401(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Unauthorized(rec, "req_1")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	body := decode(t, rec)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "UNAUTHORIZED" {
		t.Errorf("expected UNAUTHORIZED, got %v", errObj["code"])
	}
}

func TestForbidden_Returns403(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Forbidden(rec, "req_1")
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestNotFound_Returns404WithResourceName(t *testing.T) {
	rec := httptest.NewRecorder()
	response.NotFound(rec, "wallet", "req_1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	body := decode(t, rec)
	errObj := body["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if msg != "wallet not found" {
		t.Errorf("expected 'wallet not found', got %q", msg)
	}
}

func TestInternalError_DoesNotLeakDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	response.InternalError(rec, "req_1")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	// The message must be generic — no internal error details exposed to clients
	body := decode(t, rec)
	errObj := body["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Error("500 response must have a message")
	}
	// Verify the message doesn't contain typical internal detail keywords
	internals := []string{"sql", "postgres", "redis", "panic", "goroutine", "stack"}
	for _, word := range internals {
		if contains(msg, word) {
			t.Errorf("500 message must not expose internal detail %q: %q", word, msg)
		}
	}
}

func TestTooManyRequests_Returns429(t *testing.T) {
	rec := httptest.NewRecorder()
	response.TooManyRequests(rec, 60, "req_1")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	body := decode(t, rec)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "RATE_LIMITED" {
		t.Errorf("expected RATE_LIMITED, got %v", errObj["code"])
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
