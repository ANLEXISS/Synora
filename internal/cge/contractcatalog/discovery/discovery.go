// Package discovery finds CGE transport, durable-writer, output, identifier
// and timestamp candidates directly from Go syntax. It is intentionally
// independent from the YAML catalog: the catalog is only consulted by the
// caller when joining discovered records with approved declarations.
package discovery

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"synora/internal/cge/contractcatalog/gosurface"
)

type Surface struct {
	Kind      string `json:"kind"`
	Package   string `json:"package"`
	Function  string `json:"function"`
	Operation string `json:"operation"`
	Path      string `json:"path_or_channel"`
	Type      string `json:"type"`
}

type SemanticCandidate struct {
	Package  string `json:"package"`
	Type     string `json:"type"`
	Field    string `json:"field"`
	WireName string `json:"wire_name"`
	Semantic string `json:"semantic"`
}

func ScanTransports(root string) ([]Surface, error) {
	var result []Surface
	err := walkGo(root, []string{"cmd/synora-api", "internal/rpc", "cmd/synora-core"}, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				operation := selector.Sel.Name
				if operation != "HandleFunc" && operation != "Handle" && operation != "Get" && operation != "Post" && operation != "Put" && operation != "Patch" && operation != "Delete" && operation != "Route" {
					return true
				}
				path, ok := stringLiteral(call.Args[0])
				if !ok || (!strings.HasPrefix(path, "/api/cge") && path != "/api/ws" && path != "/ws") {
					return true
				}
				result = append(result, Surface{Kind: "transport", Package: pkg, Function: function.Name.Name, Operation: operation, Path: path})
				return true
			})
		}
	})
	if err != nil {
		return nil, err
	}
	return uniqueSurfaces(result), nil
}

func ScanWriters(root string) ([]Surface, error) {
	var result []Surface
	paths := []string{"internal/cge", "internal/cge/chains", "internal/cge/durableworkflow", "internal/cge/fieldtrial", "internal/cge/calibrationledger"}
	err := walkGo(root, paths, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if strings.Contains(pkg, "/contractcatalog") || strings.Contains(pkg, "/validation") || strings.Contains(pkg, "/demo") || strings.Contains(pkg, "/campaign") || strings.Contains(pkg, "/shadowworkflow") {
				continue
			}
			operations := []string{}
			guarded := false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := selectorName(call.Fun)
				if name == "ValidateStoreWrite" {
					guarded = true
				}
				switch name {
				case "Write", "WriteString", "WriteFile", "Rename", "Sync":
					operations = append(operations, name)
				}
				return true
			})
			writerName := strings.ToLower(function.Name.Name)
			genericHelper := writerName == "writeall" || writerName == "writejson" || writerName == "writendjson" || writerName == "syncdirectory" || writerName == "copyfile"
			explicitSerializer := guarded || writerName == "buildrecord" || writerName == "encodeenvelope" || writerName == "encodeannotationenvelope"
			fileWriter := len(operations) != 0 && !genericHelper && (strings.Contains(writerName, "append") || strings.Contains(writerName, "save") || strings.Contains(writerName, "write") || strings.Contains(writerName, "record") || strings.Contains(writerName, "checkpoint"))
			if !explicitSerializer && !fileWriter {
				continue
			}
			receiver := ""
			if function.Recv != nil && len(function.Recv.List) != 0 {
				receiver = receiverName(function.Recv.List[0].Type)
			}
			operation := "serialize"
			if len(operations) != 0 {
				operation = uniqueStrings(operations)[0]
			}
			result = append(result, Surface{Kind: "writer", Package: pkg, Type: receiver, Function: function.Name.Name, Operation: operation})
		}
	})
	if err != nil {
		return nil, err
	}
	return uniqueSurfaces(result), nil
}

