package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/lint"
	"github.com/odvcencio/gts-suite/pkg/sarif"
)

func newLintCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var failOnViolations bool
	var jsonOutput bool
	var format string
	var rawRules []string
	var rawPatterns []string
	var noDefaults bool
	var thresholdOverrides []string

	cmd := &cobra.Command{
		Use:     "lint [path]",
		Aliases: []string{"gtslint"},
		Short:   "Run structural lint rules against indexed symbols",
		Long: `Run structural lint rules against indexed symbols.

When no --rule or --pattern flags are given, built-in threshold rules are used
automatically. These check cyclomatic complexity, cognitive complexity, function
length, nesting depth, parameter count, fan-in, and fan-out against sensible
defaults.

Use --no-defaults to disable built-in rules. Use --threshold to override
individual thresholds (e.g. --threshold cyclomatic=35).

Built-in rules compose with explicit --rule and --pattern flags: all fire together.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			rules := make([]lint.Rule, 0, len(rawRules))
			for _, rawRule := range rawRules {
				rule, err := lint.ParseRule(rawRule)
				if err != nil {
					return fmt.Errorf("parse rule %q: %w", rawRule, err)
				}
				rules = append(rules, rule)
			}
			patterns := make([]lint.QueryPattern, 0, len(rawPatterns))
			for _, rawPattern := range rawPatterns {
				pattern, err := lint.LoadQueryPattern(rawPattern)
				if err != nil {
					return fmt.Errorf("load pattern %q: %w", rawPattern, err)
				}
				patterns = append(patterns, pattern)
			}

			// Determine whether to use built-in threshold rules.
			useDefaults := !noDefaults
			var thresholdRules []lint.ThresholdRule
			if useDefaults {
				// Copy DefaultRules so overrides don't mutate the package-level slice.
				thresholdRules = make([]lint.ThresholdRule, len(lint.DefaultRules))
				copy(thresholdRules, lint.DefaultRules)
				for _, override := range thresholdOverrides {
					if err := lint.ParseThresholdOverride(override, thresholdRules); err != nil {
						return err
					}
				}
			}

			lintCfg, cfgErr := lint.LoadConfig(target)
			if cfgErr != nil {
				return fmt.Errorf("loading .gtslint: %w", cfgErr)
			}
			if lintCfg != nil && useDefaults {
				for _, override := range lintCfg.Overrides {
					if override.Scope != "" {
						continue
					}
					for i := range thresholdRules {
						if thresholdRules[i].Metric == override.Metric {
							thresholdRules[i].Threshold = override.Threshold
							thresholdRules[i].Severity = override.Severity
							if override.Message != "" {
								thresholdRules[i].Message = override.Message
							}
							break
						}
					}
				}
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			idx = applyGeneratedFilter(cmd, idx)

			violations := lint.Evaluate(idx, rules)

			// When defaults are enabled, include built-in secrets detection patterns.
			if useDefaults {
				patterns = append(patterns, lint.SecretsPatterns()...)
			}

			patternViolations, err := lint.EvaluatePatterns(idx, patterns)
			if err != nil {
				return err
			}
			violations = append(violations, patternViolations...)

			if len(thresholdRules) > 0 {
				thresholdViolations, err := lint.EvaluateThresholds(idx, thresholdRules)
				if err != nil {
					return err
				}
				violations = append(violations, thresholdViolations...)
			}

			if lintCfg != nil {
				var filtered []lint.Violation
				for _, v := range violations {
					if !lintCfg.ShouldIgnore(v.File, v.Name, v.RuleID) {
						filtered = append(filtered, v)
					}
				}
				violations = filtered
			}

			sort.Slice(violations, func(i, j int) bool {
				if violations[i].File == violations[j].File {
					if violations[i].StartLine == violations[j].StartLine {
						if violations[i].RuleID == violations[j].RuleID {
							return violations[i].Name < violations[j].Name
						}
						return violations[i].RuleID < violations[j].RuleID
					}
					return violations[i].StartLine < violations[j].StartLine
				}
				return violations[i].File < violations[j].File
			})

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
					if !seen[v.RuleID] {
						log.AddRule(v.RuleID, v.RuleID)
						seen[v.RuleID] = true
					}
					level := sarif.MapSeverity(v.Severity)
					log.AddResult(v.RuleID, level, v.Message, v.File, v.StartLine, v.EndLine)
				}
				if err := log.Encode(os.Stdout); err != nil {
					return err
				}
			case "json":
				return emitJSON(struct {
					Rules          []lint.Rule         `json:"rules,omitempty"`
					Patterns       []lint.QueryPattern `json:"patterns,omitempty"`
					ThresholdRules []lint.ThresholdRule `json:"threshold_rules,omitempty"`
					Violations     []lint.Violation     `json:"violations,omitempty"`
					Count          int                  `json:"count"`
				}{
					Rules:          rules,
					Patterns:       patterns,
					ThresholdRules: thresholdRules,
					Violations:     violations,
					Count:          len(violations),
				})
			default:
				for _, violation := range violations {
					severity := violation.Severity
					if severity == "" {
						severity = "warn"
					}
					if violation.StartLine <= 0 {
						fmt.Printf(
							"[%s] %s %s %s rule=%s %s\n",
							severity,
							violation.File,
							violation.Kind,
							violation.Name,
							violation.RuleID,
							violation.Message,
						)
						continue
					}
					fmt.Printf(
						"[%s] %s:%d:%d %s %s rule=%s %s\n",
						severity,
						violation.File,
						violation.StartLine,
						violation.EndLine,
						violation.Kind,
						violation.Name,
						violation.RuleID,
						violation.Message,
					)
				}

				thresholdCount := len(thresholdRules)
				fmt.Printf("lint: rules=%d patterns=%d thresholds=%d violations=%d\n", len(rules), len(patterns), thresholdCount, len(violations))
				if len(idx.Errors) > 0 {
					fmt.Printf("lint: parse errors=%d (ignored)\n", len(idx.Errors))
				}
			}

			if len(violations) > 0 && failOnViolations {
				return exitCodeError{
					code: 3,
					err:  fmt.Errorf("%d lint violations", len(violations)),
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&failOnViolations, "fail-on-violations", true, "exit non-zero when violations are found")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, json, sarif")
	cmd.Flags().StringArrayVar(&rawRules, "rule", nil, "lint rule expression (repeatable)")
	cmd.Flags().StringArrayVar(&rawPatterns, "pattern", nil, "tree-sitter query pattern file (.scm) (repeatable)")
	cmd.Flags().BoolVar(&noDefaults, "no-defaults", false, "disable built-in threshold rules")
	cmd.Flags().StringArrayVar(&thresholdOverrides, "threshold", nil, "override a built-in threshold (e.g. cyclomatic=35) (repeatable)")
	return cmd
}

func runLint(args []string) error {
	cmd := newLintCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
