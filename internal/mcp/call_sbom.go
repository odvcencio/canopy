package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/capa"
)

func (s *Service) callSBOM(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	report, err := deps.Build(idx, deps.Options{
		Mode:         "package",
		IncludeEdges: true,
	})
	if err != nil {
		return nil, err
	}

	// Collect external import targets.
	externalSet := map[string]bool{}
	depGraph := map[string]map[string]bool{}
	for _, edge := range report.Edges {
		if edge.Internal {
			continue
		}
		externalSet[edge.To] = true
		if depGraph[edge.From] == nil {
			depGraph[edge.From] = map[string]bool{}
		}
		depGraph[edge.From][edge.To] = true
	}

	externalNames := make([]string, 0, len(externalSet))
	for name := range externalSet {
		externalNames = append(externalNames, name)
	}
	sort.Strings(externalNames)

	// Resolve versions.
	absRoot, err := filepath.Abs(idx.Root)
	if err != nil {
		absRoot = idx.Root
	}
	versions := sbomResolveVersions(absRoot)

	// Optionally detect capabilities.
	includeCapa := boolArg(args, "include_capabilities", false)
	var capaMatches []capa.Match
	if includeCapa {
		capaMatches = capa.Detect(idx, capa.BuiltinRules())
	}
	capaByAPI := sbomBuildCapaIndex(capaMatches)

	type bomProperty struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type bomComponent struct {
		Type       string        `json:"type"`
		Name       string        `json:"name"`
		Version    string        `json:"version,omitempty"`
		Purl       string        `json:"purl,omitempty"`
		Scope      string        `json:"scope,omitempty"`
		Properties []bomProperty `json:"properties,omitempty"`
	}
	type bomDependency struct {
		Ref       string   `json:"ref"`
		DependsOn []string `json:"dependsOn,omitempty"`
	}
	type bomTool struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	type bomMetadata struct {
		Timestamp string    `json:"timestamp"`
		Tools     []bomTool `json:"tools,omitempty"`
	}
	type cycloneDXBOM struct {
		BOMFormat    string          `json:"bomFormat"`
		SpecVersion  string          `json:"specVersion"`
		Version      int             `json:"version"`
		Metadata     bomMetadata     `json:"metadata"`
		Components   []bomComponent  `json:"components"`
		Dependencies []bomDependency `json:"dependencies,omitempty"`
	}

	components := make([]bomComponent, 0, len(externalNames))
	for _, name := range externalNames {
		comp := bomComponent{
			Type:  "library",
			Name:  name,
			Scope: "required",
		}
		if ver, ok := versions[name]; ok {
			comp.Version = ver
			comp.Purl = sbomBuildPURL(name, ver)
		}
		if includeCapa {
			if tags, ok := capaByAPI[name]; ok {
				for _, tag := range tags {
					comp.Properties = append(comp.Properties, bomProperty{
						Name:  "gts:capability",
						Value: tag,
					})
				}
			}
		}
		components = append(components, comp)
	}

	var bomDeps []bomDependency
	internalNodes := make([]string, 0, len(depGraph))
	for node := range depGraph {
		internalNodes = append(internalNodes, node)
	}
	sort.Strings(internalNodes)
	for _, node := range internalNodes {
		targets := depGraph[node]
		refs := make([]string, 0, len(targets))
		for t := range targets {
			refs = append(refs, t)
		}
		sort.Strings(refs)
		bomDeps = append(bomDeps, bomDependency{
			Ref:       node,
			DependsOn: refs,
		})
	}

	bom := cycloneDXBOM{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: bomMetadata{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Tools:     []bomTool{{Name: "gts", Version: "0.13.1"}},
		},
		Components:   components,
		Dependencies: bomDeps,
	}

	return bom, nil
}

// sbomResolveVersions collects dependency versions from manifest files.
func sbomResolveVersions(root string) map[string]string {
	versions := map[string]string{}
	sbomParseGoMod(filepath.Join(root, "go.mod"), versions)
	sbomParsePackageJSON(filepath.Join(root, "package.json"), versions)
	sbomParseRequirementsTxt(filepath.Join(root, "requirements.txt"), versions)
	return versions
}

func sbomParseGoMod(path string, versions map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inRequire := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "require (") || strings.HasPrefix(line, "require(") {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 2 {
				versions[parts[0]] = parts[1]
			}
			continue
		}
		if inRequire {
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				versions[parts[0]] = parts[1]
			}
		}
	}
}

func sbomParsePackageJSON(path string, versions map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return
	}
	for name, ver := range pkg.Dependencies {
		versions[name] = ver
	}
	for name, ver := range pkg.DevDependencies {
		if _, exists := versions[name]; !exists {
			versions[name] = ver
		}
	}
}

func sbomParseRequirementsTxt(path string, versions map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if idx := strings.Index(line, "=="); idx > 0 {
			pkg := strings.TrimSpace(line[:idx])
			ver := strings.TrimSpace(line[idx+2:])
			if pkg != "" && ver != "" {
				versions[pkg] = ver
			}
		}
	}
}

func sbomBuildPURL(name, version string) string {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) > 0 && strings.Contains(parts[0], ".") {
		return fmt.Sprintf("pkg:golang/%s@%s", name, version)
	}
	return fmt.Sprintf("pkg:npm/%s@%s", name, version)
}

func sbomBuildCapaIndex(matches []capa.Match) map[string][]string {
	result := map[string][]string{}
	for _, m := range matches {
		tag := m.Rule.Category
		if m.Rule.AttackID != "" {
			tag = fmt.Sprintf("%s (%s)", m.Rule.Category, m.Rule.AttackID)
		}
		for _, api := range m.MatchedAPIs {
			found := false
			for _, existing := range result[api] {
				if existing == tag {
					found = true
					break
				}
			}
			if !found {
				result[api] = append(result[api], tag)
			}
		}
	}
	return result
}
