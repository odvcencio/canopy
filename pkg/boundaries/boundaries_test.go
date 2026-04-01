package boundaries

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Task 1: ParseConfig
// ---------------------------------------------------------------------------

func TestParseConfig_EmptyInput(t *testing.T) {
	cfg, err := ParseConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(cfg.Rules))
	}
}

func TestParseConfig_CommentsAndBlankLines(t *testing.T) {
	input := `# This is a comment

# Another comment
   # indented comment
`
	cfg, err := ParseConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(cfg.Rules))
	}
}

func TestParseConfig_SingleAllowRule(t *testing.T) {
	input := `module pkg/index allow pkg/model, pkg/lang`
	cfg, err := ParseConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
	r := cfg.Rules[0]
	if r.Module != "pkg/index" {
		t.Errorf("module = %q, want %q", r.Module, "pkg/index")
	}
	if r.Type != "allow" {
		t.Errorf("type = %q, want %q", r.Type, "allow")
	}
	if len(r.Targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(r.Targets))
	}
	if r.Targets[0] != "pkg/model" {
		t.Errorf("targets[0] = %q, want %q", r.Targets[0], "pkg/model")
	}
	if r.Targets[1] != "pkg/lang" {
		t.Errorf("targets[1] = %q, want %q", r.Targets[1], "pkg/lang")
	}
}

func TestParseConfig_SingleDenyRule(t *testing.T) {
	input := `module internal/* deny internal/*`
	cfg, err := ParseConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
	r := cfg.Rules[0]
	if r.Module != "internal/*" {
		t.Errorf("module = %q, want %q", r.Module, "internal/*")
	}
	if r.Type != "deny" {
		t.Errorf("type = %q, want %q", r.Type, "deny")
	}
	if len(r.Targets) != 1 || r.Targets[0] != "internal/*" {
		t.Errorf("targets = %v, want [internal/*]", r.Targets)
	}
}

func TestParseConfig_DashMeansNoTargets(t *testing.T) {
	input := `module pkg/model allow -`
	cfg, err := ParseConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
	r := cfg.Rules[0]
	if r.Module != "pkg/model" {
		t.Errorf("module = %q, want %q", r.Module, "pkg/model")
	}
	if r.Type != "allow" {
		t.Errorf("type = %q, want %q", r.Type, "allow")
	}
	if len(r.Targets) != 0 {
		t.Errorf("targets = %v, want empty (dash means no targets)", r.Targets)
	}
}

func TestParseConfig_MultipleRules(t *testing.T) {
	input := `# Module boundary definitions
module pkg/model      allow -
module pkg/index      allow pkg/model, pkg/lang
module internal/*     allow pkg/*
module internal/*     deny  internal/*
`
	cfg, err := ParseConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(cfg.Rules))
	}
}

func TestParseConfig_ExtraWhitespace(t *testing.T) {
	input := `   module   pkg/index   allow   pkg/model ,  pkg/lang   `
	cfg, err := ParseConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
	r := cfg.Rules[0]
	if r.Module != "pkg/index" {
		t.Errorf("module = %q, want %q", r.Module, "pkg/index")
	}
	if len(r.Targets) != 2 || r.Targets[0] != "pkg/model" || r.Targets[1] != "pkg/lang" {
		t.Errorf("targets = %v, want [pkg/model pkg/lang]", r.Targets)
	}
}

func TestParseConfig_ErrorMissingKeyword(t *testing.T) {
	input := `module pkg/index`
	_, err := ParseConfig(input)
	if err == nil {
		t.Fatal("expected error for malformed line, got nil")
	}
}

func TestParseConfig_ErrorBadType(t *testing.T) {
	input := `module pkg/index block pkg/model`
	_, err := ParseConfig(input)
	if err == nil {
		t.Fatal("expected error for invalid rule type, got nil")
	}
}

func TestParseConfig_ErrorMissingTargets(t *testing.T) {
	input := `module pkg/index allow`
	_, err := ParseConfig(input)
	if err == nil {
		t.Fatal("expected error for missing targets, got nil")
	}
}

