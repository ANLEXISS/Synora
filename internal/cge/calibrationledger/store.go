package calibrationledger

import "context"

type Store interface {
	Append(context.Context, CalibrationRecord) (AppendResult, error)
	Recover(context.Context) (RecoveryResult, error)
	Snapshot() Snapshot
	Query(Query) (QueryResult, error)
	Close() error
}
