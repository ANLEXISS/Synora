package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"synora/pkg/contract"
)

const maxAPIJSONBody = 1 << 20

func readJSONObject(w http.ResponseWriter, r *http.Request, required bool) (json.RawMessage, bool) {
	if r == nil || r.Body == nil {
		if !required {
			return json.RawMessage(`{}`), true
		}
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "JSON body is required"))
		return nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAPIJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		if errors.Is(err, io.EOF) && !required {
			return json.RawMessage(`{}`), true
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "JSON body exceeds %d bytes", maxAPIJSONBody))
			return nil, false
		}
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid JSON body"))
		return nil, false
	}

	object, ok := value.(map[string]any)
	if !ok || object == nil {
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "JSON body must be an object"))
		return nil, false
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "JSON body must contain exactly one object"))
		return nil, false
	}

	encoded, err := json.Marshal(object)
	if err != nil {
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid JSON body"))
		return nil, false
	}
	return encoded, true
}

func resourceID(path string, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	id = strings.TrimSpace(id)
	return id, id != "" && !strings.Contains(id, "/")
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	if len(methods) > 0 {
		w.Header().Set("Allow", strings.Join(methods, ", "))
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
		"error": "method_not_allowed",
	})
}

func writeRouteNotFound(w http.ResponseWriter, resource string) {
	writeError(w, contract.NewAPIError(contract.ErrorNotFound, "%s route not found", resource))
}
