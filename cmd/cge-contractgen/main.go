// Command cge-contractgen generates and verifies the executable CGE contract
// registry. It never writes runtime configuration or durable CGE data.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"synora/internal/cge/contractcatalog"
	"synora/internal/cge/contractcatalog/gosurface"
)

const generatedPath = "internal/cge/contractcatalog/generated_registry.go"
const inventoryPath = "configs/cge/contracts/surface-inventory.yaml"
const baselinePath = "configs/cge/contracts/baselines/cge-contract-set-v1.json"

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "generate" && os.Args[1] != "check" && os.Args[1] != "check-compat" && os.Args[1] != "coverage" && os.Args[1] != "freeze-baseline" && os.Args[1] != "bootstrap-mappings") {
		fmt.Fprintln(os.Stderr, "usage: cge-contractgen generate|check|check-compat|coverage|freeze-baseline|bootstrap-mappings")
		os.Exit(2)
	}
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	command := os.Args[1]
	if command == "bootstrap-mappings" {
		set, err := contractcatalog.Load(root)
		if err != nil {
			fatal(err)
		}
		if err := bootstrapMappings(root, set); err != nil {
			fatal(err)
		}
		return
	}
	set, err := contractcatalog.Validate(root)
	if err != nil {
		fatal(err)
	}
	infos, err := gosurface.Scan(root, filepath.Join(root, "configs/cge/contracts/go-surfaces.yaml"))
	if err != nil {
		fatal(err)
	}
	implemented := make(map[string]bool, len(infos))
	for _, info := range infos {
		implemented[info.Package+"/"+info.Name] = true
	}
	for _, contract := range set.Catalog.Contracts {
		if contract.Implementation.Kind == "external" {
			continue
		}
		key := contract.Implementation.Package + "/" + contract.Implementation.Type
		if !implemented[key] {
			fatal(fmt.Errorf("contract %s implementation %s is not in the monitored Go surface", contract.ID, key))
		}
	}
	data, err := render(set)
	if err != nil {
		fatal(err)
	}
	target := filepath.Join(root, generatedPath)
	inventory, err := gosurface.BuildInventory(root, filepath.Join(root, "configs/cge/contracts/go-surfaces.yaml"))
	if err != nil {
		fatal(err)
	}
	if err := validateMappingsAgainstInventory(set, inventory); err != nil {
		fatal(err)
	}
	if command == "freeze-baseline" {
		if _, err := os.Stat(filepath.Join(root, baselinePath)); err == nil {
			fatal(fmt.Errorf("baseline already exists: refusing to overwrite"))
		} else if !os.IsNotExist(err) {
			fatal(err)
		}
	}
	inventoryBytes, err := yaml.Marshal(inventory)
	if err != nil {
		fatal(err)
	}
	if command == "coverage" {
		if err := writeCoverage(os.Stdout, set, inventory, infos); err != nil {
			fatal(err)
		}
		return
	}
	if command == "check-compat" {
		if err := checkCompatibility(root, set); err != nil {
			fatal(err)
		}
		return
	}
	if command == "check" {
		current, err := os.ReadFile(target)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", generatedPath, err))
		}
		if string(current) != string(data) {
			fatal(fmt.Errorf("%s: %s", generatedPath, contractcatalog.ErrGeneratedRegistryStale))
		}
		inventoryCurrent, err := os.ReadFile(filepath.Join(root, inventoryPath))
		if err != nil || string(inventoryCurrent) != string(inventoryBytes) {
			fatal(fmt.Errorf("%s: %s", inventoryPath, contractcatalog.ErrGeneratedRegistryStale))
		}
		return
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, inventoryPath), inventoryBytes, 0o644); err != nil {
		fatal(err)
	}
	if command == "freeze-baseline" {
		if err := writeBaseline(root, set); err != nil {
			fatal(err)
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func bootstrapMappings(root string, set contractcatalog.CatalogSet) error {
	explicit, err := gosurface.Scan(root, filepath.Join(root, "configs/cge/contracts/go-surfaces.yaml"))
	if err != nil {
		return err
	}
	inventory, err := gosurface.BuildInventory(root, filepath.Join(root, "configs/cge/contracts/go-surfaces.yaml"))
	if err != nil {
		return err
	}
	contractByType := map[string]string{}
	for _, item := range set.Catalog.Contracts {
		if item.Implementation.Package != "" && item.Implementation.Type != "" {
			contractByType[item.Implementation.Package+"/"+item.Implementation.Type] = item.ID
		}
	}
	for _, item := range set.JournalKinds.Kinds {
		contractByType[item.GoPackage+"/"+item.GoType] = item.Contract
	}
	byType := map[string]gosurface.InventoryType{}
	for _, item := range inventory.Types {
		byType[item.Package+"/"+item.Name] = item
	}
	seen := map[string]bool{}
	result := contractcatalog.FieldMappingsFile{SchemaVersion: 1, Namespace: "synora.cge"}
	for _, info := range explicit {
		key := info.Package + "/" + info.Name
		item, ok := byType[key]
		if !ok {
			return fmt.Errorf("explicit surface %s missing from inventory", key)
		}
		if contractID := contractByType[key]; contractID != "" {
			result.Mappings = append(result.Mappings, typeMappingFromInventory(contractID, item))
		} else {
			result.Exemptions = append(result.Exemptions, exemptionsForType(item, "explicit surface is not a serialized CGE contract")...)
		}
		seen[key] = true
	}
	for _, item := range inventory.Types {
		key := item.Package + "/" + item.Name
		if !seen[key] {
			result.Exemptions = append(result.Exemptions, exemptionsForType(item, "discovered helper is outside the CGE contract surface")...)
		}
	}
	data, err := yaml.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "configs/cge/contracts/field-mappings.yaml"), data, 0o644)
}