func TestParseConfig_ErrorUnrecognizedDirective(t *testing.T) {
	input := `boundary pkg/index allow pkg/model`
	_, err := ParseConfig(input)
	if err == nil {
		t.Fatal("expected error for unrecognized directive, got nil")
	}
}

// ---------------------------------------------------------------------------
// Task 2: matchGlob
// ---------------------------------------------------------------------------

func TestMatchGlob_ExactMatch(t *testing.T) {
	if !matchGlob("pkg/model", "pkg/model") {
		t.Error("exact match should return true")
	}
	if matchGlob("pkg/model", "pkg/lang") {
		t.Error("different paths should not match")
	}
}

func TestMatchGlob_WildcardStar(t *testing.T) {
	if !matchGlob("*", "anything") {
		t.Error("* should match anything")
	}
	if !matchGlob("*", "pkg/model") {
		t.Error("* should match any path")
	}
}

func TestMatchGlob_PrefixWildcard(t *testing.T) {
	if !matchGlob("pkg/*", "pkg/model") {
		t.Error("pkg/* should match pkg/model")
	}
	if !matchGlob("pkg/*", "pkg/lang") {
		t.Error("pkg/* should match pkg/lang")
	}
	if !matchGlob("internal/*", "internal/deps") {
		t.Error("internal/* should match internal/deps")
	}
	if matchGlob("pkg/*", "internal/deps") {
		t.Error("pkg/* should not match internal/deps")
	}
	if matchGlob("pkg/*", "pkg") {
		t.Error("pkg/* should not match bare 'pkg'")
	}
}

func TestMatchGlob_NoMatchEmptyPattern(t *testing.T) {
	if matchGlob("", "pkg/model") {
		t.Error("empty pattern should not match anything")
	}
}

// ---------------------------------------------------------------------------
// Task 2: Evaluate
// ---------------------------------------------------------------------------

func TestEvaluate_NoRulesNoViolations(t *testing.T) {
	cfg := &Config{}
	edges := []ImportEdge{{From: "pkg/model", To: "pkg/lang"}}
	violations := Evaluate(cfg, edges)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestEvaluate_AllowRulePermitsImport(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "pkg/index", Type: "allow", Targets: []string{"pkg/model", "pkg/lang"}},
		},
	}
	edges := []ImportEdge{
		{From: "pkg/index", To: "pkg/model"},
		{From: "pkg/index", To: "pkg/lang"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(violations), violations)
	}
}

func TestEvaluate_AllowRuleBlocksUnlisted(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "pkg/index", Type: "allow", Targets: []string{"pkg/model"}},
		},
	}
	edges := []ImportEdge{
		{From: "pkg/index", To: "pkg/lang"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.From != "pkg/index" || v.To != "pkg/lang" {
		t.Errorf("violation = {From:%q, To:%q}, want {From:pkg/index, To:pkg/lang}", v.From, v.To)
	}
}

func TestEvaluate_AllowDashBlocksEverything(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "pkg/model", Type: "allow", Targets: []string{}},
		},
	}
	edges := []ImportEdge{
		{From: "pkg/model", To: "pkg/lang"},
		{From: "pkg/model", To: "pkg/index"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
}

func TestEvaluate_DenyRuleBlocksMatchingImport(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "internal/*", Type: "deny", Targets: []string{"internal/*"}},
		},
	}
	edges := []ImportEdge{
		{From: "internal/deps", To: "internal/lint"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].From != "internal/deps" || violations[0].To != "internal/lint" {
		t.Errorf("violation mismatch: %+v", violations[0])
	}
}

func TestEvaluate_DenyRuleAllowsNonMatchingImport(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "internal/*", Type: "deny", Targets: []string{"internal/*"}},
		},
	}
	edges := []ImportEdge{
		{From: "internal/deps", To: "pkg/model"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(violations), violations)
	}
}

