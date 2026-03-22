package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	tsgrep "github.com/odvcencio/gotreesitter/grep"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gts-suite/pkg/query"
)

// grepMode indicates which engine to dispatch to.
type grepMode int

const (
	grepModeAuto       grepMode = iota
	grepModeStructural          // forced via --structural / -S
	grepModeSelector            // forced via --selector
)

// detectGrepMode inspects the pattern text and returns a best-guess mode.
//
//  1. If the pattern starts with "find " → structural (full query syntax).
//  2. If the pattern contains $ followed by a letter → structural (metavariable).
//  3. If the pattern matches word[ → selector DSL.
//  4. Otherwise → structural (try first, fall back to selector).
func detectGrepMode(pattern string) grepMode {
	trimmed := strings.TrimSpace(pattern)

	// Rule 1: explicit "find" keyword.
	if strings.HasPrefix(trimmed, "find ") || strings.HasPrefix(trimmed, "find\t") {
		return grepModeStructural
	}

	// Rule 2: metavariable ($NAME, $$$PARAMS, etc.)
	metavarRE := regexp.MustCompile(`\$[A-Za-z]`)
	if metavarRE.MatchString(trimmed) {
		return grepModeStructural
	}

	// Rule 3: selector DSL — word followed by [
	selectorRE := regexp.MustCompile(`^[a-z_][a-z0-9_]*\[`)
	if selectorRE.MatchString(trimmed) {
		return grepModeSelector
	}

	// Rule 3b: bare kind without brackets is also selector (e.g. "type_definition")
	bareKindRE := regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	if bareKindRE.MatchString(trimmed) {
		return grepModeSelector
	}

	// Rule 3c: wildcard selector
	if trimmed == "*" || strings.HasPrefix(trimmed, "*[") {
		return grepModeSelector
	}

	// Rule 4: ambiguous — prefer structural.
	return grepModeStructural
}

func newGrepCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var forceStructural bool
	var forceSelector bool
	var lang string
	var rewrite string
	var where string
	var limit int

	cmd := &cobra.Command{
		Use:     "grep <pattern> [path]",
		Aliases: []string{"gtsgrep"},
		Short:   "Structural grep — code patterns and selector DSL",
		Long: `Structural grep over source code using two complementary engines.

STRUCTURAL MODE (code patterns with metavariables):
  Patterns use $NAME metavariables that match AST nodes structurally.
  Metavariables: $NAME (single), $$$NAME (variadic), $_ (wildcard).
  Uses the gotreesitter structural grep engine.

SELECTOR MODE (indexed symbol queries):
  Patterns use the selector DSL: kind[filter1,filter2,...] against the
  structural index. Useful for kind-based queries without full parsing.

AUTO-DETECTION:
  The engine is chosen automatically based on the pattern syntax:
  - Starts with "find " or contains $+letter → structural
  - Matches word[ or bare tree-sitter kind      → selector
  - Otherwise                                    → structural

  Use --structural/-S or --selector to force a specific engine.`,
		Example: `  # Structural mode — find Go functions returning error
  gts grep 'func $NAME($$$) error' pkg/

  # Structural mode — with language prefix
  gts grep 'find go::func $NAME($$$) error' .

  # Structural mode — with where clause
  gts grep 'func $NAME($$$)' pkg/ --where 'matches($NAME, "^Test")'

  # Structural mode — rewrite
  gts grep '$ERR.Unwrap()' pkg/ --rewrite '$ERR.Error()'

  # Selector mode — find functions by name regex
  gts grep 'function_definition[name=/^Test/]' ./tests/

  # Selector mode — methods by receiver
  gts grep 'method_definition[receiver=/Server/]' internal/api/

  # Force a specific mode
  gts grep -S 'error' pkg/
  gts grep --selector 'type_definition' pkg/`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern := args[0]
			target := "."
			if len(args) == 2 {
				target = args[1]
			}

			// Determine mode.
			mode := grepModeAuto
			if forceStructural && forceSelector {
				return fmt.Errorf("cannot use both --structural and --selector")
			}
			if forceStructural {
				mode = grepModeStructural
			} else if forceSelector {
				mode = grepModeSelector
			} else {
				mode = detectGrepMode(pattern)
			}

			// Validate mode-specific flags.
			if mode == grepModeSelector {
				if rewrite != "" {
					return fmt.Errorf("--rewrite is only supported in structural mode (-S)")
				}
				if where != "" {
					return fmt.Errorf("--where is only supported in structural mode (-S)")
				}
				if lang != "" {
					return fmt.Errorf("--lang is only supported in structural mode (-S)")
				}
			}

			switch mode {
			case grepModeStructural:
				return runStructuralGrep(pattern, target, lang, where, rewrite, jsonOutput, countOnly, limit)
			case grepModeSelector:
				return runSelectorGrep(pattern, target, cachePath, noCache, jsonOutput, countOnly, limit)
			default:
				// Auto resolved to structural above; this shouldn't happen.
				return runStructuralGrep(pattern, target, lang, where, rewrite, jsonOutput, countOnly, limit)
			}
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing (selector mode)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of matches")
	cmd.Flags().BoolVarP(&forceStructural, "structural", "S", false, "force structural mode (code patterns)")
	cmd.Flags().BoolVar(&forceSelector, "selector", false, "force selector mode (indexed symbol queries)")
	cmd.Flags().StringVar(&lang, "lang", "", "language for structural grep (auto-detected from files if omitted)")
	cmd.Flags().StringVar(&rewrite, "rewrite", "", "replacement template for structural matches")
	cmd.Flags().StringVar(&where, "where", "", "where-clause constraint for structural matches")
	cmd.Flags().IntVar(&limit, "limit", 1000, "maximum number of results (0 for unlimited)")
	return cmd
}

