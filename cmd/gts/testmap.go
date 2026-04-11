package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/testmap"
)

func newTestmapCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var kind string
	var untestedOnly bool
	var jsonOutput bool
	var countOnly bool
	var limit int

	cmd := &cobra.Command{
		Use:     "testmap [path]",
		Aliases: []string{"gtstestmap"},
		Short:   "Map test functions to implementation functions via call graph",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(kind))
			switch mode {
			case "function", "method", "":
			default:
				return fmt.Errorf("unsupported --kind %q (expected function|method or empty for all)", kind)
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			report, err := testmap.Map(idx, testmap.Options{
				UntestedOnly: untestedOnly,
				Kind:         mode,
			})
			if err != nil {
				return err
			}

			truncated := false
			if limit > 0 && len(report.Mappings) > limit {
				report.Mappings = report.Mappings[:limit]
				truncated = true
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						TestedCount   int     `json:"tested_count"`
						UntestedCount int     `json:"untested_count"`
						Coverage      float64 `json:"coverage"`
						Truncated     bool    `json:"truncated,omitempty"`
					}{
						TestedCount:   report.TestedCount,
						UntestedCount: report.UntestedCount,
						Coverage:      report.Coverage,
						Truncated:     truncated,
					})
				}
				if truncated {
					return emitJSON(struct {
						TestedCount   int                    `json:"tested_count"`
						UntestedCount int                    `json:"untested_count"`
						Coverage      float64                `json:"coverage"`
						Truncated     bool                   `json:"truncated"`
						Mappings      []testmap.TestMapping  `json:"mappings,omitempty"`
					}{
						TestedCount:   report.TestedCount,
						UntestedCount: report.UntestedCount,
						Coverage:      report.Coverage,
						Truncated:     true,
						Mappings:      report.Mappings,
					})
				}
				return emitJSON(report)
			}

			if countOnly {
				fmt.Printf("tested=%d untested=%d coverage=%.1f%%\n",
					report.TestedCount, report.UntestedCount, report.Coverage*100)
				if truncated {
					fmt.Printf("truncated: limit=%d\n", limit)
				}
				return nil
			}

			for _, m := range report.Mappings {
				if len(m.Tests) == 0 {
					fmt.Printf("%s:%d-%d %s [untested]\n",
						m.File, m.StartLine, m.EndLine, m.Symbol)
				} else {
					var parts []string
					for _, ref := range m.Tests {
						parts = append(parts, fmt.Sprintf("%s (d=%d)", ref.Name, ref.Distance))
					}
					fmt.Printf("%s:%d-%d %s [%s] tests: %s\n",
						m.File, m.StartLine, m.EndLine, m.Symbol, m.Coverage,
						strings.Join(parts, ", "))
				}
			}
			fmt.Printf("testmap: tested=%d untested=%d coverage=%.1f%%\n",
				report.TestedCount, report.UntestedCount, report.Coverage*100)
			if truncated {
				fmt.Fprintf(os.Stderr, "warning: results truncated at limit=%d, use --limit 0 for all\n", limit)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by function|method (empty for all)")
	cmd.Flags().BoolVar(&untestedOnly, "untested-only", false, "show only untested symbols")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print summary counts only")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of results (0 for unlimited)")
	return cmd
}

func runTestmap(args []string) error {
	cmd := newTestmapCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
