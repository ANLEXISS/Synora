// Package contractcatalog validates the descriptive CGE architecture catalog.
// It is intentionally not used by the CGE runtime: the catalog is a review
// and architecture gate, not a second source of runtime behavior.
package contractcatalog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var versionedID = regexp.MustCompile(`\.v[0-9]+$`)

var validCategories = map[string]bool{
	"raw_measurement": true, "external_event": true, "normalized_observation": true,
	"context_fact": true, "derived_fact": true, "entity_reference": true,
	"relation": true, "hypothesis": true, "evidence": true, "assessment": true,
	"cognitive_situation": true, "recommendation": true, "historical_comparison": true,
	"feedback": true, "diagnostic": true, "audit_record": true, "configuration": true,
}

var validTrust = map[string]bool{
	"untrusted": true, "sensor_reported": true, "validated": true, "derived": true,
	"corroborated": true, "confirmed": true, "invalidated": true,
}

var validSensitivity = map[string]bool{
	"public": true, "operational": true, "personal": true, "sensitive_personal": true,
	"biometric": true, "security_sensitive": true, "secret": true,
}

var validAuthority = map[string]bool{
	"descriptive": true, "diagnostic": true, "advisory": true,
	"authorized_decision": true, "authorized_action": true,
}

var validStatus = map[string]bool{
	"experimental": true, "internal": true, "stable": true, "deprecated": true,
}

var canonicalCategories = []string{
	"raw_measurement", "external_event", "normalized_observation", "context_fact", "derived_fact",
	"entity_reference", "relation", "hypothesis", "evidence", "assessment", "cognitive_situation",
	"recommendation", "historical_comparison", "feedback", "diagnostic", "audit_record", "configuration",
}
var canonicalTrust = []string{"untrusted", "sensor_reported", "validated", "derived", "corroborated", "confirmed", "invalidated"}
var canonicalSensitivity = []string{"public", "operational", "personal", "sensitive_personal", "biometric", "security_sensitive", "secret"}
var canonicalAuthority = []string{"descriptive", "diagnostic", "advisory", "authorized_decision", "authorized_action"}
var canonicalStatus = []string{"experimental", "internal", "stable", "deprecated"}

type CatalogFile struct {
	Catalog         Catalog           `yaml:"catalog"`
	Contracts       []CatalogContract `yaml:"contracts"`
	AdmissionEvents []AdmissionEvent  `yaml:"admission_events"`
	OutputProfiles  []OutputProfile   `yaml:"output_profiles"`
}

type Catalog struct {
	SchemaVersion      int           `yaml:"schema_version"`
	Namespace          string        `yaml:"namespace"`
	AuthorityCeiling   string        `yaml:"authority_ceiling"`
	Categories         []string      `yaml:"categories"`
	TrustLevels        []string      `yaml:"trust_levels"`
	SensitivityClasses []string      `yaml:"sensitivity_classes"`
	AuthorityLevels    []string      `yaml:"authority_levels"`
	StabilityStatuses  []string      `yaml:"stability_statuses"`
	DataControls       []DataControl `yaml:"data_controls"`
}

type DataControl struct {
	Name           string `yaml:"name"`
	Classification string `yaml:"classification"`
	Memory         string `yaml:"memory"`
	Logs           string `yaml:"logs"`
	Journal        string `yaml:"journal"`
	WAL            string `yaml:"wal"`
	Ledger         string `yaml:"ledger"`
	Protection     string `yaml:"protection"`
}

type CatalogContract struct {
	ID                  string         `yaml:"id"`
	Category            string         `yaml:"category"`
	Owner               string         `yaml:"owner"`
	Producers           []string       `yaml:"producers"`
	Consumers           []string       `yaml:"consumers"`
	Transport           string         `yaml:"transport"`
	Persistence         []string       `yaml:"persistence"`
	Authority           string         `yaml:"authority"`
	Trust               string         `yaml:"trust"`
	Sensitivity         string         `yaml:"sensitivity"`
	Status              string         `yaml:"status"`
	Justification       string         `yaml:"justification"`
	MigrationPolicy     string         `yaml:"migration_policy"`
	CompatibilityPolicy string         `yaml:"compatibility_policy"`
	RetentionPolicy     string         `yaml:"retention_policy"`
	Implementation      Implementation `yaml:"implementation"`
	Fields              []Field        `yaml:"fields"`
}

type Implementation struct {
	Kind          string `yaml:"kind"`
	Package       string `yaml:"package"`
	Type          string `yaml:"type"`
	WireFormat    string `yaml:"wire_format"`
	Validator     string `yaml:"validator"`
	Justification string `yaml:"justification"`
}

