package calibrationledger

import (
	"context"
	"testing"
)

func TestAppendRecoveryPropertyLikeSequenceAndDigest(t *testing.T) {
	s := openTestStore(t, DefaultPolicy())
	var previous *CalibrationRecord
	for i := 0; i < 32; i++ {
		r := testRecord(t, "property-"+string(rune('a'+i)), previous)
		result, err := s.Append(context.Background(), r)
		if err != nil || result.Sequence != uint64(i+1) {
			t.Fatalf("i=%d result=%+v err=%v", i, result, err)
		}
		copy := r
		copy.Sequence = result.Sequence
		previous = &copy
	}
	snapshot := s.Snapshot()
	if snapshot.RecordCount != 32 || snapshot.Aggregate.TotalRecords != 32 || snapshot.Digest == "" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}
