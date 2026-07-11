package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"synora/pkg/contract"
)

type deviceConfigurationProvider interface {
	Devices() ([]map[string]any, error)
	Device(string) (map[string]any, error)
	CreateDevice(json.RawMessage) (map[string]any, error)
	UpdateDevice(string, json.RawMessage) (map[string]any, error)
	DeleteDevice(string) (map[string]any, error)
}

type residentConfigurationProvider interface {
	Residents() ([]map[string]any, error)
	Resident(string) (map[string]any, error)
	CreateResident(json.RawMessage) (map[string]any, error)
	UpdateResident(string, json.RawMessage) (map[string]any, error)
	DeleteResident(string) (map[string]any, error)
}

func registerResidentRoutes(mux *http.ServeMux, core residentConfigurationProvider, faces *faceStore) {
	mux.HandleFunc("/api/residents", handleResidentCollection(core, faces))
	mux.HandleFunc("/api/residents/", handleResidentRoute(core, faces))
}

// residentCreateRequest and residentPatchRequest are the HTTP boundary for
// resident configuration. Runtime presence and face_profile are deliberately
// absent; face_profile is managed only by face endpoints.
type residentCreateRequest struct {
	ID              string  `json:"id"`
	FirstName       *string `json:"first_name,omitempty"`
	LastName        *string `json:"last_name,omitempty"`
	DisplayName     *string `json:"display_name,omitempty"`
	Role            *string `json:"role,omitempty"`
	Admin           *bool   `json:"admin,omitempty"`
	Trusted         *bool   `json:"trusted,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	ReferenceNodeID *string `json:"reference_node_id,omitempty"`
	AccountID       *string `json:"account_id,omitempty"`
}

type residentPatchRequest struct {
	FirstName       *string `json:"first_name,omitempty"`
	LastName        *string `json:"last_name,omitempty"`
	DisplayName     *string `json:"display_name,omitempty"`
	Role            *string `json:"role,omitempty"`
	Admin           *bool   `json:"admin,omitempty"`
	Trusted         *bool   `json:"trusted,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	ReferenceNodeID *string `json:"reference_node_id,omitempty"`
	AccountID       *string `json:"account_id,omitempty"`
}

func normalizeResidentHTTPPayload(w http.ResponseWriter, body json.RawMessage, create bool) (json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if create {
		var request residentCreateRequest
		if err := decoder.Decode(&request); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid resident JSON: %v", err))
			return nil, false
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid resident JSON"))
			return nil, false
		}
		return encoded, true
	}
	var request residentPatchRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid resident JSON: %v", err))
		return nil, false
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid resident JSON"))
		return nil, false
	}
	return encoded, true
}

type automationConfigurationProvider interface {
	Automations() ([]map[string]any, error)
	Automation(string) (map[string]any, error)
	CreateAutomation(json.RawMessage) (map[string]any, error)
	UpdateAutomation(string, json.RawMessage) (map[string]any, error)
	DeleteAutomation(string) (map[string]any, error)
}

type topologyConfigurationProvider interface {
	Topology() (map[string]any, error)
	ReplaceTopology(json.RawMessage) (map[string]any, error)
	DeleteTopology() (map[string]any, error)
}

type validationConfigurationProvider interface {
	Validations() ([]contract.ValidationRequest, error)
	Validation(string) (*contract.ValidationRequest, error)
	CreateValidation(json.RawMessage) (*contract.ValidationRequest, error)
	UpdateValidation(string, json.RawMessage) (*contract.ValidationRequest, error)
	DeleteValidation(string) (*contract.ValidationRequest, error)
	ResolveValidation(string, json.RawMessage) (*contract.ValidationRequest, error)
}

