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
			// Use lenient load: accept older schema versions with a warning
			// rather than rebuilding from scratch (which loads all grammars
			// and can OOM on large repos).
			if idx, loadErr := index.LoadLenient(autoPath); loadErr == nil {
				age := time.Since(fi.ModTime()).Truncate(time.Second)
				if idx.ConfigHashes == nil {
					fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, rebuild with 'gts index build' for config tracking)\n", autoPath, age)
					return idx.ExcludePaths(excludes), false, nil
				}
				current, hashErr := index.ComputeConfigHashes(target)
				if hashErr == nil && configHashesMatch(idx.ConfigHashes, current) {
					if len(excludes) > 0 {
						fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, applying %d exclusion patterns post-load)\n", autoPath, age, len(excludes))
					} else {
						fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, pass --no-cache for fresh)\n", autoPath, age)
					}
					return idx.ExcludePaths(excludes), false, nil
				}
				fmt.Fprintf(os.Stderr, "index: config changed since last build, rebuilding...\n")
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

func configHashesMatch(cached, current map[string]string) bool {
	if len(cached) != len(current) {
		return false
	}
	for k, v := range cached {
		if current[k] != v {
			return false
		}
	}
	return true
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
