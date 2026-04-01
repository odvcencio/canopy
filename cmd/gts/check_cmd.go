package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/complexity"
)

type checkViolation struct {
	Check     string `json:"check"`
	File      string `json:"file"`
	Name      string `json:"name"`
	Line      int    `json:"line"`
	Value     int    `json:"value"`
	Threshold int    `json:"threshold"`
}

type checkResult struct {
	Status     string           `json:"status"`
	Checks     int              `json:"checks"`
	Violations int              `json:"violations"`
	Details    []checkViolation `json:"details,omitempty"`
}

func newCheckCmd() *cobra.Command {
	var (
		cachePath       string
		noCache         bool
		jsonOutput      bool
		maxCyclomatic   int
		maxCognitive    int
		maxLines        int
		maxGeneratedPct int
	)

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Run quality gates for CI -- exits non-zero on violations",
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
			// Filter to human code for complexity analysis.
			analysisIdx := applyGeneratedFilter(cmd, idx)

			var violations []checkViolation
			checksRun := 0

			// Checks 1-3 share a single complexity report.
			if maxCyclomatic > 0 || maxCognitive > 0 || maxLines > 0 {
				report, analyzeErr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{})

				// Check 1: Cyclomatic complexity.
				if maxCyclomatic > 0 {
					checksRun++
					if analyzeErr == nil {
						for _, fn := range report.Functions {
							if fn.Cyclomatic > maxCyclomatic {
								violations = append(violations, checkViolation{
									Check:     "cyclomatic",
									File:      fn.File,
									Name:      fn.Name,
									Line:      fn.StartLine,
									Value:     fn.Cyclomatic,
									Threshold: maxCyclomatic,
								})
							}
						}
					}
				}

				// Check 2: Cognitive complexity.
				if maxCognitive > 0 {
					checksRun++
					if analyzeErr == nil {
						for _, fn := range report.Functions {
							if fn.Cognitive > maxCognitive {
								violations = append(violations, checkViolation{
									Check:     "cognitive",
									File:      fn.File,
									Name:      fn.Name,
									Line:      fn.StartLine,
									Value:     fn.Cognitive,
									Threshold: maxCognitive,
								})
							}
						}
					}
				}

				// Check 3: Function length.
				if maxLines > 0 {
					checksRun++
					if analyzeErr == nil {
						for _, fn := range report.Functions {
							if fn.Lines > maxLines {
								violations = append(violations, checkViolation{
									Check:     "lines",
									File:      fn.File,
									Name:      fn.Name,
									Line:      fn.StartLine,
									Value:     fn.Lines,
									Threshold: maxLines,
								})
							}
						}
					}
				}
			}

			// Check 4: Generated ratio (uses full index, not filtered).
			if maxGeneratedPct > 0 {
				checksRun++
				totalFiles := idx.FileCount()
				genFiles := idx.GeneratedFileCount()
				if totalFiles > 0 {
					pct := genFiles * 100 / totalFiles
					if pct > maxGeneratedPct {
						violations = append(violations, checkViolation{
							Check:     "generated-ratio",
							File:      "",
							Name:      fmt.Sprintf("%d%% generated (%d/%d files)", pct, genFiles, totalFiles),
							Value:     pct,
							Threshold: maxGeneratedPct,
						})
					}
				}
			}

			result := checkResult{
				Status:     "PASS",
				Checks:     checksRun,
				Violations: len(violations),
				Details:    violations,
			}
			if len(violations) > 0 {
				result.Status = "FAIL"
			}

			if jsonOutput {
				if err := emitJSON(result); err != nil {
					return err
				}
			} else {
				fmt.Printf("check: %s (%d checks, %d violations)\n", result.Status, result.Checks, result.Violations)
				if len(violations) > 0 {
					fmt.Println("\nviolations:")
					for _, v := range violations {
						if v.File != "" {
							fmt.Printf("  %s: %s:%d %s value=%d (max=%d)\n", v.Check, v.File, v.Line, v.Name, v.Value, v.Threshold)
						} else {
							fmt.Printf("  %s: %s value=%d (max=%d)\n", v.Check, v.Name, v.Value, v.Threshold)
						}
					}
				}
			}

			if len(violations) > 0 {
				return exitCodeError{code: 1, err: fmt.Errorf("check failed with %d violations", len(violations))}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().IntVar(&maxCyclomatic, "max-cyclomatic", 50, "max cyclomatic complexity per function (0 to disable)")
	cmd.Flags().IntVar(&maxCognitive, "max-cognitive", 80, "max cognitive complexity per function (0 to disable)")
	cmd.Flags().IntVar(&maxLines, "max-lines", 300, "max lines per function (0 to disable)")
	cmd.Flags().IntVar(&maxGeneratedPct, "max-generated-pct", 60, "max % of files that are generated (0 to disable)")
	return cmd
}
