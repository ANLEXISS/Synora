package campaign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	cge "synora/internal/cge"
	"synora/internal/cge/chains/generations"
	cgecontext "synora/internal/cge/context"
)

type campaignClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *campaignClock) Now() time.Time      { c.mu.RLock(); defer c.mu.RUnlock(); return c.now }
func (c *campaignClock) Set(value time.Time) { c.mu.Lock(); c.now = value.UTC(); c.mu.Unlock() }

type campaignProvider struct {
	mu       sync.RWMutex
	current  TimelineEvent
	topology cgecontext.TopologySnapshot
	timezone string
}

func (p *campaignProvider) Set(event TimelineEvent) { p.mu.Lock(); p.current = event; p.mu.Unlock() }

func (p *campaignProvider) Resolve(ctx context.Context, observationID string, observedAt time.Time, nodeID string) (cgecontext.Frame, error) {
	if err := ctx.Err(); err != nil {
		return cgecontext.Frame{}, err
	}
	p.mu.RLock()
	event := p.current
	topology := p.topology.Clone()
	timezone := p.timezone
	p.mu.RUnlock()
	if event.ID != observationID || !event.ContextAvailable {
		return cgecontext.Frame{}, errors.New("campaign_context_unavailable")
	}
	if event.ContextQuality == cgecontext.QualityPartial {
		nodeID = ""
	}
	if !event.TopologyAvailable {
		topology = cgecontext.TopologySnapshot{}
	}
	occupancy := cgecontext.OccupancyOccupied
	mode := cgecontext.HouseModeHome
	if event.ResidentID == "" {
		occupancy, mode = cgecontext.OccupancyUnknown, cgecontext.HouseModeUnknown
	}
	return cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: observationID, ObservedAt: observedAt, NodeID: nodeID, Timezone: timezone, Occupancy: occupancy, HouseMode: mode, Topology: topology, AllowPartial: true})
}

func (p *campaignProvider) CurrentTopology(ctx context.Context) (cgecontext.TopologySnapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return cgecontext.TopologySnapshot{}, false, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.current.TopologyAvailable {
		return cgecontext.TopologySnapshot{}, false, nil
	}
	return p.topology.Clone(), true, nil
}

type campaignLogger struct{}

func (campaignLogger) Printf(string, ...any) {}

