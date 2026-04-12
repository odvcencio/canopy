package main

import "github.com/spf13/cobra"

func newGraphGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Call graph, dependency, and coverage analysis",
	}
	cmd.AddCommand(
		newCallgraphCmd(),
		newDeadCmd(),
		newDepsCmd(),
		newBridgeCmd(),
		newImpactCmd(),
		newTestmapCmd(),
		newUnresolvedCmd(),
		newFaninCmd(),
		newDriftCmd(),
		newServicesCmd(),
	)
	return cmd
}
