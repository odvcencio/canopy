// Package complexity provides AST-based complexity analysis for functions across 206 languages using gotreesitter.
package complexity

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

const (
	defaultFunctionParseTimeout = 2 * time.Second
	defaultComplexityWorkers    = 4
)

// FunctionMetrics holds all computed complexity metrics for a single function or method.
type FunctionMetrics struct {
	File       string `json:"file"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Language   string `json:"language"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Lines      int    `json:"lines"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
	MaxNesting int    `json:"max_nesting"`
	Parameters int    `json:"parameters"`
	FanIn      int    `json:"fan_in"`
	FanOut     int    `json:"fan_out"`
	Returns    int    `json:"returns"`
	BoolDepth  int    `json:"bool_depth"`

	LOC             LOCMetrics             `json:"loc"`
	Halstead        HalsteadMetrics        `json:"halstead"`
	Maintainability MaintainabilityMetrics `json:"maintainability"`
}

// Summary holds aggregate statistics across all analyzed functions.
type Summary struct {
	Count         int     `json:"count"`
	AvgCyclomatic float64 `json:"avg_cyclomatic"`
	MaxCyclomatic int     `json:"max_cyclomatic"`
	P90Cyclomatic int     `json:"p90_cyclomatic"`
	AvgCognitive  float64 `json:"avg_cognitive"`
	MaxCognitive  int     `json:"max_cognitive"`
	AvgLines      float64 `json:"avg_lines"`
	MaxLines      int     `json:"max_lines"`
	AvgMaxNesting float64 `json:"avg_max_nesting"`

	AvgVolume          float64 `json:"avg_volume"`
	AvgMaintainability float64 `json:"avg_maintainability"`
	MinMaintainability float64 `json:"min_maintainability"`
}

// Report contains the full complexity analysis result.
type Report struct {
	Functions []FunctionMetrics `json:"functions"`
	Summary   Summary           `json:"summary"`
}

// Options controls filtering, sorting, and limiting of the analysis output.
type Options struct {
	MinCyclomatic int
	Sort          string // "cyclomatic", "cognitive", "lines", "nesting"
	Top           int
}

// Analyze computes complexity metrics for every function/method in the index.
func Analyze(idx *model.Index, root string, opts Options) (*Report, error) {
	if idx == nil {
		return &Report{}, nil
	}

	functions := make([]FunctionMetrics, 0, 128)
	fileResults := make([][]FunctionMetrics, len(idx.Files))
	workerCount := min(defaultComplexityWorkers, runtime.GOMAXPROCS(0), len(idx.Files))
	if workerCount < 1 {
		workerCount = 1
	}
	jobs := make(chan int, workerCount)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			// Keep parsers worker-local because parser state is mutable.
			parserCache := map[*gotreesitter.Language]*gotreesitter.Parser{}
			for fileIndex := range jobs {
				fileResults[fileIndex] = analyzeFile(idx.Files[fileIndex], root, opts, parserCache)
			}
		}()
	}
	for fileIndex := range idx.Files {
		jobs <- fileIndex
	}
	close(jobs)
	workers.Wait()
	for _, fileFunctions := range fileResults {
		functions = append(functions, fileFunctions...)
	}

	sortFunctions(functions, opts.Sort)

	if opts.Top > 0 && opts.Top < len(functions) {
		functions = functions[:opts.Top]
	}

	summary := computeSummary(functions)

	return &Report{
		Functions: functions,
		Summary:   summary,
	}, nil
}

func analyzeFile(file model.FileSummary, root string, opts Options, parserCache map[*gotreesitter.Language]*gotreesitter.Parser) []FunctionMetrics {
	entry := grammars.DetectLanguage(file.Path)
	if entry == nil {
		return nil
	}
	hasCallable := false
	for _, sym := range file.Symbols {
		if isCallableSymbol(sym.Kind) {
			hasCallable = true
			break
		}
	}
	if !hasCallable {
		return nil
	}

	absPath := file.Path
	if !filepath.IsAbs(absPath) && root != "" {
		absPath = filepath.Join(root, absPath)
	}
	source, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	lineOffsets := buildLineOffsets(source)
	spanCache := make(map[[2]int]functionAnalysis)

	lang := entry.Language()
	parser, ok := parserCache[lang]
	if !ok {
		parser = gotreesitter.NewParser(lang)
		parser.SetTimeoutMicros(uint64(defaultFunctionParseTimeout / time.Microsecond))
		parserCache[lang] = parser
	}

	functions := make([]FunctionMetrics, 0, len(file.Symbols))
	for _, sym := range file.Symbols {
		if !isCallableSymbol(sym.Kind) {
			continue
		}
		body := extractBodyWithOffsets(source, lineOffsets, sym.StartLine, sym.EndLine)
		if len(body) == 0 {
			continue
		}

		span := [2]int{sym.StartLine, sym.EndLine}
		analysis, cached := spanCache[span]
		if !cached {
			analysis = analyzeFunctionBody(parser, entry, lang, body)
			spanCache[span] = analysis
		}
		if !analysis.valid {
			continue
		}

		mi := maintainability(
			analysis.halstead.Volume,
			analysis.cyclomatic,
			analysis.loc.SLOC,
			commentPercent(analysis.loc),
		)
		metrics := FunctionMetrics{
			File:       file.Path,
			Name:       sym.Name,
			Kind:       sym.Kind,
			Language:   entry.Name,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			Lines:      countNonBlankLines(body),
			Cyclomatic: analysis.cyclomatic,
			Cognitive:  analysis.cognitive,
			MaxNesting: analysis.maxNesting,
			Parameters: countParameters(sym.Signature),
			Returns:    analysis.returns,
			BoolDepth:  analysis.boolDepth,

			LOC:             analysis.loc,
			Halstead:        analysis.halstead,
			Maintainability: mi,
		}
		if opts.MinCyclomatic > 0 && metrics.Cyclomatic < opts.MinCyclomatic {
			continue
		}
		functions = append(functions, metrics)
	}
	return functions
}

type functionAnalysis struct {
	cyclomatic int
	cognitive  int
	maxNesting int
	returns    int
	boolDepth  int
	loc        LOCMetrics
	halstead   HalsteadMetrics
	valid      bool
}

func analyzeFunctionBody(parser *gotreesitter.Parser, entry *grammars.LangEntry, lang *gotreesitter.Language, body []byte) functionAnalysis {
	var (
		tree     *gotreesitter.Tree
		parseErr error
	)
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(body, lang)
		tree, parseErr = parser.ParseWithTokenSource(body, ts)
	} else {
		tree, parseErr = parser.Parse(body)
	}
	if parseErr != nil || tree == nil {
		return functionAnalysis{}
	}
	defer tree.Release()
	if tree.ParseStoppedEarly() {
		return functionAnalysis{}
	}
	rootNode := tree.RootNode()
	if rootNode == nil {
		return functionAnalysis{}
	}
	cyc, cog, maxNest, rets, boolDep := computeComplexity(rootNode, lang, body)
	loc, hal := analyzeSourceMetrics(rootNode, lang, body)
	return functionAnalysis{
		cyclomatic: cyc,
		cognitive:  cog,
		maxNesting: maxNest,
		returns:    rets,
		boolDepth:  boolDep,
		loc:        loc,
		halstead:   hal,
		valid:      true,
	}
}

// EnrichWithXref populates fan-in and fan-out metrics by matching functions against xref definitions.
func EnrichWithXref(report *Report, graph xref.Graph) {
	if report == nil || len(report.Functions) == 0 {
		return
	}

	// Build a lookup from (file, name, startLine) to definition ID.
	defLookup := map[string]string{}
	for _, def := range graph.Definitions {
		key := fmt.Sprintf("%s\x00%s\x00%d", def.File, def.Name, def.StartLine)
		defLookup[key] = def.ID
	}

	for i := range report.Functions {
		fn := &report.Functions[i]
		key := fmt.Sprintf("%s\x00%s\x00%d", fn.File, fn.Name, fn.StartLine)
		defID, ok := defLookup[key]
		if !ok {
			continue
		}
		fn.FanIn = graph.IncomingCount(defID)
		fn.FanOut = graph.OutgoingCount(defID)
	}
}

// extractBody returns the source bytes between startLine and endLine
// (1-indexed, inclusive). The returned slice shares the source backing array,
// so callers must not retain it beyond the lifetime of source.
func extractBody(source []byte, startLine, endLine int) []byte {
	return extractBodyWithOffsets(source, buildLineOffsets(source), startLine, endLine)
}

func buildLineOffsets(source []byte) []int {
	offsets := make([]int, 1, 64)
	for i, b := range source {
		if b == '\n' && i+1 < len(source) {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func extractBodyWithOffsets(source []byte, offsets []int, startLine, endLine int) []byte {
	if len(source) == 0 || startLine > endLine || endLine < 1 || startLine > len(offsets) {
		return nil
	}
	if startLine < 1 {
		startLine = 1
	}
	start := offsets[startLine-1]
	end := len(source)
	if endLine < len(offsets) {
		end = offsets[endLine]
	}
	if start >= end {
		return nil
	}
	return source[start:end]
}

// countNonBlankLines counts lines that contain at least one non-whitespace character.
// Uses byte scanning to avoid allocating a []string from Split.
func countNonBlankLines(body []byte) int {
	count := 0
	hasNonSpace := false
	for _, b := range body {
		switch b {
		case '\n':
			if hasNonSpace {
				count++
			}
			hasNonSpace = false
		case ' ', '\t', '\r':
			// whitespace, skip
		default:
			hasNonSpace = true
		}
	}
	// Count final line if it has content and no trailing newline.
	if hasNonSpace {
		count++
	}
	return count
}

// countParameters parses the signature string to count function parameters.
// Counts commas between the first '(' and last ')' then adds 1, or 0 if empty.
func countParameters(signature string) int {
	start := strings.Index(signature, "(")
	if start < 0 {
		return 0
	}
	end := strings.LastIndex(signature, ")")
	if end < 0 || end <= start {
		return 0
	}
	inner := strings.TrimSpace(signature[start+1 : end])
	if inner == "" {
		return 0
	}
	return strings.Count(inner, ",") + 1
}

// isCallableSymbol returns true for function and method definition kinds.
func isCallableSymbol(kind string) bool {
	switch kind {
	case "function_definition", "method_definition":
		return true
	default:
		return false
	}
}

// isBranchingNode returns true for node types that represent control flow branching.
func isBranchingNode(nodeType string) bool {
	switch nodeType {
	case "if_statement", "if_expression", "if_let_expression",
		"for_statement", "for_expression", "for_in_statement",
		"while_statement", "while_expression",
		"switch_statement", "switch_expression",
		"match_expression", "match_statement",
		"case_clause", "case_statement", "match_arm",
		"try_statement",
		"catch_clause", "except_clause", "rescue",
		"conditional_expression", "ternary_expression",
		"elif_clause", "else_if_clause":
		return true
	default:
		return false
	}
}

// isLogicalOperatorNode returns true for nodes that represent logical operators (&&, ||, and, or).
func isLogicalOperatorNode(nodeType string) bool {
	switch nodeType {
	case "binary_expression", "boolean_operator", "logical_expression":
		return true
	default:
		return false
	}
}

// containsLogicalOperator checks if the node's text contains a logical operator.
func containsLogicalOperator(text string) bool {
	if strings.Contains(text, "&&") || strings.Contains(text, "||") {
		return true
	}
	if strings.Contains(text, " and ") || strings.Contains(text, " or ") {
		return true
	}
	return false
}

// computeComplexity performs a recursive walk of the AST to compute cyclomatic complexity,
// cognitive complexity, and maximum nesting depth.
func computeComplexity(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte) (cyclomatic, cognitive, maxNesting, returns, boolDepth int) {
	cyclomatic = 1 // base path
	cognitive = 0
	maxNesting = 0
	returns = 0
	boolDepth = 0

	var walk func(node *gotreesitter.Node, branchingDepth int, boolNesting int)
	walk = func(node *gotreesitter.Node, branchingDepth int, boolNesting int) {
		if node == nil {
			return
		}

		nodeType := node.Type(lang)
		isBranch := isBranchingNode(nodeType)

		if isBranch {
			cyclomatic++
			cognitive += 1 + branchingDepth
			branchingDepth++
			if branchingDepth > maxNesting {
				maxNesting = branchingDepth
			}
		}

		if nodeType == "return_statement" {
			returns++
		}

		if isLogicalOperatorNode(nodeType) {
			text := node.Text(source)
			if containsLogicalOperator(text) {
				// Count individual logical operators in direct text.
				// For nested binary_expression nodes, each node contributes its own operator.
				cyclomatic++
				cognitive++
				boolNesting++
				if boolNesting > boolDepth {
					boolDepth = boolNesting
				}
			}
		}

		for _, child := range node.Children() {
			walk(child, branchingDepth, boolNesting)
		}
	}

	walk(root, 0, 0)
	return
}

// sortFunctions sorts the function list descending by the selected field,
// with stable tiebreak by file + startLine.
func sortFunctions(functions []FunctionMetrics, sortField string) {
	sort.SliceStable(functions, func(i, j int) bool {
		// Float-valued fields are sorted separately: volume descending (most
		// voluminous first), maintainability ascending (worst/lowest first).
		switch sortField {
		case "volume":
			if fi, fj := functions[i].Halstead.Volume, functions[j].Halstead.Volume; fi != fj {
				return fi > fj
			}
			return tiebreak(functions, i, j)
		case "mi":
			if fi, fj := functions[i].Maintainability.Original, functions[j].Maintainability.Original; fi != fj {
				return fi < fj
			}
			return tiebreak(functions, i, j)
		}

		var vi, vj int
		switch sortField {
		case "cognitive":
			vi, vj = functions[i].Cognitive, functions[j].Cognitive
		case "lines":
			vi, vj = functions[i].Lines, functions[j].Lines
		case "nesting":
			vi, vj = functions[i].MaxNesting, functions[j].MaxNesting
		default: // "cyclomatic" or unspecified
			vi, vj = functions[i].Cyclomatic, functions[j].Cyclomatic
		}
		if vi != vj {
			return vi > vj // descending
		}
		return tiebreak(functions, i, j)
	})
}

// tiebreak orders equal-metric functions stably by file then start line.
func tiebreak(functions []FunctionMetrics, i, j int) bool {
	if functions[i].File != functions[j].File {
		return functions[i].File < functions[j].File
	}
	return functions[i].StartLine < functions[j].StartLine
}

// computeSummary calculates aggregate statistics for the function metrics.
func computeSummary(functions []FunctionMetrics) Summary {
	n := len(functions)
	if n == 0 {
		return Summary{}
	}

	var sumCyc, sumCog, sumLines, sumNesting int
	maxCyc, maxCog, maxLines := 0, 0, 0
	var sumVolume, sumMI float64
	minMI := math.Inf(1)

	for _, fn := range functions {
		sumCyc += fn.Cyclomatic
		sumCog += fn.Cognitive
		sumLines += fn.Lines
		sumNesting += fn.MaxNesting
		sumVolume += fn.Halstead.Volume
		sumMI += fn.Maintainability.Original
		if fn.Maintainability.Original < minMI {
			minMI = fn.Maintainability.Original
		}

		if fn.Cyclomatic > maxCyc {
			maxCyc = fn.Cyclomatic
		}
		if fn.Cognitive > maxCog {
			maxCog = fn.Cognitive
		}
		if fn.Lines > maxLines {
			maxLines = fn.Lines
		}
	}

	// P90 cyclomatic: sort a copy of cyclomatic values and pick the 90th percentile.
	cycValues := make([]int, n)
	for i, fn := range functions {
		cycValues[i] = fn.Cyclomatic
	}
	sort.Ints(cycValues)
	p90Index := int(float64(n-1) * 0.9)
	if p90Index >= n {
		p90Index = n - 1
	}

	return Summary{
		Count:         n,
		AvgCyclomatic: float64(sumCyc) / float64(n),
		MaxCyclomatic: maxCyc,
		P90Cyclomatic: cycValues[p90Index],
		AvgCognitive:  float64(sumCog) / float64(n),
		MaxCognitive:  maxCog,
		AvgLines:      float64(sumLines) / float64(n),
		MaxLines:      maxLines,
		AvgMaxNesting: float64(sumNesting) / float64(n),

		AvgVolume:          sumVolume / float64(n),
		AvgMaintainability: sumMI / float64(n),
		MinMaintainability: minMI,
	}
}
