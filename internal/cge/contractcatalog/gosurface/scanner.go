// Package gosurface extracts the serializable Go surface named by the CGE
// catalog. It deliberately uses parser/ast rather than running application
// code, so it is safe for architecture validation and generator checks.
package gosurface

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type SurfaceConfig struct {
	SchemaVersion int         `yaml:"schema_version"`
	Namespace     string      `yaml:"namespace"`
	Packages      []Package   `yaml:"packages"`
	Exemptions    []Exemption `yaml:"exemptions"`
}

type Package struct {
	Import string   `yaml:"import"`
	Role   string   `yaml:"role"`
	Types  []string `yaml:"types"`
}

type Exemption struct {
	Package string `yaml:"package"`
	Type    string `yaml:"type"`
	Reason  string `yaml:"reason"`
	Scope   string `yaml:"scope"`
}

type TypeInfo struct {
	Package string
	Name    string
	Kind    string
	Alias   string
	Fields  []FieldInfo
}

type FieldInfo struct {
	Name      string
	Type      string
	WireType  string
	WireName  string
	Omitempty bool
	Tagged    bool
	Embedded  bool
	Pointer   bool
	Nullable  bool
	Slice     bool
	Map       bool
}

type FieldSpec struct {
	GoField         string
	WireName        string
	Type            string
	Omitempty       *bool
	RuntimeOnly     bool
	CatalogOnly     bool
	ExceptionReason string
}

// LoadConfig strictly loads the package/type surface declaration.
func LoadConfig(path string) (SurfaceConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return SurfaceConfig{}, err
	}
	defer file.Close()
	var config SurfaceConfig
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		if err == io.EOF {
			return SurfaceConfig{}, fmt.Errorf("surface config is empty")
		}
		return SurfaceConfig{}, err
	}
	var second any
	if err := decoder.Decode(&second); err != io.EOF {
		if err == nil {
			return SurfaceConfig{}, fmt.Errorf("surface config has multiple documents")
		}
		return SurfaceConfig{}, err
	}
	if config.SchemaVersion != 1 || config.Namespace != "synora.cge" {
		return SurfaceConfig{}, fmt.Errorf("surface config header is invalid")
	}
	return config, nil
}

// Scan validates and extracts every explicitly monitored package/type.
func Scan(root, path string) ([]TypeInfo, error) {
	config, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	result := make([]TypeInfo, 0)
	for _, pkg := range config.Packages {
		if pkg.Import == "" || len(pkg.Types) == 0 {
			return nil, fmt.Errorf("surface package %q is incomplete", pkg.Import)
		}
		directory, err := packageDirectory(root, pkg.Import)
		if err != nil {
			return nil, err
		}
		types, err := parsePackage(directory, pkg.Import)
		if err != nil {
			return nil, err
		}
		for _, name := range pkg.Types {
			info, ok := types[name]
			if !ok {
				return nil, fmt.Errorf("surface type %s.%s does not exist", pkg.Import, name)
			}
			result = append(result, info)
		}
	}
	return result, nil
}

// ScanAll returns every exported struct or alias in the monitored packages.
// It is used for the machine-readable inventory; unlike Scan it does not rely
// on a hand-maintained type list.
func ScanAll(root, path string) ([]TypeInfo, error) {
	config, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	result := make([]TypeInfo, 0)
	seen := map[string]bool{}
	for _, pkg := range config.Packages {
		directory, err := packageDirectory(root, pkg.Import)
		if err != nil {
			return nil, err
		}
		types, err := parsePackage(directory, pkg.Import)
		if err != nil {
			return nil, err
		}
		for _, info := range types {
			if info.Kind != "struct" && info.Kind != "alias" {
				continue
			}
			if info.Kind == "struct" && len(info.Fields) == 0 {
				continue
			}
			key := info.Package + "/" + info.Name
			if !seen[key] {
				result = append(result, info)
				seen[key] = true
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Package == result[j].Package {
			return result[i].Name < result[j].Name
		}
		return result[i].Package < result[j].Package
	})
	return result, nil
}

// CompareFields compares a scanned type with explicit catalog field mappings.
// Runtime-only fields are ignored; catalog-only fields are accepted only when
// they carry an explicit reason. This function is also used by fixture tests
// to prove that additions, removals, tag changes and type changes are red.
func CompareFields(info TypeInfo, specs []FieldSpec) error {
	byGo := make(map[string]FieldInfo, len(info.Fields))
	for _, field := range info.Fields {
		if field.WireName == "-" {
			continue
		}
		byGo[field.Name] = field
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		if spec.CatalogOnly {
			if strings.TrimSpace(spec.ExceptionReason) == "" {
				return fmt.Errorf("catalog-only field %q lacks exception reason", spec.GoField)
			}
			continue
		}
		name := spec.GoField
		if name == "" {
			name = spec.WireName
		}
		field, ok := byGo[name]
		if !ok {
			if spec.RuntimeOnly {
				continue
			}
			return fmt.Errorf("catalog field %q is absent from Go type %s", name, info.Name)
		}
		seen[name] = true
		if spec.WireName != "" && field.WireName != spec.WireName {
			return fmt.Errorf("wire name mismatch for %s.%s", info.Name, name)
		}
		if spec.Type != "" && normalizeType(spec.Type) != normalizeType(field.Type) {
			return fmt.Errorf("type mismatch for %s.%s", info.Name, name)
		}
		if spec.Omitempty != nil && *spec.Omitempty != field.Omitempty {
			return fmt.Errorf("omitempty mismatch for %s.%s", info.Name, name)
		}
	}
	for name, field := range byGo {
		if !seen[name] && !field.Embedded && field.WireName != "-" {
			return fmt.Errorf("Go field %s.%s is not catalogued", info.Name, name)
		}
	}
	return nil
}

func normalizeType(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, " ", ""))
}

