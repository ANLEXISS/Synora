package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	apiV1DefaultLimit = 50
	apiV1MaxLimit     = 100
)

// apiV1Envelope is the stable wire format for JSON responses introduced by
// M028. The legacy /api routes deliberately retain their existing payloads.
type apiV1Envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *apiV1Error     `json:"error,omitempty"`
	Meta  apiV1Meta       `json:"meta"`
}

type apiV1Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiV1Meta struct {
	Revision   any    `json:"revision"`
	ETag       string `json:"etag"`
	NextCursor string `json:"next_cursor,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type apiV1Handler struct {
	legacy http.Handler
}

// newAPIV1Handler adapts the already-owned handlers. This keeps business
// decisions in their existing owners while providing one public REST shape.
func newAPIV1Handler(legacy http.Handler) http.Handler {
	return apiV1Handler{legacy: legacy}
}

func (h apiV1Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil || h.legacy == nil {
		writeAPIV1Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if strings.TrimSuffix(r.URL.Path, "/") == "/api/v1/openapi.json" {
		h.serveOpenAPI(w, r)
		return
	}

	if ifMatch := strings.TrimSpace(r.Header.Get("If-Match")); ifMatch != "" && isV1ConditionalMutation(r) {
		if ifMatch != "*" {
			current, ok := h.currentETag(r)
			if !ok || !etagMatches(ifMatch, current) {
				writeAPIV1Error(w, http.StatusPreconditionFailed, "precondition_failed", "resource has changed")
				return
			}
		}
	}

	request := r.Clone(r.Context())
	request.URL = cloneURL(r.URL)
	request.URL.Path = canonicalAPIPath(r.URL.Path)
	request.URL.RawPath = ""
	request.RequestURI = request.URL.RequestURI()
	if request.Method == http.MethodHead {
		// Existing owners generally implement GET only. HEAD has identical
		// authorization and representation semantics, but never returns a body.
		request.Method = http.MethodGet
	}

	collection := isV1CollectionPath(r.URL.Path)
	if collection {
		limit, err := prepareV1Pagination(request)
		if err != nil {
			writeAPIV1Error(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		request = request.WithContext(context.WithValue(request.Context(), apiV1LimitContextKey{}, limit))
	}

	capture := newAPIV1Capture()
	h.legacy.ServeHTTP(capture, request)
	h.writeAdapted(w, r.WithContext(request.Context()), capture, collection)
}

func (h apiV1Handler) writeAdapted(w http.ResponseWriter, r *http.Request, capture *apiV1Capture, collection bool) {
	status := capture.status
	if status == 0 {
		status = http.StatusOK
	}
	copyHeader(w.Header(), capture.header)

	contentType := strings.ToLower(capture.header.Get("Content-Type"))
	jsonResponse := strings.HasPrefix(contentType, "application/json")
	if !jsonResponse && status >= http.StatusBadRequest {
		code := strings.ToLower(strings.ReplaceAll(http.StatusText(status), " ", "_"))
		if code == "" {
			code = "request_failed"
		}
		writeAPIV1Error(w, status, code, code)
		return
	}
	if !jsonResponse {
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(capture.body.Bytes())
		}
		return
	}

	payload := bytes.TrimSpace(capture.body.Bytes())
	if len(payload) == 0 {
		payload = []byte("null")
	}
	if !json.Valid(payload) {
		if status == http.StatusNotFound {
			writeAPIV1Error(w, status, "not_found", "route not found")
			return
		}
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(capture.body.Bytes())
		}
		return
	}

	if strings.TrimSuffix(r.URL.Path, "/") == "/api/v1/openapi.json" {
		writeAPIV1JSON(w, r, status, payload)
		return
	}

	if status >= http.StatusBadRequest {
		code, message := apiV1ErrorFields(payload, status)
		writeAPIV1Envelope(w, r, status, apiV1Envelope{
			Data:  json.RawMessage("null"),
			Error: &apiV1Error{Code: code, Message: message},
			Meta:  apiV1Meta{Revision: 0},
		})
		return
	}

	var nextCursor string
	var pageLimit int
	if collection {
		payload, nextCursor, pageLimit = paginateV1Payload(payload, r)
	}
	var data any
	if err := json.Unmarshal(payload, &data); err != nil {
		writeAPIV1Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	envelope := apiV1Envelope{
		Data: json.RawMessage(payload),
		Meta: apiV1Meta{Revision: v1Revision(data), NextCursor: nextCursor, Limit: pageLimit},
	}
	writeAPIV1Envelope(w, r, status, envelope)
}

func (h apiV1Handler) currentETag(r *http.Request) (string, bool) {
	request := r.Clone(r.Context())
	request.Method = http.MethodGet
	request.Body = http.NoBody
	request.URL = cloneURL(r.URL)
	request.URL.Path = canonicalAPIPath(r.URL.Path)
	request.URL.RawPath = ""
	request.RequestURI = request.URL.RequestURI()
	capture := newAPIV1Capture()
	h.legacy.ServeHTTP(capture, request)
	if capture.status < http.StatusOK || capture.status >= http.StatusMultipleChoices {
		return "", false
	}
	payload := bytes.TrimSpace(capture.body.Bytes())
	if !json.Valid(payload) {
		return "", false
	}
	return apiV1ETag(payload), true
}

func (h apiV1Handler) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeAPIV1Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	payload, err := json.Marshal(apiV1OpenAPISpec())
	if err != nil {
		writeAPIV1Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeAPIV1JSON(w, r, http.StatusOK, payload)
}

type apiV1Capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newAPIV1Capture() *apiV1Capture {
	return &apiV1Capture{header: make(http.Header)}
}

func (c *apiV1Capture) Header() http.Header { return c.header }

func (c *apiV1Capture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *apiV1Capture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(p)
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{}
	}
	copy := *u
	return &copy
}

func canonicalAPIPath(path string) string {
	switch {
	case path == "/api/v1":
		return "/api"
	case strings.HasPrefix(path, "/api/v1/"):
		return "/api/" + strings.TrimPrefix(path, "/api/v1/")
	default:
		return path
	}
}

func isAPIV1Path(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func rewriteAPIPath(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Clone(r.Context())
		request.URL = cloneURL(r.URL)
		request.URL.Path = path
		request.URL.RawPath = ""
		request.RequestURI = request.URL.RequestURI()
		next.ServeHTTP(w, request)
	})
}

type apiV1LimitContextKey struct{}

func prepareV1Pagination(r *http.Request) (int, error) {
	limit, err := v1Limit(r.URL.Query().Get("limit"))
	if err != nil {
		return 0, err
	}
	if _, err := decodeV1Cursor(r.URL.Query().Get("cursor")); err != nil {
		return 0, err
	}
	query := r.URL.Query()
	query.Set("limit", strconv.Itoa(apiV1MaxLimit))
	r.URL.RawQuery = query.Encode()
	return limit, nil
}

func paginateV1Payload(payload []byte, r *http.Request) ([]byte, string, int) {
	var items []json.RawMessage
	if json.Unmarshal(payload, &items) != nil {
		return payload, "", 0
	}
	limit, ok := r.Context().Value(apiV1LimitContextKey{}).(int)
	if !ok || limit < 1 {
		limit = apiV1DefaultLimit
	}
	offset, err := decodeV1Cursor(r.URL.Query().Get("cursor"))
	if err != nil || offset >= len(items) {
		items = nil
		offset = 0
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[offset:end]
	encoded, err := json.Marshal(page)
	if err != nil {
		return []byte("[]"), "", limit
	}
	next := ""
	if end < len(items) {
		next = encodeV1Cursor(end)
	}
	return encoded, next, limit
}

func v1Limit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return apiV1DefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > apiV1MaxLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", apiV1MaxLimit)
	}
	return limit, nil
}

func encodeV1Cursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeV1Cursor(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, fmt.Errorf("cursor is invalid")
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("cursor is invalid")
	}
	return offset, nil
}

func isV1CollectionPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	switch path {
	case "/api/v1/events", "/api/v1/incidents", "/api/v1/clips", "/api/v1/devices", "/api/v1/residents", "/api/v1/streams", "/api/v1/validations":
		return true
	default:
		return strings.HasSuffix(path, "/photos") || strings.HasSuffix(path, "/chains")
	}
}

func isV1ConditionalMutation(r *http.Request) bool {
	if r == nil || !isMutatingMethod(r.Method) {
		return false
	}
	path := canonicalAPIPath(r.URL.Path)
	return strings.HasPrefix(path, "/api/devices/") && !strings.HasPrefix(path, "/api/devices/pairing/") ||
		strings.HasPrefix(path, "/api/residents/") || path == "/api/topology"
}

func v1Revision(data any) any {
	if object, ok := data.(map[string]any); ok {
		if revision, exists := object["revision"]; exists {
			return revision
		}
	}
	return 0
}

func apiV1ETag(payload []byte) string {
	canonical := bytes.TrimSpace(payload)
	var compact bytes.Buffer
	if err := json.Compact(&compact, canonical); err == nil {
		canonical = compact.Bytes()
	}
	hash := sha256.Sum256(canonical)
	return `"` + hex.EncodeToString(hash[:]) + `"`
}

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == current {
			return true
		}
	}
	return false
}

func writeAPIV1Envelope(w http.ResponseWriter, r *http.Request, status int, envelope apiV1Envelope) {
	payload, err := json.Marshal(envelope.Data)
	if err != nil {
		payload = []byte("null")
	}
	envelope.Meta.ETag = apiV1ETag(payload)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		writeAPIV1Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeAPIV1JSONWithETag(w, r, status, encoded, envelope.Meta.ETag)
}

func writeAPIV1JSON(w http.ResponseWriter, r *http.Request, status int, payload []byte) {
	writeAPIV1JSONWithETag(w, r, status, payload, apiV1ETag(payload))
}

func writeAPIV1JSONWithETag(w http.ResponseWriter, r *http.Request, status int, payload []byte, etag string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	if r != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) && etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(status)
	if r == nil || r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}

func writeAPIV1Error(w http.ResponseWriter, status int, code, message string) {
	envelope := apiV1Envelope{
		Data:  json.RawMessage("null"),
		Error: &apiV1Error{Code: code, Message: message},
		Meta:  apiV1Meta{Revision: 0},
	}
	writeAPIV1Envelope(w, nil, status, envelope)
}

func apiV1ErrorFields(payload []byte, status int) (string, string) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return "internal_error", "internal server error"
	}
	var code, message string
	_ = json.Unmarshal(raw["error"], &code)
	_ = json.Unmarshal(raw["message"], &message)
	if code == "" {
		code = http.StatusText(status)
		code = strings.ToLower(strings.ReplaceAll(code, " ", "_"))
	}
	if message == "" {
		message = code
	}
	if status >= http.StatusInternalServerError {
		message = "internal server error"
	}
	return code, message
}

func apiV1OpenAPISpec() map[string]any {
	paths := make(map[string]any)
	for _, path := range []string{"/state", "/events", "/incidents", "/clips", "/residents", "/devices", "/streams", "/topology", "/system/health", "/system/version", "/devices/pairing", "/openapi.json"} {
		paths[path] = map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "success"}}}}
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "Synora API", "version": "v1"},
		"paths":   paths,
	}
}
