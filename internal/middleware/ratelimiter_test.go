package middleware_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Disneyjr/Rate-Limiter/internal/limiter"
	"github.com/Disneyjr/Rate-Limiter/internal/middleware"
	"github.com/Disneyjr/Rate-Limiter/internal/storage/memory"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	l, err := limiter.New(memory.New(), limiter.Config{
		IPLimit:            3,
		IPBlockDuration:    time.Minute,
		TokenLimits:        map[string]int64{"abc123": 6},
		TokenBlockDuration: time.Minute,
		Window:             time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return middleware.RateLimiter(l, middleware.Options{})(ok)
}

func do(h http.Handler, ip, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip + ":12345"
	if token != "" {
		req.Header.Set(middleware.TokenHeader, token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddlewareBlocksByIP(t *testing.T) {
	h := newHandler(t)
	for i := 0; i < 3; i++ {
		if rec := do(h, "192.168.0.1", ""); rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
	rec := do(h, "192.168.0.1", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != middleware.BlockedMessage {
		t.Fatalf("unexpected body: %q", body)
	}
	if string(body) != "you have reached the maximum number of requests or actions allowed within a certain time frame" {
		t.Fatalf("body does not match the required message: %q", body)
	}
}

func TestMiddlewareTokenOverridesIP(t *testing.T) {
	h := newHandler(t)
	for i := 0; i < 6; i++ {
		if rec := do(h, "192.168.0.2", "abc123"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 with token, got %d", i+1, rec.Code)
		}
	}
	if rec := do(h, "192.168.0.2", "abc123"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after token limit, got %d", rec.Code)
	}
	if got := do(h, "192.168.0.2", "").Header().Get("X-RateLimit-Limit"); got != "3" {
		t.Fatalf("expected IP limit header 3 for request without token, got %q", got)
	}
}

func TestMiddlewareBlockedKeepsRejecting(t *testing.T) {
	h := newHandler(t)
	for i := 0; i < 4; i++ {
		do(h, "192.168.0.3", "")
	}
	for i := 0; i < 5; i++ {
		if rec := do(h, "192.168.0.3", ""); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected blocked IP to keep receiving 429, got %d", rec.Code)
		}
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.1.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")

	if ip := middleware.ClientIP(req, false); ip != "10.1.1.1" {
		t.Fatalf("expected RemoteAddr IP, got %s", ip)
	}
	if ip := middleware.ClientIP(req, true); ip != "203.0.113.7" {
		t.Fatalf("expected X-Forwarded-For IP, got %s", ip)
	}
}

type errAllower struct{}

func (errAllower) Allow(context.Context, string, string) (limiter.Result, error) {
	return limiter.Result{}, errors.New("storage down")
}

func TestMiddlewareFailOpen(t *testing.T) {
	h := middleware.RateLimiter(errAllower{}, middleware.Options{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	if rec := do(h, "1.2.3.4", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open 200, got %d", rec.Code)
	}
}
