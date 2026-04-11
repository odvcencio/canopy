package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/contextpack"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

func newContextCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var rootPath string
	var line int
	var tokens int
	var semantic bool
	var semanticDepth int
	var jsonOutput bool
	var concept string

	cmd := &cobra.Command{
		Use:     "context <file>",
		Aliases: []string{"gtscontext"},
		Short:   "Pack focused code context for a file and line",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Concept mode: search symbols and pack context around matches.
			if concept != "" {
				idx, err := loadOrBuild(cmd, cachePath, rootPath, noCache)
				if err != nil {
					return err
				}
				idx = applyGeneratedFilter(cmd, idx)

				report, err := buildConceptContext(idx, concept, tokens)
				if err != nil {
					return err
				}
				if jsonOutput {
					return emitJSON(report)
				}
				fmt.Printf("concept: %q tokens=%d matches=%d\n", report.Concept, report.TokenBudget, len(report.Matches))
				for _, m := range report.Matches {
					fmt.Printf("  %s %s %s [%d:%d]\n", m.File, m.Kind, m.Name, m.StartLine, m.EndLine)
				}
				if len(report.CallChain) > 0 {
					fmt.Printf("call chain (%d related):\n", len(report.CallChain))
					for _, r := range report.CallChain {
						fmt.Printf("  %s %s %s [%d:%d]\n", r.File, r.Kind, r.Name, r.StartLine, r.EndLine)
					}
				}
				if report.Truncated {
					fmt.Println("truncated: true")
				}
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("requires a file argument or --concept flag")
			}
			filePath := args[0]
			idx, err := loadOrBuild(cmd, cachePath, rootPath, noCache)
			if err != nil {
				return err
			}
			idx = applyGeneratedFilter(cmd, idx)

			report, err := contextpack.Build(idx, contextpack.Options{
				FilePath:      filePath,
				Line:          line,
				TokenBudget:   tokens,
				Semantic:      semantic,
				SemanticDepth: semanticDepth,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf("file: %s\n", report.File)
			fmt.Printf("line: %d\n", report.Line)
			fmt.Printf("budget: %d (estimated: %d)\n", report.TokenBudget, report.EstimatedTokens)
			fmt.Printf("semantic: %t\n", report.Semantic)
			if report.Semantic {
				fmt.Printf("semantic-depth: %d\n", report.SemanticDepth)
			}
			if report.Focus != nil {
				fmt.Printf("focus: %s %s [%d:%d]\n", report.Focus.Kind, symbolLabel(report.Focus.Name, report.Focus.Signature), report.Focus.StartLine, report.Focus.EndLine)
			}
			if len(report.Imports) > 0 {
				fmt.Printf("imports: %s\n", strings.Join(report.Imports, ", "))
			}
			fmt.Printf("snippet [%d:%d]:\n", report.SnippetStart, report.SnippetEnd)
			fmt.Print(report.Snippet)
			if len(report.Related) > 0 {
				fmt.Println("related:")
				for _, symbol := range report.Related {
					fmt.Printf("  %s %s [%d:%d]\n", symbol.Kind, symbolLabel(symbol.Name, symbol.Signature), symbol.StartLine, symbol.EndLine)
				}
			}
			if report.Truncated {
				fmt.Println("truncated: true")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&rootPath, "root", ".", "parse root path when cache is not provided")
	cmd.Flags().IntVar(&line, "line", 1, "cursor line (1-based)")
	cmd.Flags().IntVar(&tokens, "tokens", 800, "token budget")
	cmd.Flags().BoolVar(&semantic, "semantic", false, "pack semantic dependency context when possible")
	cmd.Flags().IntVar(&semanticDepth, "semantic-depth", 1, "dependency traversal depth in semantic mode")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&concept, "concept", "", "search concept query: find symbols matching this term and pack related context")
	return cmd
}

// conceptReport holds the result of concept-aware context packing.
type conceptReport struct {
	Concept     string         `json:"concept"`
	TokenBudget int            `json:"token_budget"`
	Matches     []model.Symbol `json:"matches"`
	CallChain   []model.Symbol `json:"call_chain,omitempty"`
	Truncated   bool           `json:"truncated"`
}

// buildConceptContext searches symbols and file paths for concept matches,
// then uses xref to find related call chains, packing within the token budget.
func buildConceptContext(idx *model.Index, concept string, budget int) (conceptReport, error) {
	if budget <= 0 {
		budget = 800
	}
	query := strings.ToLower(strings.TrimSpace(concept))
	if query == "" {
		return conceptReport{}, fmt.Errorf("concept query cannot be empty")
	}

	report := conceptReport{
		Concept:     concept,
		TokenBudget: budget,
	}

	// Search symbol names and file paths for case-insensitive substring matches.
	type matchInfo struct {
		symbol model.Symbol
		file   string
	}
	var matches []matchInfo

	for _, file := range idx.Files {
		fileMatches := strings.Contains(strings.ToLower(filepath.Base(file.Path)), query)
		for _, sym := range file.Symbols {
			if fileMatches || strings.Contains(strings.ToLower(sym.Name), query) {
				matches = append(matches, matchInfo{symbol: sym, file: file.Path})
			}
		}
	}

	// Pack matches within budget.
	used := 0
	for _, m := range matches {
		cost := estimateConceptTokens(m.symbol)
		if used+cost > budget {
			report.Truncated = true
			break
		}
		sym := m.symbol
		sym.File = m.file
		report.Matches = append(report.Matches, sym)
		used += cost
	}

	if len(report.Matches) == 0 {
		return report, nil
	}

	// Use xref to find related call chains for matching symbols.
	graph, err := xref.Build(idx)
	if err != nil {
		return report, nil // Return what we have without call chains.
	}

	// Find definition IDs for matched symbols.
	var rootIDs []string
	matchSet := make(map[string]bool)
	for _, m := range report.Matches {
		for _, def := range graph.Definitions {
			if def.Name == m.Name && def.File == m.File && def.StartLine == m.StartLine {
				if !matchSet[def.ID] {
					matchSet[def.ID] = true
					rootIDs = append(rootIDs, def.ID)
				}
				break
			}
		}
	}

	if len(rootIDs) > 0 {
		walk := graph.Walk(rootIDs, 2, false)
		for _, node := range walk.Nodes {
			if matchSet[node.ID] {
				continue
			}
			cost := estimateConceptTokens(model.Symbol{Name: node.Name, Signature: node.Signature})
			if used+cost > budget {
				report.Truncated = true
				break
			}
			report.CallChain = append(report.CallChain, model.Symbol{
				File:      node.File,
				Kind:      node.Kind,
				Name:      node.Name,
				Signature: node.Signature,
				Receiver:  node.Receiver,
				StartLine: node.StartLine,
				EndLine:   node.EndLine,
			})
			used += cost
		}
	}

	return report, nil
}

func estimateConceptTokens(sym model.Symbol) int {
	text := sym.Name
	if sym.Signature != "" {
		text = sym.Signature
	}
	return (len(text)+3)/4 + 4
}

func runContext(args []string) error {
	cmd := newContextCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
