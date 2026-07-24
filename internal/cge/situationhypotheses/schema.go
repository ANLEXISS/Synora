package situationhypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"synora/internal/cge/situationfacts"
)

type RuleOperator string

const (
	OperatorExists         RuleOperator = "exists"
	OperatorNotExists      RuleOperator = "not_exists"
	OperatorEquals         RuleOperator = "equals"
	OperatorNotEquals      RuleOperator = "not_equals"
	OperatorGreaterThan    RuleOperator = "greater_than"
	OperatorGreaterOrEqual RuleOperator = "greater_or_equal"
	OperatorLessThan       RuleOperator = "less_than"
	OperatorContains       RuleOperator = "contains"
	OperatorSetContains    RuleOperator = "set_contains"
	OperatorStatusIs       RuleOperator = "status_is"
	OperatorConflictExists RuleOperator = "conflict_exists"
)

type EvidenceRule struct {
	ID string

	FactCode situationfacts.FactCode
	Scope    situationfacts.FactScope

	Operator RuleOperator

	ExpectedValue  *situationfacts.FactValue
	ExpectedStatus situationfacts.FactStatus

	WeightPermille int
	ReasonCode     string
}

type MissingRule struct {
	ID string

	RequiredFactCode   situationfacts.FactCode
	RequiredScope      situationfacts.FactScope
	ImportancePermille int
	ReasonCode         string
}

type HypothesisDefinition struct {
	Kind        HypothesisKind
	Description string

	SupportRules       []EvidenceRule
	ContradictionRules []EvidenceRule
	MissingRules       []MissingRule

	AllowsCoexistenceWith []HypothesisKind
}

type HypothesisSchema struct {
	Version     string
	Definitions []HypothesisDefinition
	index       map[HypothesisKind]HypothesisDefinition
}

func (s HypothesisSchema) Definition(kind HypothesisKind) (HypothesisDefinition, bool) {
	if s.index != nil {
		value, ok := s.index[kind]
		return cloneDefinition(value), ok
	}
	for _, definition := range s.Definitions {
		if definition.Kind == kind {
			return cloneDefinition(definition), true
		}
	}
	return HypothesisDefinition{}, false
}

func (s HypothesisSchema) Validate() error {
	if s.Version == "" || len(s.Definitions) == 0 {
		return ErrInvalidSchema
	}
	last := HypothesisKind("")
	seen := map[HypothesisKind]struct{}{}
	for _, definition := range s.Definitions {
		if definition.Kind == "" || definition.Kind <= last || definition.Description == "" || containsForbiddenHypothesisTerm(string(definition.Kind)) || containsForbiddenHypothesisTerm(definition.Description) {
			return ErrInvalidDefinition
		}
		if _, ok := seen[definition.Kind]; ok {
			return ErrInvalidDefinition
		}
		seen[definition.Kind] = struct{}{}
		if err := ValidateDefinition(definition); err != nil {
			return err
		}
		coexistSeen := map[HypothesisKind]struct{}{}
		for _, kind := range definition.AllowsCoexistenceWith {
			if _, ok := coexistSeen[kind]; ok {
				return ErrInvalidDefinition
			}
			coexistSeen[kind] = struct{}{}
		}
		last = definition.Kind
	}
	return nil
}

func ValidateDefinition(definition HypothesisDefinition) error {
	if definition.Kind == "" || definition.Description == "" || containsForbiddenHypothesisTerm(string(definition.Kind)) || containsForbiddenHypothesisTerm(definition.Description) {
		return ErrInvalidDefinition
	}
	seen := map[string]struct{}{}
	for _, rule := range definition.SupportRules {
		if err := validateRule(rule, seen); err != nil {
			return err
		}
	}
	for _, rule := range definition.ContradictionRules {
		if err := validateRule(rule, seen); err != nil {
			return err
		}
	}
	missingSeen := map[string]struct{}{}
	for _, rule := range definition.MissingRules {
		if rule.ID == "" || rule.RequiredFactCode == "" || rule.RequiredScope == "" || rule.ReasonCode == "" || rule.ImportancePermille < 0 || rule.ImportancePermille > 1000 {
			return ErrInvalidDefinition
		}
		if _, ok := missingSeen[rule.ID]; ok {
			return ErrInvalidDefinition
		}
		missingSeen[rule.ID] = struct{}{}
		if _, ok := situationfacts.Schema().Definition(rule.RequiredFactCode); !ok {
			return ErrUnknownFactCode
		}
		factDefinition, _ := situationfacts.Schema().Definition(rule.RequiredFactCode)
		if factDefinition.Scope != rule.RequiredScope {
			return ErrInvalidDefinition
		}
	}
	return nil
}

