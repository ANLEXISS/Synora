package routines

import (
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/context"
)

type TemporalBin struct {
	Weekday    time.Weekday `json:"weekday"`
	TimeBucket int          `json:"time_bucket"`
	Count      uint64       `json:"count"`
}
type DayPartCount struct {
	DayPart context.DayPart `json:"day_part"`
	Count   uint64          `json:"count"`
}
type IntervalStatistics struct {
	Count   uint64        `json:"count"`
	Minimum time.Duration `json:"minimum"`
	Maximum time.Duration `json:"maximum"`
	Total   time.Duration `json:"total"`
	Mean    time.Duration `json:"mean"`
}

type DescriptiveMetrics struct {
	OccurrenceCount      uint64
	DistinctLocalDays    uint64
	DistinctLocalWeeks   uint64
	Span                 time.Duration
	MeanInterval         time.Duration
	DominantTemporalBins []TemporalBin
	DominantDayParts     []DayPartCount
}

func deriveStatistics(occurrences []OccurrenceRef) ([]TemporalBin, []DayPartCount, IntervalStatistics, uint64, uint64, time.Time, time.Time, error) {
	if len(occurrences) == 0 {
		return nil, nil, IntervalStatistics{}, 0, 0, time.Time{}, time.Time{}, nil
	}
	copyRefs := cloneOccurrenceRefs(occurrences)
	sort.Slice(copyRefs, func(i, j int) bool { return occurrenceRefLess(copyRefs[i], copyRefs[j]) })
	binMap := map[[2]int]uint64{}
	dayMap := map[string]uint64{}
	weekMap := map[string]uint64{}
	intervals := IntervalStatistics{}
	first, last := copyRefs[0].ObservedAt, copyRefs[0].ObservedAt
	for i, ref := range copyRefs {
		binMap[[2]int{int(ref.Weekday), ref.TimeBucket}]++
		dayMap[ref.LocalDate]++
		weekMap[localWeekKey(ref.LocalDate)]++
		if ref.ObservedAt.Before(first) {
			first = ref.ObservedAt
		}
		if ref.ObservedAt.After(last) {
			last = ref.ObservedAt
		}
		if i > 0 {
			gap := ref.ObservedAt.Sub(copyRefs[i-1].ObservedAt)
			intervals.Count++
			intervals.Total += gap
			if intervals.Count == 1 || gap < intervals.Minimum {
				intervals.Minimum = gap
			}
			if gap > intervals.Maximum {
				intervals.Maximum = gap
			}
		}
	}
	if intervals.Count > 0 {
		intervals.Mean = intervals.Total / time.Duration(intervals.Count)
	}
	bins := make([]TemporalBin, 0, len(binMap))
	for key, count := range binMap {
		bins = append(bins, TemporalBin{Weekday: time.Weekday(key[0]), TimeBucket: key[1], Count: count})
	}
	sort.Slice(bins, func(i, j int) bool {
		if bins[i].Weekday != bins[j].Weekday {
			return bins[i].Weekday < bins[j].Weekday
		}
		return bins[i].TimeBucket < bins[j].TimeBucket
	})
	partsMap := map[context.DayPart]uint64{}
	for _, ref := range copyRefs {
		partsMap[dayPartForRef(ref)]++
	}
	parts := make([]DayPartCount, 0, len(partsMap))
	for part, count := range partsMap {
		parts = append(parts, DayPartCount{part, count})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].DayPart < parts[j].DayPart })
	return bins, parts, intervals, uint64(len(dayMap)), uint64(len(weekMap)), first, last, nil
}

func dayPartForRef(ref OccurrenceRef) context.DayPart { return ref.DayPart }
func localWeekKey(date string) string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	year, week := parsed.ISOWeek()
	return fmt.Sprintf("%04d-%02d", year, week)
}
func occurrenceRefLess(a, b OccurrenceRef) bool {
	if !a.ObservedAt.Equal(b.ObservedAt) {
		return a.ObservedAt.Before(b.ObservedAt)
	}
	return a.ID < b.ID
}
func cloneOccurrenceRefs(values []OccurrenceRef) []OccurrenceRef {
	if values == nil {
		return nil
	}
	out := make([]OccurrenceRef, len(values))
	for i, v := range values {
		out[i] = v
		out[i].ObservationIDs = append([]string(nil), v.ObservationIDs...)
		out[i].TopologyRevisions = append([]string(nil), v.TopologyRevisions...)
	}
	return out
}

func (r *Routine) DescriptiveMetrics() DescriptiveMetrics {
	if r == nil {
		return DescriptiveMetrics{}
	}
	bins := append([]TemporalBin(nil), r.temporalBins...)
	parts := append([]DayPartCount(nil), r.dayPartCounts...)
	sort.SliceStable(bins, func(i, j int) bool {
		if bins[i].Count != bins[j].Count {
			return bins[i].Count > bins[j].Count
		}
		if bins[i].Weekday != bins[j].Weekday {
			return bins[i].Weekday < bins[j].Weekday
		}
		return bins[i].TimeBucket < bins[j].TimeBucket
	})
	sort.SliceStable(parts, func(i, j int) bool {
		if parts[i].Count != parts[j].Count {
			return parts[i].Count > parts[j].Count
		}
		return parts[i].DayPart < parts[j].DayPart
	})
	span := time.Duration(0)
	if !r.firstSeenAt.IsZero() && !r.lastSeenAt.IsZero() {
		span = r.lastSeenAt.Sub(r.firstSeenAt)
	}
	return DescriptiveMetrics{OccurrenceCount: uint64(len(r.occurrences)), DistinctLocalDays: r.distinctLocalDays, DistinctLocalWeeks: r.distinctLocalWeeks, Span: span, MeanInterval: r.intervalStatistics.Mean, DominantTemporalBins: bins, DominantDayParts: parts}
}