func packageDirectory(root, importPath string) (string, error) {
	const module = "synora/"
	if !strings.HasPrefix(importPath, module) {
		return "", fmt.Errorf("unsupported module import %q", importPath)
	}
	directory := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(importPath, module)))
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("package directory unavailable for %q", importPath)
	}
	return directory, nil
}

func parsePackage(directory, importPath string) (map[string]TypeInfo, error) {
	files, err := parser.ParseDir(token.NewFileSet(), directory, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse package %s: %w", importPath, err)
	}
	result := map[string]TypeInfo{}
	for _, pkg := range files {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				gen, ok := declaration.(*ast.GenDecl)
				if !ok || gen.Tok.String() != "type" {
					continue
				}
				for _, specification := range gen.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if !ok || !ast.IsExported(typeSpec.Name.Name) {
						continue
					}
					info := TypeInfo{Package: importPath, Name: typeSpec.Name.Name, Kind: typeKind(typeSpec.Type)}
					if typeSpec.Assign.IsValid() {
						info.Kind = "alias"
						info.Alias = exprString(typeSpec.Type)
					}
					if structure, ok := typeSpec.Type.(*ast.StructType); ok {
						info.Fields = structFields(structure)
					}
					result[info.Name] = info
				}
			}
		}
	}
	return result, nil
}

func typeKind(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	default:
		return "scalar_or_alias"
	}
}

func structFields(structure *ast.StructType) []FieldInfo {
	fields := make([]FieldInfo, 0)
	if structure.Fields == nil {
		return fields
	}
	for _, field := range structure.Fields.List {
		wireName, omitempty, tagged := jsonFieldTag(field.Tag)
		if len(field.Names) == 0 {
			typeName := exprString(field.Type)
			fields = append(fields, FieldInfo{Name: typeName, Type: typeName, WireType: typeName, WireName: wireName, Omitempty: omitempty, Tagged: tagged, Embedded: true, Pointer: isPointer(field.Type), Nullable: isNullable(field.Type), Slice: isSlice(field.Type), Map: isMap(field.Type)})
			continue
		}
		for _, name := range field.Names {
			if !ast.IsExported(name.Name) {
				continue
			}
			typeName := exprString(field.Type)
			fields = append(fields, FieldInfo{Name: name.Name, Type: typeName, WireType: typeName, WireName: wireNameOrDefault(wireName, name.Name), Omitempty: omitempty, Tagged: tagged, Pointer: isPointer(field.Type), Nullable: isNullable(field.Type), Slice: isSlice(field.Type), Map: isMap(field.Type)})
		}
	}
	return fields
}

func isPointer(expr ast.Expr) bool { _, ok := expr.(*ast.StarExpr); return ok }
func isSlice(expr ast.Expr) bool   { _, ok := expr.(*ast.ArrayType); return ok }
func isMap(expr ast.Expr) bool     { _, ok := expr.(*ast.MapType); return ok }
func isNullable(expr ast.Expr) bool {
	return isPointer(expr) || isSlice(expr) || isMap(expr)
}

func jsonFieldTag(tag *ast.BasicLit) (string, bool, bool) {
	if tag == nil {
		return "", false, false
	}
	raw, err := strconv.Unquote(tag.Value)
	if err != nil {
		return "", false, true
	}
	value := reflect.StructTag(raw).Get("json")
	if value == "" {
		return "", false, true
	}
	parts := strings.Split(value, ",")
	if len(parts) > 0 && parts[0] == "-" {
		return "-", false, true
	}
	omitempty := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitempty = true
		}
	}
	return parts[0], omitempty, true
}

func wireNameOrDefault(wireName, fieldName string) string {
	if wireName != "" {
		return wireName
	}
	return fieldName
}

func exprString(expr ast.Expr) string {
	var builder strings.Builder
	if err := format.Node(&builder, token.NewFileSet(), expr); err != nil {
		return "<invalid>"
	}
	return builder.String()
}
