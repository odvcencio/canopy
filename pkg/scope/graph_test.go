package scope

import "testing"

func TestNewGraph(t *testing.T) {
	g := NewGraph()
	if g.FileScopes == nil {
		t.Fatal("NewGraph: FileScopes map is nil")
	}
	if g.Packages == nil {
		t.Fatal("NewGraph: Packages map is nil")
	}
	if len(g.FileScopes) != 0 {
		t.Fatalf("NewGraph: FileScopes should be empty, got %d", len(g.FileScopes))
	}
	if len(g.Packages) != 0 {
		t.Fatalf("NewGraph: Packages should be empty, got %d", len(g.Packages))
	}
}

func TestScopeAddDef(t *testing.T) {
	s := NewScope(ScopeFunction, nil)

	def := Definition{
		Name:      "x",
		Kind:      DefVariable,
		TypeAnnot: "int",
		Loc: Location{
			File:      "main.go",
			StartLine: 10,
			EndLine:   10,
			StartCol:  1,
			EndCol:    5,
		},
	}
	s.AddDef(def)

	if len(s.Defs) != 1 {
		t.Fatalf("AddDef: expected 1 def, got %d", len(s.Defs))
	}
	if s.Defs[0].Name != "x" {
		t.Fatalf("AddDef: expected name 'x', got %q", s.Defs[0].Name)
	}
	if s.Defs[0].Kind != DefVariable {
		t.Fatalf("AddDef: expected kind %q, got %q", DefVariable, s.Defs[0].Kind)
	}
	if s.Defs[0].TypeAnnot != "int" {
		t.Fatalf("AddDef: expected TypeAnnot 'int', got %q", s.Defs[0].TypeAnnot)
	}

	// Add a second def to ensure append works.
	s.AddDef(Definition{
		Name: "y",
		Kind: DefParam,
		Loc: Location{
			File:      "main.go",
			StartLine: 11,
			EndLine:   11,
			StartCol:  1,
			EndCol:    5,
		},
	})
	if len(s.Defs) != 2 {
		t.Fatalf("AddDef: expected 2 defs, got %d", len(s.Defs))
	}
	if s.Defs[1].Name != "y" {
		t.Fatalf("AddDef: second def name should be 'y', got %q", s.Defs[1].Name)
	}
}

func TestScopeAddRef(t *testing.T) {
	s := NewScope(ScopeBlock, nil)

	ref := Ref{
		Name: "fmt",
		Loc: Location{
			File:      "main.go",
			StartLine: 5,
			EndLine:   5,
			StartCol:  2,
			EndCol:    5,
		},
	}
	s.AddRef(ref)

	if len(s.Refs) != 1 {
		t.Fatalf("AddRef: expected 1 ref, got %d", len(s.Refs))
	}
	if s.Refs[0].Name != "fmt" {
		t.Fatalf("AddRef: expected name 'fmt', got %q", s.Refs[0].Name)
	}

	// Add a dotted ref (e.g. fmt.Println).
	s.AddRef(Ref{
		Name:   "fmt",
		Member: "Println",
		Loc: Location{
			File:      "main.go",
			StartLine: 6,
			EndLine:   6,
			StartCol:  2,
			EndCol:    13,
		},
	})
	if len(s.Refs) != 2 {
		t.Fatalf("AddRef: expected 2 refs, got %d", len(s.Refs))
	}
	if s.Refs[1].Member != "Println" {
		t.Fatalf("AddRef: expected Member 'Println', got %q", s.Refs[1].Member)
	}
}

