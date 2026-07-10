package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIArchitectureBoundaries(t *testing.T) {
	assertNoForbiddenImportsFromRoot(t, ".", []string{
		"synora/internal/actions",
		"synora/internal/engine",
		"synora/tools/dev",
	})
	assertNoForbiddenImportsFromRoot(t, "../../internal/simulation", []string{
		"synora/cmd/synora-api",
		"github.com/gorilla/websocket",
	})
	assertNoForbiddenImportsFromRoot(t, "../../internal/bus", []string{
		"synora/internal/simulation",
	})
}

func assertNoForbiddenImportsFromRoot(t *testing.T, root string, forbiddenImports []string) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
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
		t.Fatal(err)
	}
}
