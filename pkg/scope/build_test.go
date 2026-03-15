package scope

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func mustParseGo(t *testing.T, src string) (*gotreesitter.Tree, *gotreesitter.Language) {
	t.Helper()
	entry := grammars.DetectLanguage("test.go")
	if entry == nil {
		t.Fatal("Go grammar not found")
	}
	lang := entry.Language()
	parser := gotreesitter.NewParser(lang)
	srcBytes := []byte(src)
	ts := entry.TokenSourceFactory(srcBytes, lang)
	tree, err := parser.ParseWithTokenSource(srcBytes, ts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree, lang
}

func TestBuildGoFunctionDef(t *testing.T) {
	src := `package main

func hello() {
}
`
	tree, lang := mustParseGo(t, src)
	rules, err := LoadRules("go", lang)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	scope := BuildFileScope(tree, lang, []byte(src), rules, "main.go")

	// Should have a definition for "hello"
	found := false
	for _, d := range scope.Defs {
		if d.Name == "hello" && d.Kind == DefFunction {
			found = true
		}
	}
	if !found {
		t.Errorf("expected definition for 'hello', got defs: %+v", scope.Defs)
	}
}

func TestBuildGoImport(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println("hi")
}
`
	tree, lang := mustParseGo(t, src)
	rules, err := LoadRules("go", lang)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	scope := BuildFileScope(tree, lang, []byte(src), rules, "main.go")

	// Should have import def for "fmt"
	found := false
	for _, d := range scope.Defs {
		if d.Kind == DefImport && d.ImportPath == `"fmt"` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected import def for fmt, got defs: %+v", scope.Defs)
	}
}

func TestBuildGoVarWithType(t *testing.T) {
	src := `package main

var x int
`
	tree, lang := mustParseGo(t, src)
	rules, err := LoadRules("go", lang)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	scope := BuildFileScope(tree, lang, []byte(src), rules, "main.go")

	found := false
	for _, d := range scope.Defs {
		if d.Name == "x" && d.Kind == DefVariable {
			found = true
			if d.TypeAnnot == "" {
				t.Error("expected type annotation 'int'")
			}
		}
	}
	if !found {
		t.Errorf("expected variable def for x")
	}
}

func TestProcessMatchTypeCaptures(t *testing.T) {
	// Simulate captures that would come from type-aware .scm rules
	fileScope := NewScope(ScopeFile, nil)
	seen := make(map[defKey]bool)

	// Simulate: function with return type
	// @def.function captures "MyFunc", then @def.function.return captures "error"
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.function", text: "MyFunc", startLine: 5, endLine: 10},
	}, "test.go", seen)
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.function.return", text: "error", startLine: 5, endLine: 5},
	}, "test.go", seen)

	if len(fileScope.Defs) < 1 {
		t.Fatal("expected at least 1 def")
	}
	if fileScope.Defs[0].ReturnType != "error" {
		t.Errorf("ReturnType = %q, want error", fileScope.Defs[0].ReturnType)
	}

	// Simulate: method with receiver
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.method", text: "DoWork", startLine: 15, endLine: 20},
	}, "test.go", seen)
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.method.receiver", text: "*Server", startLine: 15, endLine: 15},
	}, "test.go", seen)

	methodIdx := -1
	for i, d := range fileScope.Defs {
		if d.Name == "DoWork" {
			methodIdx = i
			break
		}
	}
	if methodIdx < 0 {
		t.Fatal("DoWork not found")
	}
	if fileScope.Defs[methodIdx].Receiver != "*Server" {
		t.Errorf("Receiver = %q, want *Server", fileScope.Defs[methodIdx].Receiver)
	}

	// Simulate: class with extends
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.class", text: "MyClass", startLine: 25, endLine: 30},
	}, "test.go", seen)
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.class.extends", text: "BaseClass", startLine: 25, endLine: 25},
	}, "test.go", seen)

	classIdx := -1
	for i, d := range fileScope.Defs {
		if d.Name == "MyClass" {
			classIdx = i
			break
		}
	}
	if classIdx < 0 {
		t.Fatal("MyClass not found")
	}
	if len(fileScope.Defs[classIdx].BaseClasses) != 1 || fileScope.Defs[classIdx].BaseClasses[0] != "BaseClass" {
		t.Errorf("BaseClasses = %v, want [BaseClass]", fileScope.Defs[classIdx].BaseClasses)
	}

	// Simulate: field with type
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.field", text: "Name", startLine: 35, endLine: 35},
	}, "test.go", seen)
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.field.type", text: "string", startLine: 35, endLine: 35},
	}, "test.go", seen)

	fieldIdx := -1
	for i, d := range fileScope.Defs {
		if d.Name == "Name" && d.Kind == DefField {
			fieldIdx = i
			break
		}
	}
	if fieldIdx < 0 {
		t.Fatal("field Name not found")
	}
	if fileScope.Defs[fieldIdx].TypeAnnot != "string" {
		t.Errorf("field TypeAnnot = %q, want string", fileScope.Defs[fieldIdx].TypeAnnot)
	}

	// Simulate: param with type
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.param", text: "ctx", startLine: 40, endLine: 40},
	}, "test.go", seen)
	processMatchCaptures(fileScope, []testCapture{
		{name: "def.param.type", text: "context.Context", startLine: 40, endLine: 40},
	}, "test.go", seen)

	paramIdx := -1
	for i, d := range fileScope.Defs {
		if d.Name == "ctx" && d.Kind == DefParam {
			paramIdx = i
			break
		}
	}
	if paramIdx < 0 {
		t.Fatal("param ctx not found")
	}
	if fileScope.Defs[paramIdx].TypeAnnot != "context.Context" {
		t.Errorf("param TypeAnnot = %q, want context.Context", fileScope.Defs[paramIdx].TypeAnnot)
	}
}

type testCapture struct {
	name      string
	text      string
	startLine int
	endLine   int
}

// processMatchCaptures is a test helper that simulates processCaptures with fake captures.
func processMatchCaptures(fileScope *Scope, caps []testCapture, path string, seen map[defKey]bool) {
	cd := make([]capData, len(caps))
	for i, c := range caps {
		cd[i] = capData{
			name: c.name,
			text: c.text,
			loc: Location{
				File:      path,
				StartLine: c.startLine,
				EndLine:   c.endLine,
			},
		}
	}
	processCaptures(fileScope, cd, seen)
}
