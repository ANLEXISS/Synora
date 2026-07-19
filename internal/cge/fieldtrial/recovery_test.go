package fieldtrial

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestVerifyRejectsModifiedHistoricalRecord(t *testing.T) {
	root := t.TempDir()
	c := testConfig(root)
	c.SegmentMaxBytes = 1 << 20
	r, err := OpenWithKey(context.Background(), c, OpenMetadata{}, time.Now().UTC(), []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(context.Background(), testInput(time.Now().UTC(), "event-a")); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	path := c.RootDir + "/" + c.SessionID + "/events-000001.ndjson"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 10 {
		t.Fatal("record too short")
	}
	data[len(data)-3] ^= 1
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySession(context.Background(), c.RootDir+"/"+c.SessionID, false); err == nil {
		t.Fatal("expected hash failure")
	}
}
