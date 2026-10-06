package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"synora/pkg/contract"
)

type systemDataResetRequester interface {
	RequestWithTimeout(string, string, []byte, string, time.Duration) (*contract.Message, error)
}

type systemDataResetInput struct {
	Reason string `json:"reason"`
}

func handleSystemDataDelete(w http.ResponseWriter, r *http.Request, requester systemDataResetRequester, createdBy string) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}
	if requester == nil || strings.TrimSpace(createdBy) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "state reset unavailable"})
		return
	}
	var input systemDataResetInput
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Reason) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "rejected", "error": "a reason is required"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "rejected", "error": "request must contain one JSON object"})
		return
	}
	payload, err := json.Marshal(contract.SystemStateResetRequest{
		TargetState: "empty",
		Reason:      strings.TrimSpace(input.Reason),
		CreatedBy:   strings.TrimSpace(createdBy),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": "reset request encoding failed"})
		return
	}
	results := make([]contract.SystemStateResetResult, 0, 2)
	for _, target := range []string{"core", "discovery"} {
		response, requestErr := requester.RequestWithTimeout(contract.RPCSystemResetState, "api", payload, target, 3*time.Second)
		if requestErr != nil || response == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "state reset incomplete", "scope": target})
			return
		}
		var result contract.SystemStateResetResult
		if err := json.Unmarshal(response.Payload, &result); err != nil || result.Status != "erased" || result.TargetState != "empty" || result.Scope != target {
			var failure map[string]any
			_ = json.Unmarshal(response.Payload, &failure)
			errorText := "invalid state reset response"
			if value, ok := failure["error"].(string); ok && strings.TrimSpace(value) != "" {
				errorText = value
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": errorText, "scope": target})
			return
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, contract.SystemStateResetResult{
		Status: "erased", Scope: "global", Scopes: []string{"core", "discovery"}, TargetState: "empty",
		CreatedBy: results[0].CreatedBy, Reason: results[0].Reason, ErasedAt: results[1].ErasedAt,
	})
}
