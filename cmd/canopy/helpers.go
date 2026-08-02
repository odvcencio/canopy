package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"m31labs.dev/canopy/pkg/generated"
	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

// cmdExcludes pulls the root-level persistent --exclude flag off any command
// (each subcommand inherits the flag via PersistentFlags). Returns nil when
// the flag is unset so callers can test with len(...) == 0.
func cmdExcludes(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	patterns, _ := cmd.Flags().GetStringSlice("exclude")
	return patterns
}

func loadOrBuild(cmd *cobra.Command, cachePath string, target string, noCache bool) (*model.Index, error) {
	idx, changedScoped, err := loadOrBuildWithScope(cmd, cachePath, target, noCache, nil)
	setAnalysisIndexScope(cmd, changedScoped)
	return idx, err
}

func loadOrBuildCheck(cmd *cobra.Command, cachePath string, target string, noCache bool, base string) (*model.Index, error) {
	if strings.TrimSpace(base) == "" {
		return loadOrBuild(cmd, cachePath, target, noCache)
	}
	changedSet, err := changedFiles(base, target)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(changedSet))
	for path := range changedSet {
		changed = append(changed, path)
	}
	idx, _, err := loadOrBuildChanged(cmd, cachePath, target, noCache, changed)
	return idx, err
}

func loadOrBuildChanged(cmd *cobra.Command, cachePath string, target string, noCache bool, changed []string) (*model.Index, bool, error) {
	idx, changedScoped, err := loadOrBuildWithScope(cmd, cachePath, target, noCache, changed)
	setAnalysisIndexScope(cmd, changedScoped)
	return idx, changedScoped, err
}

const analysisIndexScopeAnnotation = "canopy.analysis.index-scope"

func setAnalysisIndexScope(cmd *cobra.Command, changedScoped bool) {
	if cmd == nil {
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[analysisIndexScopeAnnotation] = "repository"
	if changedScoped {
		cmd.Annotations[analysisIndexScopeAnnotation] = "changed"
	}
}

func analysisIndexScope(cmd *cobra.Command) string {
	if cmd != nil && cmd.Annotations != nil {
		if scope := cmd.Annotations[analysisIndexScopeAnnotation]; scope != "" {
			return scope
		}
	}
	return "repository"
}

func loadOrBuildWithScope(cmd *cobra.Command, cachePath string, target string, noCache bool, changed []string) (*model.Index, bool, error) {
	excludes := cmdExcludes(cmd)

	if strings.TrimSpace(cachePath) != "" {
		idx, err := index.Load(cachePath)
		if err != nil {
			return nil, false, err
		}
		return idx.ExcludePaths(excludes), false, nil
	}

	if !noCache {
		autoPath := filepath.Join(target, ".canopy", "index.json")
		if fi, err := os.Stat(autoPath); err == nil {
			if idx, loadErr := index.LoadLenient(autoPath); loadErr == nil {
				builder, buildErr := index.NewBuilderWithWorkspaceIgnores(target)
				if buildErr != nil {
					return nil, false, buildErr
				}
				idx, status, refreshErr := builder.EnsureFreshCache(cmd.Context(), target, autoPath, idx)
				if refreshErr != nil {
					return nil, false, refreshErr
				}
				age := time.Since(fi.ModTime()).Truncate(time.Second)
				switch status {
				case index.CacheIncrementallyRefreshed:
					fmt.Fprintf(os.Stderr, "index: refreshed stale cache %s\n", autoPath)
				case index.CacheFullyRebuilt:
					fmt.Fprintf(os.Stderr, "index: rebuilt cache %s after configuration or root change\n", autoPath)
				default:
					if len(excludes) > 0 {
						fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, applying %d exclusion patterns post-load)\n", autoPath, age, len(excludes))
					} else {
						fmt.Fprintf(os.Stderr, "index: using fresh cache %s (age %s)\n", autoPath, age)
					}
				}
				return idx.ExcludePaths(excludes), false, nil
			}
		}
	}

	builder, err := index.NewBuilderWithWorkspaceIgnoresAndExtras(target, excludes)
	if err != nil {
		return nil, false, err
	}
	if changed != nil {
		fmt.Fprintf(os.Stderr, "index: no usable cache; building a changed-only snapshot for %d paths\n", len(changed))
		idx, buildErr := builder.BuildPaths(cmd.Context(), target, changed)
		return idx, true, buildErr
	}
	idx, _, err := builder.BuildPathIncremental(cmd.Context(), target, nil)
	return idx, false, err
}

func emitJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func compactNodeText(text string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const maxLen = 160
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "..."
}

func symbolLabel(name, signature string) string {
	if strings.TrimSpace(signature) != "" {
		return signature
	}
	return name
}

func definitionLabel(definition xref.Definition) string {
	if strings.TrimSpace(definition.Signature) != "" {
		return definition.Signature
	}
	return definition.Name
}

// applyGeneratedFilter removes generated files from the index unless
// --include-generated was passed. If --generator is set, it filters to
// only files from that generator (or "human" for non-generated files).
func applyGeneratedFilter(cmd *cobra.Command, idx *model.Index) *model.Index {
	idx = generated.ClassifyMinifiedBundles(idx)
	generator, _ := cmd.Flags().GetString("generator")
	includeGenerated, _ := cmd.Flags().GetBool("include-generated")
	if generator != "" {
		return idx.FilterByGenerator(generator)
	}
	if includeGenerated {
		return idx
	}
	return idx.WithoutGenerated()
}

// generatedFileMap builds a path → GeneratedInfo lookup from the index.
func generatedFileMap(idx *model.Index) map[string]*model.GeneratedInfo {
	m := make(map[string]*model.GeneratedInfo, len(idx.Files))
	for i := range idx.Files {
		if idx.Files[i].Generated != nil {
			m[idx.Files[i].Path] = idx.Files[i].Generated
		}
	}
	return m
}
