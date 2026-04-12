package sarif

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewLog(t *testing.T) {
	log := NewLog()
	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want %q", log.Version, "2.1.0")
	}
	if log.Schema == "" {
		t.Error("schema URI is empty")
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	if log.Runs[0].Tool.Driver.Name != "canopy" {
		t.Errorf("driver name = %q, want %q", log.Runs[0].Tool.Driver.Name, "canopy")
	}
	if log.Runs[0].Results != nil {
		t.Errorf("results should be nil initially, got %v", log.Runs[0].Results)
	}
}

func TestAddRule(t *testing.T) {
	log := NewLog()
	log.AddRule("cyclomatic", "Cyclomatic complexity exceeds threshold")
	log.AddRule("cognitive", "Cognitive complexity exceeds threshold")

	rules := log.Runs[0].Tool.Driver.Rules
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(rules))
	}
	if rules[0].ID != "cyclomatic" {
		t.Errorf("rule[0].ID = %q, want %q", rules[0].ID, "cyclomatic")
	}
	if rules[1].ShortDescription.Text != "Cognitive complexity exceeds threshold" {
		t.Errorf("rule[1].ShortDescription.Text = %q", rules[1].ShortDescription.Text)
	}
}

func TestAddResult(t *testing.T) {
	log := NewLog()

	// Result with file and line info.
	log.AddResult("cyclomatic", "warning", "complexity 55 exceeds 50", "pkg/foo/bar.go", 10, 42)

	results := log.Runs[0].Results
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	r := results[0]
	if r.RuleID != "cyclomatic" {
		t.Errorf("ruleId = %q", r.RuleID)
	}
	if r.Level != "warning" {
		t.Errorf("level = %q", r.Level)
	}
	if r.Message.Text != "complexity 55 exceeds 50" {
		t.Errorf("message = %q", r.Message.Text)
	}
	if len(r.Locations) != 1 {
		t.Fatalf("locations = %d, want 1", len(r.Locations))
	}
	loc := r.Locations[0]
	if loc.PhysicalLocation.ArtifactLocation.URI != "pkg/foo/bar.go" {
		t.Errorf("uri = %q", loc.PhysicalLocation.ArtifactLocation.URI)
	}
	if loc.PhysicalLocation.Region == nil {
		t.Fatal("region is nil")
	}
	if loc.PhysicalLocation.Region.StartLine != 10 {
		t.Errorf("startLine = %d", loc.PhysicalLocation.Region.StartLine)
	}
	if loc.PhysicalLocation.Region.EndLine != 42 {
		t.Errorf("endLine = %d", loc.PhysicalLocation.Region.EndLine)
	}

	// Result without file (e.g. generated-ratio check).
	log.AddResult("generated-ratio", "error", "too many generated files", "", 0, 0)
	r2 := log.Runs[0].Results[1]
	if len(r2.Locations) != 0 {
		t.Errorf("expected no locations for empty file, got %d", len(r2.Locations))
	}

	// Result with file but no line info.
	log.AddResult("boundary", "warning", "illegal import", "pkg/a/b.go", 0, 0)
	r3 := log.Runs[0].Results[2]
	if len(r3.Locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(r3.Locations))
	}
	if r3.Locations[0].PhysicalLocation.Region != nil {
		t.Error("expected nil region when lines are 0")
	}
}

func TestMapSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"error", "error"},
		{"warn", "warning"},
		{"warning", "warning"},
		{"note", "note"},
		{"info", "note"},
		{"", "warning"},
		{"unknown", "warning"},
	}
	for _, tt := range tests {
		got := MapSeverity(tt.input)
		if got != tt.want {
			t.Errorf("MapSeverity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncode(t *testing.T) {
	log := NewLog()
	log.AddRule("test-rule", "A test rule")
	log.AddResult("test-rule", "warning", "something happened", "src/main.go", 5, 10)

	var buf bytes.Buffer
	if err := log.Encode(&buf); err != nil {
		t.Fatal(err)
	}

	// Verify it's valid JSON.
	var decoded Log
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if decoded.Version != "2.1.0" {
		t.Errorf("decoded version = %q", decoded.Version)
	}
	if decoded.Schema == "" {
		t.Error("decoded schema is empty")
	}
	if len(decoded.Runs) != 1 {
		t.Fatalf("decoded runs = %d", len(decoded.Runs))
	}
	if decoded.Runs[0].Tool.Driver.Name != "canopy" {
		t.Errorf("decoded driver = %q", decoded.Runs[0].Tool.Driver.Name)
	}
	if len(decoded.Runs[0].Tool.Driver.Rules) != 1 {
		t.Fatalf("decoded rules = %d", len(decoded.Runs[0].Tool.Driver.Rules))
	}
	if len(decoded.Runs[0].Results) != 1 {
		t.Fatalf("decoded results = %d", len(decoded.Runs[0].Results))
	}

	// Verify $schema key is present in raw JSON.
	raw := buf.String()
	var rawMap map[string]any
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := rawMap["$schema"]; !ok {
		t.Error("$schema key missing from output")
	}
}