func Run(ctx context.Context, profile Profile, options RunOptions) (Report, error) {
	if err := profile.Validate(); err != nil {
		return Report{}, err
	}
	if options.RootDir == "" {
		return Report{}, errors.New("campaign root directory is required")
	}
	timeline, err := GenerateTimeline(profile)
	if err != nil {
		return Report{}, err
	}
	if options.DaysOverride > 0 && options.DaysOverride < profile.DurationDays {
		profile.DurationDays = options.DaysOverride
		timeline, err = GenerateTimeline(profile)
		if err != nil {
			return Report{}, err
		}
	}
	if err := os.MkdirAll(options.RootDir, 0o700); err != nil {
		return Report{}, err
	}
	report := Report{CampaignID: campaignID(profile), ProfileID: profile.ID, Seed: profile.Seed, StartedAt: time.Now().UTC(), SimulatedStart: timeline.StartAt, SimulatedEnd: timeline.EndAt, EventCount: len(timeline.Events), Configuration: configurationSnapshot(profile)}
	clock := &campaignClock{now: profile.StartAt}
	provider := &campaignProvider{topology: campaignTopology(profile), timezone: profile.Timezone}
	engine, err := openEngine(ctx, profile, options.RootDir, clock, provider, true, false)
	if err != nil {
		return report, err
	}
	defer engine.Close()
	results := make([]EventResult, 0, len(timeline.Events))
	latencies := make([]time.Duration, 0, len(timeline.Events))
	lastDay := -1
	for index, event := range timeline.Events {
		if err := ctx.Err(); err != nil {
			_ = engine.Close()
			return report, err
		}
		day := int(event.OccurredAt.Sub(timeline.StartAt).Hours() / 24)
		if lastDay >= 0 && day != lastDay {
			if err := boundaryCheck(ctx, engine, event); err != nil {
				report.InvariantFailures = append(report.InvariantFailures, InvariantFailure{Code: "daily_validation_failed", At: event.OccurredAt, EventID: event.ID})
				_ = err
			}
		}
		if event.RestartBefore {
			if err := restartEngine(ctx, &engine, profile, options.RootDir, clock, provider, event.OccurredAt); err != nil {
				report.InvariantFailures = append(report.InvariantFailures, InvariantFailure{Code: "restart_failed", At: event.OccurredAt, EventID: event.ID})
				return report, err
			}
			report.RestartCount++
		}
		clock.Set(event.OccurredAt)
		provider.Set(event)
		before := engine.Metrics()
		start := time.Now()
		_, observeErr := engine.Observe(ctx, buildEvent(event))
		elapsed := time.Since(start)
		latencies = append(latencies, elapsed)
		after := engine.Metrics()
		result := captureEventResult(event, engine, before, after, observeErr)
		results = append(results, result)
		if observeErr == nil {
			report.EventsSucceeded++
		} else {
			report.EventsFailed++
		}
		if index%23 == 0 {
			report.IdempotenceChecks++
			beforeRetry := engine.Status()
			_, retryErr := engine.Observe(ctx, buildEvent(event))
			afterRetry := engine.Status()
			if retryErr != nil || afterRetry.JournalSequence != beforeRetry.JournalSequence {
				report.IdempotenceFailures++
				code := "timeline_idempotence_failed"
				if retryErr != nil {
					code += ":" + cge.ErrorCode(retryErr) + ":" + afterRetry.DegradedReason
				}
				report.InvariantFailures = append(report.InvariantFailures, InvariantFailure{Code: code, At: event.OccurredAt, EventID: event.ID})
			}
		}
		if event.CheckpointAfter {
			if _, checkpointErr := engine.CreateCheckpoint(ctx, event.OccurredAt); checkpointErr != nil {
				report.InvariantFailures = append(report.InvariantFailures, InvariantFailure{Code: "checkpoint_failed", At: event.OccurredAt, EventID: event.ID})
				return report, checkpointErr
			}
			report.CheckpointCount++
		}
		if event.CheckpointAfter || event.RestartBefore || index == len(timeline.Events)-1 {
			if err := boundaryCheck(ctx, engine, event); err != nil {
				report.InvariantFailures = append(report.InvariantFailures, InvariantFailure{Code: "boundary_validation_failed", At: event.OccurredAt, EventID: event.ID})
			}
			generationCount := 0
			if store, storeErr := generations.NewStore(options.RootDir, generations.StoreOptions{}); storeErr == nil {
				if values, listErr := store.ListGenerations(ctx); listErr == nil {
					generationCount = len(values)
				}
			}
			report.Growth = append(report.Growth, growthSample(ctx, engine, options.RootDir, len(results), generationCount, event.OccurredAt))
		}
		lastDay = day
	}
	report.Events = results
	report.Memory = collectMemory(options.RootDir, engine)
	report.Latency = latencyMetrics(latencies)
	report.DurableStateDigest, err = engine.DurableStateDigest(ctx)
	if err != nil {
		return report, err
	}
	report.Warmup = aggregateWarmup(results, timeline)
	report.Labels = aggregateLabels(results)
	report.BenignDeviation = aggregateBenign(report.Labels)
	report.Separation = computeSeparation(results)
	if profile.ID == "routine_shift_45d" {
		adaptation := computeAdaptation(results, timeline)
		report.Adaptation = &adaptation
	}
	report.CalibrationFindings = calibrationFindings(report, profile)
	report.Success = report.EventsFailed == 0 && len(report.InvariantFailures) == 0
	if !report.Success {
		report.BlockingReasons = append(report.BlockingReasons, "campaign_technical_failure")
	}
	if options.EventsOutput != "" {
		if err := writeEvents(options.EventsOutput, results); err != nil {
			return report, err
		}
	}
	report.EndedAt = time.Now().UTC()
	return report, nil
}

func openEngine(ctx context.Context, profile Profile, root string, clock *campaignClock, provider *campaignProvider, initialize bool, journalOnly bool) (*cge.ShadowEngine, error) {
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = initialize
	config.JournalOnlyRecovery = journalOnly
	config.JournalID = "cge-campaign-" + profile.ID
	config.Context.Enabled, config.Context.Timezone, config.Context.AllowPartial = true, profile.Timezone, true
	config.Cognitive.Enabled = true
	config.Cognitive.AutoApplyDecisiveEvidence = false
	config.Routines.Enabled = true
	config.Routines.AllowPartialContext = true
	config.Deviation.Enabled = true
	config.Deviation.RecentAssessmentLimit = 256
	config.Deviation.MaxAssessmentsPerObservation = 2
	engine, err := cge.NewShadowEngineWithConfig(ctx, config, clock, campaignLogger{})
	if err != nil {
		return nil, err
	}
	engine.SetContextProvider(provider)
	engine.SetRoutineTopologyProvider(provider)
	return engine, nil
}

