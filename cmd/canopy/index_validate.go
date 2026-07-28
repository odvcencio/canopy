package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
)

type validateReport struct {
	Total        int      `json:"total"`
	OK           int      `json:"ok"`
	Stale        int      `json:"stale"`
	Missing      int      `json:"missing"`
	New          int      `json:"new"`
	ParseErrors  int      `json:"parse_errors"`
	RootMismatch bool     `json:"root_mismatch,omitempty"`
	StaleFiles   []string `json:"stale_files,omitempty"`
	MissingFiles []string `json:"missing_files,omitempty"`
	NewFiles     []string `json:"new_files,omitempty"`
}

func newValidateCmd() *cobra.Command {
	var cachePath string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Check index integrity and detect stale, missing, or new files",
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

			report, err := buildValidateReport(cmd.Context(), root, idx)
			if err != nil {
				return err
			}

			if jsonOutput {
				if err := emitJSON(report); err != nil {
					return err
				}
			} else {
				fmt.Printf("validate: total=%d ok=%d stale=%d missing=%d new=%d parse_errors=%d root_mismatch=%t\n",
					report.Total, report.OK, report.Stale, report.Missing, report.New, report.ParseErrors, report.RootMismatch)
				printValidateFiles("missing", report.MissingFiles)
				printValidateFiles("stale", report.StaleFiles)
				printValidateFiles("new", report.NewFiles)
			}

			if report.RootMismatch || report.Stale > 0 || report.Missing > 0 || report.New > 0 {
				return exitCodeError{
					code: 2,
					err:  errors.New("index source set is stale"),
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "path to cached index (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}

func buildValidateReport(ctx context.Context, root string, idx *model.Index) (validateReport, error) {
	builder, err := index.NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		return validateReport{}, err
	}
	freshness, err := builder.CheckFreshness(ctx, root, idx)
	if err != nil {
		return validateReport{}, err
	}

	report := validateReport{
		Total:        len(idx.Files),
		OK:           len(idx.Files),
		Stale:        len(freshness.StaleFiles),
		Missing:      len(freshness.MissingFiles),
		New:          len(freshness.NewFiles),
		ParseErrors:  len(idx.Errors),
		RootMismatch: freshness.RootMismatch,
		StaleFiles:   freshness.StaleFiles,
		MissingFiles: freshness.MissingFiles,
		NewFiles:     freshness.NewFiles,
	}
	if freshness.RootMismatch {
		report.OK = 0
		return report, nil
	}
	indexedPaths := make(map[string]struct{}, len(idx.Files))
	for _, file := range idx.Files {
		indexedPaths[file.Path] = struct{}{}
	}
	for _, path := range append(freshness.StaleFiles, freshness.MissingFiles...) {
		if _, ok := indexedPaths[path]; ok {
			report.OK--
		}
	}
	return report, nil
}

func printValidateFiles(label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Printf("%s:\n", label)
	for _, path := range paths {
		fmt.Printf("  %s\n", path)
	}
}