func typeMappingFromInventory(contractID string, item gosurface.InventoryType) contractcatalog.TypeMapping {
	mapping := contractcatalog.TypeMapping{Contract: contractID, Package: item.Package, Type: item.Name}
	for _, field := range item.Fields {
		mapping.Fields = append(mapping.Fields, contractcatalog.FieldMapping{
			GoField: field.GoField, FieldPath: field.FieldPath, WireName: field.WireName,
			GoType: field.GoType, WireType: field.WireType, Required: field.Required,
			Nullable: field.Nullable, Omitempty: field.Omitempty, Sensitivity: field.Sensitivity,
			Protection: field.Protection, Persistence: field.Persistence, Retention: field.Retention,
			IdentifierSemantic: field.IdentifierSemantic, TimestampSemantic: field.TimestampSemantic,
		})
	}
	return mapping
}

func exemptionsForType(item gosurface.InventoryType, reason string) []contractcatalog.MappingExemption {
	result := make([]contractcatalog.MappingExemption, 0, len(item.Fields)+1)
	if len(item.Fields) == 0 {
		return []contractcatalog.MappingExemption{{Package: item.Package, Type: item.Name, Reason: reason, Scope: "non_contract_surface", PersistenceAllowed: false, PublicOutputAllowed: false}}
	}
	for _, field := range item.Fields {
		result = append(result, contractcatalog.MappingExemption{Package: item.Package, Type: item.Name, Field: field.GoField, Reason: reason, Scope: "non_contract_surface", PersistenceAllowed: false, PublicOutputAllowed: false})
	}
	return result
}

type canonicalSet struct {
	Catalog       contractcatalog.CatalogFile
	Boundaries    contractcatalog.BoundariesFile
	Stores        contractcatalog.StoresFile
	Errors        contractcatalog.ErrorsFile
	Identifiers   contractcatalog.IdentifiersFile
	Timestamps    contractcatalog.TimestampsFile
	Transports    contractcatalog.TransportsFile
	Writers       contractcatalog.WritersFile
	JournalKinds  contractcatalog.JournalKindsFile
	FieldMappings contractcatalog.FieldMappingsFile
}

