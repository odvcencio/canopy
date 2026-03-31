package generated

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// ParseConfigLines parses .gtsgenerated content lines into ConfigEntry values.
// Lines with "generator: pattern" syntax use the named generator.
// Lines without a colon default to generator "config".
// Comments (#) and blank lines are skipped.
func ParseConfigLines(lines []string) []ConfigEntry {
	var entries []ConfigEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
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
	return entries
}

// LoadConfigFile reads a .gtsgenerated file and returns parsed entries.
// Returns nil entries (no error) if the file does not exist.
func LoadConfigFile(path string) ([]ConfigEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
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
	return ParseConfigLines(lines), nil
}
