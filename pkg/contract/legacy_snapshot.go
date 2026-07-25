package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// These field-specific envelopes are closed legacy wire variants. The
// original JSON bytes are retained only between decode and encode so the
// legacy RPC remains byte-compatible; callers cannot construct an arbitrary
// map-backed payload through the contract types.
type LegacyTopologyNode struct{ raw []byte }
type LegacyDeviceView struct{ raw []byte }
type LegacyResidentView struct{ raw []byte }

func NewLegacyTopologyNodeJSON(data []byte) (LegacyTopologyNode, error) {
	value, err := decodeLegacy(data, legacyTopologyKeys)
	return LegacyTopologyNode{raw: value}, err
}
func NewLegacyDeviceViewJSON(data []byte) (LegacyDeviceView, error) {
	value, err := decodeLegacy(data, legacyDeviceKeys)
	return LegacyDeviceView{raw: value}, err
}
func NewLegacyResidentViewJSON(data []byte) (LegacyResidentView, error) {
	value, err := decodeLegacy(data, legacyResidentKeys)
	return LegacyResidentView{raw: value}, err
}

func (v LegacyTopologyNode) MarshalJSON() ([]byte, error) {
	return marshalLegacy(v.raw, legacyTopologyKeys)
}
func (v LegacyDeviceView) MarshalJSON() ([]byte, error) {
	return marshalLegacy(v.raw, legacyDeviceKeys)
}
func (v LegacyResidentView) MarshalJSON() ([]byte, error) {
	return marshalLegacy(v.raw, legacyResidentKeys)
}

func (v *LegacyTopologyNode) UnmarshalJSON(data []byte) error {
	value, err := decodeLegacy(data, legacyTopologyKeys)
	if err == nil {
		v.raw = value
	}
	return err
}
func (v *LegacyDeviceView) UnmarshalJSON(data []byte) error {
	value, err := decodeLegacy(data, legacyDeviceKeys)
	if err == nil {
		v.raw = value
	}
	return err
}
func (v *LegacyResidentView) UnmarshalJSON(data []byte) error {
	value, err := decodeLegacy(data, legacyResidentKeys)
	if err == nil {
		v.raw = value
	}
	return err
}

var legacyTopologyKeys = map[string]bool{"id": true, "name": true, "type": true, "dynamic_score": true, "connect": true, "children": true}
var legacyDeviceKeys = map[string]bool{"id": true, "name": true, "type": true, "role": true, "node_id": true, "zone_role": true, "room_name": true, "enabled": true, "trusted": true, "capabilities": true, "config": true, "metadata": true, "created_at": true, "updated_at": true, "deleted_at": true, "online": true, "last_seen": true}
var legacyResidentKeys = map[string]bool{"id": true, "name": true, "display_name": true, "role": true, "admin": true, "enabled": true, "trusted": true, "metadata": true, "created_at": true, "updated_at": true, "deleted_at": true, "node_id": true, "last_seen": true, "state": true, "confidence": true}

func marshalLegacy(data []byte, allowed map[string]bool) ([]byte, error) {
	if len(data) == 0 {
		return []byte("null"), nil
	}
	if _, err := decodeLegacy(data, allowed); err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

func decodeLegacy(data []byte, allowed map[string]bool) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return nil, fmt.Errorf("legacy snapshot contains an unknown or invalid variant")
	}
	for key := range fields {
		if !allowed[key] {
			return nil, fmt.Errorf("legacy snapshot field %q is not allowed", key)
		}
	}
	return append([]byte(nil), data...), nil
}
