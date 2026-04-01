package mcp

import (
	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/boundaries"
	"github.com/odvcencio/gts-suite/pkg/capa"
	"github.com/odvcencio/gts-suite/pkg/complexity"
	"github.com/odvcencio/gts-suite/pkg/hotspot"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

type mcpReportResult struct {
	Files        int            `json:"files"`
	Languages    map[string]int `json:"languages"`
	TotalSymbols int            `json:"total_symbols"`
	GeneratedPct int            `json:"generated_pct"`

	FunctionCount int `json:"function_count"`
	CyclomaticMax int `json:"cyclomatic_max"`
	CyclomaticP90 int `json:"cyclomatic_p90"`
	CognitiveMax  int `json:"cognitive_max"`

	BoundaryViolations int `json:"boundary_violations"`
	ImportCycles       int `json:"import_cycles"`

	Capabilities  int `json:"capabilities"`
	DeadFunctions int `json:"dead_functions"`

	Hotspots []mcpHotspotEntry `json:"hotspots,omitempty"`
}

type mcpHotspotEntry struct {
	File       string  `json:"file"`
	Name       string  `json:"name"`
	Cyclomatic int     `json:"cyclomatic"`
	Score      float64 `json:"score"`
}

func (s *Service) callReport(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	analysisIdx := applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	rpt := mcpReportResult{
		Languages: make(map[string]int),
	}

	// Codebase overview
	rpt.Files = len(idx.Files)
	for _, f := range idx.Files {
		lang := f.Language
		if lang == "" {
			lang = "unknown"
		}
		rpt.Languages[lang]++
	}
	for _, f := range idx.Files {
		rpt.TotalSymbols += len(f.Symbols)
	}
	totalFiles := idx.FileCount()
	genFiles := idx.GeneratedFileCount()
	if totalFiles > 0 {
		rpt.GeneratedPct = genFiles * 100 / totalFiles
	}

	// Complexity
	complexityReport, complexityErr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{})
	if complexityErr == nil {
		rpt.FunctionCount = complexityReport.Summary.Count
		rpt.CyclomaticMax = complexityReport.Summary.MaxCyclomatic
		rpt.CyclomaticP90 = complexityReport.Summary.P90Cyclomatic
		rpt.CognitiveMax = complexityReport.Summary.MaxCognitive
	}

	// Boundaries
	cfg, _ := boundaries.LoadConfig(target)
	if cfg != nil && len(cfg.Rules) > 0 {
		depReport, depErr := deps.Build(idx, deps.Options{
			Mode:         "package",
			IncludeEdges: true,
		})
		if depErr == nil {
			importEdges := make([]boundaries.ImportEdge, 0, len(depReport.Edges))
			for _, edge := range depReport.Edges {
				if edge.Internal {
					importEdges = append(importEdges, boundaries.ImportEdge{From: edge.From, To: edge.To})
				}
			}
			violations := boundaries.Evaluate(cfg, importEdges)
			rpt.BoundaryViolations = len(violations)
		}
	}

	// Import cycles
	depReport, depErr := deps.Build(analysisIdx, deps.Options{
		Mode:         "package",
		IncludeEdges: true,
	})
	if depErr == nil {
		graph := deps.GraphFromEdges(depReport.Edges)
		cycles := deps.DetectCycles(graph)
		rpt.ImportCycles = len(cycles)
	}

	// Capabilities
	rules := capa.BuiltinRules()
	capaMatches := capa.Detect(analysisIdx, rules)
	rpt.Capabilities = len(capaMatches)

	// Dead code
	xrefGraph, xrefErr := xref.Build(analysisIdx)
	if xrefErr == nil {
		deadCount := 0
		for _, definition := range xrefGraph.Definitions {
			if !definition.Callable {
				continue
			}
			if isEntrypointDefinition(definition) {
				continue
			}
			if isTestSourceFile(definition.File) {
				continue
			}
			if xrefGraph.IncomingCount(definition.ID) == 0 {
				deadCount++
			}
		}
		rpt.DeadFunctions = deadCount
	}

	// Hotspots (top 5)
	hotspotReport, hotspotErr := hotspot.Analyze(analysisIdx, hotspot.Options{
		Root:  target,
		Since: "90d",
		Top:   5,
	})
	if hotspotErr == nil {
		for _, h := range hotspotReport.Functions {
			rpt.Hotspots = append(rpt.Hotspots, mcpHotspotEntry{
				File:       h.File,
				Name:       h.Name,
				Cyclomatic: h.Cyclomatic,
				Score:      h.Score,
			})
		}
	}

	return rpt, nil
}
