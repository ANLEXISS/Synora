package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHostileLoginBruteforceIsRateLimitedPerClient(t *testing.T) {
	service := newAuthTestService(t)
	current := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return current }
	for attempt := 0; attempt < 8; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"invalid"}`))
		request.RemoteAddr = "198.51.100.24:4040"
		response := httptest.NewRecorder()
		service.LoginHandler(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	blocked := httptest.NewRecorder()
	blockedRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"invalid"}`))
	blockedRequest.RemoteAddr = "198.51.100.24:5050"
	service.LoginHandler(blocked, blockedRequest)
	if blocked.Code != http.StatusTooManyRequests || len(service.limiters) != 1 {
		t.Fatalf("bruteforce was not bounded: status=%d clients=%d", blocked.Code, len(service.limiters))
	}
	current = current.Add(time.Minute)
	allowed := httptest.NewRecorder()
	resetRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"invalid"}`))
	resetRequest.RemoteAddr = "198.51.100.24:5050"
	service.LoginHandler(allowed, resetRequest)
	if allowed.Code != http.StatusUnauthorized {
		t.Fatalf("rate limit did not reset after its window: status=%d", allowed.Code)
	}
}
