package shadowworkflow

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

const (
	qualificationSamplesName  = "qualification.samples.ndjson"
	qualificationSummaryName  = "qualification.summary.json"
	qualificationManifestName = "qualification.manifest.json"
)

type QualificationManifest struct {
	SchemaVersion            string               `json:"schema_version"`
	RunID                    string               `json:"run_id"`
	Profile                  QualificationProfile `json:"profile"`
	StartedAt                time.Time            `json:"started_at"`
	ConfigurationFingerprint string               `json:"configuration_fingerprint"`
	SampleInterval           time.Duration        `json:"sample_interval"`
	MaxSamples               int                  `json:"max_samples"`
	MaxOutputBytes           int64                `json:"max_output_bytes"`
	OutputLimitReached       bool                 `json:"output_limit_reached"`
	MaxWALBytes              int64                `json:"max_wal_bytes"`
	System                   string               `json:"system"`
	GoVersion                string               `json:"go_version"`
	GitCommit                string               `json:"git_commit,omitempty"`
	Fingerprint              string               `json:"fingerprint"`
}

func QualificationManifestFingerprint(value QualificationManifest) string {
	copy := value
	copy.Fingerprint = ""
	encoded, _ := json.Marshal(copy)
	return qualificationDigest("shadow-qualification-manifest-v1:", encoded)
}

type qualificationStageAggregate struct {
	StageSample
	recent []uint64
}

type QualificationRecorder struct {
	mu                       sync.Mutex
	cfg                      QualificationConfig
	startedAt                time.Time
	file                     *os.File
	writer                   *bufio.Writer
	stop                     chan struct{}
	done                     chan struct{}
	sampleFn                 func() QualificationSample
	sampleSequence           uint64
	sampleCount              int
	outputBytes              int64
	outputLimited            bool
	closed                   bool
	queueHighWater           int
	lastWALBytes             int64
	recoveryDurationNS       uint64
	transactionCount         uint64
	transactionBytes         int64
	maximumTransactionBytes  int64
	lastCheckpointDurationNS uint64
	stages                   map[string]*qualificationStageAggregate
	trySubmitFailures        uint64
}

