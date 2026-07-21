package shadowworkflow

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

type QualificationStatus string

const (
	QualificationPass       QualificationStatus = "pass"
	QualificationWarning    QualificationStatus = "warning"
	QualificationFail       QualificationStatus = "fail"
	QualificationIncomplete QualificationStatus = "incomplete"
)

type EventRateReport struct {
	DurationSeconds    float64 `json:"duration_seconds"`
	ReceivedPerSecond  float64 `json:"received_per_second"`
	AcceptedPerSecond  float64 `json:"accepted_per_second"`
	CommittedPerSecond float64 `json:"committed_per_second"`
}

type QueueReport struct {
	Capacity          int    `json:"capacity"`
	MaximumDepth      int    `json:"maximum_depth"`
	DropRatioPermille int    `json:"drop_ratio_permille"`
	Dropped           uint64 `json:"dropped"`
}

type StageReport struct {
	Stage     string `json:"stage"`
	Count     uint64 `json:"count"`
	Successes uint64 `json:"successes"`
	Failures  uint64 `json:"failures"`
	Timeouts  uint64 `json:"timeouts"`
	P50NS     uint64 `json:"p50_ns"`
	P95NS     uint64 `json:"p95_ns"`
	P99NS     uint64 `json:"p99_ns"`
}

type ProcessReport struct {
	RSSMinimumBytes          int64   `json:"rss_minimum_bytes"`
	RSSMaximumBytes          int64   `json:"rss_maximum_bytes"`
	RSSMedianBytes           int64   `json:"rss_median_bytes"`
	RSSP95Bytes              int64   `json:"rss_p95_bytes"`
	RSSGrowthBytesPerHour    int64   `json:"rss_growth_bytes_per_hour"`
	HeapAllocMedian          uint64  `json:"heap_alloc_median"`
	HeapAllocP95             uint64  `json:"heap_alloc_p95"`
	HeapObjectsGrowthPerHour int64   `json:"heap_objects_growth_per_hour"`
	GoroutineGrowthPerHour   int     `json:"goroutine_growth_per_hour"`
	NumGCPerHour             float64 `json:"num_gc_per_hour"`
	GCPauseP95NS             uint64  `json:"gc_pause_p95_ns"`
	CPUAverageNSPerSecond    int64   `json:"cpu_average_ns_per_second"`
	CPUP95NSPerSecond        int64   `json:"cpu_p95_ns_per_second"`
	CPUTimePerCycleNS        int64   `json:"cpu_time_per_cycle_ns"`
}

type StorageReport struct {
	StoreMode               string  `json:"store_mode"`
	WALStartBytes           int64   `json:"wal_start_bytes"`
	WALEndBytes             int64   `json:"wal_end_bytes"`
	WALGrowthBytesPerHour   int64   `json:"wal_growth_bytes_per_hour"`
	EstimatedHoursToLimit   float64 `json:"estimated_hours_to_limit"`
	CheckpointCount         uint64  `json:"checkpoint_count"`
	CheckpointFailures      uint64  `json:"checkpoint_failures"`
	AverageTransactionBytes int64   `json:"average_transaction_bytes"`
	MaximumTransactionBytes int64   `json:"maximum_transaction_bytes"`
	LastRecoveryDurationNS  uint64  `json:"last_recovery_duration_ns"`
}

type RecoveryReport struct {
	DurationNS     uint64 `json:"duration_ns"`
	DigestMismatch uint64 `json:"digest_mismatch"`
}

type HistoricalIsolationReport struct {
	DecisionComparisons                uint64 `json:"decision_comparisons"`
	DecisionMismatches                 uint64 `json:"decision_mismatches"`
	HistoricalFingerprintComparisons   uint64 `json:"historical_fingerprint_comparisons"`
	HistoricalFingerprintMismatches    uint64 `json:"historical_fingerprint_mismatches"`
	TrySubmitFailuresObserved          uint64 `json:"try_submit_failures_observed"`
	TrySubmitFailuresAffectingDecision uint64 `json:"try_submit_failures_affecting_decision"`
}

type QualificationGateResult struct {
	Name       string              `json:"name"`
	Status     QualificationStatus `json:"status"`
	Value      string              `json:"value"`
	Threshold  string              `json:"threshold,omitempty"`
	Critical   bool                `json:"critical"`
	ReasonCode string              `json:"reason_code"`
}

