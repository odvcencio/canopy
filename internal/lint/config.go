package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ConfigOverride redefines the threshold and severity for a specific metric.
type ConfigOverride struct {
	Metric    string `json:"metric"`
	Threshold int    `json:"threshold"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Scope     string `json:"scope,omitempty"`
}

// PackageRule defines a package-level lint directive. Threshold rules check a
// metric against a numeric limit; enforcement rules (Enforcement=true) are
// boolean checks whose metric name conventionally starts with "no_".
type PackageRule struct {
	Metric      string `json:"metric"`
	Threshold   int    `json:"threshold,omitempty"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Scope       string `json:"scope,omitempty"`
	Enforcement bool   `json:"enforcement,omitempty"`
}

// ConfigIgnore suppresses lint violations for a specific metric, file, or symbol.
type ConfigIgnore struct {
	Metric   string `json:"metric"`    // "*" means all metrics
	FilePath string `json:"file_path"` // supports trailing / for directory matching
	Symbol   string `json:"symbol"`    // optional, extracted after : in "file.go:funcName"
}

// Config holds all parsed directives from a .gtslint configuration file.
type Config struct {
	Overrides    []ConfigOverride `json:"overrides,omitempty"`
	Ignores      []ConfigIgnore   `json:"ignores,omitempty"`
	PackageRules []PackageRule    `json:"package_rules,omitempty"`
}

// packageThresholdPattern matches lines like: package import_depth > 5 -> error "dependency chain too deep"
// The optional "in scope" group supports scoped package rules.
var packageThresholdPattern = regexp.MustCompile(
	`^\s*package\s+(\S+)\s*>\s*(\d+)(?:\s+in\s+(\S+))?\s*(?:→|->)\s*(\w+)\s+"([^"]*)"`,
)

// packageEnforcementPattern matches lines like: package no_import_cycles -> error "import cycle detected"
// Enforcement metrics conventionally start with "no_".
var packageEnforcementPattern = regexp.MustCompile(
	`^\s*package\s+(no_\S+)(?:\s+in\s+(\S+))?\s*(?:→|->)\s*(\w+)\s+"([^"]*)"`,
)

// scopedOverridePattern matches lines like: fan_out > 10 in pkg/* -> warn "high fan-out"
var scopedOverridePattern = regexp.MustCompile(
	`^\s*(\S+)\s*>\s*(\d+)\s+in\s+(\S+)\s*(?:→|->)\s*(\w+)\s+"([^"]*)"`,
)

// overridePattern matches lines like: cyclomatic > 35 → warn "function too complex"
var overridePattern = regexp.MustCompile(
	`^\s*(\S+)\s*>\s*(\d+)\s*(?:→|->)\s*(\w+)\s+"([^"]*)"`,
)

// ignorePattern matches lines like: ignore cyclomatic in policy.go:listPREntityChanges
var ignorePattern = regexp.MustCompile(
	`^\s*ignore\s+(\S+)\s+in\s+(\S+)\s*$`,
)

// LoadConfig searches for a .gtslint file starting in dir and walking up
// parent directories until it finds one or reaches the filesystem root.
// Returns a nil Config with no error if no config file is found.
func LoadConfig(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory: %w", err)
	}

	for {
		candidate := filepath.Join(abs, ".gtslint")
		data, err := os.ReadFile(candidate)
		if err == nil {
			cfg, parseErr := ParseConfig(string(data))
			if parseErr != nil {
				return nil, fmt.Errorf("parsing %s: %w", candidate, parseErr)
			}
			return cfg, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", candidate, err)
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			// Reached filesystem root without finding a config file.
			return nil, nil
		}
		abs = parent
	}
}

