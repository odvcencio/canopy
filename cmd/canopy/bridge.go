package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/internal/bridge"
)

func newBridgeCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var top int
	var focus string
	var depth int
	var reverse bool
	var jsonOutput bool
	var countOnly bool
	var dotOutput bool

	cmd := &cobra.Command{
		Use:     "bridge [path]",
		Aliases: []string{"gtsbridge"},
		Short:   "Map cross-component dependency bridges",
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

			report, err := bridge.Build(idx, bridge.Options{
				Top:     top,
				Focus:   focus,
				Depth:   depth,
				Reverse: reverse,
			})
			if err != nil {
				return err
			}

			if dotOutput {
				fmt.Println("digraph bridge {")
				for _, edge := range report.TopBridges {
					fmt.Printf("  %q -> %q;\n", edge.From, edge.To)
				}
				fmt.Println("}")
				return nil
			}

			if countOnly {
				fmt.Println(report.BridgeCount)
				return nil
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf(
				"bridge: components=%d packages=%d bridges=%d root=%s module=%s\n",
				report.ComponentCount,
				report.PackageCount,
				report.BridgeCount,
				report.Root,
				report.Module,
			)
			if len(report.Components) > 0 {
				fmt.Println("components:")
				for _, component := range report.Components {
					fmt.Printf(
						"  %s packages=%d files=%d imports:internal=%d external=%d\n",
						component.Name,
						component.PackageCount,
						component.FileCount,
						component.InternalImports,
						component.ExternalImports,
					)
				}
			}
			if len(report.TopBridges) > 0 {
				fmt.Printf("top bridges (limit=%d):\n", top)
				for _, edge := range report.TopBridges {
					line := fmt.Sprintf("  %s -> %s count=%d", edge.From, edge.To, edge.Count)
					if len(edge.Samples) > 0 {
						line += " samples=" + strings.Join(edge.Samples, ",")
					}
					fmt.Println(line)
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
			if len(report.ExternalByComponent) > 0 {
				fmt.Printf("external pressure (limit=%d):\n", top)
				for _, item := range report.ExternalByComponent {
					line := fmt.Sprintf("  %s count=%d", item.Component, item.Count)
					if len(item.TopImports) > 0 {
						line += " top=" + strings.Join(item.TopImports, ",")
					}
					fmt.Println(line)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().IntVar(&top, "top", 20, "number of top bridge and external rows to show")
	cmd.Flags().StringVar(&focus, "focus", "", "focus component for bridge traversal")
	cmd.Flags().IntVar(&depth, "depth", 1, "transitive traversal depth from focus")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "walk reverse bridge direction from focus")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of bridge edges")
	cmd.Flags().BoolVar(&dotOutput, "dot", false, "emit DOT graph for Graphviz visualization")
	return cmd
}

func runBridge(args []string) error {
	cmd := newBridgeCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
