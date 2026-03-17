package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/impact"
)

func newImpactCmd() *cobra.Command {
	var cachePath string
	var jsonOutput bool
	var changed string
	var diffRef string
	var maxDepth int

	cmd := &cobra.Command{
		Use:     "impact [symbol] [path]",
		Aliases: []string{"gtsimpact"},
		Short:   "Compute blast radius of changed symbols via reverse call graph",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			switch len(args) {
			case 2:
				// gts impact <symbol> <path>
				if strings.TrimSpace(changed) == "" {
					changed = args[0]
				}
				target = args[1]
			case 1:
				// Heuristic: if it looks like a path (contains / or . or exists as dir), treat as path.
				// Otherwise treat as symbol name.
				if looksLikePath(args[0]) {
					target = args[0]
				} else if strings.TrimSpace(changed) == "" {
					changed = args[0]
				} else {
					target = args[0]
				}
			}

			idx, err := loadOrBuild(cachePath, target)
			if err != nil {
				return err
			}

			opts := impact.Options{
				DiffRef:  strings.TrimSpace(diffRef),
				Root:     target,
				MaxDepth: maxDepth,
			}

			if strings.TrimSpace(changed) != "" {
				parts := strings.Split(changed, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if part != "" {
						opts.Changed = append(opts.Changed, part)
					}
				}
			}

			result, err := impact.Analyze(idx, opts)
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(result)
			}

			for _, sym := range result.Affected {
				fmt.Printf(
					"%s:%d-%d %s distance=%d risk=%.2f\n",
					sym.File,
					sym.StartLine,
					sym.EndLine,
					sym.Name,
					sym.Distance,
					sym.Risk,
				)
			}
			fmt.Printf(
				"impact: changed=%d affected=%d files=%d\n",
				len(result.Changed),
				result.TotalAffected,
				len(result.AffectedFiles),
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&changed, "changed", "", "comma-separated list of changed symbol names")
	cmd.Flags().StringVar(&diffRef, "diff", "", "git diff ref (e.g. HEAD~1)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 10, "max reverse walk depth")
	return cmd
}

func looksLikePath(s string) bool {
	if strings.ContainsAny(s, "/\\") {
		return true
	}
	info, err := os.Stat(s)
	if err == nil && info.IsDir() {
		return true
	}
	return strings.HasSuffix(s, ".go") || strings.HasSuffix(s, ".py") ||
		strings.HasSuffix(s, ".ts") || strings.HasSuffix(s, ".js") ||
		strings.HasSuffix(s, ".rs") || strings.HasSuffix(s, ".java") ||
		s == "."
}