type QualificationReport struct {
	SchemaVersion               string                    `json:"schema_version"`
	RunID                       string                    `json:"run_id"`
	Profile                     QualificationProfile      `json:"profile"`
	StartedAt                   time.Time                 `json:"started_at"`
	EndedAt                     time.Time                 `json:"ended_at"`
	Duration                    time.Duration             `json:"duration"`
	SamplesRead                 int                       `json:"samples_read"`
	SamplesInvalid              int                       `json:"samples_invalid"`
	OutputLimitReached          bool                      `json:"output_limit_reached"`
	EventRates                  EventRateReport           `json:"event_rates"`
	Queue                       QueueReport               `json:"queue"`
	Stages                      []StageReport             `json:"stages"`
	Process                     ProcessReport             `json:"process"`
	Storage                     StorageReport             `json:"storage"`
	Recovery                    RecoveryReport            `json:"recovery"`
	HistoricalIsolation         HistoricalIsolationReport `json:"historical_isolation"`
	GateResults                 []QualificationGateResult `json:"gate_results"`
	OverallStatus               QualificationStatus       `json:"overall_status"`
	PhysicalDeploymentPerformed bool                      `json:"physical_deployment_performed"`
	MultiDayStabilityValidated  bool                      `json:"multi_day_stability_validated"`
	Limitations                 []string                  `json:"limitations"`
	Fingerprint                 string                    `json:"fingerprint"`
}

func QualificationReportExitCode(status QualificationStatus) int {
	switch status {
	case QualificationPass:
		return 0
	case QualificationWarning:
		return 1
	case QualificationFail:
		return 2
	default:
		return 3
	}
}

func ReadQualificationSamples(path string, maxSamples int) ([]QualificationSample, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	values := make([]QualificationSample, 0)
	invalid := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if readErr == io.EOF {
				invalid++
				break
			}
			var sample QualificationSample
			if json.Unmarshal(line, &sample) != nil || !validQualificationSample(sample) {
				invalid++
			} else if maxSamples <= 0 || len(values) < maxSamples {
				values = append(values, sample)
			} else {
				invalid++
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return values, invalid, readErr
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].SampleSequence < values[j].SampleSequence })
	return values, invalid, nil
}

func ReadQualificationManifest(path string) (QualificationManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return QualificationManifest{}, err
	}
	var manifest QualificationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return QualificationManifest{}, err
	}
	if manifest.Fingerprint == "" || manifest.Fingerprint != QualificationManifestFingerprint(manifest) {
		return QualificationManifest{}, fmt.Errorf("%w: qualification manifest", ErrQualificationInvalidSample)
	}
	return manifest, nil
}

func BuildQualificationReport(samples []QualificationSample, invalid int, manifest QualificationManifest, thresholds QualificationThresholds) QualificationReport {
	report := QualificationReport{SchemaVersion: "shadow-qualification-report-v1", RunID: manifest.RunID, Profile: manifest.Profile, StartedAt: manifest.StartedAt, SamplesRead: len(samples), SamplesInvalid: invalid, OutputLimitReached: manifest.OutputLimitReached, PhysicalDeploymentPerformed: false, MultiDayStabilityValidated: false, Limitations: []string{"Physical deployment was not performed by this software qualification."}}
	if manifest.OutputLimitReached {
		report.Limitations = append(report.Limitations, "Qualification output limit was reached; the workflow remained active.")
	}
	if len(samples) == 0 {
		report.OverallStatus = QualificationIncomplete
		report.GateResults = []QualificationGateResult{{Name: "samples_present", Status: QualificationIncomplete, Critical: true, ReasonCode: "qualification.no_samples"}}
		report.Fingerprint = QualificationReportFingerprint(report)
		return report
	}
	last := samples[len(samples)-1]
	report.EndedAt = last.SampledAt
	report.Duration = last.SampledAt.Sub(manifest.StartedAt)
	if report.Duration < 0 {
		report.Duration = 0
	}
	seconds := report.Duration.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	first := samples[0]
	report.EventRates = EventRateReport{DurationSeconds: seconds, ReceivedPerSecond: float64(last.Received-first.Received) / seconds, AcceptedPerSecond: float64(last.Accepted-first.Accepted) / seconds, CommittedPerSecond: float64(last.CommitsSucceeded-first.CommitsSucceeded) / seconds}
	report.Queue = QueueReport{Capacity: last.QueueCapacity, MaximumDepth: last.QueueHighWaterMark, Dropped: last.DroppedQueueFull}
	if last.Received > 0 {
		report.Queue.DropRatioPermille = int(last.DroppedQueueFull * 1000 / last.Received)
	}
	report.Stages = stageReports(last.StageCounters)
	report.Process = processReport(samples, report.Duration, thresholds.WarmupDuration)
	report.Storage = storageReport(samples, report.Duration, manifest.MaxWALBytes)
	report.Recovery = RecoveryReport{DurationNS: last.Storage.LastRecoveryDurationNS}
	report.HistoricalIsolation = historicalReport(last.HistoricalIsolation)
	report.GateResults = qualificationGates(samples, report, thresholds)
	report.OverallStatus = qualificationOverallStatus(report.GateResults, invalid)
	if manifest.OutputLimitReached && report.OverallStatus == QualificationPass {
		report.OverallStatus = QualificationIncomplete
	}
	report.Fingerprint = QualificationReportFingerprint(report)
	return report
}

