package connectivity

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"synora/internal/bus"
	"synora/pkg/contract"
	"synora/pkg/contracts"
)

const ServiceName = "connectivity"

type Agent struct {
	cfg      Config
	identity Identity
	state    *StateStore
	tunnel   TunnelController
}

func NewAgent(cfg Config, dataDir string, tunnel TunnelController) (*Agent, error) {
	identity, err := LoadOrGenerateIdentity(dataDir)
	if err != nil {
		return nil, err
	}
	if tunnel == nil {
		tunnel = NoopTunnelController{}
	}
	store := NewStateStore(dataDir)
	if _, err := store.Initialize(cfg, identity); err != nil {
		return nil, err
	}
	return &Agent{cfg: cfg, identity: identity, state: store, tunnel: tunnel}, nil
}

func (a *Agent) Status() contracts.Status {
	if a == nil || a.state == nil {
		return contracts.Status{}
	}
	status, err := a.state.Load()
	if err != nil {
		return a.state.Current()
	}
	return status
}

func (a *Agent) StatusResponse(request contract.Message) (contract.Message, error) {
	if request.Kind != contract.KindRPC || request.Type != "connectivity.status" {
		return contract.Message{}, errors.New("unsupported connectivity RPC")
	}
	payload, err := json.Marshal(a.Status())
	if err != nil {
		return contract.Message{}, errors.New("encode connectivity status")
	}
	return contract.Message{ID: request.ID, Type: request.Type, Kind: contract.KindRPC, Source: ServiceName, Target: request.Source, SourceType: contract.SourceSystem, Timestamp: time.Now().UTC(), Payload: payload}, nil
}

func (a *Agent) Run(ctx context.Context, busPath string) error {
	client, err := bus.NewClient(busPath, ServiceName)
	if err != nil {
		return errors.New("connect to local Synora bus")
	}
	// NewClient uses only the Unix socket. The tunnel controller is intentionally
	// not invoked: this agent has no network interface or route side effects.
	for {
		select {
		case <-ctx.Done():
			return nil
		case message := <-client.SubscribeChannel(ServiceName):
			if message.Kind != contract.KindRPC || message.Type != "connectivity.status" {
				continue
			}
			response, err := a.StatusResponse(message)
			if err != nil {
				continue
			}
			_ = client.Send(response)
		}
	}
}

func (a *Agent) Identity() Identity       { return a.identity }
func (a *Agent) Tunnel() TunnelController { return a.tunnel }
