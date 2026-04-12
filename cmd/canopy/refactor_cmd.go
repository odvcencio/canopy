package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/query"
	"github.com/odvcencio/canopy/pkg/refactor"
)

func newRefactorCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var engine string
	var updateCallsites bool
	var crossPackage bool
	var writeChanges bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "refactor <selector> <new-name> [path]",
		Aliases: []string{"gtsrefactor"},
		Short:   "Apply structural declaration renames (dry-run by default)",
		Args:    cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if crossPackage && !updateCallsites {
				return errors.New("--cross-package requires --callsites")
			}

			selector, err := query.ParseSelector(args[0])
			if err != nil {
				return err
			}
			newName := args[1]

			target := "."
			if len(args) == 3 {
				target = args[2]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			report, err := refactor.RenameDeclarations(idx, selector, newName, refactor.Options{
				Write:                 writeChanges,
				UpdateCallsites:       updateCallsites,
				CrossPackageCallsites: crossPackage,
				Engine:                engine,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(report)
			}

			for _, edit := range report.Edits {
				if edit.Skipped {
					fmt.Printf(
						"%s:%d:%d %s %s %s -> %s skipped=%s\n",
						edit.File,
						edit.Line,
						edit.Column,
						edit.Category,
						edit.Kind,
						edit.OldName,
						edit.NewName,
						edit.SkipNote,
					)
					continue
				}
				status := "planned"
				if edit.Applied {
					status = "applied"
				}
				fmt.Printf("%s:%d:%d %s %s %s -> %s %s\n", edit.File, edit.Line, edit.Column, edit.Category, edit.Kind, edit.OldName, edit.NewName, status)
			}
			fmt.Printf(
				"refactor: selector=%q new=%q engine=%q callsites=%t cross-package=%t matches=%d planned=%d (decl=%d callsites=%d) applied=%d files=%d\n",
				report.Selector,
				report.NewName,
				report.Engine,
				report.UpdateCallsites,
				report.CrossPackageCallsites,
				report.MatchCount,
				report.PlannedEdits,
				report.PlannedDeclEdits,
				report.PlannedUseEdits,
				report.AppliedEdits,
				report.ChangedFiles,
			)
			if !report.Write {
				fmt.Println("refactor: dry-run (add --write to apply edits)")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&engine, "engine", "go", "refactor engine: go|treesitter")
	cmd.Flags().BoolVar(&updateCallsites, "callsites", false, "update resolved same-package callsites")
	cmd.Flags().BoolVar(&crossPackage, "cross-package", false, "update resolved cross-package callsites within the module")
	cmd.Flags().BoolVar(&writeChanges, "write", false, "apply edits in-place (default is dry-run)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}

func runRefactor(args []string) error {
	cmd := newRefactorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
