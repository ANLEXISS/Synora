package shadowworkflow

import "time"

type circuitState string

const (
	circuitClosed   circuitState = "closed"
	circuitOpen     circuitState = "open"
	circuitHalfOpen circuitState = "half_open"
)

type breaker struct {
	state        circuitState
	failures     int
	openedAt     time.Time
	halfOpenBusy bool
}

func (b *breaker) permit(now time.Time, reset time.Duration) bool {
	if b.state == circuitOpen {
		if now.Before(b.openedAt.Add(reset)) {
			return false
		}
		b.state, b.halfOpenBusy = circuitHalfOpen, false
	}
	if b.state == circuitHalfOpen {
		if b.halfOpenBusy {
			return false
		}
		b.halfOpenBusy = true
	}
	return true
}
func (b *breaker) success() { b.state, b.failures, b.halfOpenBusy = circuitClosed, 0, false }
func (b *breaker) failure(now time.Time, limit int) {
	b.halfOpenBusy = false
	b.failures++
	if b.failures >= limit {
		b.state, b.openedAt = circuitOpen, now
	}
}
