package cge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"synora/internal/cge/shadowworkflow"
	"synora/pkg/contract"
)

const shadowAdmissionPolicyPrefix = "shadow-event-admission-policy-v1:"

var ErrInvalidShadowAdmissionPolicy = errors.New("invalid_shadow_event_admission_policy")

// ShadowAdmissionCode is a closed vocabulary for the Core-to-Shadow
// admission boundary. It contains no event-specific failure text.
type ShadowAdmissionCode string

const (
	ShadowAdmissionAccepted        ShadowAdmissionCode = "accepted"
	ShadowAdmissionIgnoredByPolicy ShadowAdmissionCode = "ignored_by_policy"
	ShadowAdmissionInvalid         ShadowAdmissionCode = "invalid"
	ShadowAdmissionQueueFull       ShadowAdmissionCode = "queue_full"
	ShadowAdmissionStopping        ShadowAdmissionCode = "stopping"
	ShadowAdmissionStopped         ShadowAdmissionCode = "stopped"
	ShadowAdmissionDisabled        ShadowAdmissionCode = "workflow_disabled"
	ShadowAdmissionUnavailable     ShadowAdmissionCode = "workflow_unavailable"
)

func (c ShadowAdmissionCode) Valid() bool {
	switch c {
	case ShadowAdmissionAccepted, ShadowAdmissionIgnoredByPolicy, ShadowAdmissionInvalid,
		ShadowAdmissionQueueFull, ShadowAdmissionStopping, ShadowAdmissionStopped,
		ShadowAdmissionDisabled, ShadowAdmissionUnavailable:
		return true
	default:
		return false
	}
}

// ShadowEventAdmissionPolicy is an immutable, canonical allowlist. Its
// internal set and ordered values are never exposed to callers.
type ShadowEventAdmissionPolicy struct {
	ordered     []string
	allowed     map[string]struct{}
	fingerprint string
}

// NewShadowEventAdmissionPolicy validates, deduplicates strictly, and
// canonicalizes a policy. Only event types already defined by contract are
// accepted; this prevents an arbitrary event name from becoming admissible.
func NewShadowEventAdmissionPolicy(eventTypes []string) (ShadowEventAdmissionPolicy, error) {
	if len(eventTypes) == 0 {
		return ShadowEventAdmissionPolicy{}, fmt.Errorf("%w: empty", ErrInvalidShadowAdmissionPolicy)
	}
	known := knownShadowEventTypes()
	seen := make(map[string]struct{}, len(eventTypes))
	ordered := make([]string, 0, len(eventTypes))
	for _, raw := range eventTypes {
		eventType := strings.ToLower(strings.TrimSpace(raw))
		if eventType == "" {
			return ShadowEventAdmissionPolicy{}, fmt.Errorf("%w: empty event type", ErrInvalidShadowAdmissionPolicy)
		}
		if _, ok := known[eventType]; !ok {
			return ShadowEventAdmissionPolicy{}, fmt.Errorf("%w: unknown event type", ErrInvalidShadowAdmissionPolicy)
		}
		if _, duplicate := seen[eventType]; duplicate {
			return ShadowEventAdmissionPolicy{}, fmt.Errorf("%w: duplicate event type", ErrInvalidShadowAdmissionPolicy)
		}
		seen[eventType] = struct{}{}
		ordered = append(ordered, eventType)
	}
	sort.Strings(ordered)
	canonical := strings.Join(ordered, "\n")
	digest := sha256.Sum256([]byte(canonical))
	allowed := make(map[string]struct{}, len(ordered))
	for _, eventType := range ordered {
		allowed[eventType] = struct{}{}
	}
	return ShadowEventAdmissionPolicy{ordered: ordered, allowed: allowed, fingerprint: shadowAdmissionPolicyPrefix + hex.EncodeToString(digest[:])}, nil
}

// DefaultShadowEventAdmissionPolicy returns the unchanged three-event policy
// from the previous pass.
func DefaultShadowEventAdmissionPolicy() ShadowEventAdmissionPolicy {
	policy, err := NewShadowEventAdmissionPolicy([]string{contract.EventVisionIdentity, contract.EventVisionUnknown, contract.EventVisionUncertain})
	if err != nil {
		panic(err)
	}
	return policy
}

// DefaultEligibleEventTypes preserves the existing configuration API while
// sourcing its values from the explicit immutable policy.
func DefaultEligibleEventTypes() []string {
	return DefaultShadowEventAdmissionPolicy().AllowedEventTypes()
}

func (p ShadowEventAdmissionPolicy) AllowedEventTypes() []string {
	return append([]string(nil), p.ordered...)
}

