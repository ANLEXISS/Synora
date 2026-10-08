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
		if capability.State == "available" && capability.Mode != StatusAvailable && capability.Mode != StatusSimulatedTest && capability.Mode != StatusDryRun {
			return errors.New("available capability requires an explicit available or simulated mode")
		}
	}
	if item.DesiredState == "" {
		item.DesiredState = "unknown"
	}
	if item.ObservedState == "" {
		item.ObservedState = "unknown"
	}
	if item.Health == "" {
		item.Health = "unknown"
	}
	if item.Provenance == "" {
		item.Provenance = "unknown"
	}
	if !validPeripheralState(item.DesiredState) || !validPeripheralState(item.ObservedState) || !validPeripheralHealth(item.Health) || !validProvenance(item.Provenance) {
		return errors.New("invalid peripheral state, health, or provenance")
	}
	confirmationAge := now.UTC().Sub(item.LastConfirmation.UTC())
	if item.Status == StatusAvailable && (item.Health != "healthy" || item.ObservedState == "unknown" || item.LastConfirmation.IsZero() || item.Provenance != "device_feedback" || confirmationAge < 0 || confirmationAge > 30*time.Second) {
		return errors.New("peripheral cannot be available without confirmed device feedback")
	}
	for _, permission := range item.Permissions {
		if !validPermission(permission) {
			return errors.New("invalid abstract peripheral permission")
		}
	}
	sort.Slice(item.Capabilities, func(i, j int) bool { return item.Capabilities[i].Name < item.Capabilities[j].Name })
	sort.Strings(item.Permissions)
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

func validPeripheralState(value string) bool {
	return value == "on" || value == "off" || value == "open" || value == "closed" || value == "unknown"
}
func validPeripheralHealth(value string) bool {
	return value == "healthy" || value == "degraded" || value == "unavailable" || value == "unknown"
}
func validProvenance(value string) bool {
	return value == "observed" || value == "inferred" || value == "user_provided" || value == "device_feedback" || value == "unknown"
}

func validPermission(value string) bool {
	if !strings.HasPrefix(value, "action.") {
		return false
	}
	return validOpaqueID(strings.TrimPrefix(value, "action."))
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
		prior.IdempotentReplay = true
		return prior, nil
	}
	result := ActionResult{RequestID: proposal.RequestID, PeripheralState: "unknown", PhysicalActionExecuted: false, FunctionStatus: StatusDryRun}
	switch {
	case proposal.Action == "no_action":
		result.Status, result.Reason = ActionSuppressed, "no_action_proposed"
	case !gateAllowed:
		result.Status, result.Reason = ActionBlocked, "safety_gate_denied"
	case capabilityState != "available":
		result.Status, result.Reason, result.FunctionStatus = ActionSuppressed, "capability_"+normalizeCapabilityState(capabilityState), StatusUnavailable
	default:
		result.Status, result.Reason = ActionAllowedDryRun, "physical_execution_disabled"
	}
	e.results[proposal.RequestID], e.fprints[proposal.RequestID] = result, fingerprint
	return result, nil
}

// ActionPolicy is a deterministic, non-actuating permission/capability check.
// It never upgrades simulated or unknown peripheral state to available.
type ActionPolicy struct {
	Peripherals []Peripheral
}

