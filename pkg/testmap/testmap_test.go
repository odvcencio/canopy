package testmap

import (
	"fmt"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

// defID mirrors the xref keyDefinition format for building test indexes.
func defID(file, kind, name string, startLine int) string {
	return file + "\x00" + kind + "\x00" + name + "\x00" + fmt.Sprintf("%d", startLine)
}

func TestMapEmpty(t *testing.T) {
	idx := &model.Index{
		Root:  "/tmp/empty",
		Files: []model.FileSummary{},
	}
	report, err := Map(idx, Options{})
	if err != nil {
		t.Fatalf("Map returned error: %v", err)
	}
	if len(report.Mappings) != 0 {
		t.Fatalf("expected 0 mappings, got %d", len(report.Mappings))
	}
	if report.TestedCount != 0 {
		t.Fatalf("expected 0 tested, got %d", report.TestedCount)
	}
	if report.UntestedCount != 0 {
		t.Fatalf("expected 0 untested, got %d", report.UntestedCount)
	}
	if report.Coverage != 0.0 {
		t.Fatalf("expected 0.0 coverage, got %f", report.Coverage)
	}
}

func TestMapNilIndex(t *testing.T) {
	_, err := Map(nil, Options{})
	if err == nil {
		t.Fatal("expected error for nil index")
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path     string
		language string
		want     bool
	}{
		// Go
		{"handler_test.go", "go", true},
		{"pkg/handler_test.go", "go", true},
		{"handler.go", "go", false},

		// Python
		{"test_handler.py", "python", true},
		{"handler_test.py", "python", true},
		{"tests/handler.py", "python", true},
		{"handler.py", "python", false},

		// JavaScript/TypeScript
		{"handler.test.js", "javascript", true},
		{"handler.spec.ts", "typescript", true},
		{"handler.test.tsx", "tsx", true},
		{"__tests__/handler.js", "javascript", true},
		{"src/__tests__/handler.ts", "typescript", true},
		{"handler.js", "javascript", false},

		// Java/Kotlin
		{"HandlerTest.java", "java", true},
		{"HandlerTests.java", "java", true},
		{"HandlerTest.kt", "kotlin", true},
		{"src/test/Handler.java", "java", true},
		{"Handler.java", "java", false},

		// Rust
		{"tests/handler.rs", "rust", true},
		{"src/handler.rs", "rust", false},

		// Ruby
		{"handler_spec.rb", "ruby", true},
		{"handler_test.rb", "ruby", true},
		{"handler.rb", "ruby", false},

		// Unknown language fallback
		{"test/handler.xyz", "", true},
		{"tests/handler.xyz", "", true},
		{"handler_test.go", "", true},
		{"src/handler.xyz", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := IsTestFile(tc.path, tc.language)
			if got != tc.want {
				t.Errorf("IsTestFile(%q, %q) = %v, want %v", tc.path, tc.language, got, tc.want)
			}
		})
	}
}

