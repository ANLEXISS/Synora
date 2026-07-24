package fieldtrial

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/internal/cge/contractcatalog"
)

type Stats struct {
	RecordAttempted  uint64
	RecordWritten    uint64
	RecordErrors     uint64
	RecordPanics     uint64
	SegmentsCreated  uint64
	Rotations        uint64
	Recoveries       uint64
	RecoveryErrors   uint64
	EventsDropped    uint64
	SyncErrors       uint64
	AnnotationsAdded uint64
	AnnotationErrors uint64
	EventsBytes      int64
	AnnotationBytes  int64
}

type Recorder struct {
	mu sync.RWMutex

	config     Config
	manifest   SessionManifest
	sessionDir string
	pseudo     *Pseudonymizer

	segmentFile  *os.File
	segmentIndex int
	segmentBytes int64
	headHash     string
	nextSequence uint64
	eventRefs    map[string]struct{}
	eventKeys    map[string]TrialEvent

	annotationFile     *os.File
	annotationSequence uint64
	annotationHash     string

	status SessionStatus
	stats  Stats
	closed bool
}

func Open(ctx context.Context, config Config, metadata OpenMetadata) (*Recorder, error) {
	return OpenAt(ctx, config, metadata, time.Now().UTC(), nil)
}

func OpenWithKey(ctx context.Context, config Config, metadata OpenMetadata, createdAt time.Time, key []byte) (*Recorder, error) {
	return OpenAt(ctx, config, metadata, createdAt, key)
}

