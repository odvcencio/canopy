package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/capa"
)

// CycloneDX 1.5 JSON types.

type CycloneDXBOM struct {
	BOMFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	Version      int             `json:"version"`
	Metadata     BOMMetadata     `json:"metadata"`
	Components   []BOMComponent  `json:"components"`
	Dependencies []BOMDependency `json:"dependencies,omitempty"`
}

type BOMMetadata struct {
	Timestamp string    `json:"timestamp"`
	Tools     []BOMTool `json:"tools,omitempty"`
}

type BOMTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type BOMComponent struct {
	Type       string        `json:"type"`
	Name       string        `json:"name"`
	Version    string        `json:"version,omitempty"`
	Purl       string        `json:"purl,omitempty"`
	Scope      string        `json:"scope,omitempty"`
	Properties []BOMProperty `json:"properties,omitempty"`
}

type BOMProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type BOMDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

func newSBOMCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var includeCapabilities bool

	cmd := &cobra.Command{
		Use:   "sbom [path]",
		Short: "Generate CycloneDX 1.5 SBOM from structural index",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			report, err := deps.Build(idx, deps.Options{
				Mode:         "package",
				IncludeEdges: true,
			})
			if err != nil {
				return err
			}

			// Collect external (non-internal) import targets.
			externalSet := map[string]bool{}
			// Build dependency map: for each external package, track what depends on it.
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

			// Resolve versions from manifest files.
			absRoot, err := filepath.Abs(idx.Root)
			if err != nil {
				absRoot = idx.Root
			}
			versions := resolveVersions(absRoot)

			// Optionally detect capabilities.
			var capaMatches []capa.Match
			if includeCapabilities {
				capaMatches = capa.Detect(idx, capa.BuiltinRules())
			}
			capaByAPI := buildCapaIndex(capaMatches)

			components := make([]BOMComponent, 0, len(externalNames))
			for _, name := range externalNames {
				comp := BOMComponent{
					Type: "library",
					Name: name,
				}
				if ver, ok := versions[name]; ok {
					comp.Version = ver
					comp.Purl = buildPURL(name, ver)
				}
				comp.Scope = "required"

				if includeCapabilities {
					if tags, ok := capaByAPI[name]; ok {
						for _, tag := range tags {
							comp.Properties = append(comp.Properties, BOMProperty{
								Name:  "gts:capability",
								Value: tag,
							})
						}
					}
				}

				components = append(components, comp)
			}

			// Build dependency entries: one per internal node with external deps.
			var bomDeps []BOMDependency
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
				bomDeps = append(bomDeps, BOMDependency{
					Ref:       node,
					DependsOn: refs,
				})
			}

			bom := CycloneDXBOM{
				BOMFormat:   "CycloneDX",
				SpecVersion: "1.5",
				Version:     1,
				Metadata: BOMMetadata{
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Tools: []BOMTool{
						{Name: "gts", Version: "0.13.1"},
					},
				},
				Components:   components,
				Dependencies: bomDeps,
			}

			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(bom)
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&includeCapabilities, "include-capabilities", false, "enrich components with capability tags from capa detection")
	return cmd
}

// resolveVersions collects dependency versions from go.mod, package.json, and requirements.txt.
func resolveVersions(root string) map[string]string {
	versions := map[string]string{}
	parseGoMod(filepath.Join(root, "go.mod"), versions)
	parsePackageJSON(filepath.Join(root, "package.json"), versions)
	parseRequirementsTxt(filepath.Join(root, "requirements.txt"), versions)
	return versions
}

// parseGoMod extracts module versions from go.mod require blocks.
func parseGoMod(path string, versions map[string]string) {
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
			// Single-line require: require module/path v1.2.3
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 2 {
				mod := parts[0]
				ver := parts[1]
				versions[mod] = ver
			}
			continue
		}
		if inRequire {
			// Lines inside require block: module/path v1.2.3
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				mod := parts[0]
				ver := parts[1]
				versions[mod] = ver
			}
		}
	}
}

// parsePackageJSON extracts versions from dependencies and devDependencies.
func parsePackageJSON(path string, versions map[string]string) {
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

// parseRequirementsTxt extracts versions from pip requirements (pkg==version format).
func parseRequirementsTxt(path string, versions map[string]string) {
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

// buildPURL constructs a Package URL from an import name and version.
func buildPURL(name, version string) string {
	// Heuristic: Go modules typically contain dots in the first path element.
	parts := strings.SplitN(name, "/", 2)
	if len(parts) > 0 && strings.Contains(parts[0], ".") {
		// Go module: pkg:golang/module@version
		return fmt.Sprintf("pkg:golang/%s@%s", name, version)
	}
	// npm-style: pkg:npm/name@version
	return fmt.Sprintf("pkg:npm/%s@%s", name, version)
}

// buildCapaIndex creates a map from API name to capability tags.
func buildCapaIndex(matches []capa.Match) map[string][]string {
	result := map[string][]string{}
	for _, m := range matches {
		tag := m.Rule.Category
		if m.Rule.AttackID != "" {
			tag = fmt.Sprintf("%s (%s)", m.Rule.Category, m.Rule.AttackID)
		}
		for _, api := range m.MatchedAPIs {
			// Check if tag already present.
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
