package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/reachability"
)

func newReachabilityCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var category string
	var attackID string
	var depth int

	cmd := &cobra.Command{
		Use:   "reachability <package> [path]",
		Short: "Check whether a package transitively reaches sensitive capabilities",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg := args[0]
			target := "."
			if len(args) == 2 {
				target = args[1]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			opts := reachability.Options{
				Category: category,
				AttackID: attackID,
				Depth:    depth,
			}

			result, err := reachability.Analyze(idx, pkg, opts)
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(result)
			}

			if len(result.Findings) == 0 {
				fmt.Printf("reachability: %s — no capabilities reached\n", result.Package)
				return nil
			}

			fmt.Printf("reachability: %s — %d findings\n", result.Package, len(result.Findings))
			for _, f := range result.Findings {
				chain := formatReachPath(f.ReachPath)
				attackSuffix := ""
				if f.AttackID != "" {
					attackSuffix = fmt.Sprintf(" (ATT&CK: %s)", f.AttackID)
				}
				fmt.Printf("  [%s] %s%s\n", f.Category, chain, attackSuffix)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&category, "category", "", "filter by capability category")
	cmd.Flags().StringVar(&attackID, "attack-id", "", "filter by MITRE ATT&CK ID")
	cmd.Flags().IntVar(&depth, "depth", 10, "max traversal depth")
	return cmd
}

// formatReachPath renders a reach path as "pkg.Func -> pkg.Func -> ..."
func formatReachPath(path []reachability.Path) string {
	parts := make([]string, 0, len(path))
	for _, p := range path {
		label := p.Function
		if p.Package != "" {
			label = p.Package + "." + p.Function
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " -> ")
}
