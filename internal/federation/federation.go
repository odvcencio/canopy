// Package federation loads and merges exported structural indexes from multiple repositories.
package federation

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/canopy/pkg/model"
)

// ExportedIndex is the portable envelope written by "canopy index export".
type ExportedIndex struct {
	RepoURL    string      `json:"repo_url,omitempty"`
	RepoName   string      `json:"repo_name"`
	CommitSHA  string      `json:"commit_sha,omitempty"`
	ExportedAt time.Time   `json:"exported_at"`
	Index      model.Index `json:"index"`
}

// ExportedEntry pairs a repo name with its deserialized index.
type ExportedEntry struct {
	RepoName string
	Index    *model.Index
}

// FederatedIndex holds multiple repo indexes and a merged view.
type FederatedIndex struct {
	Indexes []ExportedEntry
	Merged  *model.Index // merged index with repo-prefixed paths
}

// ServiceEdge represents a dependency from one repo to another.
type ServiceEdge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	ImportPath string `json:"import_path"`
}

// ServiceReport is the output of repo-to-repo dependency analysis.
type ServiceReport struct {
	Repos []string      `json:"repos"`
	Edges []ServiceEdge `json:"edges"`
}

// Save writes an ExportedIndex as gzipped JSON.
func Save(path string, exported *ExportedIndex) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	enc := json.NewEncoder(gw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(exported); err != nil {
		return err
	}
	return gw.Close()
}

// LoadFile reads a single .canopyindex (gzipped JSON) file.
func LoadFile(path string) (*ExportedIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", path, err)
	}
	defer gr.Close()

	var exported ExportedIndex
	if err := json.NewDecoder(gr).Decode(&exported); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &exported, nil
}

// Load reads all .canopyindex files from a directory.
func Load(dir string) (*FederatedIndex, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.canopyindex"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no .canopyindex files found in %s", dir)
	}
	sort.Strings(matches)

	fi := &FederatedIndex{}
	for _, path := range matches {
		exported, err := LoadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		fi.Indexes = append(fi.Indexes, ExportedEntry{
			RepoName: exported.RepoName,
			Index:    &exported.Index,
		})
	}
	fi.Merged = fi.merge()
	return fi, nil
}

// merge creates a unified index with repo-prefixed file paths.
func (fi *FederatedIndex) merge() *model.Index {
	merged := &model.Index{
		Version:     "federated",
		GeneratedAt: time.Now(),
	}

	for _, entry := range fi.Indexes {
		prefix := entry.RepoName + ":"
		for _, file := range entry.Index.Files {
			clone := file
			clone.Path = prefix + file.Path
			// Prefix symbol files too.
			prefixed := make([]model.Symbol, len(clone.Symbols))
			for i, sym := range clone.Symbols {
				sym.File = prefix + sym.File
				prefixed[i] = sym
			}
			clone.Symbols = prefixed
			// Prefix reference files.
			prefixedRefs := make([]model.Reference, len(clone.References))
			for i, ref := range clone.References {
				ref.File = prefix + ref.File
				prefixedRefs[i] = ref
			}
			clone.References = prefixedRefs
			merged.Files = append(merged.Files, clone)
		}
		for _, pe := range entry.Index.Errors {
			merged.Errors = append(merged.Errors, model.ParseError{
				Path:  prefix + pe.Path,
				Error: pe.Error,
			})
		}
	}

	return merged
}

// BuildServiceGraph analyzes cross-repo dependencies from import edges.
// It matches imports against known module paths of each repo to determine
// repo-to-repo edges.
func BuildServiceGraph(fi *FederatedIndex) ServiceReport {
	// Collect module paths per repo from go.mod-style analysis of the indexes.
	// We use the index Root field (often the module path) and the repo name.
	repoModules := map[string]string{} // module path -> repo name
	var repos []string
	for _, entry := range fi.Indexes {
		repos = append(repos, entry.RepoName)
		// The index root often contains the repo root path, but for module
		// detection we check if any file imports look like internal paths.
		// Use a heuristic: find the most common import prefix among internal files.
		mod := detectModule(entry.Index)
		if mod != "" {
			repoModules[mod] = entry.RepoName
		}
	}
	sort.Strings(repos)

	edgeSet := map[string]ServiceEdge{}
	for _, entry := range fi.Indexes {
		fromRepo := entry.RepoName
		fromMod := detectModule(entry.Index)
		for _, file := range entry.Index.Files {
			for _, imp := range file.Imports {
				imp = strings.TrimSpace(imp)
				if imp == "" {
					continue
				}
				// Skip self-imports.
				if fromMod != "" && (imp == fromMod || strings.HasPrefix(imp, fromMod+"/")) {
					continue
				}
				// Check if the import matches any other repo's module.
				for mod, toRepo := range repoModules {
					if toRepo == fromRepo {
						continue
					}
					if imp == mod || strings.HasPrefix(imp, mod+"/") {
						key := fromRepo + "->" + toRepo + ":" + imp
						edgeSet[key] = ServiceEdge{
							From:       fromRepo,
							To:         toRepo,
							ImportPath: imp,
						}
					}
				}
			}
		}
	}

	edges := make([]ServiceEdge, 0, len(edgeSet))
	for _, e := range edgeSet {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].ImportPath < edges[j].ImportPath
	})

	return ServiceReport{
		Repos: repos,
		Edges: edges,
	}
}

// detectModule attempts to extract the Go module path from the index.
// It looks at import paths used by files in the index to find a common module prefix.
func detectModule(idx *model.Index) string {
	if idx == nil {
		return ""
	}

	// Check the root-based go.mod if it exists.
	if idx.Root != "" {
		mod := moduleFromGoMod(idx.Root)
		if mod != "" {
			return mod
		}
	}

	// Fallback: scan imports for a self-referencing pattern.
	// Find all directory paths of source files, and see which import prefixes
	// map to those directories.
	fileDirs := map[string]bool{}
	for _, f := range idx.Files {
		d := filepath.ToSlash(filepath.Dir(f.Path))
		fileDirs[d] = true
	}

	// Count import prefixes that match file directories.
	candidates := map[string]int{}
	for _, f := range idx.Files {
		for _, imp := range f.Imports {
			imp = strings.TrimSpace(imp)
			if imp == "" {
				continue
			}
			// Check each possible prefix: strip one path component at a time.
			parts := strings.Split(imp, "/")
			for end := len(parts); end >= 2; end-- {
				prefix := strings.Join(parts[:end], "/")
				suffix := strings.Join(parts[end:], "/")
				if suffix == "" {
					// The import IS the prefix; check if "." is a file dir.
					if fileDirs["."] || fileDirs[""] {
						candidates[prefix]++
					}
				} else if fileDirs[suffix] {
					candidates[prefix]++
				}
			}
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Pick the most common prefix.
	best := ""
	bestCount := 0
	for prefix, count := range candidates {
		if count > bestCount || (count == bestCount && prefix < best) {
			best = prefix
			bestCount = count
		}
	}
	return best
}

// moduleFromGoMod reads the module directive from a go.mod file at root.
func moduleFromGoMod(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			return strings.Trim(mod, `"`)
		}
	}
	return ""
}
