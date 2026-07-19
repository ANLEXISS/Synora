package validation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

type Runner struct {
	RootDir string
	// Full is retained as an explicit qualification mode marker. Both standard
	// and full runs use local step checks plus complete validation at semantic
	// boundaries; full mode selects the exhaustive workload in the qualification
	// command rather than multiplying global validation per event.
	Full bool
}

type runState struct {
	root             string
	journal          *journal.FileJournal
	generations      *generations.Store
	coordinator      *durable.Coordinator
	plans            map[string]hypotheses.ResolutionPlan
	associationPlans map[string]association.Plan
	evaluations      map[string]evidence.EvidenceEvaluation
	resolutions      map[string]durable.HypothesisResolutionResult
	metrics          ScenarioMetrics
	lastHead         uint64
}

func (r *Runner) Run(ctx context.Context, scenario Scenario) (report ScenarioReport, err error) {
	if err := validateScenario(scenario); err != nil {
		return ScenarioReport{ScenarioID: scenario.ID, Success: false, Failures: []InvariantFailure{{Code: "invalid_scenario", Path: "scenario", Message: "scenario definition is invalid"}}}, err
	}
	if err := ctx.Err(); err != nil {
		return ScenarioReport{ScenarioID: scenario.ID, Success: false}, err
	}
	root := r.RootDir
	owned := false
	if root == "" {
		root, err = os.MkdirTemp("", "synora-cge-validation-")
		if err != nil {
			return report, err
		}
		owned = true
	}
	if owned {
		defer os.RemoveAll(root)
	}
	state := &runState{root: root, plans: make(map[string]hypotheses.ResolutionPlan), associationPlans: make(map[string]association.Plan), evaluations: make(map[string]evidence.EvidenceEvaluation), resolutions: make(map[string]durable.HypothesisResolutionResult)}
	defer func() {
		if state.coordinator != nil {
			_ = state.coordinator.Close()
		}
	}()
	report = ScenarioReport{ScenarioID: scenario.ID, StartedAt: scenario.StartAt}
	initialCaptured := false
	for _, step := range scenario.Steps {
		if err := ctx.Err(); err != nil {
			report.Failures = append(report.Failures, InvariantFailure{Code: "context_cancelled", Path: step.ID, Message: "scenario cancelled"})
			break
		}
		result := StepResult{StepID: step.ID, StepKind: step.Kind, StartedAt: step.At, CompletedAt: step.At}
		stepErr := executeStep(ctx, state, step, &result)
		if state.coordinator != nil {
			status := state.coordinator.Status()
			result.JournalSequence, result.JournalHeadHash = status.JournalSequence, status.JournalHeadHash
		}
		sortIDs(result.ChainIDs)
		sortIDs(result.HypothesisIDs)
		if stepErr != nil && step.ExpectError {
			result.Success = true
			result.ErrorCode = errorCode(stepErr)
			result.Error = result.ErrorCode
			report.Steps = append(report.Steps, result)
			continue
		}
		if stepErr == nil && state.coordinator != nil {
			failures := ValidateCoordinatorLocalState(state.coordinator, result)
			status := state.coordinator.Status()
			failures = append(failures, ValidateJournalHead(ctx, state.journal, status.JournalSequence, status.JournalHeadHash)...)
			// Both modes use the same per-step local checks. Full mode expands
			// the workload and matrix; it does not turn every event into a
			// complete registry and journal replay. Global validation remains at
			// semantic boundaries where it adds independent detection value.
			globalBoundary := step.Kind == StepRestartFromJournal || step.Kind == StepRestartFromManifest || step.Kind == StepCreateGeneration
			if globalBoundary {
				failures = append(failures, ValidateCoordinatorState(state.coordinator)...)
				failures = append(failures, ValidateJournal(ctx, state.journal)...)
			}
			if len(failures) > 0 {
				report.Failures = append(report.Failures, failures...)
				state.metrics.InvariantFailures += len(failures)
				stepErr = ErrScenarioFailed
			}
		}
		if stepErr != nil {
			result.Success = false
			result.ErrorCode = errorCode(stepErr)
			result.Error = result.ErrorCode
			report.Steps = append(report.Steps, result)
			break
		}
		result.Success = true
		report.Steps = append(report.Steps, result)
		if !initialCaptured && state.coordinator != nil {
			status := state.coordinator.Status()
			report.InitialState, _ = StateDigestOf(state.coordinator, status.JournalSequence, status.JournalHeadHash)
			initialCaptured = true
		}
	}
	if state.coordinator != nil {
		status := state.coordinator.Status()
		report.FinalState, _ = StateDigestOf(state.coordinator, status.JournalSequence, status.JournalHeadHash)
		if len(report.Failures) == 0 && !scenario.Expected.AllowJournalFailure {
			report.Failures = append(report.Failures, ValidateCoordinatorState(state.coordinator)...)
			report.Failures = append(report.Failures, ValidateJournal(ctx, state.journal)...)
		} else if len(report.Failures) == 0 {
			report.Failures = append(report.Failures, ValidateCoordinatorState(state.coordinator)...)
		}
	}
	report.Metrics = state.metrics
	report.CompletedAt = scenario.StartAt
	if len(scenario.Steps) > 0 {
		report.CompletedAt = scenario.Steps[len(scenario.Steps)-1].At
	}
	report.Success = len(report.Failures) == 0 && len(report.Steps) == len(scenario.Steps)
	for _, required := range scenario.Expected.RequiredEffects {
		found := false
		for _, item := range state.resolutions {
			if item.EffectKind == required && item.Applied {
				found = true
				break
			}
		}
		if !found {
			report.Failures = append(report.Failures, InvariantFailure{Code: "expected_effect_missing", Path: "resolutions", Message: "required resolution effect was not applied"})
			report.Success = false
		}
	}
	if report.Success {
		for _, step := range report.Steps {
			if !step.Success {
				report.Success = false
				break
			}
		}
	}
	if !report.Success && err == nil {
		err = ErrScenarioFailed
	}
	return report, err
}

