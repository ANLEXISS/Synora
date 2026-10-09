package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"synora/internal/security"
	"synora/pkg/contract"
)

type EventPublisher interface{ Publish(contract.Event) error }

type HistoryReader interface{ HistoryJSON() ([]byte, error) }

type EventSubscription interface {
	Subscribe(context.Context) <-chan contract.Event
}

type ResidentGalleryView struct {
	ResidentRef   string `json:"resident_ref"`
	GalleryStatus string `json:"gallery_status"`
	Generation    uint64 `json:"gallery_generation"`
	PolicyVersion string `json:"policy_version"`
	ErrorCategory string `json:"last_error_category,omitempty"`
}

type ResidentGalleryService interface {
	Create(context.Context, string, string) (ResidentGalleryView, bool, error)
	Status(context.Context, string) (ResidentGalleryView, error)
	Rollback(context.Context, string) (ResidentGalleryView, error)
	Delete(context.Context, string) (ResidentGalleryView, error)
}

var residentRefPattern = regexp.MustCompile(`^res_[a-f0-9]{32}$`)

type WebAPI struct {
	Boundary  *Boundary
	Publisher EventPublisher
	Health    func() map[string]any
	Updates   EventSubscription
	Security  *security.Config
	Residents ResidentGalleryService
}

type webCommand struct {
	Command   string         `json:"command"`
	Arguments map[string]any `json:"arguments"`
}

// Handler exposes only Discovery operations. A POST becomes an event; no
// handler has a device client or a Store writer.
func (a *WebAPI) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/api/v1/state" {
			a.readState(writer)
			return
		}
		if request.Method == http.MethodGet && request.URL.Path == "/api/v1/history" {
			a.readHistory(writer)
			return
		}
		if request.Method == http.MethodGet && request.URL.Path == "/api/v1/health" {
			a.readHealth(writer)
			return
		}
		if request.Method == http.MethodGet && request.URL.Path == "/api/v1/subscribe" {
			a.subscribe(writer, request)
			return
		}
		if request.URL.Path == "/api/residents" && request.Method == http.MethodPost {
			a.createResident(writer, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/residents/") {
			if request.Method == http.MethodDelete {
				a.deleteResident(writer, request)
				return
			}
			a.residentGalleryRoute(writer, request)
			return
		}
		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/commands" {
			a.command(writer, request)
			return
		}
		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/messages" {
			a.message(writer, request)
			return
		}
		http.NotFound(writer, request)
	})
}

