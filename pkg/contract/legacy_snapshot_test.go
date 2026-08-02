package contract

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLegacySnapshotClosedVariantsPreserveJSON(t *testing.T) {
	input := []byte(`{"id":"node-1","name":"entry","type":"zone","dynamic_score":0,"connect":[],"children":[]}`)
	value, err := NewLegacyTopologyNodeJSON(input)
	if err != nil { t.Fatal(err) }
	encoded, err := json.Marshal(value)
	if err != nil { t.Fatal(err) }
	if !bytes.Equal(encoded, input) { t.Fatalf("legacy bytes changed: %s != %s", encoded, input) }
}

func TestLegacySnapshotRejectsUnknownVariantField(t *testing.T) {
	if _, err := NewLegacyResidentViewJSON([]byte(`{"id":"resident-1","unknown_variant":true}`)); err == nil {
		t.Fatal("unknown legacy field accepted")
	}
}
