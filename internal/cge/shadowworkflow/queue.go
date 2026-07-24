package shadowworkflow

type SubmitStatus string

const (
	SubmitAccepted     SubmitStatus = "accepted"
	SubmitDisabled     SubmitStatus = "disabled"
	SubmitQueueFull    SubmitStatus = "queue_full"
	SubmitRejected     SubmitStatus = "rejected"
	SubmitStopped      SubmitStatus = "stopped"
	SubmitCircuitOpen  SubmitStatus = "circuit_open"
	SubmitStorageLimit SubmitStatus = "storage_limit_reached"
)

type SubmitResult struct {
	Status     SubmitStatus
	ReasonCode string
}

type ShadowObservationSink interface {
	TrySubmit(ShadowWorkflowInput) SubmitResult
}

var _ ShadowObservationSink = (*Runtime)(nil)
