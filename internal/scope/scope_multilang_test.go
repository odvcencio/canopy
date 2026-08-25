package scope

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/pkg/index"
)

func TestBuild_RustLexicalScope(t *testing.T) {
	const source = `use std::collections::HashMap;
use crate::service::Worker as ServiceWorker;

fn run(input: i32, (left, right): (i32, i32)) -> i32 {
    let count: i32 = input;
    let (first, second) = (1, 2);
    if count > 0 {
        let nested = count;
        return nested;
    }
    let future = count;
    future
}
`
	report := buildScopeReport(t, "sample.rs", source, 9)
	if report.Language != "rust" {
		t.Fatalf("Language = %q, want rust", report.Language)
	}

	for name, kind := range map[string]string{
		"HashMap":       "import",
		"ServiceWorker": "import",
		"run":           "package_function",
		"input":         "param",
		"left":          "param",
		"right":         "param",
		"count":         "local_var",
		"first":         "local_var",
		"second":        "local_var",
		"nested":        "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
	assertScopeMissing(t, report, "future")
}

func TestBuild_JavaLexicalScope(t *testing.T) {
	const source = `package demo;
import java.util.List;
import static java.util.Collections.emptyList;

class Service {
    int run(String input, int limit) {
        int count = limit;
        for (String item : emptyList()) {
            int size = item.length();
            return size;
        }
        try {
            return count;
        } catch (RuntimeException err) {
            String message = err.getMessage();
            return message.length();
        }
        int future = count;
        return future;
    }
}
`
	report := buildScopeReport(t, "Service.java", source, 9)

	if report.Language != "java" {
		t.Fatalf("Language = %q, want java", report.Language)
	}
	if report.Package != "demo" {
		t.Fatalf("Package = %q, want demo", report.Package)
	}
	for name, kind := range map[string]string{
		"List":      "import",
		"emptyList": "import",
		"Service":   "package_type",
		"run":       "package_method",
		"input":     "param",
		"limit":     "param",
		"count":     "local_var",
		"item":      "local_var",
		"size":      "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
	assertScopeMissing(t, report, "future")
}

func TestBuild_JavaNestedBindingsDoNotLeak(t *testing.T) {
	const source = `package demo;

class Service {
    int run(int limit) {
        int count = limit;
        for (String item : items) {
            int size = item.length();
        }
        try {
            return count;
        } catch (RuntimeException err) {
            String message = err.getMessage();
            return message.length();
        }
    }
}
`
	report := buildScopeReport(t, "Service.java", source, 12)

	for name, kind := range map[string]string{
		"limit":   "param",
		"count":   "local_var",
		"err":     "local_var",
		"message": "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
	assertScopeMissing(t, report, "item")
	assertScopeMissing(t, report, "size")
}

func TestBuild_CPPLexicalScope(t *testing.T) {
	const source = `#include <string>
#include <vector>

int run(const std::string& input, int limit) {
    int count = limit;
    auto [left, right] = std::pair{1, 2};
    for (const auto& item : items) {
        int size = item.size();
        return size;
    }
    if (auto value = lookup()) {
        return value;
    }
    int future = count;
    return future;
}
`
	report := buildScopeReport(t, "sample.cpp", source, 8)
	if report.Language != "cpp" {
		t.Fatalf("Language = %q, want cpp", report.Language)
	}

	for name, kind := range map[string]string{
		"run":   "package_function",
		"input": "param",
		"limit": "param",
		"count": "local_var",
		"left":  "local_var",
		"right": "local_var",
		"item":  "local_var",
		"size":  "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
	assertScopeMissing(t, report, "#include <string>")
	assertScopeMissing(t, report, "future")
}

func TestBuild_CPPControlBindingsDoNotLeak(t *testing.T) {
	const source = `int run(int limit) {
    int count = limit;
    for (const auto& item : items) {
        int size = item.size();
    }
    if (auto value = lookup()) {
        return value;
    }
    return count;
}
`
	report := buildScopeReport(t, "sample.cpp", source, 7)

	for name, kind := range map[string]string{
		"limit": "param",
		"count": "local_var",
		"value": "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
	assertScopeMissing(t, report, "item")
	assertScopeMissing(t, report, "size")
}

func TestBuild_TypeScriptDestructuringScope(t *testing.T) {
	const source = `import {Config as Settings} from "./config";

function work(input: {left: number; right: number}) {
    const {left, right: renamed} = input;
    const [first, second] = [1, 2];
    return left + renamed + first + second;
}
`
	report := buildScopeReport(t, "sample.ts", source, 6)

	for name, kind := range map[string]string{
		"Settings": "import",
		"work":     "package_function",
		"input":    "param",
		"left":     "local_var",
		"renamed":  "local_var",
		"first":    "local_var",
		"second":   "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
}

func TestBuild_PythonDestructuringScope(t *testing.T) {
	const source = `def work(value):
    left, right = value
    [first, second] = value
    return left + right + first + second
`
	report := buildScopeReport(t, "sample.py", source, 4)

	for name, kind := range map[string]string{
		"work":   "package_function",
		"value":  "param",
		"left":   "local_var",
		"right":  "local_var",
		"first":  "local_var",
		"second": "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
}

func TestBuild_GoLoopBindingsDoNotLeak(t *testing.T) {
	const source = `package sample

func work(items []int) int {
    total := 0
    for i, value := range items {
        total += i + value
    }
    return total
}
`
	report := buildScopeReport(t, "sample.go", source, 8)

	for name, kind := range map[string]string{
		"items": "param",
		"total": "local_var",
	} {
		assertScopeSymbol(t, report, name, kind)
	}
	assertScopeMissing(t, report, "i")
	assertScopeMissing(t, report, "value")
}

func TestBuild_PackageSymbolsDoNotCrossLanguageFamilies(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"sample.rs": `fn run(input: i32) -> i32 {
    input
}
`,
		"Foreign.java": `class Foreign {
    int unrelated() { return 1; }
}
`,
		"foreign.py": `def python_only():
    return 1
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}
	idx, err := index.NewBuilder().BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath: %v", err)
	}
	report, err := Build(idx, Options{FilePath: filepath.Join(dir, "sample.rs"), Line: 2})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	assertScopeSymbol(t, report, "run", "package_function")
	assertScopeMissing(t, report, "Foreign")
	assertScopeMissing(t, report, "unrelated")
	assertScopeMissing(t, report, "python_only")
}

func TestSameScopeLanguage(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  bool
	}{
		{left: "rust", right: "rust", want: true},
		{left: "javascript", right: "typescript", want: true},
		{left: "typescript", right: "tsx", want: true},
		{left: "c", right: "cpp", want: true},
		{left: "rust", right: "java", want: false},
		{left: "cpp", right: "java", want: false},
	}
	for _, tt := range tests {
		if got := sameScopeLanguage(tt.left, tt.right); got != tt.want {
			t.Errorf("sameScopeLanguage(%q, %q) = %v, want %v", tt.left, tt.right, got, tt.want)
		}
	}
}

func buildScopeReport(t *testing.T, filename, source string, line int) Report {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", filename, err)
	}
	idx, err := index.NewBuilder().BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath(%s): %v", filename, err)
	}
	report, err := Build(idx, Options{FilePath: path, Line: line})
	if err != nil {
		t.Fatalf("Build(%s): %v", filename, err)
	}
	return report
}

func assertScopeSymbol(t *testing.T, report Report, name, kind string) {
	t.Helper()
	for _, symbol := range report.Symbols {
		if symbol.Name != name {
			continue
		}
		if symbol.Kind != kind {
			t.Fatalf("symbol %q kind = %q, want %q; symbols=%+v", name, symbol.Kind, kind, report.Symbols)
		}
		return
	}
	t.Fatalf("missing symbol %q (%s); symbols=%+v", name, kind, report.Symbols)
}

func assertScopeMissing(t *testing.T, report Report, name string) {
	t.Helper()
	for _, symbol := range report.Symbols {
		if symbol.Name == name {
			t.Fatalf("unexpected symbol %q in scope; symbols=%+v", name, report.Symbols)
		}
	}
}
