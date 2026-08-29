package ota

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyMarksGoodAndClearsJournal(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "update.raucb")
	if err := os.WriteFile(bundle, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	c := NewController(run, "rauc", "healthcheck")
	c.SetJournalPath(filepath.Join(t.TempDir(), "ota", "journal.json"))
	if err := c.Apply(context.Background(), bundle); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0] != "rauc install "+bundle || calls[1] != "healthcheck run --readonly" || calls[2] != "rauc status mark-good" {
		t.Fatalf("calls=%v", calls)
	}
	if _, err := os.Stat(c.journalPath); !os.IsNotExist(err) {
		t.Fatalf("journal remains after successful update: %v", err)
	}
}

func TestApplyRequestsRollbackAfterHealthFailure(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "update.raucb")
	if err := os.WriteFile(bundle, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "healthcheck" {
			return nil, errors.New("new slot unhealthy")
		}
		return nil, nil
	}
	if err := NewController(run, "rauc", "healthcheck").Apply(context.Background(), bundle); err == nil {
		t.Fatal("unhealthy slot accepted")
	}
	if len(calls) != 3 || calls[2] != "rauc status mark-bad" {
		t.Fatalf("rollback calls=%v", calls)
	}
}

func TestRecoverMarksInterruptedUpdateBad(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	c := NewController(run, "rauc", "healthcheck")
	c.SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	if err := c.writeJournal("installed", "/var/lib/update.raucb"); err != nil {
		t.Fatal(err)
	}
	if err := c.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "rauc status mark-bad" {
		t.Fatalf("recovery calls=%v", calls)
	}
}

func TestApplyRollsBackWhenInstallReportsInsufficientSpace(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "update.raucb")
	if err := os.WriteFile(bundle, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "rauc" && len(args) > 0 && args[0] == "install" {
			return []byte("not enough space"), errors.New("no space left on device")
		}
		return nil, nil
	}
	c := NewController(run, "rauc", "healthcheck")
	if err := c.Apply(context.Background(), bundle); err == nil {
		t.Fatal("insufficient space accepted")
	}
	if len(calls) != 2 || calls[1] != "rauc status mark-bad" {
		t.Fatalf("insufficient-space rollback calls=%v", calls)
	}
}

func TestApplyRollsBackWhenMigrationCannotBePlanned(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "update.raucb")
	if err := os.WriteFile(bundle, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "security.yaml")
	if err := os.WriteFile(config, []byte("schema_version: 0\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	sum := sha256.Sum256([]byte("bundle"))
	manifestValue := BundleManifest{SchemaVersion: ManifestSchemaVersion, Version: "1.5.0", BundleBytes: 6, BundleSHA256: hex.EncodeToString(sum[:]), MigrationTarget: 4}
	if err := manifestValue.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	manifestData, err := json.Marshal(manifestValue)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifest, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	c := NewController(run, "rauc", "healthcheck")
	c.SetVerification(Verification{ManifestPath: manifest, PublicKey: publicKey})
	c.SetMigration(config, 0)
	if err := c.Apply(context.Background(), bundle); err == nil {
		t.Fatal("unplanifiable migration accepted")
	}
	if len(calls) != 2 || calls[0] != "rauc install "+bundle || calls[1] != "rauc status mark-bad" {
		t.Fatalf("migration rollback calls=%v", calls)
	}
	data, err := os.ReadFile(config)
	if err != nil || string(data) != "schema_version: 0\n" {
		t.Fatalf("migration mutated config: %q err=%v", data, err)
	}
}
