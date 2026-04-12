// Package xref builds cross-reference graphs from structural indexes, enabling call graph traversal and dead code detection.
package xref

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

type Definition struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	Package   string `json:"package"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Callable  bool   `json:"callable"`
}

type CallSample struct {
	File        string `json:"file"`
	StartLine   int    `json:"start_line"`
	StartColumn int    `json:"start_column"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
}

type Edge struct {
	CallerIdx  int          `json:"-"`
	CalleeIdx  int          `json:"-"`
	Resolution string       `json:"resolution"`
	Count      int          `json:"count"`
	Samples    []CallSample `json:"samples,omitempty"`
}

// MaterializedEdge is the serialization-friendly form of Edge with full Definition copies.
type MaterializedEdge struct {
	Caller     Definition   `json:"caller"`
	Callee     Definition   `json:"callee"`
	Resolution string       `json:"resolution"`
	Count      int          `json:"count"`
	Samples    []CallSample `json:"samples,omitempty"`
}

type UnresolvedCall struct {
	File           string      `json:"file"`
	Package        string      `json:"package"`
	Kind           string      `json:"kind"`
	Name           string      `json:"name"`
	StartLine      int         `json:"start_line"`
	StartColumn    int         `json:"start_column"`
	EndLine        int         `json:"end_line"`
	EndColumn      int         `json:"end_column"`
	Caller         *Definition `json:"caller,omitempty"`
	Reason         string      `json:"reason"`
	CandidateCount int         `json:"candidate_count,omitempty"`
}

// internalEdge stores edges compactly during Build using indices into the definitions slice.
type internalEdge struct {
	callerIdx  int
	calleeIdx  int
	resolution string
	count      int
	samples    []CallSample
}

type Graph struct {
	Root        string           `json:"root"`
	Definitions []Definition     `json:"definitions,omitempty"`
	Edges       []Edge           `json:"-"` // compact; use MaterializeEdges for JSON
	Unresolved  []UnresolvedCall `json:"unresolved,omitempty"`

	// Index-based lookup maps — values are indices into Definitions or Edges.
	defByID              map[string]int   // defID -> index into Definitions
	callableByName       map[string][]int // name -> indices into Definitions
	callableByPkgName    map[string][]int // pkg\x00name -> indices into Definitions
	callableByFileName   map[string][]int // file\x00name -> indices into Definitions
	callableByFile       map[string][]int // file -> indices into Definitions

	outgoingByDef map[string][]int // defID -> indices into Edges
	incomingByDef map[string][]int // defID -> indices into Edges
	outgoingCount map[string]int
	incomingCount map[string]int
}

// EdgeCaller returns a pointer to the caller Definition for the given edge.
func (g *Graph) EdgeCaller(e Edge) *Definition {
	if e.CallerIdx < 0 || e.CallerIdx >= len(g.Definitions) {
		return &Definition{Name: "<invalid>", ID: "<invalid>"}
	}
	return &g.Definitions[e.CallerIdx]
}

// EdgeCallee returns a pointer to the callee Definition for the given edge.
func (g *Graph) EdgeCallee(e Edge) *Definition {
	if e.CalleeIdx < 0 || e.CalleeIdx >= len(g.Definitions) {
		return &Definition{Name: "<invalid>", ID: "<invalid>"}
	}
	return &g.Definitions[e.CalleeIdx]
}

// MaterializeEdge produces a MaterializedEdge with full Definition copies.
func (g *Graph) MaterializeEdge(e Edge) MaterializedEdge {
	return MaterializedEdge{
		Caller:     g.Definitions[e.CallerIdx],
		Callee:     g.Definitions[e.CalleeIdx],
		Resolution: e.Resolution,
		Count:      e.Count,
		Samples:    e.Samples,
	}
}

// MaterializeEdges produces MaterializedEdge copies for a slice of compact edges.
func (g *Graph) MaterializeEdges(edges []Edge) []MaterializedEdge {
	result := make([]MaterializedEdge, len(edges))
	for i, e := range edges {
		result[i] = g.MaterializeEdge(e)
	}
	return result
}

