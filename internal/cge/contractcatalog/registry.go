package contractcatalog

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"synora/internal/cge/durableids"
)

const (
	ErrUnknownContract         = "cge.contract.unknown"
	ErrTypeMismatch            = "cge.contract.type_mismatch"
	ErrFieldMismatch           = "cge.contract.field_mismatch"
	ErrStoreForbidden          = "cge.contract.store_forbidden"
	ErrAuthorityViolation      = "cge.contract.authority_violation"
	ErrProtectionViolation     = "cge.contract.protection_violation"
	ErrSensitiveWriteForbidden = "cge.contract.sensitive_write_forbidden"
	ErrGeneratedRegistryStale  = "cge.contract.generated_registry_stale"
)

type Authority string

const (
	AuthorityDescriptive        Authority = "descriptive"
	AuthorityDiagnostic         Authority = "diagnostic"
	AuthorityAdvisory           Authority = "advisory"
	AuthorityAuthorizedDecision Authority = "authorized_decision"
	AuthorityAuthorizedAction   Authority = "authorized_action"
)

type FieldDescriptor struct {
	Name        string
	GoField     string
	FieldPath   string
	Type        string
	GoType      string
	WireType    string
	WireName    string
	WirePath    string
	Required    bool
	Nullable    bool
	Protection  string
	Sensitivity string
	Persistence []string
	Identifier  string
	Timestamp   string
}

type ImplementationDescriptor struct {
	Kind          string
	Package       string
	Type          string
	WireFormat    string
	Validator     string
	Justification string
}

type Descriptor struct {
	ID             string
	Version        string
	Category       string
	Owner          string
	Authority      Authority
	Trust          string
	Sensitivity    string
	Status         string
	Persistence    []string
	Fields         []FieldDescriptor
	Implementation ImplementationDescriptor
	SchemaHash     string
}

type BoundaryDescriptor struct {
	ID              string
	InputContracts  []string
	OutputContracts []string
	Persistence     []string
	Authority       Authority
}

type StoreDescriptor struct {
	ID                    string
	Authority             Authority
	Sensitivity           string
	ContractRefs          []string
	Durable               bool
	ClearSecretAllowed    bool
	ClearBiometricAllowed bool
}

type ErrorDescriptor struct {
	ID        string
	Owner     string
	Category  string
	Retryable bool
	Public    bool
	Authority Authority
}

type WriterDescriptor struct {
	ID                         string
	Owner                      string
	Package                    string
	Type                       string
	Function                   string
	Store                      string
	Contract                   string
	ContractResolutionMode     string
	ContractResolutionField    string
	ContractResolutionRegistry string
	Guard                      string
	Format                     string
	BeforeWrite                string
}

type JournalKindDescriptor struct {
	Kind       string
	GoPackage  string
	GoType     string
	Contract   string
	Validator  string
	LegacyRead bool
}

// AuditRecord is the closed envelope used by the catalog for integrity-only
// durable markers. Runtime stores may use a more specific envelope; this type
// exists so the catalog has an explicit Go owner for the audit surface.
type AuditRecord struct {
	Operation string `json:"operation"`
	Sequence  uint64 `json:"sequence"`
	Revision  uint64 `json:"revision"`
	Digest    string `json:"digest"`
}

type Registry struct {
	Contracts          map[string]Descriptor
	Boundaries         map[string]BoundaryDescriptor
	Stores             map[string]StoreDescriptor
	Errors             map[string]ErrorDescriptor
	Identifiers        map[string]IdentifierSpec
	Timestamps         map[string]TimestampSpec
	Transports         map[string]TransportSpec
	Writers            map[string]WriterDescriptor
	JournalKinds       map[string]JournalKindDescriptor
	ContractHashes     map[string]string
	CatalogFingerprint string
}

func Contract(id string) (Descriptor, bool) {
	value, ok := generatedRegistry.Contracts[id]
	return value, ok
}
func Boundary(id string) (BoundaryDescriptor, bool) {
	value, ok := generatedRegistry.Boundaries[id]
	return value, ok
}
func Store(id string) (StoreDescriptor, bool) {
	value, ok := generatedRegistry.Stores[id]
	return value, ok
}
func ErrorDescriptorFor(id string) (ErrorDescriptor, bool) {
	value, ok := generatedRegistry.Errors[id]
	return value, ok
}
func JournalKind(kind string) (JournalKindDescriptor, bool) {
	value, ok := generatedRegistry.JournalKinds[kind]
	return value, ok
}
func CatalogFingerprint() string { return generatedRegistry.CatalogFingerprint }

