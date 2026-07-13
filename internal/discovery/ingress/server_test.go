package ingress

import (
	"testing"
)

func TestStartServerDisablesTLSIngressWhenCertificatesAreMissing(t *testing.T) {
	status := ""
	reason := ""
	StartServer(Config{
		Addr:     "127.0.0.1:0",
		CertFile: t.TempDir() + "/server.crt",
		KeyFile:  t.TempDir() + "/server.key",
		OnStatus: func(value, message string) {
			status, reason = value, message
		},
	})
	if status != "disabled" || reason != "tls_cert_missing" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

func TestRegularFileRejectsMissingAndDirectories(t *testing.T) {
	if regularFile(t.TempDir()) {
		t.Fatal("directory must not be accepted as a TLS certificate")
	}
	if regularFile(t.TempDir() + "/missing.crt") {
		t.Fatal("missing certificate must not be accepted")
	}
}
