package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/internal/lint"
	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/coupling"
	"github.com/odvcencio/canopy/pkg/risk"
	"github.com/odvcencio/canopy/pkg/sarif"
	"github.com/odvcencio/canopy/pkg/smells"
	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
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
	Check          string  `json:"check"`
	File           string  `json:"file"`
	Name           string  `json:"name"`
	Line           int     `json:"line"`
	Value          int     `json:"value"`
	Threshold      int     `json:"threshold"`
	FloatValue     float64 `json:"float_value,omitempty"`
	FloatThreshold float64 `json:"float_threshold,omitempty"`
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
		maxInstability     float64
		maxDistance         float64
		maxLCOM            int
		maxFields          int
		maxInterfaceWidth  int
		maxSmellsError     int
		maxRisk            float64
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
				return fmt.Errorf("loading .canopylint: %w", cfgErr)
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

			// Build xref graph once if needed by coupling, type metrics, or smell checks.
			var graph xref.Graph
			var graphErr error
			needGraph := maxInstability > 0 || maxDistance > 0 || maxLCOM > 0 || maxFields > 0 || maxInterfaceWidth > 0 || maxSmellsError > 0
			if needGraph {
				graph, graphErr = xref.Build(analysisIdx)
			}

			// Checks 5-7: Coupling metrics (instability, distance, LCOM).
			if maxInstability > 0 || maxDistance > 0 || maxLCOM > 0 {
				if graphErr == nil {
					couplingReport, couplingErr := coupling.Analyze(analysisIdx, graph)
					if couplingErr == nil {
						// Check 5: Instability.
						if maxInstability > 0 {
							checksRun++
							for _, pm := range couplingReport.Packages {
								if pm.Instability > maxInstability {
									violations = append(violations, checkViolation{
										Check:          "instability",
										File:           pm.Package,
										Name:           pm.Package,
										FloatValue:     pm.Instability,
										FloatThreshold: maxInstability,
									})
								}
							}
						}

						// Check 6: Distance from main sequence.
						if maxDistance > 0 {
							checksRun++
							for _, pm := range couplingReport.Packages {
								if pm.Distance > maxDistance {
									violations = append(violations, checkViolation{
										Check:          "distance",
										File:           pm.Package,
										Name:           pm.Package,
										FloatValue:     pm.Distance,
										FloatThreshold: maxDistance,
									})
								}
							}
						}

						// Check 7: LCOM (lack of cohesion).
						if maxLCOM > 0 {
							checksRun++
							for _, pm := range couplingReport.Packages {
								if pm.LCOM > maxLCOM {
									violations = append(violations, checkViolation{
										Check:     "lcom",
										File:      pm.Package,
										Name:      pm.Package,
										Value:     pm.LCOM,
										Threshold: maxLCOM,
									})
								}
							}
						}
					}
				}
			}

			// Checks 8-9: Type metrics (fields, interface width).
			if maxFields > 0 || maxInterfaceWidth > 0 {
				if graphErr == nil {
					typeReport, typeErr := typemetrics.Analyze(analysisIdx, analysisIdx.Root, graph)
					if typeErr == nil {
						// Check 8: Max fields per type.
						if maxFields > 0 {
							checksRun++
							for _, tm := range typeReport.Types {
								if tm.Fields > maxFields {
									violations = append(violations, checkViolation{
										Check:     "fields",
										File:      tm.File,
										Name:      tm.Name,
										Line:      tm.StartLine,
										Value:     tm.Fields,
										Threshold: maxFields,
									})
								}
							}
						}

						// Check 9: Max interface width.
						if maxInterfaceWidth > 0 {
							checksRun++
							for _, tm := range typeReport.Types {
								if tm.InterfaceWidth > maxInterfaceWidth {
									violations = append(violations, checkViolation{
										Check:     "interface-width",
										File:      tm.File,
										Name:      tm.Name,
										Line:      tm.StartLine,
										Value:     tm.InterfaceWidth,
										Threshold: maxInterfaceWidth,
									})
								}
							}
						}
					}
				}
			}

			// Check 10: Structural smells (error severity count).
			if maxSmellsError > 0 && graphErr == nil {
				checksRun++
				var compReport *complexity.Report
				var couplingReport *coupling.Report
				var typeReport *typemetrics.Report

				if cr, cerr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{}); cerr == nil {
					complexity.EnrichWithXref(cr, graph)
					compReport = cr
				}
				if coupl, cErr := coupling.Analyze(analysisIdx, graph); cErr == nil {
					couplingReport = coupl
				}
				if tr, tErr := typemetrics.Analyze(analysisIdx, analysisIdx.Root, graph); tErr == nil {
					typeReport = tr
				}

				input := smells.Input{
					Index:      analysisIdx,
					XrefGraph:  graph,
					Complexity: compReport,
					Coupling:   couplingReport,
					Types:      typeReport,
				}
				smellReport := smells.Detect(input)
				errorCount := smellReport.Summary.BySeverity["error"]
				if errorCount > maxSmellsError {
					violations = append(violations, checkViolation{
						Check:     "smells-error",
						Name:      fmt.Sprintf("%d error-severity smells detected", errorCount),
						Value:     errorCount,
						Threshold: maxSmellsError,
					})
				}
			}

			// Check 11: Risk score threshold.
			if maxRisk > 0 {
				checksRun++
				// Build complexity report if not already done.
				var riskCompReport *complexity.Report
				if maxCyclomatic > 0 || maxCognitive > 0 || maxLines > 0 {
					// Reuse the complexity report from checks 1-3 if available.
					if cr, cerr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{}); cerr == nil {
						riskCompReport = cr
					}
				} else {
					if cr, cerr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{}); cerr == nil {
						riskCompReport = cr
					}
				}

				if riskCompReport != nil {
					// Ensure xref graph is available.
					if !needGraph {
						graph, graphErr = xref.Build(analysisIdx)
					}
					if graphErr == nil {
						complexity.EnrichWithXref(riskCompReport, graph)
						testMapLookup := buildTestMapLookup(analysisIdx)
						riskReport, riskErr := risk.Analyze(risk.Input{
							Index:      analysisIdx,
							Root:       target,
							Complexity: riskCompReport,
							XrefGraph:  graph,
							TestMap:    testMapLookup,
							Since:      "90d",
						})
						if riskErr == nil {
							for _, fn := range riskReport.Functions {
								if fn.Risk > maxRisk {
									violations = append(violations, checkViolation{
										Check:          "risk",
										File:           fn.File,
										Name:           fn.Name,
										Line:           fn.StartLine,
										FloatValue:     fn.Risk,
										FloatThreshold: maxRisk,
									})
								}
							}
						}
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
					var msg string
					if v.FloatThreshold > 0 {
						msg = fmt.Sprintf("%s %s value=%.2f (max=%.2f)", v.File, v.Name, v.FloatValue, v.FloatThreshold)
					} else {
						msg = fmt.Sprintf("%s %s value=%d (max=%d)", v.File, v.Name, v.Value, v.Threshold)
					}
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
						if v.FloatThreshold > 0 {
							if v.File != "" && v.Line > 0 {
								fmt.Printf("  %s: %s:%d %s value=%.2f (max=%.2f)\n", v.Check, v.File, v.Line, v.Name, v.FloatValue, v.FloatThreshold)
							} else if v.File != "" {
								fmt.Printf("  %s: %s %s value=%.2f (max=%.2f)\n", v.Check, v.File, v.Name, v.FloatValue, v.FloatThreshold)
							} else {
								fmt.Printf("  %s: %s value=%.2f (max=%.2f)\n", v.Check, v.Name, v.FloatValue, v.FloatThreshold)
							}
						} else if v.File != "" && v.Line > 0 {
							fmt.Printf("  %s: %s:%d %s value=%d (max=%d)\n", v.Check, v.File, v.Line, v.Name, v.Value, v.Threshold)
						} else if v.File != "" {
							fmt.Printf("  %s: %s %s value=%d (max=%d)\n", v.Check, v.File, v.Name, v.Value, v.Threshold)
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
	cmd.Flags().Float64Var(&maxInstability, "max-instability", 0, "max package instability 0.0-1.0 (0 to disable)")
	cmd.Flags().Float64Var(&maxDistance, "max-distance", 0, "max distance from main sequence 0.0-1.0 (0 to disable)")
	cmd.Flags().IntVar(&maxLCOM, "max-lcom", 0, "max LCOM-4 per package (0 to disable)")
	cmd.Flags().IntVar(&maxFields, "max-fields", 0, "max fields per type (0 to disable)")
	cmd.Flags().IntVar(&maxInterfaceWidth, "max-interface-width", 0, "max interface width (0 to disable)")
	cmd.Flags().IntVar(&maxSmellsError, "max-smells-error", 0, "max error-severity structural smells (0 to disable)")
	cmd.Flags().Float64Var(&maxRisk, "max-risk", 0, "max composite risk score per function 0.0-1.0 (0 to disable)")
	return cmd
}
