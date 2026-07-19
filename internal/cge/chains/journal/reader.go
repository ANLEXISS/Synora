package journal

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/persistence"
	"synora/internal/cge/hypotheses"
)

type journalFileState struct {
	snapshot JournalSnapshot
	size     int64
	exists   bool
	valid    bool
}

func readJournalFile(ctx context.Context, path string, maxJournalSize int64, maxRecordSize int) (journalFileState, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return journalFileState{}, ErrJournalNotFound
	}
	if err != nil {
		return journalFileState{}, fmt.Errorf("%w: stat journal: %v", ErrInvalidRecord, err)
	}
	if info.IsDir() {
		return journalFileState{}, fmt.Errorf("%w: journal path is a directory", ErrInvalidPath)
	}
	if info.Size() == 0 {
		return journalFileState{exists: true, size: 0}, nil
	}
	if info.Size() > maxJournalSize {
		return journalFileState{}, fmt.Errorf("%w: size=%d limit=%d", ErrJournalTooLarge, info.Size(), maxJournalSize)
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return journalFileState{}, ErrJournalNotFound
		}
		return journalFileState{}, fmt.Errorf("%w: open journal: %v", ErrInvalidRecord, err)
	}
	data, readErr := readContextLimited(ctx, file, maxJournalSize)
	closeErr := file.Close()
	if readErr != nil {
		return journalFileState{}, readErr
	}
	if closeErr != nil {
		return journalFileState{}, fmt.Errorf("%w: close journal: %v", ErrInvalidRecord, closeErr)
	}
	if len(data) == 0 {
		return journalFileState{exists: true, size: 0}, nil
	}
	snapshot, err := parseJournalData(ctx, data, maxRecordSize)
	if err != nil {
		return journalFileState{}, err
	}
	return journalFileState{snapshot: snapshot, size: int64(len(data)), exists: true, valid: true}, nil
}