func render(set contractcatalog.CatalogSet) ([]byte, error) {
	contracts := append([]contractcatalog.CatalogContract(nil), set.Catalog.Contracts...)
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].ID < contracts[j].ID })
	boundaries := append([]contractcatalog.CatalogBoundary(nil), set.Boundaries.Boundaries...)
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].ID < boundaries[j].ID })
	stores := append([]contractcatalog.CatalogStore(nil), set.Stores.Stores...)
	sort.Slice(stores, func(i, j int) bool { return stores[i].ID < stores[j].ID })
	errors := append([]contractcatalog.CatalogError(nil), set.Errors.Errors...)
	sort.Slice(errors, func(i, j int) bool { return errors[i].ID < errors[j].ID })
	canonical := canonicalize(set)
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	global := sha256.Sum256(canonicalBytes)

	var b strings.Builder
	b.WriteString("// Code generated by cge-contractgen. DO NOT EDIT.\n\npackage contractcatalog\n\n")
	fmt.Fprintf(&b, "var generatedRegistry = Registry{\n\tCatalogFingerprint: %q,\n", "sha256:"+hex.EncodeToString(global[:]))
	b.WriteString("\tContracts: map[string]Descriptor{\n")
	for _, c := range contracts {
		registryContract := contractWithMappings(c, set.FieldMappings)
		hash := contractHash(registryContract)
		fmt.Fprintf(&b, "\t\t%q: {ID: %q, Version: %q, Category: %q, Owner: %q, Authority: Authority(%q), Trust: %q, Sensitivity: %q, Status: %q, Persistence: %s, Fields: %s, Implementation: %s, SchemaHash: %q},\n", c.ID, c.ID, version(c.ID), c.Category, c.Owner, c.Authority, c.Trust, c.Sensitivity, c.Status, stringSlice(c.Persistence), fieldSlice(registryContract.Fields), implementation(c.Implementation), hash)
	}
	b.WriteString("\t},\n\tBoundaries: map[string]BoundaryDescriptor{\n")
	for _, value := range boundaries {
		fmt.Fprintf(&b, "\t\t%q: {ID: %q, InputContracts: %s, OutputContracts: %s, Persistence: %s, Authority: Authority(%q)},\n", value.ID, value.ID, stringSlice(value.InputContracts), stringSlice(value.OutputContracts), stringSlice(value.Persistence), value.Authority)
	}
	b.WriteString("\t},\n\tStores: map[string]StoreDescriptor{\n")
	for _, value := range stores {
		fmt.Fprintf(&b, "\t\t%q: {ID: %q, Authority: Authority(%q), Sensitivity: %q, ContractRefs: %s, Durable: %t, ClearSecretAllowed: %t, ClearBiometricAllowed: %t},\n", value.ID, value.ID, value.Authority, value.Sensitivity, stringSlice(value.ContractRefs), value.Durable, value.ClearSecretAllowed, value.ClearBiometricAllowed)
	}
	b.WriteString("\t},\n\tErrors: map[string]ErrorDescriptor{\n")
	for _, value := range errors {
		fmt.Fprintf(&b, "\t\t%q: {ID: %q, Owner: %q, Category: %q, Retryable: %t, Public: %t, Authority: Authority(%q)},\n", value.ID, value.ID, value.Owner, value.Category, value.Retryable, value.Public, value.Authority)
	}
	b.WriteString("\t},\n\tIdentifiers: map[string]IdentifierSpec{\n")
	for _, value := range canonical.Identifiers.Identifiers {
		fmt.Fprintf(&b, "\t\t%q: %s,\n", value.ID, identifierLiteral(value))
	}
	b.WriteString("\t},\n\tTimestamps: map[string]TimestampSpec{\n")
	for _, value := range canonical.Timestamps.Timestamps {
		fmt.Fprintf(&b, "\t\t%q: %s,\n", value.ID, timestampLiteral(value))
	}
	b.WriteString("\t},\n\tTransports: map[string]TransportSpec{\n")
	for _, value := range canonical.Transports.Transports {
		fmt.Fprintf(&b, "\t\t%q: %s,\n", value.ID, transportLiteral(value))
	}
	b.WriteString("\t},\n\tContractHashes: map[string]string{\n")
	for _, value := range contracts {
		fmt.Fprintf(&b, "\t\t%q: %q,\n", value.ID, contractHash(contractWithMappings(value, set.FieldMappings)))
	}
	b.WriteString("\t},\n\tWriters: map[string]WriterDescriptor{\n")
	for _, value := range canonical.Writers.Writers {
		fmt.Fprintf(&b, "\t\t%q: {ID: %q, Owner: %q, Package: %q, Type: %q, Function: %q, Store: %q, Contract: %q, ContractResolutionMode: %q, ContractResolutionField: %q, ContractResolutionRegistry: %q, Guard: %q, Format: %q, BeforeWrite: %q},\n", value.ID, value.ID, value.Owner, value.Package, value.Type, value.Function, value.Store, value.Contract, value.ContractResolution.Mode, value.ContractResolution.Field, value.ContractResolution.Registry, value.Guard, value.Format, value.BeforeWrite)
	}
	b.WriteString("\t},\n\tJournalKinds: map[string]JournalKindDescriptor{\n")
	for _, value := range canonical.JournalKinds.Kinds {
		fmt.Fprintf(&b, "\t\t%q: {Kind: %q, GoPackage: %q, GoType: %q, Contract: %q, Validator: %q, LegacyRead: %t},\n", value.Kind, value.Kind, value.GoPackage, value.GoType, value.Contract, value.Validator, value.LegacyRead)
	}
	b.WriteString("\t},\n}\n")
	return format.Source([]byte(b.String()))
}

