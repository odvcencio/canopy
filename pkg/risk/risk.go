// Package risk computes composite risk scores per function and per package by
// combining complexity, coupling (fan-out), git churn, and test coverage into a
// single prioritised score.
package risk

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/hotspot"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// FunctionRisk holds the composite risk score and its constituent signals for a single function.
type FunctionRisk struct {
	File          string  `json:"file"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	StartLine     int     `json:"start_line"`
	EndLine       int     `json:"end_line"`
	Risk          float64 `json:"risk"`
	ComplexityPct float64 `json:"complexity_pct"`
	CouplingPct   float64 `json:"coupling_pct"`
	ChurnPct      float64 `json:"churn_pct"`
	UntestedPct   float64 `json:"untested_pct"`
	Cyclomatic    int     `json:"cyclomatic"`
	FanOut        int     `json:"fan_out"`
	Commits       int     `json:"commits"`
	HasTest       bool    `json:"has_test"`
}

// PackageRisk aggregates function-level risk scores at the package (directory) level.
type PackageRisk struct {
	Package       string  `json:"package"`
	MaxRisk       float64 `json:"max_risk"`
	P90Risk       float64 `json:"p90_risk"`
	AvgRisk       float64 `json:"avg_risk"`
	HighRiskCount int     `json:"high_risk_count"`
	Functions     int     `json:"functions"`
}

// RiskSummary provides repo-wide aggregate risk statistics.
type RiskSummary struct {
	TotalFunctions int     `json:"total_functions"`
	MaxRisk        float64 `json:"max_risk"`
	P90Risk        float64 `json:"p90_risk"`
	AvgRisk        float64 `json:"avg_risk"`
	HighRiskCount  int     `json:"high_risk_count"`
}

// Report contains the full risk analysis result.
type Report struct {
	Functions []FunctionRisk `json:"functions"`
	Packages  []PackageRisk  `json:"packages"`
	Summary   RiskSummary    `json:"summary"`
}

// Input supplies the data sources required by Analyze.
type Input struct {
	Index      *model.Index
	Root       string
	Complexity *complexity.Report
	XrefGraph  xref.Graph
	TestMap    map[string]bool // key = "file\x00name\x00startLine" -> has test
	Since      string          // git churn window, e.g. "90d"
}

// Analyze computes composite risk scores for every function in the complexity
// report, aggregates them per package, and returns a sorted report.
func Analyze(input Input) (*Report, error) {
	if input.Complexity == nil || len(input.Complexity.Functions) == 0 {
		return &Report{}, nil
	}

	fns := input.Complexity.Functions

	// Collect git churn per file.
	since := input.Since
	if since == "" {
		since = "90d"
	}
	root := input.Root
	if root == "" {
		root = "."
	}
	rawChurn, err := hotspot.GitChurn(root, since)
	if err != nil {
		rawChurn = map[string]hotspot.FileChurn{}
	}

	// Build raw value slices for percentile ranking.
	n := len(fns)
	rawCyclomatic := make([]float64, n)
	rawFanOut := make([]float64, n)
	rawChurnValues := make([]float64, n)

	for i, fn := range fns {
		rawCyclomatic[i] = float64(fn.Cyclomatic)
		rawFanOut[i] = float64(fn.FanOut)
		fc := rawChurn[fn.File]
		rawChurnValues[i] = float64(fc.Commits) + float64(fc.Authors)*0.5
	}

	pctCyclomatic := percentileRank(rawCyclomatic)
	pctFanOut := percentileRank(rawFanOut)
	pctChurn := percentileRank(rawChurnValues)

	// Build function risk entries.
	results := make([]FunctionRisk, n)
	for i, fn := range fns {
		key := fmt.Sprintf("%s\x00%s\x00%d", fn.File, fn.Name, fn.StartLine)
		hasTested := false
		if input.TestMap != nil {
			hasTested = input.TestMap[key]
		}

		untested := 1.0
		if hasTested {
			untested = 0.01
		}

		fc := rawChurn[fn.File]

		risk := geometricMean(pctCyclomatic[i], pctFanOut[i], pctChurn[i], untested)

		results[i] = FunctionRisk{
			File:          fn.File,
			Name:          fn.Name,
			Kind:          fn.Kind,
			StartLine:     fn.StartLine,
			EndLine:       fn.EndLine,
			Risk:          risk,
			ComplexityPct: pctCyclomatic[i],
			CouplingPct:   pctFanOut[i],
			ChurnPct:      pctChurn[i],
			UntestedPct:   untested,
			Cyclomatic:    fn.Cyclomatic,
			FanOut:        fn.FanOut,
			Commits:       fc.Commits,
			HasTest:       hasTested,
		}
	}

	// Sort functions by risk descending.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Risk != results[j].Risk {
			return results[i].Risk > results[j].Risk
		}
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		return results[i].StartLine < results[j].StartLine
	})

	// Package aggregation.
	pkgMap := map[string][]FunctionRisk{}
	for _, fr := range results {
		pkg := filepath.Dir(fr.File)
		pkgMap[pkg] = append(pkgMap[pkg], fr)
	}

	packages := make([]PackageRisk, 0, len(pkgMap))
	for pkg, funcs := range pkgMap {
		pr := aggregatePackage(pkg, funcs)
		packages = append(packages, pr)
	}
	sort.SliceStable(packages, func(i, j int) bool {
		return packages[i].MaxRisk > packages[j].MaxRisk
	})

	// Summary.
	summary := computeSummary(results)

	return &Report{
		Functions: results,
		Packages:  packages,
		Summary:   summary,
	}, nil
}

// percentileRank converts raw values to percentile ranks in [0, 1].
func percentileRank(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return []float64{0.5}
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	ranks := make([]float64, n)
	for i, v := range values {
		pos := sort.SearchFloat64s(sorted, v)
		ranks[i] = float64(pos) / float64(n-1)
	}
	return ranks
}

// geometricMean computes the geometric mean of four values, clamping each to a
// minimum epsilon to prevent a zero in one dimension from collapsing the score.
func geometricMean(a, b, c, d float64) float64 {
	const epsilon = 0.01
	a = math.Max(a, epsilon)
	b = math.Max(b, epsilon)
	c = math.Max(c, epsilon)
	d = math.Max(d, epsilon)
	return math.Pow(a*b*c*d, 0.25)
}

// aggregatePackage computes aggregate risk statistics for a set of functions
// belonging to the same package.
func aggregatePackage(pkg string, funcs []FunctionRisk) PackageRisk {
	n := len(funcs)
	if n == 0 {
		return PackageRisk{Package: pkg}
	}

	var maxR, sumR float64
	highCount := 0
	risks := make([]float64, n)

	for i, fr := range funcs {
		risks[i] = fr.Risk
		sumR += fr.Risk
		if fr.Risk > maxR {
			maxR = fr.Risk
		}
		if fr.Risk > 0.7 {
			highCount++
		}
	}

	sort.Float64s(risks)
	p90Idx := int(float64(n-1) * 0.9)
	if p90Idx >= n {
		p90Idx = n - 1
	}

	return PackageRisk{
		Package:       pkg,
		MaxRisk:       maxR,
		P90Risk:       risks[p90Idx],
		AvgRisk:       sumR / float64(n),
		HighRiskCount: highCount,
		Functions:     n,
	}
}

// computeSummary calculates repo-wide aggregate risk statistics.
func computeSummary(funcs []FunctionRisk) RiskSummary {
	n := len(funcs)
	if n == 0 {
		return RiskSummary{}
	}

	var maxR, sumR float64
	highCount := 0
	risks := make([]float64, n)

	for i, fr := range funcs {
		risks[i] = fr.Risk
		sumR += fr.Risk
		if fr.Risk > maxR {
			maxR = fr.Risk
		}
		if fr.Risk > 0.7 {
			highCount++
		}
	}

	sort.Float64s(risks)
	p90Idx := int(float64(n-1) * 0.9)
	if p90Idx >= n {
		p90Idx = n - 1
	}

	return RiskSummary{
		TotalFunctions: n,
		MaxRisk:        maxR,
		P90Risk:        risks[p90Idx],
		AvgRisk:        sumR / float64(n),
		HighRiskCount:  highCount,
	}
}
