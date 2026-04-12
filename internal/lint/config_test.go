package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfig_Overrides(t *testing.T) {
	content := `# Threshold overrides
cyclomatic > 35         → warn  "function too complex"
cognitive > 60          → warn  "hard to reason about"
`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 2 {
		t.Fatalf("expected 2 overrides, got %d", len(cfg.Overrides))
	}

	o := cfg.Overrides[0]
	if o.Metric != "cyclomatic" {
		t.Errorf("override[0].Metric = %q, want %q", o.Metric, "cyclomatic")
	}
	if o.Threshold != 35 {
		t.Errorf("override[0].Threshold = %d, want 35", o.Threshold)
	}
	if o.Severity != "warn" {
		t.Errorf("override[0].Severity = %q, want %q", o.Severity, "warn")
	}
	if o.Message != "function too complex" {
		t.Errorf("override[0].Message = %q, want %q", o.Message, "function too complex")
	}

	o = cfg.Overrides[1]
	if o.Metric != "cognitive" {
		t.Errorf("override[1].Metric = %q, want %q", o.Metric, "cognitive")
	}
	if o.Threshold != 60 {
		t.Errorf("override[1].Threshold = %d, want 60", o.Threshold)
	}
}

func TestParseConfig_OverrideWithArrow(t *testing.T) {
	content := `cyclomatic > 20 -> error "too complex"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(cfg.Overrides))
	}
	o := cfg.Overrides[0]
	if o.Severity != "error" {
		t.Errorf("severity = %q, want %q", o.Severity, "error")
	}
	if o.Message != "too complex" {
		t.Errorf("message = %q, want %q", o.Message, "too complex")
	}
}

func TestParseConfig_IgnoreFileSymbol(t *testing.T) {
	content := `ignore cyclomatic in policy.go:listPREntityChanges`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Ignores) != 1 {
		t.Fatalf("expected 1 ignore, got %d", len(cfg.Ignores))
	}
	ig := cfg.Ignores[0]
	if ig.Metric != "cyclomatic" {
		t.Errorf("metric = %q, want %q", ig.Metric, "cyclomatic")
	}
	if ig.FilePath != "policy.go" {
		t.Errorf("file_path = %q, want %q", ig.FilePath, "policy.go")
	}
	if ig.Symbol != "listPREntityChanges" {
		t.Errorf("symbol = %q, want %q", ig.Symbol, "listPREntityChanges")
	}
}

func TestParseConfig_IgnoreFileOnly(t *testing.T) {
	content := `ignore lines in api_test.go`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Ignores) != 1 {
		t.Fatalf("expected 1 ignore, got %d", len(cfg.Ignores))
	}
	ig := cfg.Ignores[0]
	if ig.Metric != "lines" {
		t.Errorf("metric = %q, want %q", ig.Metric, "lines")
	}
	if ig.FilePath != "api_test.go" {
		t.Errorf("file_path = %q, want %q", ig.FilePath, "api_test.go")
	}
	if ig.Symbol != "" {
		t.Errorf("symbol = %q, want empty", ig.Symbol)
	}
}

func TestParseConfig_IgnoreWildcardDirectory(t *testing.T) {
	content := `ignore * in internal/database/sqlcgen/`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Ignores) != 1 {
		t.Fatalf("expected 1 ignore, got %d", len(cfg.Ignores))
	}
	ig := cfg.Ignores[0]
	if ig.Metric != "*" {
		t.Errorf("metric = %q, want %q", ig.Metric, "*")
	}
	if ig.FilePath != "internal/database/sqlcgen/" {
		t.Errorf("file_path = %q, want %q", ig.FilePath, "internal/database/sqlcgen/")
	}
}

func TestParseConfig_CommentsAndBlanks(t *testing.T) {
	content := `
# This is a comment
# Another comment

ignore lines in foo.go

# Final comment
`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Ignores) != 1 {
		t.Fatalf("expected 1 ignore, got %d", len(cfg.Ignores))
	}
	if len(cfg.Overrides) != 0 {
		t.Fatalf("expected 0 overrides, got %d", len(cfg.Overrides))
	}
}

func TestParseConfig_InvalidLine(t *testing.T) {
	content := `this is not a valid directive`
	_, err := ParseConfig(content)
	if err == nil {
		t.Fatal("expected ParseConfig to return error for invalid directive")
	}
}

func TestParseConfig_InvalidSeverity(t *testing.T) {
	content := `cyclomatic > 10 → fatal "bad"`
	_, err := ParseConfig(content)
	if err == nil {
		t.Fatal("expected ParseConfig to return error for invalid severity")
	}
}

func TestParseConfig_Empty(t *testing.T) {
	cfg, err := ParseConfig("")
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 0 || len(cfg.Ignores) != 0 {
		t.Fatal("expected empty config for empty input")
	}
}

func TestShouldIgnore_WildcardMetric(t *testing.T) {
	cfg := &Config{
		Ignores: []ConfigIgnore{
			{Metric: "*", FilePath: "internal/database/sqlcgen/"},
		},
	}

	if !cfg.ShouldIgnore("internal/database/sqlcgen/queries.go", "Insert", "cyclomatic") {
		t.Error("expected file in sqlcgen/ directory to be ignored for cyclomatic")
	}
	if !cfg.ShouldIgnore("internal/database/sqlcgen/queries.go", "Insert", "lines") {
		t.Error("expected file in sqlcgen/ directory to be ignored for lines")
	}
	if cfg.ShouldIgnore("internal/database/other.go", "Insert", "cyclomatic") {
		t.Error("expected file outside sqlcgen/ to not be ignored")
	}
}

func TestShouldIgnore_SpecificMetricAndSymbol(t *testing.T) {
	cfg := &Config{
		Ignores: []ConfigIgnore{
			{Metric: "cyclomatic", FilePath: "policy.go", Symbol: "listPREntityChanges"},
		},
	}

	if !cfg.ShouldIgnore("policy.go", "listPREntityChanges", "cyclomatic") {
		t.Error("expected specific file+symbol+metric to be ignored")
	}
	if cfg.ShouldIgnore("policy.go", "listPREntityChanges", "lines") {
		t.Error("expected different metric to not be ignored")
	}
	if cfg.ShouldIgnore("policy.go", "otherFunc", "cyclomatic") {
		t.Error("expected different symbol to not be ignored")
	}
	if cfg.ShouldIgnore("other.go", "listPREntityChanges", "cyclomatic") {
		t.Error("expected different file to not be ignored")
	}
}

func TestShouldIgnore_FileOnlyNoSymbol(t *testing.T) {
	cfg := &Config{
		Ignores: []ConfigIgnore{
			{Metric: "lines", FilePath: "api_test.go"},
		},
	}

	if !cfg.ShouldIgnore("api_test.go", "TestSomething", "lines") {
		t.Error("expected file-level ignore to match any symbol")
	}
	if !cfg.ShouldIgnore("api_test.go", "", "lines") {
		t.Error("expected file-level ignore to match empty symbol")
	}
	if cfg.ShouldIgnore("api_test.go", "TestSomething", "cyclomatic") {
		t.Error("expected different metric to not be ignored")
	}
}

func TestShouldIgnore_NilConfig(t *testing.T) {
	var cfg *Config
	if cfg.ShouldIgnore("foo.go", "bar", "lines") {
		t.Error("nil config should never ignore")
	}
}

func TestShouldIgnore_DirectoryPrefix(t *testing.T) {
	cfg := &Config{
		Ignores: []ConfigIgnore{
			{Metric: "*", FilePath: "internal/gen/"},
		},
	}

	if !cfg.ShouldIgnore("internal/gen/models.go", "Insert", "lines") {
		t.Error("expected directory prefix match")
	}
	if !cfg.ShouldIgnore("internal/gen/sub/deep.go", "Process", "cyclomatic") {
		t.Error("expected nested directory match")
	}
	if cfg.ShouldIgnore("internal/genuine/other.go", "Foo", "lines") {
		t.Error("expected non-matching prefix to not match")
	}
}

func TestShouldIgnore_SuffixMatch(t *testing.T) {
	cfg := &Config{
		Ignores: []ConfigIgnore{
			{Metric: "cyclomatic", FilePath: "internal/handler.go"},
		},
	}

	if !cfg.ShouldIgnore("pkg/internal/handler.go", "Handle", "cyclomatic") {
		t.Error("expected suffix match to work")
	}
	if !cfg.ShouldIgnore("internal/handler.go", "Handle", "cyclomatic") {
		t.Error("expected exact match to work")
	}
}

func TestLoadConfig_FindsInParent(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `ignore lines in api_test.go`
	if err := os.WriteFile(filepath.Join(tmpDir, ".canopylint"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	childDir := filepath.Join(tmpDir, "sub", "deep")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	cfg, err := LoadConfig(childDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config to be found")
	}
	if len(cfg.Ignores) != 1 {
		t.Fatalf("expected 1 ignore, got %d", len(cfg.Ignores))
	}
	if cfg.Ignores[0].FilePath != "api_test.go" {
		t.Errorf("file_path = %q, want %q", cfg.Ignores[0].FilePath, "api_test.go")
	}
}

func TestLoadConfig_DirectoryContainsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `cyclomatic > 25 → warn "complex function"`
	if err := os.WriteFile(filepath.Join(tmpDir, ".canopylint"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config to be found")
	}
	if len(cfg.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(cfg.Overrides))
	}
}

func TestLoadConfig_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when no .canopylint file exists")
	}
}

// --- Task 5: Scoped Override tests ---

func TestParseConfig_ScopedOverride(t *testing.T) {
	content := `fan_out > 10 in pkg/* -> warn "high fan-out"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(cfg.Overrides))
	}
	o := cfg.Overrides[0]
	if o.Metric != "fan_out" {
		t.Errorf("metric = %q, want %q", o.Metric, "fan_out")
	}
	if o.Threshold != 10 {
		t.Errorf("threshold = %d, want 10", o.Threshold)
	}
	if o.Scope != "pkg/*" {
		t.Errorf("scope = %q, want %q", o.Scope, "pkg/*")
	}
	if o.Severity != "warn" {
		t.Errorf("severity = %q, want %q", o.Severity, "warn")
	}
	if o.Message != "high fan-out" {
		t.Errorf("message = %q, want %q", o.Message, "high fan-out")
	}
}

func TestParseConfig_ScopedOverrideWithUnicode(t *testing.T) {
	content := `cognitive > 30 in internal/* → error "too complex"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(cfg.Overrides))
	}
	o := cfg.Overrides[0]
	if o.Metric != "cognitive" {
		t.Errorf("metric = %q, want %q", o.Metric, "cognitive")
	}
	if o.Threshold != 30 {
		t.Errorf("threshold = %d, want 30", o.Threshold)
	}
	if o.Scope != "internal/*" {
		t.Errorf("scope = %q, want %q", o.Scope, "internal/*")
	}
	if o.Severity != "error" {
		t.Errorf("severity = %q, want %q", o.Severity, "error")
	}
	if o.Message != "too complex" {
		t.Errorf("message = %q, want %q", o.Message, "too complex")
	}
}

// --- Task 6: Package-Level Directive tests ---

func TestParseConfig_PackageImportDepth(t *testing.T) {
	content := `package import_depth > 5 -> error "dependency chain too deep"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.PackageRules) != 1 {
		t.Fatalf("expected 1 package rule, got %d", len(cfg.PackageRules))
	}
	r := cfg.PackageRules[0]
	if r.Metric != "import_depth" {
		t.Errorf("metric = %q, want %q", r.Metric, "import_depth")
	}
	if r.Threshold != 5 {
		t.Errorf("threshold = %d, want 5", r.Threshold)
	}
	if r.Severity != "error" {
		t.Errorf("severity = %q, want %q", r.Severity, "error")
	}
	if r.Message != "dependency chain too deep" {
		t.Errorf("message = %q, want %q", r.Message, "dependency chain too deep")
	}
	if r.Scope != "" {
		t.Errorf("scope = %q, want empty", r.Scope)
	}
	if r.Enforcement {
		t.Error("enforcement should be false for threshold rule")
	}
}

func TestParseConfig_PackageScopedRule(t *testing.T) {
	content := `package exported_symbols > 50 in pkg/* -> warn "package API surface too large"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.PackageRules) != 1 {
		t.Fatalf("expected 1 package rule, got %d", len(cfg.PackageRules))
	}
	r := cfg.PackageRules[0]
	if r.Metric != "exported_symbols" {
		t.Errorf("metric = %q, want %q", r.Metric, "exported_symbols")
	}
	if r.Threshold != 50 {
		t.Errorf("threshold = %d, want 50", r.Threshold)
	}
	if r.Scope != "pkg/*" {
		t.Errorf("scope = %q, want %q", r.Scope, "pkg/*")
	}
	if r.Severity != "warn" {
		t.Errorf("severity = %q, want %q", r.Severity, "warn")
	}
	if r.Message != "package API surface too large" {
		t.Errorf("message = %q, want %q", r.Message, "package API surface too large")
	}
	if r.Enforcement {
		t.Error("enforcement should be false for threshold rule")
	}
}

func TestParseConfig_PackageNoCycles(t *testing.T) {
	content := `package no_import_cycles -> error "import cycle detected"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.PackageRules) != 1 {
		t.Fatalf("expected 1 package rule, got %d", len(cfg.PackageRules))
	}
	r := cfg.PackageRules[0]
	if r.Metric != "no_import_cycles" {
		t.Errorf("metric = %q, want %q", r.Metric, "no_import_cycles")
	}
	if r.Severity != "error" {
		t.Errorf("severity = %q, want %q", r.Severity, "error")
	}
	if r.Message != "import cycle detected" {
		t.Errorf("message = %q, want %q", r.Message, "import cycle detected")
	}
	if !r.Enforcement {
		t.Error("enforcement should be true for no_ enforcement rule")
	}
	if r.Threshold != 0 {
		t.Errorf("threshold = %d, want 0 for enforcement rule", r.Threshold)
	}
}

func TestParseConfig_MultipleDirectives(t *testing.T) {
	content := `# Override thresholds
cyclomatic > 35         → warn  "function too complex"
cognitive > 60          → warn  "hard to reason about"

# Ignore specific functions
ignore cyclomatic in policy.go:listPREntityChanges
ignore lines in api_test.go

# Ignore whole files
ignore * in internal/database/sqlcgen/
`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 2 {
		t.Fatalf("expected 2 overrides, got %d", len(cfg.Overrides))
	}
	if len(cfg.Ignores) != 3 {
		t.Fatalf("expected 3 ignores, got %d", len(cfg.Ignores))
	}
}

// --- License Rule Directive tests ---

func TestParseConfig_LicenseDenySingle(t *testing.T) {
	content := `license deny GPL-3.0 -> error "copyleft not allowed"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.LicenseRules) != 1 {
		t.Fatalf("expected 1 license rule, got %d", len(cfg.LicenseRules))
	}
	r := cfg.LicenseRules[0]
	if r.Type != "deny" {
		t.Errorf("type = %q, want %q", r.Type, "deny")
	}
	if len(r.Licenses) != 1 || r.Licenses[0] != "GPL-3.0" {
		t.Errorf("licenses = %v, want [GPL-3.0]", r.Licenses)
	}
	if r.Severity != "error" {
		t.Errorf("severity = %q, want %q", r.Severity, "error")
	}
	if r.Message != "copyleft not allowed" {
		t.Errorf("message = %q, want %q", r.Message, "copyleft not allowed")
	}
}

func TestParseConfig_LicenseDenyMultiple(t *testing.T) {
	content := `license deny GPL-2.0, GPL-3.0, AGPL-3.0 -> error "copyleft not permitted"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.LicenseRules) != 1 {
		t.Fatalf("expected 1 license rule, got %d", len(cfg.LicenseRules))
	}
	r := cfg.LicenseRules[0]
	if len(r.Licenses) != 3 {
		t.Fatalf("expected 3 licenses, got %d", len(r.Licenses))
	}
	expected := []string{"GPL-2.0", "GPL-3.0", "AGPL-3.0"}
	for i, want := range expected {
		if r.Licenses[i] != want {
			t.Errorf("licenses[%d] = %q, want %q", i, r.Licenses[i], want)
		}
	}
}

func TestParseConfig_LicenseDenyUnicodeArrow(t *testing.T) {
	content := `license deny MIT → warn "prefer Apache"`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.LicenseRules) != 1 {
		t.Fatalf("expected 1 license rule, got %d", len(cfg.LicenseRules))
	}
	r := cfg.LicenseRules[0]
	if r.Severity != "warn" {
		t.Errorf("severity = %q, want %q", r.Severity, "warn")
	}
	if len(r.Licenses) != 1 || r.Licenses[0] != "MIT" {
		t.Errorf("licenses = %v, want [MIT]", r.Licenses)
	}
}

func TestParseConfig_LicenseDenyInvalidSeverity(t *testing.T) {
	content := `license deny GPL-3.0 -> fatal "nope"`
	_, err := ParseConfig(content)
	if err == nil {
		t.Fatal("expected ParseConfig to return error for invalid severity")
	}
}

func TestParseConfig_LicenseWithOtherDirectives(t *testing.T) {
	content := `cyclomatic > 35 -> warn "too complex"
license deny AGPL-3.0 -> error "no AGPL"
ignore lines in big_test.go
`
	cfg, err := ParseConfig(content)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if len(cfg.Overrides) != 1 {
		t.Errorf("expected 1 override, got %d", len(cfg.Overrides))
	}
	if len(cfg.LicenseRules) != 1 {
		t.Errorf("expected 1 license rule, got %d", len(cfg.LicenseRules))
	}
	if len(cfg.Ignores) != 1 {
		t.Errorf("expected 1 ignore, got %d", len(cfg.Ignores))
	}
}
