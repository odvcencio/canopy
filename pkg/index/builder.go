// Package index builds and caches structural indexes by walking source trees and parsing files with registered language parsers.
package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/gts-suite/pkg/generated"
	"github.com/odvcencio/gts-suite/pkg/ignore"
	"github.com/odvcencio/gts-suite/pkg/lang"
	"github.com/odvcencio/gts-suite/pkg/lang/treesitter"
	"github.com/odvcencio/gts-suite/pkg/model"
)

const schemaVersion = "0.2.0"

type Builder struct {
	parsers      map[string]lang.Parser
	ignore       *ignore.Matcher
	detector     *generated.Detector
	configHashes map[string]string
}

// SetConfigHashes stores pre-computed config file hashes to embed in built indexes.
func (b *Builder) SetConfigHashes(h map[string]string) { b.configHashes = h }

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
	// AllLanguages returns metadata without loading grammars. We register lazy
	// parsers that defer grammar loading and tags-query inference until the
	// first file of each language is actually parsed — avoiding the 2-4 GB
	// memory spike from decompressing all 200+ grammars at startup.
	entries := append([]grammars.LangEntry(nil), grammars.AllLanguages()...)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	for _, entry := range entries {
		// Skip entries that have no Language loader — they can never parse.
		if entry.Language == nil {
			continue
		}

		lp := newLazyParser(entry)
		for _, ext := range entry.Extensions {
			normalized := normalizeExtension(ext)
			if normalized == "" {
				continue
			}
			if _, exists := b.parsers[normalized]; exists {
				continue
			}
			b.parsers[normalized] = lp
		}
	}
}

// fallbackTagsQueries provides custom tags queries for languages where
// gotreesitter's inference produces nothing (typically RE-oriented grammars
// with non-standard node structures).
var fallbackTagsQueries = map[string]string{
	"llvm": "(fn_define (function_header name: (global_var) @name)) @definition.function\n" +
		"(declare (function_header name: (global_var) @name)) @definition.function\n" +
		"(_ callee: (value (var (global_var) @name))) @reference.call",
	"asm":         "(label (ident) @name) @definition.function",
	"disassembly": "(code_location (identifier) @name) @definition.function",
}

// lazyParser implements lang.Parser but defers grammar loading and tags-query
// inference until the first call to Parse. This avoids loading all 200+
// grammars at NewBuilder time (the root cause of OOM on large repos).
type lazyParser struct {
	entry  grammars.LangEntry
	parser *treesitter.Parser
	once   sync.Once
	err    error
}

func newLazyParser(entry grammars.LangEntry) *lazyParser {
	return &lazyParser{entry: entry}
}

func (lp *lazyParser) init() {
	// Infer the tags query on demand (loads the grammar for this one language).
	entry := lp.entry
	entry.TagsQuery = grammars.ResolveTagsQuery(entry)
	if strings.TrimSpace(entry.TagsQuery) == "" {
		entry.TagsQuery = fallbackTagsQueries[entry.Name]
	}
	if strings.TrimSpace(entry.TagsQuery) == "" {
		lp.err = fmt.Errorf("no tags query available for %q", entry.Name)
		return
	}
	lp.parser, lp.err = treesitter.NewParser(entry)
}

func (lp *lazyParser) Language() string {
	return lp.entry.Name
}

func (lp *lazyParser) TreesitterParser() (*treesitter.Parser, error) {
	lp.once.Do(lp.init)
	if lp.err != nil {
		return nil, lp.err
	}
	return lp.parser, nil
}

func (lp *lazyParser) Parse(path string, src []byte) (model.FileSummary, error) {
	lp.once.Do(lp.init)
	if lp.err != nil {
		return model.FileSummary{}, lp.err
	}
	return lp.parser.Parse(path, src)
}

// SetIgnore configures a .gtsignore-style matcher to skip paths during indexing.
func (b *Builder) SetIgnore(m *ignore.Matcher) {
	b.ignore = m
}

// Ignore returns the current ignore matcher, or nil if none is set.
func (b *Builder) Ignore() *ignore.Matcher {
	return b.ignore
}

// SetDetector configures a generated-file detector to tag files during indexing.
func (b *Builder) SetDetector(d *generated.Detector) {
	b.detector = d
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
	idx, _, err := b.BuildPathIncrementalWithOptions(context.Background(), path, nil, BuildOptions{})
	return idx, err
}

func (b *Builder) BuildPathIncremental(ctx context.Context, path string, previous *model.Index) (*model.Index, BuildStats, error) {
	return b.BuildPathIncrementalWithOptions(ctx, path, previous, BuildOptions{})
}

