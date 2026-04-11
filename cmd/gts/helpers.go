package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/xref"
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
	excludes := cmdExcludes(cmd)

	// --exclude bypasses the cache. A cached index was built without the
	// caller's exclusion patterns in effect, so loading it would leak
	// excluded files into analysis output (phantom results). Simplest
	// correct behavior is to build fresh when --exclude is set; if that
	// becomes a perf issue we can hash the pattern list into the cache key.
	if len(excludes) == 0 {
		if strings.TrimSpace(cachePath) != "" {
			return index.Load(cachePath)
		}
		if !noCache {
			autoPath := filepath.Join(target, ".gts", "index.json")
			if fi, err := os.Stat(autoPath); err == nil {
				if idx, loadErr := index.Load(autoPath); loadErr == nil {
					age := time.Since(fi.ModTime()).Truncate(time.Second)
					if idx.ConfigHashes == nil {
						// Old cache without config tracking — use it but suggest rebuild
						fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, rebuild with 'gts index build' for config tracking)\n", autoPath, age)
						return idx, nil
					}
					current, hashErr := index.ComputeConfigHashes(target)
					if hashErr == nil && configHashesMatch(idx.ConfigHashes, current) {
						fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, pass --no-cache for fresh)\n", autoPath, age)
						return idx, nil
					}
					fmt.Fprintf(os.Stderr, "index: config changed since last build, rebuilding...\n")
				}
			}
		}
	} else if strings.TrimSpace(cachePath) != "" || !noCache {
		// Tell the user we're ignoring their cache preference on purpose so a
		// slower run isn't surprising.
		fmt.Fprintf(os.Stderr, "index: --exclude set, bypassing cache (rebuilding fresh)\n")
	}

	builder, err := index.NewBuilderWithWorkspaceIgnoresAndExtras(target, excludes)
	if err != nil {
		return nil, err
	}
	return builder.BuildPath(target)
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
