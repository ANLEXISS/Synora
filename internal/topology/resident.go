package topology

import "synora/pkg/contract"

type Presence struct {
	ResidentID string  `json:"resident_id"`
	Location   string  `json:"location"`
	LastSeen   int64   `json:"last_seen"`
	Confidence float64 `json:"confidence"`
}

type Resident struct {
	ID    string `json:"id" yaml:"id"`
	Name  string `json:"name" yaml:"name"`
	Role  string `json:"role" yaml:"role"`
	Admin bool   `json:"admin" yaml:"admin"`

	Contact contract.Contact `json:"contact" yaml:"contact"`

	Baseline contract.Baseline `json:"baseline" yaml:"baseline"`

	Presence *Presence `json:"presence,omitempty" yaml:"-"`
}