func (b *Builder) BuildPathIncrementalWithOptions(ctx context.Context, path string, previous *model.Index, opts BuildOptions) (*model.Index, BuildStats, error) {
	stats := BuildStats{}
	if ctx == nil {
		ctx = context.Background()
	}

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
		return b.buildSingleFileWithOptions(ctx, target, info, previous, opts)
	}

	root := filepath.Clean(target)

	previousByPath := previousFilesByPath(previous, root)
	filesByPath := make(map[string]model.FileSummary, len(previousByPath))
	errorsByPath := map[string]model.ParseError{}

	// Build the gateway policy.
	policy := grammars.DefaultPolicy()
	for dir := range DefaultSkipDirs() {
		policy.SkipDirs = append(policy.SkipDirs, dir)
	}
	// DefaultPolicy uses GOMAXPROCS for concurrency. Lazy grammar loading
	// (sync.Once per language) prevents the OOM spikes that originally
	// motivated serial parsing. GTS_MAX_CONCURRENT env var still overrides.
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

	// Skip tree-sitter parsing for large generated files. The gateway will
	// still read the file (Source populated, Tree nil) so the builder can
	// do fast regex-based symbol extraction instead of a full AST parse.
	if b.detector != nil {
		policy.SkipTreeParse = func(absPath string, size int64) bool {
			if size < generated.FastExtractThreshold {
				return false
			}
			relPath, relErr := filepath.Rel(root, absPath)
			if relErr != nil {
				return false
			}
			relPath = filepath.ToSlash(relPath)
			// Detect by filename only (nil source). Returns non-nil for
			// filename-pattern matches without needing file contents.
			info := b.detector.Detect(relPath, nil)
			return info != nil && info.Reason == "filename"
		}
	}

	// Wire ignore matcher's directory-level patterns into SkipDirs
	// is not possible generically, but the gateway already skips .git,
	// .graft, .hg, .svn, vendor, node_modules. The ShouldParse hook
	// above handles hidden dirs and ignore-matched files.

	// Collect reused files from the previous index that are still present
	// on disk and unchanged. We must also walk to discover them, but the
	// gateway's ShouldParse=false means they won't appear in the channel.
	// We pre-collect reused entries before the walk.
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
		filesByPath[relPath] = entry
		stats.CandidateFiles++
		stats.ReusedFiles++
		emitBuildEvent(opts, BuildEvent{
			Kind:    BuildEventReused,
			Path:    relPath,
			Summary: entry,
			Stats:   stats,
		})
	}

	results, statsFn := grammars.WalkAndParse(ctx, root, policy)
	for file := range results {
		relPath, relErr := filepath.Rel(root, file.Path)
		if relErr != nil {
			relPath = file.Path
		}
		relPath = filepath.ToSlash(relPath)

		stats.CandidateFiles++

		parser, ok := b.parserForPath(file.Path)
		if !ok {
			file.Close()
			continue
		}

		if file.Err != nil && !file.IsRead {
			parseErr := model.ParseError{
				Path:  relPath,
				Error: file.Err.Error(),
			}
			errorsByPath[relPath] = parseErr
			emitBuildEvent(opts, BuildEvent{
				Kind:       BuildEventError,
				Path:       relPath,
				ParseError: parseErr,
				Stats:      stats,
			})
			file.Close()
			continue
		}

		// Fast path: file was read but tree-sitter parse was skipped
		// (SkipTreeParse hook returned true). Use regex extraction.
		if file.Tree == nil && file.IsRead && file.Err == nil {
			summary := generated.FastExtractSymbols(relPath, file.Source, parser.Language())
			summary.Path = relPath
			summary.SizeBytes = file.Size
			summary.Language = parser.Language()
			if b.detector != nil {
				summary.Generated = b.detector.Detect(relPath, file.Source)
			}
			if fi, statErr := os.Stat(file.Path); statErr == nil {
				summary.ModTimeUnixNano = fi.ModTime().UnixNano()
			}
			for i := range summary.Symbols {
				summary.Symbols[i].File = relPath
			}
			file.Close()
			filesByPath[relPath] = summary
			stats.ParsedFiles++
			emitBuildEvent(opts, BuildEvent{
				Kind:    BuildEventParsed,
				Path:    relPath,
				Summary: summary,
				Stats:   stats,
			})
			continue
		}

		summary, parseErr := parseIndexedFile(parser, file.Path, file.Source, file.Tree)

		// Run generated-file detection before Close(), which nils Source.
		var genInfo *model.GeneratedInfo
		if b.detector != nil {
			genInfo = b.detector.Detect(relPath, file.Source)
		}
		file.Close()

		if parseErr != nil {
			parseFailure := model.ParseError{
				Path:  relPath,
				Error: parseErr.Error(),
			}
			errorsByPath[relPath] = parseFailure
			emitBuildEvent(opts, BuildEvent{
				Kind:       BuildEventError,
				Path:       relPath,
				ParseError: parseFailure,
				Stats:      stats,
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

		summary.Generated = genInfo

		delete(errorsByPath, relPath)
		filesByPath[relPath] = summary
		stats.ParsedFiles++
		emitBuildEvent(opts, BuildEvent{
			Kind:    BuildEventParsed,
			Path:    relPath,
			Summary: summary,
			Stats:   stats,
		})
	}
	_ = statsFn()

	if langCount := countDistinctLanguages(filesByPath); langCount > 20 {
		fmt.Fprintf(os.Stderr, "warning: %d distinct languages detected — this may cause high memory usage\n", langCount)
	}

	index := snapshotIndex(root, filesByPath, errorsByPath)
	index.ConfigHashes = b.configHashes
	if ctxErr := ctx.Err(); ctxErr != nil {
		return index, stats, ctxErr
	}
	return index, stats, nil
}

type indexTreesitterParser interface {
	TreesitterParser() (*treesitter.Parser, error)
}

func parseIndexedFile(parser lang.Parser, path string, source []byte, tree *gotreesitter.BoundTree) (model.FileSummary, error) {
	if tsParser, ok := parser.(*treesitter.Parser); ok && tree != nil {
		return tsParser.ParseBoundTree(path, tree)
	}
	if lazy, ok := parser.(indexTreesitterParser); ok && tree != nil {
		tsParser, err := lazy.TreesitterParser()
		if err != nil {
			return model.FileSummary{}, err
		}
		return tsParser.ParseBoundTree(path, tree)
	}
	return parser.Parse(path, source)
}

// buildSingleFile handles the single-file indexing path (when the target is
// a file rather than a directory).
func (b *Builder) buildSingleFileWithOptions(ctx context.Context, target string, info os.FileInfo, previous *model.Index, opts BuildOptions) (*model.Index, BuildStats, error) {
	stats := BuildStats{}
	root := filepath.Clean(filepath.Dir(target))
	filesByPath := map[string]model.FileSummary{}
	errorsByPath := map[string]model.ParseError{}

	parser, ok := b.parserForPath(target)
	if !ok {
		return snapshotIndex(root, filesByPath, errorsByPath), stats, nil
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
		filesByPath[relPath] = reused
		stats.ReusedFiles = 1
		emitBuildEvent(opts, BuildEvent{
			Kind:    BuildEventReused,
			Path:    relPath,
			Summary: reused,
			Stats:   stats,
		})
		return snapshotIndex(root, filesByPath, errorsByPath), stats, nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return snapshotIndex(root, filesByPath, errorsByPath), stats, ctxErr
	}

	source, readErr := os.ReadFile(target)
	if readErr != nil {
		parseErr := model.ParseError{
			Path:  relPath,
			Error: readErr.Error(),
		}
		errorsByPath[relPath] = parseErr
		emitBuildEvent(opts, BuildEvent{
			Kind:       BuildEventError,
			Path:       relPath,
			ParseError: parseErr,
			Stats:      stats,
		})
		return snapshotIndex(root, filesByPath, errorsByPath), stats, nil
	}

	summary, parseErr := parser.Parse(target, source)
	if parseErr != nil {
		parseFailure := model.ParseError{
			Path:  relPath,
			Error: parseErr.Error(),
		}
		errorsByPath[relPath] = parseFailure
		emitBuildEvent(opts, BuildEvent{
			Kind:       BuildEventError,
			Path:       relPath,
			ParseError: parseFailure,
			Stats:      stats,
		})
		return snapshotIndex(root, filesByPath, errorsByPath), stats, nil
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
	if b.detector != nil {
		summary.Generated = b.detector.Detect(relPath, source)
	}
	filesByPath[relPath] = summary
	stats.ParsedFiles = 1
	emitBuildEvent(opts, BuildEvent{
		Kind:    BuildEventParsed,
		Path:    relPath,
		Summary: summary,
		Stats:   stats,
	})

	index := snapshotIndex(root, filesByPath, errorsByPath)
	index.ConfigHashes = b.configHashes
	if ctxErr := ctx.Err(); ctxErr != nil {
		return index, stats, ctxErr
	}
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

func countDistinctLanguages(files map[string]model.FileSummary) int {
	langs := make(map[string]bool)
	for _, f := range files {
		if f.Language != "" {
			langs[f.Language] = true
		}
	}
	return len(langs)
}
