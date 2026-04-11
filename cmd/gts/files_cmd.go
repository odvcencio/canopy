package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/files"
)

func newFilesCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var language string
	var minSymbols int
	var sortBy string
	var top int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "files [path]",
		Aliases: []string{"gtsfiles"},
		Short:   "List/index files with structural density filters",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if minSymbols < 0 {
				return fmt.Errorf("min-symbols must be >= 0")
			}
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

			report, err := files.Build(idx, files.Options{
				Language:   language,
				MinSymbols: minSymbols,
				SortBy:     sortBy,
				Top:        top,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(report)
			}

			genMap := generatedFileMap(idx)

			fmt.Printf("files: total=%d shown=%d root=%s\n", report.TotalFiles, report.ShownFiles, report.Root)
			for _, entry := range report.Entries {
				genTag := ""
				if gi := genMap[entry.Path]; gi != nil {
					genTag = fmt.Sprintf(" [gen:%s]", gi.Generator)
				}
				fmt.Printf(
					"%s language=%s symbols=%d imports=%d size=%d%s\n",
					entry.Path,
					entry.Language,
					entry.Symbols,
					entry.Imports,
					entry.SizeBytes,
					genTag,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&language, "language", "", "filter by language (e.g. go)")
	cmd.Flags().IntVar(&minSymbols, "min-symbols", 0, "minimum symbols per file")
	cmd.Flags().StringVar(&sortBy, "sort", "symbols", "sort by symbols|imports|size|path")
	cmd.Flags().IntVar(&top, "top", 50, "maximum files to show")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}

func runFiles(args []string) error {
	cmd := newFilesCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
