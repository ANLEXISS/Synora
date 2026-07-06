package contract

import "time"

type StateSnapshot struct {
	Version    string         `json:"version,omitempty"`
	Timestamp  time.Time      `json:"timestamp,omitempty"`
	System     map[string]any `json:"system,omitempty"`
	Metrics    map[string]any `json:"metrics,omitempty"`
	Nodes      map[string]any `json:"nodes,omitempty"`
	Devices    map[string]any `json:"devices,omitempty"`
	Cameras    map[string]any `json:"cameras,omitempty"`
	Presence   map[string]any `json:"presence,omitempty"`
	Tracks     map[string]any `json:"tracks,omitempty"`
	Clusters   map[string]any `json:"clusters,omitempty"`
	Clips      map[string]any `json:"clips,omitempty"`
	Identities map[string]any `json:"identities,omitempty"`
	Topology   any            `json:"topology,omitempty"`
	Residents  any            `json:"residents,omitempty"`
}

type Snapshot struct {
	Structure StructureSnapshot `json:"structure"`
	Residents ResidentsSnapshot `json:"residents"`
}

type StructureSnapshot struct {
	Topology []map[string]any `json:"topology"`
	Devices  []map[string]any `json:"devices"`
}

type ResidentsSnapshot struct {
	Residents []map[string]any `json:"residents"`
}
