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

func TestRecursiveReachabilityFindsNestedAndCrossPackageTypes(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/cge/root/root.go", `package root
type ContractRoot struct { Payload NestedPayload `+"`json:\"payload\"`"+` }
`)
	writeFixture(t, root, "internal/cge/nested/nested.go", `package nested
type NestedPayload struct { Reference SensitiveReference `+"`json:\"reference\"`"+` }
type SensitiveReference struct { EntityID string `+"`json:\"entity_id\"`"+` }
`)
	inventory := gosurface.Inventory{Types: []gosurface.InventoryType{
		{Package: "synora/internal/cge/root", Name: "ContractRoot", Fields: []gosurface.InventoryField{{GoField: "Payload", FieldPath: "Payload", GoType: "NestedPayload", WireName: "payload"}}},
		{Package: "synora/internal/cge/nested", Name: "NestedPayload", Fields: []gosurface.InventoryField{{GoField: "Reference", FieldPath: "Reference", GoType: "SensitiveReference", WireName: "reference"}}},
		{Package: "synora/internal/cge/nested", Name: "SensitiveReference", Fields: []gosurface.InventoryField{{GoField: "EntityID", FieldPath: "EntityID", GoType: "string", WireName: "entity_id"}}},
	}}
	reachability := RecursiveReachability(inventory, []string{"synora/internal/cge/root/ContractRoot"})
	if len(reachability.Types) != 3 || !reachability.Types["synora/internal/cge/nested/SensitiveReference"] {
		t.Fatalf("nested types escaped reachability: %+v", reachability)
	}
	if !reachability.Fields["synora/internal/cge/nested/SensitiveReference/EntityID"] {
		t.Fatalf("nested field escaped reachability: %+v", reachability)
	}
}

func TestRecursiveReachabilityFollowsSlicesMapsAndEmbeds(t *testing.T) {
	inventory := gosurface.Inventory{Types: []gosurface.InventoryType{
		{Package: "synora/test", Name: "Root", Fields: []gosurface.InventoryField{
			{GoField: "Items", FieldPath: "Items", GoType: "[]Nested", WireName: "items"},
			{GoField: "Lookup", FieldPath: "Lookup", GoType: "map[string]Nested", WireName: "lookup"},
			{GoField: "Embedded", FieldPath: "Embedded", GoType: "Embedded", WireName: "embedded"},
		}},
		{Package: "synora/test", Name: "Nested", Fields: []gosurface.InventoryField{{GoField: "Reference", FieldPath: "Reference", GoType: "SensitiveReference", WireName: "reference"}}},
		{Package: "synora/test", Name: "Embedded", Fields: []gosurface.InventoryField{{GoField: "Reference", FieldPath: "Reference", GoType: "SensitiveReference", WireName: "reference"}}},
		{Package: "synora/test", Name: "SensitiveReference", Fields: []gosurface.InventoryField{{GoField: "EntityID", FieldPath: "EntityID", GoType: "string", WireName: "entity_id"}}},
	}}
	reachability := RecursiveReachability(inventory, []string{"synora/test/Root"})
	if len(reachability.Types) != 4 || len(reachability.Fields) != 6 { t.Fatalf("container or embedded reachability incomplete: %+v", reachability) }
}

func TestTransportDiscoveryFindsRPCBusAndWebSocket(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cmd/synora-core/transport.go", `package core
var rpc = map[string]func(){}
var bus busFacade
type busFacade struct{}
func (busFacade) SubscribeChannel(string) {}
func (busFacade) Send(Message) {}
type Message struct { Type string }
func register() { rpc["fixture.rpc"] = nil; bus.SubscribeChannel("fixture.channel"); bus.Send("fixture.channel", Message{Type:"fixture.bus"}) }
`)
	writeFixture(t, root, "cmd/synora-api/socket.go", `package api
type socket struct{}
func (socket) WriteJSON(value any) {}
func emit(s socket, value any) { s.WriteJSON(value) }
`)
	surfaces, err := ScanTransports(root)
	if err != nil {
		t.Fatal(err)
	}
	has := func(transport, method, path string) bool {
		for _, surface := range surfaces {
			if surface.Transport == transport && surface.Method == method && surface.Path == path {
				return true
			}
		}
		return false
	}
	if !has("rpc", "fixture.rpc", "") || !has("bus", "fixture.bus", "fixture.channel") || !has("websocket", "payload", "/ws") {
		t.Fatalf("incomplete transport discovery: %+v", surfaces)
	}
}

func TestTransportOutputDiscoveryFindsNonEngineResponse(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cmd/synora-api/response.go", `package api
import "encoding/json"
type TransportOnlyResponse struct { Value string `+"`json:\"value\"`"+` }
func handler(w jsonEncoder) { response := TransportOnlyResponse{}; w.Encode(response) }
type jsonEncoder interface { Encode(any) error }
`)
	outputs, err := ScanOutputs(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range outputs {
		if output.Type == "TransportOnlyResponse" && output.Transport == "http" {
			return
		}
	}
	t.Fatalf("transport-only output was not discovered: %+v", outputs)
}

func TestWriterDiscoveryDoesNotExcludePackageNames(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/cge/shadowworkflow/writer.go", `package shadowworkflow
import "os"
func PersistFixture(path string, data []byte) error { return os.WriteFile(path, data, 0600) }
`)
	writers, err := ScanWriters(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range writers {
		if writer.Function == "PersistFixture" {
			return
		}
	}
	t.Fatalf("writer in excluded package was not discovered: %+v", writers)
}

func TestPhysicalWriterRejectsGuardAfterWrite(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/cge/validation/writer.go", `package validation
import "os"
func bad(path string, data []byte) error { if err := os.WriteFile(path, data, 0600); err != nil { return err }; return ValidateStoreWrite() }
func ValidateStoreWrite() error { return nil }
`)
	sites, err := ScanWriteSites(root)
	if err != nil { t.Fatal(err) }
	if len(sites) != 1 || sites[0].Guarded { t.Fatalf("write-after-guard was accepted: %+v", sites) }
}
