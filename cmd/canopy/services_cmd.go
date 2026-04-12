package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/internal/federation"
)

func newServicesCmd() *cobra.Command {
	var fedDir string
	var dotOutput bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "services",
		Short: "Build repo-to-repo dependency graph from federated indexes",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := strings.TrimSpace(fedDir)
			if dir == "" {
				// Fall back to the persistent --federation flag.
				dir, _ = cmd.Flags().GetString("federation")
				dir = strings.TrimSpace(dir)
			}
			if dir == "" {
				return fmt.Errorf("--federation <dir> is required (pass it as a global flag or use --dir)")
			}

			fi, err := federation.Load(dir)
			if err != nil {
				return err
			}

			report := federation.BuildServiceGraph(fi)

			if dotOutput {
				fmt.Println("digraph services {")
				// Deduplicate edges to repo-level for dot output.
				seen := map[string]bool{}
				for _, e := range report.Edges {
					key := e.From + "->" + e.To
					if seen[key] {
						continue
					}
					seen[key] = true
					fmt.Printf("  %q -> %q;\n", e.From, e.To)
				}
				fmt.Println("}")
				return nil
			}

			if jsonOutput {
				return emitJSON(report)
			}

			// Deduplicate to repo-level edges for summary.
			type repoEdge struct{ from, to string }
			dedupEdges := map[repoEdge]int{}
			for _, e := range report.Edges {
				dedupEdges[repoEdge{e.From, e.To}]++
			}

			fmt.Printf("services: repos=%d edges=%d\n", len(report.Repos), len(dedupEdges))
			for _, r := range report.Repos {
				fmt.Printf("  %s\n", r)
			}
			if len(dedupEdges) > 0 {
				fmt.Println("dependencies:")
				for edge, count := range dedupEdges {
					fmt.Printf("  %s -> %s (%d imports)\n", edge.from, edge.to, count)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&fedDir, "dir", "", "directory containing .canopyindex files (overrides --federation)")
	cmd.Flags().BoolVar(&dotOutput, "dot", false, "emit DOT graph for Graphviz visualization")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}
