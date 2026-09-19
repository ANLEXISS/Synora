package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"synora/pkg/contract"
)

type EventPublisher interface{ Publish(contract.Event) error }

type HistoryReader interface{ HistoryJSON() ([]byte, error) }

type EventSubscription interface {
	Subscribe(context.Context) <-chan contract.Event
}

type WebAPI struct {
	Boundary  *Boundary
	Publisher EventPublisher
	Health    func() map[string]any
	Updates   EventSubscription
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