// runSelectorGrep runs the original selector-DSL based grep against the structural index.
func runSelectorGrep(pattern, target, cachePath string, noCache, jsonOutput, countOnly bool, limit int) error {
	selector, err := query.ParseSelector(pattern)
	if err != nil {
		return err
	}

	idx, err := loadOrBuild(cachePath, target, noCache)
	if err != nil {
		return err
	}

	truncated := false
	matches := make([]grepMatch, 0, 256)
selectorOuter:
	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			if !selector.Match(symbol) {
				continue
			}
			matches = append(matches, grepMatch{
				File:      file.Path,
				Kind:      symbol.Kind,
				Name:      symbol.Name,
				Signature: symbol.Signature,
				StartLine: symbol.StartLine,
				EndLine:   symbol.EndLine,
			})
			if limit > 0 && len(matches) >= limit {
				truncated = true
				break selectorOuter
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			if matches[i].StartLine == matches[j].StartLine {
				return matches[i].Name < matches[j].Name
			}
			return matches[i].StartLine < matches[j].StartLine
		}
		return matches[i].File < matches[j].File
	})

	if jsonOutput {
		if countOnly {
			return emitJSON(struct {
				Mode      string `json:"mode"`
				Count     int    `json:"count"`
				Truncated bool   `json:"truncated,omitempty"`
			}{
				Mode:      "selector",
				Count:     len(matches),
				Truncated: truncated,
			})
		}
		return emitJSON(struct {
			Mode      string      `json:"mode"`
			Matches   []grepMatch `json:"matches"`
			Count     int         `json:"count"`
			Truncated bool        `json:"truncated,omitempty"`
		}{
			Mode:      "selector",
			Matches:   matches,
			Count:     len(matches),
			Truncated: truncated,
		})
	}

	if countOnly {
		fmt.Println(len(matches))
		if truncated {
			fmt.Printf("truncated: limit=%d\n", limit)
		}
		return nil
	}

	for _, match := range matches {
		if match.Signature != "" {
			fmt.Printf("%s:%d:%d %s %s\n", match.File, match.StartLine, match.EndLine, match.Kind, match.Signature)
			continue
		}
		fmt.Printf("%s:%d:%d %s %s\n", match.File, match.StartLine, match.EndLine, match.Kind, match.Name)
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "warning: results truncated at limit=%d, use --limit 0 for all\n", limit)
	}
	return nil
}

