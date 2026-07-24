package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/contractcatalog"
	"synora/internal/cge/hypotheses"
)

// FileJournalOptions configures the local NDJSON writer and reader.
type FileJournalOptions struct {
	FileMode         fs.FileMode
	MaxJournalSize   int64
	MaxRecordSize    int
	CreateParentDirs bool
}

// FileJournal is a single-process append-only writer. Its mutex serializes
// calls from one process; cross-process writers are intentionally unsupported.
type FileJournal struct {
	mu                sync.Mutex
	path              string
	options           FileJournalOptions
	state             journalWriterState
	qualificationHook func(string) error
}

// SetQualificationHook installs an explicitly injected validation seam on
// this journal instance. Runtime code never sets it; it exists solely for
// deterministic transaction-failure qualification.
func (j *FileJournal) SetQualificationHook(hook func(stage string) error) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.qualificationHook = hook
}

type journalWriterState struct {
	known        bool
	fileSize     int64
	headSequence uint64
	headHash     string
	journalID    string
}

// NewFileJournal validates configuration only. It never creates or
// initializes the journal file.
func NewFileJournal(path string, options FileJournalOptions) (*FileJournal, error) {
	journal := &FileJournal{path: path, options: options}
	if journal.options.FileMode == 0 {
		journal.options.FileMode = defaultFileMode
	}
	if journal.options.MaxJournalSize == 0 {
		journal.options.MaxJournalSize = defaultMaxJournalSize
	}
	if journal.options.MaxRecordSize == 0 {
		journal.options.MaxRecordSize = defaultMaxRecordSize
	}
	if !options.CreateParentDirs {
		journal.options.CreateParentDirs = false
	} else {
		journal.options.CreateParentDirs = true
	}
	if err := journal.validate(); err != nil {
		return nil, err
	}
	return journal, nil
}

