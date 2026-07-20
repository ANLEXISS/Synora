package capabilitymapping

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"synora/internal/cge/advisoryrequests"
	"synora/internal/cge/evidencediscrimination"
)

var capabilityTestTime = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func testRequest(status advisoryrequests.AdvisoryRequestStatus) advisoryrequests.AdvisoryEvidenceRequest {
	r := advisoryrequests.AdvisoryEvidenceRequest{Generation: 1, EpisodeID: "episode-capability", Status: status, CandidateID: evidencediscrimination.EvidenceCandidateID("candidate-capability"), Kind: evidencediscrimination.KindContextConfirmation, Dimension: evidencediscrimination.DimensionDomesticContext, RequiredFactCodes: []string{"context.state"}, HypothesisPairs: []advisoryrequests.AdvisoryHypothesisPair{{FirstID: "hyp-a", SecondID: "hyp-b"}}, DiscriminationPermille: 700, CoverageGainPermille: 500, RedundancyPermille: 100, UtilityPermille: 800, CostClass: string(evidencediscrimination.CostLow), LatencyClass: string(evidencediscrimination.LatencyImmediate), SensitivityClass: string(evidencediscrimination.SensitivityLow), ReasonCodes: []string{"dimension_unresolved"}, FirstProposedAt: capabilityTestTime, LastEvaluatedAt: capabilityTestTime, StatusChangedAt: capabilityTestTime, ExpiresAt: capabilityTestTime.Add(15 * time.Minute), SourceAssessmentFingerprint: "assessment-source", SourceCandidateFingerprint: "candidate-source", Revision: 1, Flags: advisoryrequests.AdvisoryRequestFlags{NotACommand: true, NotAProbability: true, NoSecurityMeaning: true, RequiresExternalMapping: true, RequiresExternalAuthorization: true}}
	r.Key = advisoryrequests.AdvisoryRequestKeyFor(r)
	r.ID = advisoryrequests.AdvisoryRequestIDFor(r.Key, r.Generation)
	r.Fingerprint = advisoryrequests.AdvisoryRequestFingerprint(r)
	return r
}

func testInstance(t *testing.T, id string, status CapabilityStatus, quality int) CapabilityInstance {
	t.Helper()
	catalog := Catalog()
	definition, ok := definitionFor(catalog, CapabilityContextStateObservation)
	if !ok {
		t.Fatal("context definition missing")
	}
	instance := CapabilityInstance{ID: CapabilityInstanceID(id), Kind: CapabilityContextStateObservation, DomainID: "domain-alpha", ProviderID: "provider-alpha", Status: status, Quality: CapabilityQuality{ReliabilityPermille: quality, CompletenessPermille: quality, FreshnessPermille: quality, Calibrated: true, SourceCount: 1}, CostClass: CapabilityCostLow, LatencyClass: CapabilityLatencyImmediate, SensitivityClass: CapabilitySensitivityLow, SupportedScopes: []CapabilityScope{{Kind: "domain", Ref: "home"}}, Revision: 1, DefinitionFingerprint: definitionFingerprint(definition)}
	instance.Fingerprint = instanceFingerprint(instance)
	return instance
}

func testInventory(t *testing.T, instances ...CapabilityInstance) CapabilityInventory {
	t.Helper()
	catalog := Catalog()
	inventory := CapabilityInventory{ID: "inventory-alpha", DomainID: "domain-alpha", Revision: 1, Instances: instances, CatalogFingerprint: catalog.Fingerprint}
	sortInventoryInstances(inventory.Instances)
	inventory.Fingerprint = inventoryFingerprint(inventory)
	return inventory
}

func testAnalysis(t *testing.T, request advisoryrequests.AdvisoryEvidenceRequest, inventory CapabilityInventory, policy Policy) CapabilityMappingAssessment {
	t.Helper()
	assessment, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, policy)
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}

func sortInventoryInstances(values []CapabilityInstance) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j].ID < values[j-1].ID; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func TestExactCompatibleCapabilityAndPreferredMapping(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	assessment := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850)), DefaultPolicy())
	if !assessment.MappingAvailable || assessment.PreferredCandidateID == "" || assessment.MappingAmbiguous || len(assessment.Candidates) != 1 {
		t.Fatalf("unexpected exact mapping: %+v", assessment)
	}
	if assessment.Candidates[0].Status != MappingCompatible || !assessment.Candidates[0].Compatible {
		t.Fatalf("not compatible: %+v", assessment.Candidates[0])
	}
	if assessment.Candidates[0].UtilityPermille < DefaultPolicy().MinUtilityPermille {
		t.Fatalf("utility below threshold: %d", assessment.Candidates[0].UtilityPermille)
	}
}

