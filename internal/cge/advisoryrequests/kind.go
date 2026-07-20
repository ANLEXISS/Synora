package advisoryrequests

import "strings"

type AdvisoryRequestKey string
type AdvisoryRequestID string

type AdvisoryRequestStatus string

const (
	StatusProposed     AdvisoryRequestStatus = "proposed"
	StatusAcknowledged AdvisoryRequestStatus = "acknowledged"
	StatusDeferred     AdvisoryRequestStatus = "deferred"
	StatusSuppressed   AdvisoryRequestStatus = "suppressed"
	StatusSatisfied    AdvisoryRequestStatus = "satisfied"
	StatusExpired      AdvisoryRequestStatus = "expired"
	StatusCancelled    AdvisoryRequestStatus = "cancelled"
	StatusInvalidated  AdvisoryRequestStatus = "invalidated"
)

type AdvisoryDispositionKind string

const (
	DispositionAcknowledge     AdvisoryDispositionKind = "acknowledge"
	DispositionDefer           AdvisoryDispositionKind = "defer"
	DispositionCancel          AdvisoryDispositionKind = "cancel"
	DispositionRestoreProposal AdvisoryDispositionKind = "restore_proposal"
)

type AdvisoryRequestFlags struct {
	NotACommand                   bool
	NotAProbability               bool
	NoSecurityMeaning             bool
	RequiresExternalMapping       bool
	RequiresExternalAuthorization bool
}

type AdvisoryHypothesisPair struct {
	FirstID  string
	SecondID string
}

func canonicalPair(first, second string) (AdvisoryHypothesisPair, bool) {
	if first == "" || second == "" || first == second {
		return AdvisoryHypothesisPair{}, false
	}
	if second < first {
		first, second = second, first
	}
	return AdvisoryHypothesisPair{FirstID: first, SecondID: second}, true
}

func validStatus(value AdvisoryRequestStatus) bool {
	switch value {
	case StatusProposed, StatusAcknowledged, StatusDeferred, StatusSuppressed, StatusSatisfied, StatusExpired, StatusCancelled, StatusInvalidated:
		return true
	default:
		return false
	}
}

func terminal(value AdvisoryRequestStatus) bool {
	return value == StatusSatisfied || value == StatusExpired || value == StatusCancelled || value == StatusInvalidated
}

func active(value AdvisoryRequestStatus) bool {
	return value == StatusProposed || value == StatusAcknowledged || value == StatusDeferred
}

func forbiddenTerm(value string) bool {
	lowered := strings.ToLower(value)
	for _, term := range []string{"execute", "capture", "probe", "scan", "block", "quarantine", "lock", "alarm", "intrusion", "threat", "danger", "malicious", "attack", "weapon", "safe", "unsafe", "authorized_execution"} {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}

func validDisposition(value AdvisoryDispositionKind) bool {
	return value == DispositionAcknowledge || value == DispositionDefer || value == DispositionCancel || value == DispositionRestoreProposal
}
