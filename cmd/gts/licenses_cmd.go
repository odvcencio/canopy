package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/lint"
)

// LicenseMatch represents a single detected license for a dependency.
type LicenseMatch struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
	License string `json:"license"`
	Source  string `json:"source"`
	File   string `json:"file,omitempty"`
}

// LicenseResult is the top-level output of the licenses command.
type LicenseResult struct {
	Matches     []LicenseMatch `json:"matches"`
	Denied      []LicenseMatch `json:"denied,omitempty"`
	Total       int            `json:"total"`
	DeniedCount int            `json:"denied_count"`
}

// spdxPatterns maps SPDX license identifiers to distinguishing phrases found
// in the corresponding LICENSE file text.
var spdxPatterns = map[string]*regexp.Regexp{
	"MIT":          regexp.MustCompile(`Permission is hereby granted, free of charge`),
	"Apache-2.0":   regexp.MustCompile(`Apache License.*Version 2`),
	"GPL-2.0":      regexp.MustCompile(`GNU General Public License.*version 2`),
	"GPL-3.0":      regexp.MustCompile(`GNU General Public License.*version 3`),
	"AGPL-3.0":     regexp.MustCompile(`GNU Affero General Public License`),
	"BSD-2-Clause": regexp.MustCompile(`Redistribution and use.*permitted provided that`),
	"BSD-3-Clause": regexp.MustCompile(`Redistribution and use.*permitted provided.*Neither the name`),
	"ISC":          regexp.MustCompile(`Permission to use, copy, modify`),
	"MPL-2.0":      regexp.MustCompile(`Mozilla Public License.*2\.0`),
	"LGPL-2.1":     regexp.MustCompile(`GNU Lesser General Public License.*2\.1`),
	"Unlicense":    regexp.MustCompile(`This is free and unencumbered software`),
}

func newLicensesCmd() *cobra.Command {
	var (
		cachePath  string
		noCache    bool
		jsonOutput bool
		denyFlags  []string
	)

	cmd := &cobra.Command{
		Use:   "licenses [path]",
		Short: "Detect dependency licenses from manifests and vendored LICENSE files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			absTarget, err := filepath.Abs(target)
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}

			// Suppress unused variable warnings for cache flags that are
			// reserved for future index-backed license analysis.
			_ = cachePath
			_ = noCache

			// Collect all license matches.
			var matches []LicenseMatch
			matches = append(matches, scanGoMod(absTarget)...)
			matches = append(matches, scanPackageJSON(absTarget)...)
			matches = append(matches, scanRequirementsTxt(absTarget)...)
			matches = append(matches, scanVendorLicenses(absTarget)...)

			// Build deny set from .gtslint config + CLI flags.
			denySet := buildDenySet(absTarget, denyFlags)

			// Partition into denied vs allowed.
			var denied []LicenseMatch
			for i := range matches {
				if denySet[matches[i].License] {
					denied = append(denied, matches[i])
				}
			}

			result := LicenseResult{
				Matches:     matches,
				Denied:      denied,
				Total:       len(matches),
				DeniedCount: len(denied),
			}

			if jsonOutput {
				if err := emitJSON(result); err != nil {
					return err
				}
			} else {
				fmt.Printf("licenses: %d detected, %d denied\n", result.Total, result.DeniedCount)
				for _, m := range matches {
					tag := ""
					if denySet[m.License] {
						tag = " [DENIED]"
					}
					if m.Version != "" {
						fmt.Printf("  %-40s %-12s %s (%s)%s\n", m.Package, m.Version, m.License, m.Source, tag)
					} else {
						fmt.Printf("  %-40s %-12s %s (%s)%s\n", m.Package, "", m.License, m.Source, tag)
					}
				}
			}

			if len(denied) > 0 {
				return exitCodeError{code: 1, err: fmt.Errorf("found %d denied license(s)", len(denied))}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringArrayVar(&denyFlags, "deny", nil, "additional denied SPDX license IDs")
	return cmd
}

// buildDenySet merges license deny rules from .gtslint with CLI --deny flags.
func buildDenySet(root string, cliDeny []string) map[string]bool {
	deny := make(map[string]bool)
	for _, id := range cliDeny {
		deny[strings.TrimSpace(id)] = true
	}

	cfg, err := lint.LoadConfig(root)
	if err == nil && cfg != nil {
		for _, rule := range cfg.LicenseRules {
			if rule.Type == "deny" {
				for _, id := range rule.Licenses {
					deny[id] = true
				}
			}
		}
	}
	return deny
}

// scanGoMod parses go.mod for require directives.
func scanGoMod(root string) []LicenseMatch {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var matches []LicenseMatch
	inRequire := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "require (") || strings.HasPrefix(line, "require(") {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Single-line require: require mod v1.2.3
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				matches = append(matches, LicenseMatch{
					Package: parts[1],
					Version: parts[2],
					License: "unknown",
					Source:  "go.mod",
				})
			}
			continue
		}

		if inRequire {
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.HasPrefix(parts[0], "//") {
				matches = append(matches, LicenseMatch{
					Package: parts[0],
					Version: parts[1],
					License: "unknown",
					Source:  "go.mod",
				})
			}
		}
	}
	return matches
}