func TestMultipleAndEqualMappings(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	assessment := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850), testInstance(t, "capability-02", CapabilityStatusAvailable, 850)), DefaultPolicy())
	if len(assessment.Candidates) != 2 || !assessment.MappingAmbiguous || assessment.PreferredCandidateID != "" {
		t.Fatalf("expected equal ambiguous mappings: %+v", assessment)
	}
}

func TestAvailabilityQualityAndDegradedModes(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	unavailable := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-unavailable", CapabilityStatusUnavailable, 900)), DefaultPolicy())
	if !unavailable.CapabilityUnavailable || unavailable.MappingAvailable || unavailable.Candidates[0].Status != MappingUnavailable {
		t.Fatalf("unexpected unavailable result: %+v", unavailable)
	}
	degraded := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-degraded", CapabilityStatusDegraded, 850)), DefaultPolicy())
	if !degraded.MappingAvailable || degraded.Candidates[0].Status != MappingCompatibleDegraded {
		t.Fatalf("degraded capability not admitted: %+v", degraded.Candidates[0])
	}
	policy := DefaultPolicy()
	policy.AllowDegradedCapabilities = false
	degradedRejected := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-degraded", CapabilityStatusDegraded, 850)), policy)
	if degradedRejected.MappingAvailable || degradedRejected.Candidates[0].Status != MappingIncompatible {
		t.Fatalf("degraded capability was not rejected: %+v", degradedRejected.Candidates[0])
	}
	lowQuality := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-low-quality", CapabilityStatusAvailable, 200)), DefaultPolicy())
	if lowQuality.MappingAvailable || !containsString(lowQuality.Candidates[0].ReasonCodes, "capability.quality_insufficient") {
		t.Fatalf("quality was not enforced: %+v", lowQuality.Candidates[0])
	}
}

func TestScopesConstraintsAndLimits(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	requirement, err := BuildRequirement(request, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	requirement.RequiredScopes = []CapabilityScope{{Kind: "zone", Ref: "entry"}}
	requirement.RequiredConstraints = []CapabilityConstraint{{Code: "supports_context_state", Operator: ConstraintEquals, Value: ConstraintValue{Bool: boolPointer(true)}, Hard: true}}
	requirement.Fingerprint = requirementFingerprint(requirement)
	if validationErr := ValidateRequirement(requirement); validationErr != nil {
		t.Fatalf("custom requirement validation: %v", validationErr)
	}
	instance := testInstance(t, "capability-scoped", CapabilityStatusAvailable, 850)
	instance.SupportedScopes = []CapabilityScope{{Kind: "zone", Ref: "entry"}}
	instance.Constraints = []CapabilityConstraint{{Code: "supports_context_state", Operator: ConstraintEquals, Value: ConstraintValue{Bool: boolPointer(true)}, Hard: true}}
	instance.Fingerprint = instanceFingerprint(instance)
	assessment, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: testInventory(t, instance), Requirement: &requirement}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.MappingAvailable || assessment.Candidates[0].ScopePermille != 1000 || assessment.Candidates[0].ConstraintPermille != 1000 {
		t.Fatalf("scope/constraint failed: %+v", assessment.Candidates[0])
	}
	instance.SupportedScopes = []CapabilityScope{{Kind: "zone", Ref: "other"}}
	instance.Fingerprint = instanceFingerprint(instance)
	requirement.RequiredScopes = []CapabilityScope{{Kind: "zone", Ref: "entry"}}
	requirement.Fingerprint = requirementFingerprint(requirement)
	assessment, err = Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: testInventory(t, instance), Requirement: &requirement}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.MappingAvailable {
		t.Fatal("scope mismatch unexpectedly matched without requirement override")
	}
}

func boolPointer(value bool) *bool { return &value }

