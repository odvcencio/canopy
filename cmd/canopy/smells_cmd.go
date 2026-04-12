package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/coupling"
	"github.com/odvcencio/canopy/pkg/smells"
	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
)

func newSmellsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var idFilter string
	var severity string
	var top int

	cmd := &cobra.Command{
		Use:   "smells [path]",
		Short: "Detect structural code smells from complexity, coupling, and type metrics",
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

			graph, err := xref.Build(idx)
			if err != nil {
				return err
			}

			compReport, compErr := complexity.Analyze(idx, idx.Root, complexity.Options{})
			if compErr == nil {
				complexity.EnrichWithXref(compReport, graph)
			}

			couplingReport, couplingErr := coupling.Analyze(idx, graph)

			typeReport, typeErr := typemetrics.Analyze(idx, idx.Root, graph)

			input := smells.Input{
				Index:     idx,
				XrefGraph: graph,
			}
			if compErr == nil {
				input.Complexity = compReport
			}
			if couplingErr == nil {
				input.Coupling = couplingReport
			}
			if typeErr == nil {
				input.Types = typeReport
			}

			report := smells.Detect(input)

			// Apply --id filter.
			if idFilter != "" {
				ids := map[string]bool{}
				for _, id := range strings.Split(idFilter, ",") {
					id = strings.TrimSpace(id)
					if id != "" {
						ids[id] = true
					}
				}
				filtered := report.Smells[:0]
				for _, s := range report.Smells {
					if ids[s.ID] {
						filtered = append(filtered, s)
					}
				}
				report.Smells = filtered
				report.Summary = smells.RecomputeSummary(report.Smells)
			}

			// Apply --severity filter.
			if severity != "" {
				sev := strings.ToLower(strings.TrimSpace(severity))
				filtered := report.Smells[:0]
				for _, s := range report.Smells {
					if s.Severity == sev {
						filtered = append(filtered, s)
					}
				}
				report.Smells = filtered
				report.Summary = smells.RecomputeSummary(report.Smells)
			}

			// Apply --top limit.
			if top > 0 && len(report.Smells) > top {
				report.Smells = report.Smells[:top]
				report.Summary = smells.RecomputeSummary(report.Smells)
			}

			if jsonOutput {
				return emitJSON(report)
			}

			for _, s := range report.Smells {
				loc := s.File
				if s.StartLine > 0 {
					loc = fmt.Sprintf("%s:%d", s.File, s.StartLine)
				}

				// Build signals string.
				signals := formatSignals(s.Signals)

				fmt.Printf("%-5s %-20s %-50s %s %s\n", s.Severity, s.ID, loc, s.Name, signals)
			}

			fmt.Printf("smells: total=%d errors=%d warnings=%d\n",
				report.Summary.Total,
				report.Summary.BySeverity["error"],
				report.Summary.BySeverity["warn"],
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&idFilter, "id", "", "comma-separated filter for smell IDs")
	cmd.Flags().StringVar(&severity, "severity", "", "filter by severity: error or warn")
	cmd.Flags().IntVar(&top, "top", 0, "limit output to top N smells (0 for all)")
	return cmd
}

func formatSignals(signals map[string]any) string {
	if len(signals) == 0 {
		return ""
	}
	keys := make([]string, 0, len(signals))
	for k := range signals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, signals[k]))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
