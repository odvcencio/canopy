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

func loadOrBuild(cachePath string, target string, noCache bool) (*model.Index, error) {
	if strings.TrimSpace(cachePath) != "" {
		return index.Load(cachePath)
	}
	if !noCache {
		autoPath := filepath.Join(target, ".gts", "index.json")
		if fi, err := os.Stat(autoPath); err == nil {
			if idx, loadErr := index.Load(autoPath); loadErr == nil {
				if idx.ConfigHashes == nil {
					fmt.Fprintf(os.Stderr, "index: cache predates config tracking, rebuilding...\n")
				} else {
					current, hashErr := index.ComputeConfigHashes(target)
					if hashErr == nil && configHashesMatch(idx.ConfigHashes, current) {
						age := time.Since(fi.ModTime()).Truncate(time.Second)
						fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, pass --no-cache for fresh)\n", autoPath, age)
						return idx, nil
					}
					fmt.Fprintf(os.Stderr, "index: config changed since last build, rebuilding...\n")
				}
			}
		}
	}
	builder, err := index.NewBuilderWithWorkspaceIgnores(target)
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
