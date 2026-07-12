package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webapi "synora/internal/api"
)

type fakeCGEProfileProvider struct {
	profile map[string]any
	updated bool
}

func (f *fakeCGEProfileProvider) CGESecurityProfile() (map[string]any, error) { return f.profile, nil }
func (f *fakeCGEProfileProvider) UpdateCGESecurityProfile(json.RawMessage) (map[string]any, error) {
	f.updated = true
	return f.profile, nil
}

type fakeCGEFeedbackProvider struct{}

func (fakeCGEFeedbackProvider) CgeFeedbackList(map[string]any) ([]map[string]any, error) {
	return []map[string]any{}, nil
}
func (fakeCGEFeedbackProvider) SubmitCgeEvaluationFeedback(json.RawMessage) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}
func (fakeCGEFeedbackProvider) SubmitCgeChainFeedback(json.RawMessage) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func withPrincipal(r *http.Request, role string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authPrincipalContextKey{}, webapi.AuthUser{ID: role, Role: role}))
}

func TestCGESecurityProfilePatchIsAdminOnly(t *testing.T) {
	provider := &fakeCGEProfileProvider{profile: map[string]any{"mode": "balanced"}}
	residentRequest := withPrincipal(httptest.NewRequest(http.MethodPatch, "/api/cge/security-profile", strings.NewReader(`{"mode":"strict"}`)), webapi.RoleResident)
	resident := httptest.NewRecorder()
	handleCGESecurityProfile(provider).ServeHTTP(resident, residentRequest)
	if resident.Code != http.StatusForbidden || provider.updated {
		t.Fatalf("resident patch status=%d updated=%v", resident.Code, provider.updated)
	}
	admin := httptest.NewRecorder()
	handleCGESecurityProfile(provider).ServeHTTP(admin, withPrincipal(httptest.NewRequest(http.MethodPatch, "/api/cge/security-profile", strings.NewReader(`{"mode":"strict"}`)), webapi.RoleAdmin))
	if admin.Code != http.StatusOK || !provider.updated {
		t.Fatalf("admin patch status=%d updated=%v", admin.Code, provider.updated)
	}
}

func TestCGEFeedbackPostIsAdminOnly(t *testing.T) {
	provider := fakeCGEFeedbackProvider{}
	resident := httptest.NewRecorder()
	handleCGEFeedbackEvaluation(provider).ServeHTTP(resident, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/cge/feedback/evaluation", strings.NewReader(`{"chain_id":"c","event_id":"e"}`)), webapi.RoleResident))
	if resident.Code != http.StatusForbidden {
		t.Fatalf("resident feedback status=%d", resident.Code)
	}
	admin := httptest.NewRecorder()
	handleCGEFeedbackEvaluation(provider).ServeHTTP(admin, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/cge/feedback/evaluation", strings.NewReader(`{"chain_id":"c","event_id":"e"}`)), webapi.RoleAdmin))
	if admin.Code != http.StatusOK {
		t.Fatalf("admin feedback status=%d body=%s", admin.Code, admin.Body.String())
	}
}
