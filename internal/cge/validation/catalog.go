package validation

import (
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/hypotheses"
)

var validationBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func Catalog() []Scenario {
	items := []Scenario{
		associationAttachScenario(),
		associationCreateCandidateScenario(),
		evidenceScenario("evidence_contradiction_resolution", hypotheses.AlternativeContradiction),
		evidenceScenario("evidence_neutral_resolution", hypotheses.AlternativeNeutral),
		evidenceScenario("evidence_support_resolution", hypotheses.AlternativeSupport),
		evidenceInsufficientScenario(),
		rebaseStaleScenario(),
		supersessionScenario(),
	}
	for i := range items {
		last := items[i].Steps[len(items[i].Steps)-1].At
		items[i].Steps = append(items[i].Steps, step("restart-journal", StepRestartFromJournal, last.Add(time.Second), RestartInput{}))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func FindScenario(id string) (Scenario, error) {
	for _, item := range Catalog() {
		if item.ID == id {
			return item, nil
		}
	}
	return Scenario{}, fmt.Errorf("scenario %q not found", id)
}

func step(id string, kind StepKind, at time.Time, input StepInput) Step {
	return Step{ID: id, Kind: kind, At: at, Input: input, Mutation: chains.MutationContext{At: at, Actor: "cge-validation", Reason: "qualification scenario", CorrelationID: id}}
}
func obs(id, entity, sequence string, at time.Time) chains.ObservationRef {
	return chains.ObservationRef{ID: id, EventType: "vision.identity", Timestamp: at, EntityID: entity, SequenceKey: sequence, NodeID: "node-1", DeviceID: "device-1", TrackID: "track-1"}
}

func associationAttachScenario() Scenario {
	b := validationBase
	seedA, seedB, target := obs("seed-a", "entity-1", "sequence-1", b.Add(time.Second)), obs("seed-b", "entity-1", "sequence-1", b.Add(2*time.Second)), obs("association-target", "entity-1", "sequence-1", b.Add(3*time.Second))
	steps := []Step{
		step("open", StepOpenCoordinator, b, OpenCoordinatorInput{Purpose: "association attach qualification"}),
		step("chain-a", StepAddChain, b.Add(time.Second), AddChainInput{ChainID: chains.ChainID("cge-validation-a"), InitialObservations: []chains.ObservationRef{seedA}}),
		step("chain-b", StepAddChain, b.Add(2*time.Second), AddChainInput{ChainID: chains.ChainID("cge-validation-b"), InitialObservations: []chains.ObservationRef{seedB}}),
		step("plan", StepPlanAssociation, b.Add(3*time.Second), PlanAssociationInput{Observation: target, Policy: association.DefaultPolicy()}),
		step("open-hypothesis", StepOpenHypothesis, b.Add(4*time.Second), OpenHypothesisInput{AssociationPlanStepID: "plan"}),
		step("plan-resolution", StepPlanResolution, b.Add(5*time.Second), PlanResolutionInput{AlternativeKind: hypotheses.AlternativeAttachExisting, AlternativeChainID: chains.ChainID("cge-validation-a")}),
		step("resolve", StepResolveHypothesis, b.Add(6*time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}),
		step("assert", StepAssertState, b.Add(7*time.Second), AssertStateInput{ChainCount: 2, HypothesisCount: 1, ResolvedEffect: hypotheses.ResolutionEffectAttachObservation}),
	}
	return Scenario{ID: "association_attach_existing", Description: "Explicitly resolve an ambiguous association to one existing chain.", StartAt: b, Steps: steps, Expected: ExpectedOutcome{RequiredEffects: []hypotheses.ResolutionEffectKind{hypotheses.ResolutionEffectAttachObservation}, RequireReplayEquality: true}}
}

func associationCreateCandidateScenario() Scenario {
	b := validationBase.Add(30 * time.Minute)
	target := obs("create-target", "entity-new", "sequence-new", b.Add(time.Second))
	steps := []Step{
		step("open", StepOpenCoordinator, b, OpenCoordinatorInput{Purpose: "association create candidate qualification"}),
		step("plan", StepPlanAssociation, b.Add(time.Second), PlanAssociationInput{Observation: target, Policy: association.DefaultPolicy()}),
		step("apply", StepApplyAssociation, b.Add(2*time.Second), ApplyAssociationInput{PlanStepID: "plan"}),
		step("assert", StepAssertState, b.Add(3*time.Second), AssertStateInput{ChainCount: 1, HypothesisCount: 0}),
	}
	return Scenario{ID: "association_create_candidate", Description: "Qualify the real association create-candidate planner and constructor path.", StartAt: b, Steps: steps, Expected: ExpectedOutcome{RequireReplayEquality: true}}
}

func evidencePolicy() evidence.Policy { return evidence.DefaultPolicy() }

func evidenceScenario(id string, selected hypotheses.AlternativeKind) Scenario {
	b := validationBase.Add(1 * time.Hour)
	contextSame := obs(id+"-context-same", "entity-target", "sequence-evidence", b.Add(time.Second))
	contextDifferentA := obs(id+"-context-different-a", "entity-other-a", "sequence-evidence", b.Add(2*time.Second))
	contextDifferentB := obs(id+"-context-different-b", "entity-other-b", "sequence-evidence", b.Add(3*time.Second))
	target := obs(id+"-target", "entity-target", "sequence-evidence", b.Add(4*time.Second))
	chainID := chains.ChainID("cge-validation-evidence")
	steps := []Step{
		step("open", StepOpenCoordinator, b, OpenCoordinatorInput{Purpose: id}),
		step("chain", StepAddChain, b.Add(time.Second), AddChainInput{ChainID: chainID}),
		step("context-same", StepAddObservation, b.Add(2*time.Second), AddObservationInput{ChainID: chainID, SourceRevision: 1, Observation: contextSame}),
		step("context-different-a", StepAddObservation, b.Add(3*time.Second), AddObservationInput{ChainID: chainID, SourceRevision: 2, Observation: contextDifferentA}),
		step("context-different-b", StepAddObservation, b.Add(4*time.Second), AddObservationInput{ChainID: chainID, SourceRevision: 3, Observation: contextDifferentB}),
		step("target", StepAddObservation, b.Add(5*time.Second), AddObservationInput{ChainID: chainID, SourceRevision: 4, Observation: target}),
		step("evaluate", StepEvaluateEvidence, b.Add(6*time.Second), EvaluateEvidenceInput{ChainID: chainID, TargetObservationID: target.ID, Policy: evidencePolicy()}),
		step("open-hypothesis", StepOpenHypothesis, b.Add(7*time.Second), OpenHypothesisInput{EvidenceEvaluationStepID: "evaluate"}),
		step("plan-resolution", StepPlanResolution, b.Add(8*time.Second), PlanResolutionInput{AlternativeKind: selected}),
		step("resolve", StepResolveHypothesis, b.Add(9*time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}),
		step("assert", StepAssertState, b.Add(10*time.Second), AssertStateInput{ChainCount: 1, HypothesisCount: 1, ResolvedEffect: hypotheses.ResolutionEffectAddContribution}),
	}
	return Scenario{ID: id, Description: "Evaluate evidence and explicitly select one immutable contribution alternative.", StartAt: b, Steps: steps, Expected: ExpectedOutcome{RequiredEffects: []hypotheses.ResolutionEffectKind{hypotheses.ResolutionEffectAddContribution}, RequireReplayEquality: true}}
}

func evidenceInsufficientScenario() Scenario {
	b := validationBase.Add(2 * time.Hour)
	target := obs("insufficient-target", "entity-unknown", "sequence-insufficient", b.Add(time.Second))
	chainID := chains.ChainID("cge-validation-insufficient")
	steps := []Step{step("open", StepOpenCoordinator, b, OpenCoordinatorInput{Purpose: "explicit insufficiency qualification"}), step("chain", StepAddChain, b.Add(time.Second), AddChainInput{ChainID: chainID}), step("target", StepAddObservation, b.Add(2*time.Second), AddObservationInput{ChainID: chainID, SourceRevision: 1, Observation: target}), step("evaluate", StepEvaluateEvidence, b.Add(3*time.Second), EvaluateEvidenceInput{ChainID: chainID, TargetObservationID: target.ID, Policy: evidence.DefaultPolicy(), ForceAmbiguous: true}), step("open-hypothesis", StepOpenHypothesis, b.Add(4*time.Second), OpenHypothesisInput{EvidenceEvaluationStepID: "evaluate"}), step("plan-resolution", StepPlanResolution, b.Add(5*time.Second), PlanResolutionInput{AlternativeKind: hypotheses.AlternativeInsufficient}), step("resolve", StepResolveHypothesis, b.Add(6*time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}), step("assert", StepAssertState, b.Add(7*time.Second), AssertStateInput{ChainCount: 1, HypothesisCount: 1, ResolvedEffect: hypotheses.ResolutionEffectNoChain})}
	return Scenario{ID: "evidence_insufficient_resolution", Description: "Explicitly retain an insufficient evidence alternative without a chain mutation.", StartAt: b, Steps: steps, Expected: ExpectedOutcome{RequiredEffects: []hypotheses.ResolutionEffectKind{hypotheses.ResolutionEffectNoChain}, RequireReplayEquality: true}}
}

func rebaseStaleScenario() Scenario {
	s := evidenceScenario("rebase_invalidates_resolution_plan", hypotheses.AlternativeSupport)
	steps := s.Steps[:len(s.Steps)-2]
	b := steps[len(steps)-1].At
	steps = append(steps, step("mark-under-review", StepSetHypothesisStatus, b.Add(time.Second), SetHypothesisStatusInput{Target: hypotheses.StatusUnderReview, SourceRevision: 1}), step("stale-resolve", StepResolveHypothesis, b.Add(2*time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}))
	steps[len(steps)-1].ExpectError = true
	s.ID = "rebase_invalidates_resolution_plan"
	s.Description = "A prepared resolution is rejected after a hypothesis revision changes."
	s.Steps = steps
	s.Expected.RequiredEffects = nil
	return s
}

func supersessionScenario() Scenario {
	s := evidenceScenario("supersession_invalidates_previous_resolution", hypotheses.AlternativeSupport)
	// Keep the first explicit plan, then supersede its dossier with a new
	// evaluation fingerprint. The old plan is intentionally attempted once
	// and rejected before the successor is explicitly resolved.
	steps := s.Steps[:len(s.Steps)-2]
	last := steps[len(steps)-1].At
	newPolicy := evidencePolicy()
	newPolicy.Namespace = "synora.cge.evidence.rebased"
	newPolicy.Version = "evidence-v2"
	steps = append(steps,
		step("evaluate-new", StepEvaluateEvidence, last.Add(time.Second), EvaluateEvidenceInput{ChainID: chains.ChainID("cge-validation-evidence"), TargetObservationID: "supersession_invalidates_previous_resolution-target", Policy: newPolicy}),
		step("supersede", StepSupersedeHypothesis, last.Add(2*time.Second), SupersedeHypothesisInput{EvaluationStepID: "evaluate-new"}),
		step("stale-resolve", StepResolveHypothesis, last.Add(3*time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}),
		step("plan-successor", StepPlanResolution, last.Add(4*time.Second), PlanResolutionInput{AlternativeKind: hypotheses.AlternativeSupport}),
		step("resolve-successor", StepResolveHypothesis, last.Add(5*time.Second), ResolveHypothesisInput{PlanStepID: "plan-successor"}),
		step("assert", StepAssertState, last.Add(6*time.Second), AssertStateInput{ChainCount: 1, HypothesisCount: 2, SupersededCount: 1, ResolvedEffect: hypotheses.ResolutionEffectAddContribution}),
	)
	for i := range steps {
		if steps[i].ID == "stale-resolve" {
			steps[i].ExpectError = true
		}
	}
	s.Steps = steps
	s.Expected.RequiredEffects = []hypotheses.ResolutionEffectKind{hypotheses.ResolutionEffectAddContribution}
	s.Description = "Supersession invalidates the predecessor plan; the successor is resolved explicitly."
	return s
}

// CheckpointMatrix returns the same deterministic scenarios with a generation
// checkpoint both before and after the explicit resolution. It is separate
// from Catalog so the small CLI catalogue stays focused on A-H.
func CheckpointMatrix() []Scenario {
	bases := []Scenario{
		associationAttachScenario(),
		evidenceScenario("evidence_support_resolution", hypotheses.AlternativeSupport),
		evidenceScenario("evidence_contradiction_resolution", hypotheses.AlternativeContradiction),
		evidenceScenario("evidence_neutral_resolution", hypotheses.AlternativeNeutral),
		evidenceInsufficientScenario(),
	}
	result := make([]Scenario, 0, len(bases)*2)
	for _, base := range bases {
		for _, before := range []bool{true, false} {
			copyScenario := base
			copyScenario.ID = base.ID + map[bool]string{true: "_checkpoint_before", false: "_checkpoint_after"}[before]
			copyScenario.Description += " with a generation checkpoint."
			copyScenario.Steps = checkpointSteps(base.Steps, before)
			result = append(result, copyScenario)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// JournalReplayScenarios exercises the same WAL through a journal-only
// restart. The journal is read once by the real durable recovery path.
func JournalReplayScenarios() []Scenario {
	result := []Scenario{associationAttachScenario(), evidenceScenario("evidence_support_resolution", hypotheses.AlternativeSupport), evidenceScenario("evidence_contradiction_resolution", hypotheses.AlternativeContradiction), evidenceScenario("evidence_neutral_resolution", hypotheses.AlternativeNeutral), evidenceInsufficientScenario()}
	for i := range result {
		last := result[i].Steps[len(result[i].Steps)-1].At
		result[i].ID += "_journal_only"
		result[i].Steps = append(result[i].Steps, step("restart-journal", StepRestartFromJournal, last.Add(time.Second), RestartInput{}))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// VolumeScenario builds a deterministic, real-API workload. Each item has a
// chain, an evidence hypothesis, and one explicit resolution; the selected
// effect cycles through support, contradiction, neutral, and no-chain.
func VolumeScenario(count int) Scenario {
	if count < 1 {
		count = 1
	}
	b := validationBase.Add(4 * time.Hour)
	steps := []Step{step("open", StepOpenCoordinator, b, OpenCoordinatorInput{Purpose: fmt.Sprintf("CGE volume qualification %d", count)})}
	index := 1
	next := func() time.Time { at := b.Add(time.Duration(index) * time.Millisecond); index++; return at }
	for i := 0; i < count; i++ {
		chainID := chains.ChainID(fmt.Sprintf("cge-validation-volume-%03d", i))
		baseID := fmt.Sprintf("volume-%03d", i)
		noChain := i%4 == 3
		steps = append(steps, step(baseID+"-chain", StepAddChain, next(), AddChainInput{ChainID: chainID}))
		var target chains.ObservationRef
		if noChain {
			target = obs(baseID+"-target", "volume-unknown", baseID, next())
			steps = append(steps, step(baseID+"-target", StepAddObservation, next(), AddObservationInput{ChainID: chainID, SourceRevision: 1, Observation: target}))
		} else {
			contextSame := obs(baseID+"-same", "volume-entity", baseID, next())
			contextDifferentA := obs(baseID+"-different-a", "volume-other-a", baseID, next())
			contextDifferentB := obs(baseID+"-different-b", "volume-other-b", baseID, next())
			target = obs(baseID+"-target", "volume-entity", baseID, next())
			steps = append(steps,
				step(baseID+"-same", StepAddObservation, next(), AddObservationInput{ChainID: chainID, SourceRevision: 1, Observation: contextSame}),
				step(baseID+"-different-a", StepAddObservation, next(), AddObservationInput{ChainID: chainID, SourceRevision: 2, Observation: contextDifferentA}),
				step(baseID+"-different-b", StepAddObservation, next(), AddObservationInput{ChainID: chainID, SourceRevision: 3, Observation: contextDifferentB}),
				step(baseID+"-target", StepAddObservation, next(), AddObservationInput{ChainID: chainID, SourceRevision: 4, Observation: target}),
			)
		}
		steps = append(steps,
			step(baseID+"-evaluate", StepEvaluateEvidence, next(), EvaluateEvidenceInput{ChainID: chainID, TargetObservationID: target.ID, Policy: evidence.DefaultPolicy(), ForceAmbiguous: noChain}),
			step(baseID+"-open", StepOpenHypothesis, next(), OpenHypothesisInput{EvidenceEvaluationStepID: baseID + "-evaluate"}),
		)
		selected := hypotheses.AlternativeSupport
		if i%4 == 1 {
			selected = hypotheses.AlternativeContradiction
		}
		if i%4 == 2 {
			selected = hypotheses.AlternativeNeutral
		}
		if noChain {
			selected = hypotheses.AlternativeInsufficient
		}
		steps = append(steps,
			step(baseID+"-plan", StepPlanResolution, next(), PlanResolutionInput{AlternativeKind: selected}),
			step(baseID+"-resolve", StepResolveHypothesis, next(), ResolveHypothesisInput{PlanStepID: baseID + "-plan"}),
		)
		if i == count/2-1 {
			steps = append(steps,
				step("generation-mid", StepCreateGeneration, next(), CreateGenerationInput{Actor: "cge-validation", CorrelationID: "volume-checkpoint"}),
				step("restart-manifest-mid", StepRestartFromManifest, next(), RestartInput{}),
			)
		}
	}
	steps = append(steps, step("assert", StepAssertState, next(), AssertStateInput{ChainCount: count, HypothesisCount: count}), step("restart-journal", StepRestartFromJournal, next(), RestartInput{}))
	return Scenario{ID: fmt.Sprintf("volume_%d", count), Description: "Large deterministic mixed-effect CGE qualification workload.", StartAt: b, Steps: steps, Expected: ExpectedOutcome{RequiredEffects: []hypotheses.ResolutionEffectKind{hypotheses.ResolutionEffectAddContribution, hypotheses.ResolutionEffectNoChain}, RequireReplayEquality: true}}
}

func checkpointSteps(source []Step, before bool) []Step {
	steps := append([]Step(nil), source...)
	resolutionIndex := -1
	for i, item := range steps {
		if item.Kind == StepResolveHypothesis || item.Kind == StepApplyAssociation {
			resolutionIndex = i
			break
		}
	}
	if resolutionIndex < 0 {
		resolutionIndex = len(steps) - 1
	}
	generation := step("generation", StepCreateGeneration, steps[resolutionIndex].At.Add(-time.Second), CreateGenerationInput{Actor: "cge-validation", CorrelationID: "checkpoint"})
	restart := step("restart-manifest", StepRestartFromManifest, steps[resolutionIndex].At.Add(10*time.Second), RestartInput{})
	if before {
		generation.At = steps[resolutionIndex].At.Add(time.Second)
		generation.Mutation.At = generation.At
		restart.At = generation.At.Add(time.Second)
		restart.Mutation.At = restart.At
		at := resolutionIndex + 1
		steps = append(steps[:at], append([]Step{generation, restart}, steps[at:]...)...)
		for i := at + 2; i < len(steps); i++ {
			steps[i].At = steps[i-1].At.Add(time.Second)
			steps[i].Mutation.At = steps[i].At
		}
	} else {
		steps = append(steps[:resolutionIndex], append([]Step{generation}, steps[resolutionIndex:]...)...)
		restart.At = steps[len(steps)-1].At.Add(time.Second)
		restart.Mutation.At = restart.At
		steps = append(steps, restart)
	}
	return steps
}
