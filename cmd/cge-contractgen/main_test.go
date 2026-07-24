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
