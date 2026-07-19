package deviation

import (
	"testing"

	"synora/internal/cge/routines"
)

func TestTemporalCircularDistance(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, occurrence := testRoutine(t, subject, false)
	policy := testPolicy()
	occurrence.Weekday = baseline.TemporalBins[0].Weekday
	occurrence.TimeBucket = baseline.TemporalBins[0].TimeBucket
	factor, err := EvaluateTemporal(occurrence, baseline, policy)
	if err != nil || !factor.Available || factor.Score != 0 {
		t.Fatalf("expected exact temporal bin: %+v, %v", factor, err)
	}
	occurrence.Weekday = 1
	occurrence.TimeBucket = 0
	baseline.TemporalBins = []routines.TemporalBin{{Weekday: 0, TimeBucket: 95, Count: 1}}
	factor, err = EvaluateTemporal(occurrence, baseline, policy)
	if err != nil || !factor.Available || factor.Score == 0 {
		t.Fatalf("expected circular near-bin distance: %+v, %v", factor, err)
	}
}
