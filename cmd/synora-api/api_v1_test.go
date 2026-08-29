package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"synora/internal/security"
)

func TestAPIV1EnvelopePaginationAndETag(t *testing.T) {
	legacy := http.NewServeMux()
	legacy.HandleFunc("/api/incidents", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "incident-1"},
			{"id": "incident-2"},
		})
	})
	handler := newAPIV1Handler(legacy)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents?limit=1", nil)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, req)
	if first.Code != http.StatusOK || first.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d content-type=%q body=%s", first.Code, first.Header().Get("Content-Type"), first.Body.String())
	}
	if first.Header().Get("ETag") == "" {
		t.Fatal("missing deterministic ETag")
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			ETag       string `json:"etag"`
			NextCursor string `json:"next_cursor"`
			Limit      int    `json:"limit"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0]["id"] != "incident-1" {
		t.Fatalf("unexpected first page: %s", first.Body.String())
	}
	if envelope.Meta.NextCursor == "" || envelope.Meta.Limit != 1 || envelope.Meta.ETag != first.Header().Get("ETag") {
		t.Fatalf("unexpected pagination metadata: %s", first.Body.String())
	}

	next := httptest.NewRequest(http.MethodGet, "/api/v1/incidents?limit=1&cursor="+envelope.Meta.NextCursor, nil)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, next)
	var secondEnvelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(secondEnvelope.Data) != 1 || secondEnvelope.Data[0]["id"] != "incident-2" {
		t.Fatalf("unexpected second page: %s", second.Body.String())
	}

	conditional := httptest.NewRequest(http.MethodGet, "/api/v1/incidents?limit=1", nil)
	conditional.Header.Set("If-None-Match", first.Header().Get("ETag"))
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional response status=%d body=%s", notModified.Code, notModified.Body.String())
	}
}

func TestAPIV1ConditionalMutationRejectsStaleETag(t *testing.T) {
	legacy := http.NewServeMux()
	updated := false
	legacy.HandleFunc("/api/devices/camera-1", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"id": "camera-1", "revision": 7})
		case http.MethodPatch:
			updated = true
			writeJSON(w, http.StatusOK, map[string]any{"id": "camera-1", "revision": 8})
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
		}
	})
	handler := newAPIV1Handler(legacy)

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/devices/camera-1", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status=%d", get.Code)
	}

	stale := httptest.NewRequest(http.MethodPatch, "/api/v1/devices/camera-1", strings.NewReader(`{"name":"new"}`))
	stale.Header.Set("If-Match", `"stale"`)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusPreconditionFailed || updated {
		t.Fatalf("stale mutation status=%d updated=%t body=%s", staleResponse.Code, updated, staleResponse.Body.String())
	}

	valid := httptest.NewRequest(http.MethodPatch, "/api/v1/devices/camera-1", strings.NewReader(`{"name":"new"}`))
	valid.Header.Set("If-Match", get.Header().Get("ETag"))
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK || !updated {
		t.Fatalf("valid mutation status=%d updated=%t body=%s", validResponse.Code, updated, validResponse.Body.String())
	}
}

func TestAPIV1NormalizesErrorsAndExposesOpenAPI(t *testing.T) {
	legacy := http.NewServeMux()
	legacy.HandleFunc("/api/secret", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret backend token", http.StatusInternalServerError)
	})
	handler := newAPIV1Handler(legacy)

	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/api/v1/secret", nil))
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "secret backend token") {
		t.Fatalf("sensitive error leaked: %d %s", failed.Code, failed.Body.String())
	}
	var errorEnvelope map[string]any
	if err := json.Unmarshal(failed.Body.Bytes(), &errorEnvelope); err != nil || errorEnvelope["error"] == nil {
		t.Fatalf("invalid error envelope: %s", failed.Body.String())
	}

	doc := httptest.NewRecorder()
	handler.ServeHTTP(doc, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if doc.Code != http.StatusOK || doc.Header().Get("ETag") == "" || !strings.Contains(doc.Body.String(), `"openapi":"3.0.3"`) {
		t.Fatalf("invalid OpenAPI document: %d %s", doc.Code, doc.Body.String())
	}
}

func TestAPIV1PreservesBodyValidationAndCanonicalAuth(t *testing.T) {
	legacy := http.NewServeMux()
	legacy.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	legacy.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if _, ok := readJSONObject(w, r, true); ok {
			writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
		}
	})
	cfg := &security.Config{APITokenHash: security.HashSecret("admin-token")}
	handler := buildServerHandler(cfg, legacy, nil, false, nil)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/state", nil))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected v1 unauthorized response: %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/state", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected authenticated response: %d %s", response.Code, response.Body.String())
	}

	malformed := httptest.NewRequest(http.MethodPost, "/api/v1/devices", strings.NewReader(`{"id":`))
	malformed.Header.Set("Authorization", "Bearer admin-token")
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformed)
	if malformedResponse.Code != http.StatusBadRequest || !strings.Contains(malformedResponse.Body.String(), `"code":"invalid_json"`) {
		t.Fatalf("unexpected malformed response: %d %s", malformedResponse.Code, malformedResponse.Body.String())
	}

	tooLarge := httptest.NewRequest(http.MethodPost, "/api/v1/devices", strings.NewReader(`{"payload":"`+strings.Repeat("x", maxAPIJSONBody)+`"}`))
	tooLarge.Header.Set("Authorization", "Bearer admin-token")
	tooLargeResponse := httptest.NewRecorder()
	handler.ServeHTTP(tooLargeResponse, tooLarge)
	if tooLargeResponse.Code != http.StatusBadRequest || !strings.Contains(tooLargeResponse.Body.String(), `"code":"invalid_json"`) {
		t.Fatalf("unexpected oversized response: %d %s", tooLargeResponse.Code, tooLargeResponse.Body.String())
	}
}
