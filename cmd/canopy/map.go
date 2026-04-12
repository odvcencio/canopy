package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
	"github.com/spf13/cobra"
)

func newMapCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var limit int
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "map [path]",
		Aliases: []string{"gtsmap"},
		Short:   "Print structural summaries for indexed files",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			if countOnly {
				fmt.Println(len(idx.Files))
				return nil
			}

			if jsonOutput {
				return streamIndexJSON(os.Stdout, idx, limit)
			}

			genMap := generatedFileMap(idx)

			fileCount := len(idx.Files)
			if limit > 0 && limit < fileCount {
				fileCount = limit
			}
			for i := 0; i < fileCount; i++ {
				file := idx.Files[i]
				genTag := ""
				if gi := genMap[file.Path]; gi != nil {
					genTag = fmt.Sprintf(" [gen:%s]", gi.Generator)
				}
				fmt.Printf("%s (%s)%s\n", file.Path, file.Language, genTag)
				if len(file.Imports) > 0 {
					fmt.Printf("  imports: %s\n", strings.Join(file.Imports, ", "))
				}
				for _, symbol := range file.Symbols {
					if symbol.Signature != "" {
						fmt.Printf("  %s %s [%d:%d]\n", symbol.Kind, symbol.Signature, symbol.StartLine, symbol.EndLine)
						continue
					}
					fmt.Printf("  %s %s [%d:%d]\n", symbol.Kind, symbol.Name, symbol.StartLine, symbol.EndLine)
				}
			}

			if limit > 0 && limit < len(idx.Files) {
				fmt.Fprintf(os.Stderr, "warning: output truncated at limit=%d of %d files, use --limit 0 for all\n", limit, len(idx.Files))
			}

			if len(idx.Errors) > 0 {
				fmt.Printf("errors: %d\n", len(idx.Errors))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().IntVar(&limit, "limit", 0, "limit number of files in output (0 for all)")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of files")
	return cmd
}

func runMap(args []string) error {
	cmd := newMapCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func streamIndexJSON(w io.Writer, idx *model.Index, limit int) error {
	// Write opening and scalar fields manually
	fmt.Fprintf(w, "{\n")
	fmt.Fprintf(w, "  \"version\": %s,\n", jsonString(idx.Version))
	fmt.Fprintf(w, "  \"root\": %s,\n", jsonString(idx.Root))

	genAt, _ := json.Marshal(idx.GeneratedAt)
	fmt.Fprintf(w, "  \"generated_at\": %s,\n", string(genAt))

	// Stream files array
	fmt.Fprintf(w, "  \"files\": [\n")
	fileCount := len(idx.Files)
	if limit > 0 && limit < fileCount {
		fileCount = limit
	}
	for i := 0; i < fileCount; i++ {
		data, err := json.MarshalIndent(idx.Files[i], "    ", "  ")
		if err != nil {
			return err
		}
		if i > 0 {
			fmt.Fprintf(w, ",\n")
		}
		fmt.Fprintf(w, "    %s", string(data))
	}
	fmt.Fprintf(w, "\n  ]")

	// Errors
	if len(idx.Errors) > 0 {
		fmt.Fprintf(w, ",\n  \"errors\": ")
		errData, err := json.Marshal(idx.Errors)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%s", string(errData))
	}

	// Truncation indicator
	if limit > 0 && limit < len(idx.Files) {
		fmt.Fprintf(w, ",\n  \"truncated\": true")
	}

	fmt.Fprintf(w, "\n}\n")
	return nil
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
