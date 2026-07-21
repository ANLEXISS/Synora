package calibrationledger

import "errors"

var (
	ErrInvalidPolicy               = errors.New("calibration ledger: invalid policy")
	ErrInvalidRecord               = errors.New("calibration ledger: invalid record")
	ErrInvalidMarkers              = errors.New("calibration ledger: invalid markers")
	ErrRecordTooLarge              = errors.New("calibration ledger: record too large")
	ErrLedgerLimitReached          = errors.New("calibration ledger: ledger limit reached")
	ErrInvalidGenesis              = errors.New("calibration ledger: invalid genesis")
	ErrInvalidEnvelope             = errors.New("calibration ledger: invalid envelope")
	ErrInvalidSequence             = errors.New("calibration ledger: invalid sequence")
	ErrSequenceGap                 = errors.New("calibration ledger: sequence gap")
	ErrDuplicateSequence           = errors.New("calibration ledger: duplicate sequence")
	ErrHashChainMismatch           = errors.New("calibration ledger: hash chain mismatch")
	ErrRecordFingerprintMismatch   = errors.New("calibration ledger: record fingerprint mismatch")
	ErrEnvelopeFingerprintMismatch = errors.New("calibration ledger: envelope fingerprint mismatch")
	ErrDuplicateComparisonConflict = errors.New("calibration ledger: duplicate comparison conflict")
	ErrTrailingRecordTruncated     = errors.New("calibration ledger: trailing record truncated")
	ErrMidFileCorruption           = errors.New("calibration ledger: mid-file corruption")
	ErrUnsupportedSchema           = errors.New("calibration ledger: unsupported schema")
	ErrRecoveryFailed              = errors.New("calibration ledger: recovery failed")
	ErrRepairDisabled              = errors.New("calibration ledger: repair disabled")
	ErrRepairFailed                = errors.New("calibration ledger: repair failed")
	ErrStoreClosed                 = errors.New("calibration ledger: store closed")
	ErrAppendFailed                = errors.New("calibration ledger: append failed")
	ErrSyncFailed                  = errors.New("calibration ledger: sync failed")
	ErrInvalidQuery                = errors.New("calibration ledger: invalid query")
	ErrQueryLimitExceeded          = errors.New("calibration ledger: query limit exceeded")
	ErrSnapshotUnavailable         = errors.New("calibration ledger: snapshot unavailable")
)

func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTrailingRecordTruncated):
		return "trailing_record_truncated"
	case errors.Is(err, ErrMidFileCorruption):
		return "mid_file_corruption"
	case errors.Is(err, ErrHashChainMismatch):
		return "hash_chain_mismatch"
	case errors.Is(err, ErrRecordFingerprintMismatch):
		return "record_fingerprint_mismatch"
	case errors.Is(err, ErrEnvelopeFingerprintMismatch):
		return "envelope_fingerprint_mismatch"
	case errors.Is(err, ErrUnsupportedSchema):
		return "unsupported_schema"
	case errors.Is(err, ErrLedgerLimitReached):
		return "ledger_limit_reached"
	case errors.Is(err, ErrRecordTooLarge):
		return "record_too_large"
	case errors.Is(err, ErrSyncFailed):
		return "sync_failed"
	case errors.Is(err, ErrAppendFailed):
		return "append_failed"
	default:
		return "ledger_error"
	}
}
