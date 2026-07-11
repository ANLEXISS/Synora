package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotateAPITokenWritesNewPlainTokenAndHashAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.yaml")
	if err := os.WriteFile(path, []byte("api_token: old-token\nallowed_origins: []\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	token, err := RotateAPIToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token == "old-token" {
		t.Fatalf("unexpected rotated token %q", token)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	if !strings.Contains(contents, "api_token: "+token) || !strings.Contains(contents, "api_token_hash: "+HashSecret(token)) {
		t.Fatalf("rotated config missing token/hash: %s", contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("permissions changed from 0640 to %o", info.Mode().Perm())
	}
}

func TestLoadAppliesNativeHTTPSDefaultsAndConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.yaml")
	if err := os.WriteFile(path, []byte("api_token: dev-token\nserver:\n  http_addr: ':18080'\n  https_enabled: true\n  https_addr: ':18443'\n  tls_cert_file: '/tmp/synora.crt'\n  tls_key_file: '/tmp/synora.key'\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.HTTPAddr != ":18080" || !cfg.Server.HTTPSEnabled || cfg.Server.HTTPSAddr != ":18443" {
		t.Fatalf("unexpected server config: %#v", cfg.Server)
	}
	if cfg.Server.TLSCertFile != "/tmp/synora.crt" || cfg.Server.TLSKeyFile != "/tmp/synora.key" {
		t.Fatalf("unexpected TLS paths: %#v", cfg.Server)
	}
}