func TestScopeNesting(t *testing.T) {
	parent := NewScope(ScopeFile, nil)
	child := NewScope(ScopeFunction, parent)

	// Child should reference parent.
	if child.Parent != parent {
		t.Fatal("ScopeNesting: child.Parent should be parent")
	}

	// Parent should contain child.
	if len(parent.Children) != 1 {
		t.Fatalf("ScopeNesting: parent should have 1 child, got %d", len(parent.Children))
	}
	if parent.Children[0] != child {
		t.Fatal("ScopeNesting: parent.Children[0] should be child")
	}

	// Parent should have no parent.
	if parent.Parent != nil {
		t.Fatal("ScopeNesting: root parent should have nil Parent")
	}

	// Add a second child.
	child2 := NewScope(ScopeBlock, parent)
	if len(parent.Children) != 2 {
		t.Fatalf("ScopeNesting: parent should have 2 children, got %d", len(parent.Children))
	}
	if child2.Parent != parent {
		t.Fatal("ScopeNesting: child2.Parent should be parent")
	}

	// Verify kinds are preserved.
	if parent.Kind != ScopeFile {
		t.Fatalf("ScopeNesting: parent kind should be ScopeFile, got %v", parent.Kind)
	}
	if child.Kind != ScopeFunction {
		t.Fatalf("ScopeNesting: child kind should be ScopeFunction, got %v", child.Kind)
	}
	if child2.Kind != ScopeBlock {
		t.Fatalf("ScopeNesting: child2 kind should be ScopeBlock, got %v", child2.Kind)
	}

	// Grandchild nesting.
	grandchild := NewScope(ScopeBlock, child)
	if grandchild.Parent != child {
		t.Fatal("ScopeNesting: grandchild.Parent should be child")
	}
	if len(child.Children) != 1 {
		t.Fatalf("ScopeNesting: child should have 1 child, got %d", len(child.Children))
	}
}

func TestGraphAddFileScope(t *testing.T) {
	g := NewGraph()

	s := NewScope(ScopeFile, nil)
	s.AddDef(Definition{
		Name: "main",
		Kind: DefFunction,
		Loc: Location{
			File:      "main.go",
			StartLine: 3,
			EndLine:   10,
			StartCol:  1,
			EndCol:    1,
		},
	})

	g.AddFileScope("main.go", s)

	got := g.FileScope("main.go")
	if got == nil {
		t.Fatal("GraphAddFileScope: FileScope returned nil for 'main.go'")
	}
	if got != s {
		t.Fatal("GraphAddFileScope: FileScope returned wrong scope")
	}
	if len(got.Defs) != 1 {
		t.Fatalf("GraphAddFileScope: expected 1 def, got %d", len(got.Defs))
	}
	if got.Defs[0].Name != "main" {
		t.Fatalf("GraphAddFileScope: expected def name 'main', got %q", got.Defs[0].Name)
	}

	// Retrieve a file that doesn't exist.
	missing := g.FileScope("nonexistent.go")
	if missing != nil {
		t.Fatal("GraphAddFileScope: FileScope should return nil for unknown file")
	}

	// Overwrite an existing file scope.
	s2 := NewScope(ScopeFile, nil)
	g.AddFileScope("main.go", s2)
	got2 := g.FileScope("main.go")
	if got2 != s2 {
		t.Fatal("GraphAddFileScope: overwriting file scope should replace the old one")
	}
}

func TestGraphAddPackageScope(t *testing.T) {
	g := NewGraph()

	s := NewScope(ScopePackage, nil)
	s.AddDef(Definition{
		Name: "Handler",
		Kind: DefType,
		Loc: Location{
			File:      "handler.go",
			StartLine: 5,
			EndLine:   20,
			StartCol:  1,
			EndCol:    1,
		},
	})

	g.AddPackageScope("net/http", s)

	got := g.PackageScope("net/http")
	if got == nil {
		t.Fatal("GraphAddPackageScope: PackageScope returned nil for 'net/http'")
	}
	if got != s {
		t.Fatal("GraphAddPackageScope: PackageScope returned wrong scope")
	}

	missing := g.PackageScope("nonexistent")
	if missing != nil {
		t.Fatal("GraphAddPackageScope: PackageScope should return nil for unknown package")
	}
}

func TestDefinitionWithChildScope(t *testing.T) {
	fileScope := NewScope(ScopeFile, nil)
	funcScope := NewScope(ScopeFunction, fileScope)

	def := Definition{
		Name:  "processData",
		Kind:  DefFunction,
		Scope: funcScope,
		Loc: Location{
			File:      "data.go",
			StartLine: 10,
			EndLine:   25,
			StartCol:  1,
			EndCol:    1,
		},
	}
	fileScope.AddDef(def)

	if fileScope.Defs[0].Scope != funcScope {
		t.Fatal("Definition.Scope should point to the child function scope")
	}
	if fileScope.Defs[0].Scope.Kind != ScopeFunction {
		t.Fatalf("Definition.Scope.Kind should be ScopeFunction, got %v", fileScope.Defs[0].Scope.Kind)
	}
}

