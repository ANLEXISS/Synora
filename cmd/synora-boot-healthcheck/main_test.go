package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"synora/internal/boothealth"
)

func TestGetVersionRejectsCommitMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/system/version" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"git_commit":"deployed-1","bundle_id":"local-deployed-1"}`)
	}))
	defer server.Close()
	status, _ := getVersion(context.Background(), server.URL, "source-2")
	if status != "fatal" {
		t.Fatalf("commit mismatch status=%q, want fatal", status)
	}
}

func TestGetVersionAcceptsMatchingCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"git_commit":"source-2","bundle_id":"local-source-2"}`)
	}))
	defer server.Close()
	status, message := getVersion(context.Background(), server.URL, "source-2")
	if status != "ok" || message == "" {
		t.Fatalf("matching version status=%q message=%q", status, message)
	}
}

func TestWriteReportDoesNotContainSecretValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-health.json")
	report := boothealth.Report{
		Status:          boothealth.StatusDegraded,
		CheckedAt:       "2026-01-01T00:00:00Z",
		Checks:          []boothealth.Check{{Name: "model.weapon", Status: "degraded", Message: "optional model missing"}},
		DegradedReasons: []string{"model.weapon"},
	}
	if err := writeReport(path, report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || string(data) == "" {
		t.Fatal("invalid report")
	}
	for _, secret := range []string{"password", "token", "psk", "private_key"} {
		if containsFold(string(data), secret) {
			t.Fatalf("report contains sensitive field %q: %s", secret, data)
		}
	}
}

func containsFold(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		match := true
		for j := range needle {
			left, right := value[i+j], needle[j]
			if left >= 'A' && left <= 'Z' {
				left += 'a' - 'A'
			}
			if right >= 'A' && right <= 'Z' {
				right += 'a' - 'A'
			}
			if left != right {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