func handleDeviceCollection(core deviceConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.Devices()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			item, err := core.CreateDevice(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

func handleDeviceItem(core deviceConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := resourceID(r.URL.Path, "/api/devices/")
		if !ok {
			writeRouteNotFound(w, "device")
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, err := core.Device(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			body, valid := readJSONObject(w, r, true)
			if !valid {
				return
			}
			item, err := core.UpdateDevice(id, body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			item, err := core.DeleteDevice(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
	}
}

func rejectFaceProfileMutation(w http.ResponseWriter, body json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid resident payload"))
		return false
	}
	if _, exists := fields["face_profile"]; exists {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "face_profile is managed through the face endpoints",
		})
		return false
	}
	return true
}

func handleResidentCollection(core residentConfigurationProvider, faceStores ...*faceStore) http.HandlerFunc {
	var faces *faceStore
	if len(faceStores) > 0 {
		faces = faceStores[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.Residents()
			if err != nil {
				writeError(w, err)
				return
			}
			enrichResidentFaceProfiles(items, core, faces)
			writeJSON(w, http.StatusOK, redactResidentItems(items, r))
		case http.MethodPost:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			if !rejectFaceProfileMutation(w, body) {
				return
			}
			body, ok = normalizeResidentHTTPPayload(w, body, true)
			if !ok {
				return
			}
			item, err := core.CreateResident(body)
			if err != nil {
				writeError(w, err)
				return
			}
			if faces != nil {
				if id, ok := item["id"].(string); ok {
					if err := faces.ensureResidentDirs(id); err != nil {
						writeError(w, err)
						return
					}
				}
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

func handleResidentItem(core residentConfigurationProvider, faceStores ...*faceStore) http.HandlerFunc {
	var faces *faceStore
	if len(faceStores) > 0 {
		faces = faceStores[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := resourceID(r.URL.Path, "/api/residents/")
		if !ok {
			writeRouteNotFound(w, "resident")
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, err := core.Resident(id)
			if err != nil {
				writeError(w, err)
				return
			}
			enrichResidentFaceProfiles([]map[string]any{item}, core, faces)
			writeJSON(w, http.StatusOK, redactResidentItem(item, r))
		case http.MethodPatch:
			body, valid := readJSONObject(w, r, true)
			if !valid {
				return
			}
			if !rejectFaceProfileMutation(w, body) {
				return
			}
			body, valid = normalizeResidentHTTPPayload(w, body, false)
			if !valid {
				return
			}
			item, err := core.UpdateResident(id, body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			item, err := core.DeleteResident(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
	}
}

func enrichResidentFaceProfiles(items []map[string]any, core residentConfigurationProvider, faces *faceStore) {
	if faces == nil {
		return
	}
	for _, item := range items {
		id, ok := item["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			continue
		}
		profile, err := faces.profile(core, id)
		if err == nil {
			item["face_profile"] = profile
		}
	}
}

func redactResidentItems(items []map[string]any, r *http.Request) []map[string]any {
	if isAdminRequest(r) {
		return items
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, redactResidentItem(item, r))
	}
	return out
}

func redactResidentItem(item map[string]any, r *http.Request) map[string]any {
	if item == nil || isAdminRequest(r) {
		return item
	}
	copy := make(map[string]any, len(item))
	for key, value := range item {
		switch strings.ToLower(key) {
		case "contact", "baseline", "presence_profile", "identity_profile", "permissions", "face_profile":
			continue
		default:
			copy[key] = value
		}
	}
	return copy
}

func handleAutomationCollection(core automationConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.Automations()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			item, err := core.CreateAutomation(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

func handleAutomationItem(core automationConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := resourceID(r.URL.Path, "/api/automations/")
		if !ok {
			writeRouteNotFound(w, "automation")
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, err := core.Automation(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			body, valid := readJSONObject(w, r, true)
			if !valid {
				return
			}
			item, err := core.UpdateAutomation(id, body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			item, err := core.DeleteAutomation(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
	}
}

func handleTopologyConfiguration(core topologyConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.Topology()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			items, err := core.ReplaceTopology(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodDelete:
			items, err := core.DeleteTopology()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodDelete)
		}
	}
}

func handleTopologySubroute() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeRouteNotFound(w, "topology")
	}
}

func handleValidationCollection(core validationConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.Validations()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			item, err := core.CreateValidation(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

func handleValidationItem(core validationConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/validations/"))
		parts := strings.Split(path, "/")
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && parts[1] == "resolve" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			item, err := core.ResolveValidation(strings.TrimSpace(parts[0]), body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
			return
		}

		id, ok := resourceID(r.URL.Path, "/api/validations/")
		if !ok {
			writeRouteNotFound(w, "validation")
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, err := core.Validation(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			body, valid := readJSONObject(w, r, true)
			if !valid {
				return
			}
			item, err := core.UpdateValidation(id, body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			item, err := core.DeleteValidation(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
	}
}
