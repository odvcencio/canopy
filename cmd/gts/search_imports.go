package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/spf13/cobra"
)

type importMatch struct {
	File   string `json:"file"`
	Import string `json:"import"`
}

type importFileMatch struct {
	File    string   `json:"file"`
	Imports []string `json:"imports"`
}

func newImportsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var patternFilter string
	var fileFilter string
	var reverse bool

	cmd := &cobra.Command{
		Use:   "imports [path]",
		Short: "Search and list import relationships across the codebase",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cachePath, target, noCache)
			if err != nil {
				return err
			}

			var patternRE *regexp.Regexp
			if patternFilter != "" {
				compiled, compileErr := regexp.Compile(patternFilter)
				if compileErr != nil {
					return fmt.Errorf("compile --pattern regex: %w", compileErr)
				}
				patternRE = compiled
			}

			var fileRE *regexp.Regexp
			if fileFilter != "" {
				compiled, compileErr := regexp.Compile(fileFilter)
				if compileErr != nil {
					return fmt.Errorf("compile --file regex: %w", compileErr)
				}
				fileRE = compiled
			}

			// Reverse mode: find files that import something matching --pattern.
			if reverse {
				if patternRE == nil {
					return fmt.Errorf("--reverse requires --pattern to be set")
				}
				matches := make([]importFileMatch, 0, 64)
				for _, file := range idx.Files {
					if fileRE != nil && !fileRE.MatchString(file.Path) {
						continue
					}
					var matched []string
					for _, imp := range file.Imports {
						if patternRE.MatchString(imp) {
							matched = append(matched, imp)
						}
					}
					if len(matched) > 0 {
						matches = append(matches, importFileMatch{
							File:    file.Path,
							Imports: matched,
						})
					}
				}

				sort.Slice(matches, func(i, j int) bool {
					return matches[i].File < matches[j].File
				})

				if jsonOutput {
					if countOnly {
						return emitJSON(struct {
							Count int `json:"count"`
						}{Count: len(matches)})
					}
					return emitJSON(struct {
						Files []importFileMatch `json:"files"`
						Count int               `json:"count"`
					}{Files: matches, Count: len(matches)})
				}

				if countOnly {
					fmt.Println(len(matches))
					return nil
				}

				for _, m := range matches {
					for _, imp := range m.Imports {
						fmt.Printf("%s imports %s\n", m.File, imp)
					}
				}
				return nil
			}

			// Default mode: list imports, optionally filtered.
			allImports := make([]importMatch, 0, 256)
			uniqueImports := make(map[string]struct{})
			for _, file := range idx.Files {
				if fileRE != nil && !fileRE.MatchString(file.Path) {
					continue
				}
				for _, imp := range file.Imports {
					if patternRE != nil && !patternRE.MatchString(imp) {
						continue
					}
					allImports = append(allImports, importMatch{
						File:   file.Path,
						Import: imp,
					})
					uniqueImports[imp] = struct{}{}
				}
			}

			sort.Slice(allImports, func(i, j int) bool {
				if allImports[i].File == allImports[j].File {
					return allImports[i].Import < allImports[j].Import
				}
				return allImports[i].File < allImports[j].File
			})

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						UniqueImports int `json:"unique_imports"`
						TotalImports  int `json:"total_imports"`
					}{UniqueImports: len(uniqueImports), TotalImports: len(allImports)})
				}
				return emitJSON(struct {
					Imports       []importMatch `json:"imports"`
					UniqueImports int           `json:"unique_imports"`
					TotalImports  int           `json:"total_imports"`
				}{Imports: allImports, UniqueImports: len(uniqueImports), TotalImports: len(allImports)})
			}

			if countOnly {
				fmt.Printf("%d unique imports, %d total\n", len(uniqueImports), len(allImports))
				return nil
			}

			currentFile := ""
			for _, m := range allImports {
				if m.File != currentFile {
					if currentFile != "" {
						fmt.Println()
					}
					fmt.Printf("%s:\n", m.File)
					currentFile = m.File
				}
				fmt.Printf("  %s\n", m.Import)
			}
			if len(allImports) > 0 {
				fmt.Fprintf(os.Stderr, "\n%d unique imports, %d total\n", len(uniqueImports), len(allImports))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of matches")
	cmd.Flags().StringVar(&patternFilter, "pattern", "", "regex filter on import path")
	cmd.Flags().StringVar(&fileFilter, "file", "", "regex filter on file path")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "find files that import something matching --pattern")
	return cmd
}