func (p ActionPolicy) Authorize(proposal ActionProposal, now time.Time) (bool, string, string) {
	if !validActionProposal(proposal) {
		return false, "invalid_action_proposal", "unknown"
	}
	capability, ok := requiredCapability(proposal.Action)
	if !ok || capability != proposal.Capability {
		return false, "action_capability_mismatch", "unknown"
	}
	for _, peripheral := range p.Peripherals {
		if peripheral.Zone != proposal.Zone {
			continue
		}
		observedState := peripheral.ObservedState
		if observedState == "" {
			observedState = "unknown"
		}
		if peripheral.Status == StatusUnavailable || peripheral.Status == StatusNotConfigured || peripheral.Health == "unavailable" {
			continue
		}
		if peripheral.Status == StatusAvailable && peripheral.Health != "healthy" {
			return false, "peripheral_health_unconfirmed", "unknown"
		}
		permission := "action." + proposal.Action
		permitted := false
		for _, grant := range peripheral.Permissions {
			if grant == permission {
				permitted = true
				break
			}
		}
		if !permitted {
			return false, "permission_denied", "unknown"
		}
		for _, candidate := range peripheral.Capabilities {
			if candidate.Name != capability {
				continue
			}
			if candidate.State != "available" || (candidate.Mode != StatusAvailable && candidate.Mode != StatusSimulatedTest && candidate.Mode != StatusDryRun) {
				return false, "capability_" + normalizeCapabilityState(candidate.State), observedState
			}
			age := now.UTC().Sub(peripheral.LastConfirmation.UTC())
			if peripheral.Status == StatusAvailable && (peripheral.Provenance != "device_feedback" || peripheral.LastConfirmation.IsZero() || peripheral.ObservedState == "unknown" || age < 0 || age > 30*time.Second) {
				return false, "device_feedback_unconfirmed", "unknown"
			}
			return true, "physical_execution_disabled", observedState
		}
		return false, "capability_absent", observedState
	}
	return false, "peripheral_absent", "unknown"
}

func validActionProposal(proposal ActionProposal) bool {
	return validOpaqueID(proposal.RequestID) && validOpaqueID(proposal.Action) && validOpaqueID(proposal.Capability) && validOpaqueID(proposal.Zone) && (proposal.EpisodeID == "" || validOpaqueID(proposal.EpisodeID)) && proposal.CooldownNS >= 0 && (proposal.Priority == "" || validPriority(proposal.Priority))
}

func requiredCapability(action string) (string, bool) {
	capabilities := map[string]string{"notify": "notify", "record": "record", "lock": "lock", "unlock": "lock", "open": "open", "close": "close", "set_mode": "mode"}
	capability, ok := capabilities[action]
	return capability, ok
}

type ArbitrationDecision struct {
	RequestID string `json:"request_id"`
	Outcome   string `json:"outcome"` // eligible, suppressed
	Reason    string `json:"reason,omitempty"`
	Replay    bool   `json:"idempotent_replay"`
}

type ActionArbitrator struct {
	mu           sync.Mutex
	seen         map[string]string
	lastByAction map[string]time.Time
}

func NewActionArbitrator() *ActionArbitrator {
	return &ActionArbitrator{seen: make(map[string]string), lastByAction: make(map[string]time.Time)}
}

func (a *ActionArbitrator) RestoreFromRecords(records []Record) error {
	if a == nil {
		return errors.New("action arbitrator is not configured")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = make(map[string]string)
		a.lastByAction = make(map[string]time.Time)
	}
	for _, record := range records {
		if record.Kind != "action_result" {
			continue
		}
		var result ActionResult
		if !decodePayload(record.Payload, &result) || result.Proposal == nil || !validActionProposal(*result.Proposal) {
			return errors.New("invalid action result record in Universal Store")
		}
		proposal := *result.Proposal
		encoded, _ := json.Marshal(proposal)
		digest := sha256.Sum256(encoded)
		a.seen[proposal.RequestID] = hex.EncodeToString(digest[:])
		if proposal.CooldownNS > 0 && record.CreatedAt.After(a.lastByAction[proposal.Zone+":"+proposal.Action]) {
			a.lastByAction[proposal.Zone+":"+proposal.Action] = record.CreatedAt.UTC()
		}
	}
	return nil
}

