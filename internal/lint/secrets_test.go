package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestSecretsPatterns_ReturnsExpectedCount(t *testing.T) {
	patterns := SecretsPatterns()
	if len(patterns) != 3 {
		t.Fatalf("expected 3 secrets patterns, got %d", len(patterns))
	}
	ids := map[string]bool{}
	for _, p := range patterns {
		ids[p.ID] = true
		if p.Query == "" {
			t.Fatalf("pattern %q has empty query", p.ID)
		}
		if p.Message == "" {
			t.Fatalf("pattern %q has empty message", p.ID)
		}
	}
	for _, expected := range []string{"secrets/hardcoded-go", "secrets/hardcoded-js", "secrets/hardcoded-python"} {
		if !ids[expected] {
			t.Fatalf("missing expected pattern id %q", expected)
		}
	}
}

func TestSecretsPatterns_GoDetectsShortVarDecl(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package main

var safe = "hello"

func connect() {
	dbPassword := "hunter2"
	host := "localhost"
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "main.go", Language: "go"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	// Should detect dbPassword but not safe or host.
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].RuleID != "secrets/hardcoded-go" {
		t.Fatalf("unexpected rule id %q", violations[0].RuleID)
	}
}

func TestSecretsPatterns_GoDetectsConstSpec(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "config.go")
	source := `package config

const apiKey = "sk-1234567890abcdef"
const version = "1.0.0"
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "config.go", Language: "go"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for apiKey, got %d: %+v", len(violations), violations)
	}
}

func TestSecretsPatterns_GoDetectsVarSpec(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "vars.go")
	source := `package vars

var authToken = "bearer-abc123"
var count = "42"
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "vars.go", Language: "go"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for authToken, got %d: %+v", len(violations), violations)
	}
}

func TestSecretsPatterns_GoNoFalsePositives(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "clean.go")
	source := `package clean

const version = "1.2.3"
var name = "service"

func main() {
	host := "localhost"
	port := "8080"
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "clean.go", Language: "go"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for clean file, got %d: %+v", len(violations), violations)
	}
}

func TestSecretsPatterns_JSDetectsVariableDeclarator(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "config.js")
	source := `const apiKey = "sk-1234567890";
const host = "localhost";
let dbPassword = "hunter2";
var name = "myapp";
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "config.js", Language: "javascript"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	// Should detect apiKey and dbPassword.
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}
	for _, v := range violations {
		if v.RuleID != "secrets/hardcoded-js" {
			t.Fatalf("unexpected rule id %q", v.RuleID)
		}
	}
}

func TestSecretsPatterns_PythonDetectsAssignment(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "config.py")
	source := `api_key = "sk-1234567890"
host = "localhost"
db_password = "hunter2"
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "config.py", Language: "python"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	// Should detect api_key and db_password.
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}
	for _, v := range violations {
		if v.RuleID != "secrets/hardcoded-python" {
			t.Fatalf("unexpected rule id %q", v.RuleID)
		}
	}
}

func TestSecretsPatterns_GoCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "mixed.go")
	source := `package mixed

const DBPassword = "secret1"
const SecretKey = "secret2"

func init() {
	myAPIKey := "secret3"
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "mixed.go", Language: "go"},
		},
	}

	violations, err := EvaluatePatterns(idx, SecretsPatterns())
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}

	// DBPassword, SecretKey, and myAPIKey should all match.
	if len(violations) != 3 {
		t.Fatalf("expected 3 violations, got %d: %+v", len(violations), violations)
	}
}
