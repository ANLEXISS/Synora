package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"synora/pkg/contract"
)

func TestHealthProbesExposeOnlyBoundedPublicState(t *testing.T) {
	live := httptest.NewRecorder()
	handleLiveness(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"status":"alive"`) {
		t.Fatalf("liveness=%d %s", live.Code, live.Body.String())
	}

	ready := httptest.NewRecorder()
	handleReadiness(fakeHealthProvider{health: &contract.RuntimeHealth{Status: "ok"}}).ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"status":"ready"`) || strings.Contains(ready.Body.String(), "services") {
		t.Fatalf("readiness=%d %s", ready.Code, ready.Body.String())
	}

	degraded := httptest.NewRecorder()
	handleReadiness(fakeHealthProvider{health: &contract.RuntimeHealth{Status: "degraded"}}).ServeHTTP(degraded, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if degraded.Code != http.StatusServiceUnavailable || !strings.Contains(degraded.Body.String(), `"status":"degraded"`) {
		t.Fatalf("degraded readiness=%d %s", degraded.Code, degraded.Body.String())
	}
}

type fakeHealthProvider struct {
	health *contract.RuntimeHealth
}

func (f fakeHealthProvider) SystemHealth() (*contract.RuntimeHealth, error) { return f.health, nil }

func TestMetricsAreBoundedAndDoNotExposeRequestLabels(t *testing.T) {
	apiObservability = struct {
		requests  uint64
		inflight  int64
		metrics   uint64
		readiness uint32
	}{}
	atomicHandler := metricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "/metrics?secret=must-not-escape", nil)
	request.Header.Set("X-Request-ID", "request-1")
	atomicHandler.ServeHTTP(httptest.NewRecorder(), request)

	metrics := httptest.NewRecorder()
	handleMetrics(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()
	if !strings.Contains(body, "synora_api_requests_total") || !strings.Contains(body, "synora_api_inflight_requests") || strings.Contains(body, "secret") || strings.Contains(body, "request-1") {
		t.Fatalf("metrics leaked or incomplete: %s", body)
	}
}

func TestCorrelationValuesRejectSecretsAndBoundLength(t *testing.T) {
	if validCorrelation("request-1") != "request-1" {
		t.Fatal("safe correlation value was rejected")
	}
	if validCorrelation("token=private") != "" || validCorrelation(strings.Repeat("a", maxCorrelationLength+1)) != "" {
		t.Fatal("unsafe correlation value was accepted")
	}
}
