package gosurface

import "strings"

// Inventory is a deterministic description of every exported serializable
// type discovered in the monitored Go packages. It is architecture data, not
// a runtime dependency.
type Inventory struct {
	SchemaVersion int             `yaml:"schema_version"`
	Namespace     string          `yaml:"namespace"`
	Types         []InventoryType `yaml:"types"`
}

type InventoryType struct {
	Package        string           `yaml:"package"`
	Name           string           `yaml:"name"`
	Kind           string           `yaml:"kind"`
	Implementation string           `yaml:"implementation"`
	Fields         []InventoryField `yaml:"fields"`
}

type InventoryField struct {
	Name               string   `yaml:"name"`
	GoField            string   `yaml:"go_field"`
	FieldPath          string   `yaml:"field_path"`
	WireName           string   `yaml:"wire_name"`
	GoType             string   `yaml:"go_type"`
	WireType           string   `yaml:"wire_type"`
	Required           bool     `yaml:"required"`
	Nullable           bool     `yaml:"nullable"`
	Omitempty          bool     `yaml:"omitempty"`
	RuntimeOnly        bool     `yaml:"runtime_only"`
	Source             string   `yaml:"source"`
	Trust              string   `yaml:"trust"`
	Sensitivity        string   `yaml:"sensitivity"`
	Protection         string   `yaml:"protection"`
	Persistence        []string `yaml:"persistence"`
	Retention          string   `yaml:"retention"`
	Validation         string   `yaml:"validation"`
	IdentifierSemantic string   `yaml:"identifier_semantic"`
	TimestampSemantic  string   `yaml:"timestamp_semantic"`
}

func BuildInventory(root, configPath string) (Inventory, error) {
	infos, err := ScanAll(root, configPath)
	if err != nil {
		return Inventory{}, err
	}
	result := Inventory{SchemaVersion: 1, Namespace: "synora.cge", Types: make([]InventoryType, 0, len(infos))}
	for _, info := range infos {
		item := InventoryType{Package: info.Package, Name: info.Name, Kind: info.Kind, Implementation: info.Package + "/" + info.Name, Fields: make([]InventoryField, 0, len(info.Fields))}
		for _, field := range info.Fields {
			if field.WireName == "-" {
				continue
			}
			identifier, timestamp := fieldSemantics(field)
			item.Fields = append(item.Fields, InventoryField{
				Name: field.Name, GoField: field.Name, FieldPath: field.Name, WireName: field.WireName,
				GoType: field.Type, WireType: field.WireType, Required: !field.Omitempty && !field.Nullable,
				Nullable: field.Nullable, Omitempty: field.Omitempty, Source: "go_surface",
				Trust: "derived", Sensitivity: "operational", Protection: protectionFor(identifier),
				Persistence: []string{}, Retention: "process_lifetime", Validation: "generated_shape",
				IdentifierSemantic: identifier, TimestampSemantic: timestamp,
			})
		}
		result.Types = append(result.Types, item)
	}
	return result, nil
}

func fieldSemantics(field FieldInfo) (string, string) {
	name := strings.ToLower(field.Name)
	switch name {
	case "eventid", "eventref":
		return "event_id", "not_a_timestamp"
	case "observationid", "sourceeventref":
		return "observation_id", "not_a_timestamp"
	case "entityid", "subjectid", "candidateentityid", "candidateentityids":
		return "entity_id", "not_a_timestamp"
	case "deviceid", "cameraid":
		return "device_id", "not_a_timestamp"
	case "nodeid", "noderef":
		return "node_id", "not_a_timestamp"
	case "zoneid", "zoneref":
		return "zone_id", "not_a_timestamp"
	case "clipid":
		return "clip_id", "not_a_timestamp"
	case "trackid":
		return "track_id", "not_a_timestamp"
	case "activationid":
		return "activation_id", "not_a_timestamp"
	case "sequencekey":
		return "sequence_key", "not_a_timestamp"
	case "episodeid":
		return "episode_id", "not_a_timestamp"
	case "chainid":
		return "chain_id", "not_a_timestamp"
	case "situationid":
		return "situation_id", "not_a_timestamp"
	case "createdat":
		return "not_an_identifier", "created_at"
	case "updatedat":
		return "not_an_identifier", "updated_at"
	case "closedat":
		return "not_an_identifier", "closed_at"
	case "observedat":
		return "not_an_identifier", "observed_at"
	case "receivedat":
		return "not_an_identifier", "received_at"
	case "processedat":
		return "not_an_identifier", "processed_at"
	case "committedat":
		return "not_an_identifier", "committed_at"
	case "persistedat":
		return "not_an_identifier", "persisted_at"
	case "lastseen", "lastseenat":
		return "not_an_identifier", "last_seen_at"
	default:
		return "not_an_identifier", "not_a_timestamp"
	}
}

func protectionFor(identifier string) string {
	if identifier == "not_an_identifier" || identifier == "node_id" || identifier == "zone_id" {
		return "none_or_topology_preserved"
	}
	return "registry_domain_required"
}
