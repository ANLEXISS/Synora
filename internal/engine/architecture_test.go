package engine

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineDoesNotImportRuntimeDomains(t *testing.T) {
	forbiddenImports := []string{
		"synora/internal/actions",
		"synora/internal/api",
		"synora/internal/bus",
		"synora/internal/discovery",
	}

	assertNoForbiddenImports(t, ".", forbiddenImports)
}

func TestGraphDoesNotImportSynoraBoundaryDomains(t *testing.T) {
	forbiddenImports := []string{
		"synora/internal/actions",
		"synora/internal/api",
		"synora/internal/bus",
		"synora/internal/discovery",
		"synora/internal/state",
		"synora/pkg/contract",
	}

	assertNoForbiddenImports(t, "graph", forbiddenImports)
}

func TestCognitiveDoesNotImportSynoraBoundaryDomains(t *testing.T) {
	forbiddenImports := []string{
		"synora/internal/actions",
		"synora/internal/api",
		"synora/internal/bus",
		"synora/internal/discovery",
		"synora/internal/state",
		"synora/pkg/contract",
	}

	assertNoForbiddenImports(t, "cognitive", forbiddenImports)
}

func TestCoreDoesNotImportGraphMemoryDirectly(t *testing.T) {
	forbiddenImports := []string{
		"synora/internal/engine/graph",
		"synora/internal/engine/cognitive",
		"synora/internal/engine/contracts",
	}

	assertNoForbiddenImports(t, "../../cmd/synora-core", forbiddenImports)
}

func assertNoForbiddenImports(t *testing.T, root string, forbiddenImports []string) {
	t.Helper()

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			for _, forbidden := range forbiddenImports {
				if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
					t.Errorf("%s imports forbidden domain %q", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s imports: %v", root, err)
	}
}
