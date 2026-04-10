package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler is a simple handler that writes 200.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func applyAuth(apiKey string, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	Auth(apiKey)(okHandler).ServeHTTP(rec, r)
	return rec
}

func newReq() *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	return r
}

func TestAuth_NoCredentials_Returns401(t *testing.T) {
	rec := applyAuth("secret", newReq())
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_EmptyConfiguredKey_Returns401(t *testing.T) {
	r := newReq()
	r.Header.Set("Authorization", "Bearer anything")
	rec := applyAuth("", r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when agent key not configured, got %d", rec.Code)
	}
}

func TestAuth_WrongKey_Returns401(t *testing.T) {
	r := newReq()
	r.Header.Set("Authorization", "Bearer wrong")
	rec := applyAuth("secret", r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong key, got %d", rec.Code)
	}
}

func TestAuth_CorrectBearerToken_Passes(t *testing.T) {
	r := newReq()
	r.Header.Set("Authorization", "Bearer secret")
	rec := applyAuth("secret", r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for correct bearer token, got %d", rec.Code)
	}
}

func TestAuth_BearerTokenCaseInsensitive(t *testing.T) {
	r := newReq()
	r.Header.Set("Authorization", "BEARER secret")
	rec := applyAuth("secret", r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for case-insensitive bearer, got %d", rec.Code)
	}
}

func TestAuth_XAPIKeyHeader_Passes(t *testing.T) {
	r := newReq()
	r.Header.Set("X-API-Key", "secret")
	rec := applyAuth("secret", r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for X-API-Key header, got %d", rec.Code)
	}
}

func TestAuth_XAPIKeyWrong_Returns401(t *testing.T) {
	r := newReq()
	r.Header.Set("X-API-Key", "badkey")
	rec := applyAuth("secret", r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong X-API-Key, got %d", rec.Code)
	}
}

func TestAuth_BearerPrecedesXAPIKey(t *testing.T) {
	// Bearer is wrong, X-API-Key is right — Bearer should win and fail.
	r := newReq()
	r.Header.Set("Authorization", "Bearer wrong")
	r.Header.Set("X-API-Key", "secret")
	rec := applyAuth("secret", r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when bearer is present and wrong, got %d", rec.Code)
	}
}

// --- secureCompare ---

func TestSecureCompare_Equal(t *testing.T) {
	if !secureCompare("abc", "abc") {
		t.Error("expected equal strings to match")
	}
}

func TestSecureCompare_Unequal(t *testing.T) {
	if secureCompare("abc", "xyz") {
		t.Error("expected different strings to not match")
	}
}

func TestSecureCompare_DifferentLengths(t *testing.T) {
	if secureCompare("short", "longer") {
		t.Error("expected different-length strings to not match")
	}
}

func TestSecureCompare_Empty(t *testing.T) {
	if !secureCompare("", "") {
		t.Error("expected two empty strings to match")
	}
}

// --- extractKey ---

func TestExtractKey_Bearer(t *testing.T) {
	r := newReq()
	r.Header.Set("Authorization", "Bearer mytoken")
	if got := extractKey(r); got != "mytoken" {
		t.Errorf("extractKey: got %q, want mytoken", got)
	}
}

func TestExtractKey_XAPIKey(t *testing.T) {
	r := newReq()
	r.Header.Set("X-API-Key", "mykey")
	if got := extractKey(r); got != "mykey" {
		t.Errorf("extractKey: got %q, want mykey", got)
	}
}

func TestExtractKey_None(t *testing.T) {
	if got := extractKey(newReq()); got != "" {
		t.Errorf("extractKey: expected empty, got %q", got)
	}
}

func TestExtractKey_InvalidAuthScheme(t *testing.T) {
	r := newReq()
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if got := extractKey(r); got != "" {
		t.Errorf("extractKey: expected empty for Basic auth, got %q", got)
	}
}
