// Package smells detects multi-signal structural code smells by combining coupling,
// complexity, type metrics, and cross-reference data. Each smell has a deterministic
// detection rule with configurable thresholds.
package smells

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/coupling"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
)

// Smell represents a single detected structural code smell with contributing signals.
type Smell struct {
	ID        string         `json:"id"`
	Severity  string         `json:"severity"` // "error" or "warn"
	File      string         `json:"file"`
	Package   string         `json:"package"`
	Name      string         `json:"name"`
	StartLine int            `json:"start_line"`
	EndLine   int            `json:"end_line"`
	Message   string         `json:"message"`
	Signals   map[string]any `json:"signals"`
}

// SmellSummary holds aggregate counts of detected smells.
type SmellSummary struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
	ByID       map[string]int `json:"by_id"`
}

// Report contains all detected smells and summary statistics.
type Report struct {
	Smells  []Smell      `json:"smells"`
	Summary SmellSummary `json:"summary"`
}

// Input provides the data sources for smell detection. Nil fields cause
// graceful degradation: smells that depend on the missing data are skipped.
type Input struct {
	Index      *model.Index
	XrefGraph  xref.Graph
	Coupling   *coupling.Report
	Complexity *complexity.Report
	Types      *typemetrics.Report
}

// Detect runs all smell detectors against the provided input and returns a report.
func Detect(input Input) *Report {
	var smells []Smell

	if input.Complexity != nil {
		smells = append(smells, detectGodFunction(input.Complexity)...)
		smells = append(smells, detectLongParams(input.Complexity)...)
		smells = append(smells, detectDeepNesting(input.Complexity)...)
		smells = append(smells, detectDataClump(input.Index, input.Complexity)...)
	}

	if input.Coupling != nil {
		smells = append(smells, detectGodPackage(input.Coupling)...)
	}

	if input.Types != nil {
		smells = append(smells, detectGodType(input.Types)...)
		smells = append(smells, detectWideInterface(input.Types)...)
	}

	smells = append(smells, detectShotgunSurgery(input.XrefGraph)...)
	smells = append(smells, detectFeatureEnvy(input.XrefGraph)...)

	if input.Coupling != nil {
		smells = append(smells, detectUnstableDep(input.XrefGraph, input.Coupling)...)
	}

	sortSmells(smells)

	return &Report{
		Smells:  smells,
		Summary: computeSummary(smells),
	}
}

// detectGodFunction finds functions with Cyclomatic > 30 AND Lines > 200 AND FanOut > 20.
func detectGodFunction(cr *complexity.Report) []Smell {
	var out []Smell
	for _, fn := range cr.Functions {
		if fn.Cyclomatic > 30 && fn.Lines > 200 && fn.FanOut > 20 {
			out = append(out, Smell{
				ID:        "god_function",
				Severity:  "error",
				File:      fn.File,
				Package:   packageFromPath(fn.File),
				Name:      fn.Name,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
				Message:   fmt.Sprintf("%s has cyclomatic=%d, lines=%d, fan_out=%d", fn.Name, fn.Cyclomatic, fn.Lines, fn.FanOut),
				Signals: map[string]any{
					"cyclomatic": fn.Cyclomatic,
					"lines":      fn.Lines,
					"fan_out":    fn.FanOut,
				},
			})
		}
	}
	return out
}

// detectGodPackage finds packages with Ce > 15 AND Symbols > 50 AND LCOM > 3.
func detectGodPackage(cr *coupling.Report) []Smell {
	var out []Smell
	for _, pkg := range cr.Packages {
		if pkg.Ce > 15 && pkg.Symbols > 50 && pkg.LCOM > 3 {
			out = append(out, Smell{
				ID:       "god_package",
				Severity: "error",
				File:     "",
				Package:  pkg.Package,
				Name:     pkg.Package,
				Message:  fmt.Sprintf("package %s has Ce=%d, symbols=%d, LCOM=%d", pkg.Package, pkg.Ce, pkg.Symbols, pkg.LCOM),
				Signals: map[string]any{
					"ce":      pkg.Ce,
					"symbols": pkg.Symbols,
					"lcom":    pkg.LCOM,
				},
			})
		}
	}
	return out
}

// detectGodType finds types with MethodSetSize > 20 AND Fields > 15.
func detectGodType(tr *typemetrics.Report) []Smell {
	var out []Smell
	for _, t := range tr.Types {
		if t.MethodSetSize > 20 && t.Fields > 15 {
			out = append(out, Smell{
				ID:        "god_type",
				Severity:  "warn",
				File:      t.File,
				Package:   packageFromPath(t.File),
				Name:      t.Name,
				StartLine: t.StartLine,
				EndLine:   t.EndLine,
				Message:   fmt.Sprintf("%s has method_set_size=%d, fields=%d", t.Name, t.MethodSetSize, t.Fields),
				Signals: map[string]any{
					"method_set_size": t.MethodSetSize,
					"fields":          t.Fields,
				},
			})
		}
	}
	return out
}