// ReadAll validates the complete journal and returns only a defensive view.
func (j *FileJournal) ReadAll(ctx context.Context) (JournalSnapshot, error) {
	if err := checkContext(ctx); err != nil {
		return JournalSnapshot{}, err
	}
	if err := j.validate(); err != nil {
		return JournalSnapshot{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := readJournalFile(ctx, j.path, j.options.MaxJournalSize, j.options.MaxRecordSize)
	if errors.Is(err, ErrJournalNotFound) {
		return JournalSnapshot{}, fmt.Errorf("%w: %s", ErrJournalNotFound, j.path)
	}
	if err != nil {
		return JournalSnapshot{}, err
	}
	if !state.valid {
		return JournalSnapshot{}, ErrJournalEmpty
	}
	return state.snapshot.Clone(), nil
}

func parseJournalData(ctx context.Context, data []byte, maxRecordSize int) (JournalSnapshot, error) {
	if len(data) == 0 {
		return JournalSnapshot{}, ErrJournalEmpty
	}
	if !utf8.Valid(data) {
		return JournalSnapshot{}, fmt.Errorf("%w: journal is not UTF-8", ErrInvalidRecord)
	}
	reader := bufio.NewReader(bytes.NewReader(data))
	records := make([]Record, 0)
	for {
		if err := checkContext(ctx); err != nil {
			return JournalSnapshot{}, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if len(line) > maxRecordSize {
				return JournalSnapshot{}, fmt.Errorf("%w: size=%d limit=%d", ErrRecordTooLarge, len(line), maxRecordSize)
			}
			if line[len(line)-1] != '\n' {
				return JournalSnapshot{}, fmt.Errorf("%w: final line has no newline", ErrInvalidRecord)
			}
			content := line[:len(line)-1]
			if len(bytes.TrimSpace(content)) == 0 {
				return JournalSnapshot{}, fmt.Errorf("%w: empty journal line", ErrInvalidRecord)
			}
			var record Record
			if err := decodeStrictJSON(content, &record); err != nil {
				return JournalSnapshot{}, fmt.Errorf("%w: decode line %d: %v", ErrInvalidRecord, len(records)+1, err)
			}
			records = append(records, record)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return JournalSnapshot{}, fmt.Errorf("%w: read line %d: %v", ErrInvalidRecord, len(records)+1, err)
		}
	}
	if len(records) == 0 {
		return JournalSnapshot{}, ErrJournalEmpty
	}
	return validateRecords(ctx, records)
}

func validateRecords(ctx context.Context, records []Record) (JournalSnapshot, error) {
	if len(records) == 0 {
		return JournalSnapshot{}, ErrJournalEmpty
	}
	chainRevisions := make(map[chains.ChainID]uint64)
	chainStatuses := make(map[chains.ChainID]chains.Status)
	chainObservations := make(map[chains.ChainID]map[string]struct{})
	chainContributions := make(map[chains.ChainID]map[string]struct{})
	chainConfidence := make(map[chains.ChainID]float64)
	chainSupport := make(map[chains.ChainID]uint64)
	chainContradiction := make(map[chains.ChainID]uint64)
	chainRevisionAt := make(map[chains.ChainID]time.Time)
	seenChains := make(map[chains.ChainID]struct{})
	var journalID string
	var previousHash string
	for index := range records {
		if err := checkContext(ctx); err != nil {
			return JournalSnapshot{}, err
		}
		record := &records[index]
		expectedSequence := uint64(index + 1)
		if record.Sequence == 0 {
			return JournalSnapshot{}, InvalidSequenceError{Sequence: record.Sequence}
		}
		if record.Sequence != expectedSequence {
			return JournalSnapshot{}, SequenceError{Expected: expectedSequence, Found: record.Sequence}
		}
		if err := validateRecordFields(*record); err != nil {
			return JournalSnapshot{}, err
		}
		if index == 0 {
			if record.Kind != RecordKindGenesis {
				return JournalSnapshot{}, fmt.Errorf("%w: first record must be genesis", ErrInvalidGenesis)
			}
			if record.PreviousHash != GenesisPreviousHash {
				return JournalSnapshot{}, fmt.Errorf("%w: genesis previous hash is invalid", ErrPreviousHashMismatch)
			}
		} else {
			if record.Kind == RecordKindGenesis {
				return JournalSnapshot{}, fmt.Errorf("%w: genesis at sequence %d", ErrDuplicateGenesis, record.Sequence)
			}
			if record.PreviousHash != previousHash {
				return JournalSnapshot{}, fmt.Errorf("%w: sequence=%d", ErrPreviousHashMismatch, record.Sequence)
			}
		}
		switch record.Kind {
		case RecordKindGenesis:
			payload, err := decodeGenesisPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if journalID != "" {
				return JournalSnapshot{}, ErrDuplicateGenesis
			}
			if !payload.CreatedAt.Equal(record.RecordedAt) {
				return JournalSnapshot{}, fmt.Errorf("%w: created_at does not match recorded_at", ErrInvalidGenesis)
			}
			journalID = payload.JournalID
		case RecordKindChainAdded:
			payload, err := decodeChainAddedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			chainID := payload.Chain.ID
			if _, exists := seenChains[chainID]; exists {
				return JournalSnapshot{}, fmt.Errorf("%w: chain %s added twice", ErrInvalidPayload, chainID)
			}
			seenChains[chainID] = struct{}{}
			chainRevisions[chainID] = payload.Chain.Revision
			chainStatuses[chainID] = payload.Chain.Status
			chainObservations[chainID] = make(map[string]struct{}, len(payload.Chain.Observations))
			chainContributions[chainID] = make(map[string]struct{}, len(payload.Chain.Contributions))
			for _, observation := range payload.Chain.Observations {
				chainObservations[chainID][observation.ID] = struct{}{}
			}
			for _, contribution := range payload.Chain.Contributions {
				chainContributions[chainID][contribution.ID] = struct{}{}
			}
			chainConfidence[chainID] = payload.Chain.CurrentConfidence
			chainSupport[chainID] = payload.Chain.ConfirmationCount
			chainContradiction[chainID] = payload.Chain.ContradictionCount
			if history := payload.Chain.History; len(history) > 0 {
				chainRevisionAt[chainID] = history[len(history)-1].At
			}
		case RecordKindLifecycleTransitioned:
			payload, err := decodeLifecyclePayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if !payload.Revision.At.Equal(record.RecordedAt) || payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID {
				return JournalSnapshot{}, fmt.Errorf("%w: lifecycle record provenance does not match envelope", ErrInvalidPayload)
			}
			currentRevision, exists := chainRevisions[payload.ChainID]
			if !exists {
				return JournalSnapshot{}, fmt.Errorf("%w: transition before chain.added", ErrInvalidPayload)
			}
			if payload.PreviousRevision != currentRevision || payload.NewRevision != currentRevision+1 {
				return JournalSnapshot{}, fmt.Errorf("%w: lifecycle revision chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			if chainStatuses[payload.ChainID] != payload.From {
				return JournalSnapshot{}, fmt.Errorf("%w: lifecycle status chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			if lastAt := chainRevisionAt[payload.ChainID]; payload.Revision.At.Before(lastAt) {
				return JournalSnapshot{}, fmt.Errorf("%w: lifecycle timestamp chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			chainRevisions[payload.ChainID] = payload.NewRevision
			chainStatuses[payload.ChainID] = payload.To
			chainRevisionAt[payload.ChainID] = payload.Revision.At
		case RecordKindSnapshotCheckpointed:
			payload, err := decodeCheckpointPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if payload.JournalSequence != uint64(index) || payload.JournalHeadHash != previousHeadHash(index, records) {
				return JournalSnapshot{}, fmt.Errorf("%w: checkpoint does not identify preceding head", ErrInvalidCheckpoint)
			}
		case RecordKindObservationAdded:
			payload, err := decodeObservationAddedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			currentRevision, exists := chainRevisions[payload.ChainID]
			if !exists {
				return JournalSnapshot{}, fmt.Errorf("%w: observation before chain.added", ErrInvalidPayload)
			}
			if currentRevision != payload.PreviousRevision || payload.NewRevision != payload.PreviousRevision+1 {
				return JournalSnapshot{}, fmt.Errorf("%w: observation revision chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			if err := chainStatuses[payload.ChainID].ValidateObservationMutation(); err != nil {
				return JournalSnapshot{}, err
			}
			if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID {
				return JournalSnapshot{}, fmt.Errorf("%w: observation provenance does not match envelope", ErrInvalidPayload)
			}
			if lastAt := chainRevisionAt[payload.ChainID]; payload.Revision.At.Before(lastAt) {
				return JournalSnapshot{}, fmt.Errorf("%w: observation timestamp chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			if _, duplicate := chainObservations[payload.ChainID][payload.Observation.ID]; duplicate {
				return JournalSnapshot{}, fmt.Errorf("%w: %s", ErrDuplicateObservation, payload.Observation.ID)
			}
			chainObservations[payload.ChainID][payload.Observation.ID] = struct{}{}
			chainRevisions[payload.ChainID] = payload.NewRevision
			chainRevisionAt[payload.ChainID] = payload.Revision.At
		case RecordKindContributionAdded:
			payload, err := decodeContributionAddedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			currentRevision, exists := chainRevisions[payload.ChainID]
			if !exists {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution before chain.added", ErrInvalidPayload)
			}
			if currentRevision != payload.PreviousRevision || payload.NewRevision != payload.PreviousRevision+1 {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution revision chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			if err := chainStatuses[payload.ChainID].ValidateContributionMutation(); err != nil {
				return JournalSnapshot{}, err
			}
			if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution provenance does not match envelope", ErrInvalidPayload)
			}
			if lastAt := chainRevisionAt[payload.ChainID]; payload.Revision.At.Before(lastAt) {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution timestamp chain=%s", ErrInvalidPayload, payload.ChainID)
			}
			if _, duplicate := chainContributions[payload.ChainID][payload.Contribution.ID]; duplicate {
				return JournalSnapshot{}, fmt.Errorf("%w: %s", chains.ErrDuplicateContribution, payload.Contribution.ID)
			}
			if payload.PreviousConfidence != chainConfidence[payload.ChainID] || payload.PreviousSupportCount != chainSupport[payload.ChainID] || payload.PreviousContradictionCount != chainContradiction[payload.ChainID] {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution before-state mismatch", ErrInvalidPayload)
			}
			for _, observationID := range payload.Revision.ObservationIDs {
				if _, known := chainObservations[payload.ChainID][observationID]; !known {
					return JournalSnapshot{}, fmt.Errorf("%w: contribution references unknown observation", chains.ErrUnknownObservationReference)
				}
			}
			expectedConfidence, _ := chains.ProjectedConfidence(payload.PreviousConfidence, payload.Contribution)
			if payload.NewConfidence != expectedConfidence {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution confidence mismatch", ErrInvalidPayload)
			}
			expectedSupport, expectedContradiction := payload.PreviousSupportCount, payload.PreviousContradictionCount
			switch payload.Contribution.Kind {
			case chains.ContributionSupport:
				expectedSupport++
			case chains.ContributionContradiction:
				expectedContradiction++
			}
			if payload.NewSupportCount != expectedSupport || payload.NewContradictionCount != expectedContradiction {
				return JournalSnapshot{}, fmt.Errorf("%w: contribution counter mismatch", ErrInvalidPayload)
			}
			chainContributions[payload.ChainID][payload.Contribution.ID] = struct{}{}
			chainConfidence[payload.ChainID] = payload.NewConfidence
			chainSupport[payload.ChainID] = payload.NewSupportCount
			chainContradiction[payload.ChainID] = payload.NewContradictionCount
			chainRevisions[payload.ChainID] = payload.NewRevision
			chainRevisionAt[payload.ChainID] = payload.Revision.At
		case RecordKindHypothesisOpened:
			payload, err := decodeHypothesisOpenedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			opening := payload.Hypothesis.History[0]
			if opening.Actor != record.Actor || opening.CorrelationID != record.CorrelationID || record.RecordedAt.Before(opening.At) {
				return JournalSnapshot{}, fmt.Errorf("%w: hypothesis opening provenance does not match envelope", ErrInvalidPayload)
			}
		case RecordKindHypothesisStatusChanged:
			payload, err := decodeHypothesisStatusChangedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Revision.At) {
				return JournalSnapshot{}, fmt.Errorf("%w: hypothesis status provenance does not match envelope", ErrInvalidPayload)
			}
		case RecordKindHypothesisRebased:
			payload, err := decodeHypothesisRebasedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Revision.At) {
				return JournalSnapshot{}, fmt.Errorf("%w: hypothesis rebase provenance does not match envelope", ErrInvalidPayload)
			}
		case RecordKindHypothesisSuperseded:
			payload, err := decodeHypothesisSupersededPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if payload.PreviousSetRevision.Actor != record.Actor || payload.PreviousSetRevision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.PreviousSetRevision.At) {
				return JournalSnapshot{}, fmt.Errorf("%w: hypothesis supersession provenance does not match envelope", ErrInvalidPayload)
			}
		case RecordKindHypothesisResolved:
			payload, err := decodeHypothesisResolvedPayload(record.Payload)
			if err != nil {
				return JournalSnapshot{}, err
			}
			if payload.HypothesisRevision.Actor != record.Actor || payload.HypothesisRevision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.HypothesisRevision.At) {
				return JournalSnapshot{}, fmt.Errorf("%w: hypothesis resolution provenance does not match envelope", ErrInvalidPayload)
			}
			if err := applyResolutionChainState(payload.ChainDelta, chainRevisions, chainStatuses, chainObservations, chainContributions, chainConfidence, chainSupport, chainContradiction, chainRevisionAt); err != nil {
				return JournalSnapshot{}, err
			}
		case RecordKindRoutineCreated:
			if _, err := decodeRoutineCreatedPayload(*record); err != nil {
				return JournalSnapshot{}, err
			}
		case RecordKindRoutineOccurrenceAdded:
			if _, err := decodeRoutineOccurrenceAddedPayload(*record); err != nil {
				return JournalSnapshot{}, err
			}
		case RecordKindRoutineStatusChanged:
			if _, err := decodeRoutineStatusChangedPayload(*record); err != nil {
				return JournalSnapshot{}, err
			}
		}
		previousHash = record.RecordHash
	}
	result := JournalSnapshot{
		SchemaVersion: CurrentSchemaVersion,
		JournalID:     journalID,
		RecordCount:   uint64(len(records)),
		HeadSequence:  records[len(records)-1].Sequence,
		HeadHash:      records[len(records)-1].RecordHash,
		Records:       make([]Record, len(records)),
	}
	for i, record := range records {
		result.Records[i] = cloneRecord(record)
	}
	return result, nil
}

func previousHeadHash(index int, records []Record) string {
	if index == 0 {
		return GenesisPreviousHash
	}
	return records[index-1].RecordHash
}

func validateRecordFields(record Record) error {
	if record.SchemaVersion == 0 {
		return ErrInvalidSchema
	}
	if record.SchemaVersion != CurrentSchemaVersion {
		return UnsupportedSchemaError{Found: record.SchemaVersion, Supported: CurrentSchemaVersion}
	}
	if err := record.Kind.Validate(); err != nil {
		return err
	}
	if record.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is zero", ErrInvalidRecord)
	}
	if err := validateActorCorrelation(record.Actor, record.CorrelationID); err != nil {
		return err
	}
	if len(bytes.TrimSpace(record.Payload)) == 0 || bytes.Equal(bytes.TrimSpace(record.Payload), []byte("null")) || !json.Valid(record.Payload) {
		return fmt.Errorf("%w: payload is not a JSON value", ErrInvalidPayload)
	}
	if record.PayloadSHA256 != payloadChecksum(record.Payload) {
		return fmt.Errorf("%w: sequence=%d", ErrPayloadChecksumMismatch, record.Sequence)
	}
	if record.RecordHash != hashRecord(record) {
		return fmt.Errorf("%w: sequence=%d", ErrRecordHashMismatch, record.Sequence)
	}
	return nil
}

func validateActorCorrelation(actor, correlation string) error {
	if strings.TrimSpace(actor) == "" || actor != strings.TrimSpace(actor) || len([]rune(actor)) > maxActorLength || strings.ContainsAny(actor, "\r\n") {
		return fmt.Errorf("%w: actor", ErrInvalidRecord)
	}
	if strings.TrimSpace(correlation) == "" || correlation != strings.TrimSpace(correlation) || len([]rune(correlation)) > maxCorrelationLength || strings.ContainsAny(correlation, "\r\n") {
		return fmt.Errorf("%w: correlation", ErrInvalidRecord)
	}
	return nil
}

type recordHashBody struct {
	SchemaVersion int             `json:"schema_version"`
	Sequence      uint64          `json:"sequence"`
	Kind          RecordKind      `json:"kind"`
	RecordedAt    string          `json:"recorded_at"`
	Actor         string          `json:"actor"`
	CorrelationID string          `json:"correlation_id"`
	PreviousHash  string          `json:"previous_hash"`
	Payload       json.RawMessage `json:"payload"`
	PayloadSHA256 string          `json:"payload_sha256"`
}

func hashRecord(record Record) string {
	body := recordHashBody{
		SchemaVersion: record.SchemaVersion,
		Sequence:      record.Sequence,
		Kind:          record.Kind,
		RecordedAt:    record.RecordedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Actor:         record.Actor,
		CorrelationID: record.CorrelationID,
		PreviousHash:  record.PreviousHash,
		Payload:       record.Payload,
		PayloadSHA256: record.PayloadSHA256,
	}
	data, _ := json.Marshal(body)
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func payloadChecksum(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validateHash(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil && value == strings.ToLower(value)
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func decodeGenesisPayload(data []byte) (GenesisPayload, error) {
	var payload GenesisPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return GenesisPayload{}, fmt.Errorf("%w: genesis: %v", ErrInvalidGenesis, err)
	}
	if strings.TrimSpace(payload.JournalID) == "" || len([]rune(payload.JournalID)) > maxPurposeLength || strings.ContainsAny(payload.JournalID, "\r\n") || payload.CreatedAt.IsZero() || strings.TrimSpace(payload.Purpose) == "" || len([]rune(payload.Purpose)) > maxPurposeLength || strings.ContainsAny(payload.Purpose, "\r\n") {
		return GenesisPayload{}, fmt.Errorf("%w: genesis fields are invalid", ErrInvalidGenesis)
	}
	return payload, nil
}

func decodeChainAddedPayload(data []byte) (ChainAddedPayload, error) {
	var payload ChainAddedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return ChainAddedPayload{}, fmt.Errorf("%w: chain.added: %v", ErrInvalidPayload, err)
	}
	if _, err := chains.Restore(payload.Chain); err != nil {
		return ChainAddedPayload{}, fmt.Errorf("%w: chain.added restore: %v", ErrInvalidPayload, err)
	}
	return payload, nil
}

func decodeLifecyclePayload(data []byte) (LifecycleTransitionPayload, error) {
	var payload LifecycleTransitionPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return LifecycleTransitionPayload{}, fmt.Errorf("%w: lifecycle: %v", ErrInvalidPayload, err)
	}
	if _, err := chains.NewChainID(string(payload.ChainID)); err != nil || payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 || payload.From.Validate() != nil || payload.To.Validate() != nil || chains.ValidateTransition(payload.From, payload.To) != nil {
		return LifecycleTransitionPayload{}, fmt.Errorf("%w: lifecycle transition fields are invalid", ErrInvalidPayload)
	}
	if payload.Revision.ChainID != payload.ChainID || payload.Revision.PreviousRevision != payload.PreviousRevision || payload.Revision.NewRevision != payload.NewRevision || payload.Revision.PreviousStatus != payload.From || payload.Revision.NewStatus != payload.To || payload.Revision.At.IsZero() || payload.Revision.Operation.Validate() != nil || strings.TrimSpace(payload.Revision.Reason) == "" || payload.Revision.Actor == "" || payload.Revision.CorrelationID == "" {
		return LifecycleTransitionPayload{}, fmt.Errorf("%w: lifecycle revision provenance is invalid", ErrInvalidPayload)
	}
	if err := validateRevisionOperation(payload.From, payload.To, payload.Revision.Operation); err != nil {
		return LifecycleTransitionPayload{}, err
	}
	return payload, nil
}

func decodeObservationAddedPayload(data []byte) (ObservationAddedPayload, error) {
	var payload ObservationAddedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return ObservationAddedPayload{}, fmt.Errorf("%w: observation.added: %v", ErrInvalidPayload, err)
	}
	if _, err := chains.NewChainID(string(payload.ChainID)); err != nil || payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 {
		return ObservationAddedPayload{}, fmt.Errorf("%w: observation revision fields are invalid", ErrInvalidPayload)
	}
	if err := payload.Observation.Validate(); err != nil {
		return ObservationAddedPayload{}, fmt.Errorf("%w: observation: %v", ErrInvalidPayload, err)
	}
	if err := payload.Revision.Validate(); err != nil {
		return ObservationAddedPayload{}, fmt.Errorf("%w: revision: %v", ErrInvalidPayload, err)
	}
	revision := payload.Revision
	if revision.ChainID != payload.ChainID || revision.Operation != chains.OperationObservationAdded || revision.PreviousRevision != payload.PreviousRevision || revision.NewRevision != payload.NewRevision || len(revision.ObservationIDs) != 1 || revision.ObservationIDs[0] != payload.Observation.ID || len(revision.ContributionIDs) != 0 || revision.PreviousStatus != "" || revision.NewStatus != "" || revision.Actor == "" || revision.CorrelationID == "" {
		return ObservationAddedPayload{}, fmt.Errorf("%w: observation revision does not match payload", ErrInvalidPayload)
	}
	return payload, nil
}

func decodeContributionAddedPayload(data []byte) (ContributionAddedPayload, error) {
	var payload ContributionAddedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return ContributionAddedPayload{}, fmt.Errorf("%w: contribution.added: %v", ErrInvalidPayload, err)
	}
	if _, err := chains.NewChainID(string(payload.ChainID)); err != nil || payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 {
		return ContributionAddedPayload{}, fmt.Errorf("%w: contribution revision fields are invalid", ErrInvalidPayload)
	}
	if err := payload.Contribution.Validate(); err != nil {
		return ContributionAddedPayload{}, fmt.Errorf("%w: contribution: %v", ErrInvalidPayload, err)
	}
	if _, err := chains.ProjectedConfidence(payload.PreviousConfidence, payload.Contribution); err != nil {
		return ContributionAddedPayload{}, fmt.Errorf("%w: previous confidence: %v", ErrInvalidPayload, err)
	}
	expected, _ := chains.ProjectedConfidence(payload.PreviousConfidence, payload.Contribution)
	if expected != payload.NewConfidence {
		return ContributionAddedPayload{}, fmt.Errorf("%w: confidence result is inconsistent", ErrInvalidPayload)
	}
	if err := payload.Revision.Validate(); err != nil {
		return ContributionAddedPayload{}, fmt.Errorf("%w: revision: %v", ErrInvalidPayload, err)
	}
	revision := payload.Revision
	if revision.ChainID != payload.ChainID || revision.Operation != chains.OperationContributionAdded || revision.PreviousRevision != payload.PreviousRevision || revision.NewRevision != payload.NewRevision || len(revision.ContributionIDs) != 1 || revision.ContributionIDs[0] != payload.Contribution.ID || revision.PreviousStatus != "" || revision.NewStatus != "" || revision.PreviousConfidence != nil || revision.NewConfidence != nil || revision.Actor == "" || revision.CorrelationID == "" {
		return ContributionAddedPayload{}, fmt.Errorf("%w: contribution revision does not match payload", ErrInvalidPayload)
	}
	for _, observationID := range payload.Contribution.ObservationIDs {
		found := false
		for _, revisionID := range revision.ObservationIDs {
			if observationID == revisionID {
				found = true
				break
			}
		}
		if !found {
			return ContributionAddedPayload{}, fmt.Errorf("%w: contribution observation reference is absent from revision", ErrInvalidPayload)
		}
	}
	return payload, nil
}

func decodeCheckpointPayload(data []byte) (SnapshotCheckpointPayload, error) {
	var payload SnapshotCheckpointPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return SnapshotCheckpointPayload{}, fmt.Errorf("%w: checkpoint: %v", ErrInvalidCheckpoint, err)
	}
	if payload.SnapshotSchemaVersion != persistence.CurrentSchemaVersion || payload.SnapshotCreatedAt.IsZero() || payload.SnapshotChainCount < 0 || payload.SnapshotSizeBytes <= 0 || !validateHash(payload.SnapshotPayloadSHA256) || payload.JournalSequence == 0 || !validateHash(payload.JournalHeadHash) {
		return SnapshotCheckpointPayload{}, fmt.Errorf("%w: checkpoint fields are invalid", ErrInvalidCheckpoint)
	}
	return payload, nil
}

