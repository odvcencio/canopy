package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/risk"
	"github.com/odvcencio/canopy/pkg/testmap"
	"github.com/odvcencio/canopy/pkg/xref"
)

func newRiskCmd() *cobra.Command {
	var (
		cachePath  string
		noCache    bool
		jsonOutput bool
		top        int
		minRisk    float64
		since      string
		byPackage  bool
	)

	cmd := &cobra.Command{
		Use:   "risk [path]",
		Short: "Compute composite risk scores from complexity, coupling, churn, and test coverage",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			idx = applyGeneratedFilter(cmd, idx)

			// Build xref graph.
			graph, err := xref.Build(idx)
			if err != nil {
				return fmt.Errorf("xref build: %w", err)
			}

			// Run complexity analysis and enrich with xref.
			compReport, err := complexity.Analyze(idx, idx.Root, complexity.Options{})
			if err != nil {
				return fmt.Errorf("complexity analysis: %w", err)
			}
			complexity.EnrichWithXref(compReport, graph)

			// Build test coverage map.
			testMapLookup := buildTestMapLookup(idx)

			// Run risk analysis.
			report, err := risk.Analyze(risk.Input{
				Index:      idx,
				Root:       target,
				Complexity: compReport,
				XrefGraph:  graph,
				TestMap:    testMapLookup,
				Since:      since,
			})
			if err != nil {
				return fmt.Errorf("risk analysis: %w", err)
			}

			// Apply --min-risk filter.
			if minRisk > 0 {
				filtered := report.Functions[:0]
				for _, fn := range report.Functions {
					if fn.Risk >= minRisk {
						filtered = append(filtered, fn)
					}
				}
				report.Functions = filtered

				filteredPkgs := report.Packages[:0]
				for _, pkg := range report.Packages {
					if pkg.MaxRisk >= minRisk {
						filteredPkgs = append(filteredPkgs, pkg)
					}
				}
				report.Packages = filteredPkgs
			}

			// Apply --top limit.
			if top > 0 {
				if len(report.Functions) > top {
					report.Functions = report.Functions[:top]
				}
				if len(report.Packages) > top {
					report.Packages = report.Packages[:top]
				}
			}

			if jsonOutput {
				return emitJSON(report)
			}

			if byPackage {
				for _, pkg := range report.Packages {
					fmt.Printf("%.2f %-40s max=%.2f p90=%.2f avg=%.2f high_risk=%d functions=%d\n",
						pkg.MaxRisk,
						pkg.Package,
						pkg.MaxRisk,
						pkg.P90Risk,
						pkg.AvgRisk,
						pkg.HighRiskCount,
						pkg.Functions,
					)
				}
			} else {
				for _, fn := range report.Functions {
					testLabel := "untested"
					if fn.HasTest {
						testLabel = "tested"
					}
					fmt.Printf("%.2f %-40s %-20s cyc=%d fan_out=%d commits=%d %s\n",
						fn.Risk,
						fmt.Sprintf("%s:%d", fn.File, fn.StartLine),
						fn.Name,
						fn.Cyclomatic,
						fn.FanOut,
						fn.Commits,
						testLabel,
					)
				}
			}

			fmt.Printf("risk: functions=%d max=%.2f p90=%.2f avg=%.2f high_risk=%d\n",
				report.Summary.TotalFunctions,
				report.Summary.MaxRisk,
				report.Summary.P90Risk,
				report.Summary.AvgRisk,
				report.Summary.HighRiskCount,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().IntVar(&top, "top", 20, "limit output to top N results (0 for all)")
	cmd.Flags().Float64Var(&minRisk, "min-risk", 0, "minimum risk score to include")
	cmd.Flags().StringVar(&since, "since", "90d", "git log period for churn (e.g. 90d, 6m, 1y)")
	cmd.Flags().BoolVar(&byPackage, "by-package", false, "aggregate results by package")
	return cmd
}

// buildTestMapLookup constructs a test coverage lookup map from testmap analysis.
// Returns nil if testmap analysis fails (untested signal defaults to 1.0 for all functions).
func buildTestMapLookup(idx *model.Index) map[string]bool {
	tmReport, err := testmap.Map(idx, testmap.Options{})
	if err != nil || tmReport == nil {
		return nil
	}
	lookup := make(map[string]bool, len(tmReport.Mappings))
	for _, m := range tmReport.Mappings {
		if m.Coverage == "tested" || m.Coverage == "indirectly_tested" {
			key := fmt.Sprintf("%s\x00%s\x00%d", m.File, m.Symbol, m.StartLine)
			lookup[key] = true
		}
	}
	return lookup
}