// Initialize explicitly appends the only permitted journal.genesis record.
func (j *FileJournal) Initialize(ctx context.Context, input GenesisInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateGenesisInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := readJournalFile(ctx, j.path, j.options.MaxJournalSize, j.options.MaxRecordSize)
	if j.state.known {
		if err != nil || !state.valid || j.state.fileSize != state.size || j.state.headSequence != state.snapshot.HeadSequence || j.state.headHash != state.snapshot.HeadHash || j.state.journalID != state.snapshot.JournalID {
			return Record{}, ErrExternalModificationDetected
		}
	}
	if err != nil && !errors.Is(err, ErrJournalNotFound) {
		return Record{}, err
	}
	if err == nil && state.valid {
		j.remember(state)
		return Record{}, ErrJournalAlreadyInitialized
	}
	if err == nil && state.exists && state.size > 0 {
		return Record{}, ErrInvalidRecord
	}
	record, err := buildRecord(1, RecordKindGenesis, input.RecordedAt, input.Actor, input.CorrelationID, GenesisPayload{
		JournalID: input.JournalID,
		CreatedAt: input.CreatedAt,
		Purpose:   input.Purpose,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, journalFileState{exists: state.exists, size: state.size}, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendChainAdded explicitly records a chain's first global appearance.
func (j *FileJournal) AppendChainAdded(ctx context.Context, input ChainAddedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateChainAddedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindChainAdded, input.RecordedAt, input.Actor, input.CorrelationID, ChainAddedPayload{Chain: input.Chain})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendLifecycleTransition explicitly records a compact lifecycle delta.
func (j *FileJournal) AppendLifecycleTransition(ctx context.Context, input LifecycleTransitionInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateLifecycleInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindLifecycleTransitioned, input.RecordedAt, input.Actor, input.CorrelationID, LifecycleTransitionPayload{
		ChainID:          input.ChainID,
		PreviousRevision: input.PreviousRevision,
		NewRevision:      input.NewRevision,
		From:             input.From,
		To:               input.To,
		Revision:         input.Revision,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendObservationAdded records one explicit observation delta after the
// domain candidate has already been prepared. It never stores raw event data
// or a complete chain snapshot.
func (j *FileJournal) AppendObservationAdded(ctx context.Context, input ObservationAddedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateObservationAddedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindObservationAdded, input.RecordedAt, input.Actor, input.CorrelationID, ObservationAddedPayload{
		ChainID: input.ChainID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		Observation: input.Observation, Revision: input.Revision,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendContributionAdded records one explicit contribution delta after the
// domain candidate has been prepared. It never stores a complete chain.
func (j *FileJournal) AppendContributionAdded(ctx context.Context, input ContributionAddedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	input.Contribution = input.Contribution.Clone()
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateContributionAddedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindContributionAdded, input.RecordedAt, input.Actor, input.CorrelationID, ContributionAddedPayload{
		ChainID: input.ChainID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		Contribution: input.Contribution.Clone(), PreviousConfidence: input.PreviousConfidence, NewConfidence: input.NewConfidence,
		PreviousSupportCount: input.PreviousSupportCount, NewSupportCount: input.NewSupportCount,
		PreviousContradictionCount: input.PreviousContradictionCount, NewContradictionCount: input.NewContradictionCount,
		Revision: input.Revision,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendHypothesisOpened records the complete initial hypothesis snapshot
// after it has been validated by the owning durable coordinator.
func (j *FileJournal) AppendHypothesisOpened(ctx context.Context, input HypothesisOpenedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateHypothesisOpenedInput(input); err != nil {
		return Record{}, err
	}
	owned, err := hypotheses.Restore(input.Hypothesis)
	if err != nil {
		return Record{}, fmt.Errorf("%w: hypothesis snapshot is invalid: %v", ErrInvalidPayload, err)
	}
	input.Hypothesis = owned.Snapshot()
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindHypothesisOpened, input.RecordedAt, input.Actor, input.CorrelationID, HypothesisOpenedPayload{Hypothesis: input.Hypothesis})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendHypothesisStatusChanged records one explicit hypothesis status delta.
func (j *FileJournal) AppendHypothesisStatusChanged(ctx context.Context, input HypothesisStatusChangedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateHypothesisStatusChangedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindHypothesisStatusChanged, input.RecordedAt, input.Actor, input.CorrelationID, HypothesisStatusChangedPayload{
		SetID: input.SetID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		PreviousStatus: input.PreviousStatus, NewStatus: input.NewStatus, Revision: input.Revision,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendHypothesisRebased records one append-only assessment version.
func (j *FileJournal) AppendHypothesisRebased(ctx context.Context, input HypothesisRebasedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	input.Assessment = input.Assessment.Clone()
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateHypothesisRebasedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindHypothesisRebased, input.RecordedAt, input.Actor, input.CorrelationID, HypothesisRebasedPayload{
		SetID: input.SetID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		PreviousAssessmentVersion: input.PreviousAssessmentVersion, NewAssessmentVersion: input.NewAssessmentVersion,
		PreviousAssessmentID: input.PreviousAssessmentID, NewAssessmentID: input.NewAssessmentID,
		PreviousFingerprint: input.PreviousFingerprint, NewFingerprint: input.NewFingerprint,
		Assessment: input.Assessment.Clone(), Revision: input.Revision,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

func (j *FileJournal) AppendHypothesisSuperseded(ctx context.Context, input HypothesisSupersededInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateHypothesisSupersededInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindHypothesisSuperseded, input.RecordedAt, input.Actor, input.CorrelationID, HypothesisSupersededPayload{
		PreviousSetID: input.PreviousSetID, NewSetID: input.NewSetID, PreviousRevision: input.PreviousRevision, NewPreviousRevision: input.NewPreviousRevision,
		PreviousStatus: input.PreviousStatus, NewStatus: input.NewStatus, PreviousSuccessorSetID: input.PreviousSuccessorSetID, NewSuccessorSetID: input.NewSuccessorSetID,
		PreviousSetRevision: input.PreviousSetRevision, NewHypothesis: input.NewHypothesis,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendHypothesisResolved appends the one global record coupling the
// hypothesis transition and its exact chain delta.
func (j *FileJournal) AppendHypothesisResolved(ctx context.Context, input HypothesisResolvedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateHypothesisResolvedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	payload := HypothesisResolvedPayload{SetID: input.SetID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision, PreviousStatus: input.PreviousStatus, NewStatus: input.NewStatus, AssessmentVersion: input.AssessmentVersion, AssessmentID: input.AssessmentID, AssessmentFingerprint: input.AssessmentFingerprint, AlternativeID: input.AlternativeID, AlternativeKind: input.AlternativeKind, Effect: input.Effect.Clone(), EffectFingerprint: input.EffectFingerprint, Outcome: input.Outcome.Clone(), HypothesisRevision: input.HypothesisRevision, ChainDelta: input.ChainDelta}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindHypothesisResolved, input.RecordedAt, input.Actor, input.CorrelationID, payload)
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendSnapshotCheckpoint records metadata for a snapshot already produced
// elsewhere. It never calls persistence.FileStore.Save.
func (j *FileJournal) AppendSnapshotCheckpoint(ctx context.Context, input SnapshotCheckpointInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateCheckpointInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	if input.JournalSequence != state.snapshot.HeadSequence || input.JournalHeadHash != state.snapshot.HeadHash {
		return Record{}, fmt.Errorf("%w: checkpoint head does not match current journal", ErrInvalidCheckpoint)
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindSnapshotCheckpointed, input.RecordedAt, input.Actor, input.CorrelationID, SnapshotCheckpointPayload{
		SnapshotSchemaVersion: input.SnapshotSchemaVersion,
		SnapshotCreatedAt:     input.SnapshotCreatedAt,
		SnapshotChainCount:    input.SnapshotChainCount,
		SnapshotPayloadSHA256: input.SnapshotPayloadSHA256,
		SnapshotSizeBytes:     input.SnapshotSizeBytes,
		JournalSequence:       input.JournalSequence,
		JournalHeadHash:       input.JournalHeadHash,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

func (j *FileJournal) loadAppendBaseLocked(ctx context.Context) (journalFileState, error) {
	// Once this writer has a validated head, normal appends only need to
	// revalidate the bounded tail and file size. Complete journal validation is
	// intentionally retained for initialization, replay, recovery, and the
	// explicit ReadAll qualification path. A historical-byte edit with an
	// unchanged tail is therefore detected by the next full validation, not by
	// this O(1)-with-record-size append check.
	if j.state.known {
		head, err := readJournalHead(ctx, j.path, j.options.MaxRecordSize)
		if err != nil {
			if errors.Is(err, ErrJournalNotFound) || errors.Is(err, ErrJournalEmpty) {
				return journalFileState{}, ErrExternalModificationDetected
			}
			return journalFileState{}, err
		}
		if head.Size != j.state.fileSize || head.Sequence != j.state.headSequence || head.Hash != j.state.headHash {
			return journalFileState{}, ErrExternalModificationDetected
		}
		return journalFileState{
			snapshot: JournalSnapshot{
				SchemaVersion: CurrentSchemaVersion,
				JournalID:     j.state.journalID,
				RecordCount:   head.Sequence,
				HeadSequence:  head.Sequence,
				HeadHash:      head.Hash,
			},
			size: head.Size, exists: true, valid: true,
		}, nil
	}
	state, err := readJournalFile(ctx, j.path, j.options.MaxJournalSize, j.options.MaxRecordSize)
	if errors.Is(err, ErrJournalNotFound) {
		return journalFileState{}, ErrJournalNotInitialized
	}
	if err != nil {
		return journalFileState{}, err
	}
	if !state.valid {
		return journalFileState{}, ErrJournalNotInitialized
	}
	if j.state.known && (j.state.fileSize != state.size || j.state.headSequence != state.snapshot.HeadSequence || j.state.headHash != state.snapshot.HeadHash || j.state.journalID != state.snapshot.JournalID) {
		return journalFileState{}, ErrExternalModificationDetected
	}
	j.remember(state)
	return state, nil
}

func (j *FileJournal) appendRecordLocked(ctx context.Context, base journalFileState, record Record) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if j.qualificationHook != nil {
		if err := j.qualificationHook("before_append"); err != nil {
			return Record{}, fmt.Errorf("%w: qualification before append: %v", ErrAppendFailed, err)
		}
	}
	if base.valid {
		record.PreviousHash = base.snapshot.HeadHash
	} else {
		record.PreviousHash = GenesisPreviousHash
	}
	record.RecordHash = hashRecord(record)
	line, err := encodeRecordLine(record)
	if err != nil {
		return Record{}, fmt.Errorf("%w: encode record: %v", ErrAppendFailed, err)
	}
	if len(line) > j.options.MaxRecordSize {
		return Record{}, fmt.Errorf("%w: size=%d limit=%d", ErrRecordTooLarge, len(line), j.options.MaxRecordSize)
	}
	if base.size+int64(len(line)) > j.options.MaxJournalSize {
		return Record{}, fmt.Errorf("%w: journal size limit exceeded", ErrJournalTooLarge)
	}
	if j.options.CreateParentDirs {
		if err := os.MkdirAll(filepath.Dir(j.path), 0o750); err != nil {
			return Record{}, fmt.Errorf("%w: create parent directory: %v", ErrAppendFailed, err)
		}
	}
	flags := os.O_WRONLY | os.O_APPEND
	if !base.exists {
		flags |= os.O_CREATE
	}
	file, err := os.OpenFile(j.path, flags, j.options.FileMode.Perm())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, fmt.Errorf("%w: %s", ErrJournalNotFound, j.path)
		}
		return Record{}, fmt.Errorf("%w: open append file: %v", ErrAppendFailed, err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return Record{}, fmt.Errorf("%w: stat append file: %v", ErrAppendFailed, err)
	}
	if stat.Size() != base.size {
		_ = file.Close()
		return Record{}, ErrExternalModificationDetected
	}
	if err := checkContext(ctx); err != nil {
		_ = file.Close()
		return Record{}, err
	}
	if err := writeAll(file, line); err != nil {
		_ = file.Close()
		return Record{}, fmt.Errorf("%w: write record: %v", ErrAppendFailed, err)
	}
	if j.qualificationHook != nil {
		if err := j.qualificationHook("after_write"); err != nil {
			_ = file.Close()
			return Record{}, fmt.Errorf("%w: qualification after write: %v", ErrAppendFailed, err)
		}
	}
	if err := checkContext(ctx); err != nil {
		_ = file.Close()
		return Record{}, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return Record{}, fmt.Errorf("%w: sync record: %v", ErrAppendFailed, err)
	}
	if j.qualificationHook != nil {
		if err := j.qualificationHook("after_sync"); err != nil {
			_ = file.Close()
			return Record{}, fmt.Errorf("%w: qualification after sync: %v", ErrAppendFailed, err)
		}
	}
	if err := file.Close(); err != nil {
		return Record{}, fmt.Errorf("%w: close record: %v", ErrAppendFailed, err)
	}
	j.state = journalWriterState{
		known:        true,
		fileSize:     base.size + int64(len(line)),
		headSequence: record.Sequence,
		headHash:     record.RecordHash,
		journalID:    j.journalIDFromBase(base, record),
	}
	return record, nil
}

func (j *FileJournal) remember(state journalFileState) {
	if !state.valid {
		return
	}
	j.state = journalWriterState{
		known:        true,
		fileSize:     state.size,
		headSequence: state.snapshot.HeadSequence,
		headHash:     state.snapshot.HeadHash,
		journalID:    state.snapshot.JournalID,
	}
}

func (j *FileJournal) journalIDFromBase(base journalFileState, record Record) string {
	if base.valid {
		return base.snapshot.JournalID
	}
	if record.Kind == RecordKindGenesis {
		payload, _ := decodeGenesisPayload(record.Payload)
		return payload.JournalID
	}
	return j.state.journalID
}

func buildRecord(sequence uint64, kind RecordKind, recordedAt time.Time, actor, correlation string, payload any) (Record, error) {
	if sequence == 0 {
		return Record{}, InvalidSequenceError{Sequence: sequence}
	}
	if err := kind.Validate(); err != nil {
		return Record{}, err
	}
	if err := validateActorCorrelation(actor, correlation); err != nil {
		return Record{}, err
	}
	if recordedAt.IsZero() {
		return Record{}, fmt.Errorf("%w: recorded_at is zero", ErrInvalidRecord)
	}
	kindDescriptor, ok := contractcatalog.JournalKind(string(kind))
	if !ok {
		return Record{}, fmt.Errorf("%w: uncatalogued journal kind", ErrInvalidRecordKind)
	}
	if err := contractcatalog.ValidateTypedPayload(kindDescriptor.GoPackage, kindDescriptor.GoType, kindDescriptor.Validator, payload); err != nil {
		return Record{}, fmt.Errorf("%w: journal payload type: %v", ErrInvalidPayload, err)
	}
	if err := contractcatalog.ValidateStoreWrite("synora.store.cge-journal", "synora.cge.audit-record.v1", payload); err != nil {
		return Record{}, fmt.Errorf("%w: contract guard: %v", ErrInvalidPayload, err)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("%w: encode payload: %v", ErrInvalidPayload, err)
	}
	return Record{
		SchemaVersion: CurrentSchemaVersion,
		Sequence:      sequence,
		Kind:          kind,
		RecordedAt:    recordedAt,
		Actor:         actor,
		CorrelationID: correlation,
		PreviousHash:  "",
		Payload:       append(json.RawMessage(nil), payloadBytes...),
		PayloadSHA256: payloadChecksum(payloadBytes),
	}, nil
}

func encodeRecordLine(record Record) ([]byte, error) {
	if record.Sequence == 1 {
		if record.PreviousHash == "" {
			record.PreviousHash = GenesisPreviousHash
		}
	}
	if record.PreviousHash == "" {
		return nil, ErrPreviousHashMismatch
	}
	if record.RecordHash == "" {
		record.RecordHash = hashRecord(record)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return data, nil
}

func (j *FileJournal) validate() error {
	if j == nil {
		return fmt.Errorf("%w: journal is nil", ErrInvalidPath)
	}
	path := strings.TrimSpace(j.path)
	if path == "" || path != j.path || filepath.Clean(path) == "." || filepath.Base(filepath.Clean(path)) == ".." {
		return fmt.Errorf("%w: journal path must name a file", ErrInvalidPath)
	}
	if j.options.FileMode.Type() != 0 || j.options.FileMode.Perm() == 0 || j.options.FileMode.Perm()&0o022 != 0 {
		return fmt.Errorf("%w: mode must be regular and not group/world writable", ErrInvalidFileMode)
	}
	if j.options.MaxJournalSize <= 0 || j.options.MaxRecordSize <= 0 || int64(j.options.MaxRecordSize) > j.options.MaxJournalSize {
		return ErrInvalidLimit
	}
	return nil
}

func validateGenesisInput(input GenesisInput) error {
	if strings.TrimSpace(input.JournalID) == "" || len([]rune(input.JournalID)) > maxPurposeLength || strings.ContainsAny(input.JournalID, "\r\n") || input.CreatedAt.IsZero() || input.RecordedAt.IsZero() || !input.CreatedAt.Equal(input.RecordedAt) || strings.TrimSpace(input.Purpose) == "" || len([]rune(input.Purpose)) > maxPurposeLength || strings.ContainsAny(input.Purpose, "\r\n") {
		return fmt.Errorf("%w: genesis input fields are invalid", ErrInvalidGenesis)
	}
	return validateActorCorrelation(input.Actor, input.CorrelationID)
}

func validateChainAddedInput(input ChainAddedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if _, err := chains.Restore(input.Chain); err != nil {
		return fmt.Errorf("%w: chain snapshot is invalid: %v", ErrInvalidPayload, err)
	}
	return nil
}

func validateLifecycleInput(input LifecycleTransitionInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if _, err := chains.NewChainID(string(input.ChainID)); err != nil || input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 || input.From.Validate() != nil || input.To.Validate() != nil {
		return fmt.Errorf("%w: lifecycle input fields are invalid", ErrInvalidPayload)
	}
	if err := chains.ValidateTransition(input.From, input.To); err != nil {
		return err
	}
	if input.Revision.ChainID != input.ChainID || input.Revision.PreviousRevision != input.PreviousRevision || input.Revision.NewRevision != input.NewRevision || input.Revision.PreviousStatus != input.From || input.Revision.NewStatus != input.To || !input.Revision.At.Equal(input.RecordedAt) || input.Revision.Actor != input.Actor || input.Revision.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: local revision does not match lifecycle input", ErrInvalidPayload)
	}
	if err := input.Revision.Operation.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	return validateRevisionOperation(input.From, input.To, input.Revision.Operation)
}

func validateObservationAddedInput(input ObservationAddedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if _, err := chains.NewChainID(string(input.ChainID)); err != nil || input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 {
		return fmt.Errorf("%w: observation revision fields are invalid", ErrInvalidPayload)
	}
	if err := input.Observation.Validate(); err != nil {
		return fmt.Errorf("%w: observation is invalid: %v", ErrInvalidPayload, err)
	}
	if err := input.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: revision is invalid: %v", ErrInvalidPayload, err)
	}
	revision := input.Revision
	if revision.ChainID != input.ChainID || revision.Operation != chains.OperationObservationAdded || revision.PreviousRevision != input.PreviousRevision || revision.NewRevision != input.NewRevision || revision.Actor != input.Actor || revision.CorrelationID != input.CorrelationID || len(revision.ObservationIDs) != 1 || revision.ObservationIDs[0] != input.Observation.ID || len(revision.ContributionIDs) != 0 || revision.PreviousStatus != "" || revision.NewStatus != "" {
		return fmt.Errorf("%w: observation revision does not match payload", ErrInvalidPayload)
	}
	return nil
}

func validateContributionAddedInput(input ContributionAddedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if _, err := chains.NewChainID(string(input.ChainID)); err != nil || input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 {
		return fmt.Errorf("%w: contribution revision fields are invalid", ErrInvalidPayload)
	}
	if err := input.Contribution.Validate(); err != nil {
		return fmt.Errorf("%w: contribution is invalid: %v", ErrInvalidPayload, err)
	}
	if _, err := chains.ProjectedConfidence(input.PreviousConfidence, input.Contribution); err != nil {
		return fmt.Errorf("%w: previous confidence is invalid: %v", ErrInvalidPayload, err)
	}
	expectedConfidence, err := chains.ProjectedConfidence(input.PreviousConfidence, input.Contribution)
	if err != nil || expectedConfidence != input.NewConfidence {
		return fmt.Errorf("%w: confidence result is inconsistent", ErrInvalidPayload)
	}
	if err := input.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: revision is invalid: %v", ErrInvalidPayload, err)
	}
	revision := input.Revision
	if revision.ChainID != input.ChainID || revision.Operation != chains.OperationContributionAdded || revision.PreviousRevision != input.PreviousRevision || revision.NewRevision != input.NewRevision || revision.Actor != input.Actor || revision.CorrelationID != input.CorrelationID || len(revision.ContributionIDs) != 1 || revision.ContributionIDs[0] != input.Contribution.ID || revision.PreviousStatus != "" || revision.NewStatus != "" || revision.PreviousConfidence != nil || revision.NewConfidence != nil || revision.PreviousHistoricalReliability != nil || revision.NewHistoricalReliability != nil {
		return fmt.Errorf("%w: contribution revision does not match payload", ErrInvalidPayload)
	}
	if input.NewSupportCount != input.PreviousSupportCount || input.NewContradictionCount != input.PreviousContradictionCount {
		switch input.Contribution.Kind {
		case chains.ContributionSupport:
			if input.NewSupportCount != input.PreviousSupportCount+1 || input.NewContradictionCount != input.PreviousContradictionCount {
				return fmt.Errorf("%w: support counters are inconsistent", ErrInvalidPayload)
			}
		case chains.ContributionContradiction:
			if input.NewContradictionCount != input.PreviousContradictionCount+1 || input.NewSupportCount != input.PreviousSupportCount {
				return fmt.Errorf("%w: contradiction counters are inconsistent", ErrInvalidPayload)
			}
		}
	} else if input.Contribution.Kind == chains.ContributionSupport || input.Contribution.Kind == chains.ContributionContradiction {
		return fmt.Errorf("%w: contribution counters are inconsistent", ErrInvalidPayload)
	}
	return nil
}

func validateHypothesisOpenedInput(input HypothesisOpenedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.RecordedAt.Before(input.Hypothesis.UpdatedAt) {
		return fmt.Errorf("%w: hypothesis record precedes local update", ErrInvalidPayload)
	}
	if input.Hypothesis.Status != hypotheses.StatusOpen || input.Hypothesis.Revision != 1 || len(input.Hypothesis.History) != 1 || input.Hypothesis.History[0].Operation != hypotheses.OperationHypothesisOpened || input.Hypothesis.History[0].PreviousRevision != 0 || input.Hypothesis.History[0].NewRevision != 1 {
		return fmt.Errorf("%w: hypothesis opening snapshot is not initial", ErrInvalidPayload)
	}
	opening := input.Hypothesis.History[0]
	if opening.SetID != input.Hypothesis.ID || opening.NewStatus != hypotheses.StatusOpen || opening.Actor != input.Actor || opening.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: hypothesis opening provenance does not match envelope", ErrInvalidPayload)
	}
	if _, err := hypotheses.Restore(input.Hypothesis); err != nil {
		return fmt.Errorf("%w: hypothesis snapshot is invalid: %v", ErrInvalidPayload, err)
	}
	return nil
}

func validateHypothesisStatusChangedInput(input HypothesisStatusChangedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 || input.Revision.SetID != input.SetID || input.Revision.Operation != hypotheses.OperationHypothesisStatusChanged || input.Revision.PreviousRevision != input.PreviousRevision || input.Revision.NewRevision != input.NewRevision || input.Revision.PreviousStatus != input.PreviousStatus || input.Revision.NewStatus != input.NewStatus || input.Revision.Actor != input.Actor || input.Revision.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: hypothesis status delta is inconsistent", ErrInvalidPayload)
	}
	if input.PreviousStatus.Validate() != nil || input.NewStatus.Validate() != nil || input.NewStatus == hypotheses.StatusSuperseded || hypotheses.ValidateTransition(input.PreviousStatus, input.NewStatus) != nil {
		return fmt.Errorf("%w: hypothesis status transition is invalid", ErrInvalidPayload)
	}
	if input.RecordedAt.Before(input.Revision.At) || input.Revision.At.IsZero() {
		return fmt.Errorf("%w: hypothesis revision timestamp is invalid", ErrInvalidPayload)
	}
	if err := input.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: hypothesis revision is invalid: %v", ErrInvalidPayload, err)
	}
	return nil
}

func validateHypothesisRebasedInput(input HypothesisRebasedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 || input.PreviousAssessmentVersion == 0 || input.NewAssessmentVersion != input.PreviousAssessmentVersion+1 || input.SetID == "" || input.PreviousAssessmentID == "" || input.NewAssessmentID == "" || !validHypothesisFingerprint(input.PreviousFingerprint) || !validHypothesisFingerprint(input.NewFingerprint) {
		return fmt.Errorf("%w: hypothesis rebase envelope is invalid", ErrInvalidPayload)
	}
	if input.PreviousFingerprint == input.NewFingerprint || input.Assessment.Version != input.NewAssessmentVersion || input.Assessment.ID != input.NewAssessmentID || input.Assessment.Fingerprint != input.NewFingerprint {
		return fmt.Errorf("%w: hypothesis rebase assessment is inconsistent", ErrInvalidPayload)
	}
	if input.Revision.SetID != input.SetID || input.Revision.Operation != hypotheses.OperationHypothesisRebased || input.Revision.PreviousRevision != input.PreviousRevision || input.Revision.NewRevision != input.NewRevision || input.Revision.PreviousStatus == "" || input.Revision.PreviousStatus != input.Revision.NewStatus || input.Revision.PreviousAssessmentVersion != input.PreviousAssessmentVersion || input.Revision.NewAssessmentVersion != input.NewAssessmentVersion || input.Revision.PreviousAssessmentID != input.PreviousAssessmentID || input.Revision.NewAssessmentID != input.NewAssessmentID || input.Revision.PreviousAssessmentFingerprint != input.PreviousFingerprint || input.Revision.NewAssessmentFingerprint != input.NewFingerprint || input.Revision.Actor != input.Actor || input.Revision.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: hypothesis rebase revision is inconsistent", ErrInvalidPayload)
	}
	if input.RecordedAt.Before(input.Revision.At) || input.Revision.At.IsZero() || input.Revision.PreviousStatus.Validate() != nil || input.Revision.NewStatus.Validate() != nil {
		return fmt.Errorf("%w: hypothesis rebase timestamp or status is invalid", ErrInvalidPayload)
	}
	if err := input.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: hypothesis rebase revision is invalid: %v", ErrInvalidPayload, err)
	}
	if input.Assessment.Provenance.Source == "" || input.Assessment.Provenance.PlannedOrEvaluatedAt.IsZero() || input.Assessment.Provenance.PolicyNamespace == "" || input.Assessment.Provenance.PolicyVersion == "" {
		return fmt.Errorf("%w: hypothesis rebase provenance is invalid", ErrInvalidPayload)
	}
	if len(input.Assessment.Alternatives) < 2 || input.Assessment.CreatedAt.IsZero() {
		return fmt.Errorf("%w: hypothesis rebase assessment is incomplete", ErrInvalidPayload)
	}
	return nil
}

func validateHypothesisSupersededInput(input HypothesisSupersededInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.PreviousSetID == "" || input.NewSetID == "" || input.PreviousSetID == input.NewSetID || input.PreviousRevision == 0 || input.NewPreviousRevision != 1 || input.PreviousStatus == hypotheses.StatusSuperseded || (input.PreviousStatus != hypotheses.StatusOpen && input.PreviousStatus != hypotheses.StatusUnderReview) || input.NewStatus != hypotheses.StatusSuperseded || input.PreviousSuccessorSetID != "" || input.NewSuccessorSetID != input.NewSetID {
		return fmt.Errorf("%w: hypothesis supersession envelope is invalid", ErrInvalidPayload)
	}
	if input.PreviousSetRevision.SetID != input.PreviousSetID || input.PreviousSetRevision.Operation != hypotheses.OperationHypothesisSuperseded || input.PreviousSetRevision.PreviousRevision != input.PreviousRevision || input.PreviousSetRevision.NewRevision != input.PreviousRevision+1 || input.PreviousSetRevision.PreviousStatus != input.PreviousStatus || input.PreviousSetRevision.NewStatus != hypotheses.StatusSuperseded || input.PreviousSetRevision.PreviousSuccessorSetID != "" || input.PreviousSetRevision.NewSuccessorSetID != input.NewSetID || input.PreviousSetRevision.Actor != input.Actor || input.PreviousSetRevision.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: hypothesis supersession revision is inconsistent", ErrInvalidPayload)
	}
	if input.RecordedAt.Before(input.PreviousSetRevision.At) || input.PreviousSetRevision.At.IsZero() {
		return fmt.Errorf("%w: hypothesis supersession timestamp is invalid", ErrInvalidPayload)
	}
	if err := input.PreviousSetRevision.Validate(); err != nil {
		return fmt.Errorf("%w: hypothesis supersession revision is invalid: %v", ErrInvalidPayload, err)
	}
	newSnapshot := input.NewHypothesis
	if newSnapshot.ID != input.NewSetID || newSnapshot.Family != hypotheses.FamilyEvidence || newSnapshot.Status != hypotheses.StatusOpen || newSnapshot.Revision != 1 || len(newSnapshot.History) != 1 || newSnapshot.Lineage.PredecessorSetID != input.PreviousSetID || newSnapshot.Lineage.RootSetID == "" || newSnapshot.Lineage.Generation != input.PreviousSetRevision.SuccessorGeneration || newSnapshot.Subject.ChainID == "" || newSnapshot.Subject.ObservationID == "" {
		return fmt.Errorf("%w: hypothesis successor snapshot is invalid", ErrInvalidPayload)
	}
	opening := newSnapshot.History[0]
	if opening.Operation != hypotheses.OperationHypothesisOpened || opening.PreviousRevision != 0 || opening.NewRevision != 1 || opening.Actor != input.Actor || opening.CorrelationID != input.CorrelationID || input.RecordedAt.Before(opening.At) {
		return fmt.Errorf("%w: hypothesis successor opening is inconsistent", ErrInvalidPayload)
	}
	if _, err := hypotheses.Restore(newSnapshot); err != nil {
		return fmt.Errorf("%w: hypothesis successor restore failed: %v", ErrInvalidPayload, err)
	}
	if newSnapshot.Subject.EvidenceFingerprint == "" {
		return fmt.Errorf("%w: successor fingerprint is empty", ErrInvalidPayload)
	}
	return nil
}

func validateHypothesisResolvedInput(input HypothesisResolvedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.SetID == "" || input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 || (input.PreviousStatus != hypotheses.StatusOpen && input.PreviousStatus != hypotheses.StatusUnderReview) || input.NewStatus != hypotheses.StatusResolved || input.AssessmentVersion == 0 || input.AssessmentID == "" || !validHypothesisFingerprint(input.AssessmentFingerprint) || input.AlternativeID == "" || input.AlternativeKind.Validate() != nil || !validHypothesisFingerprint(input.EffectFingerprint) {
		return fmt.Errorf("%w: hypothesis resolution envelope is invalid", ErrInvalidPayload)
	}
	if input.HypothesisRevision.SetID != input.SetID || input.HypothesisRevision.Operation != hypotheses.OperationHypothesisResolved || input.HypothesisRevision.PreviousRevision != input.PreviousRevision || input.HypothesisRevision.NewRevision != input.NewRevision || input.HypothesisRevision.PreviousStatus != input.PreviousStatus || input.HypothesisRevision.NewStatus != input.NewStatus || input.HypothesisRevision.SelectedAssessmentVersion != input.AssessmentVersion || input.HypothesisRevision.SelectedAssessmentID != input.AssessmentID || input.HypothesisRevision.SelectedAssessmentFingerprint != input.AssessmentFingerprint || input.HypothesisRevision.SelectedAlternativeID != input.AlternativeID || input.HypothesisRevision.SelectedAlternativeKind != input.AlternativeKind || input.HypothesisRevision.SelectedEffectKind != input.Effect.Kind || input.HypothesisRevision.SelectedEffectFingerprint != input.EffectFingerprint || input.HypothesisRevision.Actor != input.Actor || input.HypothesisRevision.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: hypothesis resolution revision is inconsistent", ErrInvalidPayload)
	}
	if input.RecordedAt.Before(input.HypothesisRevision.At) || input.HypothesisRevision.At.IsZero() {
		return fmt.Errorf("%w: hypothesis resolution timestamp is invalid", ErrInvalidPayload)
	}
	if err := input.HypothesisRevision.Validate(); err != nil {
		return fmt.Errorf("%w: hypothesis resolution revision: %v", ErrInvalidPayload, err)
	}
	if _, err := input.Effect.Fingerprint(); err != nil {
		return fmt.Errorf("%w: effect: %v", ErrInvalidPayload, err)
	}
	fingerprint, _ := input.Effect.Fingerprint()
	if fingerprint != input.EffectFingerprint {
		return fmt.Errorf("%w: effect fingerprint is inconsistent", ErrInvalidPayload)
	}
	if err := input.Outcome.Validate(); err != nil || input.Outcome.Kind != input.Effect.Kind {
		return fmt.Errorf("%w: outcome: %v", ErrInvalidPayload, err)
	}
	if err := validateResolutionChainDelta(input.ChainDelta, input.Effect, input.Outcome); err != nil {
		return err
	}
	return nil
}

func validateResolutionChainDelta(delta ResolutionChainDelta, effect hypotheses.ResolutionEffect, outcome hypotheses.ResolutionOutcome) error {
	if delta.Kind != effect.Kind {
		return fmt.Errorf("%w: chain delta kind mismatch", ErrInvalidPayload)
	}
	count := 0
	if delta.ObservationAdded != nil {
		count++
	}
	if delta.ChainAdded != nil {
		count++
	}
	if delta.ContributionAdded != nil {
		count++
	}
	if delta.NoChainEffect != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: resolution chain delta union is not exact", ErrInvalidPayload)
	}
	switch effect.Kind {
	case hypotheses.ResolutionEffectAttachObservation:
		if delta.ObservationAdded == nil || outcome.AttachObservation == nil || delta.ObservationAdded.ChainID != outcome.AttachObservation.ChainID || delta.ObservationAdded.PreviousRevision != outcome.AttachObservation.PreviousRevision || delta.ObservationAdded.NewRevision != outcome.AttachObservation.NewRevision || delta.ObservationAdded.Observation.ID != outcome.AttachObservation.ObservationID || delta.ObservationAdded.Revision.PreviousRevision != outcome.AttachObservation.PreviousRevision || delta.ObservationAdded.Revision.NewRevision != outcome.AttachObservation.NewRevision {
			return fmt.Errorf("%w: observation delta does not match outcome", ErrInvalidPayload)
		}
	case hypotheses.ResolutionEffectCreateCandidate:
		if delta.ChainAdded == nil || outcome.CreateCandidate == nil || delta.ChainAdded.Chain.ID != outcome.CreateCandidate.ChainID || delta.ChainAdded.Chain.Revision != outcome.CreateCandidate.NewRevision || delta.ChainAdded.Chain.Status != outcome.CreateCandidate.Status {
			return fmt.Errorf("%w: chain delta does not match outcome", ErrInvalidPayload)
		}
	case hypotheses.ResolutionEffectAddContribution:
		if delta.ContributionAdded == nil || outcome.AddContribution == nil || delta.ContributionAdded.ChainID != outcome.AddContribution.ChainID || delta.ContributionAdded.PreviousRevision != outcome.AddContribution.PreviousRevision || delta.ContributionAdded.NewRevision != outcome.AddContribution.NewRevision || delta.ContributionAdded.Contribution.ID != outcome.AddContribution.ContributionID || delta.ContributionAdded.PreviousConfidence != outcome.AddContribution.PreviousConfidence || delta.ContributionAdded.NewConfidence != outcome.AddContribution.NewConfidence {
			return fmt.Errorf("%w: contribution delta does not match outcome", ErrInvalidPayload)
		}
	case hypotheses.ResolutionEffectNoChain:
		if delta.NoChainEffect == nil || outcome.NoChainEffect == nil || delta.NoChainEffect.ReasonCode != outcome.NoChainEffect.ReasonCode {
			return fmt.Errorf("%w: no-chain delta does not match outcome", ErrInvalidPayload)
		}
	}
	return nil
}

func validHypothesisFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validateCheckpointInput(input SnapshotCheckpointInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.SnapshotSchemaVersion != 1 || input.SnapshotCreatedAt.IsZero() || input.SnapshotChainCount < 0 || input.SnapshotSizeBytes <= 0 || !validateHash(input.SnapshotPayloadSHA256) || input.JournalSequence == 0 || !validateHash(input.JournalHeadHash) {
		return ErrInvalidCheckpoint
	}
	return nil
}

func validateRecordedMutation(recordedAt time.Time, actor, correlation string) error {
	if recordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is zero", ErrInvalidRecord)
	}
	return validateActorCorrelation(actor, correlation)
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
