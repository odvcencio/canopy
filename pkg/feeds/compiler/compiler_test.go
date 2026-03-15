package compiler

import (
	"testing"

	"github.com/odvcencio/gts-suite/pkg/scope"
)

func TestParseColonFormat(t *testing.T) {
	output := []byte(`main.go:10:5: undefined: Foo
main.go:25:12: cannot use x as type y
other.go:5:1: some error
`)
	diags := parseColonFormat(output, "main.go", "go vet")
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics for main.go, got %d", len(diags))
	}
	if diags[0].Line != 10 {
		t.Errorf("diag[0].Line = %d, want 10", diags[0].Line)
	}
	if diags[0].Message != "undefined: Foo" {
		t.Errorf("diag[0].Message = %q", diags[0].Message)
	}
	if diags[1].Line != 25 {
		t.Errorf("diag[1].Line = %d, want 25", diags[1].Line)
	}
}

func TestParseTSC(t *testing.T) {
	output := []byte(`app.ts(10,5): error TS2304: Cannot find name 'foo'
app.ts(20,1): warning TS6133: 'x' is declared but never used
other.ts(5,1): error TS1005: ';' expected
`)
	diags := parseTSC(output, "app.ts")
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics for app.ts, got %d", len(diags))
	}
	if diags[0].Line != 10 || diags[0].Severity != "error" {
		t.Errorf("diag[0] = line %d severity %q", diags[0].Line, diags[0].Severity)
	}
	if diags[1].Line != 20 || diags[1].Severity != "warning" {
		t.Errorf("diag[1] = line %d severity %q", diags[1].Line, diags[1].Severity)
	}
}

func TestParseColonFormatWarning(t *testing.T) {
	output := []byte(`main.py:5:1: warning: unused import
`)
	diags := parseColonFormat(output, "main.py", "mypy")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", diags[0].Severity)
	}
	if diags[0].Message != "unused import" {
		t.Errorf("message = %q, want 'unused import'", diags[0].Message)
	}
}

func TestFeedName(t *testing.T) {
	f := &Feed{specs: map[string]*CompilerSpec{"go": {}}}
	if f.Name() != "compiler" {
		t.Errorf("Name() = %q", f.Name())
	}
	if f.Priority() != 60 {
		t.Errorf("Priority() = %d", f.Priority())
	}
}

func TestFeedSupports(t *testing.T) {
	f := &Feed{specs: map[string]*CompilerSpec{"go": {}, "python": {}}}
	if !f.Supports("go") {
		t.Error("should support go")
	}
	if !f.Supports("python") {
		t.Error("should support python")
	}
	if f.Supports("rust") {
		t.Error("should not support rust (not configured)")
	}
}

func TestFeedSkipsNoScope(t *testing.T) {
	graph := scope.NewGraph()
	f := &Feed{specs: map[string]*CompilerSpec{"go": {}}}
	err := f.Feed(graph, "main.go", nil, nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}