// ValidateTypedPayload verifies a closed union member before its enclosing
// durable record is encoded. The validator name is retained in the catalog so
// future members can add semantic checks without weakening the root type test.
func ValidateTypedPayload(packagePath, typeName, validator string, value any) error {
	if value == nil || packagePath == "" || typeName == "" || validator == "" {
		return errors.New(ErrTypeMismatch)
	}
	typ := reflect.TypeOf(value)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ.PkgPath() != packagePath || typ.Name() != typeName {
		return errors.New(ErrTypeMismatch)
	}
	return nil
}

func ValidateInput(contractID string, value any) error {
	return validateContractValue(contractID, value)
}

func ValidateOutput(contractID string, value any) error {
	return validateContractValue(contractID, value)
}

func validateContractValue(contractID string, value any) error {
	if err := validateValue(contractID, value); err != nil {
		return err
	}
	descriptor, ok := Contract(contractID)
	if !ok {
		return errors.New(ErrUnknownContract)
	}
	if err := validateProtectedFields(descriptor, value); err != nil {
		return err
	}
	return validateDurableJSON(descriptor, value)
}

func ValidateAuthority(contractID string, requested Authority) error {
	descriptor, ok := Contract(contractID)
	if !ok {
		return errors.New(ErrUnknownContract)
	}
	if strings.HasPrefix(descriptor.Owner, "cge") && (requested == AuthorityAuthorizedAction || requested == AuthorityAuthorizedDecision) {
		return errors.New(ErrAuthorityViolation)
	}
	if authorityRank(requested) > authorityRank(descriptor.Authority) && descriptor.Owner != "historical_core" && descriptor.Owner != "automation" {
		return errors.New(ErrAuthorityViolation)
	}
	return nil
}

// ValidateStoreWrite checks a value without changing it. It must be called by
// a writer before its durable marshal/append/rename operation.
func ValidateStoreWrite(storeID string, contractID string, value any) error {
	store, ok := Store(storeID)
	if !ok {
		return errors.New(ErrStoreForbidden)
	}
	descriptor, ok := Contract(contractID)
	if !ok {
		return errors.New(ErrUnknownContract)
	}
	allowed := false
	for _, candidate := range store.ContractRefs {
		if candidate == contractID {
			allowed = true
			break
		}
	}
	if !allowed {
		return errors.New(ErrStoreForbidden)
	}
	if store.Durable && descriptor.Sensitivity == "secret" {
		return errors.New(ErrSensitiveWriteForbidden)
	}
	if err := ValidateAuthority(contractID, descriptor.Authority); err != nil {
		return err
	}
	if err := validateValue(contractID, value); err != nil {
		return err
	}
	if err := validateProtectedFields(descriptor, value); err != nil {
		return err
	}
	return validateDurableJSON(descriptor, value)
}

func validateValue(contractID string, value any) error {
	descriptor, ok := Contract(contractID)
	if !ok {
		return errors.New(ErrUnknownContract)
	}
	if value == nil {
		return errors.New(ErrTypeMismatch)
	}
	if descriptor.Implementation.Kind == "go_scalar" || descriptor.Implementation.Kind == "go_alias" || descriptor.Implementation.Kind == "external" {
		return nil
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return errors.New(ErrTypeMismatch)
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return errors.New(ErrTypeMismatch)
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return errors.New(ErrTypeMismatch)
	}
	expected := descriptor.Implementation.Type
	if expected == "" || rv.Type().Name() != expected || rv.Type().PkgPath() != descriptor.Implementation.Package {
		return errors.New(ErrTypeMismatch)
	}
	return nil
}

