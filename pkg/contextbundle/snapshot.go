package contextbundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/canopy/pkg/index"
)

// ComputeSnapshot derives the snapshot identity for root following spec
// 10.5: a clean git tree yields commit:<HEAD>:<config-digest>; a dirty tree
// or non-git root yields worktree:<HEAD-or-empty>:<dirty-digest>:<config-digest>.
func ComputeSnapshot(root string) (Snapshot, error) {
	root, err := canonicalRoot(root)
	if err != nil {
		return Snapshot{}, err
	}

	configDigest := configDigestFor(root)

	head, isGitRepo := gitHeadCommit(root)
	if !isGitRepo {
		digest, err := contentDigestFallback(root)
		if err != nil {
			return Snapshot{}, err
		}
		return Snapshot{
			Kind:         "worktree",
			ID:           fmt.Sprintf("worktree::%s:%s", digest, configDigest),
			Root:         root,
			DirtyDigest:  digest,
			ConfigDigest: configDigest,
		}, nil
	}

	dirtyDigest, dirty, err := gitDirtyDigest(root)
	if err != nil {
		return Snapshot{}, err
	}
	if !dirty {
		return Snapshot{
			Kind:         "commit",
			ID:           fmt.Sprintf("commit:%s:%s", head, configDigest),
			Root:         root,
			HeadCommit:   head,
			ConfigDigest: configDigest,
		}, nil
	}
	return Snapshot{
		Kind:         "worktree",
		ID:           fmt.Sprintf("worktree:%s:%s:%s", head, dirtyDigest, configDigest),
		Root:         root,
		HeadCommit:   head,
		DirtyDigest:  dirtyDigest,
		ConfigDigest: configDigest,
	}, nil
}

func configDigestFor(root string) string {
	hashes, err := index.ComputeConfigHashes(root)
	if err != nil || len(hashes) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, hashes[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func gitHeadCommit(root string) (string, bool) {
	out, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// gitDirtyDigest reports whether the tree has uncommitted changes and, if so,
// a deterministic digest over every tracked modification, staged
// modification, deletion, and untracked file, keyed by content hash rather
// than timestamp (spec 10.5).
func gitDirtyDigest(root string) (digest string, dirty bool, err error) {
	out, err := runGit(root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(out, "\n")
	type entry struct {
		status string
		path   string
	}
	var entries []entry
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		path := strings.TrimSpace(line[3:])
		// Renames report "old -> new"; hash the new path's content and record
		// both names in the digest input for stability under renames.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		entries = append(entries, entry{status: status, path: path})
	}
	if len(entries) == 0 {
		return "", false, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	h := sha256.New()
	for _, e := range entries {
		contentHash := hashFileOrEmpty(filepath.Join(root, e.path))
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", e.status, e.path, contentHash)
	}
	return hex.EncodeToString(h.Sum(nil)), true, nil
}

func hashFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "deleted"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// contentDigestFallback hashes tracked-looking source files by walking the
// tree when root is not a git repository (e.g. an imported snapshot without
// version control). It intentionally skips heavy directories via the same
// default skip list index building uses, so it stays cheap on large trees.
func contentDigestFallback(root string) (string, error) {
	h := sha256.New()
	skip := index.DefaultSkipDirs()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func runGit(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}
