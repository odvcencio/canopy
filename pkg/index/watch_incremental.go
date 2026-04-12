package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/gotreesitter"

	"github.com/odvcencio/canopy/pkg/lang"
	"github.com/odvcencio/canopy/pkg/lang/treesitter"
	"github.com/odvcencio/canopy/pkg/model"
)

type WatchUpdateOptions struct {
	SubfileIncremental bool
}

type WatchState struct {
	trees map[string]watchTreeState
}

type watchTreeState struct {
	Source   []byte
	Tree     *gotreesitter.Tree
	Language string
}

func NewWatchState() *WatchState {
	return &WatchState{
		trees: make(map[string]watchTreeState),
	}
}

func (s *WatchState) Release() {
	if s == nil {
		return
	}
	for path := range s.trees {
		s.drop(path)
	}
}

func (s *WatchState) Clear() {
	s.Release()
}

func (s *WatchState) drop(path string) {
	if s == nil {
		return
	}
	current, ok := s.trees[path]
	if !ok {
		return
	}
	if current.Tree != nil {
		current.Tree.Release()
	}
	delete(s.trees, path)
}

func (s *WatchState) put(path string, next watchTreeState) {
	if s == nil {
		if next.Tree != nil {
			next.Tree.Release()
		}
		return
	}
	if current, ok := s.trees[path]; ok {
		if current.Tree != nil && current.Tree != next.Tree {
			current.Tree.Release()
		}
	}
	s.trees[path] = next
}

func (s *WatchState) get(path string) (watchTreeState, bool) {
	if s == nil {
		return watchTreeState{}, false
	}
	value, ok := s.trees[path]
	return value, ok
}

func (b *Builder) ApplyWatchChanges(current *model.Index, changedAbsPaths []string, state *WatchState, opts WatchUpdateOptions) (*model.Index, BuildStats, error) {
	stats := BuildStats{}
	if current == nil {
		return b.BuildPathIncremental(context.Background(), ".", nil)
	}
	if len(changedAbsPaths) == 0 {
		next, nextStats, err := b.BuildPathIncremental(context.Background(), current.Root, current)
		if err != nil {
			return nil, stats, err
		}
		if state != nil {
			state.Clear()
		}
		return next, nextStats, nil
	}

	root := filepath.Clean(current.Root)
	changedRel := normalizeChangedPaths(root, changedAbsPaths)
	if len(changedRel) == 0 {
		return current, stats, nil
	}

	stats.CandidateFiles = len(changedRel)

	filesByPath := make(map[string]model.FileSummary, len(current.Files))
	for _, file := range current.Files {
		filesByPath[file.Path] = file
	}
	errorsByPath := make(map[string]model.ParseError, len(current.Errors))
	for _, parseErr := range current.Errors {
		errorsByPath[parseErr.Path] = parseErr
	}

	changed := make([]string, 0, len(changedRel))
	for relPath := range changedRel {
		changed = append(changed, relPath)
	}
	sort.Strings(changed)

	for _, relPath := range changed {
		absPath := filepath.Join(root, filepath.FromSlash(relPath))
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				delete(filesByPath, relPath)
				delete(errorsByPath, relPath)
				if state != nil {
					state.drop(relPath)
				}
				continue
			}
			return nil, stats, err
		}
		if info.IsDir() {
			continue
		}

		parser, ok := b.ParserForPath(absPath)
		if !ok {
			delete(filesByPath, relPath)
			delete(errorsByPath, relPath)
			if state != nil {
				state.drop(relPath)
			}
			continue
		}

		source, readErr := os.ReadFile(absPath)
		if readErr != nil {
			delete(filesByPath, relPath)
			errorsByPath[relPath] = model.ParseError{
				Path:  relPath,
				Error: readErr.Error(),
			}
			if state != nil {
				state.drop(relPath)
			}
			continue
		}

		summary, parseErr := parseWatchFile(relPath, absPath, source, info, parser, state, opts.SubfileIncremental)
		if parseErr != nil {
			delete(filesByPath, relPath)
			errorsByPath[relPath] = model.ParseError{
				Path:  relPath,
				Error: parseErr.Error(),
			}
			if state != nil {
				state.drop(relPath)
			}
			continue
		}

		delete(errorsByPath, relPath)
		filesByPath[relPath] = summary
		stats.ParsedFiles++
	}

	next := &model.Index{
		Version:     schemaVersion,
		Root:        root,
		GeneratedAt: time.Now().UTC(),
	}

	paths := make([]string, 0, len(filesByPath))
	for relPath := range filesByPath {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	for _, relPath := range paths {
		next.Files = append(next.Files, filesByPath[relPath])
		if !changedRel[relPath] {
			stats.ReusedFiles++
		}
	}

	errorPaths := make([]string, 0, len(errorsByPath))
	for relPath := range errorsByPath {
		errorPaths = append(errorPaths, relPath)
	}
	sort.Strings(errorPaths)
	for _, relPath := range errorPaths {
		next.Errors = append(next.Errors, errorsByPath[relPath])
	}

	return next, stats, nil
}

