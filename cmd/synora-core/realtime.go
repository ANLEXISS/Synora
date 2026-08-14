package main

import "synora/internal/idgen"

func (a *coreApp) nextRealtimeCursor() (string, uint64) {
	if a == nil {
		return idgen.New("core-epoch"), 1
	}
	a.realtimeMu.Lock()
	if a.realtimeEpoch == "" {
		a.realtimeEpoch = idgen.New("core-epoch")
	}
	epoch := a.realtimeEpoch
	a.realtimeMu.Unlock()
	return epoch, a.realtimeSequence.Add(1)
}

func (a *coreApp) realtimeMetadata() (string, uint64, uint64) {
	epoch, sequence := a.nextRealtimeCursor()
	revision := uint64(0)
	if a != nil && a.state != nil {
		revision = a.state.Revision()
	}
	return epoch, sequence, revision
}
