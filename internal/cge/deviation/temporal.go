package deviation

import (
	"sort"
	"synora/internal/cge/routines"
)

func EvaluateTemporal(occurrence routines.Occurrence, routine routines.Snapshot, policy Policy) (Factor, error) {
	if err := policy.Validate(); err != nil {
		return Factor{}, err
	}
	if len(routine.TemporalBins) == 0 {
		return Factor{Kind: FactorTemporal, Available: false, ReasonCodes: []string{"temporal.no_bins"}}, nil
	}
	perDay := 1440 / policy.TemporalBucketMinutes
	weekSize := 7 * perDay
	observed := int(occurrence.Weekday)*perDay + occurrence.TimeBucket
	best := weekSize
	for _, bin := range routine.TemporalBins {
		candidate := int(bin.Weekday)*perDay + bin.TimeBucket
		distance := observed - candidate
		if distance < 0 {
			distance = -distance
		}
		if circular := weekSize - distance; circular < distance {
			distance = circular
		}
		if distance < best {
			best = distance
		}
	}
	score := roundedRatio(int64(best), int64(policy.TemporalToleranceBuckets))
	reasons := []string{"temporal.distant_bin"}
	if best == 0 {
		score = 0
		reasons = []string{"temporal.exact_bin"}
	} else if best <= policy.TemporalToleranceBuckets {
		reasons = []string{"temporal.near_bin"}
	}
	sort.Strings(reasons)
	return Factor{Kind: FactorTemporal, Available: true, Score: score, ReasonCodes: reasons}, nil
}
