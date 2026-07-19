package fieldtrial

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOpenRejectsCognitiveConfigurationDrift(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.RootDir = t.TempDir()
	config.SessionID = "drift-session"
	key := []byte("01234567890123456789012345678901")
	first, err := OpenWithKey(context.Background(), config, OpenMetadata{CognitiveConfigurationFingerprint: "sha256:first"}, time.Unix(1, 0).UTC(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = OpenWithKey(context.Background(), config, OpenMetadata{CognitiveConfigurationFingerprint: "sha256:second"}, time.Unix(2, 0).UTC(), key)
	if !errors.Is(err, ErrConfigurationDrift) {
		t.Fatalf("err=%v, want configuration drift", err)
	}
}