func (p ShadowEventAdmissionPolicy) Fingerprint() string { return p.fingerprint }

func (p ShadowEventAdmissionPolicy) allows(eventType string) bool {
	_, ok := p.allowed[eventType]
	return ok
}

func admissionEventType(event Event) string {
	eventType := contract.NormalizeEventType(event.Type)
	if eventType == "" {
		return contract.EventSystemUnknown
	}
	return eventType
}

// ShadowAdmissionResult is a redacted, aggregate-only result. It deliberately
// carries no event, device, identity, clip, payload, or source fingerprint.
type ShadowAdmissionResult struct {
	Code ShadowAdmissionCode

	EventType string

	Eligible  bool
	Adapted   bool
	Submitted bool

	HistoricalAuthorityUnchanged bool
	NoActionProduced             bool
}

func (r ShadowAdmissionResult) Validate() error {
	if !r.Code.Valid() {
		return fmt.Errorf("%w: invalid admission code", ErrInvalidShadowAdmissionPolicy)
	}
	if len([]rune(r.EventType)) > 128 || strings.ContainsAny(r.EventType, "\r\n") {
		return fmt.Errorf("%w: invalid event type", ErrInvalidShadowAdmissionPolicy)
	}
	if r.Code == ShadowAdmissionAccepted && (!r.Eligible || !r.Adapted || !r.Submitted) {
		return fmt.Errorf("%w: accepted result is incomplete", ErrInvalidShadowAdmissionPolicy)
	}
	if r.Submitted && r.Code != ShadowAdmissionAccepted {
		return fmt.Errorf("%w: rejected result submitted", ErrInvalidShadowAdmissionPolicy)
	}
	if !r.HistoricalAuthorityUnchanged || !r.NoActionProduced {
		return fmt.Errorf("%w: authority markers missing", ErrInvalidShadowAdmissionPolicy)
	}
	return nil
}

// ShadowAdmissionStatus is a defensive aggregate snapshot. LastEventType is
// normalized and bounded; it is not an event identifier.
type ShadowAdmissionStatus struct {
	LastCode      ShadowAdmissionCode
	LastEventType string
	LastEligible  bool
	LastAdapted   bool
	LastSubmitted bool

	HistoricalAuthorityUnchanged bool
	NoActionProduced             bool

	AcceptedTotal        uint64
	IgnoredByPolicyTotal uint64
	InvalidTotal         uint64
	QueueFullTotal       uint64
	StoppingTotal        uint64
	StoppedTotal         uint64
	DisabledTotal        uint64
	UnavailableTotal     uint64
}

func (s ShadowAdmissionStatus) clone() ShadowAdmissionStatus { return s }

func (s *ShadowAdmissionStatus) record(result ShadowAdmissionResult) {
	s.LastCode = result.Code
	s.LastEventType = result.EventType
	s.LastEligible = result.Eligible
	s.LastAdapted = result.Adapted
	s.LastSubmitted = result.Submitted
	s.HistoricalAuthorityUnchanged = result.HistoricalAuthorityUnchanged
	s.NoActionProduced = result.NoActionProduced
	switch result.Code {
	case ShadowAdmissionAccepted:
		s.AcceptedTotal++
	case ShadowAdmissionIgnoredByPolicy:
		s.IgnoredByPolicyTotal++
	case ShadowAdmissionInvalid:
		s.InvalidTotal++
	case ShadowAdmissionQueueFull:
		s.QueueFullTotal++
	case ShadowAdmissionStopping:
		s.StoppingTotal++
	case ShadowAdmissionStopped:
		s.StoppedTotal++
	case ShadowAdmissionDisabled:
		s.DisabledTotal++
	case ShadowAdmissionUnavailable:
		s.UnavailableTotal++
	}
}

func admissionMetricName(code ShadowAdmissionCode) string {
	return "cge_shadow_admission_" + string(code) + "_total"
}

func (s ShadowAdmissionStatus) Metrics() map[string]uint64 {
	return map[string]uint64{
		admissionMetricName(ShadowAdmissionAccepted):        s.AcceptedTotal,
		admissionMetricName(ShadowAdmissionIgnoredByPolicy): s.IgnoredByPolicyTotal,
		admissionMetricName(ShadowAdmissionInvalid):         s.InvalidTotal,
		admissionMetricName(ShadowAdmissionQueueFull):       s.QueueFullTotal,
		admissionMetricName(ShadowAdmissionStopping):        s.StoppingTotal,
		admissionMetricName(ShadowAdmissionStopped):         s.StoppedTotal,
		admissionMetricName(ShadowAdmissionDisabled):        s.DisabledTotal,
		admissionMetricName(ShadowAdmissionUnavailable):     s.UnavailableTotal,
	}
}