// scanPackageJSON parses package.json for dependencies.
func scanPackageJSON(root string) []LicenseMatch {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	// Lightweight JSON extraction without full parser — scan for dependency keys.
	var matches []LicenseMatch
	content := string(data)

	for _, section := range []string{"dependencies", "devDependencies", "peerDependencies"} {
		matches = append(matches, extractJSONDeps(content, section, "package.json")...)
	}
	return matches
}

// extractJSONDeps does a simple extraction of "key": "value" pairs inside a
// named JSON object section. This avoids pulling in encoding/json for a
// potentially messy package.json.
var jsonDepPattern = regexp.MustCompile(`"([^"]+)"\s*:\s*"([^"]+)"`)

func extractJSONDeps(content, section, source string) []LicenseMatch {
	marker := fmt.Sprintf(`"%s"`, section)
	idx := strings.Index(content, marker)
	if idx < 0 {
		return nil
	}
	// Find the opening brace after the section key.
	rest := content[idx+len(marker):]
	braceIdx := strings.Index(rest, "{")
	if braceIdx < 0 {
		return nil
	}
	rest = rest[braceIdx:]

	// Find the matching closing brace.
	depth := 0
	end := -1
	for i, ch := range rest {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil
	}
	block := rest[:end+1]

	var matches []LicenseMatch
	for _, m := range jsonDepPattern.FindAllStringSubmatch(block, -1) {
		matches = append(matches, LicenseMatch{
			Package: m[1],
			Version: m[2],
			License: "unknown",
			Source:  source,
		})
	}
	return matches
}

// scanRequirementsTxt parses requirements.txt for Python packages.
func scanRequirementsTxt(root string) []LicenseMatch {
	path := filepath.Join(root, "requirements.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var matches []LicenseMatch
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}

		// Handle ==, >=, <=, ~=, != version specs.
		var pkg, version string
		for _, sep := range []string{"==", ">=", "<=", "~=", "!="} {
			if parts := strings.SplitN(line, sep, 2); len(parts) == 2 {
				pkg = strings.TrimSpace(parts[0])
				version = strings.TrimSpace(parts[1])
				break
			}
		}
		if pkg == "" {
			pkg = strings.TrimSpace(line)
		}
		// Strip extras like package[extra]
		if bracket := strings.Index(pkg, "["); bracket > 0 {
			pkg = pkg[:bracket]
		}

		matches = append(matches, LicenseMatch{
			Package: pkg,
			Version: version,
			License: "unknown",
			Source:  "requirements.txt",
		})
	}
	return matches
}

