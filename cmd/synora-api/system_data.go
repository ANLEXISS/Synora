package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

const systemDataResetMarkerSchema = "synora.api-data-reset/v1"

type systemDataResetMarker struct {
	Schema    string    `json:"schema"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"created_by"`
	StartedAt time.Time `json:"started_at"`
}

func handleSystemDataDelete(w http.ResponseWriter, r *http.Request, requester systemDataResetRequester, createdBy, markerPath string) {
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
	request := contract.SystemStateResetRequest{TargetState: "empty", Reason: strings.TrimSpace(input.Reason), CreatedBy: strings.TrimSpace(createdBy)}
	if err := writeSystemDataResetMarker(markerPath, request); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "reset marker unavailable"})
		return
	}
	results, err := requestSystemResetScopes(requester, payload, 3*time.Second)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "state reset incomplete", "scope": err.Error()})
		return
	}
	if err := removeSystemDataResetMarker(markerPath); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "error": "reset completion marker unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, contract.SystemStateResetResult{
		Status: "erased", Scope: "global", Scopes: []string{"core", "discovery"}, TargetState: "empty",
		CreatedBy: results[0].CreatedBy, Reason: results[0].Reason, ErasedAt: results[1].ErasedAt,
	})
}

func requestSystemResetScopes(requester systemDataResetRequester, payload []byte, timeout time.Duration) ([]contract.SystemStateResetResult, error) {
	results := make([]contract.SystemStateResetResult, 0, 2)
	for _, target := range []string{"core", "discovery"} {
		response, requestErr := requester.RequestWithTimeout(contract.RPCSystemResetState, "api", payload, target, timeout)
		if requestErr != nil || response == nil {
			return nil, fmt.Errorf("%s", target)
		}
		var result contract.SystemStateResetResult
		if err := json.Unmarshal(response.Payload, &result); err != nil || result.Status != "erased" || result.TargetState != "empty" || result.Scope != target {
			return nil, fmt.Errorf("%s", target)
		}
		results = append(results, result)
	}
	return results, nil
}

func recoverSystemDataReset(ctx context.Context, requester systemDataResetRequester, markerPath string) error {
	marker, err := readSystemDataResetMarker(markerPath)
	if err != nil || marker == nil {
		return err
	}
	payload, err := json.Marshal(contract.SystemStateResetRequest{TargetState: "empty", Reason: marker.Reason, CreatedBy: marker.CreatedBy})
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if _, err := requestSystemResetScopes(requester, payload, 3*time.Second); err != nil {
		return err
	}
	return removeSystemDataResetMarker(markerPath)
}

func writeSystemDataResetMarker(path string, request contract.SystemStateResetRequest) error {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("reset marker path is invalid")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(systemDataResetMarker{Schema: systemDataResetMarkerSchema, Reason: request.Reason, CreatedBy: request.CreatedBy, StartedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".synora-api-reset-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	keep = true
	return syncResetMarkerDirectory(parent)
}

func readSystemDataResetMarker(path string) (*systemDataResetMarker, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marker systemDataResetMarker
	if err := json.Unmarshal(body, &marker); err != nil || marker.Schema != systemDataResetMarkerSchema || strings.TrimSpace(marker.Reason) == "" || strings.TrimSpace(marker.CreatedBy) == "" {
		return nil, errors.New("invalid reset marker")
	}
	return &marker, nil
}

func removeSystemDataResetMarker(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncResetMarkerDirectory(filepath.Dir(path))
}

func syncResetMarkerDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