type ShadowEventDisposition string

const (
	ShadowEventAdmitted        ShadowEventDisposition = "shadow_admitted"
	ShadowEventHistoricalOnly  ShadowEventDisposition = "historical_only"
	ShadowEventIgnoredByDesign ShadowEventDisposition = "ignored_by_design"
	ShadowEventUnsupported     ShadowEventDisposition = "unsupported"
	ShadowEventUnknown         ShadowEventDisposition = "unknown"
)

type ShadowEventAdmissionMatrixEntry struct {
	EventType   string
	Disposition ShadowEventDisposition
}

// DefaultShadowEventAdmissionMatrix documents every contract event type
// known in this repository without changing the executable default policy.
func DefaultShadowEventAdmissionMatrix() []ShadowEventAdmissionMatrixEntry {
	admitted := map[string]ShadowEventDisposition{
		contract.EventVisionIdentity:   ShadowEventAdmitted,
		contract.EventVisionUnknown:    ShadowEventAdmitted,
		contract.EventVisionUncertain:  ShadowEventAdmitted,
		contract.EventActionRequest:    ShadowEventIgnoredByDesign,
		contract.EventActionResult:     ShadowEventIgnoredByDesign,
		contract.EventAutomationAction: ShadowEventIgnoredByDesign,
		contract.EventSystemUnknown:    ShadowEventUnknown,
	}
	known := knownShadowEventTypes()
	keys := make([]string, 0, len(known))
	for eventType := range known {
		keys = append(keys, eventType)
	}
	sort.Strings(keys)
	out := make([]ShadowEventAdmissionMatrixEntry, 0, len(keys))
	for _, eventType := range keys {
		disposition := ShadowEventHistoricalOnly
		if value, ok := admitted[eventType]; ok {
			disposition = value
		}
		out = append(out, ShadowEventAdmissionMatrixEntry{EventType: eventType, Disposition: disposition})
	}
	return out
}

func knownShadowEventTypes() map[string]struct{} {
	values := []string{
		contract.EventVisionIdentity, contract.EventVisionUnknown, contract.EventVisionUncertain,
		contract.EventVisionMotion, contract.EventVisionWeapon, contract.EventVisionFall,
		contract.EventVisionFight, contract.EventVisionTamper, contract.EventDeviceTrigger,
		contract.EventDeviceOffline, contract.EventDiscoveryCameraOnline, contract.EventDiscoveryCameraOffline,
		contract.EventDiscoveryWorkerStarted, contract.EventDiscoveryWorkerStopped, contract.EventDiscoveryWorkerCrashed,
		contract.EventDiscoveryVisionWorkerUnavailable, contract.EventDiscoveryNetworkDegraded,
		contract.EventDiscoveryVisionIngressStatus, contract.EventDiscoveryRuntimeStatus,
		contract.EventRuntimeComponentFlapping, contract.EventRuntimeModelMissing,
		contract.EventSystemStateChanged, contract.EventSystemPresence, contract.EventSystemUnknown,
		contract.EventActionRequest, contract.EventActionResult, contract.EventActionServiceStarted,
		contract.EventAutomationAction, contract.EventManualRisk, contract.EventSystemStateReset,
		contract.EventSecurityModeChanged,
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func mapSubmitStatus(status shadowworkflow.SubmitStatus, runtimeState shadowworkflow.RuntimeState) ShadowAdmissionCode {
	switch status {
	case shadowworkflow.SubmitAccepted:
		return ShadowAdmissionAccepted
	case shadowworkflow.SubmitQueueFull:
		return ShadowAdmissionQueueFull
	case shadowworkflow.SubmitStopped:
		if runtimeState == shadowworkflow.StateStopping {
			return ShadowAdmissionStopping
		}
		if runtimeState == shadowworkflow.StateStarting || runtimeState == shadowworkflow.StateRecovering || runtimeState == shadowworkflow.StateRecoveryFailed {
			return ShadowAdmissionUnavailable
		}
		return ShadowAdmissionStopped
	case shadowworkflow.SubmitDisabled:
		return ShadowAdmissionDisabled
	case shadowworkflow.SubmitRejected:
		return ShadowAdmissionInvalid
	case shadowworkflow.SubmitCircuitOpen, shadowworkflow.SubmitStorageLimit:
		return ShadowAdmissionUnavailable
	default:
		return ShadowAdmissionUnavailable
	}
}
