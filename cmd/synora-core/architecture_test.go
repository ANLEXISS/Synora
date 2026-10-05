package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are component architecture checks, not a second system runner. The
// central bus harness owns all Core/Discovery/Store end-to-end scenarios.
func TestV1ArchitectureHasNoRetiredDecisionImports(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "build" || info.Name() == "node_modules" || info.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imported := range file.Imports {
			value := strings.Trim(imported.Path.Value, `"`)
			forbidden := []string{"synora/internal/cge", "synora/internal/engine", "teacher", "shadow"}
			for _, marker := range forbidden {
				if value == marker || strings.HasPrefix(value, marker+"/") {
					t.Errorf("%s imports retired runtime domain %q", path, value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestV1ActiveRuntimeHasNoLegacyDecisionRuntime(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	markers := []string{"advisory_shadow", "state-encoder/v4", "V4StateEncoder", "EncoderV4", "teacher", "shadow_mode"}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "build" || info.Name() == "node_modules" || info.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, string(filepath.Separator)+"docs"+string(filepath.Separator)) {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, marker := range markers {
			if strings.Contains(string(body), marker) {
				t.Errorf("%s contains retired runtime marker %q", path, marker)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