func OpenAt(ctx context.Context, config Config, metadata OpenMetadata, createdAt time.Time, injectedKey []byte) (*Recorder, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, nil
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if createdAt.IsZero() {
		return nil, ErrInvalidConfig
	}
	if config.SessionID == "" {
		randomValue := make([]byte, 8)
		if _, err := rand.Read(randomValue); err != nil {
			return nil, err
		}
		config.SessionID = sessionID(createdAt, hex.EncodeToString(randomValue))
	}
	if !validSessionID(config.SessionID) {
		return nil, ErrInvalidSessionID
	}
	if err := os.MkdirAll(config.RootDir, 0o750); err != nil {
		return nil, err
	}
	sessionDir := filepath.Join(config.RootDir, config.SessionID)
	if info, statErr := os.Lstat(sessionDir); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: session directory symlink", ErrTelemetryCorrupt)
	}
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		return nil, err
	}
	recorder := &Recorder{config: config, sessionDir: sessionDir, status: SessionOpen, eventRefs: map[string]struct{}{}, eventKeys: map[string]TrialEvent{}}
	manifestPath := filepath.Join(sessionDir, "manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		if err := json.Unmarshal(data, &recorder.manifest); err != nil {
			return nil, fmt.Errorf("%w: manifest", ErrTelemetryCorrupt)
		}
		if recorder.manifest.SessionID != config.SessionID || recorder.manifest.Status == SessionClosed {
			return nil, ErrSessionClosed
		}
		if metadata.CognitiveConfigurationFingerprint != "" && recorder.manifest.CognitiveConfigurationFingerprint != "" && metadata.CognitiveConfigurationFingerprint != recorder.manifest.CognitiveConfigurationFingerprint {
			return nil, ErrConfigurationDrift
		}
		recorder.status = SessionRecovered
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		recorder.manifest = SessionManifest{SchemaVersion: SchemaVersion, SessionID: config.SessionID, CreatedAt: createdAt.UTC(), HostArchitecture: runtime.GOARCH, CGEConfiguration: metadata.CGEConfiguration, PolicyVersions: metadata.PolicyVersions, CognitiveConfigurationFingerprint: metadata.CognitiveConfigurationFingerprint, ContextSchemaVersion: metadata.CGEConfiguration.ContextSchemaVersion, RoutinePolicyVersion: metadata.CGEConfiguration.RoutinePolicyVersion, DeviationPolicyVersion: metadata.CGEConfiguration.DeviationPolicy, Status: SessionOpen}
	}
	if metadata.CGEConfiguration.ContextSchemaVersion != "" || metadata.CGEConfiguration.RoutinePolicyVersion != "" || metadata.CGEConfiguration.DeviationPolicy != "" || metadata.PolicyVersions.Association != "" {
		recorder.manifest.CGEConfiguration, recorder.manifest.PolicyVersions = metadata.CGEConfiguration, metadata.PolicyVersions
	}
	if metadata.CognitiveConfigurationFingerprint != "" {
		recorder.manifest.CognitiveConfigurationFingerprint = metadata.CognitiveConfigurationFingerprint
	}
	key, _, err := loadOrCreateKey(config, sessionDir, injectedKey)
	if err != nil {
		return nil, err
	}
	recorder.pseudo, err = NewPseudonymizer(config.SessionID, key)
	if err != nil {
		return nil, err
	}
	if err := recorder.recoverSegmentsLocked(config.RepairTerminalPartial); err != nil {
		recorder.stats.RecoveryErrors++
		return nil, err
	}
	annotationPath := filepath.Join(sessionDir, "annotations.ndjson")
	var annotationCount uint64
	recorder.annotationFile, recorder.annotationSequence, recorder.annotationHash, annotationCount, err = openAnnotations(annotationPath)
	if err != nil {
		return nil, err
	}
	recorder.manifest.AnnotationCount = annotationCount
	if recorder.status == SessionRecovered {
		recorder.stats.Recoveries++
	}
	recorder.manifest.Status = recorder.status
	if err := recorder.openCurrentSegmentLocked(); err != nil {
		recorder.closeFilesLocked()
		return nil, err
	}
	recorder.manifest.SegmentCount = recorder.segmentIndex
	if err := recorder.writeManifestLocked(); err != nil {
		recorder.closeFilesLocked()
		return nil, err
	}
	return recorder, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (r *Recorder) recoverSegmentsLocked(repair bool) error {
	entries, err := os.ReadDir(r.sessionDir)
	if err != nil {
		return err
	}
	indices := make([]int, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "events-") && strings.HasSuffix(name, ".ndjson") {
			var index int
			if _, scanErr := fmt.Sscanf(name, "events-%06d.ndjson", &index); scanErr != nil || index <= 0 {
				return ErrTelemetryCorrupt
			}
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	if len(indices) == 0 {
		r.segmentIndex = 1
		r.nextSequence = 1
		r.manifest.SegmentCount = 1
		return nil
	}
	r.manifest.EventCount = 0
	r.eventRefs = map[string]struct{}{}
	r.eventKeys = map[string]TrialEvent{}
	r.nextSequence = 1
	r.headHash = ""
	r.stats.EventsBytes = 0
	for index, value := range indices {
		if value != index+1 {
			return fmt.Errorf("%w: missing segment", ErrTelemetryCorrupt)
		}
		state, err := verifyEventSegment(filepath.Join(r.sessionDir, segmentName(value)), r.nextSequence, r.headHash, repair && index == len(indices)-1, r.manifest.SessionID)
		if err != nil {
			return err
		}
		r.nextSequence = state.Sequence + 1
		r.headHash = state.Hash
		r.manifest.EventCount += state.Events
		for ref := range state.Refs {
			r.eventRefs[ref] = struct{}{}
		}
		for _, event := range state.Values {
			r.eventKeys[eventIdentity(event)] = event
		}
		r.manifest.LastSequence = state.Sequence
		r.manifest.LastSegmentHash = state.Hash
		r.stats.EventsBytes += state.Bytes
		r.segmentIndex = value
	}
	r.manifest.SegmentCount = len(indices)
	return nil
}

func (r *Recorder) openCurrentSegmentLocked() error {
	path := filepath.Join(r.sessionDir, segmentName(r.segmentIndex))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o640)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	r.segmentFile, r.segmentBytes = file, info.Size()
	return nil
}

func (r *Recorder) rotateLocked() error {
	if r.segmentFile == nil {
		return r.openCurrentSegmentLocked()
	}
	if err := r.segmentFile.Sync(); err != nil {
		r.stats.SyncErrors++
		return err
	}
	if err := r.segmentFile.Close(); err != nil {
		return err
	}
	r.segmentIndex++
	r.segmentBytes = 0
	r.stats.Rotations++
	r.stats.SegmentsCreated++
	r.manifest.SegmentCount = r.segmentIndex
	return r.openCurrentSegmentLocked()
}

