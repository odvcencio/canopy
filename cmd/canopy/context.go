package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"m31labs.dev/canopy/internal/contextpack"
	"m31labs.dev/canopy/pkg/contextbundle"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
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

	// Bundle-mode flags (spec 10.13). Setting any of these routes the
	// request through pkg/contextbundle instead of the legacy line/semantic
	// packer; leaving them all unset keeps `canopy search context` exactly
	// as it was.
	var bundleMode string
	var bundleTask string
	var bundleFocus string
	var bundleSymbol string
	var bundleManifestPath string
	var bundleReceiptPath string

	cmd := &cobra.Command{
		Use:     "context [file]",
		Aliases: []string{"gtscontext"},
		Short:   "Pack focused code context for a file and line, or build a task-conditioned context bundle",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if wantsBundleMode(bundleMode, bundleTask, bundleFocus, bundleSymbol) && !contextpack.UseLegacy() {
				idx, err := loadOrBuild(cmd, cachePath, rootPath, noCache)
				if err != nil {
					return err
				}
				generator, _ := cmd.Flags().GetString("generator")
				includeGenerated, _ := cmd.Flags().GetBool("include-generated")
				if generator != "" {
					idx = idx.FilterByGenerator(generator)
					includeGenerated = true
				}

				var filePath string
				if len(args) > 0 {
					filePath = args[0]
				}
				selectors, err := bundleSelectors(filePath, bundleFocus, bundleSymbol)
				if err != nil {
					return err
				}

				opts := contextpack.BundleOptions{
					Mode:             contextbundle.TaskKind(bundleMode),
					Task:             bundleTask,
					Tokens:           tokens,
					Focus:            selectors,
					IncludeGenerated: includeGenerated,
				}
				result, err := contextpack.BuildBundle(context.Background(), idx, resolveContextRoot(rootPath), opts)
				if err != nil {
					return err
				}

				if bundleManifestPath != "" {
					if err := writeJSONFile(bundleManifestPath, result.Manifest); err != nil {
						return fmt.Errorf("writing manifest: %w", err)
					}
				}
				if bundleReceiptPath != "" {
					if err := writeJSONFile(bundleReceiptPath, result.Receipt); err != nil {
						return fmt.Errorf("writing receipt: %w", err)
					}
				}
				if jsonOutput {
					return emitJSON(struct {
						Receipt  contextbundle.Receipt  `json:"receipt"`
						Manifest contextbundle.Manifest `json:"manifest"`
					}{Receipt: result.Receipt, Manifest: result.Manifest})
				}
				fmt.Print(string(result.Content))
				return nil
			}

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

	cmd.Flags().StringVar(&bundleMode, "mode", "", "bundle mode: explore|implement|debug|review|test|commit|resume (enables bundle mode)")
	cmd.Flags().StringVar(&bundleTask, "task", "", "bundle mode: free-text description of the task (enables bundle mode)")
	cmd.Flags().StringVar(&bundleFocus, "focus", "", "bundle mode: path:line to focus on (enables bundle mode)")
	cmd.Flags().StringVar(&bundleSymbol, "symbol", "", "bundle mode: symbol name to focus on, combined with [file] or --focus (enables bundle mode)")
	cmd.Flags().StringVar(&bundleManifestPath, "manifest", "", "bundle mode: write the projection manifest as JSON to this path")
	cmd.Flags().StringVar(&bundleReceiptPath, "receipt", "", "bundle mode: write the context receipt as JSON to this path")
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

// wantsBundleMode reports whether any bundle-mode flag was set (spec
// 10.13). Leaving all of them unset keeps the legacy `canopy search
// context <file>` path byte-for-byte unchanged.
func wantsBundleMode(mode, task, focus, symbol string) bool {
	return mode != "" || task != "" || focus != "" || symbol != ""
}

// bundleSelectors builds the contextbundle.Selector list for --focus
// path:line and --symbol Name (spec 10.13). --symbol combines with the
// positional file argument, or with --focus's path when --focus is also
// given.
func bundleSelectors(filePath, focus, symbol string) ([]contextbundle.Selector, error) {
	var selectors []contextbundle.Selector

	focusPath := filePath
	focusLine := 0
	if focus != "" {
		idx := strings.LastIndex(focus, ":")
		if idx <= 0 || idx == len(focus)-1 {
			return nil, fmt.Errorf("--focus must be path:line, got %q", focus)
		}
		focusPath = focus[:idx]
		line, err := strconv.Atoi(focus[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("--focus line must be an integer, got %q", focus)
		}
		focusLine = line
	}

	if symbol != "" {
		if focusPath == "" {
			return nil, fmt.Errorf("--symbol requires a file argument or --focus path:line")
		}
		selectors = append(selectors, contextbundle.Selector{File: focusPath, Symbol: symbol, Required: true})
		return selectors, nil
	}

	if focus != "" {
		selectors = append(selectors, contextbundle.Selector{File: focusPath, Line: focusLine, Required: true})
	} else if filePath != "" {
		selectors = append(selectors, contextbundle.Selector{File: filePath, Required: true})
	}
	return selectors, nil
}

// resolveContextRoot normalizes the --root flag the same way loadOrBuild
// does, so the snapshot Canopy computes matches the index it just loaded.
func resolveContextRoot(rootPath string) string {
	if strings.TrimSpace(rootPath) == "" {
		return "."
	}
	return rootPath
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
