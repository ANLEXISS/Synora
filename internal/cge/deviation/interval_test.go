package deviation

import (
	"testing"

	"synora/internal/cge/routines"
)

func TestIntervalRangeAndLateObservation(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, occurrence := testRoutine(t, subject, false)
	factor, err := EvaluateInterval(occurrence, baseline, testPolicy())
	if err != nil || !factor.Available || factor.Score != 0 {
		t.Fatalf("expected in-range interval: %+v, %v", factor, err)
	}
	late := occurrence
	late.ObservedAt = baseline.FirstSeenAt
	factor, err = EvaluateInterval(late, baseline, testPolicy())
	if err != nil || factor.Available || len(factor.ReasonCodes) != 1 || factor.ReasonCodes[0] != "interval.late_observation" {
		t.Fatalf("expected late interval unavailable: %+v, %v", factor, err)
	}
}
