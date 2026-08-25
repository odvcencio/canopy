package mcp

import (
	"fmt"
	"path/filepath"
	"strings"

	"m31labs.dev/canopy/internal/indexcoverage"
	"m31labs.dev/canopy/pkg/boundaries"
	"m31labs.dev/canopy/pkg/complexity"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

type guardrailResult struct {
	File         string              `json:"file"`
	Generated    guardrailGen        `json:"generated"`
	Boundary     guardrailBoundary   `json:"boundary"`
	Complexity   guardrailComplex    `json:"complexity"`
	ParserHealth model.ParseCoverage `json:"parser_health"`
	Warnings     []string            `json:"warnings"`
}

type guardrailGen struct {
	Is        bool   `json:"is"`
	Generator string `json:"generator,omitempty"`
}

type guardrailBoundary struct {
	Module     string `json:"module"`
	HasConfig  bool   `json:"has_config"`
	Violations int    `json:"violations,omitempty"`
}

type guardrailComplex struct {
	MaxCyclomatic int  `json:"max_cyclomatic"`
	MaxCognitive  int  `json:"max_cognitive"`
	Hotspot       bool `json:"hotspot"`
}

func (s *Service) callGuardrails(args map[string]any) (any, error) {
	filePath, err := requiredStringArg(args, "file")
	if err != nil {
		return nil, err
	}

	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}

	// Normalize file path to match index entries.
	relPath := filePath
	if filepath.IsAbs(relPath) && target != "" {
		if rel, relErr := filepath.Rel(target, relPath); relErr == nil && !strings.HasPrefix(rel, "..") {
			relPath = filepath.ToSlash(rel)
		}
	}
	relPath = filepath.ToSlash(filepath.Clean(relPath))

	// Find file in index.
	fileIdx := -1
	for i, f := range idx.Files {
		if filepath.ToSlash(filepath.Clean(f.Path)) == relPath {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		return nil, fmt.Errorf("file %q not found in index", filePath)
	}
	file := idx.Files[fileIdx]

	result := guardrailResult{
		File:         file.Path,
		ParserHealth: indexcoverage.Normalize(file.ParseCoverage),
	}
	appendParserHealthWarnings(&result)

	// 1. Generated check.
	if file.Generated != nil {
		result.Generated = guardrailGen{Is: true, Generator: file.Generated.Generator}
		result.Warnings = append(result.Warnings, fmt.Sprintf("generated file (generator: %s) - edits may be overwritten", file.Generated.Generator))
	}

	// 2. Boundary module.
	dir := filepath.Dir(file.Path)
	if dir == "." {
		dir = "."
	}
	result.Boundary.Module = filepath.ToSlash(dir)

	cfg, cfgErr := boundaries.LoadConfig(target)
	if cfgErr == nil && cfg != nil && len(cfg.Rules) > 0 {
		result.Boundary.HasConfig = true
	}

	// 3. Complexity analysis for functions in this file.
	// Build a sub-index with just this file for complexity analysis.
	subIdx := *idx
	subIdx.Files = idx.Files[fileIdx : fileIdx+1]

	report, compErr := complexity.Analyze(&subIdx, idx.Root, complexity.Options{})
	if compErr == nil && report != nil {
		maxCyc := 0
		maxCog := 0
		for _, fn := range report.Functions {
			if fn.Cyclomatic > maxCyc {
				maxCyc = fn.Cyclomatic
			}
			if fn.Cognitive > maxCog {
				maxCog = fn.Cognitive
			}
		}
		result.Complexity.MaxCyclomatic = maxCyc
		result.Complexity.MaxCognitive = maxCog
		if maxCyc >= 15 || maxCog >= 30 {
			result.Complexity.Hotspot = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("high complexity (max cyclomatic=%d, max cognitive=%d) - consider refactoring", maxCyc, maxCog))
		}
	}

	// 4. Fan-in analysis via xref.
	graph, xrefErr := xref.Build(idx)
	if xrefErr == nil {
		maxFanIn := 0
		for _, def := range graph.Definitions {
			if def.File != file.Path {
				continue
			}
			if !def.Callable {
				continue
			}
			fanIn := graph.IncomingCount(def.ID)
			if fanIn > maxFanIn {
				maxFanIn = fanIn
			}
		}
		if maxFanIn >= 10 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("high fan-in (%d callers) - changes here have wide blast radius", maxFanIn))
		}
	}

	return result, nil
}

func appendParserHealthWarnings(result *guardrailResult) {
	if result == nil {
		return
	}
	coverage := result.ParserHealth
	switch coverage.Status {
	case model.ParseCoverageStopped:
		reason := coverage.StopReason
		if reason == "" {
			reason = "unspecified reason"
		}
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("parser stopped early (%s) - structural results may omit code", reason))
	case model.ParseCoveragePartial:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("parser recovered with %d unresolved regions - structural results may be incomplete", len(coverage.Gaps)))
	case model.ParseCoverageUnknown:
		result.Warnings = append(result.Warnings,
			"parser coverage receipt is missing - structural results are unverified")
	}
	if coverage.Truncated {
		result.Warnings = append(result.Warnings,
			"parser gap details were truncated - inspect the coverage report before editing")
	}
}
