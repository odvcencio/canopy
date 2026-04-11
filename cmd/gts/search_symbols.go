package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/query"
)

type symbolMatch struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Generated string `json:"generated,omitempty"`
}

func newSymbolsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var limit int
	var kindFilter string
	var nameFilter string
	var fileFilter string
	var selectorFilter string

	cmd := &cobra.Command{
		Use:   "symbols [path]",
		Short: "Search and filter symbols across the index",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			// Build matchers from flags.
			var nameRE *regexp.Regexp
			if nameFilter != "" {
				compiled, compileErr := regexp.Compile(nameFilter)
				if compileErr != nil {
					return fmt.Errorf("compile --name regex: %w", compileErr)
				}
				nameRE = compiled
			}

			var fileRE *regexp.Regexp
			if fileFilter != "" {
				compiled, compileErr := regexp.Compile(fileFilter)
				if compileErr != nil {
					return fmt.Errorf("compile --file regex: %w", compileErr)
				}
				fileRE = compiled
			}

			var selector *query.Selector
			if selectorFilter != "" {
				sel, selErr := query.ParseSelector(selectorFilter)
				if selErr != nil {
					return fmt.Errorf("parse --selector: %w", selErr)
				}
				selector = &sel
			}

			genMap := generatedFileMap(idx)

			truncated := false
			matches := make([]symbolMatch, 0, 256)
		outer:
			for _, file := range idx.Files {
				if fileRE != nil && !fileRE.MatchString(file.Path) {
					continue
				}
				genTag := ""
				if gi := genMap[file.Path]; gi != nil {
					genTag = gi.Generator
				}
				for _, sym := range file.Symbols {
					if selector != nil {
						if !selector.Match(sym) {
							continue
						}
					} else {
						if kindFilter != "" && sym.Kind != kindFilter {
							continue
						}
						if nameRE != nil && !nameRE.MatchString(sym.Name) {
							continue
						}
					}
					matches = append(matches, symbolMatch{
						File:      file.Path,
						Kind:      sym.Kind,
						Name:      sym.Name,
						Signature: sym.Signature,
						Receiver:  sym.Receiver,
						StartLine: sym.StartLine,
						EndLine:   sym.EndLine,
						Generated: genTag,
					})
					if limit > 0 && len(matches) >= limit {
						truncated = true
						break outer
					}
				}
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
						Count     int  `json:"count"`
						Truncated bool `json:"truncated,omitempty"`
					}{Count: len(matches), Truncated: truncated})
				}
				if truncated {
					return emitJSON(struct {
						Symbols   []symbolMatch `json:"symbols"`
						Count     int           `json:"count"`
						Truncated bool          `json:"truncated"`
					}{Symbols: matches, Count: len(matches), Truncated: true})
				}
				return emitJSON(struct {
					Symbols []symbolMatch `json:"symbols"`
					Count   int           `json:"count"`
				}{Symbols: matches, Count: len(matches)})
			}

			if countOnly {
				fmt.Println(len(matches))
				if truncated {
					fmt.Printf("truncated: limit=%d\n", limit)
				}
				return nil
			}

			for _, m := range matches {
				label := m.Name
				if m.Signature != "" {
					label = m.Signature
				}
				genSuffix := ""
				if m.Generated != "" {
					genSuffix = fmt.Sprintf(" [gen:%s]", m.Generated)
				}
				fmt.Printf("%s:%d %s %s%s\n", m.File, m.StartLine, m.Kind, label, genSuffix)
			}
			if truncated {
				fmt.Fprintf(os.Stderr, "warning: results truncated at limit=%d, use --limit 0 for all\n", limit)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of matches")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum number of results (0 for unlimited)")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "filter by symbol kind (e.g. function_definition, method_definition)")
	cmd.Flags().StringVar(&nameFilter, "name", "", "filter by name regex")
	cmd.Flags().StringVar(&fileFilter, "file", "", "filter by file path regex")
	cmd.Flags().StringVar(&selectorFilter, "selector", "", "full query DSL selector string (overrides --kind and --name)")
	return cmd
}
