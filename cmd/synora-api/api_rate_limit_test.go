package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAPIRequestRateLimiterRejectsAfterBoundedQuota(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	limiter := &apiRequestRateLimiter{
		window:     time.Minute,
		limit:      2,
		maxClients: 4,
		now:        func() time.Time { return now },
		clients:    make(map[string]apiRateLimitEntry),
	}
	handler := apiRateLimitMiddleware(limiter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for index, want := range []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("request %d status=%d want=%d body=%s", index, rec.Code, want, rec.Body.String())
		}
	}
	if got := limiter.clients["192.0.2.10"].count; got != 2 {
		t.Fatalf("count=%d, want 2", got)
	}
	if got := handlerResponseRetryAfter(t, limiter, "192.0.2.10:1234"); got != "60" {
		t.Fatalf("retry-after=%q, want 60", got)
	}
	now = now.Add(time.Minute)
	allowed, _ := limiter.allow("192.0.2.10")
	if !allowed {
		t.Fatal("quota did not reset after window")
	}
}

func handlerResponseRetryAfter(t *testing.T, limiter *apiRequestRateLimiter, remoteAddr string) string {
	t.Helper()
	handler := apiRateLimitMiddleware(limiter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "too_many_requests") {
		t.Fatalf("unexpected limiter response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Retry-After")
}

func apiRateLimitLimiterForTest() *apiRequestRateLimiter {
	return &apiRequestRateLimiter{
		window:     time.Minute,
		limit:      100,
		maxClients: 4,
		now:        func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) },
		clients:    make(map[string]apiRateLimitEntry),
	}
}

func TestAPIRequestRateLimiterConcurrentTableBound(t *testing.T) {
	limiter := apiRateLimitLimiterForTest()
	var group sync.WaitGroup
	for i := 0; i < 64; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			limiter.allow("198.51.100." + strconv.Itoa(index))
		}(i)
	}
	group.Wait()
	if len(limiter.clients) > limiter.maxClients {
		t.Fatalf("client table grew to %d, max=%d", len(limiter.clients), limiter.maxClients)
	}
}
