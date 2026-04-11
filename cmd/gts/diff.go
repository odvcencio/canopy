package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/structdiff"
)

func newDiffCmd() *cobra.Command {
	var beforeCache string
	var afterCache string
	var noCache bool
	var jsonOutput bool
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "diff [before-path] [after-path]",
		Aliases: []string{"gtsdiff"},
		Short:   "Structural diff between two snapshots",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			beforeTarget, afterTarget, err := resolveDiffSources(args, beforeCache, afterCache)
			if err != nil {
				return err
			}

			beforeIndex, err := loadOrBuild(cmd, beforeCache, beforeTarget, noCache)
			if err != nil {
				return fmt.Errorf("load before snapshot: %w", err)
			}
			afterIndex, err := loadOrBuild(cmd, afterCache, afterTarget, noCache)
			if err != nil {
				return fmt.Errorf("load after snapshot: %w", err)
			}

			report := structdiff.Compare(beforeIndex, afterIndex)

			if countOnly {
				fmt.Println(report.Stats.AddedSymbols + report.Stats.RemovedSymbols + report.Stats.ModifiedSymbols)
				return nil
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf("changed files: %d\n", report.Stats.ChangedFiles)
			fmt.Printf("symbols: +%d -%d ~%d\n", report.Stats.AddedSymbols, report.Stats.RemovedSymbols, report.Stats.ModifiedSymbols)

			for _, item := range report.AddedSymbols {
				fmt.Printf("+ %s:%d:%d %s %s\n", item.File, item.StartLine, item.EndLine, item.Kind, symbolLabel(item.Name, item.Signature))
			}
			for _, item := range report.RemovedSymbols {
				fmt.Printf("- %s:%d:%d %s %s\n", item.File, item.StartLine, item.EndLine, item.Kind, symbolLabel(item.Name, item.Signature))
			}
			for _, item := range report.ModifiedSymbols {
				fmt.Printf("~ %s:%d:%d %s %s fields=%s\n",
					item.After.File,
					item.After.StartLine,
					item.After.EndLine,
					item.After.Kind,
					symbolLabel(item.After.Name, item.After.Signature),
					strings.Join(item.Fields, ","))
			}
			for _, change := range report.ImportChanges {
				parts := make([]string, 0, 2)
				if len(change.Added) > 0 {
					parts = append(parts, "added="+strings.Join(change.Added, ","))
				}
				if len(change.Removed) > 0 {
					parts = append(parts, "removed="+strings.Join(change.Removed, ","))
				}
				fmt.Printf("i %s %s\n", change.File, strings.Join(parts, " "))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&beforeCache, "before-cache", "", "load before snapshot from cache file")
	cmd.Flags().StringVar(&afterCache, "after-cache", "", "load after snapshot from cache file")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of changed symbols")
	return cmd
}

func runDiff(args []string) error {
	cmd := newDiffCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func resolveDiffSources(args []string, beforeCache, afterCache string) (string, string, error) {
	positionals := append([]string(nil), args...)

	beforeTarget := ""
	afterTarget := ""

	if strings.TrimSpace(beforeCache) == "" {
		if len(positionals) == 0 {
			return "", "", errors.New("missing before source: provide [before-path] or --before-cache")
		}
		beforeTarget = positionals[0]
		positionals = positionals[1:]
	}

	if strings.TrimSpace(afterCache) == "" {
		if len(positionals) == 0 {
			return "", "", errors.New("missing after source: provide [after-path] or --after-cache")
		}
		afterTarget = positionals[0]
		positionals = positionals[1:]
	}

	if len(positionals) > 0 {
		return "", "", fmt.Errorf("unexpected positional arguments: %s", strings.Join(positionals, " "))
	}

	return beforeTarget, afterTarget, nil
}
