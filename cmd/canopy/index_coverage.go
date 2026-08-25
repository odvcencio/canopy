package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"m31labs.dev/canopy/internal/indexcoverage"
	"m31labs.dev/canopy/pkg/model"
)

func newIndexCoverageCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var includeAll bool
	var strict bool
	var limit int

	cmd := &cobra.Command{
		Use:   "coverage [path]",
		Short: "Report parser coverage gaps and missing parse receipts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return fmt.Errorf("limit must be > 0")
			}
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			if generator, _ := cmd.Flags().GetString("generator"); generator != "" {
				idx = idx.FilterByGenerator(generator)
			}
			includeGenerated, _ := cmd.Flags().GetBool("include-generated")
			report, err := indexcoverage.Build(idx, indexcoverage.Options{
				IncludeClean:     includeAll,
				IncludeGenerated: includeAll || includeGenerated,
				MaxFiles:         limit,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				if err := emitJSON(report); err != nil {
					return err
				}
			} else {
				printCoverageReport(report)
			}
			if strict && report.StrictFailure() {
				return exitCodeError{code: 2, err: errors.New("index contains incomplete or unknown parser coverage")}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&includeAll, "all", false, "include clean and generated-file receipts")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit with code 2 for partial, stopped, unknown, or failed parses")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum number of file receipts to emit")
	return cmd
}

func printCoverageReport(report indexcoverage.Report) {
	summary := report.Summary
	fmt.Printf(
		"coverage: files=%d clean=%d partial=%d stopped=%d generated=%d unknown=%d parse_errors=%d gaps=%d root=%s\n",
		report.TotalFiles,
		summary.Clean,
		summary.Partial,
		summary.Stopped,
		summary.Generated,
		summary.Unknown,
		summary.ParseErrors,
		summary.Gaps,
		report.Root,
	)
	if summary.Recovered > 0 || summary.IgnoredEOF > 0 || summary.Truncated > 0 {
		fmt.Printf(
			"receipts: recovered_regions=%d ignored_eof_missing=%d truncated_files=%d\n",
			summary.Recovered,
			summary.IgnoredEOF,
			summary.Truncated,
		)
	}
	for _, file := range report.Files {
		coverage := file.Coverage
		fmt.Printf(
			"  %s status=%s language=%s gaps=%d errors=%d missing=%d",
			file.Path,
			coverage.Status,
			file.Language,
			len(coverage.Gaps),
			coverage.ErrorNodes,
			coverage.MissingNodes,
		)
		if coverage.StopReason != "" {
			fmt.Printf(" reason=%s", coverage.StopReason)
		}
		fmt.Println()
		for _, gap := range coverage.Gaps {
			printCoverageGap(gap)
		}
	}
	if report.DetailsTruncated {
		fmt.Println("  ... file receipts truncated; raise --limit to inspect more")
	}
	if len(report.Errors) > 0 {
		fmt.Println("parse errors:")
		for _, parseErr := range report.Errors {
			fmt.Printf("  %s: %s\n", parseErr.Path, parseErr.Error)
		}
	}
}

func printCoverageGap(gap model.ParseGap) {
	fmt.Printf(
		"    %s %d:%d-%d:%d bytes=%d-%d",
		gap.Kind,
		gap.StartLine,
		gap.StartColumn,
		gap.EndLine,
		gap.EndColumn,
		gap.StartByte,
		gap.EndByte,
	)
	if gap.NodeType != "" {
		fmt.Printf(" node=%s", gap.NodeType)
	}
	fmt.Println()
}

func runIndexCoverage(args []string) error {
	cmd := newIndexCoverageCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
