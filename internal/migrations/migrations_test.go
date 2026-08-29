package migrations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanIsVersioned(t *testing.T) {
	plan, err := Plan(0, 3)
	if err != nil || len(plan) != 3 {
		t.Fatalf("unexpected plan: %#v %v", plan, err)
	}
	if plan[0].ID != "0001_network_security" || plan[2].ToSchema != 3 {
		t.Fatalf("unexpected migrations: %#v", plan)
	}
}

func TestDryRunIsIdempotentAndDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.yaml")
	original := []byte("schema_version: 0\napi_token_hash: hashed\n")
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	first, err := Apply(path, 0, 3, true)
	if err != nil || len(first.Planned) != 3 {
		t.Fatalf("unexpected dry run: %#v %v", first, err)
	}
	second, err := Apply(path, 0, 3, true)
	if err != nil || len(second.Planned) != len(first.Planned) {
		t.Fatalf("dry run not repeatable: %#v %v", second, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("dry-run changed migration target")
	}
}

func TestSkeletonApplyIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "features.yaml")
	original := []byte("schema_version: 0\nfeatures:\n  debug: false\n")
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	result, err := Apply(path, 0, 3, false)
	if err != nil || len(result.Applied) != 0 {
		t.Fatalf("unexpected skeleton apply: %#v %v", result, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("no-op skeleton changed file")
	}
}

func TestUnsupportedMigrationFailsBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.yaml")
	original := []byte("schema_version: 0\napi_token_hash: hashed\n")
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(path, 0, 4, false); err == nil {
		t.Fatal("unsupported migration target accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("failed migration mutated configuration: data=%q err=%v", data, err)
	}
}

func TestRestoreCheckpointPreservesOriginalData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.yaml")
	original := []byte("schema_version: 0\napi_token_hash: hashed\n")
	updated := []byte("schema_version: 1\napi_token_hash: hashed\n")
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	checkpoint := filepath.Join(filepath.Dir(path), "checkpoint.yaml")
	if err := os.WriteFile(checkpoint, original, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0640); err != nil {
		t.Fatal(err)
	}
	if err := RestoreCheckpoint(path, checkpoint); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("restored data=%q err=%v", data, err)
	}
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("checkpoint removed: %v", err)
	}
}

func TestSanitizeError(t *testing.T) {
	if got := SanitizeError(os.ErrPermission); got == "" {
		t.Fatal("expected error")
	}
	if got := SanitizeError(assertError("api_token=secret-value")); got != "migration failed (sensitive details redacted)" {
		t.Fatalf("got %q", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
