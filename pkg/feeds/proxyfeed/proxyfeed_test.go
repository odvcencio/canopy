package proxyfeed

import (
	"encoding/json"
	"testing"

	"github.com/odvcencio/canopy/pkg/scope"
)

func TestFeedIsNoOp(t *testing.T) {
	f := New()
	graph := scope.NewGraph()
	err := f.Feed(graph, "test.go", nil, nil)
	if err != nil {
		t.Errorf("Feed() should be no-op, got error: %v", err)
	}
}

func TestFeedNamePriority(t *testing.T) {
	f := New()
	if f.Name() != "proxy" {
		t.Errorf("Name() = %q", f.Name())
	}
	if f.Priority() != 70 {
		t.Errorf("Priority() = %d", f.Priority())
	}
}

func TestEnrichFromHover(t *testing.T) {
	f := New()
	graph := scope.NewGraph()
	fs := scope.NewScope(scope.ScopeFile, nil)
	fs.AddDef(scope.Definition{
		Name: "ProcessOrder",
		Kind: scope.DefFunction,
		Loc:  scope.Location{File: "main.go", StartLine: 5, EndLine: 20},
	})
	graph.AddFileScope("main.go", fs)

	hover := map[string]any{
		"contents": map[string]string{
			"kind":  "markdown",
			"value": "```go\nfunc ProcessOrder(ctx context.Context, order Order) error\n```",
		},
	}
	data, _ := json.Marshal(hover)

	f.EnrichFromHover(graph, "main.go", 10, data)

	def := &graph.FileScope("main.go").Defs[0]
	typ, ok := scope.GetMeta[string](def, "proxy.type")
	if !ok {
		t.Fatal("proxy.type not set")
	}
	if typ != "func ProcessOrder(ctx context.Context, order Order) error" {
		t.Errorf("proxy.type = %q", typ)
	}

	doc, ok := scope.GetMeta[string](def, "proxy.documentation")
	if !ok {
		t.Fatal("proxy.documentation not set")
	}
	if doc == "" {
		t.Error("proxy.documentation is empty")
	}
}

func TestEnrichFromHoverNoMatch(t *testing.T) {
	f := New()
	graph := scope.NewGraph()
	fs := scope.NewScope(scope.ScopeFile, nil)
	fs.AddDef(scope.Definition{
		Name: "Foo",
		Kind: scope.DefFunction,
		Loc:  scope.Location{File: "main.go", StartLine: 5, EndLine: 10},
	})
	graph.AddFileScope("main.go", fs)

	hover := map[string]any{
		"contents": map[string]string{
			"kind":  "markdown",
			"value": "```go\nfunc Bar() string\n```",
		},
	}
	data, _ := json.Marshal(hover)

	// Line 50 is outside Foo's range
	f.EnrichFromHover(graph, "main.go", 50, data)

	_, ok := scope.GetMeta[string](&graph.FileScope("main.go").Defs[0], "proxy.type")
	if ok {
		t.Error("should not have set proxy.type for non-matching line")
	}
}

func TestEnrichFromCompletion(t *testing.T) {
	f := New()
	graph := scope.NewGraph()
	fs := scope.NewScope(scope.ScopeFile, nil)
	fs.AddDef(scope.Definition{
		Name: "ProcessOrder",
		Kind: scope.DefFunction,
		Loc:  scope.Location{File: "main.go", StartLine: 5, EndLine: 20},
	})
	fs.AddDef(scope.Definition{
		Name: "Unknown",
		Kind: scope.DefFunction,
		Loc:  scope.Location{File: "main.go", StartLine: 25, EndLine: 30},
	})
	graph.AddFileScope("main.go", fs)

	items := []CompletionItem{
		{Label: "ProcessOrder", Detail: "func(ctx, order) error", Kind: 3},
		{Label: "OtherFunc", Detail: "func() string", Kind: 3},
	}

	f.EnrichFromCompletion(graph, "main.go", items)

	typ, ok := scope.GetMeta[string](&graph.FileScope("main.go").Defs[0], "proxy.type")
	if !ok || typ != "func(ctx, order) error" {
		t.Errorf("ProcessOrder proxy.type = %q, ok=%v", typ, ok)
	}

	// Unknown has no matching completion item
	_, ok = scope.GetMeta[string](&graph.FileScope("main.go").Defs[1], "proxy.type")
	if ok {
		t.Error("Unknown should not have proxy.type")
	}
}

func TestExtractType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"```go\nfunc Foo() error\n```", "func Foo() error"},
		{"```\nsome type\n```", "some type"},
		{"plain text", "plain text"},
		{"---\nfoo", "foo"},
	}
	for _, tt := range tests {
		got := extractType(tt.input)
		if got != tt.want {
			t.Errorf("extractType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
