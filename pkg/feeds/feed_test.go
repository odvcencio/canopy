package feeds

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/odvcencio/canopy/pkg/scope"
)

type mockFeed struct {
	name     string
	lang     string
	priority int
	calls    []string
	orderLog *[]string
	err      error
}

func (m *mockFeed) Name() string             { return m.name }
func (m *mockFeed) Supports(lang string) bool { return m.lang == "" || m.lang == lang }
func (m *mockFeed) Priority() int             { return m.priority }
func (m *mockFeed) Feed(graph *scope.Graph, file string, src []byte, ctx *FeedContext) error {
	m.calls = append(m.calls, file)
	if m.orderLog != nil {
		*m.orderLog = append(*m.orderLog, m.name)
	}
	if m.err != nil {
		return m.err
	}
	fs := graph.FileScope(file)
	if fs != nil && len(fs.Defs) > 0 {
		scope.SetMeta(&fs.Defs[0], m.name+".ran", true)
	}
	return nil
}

func TestFeedEngineRunsInPriorityOrder(t *testing.T) {
	graph := scope.NewGraph()
	fs := scope.NewScope(scope.ScopeFile, nil)
	fs.AddDef(scope.Definition{Name: "Foo", Kind: scope.DefFunction})
	graph.AddFileScope("main.go", fs)

	var order []string
	f1 := &mockFeed{name: "alpha", priority: 10, orderLog: &order}
	f2 := &mockFeed{name: "beta", priority: 20, orderLog: &order}

	engine := NewEngine(slog.Default())
	engine.Register(f2)
	engine.Register(f1)

	ctx := &FeedContext{WorkspaceRoot: "/tmp"}
	engine.RunFile(graph, "main.go", nil, "go", ctx)

	if len(order) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(order))
	}
	if order[0] != "alpha" {
		t.Errorf("order[0] = %q, want alpha (priority 10 first)", order[0])
	}
	if order[1] != "beta" {
		t.Errorf("order[1] = %q, want beta (priority 20 second)", order[1])
	}

	ran1, ok1 := scope.GetMeta[bool](&graph.FileScope("main.go").Defs[0], "alpha.ran")
	ran2, ok2 := scope.GetMeta[bool](&graph.FileScope("main.go").Defs[0], "beta.ran")
	if !ok1 || !ran1 {
		t.Error("alpha feed did not enrich graph")
	}
	if !ok2 || !ran2 {
		t.Error("beta feed did not enrich graph")
	}
}

func TestFeedEngineSkipsUnsupportedLanguage(t *testing.T) {
	graph := scope.NewGraph()
	fs := scope.NewScope(scope.ScopeFile, nil)
	fs.AddDef(scope.Definition{Name: "Foo", Kind: scope.DefFunction})
	graph.AddFileScope("main.py", fs)

	goOnly := &mockFeed{name: "go-only", lang: "go", priority: 10}
	engine := NewEngine(slog.Default())
	engine.Register(goOnly)

	ctx := &FeedContext{WorkspaceRoot: "/tmp"}
	engine.RunFile(graph, "main.py", nil, "python", ctx)

	if len(goOnly.calls) != 0 {
		t.Error("go-only feed should not run for python files")
	}
}

func TestFeedEngineHandlesErrors(t *testing.T) {
	graph := scope.NewGraph()
	fs := scope.NewScope(scope.ScopeFile, nil)
	fs.AddDef(scope.Definition{Name: "Foo", Kind: scope.DefFunction})
	graph.AddFileScope("main.go", fs)

	errFeed := &mockFeed{name: "broken", priority: 10, err: errTestFeed}
	goodFeed := &mockFeed{name: "good", priority: 20}

	engine := NewEngine(slog.Default())
	engine.Register(errFeed)
	engine.Register(goodFeed)

	ctx := &FeedContext{WorkspaceRoot: "/tmp"}
	engine.RunFile(graph, "main.go", nil, "go", ctx)

	if len(goodFeed.calls) != 1 {
		t.Error("good feed should still run after broken feed errors")
	}
}

var errTestFeed = fmt.Errorf("test feed error")
