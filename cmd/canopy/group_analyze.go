package main

import "github.com/spf13/cobra"

func newAnalyzeGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Quality, complexity, security, and governance analysis",
		Long: `Run structural analysis: quality gates, complexity metrics, architecture
governance, capability detection, and executive reporting.`,
	}
	cmd.AddCommand(
		newCheckCmd(),
		newComplexityCmd(),
		newHotspotCmd(),
		newLintCmd(),
		newCapaCmd(),
		newReachabilityCmd(),
		newReportCmd(),
		newReviewCmd(),
		newSimilarityCmd(),
		newDuplicationCmd(),
		newSummaryCmd(),
		newBoundariesCmd(),
		newTrendsCmd(),
	)
	return cmd
}
