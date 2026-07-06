package cognitive

import (
	"time"

	"synora/internal/engine/contracts"
)

func (m *SequenceManager) ExpireSequences(
	now time.Time,
) []*contracts.ActiveSequence {

	m.Mu.Lock()
	defer m.Mu.Unlock()

	expired :=
		make(
			[]*contracts.ActiveSequence,
			0,
		)

	for _, seq := range m.Sequences {

		if seq.Closed {
			continue
		}

		if now.Sub(
			seq.LastUpdate,
		) < m.Timeout {

			continue
		}

		seq.Closed = true

		expired = append(
			expired,
			seq,
		)
	}

	return expired
}

func (m *SequenceManager) HasExpired(
	seq *contracts.ActiveSequence,
	now time.Time,
) bool {

	if seq == nil {
		return false
	}

	if seq.Closed {
		return true
	}

	return now.Sub(
		seq.LastUpdate,
	) >= m.Timeout
}

func (m *SequenceManager) CloseSequence(
	subjectID string,
) (*contracts.ActiveSequence, bool) {

	m.Mu.Lock()
	defer m.Mu.Unlock()

	seq, ok :=
		m.Sequences[subjectID]

	if !ok {
		return nil, false
	}

	if seq.Closed {
		return seq, true
	}

	seq.Closed = true

	return seq, true
}

func (m *SequenceManager) DeleteSequence(
	subjectID string,
) {

	m.Mu.Lock()
	defer m.Mu.Unlock()

	delete(
		m.Sequences,
		subjectID,
	)
}

func (m *SequenceManager) ActiveCount() int {

	m.Mu.RLock()
	defer m.Mu.RUnlock()

	count := 0

	for _, seq := range m.Sequences {

		if !seq.Closed {
			count++
		}
	}

	return count
}

func (m *SequenceManager) ClosedCount() int {

	m.Mu.RLock()
	defer m.Mu.RUnlock()

	count := 0

	for _, seq := range m.Sequences {

		if seq.Closed {
			count++
		}
	}

	return count
}