// ResolveBatch deterministically suppresses expired, duplicate-key-conflicting,
// cooldown-blocked, and contradictory proposals before any executor is called.
func (a *ActionArbitrator) ResolveBatch(proposals []ActionProposal, now time.Time) []ArbitrationDecision {
	decisions := make([]ArbitrationDecision, len(proposals))
	for i, proposal := range proposals {
		decisions[i] = ArbitrationDecision{RequestID: proposal.RequestID, Outcome: "eligible"}
		if !validActionProposal(proposal) {
			decisions[i].Outcome, decisions[i].Reason = "suppressed", "invalid_action_proposal"
		} else if !proposal.ExpiresAt.IsZero() && !proposal.ExpiresAt.After(now.UTC()) {
			decisions[i].Outcome, decisions[i].Reason = "suppressed", "proposal_expired"
		}
		if proposal.Priority != "" && !validPriority(proposal.Priority) {
			decisions[i].Outcome, decisions[i].Reason = "suppressed", "invalid_priority"
		}
	}
	for left := 0; left < len(proposals); left++ {
		for right := left + 1; right < len(proposals); right++ {
			if !contradictory(proposals[left], proposals[right]) {
				continue
			}
			lp, rp := priorityRank(proposals[left].Priority), priorityRank(proposals[right].Priority)
			switch {
			case lp > rp:
				decisions[right].Outcome, decisions[right].Reason = "suppressed", "superseded_by_higher_priority"
			case rp > lp:
				decisions[left].Outcome, decisions[left].Reason = "suppressed", "superseded_by_higher_priority"
			default:
				decisions[left].Outcome, decisions[left].Reason = "suppressed", "contradictory_action"
				decisions[right].Outcome, decisions[right].Reason = "suppressed", "contradictory_action"
			}
		}
	}
	if a == nil {
		return decisions
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = make(map[string]string)
		a.lastByAction = make(map[string]time.Time)
	}
	for i, proposal := range proposals {
		decision := &decisions[i]
		if decision.Outcome != "eligible" {
			continue
		}
		encoded, _ := json.Marshal(proposal)
		digest := sha256.Sum256(encoded)
		fingerprint := hex.EncodeToString(digest[:])
		if previous, ok := a.seen[proposal.RequestID]; ok {
			if previous == fingerprint {
				decision.Replay = true
			} else {
				decision.Outcome, decision.Reason = "suppressed", "idempotency_key_conflict"
			}
			continue
		}
		key := proposal.Zone + ":" + proposal.Action
		if proposal.CooldownNS > 0 {
			if previous, ok := a.lastByAction[key]; ok && now.UTC().Before(previous.Add(time.Duration(proposal.CooldownNS))) {
				decision.Outcome, decision.Reason = "suppressed", "action_cooldown"
				continue
			}
			a.lastByAction[key] = now.UTC()
		}
		a.seen[proposal.RequestID] = fingerprint
	}
	return decisions
}

func contradictory(a, b ActionProposal) bool {
	if a.Zone != b.Zone {
		return false
	}
	return (a.Action == "lock" && b.Action == "unlock") || (a.Action == "unlock" && b.Action == "lock") || (a.Action == "open" && b.Action == "close") || (a.Action == "close" && b.Action == "open")
}

