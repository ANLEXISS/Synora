package campaign

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	cge "synora/internal/cge"
	"synora/internal/cge/deviation"
)

func aggregateWarmup(results []EventResult, timeline Timeline) WarmupMetrics {
	var result WarmupMetrics
	for index, event := range results {
		if event.DeviationStatus == deviation.StatusInsufficientHistory {
			result.InsufficientHistoryCount++
		}
		if event.DeviationStatus == deviation.StatusEvaluated || event.DeviationStatus == deviation.StatusPartial || event.DeviationStatus == deviation.StatusAmbiguous {
			if result.FirstEvaluatedAt == nil {
				value := event.OccurredAt
				result.FirstEvaluatedAt = &value
				result.EventsBeforeFirstEvaluation = index
				result.DaysBeforeFirstEvaluation = int(event.OccurredAt.Sub(timeline.StartAt).Hours() / 24)
			}
			if event.DeviationBand != deviation.BandUnknown {
				if event.RoutineTransitionApplied && result.FirstTransitionEvaluatedAt == nil {
					value := event.OccurredAt
					result.FirstTransitionEvaluatedAt = &value
				}
				if !event.RoutineTransitionApplied && result.FirstPresenceEvaluatedAt == nil {
					value := event.OccurredAt
					result.FirstPresenceEvaluatedAt = &value
				}
			}
		}
	}
	return result
}

func aggregateLabels(results []EventResult) []LabelMetrics {
	byLabel := map[EpisodeLabel][]EventResult{}
	for _, result := range results {
		byLabel[result.Label] = append(byLabel[result.Label], result)
	}
	labels := make([]EpisodeLabel, 0, len(byLabel))
	for label := range byLabel {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i] < labels[j] })
	out := make([]LabelMetrics, 0, len(labels))
	for _, label := range labels {
		values := byLabel[label]
		metric := LabelMetrics{Label: label, EventCount: len(values)}
		scores := make([]int, 0, len(values))
		coverage := 0
		for _, value := range values {
			switch value.DeviationStatus {
			case deviation.StatusEvaluated:
				metric.EvaluatedCount++
			case deviation.StatusPartial:
				metric.PartialCount++
			case deviation.StatusInsufficientHistory:
				metric.InsufficientHistoryCount++
			case deviation.StatusAmbiguous:
				metric.AmbiguousCount++
			case deviation.StatusAlreadyEvaluated:
				metric.AlreadyEvaluatedCount++
			case deviation.StatusNotApplicable:
				metric.NotApplicableCount++
			}
			switch value.DeviationBand {
			case deviation.BandAligned:
				metric.AlignedCount++
			case deviation.BandLow:
				metric.LowCount++
			case deviation.BandModerate:
				metric.ModerateCount++
			case deviation.BandHigh:
				metric.HighCount++
			}
			if value.DeviationStatus == deviation.StatusEvaluated || value.DeviationStatus == deviation.StatusPartial || value.DeviationStatus == deviation.StatusAmbiguous {
				scores = append(scores, int(value.DeviationScore))
				coverage += int(value.DeviationCoverage)
			}
		}
		metric.MeanScore, metric.MeanCoverage = mean(scores), meanCoverage(coverage, len(scores))
		metric.MedianScore = deviation.Score(quantile(scores, 500))
		metric.P90Score = deviation.Score(quantile(scores, 900))
		metric.P95Score = deviation.Score(quantile(scores, 950))
		metric.MaximumScore = deviation.Score(quantile(scores, 1000))
		out = append(out, metric)
	}
	return out
}

func aggregateBenign(labels []LabelMetrics) BenignDeviationMetrics {
	var result BenignDeviationMetrics
	for _, metric := range labels {
		if metric.Label == LabelOrdinary || metric.Label == LabelBenignVariation || metric.Label == LabelRareLegitimate {
			result.EvaluatedEvents += metric.EvaluatedCount + metric.PartialCount + metric.AmbiguousCount
			result.ModerateOrHighCount += metric.ModerateCount + metric.HighCount
			result.HighCount += metric.HighCount
		}
	}
	if result.EvaluatedEvents > 0 {
		result.ModerateOrHighRatePermille = result.ModerateOrHighCount * 1000 / result.EvaluatedEvents
		result.HighRatePermille = result.HighCount * 1000 / result.EvaluatedEvents
	}
	return result
}

func computeSeparation(results []EventResult) SeparationMetrics {
	groups := map[EpisodeLabel][]int{}
	for _, value := range results {
		if value.DeviationStatus == deviation.StatusEvaluated || value.DeviationStatus == deviation.StatusPartial || value.DeviationStatus == deviation.StatusAmbiguous {
			groups[value.Label] = append(groups[value.Label], int(value.DeviationScore))
		}
	}
	ordinary := append([]int(nil), groups[LabelOrdinary]...)
	benign := append(groups[LabelBenignVariation], groups[LabelRareLegitimate]...)
	synthetic := groups[LabelSyntheticIntrusion]
	result := SeparationMetrics{OrdinaryMedian: deviation.Score(quantile(ordinary, 500)), BenignMedian: deviation.Score(quantile(benign, 500)), SyntheticMedian: deviation.Score(quantile(synthetic, 500)), OrdinaryP95: deviation.Score(quantile(ordinary, 950)), SyntheticP50: deviation.Score(quantile(synthetic, 500))}
	result.MedianGap = int(result.SyntheticMedian) - int(result.OrdinaryMedian)
	result.QuantileOverlapPermille = overlap(ordinary, synthetic)
	return result
}

