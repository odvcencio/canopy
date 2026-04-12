package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/odvcencio/canopy/pkg/ignore"
	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/structdiff"
)

func watchWithFSNotify(ctx context.Context, target string, debounce time.Duration, ignorePaths map[string]bool, ignoreMatcher *ignore.Matcher, onChange func(changedPaths []string)) error {
	roots, err := watchRoots(target)
	if err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	absTarget, _ := filepath.Abs(target)
	absTarget = filepath.Clean(absTarget)

	for _, root := range roots {
		if err := addWatchRecursive(watcher, root, absTarget, ignoreMatcher); err != nil {
			return err
		}
	}

	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	pending := false
	pendingPaths := map[string]bool{}

	resetDebounce := func(path string) {
		if path != "" {
			pendingPaths[path] = true
		}
		if pending {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer.Reset(debounce)
		pending = true
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			eventPath := filepath.Clean(event.Name)
			if shouldIgnoreWatchPath(eventPath, ignorePaths, absTarget, ignoreMatcher) {
				continue
			}

			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(eventPath); statErr == nil && info.IsDir() {
					_ = addWatchRecursive(watcher, eventPath, absTarget, ignoreMatcher)
				}
			}

			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
				continue
			}
			resetDebounce(eventPath)
		case <-timer.C:
			if pending {
				pending = false
				changed := make([]string, 0, len(pendingPaths))
				for path := range pendingPaths {
					changed = append(changed, path)
				}
				sort.Strings(changed)
				pendingPaths = map[string]bool{}
				onChange(changed)
			}
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return watchErr
		}
	}
}

func watchRoots(target string) ([]string, error) {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	absTarget = filepath.Clean(absTarget)

	info, err := os.Stat(absTarget)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return []string{absTarget}, nil
	}
	return []string{filepath.Dir(absTarget)}, nil
}

func addWatchRecursive(watcher *fsnotify.Watcher, root string, projectRoot string, ignoreMatcher *ignore.Matcher) error {
	root = filepath.Clean(root)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if shouldSkipWatchDir(projectRoot, path, entry.Name(), ignoreMatcher) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

func shouldSkipWatchDir(root, path, name string, ignoreMatcher *ignore.Matcher) bool {
	if path == root {
		return false
	}

	if name == ".git" || name == ".hg" || name == ".svn" || name == "node_modules" || name == "vendor" {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	if ignoreMatcher != nil {
		if relPath, err := filepath.Rel(root, path); err == nil {
			if ignoreMatcher.Match(filepath.ToSlash(relPath), true) {
				return true
			}
		}
	}
	return false
}

func shouldIgnoreWatchPath(path string, ignorePaths map[string]bool, root string, ignoreMatcher *ignore.Matcher) bool {
	if ignorePaths[path] {
		return true
	}

	base := filepath.Base(path)
	if base == ".DS_Store" || strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, ".swx") || strings.HasPrefix(base, ".#") {
		return true
	}
	if ignoreMatcher != nil {
		if relPath, err := filepath.Rel(root, path); err == nil {
			if ignoreMatcher.Match(filepath.ToSlash(relPath), false) {
				return true
			}
		}
	}
	return false
}

type fileChangeSummary struct {
	File          string
	Added         int
	Removed       int
	Modified      int
	ImportAdded   int
	ImportRemoved int
}

func printChangeReport(report structdiff.Report, hasBaseline bool) {
	if !hasBaseline {
		fmt.Println("changes: baseline cache not found; treating current index as changed")
		return
	}

	totalImportAdded := 0
	totalImportRemoved := 0
	for _, item := range report.ImportChanges {
		totalImportAdded += len(item.Added)
		totalImportRemoved += len(item.Removed)
	}

	fmt.Printf(
		"changes: files=%d symbols=+%d -%d ~%d imports=+%d -%d\n",
		report.Stats.ChangedFiles,
		report.Stats.AddedSymbols,
		report.Stats.RemovedSymbols,
		report.Stats.ModifiedSymbols,
		totalImportAdded,
		totalImportRemoved,
	)

	summaries := summarizeChangesByFile(report)
	for _, summary := range summaries {
		parts := make([]string, 0, 4)
		if summary.Added > 0 {
			parts = append(parts, fmt.Sprintf("+%d", summary.Added))
		}
		if summary.Removed > 0 {
			parts = append(parts, fmt.Sprintf("-%d", summary.Removed))
		}
		if summary.Modified > 0 {
			parts = append(parts, fmt.Sprintf("~%d", summary.Modified))
		}
		if summary.ImportAdded > 0 || summary.ImportRemoved > 0 {
			parts = append(parts, fmt.Sprintf("imports:+%d -%d", summary.ImportAdded, summary.ImportRemoved))
		}
		if len(parts) == 0 {
			continue
		}
		fmt.Printf("  %s %s\n", summary.File, strings.Join(parts, " "))
	}
}

func summarizeChangesByFile(report structdiff.Report) []fileChangeSummary {
	byFile := map[string]*fileChangeSummary{}
	ensure := func(path string) *fileChangeSummary {
		if existing, ok := byFile[path]; ok {
			return existing
		}
		created := &fileChangeSummary{File: path}
		byFile[path] = created
		return created
	}

	for _, item := range report.AddedSymbols {
		ensure(item.File).Added++
	}
	for _, item := range report.RemovedSymbols {
		ensure(item.File).Removed++
	}
	for _, item := range report.ModifiedSymbols {
		ensure(item.After.File).Modified++
	}
	for _, item := range report.ImportChanges {
		summary := ensure(item.File)
		summary.ImportAdded += len(item.Added)
		summary.ImportRemoved += len(item.Removed)
	}

	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)

	out := make([]fileChangeSummary, 0, len(files))
	for _, file := range files {
		out = append(out, *byFile[file])
	}
	return out
}

func parseErrorsEqual(left, right []model.ParseError) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Path != right[i].Path || left[i].Error != right[i].Error {
			return false
		}
	}
	return true
}

func printIndexSummary(idx *model.Index, stats index.BuildStats, incremental bool) {
	if incremental {
		fmt.Printf(
			"indexed: files=%d symbols=%d errors=%d root=%s parsed=%d reused=%d\n",
			idx.FileCount(),
			idx.SymbolCount(),
			len(idx.Errors),
			idx.Root,
			stats.ParsedFiles,
			stats.ReusedFiles,
		)
		return
	}

	fmt.Printf("indexed: files=%d symbols=%d errors=%d root=%s\n", idx.FileCount(), idx.SymbolCount(), len(idx.Errors), idx.Root)
}
