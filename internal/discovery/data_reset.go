package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"synora/internal/clipstore"
	"synora/pkg/contract"
)

var errDiscoveryResetInProgress = errors.New("discovery data reset already in progress")

// AcquireIngress admits one upload unless a data reset has started. The
// release callback is idempotent so handler error paths cannot leak a lease.
func (m *Manager) AcquireIngress() (func(), bool) {
	if m == nil {
		return func() {}, false
	}
	m.stateMu.Lock()
	if m.stateResetting {
		m.stateMu.Unlock()
		return func() {}, false
	}
	m.activeIngress++
	m.stateMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.stateMu.Lock()
			if m.activeIngress > 0 {
				m.activeIngress--
			}
			m.stateMu.Unlock()
		})
	}, true
}

// ResetData quiesces camera ingress, drains Vision work, and clears all
// Discovery-owned user data while retaining deployment configuration.
func (m *Manager) ResetData(ctx context.Context) error {
	if m == nil {
		return errors.New("discovery manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.stateMu.Lock()
	if m.stateResetting {
		m.stateMu.Unlock()
		return errDiscoveryResetInProgress
	}
	m.stateResetting = true
	m.stateMu.Unlock()
	defer func() {
		m.stateMu.Lock()
		m.stateResetting = false
		m.stateMu.Unlock()
	}()
	for {
		m.stateMu.Lock()
		active := m.activeIngress
		m.stateMu.Unlock()
		if active == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	if m.pool != nil {
		if err := m.pool.ResetData(ctx); err != nil {
			return fmt.Errorf("reset vision queue: %w", err)
		}
	}
	if err := clipstore.EraseAll(m.clipRoot); err != nil {
		return fmt.Errorf("reset clip store: %w", err)
	}
	if m.faceStore != nil {
		if err := m.faceStore.EraseAll(); err != nil {
			return fmt.Errorf("reset face store: %w", err)
		}
	}
	if m.snapshotCache != nil {
		m.snapshotCache.Clear()
	}
	if m.devices != nil {
		m.devices.ClearRuntime()
	}
	m.actionMu.Lock()
	m.actionResults = make(map[string]contract.Event)
	m.actionMu.Unlock()
	return nil
}

func (m *Manager) handleSystemStateReset(message contract.Message) {
	var request contract.SystemStateResetRequest
	if err := json.Unmarshal(message.Payload, &request); err != nil {
		m.sendSystemStateResetError(message, "invalid reset request")
		return
	}
	request.TargetState = strings.TrimSpace(request.TargetState)
	request.Reason = strings.TrimSpace(request.Reason)
	request.CreatedBy = strings.TrimSpace(request.CreatedBy)
	if message.Source != "api" || request.TargetState != "empty" || request.CreatedBy == "" || len(request.Reason) < 8 || len(request.Reason) > 500 {
		m.sendSystemStateResetError(message, "reset request is not authorized or valid")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := m.ResetData(ctx); err != nil {
		m.sendSystemStateResetError(message, "discovery state reset failed")
		return
	}
	body, err := json.Marshal(contract.SystemStateResetResult{Status: "erased", Scope: "discovery", TargetState: "empty", CreatedBy: request.CreatedBy, Reason: request.Reason, ErasedAt: m.clock()})
	if err != nil {
		m.sendSystemStateResetError(message, "discovery reset response failed")
		return
	}
	_ = m.bus.Send(contract.Message{ID: message.ID, Type: contract.RPCSystemResetState, Kind: contract.KindRPC, Source: "discovery", Target: message.Source, CorrelationID: message.ID, Timestamp: m.clock(), Payload: body})
}

func (m *Manager) sendSystemStateResetError(message contract.Message, reason string) {
	if m == nil || m.bus == nil {
		return
	}
	body, _ := json.Marshal(map[string]string{"status": "error", "scope": "discovery", "error": reason})
	_ = m.bus.Send(contract.Message{ID: message.ID, Type: contract.RPCSystemResetState, Kind: contract.KindRPC, Source: "discovery", Target: message.Source, CorrelationID: message.ID, Timestamp: m.clock(), Payload: body})
}
