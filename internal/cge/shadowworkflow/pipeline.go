package shadowworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/advisoryrequests"
	"synora/internal/cge/authorizationboundary"
	"synora/internal/cge/capabilitymapping"
	"synora/internal/cge/durableworkflow"
	"synora/internal/cge/episodes"
	"synora/internal/cge/evidencediscrimination"
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

type pipelineResult struct {
	duplicate bool
	ambiguous bool
	rejected  bool
}

func (r *Runtime) process(ctx context.Context, input ShadowWorkflowInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := r.clock.Now().UTC()
	state := r.coordinator.Snapshot()
	episodePlan, episode, outcome, err := r.planEpisode(state, input)
	_ = episodePlan
	if err != nil {
		if errors.Is(err, episodes.ErrAmbiguousPlan) {
			r.metrics.add("episode_ambiguous")
			return nil
		}
		if errors.Is(err, episodes.ErrRejectedPlan) || errors.Is(err, episodes.ErrLateObservationOutsideGrace) {
			r.metrics.add("episode_rejected")
			return nil
		}
		return fmt.Errorf("%w: episode", ErrPipelineStageFailed)
	}
	if outcome.duplicate {
		r.counters.duplicates.Add(1)
		r.metrics.add("episode_duplicate")
		return nil
	}
	if outcome.ambiguous || outcome.rejected {
		return nil
	}
	r.metrics.add("episode_planned")

	mutation := durableworkflow.WorkflowMutation{EpisodeID: string(episode.ID), Episode: &episode, SourceWorkflowRevision: state.Revision, SourceWorkflowDigest: state.Digest}
	if r.cfg.PipelineDepth == DepthEpisode {
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	facts, err := situationfacts.Extract(situationfacts.ExtractionInput{Episode: episode, ExtractedAt: now}, situationfacts.DefaultPolicy())
	if err != nil {
		return fmt.Errorf("%w: facts", ErrPipelineStageFailed)
	}
	r.metrics.add("facts_succeeded")
	mutation.Facts = &facts
	if r.cfg.PipelineDepth == DepthSituationFacts {
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	previousHyp := previousHypotheses(state, episode.ID)
	hypResult, err := situationhypotheses.Evaluate(situationhypotheses.EvaluationInput{FactSet: facts, PreviousSet: previousHyp}, situationhypotheses.Schema(), situationhypotheses.DefaultPolicy())
	if err != nil {
		return fmt.Errorf("%w: hypotheses", ErrPipelineStageFailed)
	}
	hypotheses := hypResult.Set
	r.metrics.add("hypotheses_succeeded")
	mutation.Hypotheses = &hypotheses
	if r.cfg.PipelineDepth == DepthSituationHypotheses {
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	previousDiscrimination := previousDiscrimination(state, episode.ID)
	discrimination, err := evidencediscrimination.Analyze(evidencediscrimination.AnalysisInput{FactSet: facts, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema(), PreviousAssessment: previousDiscrimination}, evidencediscrimination.Catalog(), evidencediscrimination.DefaultPolicy())
	if err != nil {
		return fmt.Errorf("%w: discrimination", ErrPipelineStageFailed)
	}
	r.metrics.add("discrimination_succeeded")
	mutation.Discrimination = &discrimination
	if r.cfg.PipelineDepth == DepthEvidenceDiscrimination {
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	requestSnapshot := advisorySnapshot(state, advisoryrequests.DefaultPolicy())
	requestPlan, err := advisoryrequests.Plan(advisoryrequests.PlanInput{Assessment: discrimination, RegistrySnapshot: requestSnapshot, EvaluatedAt: now}, advisoryrequests.DefaultPolicy())
	if err != nil {
		return fmt.Errorf("%w: advisory", ErrPipelineStageFailed)
	}
	if len(requestPlan.ResultingRequests) > r.cfg.MaxAdvisoryRequests {
		return fmt.Errorf("%w: advisory_limit", ErrQuotaExceeded)
	}
	r.metrics.add("advisory_succeeded")
	mutation.ReplaceAdvisoryRequestsSet = true
	mutation.ReplaceAdvisoryRequests = append([]advisoryrequests.AdvisoryEvidenceRequest(nil), requestPlan.ResultingRequests...)
	if r.cfg.PipelineDepth == DepthAdvisoryRequests {
		return r.commit(ctx, input, state, mutation, episode, now)
	}

	if r.capabilityProvider == nil {
		r.metrics.add("mapping_skipped")
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	mappings, err := r.mapRequests(ctx, requestPlan.ResultingRequests)
	if err != nil {
		return err
	}
	if len(mappings) > r.cfg.MaxMappingsPerCycle {
		return fmt.Errorf("%w: mapping_limit", ErrQuotaExceeded)
	}
	mutation.ReplaceCapabilityMappingsSet = true
	mutation.ReplaceCapabilityMappings = mappings
	r.metrics.add("mapping_succeeded")
	if r.cfg.PipelineDepth == DepthCapabilityMapping {
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	if r.authorizationProvider == nil {
		r.metrics.add("authorization_skipped")
		return r.commit(ctx, input, state, mutation, episode, now)
	}
	authorizations, err := r.authorizeMappings(ctx, mappings)
	if err != nil {
		return err
	}
	if len(authorizations) > r.cfg.MaxAuthorizationsPerCycle {
		return fmt.Errorf("%w: authorization_limit", ErrQuotaExceeded)
	}
	mutation.ReplaceAuthorizationAssessmentsSet = true
	mutation.ReplaceAuthorizationAssessments = authorizations
	r.metrics.add("authorization_succeeded")
	return r.commit(ctx, input, state, mutation, episode, now)
}

func (r *Runtime) planEpisode(state durableworkflow.WorkflowState, input ShadowWorkflowInput) (episodes.IngestPlan, episodes.EpisodeSnapshot, pipelineResult, error) {
	policy := episodes.DefaultPolicy()
	registry := episodes.NewRegistryWithPolicy(policy)
	snapshot := episodeSnapshot(state, policy)
	for _, ep := range snapshot.Episodes {
		if err := registry.Add(ep); err != nil {
			return episodes.IngestPlan{}, episodes.EpisodeSnapshot{}, pipelineResult{}, err
		}
	}
	plan, err := episodes.PlanIngest(registry.Snapshot(), input.Observation, nil, policy)
	if err != nil {
		return plan, episodes.EpisodeSnapshot{}, pipelineResult{}, err
	}
	switch plan.Decision {
	case episodes.DecisionDuplicate:
		return plan, episodes.EpisodeSnapshot{}, pipelineResult{duplicate: true}, nil
	case episodes.DecisionAmbiguous:
		return plan, episodes.EpisodeSnapshot{}, pipelineResult{ambiguous: true}, episodes.ErrAmbiguousPlan
	case episodes.DecisionRejected:
		return plan, episodes.EpisodeSnapshot{}, pipelineResult{rejected: true}, episodes.ErrRejectedPlan
	}
	result, err := registry.ApplyIngestPlan(plan, input.Observation, input.ObservedAt)
	if err != nil {
		return plan, episodes.EpisodeSnapshot{}, pipelineResult{}, err
	}
	return plan, result.Episode, pipelineResult{}, nil
}

func episodeSnapshot(state durableworkflow.WorkflowState, policy episodes.Policy) episodes.Snapshot {
	out := episodes.Snapshot{Revision: state.Revision, PolicyFingerprint: policy.Fingerprint(), EventIndex: map[string]episodes.EpisodeID{}}
	for _, value := range state.Episodes {
		if value.Episode == nil {
			continue
		}
		ep := value.Episode.Clone()
		out.Episodes = append(out.Episodes, ep)
		for _, observation := range ep.Observations {
			out.EventIndex[observation.EventID] = ep.ID
		}
	}
	sort.Slice(out.Episodes, func(i, j int) bool { return out.Episodes[i].ID < out.Episodes[j].ID })
	return out
}
func previousHypotheses(state durableworkflow.WorkflowState, id episodes.EpisodeID) *situationhypotheses.CompetingHypothesisSet {
	for _, v := range state.Episodes {
		if v.EpisodeID == string(id) && v.Hypotheses != nil {
			c := v.Hypotheses.Clone()
			return &c
		}
	}
	return nil
}
func previousDiscrimination(state durableworkflow.WorkflowState, id episodes.EpisodeID) *evidencediscrimination.DiscriminationAssessment {
	for _, v := range state.Episodes {
		if v.EpisodeID == string(id) && v.Discrimination != nil {
			c := v.Discrimination.Clone()
			return &c
		}
	}
	return nil
}

func advisorySnapshot(state durableworkflow.WorkflowState, policy advisoryrequests.Policy) advisoryrequests.RegistrySnapshot {
	values := []advisoryrequests.AdvisoryEvidenceRequest{}
	for _, ep := range state.Episodes {
		for _, v := range ep.AdvisoryRequests {
			values = append(values, v.Clone())
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].EpisodeID != values[j].EpisodeID {
			return values[i].EpisodeID < values[j].EpisodeID
		}
		return values[i].ID < values[j].ID
	})
	snapshot := advisoryrequests.RegistrySnapshot{Revision: state.Revision, Requests: values, RequestIndex: map[advisoryrequests.AdvisoryRequestID]int{}, KeyIndex: map[advisoryrequests.AdvisoryRequestKey][]advisoryrequests.AdvisoryRequestID{}, EpisodeIndex: map[string][]advisoryrequests.AdvisoryRequestID{}, PolicyFingerprint: policy.Fingerprint()}
	for i, v := range values {
		snapshot.RequestIndex[v.ID] = i
		snapshot.KeyIndex[v.Key] = append(snapshot.KeyIndex[v.Key], v.ID)
		snapshot.EpisodeIndex[v.EpisodeID] = append(snapshot.EpisodeIndex[v.EpisodeID], v.ID)
	}
	snapshot.Digest = advisoryrequests.RegistryDigest(snapshot)
	return snapshot
}

func (r *Runtime) mapRequests(ctx context.Context, requests []advisoryrequests.AdvisoryEvidenceRequest) ([]capabilitymapping.CapabilityMappingAssessment, error) {
	out := []capabilitymapping.CapabilityMappingAssessment{}
	for _, request := range requests {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if request.Status != advisoryrequests.StatusProposed && request.Status != advisoryrequests.StatusAcknowledged && request.Status != advisoryrequests.StatusDeferred {
			continue
		}
		catalog, inventory, available, err := r.capabilityProvider.SnapshotFor(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("%w: mapping provider", ErrProviderUnavailable)
		}
		if !available {
			r.metrics.add("mapping_skipped")
			continue
		}
		assessment, err := capabilitymapping.Analyze(capabilitymapping.AnalysisInput{Request: request, Catalog: catalog, Inventory: inventory}, capabilitymapping.DefaultPolicy())
		if err != nil {
			return nil, fmt.Errorf("%w: mapping", ErrProviderInvalid)
		}
		out = append(out, assessment)
	}
	return out, nil
}
func (r *Runtime) authorizeMappings(ctx context.Context, mappings []capabilitymapping.CapabilityMappingAssessment) ([]authorizationboundary.AuthorizationBoundaryAssessment, error) {
	out := []authorizationboundary.AuthorizationBoundaryAssessment{}
	for _, mapping := range mappings {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		contextValue, policySet, grants, available, err := r.authorizationProvider.InputsFor(ctx, mapping)
		if err != nil {
			return nil, fmt.Errorf("%w: authorization provider", ErrProviderUnavailable)
		}
		if !available {
			r.metrics.add("authorization_skipped")
			continue
		}
		assessment, err := authorizationboundary.Analyze(authorizationboundary.AnalysisInput{Mapping: mapping, Context: contextValue, PolicySet: policySet, Grants: grants}, authorizationboundary.DefaultPolicy())
		if err != nil {
			return nil, fmt.Errorf("%w: authorization", ErrProviderInvalid)
		}
		out = append(out, assessment)
	}
	return out, nil
}

func (r *Runtime) commit(ctx context.Context, input ShadowWorkflowInput, state durableworkflow.WorkflowState, mutation durableworkflow.WorkflowMutation, episode episodes.EpisodeSnapshot, now time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := r.checkWALLimit(); err != nil {
		return err
	}
	mutation.SourceWorkflowRevision = state.Revision
	mutation.SourceWorkflowDigest = state.Digest
	id := transactionID(input.EventID, string(episode.ID), episode.Revision, state.Revision, r.cfg.PipelineDepth)
	tx, _, err := durableworkflow.PlanTransaction(state, mutation, id, state.LastSequence+1, now, r.durablePolicy())
	if err != nil {
		return fmt.Errorf("%w: plan", ErrDurableCommitFailed)
	}
	if _, err := r.coordinator.Commit(tx); err != nil {
		r.counters.commitFailed.Add(1)
		r.metrics.add("transaction_conflict")
		return fmt.Errorf("%w: commit", ErrDurableCommitFailed)
	}
	r.counters.commits.Add(1)
	r.mu.Lock()
	r.transactionsSinceCheckpoint++
	transactionsSinceCheckpoint := r.transactionsSinceCheckpoint
	lastCheckpointAt := r.lastCheckpointAt
	r.mu.Unlock()
	r.metrics.add("transaction_committed")
	if transactionsSinceCheckpoint >= r.cfg.CheckpointEveryTransactions || lastCheckpointAt.IsZero() || now.Sub(lastCheckpointAt) >= r.cfg.CheckpointInterval {
		if _, err := r.coordinator.CheckpointAt(now); err != nil {
			r.counters.checkpointFailed.Add(1)
			r.metrics.add("checkpoint.failed")
			r.mu.Lock()
			r.state = StateDegraded
			r.lastErrorCode = "checkpoint_failed"
			r.mu.Unlock()
		} else {
			r.counters.checkpoints.Add(1)
			r.mu.Lock()
			r.transactionsSinceCheckpoint = 0
			r.lastCheckpointAt = now
			r.lastErrorCode = ""
			r.state = StateRunning
			r.mu.Unlock()
			r.metrics.add("checkpoint.succeeded")
		}
	}
	return nil
}
func (r *Runtime) checkWALLimit() error {
	if r.cfg.StoreMode != StoreFile {
		return nil
	}
	if sized, ok := r.store.(interface{ WALSize() (int64, error) }); ok {
		size, err := sized.WALSize()
		if err != nil {
			return err
		}
		if size >= r.cfg.MaxWALBytes {
			r.mu.Lock()
			r.state = StateStorageLimitReached
			r.accepting = false
			r.mu.Unlock()
			return ErrWALSizeLimit
		}
	}
	return nil
}
func transactionID(eventID, episodeID string, episodeRevision, workflowRevision uint64, depth PipelineDepth) durableworkflow.WorkflowTransactionID {
	payload := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", eventID, episodeID, episodeRevision, workflowRevision, depth)
	digest := sha256.Sum256([]byte(payload))
	return durableworkflow.WorkflowTransactionID("shadow-workflow-tx-" + hex.EncodeToString(digest[:]))
}
