package contracts

import "time"

const ConnectivitySchemaVersion = 1

type State string

const (
	StateDisabled      State = "disabled"
	StateUnprovisioned State = "unprovisioned"
	StateRegistering   State = "registering"
	StateControlOnline State = "control_online"
	StateConnecting    State = "connecting"
	StateConnected     State = "connected"
	StateDegraded      State = "degraded"
	StateOffline       State = "offline"
)

type Mode string

const (
	ModeNone   Mode = "none"
	ModeLocal  Mode = "local"
	ModeDirect Mode = "direct"
	ModeRelay  Mode = "relay"
)

type Status struct {
	SchemaVersion       int        `json:"schema_version"`
	DeviceID            string     `json:"device_id"`
	IdentityFingerprint string     `json:"identity_fingerprint"`
	Enabled             bool       `json:"enabled"`
	Provisioned         bool       `json:"provisioned"`
	State               State      `json:"state"`
	Mode                Mode       `json:"mode"`
	InterfaceName       string     `json:"interface"`
	VirtualAddress      string     `json:"virtual_address,omitempty"`
	ControlConnected    bool       `json:"control_connected"`
	PeerConnected       bool       `json:"peer_connected"`
	LastTransition      time.Time  `json:"last_transition"`
	LastHandshake       *time.Time `json:"last_handshake,omitempty"`
	DirectAttempts      uint64     `json:"direct_attempts"`
	DirectSuccesses     uint64     `json:"direct_successes"`
	RelayFallbacks      uint64     `json:"relay_fallbacks"`
	RelayRegion         string     `json:"relay_region,omitempty"`
	LastErrorCode       string     `json:"last_error_code,omitempty"`
}

type EnrollmentRequest struct {
	SchemaVersion       int    `json:"schema_version"`
	DeviceID            string `json:"device_id"`
	IdentityFingerprint string `json:"identity_fingerprint"`
	IdentityPublicKey   string `json:"identity_public_key"`
}

type EnrollmentResponse struct {
	SchemaVersion  int              `json:"schema_version"`
	Provisioned    bool             `json:"provisioned"`
	VirtualAddress string           `json:"virtual_address,omitempty"`
	Peers          []PeerDescriptor `json:"peers,omitempty"`
	Relay          *RelayDescriptor `json:"relay,omitempty"`
}

type EndpointReport struct {
	SchemaVersion int         `json:"schema_version"`
	DeviceID      string      `json:"device_id"`
	ReportedAt    time.Time   `json:"reported_at"`
	Status        Status      `json:"status"`
	Candidates    []Candidate `json:"candidates,omitempty"`
}

type PeerDescriptor struct {
	PeerID         string    `json:"peer_id"`
	WireGuardKey   string    `json:"wireguard_public_key"`
	VirtualAddress string    `json:"virtual_address"`
	Permissions    []string  `json:"permissions,omitempty"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
}

type CandidateType string

const (
	CandidateLAN         CandidateType = "lan"
	CandidateIPv6        CandidateType = "ipv6"
	CandidateObserved    CandidateType = "observed"
	CandidatePortMapping CandidateType = "port_mapping"
	CandidateRelay       CandidateType = "relay"
)

type Candidate struct {
	Type      CandidateType `json:"type"`
	Address   string        `json:"address"`
	Port      int           `json:"port,omitempty"`
	ExpiresAt time.Time     `json:"expires_at,omitempty"`
}

type ConnectionReport struct {
	SchemaVersion int       `json:"schema_version"`
	PeerID        string    `json:"peer_id"`
	Mode          Mode      `json:"mode"`
	Success       bool      `json:"success"`
	ReportedAt    time.Time `json:"reported_at"`
	ErrorCode     string    `json:"error_code,omitempty"`
}

type RelayDescriptor struct {
	Region    string    `json:"region"`
	Endpoint  string    `json:"endpoint"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}
