// Package vcs implements a VCS feed that enriches scope graph definitions
// with authorship and change history from graft or git.
package vcs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/odvcencio/canopy/pkg/feeds"
	"github.com/odvcencio/canopy/pkg/scope"
)

// Feed implements FeedProvider for VCS blame data.
type Feed struct {
	vcsType string // "graft", "git", or ""
	vcsRoot string
}

// Detect checks for .graft/ or .git/ in the workspace and returns a Feed, or nil.
func Detect(workspaceRoot string) *Feed {
	// Prefer graft
	if info, err := os.Stat(filepath.Join(workspaceRoot, ".graft")); err == nil && info.IsDir() {
		return &Feed{vcsType: "graft", vcsRoot: workspaceRoot}
	}
	// Fall back to git
	if info, err := os.Stat(filepath.Join(workspaceRoot, ".git")); err == nil && info.IsDir() {
		return &Feed{vcsType: "git", vcsRoot: workspaceRoot}
	}
	return nil
}

func (f *Feed) Name() string             { return "vcs" }
func (f *Feed) Supports(lang string) bool { return true }
func (f *Feed) Priority() int            { return 50 }

func (f *Feed) Feed(graph *scope.Graph, file string, src []byte, ctx *feeds.FeedContext) error {
	fs := graph.FileScope(file)
	if fs == nil || len(fs.Defs) == 0 {
		return nil
	}

	blameData, err := f.blame(file)
	if err != nil {
		return nil // VCS errors are non-fatal
	}

	// For each definition, find the blame entry covering its start line
	for i := range fs.Defs {
		def := &fs.Defs[i]
		if entry, ok := blameData[def.Loc.StartLine]; ok {
			scope.SetMeta(def, "vcs.last_author", entry.Author)
			scope.SetMeta(def, "vcs.last_commit", entry.Commit)
			scope.SetMeta(def, "vcs.last_modified", entry.Timestamp)
			scope.SetMeta(def, "vcs.type", f.vcsType)
		}
	}
	return nil
}

// blameEntry holds parsed blame data for a line.
type blameEntry struct {
	Author    string
	Commit    string
	Timestamp string
}

// blame runs the appropriate blame command and returns line -> entry mapping.
func (f *Feed) blame(file string) (map[int]blameEntry, error) {
	absPath := filepath.Join(f.vcsRoot, file)
	if _, err := os.Stat(absPath); err != nil {
		return nil, err
	}
	return f.gitBlame(file)
}

// gitBlame parses `git blame --porcelain` output.
func (f *Feed) gitBlame(file string) (map[int]blameEntry, error) {
	cmd := exec.Command("git", "blame", "--porcelain", file)
	cmd.Dir = f.vcsRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git blame: %w", err)
	}
	return parseGitBlame(out)
}

// parseGitBlame parses git blame --porcelain output into a line -> entry map.
func parseGitBlame(data []byte) (map[int]blameEntry, error) {
	result := make(map[int]blameEntry)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	var currentCommit string
	var currentAuthor string
	var currentTimestamp string
	var currentLine int

	for scanner.Scan() {
		line := scanner.Text()

		// Commit line: "<hash> <orig_line> <final_line> [<num_lines>]"
		if len(line) >= 40 && !strings.HasPrefix(line, "\t") && !strings.Contains(line[:40], " ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				currentCommit = parts[0]
				currentLine, _ = strconv.Atoi(parts[2])
			}
			continue
		}

		if strings.HasPrefix(line, "author ") {
			currentAuthor = strings.TrimPrefix(line, "author ")
		} else if strings.HasPrefix(line, "author-time ") {
			currentTimestamp = strings.TrimPrefix(line, "author-time ")
		} else if strings.HasPrefix(line, "\t") {
			// Content line -- record the entry for this line number
			if currentLine > 0 {
				result[currentLine] = blameEntry{
					Author:    currentAuthor,
					Commit:    currentCommit,
					Timestamp: currentTimestamp,
				}
			}
		}
	}
	return result, nil
}
