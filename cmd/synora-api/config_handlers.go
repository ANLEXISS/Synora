package main

import (
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

func handleResidentCollection(core residentConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.Residents()
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
			item, err := core.CreateResident(body)
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

func handleResidentItem(core residentConfigurationProvider) http.HandlerFunc {
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
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			body, valid := readJSONObject(w, r, true)
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