// detectFeatureEnvy finds functions whose outgoing calls target a single external
// package more than the function's own package.
func detectFeatureEnvy(graph xref.Graph) []Smell {
	var out []Smell
	for _, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		edges := graph.OutgoingEdges(def.ID)
		if len(edges) == 0 {
			continue
		}

		// Group outgoing call counts by callee package.
		callsByPkg := map[string]int{}
		for _, edge := range edges {
			callee := graph.EdgeCallee(edge)
			callsByPkg[callee.Package] += edge.Count
		}

		ownCalls := callsByPkg[def.Package]
		for pkg, count := range callsByPkg {
			if pkg == def.Package {
				continue
			}
			if count > ownCalls {
				out = append(out, Smell{
					ID:        "feature_envy",
					Severity:  "warn",
					File:      def.File,
					Package:   def.Package,
					Name:      def.Name,
					StartLine: def.StartLine,
					EndLine:   def.EndLine,
					Message:   fmt.Sprintf("%s makes %d calls to %s but only %d to own package %s", def.Name, count, pkg, ownCalls, def.Package),
					Signals: map[string]any{
						"own_package_calls":    ownCalls,
						"envied_package":       pkg,
						"envied_package_calls": count,
					},
				})
			}
		}
	}
	return out
}

// detectShotgunSurgery finds definitions with IncomingCount > 30.
func detectShotgunSurgery(graph xref.Graph) []Smell {
	var out []Smell
	for _, def := range graph.Definitions {
		count := graph.IncomingCount(def.ID)
		if count > 30 {
			out = append(out, Smell{
				ID:        "shotgun_surgery",
				Severity:  "warn",
				File:      def.File,
				Package:   def.Package,
				Name:      def.Name,
				StartLine: def.StartLine,
				EndLine:   def.EndLine,
				Message:   fmt.Sprintf("%s has %d incoming references", def.Name, count),
				Signals: map[string]any{
					"incoming_count": count,
				},
			})
		}
	}
	return out
}

// detectDataClump finds 3+ functions sharing identical parameter type signatures.
// Uses the Index to look up Symbol.Signature for each complexity function.
func detectDataClump(idx *model.Index, cr *complexity.Report) []Smell {
	if idx == nil {
		return nil
	}

	// Build lookup from (file, name, startLine) -> signature.
	sigLookup := map[string]string{}
	for _, file := range idx.Files {
		for _, sym := range file.Symbols {
			key := fmt.Sprintf("%s\x00%s\x00%d", file.Path, sym.Name, sym.StartLine)
			sigLookup[key] = sym.Signature
		}
	}

	// Group functions by normalized parameter signature.
	sigGroups := map[string][]complexity.FunctionMetrics{}
	for _, fn := range cr.Functions {
		key := fmt.Sprintf("%s\x00%s\x00%d", fn.File, fn.Name, fn.StartLine)
		sig := sigLookup[key]
		paramSig := normalizeParamSig(sig)
		if paramSig == "" {
			continue
		}
		sigGroups[paramSig] = append(sigGroups[paramSig], fn)
	}

	var out []Smell
	for sig, fns := range sigGroups {
		if len(fns) < 3 {
			continue
		}
		names := make([]string, len(fns))
		for i, fn := range fns {
			names[i] = fn.Name
		}
		for _, fn := range fns {
			out = append(out, Smell{
				ID:        "data_clump",
				Severity:  "warn",
				File:      fn.File,
				Package:   packageFromPath(fn.File),
				Name:      fn.Name,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
				Message:   fmt.Sprintf("%s shares parameter signature %q with %d other functions", fn.Name, sig, len(fns)-1),
				Signals: map[string]any{
					"param_signature": sig,
					"clump_size":      len(fns),
					"clump_members":   names,
				},
			})
		}
	}
	return out
}

// normalizeParamSig extracts the parameter list between the first '(' and last ')',
// then normalizes whitespace.
func normalizeParamSig(sig string) string {
	start := strings.Index(sig, "(")
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(sig, ")")
	if end < 0 || end <= start {
		return ""
	}
	inner := strings.TrimSpace(sig[start+1 : end])
	if inner == "" {
		return ""
	}
	parts := strings.Fields(inner)
	return strings.Join(parts, " ")
}

