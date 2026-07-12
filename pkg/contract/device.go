package contract

import "time"

const (
	DeviceTypeCamera  = "camera"
	DeviceTypeLight   = "light"
	DeviceTypeSensor  = "sensor"
	DeviceTypeSiren   = "siren"
	DeviceTypeSpeaker = "speaker"
	DeviceTypeLock    = "lock"
	DeviceTypeBridge  = "bridge"
	DeviceTypeUnknown = "unknown"
)

// Device is the safe configuration view transported outside Core. Secrets and
// implementation-specific configuration fields deliberately do not belong to
// this contract.
type Device struct {
	ID            string         `json:"id" yaml:"id"`
	Name          string         `json:"name,omitempty" yaml:"name,omitempty"`
	Type          string         `json:"type" yaml:"type"`
	Vendor        string         `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	Model         string         `json:"model,omitempty" yaml:"model,omitempty"`
	Serial        string         `json:"serial,omitempty" yaml:"serial,omitempty"`
	PairingMethod string         `json:"pairing_method,omitempty" yaml:"pairing_method,omitempty"`
	Status        string         `json:"status,omitempty" yaml:"status,omitempty"`
	Role          string         `json:"role,omitempty" yaml:"role,omitempty"`
	NodeID        string         `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	ZoneRole      string         `json:"zone_role,omitempty" yaml:"zone_role,omitempty"`
	RoomName      string         `json:"room_name,omitempty" yaml:"room_name,omitempty"`
	Enabled       bool           `json:"enabled" yaml:"enabled"`
	Trusted       bool           `json:"trusted" yaml:"trusted"`
	Capabilities  []string       `json:"capabilities" yaml:"capabilities"`
	Config        map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt     *time.Time     `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`
}

type DeviceView = Device

// DevicePatch uses pointers so false, empty collections and an explicit empty
// node_id remain distinguishable from omitted fields.
type DevicePatch struct {
	Name         *string         `json:"name,omitempty"`
	DisplayName  *string         `json:"display_name,omitempty"`
	Room         *string         `json:"room,omitempty"`
	Role         *string         `json:"role,omitempty"`
	NodeID       *string         `json:"node_id,omitempty"`
	ZoneRole     *string         `json:"zone_role,omitempty"`
	RoomName     *string         `json:"room_name,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
	Trusted      *bool           `json:"trusted,omitempty"`
	Capabilities *[]string       `json:"capabilities,omitempty"`
	Config       *map[string]any `json:"config,omitempty"`
	Metadata     *map[string]any `json:"metadata,omitempty"`
}
