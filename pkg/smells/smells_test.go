package smells

import (
	"fmt"
	"testing"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/coupling"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
)

func makeComplexityReport(fns ...complexity.FunctionMetrics) *complexity.Report {
	return &complexity.Report{Functions: fns}
}

func makeCouplingReport(pkgs ...coupling.PackageMetrics) *coupling.Report {
	return &coupling.Report{Packages: pkgs}
}

func makeTypeReport(types ...typemetrics.TypeMetrics) *typemetrics.Report {
	return &typemetrics.Report{Types: types}
}

// buildXrefGraph builds a Graph via xref.Build from a synthetic index.
func buildXrefGraph(idx *model.Index) xref.Graph {
	g, _ := xref.Build(idx)
	return g
}

func TestGodFunction(t *testing.T) {
	cr := makeComplexityReport(complexity.FunctionMetrics{
		File:       "pkg/big/big.go",
		Name:       "processAll",
		StartLine:  10,
		EndLine:    300,
		Cyclomatic: 35,
		Lines:      250,
		FanOut:     25,
	})

	report := Detect(Input{Complexity: cr})

	found := false
	for _, s := range report.Smells {
		if s.ID == "god_function" && s.Name == "processAll" {
			found = true
			if s.Severity != "error" {
				t.Errorf("god_function severity = %q, want %q", s.Severity, "error")
			}
			if s.Signals["cyclomatic"] != 35 {
				t.Errorf("cyclomatic signal = %v, want 35", s.Signals["cyclomatic"])
			}
		}
	}
	if !found {
		t.Error("god_function not detected for cyc=35, lines=250, fan_out=25")
	}
}

func TestGodFunctionBelowThreshold(t *testing.T) {
	cr := makeComplexityReport(complexity.FunctionMetrics{
		File:       "pkg/small/small.go",
		Name:       "processSome",
		StartLine:  1,
		EndLine:    150,
		Cyclomatic: 25,
		Lines:      150,
		FanOut:     10,
	})

	report := Detect(Input{Complexity: cr})

	for _, s := range report.Smells {
		if s.ID == "god_function" {
			t.Error("god_function should not be detected for cyc=25, lines=150, fan_out=10")
		}
	}
}

func TestLongParams(t *testing.T) {
	cr := makeComplexityReport(
		complexity.FunctionMetrics{
			File:       "a/a.go",
			Name:       "tooMany",
			StartLine:  1,
			EndLine:    10,
			Parameters: 7,
		},
		complexity.FunctionMetrics{
			File:       "a/a.go",
			Name:       "justRight",
			StartLine:  20,
			EndLine:    30,
			Parameters: 4,
		},
	)

	report := Detect(Input{Complexity: cr})

	foundTooMany := false
	foundJustRight := false
	for _, s := range report.Smells {
		if s.ID == "long_params" {
			if s.Name == "tooMany" {
				foundTooMany = true
			}
			if s.Name == "justRight" {
				foundJustRight = true
			}
		}
	}
	if !foundTooMany {
		t.Error("long_params not detected for params=7")
	}
	if foundJustRight {
		t.Error("long_params should not be detected for params=4")
	}
}

func TestDeepNesting(t *testing.T) {
	cr := makeComplexityReport(
		complexity.FunctionMetrics{
			File:       "a/a.go",
			Name:       "deep",
			StartLine:  1,
			EndLine:    50,
			MaxNesting: 5,
		},
		complexity.FunctionMetrics{
			File:       "a/a.go",
			Name:       "shallow",
			StartLine:  60,
			EndLine:    80,
			MaxNesting: 3,
		},
	)

	report := Detect(Input{Complexity: cr})

	foundDeep := false
	foundShallow := false
	for _, s := range report.Smells {
		if s.ID == "deep_nesting" {
			if s.Name == "deep" {
				foundDeep = true
			}
			if s.Name == "shallow" {
				foundShallow = true
			}
		}
	}
	if !foundDeep {
		t.Error("deep_nesting not detected for nesting=5")
	}
	if foundShallow {
		t.Error("deep_nesting should not be detected for nesting=3")
	}
}

