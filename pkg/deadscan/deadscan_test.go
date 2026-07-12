package deadscan

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/pkg/model"
)

func TestMentionCountsFindsValueReferences(t *testing.T) {
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}

	// compileLua is only used as a function value — invisible to the call
	// graph, but a mention all the same. selfOnly calls itself inside its own
	// span and is mentioned nowhere else. testedOnly is referenced solely
	// from a test file.
	write("pkg/a.go", `package a

func compileLua(p string) string { return p }

func selfOnly(n int) int {
	if n <= 0 {
		return 0
	}
	return selfOnly(n - 1)
}
`)
	write("pkg/b.go", `package a

func use() {
	dispatch("lua", compileLua)
}
`)
	write("pkg/a_test.go", `package a

func TestSelfOnly(t *testing.T) { selfOnly(1) }
func TestTestedOnly(t *testing.T) { testedOnly() }
`)
	write("pkg/c.go", `package a

func testedOnly() {}
`)
	write("other/d.go", `package other

// compileLua mentioned in an unrelated directory must not count.
var _ = "compileLua"
`)

	files := []model.FileSummary{
		{Path: "pkg/a.go"},
		{Path: "pkg/b.go"},
		{Path: "pkg/a_test.go"},
		{Path: "pkg/c.go"},
		{Path: "other/d.go"},
	}
	candidates := []Candidate{
		{File: "pkg/a.go", Name: "compileLua", StartLine: 3, EndLine: 3},
		{File: "pkg/a.go", Name: "selfOnly", StartLine: 5, EndLine: 10},
		{File: "pkg/c.go", Name: "testedOnly", StartLine: 3, EndLine: 3},
	}

	counts := MentionCounts(root, files, candidates)

	if counts[0] == 0 {
		t.Error("compileLua value reference not counted")
	}
	if counts[1] != 0 {
		t.Errorf("selfOnly self-recursion counted as %d mentions, want 0", counts[1])
	}
	if counts[2] != 0 {
		t.Errorf("testedOnly test-file mention counted as %d, want 0", counts[2])
	}
}
