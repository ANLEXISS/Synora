package main

import (
	"fmt"
	"time"

	"synora/internal/recovery"
)

func (a *coreApp) beginRecovery() error {
	if a == nil || a.recovery == nil {
		return nil
	}
	if err := a.recovery.BeginRecovery(); err != nil {
		return err
	}
	return a.syncRecoveryStatus()
}

func (a *coreApp) setRecoveryDependency(name string, healthy bool, reason string) error {
	if a == nil || a.recovery == nil {
		return nil
	}
	if err := a.recovery.SetDependency(name, healthy, reason); err != nil {
		return err
	}
	return a.syncRecoveryStatus()
}

func (a *coreApp) completeRecovery() error {
	if a == nil || a.recovery == nil {
		return nil
	}
	if err := a.recovery.CompleteRecovery(); err != nil {
		_ = a.syncRecoveryStatus()
		return err
	}
	return a.syncRecoveryStatus()
}

func (a *coreApp) failRecovery(reason string) error {
	if a == nil || a.recovery == nil {
		return nil
	}
	if err := a.recovery.Fail(reason); err != nil {
		return err
	}
	return a.syncRecoveryStatus()
}

func (a *coreApp) syncRecoveryStatus() error {
	if a == nil || a.recovery == nil || a.state == nil {
		return nil
	}
	status := a.recovery.Snapshot()
	if err := a.state.SetRecoveryStatus(status); err != nil {
		return fmt.Errorf("persist Core recovery status: %w", err)
	}
	return nil
}

func newCoreRecoveryMachine() (*recovery.Machine, error) {
	return recovery.New([]recovery.Dependency{
		{Name: "state_store", Required: true},
		{Name: "topology", Required: true},
		{Name: "bus", Required: true},
	}, time.Now)
}
