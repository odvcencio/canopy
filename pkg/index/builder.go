// Package index builds and caches structural indexes by walking source trees and parsing files with registered language parsers.
package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/gts-suite/pkg/ignore"
	"github.com/odvcencio/gts-suite/pkg/lang"
	"github.com/odvcencio/gts-suite/pkg/lang/treesitter"
	"github.com/odvcencio/gts-suite/pkg/model"
)

const schemaVersion = "0.1.0"

type Builder struct {
	parsers map[string]lang.Parser
	ignore  *ignore.Matcher
}

type BuildStats struct {
	CandidateFiles int `json:"candidate_files"`
	ParsedFiles    int `json:"parsed_files"`
	ReusedFiles    int `json:"reused_files"`
}

func NewBuilder() *Builder {
	builder := &Builder{
		parsers: make(map[string]lang.Parser),
	}
	builder.registerTreesitterParsers()
	return builder
}

func (b *Builder) registerTreesitterParsers() {
	entries := append([]grammars.LangEntry(nil), grammars.AllLanguages()...)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	for _, entry := range entries {
		if strings.TrimSpace(entry.TagsQuery) == "" {
			continue
		}

		parser, err := treesitter.NewParser(entry)
		if err != nil {
			continue
		}

		for _, ext := range entry.Extensions {
			normalized := normalizeExtension(ext)
			if normalized == "" {
				continue
			}
			if _, exists := b.parsers[normalized]; exists {
				continue
			}
			b.parsers[normalized] = parser
		}
	}
}

// SetIgnore configures a .gtsignore-style matcher to skip paths during indexing.
func (b *Builder) SetIgnore(m *ignore.Matcher) {
	b.ignore = m
}

// Ignore returns the current ignore matcher, or nil if none is set.
func (b *Builder) Ignore() *ignore.Matcher {
	return b.ignore
}

func (b *Builder) Register(extension string, parser lang.Parser) {
	if parser == nil {
		return
	}
	normalized := normalizeExtension(extension)
	if normalized == "" {
		return
	}
	b.parsers[normalized] = parser
}

func normalizeExtension(extension string) string {
	normalized := strings.ToLower(strings.TrimSpace(extension))
	if normalized == "" {
		return ""
	}
	if normalized[0] != '.' {
		normalized = "." + normalized
	}
	return normalized
}

func (b *Builder) BuildPath(path string) (*model.Index, error) {
	idx, _, err := b.BuildPathIncremental(context.Background(), path, nil)
	return idx, err
}

