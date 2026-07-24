package shadowworkflow

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type qualificationLogger struct {
	mu   sync.Mutex
	text string
}

func (l *qualificationLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	l.text += format
	l.mu.Unlock()
}

func (l *qualificationLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text
}

func TestQualificationLogsContainNoSensitiveInput(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxProcessingDuration = 2 * time.Second
	logger := &qualificationLogger{}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, logger, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())

	input := testInput(at, "secret-marker-event")
	input.SourceShadowFingerprint = "secret-marker-fingerprint"
	if result := r.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("status=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if strings.Contains(logger.String(), "secret-marker") {
		t.Fatalf("sensitive input was logged: %q", logger.String())
	}
}
