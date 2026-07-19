package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

// ValidateJournal delegates integrity and continuity checks to the real file
// journal reader and returns bounded validation failures for reports.
func ValidateJournal(ctx context.Context, source *journal.FileJournal) []InvariantFailure {
	if source == nil {
		return []InvariantFailure{{Code: "journal_missing", Path: "journal", Message: "journal is nil"}}
	}
	if _, err := source.ReadAll(ctx); err != nil {
		return []InvariantFailure{{Code: "journal_invalid", Path: "journal", Message: "journal continuity or hash validation failed"}}
	}
	return nil
}

func ValidateJournalHead(ctx context.Context, source *journal.FileJournal, sequence uint64, hash string) []InvariantFailure {
	if source == nil {
		return []InvariantFailure{{Code: "journal_missing", Path: "journal", Message: "journal is nil"}}
	}
	head, err := source.ReadHead(ctx)
	if err != nil {
		return []InvariantFailure{{Code: "journal_head_invalid", Path: "journal.head", Message: "journal head could not be validated"}}
	}
	if head.Sequence != sequence || head.Hash != hash {
		return []InvariantFailure{{Code: "journal_head_mismatch", Path: "journal.head", Message: "journal head differs from coordinator state"}}
	}
	return nil
}

// ValidateCoordinatorLocalState checks only objects named by the completed
// step. It is intentionally cheaper than ValidateCoordinatorState and is used
// by the standard runner between global validation boundaries.
func ValidateCoordinatorLocalState(c *durable.Coordinator, result StepResult) []InvariantFailure {
	if c == nil {
		return []InvariantFailure{{Code: "coordinator_not_ready", Path: "coordinator", Message: "coordinator is nil"}}
	}
	// Planning is pure and may intentionally name a not-yet-created candidate
	// chain. Its detached IDs are not local mutation targets.
	if result.StepKind == StepPlanAssociation || result.StepKind == StepPlanResolution {
		return nil
	}
	failures := make([]InvariantFailure, 0)
	for _, id := range result.ChainIDs {
		if id == "" {
			continue
		}
		snapshot, err := c.Get(id)
		if err != nil {
			failures = append(failures, InvariantFailure{Code: "chain_missing", Path: fmt.Sprintf("chains[%s]", id), Message: "touched chain is not readable"})
			continue
		}
		if _, err := chains.Restore(snapshot); err != nil {
			failures = append(failures, InvariantFailure{Code: "invalid_chain", Path: fmt.Sprintf("chains[%s]", id), Message: "touched chain failed validation"})
		}
	}
	for _, id := range result.HypothesisIDs {
		snapshot, err := c.GetHypothesis(id)
		if err != nil {
			failures = append(failures, InvariantFailure{Code: "hypothesis_missing", Path: fmt.Sprintf("hypotheses[%s]", id), Message: "touched hypothesis is not readable"})
			continue
		}
		if _, err := hypotheses.Restore(snapshot); err != nil {
			failures = append(failures, InvariantFailure{Code: "invalid_hypothesis", Path: fmt.Sprintf("hypotheses[%s]", id), Message: "touched hypothesis failed validation"})
		}
	}
	return failures
}

type ComparisonResult struct {
	Equal   bool   `json:"equal"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message,omitempty"`
}

func digest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}

func StateDigestOf(c *durable.Coordinator, journalSequence uint64, journalHead string) (StateDigest, error) {
	if c == nil {
		return StateDigest{}, fmt.Errorf("coordinator is nil")
	}
	chainValues := c.List()
	hypothesisValues := c.ListHypotheses()
	sort.Slice(chainValues, func(i, j int) bool { return chainValues[i].ID < chainValues[j].ID })
	sort.Slice(hypothesisValues, func(i, j int) bool { return hypothesisValues[i].ID < hypothesisValues[j].ID })
	chainsHash, err := digest(chainValues)
	if err != nil {
		return StateDigest{}, err
	}
	hypothesesHash, err := digest(hypothesisValues)
	if err != nil {
		return StateDigest{}, err
	}
	return StateDigest{ChainCount: len(chainValues), HypothesisCount: len(hypothesisValues), ChainsSHA256: chainsHash, HypothesesSHA256: hypothesesHash, JournalSequence: journalSequence, JournalHeadHash: journalHead}, nil
}