type importScope struct {
	paths        map[string]struct{}
	packages     map[string]struct{}
	tokens       map[string]struct{}
	hasPathHints bool
}

type Walk struct {
	Roots   []Definition `json:"roots,omitempty"`
	Nodes   []Definition `json:"nodes,omitempty"`
	Edges   []Edge       `json:"-"`
	Depth   int          `json:"depth"`
	Reverse bool         `json:"reverse"`
	graph   *Graph
}

// MaterializedEdges returns edges with full Definition copies for serialization.
func (w Walk) MaterializedEdges() []MaterializedEdge {
	if w.graph == nil {
		return nil
	}
	return w.graph.MaterializeEdges(w.Edges)
}

func Build(idx *model.Index) (Graph, error) {
	if idx == nil {
		return Graph{}, fmt.Errorf("index is nil")
	}

	definitions := make([]Definition, 0, idx.SymbolCount())
	defByID := map[string]int{}
	callableByName := map[string][]int{}
	callableByPkgName := map[string][]int{}
	callableByFileName := map[string][]int{}
	callableByFile := map[string][]int{}

	for _, file := range idx.Files {
		pkg := packageFromPath(file.Path)
		for _, symbol := range file.Symbols {
			def := definitionFromSymbol(file.Path, pkg, symbol)
			idx := len(definitions)
			definitions = append(definitions, def)
			defByID[def.ID] = idx

			if !def.Callable {
				continue
			}
			callableByName[def.Name] = append(callableByName[def.Name], idx)
			callableByPkgName[keyPackageName(pkg, def.Name)] = append(callableByPkgName[keyPackageName(pkg, def.Name)], idx)
			callableByFileName[keyFileName(def.File, def.Name)] = append(callableByFileName[keyFileName(def.File, def.Name)], idx)
			callableByFile[def.File] = append(callableByFile[def.File], idx)
		}
	}

	sortDefinitions(definitions)
	// Rebuild defByID after sorting since indices changed.
	for i := range definitions {
		defByID[definitions[i].ID] = i
	}
	// Rebuild callable maps after sorting.
	callableByName = map[string][]int{}
	callableByPkgName = map[string][]int{}
	callableByFileName = map[string][]int{}
	callableByFile = map[string][]int{}
	for i := range definitions {
		def := &definitions[i]
		if !def.Callable {
			continue
		}
		callableByName[def.Name] = append(callableByName[def.Name], i)
		callableByPkgName[keyPackageName(def.Package, def.Name)] = append(callableByPkgName[keyPackageName(def.Package, def.Name)], i)
		callableByFileName[keyFileName(def.File, def.Name)] = append(callableByFileName[keyFileName(def.File, def.Name)], i)
		callableByFile[def.File] = append(callableByFile[def.File], i)
	}

	edgeByPair := map[string]*internalEdge{}
	unresolved := make([]UnresolvedCall, 0, 32)
	modulePath := modulePathFromRoot(idx.Root)

	for _, file := range idx.Files {
		pkg := packageFromPath(file.Path)
		scope := buildImportScope(file.Imports, modulePath)
		callableIndices := callableByFile[file.Path]
		for _, ref := range file.References {
			if !isCallReference(ref.Kind) {
				continue
			}

			callerIdx := findEnclosingCallableIdx(definitions, callableIndices, ref.StartLine)
			if callerIdx == -1 {
				unresolved = append(unresolved, unresolvedFromRef(file.Path, pkg, ref, nil, "outside_callable", 0))
				continue
			}

			res := resolveCalleeIdx(file.Path, pkg, ref.Name, scope, definitions, callableByFileName, callableByPkgName, callableByName)
			if !res.ok {
				callerCopy := definitions[callerIdx]
				unresolved = append(unresolved, unresolvedFromRef(file.Path, pkg, ref, &callerCopy, res.reason, res.candidateCount))
				continue
			}

			sample := CallSample{
				File:        file.Path,
				StartLine:   ref.StartLine,
				StartColumn: ref.StartColumn,
				Kind:        ref.Kind,
				Name:        ref.Name,
			}

			// Polymorphic dispatch: create edges to ALL candidate methods.
			calleeIndices := res.candidates
			resolution := res.resolution
			if len(calleeIndices) == 0 {
				calleeIndices = []int{res.idx}
			} else {
				resolution = "poly_" + res.polyScope
			}

			for _, calleeIdx := range calleeIndices {
				pairKey := keyPair(definitions[callerIdx].ID, definitions[calleeIdx].ID)
				edge, exists := edgeByPair[pairKey]
				if !exists {
					edge = &internalEdge{
						callerIdx:  callerIdx,
						calleeIdx:  calleeIdx,
						resolution: resolution,
						count:      0,
						samples:    make([]CallSample, 0, 3),
					}
					edgeByPair[pairKey] = edge
				}
				edge.count++
				if len(edge.samples) < 3 {
					edge.samples = append(edge.samples, sample)
				}
			}
		}
	}

	// Materialize edges from internal edges, referencing the backing definitions slice.
	edges := make([]Edge, 0, len(edgeByPair))
	outgoingByDef := map[string][]int{}
	incomingByDef := map[string][]int{}
	outgoingCount := map[string]int{}
	incomingCount := map[string]int{}
	for _, ie := range edgeByPair {
		edgeIdx := len(edges)
		edges = append(edges, Edge{
			CallerIdx:  ie.callerIdx,
			CalleeIdx:  ie.calleeIdx,
			Resolution: ie.resolution,
			Count:      ie.count,
			Samples:    ie.samples,
		})
		callerID := definitions[ie.callerIdx].ID
		calleeID := definitions[ie.calleeIdx].ID
		outgoingByDef[callerID] = append(outgoingByDef[callerID], edgeIdx)
		incomingByDef[calleeID] = append(incomingByDef[calleeID], edgeIdx)
		outgoingCount[callerID] += ie.count
		incomingCount[calleeID] += ie.count
	}

	sort.Slice(edges, func(i, j int) bool {
		return edgeLessWithDefs(definitions, edges[i], edges[j])
	})
	// Rebuild edge index maps after sorting since edge indices changed.
	outgoingByDef = map[string][]int{}
	incomingByDef = map[string][]int{}
	for i := range edges {
		callerID := definitions[edges[i].CallerIdx].ID
		calleeID := definitions[edges[i].CalleeIdx].ID
		outgoingByDef[callerID] = append(outgoingByDef[callerID], i)
		incomingByDef[calleeID] = append(incomingByDef[calleeID], i)
	}

	sort.Slice(unresolved, func(i, j int) bool {
		if unresolved[i].File == unresolved[j].File {
			if unresolved[i].StartLine == unresolved[j].StartLine {
				if unresolved[i].StartColumn == unresolved[j].StartColumn {
					return unresolved[i].Name < unresolved[j].Name
				}
				return unresolved[i].StartColumn < unresolved[j].StartColumn
			}
			return unresolved[i].StartLine < unresolved[j].StartLine
		}
		return unresolved[i].File < unresolved[j].File
	})

	return Graph{
		Root:               idx.Root,
		Definitions:        definitions,
		Edges:              edges,
		Unresolved:         unresolved,
		defByID:            defByID,
		callableByName:     callableByName,
		callableByPkgName:  callableByPkgName,
		callableByFileName: callableByFileName,
		callableByFile:     callableByFile,
		outgoingByDef:      outgoingByDef,
		incomingByDef:      incomingByDef,
		outgoingCount:      outgoingCount,
		incomingCount:      incomingCount,
	}, nil
}

