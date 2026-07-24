package gosurface

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "pkg")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "fixture.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCompareFieldsDetectsGoDrift(t *testing.T) {
	tests := []struct {
		name   string
		source string
		specs  []FieldSpec
	}{
		{"added field", `package pkg; type Payload struct { ID string ` + "`json:\"id\"`" + `; Extra string ` + "`json:\"extra\"`" + ` }`, []FieldSpec{{GoField: "ID", WireName: "id", Type: "string"}}},
		{"removed field", `package pkg; type Payload struct { }`, []FieldSpec{{GoField: "ID", WireName: "id", Type: "string"}}},
		{"changed tag", `package pkg; type Payload struct { ID string ` + "`json:\"identifier\"`" + ` }`, []FieldSpec{{GoField: "ID", WireName: "id", Type: "string"}}},
		{"changed type", `package pkg; type Payload struct { ID uint64 ` + "`json:\"id\"`" + ` }`, []FieldSpec{{GoField: "ID", WireName: "id", Type: "string"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, test.source)
			info, err := parsePackage(filepath.Join(root, "pkg"), "fixture/pkg")
			if err != nil {
				t.Fatal(err)
			}
			if err := CompareFields(info["Payload"], test.specs); err == nil {
				t.Fatalf("drift %q was accepted", test.name)
			}
		})
	}
}

func TestCompareFieldsAcceptsExplicitRuntimeAndCatalogOnlyExceptions(t *testing.T) {
	root := writeFixture(t, `package pkg; type Payload struct { ID string `+"`json:\"id\"`"+`; Runtime string `+"`json:\"runtime\"`"+` }`)
	info, err := parsePackage(filepath.Join(root, "pkg"), "fixture/pkg")
	if err != nil {
		t.Fatal(err)
	}
	if err := CompareFields(info["Payload"], []FieldSpec{{GoField: "ID", WireName: "id", Type: "string"}, {GoField: "Runtime", RuntimeOnly: true}, {GoField: "derived", CatalogOnly: true, ExceptionReason: "computed projection"}}); err != nil {
		t.Fatal(err)
	}
}
