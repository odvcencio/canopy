package index

import (
	"context"
	"io/fs"
	"path/filepath"

	"m31labs.dev/canopy/pkg/model"
)

// SourceFile is an in-memory file to index without touching disk. It lets
// callers build a structural index from content that does not exist in the
// working tree, such as a file body read via `git show <ref>:<path>`.
type SourceFile struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
}

// BuildSources builds a structural index from in-memory file content instead
// of walking a directory tree. rootLabel identifies the snapshot the sources
// were drawn from (for example a git ref or "base"/"head") and is stored as
// the index Root; it is not resolved as a filesystem path.
//
// This lets callers index base-ref content obtained from `git show` without
// materializing a temporary worktree. Files whose extension has no
// registered parser are skipped, matching BuildPath's behavior. Files that
// fail to parse are recorded in the returned index's Errors.
func (b *Builder) BuildSources(ctx context.Context, rootLabel string, files []SourceFile) (*model.Index, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	filesByPath := make(map[string]model.FileSummary, len(files))
	errorsByPath := make(map[string]model.ParseError, len(files))

	for _, sf := range files {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return snapshotIndex(rootLabel, filesByPath, errorsByPath), ctxErr
		}

		relPath := filepath.ToSlash(sf.Path)
		if relPath == "" {
			continue
		}

		parser, ok := b.parserForPath(relPath)
		if !ok || !parserReadyForIndex(parser) {
			continue
		}

		summary, parseErr := parser.Parse(relPath, sf.Content)
		if parseErr != nil {
			errorsByPath[relPath] = model.ParseError{
				Path:  relPath,
				Error: parseErr.Error(),
			}
			continue
		}

		summary.Path = relPath
		summary.SizeBytes = int64(len(sf.Content))
		summary.Language = parser.Language()
		for i := range summary.Symbols {
			summary.Symbols[i].File = relPath
		}
		for i := range summary.References {
			summary.References[i].File = relPath
		}
		if b.detector != nil {
			summary.Generated = b.detector.Detect(relPath, sf.Content)
		}

		delete(errorsByPath, relPath)
		filesByPath[relPath] = summary
	}

	idx := snapshotIndex(rootLabel, filesByPath, errorsByPath)
	idx.ConfigHashes = b.configHashes
	return idx, nil
}