func (g *Graph) FindDefinitions(pattern string, regexMode bool) ([]Definition, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("definition matcher cannot be empty")
	}

	match := func(name string) bool { return name == pattern }
	if regexMode {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile regex: %w", err)
		}
		match = compiled.MatchString
	}

	matches := make([]Definition, 0, 16)
	for _, definition := range g.Definitions {
		if !definition.Callable {
			continue
		}
		if !match(definition.Name) {
			continue
		}
		matches = append(matches, definition)
	}
	sortDefinitions(matches)
	return matches, nil
}

func (g *Graph) IncomingCount(defID string) int {
	return g.incomingCount[defID]
}

func (g *Graph) OutgoingCount(defID string) int {
	return g.outgoingCount[defID]
}

func (g *Graph) OutgoingEdges(defID string) []Edge {
	indices := g.outgoingByDef[defID]
	if len(indices) == 0 {
		return nil
	}
	out := make([]Edge, len(indices))
	for i, idx := range indices {
		out[i] = g.Edges[idx]
	}
	return out
}

func (g *Graph) IncomingEdges(defID string) []Edge {
	indices := g.incomingByDef[defID]
	if len(indices) == 0 {
		return nil
	}
	out := make([]Edge, len(indices))
	for i, idx := range indices {
		out[i] = g.Edges[idx]
	}
	return out
}

