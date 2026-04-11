package index

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/gts-suite/pkg/generated"
	"github.com/odvcencio/gts-suite/pkg/ignore"
)

// workspaceIgnoreFiles lists the config files that anchor a workspace root.
var workspaceIgnoreFiles = []string{".graftignore", ".gtsignore", ".gtsgenerated"}

// workspaceIgnoreRoot walks up from target (resolved to absolute) looking for a
// directory containing any of the workspace config files. Returns the directory
// if found, or target itself (resolved) if none is found.
func workspaceIgnoreRoot(target string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	dir := abs
	if !info.IsDir() {
		dir = filepath.Dir(abs)
	}

	for {
		for _, name := range workspaceIgnoreFiles {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// No config file found; return the original resolved path.
	if info.IsDir() {
		return abs, nil
	}
	return filepath.Dir(abs), nil
}

// loadWorkspaceIgnoreLines returns the raw ignore pattern lines from the
// workspace .graftignore/.gtsignore files. Returns nil (no error) when no
// files are present. Shared between LoadWorkspaceIgnoreMatcher and the
// builder constructors so CLI-supplied extras can be merged with workspace
// patterns before parsing.
func loadWorkspaceIgnoreLines(target string) ([]string, error) {
	root, err := workspaceIgnoreRoot(target)
	if err != nil {
		return nil, err
	}

	var allPatterns []string
	for _, name := range []string{".graftignore", ".gtsignore"} {
		p := filepath.Join(root, name)
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return nil, readErr
		}
		allPatterns = append(allPatterns, splitLines(string(data))...)
	}
	return allPatterns, nil
}

// LoadWorkspaceIgnoreMatcher finds the workspace root and loads ignore patterns
// from .graftignore and .gtsignore files found there.
func LoadWorkspaceIgnoreMatcher(target string) (*ignore.Matcher, error) {
	lines, err := loadWorkspaceIgnoreLines(target)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return ignore.ParsePatterns(lines), nil
}

// splitLines splits s into lines without trailing newlines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// ComputeConfigHashes computes SHA-256 hashes of workspace config files.
// Returns a map of filename → hex hash. Missing files are omitted.
func ComputeConfigHashes(target string) (map[string]string, error) {
	root, err := workspaceIgnoreRoot(target)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string)
	for _, name := range workspaceIgnoreFiles {
		data, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			continue
		}
		h := sha256.Sum256(data)
		hashes[name] = fmt.Sprintf("%x", h)
	}
	if len(hashes) == 0 {
		return nil, nil
	}
	return hashes, nil
}

// LoadWorkspaceGeneratedConfig finds the workspace root and loads .gtsgenerated
// config entries along with any configured scan depth. Returns nil entries (no
// error) when the file is absent.
func LoadWorkspaceGeneratedConfig(target string) ([]generated.ConfigEntry, int, error) {
	root, err := workspaceIgnoreRoot(target)
	if err != nil {
		return nil, 0, err
	}
	return generated.LoadConfigFileWithOptions(filepath.Join(root, ".gtsgenerated"))
}

// NewBuilderWithWorkspaceIgnores creates a Builder pre-configured with ignore
// patterns and generated-file detection from the workspace config files found
// at or above target.
func NewBuilderWithWorkspaceIgnores(target string) (*Builder, error) {
	return NewBuilderWithWorkspaceIgnoresAndExtras(target, nil)
}

// NewBuilderWithWorkspaceIgnoresAndExtras is like NewBuilderWithWorkspaceIgnores
// but also merges an additional set of gitignore-style patterns (typically from
// CLI --exclude flags) with the workspace patterns before attaching them to the
// builder. Pass nil or an empty slice for behavior identical to
// NewBuilderWithWorkspaceIgnores.
//
// Workspace patterns are always applied first; extras are appended so negation
// patterns (`!foo`) in extras can override workspace patterns, matching the
// gitignore precedence rule that later patterns win.
func NewBuilderWithWorkspaceIgnoresAndExtras(target string, extraPatterns []string) (*Builder, error) {
	builder := NewBuilder()

	workspaceLines, err := loadWorkspaceIgnoreLines(target)
	if err != nil {
		return nil, err
	}

	allLines := make([]string, 0, len(workspaceLines)+len(extraPatterns))
	allLines = append(allLines, workspaceLines...)
	allLines = append(allLines, extraPatterns...)
	if len(allLines) > 0 {
		builder.SetIgnore(ignore.ParsePatterns(allLines))
	}

	configs, scanDepth, err := LoadWorkspaceGeneratedConfig(target)
	if err != nil {
		return nil, err
	}
	builder.SetDetector(generated.NewDetector(configs, scanDepth))

	hashes, err := ComputeConfigHashes(target)
	if err != nil {
		return nil, err
	}
	builder.SetConfigHashes(hashes)

	return builder, nil
}
