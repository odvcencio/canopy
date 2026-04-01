package generated

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// ParseConfigLines parses .gtsgenerated content lines into ConfigEntry values.
// Lines with "generator: pattern" syntax use the named generator.
// Lines without a colon default to generator "config".
// Comments (#) and blank lines are skipped.
func ParseConfigLines(lines []string) []ConfigEntry {
	entries, _ := ParseConfigLinesWithOptions(lines)
	return entries
}

// ParseConfigLinesWithOptions parses .gtsgenerated content lines and also
// extracts directive options such as @scan-depth. Returns parsed entries and
// the configured scan depth (0 means use default).
func ParseConfigLinesWithOptions(lines []string) ([]ConfigEntry, int) {
	var entries []ConfigEntry
	scanDepth := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "@scan-depth") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "@scan-depth"))
			if n, err := strconv.Atoi(rest); err == nil && n > 0 {
				scanDepth = n
				if scanDepth > 200 {
					scanDepth = 200
				}
			}
			continue
		}
		generator := "config"
		pattern := line
		if idx := strings.Index(line, ": "); idx > 0 {
			candidate := line[:idx]
			if !strings.ContainsAny(candidate, "/\\.*") {
				generator = strings.TrimSpace(candidate)
				pattern = strings.TrimSpace(line[idx+2:])
			}
		}
		if pattern == "" {
			continue
		}
		entries = append(entries, ConfigEntry{
			Generator: generator,
			Pattern:   pattern,
		})
	}
	return entries, scanDepth
}

// LoadConfigFile reads a .gtsgenerated file and returns parsed entries.
// Returns nil entries (no error) if the file does not exist.
func LoadConfigFile(path string) ([]ConfigEntry, error) {
	entries, _, err := LoadConfigFileWithOptions(path)
	return entries, err
}

// LoadConfigFileWithOptions reads a .gtsgenerated file and returns parsed
// entries along with any configured scan depth. Returns nil entries (no error)
// if the file does not exist.
func LoadConfigFileWithOptions(path string) ([]ConfigEntry, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	entries, depth := ParseConfigLinesWithOptions(lines)
	return entries, depth, nil
}
