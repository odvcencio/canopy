// Package ignore implements gitignore-style pattern matching for filtering file paths.
package ignore

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type pattern struct {
	raw     string
	negated bool
	dirOnly bool
	glob    string
}

// Matcher evaluates file paths against a set of gitignore-style patterns.
type Matcher struct {
	patterns []pattern
}

// DirectoryBasenames returns directory names that can be safely pruned by a
// basename-only directory walker. It is intentionally conservative: negated
// pattern sets return no names because later negations can re-include paths.
func (m *Matcher) DirectoryBasenames() []string {
	if m == nil || len(m.patterns) == 0 {
		return nil
	}

	for _, p := range m.patterns {
		if p.negated {
			return nil
		}
	}

	seen := map[string]bool{}
	var names []string
	for _, p := range m.patterns {
		if name := directoryBasenamePattern(p); name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// Load reads patterns from a file, one per line.
func Load(path string) (*Matcher, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ParsePatterns(lines), nil
}

// ParsePatterns builds a Matcher from raw pattern lines.
func ParsePatterns(lines []string) *Matcher {
	m := &Matcher{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := pattern{raw: line}

		if strings.HasPrefix(line, "!") {
			p.negated = true
			line = line[1:]
		}

		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		p.glob = line
		m.patterns = append(m.patterns, p)
	}
	return m
}

// Match returns true if the given path should be ignored.
// The path should be slash-separated and relative to the project root.
// isDir indicates whether the path refers to a directory.
func (m *Matcher) Match(path string, isDir bool) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}

	path = filepath.ToSlash(path)
	ignored := false

	for _, p := range m.patterns {
		if matchesPattern(p, path, isDir) {
			ignored = !p.negated
		}
	}
	return ignored
}

func matchesPattern(p pattern, path string, isDir bool) bool {
	if p.dirOnly {
		return matchDirectoryPattern(p.glob, path, isDir)
	}
	return matchPattern(p.glob, path)
}

func matchDirectoryPattern(glob, path string, isDir bool) bool {
	if isDir {
		return matchPattern(glob, path)
	}

	for _, dir := range ancestorDirectories(path) {
		if matchPattern(glob, dir) {
			return true
		}
	}
	return false
}

func ancestorDirectories(path string) []string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil
	}

	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return nil
	}

	dirs := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		dirs = append(dirs, strings.Join(parts[:i], "/"))
	}
	return dirs
}

// matchPattern checks whether a gitignore glob matches the given path.
// Patterns without a slash match against the basename only.
// Patterns with a slash match against the full path.
func matchPattern(glob, path string) bool {
	glob = filepath.ToSlash(glob)
	path = filepath.ToSlash(path)

	if strings.Contains(glob, "**") {
		return matchRecursivePattern(glob, path)
	}

	if strings.Contains(glob, "/") {
		matched, _ := filepath.Match(glob, path)
		return matched
	}

	// Match against basename
	base := filepath.Base(path)
	if matched, _ := filepath.Match(glob, base); matched {
		return true
	}

	// Also try matching against each path component for directory patterns
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if matched, _ := filepath.Match(glob, part); matched {
			return true
		}
	}
	return false
}

func matchRecursivePattern(glob, filePath string) bool {
	patternParts := splitPatternPath(glob)
	pathParts := splitPatternPath(filePath)
	memo := make(map[[2]int]bool)
	seen := make(map[[2]int]bool)

	var match func(patternIndex, pathIndex int) bool
	match = func(patternIndex, pathIndex int) bool {
		key := [2]int{patternIndex, pathIndex}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true

		var result bool
		defer func() {
			memo[key] = result
		}()

		if patternIndex == len(patternParts) {
			result = pathIndex == len(pathParts)
			return result
		}

		part := patternParts[patternIndex]
		if part == "**" {
			for nextPathIndex := pathIndex; nextPathIndex <= len(pathParts); nextPathIndex++ {
				if match(patternIndex+1, nextPathIndex) {
					result = true
					return result
				}
			}
			return false
		}

		if pathIndex >= len(pathParts) {
			return false
		}

		matched, err := path.Match(part, pathParts[pathIndex])
		if err != nil || !matched {
			return false
		}

		result = match(patternIndex+1, pathIndex+1)
		return result
	}

	return match(0, 0)
}

func splitPatternPath(path string) []string {
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	path = strings.Trim(path, "/")
	if path == "" || path == "." {
		return nil
	}
	return strings.Split(path, "/")
}

func directoryBasenamePattern(p pattern) string {
	glob := strings.TrimPrefix(filepath.ToSlash(p.glob), "./")
	glob = strings.Trim(glob, "/")
	if glob == "" || glob == "." {
		return ""
	}

	if p.dirOnly {
		return literalDirectoryBasename(glob)
	}

	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "/**")
		if strings.HasPrefix(prefix, "**/") {
			return literalDirectoryBasename(strings.TrimPrefix(prefix, "**/"))
		}
		return literalDirectoryBasename(prefix)
	}
	return ""
}

func literalDirectoryBasename(glob string) string {
	if glob == "" || strings.ContainsAny(glob, "*?[") {
		return ""
	}
	parts := strings.Split(glob, "/")
	if len(parts) != 1 {
		return ""
	}
	return parts[0]
}
