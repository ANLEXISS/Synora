package durableworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func digestJSON(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(digest[:])
}

func episodeStateFingerprint(state EpisodeWorkflowState) string {
	copy := state.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.AdvisoryRequests, func(i, j int) bool { return copy.AdvisoryRequests[i].ID < copy.AdvisoryRequests[j].ID })
	sort.Slice(copy.CapabilityMappings, func(i, j int) bool {
		return copy.CapabilityMappings[i].RequestID < copy.CapabilityMappings[j].RequestID
	})
	sort.Slice(copy.AuthorizationAssessments, func(i, j int) bool {
		return copy.AuthorizationAssessments[i].RequestID < copy.AuthorizationAssessments[j].RequestID
	})
	return digestJSON("durable-workflow-episode-state-v1:", copy)
}

func stateFingerprint(state WorkflowState) string {
	copy := state.Clone()
	copy.Digest = ""
	for i := range copy.Episodes {
		copy.Episodes[i].AdvisoryRequests = cloneRequests(copy.Episodes[i].AdvisoryRequests)
		copy.Episodes[i].CapabilityMappings = cloneMappings(copy.Episodes[i].CapabilityMappings)
		copy.Episodes[i].AuthorizationAssessments = cloneAuthorizations(copy.Episodes[i].AuthorizationAssessments)
		sort.Slice(copy.Episodes[i].AdvisoryRequests, func(a, b int) bool {
			return copy.Episodes[i].AdvisoryRequests[a].ID < copy.Episodes[i].AdvisoryRequests[b].ID
		})
		sort.Slice(copy.Episodes[i].CapabilityMappings, func(a, b int) bool {
			return copy.Episodes[i].CapabilityMappings[a].RequestID < copy.Episodes[i].CapabilityMappings[b].RequestID
		})
		sort.Slice(copy.Episodes[i].AuthorizationAssessments, func(a, b int) bool {
			return copy.Episodes[i].AuthorizationAssessments[a].RequestID < copy.Episodes[i].AuthorizationAssessments[b].RequestID
		})
	}
	sort.Slice(copy.Episodes, func(i, j int) bool { return copy.Episodes[i].EpisodeID < copy.Episodes[j].EpisodeID })
	return digestJSON("durable-workflow-state-v1:", copy)
}

func mutationFingerprint(mutation WorkflowMutation) string {
	copy := canonicalMutation(mutation)
	return digestJSON("durable-workflow-mutation-v1:", copy)
}

func transactionFingerprint(transaction WorkflowTransaction) string {
	copy := transaction.Clone()
	copy.Fingerprint = ""
	return digestJSON("durable-workflow-transaction-v1:", copy)
}

func checkpointFingerprint(checkpoint Checkpoint) string {
	copy := checkpoint.Clone()
	copy.Fingerprint = ""
	copy.Checksum = ""
	return digestJSON("durable-workflow-checkpoint-v1:", copy)
}

func recordPayloadFingerprint(payload []byte) string {
	return digestJSON("durable-workflow-payload-v1:", string(payload))
}

func checkpointChecksum(checkpoint Checkpoint) string {
	copy := checkpoint.Clone()
	copy.Checksum = ""
	return digestJSON("durable-workflow-checkpoint-checksum-v1:", copy)
}

func recordChecksum(record Record) string {
	copy := record
	copy.Checksum = ""
	return digestJSON("durable-workflow-record-v1:", struct {
		Version            uint16
		Sequence           uint64
		Kind               RecordKind
		PayloadLength      uint64
		Payload            []byte
		PayloadFingerprint string
	}{copy.Version, copy.Sequence, copy.Kind, copy.PayloadLength, copy.Payload, copy.PayloadFingerprint})
}

func WorkflowStateFingerprint(state WorkflowState) string { return stateFingerprint(state) }
func EpisodeWorkflowStateFingerprint(state EpisodeWorkflowState) string {
	return episodeStateFingerprint(state)
}
func WorkflowMutationFingerprint(value WorkflowMutation) string { return mutationFingerprint(value) }
func WorkflowTransactionFingerprint(value WorkflowTransaction) string {
	return transactionFingerprint(value)
}
func CheckpointFingerprint(value Checkpoint) string { return checkpointFingerprint(value) }
func RecordChecksum(value Record) string            { return recordChecksum(value) }
func RecordPayloadFingerprint(value []byte) string  { return recordPayloadFingerprint(value) }
func CheckpointChecksum(value Checkpoint) string    { return checkpointChecksum(value) }
