// Package boundaries provides architecture boundary enforcement through a
// simple DSL. It parses .canopyboundaries config files, evaluates import edges
// against allow/deny rules, and reports violations.
package boundaries

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rule represents a single boundary directive for a module.
type Rule struct {
	Module  string   // path-glob identifying which module(s) this rule covers
	Type    string   // "allow" or "deny"
	Targets []string // path-globs for the import targets; empty means none
}

// Config holds all parsed boundary rules from a .canopyboundaries file.
type Config struct {
	Rules []Rule
}

// ImportEdge represents a directed import from one package to another.
type ImportEdge struct {
	From string
	To   string
}

// Violation records a boundary rule that an import edge breaks.
type Violation struct {
	From    string // importing package
	To      string // imported package
	Rule    string // "allow" or "deny"
	Module  string // the module glob that matched From
	Message string // human-readable explanation
}

// ParseConfig parses the text content of a .canopyboundaries configuration file
// and returns the structured Config. Lines starting with # are comments.
// Blank lines are ignored.
func ParseConfig(content string) (*Config, error) {
	cfg := &Config{}

	for lineNo, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Expect lines beginning with "module".
		if !strings.HasPrefix(line, "module") {
			return nil, fmt.Errorf("line %d: unrecognized directive %q", lineNo+1, line)
		}

		// Tokenise: collapse whitespace, split on spaces.
		fields := strings.Fields(line)
		// Minimum: "module" <path> <allow|deny> <targets...>
		if len(fields) < 3 {
			return nil, fmt.Errorf("line %d: incomplete module directive %q", lineNo+1, line)
		}

		modulePath := fields[1]
		ruleType := strings.ToLower(fields[2])

		if ruleType != "allow" && ruleType != "deny" {
			return nil, fmt.Errorf("line %d: unsupported rule type %q (expected allow or deny)", lineNo+1, fields[2])
		}

		if len(fields) < 4 {
			return nil, fmt.Errorf("line %d: missing targets for %s rule", lineNo+1, ruleType)
		}

		// Rejoin everything after the rule type and split on commas.
		targetStr := strings.Join(fields[3:], " ")
		var targets []string

		if strings.TrimSpace(targetStr) == "-" {
			// Dash means no targets (empty allow = allow nothing).
			targets = nil
		} else {
			for _, t := range strings.Split(targetStr, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					targets = append(targets, t)
				}
			}
		}

		cfg.Rules = append(cfg.Rules, Rule{
			Module:  modulePath,
			Type:    ruleType,
			Targets: targets,
		})
	}

	return cfg, nil
}

// Evaluate checks all import edges against the boundary rules in cfg and
// returns any violations found.
//
// Evaluation logic:
//   - Only modules matching at least one rule are checked.
//   - Deny rules: imports matching any deny target are violations.
//   - Allow rules: imports NOT matching any allow target are violations.
//   - If no rules match a module, skip it.
func Evaluate(cfg *Config, edges []ImportEdge) []Violation {
	if cfg == nil {
		return nil
	}

	var violations []Violation

	for _, edge := range edges {
		// Collect all rules whose module glob matches the From package.
		var matchingRules []Rule
		for _, r := range cfg.Rules {
			if matchGlob(r.Module, edge.From) {
				matchingRules = append(matchingRules, r)
			}
		}
		if len(matchingRules) == 0 {
			continue
		}

		for _, r := range matchingRules {
			switch r.Type {
			case "deny":
				for _, target := range r.Targets {
					if matchGlob(target, edge.To) {
						violations = append(violations, Violation{
							From:    edge.From,
							To:      edge.To,
							Rule:    "deny",
							Module:  r.Module,
							Message: fmt.Sprintf("%s imports %s, denied by module %s", edge.From, edge.To, r.Module),
						})
						break // one deny match per rule is enough
					}
				}
			case "allow":
				allowed := false
				for _, target := range r.Targets {
					if matchGlob(target, edge.To) {
						allowed = true
						break
					}
				}
				if !allowed {
					violations = append(violations, Violation{
						From:    edge.From,
						To:      edge.To,
						Rule:    "allow",
						Module:  r.Module,
						Message: fmt.Sprintf("%s imports %s, not allowed by module %s", edge.From, edge.To, r.Module),
					})
				}
			}
		}
	}

	return violations
}

// matchGlob matches a pattern against a value. Supported patterns:
//   - "*" matches everything
//   - "prefix/*" matches any value starting with "prefix/"
//   - exact string match otherwise
func matchGlob(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := pattern[:len(pattern)-1] // keep trailing slash: "pkg/"
		return strings.HasPrefix(value, prefix)
	}
	return pattern == value
}

// LoadConfig searches for a .canopyboundaries file starting in dir and walking
// up parent directories until it finds one or reaches the filesystem root.
// Returns a nil Config with no error if no config file is found.
func LoadConfig(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory: %w", err)
	}

	for {
		candidate := filepath.Join(abs, ".canopyboundaries")
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
