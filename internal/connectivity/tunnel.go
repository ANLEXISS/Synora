package connectivity

import "context"

type PeerConfig struct {
	PeerID         string
	WireGuardKey   string
	VirtualAddress string
}

type TunnelStatus struct {
	InterfacePresent bool
	PeerCount        int
}

type TunnelController interface {
	EnsureInterface(context.Context, InterfaceConfig) error
	ConfigureIdentity(context.Context, WireGuardIdentity) error
	ReplacePeers(context.Context, []PeerConfig) error
	Status(context.Context) (TunnelStatus, error)
	RemoveInterface(context.Context) error
}

// NoopTunnelController is the only production implementation in this pass.
// It deliberately never invokes netlink, WireGuard, routes, or capabilities.
type NoopTunnelController struct{}

func (NoopTunnelController) EnsureInterface(context.Context, InterfaceConfig) error     { return nil }
func (NoopTunnelController) ConfigureIdentity(context.Context, WireGuardIdentity) error { return nil }
func (NoopTunnelController) ReplacePeers(context.Context, []PeerConfig) error           { return nil }
func (NoopTunnelController) Status(context.Context) (TunnelStatus, error)               { return TunnelStatus{}, nil }
func (NoopTunnelController) RemoveInterface(context.Context) error                      { return nil }

type MemoryTunnelController struct {
	EnsureCalls, IdentityCalls, PeerCalls, RemoveCalls int
	StatusValue                                        TunnelStatus
	Err                                                error
}

func (m *MemoryTunnelController) EnsureInterface(context.Context, InterfaceConfig) error {
	m.EnsureCalls++
	return m.Err
}
func (m *MemoryTunnelController) ConfigureIdentity(context.Context, WireGuardIdentity) error {
	m.IdentityCalls++
	return m.Err
}
func (m *MemoryTunnelController) ReplacePeers(context.Context, []PeerConfig) error {
	m.PeerCalls++
	return m.Err
}
func (m *MemoryTunnelController) Status(context.Context) (TunnelStatus, error) {
	return m.StatusValue, m.Err
}
func (m *MemoryTunnelController) RemoveInterface(context.Context) error {
	m.RemoveCalls++
	return m.Err
}