func (g *Graph) Walk(rootIDs []string, depth int, reverse bool) Walk {
	if depth <= 0 {
		depth = 1
	}

	roots := make([]Definition, 0, len(rootIDs))
	rootSet := map[string]bool{}
	for _, rootID := range rootIDs {
		rootID = strings.TrimSpace(rootID)
		if rootID == "" || rootSet[rootID] {
			continue
		}
		idx, ok := g.defByID[rootID]
		if !ok {
			continue
		}
		rootSet[rootID] = true
		roots = append(roots, g.Definitions[idx])
	}
	sortDefinitions(roots)

	type queueItem struct {
		id    string
		depth int
	}
	queue := make([]queueItem, 0, len(roots))
	visitedNodes := map[string]bool{}
	for _, root := range roots {
		visitedNodes[root.ID] = true
		queue = append(queue, queueItem{id: root.ID, depth: 0})
	}

	edgeSet := map[string]int{} // pair key -> index into g.Edges
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= depth {
			continue
		}

		var candidateEdgeIndices []int
		if reverse {
			candidateEdgeIndices = g.incomingByDef[current.id]
		} else {
			candidateEdgeIndices = g.outgoingByDef[current.id]
		}

		for _, ei := range candidateEdgeIndices {
			edge := &g.Edges[ei]
			callerID := g.Definitions[edge.CallerIdx].ID
			calleeID := g.Definitions[edge.CalleeIdx].ID
			edgeSet[keyPair(callerID, calleeID)] = ei

			nextID := calleeID
			if reverse {
				nextID = callerID
			}
			if visitedNodes[nextID] {
				continue
			}
			visitedNodes[nextID] = true
			queue = append(queue, queueItem{id: nextID, depth: current.depth + 1})
		}
	}

	nodes := make([]Definition, 0, len(visitedNodes))
	for id := range visitedNodes {
		if idx, ok := g.defByID[id]; ok {
			nodes = append(nodes, g.Definitions[idx])
		}
	}
	sortDefinitions(nodes)

	edges := make([]Edge, 0, len(edgeSet))
	for _, ei := range edgeSet {
		edges = append(edges, g.Edges[ei])
	}
	sort.Slice(edges, func(i, j int) bool {
		return edgeLessWithDefs(g.Definitions, edges[i], edges[j])
	})

	return Walk{
		Roots:   roots,
		Nodes:   nodes,
		Edges:   edges,
		Depth:   depth,
		Reverse: reverse,
		graph:   g,
	}
}

// calleeResolution holds the result of callee resolution.
type calleeResolution struct {
	// Single-target resolution (unique match).
	idx        int
	resolution string

	// Multi-target resolution (polymorphic dispatch).
	candidates []int
	polyScope  string // resolution scope for the polymorphic candidates

	reason         string
	candidateCount int
	ok             bool
}

