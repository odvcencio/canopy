package complexity

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"m31labs.dev/canopy/pkg/model"
)

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.6f, want %.6f (tol %g)", name, got, want, tol)
	}
}

func TestHalsteadFromCounts(t *testing.T) {
	// n1=2 distinct operators, n2=3 distinct operands, N1=5 total operators, N2=4 total operands.
	h := halsteadFromCounts(2, 3, 5, 4)

	if h.Vocabulary != 5 {
		t.Errorf("Vocabulary = %d, want 5", h.Vocabulary)
	}
	if h.Length != 9 {
		t.Errorf("Length = %d, want 9", h.Length)
	}
	approx(t, "Volume", h.Volume, 20.897353, 1e-4)        // 9 * log2(5)
	approx(t, "Difficulty", h.Difficulty, 1.333333, 1e-4) // (2/2)*(4/3)
	approx(t, "Effort", h.Effort, 27.863137, 1e-3)        // D * V
	approx(t, "Time", h.Time, 1.547952, 1e-4)             // E / 18
	approx(t, "Bugs", h.Bugs, 0.003064, 1e-4)             // E^(2/3) / 3000
}

func TestHalsteadZeroOperands(t *testing.T) {
	// No operands: difficulty/effort/volume must degrade to 0, not NaN/Inf.
	h := halsteadFromCounts(1, 0, 3, 0)

	if h.Volume != 0 {
		t.Errorf("Volume = %f, want 0 (vocabulary <= 1)", h.Volume)
	}
	if h.Difficulty != 0 {
		t.Errorf("Difficulty = %f, want 0 (no operands)", h.Difficulty)
	}
	if h.Effort != 0 || h.Time != 0 || h.Bugs != 0 {
		t.Errorf("Effort/Time/Bugs = %f/%f/%f, want 0/0/0", h.Effort, h.Time, h.Bugs)
	}
}

func TestMaintainability(t *testing.T) {
	// volume=20.897353, cyclomatic=5, sloc=9, commentPct=0
	mi := maintainability(20.897353, 5, 9, 0)

	approx(t, "Original", mi.Original, 118.449305, 1e-3)
	approx(t, "SEI", mi.SEI, 95.694357, 1e-3)
	approx(t, "VisualStudio", mi.VisualStudio, 69.268600, 1e-3)
}

func TestMaintainabilityDegenerate(t *testing.T) {
	// Zero volume and zero SLOC must not produce -Inf/NaN from ln(0)/log2(0).
	mi := maintainability(0, 1, 0, 0)

	approx(t, "Original", mi.Original, 170.77, 1e-2)
	if math.IsInf(mi.Original, 0) || math.IsNaN(mi.Original) {
		t.Errorf("Original is non-finite: %f", mi.Original)
	}
	approx(t, "VisualStudio", mi.VisualStudio, 99.8655, 1e-2)
}

func TestLocFromLineSets(t *testing.T) {
	// 6 physical lines. Line 3 has both code and a comment; line 2 is comment-only; line 4 is blank.
	codeLines := map[int]bool{1: true, 3: true, 5: true, 6: true}
	commentLines := map[int]bool{2: true, 3: true}

	loc := locFromLineSets(6, codeLines, commentLines, 4)

	if loc.SLOC != 6 {
		t.Errorf("SLOC = %d, want 6", loc.SLOC)
	}
	if loc.PLOC != 4 {
		t.Errorf("PLOC = %d, want 4", loc.PLOC)
	}
	if loc.CLOC != 2 {
		t.Errorf("CLOC = %d, want 2", loc.CLOC)
	}
	if loc.Blank != 1 { // 6 - 4 code - 1 comment-only(line 2) = 1 (line 4)
		t.Errorf("Blank = %d, want 1", loc.Blank)
	}
	if loc.LLOC != 4 {
		t.Errorf("LLOC = %d, want 4", loc.LLOC)
	}
}

func goLangForTest(t *testing.T) *gotreesitter.Language {
	t.Helper()
	entry := grammars.DetectLanguage("x.go")
	if entry == nil {
		t.Fatal("no go grammar")
	}
	return entry.Language()
}

