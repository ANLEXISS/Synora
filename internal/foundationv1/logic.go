package foundationv1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

func AssessCamera(availability, stream, tamper string, degraded bool, at time.Time, simulated bool) CameraHealth {
	if availability != "online" && availability != "offline" && availability != "unknown" {
		availability = "unknown"
	}
	if stream != "flowing" && stream != "absent" && stream != "frozen" && stream != "unknown" {
		stream = "unknown"
	}
	if tamper != "none" && tamper != "suspected" && tamper != "unknown" {
		tamper = "unknown"
	}
	status := StatusAvailable
	if simulated {
		status = StatusSimulatedTest
	}
	if availability != "online" || stream != "flowing" || tamper != "none" {
		if !simulated {
			status = StatusUnavailable
		}
		degraded = true
	}
	sceneSafety := "unknown"
	if availability == "online" && stream == "flowing" && tamper == "none" && !degraded {
		sceneSafety = "unknown" // camera health never asserts that a scene is safe
	}
	return CameraHealth{Status: status, Availability: availability, Stream: stream, Tamper: tamper, Degraded: degraded, SceneSafety: sceneSafety,
		ObservedAt: at.UTC(), Evidence: RedactedEvidence{SchemaVersion: "synora.redacted-evidence/v1", Source: "camera-health-port", Signal: stream + ":" + tamper, Confidence: "coarse", Redacted: true}}
}

// PeripheralRegistry is a validating adapter, not a separate authority. Every
// accepted change is journaled in the supplied Core-owned Universal Store port.
type PeripheralRegistry struct{ store StorePort }

func NewPeripheralRegistry(store StorePort) *PeripheralRegistry {
	return &PeripheralRegistry{store: store}
}

func (r *PeripheralRegistry) Upsert(ctx context.Context, item Peripheral, now time.Time) error {
	if r == nil || r.store == nil || !validOpaqueID(item.ID) || !validOpaqueID(item.Type) || !validOpaqueID(item.Zone) {
		return errors.New("abstract peripheral id, type, and zone are required")
	}
	if !item.Status.Valid() || (item.State != "known" && item.State != "unknown") {
		return errors.New("invalid peripheral function or knowledge state")
	}
	seen := make(map[string]bool)
	for _, capability := range item.Capabilities {
		if !validOpaqueID(capability.Name) || seen[capability.Name] || !capability.Mode.Valid() || !validCapabilityState(capability.State) {
			return errors.New("invalid peripheral capability")
		}
		seen[capability.Name] = true
	}
	sort.Slice(item.Capabilities, func(i, j int) bool { return item.Capabilities[i].Name < item.Capabilities[j].Name })
	body, err := json.Marshal(item)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	record := Record{ID: "peripheral-" + item.ID + "-" + hex.EncodeToString(digest[:8]), Kind: "peripheral", Status: item.Status, Payload: item, CreatedAt: now.UTC()}
	_, err = r.store.PutFoundation(ctx, record)
	return err
}

