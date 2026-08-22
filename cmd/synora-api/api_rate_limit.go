package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	webapi "synora/internal/api"
)

const (
	defaultAPIRequestLimit = 300
	defaultAPIRateWindow   = time.Minute
	defaultAPIRateClients  = 4096
)

type apiRateLimitEntry struct {
	startedAt time.Time
	count     int
}

// apiRequestRateLimiter is intentionally process-local: it protects the
// single local API process without claiming to be a distributed quota.
// maxClients keeps attacker-controlled source addresses from growing memory
// without bound.
type apiRequestRateLimiter struct {
	mu         sync.Mutex
	window     time.Duration
	limit      int
	maxClients int
	now        func() time.Time
	clients    map[string]apiRateLimitEntry
}

func newAPIRequestRateLimiter() *apiRequestRateLimiter {
	return &apiRequestRateLimiter{
		window:     defaultAPIRateWindow,
		limit:      defaultAPIRequestLimit,
		maxClients: defaultAPIRateClients,
		now:        func() time.Time { return time.Now().UTC() },
		clients:    make(map[string]apiRateLimitEntry),
	}
}

func (l *apiRequestRateLimiter) allow(client string) (bool, time.Duration) {
	if l == nil || l.limit <= 0 || l.window <= 0 {
		return true, 0
	}
	if client == "" {
		client = "unknown"
	}
	now := time.Now().UTC()
	if l.now != nil {
		now = l.now().UTC()
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.clients[client]
	if !exists || now.Sub(entry.startedAt) >= l.window || now.Before(entry.startedAt) {
		if !exists && len(l.clients) >= l.maxClients {
			l.evictOldestLocked()
		}
		l.clients[client] = apiRateLimitEntry{startedAt: now, count: 1}
		return true, 0
	}
	if entry.count >= l.limit {
		remaining := l.window - now.Sub(entry.startedAt)
		if remaining < time.Second {
			remaining = time.Second
		}
		return false, remaining
	}
	entry.count++
	l.clients[client] = entry
	return true, 0
}

func (l *apiRequestRateLimiter) evictOldestLocked() {
	var oldestClient string
	var oldest time.Time
	for client, entry := range l.clients {
		if oldestClient == "" || entry.startedAt.Before(oldest) {
			oldestClient = client
			oldest = entry.startedAt
		}
	}
	if oldestClient != "" {
		delete(l.clients, oldestClient)
	}
}

func apiRateLimitMiddleware(limiter *apiRequestRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || len(r.URL.Path) >= len("/api/") && r.URL.Path[:len("/api/")] == "/api/" {
			if allowed, retryAfter := limiter.allow(webapi.ClientAddress(r)); !allowed {
				seconds := int((retryAfter + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_requests"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
