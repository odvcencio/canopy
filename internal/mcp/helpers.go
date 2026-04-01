package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

func (s *Service) loadOrBuild(cachePath string, target string) (*model.Index, error) {
	if strings.TrimSpace(cachePath) != "" {
		return index.Load(cachePath)
	}

	if strings.TrimSpace(target) == "" {
		target = s.defaultRoot
	}
	// Auto-discover cached index
	autoPath := filepath.Join(target, ".gts", "index.json")
	if _, err := os.Stat(autoPath); err == nil {
		if idx, loadErr := index.Load(autoPath); loadErr == nil {
			if idx.ConfigHashes != nil {
				current, hashErr := index.ComputeConfigHashes(target)
				if hashErr == nil && configHashesMatch(idx.ConfigHashes, current) {
					return idx, nil
				}
			}
			// ConfigHashes nil (old cache) or mismatch — fall through to rebuild.
		}
	}
	builder, err := index.NewBuilderWithWorkspaceIgnores(target)
	if err != nil {
		return nil, err
	}
	return builder.BuildPath(target)
}

func (s *Service) loadIndexFromSource(pathArg, cacheArg string) (*model.Index, error) {
	cachePath := strings.TrimSpace(cacheArg)
	if cachePath != "" {
		return index.Load(cachePath)
	}

	target := strings.TrimSpace(pathArg)
	if target == "" {
		target = s.defaultRoot
	}
	// Auto-discover cached index
	autoPath := filepath.Join(target, ".gts", "index.json")
	if _, err := os.Stat(autoPath); err == nil {
		if idx, loadErr := index.Load(autoPath); loadErr == nil {
			if idx.ConfigHashes != nil {
				current, hashErr := index.ComputeConfigHashes(target)
				if hashErr == nil && configHashesMatch(idx.ConfigHashes, current) {
					return idx, nil
				}
			}
			// ConfigHashes nil (old cache) or mismatch — fall through to rebuild.
		}
	}
	builder, err := index.NewBuilderWithWorkspaceIgnores(target)
	if err != nil {
		return nil, err
	}
	return builder.BuildPath(target)
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	value := stringArg(args, key)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	return value, nil
}

func stringArg(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *Service) stringArgOrDefault(args map[string]any, key, fallback string) string {
	value := stringArg(args, key)
	if value == "" {
		return fallback
	}
	return value
}

func intArg(args map[string]any, key string, fallback int) int {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch typed := raw.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return fallback
	}
}

func floatArg(args map[string]any, key string, fallback float64) float64 {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	switch typed := raw.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	default:
		return fallback
	}
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	value, ok := raw.(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case string:
		value := strings.TrimSpace(typed)
		if value == "" {
			return nil
		}
		return []string{value}
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			values = append(values, text)
		}
		return values
	default:
		return nil
	}
}

// applyGeneratedFilter removes generated files unless includeGenerated is true.
// If generator is non-empty, it filters to only files from that generator
// (or "human" for non-generated files).
func applyGeneratedFilter(idx *model.Index, includeGenerated bool, generator string) *model.Index {
	if generator != "" {
		return idx.FilterByGenerator(generator)
	}
	if includeGenerated {
		return idx
	}
	return idx.WithoutGenerated()
}

func compactNodeText(text string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const maxLen = 160
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "..."
}

func deadKindAllowed(definition xref.Definition, mode string) bool {
	switch mode {
	case "callable":
		return definition.Callable
	case "function":
		return definition.Kind == "function_definition"
	case "method":
		return definition.Kind == "method_definition"
	default:
		return false
	}
}

func isEntrypointDefinition(definition xref.Definition) bool {
	if definition.Kind != "function_definition" {
		return false
	}
	return definition.Name == "main" || definition.Name == "init"
}

func isTestSourceFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), "_test.go")
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
