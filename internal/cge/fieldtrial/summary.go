package fieldtrial

import (
	"context"
	"sort"
	"time"
)

type Count struct {
	Key   string `json:"key"`
	Count uint64 `json:"count"`
}
type StatusCounts []Count
type BandCounts []Count

type Quantiles struct {
	Count   uint64 `json:"count"`
	P50     uint16 `json:"p50"`
	P90     uint16 `json:"p90"`
	P95     uint16 `json:"p95"`
	P99     uint16 `json:"p99"`
	Maximum uint16 `json:"maximum"`
}

type DailySummary struct {
	SessionID              string       `json:"session_id"`
	LocalDate              string       `json:"local_date"`
	EventCount             uint64       `json:"event_count"`
	StatusCounts           StatusCounts `json:"status_counts"`
	BandCounts             BandCounts   `json:"band_counts"`
	MeanScorePermille      uint64       `json:"mean_score_permille"`
	MeanCoveragePermille   uint64       `json:"mean_coverage_permille"`
	RoutineCreatedCount    uint64       `json:"routine_created_count"`
	RoutineOccurrenceCount uint64       `json:"routine_occurrence_count"`
	ErrorCount             uint64       `json:"error_count"`
	JournalRecords         uint64       `json:"journal_records"`
	JournalBytes           int64        `json:"journal_bytes"`
	CognitiveWALSequence   uint64       `json:"cognitive_wal_sequence"`
	MedianLatencyMicros    uint64       `json:"median_latency_micros"`
	P95LatencyMicros       uint64       `json:"p95_latency_micros"`
	MaximumLatencyMicros   uint64       `json:"maximum_latency_micros"`
}

type LatencySummary struct {
	Count   uint64 `json:"count"`
	P50     uint64 `json:"p50"`
	P90     uint64 `json:"p90"`
	P95     uint64 `json:"p95"`
	P99     uint64 `json:"p99"`
	Maximum uint64 `json:"maximum"`
}
type AnnotatedLabelMetrics struct {
	Label               AnnotationLabel `json:"label"`
	EventCount          uint64          `json:"event_count"`
	HighCount           uint64          `json:"high_count"`
	ModerateOrHighCount uint64          `json:"moderate_or_high_count"`
	MeanScore           uint64          `json:"mean_score"`
}
type AdaptationFinding struct {
	Code        string `json:"code"`
	EventRef    string `json:"event_ref,omitempty"`
	Days        uint64 `json:"days,omitempty"`
	Occurrences uint64 `json:"occurrences,omitempty"`
}
type GrowthPoint struct {
	At             time.Time `json:"at"`
	EventCount     uint64    `json:"event_count"`
	JournalRecords uint64    `json:"journal_records"`
	JournalBytes   int64     `json:"journal_bytes"`
}
type CalibrationFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type TrialReport struct {
	SessionID           string                  `json:"session_id"`
	StartedAt           time.Time               `json:"started_at"`
	EndedAt             *time.Time              `json:"ended_at,omitempty"`
	Duration            time.Duration           `json:"duration"`
	EventCount          uint64                  `json:"event_count"`
	AnnotationCount     uint64                  `json:"annotation_count"`
	StatusCounts        StatusCounts            `json:"status_counts"`
	BandCounts          BandCounts              `json:"band_counts"`
	ScoreQuantiles      Quantiles               `json:"score_quantiles"`
	CoverageQuantiles   Quantiles               `json:"coverage_quantiles"`
	AnnotatedMetrics    []AnnotatedLabelMetrics `json:"annotated_metrics"`
	AdaptationFindings  []AdaptationFinding     `json:"adaptation_findings"`
	JournalGrowth       []GrowthPoint           `json:"journal_growth"`
	Latency             LatencySummary          `json:"latency"`
	RecorderErrors      uint64                  `json:"recorder_errors"`
	CognitiveErrors     uint64                  `json:"cognitive_errors"`
	TechnicalSuccess    bool                    `json:"technical_success"`
	CalibrationFindings []CalibrationFinding    `json:"calibration_findings"`
}