// runStructuralGrep runs the gotreesitter structural grep engine over a file tree.
func runStructuralGrep(pattern, target, langName, whereCl, rewriteTpl string, jsonOutput, countOnly bool, limit int) error {
	// Build the full query string for the gotreesitter grep engine.
	// If the pattern already starts with "find", use it directly (full query form).
	// Otherwise, construct the query from flags.
	fullQuery := buildStructuralQuery(pattern, langName, whereCl, rewriteTpl)

	// Parse the query to extract language info (if any) for validation.
	stmt, err := tsgrep.ParseQuery(fullQuery)
	if err != nil {
		return fmt.Errorf("structural grep: %w", err)
	}

	// Resolve absolute target path.
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Walk files and match.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := grammars.DefaultPolicy()
	ch, _ := grammars.WalkAndParse(ctx, absTarget, policy)

	truncated := false
	var matches []structuralGrepMatch
	var rewriteEdits []structuralRewriteResult

structuralOuter:
	for pf := range ch {
		if pf.Err != nil {
			pf.Close()
			continue
		}

		fileLang := pf.Lang
		if fileLang == nil {
			pf.Close()
			continue
		}

		// If the query specifies a language, skip files of other languages.
		if stmt.Lang != "" {
			queryLangEntry := grammars.DetectLanguageByName(stmt.Lang)
			if queryLangEntry != nil && queryLangEntry.Name != fileLang.Name {
				pf.Close()
				continue
			}
		}

		lang := fileLang.Language()
		if lang == nil {
			pf.Close()
			continue
		}

		// Compute relative path for display.
		relPath, relErr := filepath.Rel(absTarget, pf.Path)
		if relErr != nil {
			relPath = pf.Path
		}

		// Run the query against this file's source.
		qr, qerr := tsgrep.RunQueryWithLang(fullQuery, pf.Source, lang)
		if qerr != nil {
			// Pattern may not be valid for this language — skip silently.
			pf.Close()
			continue
		}

		// Collect matches.
		for _, result := range qr.Matches {
			startLine := byteOffsetToLine(pf.Source, result.StartByte)
			endLine := byteOffsetToLine(pf.Source, result.EndByte)

			matchText := ""
			if result.EndByte <= uint32(len(pf.Source)) {
				matchText = compactNodeText(string(pf.Source[result.StartByte:result.EndByte]))
			}

			caps := make(map[string]string, len(result.Captures))
			for name, cap := range result.Captures {
				caps[name] = string(cap.Text)
			}

			matches = append(matches, structuralGrepMatch{
				File:      relPath,
				StartLine: startLine,
				EndLine:   endLine,
				Text:      matchText,
				Captures:  caps,
			})
			if limit > 0 && len(matches) >= limit {
				truncated = true
				pf.Close()
				cancel()
				// Drain remaining channel items so the walker goroutine can exit.
				for remaining := range ch {
					remaining.Close()
				}
				break structuralOuter
			}
		}

		// Collect rewrite results if present.
		if qr.ReplaceResult != nil && len(qr.ReplaceResult.Edits) > 0 {
			rewriteEdits = append(rewriteEdits, structuralRewriteResult{
				File:  relPath,
				Edits: qr.ReplaceResult.Edits,
			})
		}

		pf.Close()
	}

	// Sort results.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			if matches[i].StartLine == matches[j].StartLine {
				return matches[i].Text < matches[j].Text
			}
			return matches[i].StartLine < matches[j].StartLine
		}
		return matches[i].File < matches[j].File
	})

	// Output.
	if jsonOutput {
		if countOnly {
			return emitJSON(struct {
				Mode      string `json:"mode"`
				Count     int    `json:"count"`
				Truncated bool   `json:"truncated,omitempty"`
			}{
				Mode:      "structural",
				Count:     len(matches),
				Truncated: truncated,
			})
		}
		return emitJSON(struct {
			Mode      string                    `json:"mode"`
			Matches   []structuralGrepMatch     `json:"matches"`
			Count     int                       `json:"count"`
			Truncated bool                      `json:"truncated,omitempty"`
			Edits     []structuralRewriteResult `json:"edits,omitempty"`
		}{
			Mode:      "structural",
			Matches:   matches,
			Count:     len(matches),
			Truncated: truncated,
			Edits:     rewriteEdits,
		})
	}

	if countOnly {
		fmt.Println(len(matches))
		if truncated {
			fmt.Printf("truncated: limit=%d\n", limit)
		}
		return nil
	}

	for _, m := range matches {
		fmt.Printf("%s:%d :: %s\n", m.File, m.StartLine, m.Text)
		if len(m.Captures) > 0 {
			// Sort capture names for deterministic output.
			names := make([]string, 0, len(m.Captures))
			for name := range m.Captures {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Printf("  $%s = %s\n", name, m.Captures[name])
			}
		}
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "warning: results truncated at limit=%d, use --limit 0 for all\n", limit)
	}

	if len(rewriteEdits) > 0 {
		fmt.Printf("\n--- rewrite edits ---\n")
		for _, rr := range rewriteEdits {
			for _, edit := range rr.Edits {
				fmt.Printf("%s: replace bytes [%d:%d] with %q\n",
					rr.File, edit.StartByte, edit.EndByte, string(edit.Replacement))
			}
		}
	}

	return nil
}

// structuralRewriteResult holds rewrite edits for a single file.
type structuralRewriteResult struct {
	File  string       `json:"file"`
	Edits []tsgrep.Edit `json:"edits"`
}

// buildStructuralQuery constructs a full query string from a pattern and optional flags.
func buildStructuralQuery(pattern, langName, whereCl, rewriteTpl string) string {
	trimmed := strings.TrimSpace(pattern)

	// If the pattern already starts with "find", use it as-is (full query form).
	// The user may have embedded where/replace blocks in the pattern itself.
	if strings.HasPrefix(trimmed, "find ") || strings.HasPrefix(trimmed, "find\t") {
		// Append --where and --rewrite flags if they aren't already in the query.
		result := trimmed
		if whereCl != "" && !strings.Contains(trimmed, " where ") && !strings.Contains(trimmed, " where{") {
			result += " where { " + whereCl + " }"
		}
		if rewriteTpl != "" && !strings.Contains(trimmed, " replace ") && !strings.Contains(trimmed, " replace{") {
			result += " replace { " + rewriteTpl + " }"
		}
		return result
	}

	// Build a bare pattern with optional lang prefix.
	var b strings.Builder
	if langName != "" {
		b.WriteString(langName)
		b.WriteString("::")
	}
	b.WriteString(trimmed)

	if whereCl != "" {
		b.WriteString(" where { ")
		b.WriteString(whereCl)
		b.WriteString(" }")
	}
	if rewriteTpl != "" {
		b.WriteString(" replace { ")
		b.WriteString(rewriteTpl)
		b.WriteString(" }")
	}

	return b.String()
}

// byteOffsetToLine converts a byte offset to a 1-based line number.
func byteOffsetToLine(source []byte, offset uint32) int {
	if offset > uint32(len(source)) {
		offset = uint32(len(source))
	}
	return bytes.Count(source[:offset], []byte("\n")) + 1
}

func runGrep(args []string) error {
	cmd := newGrepCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
