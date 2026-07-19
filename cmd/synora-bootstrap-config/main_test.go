package main

import (
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGeneratesLocalSecretsAndValidateAcceptsResult(t *testing.T) {
	etc := t.TempDir()
	if err := initConfig(options{etc: etc, templates: filepath.Join("..", "..", "configs"), apply: true}); err != nil {
		t.Fatal(err)
	}
	if err := validate(options{etc: etc}); err != nil {
		t.Fatal(err)
	}

	securityData, err := os.ReadFile(filepath.Join(etc, "security.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(securityData), "__GENERATED_AT_INSTALL__") || strings.Contains(string(securityData), "api_token:") {
		t.Fatalf("generated security config still contains bootstrap placeholder or plaintext token: %s", securityData)
	}
	for _, item := range []struct {
		name string
		mode os.FileMode
	}{
		{"api_token", 0600},
		{"session_secret", 0640},
		{"synoranet_psk", 0600},
		{"admin_initial_password", 0600},
	} {
		info, statErr := os.Stat(filepath.Join(etc, "secrets", item.name))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != item.mode {
			t.Fatalf("secret %s mode=%04o, want %04o", item.name, info.Mode().Perm(), item.mode)
		}
	}
}

func TestInitDoesNotReplaceExistingSecrets(t *testing.T) {
	etc := t.TempDir()
	opts := options{etc: etc, templates: filepath.Join("..", "..", "configs"), apply: true}
	if err := initConfig(opts); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(etc, "secrets", "api_token")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := initConfig(opts); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("init replaced an existing API token secret")
	}
}

func TestInitReusesExistingAdminPasswordSecret(t *testing.T) {
	etc := t.TempDir()
	secrets := filepath.Join(etc, "secrets")
	if err := os.MkdirAll(secrets, 0700); err != nil {
		t.Fatal(err)
	}
	password := "existing-admin-password-that-is-long-enough"
	if err := os.WriteFile(filepath.Join(secrets, "admin_initial_password"), []byte(password+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := initConfig(options{etc: etc, templates: filepath.Join("..", "..", "configs"), apply: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(etc, "auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var auth map[string]any
	if err := yaml.Unmarshal(data, &auth); err != nil {
		t.Fatal(err)
	}
	users := auth["users"].([]any)
	admin := users[0].(map[string]any)
	if err := bcrypt.CompareHashAndPassword([]byte(admin["password_hash"].(string)), []byte(password)); err != nil {
		t.Fatalf("existing admin password was not used: %v", err)
	}
}

func TestValidateRejectsPlaceholderAndWorldReadableSecret(t *testing.T) {
	etc := t.TempDir()
	if err := os.WriteFile(filepath.Join(etc, "security.yaml"), []byte("api_token_hash: __GENERATED_AT_INSTALL__\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(etc, "secrets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "secrets", "session_secret"), []byte("long-enough-secret-value-123456\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validate(options{etc: etc}); err == nil {
		t.Fatal("validate accepted placeholder/world-readable configuration")
	}
}

func TestInitDryRunDoesNotWrite(t *testing.T) {
	etc := filepath.Join(t.TempDir(), "etc")
	if err := initConfig(options{etc: etc, templates: filepath.Join("..", "..", "configs")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(etc); !os.IsNotExist(err) {
		t.Fatalf("dry-run created %s", etc)
	}
}

func TestValidateAcceptsSafeVersionedTemplates(t *testing.T) {
	if err := validate(options{templates: filepath.Join("..", "..", "configs"), template: true}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsDevelopmentAuth(t *testing.T) {
	etc := t.TempDir()
	if err := initConfig(options{etc: etc, templates: filepath.Join("..", "..", "configs"), apply: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(etc, "auth.yaml")
	if err := os.WriteFile(path, []byte("users:\n  - id: test-admin\n    login: test\n    role: admin\n    enabled: true\n    password: test\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := validate(options{etc: etc}); err == nil {
		t.Fatal("validate accepted development auth configuration")
	}
}

func TestTargetedRotationDoesNotTouchOtherSecrets(t *testing.T) {
	etc := t.TempDir()
	opts := options{etc: etc, templates: filepath.Join("..", "..", "configs"), apply: true}
	if err := initConfig(opts); err != nil {
		t.Fatal(err)
	}
	apiPath := filepath.Join(etc, "secrets", "api_token")
	pskPath := filepath.Join(etc, "secrets", "synoranet_psk")
	sessionPath := filepath.Join(etc, "secrets", "session_secret")
	apiBefore, _ := os.ReadFile(apiPath)
	pskBefore, _ := os.ReadFile(pskPath)
	sessionBefore, _ := os.ReadFile(sessionPath)

	if err := rotateAPIToken(opts); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(sessionPath); string(got) != string(sessionBefore) {
		t.Fatal("API token rotation changed session secret")
	}
	if got, _ := os.ReadFile(pskPath); string(got) != string(pskBefore) {
		t.Fatal("API token rotation changed SynoraNet PSK")
	}
	if got, _ := os.ReadFile(apiPath); string(got) == string(apiBefore) {
		t.Fatal("API token rotation did not change API token")
	}
	apiAfter, _ := os.ReadFile(apiPath)

	if err := rotateSynoraNetPSK(opts); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(apiPath); string(got) != string(apiAfter) {
		t.Fatal("PSK rotation unexpectedly changed API token")
	}
}