func TestMapGoTestToImpl(t *testing.T) {
	// Test file calls HandleRequest; HandleRequest calls ValidateInput.
	// Both should be "tested" or "indirectly_tested".
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path:     "handler.go",
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      "handler.go",
						Kind:      "function_definition",
						Name:      "HandleRequest",
						StartLine: 1,
						EndLine:   10,
					},
					{
						File:      "handler.go",
						Kind:      "function_definition",
						Name:      "ValidateInput",
						StartLine: 12,
						EndLine:   20,
					},
				},
				References: []model.Reference{
					{
						File:        "handler.go",
						Kind:        "reference.call",
						Name:        "ValidateInput",
						StartLine:   5,
						EndLine:     5,
						StartColumn: 2,
						EndColumn:   15,
					},
				},
			},
			{
				Path:     "handler_test.go",
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      "handler_test.go",
						Kind:      "function_definition",
						Name:      "TestHandleRequest",
						StartLine: 1,
						EndLine:   15,
					},
				},
				References: []model.Reference{
					{
						File:        "handler_test.go",
						Kind:        "reference.call",
						Name:        "HandleRequest",
						StartLine:   5,
						EndLine:     5,
						StartColumn: 2,
						EndColumn:   15,
					},
				},
			},
		},
	}

	report, err := Map(idx, Options{MaxDepth: 3})
	if err != nil {
		t.Fatalf("Map returned error: %v", err)
	}

	if len(report.Mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(report.Mappings))
	}

	// Both impl functions should be covered.
	if report.TestedCount != 2 {
		t.Fatalf("expected 2 tested, got %d", report.TestedCount)
	}
	if report.UntestedCount != 0 {
		t.Fatalf("expected 0 untested, got %d", report.UntestedCount)
	}

	// HandleRequest should be "tested" (direct call from test).
	handleReq := findMapping(report.Mappings, "HandleRequest")
	if handleReq == nil {
		t.Fatal("missing mapping for HandleRequest")
	}
	if handleReq.Coverage != "tested" {
		t.Errorf("HandleRequest coverage = %q, want %q", handleReq.Coverage, "tested")
	}
	if len(handleReq.Tests) != 1 {
		t.Errorf("HandleRequest tests count = %d, want 1", len(handleReq.Tests))
	}
	if len(handleReq.Tests) > 0 && handleReq.Tests[0].Distance != 1 {
		t.Errorf("HandleRequest test distance = %d, want 1", handleReq.Tests[0].Distance)
	}

	// ValidateInput should be "indirectly_tested" (test -> HandleRequest -> ValidateInput).
	validateInput := findMapping(report.Mappings, "ValidateInput")
	if validateInput == nil {
		t.Fatal("missing mapping for ValidateInput")
	}
	if validateInput.Coverage != "indirectly_tested" {
		t.Errorf("ValidateInput coverage = %q, want %q", validateInput.Coverage, "indirectly_tested")
	}
	if len(validateInput.Tests) != 1 {
		t.Errorf("ValidateInput tests count = %d, want 1", len(validateInput.Tests))
	}
	if len(validateInput.Tests) > 0 && validateInput.Tests[0].Distance != 2 {
		t.Errorf("ValidateInput test distance = %d, want 2", validateInput.Tests[0].Distance)
	}
}

func TestMapUntestedFilter(t *testing.T) {
	// TestedFunc is called by test; UntestedFunc is not.
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path:     "impl.go",
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      "impl.go",
						Kind:      "function_definition",
						Name:      "TestedFunc",
						StartLine: 1,
						EndLine:   5,
					},
					{
						File:      "impl.go",
						Kind:      "function_definition",
						Name:      "UntestedFunc",
						StartLine: 7,
						EndLine:   12,
					},
				},
			},
			{
				Path:     "impl_test.go",
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      "impl_test.go",
						Kind:      "function_definition",
						Name:      "TestTestedFunc",
						StartLine: 1,
						EndLine:   10,
					},
				},
				References: []model.Reference{
					{
						File:        "impl_test.go",
						Kind:        "reference.call",
						Name:        "TestedFunc",
						StartLine:   5,
						EndLine:     5,
						StartColumn: 2,
						EndColumn:   12,
					},
				},
			},
		},
	}

	// Without filter: should have 2 mappings.
	report, err := Map(idx, Options{MaxDepth: 3})
	if err != nil {
		t.Fatalf("Map returned error: %v", err)
	}
	if len(report.Mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(report.Mappings))
	}
	if report.TestedCount != 1 {
		t.Fatalf("expected 1 tested, got %d", report.TestedCount)
	}
	if report.UntestedCount != 1 {
		t.Fatalf("expected 1 untested, got %d", report.UntestedCount)
	}

	// With UntestedOnly: should have 1 mapping.
	reportFiltered, err := Map(idx, Options{UntestedOnly: true, MaxDepth: 3})
	if err != nil {
		t.Fatalf("Map with UntestedOnly returned error: %v", err)
	}
	if len(reportFiltered.Mappings) != 1 {
		t.Fatalf("expected 1 mapping with UntestedOnly, got %d", len(reportFiltered.Mappings))
	}
	if reportFiltered.Mappings[0].Symbol != "UntestedFunc" {
		t.Errorf("expected untested mapping for UntestedFunc, got %q", reportFiltered.Mappings[0].Symbol)
	}
	if reportFiltered.Mappings[0].Coverage != "untested" {
		t.Errorf("expected coverage 'untested', got %q", reportFiltered.Mappings[0].Coverage)
	}
}

func findMapping(mappings []TestMapping, symbol string) *TestMapping {
	for i := range mappings {
		if mappings[i].Symbol == symbol {
			return &mappings[i]
		}
	}
	return nil
}