func validateRule(rule EvidenceRule, seen map[string]struct{}) error {
	if rule.ID == "" || rule.ReasonCode == "" || rule.WeightPermille < 0 || rule.WeightPermille > 1000 {
		return ErrInvalidRule
	}
	if _, ok := seen[rule.ID]; ok {
		return ErrInvalidRule
	}
	seen[rule.ID] = struct{}{}
	if rule.Operator == OperatorConflictExists {
		if rule.FactCode != "" || rule.Scope != "" {
			return ErrInvalidRule
		}
		return nil
	}
	if rule.FactCode == "" || rule.Scope == "" {
		return ErrInvalidRule
	}
	definition, ok := situationfacts.Schema().Definition(rule.FactCode)
	if !ok {
		return ErrUnknownFactCode
	}
	if definition.Scope != rule.Scope || !validRuleOperator(rule.Operator) {
		return ErrInvalidRule
	}
	if rule.ExpectedValue != nil && rule.ExpectedValue.Kind != definition.ValueKind {
		return ErrInvalidRule
	}
	if rule.ExpectedValue != nil {
		if err := rule.ExpectedValue.Validate(256, 256); err != nil {
			return ErrInvalidRule
		}
	}
	if (rule.Operator == OperatorExists || rule.Operator == OperatorNotExists || rule.Operator == OperatorStatusIs || rule.Operator == OperatorConflictExists) && rule.ExpectedValue != nil {
		return ErrInvalidRule
	}
	if rule.Operator != OperatorStatusIs && rule.ExpectedStatus != "" {
		return ErrInvalidRule
	}
	if rule.Operator == OperatorContains && rule.ExpectedValue != nil && rule.ExpectedValue.Kind != situationfacts.ValueString {
		return ErrInvalidRule
	}
	if rule.Operator == OperatorSetContains && rule.ExpectedValue != nil && rule.ExpectedValue.Kind != situationfacts.ValueString {
		return ErrInvalidRule
	}
	if rule.Operator == OperatorGreaterThan || rule.Operator == OperatorGreaterOrEqual || rule.Operator == OperatorLessThan {
		if rule.ExpectedValue == nil || rule.ExpectedValue.Kind != situationfacts.ValueInt && rule.ExpectedValue.Kind != situationfacts.ValueDurationMS && rule.ExpectedValue.Kind != situationfacts.ValuePermille {
			return ErrInvalidRule
		}
	}
	if rule.Operator == OperatorStatusIs && !validFactStatus(rule.ExpectedStatus) {
		return ErrInvalidRule
	}
	return nil
}

func validRuleOperator(value RuleOperator) bool {
	switch value {
	case OperatorExists, OperatorNotExists, OperatorEquals, OperatorNotEquals, OperatorGreaterThan, OperatorGreaterOrEqual, OperatorLessThan, OperatorContains, OperatorSetContains, OperatorStatusIs, OperatorConflictExists:
		return true
	default:
		return false
	}
}

func validFactStatus(value situationfacts.FactStatus) bool {
	return value == situationfacts.StatusAsserted || value == situationfacts.StatusUnknown || value == situationfacts.StatusConflicting || value == situationfacts.StatusRetracted
}

