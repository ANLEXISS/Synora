// Package resourcebudget centralizes the fixed V1 resource budgets.
//
// These are safety limits, not business policy and not a source of truth for
// security decisions. Callers must refuse work cleanly when a budget is
// exhausted so an untrusted producer cannot turn pressure into an OOM or a
// starvation of critical work.
package resourcebudget

import "errors"

const (
	MaxMessageBytes = 1 << 20

	CoreHighPriorityQueue = 128
	CoreNormalQueue       = 512
	CoreRPCQueue          = 256
	BusIncomingQueue      = 100

	MaxVisionWorkers = 3
	MaxVisionQueue   = 512

	MaxWebSocketClients = 64
	WebSocketQueue      = 64
	WebSocketReplay     = 256

	MaxRateControllerEntries = 4096
	MaxConcurrentClipUploads = 3
	MaxClipUploadsPerCamera  = 1
)

// Limits is exported for diagnostics and deterministic validation tests. The
// runtime currently uses the fixed defaults above; there is no V1 API to
// change them at runtime.
type Limits struct {
	MaxMessageBytes          int
	CoreHighPriorityQueue    int
	CoreNormalQueue          int
	CoreRPCQueue             int
	BusIncomingQueue         int
	MaxVisionWorkers         int
	MaxVisionQueue           int
	MaxWebSocketClients      int
	WebSocketQueue           int
	WebSocketReplay          int
	MaxRateControllerEntries int
	MaxConcurrentClipUploads int
	MaxClipUploadsPerCamera  int
}

func Defaults() Limits {
	return Limits{
		MaxMessageBytes:          MaxMessageBytes,
		CoreHighPriorityQueue:    CoreHighPriorityQueue,
		CoreNormalQueue:          CoreNormalQueue,
		CoreRPCQueue:             CoreRPCQueue,
		BusIncomingQueue:         BusIncomingQueue,
		MaxVisionWorkers:         MaxVisionWorkers,
		MaxVisionQueue:           MaxVisionQueue,
		MaxWebSocketClients:      MaxWebSocketClients,
		WebSocketQueue:           WebSocketQueue,
		WebSocketReplay:          WebSocketReplay,
		MaxRateControllerEntries: MaxRateControllerEntries,
		MaxConcurrentClipUploads: MaxConcurrentClipUploads,
		MaxClipUploadsPerCamera:  MaxClipUploadsPerCamera,
	}
}

func (l Limits) Validate() error {
	if l.MaxMessageBytes <= 0 || l.CoreHighPriorityQueue <= 0 || l.CoreNormalQueue <= 0 || l.CoreRPCQueue <= 0 ||
		l.BusIncomingQueue <= 0 || l.MaxVisionWorkers <= 0 || l.MaxVisionQueue <= 0 || l.MaxWebSocketClients <= 0 ||
		l.WebSocketQueue <= 0 || l.WebSocketReplay <= 0 || l.MaxRateControllerEntries <= 0 ||
		l.MaxConcurrentClipUploads <= 0 || l.MaxClipUploadsPerCamera <= 0 {
		return errors.New("resource budgets must be positive")
	}
	if l.MaxClipUploadsPerCamera > l.MaxConcurrentClipUploads {
		return errors.New("per-camera upload budget exceeds global budget")
	}
	return nil
}
