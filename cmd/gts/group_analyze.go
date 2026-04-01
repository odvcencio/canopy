package main

import "github.com/spf13/cobra"

func newAnalyzeGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Quality, complexity, and security analysis",
	}
	cmd.AddCommand(
		newCheckCmd(),
		newComplexityCmd(),
		newHotspotCmd(),
		newLintCmd(),
		newCapaCmd(),
		newReachabilityCmd(),
		newSimilarityCmd(),
		newDuplicationCmd(),
		newSummaryCmd(),
		newBoundariesCmd(),
	)
	return cmd
}
