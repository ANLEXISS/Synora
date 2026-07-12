package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"synora/internal/security"
	"synora/pkg/contract"
)

type fakeDeviceConfiguration struct {
	items       []map[string]any
	item        map[string]any
	createdBody json.RawMessage
	updatedID   string
	updatedBody json.RawMessage
	deletedID   string
	err         error
}

func (f *fakeDeviceConfiguration) Devices() ([]map[string]any, error) {
	return f.items, f.err
}

func (f *fakeDeviceConfiguration) Device(string) (map[string]any, error) {
	return f.item, f.err
}

func (f *fakeDeviceConfiguration) CreateDevice(body json.RawMessage) (map[string]any, error) {
	f.createdBody = append(json.RawMessage(nil), body...)
	return f.item, f.err
}

func (f *fakeDeviceConfiguration) UpdateDevice(id string, body json.RawMessage) (map[string]any, error) {
	f.updatedID = id
	f.updatedBody = append(json.RawMessage(nil), body...)
	return f.item, f.err
}

func (f *fakeDeviceConfiguration) DeleteDevice(id string) (map[string]any, error) {
	f.deletedID = id
	return f.item, f.err
}

type fakeTopologyConfiguration struct {
	value map[string]any
}

type fakeResidentAutomationConfiguration struct {
	value map[string]any
}

type fakeHTTPResidentConfiguration struct {
	value map[string]any
}

func (f *fakeHTTPResidentConfiguration) Residents() ([]map[string]any, error) {
	return []map[string]any{f.value}, nil
}

func (f *fakeHTTPResidentConfiguration) Resident(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeHTTPResidentConfiguration) CreateResident(body json.RawMessage) (map[string]any, error) {
	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		return nil, err
	}
	for key, value := range patch {
		f.value[key] = value
	}
	return f.value, nil
}

func (f *fakeHTTPResidentConfiguration) UpdateResident(_ string, body json.RawMessage) (map[string]any, error) {
	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		return nil, err
	}
	for key, value := range patch {
		f.value[key] = value
	}
	return f.value, nil
}

func (f *fakeHTTPResidentConfiguration) DeleteResident(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) Residents() ([]map[string]any, error) {
	return []map[string]any{f.value}, nil
}

func (f *fakeResidentAutomationConfiguration) Resident(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) CreateResident(json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) UpdateResident(string, json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) DeleteResident(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) Automations() ([]map[string]any, error) {
	return []map[string]any{f.value}, nil
}

func (f *fakeResidentAutomationConfiguration) Automation(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) CreateAutomation(json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) UpdateAutomation(string, json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeResidentAutomationConfiguration) DeleteAutomation(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeTopologyConfiguration) Topology() (map[string]any, error) {
	return f.value, nil
}

func (f *fakeTopologyConfiguration) ReplaceTopology(json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeTopologyConfiguration) DeleteTopology() (map[string]any, error) {
	return f.value, nil
}

type mutableCGEProvider struct {
	fakeCGEProvider
	actionID   string
	actionName string
	actionBody json.RawMessage
}

type fakeCGEConfiguration struct {
	value map[string]any
}

type fakeValidationConfiguration struct {
	value *contract.ValidationRequest
}

func (f *fakeValidationConfiguration) Validations() ([]contract.ValidationRequest, error) {
	return []contract.ValidationRequest{*f.value}, nil
}

func (f *fakeValidationConfiguration) Validation(string) (*contract.ValidationRequest, error) {
	return f.value, nil
}

func (f *fakeValidationConfiguration) CreateValidation(json.RawMessage) (*contract.ValidationRequest, error) {
	return f.value, nil
}

func (f *fakeValidationConfiguration) UpdateValidation(string, json.RawMessage) (*contract.ValidationRequest, error) {
	return f.value, nil
}

func (f *fakeValidationConfiguration) DeleteValidation(string) (*contract.ValidationRequest, error) {
	return f.value, nil
}

func (f *fakeValidationConfiguration) ResolveValidation(string, json.RawMessage) (*contract.ValidationRequest, error) {
	return f.value, nil
}

func (f *fakeCGEConfiguration) CGECriticalSeeds(map[string]any) ([]map[string]any, error) {
	return []map[string]any{f.value}, nil
}