func containsForbiddenHypothesisTerm(value string) bool {
	lowered := strings.ToLower(value)
	for _, term := range []string{"intrusion", "attack", "threat", "danger", "malicious", "suspicious", "criminal", "hostile", "safe", "unsafe", "emergency", "intent", "visitor_expected", "compromise", "burglary", "weapon"} {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}

func cloneRule(rule EvidenceRule) EvidenceRule {
	out := rule
	if rule.ExpectedValue != nil {
		value := rule.ExpectedValue.Clone()
		out.ExpectedValue = &value
	}
	return out
}

func cloneDefinition(definition HypothesisDefinition) HypothesisDefinition {
	out := definition
	out.SupportRules = make([]EvidenceRule, len(definition.SupportRules))
	for i, rule := range definition.SupportRules {
		out.SupportRules[i] = cloneRule(rule)
	}
	out.ContradictionRules = make([]EvidenceRule, len(definition.ContradictionRules))
	for i, rule := range definition.ContradictionRules {
		out.ContradictionRules[i] = cloneRule(rule)
	}
	out.MissingRules = append([]MissingRule(nil), definition.MissingRules...)
	out.AllowsCoexistenceWith = append([]HypothesisKind(nil), definition.AllowsCoexistenceWith...)
	return out
}

func buildSchema() HypothesisSchema {
	all := append([]HypothesisKind(nil), allHypothesisKinds()...)
	definition := func(kind HypothesisKind, description string, support, contradiction []EvidenceRule, missing []MissingRule) HypothesisDefinition {
		return HypothesisDefinition{Kind: kind, Description: description, SupportRules: support, ContradictionRules: contradiction, MissingRules: missing, AllowsCoexistenceWith: append([]HypothesisKind(nil), all...)}
	}
	defs := []HypothesisDefinition{
		definition(KindPatternConsistent, "observations align with available historical references", []EvidenceRule{
			rule("pattern.routine", situationfacts.CodeMemoryRoutineRefCount, situationfacts.ScopeMemory, OperatorGreaterThan, situationfacts.IntFactValue(0), 350, "routine_reference_available"),
			rule("pattern.evaluated", situationfacts.CodeMemoryDeviationEvaluated, situationfacts.ScopeMemory, OperatorEquals, situationfacts.BoolFactValue(true), 250, "deviation_assessment_available"),
			rule("pattern.low_score", situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.ScopeMemory, OperatorLessThan, situationfacts.PermilleFactValue(300), 250, "deviation_score_low"),
			rule("pattern.continuity", situationfacts.CodeContinuitySharedTrack, situationfacts.ScopeObservation, OperatorEquals, situationfacts.BoolFactValue(true), 150, "technical_continuity_present"),
		}, []EvidenceRule{
			rule("pattern.high_score", situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.ScopeMemory, OperatorGreaterOrEqual, situationfacts.PermilleFactValue(700), 600, "deviation_score_high"),
			rule("pattern.context_conflict", situationfacts.CodeContextHouseModeConflict, situationfacts.ScopeContext, OperatorEquals, situationfacts.BoolFactValue(true), 400, "context_values_conflict"),
		}, []MissingRule{{ID: "pattern.coverage", RequiredFactCode: situationfacts.CodeMemoryDeviationEvaluated, RequiredScope: situationfacts.ScopeMemory, ImportancePermille: 500, ReasonCode: "assessment_coverage_missing"}}),
		definition(KindIsolatedDeviation, "a measurable departure is present without enough repetition for a durable change", []EvidenceRule{
			rule("isolated.present", situationfacts.CodeMemoryDeviationPresent, situationfacts.ScopeMemory, OperatorEquals, situationfacts.BoolFactValue(true), 300, "deviation_present"),
			rule("isolated.positive", situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.ScopeMemory, OperatorGreaterThan, situationfacts.PermilleFactValue(0), 450, "positive_deviation"),
			rule("isolated.routine", situationfacts.CodeMemoryRoutineRefCount, situationfacts.ScopeMemory, OperatorGreaterThan, situationfacts.IntFactValue(0), 250, "routine_reference_available"),
		}, []EvidenceRule{rule("isolated.aligned", situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.ScopeMemory, OperatorLessThan, situationfacts.PermilleFactValue(150), 350, "deviation_score_low")}, nil),
		definition(KindPossiblePatternShift, "several departure signals may indicate an evolving historical fit", []EvidenceRule{
			rule("shift.positive", situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.ScopeMemory, OperatorGreaterOrEqual, situationfacts.PermilleFactValue(300), 350, "positive_deviation"),
			rule("shift.multiple", situationfacts.CodeEpisodeMultipleObservations, situationfacts.ScopeEpisode, OperatorEquals, situationfacts.BoolFactValue(true), 250, "multiple_observations"),
			rule("shift.routine", situationfacts.CodeMemoryRoutineRefCount, situationfacts.ScopeMemory, OperatorGreaterThan, situationfacts.IntFactValue(0), 250, "routine_reference_available"),
			rule("shift.evaluated", situationfacts.CodeMemoryDeviationEvaluated, situationfacts.ScopeMemory, OperatorEquals, situationfacts.BoolFactValue(true), 150, "deviation_assessment_available"),
		}, nil, []MissingRule{{ID: "shift.repetition", RequiredFactCode: situationfacts.CodeEpisodeObservationCount, RequiredScope: situationfacts.ScopeEpisode, ImportancePermille: 300, ReasonCode: "repetition_depth_limited"}}),
		definition(KindIdentityResolutionFailure, "identity evidence remains unresolved", []EvidenceRule{
			rule("identity.uncertain", situationfacts.CodeIdentityUncertainPresent, situationfacts.ScopeEntity, OperatorEquals, situationfacts.BoolFactValue(true), 450, "uncertain_identity_present"),
			rule("identity.candidates", situationfacts.CodeIdentityCandidateEntitySet, situationfacts.ScopeEntity, OperatorExists, nil, 250, "candidate_entities_present"),
			rule("identity.track", situationfacts.CodeContinuitySharedTrack, situationfacts.ScopeObservation, OperatorEquals, situationfacts.BoolFactValue(true), 200, "technical_continuity_present"),
		}, []EvidenceRule{rule("identity.known", situationfacts.CodeIdentityKnownPresent, situationfacts.ScopeEntity, OperatorEquals, situationfacts.BoolFactValue(true), 500, "known_identity_present")}, nil),
		definition(KindCoherentUnrecognizedActivity, "an unrecognized subject follows a coherent observed progression", []EvidenceRule{
			rule("unknown.present", situationfacts.CodeIdentityUnknownPresent, situationfacts.ScopeEntity, OperatorEquals, situationfacts.BoolFactValue(true), 250, "unknown_identity_present"),
			rule("unknown.track", situationfacts.CodeContinuitySharedTrack, situationfacts.ScopeObservation, OperatorEquals, situationfacts.BoolFactValue(true), 250, "technical_continuity_present"),
			rule("unknown.nodes", situationfacts.CodeContinuityMultipleNodesSameTrack, situationfacts.ScopeObservation, OperatorEquals, situationfacts.BoolFactValue(true), 200, "track_spans_nodes"),
			rule("unknown.reachable", situationfacts.CodeSpatialReachableTransitionCount, situationfacts.ScopeTransition, OperatorGreaterThan, situationfacts.IntFactValue(0), 200, "reachable_transition_present"),
			rule("unknown.multiple", situationfacts.CodeEpisodeMultipleObservations, situationfacts.ScopeEpisode, OperatorEquals, situationfacts.BoolFactValue(true), 100, "multiple_observations"),
		}, []EvidenceRule{rule("unknown.known", situationfacts.CodeIdentityKnownPresent, situationfacts.ScopeEntity, OperatorEquals, situationfacts.BoolFactValue(true), 350, "known_identity_present")}, []MissingRule{{ID: "unknown.identity", RequiredFactCode: situationfacts.CodeIdentityKnownEntitySet, RequiredScope: situationfacts.ScopeEntity, ImportancePermille: 400, ReasonCode: "identity_resolution_not_complete"}}),
		definition(KindContextOrSensorInconsistency, "independent descriptive inputs disagree", []EvidenceRule{
			rule("inconsistency.conflict", "", "", OperatorConflictExists, nil, 500, "fact_conflict_present"),
			rule("inconsistency.mode", situationfacts.CodeContextHouseModeConflict, situationfacts.ScopeContext, OperatorEquals, situationfacts.BoolFactValue(true), 250, "context_values_conflict"),
			rule("inconsistency.occupancy", situationfacts.CodeContextOccupancyConflict, situationfacts.ScopeContext, OperatorEquals, situationfacts.BoolFactValue(true), 150, "context_values_conflict"),
			rule("inconsistency.identity", situationfacts.CodeIdentityConflict, situationfacts.ScopeEntity, OperatorEquals, situationfacts.BoolFactValue(true), 150, "identity_values_conflict"),
			rule("inconsistency.unreachable", situationfacts.CodeSpatialUnreachableTransitionCount, situationfacts.ScopeTransition, OperatorGreaterThan, situationfacts.IntFactValue(0), 200, "unreachable_transition_present"),
		}, nil, nil),
		definition(KindMultiEntityActivity, "multiple distinct entities occur in one episode", []EvidenceRule{
			rule("multi.entities", situationfacts.CodeEpisodeMultipleEntities, situationfacts.ScopeEpisode, OperatorEquals, situationfacts.BoolFactValue(true), 450, "multiple_entities_present"),
			rule("multi.known", situationfacts.CodeIdentityMultipleKnownEntities, situationfacts.ScopeEntity, OperatorEquals, situationfacts.BoolFactValue(true), 350, "multiple_known_entities_present"),
			rule("multi.count", situationfacts.CodeEpisodeEntityCount, situationfacts.ScopeEpisode, OperatorGreaterThan, situationfacts.IntFactValue(1), 200, "entity_count_above_one"),
		}, nil, nil),
		definition(KindInsufficientInformation, "available facts do not cover enough dimensions", []EvidenceRule{
			rule("insufficient.partial", situationfacts.CodeContextPartialPresent, situationfacts.ScopeContext, OperatorEquals, situationfacts.BoolFactValue(true), 300, "partial_context"),
			rule("insufficient.missing", situationfacts.CodeContextMissingPresent, situationfacts.ScopeContext, OperatorEquals, situationfacts.BoolFactValue(true), 300, "missing_context"),
			rule("insufficient.topology", situationfacts.CodeSpatialTopologyAvailable, situationfacts.ScopeTransition, OperatorEquals, situationfacts.BoolFactValue(false), 250, "topology_unavailable"),
			rule("insufficient.small", situationfacts.CodeEpisodeObservationCount, situationfacts.ScopeEpisode, OperatorLessThan, situationfacts.IntFactValue(2), 150, "few_observations"),
		}, nil, nil),
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Kind < defs[j].Kind })
	index := make(map[HypothesisKind]HypothesisDefinition, len(defs))
	for _, definition := range defs {
		index[definition.Kind] = definition
	}
	return HypothesisSchema{Version: "situation-hypotheses-schema-v1", Definitions: defs, index: index}
}