func TestTerminalRequestCatalogInventoryAndForbiddenDefinitions(t *testing.T) {
	terminalRequest := testRequest(advisoryrequests.StatusSatisfied)
	if _, err := BuildRequirement(terminalRequest, DefaultPolicy()); !errors.Is(err, ErrRequestTerminal) {
		t.Fatalf("terminal request error=%v", err)
	}
	catalog := Catalog()
	catalog.Definitions[0].DescriptionCode = "capture_video"
	catalog.Fingerprint = catalogFingerprint(catalog)
	if err := ValidateCatalog(catalog); !errors.Is(err, ErrInvalidCapabilityDefinition) {
		t.Fatalf("forbidden catalog error=%v", err)
	}
	inventory := testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850))
	inventory.CatalogFingerprint = "wrong"
	inventory.Fingerprint = inventoryFingerprint(inventory)
	if err := ValidateInventory(inventory, Catalog()); !errors.Is(err, ErrInventoryCatalogMismatch) {
		t.Fatalf("catalog mismatch error=%v", err)
	}
}

func TestCompatibilityBoundaryMatrix(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	identity := testInstance(t, "kind-mismatch", CapabilityStatusAvailable, 850)
	identity.Kind = CapabilityIdentityObservation
	identity.DefinitionFingerprint = definitionFingerprint(mustDefinition(Catalog(), CapabilityIdentityObservation))
	identity.Fingerprint = instanceFingerprint(identity)
	kindMismatch := testAnalysis(t, request, testInventory(t, identity), DefaultPolicy())
	if kindMismatch.MappingAvailable || !containsString(kindMismatch.Candidates[0].ReasonCodes, "capability.kind_mismatch") {
		t.Fatalf("kind mismatch not explained: %+v", kindMismatch.Candidates[0])
	}

	uncalibrated := testInstance(t, "uncalibrated", CapabilityStatusAvailable, 850)
	uncalibrated.Quality.Calibrated = false
	uncalibrated.Fingerprint = instanceFingerprint(uncalibrated)
	policy := DefaultPolicy()
	policy.RequireCalibratedQuality = true
	calibration := testAnalysis(t, request, testInventory(t, uncalibrated), policy)
	if calibration.MappingAvailable || !containsString(calibration.Candidates[0].ReasonCodes, "capability.quality_uncalibrated") {
		t.Fatalf("calibration requirement not enforced: %+v", calibration.Candidates[0])
	}

	requirement, err := BuildRequirement(request, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	requirement.RequiredScopes = []CapabilityScope{{Kind: "zone", Ref: "entry"}}
	requirement.Fingerprint = requirementFingerprint(requirement)
	unknownScope := testInstance(t, "unknown-scope", CapabilityStatusAvailable, 850)
	unknownScope.SupportedScopes = nil
	unknownScope.Fingerprint = instanceFingerprint(unknownScope)
	policy = DefaultPolicy()
	policy.AllowUnknownScope = false
	unknownScopeResult, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: testInventory(t, unknownScope), Requirement: &requirement}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if unknownScopeResult.MappingAvailable {
		t.Fatal("unknown scope was accepted while disabled")
	}

	for name, mutate := range map[string]func(*CapabilityInstance){
		"cost":        func(instance *CapabilityInstance) { instance.CostClass = CapabilityCostHigh },
		"latency":     func(instance *CapabilityInstance) { instance.LatencyClass = CapabilityLatencyExtended },
		"sensitivity": func(instance *CapabilityInstance) { instance.SensitivityClass = CapabilitySensitivityModerate },
	} {
		t.Run(name, func(t *testing.T) {
			instance := testInstance(t, name, CapabilityStatusAvailable, 850)
			mutate(&instance)
			instance.Fingerprint = instanceFingerprint(instance)
			assessment := testAnalysis(t, request, testInventory(t, instance), DefaultPolicy())
			if assessment.MappingAvailable {
				t.Fatalf("%s limit was ignored: %+v", name, assessment.Candidates[0])
			}
		})
	}

	requirement, err = BuildRequirement(request, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	requirement.RequiredConstraints = []CapabilityConstraint{{Code: "supports_context_state", Operator: ConstraintEquals, Value: ConstraintValue{Bool: boolPointer(true)}, Hard: true}}
	requirement.Fingerprint = requirementFingerprint(requirement)
	hard := testInstance(t, "hard", CapabilityStatusAvailable, 850)
	hard.Fingerprint = instanceFingerprint(hard)
	hardResult, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: testInventory(t, hard), Requirement: &requirement}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hardResult.MappingAvailable || !containsString(hardResult.Candidates[0].ReasonCodes, "capability.constraint_failed") {
		t.Fatalf("hard constraint was not enforced: %+v", hardResult.Candidates[0])
	}
	requirement.RequiredConstraints[0].Hard = false
	requirement.Fingerprint = requirementFingerprint(requirement)
	softResult, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: testInventory(t, hard), Requirement: &requirement}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !softResult.MappingAvailable || softResult.Candidates[0].ConstraintPermille >= 1000 {
		t.Fatalf("soft constraint did not degrade mapping: %+v", softResult.Candidates[0])
	}

	empty := testAnalysis(t, request, testInventory(t), DefaultPolicy())
	if empty.MappingAvailable || len(empty.Candidates) != 0 {
		t.Fatalf("empty inventory produced mapping: %+v", empty)
	}
	forged := testInventory(t, testInstance(t, "forged", CapabilityStatusAvailable, 850))
	forged.Fingerprint = "forged"
	if _, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: forged}, DefaultPolicy()); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("forged inventory error=%v", err)
	}
}

