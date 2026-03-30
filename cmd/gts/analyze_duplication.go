package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/similarity"
)

func newDuplicationCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var limit int
	var threshold float64
	var maxFuncs int

	cmd := &cobra.Command{
		Use:   "duplication [path]",
		Short: "Find duplicated or near-identical functions within the codebase",
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

			pairs, err := similarity.Compare(idx, idx, target, target, threshold, limit, maxFuncs)
			if err != nil {
				return err
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count int `json:"count"`
					}{Count: len(pairs)})
				}
				return emitJSON(struct {
					Threshold float64           `json:"threshold"`
					Limit     int               `json:"limit"`
					Count     int               `json:"count"`
					Pairs     []similarity.Pair `json:"pairs,omitempty"`
				}{
					Threshold: threshold,
					Limit:     limit,
					Count:     len(pairs),
					Pairs:     pairs,
				})
			}

			if countOnly {
				fmt.Println(len(pairs))
				return nil
			}

			for _, p := range pairs {
				fmt.Printf(
					"%s:%d %s \u2194 %s:%d %s (score=%.2f, method=%s)\n",
					p.A.File, p.A.StartLine, p.A.Name,
					p.B.File, p.B.StartLine, p.B.Name,
					p.Score, p.Method,
				)
			}
			fmt.Printf("duplication: threshold=%.2f pairs=%d\n", threshold, len(pairs))
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the number of duplicate pairs found")
	cmd.Flags().IntVar(&limit, "limit", 20, "limit to top N results (0 for all)")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.8, "minimum similarity threshold (0.0-1.0)")
	cmd.Flags().IntVar(&maxFuncs, "max-functions", 500, "max functions to compare (0 for all, keeps largest)")
	return cmd
}