func validateProtectedFields(descriptor Descriptor, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New(ErrTypeMismatch)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil
	}
	for _, field := range descriptor.Fields {
		kind, protected := protectionKind(field.Protection)
		if !protected {
			continue
		}
		wire := field.WireName
		if wire == "" {
			wire = field.Name
		}
		raw, exists := object[wire]
		if !exists || raw == nil {
			if field.Required && !field.Nullable {
				return errors.New(ErrFieldMismatch)
			}
			continue
		}
		text, ok := raw.(string)
		if !ok {
			continue
		}
		if text == "" && !field.Required {
			continue
		}
		if !durableids.IsProtectedFor(kind, text) {
			return errors.New(ErrProtectionViolation)
		}
	}
	return nil
}

func validateDurableJSON(descriptor Descriptor, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New(ErrTypeMismatch)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil
	}
	return validateDurableValue(descriptor, decoded)
}

func validateDurableValue(descriptor Descriptor, value any) error {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			lower := strings.ToLower(key)
			if (isSecretKey(lower) || isBiometricKey(lower)) && hasNonEmptyValue(child) {
				return errors.New(ErrSensitiveWriteForbidden)
			}
			if hasProtectedDescriptorField(descriptor) {
				if kind, ok := keyProtectionKind(lower); ok {
					if err := validateProtectedValue(kind, child); err != nil {
						return err
					}
				}
			}
			if err := validateDurableValue(descriptor, child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := validateDurableValue(descriptor, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasProtectedDescriptorField(descriptor Descriptor) bool {
	for _, field := range descriptor.Fields {
		if _, ok := protectionKind(field.Protection); ok {
			return true
		}
	}
	return false
}

func validateProtectedValue(kind durableids.Kind, value any) error {
	switch current := value.(type) {
	case string:
		if current != "" && !durableids.IsProtectedFor(kind, current) {
			return errors.New(ErrProtectionViolation)
		}
	case []any:
		for _, child := range current {
			if err := validateProtectedValue(kind, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func keyProtectionKind(key string) (durableids.Kind, bool) {
	switch {
	case key == "identity" || key == "entity_id" || key == "candidate_entity_ids" || strings.Contains(key, "candidate_entity"):
		return durableids.KindEntity, true
	case key == "event_id" || key == "observation_id" || key == "source_event_ref" || strings.Contains(key, "observation_id"):
		return durableids.KindObservation, true
	case key == "device_id" || key == "camera_id":
		return durableids.KindDevice, true
	case key == "clip_id":
		return durableids.KindClip, true
	case key == "track_id":
		return durableids.KindTrack, true
	case key == "activation_id":
		return durableids.KindActivation, true
	case key == "sequence_key":
		return durableids.KindSequence, true
	default:
		return "", false
	}
}

func isSecretKey(key string) bool {
	return key == "token" || strings.HasSuffix(key, "_token") || key == "password" || strings.HasSuffix(key, "_password") || key == "secret" || strings.HasSuffix(key, "_secret") || strings.Contains(key, "private_key")
}

func isBiometricKey(key string) bool {
	return key == "image" || strings.HasSuffix(key, "_image") || key == "video" || strings.HasSuffix(key, "_video") || strings.Contains(key, "embedding") || strings.Contains(key, "face")
}

func hasNonEmptyValue(value any) bool {
	switch current := value.(type) {
	case nil:
		return false
	case string:
		return current != ""
	case []any:
		return len(current) > 0
	case map[string]any:
		return len(current) > 0
	default:
		return true
	}
}

func protectionKind(protection string) (durableids.Kind, bool) {
	protection = strings.ToLower(protection)
	switch {
	case strings.Contains(protection, "observation"):
		return durableids.KindObservation, true
	case strings.Contains(protection, "entity"):
		return durableids.KindEntity, true
	case strings.Contains(protection, "device"):
		return durableids.KindDevice, true
	case strings.Contains(protection, "clip"):
		return durableids.KindClip, true
	case strings.Contains(protection, "track"):
		return durableids.KindTrack, true
	case strings.Contains(protection, "activation"):
		return durableids.KindActivation, true
	case strings.Contains(protection, "sequence"):
		return durableids.KindSequence, true
	default:
		return "", false
	}
}

func authorityRank(value Authority) int {
	switch value {
	case AuthorityDescriptive:
		return 1
	case AuthorityDiagnostic:
		return 2
	case AuthorityAdvisory:
		return 3
	case AuthorityAuthorizedDecision:
		return 4
	case AuthorityAuthorizedAction:
		return 5
	default:
		return 99
	}
}