// BuildDailySummaries aggregates only expunged trial events. Dates use the
// UTC timestamp recorded in the trial because no resident timezone is stored
// in telemetry.
func BuildDailySummaries(ctx context.Context, sessionDir string) ([]DailySummary, error) {
	events, _, manifest, err := ReadEvents(ctx, sessionDir)
	if err != nil {
		return nil, err
	}
	type aggregate struct {
		DailySummary
		scores, coverage, latency []uint16
	}
	groups := map[string]*aggregate{}
	for _, event := range events {
		date := event.ObservedAt.UTC().Format("2006-01-02")
		item := groups[date]
		if item == nil {
			item = &aggregate{DailySummary: DailySummary{SessionID: manifest.SessionID, LocalDate: date, StatusCounts: StatusCounts{}, BandCounts: BandCounts{}}}
			groups[date] = item
		}
		item.EventCount++
		if event.DeviationStatus != "" {
			item.StatusCounts = incrementCount(item.StatusCounts, event.DeviationStatus)
		}
		if event.DeviationBand != "" {
			item.BandCounts = incrementCount(item.BandCounts, event.DeviationBand)
		}
		if event.DeviationAttempted {
			item.scores = append(item.scores, event.DeviationScore)
			item.coverage = append(item.coverage, event.DeviationCoverage)
		}
		if event.TotalLatencyMicros > 0 {
			item.latency = append(item.latency, uint16(minUint64(event.TotalLatencyMicros, 65535)))
		}
		if event.PresenceApplied || event.TransitionApplied {
			item.RoutineOccurrenceCount++
		}
		if event.ErrorCodes != nil {
			item.ErrorCount += uint64(len(event.ErrorCodes))
		}
		if event.CognitiveWALSequence > item.CognitiveWALSequence {
			item.CognitiveWALSequence = event.CognitiveWALSequence
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]DailySummary, 0, len(keys))
	for _, key := range keys {
		item := groups[key]
		if len(item.scores) > 0 {
			item.MeanScorePermille = meanUint16(item.scores)
			item.MeanCoveragePermille = meanUint16(item.coverage)
		}
		if len(item.latency) > 0 {
			q := makeQuantiles(item.latency)
			item.MedianLatencyMicros = uint64(q.P50)
			item.P95LatencyMicros = uint64(q.P95)
			item.MaximumLatencyMicros = uint64(q.Maximum)
		}
		result = append(result, item.DailySummary)
	}
	return result, nil
}

func incrementCount(values []Count, key string) []Count {
	for i := range values {
		if values[i].Key == key {
			values[i].Count++
			return values
		}
	}
	return append(values, Count{Key: key, Count: 1})
}

func meanUint16(values []uint16) uint64 {
	var total uint64
	for _, value := range values {
		total += uint64(value)
	}
	return total / uint64(len(values))
}
func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func BuildReport(ctx context.Context, sessionDir string) (TrialReport, error) {
	events, annotations, manifest, err := ReadEvents(ctx, sessionDir)
	if err != nil {
		return TrialReport{}, err
	}
	report := TrialReport{SessionID: manifest.SessionID, StartedAt: manifest.CreatedAt, EventCount: uint64(len(events)), AnnotationCount: uint64(len(annotations)), TechnicalSuccess: manifest.Status != SessionDegraded}
	if manifest.ClosedAt != nil {
		report.EndedAt = manifest.ClosedAt
		report.Duration = manifest.ClosedAt.Sub(manifest.CreatedAt)
	}
	statusMap, bandMap := map[string]uint64{}, map[string]uint64{}
	scores, coverage, latencies := make([]uint16, 0), make([]uint16, 0), make([]uint64, 0)
	for _, event := range events {
		if event.DeviationStatus != "" {
			statusMap[event.DeviationStatus]++
		}
		if event.DeviationBand != "" {
			bandMap[event.DeviationBand]++
		}
		if event.DeviationAttempted {
			scores = append(scores, event.DeviationScore)
			coverage = append(coverage, event.DeviationCoverage)
		}
		if event.TotalLatencyMicros > 0 {
			latencies = append(latencies, event.TotalLatencyMicros)
		}
		if len(event.ErrorCodes) > 0 {
			report.RecorderErrors += uint64(len(event.ErrorCodes))
		}
	}
	report.StatusCounts, report.BandCounts = sortedCounts(statusMap), sortedCounts(bandMap)
	report.ScoreQuantiles, report.CoverageQuantiles = makeQuantiles(scores), makeQuantiles(coverage)
	report.Latency = makeLatency(latencies)
	for _, annotation := range annotations {
		report.AnnotatedMetrics = append(report.AnnotatedMetrics, annotatedMetrics(annotation, events))
	}
	return report, nil
}

func sortedCounts(values map[string]uint64) []Count {
	result := make([]Count, 0, len(values))
	for key, count := range values {
		result = append(result, Count{key, count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}
func makeQuantiles(values []uint16) Quantiles {
	if len(values) == 0 {
		return Quantiles{}
	}
	copyValues := append([]uint16(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	pick := func(p int) uint16 { return copyValues[(len(copyValues)-1)*p/1000] }
	return Quantiles{uint64(len(values)), pick(500), pick(900), pick(950), pick(990), copyValues[len(copyValues)-1]}
}
func makeLatency(values []uint64) LatencySummary {
	if len(values) == 0 {
		return LatencySummary{}
	}
	copyValues := append([]uint64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	pick := func(p int) uint64 { return copyValues[(len(copyValues)-1)*p/1000] }
	return LatencySummary{uint64(len(values)), pick(500), pick(900), pick(950), pick(990), copyValues[len(copyValues)-1]}
}
func annotatedMetrics(annotation Annotation, events []TrialEvent) AnnotatedLabelMetrics {
	result := AnnotatedLabelMetrics{Label: annotation.Label}
	for _, event := range events {
		if event.EventRef != annotation.EventRef {
			continue
		}
		result.EventCount++
		result.MeanScore += uint64(event.DeviationScore)
		if event.DeviationBand == "high" {
			result.HighCount++
		}
		if event.DeviationBand == "moderate" || event.DeviationBand == "high" {
			result.ModerateOrHighCount++
		}
	}
	if result.EventCount > 0 {
		result.MeanScore /= result.EventCount
	}
	return result
}
