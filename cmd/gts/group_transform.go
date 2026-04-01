package main

import "github.com/spf13/cobra"

func newTransformGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transform",
		Short: "Code transformations and output generation",
	}
	cmd.AddCommand(
		newRefactorCmd(),
		newChunkCmd(),
		newYaraCmd(),
		newNormalizeCmd(),
		newSBOMCmd(),
	)
	return cmd
}