func TestGodPackage(t *testing.T) {
	cr := makeCouplingReport(coupling.PackageMetrics{
		Package: "pkg/everything",
		Ce:      20,
		Symbols: 60,
		LCOM:    4,
	})

	report := Detect(Input{Coupling: cr})

	found := false
	for _, s := range report.Smells {
		if s.ID == "god_package" && s.Package == "pkg/everything" {
			found = true
			if s.Severity != "error" {
				t.Errorf("god_package severity = %q, want %q", s.Severity, "error")
			}
		}
	}
	if !found {
		t.Error("god_package not detected for Ce=20, symbols=60, LCOM=4")
	}
}

func TestGodType(t *testing.T) {
	tr := makeTypeReport(typemetrics.TypeMetrics{
		File:          "pkg/big/types.go",
		Name:          "MegaStruct",
		StartLine:     5,
		EndLine:       100,
		MethodSetSize: 25,
		Fields:        20,
	})

	report := Detect(Input{Types: tr})

	found := false
	for _, s := range report.Smells {
		if s.ID == "god_type" && s.Name == "MegaStruct" {
			found = true
			if s.Severity != "warn" {
				t.Errorf("god_type severity = %q, want %q", s.Severity, "warn")
			}
		}
	}
	if !found {
		t.Error("god_type not detected for method_set_size=25, fields=20")
	}
}

func TestWideInterface(t *testing.T) {
	tr := makeTypeReport(typemetrics.TypeMetrics{
		File:           "pkg/api/iface.go",
		Name:           "BigAPI",
		StartLine:      1,
		EndLine:        50,
		InterfaceWidth: 10,
	})

	report := Detect(Input{Types: tr})

	found := false
	for _, s := range report.Smells {
		if s.ID == "wide_interface" && s.Name == "BigAPI" {
			found = true
			if s.Signals["interface_width"] != 10 {
				t.Errorf("interface_width signal = %v, want 10", s.Signals["interface_width"])
			}
		}
	}
	if !found {
		t.Error("wide_interface not detected for interface_width=10")
	}
}

func TestShotgunSurgery(t *testing.T) {
	// Build an index where one symbol (target) is called by 31+ different callers.
	files := []model.FileSummary{
		{
			Path:     "pkg/core/target.go",
			Language: "go",
			Symbols: []model.Symbol{
				{File: "pkg/core/target.go", Kind: "function_definition", Name: "Target", Signature: "func Target()", StartLine: 1, EndLine: 5},
			},
		},
	}

	// Create 31 caller files, each with a function that calls Target.
	for i := 0; i < 31; i++ {
		path := fmt.Sprintf("pkg/callers/caller%d.go", i)
		callerName := fmt.Sprintf("Caller%d", i)
		files = append(files, model.FileSummary{
			Path:     path,
			Language: "go",
			Symbols: []model.Symbol{
				{File: path, Kind: "function_definition", Name: callerName, Signature: fmt.Sprintf("func %s()", callerName), StartLine: 1, EndLine: 10},
			},
			References: []model.Reference{
				{File: path, Kind: "reference.call", Name: "Target", StartLine: 5, EndLine: 5},
			},
		})
	}

	idx := &model.Index{Files: files}
	graph := buildXrefGraph(idx)

	report := Detect(Input{XrefGraph: graph})

	found := false
	for _, s := range report.Smells {
		if s.ID == "shotgun_surgery" && s.Name == "Target" {
			found = true
			count, ok := s.Signals["incoming_count"].(int)
			if !ok || count < 31 {
				t.Errorf("incoming_count = %v, want >= 31", s.Signals["incoming_count"])
			}
		}
	}
	if !found {
		t.Error("shotgun_surgery not detected for Target with 31 callers")
	}
}

