package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	var root string
	var cachePath string
	var allowWrites bool

	cmd := &cobra.Command{
		Use:     "mcp",
		Aliases: []string{"gtsmcp"},
		Short:   "Run MCP stdio server for AI-agent tool integration",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service := mcp.NewServiceWithOptions(root, cachePath, mcp.ServiceOptions{
				AllowWrites: allowWrites,
			})
			return mcp.RunStdio(service, os.Stdin, os.Stdout, os.Stderr)
		},
	}

	cmd.Flags().StringVar(&root, "root", ".", "default root path for tool calls")
	cmd.Flags().StringVar(&cachePath, "cache", "", "default cache path for tool calls")
	cmd.Flags().BoolVar(&allowWrites, "allow-writes", false, "allow MCP tools to mutate files (e.g. gts_refactor write mode)")
	return cmd
}

func runMCP(args []string) error {
	cmd := newMCPCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
