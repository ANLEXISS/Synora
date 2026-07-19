package context

import "errors"

var (
	ErrInvalidSchema       = errors.New("invalid_context_schema")
	ErrInvalidTopology     = errors.New("invalid_context_topology")
	ErrUnknownNode         = errors.New("context_topology_unknown_node")
	ErrInvalidTimezone     = errors.New("invalid_context_timezone")
	ErrInvalidFrame        = errors.New("invalid_context_frame")
	ErrUnknownDistance     = errors.New("unknown_context_distance")
	ErrUnreachableDistance = errors.New("unreachable_context_distance")
)
