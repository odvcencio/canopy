// Package proxyfeed implements a feed that harvests type information
// from backend LSP responses and enriches the scope graph.
package proxyfeed

import (
	"encoding/json"
	"strings"

	"github.com/odvcencio/canopy/pkg/feeds"
	"github.com/odvcencio/canopy/pkg/scope"
)

// Feed implements FeedProvider for proxy response harvesting.
// Feed() is a no-op — enrichment happens via Enrich() called
// directly by the proxy manager after backend responses.
type Feed struct{}

// New creates a new proxy feed.
func New() *Feed { return &Feed{} }

func (f *Feed) Name() string              { return "proxy" }
func (f *Feed) Supports(lang string) bool { return true }
func (f *Feed) Priority() int             { return 70 }

// Feed is a no-op. Enrichment is response-driven via Enrich().
func (f *Feed) Feed(graph *scope.Graph, file string, src []byte, ctx *feeds.FeedContext) error {
	return nil
}

// HoverResponse is the minimal structure of an LSP hover response.
type HoverResponse struct {
	Contents struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"contents"`
}

// CompletionItem is a minimal LSP completion item.
type CompletionItem struct {
	Label  string `json:"label"`
	Detail string `json:"detail"` // type signature
	Kind   int    `json:"kind"`
}

// EnrichFromHover extracts type info from a backend hover response
// and stores it on the definition at the given line.
func (f *Feed) EnrichFromHover(graph *scope.Graph, file string, line int, response json.RawMessage) {
	fs := graph.FileScope(file)
	if fs == nil {
		return
	}

	var hover HoverResponse
	if err := json.Unmarshal(response, &hover); err != nil || hover.Contents.Value == "" {
		return
	}

	// Find definition at this line
	for i := range fs.Defs {
		def := &fs.Defs[i]
		if line >= def.Loc.StartLine && line <= def.Loc.EndLine {
			scope.SetMeta(def, "proxy.type", extractType(hover.Contents.Value))
			scope.SetMeta(def, "proxy.documentation", hover.Contents.Value)
			return
		}
	}
}

// EnrichFromCompletion extracts type details from completion items
// and stores them on matching definitions.
func (f *Feed) EnrichFromCompletion(graph *scope.Graph, file string, items []CompletionItem) {
	fs := graph.FileScope(file)
	if fs == nil {
		return
	}

	// Build a name→detail map from completion items
	details := make(map[string]string)
	for _, item := range items {
		if item.Detail != "" {
			details[item.Label] = item.Detail
		}
	}

	// Match to definitions by name
	for i := range fs.Defs {
		def := &fs.Defs[i]
		if detail, ok := details[def.Name]; ok {
			scope.SetMeta(def, "proxy.type", detail)
		}
	}
}

// extractType attempts to extract a type name from hover markdown.
// Backend hovers often contain code blocks like: ```go\nfunc Foo() error\n```
func extractType(markdown string) string {
	// Look for code blocks
	if idx := strings.Index(markdown, "```"); idx >= 0 {
		rest := markdown[idx+3:]
		// Skip language identifier
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	// Fallback: return first non-empty line
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "---") {
			return line
		}
	}
	return markdown
}