func QualificationReportFingerprint(report QualificationReport) string {
	copy := report
	copy.Fingerprint = ""
	encoded, _ := json.Marshal(copy)
	return qualificationDigest("shadow-qualification-report-v1:", encoded)
}

func qualificationDigest(prefix string, data []byte) string {
	digest := sha256.Sum256(data)
	return prefix + hex.EncodeToString(digest[:])
}

func stageReports(values []StageSample) []StageReport {
	out := make([]StageReport, 0, len(values))
	for _, value := range values {
		out = append(out, StageReport{Stage: value.Stage, Count: value.Count, Successes: value.Successes, Failures: value.Failures, Timeouts: value.Timeouts, P50NS: value.RecentP50NS, P95NS: value.RecentP95NS, P99NS: value.RecentP99NS})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stage < out[j].Stage })
	return out
}

func historicalReport(value HistoricalIsolationSample) HistoricalIsolationReport {
	return HistoricalIsolationReport{DecisionComparisons: value.DecisionComparisons, DecisionMismatches: value.DecisionMismatches, HistoricalFingerprintComparisons: value.HistoricalFingerprintComparisons, HistoricalFingerprintMismatches: value.HistoricalFingerprintMismatches, TrySubmitFailuresObserved: value.TrySubmitFailuresObserved, TrySubmitFailuresAffectingDecision: value.TrySubmitFailuresAffectingDecision}
}

func qualificationOverallStatus(gates []QualificationGateResult, invalid int) QualificationStatus {
	if invalid > 0 {
		return QualificationIncomplete
	}
	status := QualificationPass
	for _, gate := range gates {
		if gate.Status == QualificationFail {
			return QualificationFail
		}
		if gate.Status == QualificationWarning {
			status = QualificationWarning
		}
	}
	return status
}

