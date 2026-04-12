package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newErrorsCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:   "errors [path]",
		Short: "Show files that failed to parse with error details",
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

			if countOnly {
				fmt.Println(len(idx.Errors))
				return nil
			}

			if jsonOutput {
				return emitJSON(idx.Errors)
			}

			if len(idx.Errors) == 0 {
				fmt.Println("no parse errors")
				return nil
			}

			for _, pe := range idx.Errors {
				fmt.Printf("%s: %s\n", pe.Path, pe.Error)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the error count")
	return cmd
}