func priorityRank(priority string) int {
	switch priority {
	case "urgent":
		return 4
	case "high":
		return 3
	case "normal", "":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
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
	seenZones := make(map[string]bool, len(zones))
	for _, zone := range zones {
		if !validOpaqueID(zone) || seenZones[zone] {
			return CommunicationRequest{}, errors.New("communication zones must be unique abstract identifiers")
		}
		seenZones[zone] = true
	}
	sort.Strings(zones)
	request := CommunicationRequest{IntentID: intent.ID, Zones: zones, Priority: intent.Priority, Cooldown: intent.Cooldown, Recipient: intent.Recipient, TextKey: intent.TextKey, PermissionGranted: intent.PermissionGranted, TTSStatus: StatusNotConfigured, AudioRendered: false}
	if safetyAllowed {
		if intent.PermissionGranted {
			request.Status = "queued_dry_run"
		} else {
			request.Status, request.Reason = "suppressed", "permission_not_granted"
		}
	} else {
		request.Status, request.Reason = "suppressed", "safety_gate_denied"
	}
	return request, nil
}

type CommunicationScheduler struct {
	mu           sync.Mutex
	last         map[string]time.Time
	seen         map[string]CommunicationRequest
	fingerprints map[string]string
}

func NewCommunicationScheduler() *CommunicationScheduler {
	return &CommunicationScheduler{last: make(map[string]time.Time), seen: make(map[string]CommunicationRequest), fingerprints: make(map[string]string)}
}

func (s *CommunicationScheduler) Schedule(intent CommunicationIntent, safetyAllowed bool, now time.Time) (CommunicationRequest, error) {
	request, err := ProposeCommunication(intent, safetyAllowed)
	if err != nil {
		return request, err
	}
	if s == nil {
		return CommunicationRequest{}, errors.New("communication scheduler is not configured")
	}
	key := intent.Recipient + ":" + strings.Join(request.Zones, ",") + ":" + intent.TextKey
	canonicalIntent := intent
	canonicalIntent.Zones = append([]string(nil), request.Zones...)
	encoded, _ := json.Marshal(canonicalIntent)
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = make(map[string]CommunicationRequest)
		s.fingerprints = make(map[string]string)
		s.last = make(map[string]time.Time)
	}
	if prior, exists := s.seen[intent.ID]; exists {
		if s.fingerprints[intent.ID] != fingerprint {
			return CommunicationRequest{}, errors.New("communication id reused with different intent")
		}
		prior.IdempotentReplay = true
		return prior, nil
	}
	if !safetyAllowed || !intent.PermissionGranted {
		request.ScheduledAt = now.UTC()
		s.seen[intent.ID] = request
		s.fingerprints[intent.ID] = fingerprint
		return request, nil
	}
	if previous, exists := s.last[key]; exists && now.Sub(previous) < intent.Cooldown {
		request.Status, request.Reason = "suppressed", "cooldown"
	} else {
		request.Status = "queued_dry_run"
		s.last[key] = now.UTC()
	}
	request.ScheduledAt = now.UTC()
	s.seen[intent.ID] = request
	s.fingerprints[intent.ID] = fingerprint
	return request, nil
}

func (s *CommunicationScheduler) ScheduleBatch(intents []CommunicationIntent, safetyAllowed bool, now time.Time) ([]CommunicationRequest, error) {
	indices := make([]int, len(intents))
	for index, intent := range intents {
		if _, err := ProposeCommunication(intent, safetyAllowed); err != nil {
			return nil, err
		}
		indices[index] = index
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, right := intents[indices[i]], intents[indices[j]]
		if priorityRank(left.Priority) == priorityRank(right.Priority) {
			return left.ID < right.ID
		}
		return priorityRank(left.Priority) > priorityRank(right.Priority)
	})
	results := make([]CommunicationRequest, len(intents))
	for _, index := range indices {
		request, err := s.Schedule(intents[index], safetyAllowed, now)
		if err != nil {
			return nil, err
		}
		results[index] = request
	}
	return results, nil
}