func computeAdaptation(results []EventResult, timeline Timeline) AdaptationMetrics {
	result := AdaptationMetrics{ChangeStartedAt: timeline.StartAt.AddDate(0, 0, 15)}
	eventsSinceChange := 0
	for _, value := range results {
		if value.OccurredAt.Before(result.ChangeStartedAt) {
			continue
		}
		eventsSinceChange++
		if value.DeviationBand == deviation.BandHigh {
			if result.FirstHighAt == nil {
				v := value.OccurredAt
				result.FirstHighAt = &v
			}
			v := value.OccurredAt
			result.LastHighAt = &v
		}
		if result.FirstAlignedAfterChange == nil && value.DeviationBand == deviation.BandAligned {
			v := value.OccurredAt
			result.FirstAlignedAfterChange = &v
			result.EventsUntilAligned = eventsSinceChange
		}
		if value.RoutinePresenceApplied || value.RoutineTransitionApplied {
			result.NewRoutineOccurrenceCount++
		}
	}
	if result.FirstAlignedAfterChange != nil {
		result.DaysUntilAligned = int(result.FirstAlignedAfterChange.Sub(result.ChangeStartedAt).Hours() / 24)
	}
	return result
}

func quantile(values []int, permille int) int {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int(nil), values...)
	sort.Ints(copyValues)
	index := (len(copyValues) - 1) * permille / 1000
	return copyValues[index]
}
func mean(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0
	for _, value := range values {
		total += value
	}
	return float64(total) / float64(len(values))
}
func meanCoverage(total, count int) float64 {
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}
func overlap(left, right []int) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	minL, maxL := left[0], left[0]
	for _, v := range left {
		if v < minL {
			minL = v
		}
		if v > maxL {
			maxL = v
		}
	}
	minR, maxR := right[0], right[0]
	for _, v := range right {
		if v < minR {
			minR = v
		}
		if v > maxR {
			maxR = v
		}
	}
	low, high := minL, maxL
	if minR > low {
		low = minR
	}
	if maxR < high {
		high = maxR
	}
	if high < low {
		return 0
	}
	width := maxL - minL + 1
	if width <= 0 {
		return 0
	}
	return (high - low + 1) * 1000 / width
}

func latencyMetrics(values []time.Duration) LatencyMetrics {
	summary := durationSummary(values)
	unavailable := DurationSummary{Status: "unavailable"}
	return LatencyMetrics{Total: summary, Association: unavailable, Deviation: unavailable, Learning: unavailable, WAL: unavailable}
}
func durationSummary(values []time.Duration) DurationSummary {
	if len(values) == 0 {
		return DurationSummary{}
	}
	copyValues := append([]time.Duration(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	pick := func(p int) time.Duration { return copyValues[(len(copyValues)-1)*p/1000] }
	return DurationSummary{Count: len(values), Status: "measured", Median: pick(500), P90: pick(900), P95: pick(950), P99: pick(990), Maximum: copyValues[len(copyValues)-1]}
}

func growthSample(ctx context.Context, engine *cge.ShadowEngine, root string, eventCount, generations int, at time.Time) GrowthMetrics {
	status := engine.Status()
	routines := engine.ListRoutines()
	occurrences := 0
	for _, routine := range routines {
		occurrences += len(routine.Occurrences)
	}
	journalBytes := int64(0)
	if info, err := os.Stat(filepath.Join(root, "journal.ndjson")); err == nil {
		journalBytes = info.Size()
	}
	return GrowthMetrics{At: at, EventCount: eventCount, ChainCount: status.ChainCount, HypothesisCount: status.HypothesisCount, RoutineCount: status.RoutineCount, RoutineOccurrenceCount: occurrences, JournalRecords: status.JournalSequence, JournalBytes: journalBytes, GenerationCount: generations, RecentDeviationStoreCount: engine.SnapshotCount()}
}

func collectMemory(root string, engine *cge.ShadowEngine) []MemoryMetric {
	result := []MemoryMetric{}
	if info, err := os.Stat(filepath.Join(root, "journal.ndjson")); err == nil {
		result = append(result, MemoryMetric{Kind: "journal_size", Value: info.Size(), Unit: "bytes", Status: "measured"})
	}
	if snapshot, err := engine.Snapshot(context.Background()); err == nil {
		result = append(result, MemoryMetric{Kind: "recent_deviation_store", Value: int64(snapshot.DeviationAssessmentStoreCount), Unit: "assessments", Status: "measured"})
	}
	if data, err := json.Marshal(engine.ListRoutines()); err == nil {
		result = append(result, MemoryMetric{Kind: "routine_registry_snapshot", Value: int64(len(data)), Unit: "bytes", Status: "estimated"})
	}
	return result
}
