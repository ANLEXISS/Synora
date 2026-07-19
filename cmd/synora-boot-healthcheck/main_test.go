package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"synora/internal/boothealth"
)

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
