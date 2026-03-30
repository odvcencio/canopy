package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/complexity"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

type summaryDashboard struct {
	Files       fileSummaryInfo       `json:"files"`
	Symbols     symbolSummaryInfo     `json:"symbols"`
	References  refSummaryInfo        `json:"references"`
	ParseErrors parseSummaryInfo      `json:"parse_errors"`
	CallGraph   callGraphSummaryInfo  `json:"call_graph"`
	Complexity  complexitySummaryInfo `json:"complexity"`
	TopComplex  []topFunction         `json:"top_complex"`
	TopFanIn    []topFunction         `json:"top_fan_in"`
}

type fileSummaryInfo struct {
	Total      int            `json:"total"`
	ByLanguage map[string]int `json:"by_language"`
}

type symbolSummaryInfo struct {
	Total  int            `json:"total"`
	ByKind map[string]int `json:"by_kind"`
}

type refSummaryInfo struct {
	Total int `json:"total"`
}

type parseSummaryInfo struct {
	Count int `json:"count"`
}

type callGraphSummaryInfo struct {
	TotalEdges     int     `json:"total_edges"`
	Unresolved     int     `json:"unresolved"`
	ResolutionRate float64 `json:"resolution_rate"`
}

type complexitySummaryInfo struct {
	AvgCyclomatic float64 `json:"avg_cyclomatic"`
	MaxCyclomatic int     `json:"max_cyclomatic"`
	P90Cyclomatic int     `json:"p90_cyclomatic"`
	AvgCognitive  float64 `json:"avg_cognitive"`
	MaxCognitive  int     `json:"max_cognitive"`
}

type topFunction struct {
	File      string `json:"file"`
	Name      string `json:"name"`
	StartLine int    `json:"start_line"`
	Value     int    `json:"value"`
}

func newSummaryCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "summary [path]",
		Short: "Aggregate structural metrics dashboard for a codebase",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cachePath, target, noCache)
			if err != nil {
				return err
			}

			// Files breakdown
			langCounts := map[string]int{}
			for _, f := range idx.Files {
				lang := f.Language
				if lang == "" {
					lang = "unknown"
				}
				langCounts[lang]++
			}

			// Symbols breakdown
			kindCounts := map[string]int{}
			totalSymbols := 0
			for _, f := range idx.Files {
				for _, sym := range f.Symbols {
					kindCounts[sym.Kind]++
					totalSymbols++
				}
			}

			// References
			totalRefs := idx.ReferenceCount()

			// Parse errors
			parseErrors := len(idx.Errors)

			// Call graph
			graph, err := xref.Build(idx)
			if err != nil {
				return fmt.Errorf("building xref graph: %w", err)
			}
			totalEdges := len(graph.Edges)
			unresolvedCount := len(graph.Unresolved)
			totalAttempts := totalEdges + unresolvedCount
			resolutionRate := 0.0
			if totalAttempts > 0 {
				resolutionRate = float64(totalEdges) / float64(totalAttempts)
			}

			// Complexity
			report, err := complexity.Analyze(idx, idx.Root, complexity.Options{})
			if err != nil {
				return fmt.Errorf("complexity analysis: %w", err)
			}
			complexity.EnrichWithXref(report, graph)

			// Top 5 most complex (by cyclomatic)
			topComplex := topNByField(report.Functions, 5, func(f complexity.FunctionMetrics) int {
				return f.Cyclomatic
			})

			// Top 5 highest fan-in
			topFanIn := topNByField(report.Functions, 5, func(f complexity.FunctionMetrics) int {
				return f.FanIn
			})

			dashboard := summaryDashboard{
				Files: fileSummaryInfo{
					Total:      len(idx.Files),
					ByLanguage: langCounts,
				},
				Symbols: symbolSummaryInfo{
					Total:  totalSymbols,
					ByKind: kindCounts,
				},
				References:  refSummaryInfo{Total: totalRefs},
				ParseErrors: parseSummaryInfo{Count: parseErrors},
				CallGraph: callGraphSummaryInfo{
					TotalEdges:     totalEdges,
					Unresolved:     unresolvedCount,
					ResolutionRate: resolutionRate,
				},
				Complexity: complexitySummaryInfo{
					AvgCyclomatic: report.Summary.AvgCyclomatic,
					MaxCyclomatic: report.Summary.MaxCyclomatic,
					P90Cyclomatic: report.Summary.P90Cyclomatic,
					AvgCognitive:  report.Summary.AvgCognitive,
					MaxCognitive:  report.Summary.MaxCognitive,
				},
				TopComplex: topComplex,
				TopFanIn:   topFanIn,
			}

			if jsonOutput {
				return emitJSON(dashboard)
			}

			// Text dashboard
			fmt.Println("=== Codebase Summary ===")
			fmt.Println()

			fmt.Printf("Files: %d total\n", dashboard.Files.Total)
			for _, lang := range sortedKeys(langCounts) {
				fmt.Printf("  %-20s %d\n", lang, langCounts[lang])
			}
			fmt.Println()

			fmt.Printf("Symbols: %d total\n", dashboard.Symbols.Total)
			for _, kind := range sortedKeys(kindCounts) {
				fmt.Printf("  %-30s %d\n", kind, kindCounts[kind])
			}
			fmt.Println()

			fmt.Printf("References: %d total\n", dashboard.References.Total)
			fmt.Printf("Parse errors: %d\n", dashboard.ParseErrors.Count)
			fmt.Println()

			fmt.Println("Call Graph:")
			fmt.Printf("  Edges:           %d\n", dashboard.CallGraph.TotalEdges)
			fmt.Printf("  Unresolved:      %d\n", dashboard.CallGraph.Unresolved)
			fmt.Printf("  Resolution rate: %.1f%%\n", dashboard.CallGraph.ResolutionRate*100)
			fmt.Println()

			fmt.Println("Complexity:")
			fmt.Printf("  Cyclomatic  avg=%.1f  max=%d  p90=%d\n",
				dashboard.Complexity.AvgCyclomatic,
				dashboard.Complexity.MaxCyclomatic,
				dashboard.Complexity.P90Cyclomatic,
			)
			fmt.Printf("  Cognitive   avg=%.1f  max=%d\n",
				dashboard.Complexity.AvgCognitive,
				dashboard.Complexity.MaxCognitive,
			)
			fmt.Println()

			fmt.Println("Top 5 Most Complex Functions:")
			for i, f := range dashboard.TopComplex {
				fmt.Printf("  %d. %s:%d %s (cyclomatic=%d)\n", i+1, f.File, f.StartLine, f.Name, f.Value)
			}
			fmt.Println()

			fmt.Println("Top 5 Highest Fan-In Functions:")
			for i, f := range dashboard.TopFanIn {
				fmt.Printf("  %d. %s:%d %s (fan_in=%d)\n", i+1, f.File, f.StartLine, f.Name, f.Value)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}

func topNByField(functions []complexity.FunctionMetrics, n int, field func(complexity.FunctionMetrics) int) []topFunction {
	type ranked struct {
		fn    complexity.FunctionMetrics
		value int
	}
	items := make([]ranked, len(functions))
	for i, fn := range functions {
		items[i] = ranked{fn: fn, value: field(fn)}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].value > items[j].value
	})
	if n > len(items) {
		n = len(items)
	}
	result := make([]topFunction, n)
	for i := 0; i < n; i++ {
		result[i] = topFunction{
			File:      items[i].fn.File,
			Name:      items[i].fn.Name,
			StartLine: items[i].fn.StartLine,
			Value:     items[i].value,
		}
	}
	return result
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