func parseGo(t *testing.T, src []byte) (*gotreesitter.Node, *gotreesitter.Language) {
	t.Helper()
	lang := goLangForTest(t)
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil || tree == nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree.RootNode(), lang
}

func TestAnalyzeSourceMetricsGo(t *testing.T) {
	src := []byte("func add(a int, b int) int {\n\treturn a + b\n}\n")
	root, lang := parseGo(t, src)

	loc, hal := analyzeSourceMetrics(root, lang, src)

	if loc.SLOC != 3 {
		t.Errorf("SLOC = %d, want 3", loc.SLOC)
	}
	if loc.PLOC != 3 {
		t.Errorf("PLOC = %d, want 3", loc.PLOC)
	}
	if loc.CLOC != 0 {
		t.Errorf("CLOC = %d, want 0", loc.CLOC)
	}
	if loc.Blank != 0 {
		t.Errorf("Blank = %d, want 0", loc.Blank)
	}
	if hal.TotalOperators == 0 || hal.TotalOperands == 0 {
		t.Errorf("expected non-zero operators/operands, got N1=%d N2=%d", hal.TotalOperators, hal.TotalOperands)
	}
	if hal.Volume <= 0 {
		t.Errorf("Volume = %f, want > 0", hal.Volume)
	}
	if hal.Difficulty <= 0 {
		t.Errorf("Difficulty = %f, want > 0", hal.Difficulty)
	}
}

func TestAnalyzeSourceMetricsCommentsAndBlanks(t *testing.T) {
	src := []byte("func greet() {\n\t// say hello\n\tx := 1\n\n\t_ = x\n}\n")
	root, lang := parseGo(t, src)

	loc, _ := analyzeSourceMetrics(root, lang, src)

	if loc.SLOC != 6 {
		t.Errorf("SLOC = %d, want 6", loc.SLOC)
	}
	if loc.PLOC != 4 { // lines: func{, x:=1, _=x, }
		t.Errorf("PLOC = %d, want 4", loc.PLOC)
	}
	if loc.CLOC != 1 { // the // say hello line
		t.Errorf("CLOC = %d, want 1", loc.CLOC)
	}
	if loc.Blank != 1 { // the empty line
		t.Errorf("Blank = %d, want 1", loc.Blank)
	}
}

func TestAnalyzePopulatesRichMetrics(t *testing.T) {
	dir := t.TempDir()
	src := "package main\n\nfunc compute(a int, b int) int {\n\tif a > b {\n\t\treturn a - b\n\t}\n\treturn a + b\n}\n"
	path := filepath.Join(dir, "compute.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{{
			Path:     path,
			Language: "go",
			Symbols: []model.Symbol{{
				File:      path,
				Kind:      "function_definition",
				Name:      "compute",
				Signature: "func compute(a int, b int) int",
				StartLine: 3,
				EndLine:   8,
			}},
		}},
	}

	report, err := Analyze(idx, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(report.Functions))
	}
	fn := report.Functions[0]

	if fn.LOC.SLOC != 6 {
		t.Errorf("LOC.SLOC = %d, want 6", fn.LOC.SLOC)
	}
	if fn.LOC.PLOC == 0 {
		t.Errorf("LOC.PLOC = 0, want > 0")
	}
	if fn.Halstead.Volume <= 0 {
		t.Errorf("Halstead.Volume = %f, want > 0", fn.Halstead.Volume)
	}
	if fn.Halstead.TotalOperands == 0 {
		t.Errorf("Halstead.TotalOperands = 0, want > 0")
	}
	if fn.Maintainability.Original == 0 {
		t.Errorf("Maintainability.Original not computed")
	}
	if fn.Maintainability.VisualStudio < 0 || fn.Maintainability.VisualStudio > 100 {
		t.Errorf("Maintainability.VisualStudio = %f, want in [0,100]", fn.Maintainability.VisualStudio)
	}
	if report.Summary.AvgMaintainability == 0 && report.Summary.MinMaintainability == 0 {
		t.Errorf("summary maintainability aggregates not populated")
	}
}
