package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	cge "synora/internal/cge"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/deviation"
	"synora/internal/cge/durableids"
)

type demoClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *demoClock) Now() time.Time   { c.mu.RLock(); defer c.mu.RUnlock(); return c.now }
func (c *demoClock) Set(at time.Time) { c.mu.Lock(); c.now = at.UTC(); c.mu.Unlock() }

type demoProvider struct {
	mu        sync.RWMutex
	current   syntheticEvent
	topology  cgecontext.TopologySnapshot
	timezone  string
	available bool
}

func (p *demoProvider) Set(event syntheticEvent) { p.mu.Lock(); p.current = event; p.mu.Unlock() }
func (p *demoProvider) Resolve(ctx context.Context, id string, at time.Time, nodeID string) (cgecontext.Frame, error) {
	if err := ctx.Err(); err != nil {
		return cgecontext.Frame{}, err
	}
	p.mu.RLock()
	current, top, zone, available := p.current, p.topology.Clone(), p.timezone, p.available
	p.mu.RUnlock()
	if current.ID != id && !durableids.IsProtected(id) {
		return cgecontext.Frame{}, errors.New("demo_context_event_mismatch")
	}
	if current.Missing {
		return cgecontext.Frame{}, errors.New("demo_context_missing")
	}
	if current.Partial || !available {
		top = cgecontext.TopologySnapshot{}
	}
	return cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: id, ObservedAt: at, NodeID: nodeID, Timezone: zone, Occupancy: current.Occupancy, HouseMode: current.Mode, Topology: top, AllowPartial: true})
}
func (p *demoProvider) CurrentTopology(ctx context.Context) (cgecontext.TopologySnapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return cgecontext.TopologySnapshot{}, false, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.current.Partial || !p.available {
		return cgecontext.TopologySnapshot{}, false, nil
	}
	return p.topology.Clone(), true, nil
}

type quietLogger struct{}

func (quietLogger) Printf(string, ...any) {}

func Run(ctx context.Context, options Options) (RunResult, error) {
	if options.Scenario == "" {
		options.Scenario = "investor-core"
	}
	if options.Seed == 0 {
		options.Seed = 3501
	}
	if options.Locale == "" {
		options.Locale = "fr"
	}
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	events := buildEvents(options.Seed, options.Scenario, start)
	root := options.RootDir
	cleanup := false
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "synora-cge-demo-")
		if err != nil {
			return RunResult{}, err
		}
		cleanup = true
	}
	if cleanup {
		defer os.RemoveAll(root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return RunResult{}, err
	}
	clock := &demoClock{now: start}
	provider := &demoProvider{topology: topology(start), timezone: "Europe/Paris", available: true}
	open := func(initialize bool) (*cge.ShadowEngine, error) {
		config := cge.DefaultShadowConfig()
		config.Enabled = true
		config.DataDir = root
		config.JournalPath = filepath.Join(root, "journal.ndjson")
		config.InitializeIfMissing = initialize
		config.JournalID = "cge-demo-" + options.Scenario
		config.Context.Enabled = true
		config.Context.Timezone = "Europe/Paris"
		config.Context.AllowPartial = true
		config.Cognitive.Enabled = true
		config.Cognitive.AutoApplyDecisiveEvidence = false
		config.Routines.Enabled = true
		config.Routines.AllowPartialContext = true
		config.Deviation.Enabled = true
		config.Deviation.RecentAssessmentLimit = 256
		config.Deviation.MaxAssessmentsPerObservation = 2
		engine, err := cge.NewShadowEngineWithConfig(ctx, config, clock, quietLogger{})
		if err != nil {
			return nil, err
		}
		engine.SetContextProvider(provider)
		engine.SetRoutineTopologyProvider(provider)
		return engine, nil
	}
	engine, err := open(true)
	if err != nil {
		return RunResult{}, err
	}
	result := RunResult{Scenario: options.Scenario, Seed: options.Seed, StartedAt: time.Now().UTC()}
	seq := uint64(0)
	var replayBefore, replayAfter string
	var replayWall time.Duration
	for _, item := range events {
		if err := ctx.Err(); err != nil {
			_ = engine.Close()
			return result, err
		}
		if item.Restart {
			replayStarted := time.Now()
			before, digestErr := engine.DurableStateDigest(ctx)
			if digestErr != nil {
				_ = engine.Close()
				return result, digestErr
			}
			if err := engine.Close(); err != nil {
				return result, err
			}
			clock.Set(item.At)
			recovered, openErr := open(false)
			if openErr != nil {
				return result, openErr
			}
			after, digestErr := recovered.DurableStateDigest(ctx)
			if digestErr != nil {
				_ = recovered.Close()
				return result, digestErr
			}
			result.Events = append(result.Events, DemoEvent{Sequence: seq + 1, Chapter: "durability", Kind: "replay.completed", At: item.At, Payload: map[string]any{"before": before, "after": after, "equal": before == after, "ephemeral_deviation_store": recovered.SnapshotCount()}})
			replayBefore, replayAfter = before, after
			replayWall = time.Since(replayStarted)
			seq++
			engine = recovered
		}
		provider.Set(item)
		clock.Set(item.At)
		if item.Fixture {
			if err := engine.SeedAssociationAmbiguityFixture(ctx, item.At); err != nil {
				_ = engine.Close()
				return result, err
			}
		}
		before := engine.SnapshotCount()
		_, observeErr := engine.Observe(ctx, cge.Event{ID: item.ID, Type: "vision.identity", Source: "synthetic-demo", Timestamp: item.At, DeviceID: item.DeviceID, NodeID: item.NodeID, Identity: item.Identity, TrackID: item.TrackID, SequenceKey: item.SequenceKey})
		if observeErr != nil {
			return result, fmt.Errorf("observe %s: %w (cause=%v)", item.ID, observeErr, errors.Unwrap(observeErr))
		}
		status := engine.Status()
		orchestration := engine.LastOrchestrationResult()
		snapshot := engine.SnapshotCount()
		payload := map[string]any{"observation_id": item.ID, "label": item.Label, "node_id": item.NodeID, "context_quality": map[bool]string{true: "partial", false: "complete"}[item.Partial], "association_decision": string(orchestration.AssociationDecision), "hypothesis_action": string(orchestration.HypothesisAction), "chain_id": string(orchestration.ChainID), "journal_sequence": status.JournalSequence, "deviation_store_before": before, "deviation_store_after": snapshot, "security_action": false}
		if assessment, ok := engine.LastDeviationAssessmentDetailed(); ok {
			payload["deviation"] = assessmentPayload(assessment)
		}
		seq++
		result.Events = append(result.Events, DemoEvent{Sequence: seq, Chapter: item.Chapter, Kind: "observation.received", At: item.At, Payload: payload})
	}
	result.Snapshot = snapshotFromEngine(ctx, engine)
	result.Snapshot.ReplayDigest = replayAfter
	result.Snapshot.ReplayEqual = replayBefore != "" && replayBefore == replayAfter
	result.Snapshot.Performance = map[string]any{"scenario_wall_ms": time.Since(result.StartedAt).Milliseconds(), "transport_events": len(result.Events), "replay_wall_ms": replayWall.Milliseconds()}
	result.Manifest = Manifest{Scenario: options.Scenario, Seed: options.Seed, PolicyVersions: map[string]string{"association": "association-v1", "evidence": "evidence-v1", "routine": "routine-extraction-v1", "deviation": "deviation-v1", "context": "context-v1"}, CognitiveFingerprint: fingerprint(engine), ExecutedAt: time.Now().UTC().Format(time.RFC3339), SyntheticScenario: true, SyntheticWarning: "synthetic_episode_not_separated", SecurityAuthority: "future", QualificationAvailable: false}
	result.EndedAt = time.Now().UTC()
	if err := engine.Close(); err != nil {
		return result, err
	}
	return result, nil
}