// resolveCalleeIdx resolves a callee by name using index-based lookups,
// returning the index into the definitions slice.
func resolveCalleeIdx(
	filePath, pkg, name string,
	scope importScope,
	definitions []Definition,
	callableByFileName map[string][]int,
	callableByPkgName map[string][]int,
	callableByName map[string][]int,
) calleeResolution {
	if candidates := uniqueDefIndices(definitions, callableByFileName[keyFileName(filePath, name)]); len(candidates) > 0 {
		if len(candidates) == 1 {
			return calleeResolution{idx: candidates[0], resolution: "file", ok: true}
		}
		if allMethods(definitions, candidates) {
			return calleeResolution{candidates: candidates, polyScope: "file", ok: true}
		}
		return calleeResolution{reason: "ambiguous_file", candidateCount: len(candidates)}
	}

	if candidates := candidatesByImportScopeIdx(definitions, uniqueDefIndices(definitions, callableByName[name]), scope); len(candidates) > 0 {
		if len(candidates) == 1 {
			return calleeResolution{idx: candidates[0], resolution: "import", ok: true}
		}
		if allMethods(definitions, candidates) {
			return calleeResolution{candidates: candidates, polyScope: "import", ok: true}
		}
		return calleeResolution{reason: "ambiguous_import", candidateCount: len(candidates)}
	}

	if candidates := uniqueDefIndices(definitions, callableByPkgName[keyPackageName(pkg, name)]); len(candidates) > 0 {
		if len(candidates) == 1 {
			return calleeResolution{idx: candidates[0], resolution: "package", ok: true}
		}
		if allMethods(definitions, candidates) {
			return calleeResolution{candidates: candidates, polyScope: "package", ok: true}
		}
		return calleeResolution{reason: "ambiguous_package", candidateCount: len(candidates)}
	}

	if candidates := uniqueDefIndices(definitions, callableByName[name]); len(candidates) > 0 {
		if len(candidates) == 1 {
			return calleeResolution{idx: candidates[0], resolution: "global", ok: true}
		}
		if allMethods(definitions, candidates) {
			return calleeResolution{candidates: candidates, polyScope: "global", ok: true}
		}
		return calleeResolution{reason: "ambiguous_global", candidateCount: len(candidates)}
	}

	return calleeResolution{reason: "not_found"}
}

// allMethods returns true if every candidate is a method_definition with a
// non-empty receiver, indicating the ambiguity comes from polymorphic dispatch
// (multiple types implementing the same method name).
func allMethods(definitions []Definition, candidates []int) bool {
	for _, ci := range candidates {
		def := &definitions[ci]
		if def.Kind != "method_definition" || def.Receiver == "" {
			return false
		}
	}
	return len(candidates) > 0
}

func unresolvedFromRef(filePath, pkg string, ref model.Reference, caller *Definition, reason string, candidateCount int) UnresolvedCall {
	return UnresolvedCall{
		File:           filePath,
		Package:        pkg,
		Kind:           ref.Kind,
		Name:           ref.Name,
		StartLine:      ref.StartLine,
		StartColumn:    ref.StartColumn,
		EndLine:        ref.EndLine,
		EndColumn:      ref.EndColumn,
		Caller:         caller,
		Reason:         reason,
		CandidateCount: candidateCount,
	}
}

func definitionFromSymbol(filePath, pkg string, symbol model.Symbol) Definition {
	return Definition{
		ID:        keyDefinition(filePath, symbol.Kind, symbol.Name, symbol.StartLine),
		File:      filePath,
		Package:   pkg,
		Kind:      symbol.Kind,
		Name:      symbol.Name,
		Signature: symbol.Signature,
		Receiver:  symbol.Receiver,
		StartLine: symbol.StartLine,
		EndLine:   symbol.EndLine,
		Callable:  isCallableKind(symbol.Kind),
	}
}

// findEnclosingCallableIdx finds the enclosing callable definition for a given line,
// returning its index in the definitions slice (or -1 if not found).
func findEnclosingCallableIdx(definitions []Definition, callableIndices []int, line int) int {
	if len(callableIndices) == 0 {
		return -1
	}

	bestIdx := -1
	bestSpan := 0
	for _, ci := range callableIndices {
		def := &definitions[ci]
		if line < def.StartLine || line > def.EndLine {
			continue
		}
		span := def.EndLine - def.StartLine
		if bestIdx == -1 || span < bestSpan || (span == bestSpan && def.StartLine > definitions[bestIdx].StartLine) {
			bestIdx = ci
			bestSpan = span
		}
	}

	return bestIdx
}