type Field struct {
	Name            string   `yaml:"name"`
	Type            string   `yaml:"type"`
	Required        bool     `yaml:"required"`
	Nullable        bool     `yaml:"nullable"`
	Description     string   `yaml:"description"`
	Source          string   `yaml:"source"`
	Trust           string   `yaml:"trust"`
	Sensitivity     string   `yaml:"sensitivity"`
	Protection      string   `yaml:"protection"`
	Persistence     []string `yaml:"persistence"`
	Retention       string   `yaml:"retention"`
	Validation      string   `yaml:"validation"`
	GoField         string   `yaml:"go_field"`
	WireName        string   `yaml:"wire_name"`
	FieldPath       string   `yaml:"field_path"`
	Omitempty       *bool    `yaml:"omitempty"`
	RuntimeOnly     bool     `yaml:"runtime_only"`
	CatalogOnly     bool     `yaml:"catalog_only"`
	ExceptionReason string   `yaml:"exception_reason"`
	GoType          string   `yaml:"go_type"`
	WireType        string   `yaml:"wire_type"`
	Identifier      string   `yaml:"identifier_semantic"`
	Timestamp       string   `yaml:"timestamp_semantic"`
}

type IdentifierSpec struct {
	ID               string   `yaml:"id"`
	Semantic         string   `yaml:"semantic"`
	Owner            string   `yaml:"owner"`
	Generator        string   `yaml:"generator"`
	Scope            string   `yaml:"scope"`
	Uniqueness       string   `yaml:"uniqueness"`
	WireType         string   `yaml:"wire_type"`
	Protection       string   `yaml:"protection"`
	Domain           string   `yaml:"domain"`
	Nullable         bool     `yaml:"nullable"`
	Deduplication    string   `yaml:"deduplication"`
	Correlation      string   `yaml:"correlation"`
	Ordering         string   `yaml:"ordering"`
	Persistence      []string `yaml:"persistence"`
	RestartStability string   `yaml:"restart_stability"`
	LegacyBehavior   string   `yaml:"legacy_behavior"`
}

type IdentifiersFile struct {
	SchemaVersion int              `yaml:"schema_version"`
	Namespace     string           `yaml:"namespace"`
	Identifiers   []IdentifierSpec `yaml:"identifiers"`
}

type TimestampSpec struct {
	ID                string   `yaml:"id"`
	Semantic          string   `yaml:"semantic"`
	Producer          string   `yaml:"producer"`
	Clock             string   `yaml:"clock"`
	Timezone          string   `yaml:"timezone"`
	Required          bool     `yaml:"required"`
	Ordering          string   `yaml:"ordering"`
	SourceSupplied    bool     `yaml:"can_be_source_supplied"`
	FutureAllowed     bool     `yaml:"can_be_future"`
	MaximumFutureSkew string   `yaml:"maximum_future_skew"`
	MaximumPastAge    string   `yaml:"maximum_past_age"`
	Persistence       []string `yaml:"persistence"`
	UsedForReasoning  bool     `yaml:"used_for_reasoning"`
	UsedForAudit      bool     `yaml:"used_for_audit"`
	Fallback          string   `yaml:"fallback"`
}

type TimestampsFile struct {
	SchemaVersion int             `yaml:"schema_version"`
	Namespace     string          `yaml:"namespace"`
	Timestamps    []TimestampSpec `yaml:"timestamps"`
}

type TransportSpec struct {
	ID               string `yaml:"id"`
	Transport        string `yaml:"transport"`
	Direction        string `yaml:"direction"`
	Owner            string `yaml:"owner"`
	Method           string `yaml:"method_or_message_type"`
	Path             string `yaml:"path_or_channel"`
	RequestContract  string `yaml:"request_contract"`
	ResponseContract string `yaml:"response_contract"`
	ErrorContract    string `yaml:"error_contract"`
	Version          string `yaml:"version"`
	Authorization    string `yaml:"authorization"`
	Redaction        string `yaml:"redaction"`
	Pagination       string `yaml:"pagination"`
	Bounded          bool   `yaml:"bounded"`
	Authority        string `yaml:"authority"`
}

type TransportsFile struct {
	SchemaVersion int             `yaml:"schema_version"`
	Namespace     string          `yaml:"namespace"`
	Transports    []TransportSpec `yaml:"transports"`
}

type WriterSpec struct {
	ID                 string             `yaml:"id"`
	Owner              string             `yaml:"owner"`
	Package            string             `yaml:"package"`
	Type               string             `yaml:"type"`
	Function           string             `yaml:"function"`
	Store              string             `yaml:"store"`
	Contract           string             `yaml:"contract"`
	ContractResolution ContractResolution `yaml:"contract_resolution"`
	Guard              string             `yaml:"guard"`
	Format             string             `yaml:"format"`
	BeforeWrite        string             `yaml:"before_write"`
}

type ContractResolution struct {
	Mode     string `yaml:"mode"`
	Field    string `yaml:"field"`
	Registry string `yaml:"registry"`
}

type WritersFile struct {
	SchemaVersion int          `yaml:"schema_version"`
	Namespace     string       `yaml:"namespace"`
	Writers       []WriterSpec `yaml:"writers"`
}

type JournalKindSpec struct {
	Kind       string `yaml:"kind"`
	GoPackage  string `yaml:"go_package"`
	GoType     string `yaml:"go_type"`
	Contract   string `yaml:"contract"`
	Validator  string `yaml:"validator"`
	LegacyRead bool   `yaml:"legacy_read"`
}

