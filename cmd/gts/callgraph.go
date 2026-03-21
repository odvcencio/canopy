package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/xref"
)

func newCallgraphCmd() *cobra.Command {
	var cachePath string
	var regexMode bool
	var depth int
	var reverse bool
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "callgraph <name|regex> [path]",
		Aliases: []string{"gtscallgraph"},
		Short:   "Build call graph edges rooted at matching callable definitions",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if depth <= 0 {
				return fmt.Errorf("depth must be > 0")
			}

			target := "."
			if len(args) == 2 {
				target = args[1]
			}

			idx, err := loadOrBuild(cachePath, target)
			if err != nil {
				return err
			}

			graph, err := xref.Build(idx)
			if err != nil {
				return err
			}

			roots, err := graph.FindDefinitions(args[0], regexMode)
			if err != nil {
				return err
			}
			rootIDs := make([]string, 0, len(roots))
			for _, root := range roots {
				rootIDs = append(rootIDs, root.ID)
			}
			walk := graph.Walk(rootIDs, depth, reverse)

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						RootCount      int `json:"root_count"`
						NodeCount      int `json:"node_count"`
						EdgeCount      int `json:"edge_count"`
						UnresolvedCall int `json:"unresolved_call_count"`
					}{
						RootCount:      len(walk.Roots),
						NodeCount:      len(walk.Nodes),
						EdgeCount:      len(walk.Edges),
						UnresolvedCall: len(graph.Unresolved),
					})
				}
				return emitJSON(struct {
					Roots               []xref.Definition        `json:"roots,omitempty"`
					Nodes               []xref.Definition        `json:"nodes,omitempty"`
					Edges               []xref.MaterializedEdge  `json:"edges,omitempty"`
					Depth               int                      `json:"depth"`
					Reverse             bool                     `json:"reverse"`
					UnresolvedCallCount int                      `json:"unresolved_call_count"`
				}{
					Roots:               walk.Roots,
					Nodes:               walk.Nodes,
					Edges:               walk.MaterializedEdges(),
					Depth:               walk.Depth,
					Reverse:             walk.Reverse,
					UnresolvedCallCount: len(graph.Unresolved),
				})
			}

			if countOnly {
				fmt.Println(len(walk.Edges))
				return nil
			}

			fmt.Printf(
				"callgraph: roots=%d nodes=%d edges=%d depth=%d reverse=%t unresolved=%d\n",
				len(walk.Roots),
				len(walk.Nodes),
				len(walk.Edges),
				walk.Depth,
				walk.Reverse,
				len(graph.Unresolved),
			)
			for _, root := range walk.Roots {
				fmt.Printf("root: %s:%d %s %s\n", root.File, root.StartLine, root.Kind, definitionLabel(root))
			}
			for _, edge := range walk.Edges {
				caller := graph.EdgeCaller(edge)
				callee := graph.EdgeCallee(edge)
				fmt.Printf(
					"%s:%d %s -> %s:%d %s count=%d resolution=%s\n",
					caller.File,
					caller.StartLine,
					definitionLabel(*caller),
					callee.File,
					callee.StartLine,
					definitionLabel(*callee),
					edge.Count,
					edge.Resolution,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&regexMode, "regex", false, "treat the first argument as a regular expression")
	cmd.Flags().IntVar(&depth, "depth", 2, "call graph traversal depth")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "walk incoming callers instead of outgoing callees")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of traversed edges")
	return cmd
}

func runCallgraph(args []string) error {
	cmd := newCallgraphCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