func TestFeatureEnvy(t *testing.T) {
	// fnA in package "a" calls fnB (in package "b") 5 times, and fnC (in package "a") 2 times.
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "a/a.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "a/a.go", Kind: "function_definition", Name: "fnA", Signature: "func fnA()", StartLine: 1, EndLine: 20},
					{File: "a/a.go", Kind: "function_definition", Name: "fnC", Signature: "func fnC()", StartLine: 25, EndLine: 30},
				},
				References: []model.Reference{
					{File: "a/a.go", Kind: "reference.call", Name: "fnB", StartLine: 3, EndLine: 3},
					{File: "a/a.go", Kind: "reference.call", Name: "fnB", StartLine: 5, EndLine: 5},
					{File: "a/a.go", Kind: "reference.call", Name: "fnB", StartLine: 7, EndLine: 7},
					{File: "a/a.go", Kind: "reference.call", Name: "fnB", StartLine: 9, EndLine: 9},
					{File: "a/a.go", Kind: "reference.call", Name: "fnB", StartLine: 11, EndLine: 11},
					{File: "a/a.go", Kind: "reference.call", Name: "fnC", StartLine: 13, EndLine: 13},
					{File: "a/a.go", Kind: "reference.call", Name: "fnC", StartLine: 15, EndLine: 15},
				},
			},
			{
				Path:     "b/b.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "b/b.go", Kind: "function_definition", Name: "fnB", Signature: "func fnB()", StartLine: 1, EndLine: 10},
				},
			},
		},
	}

	graph := buildXrefGraph(idx)

	report := Detect(Input{XrefGraph: graph})

	found := false
	for _, s := range report.Smells {
		if s.ID == "feature_envy" && s.Name == "fnA" {
			found = true
			if s.Signals["envied_package"] != "b" {
				t.Errorf("envied_package = %v, want %q", s.Signals["envied_package"], "b")
			}
		}
	}
	if !found {
		t.Error("feature_envy not detected for fnA (5 calls to b, 2 to a)")
	}
}

func TestDataClump(t *testing.T) {
	// Three functions sharing the same parameter signature "int, string, bool".
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "pkg/dc/a.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "pkg/dc/a.go", Kind: "function_definition", Name: "Alpha", Signature: "func Alpha(x int, y string, z bool)", StartLine: 1, EndLine: 10},
					{File: "pkg/dc/a.go", Kind: "function_definition", Name: "Beta", Signature: "func Beta(x int, y string, z bool)", StartLine: 15, EndLine: 25},
					{File: "pkg/dc/a.go", Kind: "function_definition", Name: "Gamma", Signature: "func Gamma(x int, y string, z bool)", StartLine: 30, EndLine: 40},
					{File: "pkg/dc/a.go", Kind: "function_definition", Name: "Delta", Signature: "func Delta(a float64)", StartLine: 45, EndLine: 50},
				},
			},
		},
	}

	cr := makeComplexityReport(
		complexity.FunctionMetrics{File: "pkg/dc/a.go", Name: "Alpha", StartLine: 1, EndLine: 10, Parameters: 3},
		complexity.FunctionMetrics{File: "pkg/dc/a.go", Name: "Beta", StartLine: 15, EndLine: 25, Parameters: 3},
		complexity.FunctionMetrics{File: "pkg/dc/a.go", Name: "Gamma", StartLine: 30, EndLine: 40, Parameters: 3},
		complexity.FunctionMetrics{File: "pkg/dc/a.go", Name: "Delta", StartLine: 45, EndLine: 50, Parameters: 1},
	)

	report := Detect(Input{Index: idx, Complexity: cr})

	clumpCount := 0
	deltaClump := false
	for _, s := range report.Smells {
		if s.ID == "data_clump" {
			clumpCount++
			if s.Name == "Delta" {
				deltaClump = true
			}
		}
	}
	// 3 functions should each get a data_clump smell = 3 smells total.
	if clumpCount != 3 {
		t.Errorf("data_clump count = %d, want 3", clumpCount)
	}
	if deltaClump {
		t.Error("Delta should not have data_clump")
	}
}

func TestGracefulDegradation(t *testing.T) {
	// All nil optional inputs -- should not crash.
	report := Detect(Input{})

	if report == nil {
		t.Fatal("report is nil")
	}
	// Only xref-based smells run (feature_envy, shotgun_surgery) with empty graph = no results.
	// No crash is the key assertion.
	if report.Summary.Total != 0 {
		t.Errorf("expected 0 smells with empty input, got %d", report.Summary.Total)
	}
}

