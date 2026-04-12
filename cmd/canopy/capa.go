package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/capa"
)

func newCapaCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var category string
	var minConfidence string
	var countOnly bool

	cmd := &cobra.Command{
		Use:     "capa [path]",
		Aliases: []string{"gtscapa"},
		Short:   "Detect capabilities from structural API/import patterns with MITRE ATT&CK mapping",
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

			rules := capa.BuiltinRules()

			// Filter rules by category if specified
			if category != "" {
				filtered := make([]capa.Rule, 0, len(rules))
				for _, r := range rules {
					if strings.EqualFold(r.Category, category) {
						filtered = append(filtered, r)
					}
				}
				rules = filtered
			}

			matches := capa.Detect(idx, rules)

			// Filter by minimum confidence
			if minConfidence != "" {
				matches = filterByConfidence(matches, minConfidence)
			}

			// Sort by category then rule name for stable output
			sort.Slice(matches, func(i, j int) bool {
				if matches[i].Rule.Category == matches[j].Rule.Category {
					return matches[i].Rule.Name < matches[j].Rule.Name
				}
				return matches[i].Rule.Category < matches[j].Rule.Category
			})

			if countOnly {
				fmt.Println(len(matches))
				return nil
			}

			if jsonOutput {
				return emitJSON(struct {
					Count   int          `json:"count"`
					Matches []capa.Match `json:"matches,omitempty"`
				}{
					Count:   len(matches),
					Matches: matches,
				})
			}

			if len(matches) == 0 {
				fmt.Println("capa: no capabilities detected")
				return nil
			}

			for _, m := range matches {
				fmt.Printf("[%s] %s (%s) — %s\n",
					m.Rule.AttackID,
					m.Rule.Name,
					m.Rule.Confidence,
					m.Rule.Description,
				)
				fmt.Printf("  matched APIs: %s\n", strings.Join(m.MatchedAPIs, ", "))
				fmt.Printf("  files:        %s\n", strings.Join(m.Files, ", "))
				fmt.Printf("  functions:    %s\n", strings.Join(m.Functions, ", "))
				fmt.Println()
			}
			fmt.Printf("capa: %d capabilities detected\n", len(matches))
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&category, "category", "", "filter by capability category")
	cmd.Flags().StringVar(&minConfidence, "min-confidence", "", "filter by minimum confidence (low, medium, high)")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of detected capabilities")
	return cmd
}

var confidenceLevel = map[string]int{
	"low":    1,
	"medium": 2,
	"high":   3,
}

func filterByConfidence(matches []capa.Match, minConf string) []capa.Match {
	threshold, ok := confidenceLevel[strings.ToLower(minConf)]
	if !ok {
		return matches
	}
	filtered := make([]capa.Match, 0, len(matches))
	for _, m := range matches {
		level := confidenceLevel[strings.ToLower(m.Rule.Confidence)]
		if level >= threshold {
			filtered = append(filtered, m)
		}
	}
	return filtered
}