func (r *Runner) RunCatalog(ctx context.Context) ([]ScenarioReport, error) {
	scenarios := Catalog()
	reports := make([]ScenarioReport, 0, len(scenarios))
	for _, scenario := range scenarios {
		isolated := *r
		if r.RootDir != "" {
			isolated.RootDir = filepath.Join(r.RootDir, scenario.ID)
		}
		report, err := isolated.Run(ctx, scenario)
		reports = append(reports, report)
		if err != nil {
			return reports, err
		}
	}
	return reports, nil
}

func executeStep(ctx context.Context, state *runState, step Step, result *StepResult) error {
	if step.At.IsZero() {
		return ErrScenarioInvalid
	}
	switch input := step.Input.(type) {
	case OpenCoordinatorInput:
		if step.Kind != StepOpenCoordinator {
			return ErrScenarioInvalid
		}
		if state.coordinator != nil {
			return fmt.Errorf("%w: coordinator already open", ErrScenarioInvalid)
		}
		if err := os.MkdirAll(state.root, 0o700); err != nil {
			return err
		}
		options := journal.FileJournalOptions{CreateParentDirs: true, MaxJournalSize: input.JournalMaxSize, MaxRecordSize: input.JournalMaxRecordSize}
		j, err := journal.NewFileJournal(filepath.Join(state.root, "cge.ndjson"), options)
		if err != nil {
			return err
		}
		if _, err = j.Initialize(ctx, journal.GenesisInput{JournalID: "cge-validation-journal", CreatedAt: step.At, Purpose: input.Purpose, RecordedAt: step.At, Actor: "cge-validation", CorrelationID: step.ID}); err != nil {
			return err
		}
		state.journal = j
		j.SetQualificationHook(input.AppendHook)
		state.generations, err = generations.NewStore(filepath.Join(state.root, "generations"), generations.StoreOptions{})
		if err != nil {
			return err
		}
		state.coordinator, _, err = durable.FromJournal(ctx, j)
		if err == nil && input.PublicationHook != nil {
			state.coordinator.SetQualificationPublicationHook(input.PublicationHook)
		}
		return err
	case AddChainInput:
		chain, err := chains.New(input.ChainID, mutationOr(step, "validation chain creation"))
		if err != nil {
			return err
		}
		for _, observation := range input.InitialObservations {
			if err := chain.AddObservation(observation, mutationOr(step, "validation initial observation")); err != nil {
				return err
			}
		}
		item, err := state.coordinator.AddChain(ctx, chain, step.Mutation.Actor, step.Mutation.CorrelationID, step.At)
		if err != nil {
			return err
		}
		state.metrics.ChainsCreated++
		result.ChainIDs = []chains.ChainID{item.ChainID}
		return nil
	case AddObservationInput:
		command := chains.AddObservationCommand{ChainID: input.ChainID, SourceRevision: input.SourceRevision, Observation: input.Observation, Mutation: mutationOr(step, "validation observation")}
		before, err := state.coordinator.Get(input.ChainID)
		if err != nil {
			return err
		}
		item, err := state.coordinator.AddObservation(ctx, command, step.At)
		if err != nil {
			return err
		}
		state.metrics.ObservationsAdded++
		if before.Revision > 0 {
			state.metrics.ChainsUpdated++
		}
		result.ChainIDs = []chains.ChainID{item.ChainID}
		return nil
	case PlanAssociationInput:
		plan, err := state.coordinator.PlanAssociation(association.Input{Observation: input.Observation, SituationKind: input.SituationKind}, step.At, input.Policy)
		if err != nil {
			return err
		}
		state.associationPlans[step.ID] = plan
		result.ChainIDs = append(result.ChainIDs, plan.SelectedChainID)
		if plan.NewChainID != "" {
			result.ChainIDs = append(result.ChainIDs, plan.NewChainID)
		}
		return nil
	case ApplyAssociationInput:
		plan, ok := state.associationPlans[input.PlanStepID]
		if !ok {
			return fmt.Errorf("%w: association plan %s", ErrScenarioInvalid, input.PlanStepID)
		}
		item, err := state.coordinator.ApplyAssociationPlan(ctx, plan, step.Mutation.Actor, step.Mutation.CorrelationID, step.Mutation.At, step.At)
		if err != nil {
			return err
		}
		if item.Applied {
			state.metrics.ChainsCreated++
			state.metrics.ObservationsAdded++
		}
		if item.Idempotent {
			state.metrics.IdempotentOperations++
		}
		result.ChainIDs = []chains.ChainID{item.ChainID}
		return nil
	case EvaluateEvidenceInput:
		snapshot, err := state.coordinator.Get(input.ChainID)
		if err != nil {
			return err
		}
		evaluation, err := evidence.EvaluateObservation(snapshot, input.TargetObservationID, step.At, input.Policy)
		if err != nil {
			return err
		}
		if input.ForceAmbiguous {
			evaluation.Decision = evidence.DecisionAmbiguous
			evaluation.Proposal = nil
		}
		state.evaluations[step.ID] = evaluation
		result.ChainIDs = []chains.ChainID{input.ChainID}
		return nil
	case ApplyEvidenceInput:
		evaluation, ok := state.evaluations[input.EvaluationStepID]
		if !ok || evaluation.Proposal == nil {
			return fmt.Errorf("%w: evidence proposal %s", ErrScenarioInvalid, input.EvaluationStepID)
		}
		batch, err := state.coordinator.ApplyEvidenceProposals(ctx, []evidence.ContributionProposal{*evaluation.Proposal}, step.Mutation.Actor, step.Mutation.CorrelationID, step.Mutation.At, step.At)
		if err != nil {
			return err
		}
		if batch.Applied > 0 {
			state.metrics.ContributionsAdded++
		}
		if batch.Idempotent > 0 {
			state.metrics.IdempotentOperations += batch.Idempotent
		}
		return nil
	case OpenHypothesisInput:
		var set *hypotheses.HypothesisSet
		var err error
		if input.AssociationPlanStepID != "" {
			plan, ok := state.associationPlans[input.AssociationPlanStepID]
			if !ok {
				return ErrScenarioInvalid
			}
			set, err = hypotheses.FromAmbiguousAssociation(plan, step.At, mutationOr(step, "open association hypothesis"))
		} else {
			evaluation, ok := state.evaluations[input.EvidenceEvaluationStepID]
			if !ok {
				return ErrScenarioInvalid
			}
			set, err = hypotheses.FromAmbiguousEvidence(evaluation, step.At, mutationOr(step, "open evidence hypothesis"))
		}
		if err != nil {
			return err
		}
		item, err := state.coordinator.AddHypothesis(ctx, set, step.At)
		if err != nil {
			return err
		}
		state.metrics.HypothesesOpened++
		result.HypothesisIDs = []hypotheses.SetID{item.SetID}
		return nil
	case PlanResolutionInput:
		setID := input.SetID
		if setID == "" {
			var candidate hypotheses.Snapshot
			count := 0
			for _, value := range state.coordinator.ListHypotheses() {
				if value.Status == hypotheses.StatusOpen || value.Status == hypotheses.StatusUnderReview {
					candidate, count = value, count+1
				}
			}
			if count != 1 {
				return fmt.Errorf("%w: resolution set is ambiguous", ErrScenarioInvalid)
			}
			setID = candidate.ID
		}
		snapshot, err := state.coordinator.GetHypothesis(setID)
		if err != nil {
			return err
		}
		alternativeID := input.AlternativeID
		if alternativeID == "" {
			matches := 0
			for _, alternative := range snapshot.Alternatives {
				if alternative.Kind == input.AlternativeKind && (input.AlternativeChainID == "" || alternative.ChainID == input.AlternativeChainID) {
					alternativeID = alternative.ID
					matches++
				}
			}
			if matches != 1 {
				return fmt.Errorf("%w: explicit alternative reference is not unique", ErrScenarioInvalid)
			}
		}
		if alternativeID == "" {
			return fmt.Errorf("%w: explicit alternative is missing", ErrScenarioInvalid)
		}
		plan, err := hypotheses.PlanResolution(snapshot, alternativeID, step.At)
		if err != nil {
			return err
		}
		state.plans[step.ID] = plan
		result.HypothesisIDs = []hypotheses.SetID{setID}
		return nil
	case ResolveHypothesisInput:
		plan, ok := state.plans[input.PlanStepID]
		if !ok {
			return fmt.Errorf("%w: resolution plan %s", ErrScenarioInvalid, input.PlanStepID)
		}
		command, err := plan.Command(mutationOrResolve(step, input))
		if err != nil {
			return err
		}
		callContext := ctx
		var cancel context.CancelFunc
		if input.CancelBefore {
			canceled, cancelContext := context.WithCancel(ctx)
			cancel = cancelContext
			cancel()
			callContext = canceled
		} else if input.CancelDuring {
			canceled, cancelContext := context.WithCancel(ctx)
			cancel = cancelContext
			state.journal.SetQualificationHook(func(stage string) error {
				if stage == "after_write" {
					cancel()
				}
				return nil
			})
			callContext = canceled
		}
		item, err := state.coordinator.ResolveHypothesis(callContext, command, step.At)
		if cancel != nil {
			cancel()
			if input.CancelDuring {
				state.journal.SetQualificationHook(nil)
			}
		}
		if err != nil {
			return err
		}
		if item.Applied {
			state.metrics.HypothesesResolved++
			state.metrics.ResolutionMetric(item.EffectKind, item.AlternativeKind)
		}
		if item.Idempotent {
			state.metrics.IdempotentOperations++
		}
		state.resolutions[step.ID] = item
		result.HypothesisIDs = []hypotheses.SetID{item.SetID}
		if item.ChainAfter != nil {
			result.ChainIDs = []chains.ChainID{item.ChainAfter.ID}
		}
		return nil
	case CreateGenerationInput:
		if state.generations == nil {
			return ErrScenarioInvalid
		}
		_, err := state.coordinator.CreateSnapshotGeneration(ctx, state.generations, step.At, input.Actor, input.CorrelationID)
		if err == nil {
			state.metrics.GenerationsCreated++
		}
		return err
	case RestartInput:
		if state.journal == nil {
			return ErrScenarioInvalid
		}
		beforeStatus := state.coordinator.Status()
		wasDegraded := beforeStatus.State == durable.StateDegraded
		beforeDigest, digestErr := StateDigestOf(state.coordinator, beforeStatus.JournalSequence, beforeStatus.JournalHeadHash)
		if digestErr != nil {
			return digestErr
		}
		if state.coordinator != nil {
			_ = state.coordinator.Close()
		}
		var err error
		if step.Kind == StepRestartFromManifest {
			state.coordinator, _, err = durable.FromGenerationManifest(ctx, state.generations, state.journal)
		} else {
			state.coordinator, _, err = durable.FromJournal(ctx, state.journal)
		}
		if err == nil {
			state.metrics.ReplaysPerformed++
			status := state.coordinator.Status()
			afterDigest, digestErr := StateDigestOf(state.coordinator, status.JournalSequence, status.JournalHeadHash)
			if digestErr != nil {
				return digestErr
			}
			if !wasDegraded && (beforeDigest.ChainsSHA256 != afterDigest.ChainsSHA256 || beforeDigest.HypothesesSHA256 != afterDigest.HypothesesSHA256 || beforeDigest.JournalSequence != afterDigest.JournalSequence || beforeDigest.JournalHeadHash != afterDigest.JournalHeadHash) {
				return fmt.Errorf("%w: replay state digest differs", ErrScenarioFailed)
			}
		}
		return err
	case AssertStateInput:
		if state.coordinator == nil {
			return ErrScenarioInvalid
		}
		if len(state.coordinator.List()) != input.ChainCount || len(state.coordinator.ListHypotheses()) != input.HypothesisCount {
			return fmt.Errorf("%w: state count assertion", ErrScenarioFailed)
		}
		if input.ResolvedSetID != "" {
			snapshot, err := state.coordinator.GetHypothesis(input.ResolvedSetID)
			if err != nil || snapshot.Status != hypotheses.StatusResolved || snapshot.Resolution == nil || snapshot.Resolution.EffectKind != input.ResolvedEffect {
				return fmt.Errorf("%w: resolution assertion", ErrScenarioFailed)
			}
		}
		if input.SupersededCount >= 0 {
			count := 0
			for _, value := range state.coordinator.ListHypotheses() {
				if value.Status == hypotheses.StatusSuperseded {
					count++
				}
			}
			if count != input.SupersededCount {
				return fmt.Errorf("%w: superseded count assertion", ErrScenarioFailed)
			}
		}
		return nil
	case SetHypothesisStatusInput:
		setID := input.SetID
		if setID == "" {
			values := state.coordinator.ListHypotheses()
			if len(values) != 1 {
				return fmt.Errorf("%w: status set is ambiguous", ErrScenarioInvalid)
			}
			setID = values[0].ID
		}
		item, err := state.coordinator.SetHypothesisStatus(ctx, hypotheses.SetStatusCommand{SetID: setID, SourceRevision: input.SourceRevision, Target: input.Target, Mutation: mutationOr(step, "validation hypothesis status")}, step.At)
		if err != nil {
			return err
		}
		if item.Applied {
			state.metrics.ChainsUpdated += 0
		}
		result.HypothesisIDs = []hypotheses.SetID{setID}
		return nil
	case RebaseHypothesisInput:
		setID := input.SetID
		if setID == "" {
			values := state.coordinator.ListHypotheses()
			if len(values) != 1 {
				return fmt.Errorf("%w: rebase set is ambiguous", ErrScenarioInvalid)
			}
			setID = values[0].ID
		}
		current, err := state.coordinator.GetHypothesis(setID)
		if err != nil {
			return err
		}
		var proposal hypotheses.RebaseProposal
		if input.EvaluationStepID != "" {
			evaluation, ok := state.evaluations[input.EvaluationStepID]
			if !ok {
				return ErrScenarioInvalid
			}
			proposal, err = hypotheses.ProposeEvidenceRebase(current, evaluation, step.At)
		} else {
			plan, ok := state.associationPlans[input.AssociationPlanStepID]
			if !ok {
				return ErrScenarioInvalid
			}
			proposal, err = hypotheses.ProposeAssociationRebase(current, plan, step.At)
		}
		if err != nil {
			return err
		}
		command, err := proposal.Command(mutationOr(step, "validation hypothesis rebase"))
		if err != nil {
			return err
		}
		item, err := state.coordinator.RebaseHypothesis(ctx, command, step.At)
		if err != nil {
			return err
		}
		if item.Applied {
			state.metrics.HypothesesRebased++
		}
		result.HypothesisIDs = []hypotheses.SetID{setID}
		return nil
	case SupersedeHypothesisInput:
		setID := input.SetID
		if setID == "" {
			values := state.coordinator.ListHypotheses()
			if len(values) != 1 {
				return fmt.Errorf("%w: supersession set is ambiguous", ErrScenarioInvalid)
			}
			setID = values[0].ID
		}
		current, err := state.coordinator.GetHypothesis(setID)
		if err != nil {
			return err
		}
		evaluation, ok := state.evaluations[input.EvaluationStepID]
		if !ok {
			return fmt.Errorf("%w: supersession evaluation is missing", ErrScenarioInvalid)
		}
		proposal, err := hypotheses.ProposeEvidenceSupersession(current, evaluation, step.At)
		if err != nil {
			return err
		}
		command, err := proposal.Command(mutationOr(step, "validation hypothesis supersession"))
		if err != nil {
			return err
		}
		item, err := state.coordinator.SupersedeHypothesis(ctx, command, step.At)
		if err != nil {
			return err
		}
		if item.Applied {
			state.metrics.HypothesesSuperseded++
		}
		result.HypothesisIDs = []hypotheses.SetID{item.PreviousSetID, item.NewSetID}
		return nil
	default:
		return fmt.Errorf("%w: unsupported step %s", ErrScenarioInvalid, step.Kind)
	}
}

