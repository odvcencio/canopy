package lint

import (
	"testing"
)

func TestParseSuppressions_FunctionLevel(t *testing.T) {
	source := []byte(`package main

//canopy:lint-ignore cyclomatic — intentionally complex
func complexFunc() {}

func normalFunc() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 1 {
		t.Fatalf("expected 1 suppression, got %d", len(suppressions))
	}
	s := suppressions[0]
	if s.Metric != "cyclomatic" {
		t.Errorf("metric = %q, want %q", s.Metric, "cyclomatic")
	}
	if s.Line != 3 {
		t.Errorf("line = %d, want 3", s.Line)
	}
	if s.File {
		t.Error("expected File to be false for function-level suppression")
	}
}

func TestParseSuppressions_FileLevel(t *testing.T) {
	source := []byte(`//canopy:lint-ignore-file — generated code
package sqlcgen

func Insert() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 1 {
		t.Fatalf("expected 1 suppression, got %d", len(suppressions))
	}
	s := suppressions[0]
	if s.Metric != "*" {
		t.Errorf("metric = %q, want %q", s.Metric, "*")
	}
	if s.Line != 1 {
		t.Errorf("line = %d, want 1", s.Line)
	}
	if !s.File {
		t.Error("expected File to be true for file-level suppression")
	}
}

func TestParseSuppressions_Multiple(t *testing.T) {
	source := []byte(`//canopy:lint-ignore-file — generated code
package gen

//canopy:lint-ignore cyclomatic
func first() {}

//canopy:lint-ignore lines -- too long
func second() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 3 {
		t.Fatalf("expected 3 suppressions, got %d", len(suppressions))
	}

	// File-level
	if !suppressions[0].File || suppressions[0].Metric != "*" {
		t.Errorf("suppression[0] = %+v, expected file-level wildcard", suppressions[0])
	}

	// Function-level cyclomatic
	if suppressions[1].File || suppressions[1].Metric != "cyclomatic" {
		t.Errorf("suppression[1] = %+v, expected function-level cyclomatic", suppressions[1])
	}
	if suppressions[1].Line != 4 {
		t.Errorf("suppression[1].Line = %d, want 4", suppressions[1].Line)
	}

	// Function-level lines
	if suppressions[2].File || suppressions[2].Metric != "lines" {
		t.Errorf("suppression[2] = %+v, expected function-level lines", suppressions[2])
	}
	if suppressions[2].Line != 7 {
		t.Errorf("suppression[2].Line = %d, want 7", suppressions[2].Line)
	}
}

func TestParseSuppressions_NoDirective(t *testing.T) {
	source := []byte(`package main

// This is a normal comment
func normalFunc() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 0 {
		t.Fatalf("expected 0 suppressions, got %d", len(suppressions))
	}
}

func TestParseSuppressions_EmptySource(t *testing.T) {
	suppressions := ParseSuppressions(nil)
	if len(suppressions) != 0 {
		t.Fatalf("expected 0 suppressions for nil input, got %d", len(suppressions))
	}

	suppressions = ParseSuppressions([]byte{})
	if len(suppressions) != 0 {
		t.Fatalf("expected 0 suppressions for empty input, got %d", len(suppressions))
	}
}

func TestParseSuppressions_NoMetricMeansWildcard(t *testing.T) {
	source := []byte(`//canopy:lint-ignore
func foo() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 1 {
		t.Fatalf("expected 1 suppression, got %d", len(suppressions))
	}
	if suppressions[0].Metric != "*" {
		t.Errorf("metric = %q, want %q", suppressions[0].Metric, "*")
	}
}

func TestParseSuppressions_WithHashReason(t *testing.T) {
	source := []byte(`//canopy:lint-ignore cyclomatic # legacy code
func legacy() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 1 {
		t.Fatalf("expected 1 suppression, got %d", len(suppressions))
	}
	if suppressions[0].Metric != "cyclomatic" {
		t.Errorf("metric = %q, want %q", suppressions[0].Metric, "cyclomatic")
	}
}

func TestParseSuppressions_WithIndentation(t *testing.T) {
	source := []byte(`package main

	//canopy:lint-ignore lines
	func indented() {}
`)
	suppressions := ParseSuppressions(source)
	if len(suppressions) != 1 {
		t.Fatalf("expected 1 suppression, got %d", len(suppressions))
	}
	if suppressions[0].Metric != "lines" {
		t.Errorf("metric = %q, want %q", suppressions[0].Metric, "lines")
	}
}

func TestIsSuppressed_FileLevelWildcard(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "*", Line: 1, File: true},
	}

	if !IsSuppressed(suppressions, 10, "cyclomatic") {
		t.Error("file-level wildcard should suppress any metric at any line")
	}
	if !IsSuppressed(suppressions, 50, "lines") {
		t.Error("file-level wildcard should suppress any metric at any line")
	}
}

func TestIsSuppressed_FileLevelSpecificMetric(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "cyclomatic", Line: 1, File: true},
	}

	if !IsSuppressed(suppressions, 10, "cyclomatic") {
		t.Error("file-level cyclomatic should suppress cyclomatic violations")
	}
	if IsSuppressed(suppressions, 10, "lines") {
		t.Error("file-level cyclomatic should not suppress lines violations")
	}
}