func mustDefinition(catalog CapabilityCatalog, kind CapabilityKind) CapabilityDefinition {
	definition, ok := definitionFor(catalog, kind)
	if !ok {
		panic("missing capability definition")
	}
	return definition
}

func TestCompactStorageShapes(t *testing.T) {
	t.Logf("shallow sizes: capability instance=%d, mapping candidate=%d, mapping assessment=%d", unsafe.Sizeof(CapabilityInstance{}), unsafe.Sizeof(CapabilityMappingCandidate{}), unsafe.Sizeof(CapabilityMappingAssessment{}))
	for _, field := range []string{"Inventory", "Catalog", "FactSet", "HypothesisSet", "Endpoint", "Credential", "Command"} {
		if _, ok := reflect.TypeOf(CapabilityMappingAssessment{}).FieldByName(field); ok {
			t.Fatalf("assessment embeds forbidden field %s", field)
		}
	}
}

func TestPlanRegistryReevaluationAndIdempotence(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850))
	registry := NewRegistry()
	plan, err := Plan(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.ApplyPlan(plan)
	if err != nil || !first.Applied {
		t.Fatalf("first plan: %v %+v", err, first)
	}
	digest := registry.Snapshot().Digest
	second, err := registry.ApplyPlan(plan)
	if err != nil || !second.Idempotent || registry.Snapshot().Digest != digest {
		t.Fatalf("idempotence: %v %+v", err, second)
	}
	updatedInventory := testInventory(t, testInstance(t, "capability-01", CapabilityStatusUnavailable, 850))
	updatedPlan, err := Plan(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: updatedInventory}, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := registry.ApplyPlan(updatedPlan)
	if err != nil || !updated.Applied || updated.After.CapabilityUnavailable != true {
		t.Fatalf("reevaluation: %v %+v", err, updated)
	}
	reevaluated, err := Reevaluate(updated.After, request, inventory, Catalog(), DefaultPolicy())
	if err != nil || !reevaluated.MappingAvailable {
		t.Fatalf("reevaluation did not accept changed inventory: %v %+v", err, reevaluated)
	}
}

func TestExplanationsPropertiesAndConcurrency(t *testing.T) {
	request := testRequest(advisoryrequests.StatusProposed)
	assessment := testAnalysis(t, request, testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850)), DefaultPolicy())
	explanation, err := Explain(assessment.Candidates[0])
	if err != nil || !explanation.NotACommand || !explanation.NotAuthorization || !explanation.NotAProbability || !explanation.NoSecurityMeaning {
		t.Fatalf("explanation flags: %v %+v", err, explanation)
	}
	for i := 0; i < 32; i++ {
		reordered := testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850))
		again := testAnalysis(t, request, reordered, DefaultPolicy())
		if again.Fingerprint != assessment.Fingerprint {
			t.Fatal("same input changed assessment fingerprint")
		}
	}
	registry := NewRegistry()
	plan, err := Plan(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: testInventory(t, testInstance(t, "capability-01", CapabilityStatusAvailable, 850))}, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	planA := plan
	otherInventory := testInventory(t, testInstance(t, "capability-01", CapabilityStatusUnavailable, 850))
	planB, err := Plan(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: otherInventory}, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, candidatePlan := range []MappingPlan{planA, planB} {
		wg.Add(1)
		go func(value MappingPlan) {
			defer wg.Done()
			_, applyErr := registry.ApplyPlan(value)
			results <- applyErr
		}(candidatePlan)
	}
	wg.Wait()
	close(results)
	conflicts := 0
	successes := 0
	for applyErr := range results {
		if applyErr == nil {
			successes++
		}
		if errors.Is(applyErr, ErrSourceRevisionConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results successes=%d conflicts=%d", successes, conflicts)
	}
}
