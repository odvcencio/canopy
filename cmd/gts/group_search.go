package main

import "github.com/spf13/cobra"

func newSearchGroup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Find symbols, references, and patterns in code",
	}
	cmd.AddCommand(
		newGrepCmd(),
		newRefsCmd(),
		newQueryCmd(),
		newScopeCmd(),
		newContextCmd(),
		newSymbolsCmd(),
		newImportsCmd(),
	)
	return cmd
}