type JournalKindsFile struct {
	SchemaVersion int               `yaml:"schema_version"`
	Namespace     string            `yaml:"namespace"`
	Kinds         []JournalKindSpec `yaml:"kinds"`
}

type FieldMapping struct {
	GoField            string   `yaml:"go_field"`
	FieldPath          string   `yaml:"field_path"`
	WireName           string   `yaml:"wire_name"`
	GoType             string   `yaml:"go_type"`
	WireType           string   `yaml:"wire_type"`
	Required           bool     `yaml:"required"`
	Nullable           bool     `yaml:"nullable"`
	Omitempty          bool     `yaml:"omitempty"`
	Sensitivity        string   `yaml:"sensitivity"`
	Protection         string   `yaml:"protection"`
	Persistence        []string `yaml:"persistence"`
	Retention          string   `yaml:"retention"`
	IdentifierSemantic string   `yaml:"identifier_semantic"`
	TimestampSemantic  string   `yaml:"timestamp_semantic"`
}

type TypeMapping struct {
	Contract string         `yaml:"contract"`
	Package  string         `yaml:"package"`
	Type     string         `yaml:"type"`
	Fields   []FieldMapping `yaml:"fields"`
}

type MappingExemption struct {
	Package             string `yaml:"package"`
	Type                string `yaml:"type"`
	Field               string `yaml:"field"`
	Reason              string `yaml:"reason"`
	Scope               string `yaml:"scope"`
	ReviewStatus        string `yaml:"review_status"`
	Proof               string `yaml:"proof"`
	PersistenceAllowed  bool   `yaml:"persistence_allowed"`
	PublicOutputAllowed bool   `yaml:"public_output_allowed"`
}

type FieldMappingsFile struct {
	SchemaVersion int                `yaml:"schema_version"`
	Namespace     string             `yaml:"namespace"`
	Mappings      []TypeMapping      `yaml:"mappings"`
	Exemptions    []MappingExemption `yaml:"exemptions"`
}

type AdmissionEvent struct {
	EventType   string `yaml:"event_type"`
	Contract    string `yaml:"contract"`
	Disposition string `yaml:"disposition"`
	Workflow    bool   `yaml:"workflow"`
}

type OutputProfile struct {
	Name       string `yaml:"name"`
	Contract   string `yaml:"contract"`
	Visibility string `yaml:"visibility"`
	ReadOnly   bool   `yaml:"read_only"`
	Persisted  bool   `yaml:"persisted"`
	Bounded    bool   `yaml:"bounded"`
	Paginable  bool   `yaml:"paginable"`
	Redacted   bool   `yaml:"redacted"`
	Stable     bool   `yaml:"stable"`
	UI         bool   `yaml:"ui"`
	Automation bool   `yaml:"automation"`
	Authority  string `yaml:"authority"`
}

type StoresFile struct {
	SchemaVersion int            `yaml:"schema_version"`
	Namespace     string         `yaml:"namespace"`
	Stores        []CatalogStore `yaml:"stores"`
}

type CatalogStore struct {
	ID                    string   `yaml:"id"`
	Owner                 string   `yaml:"owner"`
	Format                string   `yaml:"format"`
	SchemaVersion         string   `yaml:"schema_version"`
	WriteMode             string   `yaml:"write_mode"`
	Atomicity             string   `yaml:"atomicity"`
	Fsync                 string   `yaml:"fsync"`
	Ordering              string   `yaml:"ordering"`
	Idempotence           string   `yaml:"idempotence"`
	Recovery              string   `yaml:"recovery"`
	Retention             string   `yaml:"retention"`
	Compaction            string   `yaml:"compaction"`
	Permissions           string   `yaml:"permissions"`
	Sensitivity           string   `yaml:"sensitivity"`
	Migration             string   `yaml:"migration"`
	Authority             string   `yaml:"authority"`
	ContractRefs          []string `yaml:"contract_refs"`
	Durable               bool     `yaml:"durable"`
	ClearSecretAllowed    bool     `yaml:"clear_secret_allowed"`
	ClearBiometricAllowed bool     `yaml:"clear_biometric_allowed"`
}

type BoundariesFile struct {
	SchemaVersion int               `yaml:"schema_version"`
	Namespace     string            `yaml:"namespace"`
	Boundaries    []CatalogBoundary `yaml:"boundaries"`
}

type CatalogBoundary struct {
	ID              string   `yaml:"id"`
	Name            string   `yaml:"name"`
	Producer        string   `yaml:"producer"`
	Consumer        string   `yaml:"consumer"`
	Owner           string   `yaml:"owner"`
	InputContracts  []string `yaml:"input_contracts"`
	OutputContracts []string `yaml:"output_contracts"`
	Transport       string   `yaml:"transport"`
	Validation      string   `yaml:"validation"`
	Transformation  string   `yaml:"transformation"`
	Errors          []string `yaml:"errors"`
	SideEffects     string   `yaml:"side_effects"`
	Persistence     []string `yaml:"persistence"`
	Authority       string   `yaml:"authority"`
}

