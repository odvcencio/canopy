package complexity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

func TestAnalyzeEmpty(t *testing.T) {
	idx := &model.Index{
		Version:     "1",
		Root:        t.TempDir(),
		GeneratedAt: time.Now(),
		Files:       nil,
	}

	report, err := Analyze(idx, idx.Root, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 0 {
		t.Fatalf("expected 0 functions, got %d", len(report.Functions))
	}
	if report.Summary.Count != 0 {
		t.Fatalf("expected summary count 0, got %d", report.Summary.Count)
	}
}

func TestAnalyzeNilIndex(t *testing.T) {
	report, err := Analyze(nil, "", Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 0 {
		t.Fatalf("expected 0 functions, got %d", len(report.Functions))
	}
}

func TestAnalyzeSimpleFunction(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func hello() string {
	return "hello world"
}
`
	path := filepath.Join(dir, "simple.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "hello",
						Signature: "func hello() string",
						StartLine: 3,
						EndLine:   5,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(report.Functions))
	}

	fn := report.Functions[0]
	if fn.Name != "hello" {
		t.Errorf("expected name 'hello', got %q", fn.Name)
	}
	if fn.Cyclomatic != 1 {
		t.Errorf("expected cyclomatic=1 (no branching), got %d", fn.Cyclomatic)
	}
	if fn.Cognitive != 0 {
		t.Errorf("expected cognitive=0 (no branching), got %d", fn.Cognitive)
	}
	if fn.MaxNesting != 0 {
		t.Errorf("expected max_nesting=0, got %d", fn.MaxNesting)
	}
	if fn.Lines < 1 {
		t.Errorf("expected lines >= 1, got %d", fn.Lines)
	}
}

func TestAnalyzeBranchingFunction(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func complex(x int, y bool) int {
	if x > 0 && y {
		for i := 0; i < x; i++ {
			if i%2 == 0 {
				x++
			}
		}
	}
	return x
}
`
	path := filepath.Join(dir, "branching.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "complex",
						Signature: "func complex(x int, y bool) int",
						StartLine: 3,
						EndLine:   12,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(report.Functions))
	}

	fn := report.Functions[0]
	// if + && + for + inner if = at least 3 branching nodes + 1 logical op
	if fn.Cyclomatic < 3 {
		t.Errorf("expected cyclomatic >= 3, got %d", fn.Cyclomatic)
	}
	if fn.MaxNesting < 2 {
		t.Errorf("expected max_nesting >= 2, got %d", fn.MaxNesting)
	}
	if fn.Cognitive < fn.Cyclomatic {
		t.Errorf("expected cognitive >= cyclomatic (%d), got cognitive=%d", fn.Cyclomatic, fn.Cognitive)
	}
	if fn.Parameters != 2 {
		t.Errorf("expected parameters=2, got %d", fn.Parameters)
	}
}

func TestAnalyzePython(t *testing.T) {
	dir := t.TempDir()
	src := `def process(items):
    for item in items:
        if item > 0:
            print(item)
`
	path := filepath.Join(dir, "process.py")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "python",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "process",
						Signature: "def process(items)",
						StartLine: 1,
						EndLine:   4,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(report.Functions))
	}

	fn := report.Functions[0]
	// for + if = at least 2 branching nodes
	if fn.Cyclomatic < 2 {
		t.Errorf("expected cyclomatic >= 2, got %d", fn.Cyclomatic)
	}
	if fn.Language != "python" {
		t.Errorf("expected language 'python', got %q", fn.Language)
	}
}

