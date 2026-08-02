package calibrationledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCrashStylePartialTailIsNeverSilentlyAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.ndjson")
	s, err := OpenFileStore(path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	r := testRecord(t, "crash", nil)
	if _, err = s.Append(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	data, _ := os.ReadFile(path)
	_ = os.WriteFile(path, data[:len(data)-2], 0600)
	if _, err := OpenFileStore(path, DefaultPolicy()); err == nil {
		t.Fatal("partial tail accepted")
	}
}