func rule(id string, code situationfacts.FactCode, scope situationfacts.FactScope, operator RuleOperator, value interface{}, weight int, reason string) EvidenceRule {
	var expected *situationfacts.FactValue
	switch typed := value.(type) {
	case situationfacts.FactValue:
		copy := typed.Clone()
		expected = &copy
	case *situationfacts.FactValue:
		if typed != nil {
			copy := typed.Clone()
			expected = &copy
		}
	case nil:
	default:
		return EvidenceRule{ID: id, FactCode: code, Scope: scope, Operator: operator, WeightPermille: -1, ReasonCode: reason}
	}
	return EvidenceRule{ID: id, FactCode: code, Scope: scope, Operator: operator, ExpectedValue: expected, WeightPermille: weight, ReasonCode: reason}
}

func Schema() HypothesisSchema {
	compiled := compiledSchema()
	defs := make([]HypothesisDefinition, len(compiled.Definitions))
	for i, definition := range compiled.Definitions {
		defs[i] = cloneDefinition(definition)
	}
	index := make(map[HypothesisKind]HypothesisDefinition, len(defs))
	for _, definition := range defs {
		index[definition.Kind] = cloneDefinition(definition)
	}
	return HypothesisSchema{Version: compiled.Version, Definitions: defs, index: index}
}

var schemaCache struct {
	sync.Once
	schema      HypothesisSchema
	fingerprint string
}

func compiledSchema() HypothesisSchema {
	schemaCache.Do(func() {
		schemaCache.schema = buildSchema()
		payload, _ := json.Marshal(schemaCache.schema)
		digest := sha256.Sum256(payload)
		schemaCache.fingerprint = "situation-hypotheses-schema-v1:" + hex.EncodeToString(digest[:])
	})
	return schemaCache.schema
}

func SchemaFingerprint() string {
	compiledSchema()
	return schemaCache.fingerprint
}

func schemaFingerprint(schema HypothesisSchema) string {
	payload, _ := json.Marshal(schema)
	digest := sha256.Sum256(payload)
	return "situation-hypotheses-schema-v1:" + hex.EncodeToString(digest[:])
}
