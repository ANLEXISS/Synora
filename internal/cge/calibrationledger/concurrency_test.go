package calibrationledger

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentIdempotentAppend(t *testing.T) {
	s := openTestStore(t, DefaultPolicy())
	r := testRecord(t, "concurrent", nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	appended, duplicates := 0, 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.Append(context.Background(), r)
			if err != nil {
				t.Errorf("append: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if result.Appended {
				appended++
			}
			if result.Duplicate {
				duplicates++
			}
		}()
	}
	wg.Wait()
	if appended != 1 || duplicates != 31 || s.Snapshot().RecordCount != 1 {
		t.Fatalf("appended=%d duplicates=%d snapshot=%+v", appended, duplicates, s.Snapshot())
	}
}