func OpenQualificationRecorder(cfg QualificationConfig, startedAt time.Time, sampleFn func() QualificationSample) (*QualificationRecorder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if sampleFn == nil {
		return nil, fmt.Errorf("%w: qualification sampler", ErrInvalidConfig)
	}
	if info, err := os.Lstat(cfg.OutputDirectory); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: qualification output symlink", ErrInvalidConfig)
	}
	if err := os.MkdirAll(cfg.OutputDirectory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(cfg.OutputDirectory, 0700); err != nil {
		return nil, err
	}
	for _, name := range []string{qualificationSamplesName, qualificationSummaryName, qualificationManifestName} {
		if info, statErr := os.Lstat(filepath.Join(cfg.OutputDirectory, name)); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: qualification output symlink", ErrInvalidConfig)
		}
	}
	file, err := os.OpenFile(filepath.Join(cfg.OutputDirectory, qualificationSamplesName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	recorder := &QualificationRecorder{cfg: cfg, startedAt: startedAt.UTC(), file: file, writer: bufio.NewWriter(file), stop: make(chan struct{}), done: make(chan struct{}), sampleFn: sampleFn, stages: map[string]*qualificationStageAggregate{}}
	manifest := QualificationManifest{SchemaVersion: "shadow-qualification-manifest-v1", RunID: cfg.RunID, Profile: cfg.Profile, StartedAt: startedAt.UTC(), ConfigurationFingerprint: cfg.Fingerprint(), SampleInterval: cfg.SampleInterval, MaxSamples: cfg.MaxSamples, MaxOutputBytes: cfg.MaxOutputBytes, MaxWALBytes: cfg.MaxWALBytes, System: "local", GoVersion: runtime.Version(), GitCommit: qualificationBuildCommit()}
	manifest.Fingerprint = QualificationManifestFingerprint(manifest)
	if err := writeQualificationJSONAtomic(filepath.Join(cfg.OutputDirectory, qualificationManifestName), manifest); err != nil {
		_ = file.Close()
		return nil, err
	}
	go recorder.loop()
	return recorder, nil
}

func qualificationBuildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

func (r *QualificationRecorder) loop() {
	ticker := time.NewTicker(r.cfg.SampleInterval)
	flushTicker := time.NewTicker(r.cfg.FlushInterval)
	defer ticker.Stop()
	defer flushTicker.Stop()
	defer close(r.done)
	for {
		select {
		case <-ticker.C:
			r.RecordSample()
		case <-flushTicker.C:
			_ = r.Flush()
		case <-r.stop:
			return
		}
	}
}

func (r *QualificationRecorder) RecordSample() {
	r.recordSample(false)
}

func (r *QualificationRecorder) recordSample(force bool) {
	if r == nil || r.sampleFn == nil {
		return
	}
	sample := r.sampleFn()
	r.mu.Lock()
	defer r.mu.Unlock()
	if (!force && r.closed) || r.sampleCount >= r.cfg.MaxSamples || r.outputLimited {
		return
	}
	r.sampleSequence++
	sample.SchemaVersion = qualificationSchemaVersion
	sample.RunID = r.cfg.RunID
	sample.Profile = r.cfg.Profile
	sample.SampleSequence = r.sampleSequence
	if sample.SampledAt.IsZero() {
		sample.SampledAt = time.Now().UTC()
	}
	sample.Uptime = sample.SampledAt.Sub(r.startedAt)
	if sample.Uptime < 0 {
		sample.Uptime = 0
	}
	sample.StageCounters = r.stageSnapshotLocked()
	sample.QueueHighWaterMark = r.queueHighWater
	sample.Storage.LastRecoveryDurationNS = r.recoveryDurationNS
	sample.Storage.LastCheckpointDurationNS = r.lastCheckpointDurationNS
	if sample.Storage.WALBytes > 0 && r.lastWALBytes > 0 {
		sample.Storage.WALGrowthBytes = sample.Storage.WALBytes - r.lastWALBytes
	}
	if sample.Storage.WALBytes > 0 {
		r.lastWALBytes = sample.Storage.WALBytes
	}
	if r.transactionCount > 0 {
		sample.Storage.AverageTransactionBytes = r.transactionBytes / int64(r.transactionCount)
	}
	sample.Storage.MaximumTransactionBytes = r.maximumTransactionBytes
	sample.Fingerprint = QualificationSampleFingerprint(sample)
	encoded, err := json.Marshal(sample)
	if err != nil {
		return
	}
	encoded = append(encoded, '\n')
	if r.outputBytes+int64(len(encoded)) > r.cfg.MaxOutputBytes {
		r.outputLimited = true
		return
	}
	if _, err := r.writer.Write(encoded); err != nil {
		r.outputLimited = true
		return
	}
	r.outputBytes += int64(len(encoded))
	r.sampleCount++
}

func (r *QualificationRecorder) ObserveQueue(depth int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if depth > r.queueHighWater {
		r.queueHighWater = depth
	}
	r.mu.Unlock()
}

func (r *QualificationRecorder) RecordStage(stage string, started time.Time, result error) {
	if r == nil || stage == "" || started.IsZero() {
		return
	}
	duration := time.Since(started)
	if duration < 0 {
		duration = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	aggregate := r.stages[stage]
	if aggregate == nil {
		aggregate = &qualificationStageAggregate{StageSample: StageSample{Stage: stage, MinDurationNS: ^uint64(0)}}
		r.stages[stage] = aggregate
	}
	nanos := uint64(duration)
	aggregate.Count++
	if result == nil {
		aggregate.Successes++
	} else {
		aggregate.Failures++
		if errors.Is(result, ErrPipelineTimeout) {
			aggregate.Timeouts++
		}
	}
	aggregate.TotalDurationNS += nanos
	if nanos < aggregate.MinDurationNS {
		aggregate.MinDurationNS = nanos
	}
	if nanos > aggregate.MaxDurationNS {
		aggregate.MaxDurationNS = nanos
	}
	aggregate.recent = append(aggregate.recent, nanos)
	if len(aggregate.recent) > 256 {
		aggregate.recent = aggregate.recent[len(aggregate.recent)-256:]
	}
}

func (r *QualificationRecorder) RecordTransactionBytes(value int64) {
	if r == nil || value <= 0 {
		return
	}
	r.mu.Lock()
	r.transactionCount++
	r.transactionBytes += value
	if value > r.maximumTransactionBytes {
		r.maximumTransactionBytes = value
	}
	r.mu.Unlock()
}

func (r *QualificationRecorder) RecordCheckpoint(duration time.Duration) {
	if r == nil || duration <= 0 {
		return
	}
	r.mu.Lock()
	r.lastCheckpointDurationNS = uint64(duration)
	r.mu.Unlock()
}

func (r *QualificationRecorder) SetRecoveryDuration(duration time.Duration) {
	if r == nil || duration < 0 {
		return
	}
	r.mu.Lock()
	r.recoveryDurationNS = uint64(duration)
	r.mu.Unlock()
}

func (r *QualificationRecorder) RecordTrySubmitFailure() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.trySubmitFailures++
	r.mu.Unlock()
}

func (r *QualificationRecorder) TrySubmitFailures() uint64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.trySubmitFailures
}