func decodeHypothesisOpenedPayload(data []byte) (HypothesisOpenedPayload, error) {
	var payload HypothesisOpenedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return HypothesisOpenedPayload{}, fmt.Errorf("%w: hypothesis.opened: %v", ErrInvalidPayload, err)
	}
	if payload.Hypothesis.Status != hypotheses.StatusOpen || payload.Hypothesis.Revision != 1 || len(payload.Hypothesis.History) != 1 {
		return HypothesisOpenedPayload{}, fmt.Errorf("%w: hypothesis opening snapshot is not initial", ErrInvalidPayload)
	}
	if _, err := hypotheses.Restore(payload.Hypothesis); err != nil {
		return HypothesisOpenedPayload{}, fmt.Errorf("%w: hypothesis.opened restore: %v", ErrInvalidPayload, err)
	}
	return payload, nil
}

func decodeHypothesisStatusChangedPayload(data []byte) (HypothesisStatusChangedPayload, error) {
	var payload HypothesisStatusChangedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return HypothesisStatusChangedPayload{}, fmt.Errorf("%w: hypothesis.status_changed: %v", ErrInvalidPayload, err)
	}
	if err := validateHypothesisStatusChangedInput(HypothesisStatusChangedInput{
		SetID: payload.SetID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision,
		PreviousStatus: payload.PreviousStatus, NewStatus: payload.NewStatus, Revision: payload.Revision,
		RecordedAt: payload.Revision.At, Actor: payload.Revision.Actor, CorrelationID: payload.Revision.CorrelationID,
	}); err != nil {
		return HypothesisStatusChangedPayload{}, err
	}
	return payload, nil
}

