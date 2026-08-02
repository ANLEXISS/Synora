package capabilitymapping

import (
	"sort"
	"strconv"
	"testing"

	"synora/internal/cge/advisoryrequests"
)

func benchmarkInventory(count int) CapabilityInventory {
	catalog := Catalog()
	definition := mustDefinition(catalog, CapabilityContextStateObservation)
	instances := make([]CapabilityInstance, count)
	for i := range instances {
		instances[i] = CapabilityInstance{ID: CapabilityInstanceID("capability-" + strconv.Itoa(i)), Kind: CapabilityContextStateObservation, DomainID: "domain-alpha", ProviderID: "provider-alpha", Status: CapabilityStatusAvailable, Quality: CapabilityQuality{ReliabilityPermille: 850, CompletenessPermille: 850, FreshnessPermille: 850, Calibrated: true, SourceCount: 1}, CostClass: CapabilityCostLow, LatencyClass: CapabilityLatencyImmediate, SensitivityClass: CapabilitySensitivityLow, Revision: 1, DefinitionFingerprint: definitionFingerprint(definition)}
		instances[i].Fingerprint = instanceFingerprint(instances[i])
	}
	inventory := CapabilityInventory{ID: CapabilityInventoryID("benchmark-inventory"), DomainID: "domain-alpha", Revision: 1, Instances: instances, CatalogFingerprint: catalog.Fingerprint}
	sort.Slice(inventory.Instances, func(i, j int) bool { return inventory.Instances[i].ID < inventory.Instances[j].ID })
	inventory.Fingerprint = inventoryFingerprint(inventory)
	return inventory
}

func benchmarkPolicy(max int) Policy {
	policy := DefaultPolicy()
	policy.MaxCandidatesPerRequest = max
	policy.MaxStoredMappingsPerRequest = max
	return policy
}

func BenchmarkBuildRequirement(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	policy := DefaultPolicy()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := BuildRequirement(request, policy)
		if err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkAnalyze1Capability(b *testing.B)      { benchmarkAnalyze(b, 1) }
func BenchmarkAnalyze10Capabilities(b *testing.B)   { benchmarkAnalyze(b, 10) }
func BenchmarkAnalyze100Capabilities(b *testing.B)  { benchmarkAnalyze(b, 100) }
func BenchmarkAnalyze1000Capabilities(b *testing.B) { benchmarkAnalyze(b, 1000) }

func benchmarkAnalyze(b *testing.B, count int) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(count)
	policy := benchmarkPolicy(count)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompatibleExact(b *testing.B) { benchmarkAnalyze(b, 1) }
func BenchmarkCompatibleDegraded(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(1)
	inventory.Instances[0].Status = CapabilityStatusDegraded
	inventory.Instances[0].Fingerprint = instanceFingerprint(inventory.Instances[0])
	inventory.Fingerprint = inventoryFingerprint(inventory)
	policy := DefaultPolicy()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, policy); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkInventoryWithoutMatch(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(100)
	for i := range inventory.Instances {
		inventory.Instances[i].Kind = CapabilityIdentityObservation
		inventory.Instances[i].DefinitionFingerprint = definitionFingerprint(mustDefinition(Catalog(), CapabilityIdentityObservation))
		inventory.Instances[i].Fingerprint = instanceFingerprint(inventory.Instances[i])
	}
	inventory.Fingerprint = inventoryFingerprint(inventory)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, benchmarkPolicy(100)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyPlan(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(10)
	policy := benchmarkPolicy(10)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		registry := NewRegistryWithPolicy(policy)
		plan, err := Plan(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, registry.Snapshot(), policy)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = registry.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRegistryMappings(count int) *Registry {
	registry := NewRegistry()
	inventory := benchmarkInventory(1)
	policy := DefaultPolicy()
	for i := 0; i < count; i++ {
		request := testRequest(advisoryrequests.StatusProposed)
		request.EpisodeID = "benchmark-episode-" + strconv.Itoa(i)
		request.RequiredFactCodes = []string{"context.state", "context.state." + strconv.Itoa(i)}
		request.Key = advisoryrequests.AdvisoryRequestKeyFor(request)
		request.ID = advisoryrequests.AdvisoryRequestIDFor(request.Key, 1)
		request.Fingerprint = advisoryrequests.AdvisoryRequestFingerprint(request)
		plan, err := Plan(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, registry.Snapshot(), policy)
		if err != nil {
			panic(err)
		}
		if _, err = registry.ApplyPlan(plan); err != nil {
			panic(err)
		}
	}
	return registry
}

func BenchmarkReevaluateStatusChanged(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(1)
	policy := DefaultPolicy()
	previous, _ := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, policy)
	inventory.Instances[0].Status = CapabilityStatusUnavailable
	inventory.Instances[0].Fingerprint = instanceFingerprint(inventory.Instances[0])
	inventory.Fingerprint = inventoryFingerprint(inventory)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Reevaluate(previous, request, inventory, Catalog(), policy); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkReevaluateCapabilityAdded(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(1)
	policy := DefaultPolicy()
	previous, _ := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, policy)
	inventory = benchmarkInventory(2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Reevaluate(previous, request, inventory, Catalog(), policy); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkReevaluateCapabilityRemoved(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	inventory := benchmarkInventory(2)
	policy := DefaultPolicy()
	previous, _ := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: inventory}, policy)
	inventory = benchmarkInventory(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Reevaluate(previous, request, inventory, Catalog(), policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublicSnapshot10(b *testing.B)   { benchmarkSnapshot(b, 10) }
func BenchmarkPublicSnapshot100(b *testing.B)  { benchmarkSnapshot(b, 100) }
func BenchmarkPublicSnapshot1000(b *testing.B) { benchmarkSnapshot(b, 1000) }
func benchmarkSnapshot(b *testing.B, count int) {
	registry := benchmarkRegistryMappings(count)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.Snapshot()
	}
}

func BenchmarkRegistryDigest(b *testing.B) {
	snapshot := benchmarkRegistryMappings(100)
	b.ReportAllocs()
	b.ResetTimer()
	value := snapshot.Snapshot()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(value)
	}
}
func BenchmarkExplain(b *testing.B) {
	request := testRequest(advisoryrequests.StatusProposed)
	assessment := testAnalysisBenchmark(request)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Explain(assessment.Candidates[0])
		if err != nil {
			b.Fatal(err)
		}
	}
}
func testAnalysisBenchmark(request advisoryrequests.AdvisoryEvidenceRequest) CapabilityMappingAssessment {
	assessment, err := Analyze(AnalysisInput{Request: request, Catalog: Catalog(), Inventory: benchmarkInventory(1)}, DefaultPolicy())
	if err != nil {
		panic(err)
	}
	return assessment
}
