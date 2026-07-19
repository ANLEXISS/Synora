package deviation

import (
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/routines"
)

func TestVolumeDeterminismAcrossSubjectsAndRoutines(t *testing.T) {
	policy := testPolicy()
	base := deviationTestBase
	for i := 0; i < 500; i++ {
		subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: fmt.Sprintf("volume-entity-%03d", i)}
		first, occurrence := testRoutine(t, subject, false)
		second, _ := testRoutine(t, subject, true)
		at := base.Add(time.Duration(i) * time.Hour)
		left, err := EvaluateOccurrence(occurrence, []routines.Snapshot{first, second}, at, policy)
		if err != nil {
			t.Fatal(err)
		}
		right, err := EvaluateOccurrence(occurrence, []routines.Snapshot{second, first}, at, policy)
		if err != nil {
			t.Fatal(err)
		}
		if left.Fingerprint != right.Fingerprint {
			t.Fatalf("baseline order changed result for subject %d", i)
		}
	}
}
