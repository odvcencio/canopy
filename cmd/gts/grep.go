package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/query"
)

func newGrepCmd() *cobra.Command {
	var cachePath string
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "grep <selector> [path]",
		Aliases: []string{"gtsgrep"},
		Short:   "Structural grep over indexed symbols",
		Long: `Structural grep over indexed symbols using a selector DSL.

Selector format: <kind>[filter1,filter2,...]

Kinds (tree-sitter node types):
  function_definition    method_definition
  type_definition        variable_definition
  constant_definition    *  (wildcard - match any kind)

Filters (comma-separated inside brackets):
  name=/regex/           Match symbol name
  signature=/regex/      Match full signature
  receiver=/regex/       Match method receiver type
  file=/regex/           Match file path
  start>=N  start<=N     Filter by start line
  end>=N    end<=N       Filter by end line
  line=N                 Symbol must span this line`,
		Example: `  gts grep 'function_definition[name=/^Test/]' ./tests/
  gts grep 'method_definition[receiver=/Server/]' internal/api/
  gts grep '*[file=/handler/]' .
  gts grep 'type_definition' pkg/
  gts grep '*[name=/Merge/,start>=100,end<=300]' internal/`,
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector, err := query.ParseSelector(args[0])
			if err != nil {
				return err
			}

			target := "."
			if len(args) == 2 {
				target = args[1]
			}

			idx, err := loadOrBuild(cachePath, target)
			if err != nil {
				return err
			}

			matches := make([]grepMatch, 0, idx.SymbolCount())
			for _, file := range idx.Files {
				for _, symbol := range file.Symbols {
					if !selector.Match(symbol) {
						continue
					}
					matches = append(matches, grepMatch{
						File:      file.Path,
						Kind:      symbol.Kind,
						Name:      symbol.Name,
						Signature: symbol.Signature,
						StartLine: symbol.StartLine,
						EndLine:   symbol.EndLine,
					})
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
						Count int `json:"count"`
					}{
						Count: len(matches),
					})
				}
				return emitJSON(matches)
			}

			if countOnly {
				fmt.Println(len(matches))
				return nil
			}

			for _, match := range matches {
				if match.Signature != "" {
					fmt.Printf("%s:%d:%d %s %s\n", match.File, match.StartLine, match.EndLine, match.Kind, match.Signature)
					continue
				}
				fmt.Printf("%s:%d:%d %s %s\n", match.File, match.StartLine, match.EndLine, match.Kind, match.Name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of matches")
	return cmd
}

func runGrep(args []string) error {
	cmd := newGrepCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
