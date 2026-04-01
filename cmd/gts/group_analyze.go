package main

import "github.com/spf13/cobra"

func newAnalyzeGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Quality, complexity, security, and governance analysis",
		Long: `Run structural analysis: quality gates, complexity metrics, architecture
governance, security intelligence, license detection, and executive reporting.`,
	}
	cmd.AddCommand(
		newCheckCmd(),
		newComplexityCmd(),
		newHotspotCmd(),
		newLicensesCmd(),
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