func canonicalize(set contractcatalog.CatalogSet) canonicalSet {
	contracts := append([]contractcatalog.CatalogContract(nil), set.Catalog.Contracts...)
	boundaries := append([]contractcatalog.CatalogBoundary(nil), set.Boundaries.Boundaries...)
	stores := append([]contractcatalog.CatalogStore(nil), set.Stores.Stores...)
	errors := append([]contractcatalog.CatalogError(nil), set.Errors.Errors...)
	identifiers := append([]contractcatalog.IdentifierSpec(nil), set.Identifiers.Identifiers...)
	timestamps := append([]contractcatalog.TimestampSpec(nil), set.Timestamps.Timestamps...)
	transports := append([]contractcatalog.TransportSpec(nil), set.Transports.Transports...)
	writers := append([]contractcatalog.WriterSpec(nil), set.Writers.Writers...)
	kinds := append([]contractcatalog.JournalKindSpec(nil), set.JournalKinds.Kinds...)
	mappings := append([]contractcatalog.TypeMapping(nil), set.FieldMappings.Mappings...)
	exemptions := append([]contractcatalog.MappingExemption(nil), set.FieldMappings.Exemptions...)
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].ID < contracts[j].ID })
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].ID < boundaries[j].ID })
	sort.Slice(stores, func(i, j int) bool { return stores[i].ID < stores[j].ID })
	sort.Slice(errors, func(i, j int) bool { return errors[i].ID < errors[j].ID })
	sort.Slice(identifiers, func(i, j int) bool { return identifiers[i].ID < identifiers[j].ID })
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i].ID < timestamps[j].ID })
	sort.Slice(transports, func(i, j int) bool { return transports[i].ID < transports[j].ID })
	sort.Slice(writers, func(i, j int) bool { return writers[i].ID < writers[j].ID })
	sort.Slice(kinds, func(i, j int) bool { return kinds[i].Kind < kinds[j].Kind })
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].Contract < mappings[j].Contract })
	sort.Slice(exemptions, func(i, j int) bool {
		if exemptions[i].Package != exemptions[j].Package {
			return exemptions[i].Package < exemptions[j].Package
		}
		if exemptions[i].Type != exemptions[j].Type {
			return exemptions[i].Type < exemptions[j].Type
		}
		return exemptions[i].Field < exemptions[j].Field
	})
	return canonicalSet{
		Catalog:       contractcatalog.CatalogFile{Catalog: set.Catalog.Catalog, Contracts: contracts, AdmissionEvents: set.Catalog.AdmissionEvents, OutputProfiles: set.Catalog.OutputProfiles},
		Boundaries:    contractcatalog.BoundariesFile{SchemaVersion: set.Boundaries.SchemaVersion, Namespace: set.Boundaries.Namespace, Boundaries: boundaries},
		Stores:        contractcatalog.StoresFile{SchemaVersion: set.Stores.SchemaVersion, Namespace: set.Stores.Namespace, Stores: stores},
		Errors:        contractcatalog.ErrorsFile{SchemaVersion: set.Errors.SchemaVersion, Namespace: set.Errors.Namespace, Errors: errors},
		Identifiers:   contractcatalog.IdentifiersFile{SchemaVersion: set.Identifiers.SchemaVersion, Namespace: set.Identifiers.Namespace, Identifiers: identifiers},
		Timestamps:    contractcatalog.TimestampsFile{SchemaVersion: set.Timestamps.SchemaVersion, Namespace: set.Timestamps.Namespace, Timestamps: timestamps},
		Transports:    contractcatalog.TransportsFile{SchemaVersion: set.Transports.SchemaVersion, Namespace: set.Transports.Namespace, Transports: transports},
		Writers:       contractcatalog.WritersFile{SchemaVersion: set.Writers.SchemaVersion, Namespace: set.Writers.Namespace, Writers: writers},
		JournalKinds:  contractcatalog.JournalKindsFile{SchemaVersion: set.JournalKinds.SchemaVersion, Namespace: set.JournalKinds.Namespace, Kinds: kinds},
		FieldMappings: contractcatalog.FieldMappingsFile{SchemaVersion: set.FieldMappings.SchemaVersion, Namespace: set.FieldMappings.Namespace, Mappings: mappings, Exemptions: exemptions},
	}
}

func version(id string) string {
	index := strings.LastIndex(id, ".v")
	if index < 0 {
		return ""
	}
	return id[index+1:]
}

func contractHash(value contractcatalog.CatalogContract) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func contractWithMappings(contract contractcatalog.CatalogContract, mappings contractcatalog.FieldMappingsFile) contractcatalog.CatalogContract {
	if len(contract.Fields) != 0 {
		return contract
	}
	for _, mapping := range mappings.Mappings {
		if mapping.Contract != contract.ID {
			continue
		}
		for _, field := range mapping.Fields {
			omitempty := field.Omitempty
			contract.Fields = append(contract.Fields, contractcatalog.Field{
				Name: field.WireName, GoField: field.GoField, FieldPath: field.FieldPath,
				Type: field.WireType, GoType: field.GoType, WireType: field.WireType,
				WireName: field.WireName, Required: field.Required, Nullable: field.Nullable,
				Omitempty: &omitempty, Sensitivity: field.Sensitivity, Protection: field.Protection,
				Persistence: field.Persistence, Retention: field.Retention,
				Identifier: field.IdentifierSemantic, Timestamp: field.TimestampSemantic,
			})
		}
		break
	}
	return contract
}