// scanVendorLicenses walks the vendor/ directory looking for LICENSE files and
// matches their content against known SPDX header patterns. When a license is
// identified, it updates any existing "unknown" match for that package.
func scanVendorLicenses(root string) []LicenseMatch {
	vendorDir := filepath.Join(root, "vendor")
	info, err := os.Stat(vendorDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var matches []LicenseMatch
	_ = filepath.Walk(vendorDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil || fi.IsDir() {
			return nil
		}
		base := strings.ToUpper(fi.Name())
		if base != "LICENSE" && base != "LICENSE.TXT" && base != "LICENSE.MD" &&
			base != "LICENCE" && base != "LICENCE.TXT" && base != "LICENCE.MD" &&
			base != "COPYING" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)
		spdx := identifyLicense(content)

		// Derive package from the relative path under vendor/.
		rel, relErr := filepath.Rel(vendorDir, filepath.Dir(path))
		if relErr != nil {
			rel = filepath.Dir(path)
		}
		rel = filepath.ToSlash(rel)

		matches = append(matches, LicenseMatch{
			Package: rel,
			License: spdx,
			Source:  "LICENSE file",
			File:    path,
		})
		return nil
	})
	return matches
}

// identifyLicense matches LICENSE file content against SPDX patterns.
func identifyLicense(content string) string {
	// Try more specific patterns first to avoid false positives.
	// BSD-3-Clause is a superset of BSD-2-Clause, so check it first.
	order := []string{
		"BSD-3-Clause", "BSD-2-Clause",
		"Apache-2.0", "MIT", "ISC",
		"GPL-3.0", "GPL-2.0", "AGPL-3.0", "LGPL-2.1",
		"MPL-2.0", "Unlicense",
	}
	for _, id := range order {
		if pat, ok := spdxPatterns[id]; ok && pat.MatchString(content) {
			return id
		}
	}
	return "unknown"
}

// MergeLicenseResults enriches manifest matches with LICENSE-file detections.
// If a vendor LICENSE was found for a package that already has a manifest
// entry, the manifest entry's license is updated from "unknown" to the
// detected SPDX ID.
func MergeLicenseResults(manifest, vendor []LicenseMatch) []LicenseMatch {
	vendorMap := make(map[string]LicenseMatch, len(vendor))
	for _, v := range vendor {
		vendorMap[v.Package] = v
	}

	merged := make([]LicenseMatch, 0, len(manifest)+len(vendor))
	seen := make(map[string]bool, len(manifest))

	for _, m := range manifest {
		if v, ok := vendorMap[m.Package]; ok && m.License == "unknown" {
			m.License = v.License
			m.File = v.File
		}
		merged = append(merged, m)
		seen[m.Package] = true
	}

	// Add vendor-only entries (packages found via LICENSE but not in manifests).
	for _, v := range vendor {
		if !seen[v.Package] {
			merged = append(merged, v)
		}
	}
	return merged
}

// RunLicenseScan executes the full license detection pipeline for the given
// root directory with the supplied deny list. Exported for use by the MCP
// call handler.
func RunLicenseScan(root string, extraDeny []string) (*LicenseResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	// Collect manifest matches.
	var manifest []LicenseMatch
	manifest = append(manifest, scanGoMod(absRoot)...)
	manifest = append(manifest, scanPackageJSON(absRoot)...)
	manifest = append(manifest, scanRequirementsTxt(absRoot)...)

	// Collect vendor LICENSE matches.
	vendor := scanVendorLicenses(absRoot)

	// Merge: enrich manifest entries with detected licenses.
	matches := MergeLicenseResults(manifest, vendor)

	// Build deny set.
	denySet := buildDenySet(absRoot, extraDeny)

	var denied []LicenseMatch
	for i := range matches {
		if denySet[matches[i].License] {
			denied = append(denied, matches[i])
		}
	}

	return &LicenseResult{
		Matches:     matches,
		Denied:      denied,
		Total:       len(matches),
		DeniedCount: len(denied),
	}, nil
}
