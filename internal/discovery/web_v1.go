package discovery

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"synora/pkg/contract"
)

type EventPublisher interface{ Publish(contract.Event) error }

type WebAPI struct {
	Boundary  *Boundary
	Publisher EventPublisher
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
		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/commands" {
			a.command(writer, request)
			return
		}
		http.NotFound(writer, request)
	})
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

func (a *WebAPI) Validate() error {
	if a == nil || a.Boundary == nil || a.Publisher == nil {
		return errors.New("Web API requires Discovery boundary and event publisher")
	}
	return nil
}
