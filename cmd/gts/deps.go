package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/deps"
)

func newDepsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var by string
	var top int
	var focus string
	var depth int
	var reverse bool
	var includeEdges bool
	var jsonOutput bool
	var countOnly bool
	var dotOutput bool
	var cyclesOnly bool

	cmd := &cobra.Command{
		Use:     "deps [path]",
		Aliases: []string{"gtsdeps"},
		Short:   "Analyze dependency graph from structural imports",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if top <= 0 {
				return fmt.Errorf("top must be > 0")
			}
			if depth <= 0 {
				return fmt.Errorf("depth must be > 0")
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			report, err := deps.Build(idx, deps.Options{
				Mode:         by,
				Top:          top,
				Focus:        focus,
				Depth:        depth,
				Reverse:      reverse,
				IncludeEdges: includeEdges || jsonOutput || dotOutput || cyclesOnly,
			})
			if err != nil {
				return err
			}

			genMap := generatedFileMap(idx)

			// Detect cycles when requested or for JSON output.
			if cyclesOnly || jsonOutput {
				graph := deps.GraphFromEdges(report.Edges)
				cycles := deps.DetectCycles(graph)
				if cycles == nil {
					cycles = []deps.Cycle{}
				}
				report.Cycles = cycles
			}

			if cyclesOnly && !jsonOutput {
				fmt.Printf("cycles: %d found\n", len(report.Cycles))
				for _, c := range report.Cycles {
					fmt.Printf("  %s\n", strings.Join(c.Path, " -> "))
				}
				return nil
			}

			if dotOutput {
				fmt.Println("digraph deps {")
				for _, edge := range report.Edges {
					fmt.Printf("  %q -> %q;\n", edge.From, edge.To)
				}
				fmt.Println("}")
				return nil
			}

			if countOnly {
				fmt.Println(report.EdgeCount)
				return nil
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf(
				"deps: mode=%s nodes=%d edges=%d internal=%d external=%d module=%s\n",
				report.Mode,
				report.NodeCount,
				report.EdgeCount,
				report.InternalEdgeCount,
				report.ExternalEdgeCount,
				report.Module,
			)

			if len(report.TopOutgoing) > 0 {
				fmt.Printf("top outgoing (limit=%d):\n", top)
				for _, item := range report.TopOutgoing {
					genTag := ""
					if genMap[item.Node] != nil {
						genTag = fmt.Sprintf(" [gen:%s]", genMap[item.Node].Generator)
					}
					fmt.Printf("  %s out=%d in=%d project=%t%s\n", item.Node, item.Outgoing, item.Incoming, item.IsProject, genTag)
				}
			}

			if len(report.TopIncoming) > 0 {
				fmt.Printf("top incoming (limit=%d):\n", top)
				for _, item := range report.TopIncoming {
					genTag := ""
					if genMap[item.Node] != nil {
						genTag = fmt.Sprintf(" [gen:%s]", genMap[item.Node].Generator)
					}
					fmt.Printf("  %s in=%d out=%d project=%t%s\n", item.Node, item.Incoming, item.Outgoing, item.IsProject, genTag)
				}
			}

			if report.Focus != "" {
				fmt.Printf("focus: %s direction=%s depth=%d\n", report.Focus, report.FocusDirection, report.FocusDepth)
				if len(report.FocusOutgoing) > 0 {
					fmt.Printf("  outgoing: %s\n", strings.Join(report.FocusOutgoing, ", "))
				}
				if len(report.FocusIncoming) > 0 {
					fmt.Printf("  incoming: %s\n", strings.Join(report.FocusIncoming, ", "))
				}
				if len(report.FocusWalk) > 0 {
					fmt.Printf("  walk: %s\n", strings.Join(report.FocusWalk, ", "))
				}
			}

			if includeEdges {
				fmt.Println("edges:")
				for _, edge := range report.Edges {
					label := "external"
					if edge.Internal {
						label = "internal"
					}
					fmt.Printf("  %s -> %s (%s)\n", edge.From, edge.To, label)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&by, "by", "package", "graph mode: package or file")
	cmd.Flags().IntVar(&top, "top", 10, "number of top nodes to show")
	cmd.Flags().StringVar(&focus, "focus", "", "focus node to inspect incoming/outgoing edges")
	cmd.Flags().IntVar(&depth, "depth", 1, "transitive depth for focus traversal")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "walk reverse dependencies from focus")
	cmd.Flags().BoolVar(&includeEdges, "edges", false, "include full edge list in output")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of dependency edges")
	cmd.Flags().BoolVar(&dotOutput, "dot", false, "emit DOT graph for Graphviz visualization")
	cmd.Flags().BoolVar(&cyclesOnly, "cycles", false, "only show import cycles")
	return cmd
}

func runDeps(args []string) error {
	cmd := newDepsCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
