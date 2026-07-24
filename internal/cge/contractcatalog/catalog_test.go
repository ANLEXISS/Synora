package contractcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/durableids"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func TestCatalogIsValid(t *testing.T) {
	set, err := Validate(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Catalog.Contracts) == 0 || len(set.Catalog.Catalog.Categories) != 17 || len(set.Boundaries.Boundaries) != 18 || len(set.Stores.Stores) == 0 {
		t.Fatalf("catalog unexpectedly incomplete: contracts=%d boundaries=%d stores=%d", len(set.Catalog.Contracts), len(set.Boundaries.Boundaries), len(set.Stores.Stores))
	}
	if len(set.Identifiers.Identifiers) != 27 || len(set.Timestamps.Timestamps) != 13 || len(set.Transports.Transports) != 8 || len(set.Writers.Writers) != 10 {
		t.Fatalf("executable registries unexpectedly incomplete: identifiers=%d timestamps=%d transports=%d writers=%d", len(set.Identifiers.Identifiers), len(set.Timestamps.Timestamps), len(set.Transports.Transports), len(set.Writers.Writers))
	}
}

func TestExecutableRegistriesHaveExpectedSemantics(t *testing.T) {
	if value, ok := generatedRegistry.Identifiers["entity_id"]; !ok || value.Domain != "entity" || value.Protection != "cgeid_v1_entity" {
		t.Fatal("entity identifier registry entry is incomplete")
	}
	if value, ok := generatedRegistry.Timestamps["observed_at"]; !ok || value.Semantic == "" || value.UsedForReasoning == false {
		t.Fatal("observed_at timestamp registry entry is incomplete")
	}
	for id, value := range generatedRegistry.Writers {
		if value.Guard != "ValidateStoreWrite" || value.Store == "" || value.Contract == "" {
			t.Fatalf("writer %s is not guarded: %+v", id, value)
		}
	}
	if len(generatedRegistry.JournalKinds) != 14 {
		t.Fatalf("journal kind registry length=%d", len(generatedRegistry.JournalKinds))
	}
	for kind, value := range generatedRegistry.JournalKinds {
		if value.GoPackage == "" || value.GoType == "" || value.Validator == "" || value.Contract == "" {
			t.Fatalf("journal kind %s is incomplete: %+v", kind, value)
		}
	}
}