func (a *WebAPI) deleteResident(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "api" || parts[1] != "residents" || !residentRefPattern.MatchString(parts[2]) {
		http.NotFound(writer, request)
		return
	}
	if !a.hasGalleryAdminScopes(request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	if a == nil || a.Residents == nil {
		http.Error(writer, "resident service unavailable", http.StatusServiceUnavailable)
		return
	}
	view, err := a.Residents.Delete(request.Context(), parts[2])
	if err != nil {
		http.Error(writer, "resident operation unavailable", http.StatusServiceUnavailable)
		return
	}
	writeResidentGalleryView(writer, view)
}

func (a *WebAPI) createResident(writer http.ResponseWriter, request *http.Request) {
	if !a.hasGalleryAdminScopes(request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	if !emptyJSONBody(writer, request) {
		return
	}
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 || strings.ContainsAny(key, " \t\r\n") {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	if a == nil || a.Residents == nil {
		http.Error(writer, "resident service unavailable", http.StatusServiceUnavailable)
		return
	}
	refBytes := make([]byte, 16)
	if _, err := rand.Read(refBytes); err != nil {
		http.Error(writer, "resident service unavailable", http.StatusServiceUnavailable)
		return
	}
	view, duplicate, err := a.Residents.Create(request.Context(), "res_"+hex.EncodeToString(refBytes), key)
	if err != nil {
		http.Error(writer, "resident operation unavailable", http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(map[string]any{"resident_ref": view.ResidentRef, "gallery_status": view.GalleryStatus, "duplicate": duplicate})
}

func (a *WebAPI) residentGalleryRoute(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "residents" || !residentRefPattern.MatchString(parts[2]) || parts[3] != "face-gallery" {
		http.NotFound(writer, request)
		return
	}
	if parts[4] == "status" && request.Method == http.MethodGet {
		a.readResidentGallery(writer, request, parts[2])
		return
	}
	if parts[4] == "rollback" && request.Method == http.MethodPost {
		a.rollbackResidentGallery(writer, request, parts[2])
		return
	}
	http.NotFound(writer, request)
}

func (a *WebAPI) readResidentGallery(writer http.ResponseWriter, request *http.Request, ref string) {
	if a == nil || a.Residents == nil {
		http.Error(writer, "resident service unavailable", http.StatusServiceUnavailable)
		return
	}
	view, err := a.Residents.Status(request.Context(), ref)
	if err != nil {
		http.Error(writer, "resident unavailable", http.StatusNotFound)
		return
	}
	writeResidentGalleryView(writer, view)
}

func (a *WebAPI) rollbackResidentGallery(writer http.ResponseWriter, request *http.Request, ref string) {
	if !a.hasGalleryAdminScopes(request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	if !emptyJSONBody(writer, request) {
		return
	}
	if a == nil || a.Residents == nil {
		http.Error(writer, "resident service unavailable", http.StatusServiceUnavailable)
		return
	}
	view, err := a.Residents.Rollback(request.Context(), ref)
	if err != nil {
		http.Error(writer, "resident operation unavailable", http.StatusServiceUnavailable)
		return
	}
	writeResidentGalleryView(writer, view)
}

func (a *WebAPI) hasGalleryAdminScopes(request *http.Request) bool {
	if a == nil || a.Security == nil {
		return false
	}
	fields := strings.Fields(request.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return false
	}
	token := strings.TrimSpace(fields[1])
	return a.Security.VerifyAPITokenScopes(token, "resident:write", "face_gallery:manage")
}

func emptyJSONBody(writer http.ResponseWriter, request *http.Request) bool {
	if request.Body == nil {
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 4096))
	if err != nil {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 || strings.TrimSpace(string(body)) == "{}" {
		return true
	}
	http.Error(writer, "invalid request", http.StatusBadRequest)
	return false
}

func writeResidentGalleryView(writer http.ResponseWriter, view ResidentGalleryView) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(view)
}

func (a *WebAPI) readHistory(writer http.ResponseWriter) {
	if a == nil || a.Boundary == nil || a.Boundary.Store == nil {
		http.Error(writer, "discovery history unavailable", http.StatusServiceUnavailable)
		return
	}
	reader, ok := a.Boundary.Store.(HistoryReader)
	if !ok {
		http.Error(writer, "history is not exposed by the Store reader", http.StatusNotImplemented)
		return
	}
	body, err := reader.HistoryJSON()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (a *WebAPI) readHealth(writer http.ResponseWriter) {
	value := map[string]any{"service": "discovery", "status": "ok"}
	if a != nil && a.Health != nil {
		if health := a.Health(); health != nil {
			value = health
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func (a *WebAPI) subscribe(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := writer.(http.Flusher)
	if a == nil || a.Updates == nil {
		_, _ = writer.Write([]byte("event: ready\ndata: {\"service\":\"discovery\"}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
	select {
	case event := <-a.Updates.Subscribe(request.Context()):
		body, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = writer.Write(append([]byte("event: update\ndata: "), append(body, []byte("\n\n")...)...))
		if flusher != nil {
			flusher.Flush()
		}
	case <-request.Context().Done():
	}
}

func (a *WebAPI) readState(writer http.ResponseWriter) {
	if a == nil || a.Boundary == nil {
		http.Error(writer, "discovery unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := a.Boundary.ReadSnapshot()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (a *WebAPI) command(writer http.ResponseWriter, request *http.Request) {
	if a == nil || a.Boundary == nil || a.Publisher == nil {
		http.Error(writer, "discovery unavailable", http.StatusServiceUnavailable)
		return
	}
	var input webCommand
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, MaxBoundaryPayload)).Decode(&input); err != nil {
		http.Error(writer, "invalid command", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.Command) == "" {
		http.Error(writer, "command is required", http.StatusBadRequest)
		return
	}
	event, err := a.Boundary.WebCommand(input.Command, input.Arguments)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.Publisher.Publish(event); err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(writer).Encode(map[string]any{"event_id": event.ID, "type": event.Type})
}

func (a *WebAPI) message(writer http.ResponseWriter, request *http.Request) {
	if a == nil || a.Boundary == nil || a.Publisher == nil {
		http.Error(writer, "discovery unavailable", http.StatusServiceUnavailable)
		return
	}
	var event contract.Event
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, MaxBoundaryPayload)).Decode(&event); err != nil {
		http.Error(writer, "invalid message", http.StatusBadRequest)
		return
	}
	event.Source = "discovery"
	normalized, err := a.Boundary.NormalizeEvent(event)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.Publisher.Publish(normalized); err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(writer).Encode(map[string]any{"event_id": normalized.ID, "type": normalized.Type})
}

func (a *WebAPI) Validate() error {
	if a == nil || a.Boundary == nil || a.Publisher == nil {
		return errors.New("Web API requires Discovery boundary and event publisher")
	}
	return nil
}
