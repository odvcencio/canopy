package mcp

import (
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/canopy/pkg/roots"
	"m31labs.dev/canopy/pkg/xref"
)

func (s *Service) callDead(args map[string]any) (any, error) {
	mode := strings.ToLower(strings.TrimSpace(s.stringArgOrDefault(args, "kind", "callable")))
	switch mode {
	case "callable", "function", "method":
	default:
		return nil, fmt.Errorf("unsupported kind %q (expected callable|function|method)", mode)
	}

	includeEntrypoints := boolArg(args, "include_entrypoints", false)
	includeTests := boolArg(args, "include_tests", false)
	exportedRoots := boolArg(args, "exported_roots", false)
	extraAnnotations := stringSliceArg(args, "profile_annotations")
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}

	// Compute call-resolution rate for confidence scoring.
	resolutionRate := roots.ResolutionRate(graph)

	// Build the language-aware root analyzer, merging an optional
	// .canopyroots.json discovered at the index root over the built-in profiles.
	rootsCfg, err := roots.LoadConfig(graph.Root)
	if err != nil {
		return nil, err
	}
	analyzer, err := roots.NewWithConfig(graph.Root, roots.Options{
		TreatExportedAsRoots: exportedRoots,
		ExtraRootAnnotations: extraAnnotations,
	}, rootsCfg)
	if err != nil {
		return nil, err
	}

	type deadMatch struct {
		File       string `json:"file"`
		Package    string `json:"package"`
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Signature  string `json:"signature,omitempty"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		Incoming   int    `json:"incoming"`
		Outgoing   int    `json:"outgoing"`
		Confidence string `json:"confidence,omitempty"`
	}

	matches := make([]deadMatch, 0, 64)
	scanned := 0
	for _, definition := range graph.Definitions {
		if !deadKindAllowed(definition, mode) {
			continue
		}

		isRoot, reason := analyzer.IsRoot(definition, graph.Definitions)

		if isRoot {
			// Entrypoints: skip unless include_entrypoints is set.
			if reason == "entrypoint" && !includeEntrypoints {
				continue
			}
			// Tests: skip unless include_tests is set.
			if reason == "test" && !includeTests {
				continue
			}
			// All other roots (framework callbacks, annotations, etc.) are
			// always suppressed — they cannot be dead by definition.
			if reason != "entrypoint" && reason != "test" {
				continue
			}
		}

		scanned++
		incoming := graph.IncomingCount(definition.ID)
		if incoming > 0 {
			continue
		}
		conf := roots.DeadConfidence(resolutionRate, definition)
		matches = append(matches, deadMatch{
			File:       definition.File,
			Package:    definition.Package,
			Kind:       definition.Kind,
			Name:       definition.Name,
			Signature:  definition.Signature,
			StartLine:  definition.StartLine,
			EndLine:    definition.EndLine,
			Incoming:   incoming,
			Outgoing:   graph.OutgoingCount(definition.ID),
			Confidence: string(conf),
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			if matches[i].StartLine == matches[j].StartLine {
				return matches[i].Name < matches[j].Name
			}
			return matches[i].StartLine < matches[j].StartLine
		}
		return matches[i].File < matches[j].File
	})

	return map[string]any{
		"kind":            mode,
		"scanned":         scanned,
		"count":           len(matches),
		"resolution_rate": resolutionRate,
		"matches":         matches,
	}, nil
}
