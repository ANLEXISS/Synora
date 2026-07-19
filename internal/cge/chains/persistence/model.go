package persistence

import (
	"encoding/json"
	"time"

	"synora/internal/cge/chains"
)

// CurrentSchemaVersion is the only disk schema accepted in this pass.
const CurrentSchemaVersion = 1

// DefaultMaxSnapshotSize limits the complete encoded envelope accepted by a
// FileStore. It is intentionally finite because this is a local snapshot
// format, not an unbounded event archive.
const DefaultMaxSnapshotSize int64 = 64 << 20

// FileEnvelope is the versioned, checksummed on-disk container. The checksum
// covers Payload's exact JSON bytes and excludes the checksum field itself.
type FileEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	CreatedAt     time.Time       `json:"created_at"`
	Payload       json.RawMessage `json:"payload"`
	PayloadSHA256 string          `json:"payload_sha256"`
}

// RegistryPayload is the deterministic snapshot of all chains. Chains are
// serialized in ChainID order by FileStore.Save.
type RegistryPayload struct {
	ChainCount int               `json:"chain_count"`
	Chains     []chains.Snapshot `json:"chains"`
}

// SnapshotMetadata describes a saved or loaded snapshot without exposing the
// chain contents.
type SnapshotMetadata struct {
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	ChainCount    int       `json:"chain_count"`
	PayloadSHA256 string    `json:"payload_sha256"`
	SizeBytes     int64     `json:"size_bytes"`
}
