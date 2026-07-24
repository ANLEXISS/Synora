package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"synora/internal/cge/contractcatalog"
	"synora/internal/cge/contractcatalog/gosurface"
)

func contractgenRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestCoverageDoesNotApproveDiscoveryAlone(t *testing.T) {
	root := contractgenRoot(t)
	set, err := contractcatalog.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := gosurface.BuildInventory(root, filepath.Join(root, "configs/cge/contracts/go-surfaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	inventory.Types = append(inventory.Types, gosurface.InventoryType{
		Package: "synora/test/future", Name: "Unreviewed", Kind: "struct",
		Fields: []gosurface.InventoryField{{GoField: "NewField", FieldPath: "NewField", WireName: "new_field", GoType: "string", WireType: "string"}},
	})
	var output bytes.Buffer
	if err := writeCoverage(&output, set, inventory, nil); err == nil {
		t.Fatal("discovered field was accepted without an explicit mapping or exemption")
	}
	if !strings.Contains(output.String(), "unreviewed_type_paths") {
		t.Fatalf("coverage report did not identify the unreviewed surface: %s", output.String())
	}
}

func TestCoverageRejectsReachableExemption(t *testing.T) {
	root := contractgenRoot(t)
	set, err := contractcatalog.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := gosurface.BuildInventory(root, filepath.Join(root, "configs/cge/contracts/go-surfaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	set.FieldMappings.Exemptions = append(set.FieldMappings.Exemptions, contractcatalog.MappingExemption{
		Package: "synora/internal/cge/chains", Type: "ObservationRef", Field: "ID",
		Reason: "fixture", Scope: "fixture", ReviewStatus: "approved", Proof: "not_reachable_from_contract_roots",
	})
	var output bytes.Buffer
	if err := writeCoverage(&output, set, inventory, nil); err == nil {
		t.Fatal("reachable exemption was accepted")
	}
	if !strings.Contains(output.String(), "reachable_exemptions") {
		t.Fatalf("coverage report omitted reachable exemption evidence: %s", output.String())
	}
}

func TestBaselineCannotBeOverwrittenAndCompatibilityIsReadOnly(t *testing.T) {
	root := t.TempDir()
	set, err := contractcatalog.Validate(contractgenRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBaseline(root, set); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, baselinePath)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBaseline(root, set); err == nil {
		t.Fatal("baseline overwrite was accepted")
	}
	if err := checkCompatibility(root, set); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeSum, afterSum := sha256.Sum256(before), sha256.Sum256(after)
	if beforeSum != afterSum {
		t.Fatal("compatibility check modified the baseline")
	}
}

func TestGenerateRenderingDoesNotTouchBaseline(t *testing.T) {
	root := contractgenRoot(t)
	set, err := contractcatalog.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, baselinePath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := render(set); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, baselinePath))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("rendering changed immutable baseline")
	}
}

func TestCompatibilityClassificationIsDeterministic(t *testing.T) {
	base := canonicalSet{Catalog: contractcatalog.CatalogFile{Contracts: []contractcatalog.CatalogContract{{ID: "synora.cge.example.v1", Status: "stable", Authority: "descriptive", Implementation: contractcatalog.Implementation{Package: "p", Type: "T"}}}}}
	optional := base
	optional.Catalog.Contracts = append(append([]contractcatalog.CatalogContract(nil), base.Catalog.Contracts...), contractcatalog.CatalogContract{ID: "synora.cge.experimental.v1", Status: "experimental"})
	if classification, _ := classifyCompatibility(base, optional); classification != "compatible" {
		t.Fatalf("optional experimental contract classified as %s", classification)
	}
	persistent := base
	persistent.Catalog.Contracts = append(append([]contractcatalog.CatalogContract(nil), base.Catalog.Contracts...), contractcatalog.CatalogContract{ID: "synora.cge.persisted.v1", Status: "experimental", Persistence: []string{"store"}})
	if classification, _ := classifyCompatibility(base, persistent); classification != "migration_required" {
		t.Fatalf("persistent addition classified as %s", classification)
	}
	breaking := base
	breaking.Catalog.Contracts = []contractcatalog.CatalogContract{{ID: "synora.cge.example.v1", Status: "stable", Authority: "advisory", Implementation: contractcatalog.Implementation{Package: "p", Type: "T"}}}
	if classification, _ := classifyCompatibility(base, breaking); classification != "breaking" {
		t.Fatalf("stable authority increase classified as %s", classification)
	}
}
