package generations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/persistence"
)

const CurrentManifestSchemaVersion = 1

// Generation is the immutable snapshot plus its durable journal checkpoint.
// SnapshotPath is always relative to the store root.
type Generation struct {
	GenerationID string `json:"generation_id"`

	SnapshotPath string `json:"snapshot_path"`

	SnapshotSchemaVersion int       `json:"snapshot_schema_version"`
	SnapshotCreatedAt     time.Time `json:"snapshot_created_at"`
	SnapshotChainCount    int       `json:"snapshot_chain_count"`
	SnapshotPayloadSHA256 string    `json:"snapshot_payload_sha256"`
	SnapshotSizeBytes     int64     `json:"snapshot_size_bytes"`

	IncludedJournalSequence uint64 `json:"included_journal_sequence"`
	IncludedJournalHeadHash string `json:"included_journal_head_hash"`

	CheckpointRecordSequence uint64 `json:"checkpoint_record_sequence"`
	CheckpointRecordHash     string `json:"checkpoint_record_hash"`
}

// PendingGeneration is durable snapshot data before a checkpoint has been
// appended. It cannot be active until Finalize succeeds and PublishManifest
// validates the same checkpoint record.
type PendingGeneration struct {
	GenerationID string
	RelativePath string

	Metadata persistence.SnapshotMetadata

	IncludedJournalSequence uint64
	IncludedJournalHeadHash string
}

// GenerationFile is a read-only directory entry returned by ListGenerations.
// It deliberately contains no deletion or repair operation.
type GenerationFile struct {
	GenerationID string `json:"generation_id"`
	RelativePath string `json:"relative_path"`
	SizeBytes    int64  `json:"size_bytes"`
}