func ScanOutputs(root string) ([]Surface, error) {
	var result []Surface
	interfaceMethods := map[string]bool{}
	err := walkGo(root, []string{"internal/cge", "cmd/synora-core", "internal/rpc"}, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != "CognitiveEngine" {
					continue
				}
				interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				for _, field := range interfaceType.Methods.List {
					if len(field.Names) != 0 {
						interfaceMethods[field.Names[0].Name] = true
					}
				}
			}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Recv == nil || function.Type == nil || function.Type.Results == nil || len(function.Type.Results.List) == 0 || !ast.IsExported(function.Name.Name) {
				continue
			}
			receiver := receiverName(function.Recv.List[0].Type)
			if receiver != "ShadowEngine" && receiver != "CognitiveEngine" {
				continue
			}
			if pkg == "synora/internal/cge" && function.Name.Name == "ContextProviderStatus" {
				continue
			}
			if receiver == "ShadowEngine" && len(interfaceMethods) != 0 && function.Name.Name != "Observe" && function.Name.Name != "Snapshot" && function.Name.Name != "Explain" && function.Name.Name != "ObserveHistoricalDecision" {
				continue
			}
			if receiver == "ShadowEngine" && len(interfaceMethods) != 0 && !interfaceMethods[function.Name.Name] {
				continue
			}
			outputType := expressionName(function.Type.Results.List[0].Type)
			if outputType == "" {
				continue
			}
			result = append(result, Surface{Kind: "output", Package: pkg, Function: function.Name.Name, Type: outputType})
		}
	})
	if err != nil {
		return nil, err
	}
	return uniqueSurfaces(result), nil
}

func ScanSemanticCandidates(inventory gosurface.Inventory) (identifiers, timestamps []SemanticCandidate) {
	for _, item := range inventory.Types {
		for _, field := range item.Fields {
			name := strings.ToLower(field.GoField)
			wire := strings.ToLower(field.WireName)
			if identifierName(name, wire) {
				identifiers = append(identifiers, SemanticCandidate{Package: item.Package, Type: item.Name, Field: field.GoField, WireName: field.WireName, Semantic: ""})
			}
			if strings.Contains(field.GoType, "time.Time") || strings.Contains(field.GoType, "*time.Time") || strings.HasSuffix(name, "timestamp") || strings.HasSuffix(name, "_at") || strings.HasSuffix(wire, "_at") {
				timestamps = append(timestamps, SemanticCandidate{Package: item.Package, Type: item.Name, Field: field.GoField, WireName: field.WireName, Semantic: ""})
			}
		}
	}
	return uniqueCandidates(identifiers), uniqueCandidates(timestamps)
}

func identifierName(name, wire string) bool {
	for _, value := range []string{name, wire} {
		if strings.HasSuffix(value, "_id") || strings.HasSuffix(value, "_ref") || strings.HasSuffix(value, "_digest") || strings.HasSuffix(value, "_fingerprint") || strings.HasSuffix(value, "_sequence") || strings.HasSuffix(value, "_revision") || strings.HasSuffix(value, "id") || strings.HasSuffix(value, "ref") {
			return true
		}
	}
	return false
}

func walkGo(root string, paths []string, visit func(string, *ast.File)) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	for _, relative := range paths {
		directory := filepath.Join(absoluteRoot, relative)
		if _, statErr := os.Stat(directory); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			return statErr
		}
		if err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") || strings.HasSuffix(info.Name(), "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			relativeDir, relErr := filepath.Rel(absoluteRoot, filepath.Dir(path))
			if relErr != nil {
				return relErr
			}
			pkg := "synora/" + filepath.ToSlash(relativeDir)
			visit(pkg, file)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind.String() != "STRING" {
		return "", false
	}
	value := strings.Trim(literal.Value, "\"")
	return value, true
}

func selectorName(expression ast.Expr) string {
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		return selector.Sel.Name
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

func receiverName(expression ast.Expr) string {
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
	}
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		return selector.Sel.Name
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

func expressionName(expression ast.Expr) string {
	if star, ok := expression.(*ast.StarExpr); ok {
		return expressionName(star.X)
	}
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		return selector.Sel.Name
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	if array, ok := expression.(*ast.ArrayType); ok {
		return expressionName(array.Elt)
	}
	return ""
}

func uniqueSurfaces(values []Surface) []Surface {
	seen := map[string]bool{}
	result := make([]Surface, 0, len(values))
	for _, value := range values {
		key := fmt.Sprintf("%s|%s|%s|%s|%s", value.Kind, value.Package, value.Function, value.Operation, value.Path+value.Type)
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprintf("%#v", result[i]) < fmt.Sprintf("%#v", result[j]) })
	return result
}

func uniqueCandidates(values []SemanticCandidate) []SemanticCandidate {
	seen := map[string]bool{}
	result := make([]SemanticCandidate, 0, len(values))
	for _, value := range values {
		key := value.Package + "/" + value.Type + "/" + value.Field
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Package+result[i].Type+result[i].Field < result[j].Package+result[j].Type+result[j].Field
	})
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
