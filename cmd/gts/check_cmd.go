package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/lint"
	"github.com/odvcencio/gts-suite/pkg/complexity"
	"github.com/odvcencio/gts-suite/pkg/sarif"
)

// changedFiles runs git diff --name-only against the given base ref and returns
// the set of file paths that differ.
func changedFiles(base, repoDir string) (map[string]bool, error) {
	cmd := exec.Command("git", "-C", repoDir, "diff", "--name-only", base)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w", base, err)
	}
	files := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files[line] = true
		}
	}
	return files, nil
}

type checkViolation struct {
	Check     string `json:"check"`
	File      string `json:"file"`
	Name      string `json:"name"`
	Line      int    `json:"line"`
	Value     int    `json:"value"`
	Threshold int    `json:"threshold"`
}

type checkResult struct {
	Status       string           `json:"status"`
	Checks       int              `json:"checks"`
	Violations   int              `json:"violations"`
	Base         string           `json:"base,omitempty"`
	ChangedFiles int              `json:"changed_files,omitempty"`
	Details      []checkViolation `json:"details,omitempty"`
}

func newCheckCmd() *cobra.Command {
	var (
		cachePath       string
		noCache         bool
		jsonOutput      bool
		format          string
		base            string
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

			lintCfg, cfgErr := lint.LoadConfig(target)
			if cfgErr != nil {
				return fmt.Errorf("loading .gtslint: %w", cfgErr)
			}
			if lintCfg != nil {
				for _, override := range lintCfg.Overrides {
					if override.Scope != "" {
						continue
					}
					switch override.Metric {
					case "cyclomatic":
						if !cmd.Flags().Changed("max-cyclomatic") {
							maxCyclomatic = override.Threshold
						}
					case "cognitive":
						if !cmd.Flags().Changed("max-cognitive") {
							maxCognitive = override.Threshold
						}
					case "lines":
						if !cmd.Flags().Changed("max-lines") {
							maxLines = override.Threshold
						}
					}
				}
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
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

			// When --base is set, restrict violations to changed files only.
			var numChanged int
			if base != "" {
				changed, diffErr := changedFiles(base, target)
				if diffErr != nil {
					return diffErr
				}
				numChanged = len(changed)
				var filtered []checkViolation
				for _, v := range violations {
					if v.File == "" || changed[v.File] {
						filtered = append(filtered, v)
					}
				}
				violations = filtered
			}

			result := checkResult{
				Status:       "PASS",
				Checks:       checksRun,
				Violations:   len(violations),
				Base:         base,
				ChangedFiles: numChanged,
				Details:      violations,
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
				seen := map[string]bool{}
				for _, v := range violations {
					if !seen[v.Check] {
						log.AddRule(v.Check, v.Check+" threshold exceeded")
						seen[v.Check] = true
					}
					msg := fmt.Sprintf("%s %s value=%d (max=%d)", v.File, v.Name, v.Value, v.Threshold)
					log.AddResult(v.Check, "error", msg, v.File, v.Line, 0)
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
					fmt.Printf("check: %s (%d checks, %d violations, base=%s, %d files changed)\n", result.Status, result.Checks, result.Violations, base, numChanged)
				} else {
					fmt.Printf("check: %s (%d checks, %d violations)\n", result.Status, result.Checks, result.Violations)
				}
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
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, json, sarif")
	cmd.Flags().StringVar(&base, "base", "", "git ref to diff against -- only report violations in changed files")
	cmd.Flags().IntVar(&maxCyclomatic, "max-cyclomatic", 50, "max cyclomatic complexity per function (0 to disable)")
	cmd.Flags().IntVar(&maxCognitive, "max-cognitive", 80, "max cognitive complexity per function (0 to disable)")
	cmd.Flags().IntVar(&maxLines, "max-lines", 300, "max lines per function (0 to disable)")
	cmd.Flags().IntVar(&maxGeneratedPct, "max-generated-pct", 60, "max % of files that are generated (0 to disable)")
	return cmd
}