type ErrorsFile struct {
	SchemaVersion int            `yaml:"schema_version"`
	Namespace     string         `yaml:"namespace"`
	Errors        []CatalogError `yaml:"errors"`
}

type CatalogError struct {
	ID        string `yaml:"id"`
	Owner     string `yaml:"owner"`
	Category  string `yaml:"category"`
	Retryable bool   `yaml:"retryable"`
	Public    bool   `yaml:"public"`
	Authority string `yaml:"authority"`
}

type CatalogSet struct {
	Catalog       CatalogFile
	Boundaries    BoundariesFile
	Stores        StoresFile
	Errors        ErrorsFile
	Identifiers   IdentifiersFile
	Timestamps    TimestampsFile
	Transports    TransportsFile
	Writers       WritersFile
	JournalKinds  JournalKindsFile
	FieldMappings FieldMappingsFile
}

// Load reads the four catalog files below root.
func Load(root string) (CatalogSet, error) {
	var result CatalogSet
	if err := decode(filepath.Join(root, "configs/cge/contracts/catalog.yaml"), &result.Catalog); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/boundaries.yaml"), &result.Boundaries); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/stores.yaml"), &result.Stores); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/errors.yaml"), &result.Errors); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/identifiers.yaml"), &result.Identifiers); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/timestamps.yaml"), &result.Timestamps); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/transports.yaml"), &result.Transports); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/writers.yaml"), &result.Writers); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/journal-kinds.yaml"), &result.JournalKinds); err != nil {
		return CatalogSet{}, err
	}
	if err := decode(filepath.Join(root, "configs/cge/contracts/field-mappings.yaml"), &result.FieldMappings); err != nil {
		return CatalogSet{}, err
	}
	if result.Boundaries.SchemaVersion != 1 || result.Boundaries.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("boundaries header must be schema 1, namespace synora.cge")
	}
	if result.Stores.SchemaVersion != 1 || result.Stores.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("stores header must be schema 1, namespace synora.cge")
	}
	if result.Errors.SchemaVersion != 1 || result.Errors.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("errors header must be schema 1, namespace synora.cge")
	}
	if result.Identifiers.SchemaVersion != 1 || result.Identifiers.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("identifiers header must be schema 1, namespace synora.cge")
	}
	if result.Timestamps.SchemaVersion != 1 || result.Timestamps.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("timestamps header must be schema 1, namespace synora.cge")
	}
	if result.Transports.SchemaVersion != 1 || result.Transports.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("transports header must be schema 1, namespace synora.cge")
	}
	if result.Writers.SchemaVersion != 1 || result.Writers.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("writers header must be schema 1, namespace synora.cge")
	}
	if result.JournalKinds.SchemaVersion != 1 || result.JournalKinds.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("journal-kinds header must be schema 1, namespace synora.cge")
	}
	if result.FieldMappings.SchemaVersion != 1 || result.FieldMappings.Namespace != "synora.cge" {
		return CatalogSet{}, fmt.Errorf("field-mappings header must be schema 1, namespace synora.cge")
	}
	return result, nil
}