func decodeHypothesisRebasedPayload(data []byte) (HypothesisRebasedPayload, error) {
	var payload HypothesisRebasedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return HypothesisRebasedPayload{}, fmt.Errorf("%w: hypothesis.rebased: %v", ErrInvalidPayload, err)
	}
	if err := validateHypothesisRebasedInput(HypothesisRebasedInput{SetID: payload.SetID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision, PreviousAssessmentVersion: payload.PreviousAssessmentVersion, NewAssessmentVersion: payload.NewAssessmentVersion, PreviousAssessmentID: payload.PreviousAssessmentID, NewAssessmentID: payload.NewAssessmentID, PreviousFingerprint: payload.PreviousFingerprint, NewFingerprint: payload.NewFingerprint, Assessment: payload.Assessment, Revision: payload.Revision, RecordedAt: payload.Revision.At, Actor: payload.Revision.Actor, CorrelationID: payload.Revision.CorrelationID}); err != nil {
		return HypothesisRebasedPayload{}, err
	}
	return payload, nil
}

func decodeHypothesisSupersededPayload(data []byte) (HypothesisSupersededPayload, error) {
	var payload HypothesisSupersededPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return HypothesisSupersededPayload{}, fmt.Errorf("%w: hypothesis.superseded: %v", ErrInvalidPayload, err)
	}
	if err := validateHypothesisSupersededInput(HypothesisSupersededInput{PreviousSetID: payload.PreviousSetID, NewSetID: payload.NewSetID, PreviousRevision: payload.PreviousRevision, NewPreviousRevision: payload.NewPreviousRevision, PreviousStatus: payload.PreviousStatus, NewStatus: payload.NewStatus, PreviousSuccessorSetID: payload.PreviousSuccessorSetID, NewSuccessorSetID: payload.NewSuccessorSetID, PreviousSetRevision: payload.PreviousSetRevision, NewHypothesis: payload.NewHypothesis, RecordedAt: payload.PreviousSetRevision.At, Actor: payload.PreviousSetRevision.Actor, CorrelationID: payload.PreviousSetRevision.CorrelationID}); err != nil {
		return HypothesisSupersededPayload{}, err
	}
	return payload, nil
}

