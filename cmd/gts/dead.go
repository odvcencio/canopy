package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

func newDeadCmd() *cobra.Command {
	var cachePath string
	var kind string
	var includeEntrypoints bool
	var includeTests bool
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "dead [path...]",
		Aliases: []string{"gtsdead"},
		Short:   "List callable definitions with zero incoming call references",
		Long: `List callable definitions with zero incoming call references.

Multiple paths can be provided to build the cross-reference graph across
packages, reducing false positives for exported symbols called from other
packages.

Examples:
  gts dead internal/service/
  gts dead internal/service/ internal/api/    # cross-package analysis`,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(kind))
			switch mode {
			case "callable", "function", "method":
			default:
				return fmt.Errorf("unsupported --kind %q (expected callable|function|method)", kind)
			}

			targets := args
			if len(targets) == 0 {
				targets = []string{"."}
			}

			var idx *model.Index
			for i, target := range targets {
				built, err := loadOrBuild(cachePath, target)
				if err != nil {
					return err
				}
				if i == 0 {
					idx = built
				} else {
					idx.Files = append(idx.Files, built.Files...)
				}
			}

			graph, err := xref.Build(idx)
			if err != nil {
				return err
			}

			matches := make([]deadMatch, 0, 64)
			scanned := 0
			for _, definition := range graph.Definitions {
				if !deadKindAllowed(definition, mode) {
					continue
				}
				if !includeEntrypoints && isEntrypointDefinition(definition) {
					continue
				}
				if !includeTests && isTestSourceFile(definition.File) {
					continue
				}

				scanned++
				incoming := graph.IncomingCount(definition.ID)
				if incoming > 0 {
					continue
				}
				matches = append(matches, deadMatch{
					File:      definition.File,
					Package:   definition.Package,
					Kind:      definition.Kind,
					Name:      definition.Name,
					Signature: definition.Signature,
					StartLine: definition.StartLine,
					EndLine:   definition.EndLine,
					Incoming:  incoming,
					Outgoing:  graph.OutgoingCount(definition.ID),
				})
			}

			sort.Slice(matches, func(i, j int) bool {
				if matches[i].File == matches[j].File {
					if matches[i].StartLine == matches[j].StartLine {
						return matches[i].Name < matches[j].Name
					}
					return matches[i].StartLine < matches[j].StartLine
				}
				return matches[i].File < matches[j].File
			})

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count   int `json:"count"`
						Scanned int `json:"scanned"`
					}{
						Count:   len(matches),
						Scanned: scanned,
					})
				}
				return emitJSON(struct {
					Kind    string      `json:"kind"`
					Scanned int         `json:"scanned"`
					Count   int         `json:"count"`
					Matches []deadMatch `json:"matches,omitempty"`
				}{
					Kind:    mode,
					Scanned: scanned,
					Count:   len(matches),
					Matches: matches,
				})
			}

			if countOnly {
				fmt.Println(len(matches))
				return nil
			}

			for _, match := range matches {
				name := strings.TrimSpace(match.Signature)
				if name == "" {
					name = match.Name
				}
				fmt.Printf(
					"%s:%d:%d %s %s incoming=%d outgoing=%d\n",
					match.File,
					match.StartLine,
					match.EndLine,
					match.Kind,
					name,
					match.Incoming,
					match.Outgoing,
				)
			}
			fmt.Printf("dead: kind=%s scanned=%d matches=%d\n", mode, scanned, len(matches))
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().StringVar(&kind, "kind", "callable", "filter dead definitions by callable|function|method")
	cmd.Flags().BoolVar(&includeEntrypoints, "include-entrypoints", false, "include main/init functions in dead code results")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "include _test files in dead code results")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of dead definitions")
	return cmd
}

func runDead(args []string) error {
	cmd := newDeadCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func deadKindAllowed(definition xref.Definition, mode string) bool {
	switch mode {
	case "callable":
		return definition.Callable
	case "function":
		return definition.Kind == "function_definition"
	case "method":
		return definition.Kind == "method_definition"
	default:
		return false
	}
}

func isEntrypointDefinition(definition xref.Definition) bool {
	if definition.Kind != "function_definition" {
		return false
	}
	return definition.Name == "main" || definition.Name == "init"
}

func isTestSourceFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), "_test.go")
}