func TestIsSuppressed_LineLevelExactLine(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "cyclomatic", Line: 5, File: false},
	}

	// Suppression is on line 5, function starts on line 6.
	if !IsSuppressed(suppressions, 6, "cyclomatic") {
		t.Error("line-level suppression should suppress function on next line")
	}
	if IsSuppressed(suppressions, 7, "cyclomatic") {
		t.Error("line-level suppression should not suppress function two lines away")
	}
	if IsSuppressed(suppressions, 5, "cyclomatic") {
		t.Error("line-level suppression should not suppress function on same line")
	}
}

func TestIsSuppressed_LineLevelWrongMetric(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "cyclomatic", Line: 5, File: false},
	}

	if IsSuppressed(suppressions, 6, "lines") {
		t.Error("line-level cyclomatic suppression should not suppress lines metric")
	}
}

func TestIsSuppressed_LineLevelWildcard(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "*", Line: 5, File: false},
	}

	if !IsSuppressed(suppressions, 6, "cyclomatic") {
		t.Error("wildcard suppression should suppress any metric")
	}
	if !IsSuppressed(suppressions, 6, "lines") {
		t.Error("wildcard suppression should suppress any metric")
	}
}

func TestIsSuppressed_EmptySuppressions(t *testing.T) {
	if IsSuppressed(nil, 10, "cyclomatic") {
		t.Error("nil suppressions should not suppress anything")
	}
	if IsSuppressed([]Suppression{}, 10, "cyclomatic") {
		t.Error("empty suppressions should not suppress anything")
	}
}

func TestIsSuppressed_CaseInsensitive(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "cyclomatic", Line: 5, File: false},
	}

	if !IsSuppressed(suppressions, 6, "Cyclomatic") {
		t.Error("metric matching should be case-insensitive")
	}
	if !IsSuppressed(suppressions, 6, "CYCLOMATIC") {
		t.Error("metric matching should be case-insensitive")
	}
}

func TestIsSuppressed_MultipleSuppressionsFirstMatches(t *testing.T) {
	suppressions := []Suppression{
		{Metric: "lines", Line: 3, File: false},
		{Metric: "cyclomatic", Line: 9, File: false},
	}

	if !IsSuppressed(suppressions, 4, "lines") {
		t.Error("first suppression should match")
	}
	if !IsSuppressed(suppressions, 10, "cyclomatic") {
		t.Error("second suppression should match")
	}
	if IsSuppressed(suppressions, 4, "cyclomatic") {
		t.Error("should not cross-match suppressions")
	}
}