func TestSummary(t *testing.T) {
	cr := makeComplexityReport(
		// god_function (error)
		complexity.FunctionMetrics{File: "a/a.go", Name: "godFn", StartLine: 1, EndLine: 300, Cyclomatic: 35, Lines: 250, FanOut: 25},
		// long_params (warn)
		complexity.FunctionMetrics{File: "a/a.go", Name: "longP", StartLine: 310, EndLine: 320, Parameters: 7},
		// deep_nesting (warn)
		complexity.FunctionMetrics{File: "a/a.go", Name: "deepN", StartLine: 330, EndLine: 380, MaxNesting: 6},
	)
	couplingR := makeCouplingReport(coupling.PackageMetrics{
		Package: "pkg/god",
		Ce:      20,
		Symbols: 60,
		LCOM:    5,
	})

	report := Detect(Input{Complexity: cr, Coupling: couplingR})

	if report.Summary.Total != 4 {
		t.Errorf("Total = %d, want 4", report.Summary.Total)
	}
	if report.Summary.BySeverity["error"] != 2 {
		t.Errorf("BySeverity[error] = %d, want 2", report.Summary.BySeverity["error"])
	}
	if report.Summary.BySeverity["warn"] != 2 {
		t.Errorf("BySeverity[warn] = %d, want 2", report.Summary.BySeverity["warn"])
	}
	if report.Summary.ByID["god_function"] != 1 {
		t.Errorf("ByID[god_function] = %d, want 1", report.Summary.ByID["god_function"])
	}
	if report.Summary.ByID["god_package"] != 1 {
		t.Errorf("ByID[god_package] = %d, want 1", report.Summary.ByID["god_package"])
	}
	if report.Summary.ByID["long_params"] != 1 {
		t.Errorf("ByID[long_params] = %d, want 1", report.Summary.ByID["long_params"])
	}
	if report.Summary.ByID["deep_nesting"] != 1 {
		t.Errorf("ByID[deep_nesting] = %d, want 1", report.Summary.ByID["deep_nesting"])
	}

	// Verify sort order: errors before warns.
	if len(report.Smells) >= 2 {
		if report.Smells[0].Severity != "error" {
			t.Errorf("first smell severity = %q, want %q", report.Smells[0].Severity, "error")
		}
	}
}

func TestUnstableDep(t *testing.T) {
	// Build an index with two packages: "stable" and "unstable".
	// "stable" calls into "unstable".
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "stable/s.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "stable/s.go", Kind: "function_definition", Name: "StableFn", Signature: "func StableFn()", StartLine: 1, EndLine: 10},
				},
				References: []model.Reference{
					{File: "stable/s.go", Kind: "reference.call", Name: "UnstableFn", StartLine: 5, EndLine: 5},
				},
			},
			{
				Path:     "unstable/u.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "unstable/u.go", Kind: "function_definition", Name: "UnstableFn", Signature: "func UnstableFn()", StartLine: 1, EndLine: 10},
				},
			},
		},
	}

	graph := buildXrefGraph(idx)

	// Build coupling report with instability values.
	cr := makeCouplingReport(
		coupling.PackageMetrics{Package: "stable", Instability: 0.1},
		coupling.PackageMetrics{Package: "unstable", Instability: 0.8},
	)

	report := Detect(Input{XrefGraph: graph, Coupling: cr})

	found := false
	for _, s := range report.Smells {
		if s.ID == "unstable_dep" {
			found = true
			if s.Signals["stable_package"] != "stable" {
				t.Errorf("stable_package = %v, want %q", s.Signals["stable_package"], "stable")
			}
			if s.Signals["unstable_package"] != "unstable" {
				t.Errorf("unstable_package = %v, want %q", s.Signals["unstable_package"], "unstable")
			}
		}
	}
	if !found {
		t.Error("unstable_dep not detected for stable(I=0.1) -> unstable(I=0.8)")
	}
}

func TestNormalizeParamSig(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"func Foo(x int, y string)", "x int, y string"},
		{"func Bar()", ""},
		{"func Baz(a  int,   b   string,  c bool)", "a int, b string, c bool"},
		{"no parens", ""},
	}
	for _, tt := range tests {
		got := normalizeParamSig(tt.input)
		if got != tt.want {
			t.Errorf("normalizeParamSig(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
