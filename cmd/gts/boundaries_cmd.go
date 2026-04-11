package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/boundaries"
	"github.com/odvcencio/gts-suite/pkg/sarif"
)

type boundaryResult struct {
	Status     string                `json:"status"`
	Violations int                   `json:"violations"`
	Base       string                `json:"base,omitempty"`
	Details    []boundaries.Violation `json:"details,omitempty"`
}

func newBoundariesCmd() *cobra.Command {
	var (
		cachePath  string
		noCache    bool
		jsonOutput bool
		format     string
		base       string
	)

	cmd := &cobra.Command{
		Use:   "boundaries [path]",
		Short: "Check module boundary rules defined in .gtsboundaries",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			// Load boundary config.
			cfg, err := boundaries.LoadConfig(target)
			if err != nil {
				return err
			}
			if cfg == nil {
				result := boundaryResult{Status: "SKIP"}
				if jsonOutput {
					return emitJSON(result)
				}
				fmt.Println("boundaries: SKIP (no .gtsboundaries found)")
				return nil
			}

			// Build the structural index.
			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			// Build the import dependency graph.
			report, err := deps.Build(idx, deps.Options{
				Mode:         "package",
				IncludeEdges: true,
			})
			if err != nil {
				return err
			}

			// Convert dep edges to boundary ImportEdge structs.
			edges := make([]boundaries.ImportEdge, 0, len(report.Edges))
			for _, e := range report.Edges {
				if !e.Internal {
					continue
				}
				edges = append(edges, boundaries.ImportEdge{
					From: e.From,
					To:   e.To,
				})
			}

			// Evaluate boundary rules.
			violations := boundaries.Evaluate(cfg, edges)

			// Filter to changed files when --base is set.
			if base != "" {
				changed, diffErr := changedFiles(base, target)
				if diffErr != nil {
					return diffErr
				}
				var filtered []boundaries.Violation
				for _, v := range violations {
					for path := range changed {
						pkg := filepath.Dir(path)
						if strings.HasPrefix(pkg, v.From) {
							filtered = append(filtered, v)
							break
						}
					}
				}
				violations = filtered
			}

			result := boundaryResult{
				Status:     "PASS",
				Violations: len(violations),
				Base:       base,
				Details:    violations,
			}
			if len(violations) > 0 {
				result.Status = "FAIL"
			}

			// Resolve output format: --json implies "json" for backward compat.
			outputFmt := format
			if jsonOutput && outputFmt == "text" {
				outputFmt = "json"
			}

			switch outputFmt {
			case "sarif":
				log := sarif.NewLog()
				log.Runs[0].Tool.Driver.Version = version
				log.AddRule("boundary-violation", "Module boundary violation")
				for _, v := range violations {
					log.AddResult("boundary-violation", "error", v.Message, v.From, 0, 0)
				}
				if err := log.Encode(os.Stdout); err != nil {
					return err
				}
			case "json":
				if err := emitJSON(result); err != nil {
					return err
				}
			default:
				if base != "" {
					fmt.Printf("boundaries: %s (%d violations, base=%s)\n", result.Status, result.Violations, base)
				} else {
					fmt.Printf("boundaries: %s (%d violations)\n", result.Status, result.Violations)
				}
				for _, v := range violations {
					fmt.Printf("  %s\n", v.Message)
				}
			}

			if len(violations) > 0 {
				return exitCodeError{code: 1, err: fmt.Errorf("boundaries check failed with %d violations", len(violations))}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, json, sarif")
	cmd.Flags().StringVar(&base, "base", "", "git ref to diff against -- only report violations in changed files")
	return cmd
}
