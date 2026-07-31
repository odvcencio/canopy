package changeintel

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// rawChange is a single line of `git diff --name-status -M` output, prior to
// classification against FileStatus.
type rawChange struct {
	code    string // "A", "D", "M", or "R<score>"
	path    string // new path (or the only path for A/D/M)
	oldPath string // set only for renames
}

// gitVerifyRef reports whether ref resolves to a commit reachable from root.
// An empty ref always verifies (it means "working tree").
func gitVerifyRef(ctx context.Context, root, ref string) bool {
	if strings.TrimSpace(ref) == "" {
		return true
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return cmd.Run() == nil
}

// gitDiffNameStatus lists changed paths between baseRef and headRef with
// rename detection. headRef == "" compares baseRef against the working tree.
func gitDiffNameStatus(ctx context.Context, root, baseRef, headRef string) ([]rawChange, error) {
	args := []string{"-C", root, "diff", "--name-status", "-M", baseRef}
	if strings.TrimSpace(headRef) != "" {
		args = append(args, headRef)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status %s: %w", baseRef, err)
	}

	var changes []rawChange
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		code := fields[0]
		if strings.HasPrefix(code, "R") || strings.HasPrefix(code, "C") {
			if len(fields) < 3 {
				continue
			}
			changes = append(changes, rawChange{code: code, oldPath: fields[1], path: fields[2]})
			continue
		}
		changes = append(changes, rawChange{code: code, path: fields[1]})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].path < changes[j].path })
	return changes, nil
}

// gitShow returns the content of path at ref, and false if the path does not
// exist at that ref (deleted-in-head or added-in-head files are expected to
// miss on one side).
func gitShow(ctx context.Context, root, ref, path string) ([]byte, bool, error) {
	spec := ref + ":" + path
	cmd := exec.CommandContext(ctx, "git", "-C", root, "show", spec)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") ||
			strings.Contains(msg, "bad revision") || strings.Contains(msg, "Invalid object name") {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("git show %s: %w: %s", spec, err, strings.TrimSpace(msg))
	}
	return stdout.Bytes(), true, nil
}

// readWorktreeFile reads path relative to root from disk, returning false if
// the file does not exist.
func readWorktreeFile(root, path string) ([]byte, bool, error) {
	abs := filepath.Join(root, filepath.FromSlash(path))
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", abs, err)
	}
	return data, true, nil
}

// snapshotContent reads path's content at the given snapshot ref. An empty
// ref reads from the working tree; otherwise it reads via `git show`.
func snapshotContent(ctx context.Context, root string, ref SnapshotRef, path string) ([]byte, bool, error) {
	if strings.TrimSpace(ref.Ref) == "" {
		return readWorktreeFile(root, path)
	}
	return gitShow(ctx, root, ref.Ref, path)
}
