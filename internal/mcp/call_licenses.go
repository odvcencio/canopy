package mcp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odvcencio/gts-suite/internal/lint"
)

// licenseMatch mirrors the CLI LicenseMatch type for MCP output.
type licenseMatch struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
	License string `json:"license"`
	Source  string `json:"source"`
	File   string `json:"file,omitempty"`
}

// mcpSPDXPatterns maps SPDX IDs to distinguishing phrases.
var mcpSPDXPatterns = map[string]*regexp.Regexp{
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

func (s *Service) callLicenses(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	extraDeny := stringSliceArg(args, "deny")

	// Collect manifest matches.
	var manifest []licenseMatch
	manifest = append(manifest, mcpScanGoMod(absTarget)...)
	manifest = append(manifest, mcpScanPackageJSON(absTarget)...)
	manifest = append(manifest, mcpScanRequirementsTxt(absTarget)...)

	// Collect vendor LICENSE matches.
	vendor := mcpScanVendorLicenses(absTarget)

	// Merge.
	matches := mcpMergeLicenseResults(manifest, vendor)

	// Build deny set.
	denySet := mcpBuildDenySet(absTarget, extraDeny)

	var denied []licenseMatch
	for i := range matches {
		if denySet[matches[i].License] {
			denied = append(denied, matches[i])
		}
	}

	status := "PASS"
	if len(denied) > 0 {
		status = "FAIL"
	}
	return map[string]any{
		"status":       status,
		"total":        len(matches),
		"denied_count": len(denied),
		"matches":      matches,
		"denied":       denied,
	}, nil
}

func mcpBuildDenySet(root string, cliDeny []string) map[string]bool {
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

func mcpScanGoMod(root string) []licenseMatch {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var matches []licenseMatch
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
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				matches = append(matches, licenseMatch{
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
				matches = append(matches, licenseMatch{
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

var mcpJSONDepPattern = regexp.MustCompile(`"([^"]+)"\s*:\s*"([^"]+)"`)

func mcpScanPackageJSON(root string) []licenseMatch {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var matches []licenseMatch
	content := string(data)
	for _, section := range []string{"dependencies", "devDependencies", "peerDependencies"} {
		matches = append(matches, mcpExtractJSONDeps(content, section)...)
	}
	return matches
}

func mcpExtractJSONDeps(content, section string) []licenseMatch {
	marker := fmt.Sprintf(`"%s"`, section)
	idx := strings.Index(content, marker)
	if idx < 0 {
		return nil
	}
	rest := content[idx+len(marker):]
	braceIdx := strings.Index(rest, "{")
	if braceIdx < 0 {
		return nil
	}
	rest = rest[braceIdx:]
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
	var matches []licenseMatch
	for _, m := range mcpJSONDepPattern.FindAllStringSubmatch(block, -1) {
		matches = append(matches, licenseMatch{
			Package: m[1],
			Version: m[2],
			License: "unknown",
			Source:  "package.json",
		})
	}
	return matches
}

func mcpScanRequirementsTxt(root string) []licenseMatch {
	path := filepath.Join(root, "requirements.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var matches []licenseMatch
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
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
		if bracket := strings.Index(pkg, "["); bracket > 0 {
			pkg = pkg[:bracket]
		}
		matches = append(matches, licenseMatch{
			Package: pkg,
			Version: version,
			License: "unknown",
			Source:  "requirements.txt",
		})
	}
	return matches
}

func mcpScanVendorLicenses(root string) []licenseMatch {
	vendorDir := filepath.Join(root, "vendor")
	info, err := os.Stat(vendorDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	var matches []licenseMatch
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
		spdx := mcpIdentifyLicense(content)
		rel, relErr := filepath.Rel(vendorDir, filepath.Dir(path))
		if relErr != nil {
			rel = filepath.Dir(path)
		}
		rel = filepath.ToSlash(rel)
		matches = append(matches, licenseMatch{
			Package: rel,
			License: spdx,
			Source:  "LICENSE file",
			File:    path,
		})
		return nil
	})
	return matches
}

func mcpIdentifyLicense(content string) string {
	order := []string{
		"BSD-3-Clause", "BSD-2-Clause",
		"Apache-2.0", "MIT", "ISC",
		"GPL-3.0", "GPL-2.0", "AGPL-3.0", "LGPL-2.1",
		"MPL-2.0", "Unlicense",
	}
	for _, id := range order {
		if pat, ok := mcpSPDXPatterns[id]; ok && pat.MatchString(content) {
			return id
		}
	}
	return "unknown"
}

func mcpMergeLicenseResults(manifest, vendor []licenseMatch) []licenseMatch {
	vendorMap := make(map[string]licenseMatch, len(vendor))
	for _, v := range vendor {
		vendorMap[v.Package] = v
	}
	merged := make([]licenseMatch, 0, len(manifest)+len(vendor))
	seen := make(map[string]bool, len(manifest))
	for _, m := range manifest {
		if v, ok := vendorMap[m.Package]; ok && m.License == "unknown" {
			m.License = v.License
			m.File = v.File
		}
		merged = append(merged, m)
		seen[m.Package] = true
	}
	for _, v := range vendor {
		if !seen[v.Package] {
			merged = append(merged, v)
		}
	}
	return merged
}
