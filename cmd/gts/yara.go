package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/yara"
)

func newYaraCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var ruleName string
	var minStrings int
	var maxStrings int

	cmd := &cobra.Command{
		Use:     "yara [path]",
		Aliases: []string{"gtsyara"},
		Short:   "Generate YARA rules from structural analysis",
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

			opts := yara.Options{
				RuleName:   ruleName,
				MinStrings: minStrings,
				MaxStrings: maxStrings,
			}

			rules, err := yara.Generate(idx, idx.Root, opts)
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(struct {
					Count int                  `json:"count"`
					Rules []yara.GeneratedRule `json:"rules,omitempty"`
				}{
					Count: len(rules),
					Rules: rules,
				})
			}

			if len(rules) == 0 {
				fmt.Println("yara: no rules generated (insufficient string literals or API calls)")
				return nil
			}

			for _, r := range rules {
				fmt.Println(r.YARAText)
				fmt.Printf("// strings: %d, apis: %d\n", len(r.Strings), len(r.APIs))
				if len(r.APIs) > 0 {
					fmt.Printf("// apis: %s\n", strings.Join(r.APIs, ", "))
				}
				fmt.Println()
			}
			fmt.Printf("yara: %d rule(s) generated\n", len(rules))
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&ruleName, "rule-name", "generated_rule", "name for the generated YARA rule")
	cmd.Flags().IntVar(&minStrings, "min-strings", 3, "minimum number of string literals required")
	cmd.Flags().IntVar(&maxStrings, "max-strings", 20, "maximum number of string literals to include")
	return cmd
}
