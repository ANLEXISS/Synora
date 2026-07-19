package deviation

import (
	"time"

	"synora/internal/cge/routines"
)

func EvaluateInterval(occurrence routines.Occurrence, routine routines.Snapshot, policy Policy) (Factor, error) {
	if err := policy.Validate(); err != nil {
		return Factor{}, err
	}
	stats := routine.IntervalStatistics
	if stats.Count < 2 || !occurrence.ObservedAt.After(routine.LastSeenAt) || stats.Mean <= 0 {
		code := "interval.unavailable"
		if !occurrence.ObservedAt.After(routine.LastSeenAt) {
			code = "interval.late_observation"
		}
		return Factor{Kind: FactorInterval, Available: false, ReasonCodes: []string{code}}, nil
	}
	elapsed := occurrence.ObservedAt.Sub(routine.LastSeenAt)
	if elapsed >= stats.Minimum && elapsed <= stats.Maximum {
		return Factor{Kind: FactorInterval, Available: true, Score: 0, ReasonCodes: []string{"interval.within_range"}}, nil
	}
	distance := stats.Minimum - elapsed
	code := "interval.below_range"
	if elapsed > stats.Maximum {
		distance = elapsed - stats.Maximum
		code = "interval.above_range"
	}
	scale := stats.Mean
	bucketScale := time.Duration(policy.TemporalBucketMinutes) * time.Minute
	if scale < bucketScale {
		scale = bucketScale
	}
	return Factor{Kind: FactorInterval, Available: true, Score: roundedRatio(int64(distance), int64(scale)), ReasonCodes: []string{code}}, nil
}