func stringSlice(values []string) string {
	if len(values) == 0 {
		return "nil"
	}
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	parts := make([]string, len(copyValues))
	for i, value := range copyValues {
		parts[i] = strconv.Quote(value)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

func fieldSlice(values []contractcatalog.Field) string {
	if len(values) == 0 {
		return "nil"
	}
	copyValues := append([]contractcatalog.Field(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i].Name < copyValues[j].Name })
	parts := make([]string, len(copyValues))
	for i, value := range copyValues {
		wirePath := value.WireName
		if value.FieldPath != "" {
			wirePath = value.FieldPath
		}
		parts[i] = fmt.Sprintf("{Name: %q, GoField: %q, FieldPath: %q, Type: %q, GoType: %q, WireType: %q, WireName: %q, WirePath: %q, Required: %t, Nullable: %t, Protection: %q, Sensitivity: %q, Persistence: %s, Identifier: %q, Timestamp: %q}", value.Name, value.GoField, value.FieldPath, value.Type, value.GoType, value.WireType, value.WireName, wirePath, value.Required, value.Nullable, value.Protection, value.Sensitivity, stringSlice(value.Persistence), value.Identifier, value.Timestamp)
	}
	return "[]FieldDescriptor{" + strings.Join(parts, ", ") + "}"
}

func implementation(value contractcatalog.Implementation) string {
	return fmt.Sprintf("ImplementationDescriptor{Kind: %q, Package: %q, Type: %q, WireFormat: %q, Validator: %q, Justification: %q}", value.Kind, value.Package, value.Type, value.WireFormat, value.Validator, value.Justification)
}

func identifierLiteral(value contractcatalog.IdentifierSpec) string {
	return fmt.Sprintf("IdentifierSpec{ID:%q, Semantic:%q, Owner:%q, Generator:%q, Scope:%q, Uniqueness:%q, WireType:%q, Protection:%q, Domain:%q, Nullable:%t, Deduplication:%q, Correlation:%q, Ordering:%q, Persistence:%s, RestartStability:%q, LegacyBehavior:%q}", value.ID, value.Semantic, value.Owner, value.Generator, value.Scope, value.Uniqueness, value.WireType, value.Protection, value.Domain, value.Nullable, value.Deduplication, value.Correlation, value.Ordering, stringSlice(value.Persistence), value.RestartStability, value.LegacyBehavior)
}

func timestampLiteral(value contractcatalog.TimestampSpec) string {
	return fmt.Sprintf("TimestampSpec{ID:%q, Semantic:%q, Producer:%q, Clock:%q, Timezone:%q, Required:%t, Ordering:%q, SourceSupplied:%t, FutureAllowed:%t, MaximumFutureSkew:%q, MaximumPastAge:%q, Persistence:%s, UsedForReasoning:%t, UsedForAudit:%t, Fallback:%q}", value.ID, value.Semantic, value.Producer, value.Clock, value.Timezone, value.Required, value.Ordering, value.SourceSupplied, value.FutureAllowed, value.MaximumFutureSkew, value.MaximumPastAge, stringSlice(value.Persistence), value.UsedForReasoning, value.UsedForAudit, value.Fallback)
}

func transportLiteral(value contractcatalog.TransportSpec) string {
	return fmt.Sprintf("TransportSpec{ID:%q, Transport:%q, Direction:%q, Owner:%q, Method:%q, Path:%q, RequestContract:%q, ResponseContract:%q, ErrorContract:%q, Version:%q, Authorization:%q, Redaction:%q, Pagination:%q, Bounded:%t, Authority:%q}", value.ID, value.Transport, value.Direction, value.Owner, value.Method, value.Path, value.RequestContract, value.ResponseContract, value.ErrorContract, value.Version, value.Authorization, value.Redaction, value.Pagination, value.Bounded, value.Authority)
}

type baselineFile struct {
	SchemaVersion int          `json:"schema_version"`
	Namespace     string       `json:"namespace"`
	Fingerprint   string       `json:"fingerprint"`
	Catalog       canonicalSet `json:"catalog"`
}

func catalogFingerprint(set contractcatalog.CatalogSet) string {
	data, _ := json.Marshal(canonicalize(set))
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeBaseline(root string, set contractcatalog.CatalogSet) error {
	canonical := canonicalize(set)
	baseline := baselineFile{SchemaVersion: 1, Namespace: "synora.cge", Fingerprint: catalogFingerprint(set), Catalog: canonical}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(root, baselinePath)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("baseline already exists: refusing to overwrite")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func checkCompatibility(root string, set contractcatalog.CatalogSet) error {
	data, err := os.ReadFile(filepath.Join(root, baselinePath))
	if err != nil {
		return fmt.Errorf("baseline v1 missing: %w", err)
	}
	var baseline baselineFile
	if err := json.Unmarshal(data, &baseline); err != nil {
		return fmt.Errorf("baseline v1 invalid: %w", err)
	}
	if baseline.SchemaVersion != 1 || baseline.Namespace != "synora.cge" || baseline.Fingerprint == "" {
		return fmt.Errorf("baseline v1 header or fingerprint is invalid")
	}
	classification, changes := classifyCompatibility(baseline.Catalog, canonicalize(set))
	if _, err := fmt.Fprintf(os.Stdout, "classification=%s\n", classification); err != nil {
		return err
	}
	for _, change := range changes {
		if _, err := fmt.Fprintln(os.Stdout, change); err != nil {
			return err
		}
	}
	return nil
}

func classifyCompatibility(previous, current canonicalSet) (string, []string) {
	classification := "compatible"
	changes := make([]string, 0)
	contracts := func(values []contractcatalog.CatalogContract) map[string]contractcatalog.CatalogContract {
		result := make(map[string]contractcatalog.CatalogContract, len(values))
		for _, value := range values {
			result[value.ID] = value
		}
		return result
	}
	previousContracts, currentContracts := contracts(previous.Catalog.Contracts), contracts(current.Catalog.Contracts)
	for id, oldContract := range previousContracts {
		newContract, ok := currentContracts[id]
		if !ok {
			level := "migration_required"
			if oldContract.Status == "stable" {
				level = "breaking"
			}
			changes = append(changes, level+": contract_removed="+id)
			if level == "breaking" {
				classification = "breaking"
			} else if classification == "compatible" {
				classification = level
			}
			continue
		}
		if oldContract.Authority != newContract.Authority && authorityRank(newContract.Authority) > authorityRank(oldContract.Authority) {
			changes = append(changes, "breaking: authority_increased="+id)
			classification = "breaking"
		}
		if oldContract.Implementation.Package != newContract.Implementation.Package || oldContract.Implementation.Type != newContract.Implementation.Type || oldContract.Implementation.WireFormat != newContract.Implementation.WireFormat {
			changes = append(changes, "breaking: implementation_changed="+id)
			classification = "breaking"
		}
		if oldContract.Persistence != nil && !equalStrings(oldContract.Persistence, newContract.Persistence) {
			changes = append(changes, "migration_required: persistence_changed="+id)
			if classification == "compatible" {
				classification = "migration_required"
			}
		}
		if oldContract.Status == "stable" && !equalFields(oldContract.Fields, newContract.Fields) {
			changes = append(changes, "breaking: stable_fields_changed="+id)
			classification = "breaking"
		}
	}
	for id, newContract := range currentContracts {
		if _, ok := previousContracts[id]; ok {
			continue
		}
		level := "compatible"
		if len(newContract.Persistence) > 0 {
			level = "migration_required"
		}
		changes = append(changes, level+": contract_added="+id)
		if level == "migration_required" && classification == "compatible" {
			classification = level
		}
	}
	previousStores := map[string]bool{}
	for _, store := range previous.Stores.Stores {
		previousStores[store.ID] = true
	}
	for _, store := range current.Stores.Stores {
		if !previousStores[store.ID] {
			changes = append(changes, "migration_required: store_added="+store.ID)
			if classification == "compatible" {
				classification = "migration_required"
			}
		}
	}
	previousWriters := map[string]contractcatalog.WriterSpec{}
	for _, writer := range previous.Writers.Writers {
		previousWriters[writer.ID] = writer
	}
	for _, writer := range current.Writers.Writers {
		old, ok := previousWriters[writer.ID]
		if ok && old.Contract == "synora.cge.audit-record.v1" && writer.Contract != old.Contract {
			changes = append(changes, "migration_required: writer_exact_contract="+writer.ID)
			if classification == "compatible" {
				classification = "migration_required"
			}
		}
	}
	sort.Strings(changes)
	return classification, changes
}

func authorityRank(value string) int {
	switch value {
	case "descriptive":
		return 1
	case "diagnostic":
		return 2
	case "advisory":
		return 3
	case "authorized_decision":
		return 4
	case "authorized_action":
		return 5
	}
	return 0
}

func equalStrings(left, right []string) bool {
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func equalFields(left, right []contractcatalog.Field) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

type coverageReport struct {
	ContractsTotal               int      `json:"contracts_total"`
	ContractsGoBound             int      `json:"contracts_go_bound"`
	ContractsExternal            int      `json:"contracts_external"`
	TypesMonitored               int      `json:"types_monitored"`
	TypesCatalogued              int      `json:"types_catalogued"`
	FieldsGoTotal                int      `json:"fields_go_total"`
	FieldsCatalogued             int      `json:"fields_catalogued"`
	WireFieldsTotal              int      `json:"wire_fields_total"`
	WireFieldsCatalogued         int      `json:"wire_fields_catalogued"`
	PersistentPayloadsTotal      int      `json:"persistent_payloads_total"`
	PersistentPayloadsTyped      int      `json:"persistent_payloads_typed"`
	OpaqueDurableEnvelopes       int      `json:"opaque_durable_envelopes"`
	WritersTotal                 int      `json:"writers_total"`
	WritersGuarded               int      `json:"writers_guarded"`
	OutputsTotal                 int      `json:"outputs_total"`
	OutputsCatalogued            int      `json:"outputs_catalogued"`
	TransportsTotal              int      `json:"transports_total"`
	TransportsCatalogued         int      `json:"transports_catalogued"`
	IdentifiersTotal             int      `json:"identifiers_total"`
	IdentifiersCatalogued        int      `json:"identifiers_catalogued"`
	TimestampsTotal              int      `json:"timestamps_total"`
	TimestampsCatalogued         int      `json:"timestamps_catalogued"`
	CriticalGaps                 int      `json:"critical_gaps"`
	HighContractGaps             int      `json:"high_contract_gaps"`
	FieldMappingCoverage         int      `json:"field_mapping_coverage"`
	WireFieldCoverage            int      `json:"wire_field_coverage"`
	PersistentContractCoverage   int      `json:"persistent_contract_coverage"`
	RuntimeOutputCoverage        int      `json:"runtime_output_coverage"`
	TransportSurfaceCoverage     int      `json:"transport_surface_coverage"`
	IdentifierSemanticsCoverage  int      `json:"identifier_semantics_coverage"`
	TimestampSemanticsCoverage   int      `json:"timestamp_semantics_coverage"`
	DurableWriterCoverage        int      `json:"durable_writer_coverage"`
	UncataloguedDurableMaps      int      `json:"uncatalogued_durable_maps"`
	DiscoveredTypesTotal         int      `json:"discovered_types_total"`
	InScopeTypesTotal            int      `json:"in_scope_types_total"`
	ExplicitlyMappedTypes        int      `json:"explicitly_mapped_types"`
	ExplicitlyExemptedTypes      int      `json:"explicitly_exempted_types"`
	UnreviewedTypes              int      `json:"unreviewed_types"`
	DiscoveredFieldsTotal        int      `json:"discovered_fields_total"`
	InScopeFieldsTotal           int      `json:"in_scope_fields_total"`
	ExplicitlyMappedFields       int      `json:"explicitly_mapped_fields"`
	ExplicitlyExemptedFields     int      `json:"explicitly_exempted_fields"`
	UnreviewedFields             int      `json:"unreviewed_fields"`
	PersistentPayloadsExact      int      `json:"persistent_payloads_with_exact_contract"`
	PersistentPayloadsGeneric    int      `json:"persistent_payloads_using_generic_contract"`
	IdentifierFieldsTotal        int      `json:"identifier_fields_total"`
	IdentifierFieldsExplicit     int      `json:"identifier_fields_with_explicit_semantic"`
	TimestampFieldsTotal         int      `json:"timestamp_fields_total"`
	TimestampFieldsExplicit      int      `json:"timestamp_fields_with_explicit_semantic"`
	WritersDiscovered            int      `json:"writers_discovered"`
	WritersCatalogued            int      `json:"writers_catalogued"`
	WritersWithExactContract     int      `json:"writers_with_exact_contract"`
	UnreviewedTypePaths          []string `json:"unreviewed_type_paths,omitempty"`
	UnreviewedFieldPaths         []string `json:"unreviewed_field_paths,omitempty"`
	GenericPersistentContractIDs []string `json:"generic_persistent_contract_ids,omitempty"`
}

func writeCoverage(w io.Writer, set contractcatalog.CatalogSet, inventory gosurface.Inventory, inScope []gosurface.TypeInfo) error {
	// Discovery is not approval. Coverage is a join between discovered types,
	// explicit mappings and explicit, safe exemptions.
	report := coverageReport{ContractsTotal: len(set.Catalog.Contracts), TypesMonitored: len(inventory.Types), OutputsTotal: len(set.Catalog.OutputProfiles), OutputsCatalogued: len(set.Catalog.OutputProfiles), TransportsTotal: len(set.Transports.Transports), TransportsCatalogued: len(set.Transports.Transports), IdentifiersTotal: len(set.Identifiers.Identifiers), IdentifiersCatalogued: len(set.Identifiers.Identifiers), TimestampsTotal: len(set.Timestamps.Timestamps), TimestampsCatalogued: len(set.Timestamps.Timestamps), WritersTotal: len(set.Writers.Writers), DiscoveredTypesTotal: len(inventory.Types), InScopeTypesTotal: len(inScope), WritersDiscovered: len(set.Writers.Writers), WritersCatalogued: len(set.Writers.Writers)}
	mappedTypes := map[string]bool{}
	mappedFields := map[string]bool{}
	for _, mapping := range set.FieldMappings.Mappings {
		key := mapping.Package + "/" + mapping.Type
		mappedTypes[key] = true
		for _, field := range mapping.Fields {
			mappedFields[key+"/"+field.FieldPath] = true
			if field.IdentifierSemantic != "not_an_identifier" && field.IdentifierSemantic != "" {
				report.IdentifierFieldsExplicit++
			}
			if field.TimestampSemantic != "not_a_timestamp" && field.TimestampSemantic != "" {
				report.TimestampFieldsExplicit++
			}
		}
	}
	exemptedTypes := map[string]bool{}
	exemptedFields := map[string]bool{}
	for _, exemption := range set.FieldMappings.Exemptions {
		key := exemption.Package + "/" + exemption.Type
		exemptedTypes[key] = true
		if exemption.Field != "" {
			exemptedFields[key+"/"+exemption.Field] = true
		}
	}
	for _, item := range inventory.Types {
		key := item.Package + "/" + item.Name
		if !mappedTypes[key] && !exemptedTypes[key] {
			report.UnreviewedTypePaths = append(report.UnreviewedTypePaths, key)
		}
		for _, field := range item.Fields {
			report.FieldsGoTotal++
			report.WireFieldsTotal++
			path := key + "/" + field.FieldPath
			if mappedFields[path] && field.IdentifierSemantic != "not_an_identifier" && field.IdentifierSemantic != "" {
				report.IdentifierFieldsTotal++
			}
			if mappedFields[path] && field.TimestampSemantic != "not_a_timestamp" && field.TimestampSemantic != "" {
				report.TimestampFieldsTotal++
			}
			if !mappedFields[path] && !exemptedFields[path] {
				report.UnreviewedFieldPaths = append(report.UnreviewedFieldPaths, path)
			}
		}
	}
	report.ExplicitlyMappedTypes = len(mappedTypes)
	report.ExplicitlyExemptedTypes = len(exemptedTypes)
	report.UnreviewedTypes = len(report.UnreviewedTypePaths)
	report.DiscoveredFieldsTotal = report.FieldsGoTotal
	report.InScopeFieldsTotal = inventoryFieldsFor(inventory, inScope)
	report.ExplicitlyMappedFields = len(mappedFields)
	report.ExplicitlyExemptedFields = len(exemptedFields)
	report.UnreviewedFields = len(report.UnreviewedFieldPaths)
	report.TypesCatalogued = report.ExplicitlyMappedTypes + report.ExplicitlyExemptedTypes
	report.FieldsCatalogued = report.ExplicitlyMappedFields + report.ExplicitlyExemptedFields
	report.WireFieldsCatalogued = report.FieldsCatalogued
	for _, writer := range set.Writers.Writers {
		if writer.Guard == "ValidateStoreWrite" {
			report.WritersGuarded++
		}
		if writer.Contract != "synora.cge.audit-record.v1" {
			report.WritersWithExactContract++
		}
	}
	for _, item := range set.Catalog.Contracts {
		if item.Implementation.Kind == "external" {
			report.ContractsExternal++
		} else {
			report.ContractsGoBound++
		}
		if len(item.Persistence) > 0 {
			report.PersistentPayloadsTotal++
			if item.Implementation.Kind != "envelope" {
				report.PersistentPayloadsTyped++
			} else {
				report.OpaqueDurableEnvelopes++
			}
		}
	}
	report.PersistentPayloadsExact = report.PersistentPayloadsTyped
	for _, writer := range set.Writers.Writers {
		if writer.Contract == "synora.cge.audit-record.v1" {
			report.PersistentPayloadsGeneric++
			report.GenericPersistentContractIDs = append(report.GenericPersistentContractIDs, writer.ID)
		}
	}
	if report.PersistentPayloadsTotal != report.PersistentPayloadsExact || report.OpaqueDurableEnvelopes != 0 || report.UnreviewedTypes != 0 || report.UnreviewedFields != 0 {
		report.CriticalGaps++
	}
	if report.PersistentPayloadsGeneric != 0 || report.WritersWithExactContract != report.WritersDiscovered || report.IdentifierFieldsExplicit != report.IdentifierFieldsTotal || report.TimestampFieldsExplicit != report.TimestampFieldsTotal {
		report.HighContractGaps++
	}
	report.FieldMappingCoverage = percentage(report.FieldsGoTotal, report.FieldsCatalogued)
	report.WireFieldCoverage = percentage(report.WireFieldsTotal, report.WireFieldsCatalogued)
	report.PersistentContractCoverage = percentage(report.PersistentPayloadsTotal, report.PersistentPayloadsTyped)
	report.RuntimeOutputCoverage = percentage(report.OutputsTotal, report.OutputsCatalogued)
	report.TransportSurfaceCoverage = percentage(report.TransportsTotal, report.TransportsCatalogued)
	report.IdentifierSemanticsCoverage = percentage(report.IdentifiersTotal, report.IdentifiersCatalogued)
	report.TimestampSemanticsCoverage = percentage(report.TimestampsTotal, report.TimestampsCatalogued)
	report.DurableWriterCoverage = percentage(report.WritersTotal, report.WritersGuarded)
	report.UncataloguedDurableMaps = report.OpaqueDurableEnvelopes
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	if err != nil {
		return err
	}
	if report.CriticalGaps != 0 || report.HighContractGaps != 0 || report.FieldMappingCoverage != 100 || report.WireFieldCoverage != 100 || report.PersistentContractCoverage != 100 || report.RuntimeOutputCoverage != 100 || report.TransportSurfaceCoverage != 100 || report.IdentifierSemanticsCoverage != 100 || report.TimestampSemanticsCoverage != 100 || report.DurableWriterCoverage != 100 || report.UncataloguedDurableMaps != 0 {
		return fmt.Errorf("coverage below mandatory v1 threshold")
	}
	return nil
}

func inventoryFieldsFor(inventory gosurface.Inventory, inScope []gosurface.TypeInfo) int {
	allowed := map[string]bool{}
	for _, item := range inScope {
		allowed[item.Package+"/"+item.Name] = true
	}
	total := 0
	for _, item := range inventory.Types {
		if allowed[item.Package+"/"+item.Name] {
			total += len(item.Fields)
		}
	}
	return total
}

// validateMappingsAgainstInventory keeps the approval source independent from
// the discovery inventory while still requiring that every approved mapping is
// a faithful description of the discovered Go surface.
func validateMappingsAgainstInventory(set contractcatalog.CatalogSet, inventory gosurface.Inventory) error {
	byType := make(map[string]gosurface.InventoryType, len(inventory.Types))
	for _, item := range inventory.Types {
		byType[item.Package+"/"+item.Name] = item
	}
	for _, mapping := range set.FieldMappings.Mappings {
		key := mapping.Package + "/" + mapping.Type
		item, ok := byType[key]
		if !ok {
			return fmt.Errorf("field mapping %s targets undiscovered type %s", mapping.Contract, key)
		}
		actual := make(map[string]gosurface.InventoryField, len(item.Fields))
		for _, field := range item.Fields {
			actual[field.FieldPath] = field
		}
		seen := make(map[string]bool, len(mapping.Fields))
		for _, field := range mapping.Fields {
			if seen[field.FieldPath] {
				return fmt.Errorf("field mapping %s duplicates %s", mapping.Contract, field.FieldPath)
			}
			seen[field.FieldPath] = true
			actualField, ok := actual[field.FieldPath]
			if !ok {
				return fmt.Errorf("field mapping %s.%s is absent from Go surface", mapping.Contract, field.FieldPath)
			}
			if field.GoField != actualField.GoField || field.WireName != actualField.WireName || field.GoType != actualField.GoType || field.WireType != actualField.WireType || field.Required != actualField.Required || field.Omitempty != actualField.Omitempty || field.Nullable != actualField.Nullable {
				return fmt.Errorf("field mapping %s.%s does not match Go surface", mapping.Contract, field.FieldPath)
			}
		}
		if len(seen) != len(actual) {
			return fmt.Errorf("field mapping %s does not cover every Go field", mapping.Contract)
		}
	}
	return nil
}

func percentage(total, covered int) int {
	if total == 0 {
		return 100
	}
	if covered != total {
		return 0
	}
	return 100
}