func (f *fakeCGEConfiguration) CGECriticalSeed(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeCGEConfiguration) CreateCGECriticalSeed(json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeCGEConfiguration) UpdateCGECriticalSeed(string, json.RawMessage) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeCGEConfiguration) DeleteCGECriticalSeed(string) (map[string]any, error) {
	return f.value, nil
}

func (f *fakeCGEConfiguration) CGEDangerAssessments(map[string]any) ([]map[string]any, error) {
	return []map[string]any{f.value}, nil
}

func (f *fakeCGEConfiguration) CGEDangerAssessment(string) (map[string]any, error) {
	return f.value, nil
}

func (f *mutableCGEProvider) UpdateCGELearnedBehavior(id string, _ json.RawMessage) (map[string]any, error) {
	return map[string]any{"id": id, "status": "suggested"}, nil
}

func (f *mutableCGEProvider) DeleteCGELearnedBehavior(id string) (map[string]any, error) {
	return map[string]any{"id": id, "status": "disabled"}, nil
}

func (f *mutableCGEProvider) ActOnCGELearnedBehavior(id string, action string, body json.RawMessage) (map[string]any, error) {
	f.actionID = id
	f.actionName = action
	f.actionBody = append(json.RawMessage(nil), body...)
	return map[string]any{"id": id, "status": "approved"}, nil
}