func CompareCoordinators(expected, actual *durable.Coordinator) ComparisonResult {
	if expected == nil || actual == nil {
		return ComparisonResult{Message: "one coordinator is nil"}
	}
	expectedChains, actualChains := expected.List(), actual.List()
	if len(expectedChains) != len(actualChains) {
		return ComparisonResult{Path: "chains", Message: "chain count differs"}
	}
	sort.Slice(expectedChains, func(i, j int) bool { return expectedChains[i].ID < expectedChains[j].ID })
	sort.Slice(actualChains, func(i, j int) bool { return actualChains[i].ID < actualChains[j].ID })
	for i := range expectedChains {
		if expectedChains[i].ID != actualChains[i].ID {
			return ComparisonResult{Path: "chains", Message: "chain identity differs"}
		}
		if !reflect.DeepEqual(expectedChains[i], actualChains[i]) {
			return ComparisonResult{Path: fmt.Sprintf("chains[%s]", expectedChains[i].ID), Message: firstDifference(expectedChains[i], actualChains[i])}
		}
	}
	expectedHyps, actualHyps := expected.ListHypotheses(), actual.ListHypotheses()
	if len(expectedHyps) != len(actualHyps) {
		return ComparisonResult{Path: "hypotheses", Message: "hypothesis count differs"}
	}
	sort.Slice(expectedHyps, func(i, j int) bool { return expectedHyps[i].ID < expectedHyps[j].ID })
	sort.Slice(actualHyps, func(i, j int) bool { return actualHyps[i].ID < actualHyps[j].ID })
	for i := range expectedHyps {
		if expectedHyps[i].ID != actualHyps[i].ID {
			return ComparisonResult{Path: "hypotheses", Message: "hypothesis identity differs"}
		}
		if !reflect.DeepEqual(expectedHyps[i], actualHyps[i]) {
			return ComparisonResult{Path: fmt.Sprintf("hypotheses[%s]", expectedHyps[i].ID), Message: firstDifference(expectedHyps[i], actualHyps[i])}
		}
	}
	es, as := expected.Status(), actual.Status()
	if es.JournalSequence != as.JournalSequence || es.JournalHeadHash != as.JournalHeadHash {
		return ComparisonResult{Path: "journal.head", Message: "journal head differs"}
	}
	return ComparisonResult{Equal: true}
}

func firstDifference(left, right any) string {
	if reflect.DeepEqual(left, right) {
		return ""
	}
	return "state differs" // Keep reports bounded and never expose raw payloads.
}