func TestImportDefinition(t *testing.T) {
	s := NewScope(ScopeFile, nil)
	s.AddDef(Definition{
		Name:       "fmt",
		Kind:       DefImport,
		ImportPath: "fmt",
		Loc: Location{
			File:      "main.go",
			StartLine: 3,
			EndLine:   3,
			StartCol:  2,
			EndCol:    7,
		},
	})

	if s.Defs[0].ImportPath != "fmt" {
		t.Fatalf("ImportPath should be 'fmt', got %q", s.Defs[0].ImportPath)
	}
	if s.Defs[0].Kind != DefImport {
		t.Fatalf("Kind should be DefImport, got %q", s.Defs[0].Kind)
	}
}

func TestScopeKindConstants(t *testing.T) {
	kinds := []ScopeKind{
		ScopeFile,
		ScopePackage,
		ScopeModule,
		ScopeClass,
		ScopeFunction,
		ScopeBlock,
	}
	seen := make(map[ScopeKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("ScopeKind %v is duplicated", k)
		}
		seen[k] = true
	}
}

func TestDefinitionTypeFields(t *testing.T) {
	def := Definition{
		Name:        "ProcessOrder",
		Kind:        DefFunction,
		ReturnType:  "error",
		Receiver:    "*OrderService",
		BaseClasses: []string{"BaseHandler"},
		TypeParams:  []string{"T", "K"},
	}
	if def.ReturnType != "error" {
		t.Errorf("ReturnType = %q, want %q", def.ReturnType, "error")
	}
	if def.Receiver != "*OrderService" {
		t.Errorf("Receiver = %q, want %q", def.Receiver, "*OrderService")
	}
	if len(def.BaseClasses) != 1 || def.BaseClasses[0] != "BaseHandler" {
		t.Errorf("BaseClasses = %v, want [BaseHandler]", def.BaseClasses)
	}
	if len(def.TypeParams) != 2 {
		t.Errorf("TypeParams = %v, want [T K]", def.TypeParams)
	}
}

func TestSetMetaInitializesMap(t *testing.T) {
	def := Definition{Name: "foo"}
	SetMeta(&def, "vcs.author", "alice")
	v, ok := GetMeta[string](&def, "vcs.author")
	if !ok || v != "alice" {
		t.Errorf("GetMeta = (%q, %v), want (alice, true)", v, ok)
	}
}

func TestGetMetaMissingKey(t *testing.T) {
	def := Definition{Name: "foo"}
	v, ok := GetMeta[string](&def, "nonexistent")
	if ok || v != "" {
		t.Errorf("GetMeta = (%q, %v), want ('', false)", v, ok)
	}
}

func TestGetMetaWrongType(t *testing.T) {
	def := Definition{Name: "foo"}
	SetMeta(&def, "count", 42)
	v, ok := GetMeta[string](&def, "count")
	if ok || v != "" {
		t.Errorf("GetMeta[string] on int = (%q, %v), want ('', false)", v, ok)
	}
	n, ok := GetMeta[int](&def, "count")
	if !ok || n != 42 {
		t.Errorf("GetMeta[int] = (%d, %v), want (42, true)", n, ok)
	}
}

func TestSetMetaOverwrites(t *testing.T) {
	def := Definition{Name: "foo"}
	SetMeta(&def, "key", "v1")
	SetMeta(&def, "key", "v2")
	v, _ := GetMeta[string](&def, "key")
	if v != "v2" {
		t.Errorf("GetMeta = %q, want v2", v)
	}
}

func TestDefKindConstants(t *testing.T) {
	defKinds := []string{
		DefFunction,
		DefMethod,
		DefVariable,
		DefParam,
		DefType,
		DefClass,
		DefImport,
		DefConstant,
		DefField,
		DefInterface,
	}
	seen := make(map[string]bool)
	for _, k := range defKinds {
		if k == "" {
			t.Fatal("DefKind constant should not be empty")
		}
		if seen[k] {
			t.Fatalf("DefKind %q is duplicated", k)
		}
		seen[k] = true
	}
}