func (m *ScenarioMetrics) ResolutionMetric(kind hypotheses.ResolutionEffectKind, alternative hypotheses.AlternativeKind) {
	switch kind {
	case hypotheses.ResolutionEffectAttachObservation:
		m.ResolutionAttachEffects++
	case hypotheses.ResolutionEffectCreateCandidate:
		m.ResolutionCreateEffects++
	case hypotheses.ResolutionEffectAddContribution:
		switch alternative {
		case hypotheses.AlternativeSupport:
			m.ResolutionSupportEffects++
		case hypotheses.AlternativeContradiction:
			m.ResolutionContradictionEffects++
		case hypotheses.AlternativeNeutral:
			m.ResolutionNeutralEffects++
		}
	case hypotheses.ResolutionEffectNoChain:
		m.ResolutionNoChainEffects++
	}
}

func mutationOr(step Step, reason string) chains.MutationContext {
	m := step.Mutation
	if m.At.IsZero() {
		m.At = step.At
	}
	if m.Actor == "" {
		m.Actor = "cge-validation"
	}
	if m.Reason == "" {
		m.Reason = reason
	}
	if m.CorrelationID == "" {
		m.CorrelationID = step.ID
	}
	return m
}
func mutationOrResolve(step Step, input ResolveHypothesisInput) chains.MutationContext {
	m := input.Mutation
	if m.At.IsZero() {
		m = step.Mutation
	}
	return mutationOr(Step{At: step.At, Mutation: m, ID: step.ID}, "explicit hypothesis resolution")
}