// detectLongParams finds functions with Parameters > 5.
func detectLongParams(cr *complexity.Report) []Smell {
	var out []Smell
	for _, fn := range cr.Functions {
		if fn.Parameters > 5 {
			out = append(out, Smell{
				ID:        "long_params",
				Severity:  "warn",
				File:      fn.File,
				Package:   packageFromPath(fn.File),
				Name:      fn.Name,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
				Message:   fmt.Sprintf("%s has %d parameters", fn.Name, fn.Parameters),
				Signals: map[string]any{
					"parameters": fn.Parameters,
				},
			})
		}
	}
	return out
}

// detectDeepNesting finds functions with MaxNesting > 4.
func detectDeepNesting(cr *complexity.Report) []Smell {
	var out []Smell
	for _, fn := range cr.Functions {
		if fn.MaxNesting > 4 {
			out = append(out, Smell{
				ID:        "deep_nesting",
				Severity:  "warn",
				File:      fn.File,
				Package:   packageFromPath(fn.File),
				Name:      fn.Name,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
				Message:   fmt.Sprintf("%s has max nesting depth of %d", fn.Name, fn.MaxNesting),
				Signals: map[string]any{
					"max_nesting": fn.MaxNesting,
				},
			})
		}
	}
	return out
}

// detectWideInterface finds interfaces with InterfaceWidth > 8.
func detectWideInterface(tr *typemetrics.Report) []Smell {
	var out []Smell
	for _, t := range tr.Types {
		if t.InterfaceWidth > 8 {
			out = append(out, Smell{
				ID:        "wide_interface",
				Severity:  "warn",
				File:      t.File,
				Package:   packageFromPath(t.File),
				Name:      t.Name,
				StartLine: t.StartLine,
				EndLine:   t.EndLine,
				Message:   fmt.Sprintf("%s has interface width of %d", t.Name, t.InterfaceWidth),
				Signals: map[string]any{
					"interface_width": t.InterfaceWidth,
				},
			})
		}
	}
	return out
}

// detectUnstableDep finds cross-package dependencies from stable packages (I < 0.3)
// to unstable packages (I > 0.7).
func detectUnstableDep(graph xref.Graph, cr *coupling.Report) []Smell {
	// Build instability lookup by package name.
	instability := map[string]float64{}
	for _, pkg := range cr.Packages {
		instability[pkg.Package] = pkg.Instability
	}

	// Track unique (stable_pkg, unstable_pkg) pairs.
	seen := map[string]bool{}
	var out []Smell

	for _, edge := range graph.Edges {
		callerPkg := graph.Definitions[edge.CallerIdx].Package
		calleePkg := graph.Definitions[edge.CalleeIdx].Package
		if callerPkg == calleePkg {
			continue
		}

		callerI, callerOK := instability[callerPkg]
		calleeI, calleeOK := instability[calleePkg]
		if !callerOK || !calleeOK {
			continue
		}

		if callerI < 0.3 && calleeI > 0.7 {
			pairKey := callerPkg + "\x00" + calleePkg
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			out = append(out, Smell{
				ID:       "unstable_dep",
				Severity: "warn",
				Package:  callerPkg,
				Name:     callerPkg,
				Message:  fmt.Sprintf("stable package %s (I=%.2f) depends on unstable package %s (I=%.2f)", callerPkg, callerI, calleePkg, calleeI),
				Signals: map[string]any{
					"stable_package":       callerPkg,
					"stable_instability":   callerI,
					"unstable_package":     calleePkg,
					"unstable_instability": calleeI,
				},
			})
		}
	}
	return out
}

// sortSmells sorts by severity (errors first), then file, then start_line.
func sortSmells(smells []Smell) {
	sort.SliceStable(smells, func(i, j int) bool {
		si, sj := severityOrder(smells[i].Severity), severityOrder(smells[j].Severity)
		if si != sj {
			return si < sj
		}
		if smells[i].File != smells[j].File {
			return smells[i].File < smells[j].File
		}
		return smells[i].StartLine < smells[j].StartLine
	})
}

func severityOrder(s string) int {
	if s == "error" {
		return 0
	}
	return 1
}

// computeSummary tallies smells by severity and by ID.
func computeSummary(smells []Smell) SmellSummary {
	summary := SmellSummary{
		Total:      len(smells),
		BySeverity: map[string]int{},
		ByID:       map[string]int{},
	}
	for _, s := range smells {
		summary.BySeverity[s.Severity]++
		summary.ByID[s.ID]++
	}
	return summary
}

// packageFromPath extracts the directory (package) from a file path.
func packageFromPath(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	return path[:idx]
}
