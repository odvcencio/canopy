package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/similarity"
)

func newSimilarityCmd() *cobra.Command {
	var cacheA string
	var cacheB string
	var noCache bool
	var threshold float64
	var jsonOutput bool
	var method string
	var top int
	var maxFuncs int
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "similarity <path-a> [path-b]",
		Aliases: []string{"gtssimilarity", "sim"},
		Short:   "Find similar functions between codebases",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(method))
			switch mode {
			case "exact", "fuzzy", "both":
			default:
				return fmt.Errorf("unsupported --method %q (expected exact|fuzzy|both)", method)
			}

			pathA := args[0]
			idxA, err := loadOrBuild(cmd, cacheA, pathA, noCache)
			if err != nil {
				return fmt.Errorf("loading index A: %w", err)
			}
			idxA = applyGeneratedFilter(cmd, idxA)

			idxB := idxA
			pathB := pathA
			if len(args) == 2 {
				pathB = args[1]
				idxB, err = loadOrBuild(cmd, cacheB, pathB, noCache)
				if err != nil {
					return fmt.Errorf("loading index B: %w", err)
				}
				idxB = applyGeneratedFilter(cmd, idxB)
			}

			pairs, err := similarity.Compare(idxA, idxB, pathA, pathB, threshold, top, maxFuncs)
			if err != nil {
				return err
			}

			// Filter by method
			if mode != "both" {
				filtered := make([]similarity.Pair, 0, len(pairs))
				for _, p := range pairs {
					if mode == "exact" && p.Method == "exact" {
						filtered = append(filtered, p)
					} else if mode == "fuzzy" && p.Method == "ngram" {
						filtered = append(filtered, p)
					}
				}
				pairs = filtered
			}

			if countOnly {
				fmt.Println(len(pairs))
				return nil
			}

			if jsonOutput {
				return emitJSON(struct {
					Threshold float64           `json:"threshold"`
					Method    string            `json:"method"`
					Count     int               `json:"count"`
					Pairs     []similarity.Pair `json:"pairs,omitempty"`
				}{
					Threshold: threshold,
					Method:    mode,
					Count:     len(pairs),
					Pairs:     pairs,
				})
			}

			for _, p := range pairs {
				fmt.Printf(
					"%.2f %s %s:%d-%d %s <-> %s:%d-%d %s\n",
					p.Score,
					p.Method,
					p.A.File, p.A.StartLine, p.A.EndLine, p.A.Name,
					p.B.File, p.B.StartLine, p.B.EndLine, p.B.Name,
				)
			}
			fmt.Printf("similarity: threshold=%.2f method=%s matches=%d\n", threshold, mode, len(pairs))
			return nil
		},
	}

	cmd.Flags().StringVar(&cacheA, "cache-a", "", "load index A from cache instead of parsing")
	cmd.Flags().StringVar(&cacheB, "cache-b", "", "load index B from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.7, "minimum similarity score (0.0-1.0)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&method, "method", "both", "match method: exact|fuzzy|both")
	cmd.Flags().IntVar(&top, "top", 100, "limit to top N results (0 for all)")
	cmd.Flags().IntVar(&maxFuncs, "max-funcs", 2000, "max functions per index to compare (0 for all, keeps largest)")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of similar pairs")
	return cmd
}
