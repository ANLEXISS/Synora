package fieldtrial

import (
	"context"
	"testing"
	"time"
)

func BenchmarkRecorderAppend(b *testing.B) {
	config := testConfig(b.TempDir())
	config.SegmentMaxBytes = 16 << 20
	config.MaximumTotalBytes = 64 << 20
	r, err := OpenWithKey(context.Background(), config, OpenMetadata{}, time.Now().UTC(), []byte("benchmark-field-trial-key"))
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background(), time.Now().UTC())
	base := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Record(context.Background(), testInput(base.Add(time.Duration(i)*time.Second), "benchmark-event"+string(rune(i)))); err != nil {
			b.Fatal(err)
		}
	}
}
