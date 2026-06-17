package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/roots"
	"m31labs.dev/canopy/pkg/xref"
)

func newDeadCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var kind string
	var includeEntrypoints bool
	var includeTests bool
	var jsonOutput bool
	var countOnly bool
	var limit int
	var exportedRoots bool
	var profileAnnotations []string
	var rootsConfig string

	cmd := &cobra.Command{
		Use:     "dead [path...]",
		Aliases: []string{"gtsdead"},
		Short:   "List callable definitions with zero incoming call references",
		Long: `List callable definitions with zero incoming call references.

Multiple paths can be provided to build the cross-reference graph across
packages, reducing false positives for exported symbols called from other
packages.

The command uses language-aware reachability-root detection to suppress false
positives for framework callbacks (Spring controllers, @Bean, @Scheduled),
JPA repository methods, test functions, and other framework-invoked callables.

Examples:
  gts dead internal/service/
  gts dead internal/service/ internal/api/    # cross-package analysis
  gts dead /path/to/java-app                  # Java Spring Boot app`,
		Args: cobra.ArbitraryArgs,
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
				built, err := loadOrBuild(cmd, cachePath, target, noCache)
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

			// Compute call-resolution rate for confidence scoring.
			resolutionRate := roots.ResolutionRate(graph)

			// Build the language-aware root analyzer.
			// Load user-defined roots config: an explicit --roots-config path, or
			// auto-discover .canopyroots.json at the index root. Merges over the
			// built-in language profiles.
			var rootsCfg *roots.Config
			if rootsConfig != "" {
				rootsCfg, err = roots.LoadConfigFile(rootsConfig)
			} else {
				rootsCfg, err = roots.LoadConfig(graph.Root)
			}
			if err != nil {
				return err
			}
			analyzer, err := roots.NewWithConfig(graph.Root, roots.Options{
				TreatExportedAsRoots: exportedRoots,
				ExtraRootAnnotations: profileAnnotations,
			}, rootsCfg)
			if err != nil {
				return err
			}

			matches := make([]deadMatch, 0, 64)
			scanned := 0
			for _, definition := range graph.Definitions {
				if !deadKindAllowed(definition, mode) {
					continue
				}

				isRoot, reason := analyzer.IsRoot(definition, graph.Definitions)

				if isRoot {
					// Entrypoints: skip unless --include-entrypoints is set.
					if reason == "entrypoint" && !includeEntrypoints {
						continue
					}
					// Tests: skip unless --include-tests is set.
					if reason == "test" && !includeTests {
						continue
					}
					// All other roots (framework callbacks, annotations, etc.) are
					// always suppressed — they cannot be dead by definition.
					if reason != "entrypoint" && reason != "test" {
						continue
					}
				}

				scanned++
				incoming := graph.IncomingCount(definition.ID)
				if incoming > 0 {
					continue
				}
				conf := roots.DeadConfidence(resolutionRate, definition)
				matches = append(matches, deadMatch{
					File:       definition.File,
					Package:    definition.Package,
					Kind:       definition.Kind,
					Name:       definition.Name,
					Signature:  definition.Signature,
					StartLine:  definition.StartLine,
					EndLine:    definition.EndLine,
					Incoming:   incoming,
					Outgoing:   graph.OutgoingCount(definition.ID),
					Confidence: string(conf),
				})
			}

			// Filter out generated files unless --include-generated is set.
			includeGenerated, _ := cmd.Flags().GetBool("include-generated")
			if !includeGenerated {
				genMap := generatedFileMap(idx)
				filtered := matches[:0]
				for _, match := range matches {
					if genMap[match.File] == nil {
						filtered = append(filtered, match)
					}
				}
				matches = filtered
			}

			// Filter by specific generator if --generator is set.
			generator, _ := cmd.Flags().GetString("generator")
			if generator != "" {
				genMap := generatedFileMap(idx)
				var genFiltered []deadMatch
				for _, match := range matches {
					gi := genMap[match.File]
					if generator == "human" && gi == nil {
						genFiltered = append(genFiltered, match)
					} else if gi != nil && gi.Generator == generator {
						genFiltered = append(genFiltered, match)
					}
				}
				matches = genFiltered
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

			truncated := false
			if limit > 0 && len(matches) > limit {
				matches = matches[:limit]
				truncated = true
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count     int  `json:"count"`
						Scanned   int  `json:"scanned"`
						Truncated bool `json:"truncated,omitempty"`
					}{
						Count:     len(matches),
						Scanned:   scanned,
						Truncated: truncated,
					})
				}
				if truncated {
					return emitJSON(struct {
						Kind      string      `json:"kind"`
						Scanned   int         `json:"scanned"`
						Count     int         `json:"count"`
						Truncated bool        `json:"truncated"`
						Matches   []deadMatch `json:"matches,omitempty"`
					}{
						Kind:      mode,
						Scanned:   scanned,
						Count:     len(matches),
						Truncated: true,
						Matches:   matches,
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
				if truncated {
					fmt.Printf("truncated: limit=%d\n", limit)
				}
				return nil
			}

			for _, match := range matches {
				name := strings.TrimSpace(match.Signature)
				if name == "" {
					name = match.Name
				}
				fmt.Printf(
					"%s:%d:%d %s %s incoming=%d outgoing=%d confidence=%s\n",
					match.File,
					match.StartLine,
					match.EndLine,
					match.Kind,
					name,
					match.Incoming,
					match.Outgoing,
					match.Confidence,
				)
			}
			fmt.Printf("dead: kind=%s scanned=%d matches=%d\n", mode, scanned, len(matches))
			if truncated {
				fmt.Fprintf(os.Stderr, "warning: results truncated at limit=%d, use --limit 0 for all\n", limit)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&kind, "kind", "callable", "filter dead definitions by callable|function|method")
	cmd.Flags().BoolVar(&includeEntrypoints, "include-entrypoints", false, "include entrypoint functions (main/init) in dead code results")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "include test files and test functions in dead code results")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of dead definitions")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of results (0 for unlimited)")
	cmd.Flags().BoolVar(&exportedRoots, "exported-roots", false, "treat public/exported callables as reachability roots (reduces false positives for library code)")
	cmd.Flags().StringArrayVar(&profileAnnotations, "profile-annotation", nil, "extra root annotations merged into every language profile (e.g. @MyFrameworkHandler)")
	cmd.Flags().StringVar(&rootsConfig, "roots-config", "", "path to a .canopyroots.json profile config (default: auto-discover at the index root)")
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