func qualificationGates(samples []QualificationSample, report QualificationReport, thresholds QualificationThresholds) []QualificationGateResult {
	last := samples[len(samples)-1]
	gates := []QualificationGateResult{
		{Name: "historical_decision_mismatch", Status: gateStatus(last.HistoricalIsolation.DecisionMismatches == 0), Value: fmt.Sprint(last.HistoricalIsolation.DecisionMismatches), Threshold: "0", Critical: true, ReasonCode: "qualification.historical_decision_mismatch"},
		{Name: "historical_fingerprint_mismatch", Status: gateStatus(last.HistoricalIsolation.HistoricalFingerprintMismatches == 0), Value: fmt.Sprint(last.HistoricalIsolation.HistoricalFingerprintMismatches), Threshold: "0", Critical: true, ReasonCode: "qualification.historical_fingerprint_mismatch"},
		{Name: "try_submit_affecting_decision", Status: gateStatus(last.HistoricalIsolation.TrySubmitFailuresAffectingDecision == 0), Value: fmt.Sprint(last.HistoricalIsolation.TrySubmitFailuresAffectingDecision), Threshold: "0", Critical: true, ReasonCode: "qualification.try_submit_decision_isolation"},
		{Name: "invalid_lineage", Status: gateStatus(last.InvalidLineage == 0), Value: fmt.Sprint(last.InvalidLineage), Threshold: "0", Critical: true, ReasonCode: "qualification.invalid_lineage"},
		{Name: "durable_commit_failure", Status: gateStatus(last.CommitsFailed == 0), Value: fmt.Sprint(last.CommitsFailed), Threshold: "0", Critical: true, ReasonCode: "qualification.durable_commit_failure"},
		{Name: "recovery_digest_mismatch", Status: gateStatus(last.RecoveryDigestMismatches == 0), Value: fmt.Sprint(last.RecoveryDigestMismatches), Threshold: "0", Critical: true, ReasonCode: "qualification.recovery_digest_mismatch"},
		{Name: "queue_drop_ratio", Status: gateStatusWarning(report.Queue.DropRatioPermille <= thresholds.MaxQueueDropRatioPermille), Value: fmt.Sprint(report.Queue.DropRatioPermille), Threshold: fmt.Sprint(thresholds.MaxQueueDropRatioPermille), ReasonCode: "qualification.queue_drop_ratio"},
	}
	if last.CyclesSucceeded+last.CyclesFailed > 0 {
		timeoutRatio := last.CyclesTimedOut * 1000 / (last.CyclesSucceeded + last.CyclesFailed)
		gates = append(gates, QualificationGateResult{Name: "timeout_ratio", Status: gateStatusWarning(int(timeoutRatio) <= thresholds.MaxTimeoutRatioPermille), Value: fmt.Sprint(timeoutRatio), Threshold: fmt.Sprint(thresholds.MaxTimeoutRatioPermille), ReasonCode: "qualification.timeout_ratio"})
	}
	if stage := findStage(report.Stages, "try_submit"); stage != nil {
		gates = append(gates, QualificationGateResult{Name: "try_submit_p99", Status: gateStatusWarning(time.Duration(stage.P99NS) <= thresholds.MaxTrySubmitP99), Value: fmt.Sprint(stage.P99NS), Threshold: fmt.Sprint(thresholds.MaxTrySubmitP99.Nanoseconds()), ReasonCode: "qualification.try_submit_p99"})
	}
	if stage := findStage(report.Stages, "full_cycle"); stage != nil {
		gates = append(gates, QualificationGateResult{Name: "full_cycle_p99", Status: gateStatusWarning(time.Duration(stage.P99NS) <= thresholds.MaxAdvisoryCycleP99), Value: fmt.Sprint(stage.P99NS), Threshold: fmt.Sprint(thresholds.MaxAdvisoryCycleP99.Nanoseconds()), ReasonCode: "qualification.full_cycle_p99"})
	}
	gates = append(gates,
		QualificationGateResult{Name: "rss_growth", Status: gateStatusWarning(report.Process.RSSGrowthBytesPerHour <= thresholds.MaxRSSGrowthBytesPerHour), Value: fmt.Sprint(report.Process.RSSGrowthBytesPerHour), Threshold: fmt.Sprint(thresholds.MaxRSSGrowthBytesPerHour), ReasonCode: "qualification.rss_growth"},
		QualificationGateResult{Name: "goroutine_growth", Status: gateStatusWarning(report.Process.GoroutineGrowthPerHour <= thresholds.MaxGoroutineGrowthPerHour), Value: fmt.Sprint(report.Process.GoroutineGrowthPerHour), Threshold: fmt.Sprint(thresholds.MaxGoroutineGrowthPerHour), ReasonCode: "qualification.goroutine_growth"},
		QualificationGateResult{Name: "wal_limit", Status: gateStatusWarning(!last.Storage.StorageLimitReached), Value: fmt.Sprint(last.Storage.StorageLimitReached), Threshold: "false", ReasonCode: "qualification.wal_limit"},
	)
	return gates
}

func findStage(values []StageReport, name string) *StageReport {
	for i := range values {
		if values[i].Stage == name {
			return &values[i]
		}
	}
	return nil
}

func gateStatus(ok bool) QualificationStatus {
	if ok {
		return QualificationPass
	}
	return QualificationFail
}

func gateStatusWarning(ok bool) QualificationStatus {
	if ok {
		return QualificationPass
	}
	return QualificationWarning
}

