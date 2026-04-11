package main

import (
	"fmt"

	"github.com/spf13/cobra"

	gtsscope "github.com/odvcencio/gts-suite/internal/scope"
)

func newScopeCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var rootPath string
	var line int
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "scope <file>",
		Aliases: []string{"gtsscope"},
		Short:   "Resolve symbols in scope for a file and line",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			idx, err := loadOrBuild(cmd, cachePath, rootPath, noCache)
			if err != nil {
				return err
			}

			report, err := gtsscope.Build(idx, gtsscope.Options{
				FilePath: filePath,
				Line:     line,
			})
			if err != nil {
				return err
			}

			if countOnly {
				fmt.Println(len(report.Symbols))
				return nil
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf("file: %s\n", report.File)
			fmt.Printf("line: %d\n", report.Line)
			fmt.Printf("package: %s\n", report.Package)
			if report.Focus != nil {
				fmt.Printf("focus: %s %s [%d:%d]\n", report.Focus.Kind, symbolLabel(report.Focus.Name, report.Focus.Signature), report.Focus.StartLine, report.Focus.EndLine)
			}
			fmt.Printf("symbols: %d\n", len(report.Symbols))
			for _, symbol := range report.Symbols {
				if symbol.Detail != "" {
					fmt.Printf("  %s (%s) line=%d detail=%s\n", symbol.Name, symbol.Kind, symbol.DeclLine, symbol.Detail)
					continue
				}
				fmt.Printf("  %s (%s) line=%d\n", symbol.Name, symbol.Kind, symbol.DeclLine)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&rootPath, "root", ".", "parse root path when cache is not provided")
	cmd.Flags().IntVar(&line, "line", 1, "cursor line (1-based)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of symbols in scope")
	return cmd
}

func runScope(args []string) error {
	cmd := newScopeCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