func isCallableKind(kind string) bool {
	switch kind {
	case "function_definition", "method_definition":
		return true
	default:
		return false
	}
}

func buildImportScope(imports []string, modulePath string) importScope {
	scope := importScope{
		paths:    map[string]struct{}{},
		packages: map[string]struct{}{},
		tokens:   map[string]struct{}{},
	}
	modulePath = normalizePathKey(modulePath)

	for _, rawImport := range imports {
		rawImport = strings.TrimSpace(rawImport)
		if rawImport == "" {
			continue
		}

		importPaths := importPathsFromEntry(rawImport)
		if len(importPaths) > 0 {
			scope.hasPathHints = true
			for _, imp := range importPaths {
				imp = normalizePathKey(imp)
				if imp == "" {
					continue
				}
				scope.paths[imp] = struct{}{}
				if pkg, ok := packageFromImportPath(imp, modulePath); ok {
					scope.packages[pkg] = struct{}{}
				}
			}
			continue
		}

		for _, token := range splitImportTokens(rawImport) {
			scope.tokens[token] = struct{}{}
		}
	}
	return scope
}

func normalizeImportPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.Trim(raw, "\"'`")
	raw = strings.TrimSuffix(raw, ";")
	return strings.TrimSpace(raw)
}

func splitImportTokens(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	replacer := strings.NewReplacer("::", "/", ".", "/", "\\", "/", ":", "/")
	normalized := replacer.Replace(path)
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// candidatesByImportScopeIdx filters definition indices by import scope.
func candidatesByImportScopeIdx(definitions []Definition, candidateIndices []int, scope importScope) []int {
	if len(candidateIndices) == 0 || (len(scope.paths) == 0 && len(scope.packages) == 0 && len(scope.tokens) == 0) {
		return nil
	}

	filtered := make([]int, 0, len(candidateIndices))
	for _, ci := range candidateIndices {
		if definitionMatchesImportScope(definitions[ci], scope) {
			filtered = append(filtered, ci)
		}
	}
	return uniqueDefIndices(definitions, filtered)
}

func definitionMatchesImportScope(def Definition, scope importScope) bool {
	pkg := normalizePathKey(def.Package)
	if pkg == "" {
		return false
	}

	if _, ok := scope.packages[pkg]; ok {
		return true
	}
	if _, ok := scope.paths[pkg]; ok {
		return true
	}

	if scope.hasPathHints {
		return false
	}
	for _, token := range splitImportTokens(pkg) {
		if _, ok := scope.tokens[token]; ok {
			return true
		}
	}
	return false
}

func importPathsFromEntry(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if looksLikeDirectImportPath(raw) {
		return []string{raw}
	}

	quoted := extractQuotedStrings(raw)
	if len(quoted) == 0 {
		return nil
	}
	paths := make([]string, 0, len(quoted))
	for _, value := range quoted {
		value = normalizeImportPath(value)
		if value == "" {
			continue
		}
		paths = append(paths, value)
	}
	return paths
}

func looksLikeDirectImportPath(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	return !strings.ContainsAny(raw, " \t\r\n")
}

func extractQuotedStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for i := 0; i < len(raw); i++ {
		quote := raw[i]
		if quote != '\'' && quote != '"' && quote != '`' {
			continue
		}
		start := i + 1
		for j := start; j < len(raw); j++ {
			if raw[j] == '\\' {
				j++
				continue
			}
			if raw[j] == quote {
				out = append(out, raw[start:j])
				i = j
				break
			}
		}
	}
	return out
}