// Finalize associates a pending snapshot with one exact checkpoint record.
func (p PendingGeneration) Finalize(record journal.Record) (Generation, error) {
	if err := validatePending(p); err != nil {
		return Generation{}, err
	}
	if err := record.Validate(); err != nil {
		return Generation{}, fmt.Errorf("%w: invalid journal record: %v", ErrCheckpointMismatch, err)
	}
	if record.Kind != journal.RecordKindSnapshotCheckpointed {
		return Generation{}, fmt.Errorf("%w: record kind is %s", ErrCheckpointMismatch, record.Kind)
	}
	payload, err := decodeCheckpoint(record)
	if err != nil {
		return Generation{}, err
	}
	if record.Sequence != p.IncludedJournalSequence+1 || record.PreviousHash != p.IncludedJournalHeadHash ||
		payload.SnapshotSchemaVersion != p.Metadata.SchemaVersion ||
		!payload.SnapshotCreatedAt.Equal(p.Metadata.CreatedAt) ||
		payload.SnapshotChainCount != p.Metadata.ChainCount ||
		payload.SnapshotPayloadSHA256 != p.Metadata.PayloadSHA256 ||
		payload.SnapshotSizeBytes != p.Metadata.SizeBytes ||
		payload.JournalSequence != p.IncludedJournalSequence ||
		payload.JournalHeadHash != p.IncludedJournalHeadHash || record.RecordHash == "" {
		return Generation{}, fmt.Errorf("%w: checkpoint does not match pending generation", ErrCheckpointMismatch)
	}
	generation := Generation{
		GenerationID: p.GenerationID, SnapshotPath: p.RelativePath,
		SnapshotSchemaVersion: p.Metadata.SchemaVersion, SnapshotCreatedAt: p.Metadata.CreatedAt,
		SnapshotChainCount: p.Metadata.ChainCount, SnapshotPayloadSHA256: p.Metadata.PayloadSHA256,
		SnapshotSizeBytes:       p.Metadata.SizeBytes,
		IncludedJournalSequence: p.IncludedJournalSequence, IncludedJournalHeadHash: p.IncludedJournalHeadHash,
		CheckpointRecordSequence: record.Sequence, CheckpointRecordHash: record.RecordHash,
	}
	if err := generation.Validate(); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

// ValidateCheckpoint verifies that a journal record is the exact durable
// checkpoint named by this generation.
func (g Generation) ValidateCheckpoint(record journal.Record) error {
	if err := g.Validate(); err != nil {
		return err
	}
	finalized, err := (PendingGeneration{
		GenerationID: g.GenerationID, RelativePath: g.SnapshotPath,
		Metadata: persistence.SnapshotMetadata{
			SchemaVersion: g.SnapshotSchemaVersion, CreatedAt: g.SnapshotCreatedAt,
			ChainCount: g.SnapshotChainCount, PayloadSHA256: g.SnapshotPayloadSHA256,
			SizeBytes: g.SnapshotSizeBytes,
		}, IncludedJournalSequence: g.IncludedJournalSequence,
		IncludedJournalHeadHash: g.IncludedJournalHeadHash,
	}).Finalize(record)
	if err != nil || finalized.CheckpointRecordSequence != g.CheckpointRecordSequence || finalized.CheckpointRecordHash != g.CheckpointRecordHash {
		return ErrCheckpointMismatch
	}
	return nil
}

// Validate checks the stable generated filename and all snapshot/checkpoint
// metadata. It deliberately does not inspect the filesystem.
func (g Generation) Validate() error {
	if !validGenerationID(g.GenerationID) {
		return ErrInvalidGenerationID
	}
	expectedPath := filepath.ToSlash(filepath.Join("snapshots", g.GenerationID+".json"))
	if g.SnapshotPath != expectedPath || filepath.IsAbs(g.SnapshotPath) || strings.Contains(filepath.ToSlash(filepath.Clean(g.SnapshotPath)), "../") {
		return ErrInvalidGenerationPath
	}
	if g.SnapshotSchemaVersion <= 0 || g.SnapshotCreatedAt.IsZero() || g.SnapshotChainCount < 0 ||
		!validSHA256(g.SnapshotPayloadSHA256) || g.SnapshotSizeBytes <= 0 ||
		g.IncludedJournalSequence == 0 || !validSHA256(g.IncludedJournalHeadHash) ||
		g.CheckpointRecordSequence != g.IncludedJournalSequence+1 || !validSHA256(g.CheckpointRecordHash) {
		return ErrGenerationMetadataMismatch
	}
	return nil
}

type manifestBody struct {
	SchemaVersion int        `json:"schema_version"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Active        Generation `json:"active"`
}

// Manifest is the single active-generation pointer. Checksum covers the
// canonical JSON body without Checksum itself.
type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Active        Generation `json:"active"`
	Checksum      string     `json:"checksum"`
}

func (m Manifest) body() manifestBody {
	return manifestBody{SchemaVersion: m.SchemaVersion, UpdatedAt: m.UpdatedAt, Active: m.Active}
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != CurrentManifestSchemaVersion {
		return ErrManifestSchemaUnsupported
	}
	if m.UpdatedAt.IsZero() {
		return ErrManifestInvalid
	}
	if err := m.Active.Validate(); err != nil {
		return fmt.Errorf("%w: active generation: %v", ErrManifestGenerationMismatch, err)
	}
	if !validSHA256(m.Checksum) {
		return ErrManifestInvalid
	}
	expected, err := checksumJSON(m.body())
	if err != nil || expected != m.Checksum {
		return ErrManifestInvalid
	}
	return nil
}

func newManifest(generation Generation, updatedAt time.Time) (Manifest, error) {
	if err := generation.Validate(); err != nil {
		return Manifest{}, err
	}
	if updatedAt.IsZero() {
		return Manifest{}, ErrInvalidTimestamp
	}
	m := Manifest{SchemaVersion: CurrentManifestSchemaVersion, UpdatedAt: updatedAt, Active: generation}
	checksum, err := checksumJSON(m.body())
	if err != nil {
		return Manifest{}, err
	}
	m.Checksum = checksum
	return m, nil
}

func validatePending(p PendingGeneration) error {
	if !validGenerationID(p.GenerationID) {
		return ErrInvalidGenerationID
	}
	if p.RelativePath != filepath.ToSlash(filepath.Join("snapshots", p.GenerationID+".json")) {
		return ErrInvalidGenerationPath
	}
	if p.Metadata.SchemaVersion <= 0 || p.Metadata.CreatedAt.IsZero() || p.Metadata.ChainCount < 0 ||
		!validSHA256(p.Metadata.PayloadSHA256) || p.Metadata.SizeBytes <= 0 ||
		p.IncludedJournalSequence == 0 || !validSHA256(p.IncludedJournalHeadHash) {
		return ErrGenerationMetadataMismatch
	}
	return nil
}

func decodeCheckpoint(record journal.Record) (journal.SnapshotCheckpointPayload, error) {
	var payload journal.SnapshotCheckpointPayload
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return journal.SnapshotCheckpointPayload{}, fmt.Errorf("%w: %v", ErrCheckpointMismatch, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return journal.SnapshotCheckpointPayload{}, ErrCheckpointMismatch
	}
	if payload.SnapshotSchemaVersion <= 0 || payload.SnapshotCreatedAt.IsZero() || payload.SnapshotChainCount < 0 ||
		!validSHA256(payload.SnapshotPayloadSHA256) || payload.SnapshotSizeBytes <= 0 ||
		payload.JournalSequence == 0 || !validSHA256(payload.JournalHeadHash) {
		return journal.SnapshotCheckpointPayload{}, ErrCheckpointMismatch
	}
	return payload, nil
}