func (r *Recorder) markDegradedLocked() { r.status = SessionDegraded }

func (r *Recorder) Record(ctx context.Context, input EventInput) (TrialEvent, error) {
	if err := contextErr(ctx); err != nil {
		return TrialEvent{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.RecordAttempted++
	if r.closed || r.status == SessionClosed {
		return TrialEvent{}, ErrSessionClosed
	}
	if r.status == SessionDegraded {
		r.stats.RecordErrors++
		return TrialEvent{}, ErrSessionDegraded
	}
	if input.EventID == "" || input.ObservedAt.IsZero() {
		r.stats.RecordErrors++
		return TrialEvent{}, ErrInvalidConfig
	}
	if input.RecordedAt.IsZero() {
		input.RecordedAt = input.ObservedAt
	}
	if input.AttemptNumber == 0 {
		input.AttemptNumber = 1
	}
	event := buildTrialEvent(input, r.manifest.SessionID, r.nextSequence, r.pseudo, r.config.IncludeContextCategories, r.config.IncludeLatencyBreakdown)
	if previous, ok := r.eventKeys[eventIdentity(event)]; ok {
		return previous, nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		r.stats.RecordErrors++
		return TrialEvent{}, err
	}
	payloadHash := hashBytes(payload)
	envelope := Envelope{Sequence: r.nextSequence, PreviousHash: r.headHash, PayloadSHA256: payloadHash, Payload: event}
	envelope.RecordHash = recordHash(envelope.Sequence, envelope.PreviousHash, envelope.PayloadSHA256)
	data, err := encodeEnvelope(envelope)
	if err != nil {
		r.stats.RecordErrors++
		return TrialEvent{}, err
	}
	if r.config.MaximumTotalBytes > 0 && r.totalBytesLocked()+int64(len(data)) > r.config.MaximumTotalBytes {
		r.markDegradedLocked()
		r.stats.EventsDropped++
		r.stats.RecordErrors++
		return TrialEvent{}, ErrQuotaExceeded
	}
	if r.segmentBytes > 0 && r.segmentBytes+int64(len(data)) > r.config.SegmentMaxBytes {
		if err := r.rotateLocked(); err != nil {
			r.markDegradedLocked()
			r.stats.RecordErrors++
			return TrialEvent{}, err
		}
	}
	if _, err := r.segmentFile.Write(data); err != nil {
		r.markDegradedLocked()
		r.stats.RecordErrors++
		return TrialEvent{}, err
	}
	if r.config.SyncEachEvent {
		if err := r.segmentFile.Sync(); err != nil {
			r.markDegradedLocked()
			r.stats.SyncErrors++
			r.stats.RecordErrors++
			return TrialEvent{}, err
		}
	}
	r.segmentBytes += int64(len(data))
	r.stats.EventsBytes += int64(len(data))
	r.stats.RecordWritten++
	r.headHash, r.nextSequence = envelope.RecordHash, r.nextSequence+1
	r.manifest.EventCount++
	if r.manifest.FirstSequence == 0 {
		r.manifest.FirstSequence = envelope.Sequence
	}
	r.manifest.LastSequence, r.manifest.LastSegmentHash = envelope.Sequence, envelope.RecordHash
	r.eventRefs[event.EventRef] = struct{}{}
	r.eventKeys[eventIdentity(event)] = event
	return event, nil
}

func eventIdentity(event TrialEvent) string {
	return event.EventRef + "|" + fmt.Sprintf("%d", event.CognitiveWALSequence) + "|" + event.DeviationFingerprint
}

func (r *Recorder) AddAnnotation(ctx context.Context, input AnnotationInput) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.addAnnotationLocked(input); err != nil {
		r.stats.AnnotationErrors++
		return err
	}
	return nil
}

func (r *Recorder) Checkpoint(ctx context.Context, at time.Time) (SessionManifest, error) {
	if err := contextErr(ctx); err != nil {
		return SessionManifest{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return SessionManifest{}, ErrSessionClosed
	}
	if r.status == SessionDegraded {
		return SessionManifest{}, ErrSessionDegraded
	}
	if at.IsZero() {
		return SessionManifest{}, ErrInvalidConfig
	}
	if err := r.syncLocked(); err != nil {
		r.markDegradedLocked()
		return SessionManifest{}, err
	}
	if err := r.writeManifestLocked(); err != nil {
		r.markDegradedLocked()
		return SessionManifest{}, err
	}
	return cloneManifest(r.manifest), nil
}

// Shutdown flushes and closes files while keeping an open session recoverable.
// It is intended for short-lived administrative commands.
func (r *Recorder) Shutdown(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if err := r.syncLocked(); err != nil {
		r.markDegradedLocked()
		return err
	}
	if err := r.writeManifestLocked(); err != nil {
		r.markDegradedLocked()
		return err
	}
	return r.closeFilesLocked()
}

func (r *Recorder) Close(ctx context.Context, closedAt time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var first error
	if err := r.syncLocked(); err != nil {
		first = err
	}
	if err := r.closeFilesLocked(); err != nil && first == nil {
		first = err
	}
	if closedAt.IsZero() {
		closedAt = time.Now().UTC()
	}
	r.manifest.ClosedAt = ptrTime(closedAt.UTC())
	if first == nil {
		r.status = SessionClosed
	} else {
		r.status = SessionDegraded
	}
	r.manifest.Status = r.status
	if err := r.writeManifestLocked(); err != nil && first == nil {
		first = err
	}
	return first
}

func (r *Recorder) syncLocked() error {
	if r.segmentFile != nil {
		if err := r.segmentFile.Sync(); err != nil {
			r.stats.SyncErrors++
			return err
		}
	}
	if r.annotationFile != nil {
		if err := r.annotationFile.Sync(); err != nil {
			r.stats.SyncErrors++
			return err
		}
	}
	return nil
}

func (r *Recorder) closeFilesLocked() error {
	var first error
	if r.segmentFile != nil {
		if err := r.segmentFile.Close(); err != nil {
			first = err
		}
		r.segmentFile = nil
	}
	if r.annotationFile != nil {
		if err := r.annotationFile.Close(); err != nil && first == nil {
			first = err
		}
		r.annotationFile = nil
	}
	return first
}

func (r *Recorder) writeManifestLocked() error {
	path := filepath.Join(r.sessionDir, "manifest.json")
	if err := contractcatalog.ValidateStoreWrite("synora.store.field-trial-recorder", "synora.cge.field-trial-record.v1", r.manifest); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.manifest, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(r.sessionDir, ".manifest-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (r *Recorder) totalBytesLocked() int64 {
	return r.stats.EventsBytes + r.stats.AnnotationBytes
}

func (r *Recorder) Stats() Stats { r.mu.RLock(); defer r.mu.RUnlock(); return r.stats }
func (r *Recorder) Manifest() SessionManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneManifest(r.manifest)
}
func (r *Recorder) SessionDir() string    { r.mu.RLock(); defer r.mu.RUnlock(); return r.sessionDir }
func (r *Recorder) Status() SessionStatus { r.mu.RLock(); defer r.mu.RUnlock(); return r.status }

func cloneManifest(value SessionManifest) SessionManifest {
	if value.ClosedAt != nil {
		closed := *value.ClosedAt
		value.ClosedAt = &closed
	}
	return value
}
func ptrTime(value time.Time) *time.Time { return &value }
func splitLines(data []byte) [][]byte    { return bytesSplit(data) }

func bytesSplit(data []byte) [][]byte {
	return stringsSplitBytes(data, '\n')
}

func stringsSplitBytes(data []byte, separator byte) [][]byte {
	var result [][]byte
	start := 0
	for index, value := range data {
		if value == separator {
			result = append(result, data[start:index])
			start = index + 1
		}
	}
	if start < len(data) {
		result = append(result, data[start:])
	}
	return result
}
