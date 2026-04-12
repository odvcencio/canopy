package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePatterns_BlankAndComments(t *testing.T) {
	m := ParsePatterns([]string{"", "  ", "# comment", "  # indented comment"})
	if len(m.patterns) != 0 {
		t.Fatalf("expected 0 patterns, got %d", len(m.patterns))
	}
}

func TestMatch_LiteralName(t *testing.T) {
	m := ParsePatterns([]string{"secret.key"})
	if !m.Match("secret.key", false) {
		t.Error("expected match on exact name")
	}
	if !m.Match("subdir/secret.key", false) {
		t.Error("expected match on nested path")
	}
	if m.Match("secret.keys", false) {
		t.Error("unexpected match on different name")
	}
}

func TestMatch_GlobPattern(t *testing.T) {
	m := ParsePatterns([]string{"*.log"})
	if !m.Match("app.log", false) {
		t.Error("expected match on .log file")
	}
	if !m.Match("logs/debug.log", false) {
		t.Error("expected match on nested .log file")
	}
	if m.Match("app.txt", false) {
		t.Error("unexpected match on .txt file")
	}
}

func TestMatch_DirectoryPattern(t *testing.T) {
	m := ParsePatterns([]string{"build/"})
	if !m.Match("build", true) {
		t.Error("expected match on directory")
	}
	if m.Match("build", false) {
		t.Error("unexpected match on file named build")
	}
	if !m.Match("project/build", true) {
		t.Error("expected match on nested directory")
	}
	if !m.Match("project/build/output/app.js", false) {
		t.Error("expected match on file within ignored directory")
	}
}

func TestMatch_DirectoryPatternWithSlashMatchesDescendants(t *testing.T) {
	m := ParsePatterns([]string{"sdks/rust/target/"})
	if !m.Match("sdks/rust/target/debug/build/out.rs", false) {
		t.Error("expected path-scoped directory pattern to match descendant file")
	}
	if m.Match("sdks/rust/src/lib.rs", false) {
		t.Error("unexpected match outside ignored directory")
	}
}

func TestMatch_Negation(t *testing.T) {
	m := ParsePatterns([]string{"*.log", "!important.log"})
	if !m.Match("debug.log", false) {
		t.Error("expected match on debug.log")
	}
	if m.Match("important.log", false) {
		t.Error("unexpected match on negated important.log")
	}
}

func TestMatch_PathWithSlash(t *testing.T) {
	m := ParsePatterns([]string{"vendor/generated/*"})
	if !m.Match("vendor/generated/foo.go", false) {
		t.Error("expected match on path pattern")
	}
	if m.Match("vendor/other/foo.go", false) {
		t.Error("unexpected match on non-matching path")
	}
}

func TestMatch_NilMatcher(t *testing.T) {
	var m *Matcher
	if m.Match("anything", false) {
		t.Error("nil matcher should never match")
	}
}

func TestMatch_EmptyPatterns(t *testing.T) {
	m := ParsePatterns(nil)
	if m.Match("anything", false) {
		t.Error("empty matcher should never match")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".canopyignore")
	content := "*.log\n# comment\nbuild/\n!important.log\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(m.patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(m.patterns))
	}
	if !m.Match("debug.log", false) {
		t.Error("expected match on .log")
	}
	if m.Match("important.log", false) {
		t.Error("unexpected match on negated pattern")
	}
	if !m.Match("build", true) {
		t.Error("expected match on build dir")
	}
}

func TestLoad_NotFound(t *testing.T) {
	_, err := Load("/nonexistent/.canopyignore")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
