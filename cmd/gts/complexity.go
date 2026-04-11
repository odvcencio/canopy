package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/complexity"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

func newComplexityCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var minCyclomatic int
	var sortField string
	var top int
	var kind string

	cmd := &cobra.Command{
		Use:     "complexity [path]",
		Aliases: []string{"gtscomplexity"},
		Short:   "Analyze function complexity metrics across the codebase",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			sortField = strings.ToLower(strings.TrimSpace(sortField))
			switch sortField {
			case "", "cyclomatic", "cognitive", "lines", "nesting":
			default:
				return fmt.Errorf("unsupported --sort %q (expected cyclomatic|cognitive|lines|nesting)", sortField)
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			idx = applyGeneratedFilter(cmd, idx)

			opts := complexity.Options{
				MinCyclomatic: minCyclomatic,
				Sort:          sortField,
				Top:           top,
			}

			report, err := complexity.Analyze(idx, idx.Root, opts)
			if err != nil {
				return err
			}

			graph, err := xref.Build(idx)
			if err == nil {
				complexity.EnrichWithXref(report, graph)
			}

			if kind != "" {
				var prefix string
				switch strings.ToLower(kind) {
				case "function":
					prefix = "function"
				case "method":
					prefix = "method"
				default:
					return fmt.Errorf("unsupported --kind %q (expected function|method)", kind)
				}
				filtered := report.Functions[:0]
				for _, fn := range report.Functions {
					if strings.Contains(fn.Kind, prefix) {
						filtered = append(filtered, fn)
					}
				}
				report.Functions = filtered
				report.Summary.Count = len(filtered)
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count int `json:"count"`
					}{
						Count: report.Summary.Count,
					})
				}
				return emitJSON(report)
			}

			if countOnly {
				fmt.Println(report.Summary.Count)
				return nil
			}

			for _, fn := range report.Functions {
				label := symbolLabel(fn.Name, "")
				fmt.Printf(
					"%s:%d:%d %s %s cyc=%d cog=%d lines=%d nesting=%d params=%d fan_in=%d fan_out=%d\n",
					fn.File,
					fn.StartLine,
					fn.EndLine,
					fn.Kind,
					label,
					fn.Cyclomatic,
					fn.Cognitive,
					fn.Lines,
					fn.MaxNesting,
					fn.Parameters,
					fn.FanIn,
					fn.FanOut,
				)
			}

			fmt.Printf(
				"complexity: count=%d avg_cyc=%.1f max_cyc=%d p90_cyc=%d avg_cog=%.1f max_cog=%d avg_lines=%.1f max_lines=%d avg_nesting=%.1f\n",
				report.Summary.Count,
				report.Summary.AvgCyclomatic,
				report.Summary.MaxCyclomatic,
				report.Summary.P90Cyclomatic,
				report.Summary.AvgCognitive,
				report.Summary.MaxCognitive,
				report.Summary.AvgLines,
				report.Summary.MaxLines,
				report.Summary.AvgMaxNesting,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the number of functions analyzed")
	cmd.Flags().IntVar(&minCyclomatic, "min-cyclomatic", 0, "minimum cyclomatic complexity to include")
	cmd.Flags().StringVar(&sortField, "sort", "cyclomatic", "sort by cyclomatic|cognitive|lines|nesting")
	cmd.Flags().IntVar(&top, "top", 0, "limit output to top N functions")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by symbol kind (function|method)")
	return cmd
}
