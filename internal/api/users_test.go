package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserDirectoryAuthenticatesWithoutExposingPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auth.yaml")
	contents := []byte("users:\n  - id: user-carole\n    login: carole\n    resident_id: carole\n    role: resident\n    enabled: true\n    password_hash: " + hash + "\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	directory, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	user, ok := directory.Authenticate("carole", "correct horse battery staple")
	if !ok {
		t.Fatal("expected password authentication to succeed")
	}
	if user.ID != "user-carole" || user.Role != RoleResident || user.ResidentID != "carole" {
		t.Fatalf("unexpected user: %#v", user)
	}
	if strings.Contains(string(contents), user.Login) == false || strings.Contains(string(contents), "password_hash") == false {
		t.Fatal("test auth config should contain its hash fixture")
	}
	if strings.Contains(string([]byte(user.Login)), hash) {
		t.Fatal("password hash leaked into public user")
	}
	if user.HasPermission(PermissionDevicesWrite) || !user.HasPermission(PermissionDevicesRead) {
		t.Fatalf("unexpected resident permissions: %#v", user.Permissions)
	}
	if _, ok := directory.Authenticate("carole", "wrong"); ok {
		t.Fatal("wrong password authenticated")
	}
}

func TestAuthLoginWithUserCredentialsReturnsRoleAndPermissions(t *testing.T) {
	hash, err := HashPassword("resident-password")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte("users:\n  - id: user-carole\n    login: carole\n    resident_id: carole\n    role: resident\n    enabled: true\n    password_hash: "+hash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	service := newAuthTestService(t)
	service.Users = directory

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"login":"carole","password":"resident-password"}`))
	rec := httptest.NewRecorder()
	service.LoginHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":"user-carole"`) || !strings.Contains(body, `"role":"resident"`) || !strings.Contains(body, `"resident_id":"carole"`) {
		t.Fatalf("missing user claims: %s", body)
	}
	if strings.Contains(body, hash) || strings.Contains(body, "password_hash") {
		t.Fatalf("password material leaked: %s", body)
	}

	cookie := mustCookie(t, rec)
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(cookie)
	meRec := httptest.NewRecorder()
	service.MeHandler(meRec, meReq)
	if meRec.Code != http.StatusOK || !strings.Contains(meRec.Body.String(), `"automations:read"`) {
		t.Fatalf("unexpected me response: %d %s", meRec.Code, meRec.Body.String())
	}
}

func TestSessionUserRefreshesRoleFromDirectory(t *testing.T) {
	adminHash, err := HashPassword("admin-password")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte("users:\n  - id: user-alexis\n    login: alexis\n    resident_id: alexis\n    role: admin\n    enabled: true\n    password_hash: "+adminHash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	service := newAuthTestService(t)
	service.Users = directory
	rawID, _, err := service.Sessions.Create(AuthUser{ID: "user-alexis", Login: "alexis", Role: RoleAdmin, Source: "auth.yaml", Permissions: PermissionsForRole(RoleAdmin)})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: rawID})
	rec := httptest.NewRecorder()
	service.MeHandler(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatalf("unexpected role response: %s", rec.Body.String())
	}
	record := directory.byID["user-alexis"]
	record.Role = RoleResident
	directory.byID["user-alexis"] = record
	directory.byLogin["alexis"] = record
	rec = httptest.NewRecorder()
	service.MeHandler(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"resident"`) {
		t.Fatalf("role change was not reloaded: %s", rec.Body.String())
	}
}