func ValidateCoordinatorState(c *durable.Coordinator) []InvariantFailure {
	failures := make([]InvariantFailure, 0)
	if c == nil {
		return []InvariantFailure{{Code: "coordinator_not_ready", Path: "coordinator", Message: "coordinator is nil"}}
	}
	chainsByID := make(map[chains.ChainID]chains.Snapshot)
	for _, snapshot := range c.List() {
		if _, err := chains.Restore(snapshot); err != nil {
			failures = append(failures, InvariantFailure{Code: "invalid_chain", Path: fmt.Sprintf("chains[%s]", snapshot.ID), Message: "chain snapshot failed validation"})
		}
		chainsByID[snapshot.ID] = snapshot
	}
	for _, snapshot := range c.ListHypotheses() {
		if _, err := hypotheses.Restore(snapshot); err != nil {
			failures = append(failures, InvariantFailure{Code: "invalid_hypothesis", Path: fmt.Sprintf("hypotheses[%s]", snapshot.ID), Message: "hypothesis snapshot failed validation"})
			continue
		}
		if snapshot.Status != hypotheses.StatusResolved || snapshot.Resolution == nil {
			continue
		}
		resolution := snapshot.Resolution
		switch resolution.EffectKind {
		case hypotheses.ResolutionEffectAttachObservation:
			if resolution.Outcome.AttachObservation == nil {
				failures = append(failures, InvariantFailure{Code: "resolution_outcome_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution.outcome", snapshot.ID), Message: "attach outcome is missing"})
				continue
			}
			chain, ok := chainsByID[resolution.Outcome.AttachObservation.ChainID]
			if !ok || !hasObservation(chain, resolution.Outcome.AttachObservation.ObservationID) {
				failures = append(failures, InvariantFailure{Code: "resolution_chain_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution", snapshot.ID), Message: "attached observation is absent from the selected chain"})
			}
		case hypotheses.ResolutionEffectCreateCandidate:
			if resolution.Outcome.CreateCandidate == nil {
				failures = append(failures, InvariantFailure{Code: "resolution_outcome_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution.outcome", snapshot.ID), Message: "create outcome is missing"})
				continue
			}
			chain, ok := chainsByID[resolution.Outcome.CreateCandidate.ChainID]
			if !ok || chain.Status != chains.StatusCandidate {
				failures = append(failures, InvariantFailure{Code: "resolution_chain_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution", snapshot.ID), Message: "candidate chain is absent or not candidate"})
			}
		case hypotheses.ResolutionEffectAddContribution:
			if resolution.Outcome.AddContribution == nil {
				failures = append(failures, InvariantFailure{Code: "resolution_outcome_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution.outcome", snapshot.ID), Message: "contribution outcome is missing"})
				continue
			}
			chain, ok := chainsByID[resolution.Outcome.AddContribution.ChainID]
			if !ok || !hasContribution(chain, resolution.Outcome.AddContribution.ContributionID) {
				failures = append(failures, InvariantFailure{Code: "resolution_chain_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution", snapshot.ID), Message: "resolved contribution is absent from the selected chain"})
			}
		case hypotheses.ResolutionEffectNoChain:
			if resolution.Outcome.NoChainEffect == nil {
				failures = append(failures, InvariantFailure{Code: "resolution_outcome_mismatch", Path: fmt.Sprintf("hypotheses[%s].resolution.outcome", snapshot.ID), Message: "no-chain outcome is missing"})
			}
		}
	}
	hypothesesByID := make(map[hypotheses.SetID]hypotheses.Snapshot)
	for _, snapshot := range c.ListHypotheses() {
		hypothesesByID[snapshot.ID] = snapshot
	}
	for _, snapshot := range c.ListHypotheses() {
		if snapshot.Lineage.PredecessorSetID != "" {
			predecessor, ok := hypothesesByID[snapshot.Lineage.PredecessorSetID]
			if !ok || predecessor.Lineage.SuccessorSetID != snapshot.ID {
				failures = append(failures, InvariantFailure{Code: "lineage_incoherent", Path: fmt.Sprintf("hypotheses[%s].lineage", snapshot.ID), Message: "predecessor does not point to this successor"})
			}
		}
		if snapshot.Lineage.SuccessorSetID != "" {
			successor, ok := hypothesesByID[snapshot.Lineage.SuccessorSetID]
			if !ok || successor.Lineage.PredecessorSetID != snapshot.ID {
				failures = append(failures, InvariantFailure{Code: "lineage_incoherent", Path: fmt.Sprintf("hypotheses[%s].lineage", snapshot.ID), Message: "successor does not point to this predecessor"})
			}
		}
	}
	return failures
}

func hasObservation(snapshot chains.Snapshot, id string) bool {
	for _, item := range snapshot.Observations {
		if item.ID == id {
			return true
		}
	}
	return false
}
func hasContribution(snapshot chains.Snapshot, id string) bool {
	for _, item := range snapshot.Contributions {
		if item.ID == id {
			return true
		}
	}
	return false
}
