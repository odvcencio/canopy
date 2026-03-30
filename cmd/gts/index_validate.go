package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/index"
)

type validateReport struct {
	Total       int      `json:"total"`
	OK          int      `json:"ok"`
	Stale       int      `json:"stale"`
	Missing     int      `json:"missing"`
	ParseErrors int      `json:"parse_errors"`
	StaleFiles  []string `json:"stale_files,omitempty"`
	MissingFiles []string `json:"missing_files,omitempty"`
}

func newValidateCmd() *cobra.Command {
	var cachePath string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Check index integrity and detect stale or missing files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cachePath == "" {
				return fmt.Errorf("--cache is required: provide path to a cached index")
			}

			idx, err := index.Load(cachePath)
			if err != nil {
				return fmt.Errorf("loading cached index: %w", err)
			}

			root := idx.Root
			if len(args) == 1 {
				root = args[0]
			}

			report := validateReport{
				Total:       len(idx.Files),
				ParseErrors: len(idx.Errors),
			}

			for _, f := range idx.Files {
				absPath := f.Path
				if !filepath.IsAbs(absPath) {
					absPath = filepath.Join(root, absPath)
				}

				info, err := os.Stat(absPath)
				if err != nil {
					report.Missing++
					report.MissingFiles = append(report.MissingFiles, f.Path)
					continue
				}

				if info.ModTime().After(idx.GeneratedAt) {
					report.Stale++
					report.StaleFiles = append(report.StaleFiles, f.Path)
					continue
				}

				report.OK++
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf("validate: total=%d ok=%d stale=%d missing=%d parse_errors=%d\n",
				report.Total, report.OK, report.Stale, report.Missing, report.ParseErrors)

			if len(report.MissingFiles) > 0 {
				fmt.Println("missing:")
				for _, p := range report.MissingFiles {
					fmt.Printf("  %s\n", p)
				}
			}
			if len(report.StaleFiles) > 0 {
				fmt.Println("stale:")
				for _, p := range report.StaleFiles {
					fmt.Printf("  %s\n", p)
				}
			}

			if report.Stale > 0 || report.Missing > 0 {
				os.Exit(2)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "path to cached index (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}
