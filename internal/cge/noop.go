package cge

import "context"

// NoopEngine is the neutral default. It preserves the pre-CGE integration
// behavior by accepting all observations without retaining or producing data.
type NoopEngine struct{}

// NewNoopEngine returns a neutral CGE implementation.
func NewNoopEngine() *NoopEngine {
	return &NoopEngine{}
}

func (e *NoopEngine) Observe(context.Context, Event) (ObservationResult, error) {
	return ObservationResult{}, nil
}

func (e *NoopEngine) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{}, nil
}

func (e *NoopEngine) Explain(_ context.Context, situationID string) (Explanation, error) {
	return Explanation{SituationID: situationID}, nil
}

// Close keeps shutdown symmetrical with the configured shadow engine.
func (e *NoopEngine) Close() error { return nil }

var _ CognitiveEngine = (*NoopEngine)(nil)
