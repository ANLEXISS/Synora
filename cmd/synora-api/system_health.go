package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"synora/internal/bus"
	"synora/pkg/contract"
)

type runtimeHealthRequester interface {
	RequestWithTimeout(string, string, []byte, string, time.Duration) (*contract.Message, error)
}

func handleSystemHealth(w http.ResponseWriter, r *http.Request, requester runtimeHealthRequester) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if requester == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "runtime health unavailable"})
		return
	}
	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	response, err := requestRuntimeHealth(requestCtx, requester)
	if err != nil || response == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "runtime health unavailable"})
		return
	}
	var health contract.RuntimeHealth
	if err := json.Unmarshal(response.Payload, &health); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "runtime health invalid"})
		return
	}
	health = contract.NormalizeRuntimeHealth(health, time.Now().UTC())
	status := http.StatusOK
	if health.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, health)
}

func requestRuntimeHealth(ctx context.Context, requester runtimeHealthRequester) (*contract.Message, error) {
	result := make(chan struct {
		message *contract.Message
		err     error
	}, 1)
	go func() {
		message, err := requester.RequestWithTimeout(contract.RPCRuntimeHealth, "api", []byte("{}"), "runtime-manager", 1200*time.Millisecond)
		result <- struct {
			message *contract.Message
			err     error
		}{message: message, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-result:
		return response.message, response.err
	}
}

var _ runtimeHealthRequester = (*bus.Client)(nil)
