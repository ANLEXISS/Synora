package ingest

import "sync/atomic"

// AdmissionClass describes overload behavior at the Core ingress boundary.
type AdmissionClass string

const (
	AdmissionCritical   AdmissionClass = "critical"
	AdmissionImportant  AdmissionClass = "important"
	AdmissionBestEffort AdmissionClass = "best_effort"
)

// AdmissionMetrics makes overload decisions observable without retaining
// unbounded per-source state.
type AdmissionMetrics struct {
	acceptedCritical   atomic.Uint64
	rejectedCritical   atomic.Uint64
	acceptedImportant  atomic.Uint64
	rejectedImportant  atomic.Uint64
	acceptedBestEffort atomic.Uint64
	rejectedBestEffort atomic.Uint64
}

func (m *AdmissionMetrics) record(class AdmissionClass, accepted bool) {
	if m == nil {
		return
	}
	switch class {
	case AdmissionCritical:
		if accepted {
			m.acceptedCritical.Add(1)
		} else {
			m.rejectedCritical.Add(1)
		}
	case AdmissionImportant:
		if accepted {
			m.acceptedImportant.Add(1)
		} else {
			m.rejectedImportant.Add(1)
		}
	default:
		if accepted {
			m.acceptedBestEffort.Add(1)
		} else {
			m.rejectedBestEffort.Add(1)
		}
	}
}

func (m *AdmissionMetrics) Snapshot() map[string]uint64 {
	if m == nil {
		return map[string]uint64{}
	}
	return map[string]uint64{
		"accepted_critical": m.acceptedCritical.Load(), "rejected_critical": m.rejectedCritical.Load(),
		"accepted_important": m.acceptedImportant.Load(), "rejected_important": m.rejectedImportant.Load(),
		"accepted_best_effort": m.acceptedBestEffort.Load(), "rejected_best_effort": m.rejectedBestEffort.Load(),
	}
}

func admissionClass(priority int) AdmissionClass {
	if priority >= 100 {
		return AdmissionCritical
	}
	if priority >= 75 {
		return AdmissionImportant
	}
	return AdmissionBestEffort
}