func TestDeviceConfigurationHandlersCRUDTransport(t *testing.T) {
	core := &fakeDeviceConfiguration{item: map[string]any{"id": "cam-1", "enabled": true}}

	req := httptest.NewRequest(http.MethodPost, "/api/devices", strings.NewReader(` { "id": "cam-1", "enabled": true } `))
	rec := httptest.NewRecorder()
	handleDeviceCollection(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/devices/cam-1", nil)
	rec = httptest.NewRecorder()
	handleDeviceItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"cam-1"`) {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(core.createdBody, &created); err != nil || created["id"] != "cam-1" {
		t.Fatalf("create body=%s err=%v", core.createdBody, err)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/devices/cam-1", strings.NewReader(`{"display_name":"Caméra entrée","room":"zoneA.L0.entree","enabled":false}`))
	rec = httptest.NewRecorder()
	handleDeviceItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || core.updatedID != "cam-1" {
		t.Fatalf("patch status=%d id=%q body=%s", rec.Code, core.updatedID, rec.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(core.updatedBody, &updated); err != nil || updated["display_name"] != "Caméra entrée" || updated["room"] != "zoneA.L0.entree" {
		t.Fatalf("patch body=%s err=%v", core.updatedBody, err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/devices/cam-1", nil)
	rec = httptest.NewRecorder()
	handleDeviceItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || core.deletedID != "cam-1" {
		t.Fatalf("delete status=%d id=%q body=%s", rec.Code, core.deletedID, rec.Body.String())
	}
}

func TestResidentAndAutomationHandlersExposeCRUDMethods(t *testing.T) {
	core := &fakeResidentAutomationConfiguration{value: map[string]any{"id": "item-1", "enabled": true}}
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		handler    http.Handler
		wantStatus int
	}{
		{"resident list", http.MethodGet, "/api/residents", "", handleResidentCollection(core), http.StatusOK},
		{"resident create", http.MethodPost, "/api/residents", `{"id":"item-1"}`, handleResidentCollection(core), http.StatusCreated},
		{"resident get", http.MethodGet, "/api/residents/item-1", "", handleResidentItem(core), http.StatusOK},
		{"resident patch", http.MethodPatch, "/api/residents/item-1", `{"enabled":false}`, handleResidentItem(core), http.StatusOK},
		{"resident delete", http.MethodDelete, "/api/residents/item-1", "", handleResidentItem(core), http.StatusOK},
		{"automation list", http.MethodGet, "/api/automations", "", handleAutomationCollection(core), http.StatusOK},
		{"automation create", http.MethodPost, "/api/automations", `{"id":"item-1"}`, handleAutomationCollection(core), http.StatusCreated},
		{"automation get", http.MethodGet, "/api/automations/item-1", "", handleAutomationItem(core), http.StatusOK},
		{"automation patch", http.MethodPatch, "/api/automations/item-1", `{"enabled":false}`, handleAutomationItem(core), http.StatusOK},
		{"automation delete", http.MethodDelete, "/api/automations/item-1", "", handleAutomationItem(core), http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestResidentHandlerRejectsFaceProfileMutation(t *testing.T) {
	core := &fakeResidentAutomationConfiguration{value: map[string]any{"id": "alexis"}}
	req := httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(`{"face_profile":{"status":"ready"}}`))
	rec := httptest.NewRecorder()
	handleResidentItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "managed through the face endpoints") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPatchResidentHTTPAcceptsAccountID(t *testing.T) {
	core := &fakeHTTPResidentConfiguration{value: map[string]any{"id": "alexis", "account_id": "old"}}
	req := httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(`{"account_id":"user_alexis"}`))
	rec := httptest.NewRecorder()
	handleResidentItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"account_id":"user_alexis"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPatchResidentHTTPCanClearAccountID(t *testing.T) {
	core := &fakeHTTPResidentConfiguration{value: map[string]any{"id": "alexis", "account_id": "user_alexis"}}
	req := httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(`{"account_id":""}`))
	rec := httptest.NewRecorder()
	handleResidentItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"account_id":""`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPatchResidentHTTPAbsentAccountIDKeepsExisting(t *testing.T) {
	core := &fakeHTTPResidentConfiguration{value: map[string]any{"id": "alexis", "account_id": "user_alexis"}}
	req := httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(`{"display_name":"Alexis 2"}`))
	rec := httptest.NewRecorder()
	handleResidentItem(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"account_id":"user_alexis"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestResidentRouterUsesProductionPatchRoute(t *testing.T) {
	core := &fakeHTTPResidentConfiguration{value: map[string]any{"id": "alexis", "account_id": "old"}}
	mux := http.NewServeMux()
	registerResidentRoutes(mux, core, nil)

	body := `{"first_name":"Alexis","last_name":"Kratz","display_name":"Alexis","role":"resident","admin":true,"trusted":true,"reference_node_id":"zoneA.L1.chambre_enfant","account_id":"user_alexis"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"first_name":"Alexis"`) || !strings.Contains(rec.Body.String(), `"account_id":"user_alexis"`) {
		t.Fatalf("production route status=%d body=%s", rec.Code, rec.Body.String())
	}

	core.value["account_id"] = "user_alexis"
	req = httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(`{"display_name":"Alexis 2"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"account_id":"user_alexis"`) {
		t.Fatalf("account preservation status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/residents/alexis", strings.NewReader(`{"face_profile":{}}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "face_profile is managed through the face endpoints") {
		t.Fatalf("face profile status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestResidentRouterPostAcceptsFirstNameAndAccountID(t *testing.T) {
	core := &fakeHTTPResidentConfiguration{value: map[string]any{"id": "new-resident"}}
	mux := http.NewServeMux()
	registerResidentRoutes(mux, core, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{"id":"new-resident","first_name":"New","account_id":"user_new"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"account_id":"user_new"`) {
		t.Fatalf("production POST status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestConfigurationHandlerRejectsMalformedAndTrailingJSON(t *testing.T) {
	core := &fakeDeviceConfiguration{item: map[string]any{"id": "cam-1"}}
	for name, body := range map[string]string{
		"malformed": `{"id":`,
		"trailing":  `{"id":"cam-1"} {"id":"cam-2"}`,
		"array":     `[{"id":"cam-1"}]`,
		"null":      `null`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/devices", strings.NewReader(body))
			rec := httptest.NewRecorder()
			handleDeviceCollection(core).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), contract.ErrorInvalidJSON) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestConfigurationHandlerRejectsOversizedJSON(t *testing.T) {
	core := &fakeDeviceConfiguration{item: map[string]any{"id": "cam-1"}}
	body := `{"id":"cam-1","metadata":{"value":"` + strings.Repeat("x", maxAPIJSONBody) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/devices", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleDeviceCollection(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), contract.ErrorInvalidJSON) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTopologyOnlyAllowsGetPostDeleteAndResetIsNotPublic(t *testing.T) {
	core := &fakeTopologyConfiguration{value: map[string]any{
		"version": 1,
		"nodes":   []any{},
		"links":   []any{},
	}}

	req := httptest.NewRequest(http.MethodPatch, "/api/topology", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handleTopologyConfiguration(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, POST, DELETE" {
		t.Fatalf("patch status=%d allow=%q body=%s", rec.Code, rec.Header().Get("Allow"), rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/topology/reset", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	handleTopologySubroute().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("reset status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLearnedBehaviorApproveAllowsEmptyBody(t *testing.T) {
	core := &mutableCGEProvider{}
	req := httptest.NewRequest(http.MethodPost, "/api/cge/learned-behaviors/beh-1/approve", nil)
	rec := httptest.NewRecorder()
	handleCGEDetail(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if core.actionID != "beh-1" || core.actionName != "approve" || string(core.actionBody) != `{}` {
		t.Fatalf("action id=%q name=%q body=%s", core.actionID, core.actionName, core.actionBody)
	}
}

func TestCriticalSeedCRUDAndDangerAssessmentReadHandlers(t *testing.T) {
	core := &fakeCGEConfiguration{value: map[string]any{"id": "seed-1", "danger_score": 0.9}}
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		handler    http.Handler
		wantStatus int
	}{
		{"seed list", http.MethodGet, "/api/cge/critical-seeds", "", handleCGECriticalSeeds(core), http.StatusOK},
		{"seed create", http.MethodPost, "/api/cge/critical-seeds", `{"id":"seed-1","danger_score":0.9}`, handleCGECriticalSeeds(core), http.StatusCreated},
		{"seed get", http.MethodGet, "/api/cge/critical-seeds/seed-1", "", handleCGECriticalSeed(core), http.StatusOK},
		{"seed patch", http.MethodPatch, "/api/cge/critical-seeds/seed-1", `{"danger_score":0.8}`, handleCGECriticalSeed(core), http.StatusOK},
		{"seed delete", http.MethodDelete, "/api/cge/critical-seeds/seed-1", "", handleCGECriticalSeed(core), http.StatusOK},
		{"danger list", http.MethodGet, "/api/cge/danger-assessments", "", handleCGEDangerAssessments(core), http.StatusOK},
		{"danger get", http.MethodGet, "/api/cge/danger-assessments/danger-1", "", handleCGEDangerAssessment(core), http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestValidationCRUDHandlers(t *testing.T) {
	core := &fakeValidationConfiguration{value: &contract.ValidationRequest{
		ID: "validation-1", Type: contract.ValidationTypeBehaviorApproval, Status: contract.ValidationStatusPending,
	}}
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		handler    http.Handler
		wantStatus int
	}{
		{"list", http.MethodGet, "/api/validations", "", handleValidationCollection(core), http.StatusOK},
		{"create", http.MethodPost, "/api/validations", `{"id":"validation-1","type":"behavior_approval"}`, handleValidationCollection(core), http.StatusCreated},
		{"get", http.MethodGet, "/api/validations/validation-1", "", handleValidationItem(core), http.StatusOK},
		{"patch", http.MethodPatch, "/api/validations/validation-1", `{"notes":"approved"}`, handleValidationItem(core), http.StatusOK},
		{"delete", http.MethodDelete, "/api/validations/validation-1", "", handleValidationItem(core), http.StatusOK},
		{"resolve", http.MethodPost, "/api/validations/validation-1/resolve", `{"action":"accept"}`, handleValidationItem(core), http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWriteErrorMapsStableAPIErrors(t *testing.T) {
	cases := []struct {
		code   string
		status int
	}{
		{contract.ErrorInvalidJSON, http.StatusBadRequest},
		{contract.ErrorInvalidRequest, http.StatusBadRequest},
		{contract.ErrorNotFound, http.StatusNotFound},
		{contract.ErrorDuplicateID, http.StatusConflict},
		{contract.ErrorValidationFailed, http.StatusUnprocessableEntity},
		{contract.ErrorForbiddenAction, http.StatusForbidden},
		{contract.ErrorTopologyRequired, http.StatusConflict},
		{contract.ErrorUnsafeAutomation, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, contract.NewAPIError(tc.code, "safe message"))
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"error":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	rec := httptest.NewRecorder()
	writeError(rec, errors.New("/etc/synora/security.yaml failed"))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "security.yaml") {
		t.Fatalf("internal error leaked detail: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCORSAdvertisesPatch(t *testing.T) {
	cfg := &security.Config{AllowedOrigins: []string{"http://localhost:3000"}}
	handler := corsMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/devices/cam-1", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), http.MethodPatch) {
		t.Fatalf("status=%d methods=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Methods"))
	}
}