func decode(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	nodeDecoder := yaml.NewDecoder(strings.NewReader(string(data)))
	var document yaml.Node
	if err := nodeDecoder.Decode(&document); err != nil {
		if err == io.EOF {
			return fmt.Errorf("parse %s: empty YAML document", path)
		}
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if err := rejectDuplicateYAMLKeys(&document); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var extra yaml.Node
	if err := nodeDecoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse %s: multiple YAML documents", path)
		}
		return fmt.Errorf("parse %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		if err == io.EOF {
			return fmt.Errorf("parse %s: empty YAML document", path)
		}
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var second any
	if err := decoder.Decode(&second); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse %s: multiple YAML documents", path)
		}
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func rejectDuplicateYAMLKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.MappingNode:
		seen := make(map[string]bool, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if key.Kind == yaml.ScalarNode && seen[key.Value] {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			if key.Kind == yaml.ScalarNode {
				seen[key.Value] = true
			}
			if err := rejectDuplicateYAMLKeys(value); err != nil {
				return err
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			if err := rejectDuplicateYAMLKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate loads and validates the catalog rooted at root.
func Validate(root string) (CatalogSet, error) {
	set, err := Load(root)
	if err != nil {
		return CatalogSet{}, err
	}
	if err := validateSet(set); err != nil {
		return CatalogSet{}, err
	}
	return set, nil
}

func validateSet(set CatalogSet) error {
	if set.Catalog.Catalog.SchemaVersion != 1 || set.Catalog.Catalog.Namespace != "synora.cge" || set.Catalog.Catalog.AuthorityCeiling != "advisory" {
		return fmt.Errorf("catalog header must be schema 1, namespace synora.cge and advisory")
	}
	if err := validateExactList("category", set.Catalog.Catalog.Categories, canonicalCategories); err != nil {
		return err
	}
	if err := validateExactList("trust", set.Catalog.Catalog.TrustLevels, canonicalTrust); err != nil {
		return err
	}
	if err := validateExactList("sensitivity", set.Catalog.Catalog.SensitivityClasses, canonicalSensitivity); err != nil {
		return err
	}
	if err := validateExactList("authority", set.Catalog.Catalog.AuthorityLevels, canonicalAuthority); err != nil {
		return err
	}
	if err := validateExactList("status", set.Catalog.Catalog.StabilityStatuses, canonicalStatus); err != nil {
		return err
	}
	controls := make(map[string]bool, len(set.Catalog.Catalog.DataControls))
	for _, control := range set.Catalog.Catalog.DataControls {
		if control.Name == "" || controls[control.Name] || !validSensitivity[control.Classification] || control.Memory == "" || control.Logs == "" || control.Journal == "" || control.WAL == "" || control.Ledger == "" || control.Protection == "" {
			return fmt.Errorf("data control %q is incomplete or duplicated", control.Name)
		}
		controls[control.Name] = true
	}
	for _, name := range []string{"identity", "candidate_identities", "device_id", "camera_id", "event_id", "clip_id", "track_id", "activation_id", "sequence_key", "ip", "tokens", "images", "videos", "embeddings", "faces", "localisation", "resident_presence", "topology"} {
		if !controls[name] {
			return fmt.Errorf("required data control %q is missing", name)
		}
	}

	contracts := make(map[string]CatalogContract, len(set.Catalog.Contracts))
	for _, contract := range set.Catalog.Contracts {
		if contract.ID == "" || contracts[contract.ID].ID != "" {
			return fmt.Errorf("contract IDs must be unique and non-empty: %q", contract.ID)
		}
		if !versionedID.MatchString(contract.ID) {
			return fmt.Errorf("contract %q has no versioned ID", contract.ID)
		}
		if !validCategories[contract.Category] || contract.Owner == "" || contract.Transport == "" || len(contract.Producers) == 0 || len(contract.Consumers) == 0 {
			return fmt.Errorf("contract %q lacks valid category, owner, transport, producer or consumer", contract.ID)
		}
		if !validAuthority[contract.Authority] || !validTrust[contract.Trust] || !validSensitivity[contract.Sensitivity] || !validStatus[contract.Status] {
			return fmt.Errorf("contract %q has an invalid trust, sensitivity, authority or status", contract.ID)
		}
		if strings.HasPrefix(contract.Owner, "cge") && (contract.Authority == "authorized_action" || contract.Authority == "authorized_decision") {
			return fmt.Errorf("CGE contract %q exceeds advisory authority ceiling", contract.ID)
		}
		if len(contract.Persistence) > 0 {
			if strings.TrimSpace(contract.Justification) == "" || strings.TrimSpace(contract.MigrationPolicy) == "" || strings.TrimSpace(contract.CompatibilityPolicy) == "" || strings.TrimSpace(contract.RetentionPolicy) == "" {
				return fmt.Errorf("persistent contract %q lacks justification, migration, compatibility or retention policy", contract.ID)
			}
		}
		if err := validateImplementation(contract.ID, contract.Implementation); err != nil {
			return err
		}
		fields := make(map[string]bool, len(contract.Fields))
		for _, field := range contract.Fields {
			if field.Name == "" || field.Type == "" || fields[field.Name] {
				return fmt.Errorf("contract %q has missing or duplicate field %q", contract.ID, field.Name)
			}
			fields[field.Name] = true
			if field.Description == "" || field.Source == "" || field.Protection == "" || field.Retention == "" || field.Validation == "" || !validTrust[field.Trust] || !validSensitivity[field.Sensitivity] {
				return fmt.Errorf("contract %q field %q is incomplete", contract.ID, field.Name)
			}
			if field.Sensitivity == "secret" && len(field.Persistence) > 0 {
				return fmt.Errorf("secret field %q in contract %q is durable", field.Name, contract.ID)
			}
			if field.Sensitivity == "biometric" && len(field.Persistence) > 0 && strings.Contains(strings.ToLower(field.Protection), "clear") {
				return fmt.Errorf("biometric field %q in contract %q is durable in clear", field.Name, contract.ID)
			}
		}
		contracts[contract.ID] = contract
	}

	stores := make(map[string]CatalogStore, len(set.Stores.Stores))
	for _, store := range set.Stores.Stores {
		if store.ID == "" || stores[store.ID].ID != "" {
			return fmt.Errorf("store IDs must be unique and non-empty: %q", store.ID)
		}
		if store.Owner == "" || store.Format == "" || store.SchemaVersion == "" || store.WriteMode == "" || store.Authority == "" || !validSensitivity[store.Sensitivity] {
			return fmt.Errorf("store %q is incomplete", store.ID)
		}
		if !validAuthority[store.Authority] {
			return fmt.Errorf("store %q has invalid authority", store.ID)
		}
		if store.Durable && (store.ClearSecretAllowed || store.ClearBiometricAllowed) {
			return fmt.Errorf("store %q permits clear secret or biometric data", store.ID)
		}
		stores[store.ID] = store
	}

	for _, contract := range contracts {
		for _, storeID := range contract.Persistence {
			if _, ok := stores[storeID]; !ok {
				return fmt.Errorf("contract %q references unknown store %q", contract.ID, storeID)
			}
		}
		for _, field := range contract.Fields {
			for _, storeID := range field.Persistence {
				if _, ok := stores[storeID]; !ok {
					return fmt.Errorf("contract %q field %q references unknown store %q", contract.ID, field.Name, storeID)
				}
			}
		}
	}
	for _, store := range stores {
		for _, contractID := range store.ContractRefs {
			if _, ok := contracts[contractID]; !ok {
				return fmt.Errorf("store %q references unknown contract %q", store.ID, contractID)
			}
		}
	}

	errors := make(map[string]CatalogError, len(set.Errors.Errors))
	for _, item := range set.Errors.Errors {
		if item.ID == "" || errors[item.ID].ID != "" {
			return fmt.Errorf("error IDs must be unique and non-empty: %q", item.ID)
		}
		if item.Owner == "" || item.Category == "" || !validAuthority[item.Authority] {
			return fmt.Errorf("error %q is incomplete", item.ID)
		}
		errors[item.ID] = item
	}

	boundaries := make(map[string]bool, len(set.Boundaries.Boundaries))
	for _, boundary := range set.Boundaries.Boundaries {
		if boundary.ID == "" || boundaries[boundary.ID] || boundary.Name == "" || boundary.Owner == "" || boundary.Producer == "" || boundary.Consumer == "" || boundary.Validation == "" || boundary.Transformation == "" || boundary.SideEffects == "" || boundary.Authority == "" {
			return fmt.Errorf("boundary %q is incomplete or duplicated", boundary.ID)
		}
		if !validAuthority[boundary.Authority] {
			return fmt.Errorf("boundary %q has invalid authority", boundary.ID)
		}
		if len(boundary.InputContracts) == 0 || len(boundary.OutputContracts) == 0 {
			return fmt.Errorf("boundary %q must have input and output contracts", boundary.ID)
		}
		for _, contractID := range append(append([]string{}, boundary.InputContracts...), boundary.OutputContracts...) {
			if _, ok := contracts[contractID]; !ok {
				return fmt.Errorf("boundary %q references unknown contract %q", boundary.ID, contractID)
			}
		}
		for _, storeID := range boundary.Persistence {
			if _, ok := stores[storeID]; !ok {
				return fmt.Errorf("boundary %q references unknown store %q", boundary.ID, storeID)
			}
		}
		for _, errorID := range boundary.Errors {
			if _, ok := errors[errorID]; !ok {
				return fmt.Errorf("boundary %q references unknown error %q", boundary.ID, errorID)
			}
		}
		boundaries[boundary.ID] = true
	}

	admission := map[string]AdmissionEvent{}
	for _, item := range set.Catalog.AdmissionEvents {
		if item.EventType == "" || admission[item.EventType].EventType != "" {
			return fmt.Errorf("admission event types must be unique and non-empty: %q", item.EventType)
		}
		if _, ok := contracts[item.Contract]; !ok {
			return fmt.Errorf("admission event %q references unknown contract %q", item.EventType, item.Contract)
		}
		if item.Disposition == "" {
			return fmt.Errorf("admission event %q has no disposition", item.EventType)
		}
		admission[item.EventType] = item
	}
	for _, eventType := range []string{"vision.identity", "vision.unknown", "vision.uncertain"} {
		item, ok := admission[eventType]
		if !ok || item.Disposition != "admitted" || !item.Workflow {
			return fmt.Errorf("allowlisted event %q is not catalogued as admitted workflow input", eventType)
		}
	}
	if item := admission["vision.motion"]; item.Disposition != "historical_only" || item.Workflow {
		return fmt.Errorf("vision.motion must be historical_only and excluded from workflow")
	}
	profiles := make(map[string]bool, len(set.Catalog.OutputProfiles))
	for _, profile := range set.Catalog.OutputProfiles {
		if profile.Name == "" || profiles[profile.Name] || profile.Contract == "" || profile.Visibility == "" || !profile.ReadOnly || !profile.Bounded || !profile.Redacted || profile.Automation || !validAuthority[profile.Authority] {
			return fmt.Errorf("output profile %q is incomplete, writable, unbounded, unredacted or executable", profile.Name)
		}
		if _, ok := contracts[profile.Contract]; !ok {
			return fmt.Errorf("output profile %q references unknown contract %q", profile.Name, profile.Contract)
		}
		profiles[profile.Name] = true
	}
	for _, name := range []string{"ObservationResult", "Snapshot", "Explanation", "AdmissionStatus", "AdmissionMetrics", "WorkflowStatus", "WorkflowProjection", "EpisodeSnapshots", "Facts", "Hypotheses", "EvidenceEvaluations", "CognitiveSituations", "RecommendationSets", "HistoricalComparisons", "CalibrationRecords", "Analytics", "Errors", "HealthStatus"} {
		if !profiles[name] {
			return fmt.Errorf("required output profile %q is missing", name)
		}
	}
	if err := validateIdentifiers(set, stores); err != nil {
		return err
	}
	if err := validateTimestamps(set, stores); err != nil {
		return err
	}
	if err := validateTransports(set, contracts); err != nil {
		return err
	}
	if err := validateWriters(set, contracts, stores); err != nil {
		return err
	}
	if err := validateJournalKinds(set, contracts); err != nil {
		return err
	}
	if err := validateFieldMappings(set, contracts); err != nil {
		return err
	}
	return nil
}

func validateFieldMappings(set CatalogSet, contracts map[string]CatalogContract) error {
	seen := map[string]bool{}
	for _, mapping := range set.FieldMappings.Mappings {
		if mapping.Contract == "" || seen[mapping.Contract] || mapping.Package == "" || mapping.Type == "" || len(mapping.Fields) == 0 {
			return fmt.Errorf("field mapping %s.%s is incomplete or duplicated", mapping.Package, mapping.Type)
		}
		contract, ok := contracts[mapping.Contract]
		if !ok {
			return fmt.Errorf("field mapping references unknown contract %q", mapping.Contract)
		}
		if contract.Implementation.Package != mapping.Package || contract.Implementation.Type != mapping.Type {
			return fmt.Errorf("field mapping %s does not match contract implementation", mapping.Contract)
		}
		fields := map[string]bool{}
		for _, field := range mapping.Fields {
			if field.GoField == "" || field.FieldPath == "" || field.WireName == "" || field.GoType == "" || field.WireType == "" || field.Sensitivity == "" || field.Protection == "" || field.Retention == "" || field.IdentifierSemantic == "" || field.TimestampSemantic == "" || fields[field.GoField] {
				return fmt.Errorf("field mapping %s has incomplete or duplicate field %q", mapping.Contract, field.GoField)
			}
			if !validSensitivity[field.Sensitivity] {
				return fmt.Errorf("field mapping %s.%s has invalid sensitivity", mapping.Contract, field.GoField)
			}
			fields[field.GoField] = true
		}
		seen[mapping.Contract] = true
	}
	for _, exemption := range set.FieldMappings.Exemptions {
		if exemption.Package == "" || exemption.Type == "" || exemption.Reason == "" || exemption.Scope == "" || exemption.ReviewStatus != "approved" || exemption.Proof == "" || exemption.PersistenceAllowed || exemption.PublicOutputAllowed {
			return fmt.Errorf("mapping exemption %s.%s.%s is incomplete or unsafe", exemption.Package, exemption.Type, exemption.Field)
		}
		if exemption.Proof != "not_reachable_from_contract_roots" && exemption.Proof != "not_serialized" && exemption.Proof != "not_returned" && exemption.Proof != "not_persisted" && exemption.Proof != "not_transported" {
			return fmt.Errorf("mapping exemption %s.%s.%s has unsupported proof %q", exemption.Package, exemption.Type, exemption.Field, exemption.Proof)
		}
	}
	return nil
}

func validateJournalKinds(set CatalogSet, contracts map[string]CatalogContract) error {
	seen := map[string]bool{}
	for _, item := range set.JournalKinds.Kinds {
		if item.Kind == "" || seen[item.Kind] || item.GoPackage == "" || item.GoType == "" || item.Contract == "" || item.Validator == "" {
			return fmt.Errorf("journal kind %q is incomplete or duplicated", item.Kind)
		}
		if _, ok := contracts[item.Contract]; !ok {
			return fmt.Errorf("journal kind %q references unknown contract %q", item.Kind, item.Contract)
		}
		seen[item.Kind] = true
	}
	if len(seen) != 14 {
		return fmt.Errorf("journal kind catalog must contain 14 accepted v1 kinds, got %d", len(seen))
	}
	return nil
}

func validateWriters(set CatalogSet, contracts map[string]CatalogContract, stores map[string]CatalogStore) error {
	seen := map[string]bool{}
	for _, writer := range set.Writers.Writers {
		if writer.ID == "" || seen[writer.ID] || writer.Owner == "" || writer.Package == "" || writer.Type == "" || writer.Function == "" || writer.Store == "" || writer.Contract == "" || writer.Guard == "" || writer.Format == "" || writer.BeforeWrite == "" {
			return fmt.Errorf("writer %q is incomplete or duplicated", writer.ID)
		}
		if _, ok := stores[writer.Store]; !ok {
			return fmt.Errorf("writer %q references unknown store %q", writer.ID, writer.Store)
		}
		if _, ok := contracts[writer.Contract]; !ok {
			return fmt.Errorf("writer %q references unknown contract %q", writer.ID, writer.Contract)
		}
		if writer.ContractResolution.Mode != "" {
			if writer.ContractResolution.Mode != "discriminator" || writer.ContractResolution.Field == "" || writer.ContractResolution.Registry == "" {
				return fmt.Errorf("writer %q has invalid contract resolution", writer.ID)
			}
		}
		if writer.Guard != "ValidateStoreWrite" {
			return fmt.Errorf("writer %q is not protected by ValidateStoreWrite", writer.ID)
		}
		seen[writer.ID] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("no CGE durable writers catalogued")
	}
	return nil
}

func validateIdentifiers(set CatalogSet, stores map[string]CatalogStore) error {
	required := []string{"event_id", "observation_id", "entity_id", "candidate_entity_id", "device_id", "camera_id", "node_id", "zone_id", "clip_id", "track_id", "activation_id", "sequence_key", "episode_id", "chain_id", "hypothesis_set_id", "fact_id", "evaluation_id", "situation_id", "recommendation_set_id", "comparison_id", "transaction_id", "journal_id", "ledger_record_id", "revision", "sequence", "digest", "fingerprint"}
	seen := map[string]bool{}
	for _, item := range set.Identifiers.Identifiers {
		if item.ID == "" || seen[item.ID] || item.Semantic == "" || item.Owner == "" || item.Generator == "" || item.Scope == "" || item.Uniqueness == "" || item.WireType == "" || item.Protection == "" || item.Domain == "" || item.Deduplication == "" || item.Correlation == "" || item.Ordering == "" || item.RestartStability == "" || item.LegacyBehavior == "" {
			return fmt.Errorf("identifier %q is incomplete or duplicated", item.ID)
		}
		for _, storeID := range item.Persistence {
			if _, ok := stores[storeID]; !ok {
				return fmt.Errorf("identifier %q references unknown store %q", item.ID, storeID)
			}
		}
		seen[item.ID] = true
	}
	for _, id := range required {
		if !seen[id] {
			return fmt.Errorf("required identifier %q is missing", id)
		}
	}
	return nil
}

func validateTimestamps(set CatalogSet, stores map[string]CatalogStore) error {
	required := []string{"observed_at", "produced_at", "received_at", "ingested_at", "processed_at", "committed_at", "persisted_at", "created_at", "updated_at", "status_changed_at", "closed_at", "last_seen_at", "recovered_at"}
	seen := map[string]bool{}
	for _, item := range set.Timestamps.Timestamps {
		if item.ID == "" || seen[item.ID] || item.Semantic == "" || item.Producer == "" || item.Clock == "" || item.Timezone == "" || item.Ordering == "" || item.MaximumFutureSkew == "" || item.MaximumPastAge == "" || item.Fallback == "" {
			return fmt.Errorf("timestamp %q is incomplete or duplicated", item.ID)
		}
		for _, storeID := range item.Persistence {
			if _, ok := stores[storeID]; !ok {
				return fmt.Errorf("timestamp %q references unknown store %q", item.ID, storeID)
			}
		}
		seen[item.ID] = true
	}
	for _, id := range required {
		if !seen[id] {
			return fmt.Errorf("required timestamp %q is missing", id)
		}
	}
	return nil
}

func validateTransports(set CatalogSet, contracts map[string]CatalogContract) error {
	seen := map[string]bool{}
	for _, item := range set.Transports.Transports {
		if item.ID == "" || seen[item.ID] || item.Transport == "" || item.Direction == "" || item.Owner == "" || item.Method == "" || item.Path == "" || item.RequestContract == "" || item.ResponseContract == "" || item.ErrorContract == "" || item.Version == "" || item.Authorization == "" || item.Redaction == "" || item.Pagination == "" || item.Authority == "" {
			return fmt.Errorf("transport %q is incomplete or duplicated", item.ID)
		}
		for _, contractID := range []string{item.RequestContract, item.ResponseContract, item.ErrorContract} {
			if _, ok := contracts[contractID]; !ok {
				return fmt.Errorf("transport %q references unknown contract %q", item.ID, contractID)
			}
		}
		if !validAuthority[item.Authority] || (strings.HasPrefix(item.Owner, "cge") && (item.Authority == "authorized_action" || item.Authority == "authorized_decision")) {
			return fmt.Errorf("transport %q has invalid authority", item.ID)
		}
		seen[item.ID] = true
	}
	return nil
}

func validateEnumList(name string, values []string, allowed map[string]bool) error {
	if len(values) == 0 {
		return fmt.Errorf("catalog %s list is empty", name)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !allowed[value] || seen[value] {
			return fmt.Errorf("invalid or duplicate catalog %s %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func validateExactList(name string, values, canonical []string) error {
	if len(values) != len(canonical) {
		return fmt.Errorf("catalog %s list must contain exactly %d values, found %d", name, len(canonical), len(values))
	}
	left, right := append([]string(nil), values...), append([]string(nil), canonical...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return fmt.Errorf("catalog %s list differs from canonical v1 set", name)
		}
	}
	return nil
}

func validateImplementation(contractID string, implementation Implementation) error {
	allowed := map[string]bool{"go_struct": true, "go_alias": true, "go_scalar": true, "external": true, "derived_projection": true, "envelope": true}
	if !allowed[implementation.Kind] {
		return fmt.Errorf("contract %q has invalid implementation kind %q", contractID, implementation.Kind)
	}
	if implementation.Kind == "external" {
		if strings.TrimSpace(implementation.Justification) == "" {
			return fmt.Errorf("external contract %q lacks implementation justification", contractID)
		}
		return nil
	}
	if strings.TrimSpace(implementation.Package) == "" || strings.TrimSpace(implementation.Type) == "" || strings.TrimSpace(implementation.WireFormat) == "" {
		return fmt.Errorf("contract %q lacks implementation package, type or wire format", contractID)
	}
	return nil
}
