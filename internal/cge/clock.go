package cge

import "time"

// Clock supplies processing time to the shadow integration. Event source
// timestamps remain part of the adapted observation.
type Clock interface {
	Now() time.Time
}

// SystemClock is the runtime composition clock. Tests should provide their
// own implementation.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