func TestSummaryStats(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func one() {
	if true {
	}
}

func two() {
	for i := 0; i < 10; i++ {
		if i > 5 {
		}
	}
}
`
	path := filepath.Join(dir, "two_funcs.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "one",
						Signature: "func one()",
						StartLine: 3,
						EndLine:   6,
					},
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "two",
						Signature: "func two()",
						StartLine: 8,
						EndLine:   13,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Summary.Count != 2 {
		t.Fatalf("expected summary count=2, got %d", report.Summary.Count)
	}
	if report.Summary.AvgCyclomatic <= 0 {
		t.Errorf("expected positive avg_cyclomatic, got %f", report.Summary.AvgCyclomatic)
	}
	if report.Summary.MaxCyclomatic < 1 {
		t.Errorf("expected max_cyclomatic >= 1, got %d", report.Summary.MaxCyclomatic)
	}
	if report.Summary.AvgLines <= 0 {
		t.Errorf("expected positive avg_lines, got %f", report.Summary.AvgLines)
	}
}

func TestEnrichWithXref(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func caller() {
	callee()
}

func callee() {
}
`
	path := filepath.Join(dir, "enrich.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	relPath := "enrich.go"
	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     relPath,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      relPath,
						Kind:      "function_definition",
						Name:      "caller",
						Signature: "func caller()",
						StartLine: 3,
						EndLine:   5,
					},
					{
						File:      relPath,
						Kind:      "function_definition",
						Name:      "callee",
						Signature: "func callee()",
						StartLine: 7,
						EndLine:   8,
					},
				},
				References: []model.Reference{
					{
						File:      relPath,
						Kind:      "reference.call",
						Name:      "callee",
						StartLine: 4,
						EndLine:   4,
					},
				},
			},
		},
	}

	graph, err := xref.Build(idx)
	if err != nil {
		t.Fatalf("xref.Build failed: %v", err)
	}

	report, err := Analyze(idx, dir, Options{})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	EnrichWithXref(report, graph)

	// Find the caller function — should have fan_out >= 1.
	var callerFn *FunctionMetrics
	for i := range report.Functions {
		if report.Functions[i].Name == "caller" {
			callerFn = &report.Functions[i]
			break
		}
	}
	if callerFn == nil {
		t.Fatal("caller function not found in report")
	}
	if callerFn.FanOut < 1 {
		t.Errorf("expected caller fan_out >= 1, got %d", callerFn.FanOut)
	}

	// Find the callee function — should have fan_in >= 1.
	var calleeFn *FunctionMetrics
	for i := range report.Functions {
		if report.Functions[i].Name == "callee" {
			calleeFn = &report.Functions[i]
			break
		}
	}
	if calleeFn == nil {
		t.Fatal("callee function not found in report")
	}
	if calleeFn.FanIn < 1 {
		t.Errorf("expected callee fan_in >= 1, got %d", calleeFn.FanIn)
	}
}

func TestCountParameters(t *testing.T) {
	tests := []struct {
		sig  string
		want int
	}{
		{"func hello()", 0},
		{"func hello(x int)", 1},
		{"func hello(x int, y string)", 2},
		{"func hello(x int, y string, z bool)", 3},
		{"def process(items)", 1},
		{"", 0},
		{"noparens", 0},
	}
	for _, tc := range tests {
		got := countParameters(tc.sig)
		if got != tc.want {
			t.Errorf("countParameters(%q) = %d, want %d", tc.sig, got, tc.want)
		}
	}
}

func TestSortOptions(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func a() {
}

func b() {
	if true {
		for i := 0; i < 10; i++ {
		}
	}
}
`
	path := filepath.Join(dir, "sort.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "a",
						Signature: "func a()",
						StartLine: 3,
						EndLine:   4,
					},
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "b",
						Signature: "func b()",
						StartLine: 6,
						EndLine:   11,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, Options{Sort: "cyclomatic"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) < 2 {
		t.Fatalf("expected at least 2 functions, got %d", len(report.Functions))
	}
	// First function should have higher cyclomatic (descending sort).
	if report.Functions[0].Cyclomatic < report.Functions[1].Cyclomatic {
		t.Errorf("expected descending sort by cyclomatic: %d < %d",
			report.Functions[0].Cyclomatic, report.Functions[1].Cyclomatic)
	}
}

func TestMinCyclomaticFilter(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func simple() {}

func branching() {
	if true {
		for i := 0; i < 10; i++ {
		}
	}
}
`
	path := filepath.Join(dir, "filter.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "simple",
						Signature: "func simple()",
						StartLine: 3,
						EndLine:   3,
					},
					{
						File:      path,
						Kind:      "function_definition",
						Name:      "branching",
						Signature: "func branching()",
						StartLine: 5,
						EndLine:   10,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, Options{MinCyclomatic: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, fn := range report.Functions {
		if fn.Cyclomatic < 2 {
			t.Errorf("function %q has cyclomatic=%d but min filter was 2", fn.Name, fn.Cyclomatic)
		}
	}
}
