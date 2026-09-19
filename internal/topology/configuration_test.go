package topology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/pkg/contract"
)

func TestFlatTopologyLoadsAndSavesCanonicalFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topology.yaml")
	flat := `version: 1
locked: true
root_id: house
nodes:
  - id: house
    name: House
    type: house
  - id: entry
    name: Entry
    type: room
    parent: house
  - id: hall
    name: Hall
    type: room
    parent: house
links:
  - from: entry
    to: hall
`
	if err := os.WriteFile(path, []byte(flat), 0o640); err != nil {
		t.Fatal(err)
	}
	topo := &Topology{}
	if err := Load(path, topo); err != nil {
		t.Fatal(err)
	}
	entry := topo.Nodes["entry"]
	hall := topo.Nodes["hall"]
	if entry == nil || hall == nil || !hasNeighbor(entry, hall) || !hasNeighbor(hall, entry) {
		t.Fatalf("link was not canonicalized: entry=%#v hall=%#v", entry, hall)
	}
	if err := Save(path, topo); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "nodes:") || !strings.Contains(string(written), "links:") || strings.Contains(string(written), "zones:") {
		t.Fatalf("topology was not saved in flat format:\n%s", written)
	}
	reloaded, err := LoadBytes(written)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Nodes) != len(topo.Nodes) || !reloaded.Locked {
		t.Fatalf("reloaded=%#v", reloaded.ConfigView())
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "topology.*.yaml"))
	if len(backups) != 1 {
		t.Fatalf("backups=%v", backups)
	}
}

func TestFlatTopologyValidatesCompleteGraph(t *testing.T) {
	config := contract.TopologyConfig{
		Version: TopologyConfigVersion,
		RootID:  "home",
		Nodes: []contract.TopologyNode{
			{ID: "home", Name: "Home", Type: string(NodeHouse)},
			{ID: "entry", Name: "Entry", Type: string(NodeRoom), Parent: "home"},
			{ID: "hall", Name: "Hall", Type: string(NodeRoom), Parent: "home"},
		},
		Links: []contract.TopologyLink{{From: "entry", To: "hall"}},
	}
	topo, err := FromConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	view := topo.ConfigView()
	if len(view.Nodes) != 3 || len(view.Links) != 1 || view.RootID != "home" {
		t.Fatalf("view=%#v", view)
	}

	invalid := config
	invalid.Links = []contract.TopologyLink{{From: "entry", To: "missing"}}
	if _, err := FromConfig(invalid); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("missing link error=%v", err)
	}
	invalid = config
	invalid.Links = append(invalid.Links, contract.TopologyLink{From: "hall", To: "entry"})
	if _, err := FromConfig(invalid); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("duplicate link error=%v", err)
	}
	invalid = config
	invalid.Nodes[0].Parent = "entry"
	invalid.Nodes[1].Parent = "home"
	if _, err := FromConfig(invalid); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("parent cycle error=%v", err)
	}
}

func TestEmptyFlatTopologyIsAValidPreparedReset(t *testing.T) {
	topo, err := FromConfig(contract.TopologyConfig{Nodes: []contract.TopologyNode{}, Links: []contract.TopologyLink{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Nodes) != 0 || len(topo.NodeIDs()) != 0 {
		t.Fatalf("reset topology=%#v", topo)
	}
}

func TestLoadRejectsInvalidPayloadWithoutMutatingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topology.yaml")
	if err := os.WriteFile(path, []byte("nodes: [{id: broken, type: room}]\nlinks: [{from: broken, to: missing}]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	destination := &Topology{Nodes: map[string]*Node{"keep": {ID: "keep", Name: "Keep", Type: NodeRoom}}}
	if err := Load(path, destination); err == nil {
		t.Fatal("expected invalid topology")
	}
	if destination.Nodes["keep"] == nil {
		t.Fatalf("invalid load mutated destination: %#v", destination.Nodes)
	}
}