func packageFromImportPath(importPath, modulePath string) (string, bool) {
	importPath = normalizePathKey(importPath)
	if importPath == "" {
		return "", false
	}

	if modulePath != "" {
		if importPath == modulePath {
			return ".", true
		}
		if strings.HasPrefix(importPath, modulePath+"/") {
			trimmed := strings.TrimPrefix(importPath, modulePath+"/")
			trimmed = normalizePathKey(trimmed)
			if trimmed == "" {
				return ".", true
			}
			return trimmed, true
		}
		return "", false
	}

	if !looksLikeLocalImportPath(importPath) {
		return "", false
	}
	return importPath, true
}

func looksLikeLocalImportPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.ContainsAny(path, " \t\r\n") {
		return false
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "/") {
		return false
	}
	if strings.Contains(path, "://") {
		return false
	}

	segments := strings.Split(path, "/")
	if len(segments) == 0 {
		return false
	}
	first := strings.TrimSpace(segments[0])
	if first == "" {
		return false
	}
	if strings.HasPrefix(first, "@") {
		return false
	}
	if strings.Contains(first, ".") && len(segments) > 1 {
		return false
	}
	return true
}

func normalizePathKey(raw string) string {
	raw = normalizeImportPath(raw)
	if raw == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(raw))
}

func isCallReference(kind string) bool {
	return strings.HasPrefix(strings.TrimSpace(kind), "reference.call")
}

func sortDefinitions(items []Definition) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].File == items[j].File {
			if items[i].StartLine == items[j].StartLine {
				if items[i].Kind == items[j].Kind {
					return items[i].Name < items[j].Name
				}
				return items[i].Kind < items[j].Kind
			}
			return items[i].StartLine < items[j].StartLine
		}
		return items[i].File < items[j].File
	})
}

func edgeLessWithDefs(defs []Definition, left, right Edge) bool {
	lCaller := &defs[left.CallerIdx]
	rCaller := &defs[right.CallerIdx]
	if lCaller.File == rCaller.File {
		if lCaller.StartLine == rCaller.StartLine {
			if lCaller.Name == rCaller.Name {
				lCallee := &defs[left.CalleeIdx]
				rCallee := &defs[right.CalleeIdx]
				if lCallee.File == rCallee.File {
					if lCallee.StartLine == rCallee.StartLine {
						return lCallee.Name < rCallee.Name
					}
					return lCallee.StartLine < rCallee.StartLine
				}
				return lCallee.File < rCallee.File
			}
			return lCaller.Name < rCaller.Name
		}
		return lCaller.StartLine < rCaller.StartLine
	}
	return lCaller.File < rCaller.File
}

// uniqueDefIndices deduplicates definition indices by ID, preserving sort order.
func uniqueDefIndices(definitions []Definition, indices []int) []int {
	if len(indices) == 0 {
		return nil
	}

	seen := map[string]bool{}
	unique := make([]int, 0, len(indices))
	for _, idx := range indices {
		id := definitions[idx].ID
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, idx)
	}
	// Sort by definition order (file, startLine, kind, name).
	sort.Slice(unique, func(i, j int) bool {
		di, dj := &definitions[unique[i]], &definitions[unique[j]]
		if di.File == dj.File {
			if di.StartLine == dj.StartLine {
				if di.Kind == dj.Kind {
					return di.Name < dj.Name
				}
				return di.Kind < dj.Kind
			}
			return di.StartLine < dj.StartLine
		}
		return di.File < dj.File
	})
	return unique
}

func packageFromPath(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	dir := filepath.ToSlash(filepath.Dir(cleaned))
	if dir == "." || dir == "/" {
		return "."
	}
	return dir
}

func keyDefinition(filePath, kind, name string, startLine int) string {
	return filePath + "\x00" + kind + "\x00" + name + "\x00" + fmt.Sprintf("%d", startLine)
}

func keyFileName(filePath, name string) string {
	return filePath + "\x00" + name
}

func keyPackageName(pkg, name string) string {
	return pkg + "\x00" + name
}

func keyPair(leftID, rightID string) string {
	return leftID + "\x00" + rightID
}

func modulePathFromRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	goModPath := filepath.Join(root, "go.mod")
	file, err := os.Open(goModPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		module := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		module = strings.Trim(module, `"`)
		return normalizePathKey(module)
	}
	return ""
}
