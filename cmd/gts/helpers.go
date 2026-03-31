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
			if idx, err := index.Load(autoPath); err == nil {
				age := time.Since(fi.ModTime()).Truncate(time.Second)
				fmt.Fprintf(os.Stderr, "index: using cached %s (age %s, pass --no-cache for fresh)\n", autoPath, age)
				return idx, nil
			}
		}
	}
	builder, err := index.NewBuilderWithWorkspaceIgnores(target)
	if err != nil {
		return nil, err
	}
	return builder.BuildPath(target)
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
// --include-generated was passed.
func applyGeneratedFilter(cmd *cobra.Command, idx *model.Index) *model.Index {
	includeGenerated, _ := cmd.Flags().GetBool("include-generated")
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