func restartEngine(ctx context.Context, engine **cge.ShadowEngine, profile Profile, root string, clock *campaignClock, provider *campaignProvider, at time.Time) error {
	before, err := (*engine).DurableStateDigest(ctx)
	if err != nil {
		return err
	}
	if err := (*engine).Close(); err != nil {
		return err
	}
	clock.Set(at)
	journalOnly, err := openEngine(ctx, profile, root, clock, provider, false, true)
	if err != nil {
		return err
	}
	journalDigest, err := journalOnly.DurableStateDigest(ctx)
	_ = journalOnly.Close()
	if err != nil {
		return err
	}
	if before != journalDigest {
		return errors.New("journal_only_state_digest_mismatch")
	}
	restarted, err := openEngine(ctx, profile, root, clock, provider, false, false)
	if err != nil {
		return err
	}
	after, err := restarted.DurableStateDigest(ctx)
	if err != nil {
		_ = restarted.Close()
		return err
	}
	if before != after {
		_ = restarted.Close()
		return errors.New("restart_state_digest_mismatch")
	}
	*engine = restarted
	return nil
}

func buildEvent(event TimelineEvent) cge.Event {
	typ := "vision.identity"
	identity := event.ResidentID
	if event.ResidentID == "" {
		typ, identity = "vision.unknown", ""
	}
	return cge.Event{ID: event.ID, Type: typ, Source: "campaign", Timestamp: event.OccurredAt, Identity: identity, DeviceID: "campaign-camera", NodeID: event.NodeID, ActivationID: "campaign-activation", TrackID: "campaign-track-" + event.ID[:12], SequenceKey: "campaign-sequence-" + event.ResidentID}
}

func captureEventResult(event TimelineEvent, engine *cge.ShadowEngine, before, after cge.MetricsSnapshot, observeErr error) EventResult {
	result := EventResult{EventID: event.ID, OccurredAt: event.OccurredAt, Label: event.Label, HistoricalSucceeded: observeErr == nil, Restarted: event.RestartBefore, Checkpointed: event.CheckpointAfter}
	orchestration := engine.LastOrchestrationResult()
	result.AssociationDecision = string(orchestration.AssociationDecision)
	result.HypothesisAction = string(orchestration.HypothesisAction)
	result.ChainCreated = after.AppliedCreateCandidate > before.AppliedCreateCandidate
	result.RoutinePresenceApplied = after.RoutineCreated > before.RoutineCreated || after.RoutineOccurrenceAdded > before.RoutineOccurrenceAdded
	result.RoutineTransitionApplied = after.RoutineTransitionExtracted > before.RoutineTransitionExtracted
	deviationResult := engine.LastDeviationResult()
	result.DeviationAttempted = deviationResult.Attempted
	if summary, ok := engine.LastDeviationAssessment(); ok && deviationResult.Attempted {
		result.DeviationStatus, result.DeviationBand, result.DeviationScore, result.DeviationCoverage = summary.Status, summary.Band, summary.Score, summary.Coverage
	}
	result.JournalSequence = engine.Status().JournalSequence
	if observeErr != nil {
		result.ErrorCode = cge.ErrorCode(observeErr)
	}
	return result
}

func boundaryCheck(ctx context.Context, engine *cge.ShadowEngine, event TimelineEvent) error {
	if err := engine.ValidateDurableState(ctx); err != nil {
		return err
	}
	status := engine.Status()
	if status.RoutineCount < 0 || status.ChainCount < 0 || status.HypothesisCount < 0 {
		return errors.New("negative_registry_count")
	}
	return nil
}

func campaignTopology(profile Profile) cgecontext.TopologySnapshot {
	base := referenceTopology()
	base.CapturedAt = profile.StartAt
	return base
}

func campaignID(profile Profile) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", profile.ID, profile.Seed, profile.StartAt.UTC().Format(time.RFC3339Nano))))
	return "campaign-" + hex.EncodeToString(digest[:8])
}

func configurationSnapshot(profile Profile) ConfigurationSnapshot {
	config := cge.DefaultShadowConfig()
	return ConfigurationSnapshot{Timezone: profile.Timezone, ContextEnabled: true, ContextAllowPartial: true, RoutineLearningEnabled: true, RoutinePolicyVersion: "routine-extraction-v1", DeviationEnabled: true, DeviationPolicy: config.Deviation.Policy, DeviationRecentLimit: 256, DeviationMaxAssessments: 2, CognitiveEnabled: true, AssociationPolicyVersion: "association-v1", EvidencePolicyVersion: "evidence-v1"}
}

func writeEvents(path string, results []EventResult) error {
	data := make([]byte, 0)
	for _, result := range results {
		line, err := json.Marshal(result)
		if err != nil {
			return err
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	return os.WriteFile(path, data, 0o600)
}
