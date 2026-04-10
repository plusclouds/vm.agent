package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- splitAndTrim / splitString / trimSpace ---

func TestSplitAndTrim_Basic(t *testing.T) {
	got := splitAndTrim("a, b , c", ",")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitAndTrim: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitAndTrim_EmptyParts(t *testing.T) {
	got := splitAndTrim("  ,  ,  ", ",")
	if len(got) != 0 {
		t.Errorf("expected empty slice for blank parts, got %v", got)
	}
}

func TestSplitAndTrim_SingleItem(t *testing.T) {
	got := splitAndTrim("  hello  ", ",")
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got %v", got)
	}
}

func TestTrimSpace_LeadingTrailing(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello  ", "hello"},
		{"\thello\t", "hello"},
		{"hello", "hello"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := trimSpace(c.in); got != c.want {
			t.Errorf("trimSpace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- realIP ---

func TestRealIP_XRealIP(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "1.2.3.4")
	if got := realIP(r); got != "1.2.3.4" {
		t.Errorf("realIP: got %q, want 1.2.3.4", got)
	}
}

func TestRealIP_XForwardedFor(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "5.6.7.8, 9.10.11.12")
	if got := realIP(r); got != "5.6.7.8" {
		t.Errorf("realIP: got %q, want 5.6.7.8", got)
	}
}

func TestRealIP_RemoteAddr_Fallback(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	if got := realIP(r); got != "10.0.0.1:1234" {
		t.Errorf("realIP fallback: got %q, want 10.0.0.1:1234", got)
	}
}

func TestRealIP_XRealIPTakesPrecedence(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "1.1.1.1")
	r.Header.Set("X-Forwarded-For", "2.2.2.2")
	if got := realIP(r); got != "1.1.1.1" {
		t.Errorf("realIP: X-Real-IP should take precedence, got %q", got)
	}
}

// --- RequestLogger middleware ---

func TestRequestLogger_PassesThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	handler := RequestLogger(zapNoop(t))(inner)

	r, _ := http.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if !called {
		t.Error("expected inner handler to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

func TestRequestLogger_SkipsHealthz(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RequestLogger(zapNoop(t))(inner)

	r, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if !called {
		t.Error("inner handler should still be called for /healthz")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestResponseWriter_CapturesStatusCode(t *testing.T) {
	rw := newResponseWriter(httptest.NewRecorder())
	rw.WriteHeader(http.StatusTeapot)
	if rw.statusCode != http.StatusTeapot {
		t.Errorf("expected 418, got %d", rw.statusCode)
	}
	// Second write should be ignored.
	rw.WriteHeader(http.StatusOK)
	if rw.statusCode != http.StatusTeapot {
		t.Error("second WriteHeader call should be a no-op")
	}
}