type watchTreesitterParser interface {
	TreesitterParser() (*treesitter.Parser, error)
}

func parseWatchFile(relPath, absPath string, source []byte, info os.FileInfo, parser lang.Parser, state *WatchState, subfileIncremental bool) (model.FileSummary, error) {
	if tsParser, ok, err := resolveWatchTreesitterParser(parser); err != nil {
		return model.FileSummary{}, err
	} else if ok {
		var (
			fileSummary model.FileSummary
			tree        *gotreesitter.Tree
			err         error
		)

		if subfileIncremental {
			if previous, hasPrevious := state.get(relPath); hasPrevious && previous.Tree != nil && previous.Language == parser.Language() {
				fileSummary, tree, err = tsParser.ParseIncrementalWithTree(absPath, source, previous.Source, previous.Tree)
				if err != nil {
					return model.FileSummary{}, err
				}
			}
		}

		if tree == nil {
			fileSummary, tree, err = tsParser.ParseWithTree(absPath, source)
			if err != nil {
				return model.FileSummary{}, err
			}
		}

		fileSummary.Path = relPath
		fileSummary.Language = parser.Language()
		fileSummary.SizeBytes = info.Size()
		fileSummary.ModTimeUnixNano = info.ModTime().UnixNano()
		for i := range fileSummary.Symbols {
			fileSummary.Symbols[i].File = relPath
		}
		for i := range fileSummary.References {
			fileSummary.References[i].File = relPath
		}

		state.put(relPath, watchTreeState{
			Source:   append([]byte(nil), source...),
			Tree:     tree,
			Language: parser.Language(),
		})

		return fileSummary, nil
	}

	fileSummary, err := parser.Parse(absPath, source)
	if err != nil {
		return model.FileSummary{}, err
	}
	fileSummary.Path = relPath
	fileSummary.Language = parser.Language()
	fileSummary.SizeBytes = info.Size()
	fileSummary.ModTimeUnixNano = info.ModTime().UnixNano()
	for i := range fileSummary.Symbols {
		fileSummary.Symbols[i].File = relPath
	}
	for i := range fileSummary.References {
		fileSummary.References[i].File = relPath
	}
	if state != nil {
		state.drop(relPath)
	}
	return fileSummary, nil
}

func resolveWatchTreesitterParser(parser lang.Parser) (*treesitter.Parser, bool, error) {
	if tsParser, ok := parser.(*treesitter.Parser); ok {
		return tsParser, true, nil
	}
	if lazy, ok := parser.(watchTreesitterParser); ok {
		tsParser, err := lazy.TreesitterParser()
		if err != nil {
			return nil, true, err
		}
		return tsParser, true, nil
	}
	return nil, false, nil
}

func normalizeChangedPaths(root string, changedAbsPaths []string) map[string]bool {
	normalized := map[string]bool{}
	root = filepath.Clean(root)
	for _, rawPath := range changedAbsPaths {
		text := strings.TrimSpace(rawPath)
		if text == "" {
			continue
		}
		absPath := filepath.Clean(text)
		if !filepath.IsAbs(absPath) {
			if resolved, err := filepath.Abs(absPath); err == nil {
				absPath = filepath.Clean(resolved)
			}
		}

		relPath, err := filepath.Rel(root, absPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			continue
		}
		relPath = filepath.ToSlash(filepath.Clean(relPath))
		if relPath == "." || relPath == "" {
			continue
		}
		normalized[relPath] = true
	}
	return normalized
}