func (r *QualificationRecorder) stageSnapshotLocked() []StageSample {
	out := make([]StageSample, 0, len(r.stages))
	for _, aggregate := range r.stages {
		value := aggregate.StageSample
		if value.MinDurationNS == ^uint64(0) {
			value.MinDurationNS = 0
		}
		recent := append([]uint64(nil), aggregate.recent...)
		sort.Slice(recent, func(i, j int) bool { return recent[i] < recent[j] })
		value.RecentP50NS = qualificationQuantile(recent, 0.50)
		value.RecentP95NS = qualificationQuantile(recent, 0.95)
		value.RecentP99NS = qualificationQuantile(recent, 0.99)
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stage < out[j].Stage })
	return out
}

func (r *QualificationRecorder) StageSnapshot() []StageSample {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stageSnapshotLocked()
}

func qualificationQuantile(values []uint64, fraction float64) uint64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * fraction)
	return values[index]
}

func (r *QualificationRecorder) Flush() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	if err := r.writer.Flush(); err != nil {
		return err
	}
	return r.file.Sync()
}

func (r *QualificationRecorder) Close(finalSample func() QualificationSample) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	close(r.stop)
	<-r.done
	if finalSample != nil {
		r.sampleFn = finalSample
		r.recordSample(true)
	}
	flushErr := r.Flush()
	r.mu.Lock()
	file := r.file
	r.file = nil
	r.writer = nil
	r.mu.Unlock()
	if file != nil {
		if err := file.Close(); flushErr == nil {
			flushErr = err
		}
	}
	if reportErr := r.writeSummary(); flushErr == nil {
		flushErr = reportErr
	}
	return flushErr
}

func (r *QualificationRecorder) writeSummary() error {
	samples, invalid, err := ReadQualificationSamples(filepath.Join(r.cfg.OutputDirectory, qualificationSamplesName), r.cfg.MaxSamples)
	if err != nil {
		return err
	}
	manifest, err := ReadQualificationManifest(filepath.Join(r.cfg.OutputDirectory, qualificationManifestName))
	if err != nil {
		return err
	}
	r.mu.Lock()
	manifest.OutputLimitReached = r.outputLimited
	r.mu.Unlock()
	manifest.Fingerprint = QualificationManifestFingerprint(manifest)
	if err := writeQualificationJSONAtomic(filepath.Join(r.cfg.OutputDirectory, qualificationManifestName), manifest); err != nil {
		return err
	}
	report := BuildQualificationReport(samples, invalid, manifest, r.cfg.Thresholds)
	return writeQualificationJSONAtomic(filepath.Join(r.cfg.OutputDirectory, qualificationSummaryName), report)
}

func writeQualificationJSONAtomic(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".qualification-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = directoryFile.Sync()
	_ = directoryFile.Close()
	return err
}
