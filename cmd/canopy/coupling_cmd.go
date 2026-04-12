package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/coupling"
	"github.com/odvcencio/canopy/pkg/xref"
)

func newCouplingCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var sortField string
	var top int
	var minDistance float64

	cmd := &cobra.Command{
		Use:     "coupling [path]",
		Aliases: []string{"canopycoupling"},
		Short:   "Analyze package-level coupling, instability, and cohesion metrics",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			sortField = strings.ToLower(strings.TrimSpace(sortField))
			switch sortField {
			case "", "distance", "instability", "lcom", "ca", "ce":
			default:
				return fmt.Errorf("unsupported --sort %q (expected instability|distance|lcom|ca|ce)", sortField)
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

			report, err := coupling.Analyze(idx, graph)
			if err != nil {
				return err
			}

			// Sort packages.
			switch sortField {
			case "instability":
				sort.Slice(report.Packages, func(i, j int) bool {
					return report.Packages[i].Instability > report.Packages[j].Instability
				})
			case "distance", "":
				sort.Slice(report.Packages, func(i, j int) bool {
					return report.Packages[i].Distance > report.Packages[j].Distance
				})
			case "lcom":
				sort.Slice(report.Packages, func(i, j int) bool {
					return report.Packages[i].LCOM > report.Packages[j].LCOM
				})
			case "ca":
				sort.Slice(report.Packages, func(i, j int) bool {
					return report.Packages[i].Ca > report.Packages[j].Ca
				})
			case "ce":
				sort.Slice(report.Packages, func(i, j int) bool {
					return report.Packages[i].Ce > report.Packages[j].Ce
				})
			}

			// Filter by minimum distance.
			if minDistance > 0 {
				filtered := report.Packages[:0]
				for _, p := range report.Packages {
					if p.Distance >= minDistance {
						filtered = append(filtered, p)
					}
				}
				report.Packages = filtered
			}

			// Truncate to top N.
			if top > 0 && len(report.Packages) > top {
				report.Packages = report.Packages[:top]
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

			for _, p := range report.Packages {
				fmt.Printf(
					"%-20s ca=%d ce=%d I=%.2f A=%.2f D=%.2f lcom=%d\n",
					p.Package,
					p.Ca,
					p.Ce,
					p.Instability,
					p.Abstractness,
					p.Distance,
					p.LCOM,
				)
			}

			fmt.Printf(
				"coupling: packages=%d avg_I=%.2f max_I=%.2f avg_D=%.2f max_D=%.2f avg_lcom=%.1f max_lcom=%d\n",
				report.Summary.Count,
				report.Summary.AvgInstability,
				report.Summary.MaxInstability,
				report.Summary.AvgDistance,
				report.Summary.MaxDistance,
				report.Summary.AvgLCOM,
				report.Summary.MaxLCOM,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the number of packages analyzed")
	cmd.Flags().StringVar(&sortField, "sort", "distance", "sort by instability|distance|lcom|ca|ce")
	cmd.Flags().IntVar(&top, "top", 0, "limit output to top N packages (0 for all)")
	cmd.Flags().Float64Var(&minDistance, "min-distance", 0, "minimum distance from main sequence to include")
	return cmd
}