func (b *Builder) BuildPathIncremental(ctx context.Context, path string, previous *model.Index) (*model.Index, BuildStats, error) {
	stats := BuildStats{}

	if strings.TrimSpace(path) == "" {
		path = "."
	}

	target, err := filepath.Abs(path)
	if err != nil {
		return nil, stats, err
	}
	target = filepath.Clean(target)

	info, err := os.Stat(target)
	if err != nil {
		return nil, stats, err
	}

	// Single-file mode: parse one file directly without the gateway walk.
	if !info.IsDir() {
		return b.buildSingleFile(target, info, previous)
	}

	root := filepath.Clean(target)

	index := &model.Index{
		Version:     schemaVersion,
		Root:        root,
		GeneratedAt: time.Now().UTC(),
	}

	previousByPath := previousFilesByPath(previous, root)

	// Build the gateway policy.
	policy := grammars.DefaultPolicy()
	policy.ShouldParse = func(absPath string, size int64, modTime time.Time) bool {
		// Skip files inside hidden directories (dot-prefixed), matching
		// the old collectCandidates behaviour.
		relPath, relErr := filepath.Rel(root, absPath)
		if relErr != nil {
			return false
		}
		relPath = filepath.ToSlash(relPath)
		for _, seg := range strings.Split(relPath, "/") {
			if strings.HasPrefix(seg, ".") && seg != "." {
				return false
			}
		}

		// Skip files matching ignore patterns.
		if b.ignore != nil {
			if b.ignore.Match(relPath, false) {
				return false
			}
		}

		// Skip files we have no parser for.
		if _, ok := b.parserForPath(absPath); !ok {
			return false
		}

		// Incremental reuse: skip files that haven't changed.
		if prev, ok := previousByPath[relPath]; ok {
			parser, _ := b.parserForPath(absPath)
			lang := ""
			if parser != nil {
				lang = parser.Language()
			}
			if canReuseSummary(prev, size, modTime.UnixNano(), lang) {
				return false
			}
		}
		return true
	}

	// Wire ignore matcher's directory-level patterns into SkipDirs
	// is not possible generically, but the gateway already skips .git,
	// .graft, .hg, .svn, vendor, node_modules. The ShouldParse hook
	// above handles hidden dirs and ignore-matched files.

	// Collect reused files from the previous index that are still present
	// on disk and unchanged. We must also walk to discover them, but the
	// gateway's ShouldParse=false means they won't appear in the channel.
	// We pre-collect reused entries before the walk.
	type reusedEntry struct {
		relPath string
		summary model.FileSummary
	}
	var reused []reusedEntry
	for relPath, prev := range previousByPath {
		absPath := filepath.Join(root, filepath.FromSlash(relPath))
		fi, statErr := os.Stat(absPath)
		if statErr != nil {
			// File removed or inaccessible — don't reuse.
			continue
		}
		parser, ok := b.parserForPath(absPath)
		if !ok {
			continue
		}
		if !canReuseSummary(prev, fi.Size(), fi.ModTime().UnixNano(), parser.Language()) {
			continue
		}
		// Check hidden dir and ignore filters for the reused path too.
		skip := false
		for _, seg := range strings.Split(relPath, "/") {
			if strings.HasPrefix(seg, ".") && seg != "." {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if b.ignore != nil && b.ignore.Match(relPath, false) {
			continue
		}
		entry := prev
		entry.Path = relPath
		entry.Language = parser.Language()
		entry.SizeBytes = fi.Size()
		entry.ModTimeUnixNano = fi.ModTime().UnixNano()
		for i := range entry.Symbols {
			entry.Symbols[i].File = relPath
		}
		for i := range entry.References {
			entry.References[i].File = relPath
		}
		reused = append(reused, reusedEntry{relPath: relPath, summary: entry})
		stats.ReusedFiles++
	}

	// Stream parsed files from the gateway.
	type parsedEntry struct {
		relPath string
		summary model.FileSummary
		err     error
	}
	var parsed []parsedEntry

	results, _ := grammars.WalkAndParse(ctx, root, policy)
	for file := range results {
		relPath, relErr := filepath.Rel(root, file.Path)
		if relErr != nil {
			relPath = file.Path
		}
		relPath = filepath.ToSlash(relPath)

		stats.CandidateFiles++

		if file.Err != nil {
			index.Errors = append(index.Errors, model.ParseError{
				Path:  relPath,
				Error: file.Err.Error(),
			})
			file.Close()
			continue
		}

		// Use the builder's parser to extract the summary from the
		// gateway-provided source bytes (option 3: re-parse via the
		// lang.Parser interface but skip the file read).
		parser, ok := b.parserForPath(file.Path)
		if !ok {
			file.Close()
			continue
		}

		summary, parseErr := parser.Parse(file.Path, file.Source)
		file.Close()

		if parseErr != nil {
			parsed = append(parsed, parsedEntry{
				relPath: relPath,
				err:     parseErr,
			})
			continue
		}

		summary.Path = relPath
		summary.SizeBytes = file.Size
		summary.ModTimeUnixNano = 0 // filled below from stat
		summary.Language = parser.Language()

		// Get mod time from disk for the summary.
		if fi, statErr := os.Stat(file.Path); statErr == nil {
			summary.ModTimeUnixNano = fi.ModTime().UnixNano()
		}

		for i := range summary.Symbols {
			summary.Symbols[i].File = relPath
		}
		for i := range summary.References {
			summary.References[i].File = relPath
		}

		parsed = append(parsed, parsedEntry{relPath: relPath, summary: summary})
		stats.ParsedFiles++
	}

	// Handle parse errors from the parsed entries.
	for _, p := range parsed {
		if p.err != nil {
			index.Errors = append(index.Errors, model.ParseError{
				Path:  p.relPath,
				Error: p.err.Error(),
			})
		}
	}

	// Merge reused and freshly parsed files, sorted by path.
	filesByPath := make(map[string]model.FileSummary, len(reused)+len(parsed))
	for _, r := range reused {
		filesByPath[r.relPath] = r.summary
	}
	for _, p := range parsed {
		if p.err == nil {
			filesByPath[p.relPath] = p.summary
		}
	}

	// Also count reused files as candidates.
	stats.CandidateFiles += stats.ReusedFiles

	paths := make([]string, 0, len(filesByPath))
	for p := range filesByPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	index.Files = make([]model.FileSummary, 0, len(paths))
	for _, p := range paths {
		index.Files = append(index.Files, filesByPath[p])
	}

	return index, stats, nil
}

// buildSingleFile handles the single-file indexing path (when the target is
// a file rather than a directory).
func (b *Builder) buildSingleFile(target string, info os.FileInfo, previous *model.Index) (*model.Index, BuildStats, error) {
	stats := BuildStats{}
	root := filepath.Clean(filepath.Dir(target))

	index := &model.Index{
		Version:     schemaVersion,
		Root:        root,
		GeneratedAt: time.Now().UTC(),
	}

	parser, ok := b.parserForPath(target)
	if !ok {
		return index, stats, nil
	}

	relPath, relErr := filepath.Rel(root, target)
	if relErr != nil {
		relPath = filepath.Base(target)
	}
	relPath = filepath.ToSlash(relPath)

	stats.CandidateFiles = 1

	previousByPath := previousFilesByPath(previous, root)
	if prev, ok := previousByPath[relPath]; ok && canReuseSummary(prev, info.Size(), info.ModTime().UnixNano(), parser.Language()) {
		reused := prev
		reused.Path = relPath
		reused.Language = parser.Language()
		reused.SizeBytes = info.Size()
		reused.ModTimeUnixNano = info.ModTime().UnixNano()
		for i := range reused.Symbols {
			reused.Symbols[i].File = relPath
		}
		for i := range reused.References {
			reused.References[i].File = relPath
		}
		index.Files = append(index.Files, reused)
		stats.ReusedFiles = 1
		return index, stats, nil
	}

	source, readErr := os.ReadFile(target)
	if readErr != nil {
		index.Errors = append(index.Errors, model.ParseError{
			Path:  relPath,
			Error: readErr.Error(),
		})
		return index, stats, nil
	}

	summary, parseErr := parser.Parse(target, source)
	if parseErr != nil {
		index.Errors = append(index.Errors, model.ParseError{
			Path:  relPath,
			Error: parseErr.Error(),
		})
		return index, stats, nil
	}

	summary.Path = relPath
	summary.SizeBytes = info.Size()
	summary.ModTimeUnixNano = info.ModTime().UnixNano()
	summary.Language = parser.Language()
	for i := range summary.Symbols {
		summary.Symbols[i].File = relPath
	}
	for i := range summary.References {
		summary.References[i].File = relPath
	}
	index.Files = append(index.Files, summary)
	stats.ParsedFiles = 1

	return index, stats, nil
}

func previousFilesByPath(previous *model.Index, root string) map[string]model.FileSummary {
	reused := map[string]model.FileSummary{}
	if previous == nil {
		return reused
	}

	previousRoot := filepath.Clean(previous.Root)
	if previousRoot != root {
		return reused
	}

	for _, file := range previous.Files {
		reused[file.Path] = file
	}
	return reused
}

func canReuseSummary(summary model.FileSummary, sizeBytes int64, modTimeUnixNano int64, language string) bool {
	if summary.Language != language {
		return false
	}
	if summary.SizeBytes != sizeBytes {
		return false
	}
	if summary.ModTimeUnixNano != modTimeUnixNano {
		return false
	}
	return true
}

func (b *Builder) parserForPath(path string) (lang.Parser, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	parser, ok := b.parsers[ext]
	return parser, ok
}

func (b *Builder) ParserForPath(path string) (lang.Parser, bool) {
	return b.parserForPath(path)
}
