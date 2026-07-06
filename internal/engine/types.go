package engine

import "synora/internal/engine/adapter"

const (
	StateAbsent   = adapter.StateAbsent
	StateEntering = adapter.StateEntering
	StatePresent  = adapter.StatePresent
	StateLeaving  = adapter.StateLeaving
)

type Result = adapter.Result
