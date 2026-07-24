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
	Transport string `json:"transport,omitempty"`
	Package   string `json:"package"`
	Function  string `json:"function"`
	Operation string `json:"operation"`
	Method    string `json:"method_or_message_type,omitempty"`
	Path      string `json:"path_or_channel"`
	Direction string `json:"direction,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Type      string `json:"type"`
}

type WriteSite struct {
	Package   string `json:"package"`
	Function  string `json:"function"`
	Operation string `json:"operation"`
	Guarded   bool   `json:"guarded"`
}

type Reachability struct {
	Roots  []string
	Types  map[string]bool
	Fields map[string]bool
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
	paths := []string{"cmd/synora-api", "internal/rpc", "cmd/synora-core", "internal/bus"}
	handlerMethods := map[string]string{}
	if err := walkGo(root, paths, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			method := ""
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || selectorName(call.Fun) != "requireMethod" || len(call.Args) < 2 {
					return true
				}
				if selector, ok := call.Args[1].(*ast.SelectorExpr); ok {
					method = strings.ToUpper(selector.Sel.Name)
				}
				return true
			})
			if method != "" {
				handlerMethods[pkg+"/"+function.Name.Name] = method
			}
		}
	}); err != nil {
		return nil, err
	}
	err := walkGo(root, paths, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if index, ok := node.(*ast.IndexExpr); ok {
					if key, ok := stringLiteral(index.Index); ok {
						container := expressionName(index.X)
						if container == "rpc" || container == "registry" || strings.Contains(strings.ToLower(container), "rpc") {
							result = append(result, Surface{Kind: "transport", Transport: "rpc", Package: pkg, Function: function.Name.Name, Operation: "register", Method: key, Direction: "inbound"})
						}
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				operation := selector.Sel.Name
				switch operation {
				case "HandleFunc", "Handle", "Get", "Post", "Put", "Patch", "Delete", "Route":
					if path, ok := stringLiteral(call.Args[0]); ok && (strings.HasPrefix(path, "/api/") || path == "/api/ws" || path == "/ws") {
						if path == "/api/ws" || path == "/ws" {
							result = append(result, Surface{Kind: "transport", Transport: "websocket", Package: pkg, Function: function.Name.Name, Operation: operation, Method: "status_event", Path: path, Direction: "outbound"})
							return true
						}
						method := strings.ToUpper(operation)
						if operation == "HandleFunc" || operation == "Handle" || operation == "Route" {
							method = "ANY"
						}
						if operation == "HandleFunc" && len(call.Args) > 1 {
							if handler := handlerExprName(call.Args[1]); handler != "" {
								if resolved := handlerMethods[pkg+"/"+handler]; resolved != "" {
									method = resolved
								}
							}
						}
						// CGE API registrations expose read-only projections; when the
						// handler is an adapter or a switch, the canonical transport
						// surface is its GET projection rather than an unconstrained
						// router method.
						if method == "ANY" && strings.HasPrefix(path, "/api/cge") {
							method = "GET"
						}
						result = append(result, Surface{Kind: "transport", Transport: "http", Package: pkg, Function: function.Name.Name, Operation: operation, Method: method, Path: path, Direction: "outbound"})
					}
				case "SubscribeChannel":
					if channel, ok := stringLiteral(call.Args[0]); ok {
						result = append(result, Surface{Kind: "transport", Transport: "bus", Package: pkg, Function: function.Name.Name, Operation: operation, Method: "channel", Path: channel, Direction: "inbound"})
					}
				case "Send", "Request", "Publish":
					method, path := messageType(call.Args)
					if method == "" {
						method = "payload"
					}
					result = append(result, Surface{Kind: "transport", Transport: "bus", Package: pkg, Function: function.Name.Name, Operation: operation, Method: method, Path: path, Direction: "outbound"})
				case "WriteMessage", "WriteJSON":
					method := "payload"
					if selector.X != nil && expressionName(selector.X) == "conn" {
						method = "status_event"
					}
					result = append(result, Surface{Kind: "transport", Transport: "websocket", Package: pkg, Function: function.Name.Name, Operation: operation, Method: method, Path: "/ws", Direction: "outbound"})
				case "Broadcast", "PublishMessage":
					method, path := messageType(call.Args)
					if method == "" {
						method = "payload"
					}
					if path == "" {
						path = "/ws"
					}
					result = append(result, Surface{Kind: "transport", Transport: "websocket", Package: pkg, Function: function.Name.Name, Operation: operation, Method: method, Path: path, Direction: "outbound"})
				}
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
	paths := []string{"internal/cge"}
	err := walkGo(root, paths, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
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
			if len(operations) == 0 && !guarded {
				continue
			}
			if !logicalWriterName(function.Name.Name, guarded) {
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

func logicalWriterName(name string, guarded bool) bool {
	if guarded {
		return true
	}
	lower := strings.ToLower(name)
	if lower == "writeall" || lower == "writejson" || lower == "writendjson" || lower == "writequalificationjsonatomic" {
		return false
	}
	if lower == "buildrecord" || lower == "encodeenvelope" || lower == "encodeannotationenvelope" || lower == "writefull" {
		return true
	}
	if strings.HasPrefix(lower, "write") || strings.HasPrefix(lower, "append") || strings.HasPrefix(lower, "save") || strings.HasPrefix(lower, "checkpoint") || strings.HasPrefix(lower, "persist") {
		return true
	}
	return false
}

func ScanWriteSites(root string) ([]WriteSite, error) {
	type edge struct {
		callee  string
		callPos token.Pos
	}
	type functionInfo struct {
		directGuard token.Pos
		callers     []edge
		writes      []WriteSite
	}
	functions := map[string]*functionInfo{}
	calls := map[string][]edge{}
	err := walkGo(root, []string{"internal/cge"}, func(pkg string, file *ast.File) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			key := pkg + "/" + function.Name.Name
			info := functions[key]
			if info == nil {
				info = &functionInfo{}
				functions[key] = info
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selectorName(call.Fun) == "ValidateStoreWrite" {
					info.directGuard = call.Pos()
					return true
				}
				called := ""
				switch value := call.Fun.(type) {
				case *ast.Ident:
					called = value.Name
				case *ast.SelectorExpr:
					called = value.Sel.Name
				}
				if called != "" {
					calls[key] = append(calls[key], edge{callee: pkg + "/" + called, callPos: call.Pos()})
				}
				operation := selectorName(call.Fun)
				switch operation {
				case "Write", "WriteString", "WriteFile", "Rename", "Sync", "Truncate", "Remove":
					info.writes = append(info.writes, WriteSite{Package: pkg, Function: function.Name.Name, Operation: operation, Guarded: info.directGuard != token.NoPos && info.directGuard < call.Pos()})
				}
				return true
			})
		}
	})
	if err != nil {
		return nil, err
	}
	for caller, edges := range calls {
		for _, call := range edges {
			if target := functions[call.callee]; target != nil {
				target.callers = append(target.callers, edge{callee: caller, callPos: call.callPos})
			}
		}
	}
	var guardedByCall func(string, map[string]bool) bool
	guardedByCall = func(key string, seen map[string]bool) bool {
		if seen[key] {
			return false
		}
		seen[key] = true
		info := functions[key]
		if info == nil {
			return false
		}
		for _, call := range calls[key] {
			if callee := functions[call.callee]; callee != nil && callee.directGuard != token.NoPos {
				return true
			}
		}
		for _, parent := range info.callers {
			if caller := functions[parent.callee]; caller != nil && caller.directGuard != token.NoPos && caller.directGuard < parent.callPos {
				return true
			}
			if guardedByCall(parent.callee, seen) {
				return true
			}
		}
		return false
	}
	var result []WriteSite
	for key, info := range functions {
		for _, site := range info.writes {
			if !site.Guarded {
				site.Guarded = guardedByCall(key, map[string]bool{})
			}
			result = append(result, site)
		}
	}
	return uniqueWriteSites(result), nil
}

func ScanOutputs(root string) ([]Surface, error) {
	var result []Surface
	interfaceMethods := map[string]bool{}
	err := walkGo(root, []string{"internal/cge", "cmd/synora-core", "cmd/synora-api", "internal/rpc"}, func(pkg string, file *ast.File) {
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
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			variableTypes := map[string]string{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				value, ok := node.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for _, name := range value.Names {
					if value.Type != nil {
						variableTypes[name.Name] = expressionName(value.Type)
					}
				}
				return true
			})
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
					if name, ok := assignment.Lhs[0].(*ast.Ident); ok {
						if composite, ok := assignment.Rhs[0].(*ast.CompositeLit); ok {
							variableTypes[name.Name] = expressionName(composite.Type)
						}
					}
				}
				return true
			})
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := selectorName(call.Fun)
				if name != "Encode" && name != "WriteJSON" && name != "WriteMessage" && name != "Broadcast" {
					return true
				}
				if len(call.Args) == 0 {
					return true
				}
				outputType := expressionName(call.Args[len(call.Args)-1])
				if resolved, ok := variableTypes[outputType]; ok {
					outputType = resolved
				}
				if outputType == "" || !ast.IsExported(outputType) {
					return true
				}
				if outputType != "" {
					transport := "http"
					if name == "WriteJSON" || name == "WriteMessage" || name == "Broadcast" {
						transport = "websocket"
					}
					result = append(result, Surface{Kind: "output", Transport: transport, Origin: "transport", Package: pkg, Function: function.Name.Name, Operation: name, Type: outputType})
				}
				return true
			})
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

func RecursiveReachability(inventory gosurface.Inventory, roots []string) Reachability {
	byKey := make(map[string]gosurface.InventoryType, len(inventory.Types))
	for _, item := range inventory.Types {
		byKey[item.Package+"/"+item.Name] = item
	}
	result := Reachability{Roots: append([]string(nil), roots...), Types: map[string]bool{}, Fields: map[string]bool{}}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if result.Types[key] {
			continue
		}
		item, ok := byKey[key]
		if !ok {
			continue
		}
		result.Types[key] = true
		for _, field := range item.Fields {
			result.Fields[key+"/"+field.FieldPath] = true
			for _, child := range referencedTypeNames(field.GoType, item.Package, byKey) {
				if !result.Types[child] {
					queue = append(queue, child)
				}
			}
		}
	}
	return result
}

func referencedTypeNames(value, currentPackage string, known map[string]gosurface.InventoryType) []string {
	value = strings.TrimSpace(value)
	for {
		switch {
		case strings.HasPrefix(value, "*"):
			value = strings.TrimSpace(value[1:])
		case strings.HasPrefix(value, "[]"):
			value = strings.TrimSpace(value[2:])
		default:
			goto containersDone
		}
	}
containersDone:
	if strings.HasPrefix(value, "[") {
		if index := strings.Index(value, "]"); index >= 0 {
			value = strings.TrimSpace(value[index+1:])
		}
	}
	if strings.HasPrefix(value, "map[") {
		if index := strings.Index(value, "]"); index >= 0 {
			value = strings.TrimSpace(value[index+1:])
		}
	}
	if value == "" || strings.HasPrefix(value, "time.") {
		return nil
	}
	if strings.Contains(value, ".") {
		name := value[strings.LastIndex(value, ".")+1:]
		var result []string
		for key := range known {
			if strings.HasSuffix(key, "/"+name) {
				result = append(result, key)
			}
		}
		sort.Strings(result)
		return result
	}
	if _, ok := known[currentPackage+"/"+value]; ok {
		return []string{currentPackage + "/" + value}
	}
	var result []string
	for key := range known {
		if strings.HasSuffix(key, "/"+value) {
			result = append(result, key)
		}
	}
	if len(result) != 1 {
		return nil
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

func messageType(args []ast.Expr) (string, string) {
	path := ""
	for _, arg := range args {
		if value, ok := stringLiteral(arg); ok {
			path = value
			break
		}
	}
	for _, arg := range args {
		if composite, ok := arg.(*ast.CompositeLit); ok {
			for _, element := range composite.Elts {
				keyValue, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key := selectorName(keyValue.Key)
				if key == "Type" || key == "Kind" {
					if value, ok := stringLiteral(keyValue.Value); ok {
						return value, path
					}
				}
			}
		}
	}
	if path != "" {
		return "payload", path
	}
	return "", ""
}

func uniqueWriteSites(values []WriteSite) []WriteSite {
	seen := map[string]bool{}
	result := make([]WriteSite, 0, len(values))
	for _, value := range values {
		key := value.Package + "/" + value.Function + "/" + value.Operation
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Package+result[i].Function+result[i].Operation < result[j].Package+result[j].Function+result[j].Operation
	})
	return result
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

func handlerExprName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.CallExpr:
		return handlerExprName(value.Fun)
	case *ast.SelectorExpr:
		return value.Sel.Name
	case *ast.Ident:
		return value.Name
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
		key := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s", value.Kind, value.Transport, value.Package, value.Function, value.Operation, value.Method, value.Path, value.Direction, value.Type)
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
