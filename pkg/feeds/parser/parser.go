// Package parser implements the parser feed, which builds scope graphs
// from source files using gotreesitter and per-language scope rules.
package parser

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gts-suite/pkg/feeds"
	"github.com/odvcencio/gts-suite/pkg/scope"
)

// Feed implements FeedProvider by parsing source files with gotreesitter
// and building scope graphs using per-language .scm rules.
type Feed struct{}

// New creates a new parser feed.
func New() *Feed {
	return &Feed{}
}

func (f *Feed) Name() string              { return "parser" }
func (f *Feed) Supports(lang string) bool { return true }
func (f *Feed) Priority() int             { return 0 }

func (f *Feed) Feed(graph *scope.Graph, file string, src []byte, ctx *feeds.FeedContext) error {
	entry := grammars.DetectLanguage(file)
	if entry == nil {
		return nil
	}

	lang := entry.Language()
	rules, err := scope.LoadRules(entry.Name, lang)
	if err != nil {
		return nil
	}

	parser := gotreesitter.NewParser(lang)
	var tree *gotreesitter.Tree
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(src, lang)
		tree, err = parser.ParseWithTokenSource(src, ts)
	} else {
		tree, err = parser.Parse(src)
	}
	if err != nil {
		return err
	}

	fileScope := scope.BuildFileScope(tree, lang, src, rules, file)
	graph.AddFileScope(file, fileScope)
	return nil
}
