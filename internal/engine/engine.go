package engine

import (
	"time"

	"synora/internal/device"
	"synora/internal/engine/adapter"
	"synora/internal/engine/cognitive"
	"synora/internal/engine/graph"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type Engine struct {
	Topology *topology.Topology
	device   *device.Registry

	graphMemory *graph.GraphMemory
	cognitive   *cognitive.Engine
}

func NewEngine(
	topo *topology.Topology,
	registry *device.Registry,
	_ map[string]*topology.Resident,
) *Engine {
	memory := graph.NewGraphMemory()
	return &Engine{
		Topology:    topo,
		device:      registry,
		graphMemory: memory,
		cognitive:   cognitive.NewEngine(memory),
	}
}

func (e *Engine) Analyze(
	event *contract.Event,
	store *state.Store,
) *Result {
	if event == nil {
		return nil
	}
	if store == nil {
		store = state.NewStore()
	}

	now := adapter.NormalizeEvent(event, e.device)
	cgeEvent := adapter.ToCGEEvent(event, store, now)

	e.graphMemory.LearnEvent(cgeEvent)
	decisionResult := e.cognitive.ProcessEvent(cgeEvent)

	return adapter.BuildResult(event, store, decisionResult, now)
}

func (e *Engine) Process(
	event *contract.Event,
	stores ...*state.Store,
) *contract.Decision {
	var store *state.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	result := e.Analyze(event, store)
	if result == nil {
		return nil
	}
	return result.Decision
}

func (e *Engine) StartDecayLoop() {}

func (e *Engine) ResetIntrusion(stores ...*state.Store) {
	if len(stores) == 0 || stores[0] == nil {
		return
	}
	current := stores[0].SystemState()
	current.LastState = "idle"
	current.LastStateTime = time.Now().UTC()
	current.IntrusionActive = false
	current.IntrusionTime = time.Time{}
	stores[0].SetSystemState(current)
}
