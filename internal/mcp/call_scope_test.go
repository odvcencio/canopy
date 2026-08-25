package mcp

import (
	"os"
	"path/filepath"
	"testing"

	gtsscope "m31labs.dev/canopy/internal/scope"
)

func TestCallScopeReportsJavaLexicalBindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Service.java")
	const source = `package demo;
import java.util.List;

class Service {
    int run(String input) {
        for (String item : List.of(input)) {
            return item.length();
        }
        return 0;
    }
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := NewService(dir, "").Call("gts_scope", map[string]any{
		"file": path,
		"line": 7,
	})
	if err != nil {
		t.Fatalf("gts_scope: %v", err)
	}
	report, ok := result.(gtsscope.Report)
	if !ok {
		t.Fatalf("gts_scope result type = %T, want scope.Report", result)
	}
	if report.Language != "java" || report.Package != "demo" {
		t.Fatalf("report language/package = %q/%q, want java/demo", report.Language, report.Package)
	}
	for _, want := range []string{"List", "Service", "run", "input", "item"} {
		found := false
		for _, symbol := range report.Symbols {
			if symbol.Name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in gts_scope result: %+v", want, report.Symbols)
		}
	}
}
