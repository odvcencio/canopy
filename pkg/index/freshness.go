package index

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"m31labs.dev/canopy/pkg/model"
)

// FreshnessReport describes source changes against a cached index.
type FreshnessReport struct {
	RootMismatch bool     `json:"root_mismatch,omitempty"`
	StaleFiles   []string `json:"stale_files,omitempty"`
	MissingFiles []string `json:"missing_files,omitempty"`
	NewFiles     []string `json:"new_files,omitempty"`
}

// IsFresh reports whether the cache still describes the current source tree.
func (r FreshnessReport) IsFresh() bool {
	return !r.RootMismatch &&
		len(r.StaleFiles) == 0 &&
		len(r.MissingFiles) == 0 &&
		len(r.NewFiles) == 0
}

// CheckFreshness compares source metadata with a cached index.
// It does not read or parse unchanged source files.
func (b *Builder) CheckFreshness(ctx context.Context, target string, cached *model.Index) (FreshnessReport, error) {
	var report FreshnessReport
	if cached == nil {
		return report, fmt.Errorf("cached index is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	root, info, err := resolveBuildTarget(target)
	if err != nil {
		return report, err
	}
	if !info.IsDir() {
		return report, fmt.Errorf("freshness target is not a directory: %s", root)
	}
	if filepath.Clean(cached.Root) != root {
		report.RootMismatch = true
		return report, nil
	}

	filesByPath := make(map[string]model.FileSummary, len(cached.Files))
	for _, file := range cached.Files {
		filesByPath[filepath.ToSlash(file.Path)] = file
	}
	errorsByPath := make(map[string]struct{}, len(cached.Errors))
	for _, parseErr := range cached.Errors {
		errorsByPath[filepath.ToSlash(parseErr.Path)] = struct{}{}
	}
	seenFiles := make(map[string]struct{}, len(filesByPath))
	seenErrors := make(map[string]struct{}, len(errorsByPath))
	readyByExtension := map[string]bool{}
	skipDirs := DefaultSkipDirs()

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if skipDirs[entry.Name()] || shouldSkipIndexPath(root, path, true, b.ignore) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipIndexPath(root, path, false, b.ignore) {
			return nil
		}

		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)

		if previous, ok := filesByPath[relPath]; ok {
			seenFiles[relPath] = struct{}{}
			fileInfo, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if indexedFileMetadataChanged(previous, fileInfo.Size(), fileInfo.ModTime(), cached.GeneratedAt) {
				report.StaleFiles = append(report.StaleFiles, relPath)
			}
			return nil
		}
		if _, ok := errorsByPath[relPath]; ok {
			seenErrors[relPath] = struct{}{}
			fileInfo, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if fileInfo.ModTime().After(cached.GeneratedAt) {
				report.StaleFiles = append(report.StaleFiles, relPath)
			}
			return nil
		}

		parser, ok := b.parserForPath(path)
		if !ok {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(path))
		ready, checked := readyByExtension[extension]
		if !checked {
			ready = parserReadyForIndex(parser)
			readyByExtension[extension] = ready
		}
		if ready {
			report.NewFiles = append(report.NewFiles, relPath)
		}
		return nil
	})
	if err != nil {
		return report, err
	}

	for path := range filesByPath {
		if _, ok := seenFiles[path]; !ok {
			report.MissingFiles = append(report.MissingFiles, path)
		}
	}
	for path := range errorsByPath {
		if _, ok := seenErrors[path]; !ok {
			report.MissingFiles = append(report.MissingFiles, path)
		}
	}
	sort.Strings(report.StaleFiles)
	sort.Strings(report.MissingFiles)
	sort.Strings(report.NewFiles)
	return report, nil
}

func indexedFileMetadataChanged(previous model.FileSummary, size int64, modTime, generatedAt time.Time) bool {
	if previous.ModTimeUnixNano == 0 {
		return modTime.After(generatedAt)
	}
	return previous.SizeBytes != size || previous.ModTimeUnixNano != modTime.UnixNano()
}
