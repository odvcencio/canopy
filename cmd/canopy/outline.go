package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"m31labs.dev/canopy/internal/outline"
)

func newOutlineCmd() *cobra.Command {
	var asJSON bool
	var opts outline.Options
	cmd := &cobra.Command{Use: "outline <file>", Short: "Read a bounded live symbol outline without building an index", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		report, err := outline.ReadFile(cmd.Context(), args[0], opts)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) source=%s\n", report.Path, report.Language, report.SourceSHA256)
		for _, sym := range report.Symbols {
			owner := ""
			if sym.Owner != "" {
				owner = " owner=" + sym.Owner
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s%s %s [%d:%d]%s\n", strings.Repeat("  ", min(sym.Depth, 8)), sym.Kind, sym.Name, sym.StartLine, sym.EndLine, owner)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "coverage kinds=%s parse_error=%t query_truncated=%t decline=%q omissions=%v owner_misses=%d\n", strings.Join(report.Coverage.DefinitionKinds, ","), report.Coverage.TreeHasError, report.Coverage.QueryTruncated, report.Coverage.DeclineReason, report.Coverage.Omissions, report.Coverage.OwnerRuleMisses)
		if report.NextCursor != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "next_cursor: %s\n", report.NextCursor)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit bounded JSON with source identity and coverage diagnostics")
	cmd.Flags().IntVar(&opts.Limit, "limit", outline.DefaultLimit, "symbols per page (maximum 100)")
	cmd.Flags().StringVar(&opts.Cursor, "cursor", "", "continue the same unchanged file; pass the returned cursor unchanged")
	return cmd
}
