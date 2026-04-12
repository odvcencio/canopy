package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/hotspot"
)

func newHotspotCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var since string
	var top int

	cmd := &cobra.Command{
		Use:     "hotspot [path]",
		Aliases: []string{"gtshotspot"},
		Short:   "Detect code hotspots from git churn, complexity, and call graph centrality",
		Args:    cobra.MaximumNArgs(1),
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

			opts := hotspot.Options{
				Root:  target,
				Since: since,
				Top:   top,
			}

			report, err := hotspot.Analyze(idx, opts)
			if err != nil {
				return err
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count int `json:"count"`
					}{Count: report.Count})
				}
				return emitJSON(report)
			}

			if countOnly {
				fmt.Println(report.Count)
				return nil
			}

			for _, h := range report.Functions {
				fmt.Printf(
					"%s:%d-%d %s  score=%.3f churn=%.2f complexity=%.2f centrality=%.2f  commits=%d authors=%d cyclomatic=%d fan_in=%d\n",
					h.File, h.StartLine, h.EndLine, h.Name,
					h.Score, h.Churn, h.Complexity, h.Centrality,
					h.Commits, h.Authors, h.Cyclomatic, h.FanIn,
				)
			}
			fmt.Printf("hotspot: count=%d\n", report.Count)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of hotspots")
	cmd.Flags().StringVar(&since, "since", "90d", "git log period (e.g. 90d, 6m, 1y)")
	cmd.Flags().IntVar(&top, "top", 20, "limit to top N results (0 for all)")
	return cmd
}