// RestoreFromRecords reconstructs cooldown and idempotency state after process
// restart from redacted communication records in the Universal Store.
func (s *CommunicationScheduler) RestoreFromRecords(records []Record) error {
	if s == nil {
		return errors.New("communication scheduler is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = make(map[string]time.Time)
		s.seen = make(map[string]CommunicationRequest)
		s.fingerprints = make(map[string]string)
	}
	for _, record := range records {
		if record.Kind != "communication" {
			continue
		}
		var request CommunicationRequest
		if !decodePayload(record.Payload, &request) || !validOpaqueID(request.IntentID) || request.ScheduledAt.IsZero() {
			return errors.New("invalid communication record in Universal Store")
		}
		intent := CommunicationIntent{ID: request.IntentID, Zones: append([]string(nil), request.Zones...), Priority: request.Priority, Cooldown: request.Cooldown, Recipient: request.Recipient, TextKey: request.TextKey, PermissionGranted: request.PermissionGranted}
		encoded, _ := json.Marshal(intent)
		digest := sha256.Sum256(encoded)
		key := intent.Recipient + ":" + strings.Join(intent.Zones, ",") + ":" + intent.TextKey
		if request.Status == "queued_dry_run" {
			if previous := s.last[key]; previous.Before(request.ScheduledAt) {
				s.last[key] = request.ScheduledAt
			}
		}
		s.seen[request.IntentID] = request
		s.fingerprints[request.IntentID] = hex.EncodeToString(digest[:])
	}
	return nil
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
	return t.AllowsAt(from, to, time.Now().UTC())
}

func (t Topology) AllowsAt(from, to string, now time.Time) bool {
	if t.Validate() != nil || t.Status == StatusUnavailable || t.Status == StatusNotConfigured || t.Integrity != "valid" || from == "" || to == "" || (!t.ExpiresAt.IsZero() && !t.ExpiresAt.After(now.UTC())) {
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

func (t Topology) EffectiveAt(now time.Time) Topology {
	if !t.ExpiresAt.IsZero() && !t.ExpiresAt.After(now.UTC()) {
		t.Status = StatusUnavailable
		t.Integrity = "incomplete"
		for index := range t.Zones {
			t.Zones[index].Coverage = "unknown"
			t.Zones[index].Uncertainty = "unknown"
		}
		return t
	}
	for index := range t.Zones {
		zone := &t.Zones[index]
		if !zone.ExpiresAt.IsZero() && !zone.ExpiresAt.After(now.UTC()) {
			zone.Coverage = "unknown"
			zone.Uncertainty = "unknown"
		}
	}
	return t
}

func (t Topology) Validate() error {
	if !t.Status.Valid() {
		return topologyError("invalid_status", "invalid topology function state")
	}
	if t.Status == StatusNotConfigured && t.Integrity == "" {
		if len(t.Zones) == 0 && len(t.Connections) == 0 {
			return nil
		}
		return topologyError("not_configured_has_nodes", "unconfigured topology must not contain declarative nodes")
	}
	if t.Integrity != "valid" && t.Integrity != "incomplete" && t.Integrity != "contradictory" && t.Integrity != "unknown" {
		return topologyError("invalid_integrity", "invalid topology integrity state")
	}
	if !validProvenance(t.Provenance) {
		return topologyError("invalid_provenance", "invalid topology provenance")
	}
	known := make(map[string]bool, len(t.Zones))
	for _, zone := range t.Zones {
		if !validOpaqueID(zone.ID) || known[zone.ID] || !validZoneType(zone.Type) || !validZoneCoverage(zone.Coverage) || !validConfidence(zone.Uncertainty) || !validProvenance(zone.Provenance) {
			return topologyError("invalid_zone", "topology zones require unique ids and bounded type, coverage, uncertainty, and provenance")
		}
		known[zone.ID] = true
		for _, deviceID := range zone.DeviceIDs {
			if !validOpaqueID(deviceID) {
				return topologyError("invalid_device_reference", "topology device references must be abstract ids")
			}
		}
	}
	seen := make(map[Transition]bool, len(t.Connections))
	for _, edge := range t.Connections {
		if !known[edge.From] || !known[edge.To] || edge.From == edge.To || seen[edge] {
			return topologyError("invalid_transition", "topology transition references invalid or duplicate zones")
		}
		seen[edge] = true
	}
	if t.Integrity == "valid" {
		if len(known) == 0 || !known[t.RootZone] {
			return topologyError("root_zone_missing", "valid topology requires a declared root zone")
		}
		reachable := map[string]bool{t.RootZone: true}
		for changed := true; changed; {
			changed = false
			for _, edge := range t.Connections {
				if reachable[edge.From] && !reachable[edge.To] {
					reachable[edge.To], changed = true, true
				}
			}
		}
		for id := range known {
			if !reachable[id] {
				return topologyError("disconnected_zone", "valid topology contains a disconnected zone")
			}
		}
	}
	if t.ExpiresAt.IsZero() && t.Integrity == "valid" && t.Status == StatusAvailable {
		return topologyError("expiration_missing", "available topology assumptions require an expiration")
	}
	if t.Status == StatusAvailable && (t.Provenance != "observed" || t.ObservedAt.IsZero()) {
		return topologyError("observation_missing", "available topology requires observed provenance and a confirmation time")
	}
	return nil
}

type TopologyValidationError struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func (e *TopologyValidationError) Error() string { return e.Code + ": " + e.Reason }

func topologyError(code, reason string) error {
	return &TopologyValidationError{Code: "topology_" + code, Reason: reason}
}

func validZoneType(value string) bool {
	return value == "public" || value == "protected" || value == "service" || value == "unknown"
}
func validZoneCoverage(value string) bool {
	return value == "covered" || value == "partial" || value == "uncovered" || value == "unknown"
}
func validConfidence(value string) bool {
	return value == "low" || value == "medium" || value == "high" || value == "unknown"
}

func validSearchResult(value string) bool {
	return value == "found" || value == "not_found" || value == "ambiguous" || value == "coverage_unknown" || value == "expired"
}

func Correlate(id, episode string, observations []RedactedObservation, confidence string, evidenceRef string) (Correlation, error) {
	return CorrelateAt(id, episode, observations, confidence, evidenceRef, time.Now().UTC())
}

func CorrelateAt(id, episode string, observations []RedactedObservation, confidence string, evidenceRef string, now time.Time) (Correlation, error) {
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
	return Correlation{ID: id, EpisodeID: episode, ObservationIDs: ids, Confidence: confidence, Classification: classification, Status: "active", Provenance: "inferred", ExpiresAt: now.UTC().Add(5 * time.Minute), EvidenceRef: evidenceRef, Biometric: false}, nil
}

// ConfirmObserved is deliberately separate from Correlate: a confidence or
// matching observation alone can never promote a hypothesis to a fact.
func ConfirmObserved(correlation Correlation, validated bool, evidenceRef string) (Correlation, error) {
	if !validated || correlation.Status != "active" || (!correlation.ExpiresAt.IsZero() && !correlation.ExpiresAt.After(time.Now().UTC())) || correlation.Classification != "hypothesis" || len(correlation.ObservationIDs) != 1 || !validOpaqueID(evidenceRef) {
		return Correlation{}, errors.New("correlation requires independently validated single-observation evidence")
	}
	correlation.Classification = "observed"
	correlation.Provenance = "observed"
	correlation.EvidenceRef = evidenceRef
	return correlation, nil
}

func ExpireCorrelation(correlation Correlation, now time.Time) Correlation {
	if !correlation.ExpiresAt.IsZero() && !correlation.ExpiresAt.After(now.UTC()) {
		correlation.Status = "expired"
		correlation.Confidence = "unknown"
		correlation.Provenance = "unknown"
	}
	return correlation
}

func NewSearchState(query SearchQuery, expiresAt time.Time, coverage string, observation *RedactedObservation, matchCount int) (SearchState, error) {
	if !validOpaqueID(query.ID) || len(query.Zones) == 0 || !validSearchTarget(query.Target) || matchCount < 0 || query.IssuedAt.IsZero() {
		return SearchState{}, errors.New("abstract search id, zones, and target are required")
	}
	if expiresAt.IsZero() || !expiresAt.After(query.IssuedAt) {
		return SearchState{}, errors.New("search result requires a future expiration")
	}
	if coverage != "unknown" && coverage != "partial" && coverage != "complete" {
		return SearchState{}, fmt.Errorf("invalid search coverage %q", coverage)
	}
	for _, zone := range query.Zones {
		if !validOpaqueID(zone) {
			return SearchState{}, errors.New("search zones must be abstract identifiers")
		}
	}
	state := SearchState{Status: StatusSimulatedTest, QueryID: query.ID, Result: "not_found", Occupancy: "unknown", Coverage: coverage, Association: "none", Confidence: "unknown", Provenance: "inferred", ExpiresAt: expiresAt.UTC(), CrossCameraIdentity: false, PTZUsed: false}
	result := "not_found"
	if coverage == "unknown" || coverage == "partial" {
		result = "coverage_unknown"
	}
	confidence := "unknown"
	if observation != nil {
		if matchCount == 0 || !observation.Redacted || !validOpaqueID(observation.ID) || !validOpaqueID(observation.Zone) || !validSearchTarget(observation.Category) || !validConfidence(observation.Confidence) {
			return SearchState{}, errors.New("search observation must be redacted")
		}
		copy := *observation
		state.LastObservation = &copy
		state.Occupancy = "occupied"
		if matchCount > 1 {
			state.Association = "ambiguous"
			state.Result = "ambiguous"
		} else {
			state.Association = "single_observation"
			state.Result = "found"
		}
		result = state.Result
		confidence = observation.Confidence
	}
	provenance := "inferred"
	if observation != nil {
		provenance = "observed"
	}
	state.Result, state.Confidence, state.Provenance = result, confidence, provenance
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
		state.Result = "expired"
		state.Occupancy = "unknown"
		state.Coverage = "unknown"
		state.Association = "none"
	}
	return state
}

// ValidatePublicState allows only this package's bounded projection and recursively
// rejects prohibited keys in data supplied to the API adapter.
func ValidatePublicState(state StateSnapshot) error {
	return ValidatePublicStateAt(state, time.Now().UTC())
}

func ValidatePublicStateAt(state StateSnapshot, now time.Time) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	if key := firstForbiddenKey(value); key != "" {
		return fmt.Errorf("%w: forbidden_field=%s", ErrForbiddenProjection, key)
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
		if !peripheral.Status.Valid() || !validKnowledge(peripheral.State) || !validOpaqueID(peripheral.ID) || !validOpaqueID(peripheral.Type) || !validOpaqueID(peripheral.Zone) || !validPeripheralState(peripheral.DesiredState) || !validPeripheralState(peripheral.ObservedState) || !validPeripheralHealth(peripheral.Health) || !validProvenance(peripheral.Provenance) {
			return ErrForbiddenProjection
		}
		confirmationAge := now.UTC().Sub(peripheral.LastConfirmation.UTC())
		if peripheral.Status == StatusAvailable && (peripheral.Health != "healthy" || peripheral.ObservedState == "unknown" || peripheral.LastConfirmation.IsZero() || peripheral.Provenance != "device_feedback" || confirmationAge < 0 || confirmationAge > 30*time.Second) {
			return ErrForbiddenProjection
		}
		for _, capability := range peripheral.Capabilities {
			if !validOpaqueID(capability.Name) || !validCapabilityState(capability.State) || !capability.Mode.Valid() {
				return ErrForbiddenProjection
			}
			if capability.State == "available" && capability.Mode != StatusAvailable && capability.Mode != StatusSimulatedTest && capability.Mode != StatusDryRun {
				return ErrForbiddenProjection
			}
		}
		for _, permission := range peripheral.Permissions {
			if !validPermission(permission) {
				return ErrForbiddenProjection
			}
		}
	}
	if state.LastAction != nil && (!validActionResult(*state.LastAction, state.LastAction.RequestID) || state.LastAction.Proposal == nil) {
		return ErrForbiddenProjection
	}
	if state.Communication != nil {
		communication := state.Communication
		if communication.AudioRendered || communication.TTSStatus != StatusNotConfigured || !validRecipient(communication.Recipient) || !validTextKey(communication.TextKey) || (communication.Status != "suppressed" && communication.Status != "queued_dry_run") || len(communication.Zones) == 0 || communication.ScheduledAt.IsZero() {
			return ErrForbiddenProjection
		}
		if communication.Status == "queued_dry_run" && !communication.PermissionGranted {
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
		if search.CrossCameraIdentity || search.PTZUsed || !search.Status.Valid() || !validSearchResult(search.Result) || !validConfidence(search.Confidence) || !validProvenance(search.Provenance) || !validSearchTarget(searchTargetFromObservation(search.LastObservation)) || (search.Occupancy != "unknown" && search.Occupancy != "occupied" && search.Occupancy != "unoccupied") || (search.Coverage != "unknown" && search.Coverage != "partial" && search.Coverage != "complete") || (search.Association != "none" && search.Association != "ambiguous" && search.Association != "single_observation") {
			return fmt.Errorf("%w: search_state_invalid", ErrForbiddenProjection)
		}
		if (search.Result == "found" && (search.LastObservation == nil || search.Association != "single_observation")) || (search.Result == "ambiguous" && search.Association != "ambiguous") || (search.Result == "not_found" && search.Coverage != "complete") || (search.Result == "coverage_unknown" && search.Coverage == "complete") || (search.Result == "expired" && search.Status != StatusUnavailable) || search.ExpiresAt.IsZero() {
			return fmt.Errorf("%w: search_result_inconsistent", ErrForbiddenProjection)
		}
		if search.LastObservation != nil && (!search.LastObservation.Redacted || !validOpaqueID(search.LastObservation.ID) || !validOpaqueID(search.LastObservation.Zone)) {
			return fmt.Errorf("%w: search_observation_not_redacted", ErrForbiddenProjection)
		}
	}
	if err := state.Topology.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrForbiddenProjection, err)
	}
	if state.Topology.Status == StatusAvailable && (state.Topology.ExpiresAt.IsZero() || !state.Topology.ExpiresAt.After(now.UTC())) {
		return ErrForbiddenProjection
	}
	if state.Search != nil && SearchExpired(*state.Search, now) && state.Search.Result != "expired" {
		return ErrForbiddenProjection
	}
	for _, correlation := range state.Correlations {
		if correlation.Biometric || (correlation.Classification != "hypothesis" && correlation.Classification != "observed" && correlation.Classification != "ambiguous") || (correlation.Status != "active" && correlation.Status != "expired" && correlation.Status != "conflict") || !validProvenance(correlation.Provenance) || !validConfidence(correlation.Confidence) || correlation.ExpiresAt.IsZero() || !validOpaqueID(correlation.ID) || !validOpaqueID(correlation.EpisodeID) {
			return fmt.Errorf("%w: correlation_invalid", ErrForbiddenProjection)
		}
		if correlation.Classification == "ambiguous" && len(correlation.ObservationIDs) < 2 {
			return ErrForbiddenProjection
		}
		if correlation.Classification == "observed" && (len(correlation.ObservationIDs) != 1 || !validOpaqueID(correlation.EvidenceRef)) {
			return ErrForbiddenProjection
		}
		if correlation.Status == "active" && correlation.Classification == "observed" && correlation.Provenance != "observed" || correlation.Status == "expired" && correlation.Provenance != "unknown" {
			return ErrForbiddenProjection
		}
		if correlation.Status == "active" && !correlation.ExpiresAt.After(now.UTC()) {
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
	return firstForbiddenKey(value) != ""
}

// firstForbiddenKey returns only the schema key, never its value, so a failed
// redaction check is diagnosable without copying sensitive payload contents.
func firstForbiddenKey(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			switch strings.ToLower(key) {
			case "media", "raw_media", "frame", "image", "raw_image", "identity", "biometric_identity", "resident_id", "embedding", "embedding_vector", "plate", "plate_text", "license_plate", "local_path", "path", "url", "uri", "secret", "token", "hardware_id", "serial_number", "mac", "bbox", "bounding_box", "crop", "keypoint", "keypoints", "track_id", "local_track_id", "email", "phone", "address":
				return key
			}
			if nested := firstForbiddenKey(child); nested != "" {
				return nested
			}
		}
	case []any:
		for _, child := range current {
			if nested := firstForbiddenKey(child); nested != "" {
				return nested
			}
		}
	}
	return ""
}
