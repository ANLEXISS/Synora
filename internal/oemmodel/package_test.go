package oemmodel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageManifestStrictSchemaAndTraversal(t *testing.T) {
	p := fixture()
	data, e := jsonMarshal(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Decode(data); e != nil {
		t.Fatal(e)
	}
	if _, e = Decode([]byte(strings.Replace(string(data), `"schema_version"`, `"unknown" : true, "schema_version"`, 1))); e == nil {
		t.Fatal("unknown field accepted")
	}
	p.ArtifactFilename = "../model.rknn"
	if p.Validate(DefaultRegistry()) == nil {
		t.Fatal("traversal accepted")
	}
}

func TestShippedPackageManifestsAreSyntheticAndValid(t *testing.T) {
	for _, name := range []string{"synora-person-rknn-prepared", "synora-pose-rknn-prepared", "synora-person-mock-test", "synora-pose-replay-test"} {
		p, _, err := Load(filepath.Join("../../configs/oem/model-packages", name+".json"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.PackageID != name || p.QualificationState != "prepared" {
			t.Fatalf("manifest promoted or mismatched: %+v", p)
		}
	}
}

func TestPackagePreflightChecksSymlinkAndHashWithoutLoading(t *testing.T) {
	p := fixture()
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, p.ArtifactFilename)); err != nil {
		t.Fatal(err)
	}
	r := Preflight(p, root, "arm64", "1.0.0", false, nil)
	if r.ErrorCategory != "artifact_invalid" {
		t.Fatalf("symlink status: %+v", r)
	}
	if r.QualificationStatus != "prepared" {
		t.Fatalf("package promoted: %+v", r)
	}
}

func TestPackageScenariosAllPassAndAreRedacted(t *testing.T) {
	results := RunScenarios()
	if len(results) < 26 {
		t.Fatalf("scenario count %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("%s: %s != %s", r.ID, r.Actual, r.Expected)
		}
		if r.AudioRendered || r.PhysicalAction || r.NetworkAccess || r.RawDataForwarded {
			t.Errorf("side effect in %s", r.ID)
		}
	}
}

func TestRuntimeAdaptersAreExplicitAndFailClosed(t *testing.T) {
	for id, adapter := range RuntimeAdapters() {
		if adapter.ID() != id {
			t.Fatalf("registry key/id mismatch %s %s", id, adapter.ID())
		}
		if id == "mock" || id == "replay" {
			if adapter.ObserveAvailability(context.Background()).State != "test_only" {
				t.Errorf("%s not test-only", id)
			}
		} else if adapter.ObserveAvailability(context.Background()).State != "unavailable" {
			t.Errorf("%s unexpectedly available", id)
		}
		if err := adapter.LoadIfAuthorized(context.Background(), fixture(), Authorization{PackageID: fixture().PackageID, ArtifactSHA256: fixture().ArtifactSHA256, AllowLoad: true, TestProfile: true}); err == nil {
			t.Errorf("%s loaded an unimplemented runtime", id)
		}
	}
}

func TestPostprocessRegistryOnlyNormalizesAggregateModuleResults(t *testing.T) {
	p := fixture()
	adapter := PostprocessAdapters()[p.PostprocessAdapterID]
	if _, err := adapter.Normalize(p, SemanticResult{ModuleID: p.ModuleID, Status: "available", Aggregate: map[string]float64{"quality_ratio": 0.8}}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Normalize(p, SemanticResult{ModuleID: p.ModuleID, Status: "available", Aggregate: map[string]float64{"embedding": 0.8}}); err == nil {
		t.Fatal("raw semantic value accepted")
	}
	if _, err := adapter.Normalize(p, SemanticResult{ModuleID: "vision-face", Status: "available"}); err == nil {
		t.Fatal("cross-module postprocessing accepted")
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