func validateScenario(s Scenario) error {
	if strings.TrimSpace(s.ID) == "" || s.StartAt.IsZero() || len(s.Steps) == 0 || s.Steps[0].Kind != StepOpenCoordinator {
		return ErrScenarioInvalid
	}
	seen := make(map[string]struct{}, len(s.Steps))
	previous := s.StartAt
	for _, step := range s.Steps {
		if step.ID == "" || step.At.IsZero() || step.At.Before(previous) {
			return ErrScenarioInvalid
		}
		if _, ok := seen[step.ID]; ok {
			return ErrScenarioInvalid
		}
		seen[step.ID] = struct{}{}
		previous = step.At
	}
	return nil
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if index := strings.Index(text, ":"); index >= 0 {
		text = text[:index]
	}
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, " ", "_")
	return text
}

func JournalKinds(ctx context.Context, j *journal.FileJournal) (JournalSummary, error) {
	snapshot, err := j.ReadAll(ctx)
	if err != nil {
		return JournalSummary{}, err
	}
	kinds := make([]journal.RecordKind, len(snapshot.Records))
	for i, record := range snapshot.Records {
		kinds[i] = record.Kind
	}
	return JournalSummary{Sequence: snapshot.HeadSequence, HeadHash: snapshot.HeadHash, Kinds: kinds}, nil
}

func sortIDs[T ~string](values []T) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}

var _ = errors.Is
