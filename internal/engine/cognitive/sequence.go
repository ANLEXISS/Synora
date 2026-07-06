package cognitive

import (
	"time"
	"sync"
	"synora/internal/engine/contracts"
)

type SequenceManager struct {
	Sequences map[string]*contracts.ActiveSequence

	Timeout time.Duration

	Mu sync.RWMutex
}

func NewSequenceManager(
	timeout time.Duration,
) *SequenceManager {

	return &SequenceManager{
		Sequences: make(
			map[string]*contracts.ActiveSequence,
		),

		Timeout: timeout,
	}
}

func (m *SequenceManager) GetOrCreateSequence(
	subjectID string,
) *contracts.ActiveSequence {

	m.Mu.Lock()
	defer m.Mu.Unlock()

	seq, ok :=
		m.Sequences[subjectID]

	if ok &&
		!seq.Closed {

		return seq
	}

	seq = &contracts.ActiveSequence{
		ID: subjectID +
			"-" +
			time.Now().Format(
				"20060102150405",
			),

		SubjectID: subjectID,

		StartedAt: time.Now(),

		LastUpdate: time.Now(),

		Events: make(
			[]*contracts.Event,
			0,
		),

		CurrentNode: nil,

		Predictions: nil,
	}

	m.Sequences[subjectID] = seq

	return seq
}

func (m *SequenceManager) AddEvent(
	event *contracts.Event,
) *contracts.ActiveSequence {

	seq :=
		m.GetOrCreateSequence(
			event.SubjectID,
		)

	m.Mu.Lock()
	defer m.Mu.Unlock()

	seq.Events =
		append(
			seq.Events,
			event,
		)

	seq.LastUpdate =
		event.Timestamp

	return seq
}