package main

import (
	"github.com/spf13/cobra"
)

type exitCodeError struct {
	code int
	err  error
}

func (e exitCodeError) Error() string {
	if e.err == nil {
		return "command failed"
	}
	return e.err.Error()
}

func (e exitCodeError) ExitCode() int {
	if e.code <= 0 {
		return 1
	}
	return e.code
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gts",
		Short: "Structural code analysis toolkit",
		Long: `gts — structural code analysis toolkit powered by tree-sitter.

AST-based indexing, search, call graph analysis, architecture governance,
security intelligence, and AI agent integration across 206+ languages.

Command groups:
  index      Build and manage structural indexes
  search     Find symbols, references, and patterns
  graph      Call graph, dependency, and coverage analysis
  analyze    Quality, complexity, security, and governance
  transform  Code transformations and output generation
  mcp        MCP stdio server for AI agents (30+ tools)
  init       Project setup and CI workflow generation

Get started:
  gts index build .              Build a structural index
  gts analyze check              Run CI quality gate
  gts analyze report             Executive summary of all analyses
  gts mcp --root .               Start MCP server for AI agents`,
		Version: version,
	}
	root.PersistentFlags().Bool("include-generated", false, "include generated files in analysis output")
	root.PersistentFlags().String("generator", "", "filter to a specific generator name (e.g. protobuf, mockgen, human)")
	root.PersistentFlags().String("federation", "", "directory containing .gtsindex files for multi-repo federated analysis")
	root.PersistentFlags().StringSliceP("exclude", "X", nil, "gitignore-style path pattern to exclude from indexing (repeatable; merged with workspace .graftignore/.gtsignore). Bypasses index cache when set.")

	root.AddCommand(
		newIndexGroup(),
		newSearchGroup(),
		newGraphGroup(),
		newAnalyzeGroup(),
		newTransformGroup(),
		newMCPCmd(),
		newInitCmd(),
	)
	return root
}
