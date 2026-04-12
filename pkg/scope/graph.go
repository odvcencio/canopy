// Package scope provides the scope graph data model for the canopyls LSP server.
// A scope graph represents lexical scoping structure: definitions introduced
// in each scope, references that need resolution, and parent/child nesting.
package scope

// ScopeKind classifies the type of lexical scope.
type ScopeKind int

const (
	ScopeFile ScopeKind = iota
	ScopePackage
	ScopeModule
	ScopeClass
	ScopeFunction
	ScopeBlock
)

// DefKind constants classify the type of a definition.
const (
	DefFunction  = "function"
	DefMethod    = "method"
	DefVariable  = "variable"
	DefParam     = "param"
	DefType      = "type"
	DefClass     = "class"
	DefImport    = "import"
	DefConstant  = "constant"
	DefField     = "field"
	DefInterface = "interface"
)

// Location identifies a span in a source file.
type Location struct {
	File      string
	StartLine int
	EndLine   int
	StartCol  int
	EndCol    int
}

// Definition is a named symbol introduced into a scope.
type Definition struct {
	Name       string
	Kind       string         // one of the Def* constants
	TypeAnnot  string         // explicit type annotation if present
	ImportPath string         // for import definitions
	Loc        Location
	Scope      *Scope         // child scope this definition creates (e.g. function body)

	// Type intelligence
	ReturnType  string        // function/method return type
	BaseClasses []string      // inheritance chain
	Receiver    string        // method receiver type
	TypeParams  []string      // generics: T, K, V

	// Feed metadata (any feed can write, LSP handlers read)
	Metadata    map[string]any
}

// Ref is a use of a name that needs resolution.
type Ref struct {
	Name     string
	Member   string      // for dotted access: foo.Bar -> Member="Bar"
	Loc      Location
	Resolved *Definition // populated by the resolution pass
}

// Scope represents a lexical scope containing definitions, references,
// and nested child scopes.
type Scope struct {
	Kind     ScopeKind
	Parent   *Scope
	Children []*Scope
	Defs     []Definition
	Refs     []Ref
}

// NewScope creates a new scope of the given kind and attaches it as a child
// of the parent scope. If parent is nil, the scope is a root.
func NewScope(kind ScopeKind, parent *Scope) *Scope {
	s := &Scope{
		Kind:   kind,
		Parent: parent,
	}
	if parent != nil {
		parent.Children = append(parent.Children, s)
	}
	return s
}

// AddDef adds a definition to this scope.
func (s *Scope) AddDef(def Definition) {
	s.Defs = append(s.Defs, def)
}

// AddRef adds a reference to this scope.
func (s *Scope) AddRef(ref Ref) {
	s.Refs = append(s.Refs, ref)
}

// Graph holds all scope trees for a project, indexed by file path and
// package import path.
type Graph struct {
	FileScopes map[string]*Scope
	Packages   map[string]*Scope
}

// NewGraph creates an empty scope graph.
func NewGraph() *Graph {
	return &Graph{
		FileScopes: make(map[string]*Scope),
		Packages:   make(map[string]*Scope),
	}
}

// AddFileScope stores a file-level scope for the given file path.
func (g *Graph) AddFileScope(path string, s *Scope) {
	g.FileScopes[path] = s
}

// FileScope retrieves the scope for the given file path, or nil if not found.
func (g *Graph) FileScope(path string) *Scope {
	return g.FileScopes[path]
}

// AddPackageScope stores a package-level scope for the given import path.
func (g *Graph) AddPackageScope(importPath string, s *Scope) {
	g.Packages[importPath] = s
}

// PackageScope retrieves the scope for the given import path, or nil if not found.
func (g *Graph) PackageScope(importPath string) *Scope {
	return g.Packages[importPath]
}

// GetMeta retrieves a typed value from definition metadata.
// Returns zero value and false if the key is missing or wrong type.
func GetMeta[T any](d *Definition, key string) (T, bool) {
	if d.Metadata == nil {
		var zero T
		return zero, false
	}
	v, ok := d.Metadata[key]
	if !ok {
		var zero T
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}

// SetMeta writes a value to definition metadata, initializing the map if needed.
func SetMeta(d *Definition, key string, value any) {
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata[key] = value
}
