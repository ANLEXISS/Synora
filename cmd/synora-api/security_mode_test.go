package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	webapi "synora/internal/api"
	"synora/pkg/contract"
)

type fakeSecurityModeProvider struct{ mode contract.SecurityModeState }

func (f *fakeSecurityModeProvider) SecurityMode() (contract.SecurityModeState, error) {
	return f.mode, nil
}
func (f *fakeSecurityModeProvider) SetSecurityMode(body json.RawMessage) (contract.SecurityModeState, error) {
	return f.mode, nil
}
func (f *fakeSecurityModeProvider) ArmSecurity(body json.RawMessage) (contract.SecurityModeState, error) {
	return f.mode, nil
}
func (f *fakeSecurityModeProvider) DisarmSecurity(body json.RawMessage) (contract.SecurityModeState, error) {
	return f.mode, nil
}

func TestSecurityModeWritesAreAdminOnly(t *testing.T) {
	provider := &fakeSecurityModeProvider{mode: contract.DefaultSecurityModeState(time.Now().UTC())}
	resident := httptest.NewRecorder()
	handleSecurityArm(provider).ServeHTTP(resident, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/security/arm", strings.NewReader(`{}`)), webapi.RoleResident))
	if resident.Code != http.StatusForbidden {
		t.Fatalf("resident arm status=%d body=%s", resident.Code, resident.Body.String())
	}
	admin := httptest.NewRecorder()
	handleSecurityArm(provider).ServeHTTP(admin, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/security/arm", strings.NewReader(`{}`)), webapi.RoleAdmin))
	if admin.Code != http.StatusOK {
		t.Fatalf("admin arm status=%d body=%s", admin.Code, admin.Body.String())
	}
}