func fingerprint(engine *cge.ShadowEngine) string {
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.Context.Enabled = true
	config.Context.Timezone = "Europe/Paris"
	config.Routines.Enabled = true
	config.Deviation.Enabled = true
	value, err := cge.CognitiveConfigurationFingerprintFor(config)
	if err != nil {
		return ""
	}
	return value.CombinedFingerprint
}

func snapshotFromEngine(ctx context.Context, engine *cge.ShadowEngine) Snapshot {
	status := engine.Status()
	digest, _ := engine.DurableStateDigest(ctx)
	snap, _ := engine.Snapshot(ctx)
	chainsValue, hypothesisValue, routinesValue := engine.ListChains(), engine.ListHypotheses(), engine.ListRoutines()
	var deviationValue any
	if value, ok := engine.LastDeviationAssessmentDetailed(); ok {
		deviationValue = assessmentPayload(value)
	}
	return Snapshot{ObservationCount: snap.ObservationCount, ChainCount: status.ChainCount, HypothesisCount: status.HypothesisCount, RoutineCount: status.RoutineCount, JournalSequence: status.JournalSequence, JournalHeadHash: status.JournalHeadHash, DurableDigest: digest, ReplayEqual: true, DeviationStore: engine.SnapshotCount(), CoordinatorState: string(status.State), Metrics: engine.Metrics(), Chains: chainsValue, Hypotheses: hypothesisValue, Routines: routinesValue, LatestDeviation: deviationValue}
}

// assessmentPayload is kept as a small JSON projection so the UI sees factors
// from the actual deviation evaluator without exposing raw identities.
func assessmentPayload(value deviation.Assessment) map[string]any {
	result := map[string]any{
		"status": value.Status, "band": value.Band, "score": value.Score,
		"coverage": value.Coverage, "kind": value.Kind, "reason_codes": value.ReasonCodes,
		"fingerprint": value.Fingerprint, "candidate_count": len(value.Candidates),
		"baseline_count": len(value.Baseline),
	}
	if value.BestMatch != nil {
		result["factors"] = []any{
			factorPayload(value.BestMatch.Structural), factorPayload(value.BestMatch.Temporal), factorPayload(value.BestMatch.Interval),
		}
		result["best_match"] = map[string]any{"revision": value.BestMatch.Routine.Revision, "occurrences": value.BestMatch.Routine.OccurrenceCount, "distinct_days": value.BestMatch.Routine.DistinctLocalDays, "exact": value.BestMatch.ExactRoutineID}
	}
	return result
}

func factorPayload(value deviation.Factor) map[string]any {
	return map[string]any{"kind": value.Kind, "available": value.Available, "score": value.Score, "weight": value.Weight, "contribution": value.EffectiveWeight, "reason_codes": value.ReasonCodes}
}

func Encode(result RunResult) ([]byte, error) { return json.MarshalIndent(result, "", "  ") }
