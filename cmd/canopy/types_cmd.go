package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
)

func newTypeMetricsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var sortField string
	var top int
	var minFields int

	cmd := &cobra.Command{
		Use:   "types [path]",
		Short: "Analyze type-level structural metrics (struct fields, interface width, method set)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			sortField = strings.ToLower(strings.TrimSpace(sortField))
			switch sortField {
			case "", "fields", "interface_width", "method_set", "nesting":
			default:
				return fmt.Errorf("unsupported --sort %q (expected fields|interface_width|method_set|nesting)", sortField)
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

			report, err := typemetrics.Analyze(idx, idx.Root, graph)
			if err != nil {
				return err
			}

			// Sort types.
			switch sortField {
			case "fields", "":
				sort.Slice(report.Types, func(i, j int) bool {
					return report.Types[i].Fields > report.Types[j].Fields
				})
			case "interface_width":
				sort.Slice(report.Types, func(i, j int) bool {
					return report.Types[i].InterfaceWidth > report.Types[j].InterfaceWidth
				})
			case "method_set":
				sort.Slice(report.Types, func(i, j int) bool {
					return report.Types[i].MethodSetSize > report.Types[j].MethodSetSize
				})
			case "nesting":
				sort.Slice(report.Types, func(i, j int) bool {
					return report.Types[i].NestingDepth > report.Types[j].NestingDepth
				})
			}

			// Filter by minimum fields.
			if minFields > 0 {
				filtered := report.Types[:0]
				for _, t := range report.Types {
					if t.Fields >= minFields {
						filtered = append(filtered, t)
					}
				}
				report.Types = filtered
			}

			// Truncate to top N.
			if top > 0 && len(report.Types) > top {
				report.Types = report.Types[:top]
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count int `json:"count"`
					}{Count: report.Summary.Count})
				}
				return emitJSON(report)
			}

			if countOnly {
				fmt.Println(report.Summary.Count)
				return nil
			}

			for _, t := range report.Types {
				fmt.Printf(
					"%s:%d %s %s fields=%d methods=%d nesting=%d\n",
					t.File,
					t.StartLine,
					t.Kind,
					t.Name,
					t.Fields,
					t.MethodSetSize,
					t.NestingDepth,
				)
			}

			fmt.Printf(
				"types: count=%d avg_fields=%.1f max_fields=%d avg_methods=%.1f max_methods=%d\n",
				report.Summary.Count,
				report.Summary.AvgFields,
				report.Summary.MaxFields,
				report.Summary.AvgMethodSet,
				report.Summary.MaxMethodSet,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the number of types analyzed")
	cmd.Flags().StringVar(&sortField, "sort", "fields", "sort by fields|interface_width|method_set|nesting")
	cmd.Flags().IntVar(&top, "top", 0, "limit output to top N types (0 for all)")
	cmd.Flags().IntVar(&minFields, "min-fields", 0, "minimum field count to include (0 for all)")
	return cmd
}