// ParseConfig parses the text content of a .gtslint configuration file and
// returns the structured Config. Lines starting with # are comments. Blank
// lines are ignored.
func ParseConfig(content string) (*Config, error) {
	cfg := &Config{}

	for lineNo, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Package threshold rules: package metric > N [in scope] → severity "msg"
		if m := packageThresholdPattern.FindStringSubmatch(line); m != nil {
			threshold, err := strconv.Atoi(m[2])
			if err != nil || threshold <= 0 {
				return nil, fmt.Errorf("line %d: invalid threshold %q", lineNo+1, m[2])
			}
			severity := strings.ToLower(m[4])
			if severity != "warn" && severity != "error" {
				return nil, fmt.Errorf("line %d: unsupported severity %q (expected warn or error)", lineNo+1, m[4])
			}
			cfg.PackageRules = append(cfg.PackageRules, PackageRule{
				Metric:    strings.ToLower(m[1]),
				Threshold: threshold,
				Scope:     m[3], // empty string when "in scope" is absent
				Severity:  severity,
				Message:   m[5],
			})
			continue
		}

		// Package enforcement rules: package no_metric [in scope] → severity "msg"
		if m := packageEnforcementPattern.FindStringSubmatch(line); m != nil {
			severity := strings.ToLower(m[3])
			if severity != "warn" && severity != "error" {
				return nil, fmt.Errorf("line %d: unsupported severity %q (expected warn or error)", lineNo+1, m[3])
			}
			cfg.PackageRules = append(cfg.PackageRules, PackageRule{
				Metric:      strings.ToLower(m[1]),
				Scope:       m[2],
				Severity:    severity,
				Message:     m[4],
				Enforcement: true,
			})
			continue
		}

		// Scoped overrides: metric > N in scope → severity "msg"
		if m := scopedOverridePattern.FindStringSubmatch(line); m != nil {
			threshold, err := strconv.Atoi(m[2])
			if err != nil || threshold <= 0 {
				return nil, fmt.Errorf("line %d: invalid threshold %q", lineNo+1, m[2])
			}
			severity := strings.ToLower(m[4])
			if severity != "warn" && severity != "error" {
				return nil, fmt.Errorf("line %d: unsupported severity %q (expected warn or error)", lineNo+1, m[4])
			}
			cfg.Overrides = append(cfg.Overrides, ConfigOverride{
				Metric:    strings.ToLower(m[1]),
				Threshold: threshold,
				Scope:     m[3],
				Severity:  severity,
				Message:   m[5],
			})
			continue
		}

		// Unscoped overrides: metric > N → severity "msg"
		if m := overridePattern.FindStringSubmatch(line); m != nil {
			threshold, err := strconv.Atoi(m[2])
			if err != nil || threshold <= 0 {
				return nil, fmt.Errorf("line %d: invalid threshold %q", lineNo+1, m[2])
			}
			severity := strings.ToLower(m[3])
			if severity != "warn" && severity != "error" {
				return nil, fmt.Errorf("line %d: unsupported severity %q (expected warn or error)", lineNo+1, m[3])
			}
			cfg.Overrides = append(cfg.Overrides, ConfigOverride{
				Metric:    strings.ToLower(m[1]),
				Threshold: threshold,
				Severity:  severity,
				Message:   m[4],
			})
			continue
		}

		if m := ignorePattern.FindStringSubmatch(line); m != nil {
			metric := strings.ToLower(m[1])
			target := m[2]

			ignore := ConfigIgnore{Metric: metric}

			// Split file:symbol if colon is present
			if idx := strings.LastIndex(target, ":"); idx > 0 {
				ignore.FilePath = target[:idx]
				ignore.Symbol = target[idx+1:]
			} else {
				ignore.FilePath = target
			}

			cfg.Ignores = append(cfg.Ignores, ignore)
			continue
		}

		return nil, fmt.Errorf("line %d: unrecognized directive %q", lineNo+1, line)
	}

	return cfg, nil
}

// ShouldIgnore reports whether a violation for the given file, symbol, and
// metric should be suppressed based on the ignore rules in this config.
func (c *Config) ShouldIgnore(file, symbol, metric string) bool {
	if c == nil {
		return false
	}
	metric = strings.ToLower(metric)

	for _, ig := range c.Ignores {
		if !metricMatches(ig.Metric, metric) {
			continue
		}
		if !fileMatches(ig.FilePath, file) {
			continue
		}
		if ig.Symbol != "" && ig.Symbol != symbol {
			continue
		}
		return true
	}
	return false
}

// metricMatches returns true if the ignore metric covers the given metric.
func metricMatches(ignoreMetric, metric string) bool {
	if ignoreMetric == "*" {
		return true
	}
	return ignoreMetric == metric
}

// fileMatches returns true if the ignore file path covers the given file.
// A trailing slash on the ignore path means directory prefix matching.
func fileMatches(pattern, file string) bool {
	// Normalize to forward slashes for consistent matching.
	pattern = filepath.ToSlash(pattern)
	file = filepath.ToSlash(file)

	// Directory matching: pattern ends with /
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(file, pattern)
	}

	// Exact match on full path.
	if file == pattern {
		return true
	}

	// Match on just the filename portion (for bare filenames like "api_test.go").
	if filepath.Base(file) == pattern {
		return true
	}

	// Suffix match: pattern "internal/foo.go" matches "pkg/internal/foo.go"
	if strings.HasSuffix(file, "/"+pattern) {
		return true
	}

	return false
}
