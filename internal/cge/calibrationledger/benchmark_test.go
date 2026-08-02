package calibrationledger

import (
	"context"
	"testing"
)

func BenchmarkBuildRecord(b *testing.B) {
	comparison := testComparison("benchmark")
	p := DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildRecord(BuildRecordInput{Comparison: comparison}, p); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkAppendDuplicate(b *testing.B) {
	s := openTestStore(b, DefaultPolicy())
	defer s.Close()
	r := testRecord(b, "benchmark-append", nil)
	if _, err := s.Append(context.Background(), r); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Append(context.Background(), r); err != nil {
			b.Fatal(err)
		}
	}
}
