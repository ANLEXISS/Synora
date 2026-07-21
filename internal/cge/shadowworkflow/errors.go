package shadowworkflow

import "errors"

var (
	ErrInvalidConfig                = errors.New("shadowworkflow: invalid config")
	ErrDisabled                     = errors.New("shadowworkflow: disabled")
	ErrStopped                      = errors.New("shadowworkflow: stopped")
	ErrQueueFull                    = errors.New("shadowworkflow: queue full")
	ErrInputRejected                = errors.New("shadowworkflow: input rejected")
	ErrInputTooOld                  = errors.New("shadowworkflow: input too old")
	ErrPipelineTimeout              = errors.New("shadowworkflow: pipeline timeout")
	ErrPipelineStageFailed          = errors.New("shadowworkflow: pipeline stage failed")
	ErrProviderUnavailable          = errors.New("shadowworkflow: provider unavailable")
	ErrProviderInvalid              = errors.New("shadowworkflow: provider invalid")
	ErrQuotaExceeded                = errors.New("shadowworkflow: quota exceeded")
	ErrWALSizeLimit                 = errors.New("shadowworkflow: wal size limit")
	ErrCircuitOpen                  = errors.New("shadowworkflow: circuit open")
	ErrCircuitHalfOpenBusy          = errors.New("shadowworkflow: circuit half open busy")
	ErrRecoveryFailed               = errors.New("shadowworkflow: recovery failed")
	ErrCheckpointFailed             = errors.New("shadowworkflow: checkpoint failed")
	ErrDurableCommitFailed          = errors.New("shadowworkflow: durable commit failed")
	ErrHistoricalIsolationViolation = errors.New("shadowworkflow: historical isolation violation")
	ErrShutdownTimeout              = errors.New("shadowworkflow: shutdown timeout")
	ErrPanicRecovered               = errors.New("shadowworkflow: panic recovered")
	ErrQualificationInvalidSample   = errors.New("shadowworkflow: qualification invalid sample")
	ErrQualificationOutputLimit     = errors.New("shadowworkflow: qualification output limit")
)
