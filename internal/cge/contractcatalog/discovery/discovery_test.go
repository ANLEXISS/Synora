package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"synora/internal/cge/contractcatalog/gosurface"
)

func writeFixture(t *testing.T, root, relative, source string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTransportDiscoveryIsIndependentOfCatalog(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cmd/synora-api/routes.go", `package api
import "net/http"
func register(r *http.ServeMux) { r.HandleFunc("/api/cge/new-surface", nil) }
`)
	surfaces, err := ScanTransports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != 1 || surfaces[0].Path != "/api/cge/new-surface" {
		t.Fatalf("unexpected transport discovery: %+v", surfaces)
	}
}

func TestWriterDiscoveryIsIndependentOfCatalog(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/cge/writer.go", `package cge
import ("os"; "encoding/json")
func writeNew(path string, value any) error { data, err := json.Marshal(value); if err != nil { return err }; return os.WriteFile(path, data, 0600) }
`)
	writers, err := ScanWriters(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(writers) != 1 || writers[0].Function != "writeNew" || writers[0].Operation != "WriteFile" {
		t.Fatalf("unexpected writer discovery: %+v", writers)
	}
}

func TestOutputDiscoveryAndSemanticCandidatesAreIndependent(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/cge/engine.go", `package cge
type ObservationResult struct{}
type ShadowEngine struct{}
func (ShadowEngine) NewOutput() (ObservationResult, error) { return ObservationResult{}, nil }
`)
	outputs, err := ScanOutputs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || outputs[0].Type != "ObservationResult" {
		t.Fatalf("unexpected output discovery: %+v", outputs)
	}
	inventory := gosurface.Inventory{Types: []gosurface.InventoryType{{
		Package: "synora/test", Name: "Payload", Fields: []gosurface.InventoryField{
			{GoField: "SourceObservationID", WireName: "source_observation_id", GoType: "string"},
			{GoField: "DeliveredAt", WireName: "delivered_at", GoType: "time.Time"},
		}}}}
	identifiers, timestamps := ScanSemanticCandidates(inventory)
	if len(identifiers) != 1 || identifiers[0].Field != "SourceObservationID" {
		t.Fatalf("unexpected identifier candidates: %+v", identifiers)
	}
	if len(timestamps) != 1 || timestamps[0].Field != "DeliveredAt" {
		t.Fatalf("unexpected timestamp candidates: %+v", timestamps)
	}
}