func processReport(samples []QualificationSample, duration time.Duration, warmup time.Duration) ProcessReport {
	trendSamples := samples
	if warmup > 0 {
		filtered := make([]QualificationSample, 0, len(samples))
		for _, sample := range samples {
			if sample.Uptime >= warmup {
				filtered = append(filtered, sample)
			}
		}
		if len(filtered) >= 2 {
			trendSamples = filtered
		}
	}
	rss := make([]int64, 0, len(trendSamples))
	heap := make([]uint64, 0, len(trendSamples))
	objects := make([]uint64, 0, len(trendSamples))
	pauses := make([]uint64, 0, len(trendSamples))
	for _, sample := range samples {
		if sample.Process.RSSBytes > 0 {
			rss = append(rss, sample.Process.RSSBytes)
		}
		heap = append(heap, sample.Process.HeapAlloc)
		objects = append(objects, sample.Process.HeapObjects)
		if sample.Process.LastGCPauseNS > 0 {
			pauses = append(pauses, sample.Process.LastGCPauseNS)
		}
	}
	sort.Slice(rss, func(i, j int) bool { return rss[i] < rss[j] })
	sort.Slice(heap, func(i, j int) bool { return heap[i] < heap[j] })
	sort.Slice(objects, func(i, j int) bool { return objects[i] < objects[j] })
	sort.Slice(pauses, func(i, j int) bool { return pauses[i] < pauses[j] })
	result := ProcessReport{RSSMinimumBytes: firstInt64(rss), RSSMaximumBytes: lastInt64(rss), RSSMedianBytes: quantileInt64(rss, .5), RSSP95Bytes: quantileInt64(rss, .95), HeapAllocMedian: quantileUint64(heap, .5), HeapAllocP95: quantileUint64(heap, .95), GCPauseP95NS: quantileUint64(pauses, .95)}
	if len(trendSamples) >= 2 && duration > 0 {
		first, last := trendSamples[0], trendSamples[len(trendSamples)-1]
		result.RSSGrowthBytesPerHour = int64(float64(last.Process.RSSBytes-first.Process.RSSBytes) / duration.Hours())
		result.HeapObjectsGrowthPerHour = int64(float64(int64(last.Process.HeapObjects)-int64(first.Process.HeapObjects)) / duration.Hours())
		result.GoroutineGrowthPerHour = int(float64(signedDeltaInt(last.Process.Goroutines, first.Process.Goroutines)) / duration.Hours())
		result.NumGCPerHour = float64(signedDeltaUint32(last.Process.NumGC, first.Process.NumGC)) / duration.Hours()
		cpuRates := make([]int64, 0, len(trendSamples)-1)
		for index := 1; index < len(trendSamples); index++ {
			wall := trendSamples[index].SampledAt.Sub(trendSamples[index-1].SampledAt)
			if wall <= 0 {
				continue
			}
			currentCPU := trendSamples[index].Process.CPUUserNS + trendSamples[index].Process.CPUSystemNS
			previousCPU := trendSamples[index-1].Process.CPUUserNS + trendSamples[index-1].Process.CPUSystemNS
			cpu := signedDeltaInt64(currentCPU, previousCPU)
			if cpu < 0 {
				continue
			}
			cpuRates = append(cpuRates, int64(float64(cpu)*float64(time.Second)/float64(wall)))
		}
		sort.Slice(cpuRates, func(i, j int) bool { return cpuRates[i] < cpuRates[j] })
		if len(cpuRates) > 0 {
			result.CPUAverageNSPerSecond = sumInt64(cpuRates) / int64(len(cpuRates))
			result.CPUP95NSPerSecond = quantileInt64(cpuRates, .95)
		}
		commits := last.CommitsSucceeded - first.CommitsSucceeded
		if commits > 0 {
			cpu := signedDeltaInt64(last.Process.CPUUserNS+last.Process.CPUSystemNS, first.Process.CPUUserNS+first.Process.CPUSystemNS)
			if cpu > 0 {
				result.CPUTimePerCycleNS = int64(cpu) / int64(commits)
			}
		}
	}
	return result
}

func storageReport(samples []QualificationSample, duration time.Duration, maxWALBytes int64) StorageReport {
	first, last := samples[0].Storage, samples[len(samples)-1].Storage
	result := StorageReport{StoreMode: last.StoreMode, WALStartBytes: first.WALBytes, WALEndBytes: last.WALBytes, AverageTransactionBytes: last.AverageTransactionBytes, MaximumTransactionBytes: last.MaximumTransactionBytes, LastRecoveryDurationNS: last.LastRecoveryDurationNS}
	for _, sample := range samples {
		result.CheckpointCount = maxUint64(result.CheckpointCount, sample.CheckpointsSucceeded)
		result.CheckpointFailures = maxUint64(result.CheckpointFailures, sample.CheckpointsFailed)
	}
	if duration > 0 {
		result.WALGrowthBytesPerHour = int64(float64(last.WALBytes-first.WALBytes) / duration.Hours())
		if result.WALGrowthBytesPerHour > 0 && last.WALBytes > 0 && maxWALBytes > last.WALBytes {
			result.EstimatedHoursToLimit = float64(maxWALBytes-last.WALBytes) / float64(result.WALGrowthBytesPerHour)
		}
	}
	return result
}

func firstInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}
func lastInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}
func quantileInt64(values []int64, fraction float64) int64 {
	if len(values) == 0 {
		return 0
	}
	return values[int(float64(len(values)-1)*fraction)]
}
func quantileUint64(values []uint64, fraction float64) uint64 {
	if len(values) == 0 {
		return 0
	}
	return values[int(float64(len(values)-1)*fraction)]
}
func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func sumInt64(values []int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}

func signedDeltaUint32(current, previous uint32) int64 {
	if current >= previous {
		return int64(current - previous)
	}
	return -int64(previous - current)
}

func signedDeltaInt64(current, previous int64) int64 {
	return current - previous
}

func signedDeltaInt(current, previous int) int64 {
	return int64(current) - int64(previous)
}