func (r *PeripheralRegistry) Snapshot(ctx context.Context) ([]Peripheral, error) {
	if r == nil || r.store == nil {
		return nil, errors.New("Universal Store port is not configured")
	}
	records, _, err := r.store.FoundationSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]Peripheral)
	for _, record := range records {
		if record.Kind != "peripheral" {
			continue
		}
		var item Peripheral
		if !decodePayload(record.Payload, &item) {
			return nil, errors.New("invalid peripheral record in Universal Store")
		}
		latest[item.ID] = item
	}
	items := make([]Peripheral, 0, len(latest))
	for _, item := range latest {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func validCapabilityState(value string) bool {
	return value == "available" || value == "unavailable" || value == "unknown"
}

type DryRunExecutor struct {
	mu      sync.Mutex
	results map[string]ActionResult
	fprints map[string]string
}

func NewDryRunExecutor() *DryRunExecutor {
	return &DryRunExecutor{results: make(map[string]ActionResult), fprints: make(map[string]string)}
}

func (e *DryRunExecutor) Execute(proposal ActionProposal, gateAllowed bool, capabilityState string) (ActionResult, error) {
	if !validOpaqueID(proposal.RequestID) || !validOpaqueID(proposal.Action) || !validOpaqueID(proposal.Capability) || !validOpaqueID(proposal.Zone) || (proposal.EpisodeID != "" && !validOpaqueID(proposal.EpisodeID)) {
		return ActionResult{}, errors.New("action request, action, capability, and zone are required")
	}
	data, _ := json.Marshal(proposal)
	digest := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(digest[:])
	e.mu.Lock()
	defer e.mu.Unlock()
	if prior, ok := e.results[proposal.RequestID]; ok {
		if e.fprints[proposal.RequestID] != fingerprint {
			return ActionResult{}, errors.New("idempotency key reused with different action")
		}
		prior.Status = "duplicate"
		return prior, nil
	}
	result := ActionResult{RequestID: proposal.RequestID, PeripheralState: "unknown", PhysicalActionExecuted: false, FunctionStatus: StatusDryRun}
	switch {
	case proposal.Action == "no_action":
		result.Status, result.Reason = "not_requested", "no_action_proposed"
	case !gateAllowed:
		result.Status, result.Reason = "blocked", "safety_gate_denied"
	case capabilityState != "available":
		result.Status, result.Reason, result.FunctionStatus = "unavailable", "capability_"+normalizeCapabilityState(capabilityState), StatusUnavailable
	default:
		result.Status, result.Reason = "dry_run", "physical_execution_disabled"
	}
	e.results[proposal.RequestID], e.fprints[proposal.RequestID] = result, fingerprint
	return result, nil
}

func normalizeCapabilityState(value string) string {
	if value == "available" || value == "unavailable" || value == "unknown" {
		return value
	}
	return "unknown"
}

func ProposeCommunication(intent CommunicationIntent, safetyAllowed bool) (CommunicationRequest, error) {
	if !validOpaqueID(intent.ID) || len(intent.Zones) == 0 || !validPriority(intent.Priority) || !validRecipient(intent.Recipient) || !validTextKey(intent.TextKey) || intent.Cooldown < 0 {
		return CommunicationRequest{}, errors.New("communication intent requires id, zones, priority, abstract recipient, text key, and nonnegative cooldown")
	}
	zones := append([]string(nil), intent.Zones...)
	for _, zone := range zones {
		if !validOpaqueID(zone) {
			return CommunicationRequest{}, errors.New("communication zones must be abstract identifiers")
		}
	}
	sort.Strings(zones)
	request := CommunicationRequest{IntentID: intent.ID, Zones: zones, Priority: intent.Priority, Cooldown: intent.Cooldown, Recipient: intent.Recipient, TextKey: intent.TextKey, TTSStatus: StatusNotConfigured, AudioRendered: false}
	if safetyAllowed {
		request.Status = "queued_dry_run"
	} else {
		request.Status, request.Reason = "suppressed", "safety_gate_denied"
	}
	return request, nil
}

type CommunicationScheduler struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func NewCommunicationScheduler() *CommunicationScheduler {
	return &CommunicationScheduler{last: make(map[string]time.Time)}
}

func (s *CommunicationScheduler) Schedule(intent CommunicationIntent, safetyAllowed bool, now time.Time) (CommunicationRequest, error) {
	request, err := ProposeCommunication(intent, safetyAllowed)
	if err != nil || !safetyAllowed {
		return request, err
	}
	if s == nil {
		return CommunicationRequest{}, errors.New("communication scheduler is not configured")
	}
	key := intent.Recipient + ":" + strings.Join(request.Zones, ",") + ":" + intent.TextKey
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, exists := s.last[key]; exists && now.Sub(previous) < intent.Cooldown {
		request.Status, request.Reason = "suppressed", "cooldown"
		return request, nil
	}
	request.Status = "queued_dry_run"
	s.last[key] = now.UTC()
	return request, nil
}

func validPriority(value string) bool {
	switch value {
	case "low", "normal", "high", "urgent":
		return true
	default:
		return false
	}
}

func validRecipient(value string) bool {
	switch value {
	case "occupant", "household", "caregiver", "operator":
		return true
	default:
		return false
	}
}

func validTextKey(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (t Topology) Allows(from, to string) bool {
	if t.Validate() != nil || t.Status == StatusUnavailable || t.Status == StatusNotConfigured || from == "" || to == "" {
		return false
	}
	known := map[string]bool{}
	for _, zone := range t.Zones {
		known[zone.ID] = true
	}
	if !known[from] || !known[to] {
		return false
	}
	for _, transition := range t.Connections {
		if transition.From == from && transition.To == to {
			return true
		}
	}
	return false
}

func (t Topology) Validate() error {
	if !t.Status.Valid() {
		return errors.New("invalid topology function state")
	}
	known := make(map[string]bool, len(t.Zones))
	for _, zone := range t.Zones {
		if !validOpaqueID(zone.ID) || known[zone.ID] {
			return errors.New("topology zone ids must be nonempty and unique")
		}
		known[zone.ID] = true
	}
	seen := make(map[Transition]bool, len(t.Connections))
	for _, edge := range t.Connections {
		if !known[edge.From] || !known[edge.To] || edge.From == edge.To || seen[edge] {
			return errors.New("topology transition references invalid or duplicate zones")
		}
		seen[edge] = true
	}
	return nil
}

func Correlate(id, episode string, observations []RedactedObservation, confidence string, evidenceRef string) (Correlation, error) {
	if !validOpaqueID(id) || !validOpaqueID(episode) || len(observations) == 0 || (confidence != "low" && confidence != "medium" && confidence != "high" && confidence != "unknown") {
		return Correlation{}, errors.New("correlation id, episode, and observations are required")
	}
	ids := make([]string, 0, len(observations))
	for _, observation := range observations {
		if !validOpaqueID(observation.ID) || !validOpaqueID(observation.Zone) || !validSearchTarget(observation.Category) || !observation.Redacted || (observation.Confidence != "low" && observation.Confidence != "medium" && observation.Confidence != "high" && observation.Confidence != "unknown") {
			return Correlation{}, errors.New("correlation requires redacted observations")
		}
		ids = append(ids, observation.ID)
	}
	sort.Strings(ids)
	uniqueIDs := ids[:0]
	for _, id := range ids {
		if len(uniqueIDs) == 0 || uniqueIDs[len(uniqueIDs)-1] != id {
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	ids = uniqueIDs
	classification := "hypothesis"
	if len(ids) > 1 {
		classification = "ambiguous"
	}
	if evidenceRef != "" && !validOpaqueID(evidenceRef) {
		return Correlation{}, errors.New("correlation evidence reference must be opaque")
	}
	return Correlation{ID: id, EpisodeID: episode, ObservationIDs: ids, Confidence: confidence, Classification: classification, EvidenceRef: evidenceRef, Biometric: false}, nil
}

// ConfirmObserved is deliberately separate from Correlate: a confidence or
// matching observation alone can never promote a hypothesis to a fact.
func ConfirmObserved(correlation Correlation, validated bool, evidenceRef string) (Correlation, error) {
	if !validated || correlation.Classification != "hypothesis" || len(correlation.ObservationIDs) != 1 || !validOpaqueID(evidenceRef) {
		return Correlation{}, errors.New("correlation requires independently validated single-observation evidence")
	}
	correlation.Classification = "observed"
	correlation.EvidenceRef = evidenceRef
	return correlation, nil
}

func NewSearchState(query SearchQuery, expiresAt time.Time, coverage string, observation *RedactedObservation, matchCount int) (SearchState, error) {
	if !validOpaqueID(query.ID) || len(query.Zones) == 0 || !validSearchTarget(query.Target) || matchCount < 0 || query.IssuedAt.IsZero() {
		return SearchState{}, errors.New("abstract search id, zones, and target are required")
	}
	if coverage != "unknown" && coverage != "partial" && coverage != "complete" {
		return SearchState{}, fmt.Errorf("invalid search coverage %q", coverage)
	}
	for _, zone := range query.Zones {
		if !validOpaqueID(zone) {
			return SearchState{}, errors.New("search zones must be abstract identifiers")
		}
	}
	status := StatusSimulatedTest
	if !expiresAt.IsZero() && !query.IssuedAt.IsZero() && !expiresAt.After(query.IssuedAt) {
		status = StatusFailed
	}
	state := SearchState{Status: status, QueryID: query.ID, Occupancy: "unknown", Coverage: coverage, Association: "none", ExpiresAt: expiresAt.UTC(), CrossCameraIdentity: false, PTZUsed: false}
	if observation != nil {
		if !observation.Redacted || !validOpaqueID(observation.ID) || !validOpaqueID(observation.Zone) || !validSearchTarget(observation.Category) {
			return SearchState{}, errors.New("search observation must be redacted")
		}
		copy := *observation
		state.LastObservation = &copy
		state.Occupancy = "occupied"
		if matchCount > 1 {
			state.Association = "ambiguous"
		} else {
			state.Association = "single_observation"
		}
	}
	return state, nil
}

func validSearchTarget(value string) bool {
	switch value {
	case "person_suspect", "vehicle", "animal", "object", "unknown":
		return true
	default:
		return false
	}
}

func SearchExpired(state SearchState, now time.Time) bool {
	return !state.ExpiresAt.IsZero() && !state.ExpiresAt.After(now.UTC())
}

func ExpireSearch(state SearchState, now time.Time) SearchState {
	if SearchExpired(state, now) {
		state.Status = StatusUnavailable
		state.Occupancy = "unknown"
		state.Coverage = "unknown"
		state.Association = "none"
	}
	return state
}

// ValidatePublicState allows only this package's bounded projection and recursively
// rejects prohibited keys in data supplied to the API adapter.
func ValidatePublicState(state StateSnapshot) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	if forbiddenKey(value) {
		return ErrForbiddenProjection
	}
	for _, status := range []FunctionStatus{state.Functions.CameraHealth, state.Functions.Peripherals, state.Functions.Action, state.Functions.Voice, state.Functions.Search, state.Functions.Topology, state.Functions.Discovery, state.Functions.Core, state.Functions.MLP, state.Functions.SafetyGate, state.Functions.Store, state.Functions.Executor, state.Functions.API, state.Camera.Status, state.Topology.Status} {
		if !status.Valid() {
			return fmt.Errorf("invalid function state %q", status)
		}
	}
	if state.PhysicalActionExecuted || state.AudioRendered || state.Camera.SceneSafety != "unknown" || !state.Camera.Evidence.Redacted {
		return ErrForbiddenProjection
	}
	if !validOpaqueID(state.Camera.Evidence.Source) || !validCameraSignal(state.Camera.Evidence.Signal) || state.Camera.Evidence.SchemaVersion != "synora.redacted-evidence/v1" || state.Camera.Evidence.Confidence != "coarse" {
		return ErrForbiddenProjection
	}
	if (state.Camera.Availability != "online" && state.Camera.Availability != "offline" && state.Camera.Availability != "unknown") || (state.Camera.Stream != "flowing" && state.Camera.Stream != "absent" && state.Camera.Stream != "frozen" && state.Camera.Stream != "unknown") || (state.Camera.Tamper != "none" && state.Camera.Tamper != "suspected" && state.Camera.Tamper != "unknown") {
		return ErrForbiddenProjection
	}
	for _, peripheral := range state.Peripherals {
		if !peripheral.Status.Valid() || !validKnowledge(peripheral.State) || !validOpaqueID(peripheral.ID) || !validOpaqueID(peripheral.Type) || !validOpaqueID(peripheral.Zone) {
			return ErrForbiddenProjection
		}
		for _, capability := range peripheral.Capabilities {
			if !validOpaqueID(capability.Name) || !validCapabilityState(capability.State) || !capability.Mode.Valid() {
				return ErrForbiddenProjection
			}
		}
	}
	if state.LastAction != nil && (state.LastAction.PhysicalActionExecuted || !state.LastAction.FunctionStatus.Valid() || !validOpaqueID(state.LastAction.RequestID)) {
		return ErrForbiddenProjection
	}
	if state.Communication != nil {
		communication := state.Communication
		if communication.AudioRendered || communication.TTSStatus != StatusNotConfigured || !validRecipient(communication.Recipient) || !validTextKey(communication.TextKey) || (communication.Status != "suppressed" && communication.Status != "queued_dry_run") || len(communication.Zones) == 0 {
			return ErrForbiddenProjection
		}
		for _, zone := range communication.Zones {
			if !validOpaqueID(zone) {
				return ErrForbiddenProjection
			}
		}
	}
	if state.Search != nil {
		search := state.Search
		if search.CrossCameraIdentity || search.PTZUsed || !search.Status.Valid() || !validSearchTarget(searchTargetFromObservation(search.LastObservation)) || (search.Occupancy != "unknown" && search.Occupancy != "occupied" && search.Occupancy != "unoccupied") || (search.Coverage != "unknown" && search.Coverage != "partial" && search.Coverage != "complete") || (search.Association != "none" && search.Association != "ambiguous" && search.Association != "single_observation") {
			return ErrForbiddenProjection
		}
		if search.LastObservation != nil && (!search.LastObservation.Redacted || !validOpaqueID(search.LastObservation.ID) || !validOpaqueID(search.LastObservation.Zone)) {
			return ErrForbiddenProjection
		}
	}
	if err := state.Topology.Validate(); err != nil {
		return ErrForbiddenProjection
	}
	for _, correlation := range state.Correlations {
		if correlation.Biometric || (correlation.Classification != "hypothesis" && correlation.Classification != "observed" && correlation.Classification != "ambiguous") || !validOpaqueID(correlation.ID) || !validOpaqueID(correlation.EpisodeID) {
			return ErrForbiddenProjection
		}
		if correlation.Classification == "ambiguous" && len(correlation.ObservationIDs) < 2 {
			return ErrForbiddenProjection
		}
		if correlation.Classification == "observed" && (len(correlation.ObservationIDs) != 1 || !validOpaqueID(correlation.EvidenceRef)) {
			return ErrForbiddenProjection
		}
		for _, id := range correlation.ObservationIDs {
			if !validOpaqueID(id) {
				return ErrForbiddenProjection
			}
		}
	}
	return nil
}

func ValidateRecord(record Record) error {
	if !validOpaqueID(record.ID) || !validOpaqueID(record.Kind) || !record.Status.Valid() {
		return errors.New("foundation record requires abstract identifiers and an explicit function state")
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	if forbiddenKey(value) {
		return ErrForbiddenProjection
	}
	return nil
}

func validCameraSignal(value string) bool {
	parts := strings.Split(value, ":")
	return len(parts) == 2 && (parts[0] == "flowing" || parts[0] == "absent" || parts[0] == "frozen" || parts[0] == "unknown") && (parts[1] == "none" || parts[1] == "suspected" || parts[1] == "unknown")
}

func validKnowledge(value string) bool { return value == "known" || value == "unknown" }

func validOpaqueID(value string) bool {
	if value == "" || len(value) > 128 || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func searchTargetFromObservation(observation *RedactedObservation) string {
	if observation == nil {
		return "unknown"
	}
	return observation.Category
}

func forbiddenKey(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			switch strings.ToLower(key) {
			case "media", "raw_media", "identity", "biometric_identity", "embedding", "plate", "license_plate", "local_path", "path", "secret", "token", "hardware_id", "serial_number", "mac":
				return true
			}
			if forbiddenKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if forbiddenKey(child) {
				return true
			}
		}
	}
	return false
}
