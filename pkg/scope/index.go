package scope

import (
	"os"
	"path/filepath"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/canopy/pkg/model"
)

// BuildFromIndex constructs a scope graph for all files in an index.
func BuildFromIndex(idx *model.Index, rootPath string) (*Graph, error) {
	graph := NewGraph()

	for _, f := range idx.Files {
		entry := grammars.DetectLanguage(f.Path)
		if entry == nil {
			continue
		}
		lang := entry.Language()
		rules, err := LoadRules(entry.Name, lang)
		if err != nil {
			// No scope rules for this language — skip
			continue
		}

		absPath := filepath.Join(rootPath, f.Path)
		src, err := os.ReadFile(absPath)
		if err != nil {
			continue
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
			continue
		}

		fileScope := BuildFileScope(tree, lang, src, rules, f.Path)
		graph.AddFileScope(f.Path, fileScope)
	}

	// Build package scopes: group files by directory, aggregate definitions
	pkgFiles := make(map[string][]string)
	for path := range graph.FileScopes {
		dir := filepath.Dir(path)
		if dir == "" || dir == "." {
			dir = "."
		}
		pkgFiles[dir] = append(pkgFiles[dir], path)
	}
	for dir, files := range pkgFiles {
		pkgScope := NewScope(ScopePackage, nil)
		for _, file := range files {
			fs := graph.FileScopes[file]
			for _, d := range fs.Defs {
				pkgScope.AddDef(d)
			}
			fs.Parent = pkgScope
			pkgScope.Children = append(pkgScope.Children, fs)
		}
		graph.AddPackageScope(dir, pkgScope)
	}

	// Resolve all references within each file scope (with graph for cross-file)
	for _, fs := range graph.FileScopes {
		ResolveAllGraph(fs, graph)
	}

	return graph, nil
}