func TestEvaluate_UnmatchedModuleSkipped(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "pkg/index", Type: "allow", Targets: []string{"pkg/model"}},
		},
	}
	edges := []ImportEdge{
		{From: "cmd/main", To: "pkg/everything"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for unmatched module, got %d", len(violations))
	}
}

func TestEvaluate_AllowWithGlobTarget(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "internal/*", Type: "allow", Targets: []string{"pkg/*"}},
		},
	}
	edges := []ImportEdge{
		{From: "internal/deps", To: "pkg/model"},
		{From: "internal/deps", To: "pkg/lang"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(violations), violations)
	}
}

func TestEvaluate_AllowAndDenyCombined(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "internal/*", Type: "allow", Targets: []string{"pkg/*"}},
			{Module: "internal/*", Type: "deny", Targets: []string{"internal/*"}},
		},
	}
	edges := []ImportEdge{
		{From: "internal/deps", To: "pkg/model"},    // allowed by allow, not matched by deny
		{From: "internal/deps", To: "internal/lint"}, // not allowed by allow + denied by deny = 2 violations
		{From: "internal/deps", To: "cmd/main"},      // not allowed by allow = 1 violation
	}
	violations := Evaluate(cfg, edges)
	// Each rule is evaluated independently:
	// internal/lint: fails allow (not in pkg/*) + matches deny (internal/*) = 2
	// cmd/main: fails allow (not in pkg/*) = 1
	// Total = 3
	if len(violations) != 3 {
		t.Fatalf("expected 3 violations, got %d: %+v", len(violations), violations)
	}
}

func TestEvaluate_ViolationHasMessage(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "pkg/model", Type: "allow", Targets: []string{}},
		},
	}
	edges := []ImportEdge{
		{From: "pkg/model", To: "pkg/lang"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Message == "" {
		t.Error("violation should have a non-empty message")
	}
}

func TestEvaluate_DenyStarBlocksAll(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Module: "pkg/model", Type: "deny", Targets: []string{"*"}},
		},
	}
	edges := []ImportEdge{
		{From: "pkg/model", To: "pkg/lang"},
		{From: "pkg/model", To: "internal/deps"},
	}
	violations := Evaluate(cfg, edges)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
}

func TestEvaluate_NilConfig(t *testing.T) {
	violations := Evaluate(nil, []ImportEdge{{From: "a", To: "b"}})
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for nil config, got %d", len(violations))
	}
}

// ---------------------------------------------------------------------------
// Task 3: LoadConfig
// ---------------------------------------------------------------------------

func TestLoadConfig_FileInCurrentDir(t *testing.T) {
	dir := t.TempDir()
	content := "module pkg/model allow -\n"
	if err := os.WriteFile(filepath.Join(dir, ".gtsboundaries"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
}

func TestLoadConfig_FileInParentDir(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "sub", "deep")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	content := "module pkg/index allow pkg/model\n"
	if err := os.WriteFile(filepath.Join(parent, ".gtsboundaries"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cfg.Rules))
	}
}

func TestLoadConfig_NotFound(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when no file found, got %+v", cfg)
	}
}

func TestLoadConfig_ParseError(t *testing.T) {
	dir := t.TempDir()
	content := "invalid line\n"
	if err := os.WriteFile(filepath.Join(dir, ".gtsboundaries"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoadConfig_ClosestFileWins(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "sub")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}

	// Parent has 2 rules
	parentContent := "module pkg/model allow -\nmodule pkg/index allow pkg/model\n"
	if err := os.WriteFile(filepath.Join(parent, ".gtsboundaries"), []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Child has 1 rule — should win
	childContent := "module pkg/lang allow pkg/model\n"
	if err := os.WriteFile(filepath.Join(child, ".gtsboundaries"), []byte(childContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("expected 1 rule (closest file wins), got %d", len(cfg.Rules))
	}
	if cfg.Rules[0].Module != "pkg/lang" {
		t.Errorf("module = %q, want %q", cfg.Rules[0].Module, "pkg/lang")
	}
}
