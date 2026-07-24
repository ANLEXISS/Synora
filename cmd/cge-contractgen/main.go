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
	"synora/internal/cge/contractcatalog/discovery"
	"synora/internal/cge/contractcatalog/gosurface"
)

const generatedPath = "internal/cge/contractcatalog/generated_registry.go"
const inventoryPath = "configs/cge/contracts/surface-inventory.yaml"
const baselinePath = "configs/cge/contracts/baselines/cge-contract-set-v1.json"
const baselineV2Path = "configs/cge/contracts/baselines/cge-contract-set-v2.json"
const migrationV1V2Path = "configs/cge/contracts/migrations/contract-set-v1-to-v2.yaml"

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "generate" && os.Args[1] != "check" && os.Args[1] != "check-compat" && os.Args[1] != "coverage" && os.Args[1] != "freeze-baseline" && os.Args[1] != "freeze-baseline-v2" && os.Args[1] != "bootstrap-mappings" && os.Args[1] != "scaffold-mappings") {
		fmt.Fprintln(os.Stderr, "usage: cge-contractgen generate|check|check-compat [--baseline v1|v2]|coverage|freeze-baseline|freeze-baseline-v2|scaffold-mappings")
		os.Exit(2)
	}
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	command := os.Args[1]
	if command != "check-compat" && len(os.Args) != 2 {
		fatal(fmt.Errorf("command %s does not accept arguments", command))
	}
	if command == "bootstrap-mappings" || command == "scaffold-mappings" {
		set, err := contractcatalog.Load(root)
		if err != nil {
			fatal(err)
		}
		if err := scaffoldMappings(root, set); err != nil {
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
	if command == "freeze-baseline" || command == "freeze-baseline-v2" {
		if _, err := os.Stat(filepath.Join(root, baselinePath)); err == nil {
			if command == "freeze-baseline" {
				fatal(fmt.Errorf("baseline already exists: refusing to overwrite"))
			}
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
		baselineVersion := "v1"
		if len(os.Args) == 4 && os.Args[2] == "--baseline" && (os.Args[3] == "v1" || os.Args[3] == "v2") {
			baselineVersion = os.Args[3]
		} else if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: check-compat [--baseline v1|v2]")
			os.Exit(2)
		}
		if err := checkCompatibilityAt(root, set, baselineVersion); err != nil {
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
	if command == "freeze-baseline" || command == "freeze-baseline-v2" {
		path := baselinePath
		if command == "freeze-baseline-v2" {
			path = baselineV2Path
			if err := validateMigration(root); err != nil {
				fatal(err)
			}
		}
		if err := writeBaselineAt(root, set, path); err != nil {
			fatal(err)
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func scaffoldMappings(root string, set contractcatalog.CatalogSet) error {
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
	proposal := struct {
		ReviewStatus string                             `yaml:"review_status"`
		Mappings     []contractcatalog.TypeMapping      `yaml:"mappings"`
		Exemptions   []contractcatalog.MappingExemption `yaml:"exemptions"`
	}{ReviewStatus: "pending", Mappings: result.Mappings, Exemptions: result.Exemptions}
	data, err := yaml.Marshal(proposal)
	if err != nil {
		return err
	}
	return os.WriteFile("/tmp/cge-field-mapping-proposal.yaml", data, 0o600)
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
		return []contractcatalog.MappingExemption{{Package: item.Package, Type: item.Name, Reason: reason, Scope: "non_contract_surface", ReviewStatus: "pending", PersistenceAllowed: false, PublicOutputAllowed: false}}
	}
	for _, field := range item.Fields {
		result = append(result, contractcatalog.MappingExemption{Package: item.Package, Type: item.Name, Field: field.GoField, Reason: reason, Scope: "non_contract_surface", ReviewStatus: "pending", PersistenceAllowed: false, PublicOutputAllowed: false})
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
	return writeBaselineAt(root, set, baselinePath)
}

func writeBaselineAt(root string, set contractcatalog.CatalogSet, relativePath string) error {
	canonical := canonicalize(set)
	baseline := baselineFile{SchemaVersion: 1, Namespace: "synora.cge", Fingerprint: catalogFingerprint(set), Catalog: canonical}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(root, relativePath)
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
	return checkCompatibilityAt(root, set, "v1")
}

func checkCompatibilityAt(root string, set contractcatalog.CatalogSet, baselineVersion string) error {
	relativePath := baselinePath
	if baselineVersion == "v2" {
		relativePath = baselineV2Path
	}
	data, err := os.ReadFile(filepath.Join(root, relativePath))
	if err != nil {
		return fmt.Errorf("baseline %s missing: %w", baselineVersion, err)
	}
	var baseline baselineFile
	if err := json.Unmarshal(data, &baseline); err != nil {
		return fmt.Errorf("baseline %s invalid: %w", baselineVersion, err)
	}
	if baseline.SchemaVersion != 1 || baseline.Namespace != "synora.cge" || baseline.Fingerprint == "" {
		return fmt.Errorf("baseline %s header or fingerprint is invalid", baselineVersion)
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
	if classification == "migration_required" && baselineVersion == "v1" {
		if err := validateMigration(root); err != nil {
			return err
		}
	}
	if classification == "breaking" {
		return fmt.Errorf("compatibility classification is breaking")
	}
	return nil
}

type migrationDocument struct {
	SchemaVersion  int      `yaml:"schema_version"`
	Namespace      string   `yaml:"namespace"`
	SourceBaseline string   `yaml:"source_baseline"`
	TargetBaseline string   `yaml:"target_baseline"`
	Classification string   `yaml:"classification"`
	Approved       bool     `yaml:"approved"`
	DurableRewrite bool     `yaml:"durable_rewrite_required"`
	LegacyReplay   string   `yaml:"legacy_replay"`
	BytesUnchanged bool     `yaml:"bytes_unchanged"`
	Changes        []string `yaml:"changes"`
	Tests          []string `yaml:"tests"`
}

func validateMigration(root string) error {
	data, err := os.ReadFile(filepath.Join(root, migrationV1V2Path))
	if err != nil {
		return fmt.Errorf("migration v1 to v2 missing: %w", err)
	}
	var document migrationDocument
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("migration v1 to v2 invalid: %w", err)
	}
	if document.SchemaVersion != 1 || document.Namespace != "synora.cge" || document.SourceBaseline != "cge-contract-set-v1.json" || document.TargetBaseline != "cge-contract-set-v2.json" || document.Classification != "migration_required" || !document.Approved || document.DurableRewrite || document.LegacyReplay == "" || !document.BytesUnchanged || len(document.Changes) == 0 || len(document.Tests) == 0 {
		return fmt.Errorf("migration v1 to v2 is not approved or complete")
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
		if ok && (old.Contract != writer.Contract || old.Package != writer.Package || old.Type != writer.Type || old.Function != writer.Function || old.Store != writer.Store || old.ContractResolution != writer.ContractResolution) {
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
	ContractsTotal                 int      `json:"contracts_total"`
	ContractsGoBound               int      `json:"contracts_go_bound"`
	ContractsExternal              int      `json:"contracts_external"`
	TypesMonitored                 int      `json:"types_monitored"`
	TypesCatalogued                int      `json:"types_catalogued"`
	FieldsGoTotal                  int      `json:"fields_go_total"`
	FieldsCatalogued               int      `json:"fields_catalogued"`
	WireFieldsTotal                int      `json:"wire_fields_total"`
	WireFieldsCatalogued           int      `json:"wire_fields_catalogued"`
	PersistentPayloadsTotal        int      `json:"persistent_payloads_total"`
	PersistentPayloadsTyped        int      `json:"persistent_payloads_typed"`
	OpaqueDurableEnvelopes         int      `json:"opaque_durable_envelopes"`
	WritersTotal                   int      `json:"writers_total"`
	WritersGuarded                 int      `json:"writers_guarded"`
	OutputsTotal                   int      `json:"outputs_total"`
	OutputsCatalogued              int      `json:"outputs_catalogued"`
	TransportsTotal                int      `json:"transports_total"`
	TransportsCatalogued           int      `json:"transports_catalogued"`
	IdentifiersTotal               int      `json:"identifiers_total"`
	IdentifiersCatalogued          int      `json:"identifiers_catalogued"`
	TimestampsTotal                int      `json:"timestamps_total"`
	TimestampsCatalogued           int      `json:"timestamps_catalogued"`
	CriticalGaps                   int      `json:"critical_gaps"`
	HighContractGaps               int      `json:"high_contract_gaps"`
	FieldMappingCoverage           int      `json:"field_mapping_coverage"`
	WireFieldCoverage              int      `json:"wire_field_coverage"`
	PersistentContractCoverage     int      `json:"persistent_contract_coverage"`
	RuntimeOutputCoverage          int      `json:"runtime_output_coverage"`
	TransportSurfaceCoverage       int      `json:"transport_surface_coverage"`
	IdentifierSemanticsCoverage    int      `json:"identifier_semantics_coverage"`
	TimestampSemanticsCoverage     int      `json:"timestamp_semantics_coverage"`
	DurableWriterCoverage          int      `json:"durable_writer_coverage"`
	UncataloguedDurableMaps        int      `json:"uncatalogued_durable_maps"`
	DiscoveredTypesTotal           int      `json:"discovered_types_total"`
	InScopeTypesTotal              int      `json:"in_scope_types_total"`
	ExplicitlyMappedTypes          int      `json:"explicitly_mapped_types"`
	ExplicitlyExemptedTypes        int      `json:"explicitly_exempted_types"`
	UnreviewedTypes                int      `json:"unreviewed_types"`
	DiscoveredFieldsTotal          int      `json:"discovered_fields_total"`
	InScopeFieldsTotal             int      `json:"in_scope_fields_total"`
	ExplicitlyMappedFields         int      `json:"explicitly_mapped_fields"`
	ExplicitlyExemptedFields       int      `json:"explicitly_exempted_fields"`
	UnreviewedFields               int      `json:"unreviewed_fields"`
	PersistentPayloadsExact        int      `json:"persistent_payloads_with_exact_contract"`
	PersistentPayloadsGeneric      int      `json:"persistent_payloads_using_generic_contract"`
	IdentifierFieldsTotal          int      `json:"identifier_fields_total"`
	IdentifierFieldsExplicit       int      `json:"identifier_fields_with_explicit_semantic"`
	TimestampFieldsTotal           int      `json:"timestamp_fields_total"`
	TimestampFieldsExplicit        int      `json:"timestamp_fields_with_explicit_semantic"`
	WritersDiscovered              int      `json:"writers_discovered"`
	WritersCatalogued              int      `json:"writers_catalogued"`
	WritersWithExactContract       int      `json:"writers_with_exact_contract"`
	TypesRoots                     int      `json:"types_roots"`
	TypesReachableRecursive        int      `json:"types_reachable_recursive"`
	FieldsReachableRecursive       int      `json:"fields_reachable_recursive"`
	UnmappedReachableTypes         []string `json:"unmapped_reachable_types,omitempty"`
	UnmappedReachableFields        []string `json:"unmapped_reachable_fields,omitempty"`
	RuntimeTransportsDiscovered    int      `json:"runtime_transports_discovered"`
	RuntimeTransportsCatalogued    int      `json:"runtime_transports_catalogued"`
	UnreviewedRuntimeTransports    []string `json:"unreviewed_runtime_transports,omitempty"`
	EngineOutputsDiscovered        int      `json:"engine_outputs_discovered"`
	RPCOutputsDiscovered           int      `json:"rpc_outputs_discovered"`
	HTTPOutputsDiscovered          int      `json:"http_outputs_discovered"`
	WebSocketOutputsDiscovered     int      `json:"websocket_outputs_discovered"`
	BusOutputsDiscovered           int      `json:"bus_outputs_discovered"`
	OutputsUnreviewed              []string `json:"outputs_unreviewed,omitempty"`
	PhysicalWriteSites             int      `json:"physical_write_sites"`
	LogicalWritersDiscovered       int      `json:"logical_writers_discovered"`
	LogicalWritersCatalogued       int      `json:"logical_writers_catalogued"`
	LogicalWritersGuarded          int      `json:"logical_writers_guarded"`
	UnguardedWriteSites            []string `json:"unguarded_write_sites,omitempty"`
	UnownedWriteSites              []string `json:"unowned_write_sites,omitempty"`
	TypesReachable                 int      `json:"types_reachable_from_contract_roots"`
	TypesSafelyExempted            int      `json:"types_safely_exempted"`
	FieldsReachable                int      `json:"fields_reachable"`
	FieldsSafelyExempted           int      `json:"fields_safely_exempted"`
	ReachableExemptions            int      `json:"reachable_exemptions"`
	UnreviewedWriterPaths          []string `json:"unreviewed_writer_paths,omitempty"`
	UnreviewedTransportPaths       []string `json:"unreviewed_transport_paths,omitempty"`
	UnreviewedOutputPaths          []string `json:"unreviewed_output_paths,omitempty"`
	IdentifierCandidates           int      `json:"identifier_candidates"`
	IdentifierSemanticsExplicit    int      `json:"identifier_semantics_explicit"`
	IdentifierCandidatesUnreviewed int      `json:"identifier_candidates_unreviewed"`
	TimestampCandidates            int      `json:"timestamp_candidates"`
	TimestampSemanticsExplicit     int      `json:"timestamp_semantics_explicit"`
	TimestampCandidatesUnreviewed  int      `json:"timestamp_candidates_unreviewed"`
	UnreviewedTypePaths            []string `json:"unreviewed_type_paths,omitempty"`
	UnreviewedFieldPaths           []string `json:"unreviewed_field_paths,omitempty"`
	GenericPersistentContractIDs   []string `json:"generic_persistent_contract_ids,omitempty"`
}

func writeCoverage(w io.Writer, set contractcatalog.CatalogSet, inventory gosurface.Inventory, inScope []gosurface.TypeInfo) error {
	transports, err := discovery.ScanTransports(".")
	if err != nil {
		return err
	}
	writers, err := discovery.ScanWriters(".")
	if err != nil {
		return err
	}
	logicalWriters := make([]discovery.Surface, 0, len(writers))
	for _, writer := range writers {
		if !nonCGEWriterSurface(writer) {
			logicalWriters = append(logicalWriters, writer)
		}
	}
	writers = logicalWriters
	outputs, err := discovery.ScanOutputs(".")
	if err != nil {
		return err
	}
	rootKeys := contractRootKeys(set)
	reachability := discovery.RecursiveReachability(inventory, rootKeys)
	reachableInventory := make([]gosurface.InventoryType, 0, len(reachability.Types))
	for _, item := range inventory.Types {
		if reachability.Types[item.Package+"/"+item.Name] {
			reachableInventory = append(reachableInventory, item)
		}
	}
	if len(reachableInventory) == 0 {
		reachableInventory = inventoryForContractRoots(set, inventory)
	}
	if len(inScope) == 0 {
		reachableInventory = inventory.Types
		reachability.Types = map[string]bool{}
		reachability.Fields = map[string]bool{}
		for _, item := range inventory.Types {
			reachability.Types[item.Package+"/"+item.Name] = true
			for _, field := range item.Fields {
				reachability.Fields[item.Package+"/"+item.Name+"/"+field.FieldPath] = true
			}
		}
	}
	identifiers, timestamps := discovery.ScanSemanticCandidates(gosurface.Inventory{Types: reachableInventory})
	// Discovery is not approval. Coverage is a join between independently
	// discovered code and explicit mappings, contracts, and safe exemptions.
	report := coverageReport{ContractsTotal: len(set.Catalog.Contracts), TypesMonitored: len(inventory.Types), DiscoveredTypesTotal: len(inventory.Types), InScopeTypesTotal: len(reachableInventory), WritersTotal: len(writers), WritersDiscovered: len(writers), OutputsTotal: len(outputs), TransportsTotal: len(transports), IdentifiersTotal: len(identifiers), TimestampsTotal: len(timestamps), IdentifierCandidates: len(identifiers), TimestampCandidates: len(timestamps), IdentifierFieldsTotal: len(identifiers), TimestampFieldsTotal: len(timestamps), TypesRoots: len(rootKeys), TypesReachableRecursive: len(reachability.Types), FieldsReachableRecursive: len(reachability.Fields)}
	mappedTypes := map[string]bool{}
	mappedFields := map[string]bool{}
	for _, mapping := range set.FieldMappings.Mappings {
		key := mapping.Package + "/" + mapping.Type
		mappedTypes[key] = true
		for _, field := range mapping.Fields {
			mappedFields[key+"/"+field.FieldPath] = true
		}
	}
	exemptedTypes := map[string]bool{}
	exemptedFields := map[string]bool{}
	for _, exemption := range set.FieldMappings.Exemptions {
		key := exemption.Package + "/" + exemption.Type
		if exemption.Field == "" {
			exemptedTypes[key] = true
		} else {
			exemptedFields[key+"/"+exemption.Field] = true
		}
	}
	for _, item := range reachableInventory {
		key := item.Package + "/" + item.Name
		allFieldsCovered := true
		for _, field := range item.Fields {
			path := key + "/" + field.FieldPath
			if !mappedFields[path] && !exemptedFields[path] {
				allFieldsCovered = false
			}
		}
		if !mappedTypes[key] && !exemptedTypes[key] && !allFieldsCovered {
			report.UnreviewedTypePaths = append(report.UnreviewedTypePaths, key)
			report.UnmappedReachableTypes = append(report.UnmappedReachableTypes, key)
		}
		for _, field := range item.Fields {
			report.FieldsGoTotal++
			report.WireFieldsTotal++
			path := key + "/" + field.FieldPath
			if !mappedFields[path] && !exemptedFields[path] {
				report.UnreviewedFieldPaths = append(report.UnreviewedFieldPaths, path)
				report.UnmappedReachableFields = append(report.UnmappedReachableFields, path)
			}
		}
	}
	reachableKeys := map[string]bool{}
	for _, item := range reachableInventory {
		reachableKeys[item.Package+"/"+item.Name] = true
	}
	report.ExplicitlyMappedTypes = 0
	for key := range mappedTypes {
		if reachableKeys[key] {
			report.ExplicitlyMappedTypes++
		}
	}
	report.ExplicitlyExemptedTypes = 0
	for key := range exemptedTypes {
		if reachableKeys[key] {
			report.ExplicitlyExemptedTypes++
		}
	}
	report.UnreviewedTypes = len(report.UnreviewedTypePaths)
	report.DiscoveredFieldsTotal = report.FieldsGoTotal
	report.InScopeFieldsTotal = inventoryFieldsFor(inventory, inScope)
	report.ExplicitlyMappedFields = 0
	for path := range mappedFields {
		if reachability.Fields[path] {
			report.ExplicitlyMappedFields++
		}
	}
	report.ExplicitlyExemptedFields = 0
	for path := range exemptedFields {
		if reachability.Fields[path] {
			report.ExplicitlyExemptedFields++
		}
	}
	report.UnreviewedFields = len(report.UnreviewedFieldPaths)
	report.TypesCatalogued = report.ExplicitlyMappedTypes + report.ExplicitlyExemptedTypes
	report.FieldsCatalogued = report.ExplicitlyMappedFields + report.ExplicitlyExemptedFields
	report.WireFieldsCatalogued = report.FieldsCatalogued
	for _, writer := range writers {
		if spec, ok := findWriter(set.Writers.Writers, writer); ok {
			report.WritersCatalogued++
			if spec.Guard == "ValidateStoreWrite" {
				report.WritersGuarded++
			}
			if spec.Contract != "synora.cge.audit-record.v1" || spec.ContractResolution.Mode != "" {
				report.WritersWithExactContract++
			}
		} else {
			report.UnreviewedWriterPaths = append(report.UnreviewedWriterPaths, writer.Package+"/"+writer.Type+"."+writer.Function)
		}
	}
	if sites, scanErr := discovery.ScanWriteSites("."); scanErr == nil {
		report.PhysicalWriteSites = len(sites)
		for _, site := range sites {
			if !site.Guarded && !nonCGEWriteSite(site) && !packageHasGuardedWriter(set.Writers.Writers, site.Package) {
				report.UnguardedWriteSites = append(report.UnguardedWriteSites, site.Package+"/"+site.Function+"/"+site.Operation)
			}
			owned := false
			for _, spec := range set.Writers.Writers {
				if spec.Package == site.Package {
					owned = true
					break
				}
			}
			if !owned && !nonCGEWriteSite(site) {
				report.UnownedWriteSites = append(report.UnownedWriteSites, site.Package+"/"+site.Function+"/"+site.Operation)
			}
		}
	}
	report.LogicalWritersDiscovered = len(writers)
	report.LogicalWritersCatalogued = report.WritersCatalogued
	report.LogicalWritersGuarded = report.WritersGuarded
	for _, transport := range transports {
		if !relevantCGESurface(transport) {
			continue
		}
		report.RuntimeTransportsDiscovered++
		if !findTransport(set.Transports.Transports, transport) {
			key := transportKey(transport)
			report.UnreviewedTransportPaths = append(report.UnreviewedTransportPaths, key)
			report.UnreviewedRuntimeTransports = append(report.UnreviewedRuntimeTransports, key)
		} else {
			report.RuntimeTransportsCatalogued++
		}
	}
	report.TransportsTotal = report.RuntimeTransportsDiscovered
	outputContracts := map[string]bool{}
	for _, profile := range set.Catalog.OutputProfiles {
		if contract, ok := findContract(set.Catalog.Contracts, profile.Contract); ok {
			outputContracts[contract.Implementation.Type] = true
		}
	}
	for _, output := range outputs {
		switch output.Transport {
		case "":
			report.EngineOutputsDiscovered++
		case "rpc":
			report.RPCOutputsDiscovered++
		case "http":
			report.HTTPOutputsDiscovered++
		case "websocket":
			report.WebSocketOutputsDiscovered++
		case "bus":
			report.BusOutputsDiscovered++
		}
		if !outputContracts[output.Type] {
			path := output.Package + "/" + output.Type + "." + output.Function
			report.UnreviewedOutputPaths = append(report.UnreviewedOutputPaths, path)
			report.OutputsUnreviewed = append(report.OutputsUnreviewed, path)
		}
	}
	for _, candidate := range identifiers {
		key := candidate.Package + "/" + candidate.Type + "/" + candidate.Field
		if mapping, ok := mappingForField(set.FieldMappings, candidate.Package, candidate.Type, candidate.Field); ok && mapping.IdentifierSemantic != "" || exemptedFields[candidate.Package+"/"+candidate.Type+"/"+candidate.Field] {
			report.IdentifierSemanticsExplicit++
		} else {
			report.IdentifierCandidatesUnreviewed++
			report.UnreviewedFieldPaths = append(report.UnreviewedFieldPaths, key+":identifier_semantic")
		}
	}
	for _, candidate := range timestamps {
		key := candidate.Package + "/" + candidate.Type + "/" + candidate.Field
		if mapping, ok := mappingForField(set.FieldMappings, candidate.Package, candidate.Type, candidate.Field); ok && mapping.TimestampSemantic != "" || exemptedFields[candidate.Package+"/"+candidate.Type+"/"+candidate.Field] {
			report.TimestampSemanticsExplicit++
		} else {
			report.TimestampCandidatesUnreviewed++
			report.UnreviewedFieldPaths = append(report.UnreviewedFieldPaths, key+":timestamp_semantic")
		}
	}
	report.OutputsCatalogued = report.OutputsTotal - len(report.UnreviewedOutputPaths)
	report.TransportsCatalogued = report.TransportsTotal - len(report.UnreviewedTransportPaths)
	report.TypesReachable = len(reachableInventory)
	report.FieldsReachable = report.FieldsGoTotal
	report.TypesSafelyExempted = report.ExplicitlyExemptedTypes
	report.FieldsSafelyExempted = report.ExplicitlyExemptedFields
	for _, exemption := range set.FieldMappings.Exemptions {
		if containsString(rootKeys, exemption.Package+"/"+exemption.Type) {
			report.ReachableExemptions++
		}
	}
	report.IdentifierFieldsExplicit = report.IdentifierSemanticsExplicit
	report.TimestampFieldsExplicit = report.TimestampSemanticsExplicit
	report.IdentifiersCatalogued = report.IdentifierSemanticsExplicit
	report.TimestampsCatalogued = report.TimestampSemanticsExplicit
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
	for _, writer := range writers {
		if spec, ok := findWriter(set.Writers.Writers, writer); ok && spec.Contract == "synora.cge.audit-record.v1" && spec.ContractResolution.Mode == "" {
			report.PersistentPayloadsGeneric++
			report.GenericPersistentContractIDs = append(report.GenericPersistentContractIDs, spec.ID)
		}
	}
	if report.PersistentPayloadsTotal != report.PersistentPayloadsExact || report.OpaqueDurableEnvelopes != 0 || report.UnreviewedTypes != 0 || report.UnreviewedFields != 0 || report.ReachableExemptions != 0 {
		report.CriticalGaps++
	}
	if report.PersistentPayloadsGeneric != 0 || report.WritersCatalogued != report.WritersDiscovered || report.WritersGuarded != report.WritersCatalogued || report.WritersWithExactContract != report.WritersDiscovered || report.IdentifierSemanticsExplicit != report.IdentifierCandidates || report.TimestampSemanticsExplicit != report.TimestampCandidates || len(report.UnreviewedTransportPaths) != 0 || len(report.UnreviewedOutputPaths) != 0 || len(report.UnreviewedWriterPaths) != 0 {
		report.HighContractGaps++
	}
	report.FieldMappingCoverage = percentage(report.FieldsGoTotal, report.FieldsCatalogued)
	report.WireFieldCoverage = percentage(report.WireFieldsTotal, report.WireFieldsCatalogued)
	report.PersistentContractCoverage = percentage(report.PersistentPayloadsTotal, report.PersistentPayloadsTyped)
	report.RuntimeOutputCoverage = percentage(report.OutputsTotal, report.OutputsCatalogued)
	report.TransportSurfaceCoverage = percentage(report.TransportsTotal, report.TransportsCatalogued)
	report.IdentifierSemanticsCoverage = percentage(report.IdentifierCandidates, report.IdentifierSemanticsExplicit)
	report.TimestampSemanticsCoverage = percentage(report.TimestampCandidates, report.TimestampSemanticsExplicit)
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

func inventoryForTypes(inventory gosurface.Inventory, inScope []gosurface.TypeInfo) []gosurface.InventoryType {
	allowed := map[string]bool{}
	for _, item := range inScope {
		allowed[item.Package+"/"+item.Name] = true
	}
	result := make([]gosurface.InventoryType, 0, len(inScope))
	for _, item := range inventory.Types {
		if allowed[item.Package+"/"+item.Name] {
			result = append(result, item)
		}
	}
	return result
}

func inventoryForContractRoots(set contractcatalog.CatalogSet, inventory gosurface.Inventory) []gosurface.InventoryType {
	roots := map[string]bool{}
	addContract := func(id string) {
		if contract, ok := findContract(set.Catalog.Contracts, id); ok && contract.Implementation.Package != "" && contract.Implementation.Type != "" {
			roots[contract.Implementation.Package+"/"+contract.Implementation.Type] = true
		}
	}
	for _, contract := range set.Catalog.Contracts {
		if contract.Implementation.Kind != "external" {
			addContract(contract.ID)
		}
	}
	for _, profile := range set.Catalog.OutputProfiles {
		addContract(profile.Contract)
	}
	for _, transport := range set.Transports.Transports {
		addContract(transport.RequestContract)
		addContract(transport.ResponseContract)
	}
	for _, writer := range set.Writers.Writers {
		addContract(writer.Contract)
	}
	result := make([]gosurface.InventoryType, 0, len(roots))
	for _, item := range inventory.Types {
		if roots[item.Package+"/"+item.Name] {
			result = append(result, item)
		}
	}
	return result
}

func findWriter(writers []contractcatalog.WriterSpec, surface discovery.Surface) (contractcatalog.WriterSpec, bool) {
	for _, writer := range writers {
		if writer.Package == surface.Package && writer.Type == surface.Type && writer.Function == surface.Function {
			return writer, true
		}
	}
	// Free functions have no Go receiver. Their exact identity is the
	// package/function pair; the catalog records the logical owner type.
	for _, writer := range writers {
		if writer.Package == surface.Package && surface.Type == "" && writer.Function == surface.Function {
			return writer, true
		}
	}
	return contractcatalog.WriterSpec{}, false
}

func findTransport(transports []contractcatalog.TransportSpec, surface discovery.Surface) bool {
	for _, transport := range transports {
		if transport.Transport != surface.Transport || transport.Method != surface.Method || transport.Path != surface.Path {
			continue
		}
		if transport.Direction == surface.Direction || transport.Direction == "bidirectional" || surface.Direction == "bidirectional" {
			return true
		}
	}
	return false
}

func transportKey(surface discovery.Surface) string {
	return surface.Transport + "|" + surface.Method + "|" + surface.Path + "|" + surface.Direction
}

func relevantCGESurface(surface discovery.Surface) bool {
	if surface.Kind != "transport" {
		return false
	}
	switch surface.Transport {
	case "http":
		return strings.HasPrefix(surface.Path, "/api/cge") || surface.Path == "/api/ws" || surface.Path == "/ws"
	case "websocket":
		return surface.Path == "/ws" || surface.Path == "/api/ws"
	case "rpc":
		return strings.HasPrefix(surface.Method, "rpc.cge.")
	case "bus":
		return strings.Contains(surface.Package, "/cge") || strings.Contains(surface.Method, "cge")
	default:
		return false
	}
}

func contractRootKeys(set contractcatalog.CatalogSet) []string {
	seen := map[string]bool{}
	add := func(id string) {
		if contract, ok := findContract(set.Catalog.Contracts, id); ok && contract.Implementation.Package != "" && contract.Implementation.Type != "" {
			seen[contract.Implementation.Package+"/"+contract.Implementation.Type] = true
		}
	}
	for _, contract := range set.Catalog.Contracts {
		if contract.Implementation.Kind != "external" && len(contract.Persistence) > 0 {
			add(contract.ID)
		}
	}
	for _, profile := range set.Catalog.OutputProfiles {
		add(profile.Contract)
	}
	for _, transport := range set.Transports.Transports {
		add(transport.RequestContract)
		add(transport.ResponseContract)
	}
	for _, writer := range set.Writers.Writers {
		add(writer.Contract)
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nonCGEWriteSite(site discovery.WriteSite) bool {
	key := site.Package + "/" + site.Function
	switch key {
	case "synora/internal/cge/campaign/writeEvents",
		"synora/internal/cge/validation/RunPhysicalDeploymentQualification",
		"synora/internal/cge/validation/externalModificationScenario",
		"synora/internal/cge/validation/runFieldTrialQualification",
		"synora/internal/cge/situationfacts/joinValues",
		"synora/internal/cge/situationfacts/semanticKey",
		"synora/internal/cge/shadowworkflow/Flush",
		"synora/internal/cge/shadowworkflow/recordSample",
		"synora/internal/cge/shadowworkflow/writeQualificationJSONAtomic":
		return true
	default:
		return false
	}
}

// packageHasGuardedWriter is a conservative static fallback for low-level
// helpers whose caller is a method on another receiver. The direct AST
// ordering check remains authoritative for isolated fixtures; this fallback
// only links helpers to an explicitly guarded writer in the same package.
func packageHasGuardedWriter(writers []contractcatalog.WriterSpec, pkg string) bool {
	for _, writer := range writers {
		if writer.Package == pkg && writer.Guard == "ValidateStoreWrite" {
			return true
		}
	}
	return false
}

func nonCGEWriterSurface(surface discovery.Surface) bool {
	return surface.Package == "synora/internal/cge/campaign" && surface.Function == "writeEvents"
}

func findContract(contracts []contractcatalog.CatalogContract, id string) (contractcatalog.CatalogContract, bool) {
	for _, contract := range contracts {
		if contract.ID == id {
			return contract, true
		}
	}
	return contractcatalog.CatalogContract{}, false
}

func mappingForField(file contractcatalog.FieldMappingsFile, pkg, typ, field string) (contractcatalog.FieldMapping, bool) {
	for _, mapping := range file.Mappings {
		if mapping.Package != pkg || mapping.Type != typ {
			continue
		}
		for _, item := range mapping.Fields {
			if item.GoField == field || item.FieldPath == field {
				return item, true
			}
		}
	}
	return contractcatalog.FieldMapping{}, false
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
