package main

import "github.com/spf13/cobra"

func newIndexGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build, inspect, and compare structural indexes",
	}
	cmd.AddCommand(
		newIndexBuildCmd(),
		newMapCmd(),
		newFilesCmd(),
		newStatsCmd(),
		newDiffCmd(),
		newErrorsCmd(),
		newValidateCmd(),
		newExportCmd(),
		newImportCmd(),
	)
	return cmd
}
