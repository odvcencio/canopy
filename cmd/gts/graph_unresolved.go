package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/xref"
)

func newUnresolvedCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var limit int
	var reason string
	var caller string

	cmd := &cobra.Command{
		Use:   "unresolved [path]",
		Short: "Show call references that could not be resolved to a definition",
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

			graph, err := xref.Build(idx)
			if err != nil {
				return err
			}

			var callerRe *regexp.Regexp
			if caller != "" {
				callerRe, err = regexp.Compile(caller)
				if err != nil {
					return fmt.Errorf("compile --caller regex: %w", err)
				}
			}

			reason = strings.TrimSpace(strings.ToLower(reason))

			matches := make([]xref.UnresolvedCall, 0, len(graph.Unresolved))
			for _, u := range graph.Unresolved {
				if reason != "" && u.Reason != reason {
					continue
				}
				if callerRe != nil {
					callerName := ""
					if u.Caller != nil {
						callerName = u.Caller.Name
					}
					if !callerRe.MatchString(callerName) {
						continue
					}
				}
				matches = append(matches, u)
			}

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count int `json:"count"`
						Total int `json:"total"`
					}{
						Count: len(matches),
						Total: len(graph.Unresolved),
					})
				}
				if limit > 0 && len(matches) > limit {
					matches = matches[:limit]
				}
				return emitJSON(struct {
					Count   int                  `json:"count"`
					Total   int                  `json:"total"`
					Entries []xref.UnresolvedCall `json:"entries,omitempty"`
				}{
					Count:   len(matches),
					Total:   len(graph.Unresolved),
					Entries: matches,
				})
			}

			if countOnly {
				fmt.Println(len(matches))
				return nil
			}

			if limit > 0 && len(matches) > limit {
				matches = matches[:limit]
			}

			for _, u := range matches {
				callerName := "<top-level>"
				if u.Caller != nil {
					callerName = definitionLabel(*u.Caller)
				}
				fmt.Printf(
					"%s → %s (%s, %d candidates)\n",
					callerName,
					u.Name,
					u.Reason,
					u.CandidateCount,
				)
			}
			fmt.Printf("unresolved: shown=%d total=%d\n", len(matches), len(graph.Unresolved))
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of unresolved references")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of entries to display")
	cmd.Flags().StringVar(&reason, "reason", "", "filter by resolution failure reason (e.g. not_found, ambiguous_global)")
	cmd.Flags().StringVar(&caller, "caller", "", "regex filter on caller name")
	return cmd
}
