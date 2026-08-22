package contract

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DeliveryState is the durable lifecycle of a message owned by a delivery
// boundary. The states intentionally distinguish a retryable failure from a
// terminal failure and from quarantine; callers must not infer delivery from
// a successful socket write.
type DeliveryState string

const (
	DeliveryPending      DeliveryState = "pending"
	DeliveryInFlight     DeliveryState = "in_flight"
	DeliveryRetryWait    DeliveryState = "retry_wait"
	DeliveryAcknowledged DeliveryState = "acknowledged"
	DeliveryFailed       DeliveryState = "failed_permanent"
	DeliveryQuarantined  DeliveryState = "quarantined"
)

// DeliveryIdentity is the stable cursor used to correlate retries, ACKs and
// replay. A retry keeps the same identity; a new publication gets a new ID or
// sequence rather than being mistaken for the original publication.
type DeliveryIdentity struct {
	ID       string `json:"id"`
	Epoch    string `json:"epoch"`
	Sequence uint64 `json:"sequence"`
	Revision uint64 `json:"revision"`
}

func (i DeliveryIdentity) Validate() error {
	if strings.TrimSpace(i.ID) == "" {
		return errors.New("delivery id is required")
	}
	if strings.TrimSpace(i.Epoch) == "" {
		return errors.New("delivery epoch is required")
	}
	if i.Sequence == 0 {
		return errors.New("delivery sequence must be positive")
	}
	return nil
}

// Key is stable and unambiguous for maps, logs and durable indexes.
func (i DeliveryIdentity) Key() string {
	return fmt.Sprintf("%s/%s/%d/%d", i.Epoch, i.ID, i.Sequence, i.Revision)
}

func (m Message) DeliveryIdentity() DeliveryIdentity {
	return DeliveryIdentity{ID: m.ID, Epoch: m.Epoch, Sequence: m.Sequence, Revision: m.Revision}
}

// DeliveryRecord is the storage representation used by a durable outbox.
// Message remains the wire contract; this wrapper keeps delivery bookkeeping
// out of existing public event payloads.
type DeliveryRecord struct {
	Identity      DeliveryIdentity `json:"identity"`
	Message       Message          `json:"message"`
	State         DeliveryState    `json:"state"`
	Attempts      uint32           `json:"attempts"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	NextAttemptAt time.Time        `json:"next_attempt_at,omitempty"`
	AckedAt       time.Time        `json:"acked_at,omitempty"`
	LastError     string           `json:"last_error,omitempty"`
}

func (r DeliveryRecord) Validate() error {
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	if got := r.Message.DeliveryIdentity(); got != r.Identity {
		return errors.New("delivery identity does not match message")
	}
	if !r.State.Valid() {
		return fmt.Errorf("invalid delivery state %q", r.State)
	}
	if r.Attempts == 0 && r.State != DeliveryPending {
		return errors.New("non-pending delivery must have an attempt")
	}
	return nil
}

func (s DeliveryState) Valid() bool {
	switch s {
	case DeliveryPending, DeliveryInFlight, DeliveryRetryWait,
		DeliveryAcknowledged, DeliveryFailed, DeliveryQuarantined:
		return true
	default:
		return false
	}
}

func (s DeliveryState) Terminal() bool {
	return s == DeliveryAcknowledged || s == DeliveryFailed || s == DeliveryQuarantined
}

// DeliveryAck carries the identity and the outcome observed by the receiver.
// It is deliberately separate from a normal event so an ACK cannot be
// mistaken for business state.
type DeliveryAck struct {
	Identity DeliveryIdentity `json:"identity"`
	State    DeliveryState    `json:"state"`
	Code     string           `json:"code,omitempty"`
	Reason   string           `json:"reason,omitempty"`
}

func (a DeliveryAck) Validate() error {
	if err := a.Identity.Validate(); err != nil {
		return err
	}
	if !a.State.Terminal() {
		return fmt.Errorf("ack state %q is not terminal", a.State)
	}
	return nil
}

// NextState applies only the legal delivery transitions. It prevents an old
// ACK or a late retry from resurrecting a terminal record.
func NextDeliveryState(current DeliveryState, next DeliveryState) error {
	if !current.Valid() || !next.Valid() {
		return errors.New("invalid delivery state transition")
	}
	if current == next {
		return nil
	}
	if current.Terminal() {
		return fmt.Errorf("terminal delivery state %q cannot transition to %q", current, next)
	}
	switch current {
	case DeliveryPending:
		if next == DeliveryInFlight || next == DeliveryQuarantined {
			return nil
		}
	case DeliveryInFlight:
		if next == DeliveryRetryWait || next == DeliveryAcknowledged || next == DeliveryFailed || next == DeliveryQuarantined {
			return nil
		}
	case DeliveryRetryWait:
		if next == DeliveryInFlight || next == DeliveryAcknowledged || next == DeliveryFailed || next == DeliveryQuarantined {
			return nil
		}
	}
	return fmt.Errorf("delivery state %q cannot transition to %q", current, next)
}
