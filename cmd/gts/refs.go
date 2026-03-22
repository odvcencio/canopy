package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newRefsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var regexMode bool
	var jsonOutput bool
	var countOnly bool
	var limit int

	cmd := &cobra.Command{
		Use:     "refs <name|regex> [path]",
		Aliases: []string{"gtsrefs"},
		Short:   "Find indexed references by symbol name",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 2 {
				target = args[1]
			}

			idx, err := loadOrBuild(cachePath, target, noCache)
			if err != nil {
				return err
			}

			pattern := strings.TrimSpace(args[0])
			if pattern == "" {
				return errors.New("reference matcher cannot be empty")
			}

			matchReference := func(name string) bool { return name == pattern }
			if regexMode {
				compiled, compileErr := regexp.Compile(pattern)
				if compileErr != nil {
					return fmt.Errorf("compile regex: %w", compileErr)
				}
				matchReference = compiled.MatchString
			}

			truncated := false
			matches := make([]referenceMatch, 0, 256)
		outer:
			for _, file := range idx.Files {
				for _, reference := range file.References {
					if !matchReference(reference.Name) {
						continue
					}
					matches = append(matches, referenceMatch{
						File:        file.Path,
						Kind:        reference.Kind,
						Name:        reference.Name,
						StartLine:   reference.StartLine,
						EndLine:     reference.EndLine,
						StartColumn: reference.StartColumn,
						EndColumn:   reference.EndColumn,
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
						if matches[i].StartColumn == matches[j].StartColumn {
							return matches[i].Name < matches[j].Name
						}
						return matches[i].StartColumn < matches[j].StartColumn
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
						Matches   []referenceMatch `json:"matches"`
						Truncated bool             `json:"truncated"`
					}{Matches: matches, Truncated: true})
				}
				return emitJSON(matches)
			}

			if countOnly {
				fmt.Println(len(matches))
				if truncated {
					fmt.Printf("truncated: limit=%d\n", limit)
				}
				return nil
			}
			for _, match := range matches {
				fmt.Printf("%s:%d:%d %s %s\n", match.File, match.StartLine, match.StartColumn, match.Kind, match.Name)
			}
			if truncated {
				fmt.Fprintf(os.Stderr, "warning: results truncated at limit=%d, use --limit 0 for all\n", limit)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&regexMode, "regex", false, "treat the first argument as a regular expression")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of matches")
	cmd.Flags().IntVar(&limit, "limit", 1000, "maximum number of results (0 for unlimited)")
	return cmd
}

func runRefs(args []string) error {
	cmd := newRefsCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