func TestRegistryContractAndErrorSurface(t *testing.T) {
	for _, id := range []string{
		"cge.contract.unknown", "cge.contract.type_mismatch", "cge.contract.field_mismatch",
		"cge.contract.store_forbidden", "cge.contract.authority_violation", "cge.contract.protection_violation",
		"cge.contract.sensitive_write_forbidden", "cge.contract.generated_registry_stale",
	} {
		if _, ok := ErrorDescriptorFor(id); !ok {
			t.Fatalf("generated error descriptor missing: %s", id)
		}
	}
	if err := ValidateOutput("synora.cge.observation.v1", chains.ObservationRef{ID: "PASS66-RAW", EventType: "vision.identity", Timestamp: time.Now().UTC()}); err == nil {
		t.Fatal("raw output identifier accepted")
	}
	if err := ValidateOutput("synora.cge.observation.v1", chains.ObservationRef{ID: durableids.Protect(durableids.KindObservation, "PASS66-OUTPUT"), EventType: "vision.identity", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutput("synora.cge.observation.v1", "not-an-observation"); err == nil {
		t.Fatal("scalar accepted for structured contract")
	}
}

func TestCatalogAuthorityAndDurablePrivacyGates(t *testing.T) {
	set, err := Validate(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if set.Catalog.Catalog.AuthorityCeiling != "advisory" {
		t.Fatalf("authority ceiling=%q", set.Catalog.Catalog.AuthorityCeiling)
	}
	for _, contract := range set.Catalog.Contracts {
		if len(contract.Persistence) == 0 && contract.ID != "synora.contract.action-request.v1" {
			continue
		}
		if contract.Owner != "automation" && contract.Owner != "historical_core" && (contract.Authority == "authorized_action" || contract.Authority == "authorized_decision") {
			t.Fatalf("CGE or non-historical contract has execution authority: %s=%s", contract.ID, contract.Authority)
		}
	}
	for _, store := range set.Stores.Stores {
		if store.Durable && (store.ClearSecretAllowed || store.ClearBiometricAllowed) {
			t.Fatalf("durable store permits clear sensitive data: %s", store.ID)
		}
	}
}

func TestAllowlistedEventsAndHistoricalMotion(t *testing.T) {
	set, err := Validate(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"vision.identity": true, "vision.unknown": true, "vision.uncertain": true}
	for _, event := range set.Catalog.AdmissionEvents {
		if want[event.EventType] && (!event.Workflow || event.Disposition != "admitted") {
			t.Fatalf("allowlisted event not admitted: %+v", event)
		}
		if event.EventType == "vision.motion" && (event.Workflow || event.Disposition != "historical_only") {
			t.Fatalf("motion is not historical-only: %+v", event)
		}
	}
}

func copyCatalogFixture(t *testing.T) string {
	t.Helper()
	root := repositoryRoot(t)
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "configs/cge/contracts"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"catalog.yaml", "boundaries.yaml", "stores.yaml", "errors.yaml", "identifiers.yaml", "timestamps.yaml", "transports.yaml", "writers.yaml", "journal-kinds.yaml", "field-mappings.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, "configs/cge/contracts", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture, "configs/cge/contracts", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func TestStrictLoaderRejectsCatalogMutations(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		mutate func(string) string
	}{
		{"unknown key", "catalog.yaml", func(value string) string { return value + "\nunknown_key: true\n" }},
		{"duplicate key", "catalog.yaml", func(value string) string {
			return "catalog:\n  schema_version: 1\n  schema_version: 2\n" + value[strings.Index(value, "  namespace:"):]
		}},
		{"second document", "catalog.yaml", func(value string) string { return value + "\n---\ncatalog: {}\n" }},
		{"wrong type", "catalog.yaml", func(value string) string {
			return strings.Replace(value, "schema_version: 1", "schema_version: wrong", 1)
		}},
		{"persistent contract policy", "catalog.yaml", func(value string) string {
			return strings.Replace(value, "justification: Historical Core state is the owner of this durable event representation.", "justification: \"\"", 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := copyCatalogFixture(t)
			path := filepath.Join(root, "configs/cge/contracts", test.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.mutate(string(data))), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Validate(root); err == nil {
				t.Fatalf("mutation %q was accepted", test.name)
			}
		})
	}
}

func TestExecutableRegistryAndDurableGuard(t *testing.T) {
	if CatalogFingerprint() == "" {
		t.Fatal("generated catalog fingerprint is empty")
	}
	if _, ok := Contract("synora.cge.observation.v1"); !ok {
		t.Fatal("generated observation contract is missing")
	}
	entity := durableids.Protect(durableids.KindEntity, "PASS66-ENTITY")
	observation := durableids.Protect(durableids.KindObservation, "PASS66-EVENT")
	payload := chains.ObservationRef{ID: observation, EventType: "vision.identity", Timestamp: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC), EntityID: entity}
	before, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoreWrite("synora.store.workflow-wal", "synora.cge.observation.v1", payload); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("store guard mutated payload")
	}
	if err := ValidateStoreWrite("synora.store.workflow-wal", "synora.cge.observation.v1", chains.ObservationRef{ID: "PASS66-RAW-EVENT", EventType: "vision.identity", Timestamp: time.Now().UTC()}); err == nil {
		t.Fatal("raw observation identifier accepted")
	}
	if err := ValidateStoreWrite("synora.store.workflow-wal", "synora.cge.observation.v1", chains.ObservationRef{ID: durableids.Protect(durableids.KindDevice, "PASS66-EVENT"), EventType: "vision.identity", Timestamp: time.Now().UTC()}); err == nil {
		t.Fatal("wrong identifier domain accepted")
	}
	if err := ValidateStoreWrite("synora.store.calibration-ledger", "synora.cge.observation.v1", payload); err == nil {
		t.Fatal("forbidden contract/store pair accepted")
	}
	if err := ValidateAuthority("synora.cge.recommendation-set.v1", AuthorityAuthorizedAction); err == nil {
		t.Fatal("CGE action authority accepted")
	}
}

func TestEveryCGEDurableStoreRejectsSensitiveWritesBeforeFileMutation(t *testing.T) {
	stores := []string{
		"synora.store.cge-journal", "synora.store.cge-generations", "synora.store.workflow-wal",
		"synora.store.workflow-checkpoint", "synora.store.calibration-ledger", "synora.store.feedback",
		"synora.store.field-trial-recorder",
	}
	for _, store := range stores {
		t.Run(store, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "durable-record")
			original := []byte("unchanged")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, payload := range []map[string]any{
				{"secret": "PASS66-SECRET"},
				{"image": "PASS66-BIOMETRIC"},
			} {
				if err := ValidateStoreWrite(store, contractForStore(store), payload); err == nil {
					t.Fatalf("sensitive payload accepted by %s", store)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != string(original) {
					t.Fatalf("file changed after rejected %s write", store)
				}
			}
		})
	}
}

func contractForStore(store string) string {
	if store == "synora.store.feedback" {
		return "synora.cge.feedback.v1"
	}
	if store == "synora.store.field-trial-recorder" {
		return "synora.cge.field-trial-envelope.v1"
	}
	switch store {
	case "synora.store.cge-journal":
		return "synora.cge.journal-record.v1"
	case "synora.store.cge-generations":
		return "synora.cge.generation-manifest.v1"
	case "synora.store.workflow-wal":
		return "synora.cge.workflow-commit.v1"
	case "synora.store.workflow-checkpoint":
		return "synora.cge.workflow-checkpoint.v1"
	case "synora.store.calibration-ledger":
		return "synora.cge.calibration-record.v1"
	default:
		return "synora.cge.audit-record.v1"
	}
}
