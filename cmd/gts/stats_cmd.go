package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/stats"
)

func newStatsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var top int
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "stats [path]",
		Aliases: []string{"gtsstats"},
		Short:   "Report structural codebase metrics from an index",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if top <= 0 {
				return fmt.Errorf("top must be > 0")
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			if gen, _ := cmd.Flags().GetString("generator"); gen != "" {
				idx = idx.FilterByGenerator(gen)
			}

			report, err := stats.Build(idx, stats.Options{
				TopFiles: top,
			})
			if err != nil {
				return err
			}

			if countOnly {
				fmt.Println(report.FileCount)
				return nil
			}

			if jsonOutput {
				genCount := idx.GeneratedFileCount()
				type jsonReport struct {
					stats.Report
					GeneratedFileCount int `json:"generated_file_count,omitempty"`
				}
				jr := jsonReport{Report: report}
				if genCount > 0 {
					jr.GeneratedFileCount = genCount
				}
				return emitJSON(jr)
			}

			genCount := idx.GeneratedFileCount()
			if genCount > 0 {
				fmt.Printf(
					"stats: files=%d (%d generated) symbols=%d errors=%d root=%s\n",
					report.FileCount,
					genCount,
					report.SymbolCount,
					report.ParseErrorCount,
					report.Root,
				)
			} else {
				fmt.Printf(
					"stats: files=%d symbols=%d errors=%d root=%s\n",
					report.FileCount,
					report.SymbolCount,
					report.ParseErrorCount,
					report.Root,
				)
			}
			if len(report.Languages) > 0 {
				fmt.Println("languages:")
				for _, language := range report.Languages {
					fmt.Printf("  %s files=%d symbols=%d\n", language.Language, language.Files, language.Symbols)
				}
			}
			if len(report.Generators) > 0 {
				fmt.Println("generators:")
				for _, gen := range report.Generators {
					fmt.Printf("  %s files=%d symbols=%d\n", gen.Generator, gen.Files, gen.Symbols)
				}
			}
			if len(report.KindCounts) > 0 {
				fmt.Println("kinds:")
				for _, kind := range report.KindCounts {
					fmt.Printf("  %s count=%d\n", kind.Kind, kind.Count)
				}
			}
			if len(report.TopFiles) > 0 {
				fmt.Printf("top files (limit=%d):\n", top)
				for _, file := range report.TopFiles {
					fmt.Printf(
						"  %s symbols=%d imports=%d language=%s size=%d\n",
						file.Path,
						file.Symbols,
						file.Imports,
						file.Language,
						file.SizeBytes,
					)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().IntVar(&top, "top", 10, "number of top files by symbol count")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the total file count")
	return cmd
}

func runStats(args []string) error {
	cmd := newStatsCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