func decodeHypothesisResolvedPayload(data []byte) (HypothesisResolvedPayload, error) {
	var payload HypothesisResolvedPayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return HypothesisResolvedPayload{}, fmt.Errorf("%w: hypothesis.resolved: %v", ErrInvalidPayload, err)
	}
	if err := validateHypothesisResolvedInput(HypothesisResolvedInput{SetID: payload.SetID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision, PreviousStatus: payload.PreviousStatus, NewStatus: payload.NewStatus, AssessmentVersion: payload.AssessmentVersion, AssessmentID: payload.AssessmentID, AssessmentFingerprint: payload.AssessmentFingerprint, AlternativeID: payload.AlternativeID, AlternativeKind: payload.AlternativeKind, Effect: payload.Effect, EffectFingerprint: payload.EffectFingerprint, Outcome: payload.Outcome, HypothesisRevision: payload.HypothesisRevision, ChainDelta: payload.ChainDelta, RecordedAt: payload.HypothesisRevision.At, Actor: payload.HypothesisRevision.Actor, CorrelationID: payload.HypothesisRevision.CorrelationID}); err != nil {
		return HypothesisResolvedPayload{}, err
	}
	return payload, nil
}

func applyResolutionChainState(delta ResolutionChainDelta, revisions map[chains.ChainID]uint64, statuses map[chains.ChainID]chains.Status, observations map[chains.ChainID]map[string]struct{}, contributions map[chains.ChainID]map[string]struct{}, confidence map[chains.ChainID]float64, support map[chains.ChainID]uint64, contradiction map[chains.ChainID]uint64, revisionAt map[chains.ChainID]time.Time) error {
	switch delta.Kind {
	case hypotheses.ResolutionEffectNoChain:
		return nil
	case hypotheses.ResolutionEffectCreateCandidate:
		if delta.ChainAdded == nil {
			return fmt.Errorf("%w: missing chain delta", ErrInvalidPayload)
		}
		p := delta.ChainAdded.Chain
		if _, exists := revisions[p.ID]; exists {
			return fmt.Errorf("%w: chain collision", ErrInvalidPayload)
		}
		revisions[p.ID] = p.Revision
		statuses[p.ID] = p.Status
		observations[p.ID] = make(map[string]struct{}, len(p.Observations))
		contributions[p.ID] = make(map[string]struct{}, len(p.Contributions))
		for _, o := range p.Observations {
			observations[p.ID][o.ID] = struct{}{}
		}
		for _, c := range p.Contributions {
			contributions[p.ID][c.ID] = struct{}{}
		}
		confidence[p.ID] = p.CurrentConfidence
		support[p.ID] = p.ConfirmationCount
		contradiction[p.ID] = p.ContradictionCount
		if len(p.History) > 0 {
			revisionAt[p.ID] = p.History[len(p.History)-1].At
		}
		return nil
	case hypotheses.ResolutionEffectAttachObservation:
		p := delta.ObservationAdded
		if p == nil {
			return fmt.Errorf("%w: missing observation delta", ErrInvalidPayload)
		}
		current, ok := revisions[p.ChainID]
		if !ok || current != p.PreviousRevision || p.NewRevision != current+1 {
			return fmt.Errorf("%w: observation revision mismatch", ErrInvalidPayload)
		}
		if _, exists := observations[p.ChainID][p.Observation.ID]; exists {
			return fmt.Errorf("%w: observation collision", ErrInvalidPayload)
		}
		observations[p.ChainID][p.Observation.ID] = struct{}{}
		revisions[p.ChainID] = p.NewRevision
		revisionAt[p.ChainID] = p.Revision.At
		return nil
	case hypotheses.ResolutionEffectAddContribution:
		p := delta.ContributionAdded
		if p == nil {
			return fmt.Errorf("%w: missing contribution delta", ErrInvalidPayload)
		}
		current, ok := revisions[p.ChainID]
		if !ok || current != p.PreviousRevision || p.NewRevision != current+1 {
			return fmt.Errorf("%w: contribution revision mismatch", ErrInvalidPayload)
		}
		if _, exists := contributions[p.ChainID][p.Contribution.ID]; exists {
			return fmt.Errorf("%w: contribution collision", ErrInvalidPayload)
		}
		if confidence[p.ChainID] != p.PreviousConfidence || support[p.ChainID] != p.PreviousSupportCount || contradiction[p.ChainID] != p.PreviousContradictionCount {
			return fmt.Errorf("%w: contribution before state mismatch", ErrInvalidPayload)
		}
		contributions[p.ChainID][p.Contribution.ID] = struct{}{}
		confidence[p.ChainID] = p.NewConfidence
		support[p.ChainID] = p.NewSupportCount
		contradiction[p.ChainID] = p.NewContradictionCount
		revisions[p.ChainID] = p.NewRevision
		revisionAt[p.ChainID] = p.Revision.At
		return nil
	default:
		return fmt.Errorf("%w: unknown resolution kind", ErrInvalidPayload)
	}
}

func validateRevisionOperation(from, to chains.Status, operation chains.RevisionOperation) error {
	want := chains.OperationStatusChanged
	if to == chains.StatusArchived {
		want = chains.OperationChainArchived
	}
	if to == chains.StatusReactivated {
		want = chains.OperationChainReactivated
	}
	if operation != want {
		return fmt.Errorf("%w: operation=%s want=%s", ErrInvalidPayload, operation, want)
	}
	return nil
}

func readContextLimited(ctx context.Context, reader io.Reader, limit int64) ([]byte, error) {
	var data bytes.Buffer
	chunk := make([]byte, 64*1024)
	for {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		n, err := reader.Read(chunk)
		if n > 0 {
			if int64(data.Len()+n) > limit {
				return nil, fmt.Errorf("%w: size exceeds limit", ErrJournalTooLarge)
			}
			_, _ = data.Write(chunk[:n])
		}
		if err == io.EOF {
			return data.Bytes(), nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: read journal: %v", ErrInvalidRecord, err)
		}
	}
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidContext, err)
	}
	return nil
}
