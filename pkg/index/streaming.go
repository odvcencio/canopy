package index

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/odvcencio/canopy/pkg/model"
)

type BuildEventKind string

const (
	BuildEventReused BuildEventKind = "reused"
	BuildEventParsed BuildEventKind = "parsed"
	BuildEventError  BuildEventKind = "error"
)

type BuildEvent struct {
	Kind       BuildEventKind
	Path       string
	Summary    model.FileSummary
	ParseError model.ParseError
	Stats      BuildStats
}

type BuildOptions struct {
	Observer func(BuildEvent)
}

func emitBuildEvent(opts BuildOptions, event BuildEvent) {
	if opts.Observer != nil {
		opts.Observer(event)
	}
}

type PartialIndex struct {
	root         string
	filesByPath  map[string]model.FileSummary
	errorsByPath map[string]model.ParseError
}

func NewPartialIndex(root string, base *model.Index) *PartialIndex {
	partial := &PartialIndex{
		root:         filepath.Clean(root),
		filesByPath:  map[string]model.FileSummary{},
		errorsByPath: map[string]model.ParseError{},
	}

	if base == nil || filepath.Clean(base.Root) != partial.root {
		return partial
	}

	for _, file := range base.Files {
		partial.filesByPath[file.Path] = cloneFileSummary(file)
	}
	for _, parseErr := range base.Errors {
		partial.errorsByPath[parseErr.Path] = parseErr
	}

	return partial
}

func (p *PartialIndex) Apply(event BuildEvent) {
	if p == nil {
		return
	}

	switch event.Kind {
	case BuildEventReused, BuildEventParsed:
		path := event.Path
		if path == "" {
			path = event.Summary.Path
		}
		if path == "" {
			return
		}

		summary := cloneFileSummary(event.Summary)
		summary.Path = path
		delete(p.errorsByPath, path)
		p.filesByPath[path] = summary
	case BuildEventError:
		path := event.Path
		if path == "" {
			path = event.ParseError.Path
		}
		if path == "" {
			return
		}

		parseErr := event.ParseError
		parseErr.Path = path
		delete(p.filesByPath, path)
		p.errorsByPath[path] = parseErr
	}
}

func (p *PartialIndex) Snapshot() *model.Index {
	if p == nil {
		return nil
	}
	return snapshotIndex(p.root, p.filesByPath, p.errorsByPath)
}

func snapshotIndex(root string, filesByPath map[string]model.FileSummary, errorsByPath map[string]model.ParseError) *model.Index {
	idx := &model.Index{
		Version:     schemaVersion,
		Root:        filepath.Clean(root),
		GeneratedAt: time.Now().UTC(),
	}

	paths := make([]string, 0, len(filesByPath))
	for path := range filesByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	idx.Files = make([]model.FileSummary, 0, len(paths))
	for _, path := range paths {
		idx.Files = append(idx.Files, cloneFileSummary(filesByPath[path]))
	}

	errorPaths := make([]string, 0, len(errorsByPath))
	for path := range errorsByPath {
		errorPaths = append(errorPaths, path)
	}
	sort.Strings(errorPaths)

	idx.Errors = make([]model.ParseError, 0, len(errorPaths))
	for _, path := range errorPaths {
		idx.Errors = append(idx.Errors, errorsByPath[path])
	}

	return idx
}

func cloneFileSummary(summary model.FileSummary) model.FileSummary {
	cloned := summary
	cloned.Imports = append([]string(nil), summary.Imports...)
	cloned.Symbols = append([]model.Symbol(nil), summary.Symbols...)
	cloned.References = append([]model.Reference(nil), summary.References...)
	if summary.Generated != nil {
		g := *summary.Generated
		cloned.Generated = &g
	}
	return cloned
}
