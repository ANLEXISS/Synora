package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"synora/internal/security"
	"synora/pkg/contract"
)

const maxCorrelationLength = 128

var apiObservability struct {
	requests  uint64
	inflight  int64
	metrics   uint64
	readiness uint32
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&apiObservability.requests, 1)
		atomic.AddInt64(&apiObservability.inflight, 1)
		defer atomic.AddInt64(&apiObservability.inflight, -1)
		next.ServeHTTP(w, r)
	})
}

func handleLiveness(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeHealthProbe(w, http.StatusOK, "alive")
}

func handleReadiness(provider systemHealthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		if provider == nil {
			writeHealthProbe(w, http.StatusOK, "ready")
			return
		}
		result := make(chan *contract.RuntimeHealth, 1)
		go func() {
			health, err := provider.SystemHealth()
			if err != nil {
				result <- nil
				return
			}
			result <- health
		}()
		select {
		case health := <-result:
			if health != nil && health.Status == "ok" {
				atomic.StoreUint32(&apiObservability.readiness, 1)
				writeHealthProbe(w, http.StatusOK, "ready")
				return
			}
		case <-time.After(500 * time.Millisecond):
		}
		atomic.StoreUint32(&apiObservability.readiness, 0)
		writeHealthProbe(w, http.StatusServiceUnavailable, "degraded")
	}
}

func writeHealthProbe(w http.ResponseWriter, status int, state string) {
	writeJSON(w, status, map[string]any{
		"service":   "synora-api",
		"status":    state,
		"timestamp": time.Now().UTC(),
	})
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	atomic.AddUint64(&apiObservability.metrics, 1)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP synora_api_requests_total Total HTTP requests received by the API.\n")
	fmt.Fprintf(w, "# TYPE synora_api_requests_total counter\n")
	fmt.Fprintf(w, "synora_api_requests_total %d\n", atomic.LoadUint64(&apiObservability.requests))
	fmt.Fprintf(w, "# HELP synora_api_inflight_requests Current in-flight HTTP requests.\n")
	fmt.Fprintf(w, "# TYPE synora_api_inflight_requests gauge\n")
	fmt.Fprintf(w, "synora_api_inflight_requests %d\n", atomic.LoadInt64(&apiObservability.inflight))
	fmt.Fprintf(w, "# HELP synora_api_metrics_scrapes_total Metrics endpoint scrapes.\n")
	fmt.Fprintf(w, "# TYPE synora_api_metrics_scrapes_total counter\n")
	fmt.Fprintf(w, "synora_api_metrics_scrapes_total %d\n", atomic.LoadUint64(&apiObservability.metrics))
	fmt.Fprintf(w, "# HELP synora_api_readiness Current dependency readiness probe result.\n")
	fmt.Fprintf(w, "# TYPE synora_api_readiness gauge\n")
	fmt.Fprintf(w, "synora_api_readiness %d\n", atomic.LoadUint32(&apiObservability.readiness))
}

func validCorrelation(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > maxCorrelationLength {
		return ""
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && !strings.ContainsRune("-_.:", character) {
			return ""
		}
	}
	return value
}

func structuredRequestLog(r *http.Request, started time.Time) {
	requestID := validCorrelation(r.Header.Get("X-Request-ID"))
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	fields := map[string]any{
		"component":   "synora-api",
		"event":       "http.request",
		"request_id":  requestID,
		"method":      r.Method,
		"path":        security.RedactSupportText(r.URL.Path),
		"duration_ms": time.Since(started).Milliseconds(),
	}
	for header, field := range map[string]string{
		"X-Synora-Event-ID":    "event_id",
		"X-Synora-Clip-ID":     "clip_id",
		"X-Synora-Incident-ID": "incident_id",
	} {
		if value := validCorrelation(r.Header.Get(header)); value != "" {
			fields[field] = value
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return
	}
	log.Print(string(encoded))
}
