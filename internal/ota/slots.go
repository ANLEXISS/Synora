package ota

import "errors"

type Slot string

const (
	SlotA Slot = "A"
	SlotB Slot = "B"
)

// SlotManager is a deterministic model of the RAUC/bootloader A/B contract.
// It models only slot state; persistent Synora data is deliberately outside
// this type and therefore cannot be erased by rollback.
type SlotManager struct {
	Active   Slot
	Pending  Slot
	Attempts map[Slot]int
	MaxAttempts int
}

func NewSlotManager(active Slot) *SlotManager {
	if active != SlotA && active != SlotB {
		active = SlotA
	}
	return &SlotManager{Active: active, Attempts: map[Slot]int{SlotA: 0, SlotB: 0}, MaxAttempts: 3}
}

func (s *SlotManager) Activate(candidate Slot) error {
	if s == nil || (candidate != SlotA && candidate != SlotB) {
		return errors.New("invalid OTA slot")
	}
	if candidate == s.Active || s.Pending != "" {
		return errors.New("OTA slot is not available for activation")
	}
	s.Pending = candidate
	s.Attempts[candidate] = 0
	return nil
}

// BootAttempt returns the pending slot while attempts remain and the last
// healthy slot after the bootloader threshold is exhausted.
func (s *SlotManager) BootAttempt() (Slot, error) {
	if s == nil {
		return "", errors.New("OTA slot manager unavailable")
	}
	if s.Pending == "" {
		return s.Active, nil
	}
	s.Attempts[s.Pending]++
	if s.Attempts[s.Pending] > s.MaxAttempts {
		s.Pending = ""
		return s.Active, nil
	}
	return s.Pending, nil
}

func (s *SlotManager) MarkGood(slot Slot) error {
	if s == nil || s.Pending != slot {
		return errors.New("slot is not pending")
	}
	s.Active = slot
	s.Pending = ""
	return nil
}

func (s *SlotManager) MarkBad(slot Slot) error {
	if s == nil || s.Pending != slot {
		return errors.New("slot is not pending")
	}
	s.Pending = ""
	return nil
}
