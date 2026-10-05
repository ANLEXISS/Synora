package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/internal/runtimeconfig"
)

func TestConfiguredHTTPSUsesExisting8443Configuration(t *testing.T) {
	root := t.TempDir()
	securityPath := filepath.Join(root, "security.yaml")
	certPath := filepath.Join(root, "synora.crt")
	keyPath := filepath.Join(root, "synora.key")
	if err := os.WriteFile(securityPath, []byte("server:\n  http_addr: \":8080\"\n  https_enabled: true\n  https_addr: \":8443\"\n  tls_cert_file: \""+certPath+"\"\n  tls_key_file: \""+keyPath+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, []byte("test-certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"SYNORA_HTTP_ADDR", "SYNORA_HTTPS_ADDR", "SYNORA_HTTPS_ENABLED", "SYNORA_TLS_CERT_FILE", "SYNORA_TLS_KEY_FILE"} {
		t.Setenv(key, "")
	}
	runtime := runtimeconfig.Defaults()
	runtime.Paths.Security = securityPath
	runtime.Paths.TLSCert = certPath
	runtime.Paths.TLSKey = keyPath
	got, err := loadAPIServerConfig(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HTTPSEnabled || got.HTTPAddr != ":8080" || got.HTTPSAddr != ":8443" || got.TLSCertFile != certPath || got.TLSKeyFile != keyPath {
		t.Fatalf("unexpected HTTPS configuration: %+v", got)
	}
}

func TestHealthIsLocalAndReadOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	handleHealth(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health response: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
