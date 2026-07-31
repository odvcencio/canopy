package changeintel

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"m31labs.dev/canopy/pkg/complexity"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

// entityKey identifies a structural entity by file (in head-path space —
// renames are pre-translated before matching), kind, receiver, and name.
type entityKey struct {
	file     string
	kind     string
	receiver string
	name     string
}

// entityBuilder accumulates base/head state for one entityKey while entity
// matching is in progress.
type entityBuilder struct {
	key      entityKey
	baseFile string
	baseSym  *model.Symbol
	headSym  *model.Symbol
	baseM    EntityMetrics
	headM    EntityMetrics
	hasBaseM bool
	hasHeadM bool
}

// analyzeEntities matches structural entities between baseIndex and
// headIndex across the given file changes, computes true (head - base)
// complexity deltas for matched callable entities, and derives the public
// API surface delta. Renames are resolved before matching: a base entity's
// key uses the file's head-side path (per FileChange.OldPath), so a
// function moved into a renamed file is still recognized as the same
// entity rather than reported as remove+add.
func analyzeEntities(
	changes []FileChange,
	baseIndex, headIndex *model.Index,
	baseContent, headContent map[string][]byte,
	fileLang map[string]string,
) ([]EntityChange, ComplexityDelta, APIDelta) {
	baseFiles := indexFilesByPath(baseIndex)
	headFiles := indexFilesByPath(headIndex)

	baseMetrics := indexFunctionMetrics(enrichFanInOut(computeFunctionMetrics(baseIndex, baseContent), baseIndex))
	headMetrics := indexFunctionMetrics(enrichFanInOut(computeFunctionMetrics(headIndex, headContent), headIndex))

	builders := map[entityKey]*entityBuilder{}
	var order []entityKey
	get := func(k entityKey) *entityBuilder {
		if b, ok := builders[k]; ok {
			return b
		}
		b := &entityBuilder{key: k}
		builders[k] = b
		order = append(order, k)
		return b
	}

	for _, fc := range changes {
		if fc.Status != FileRemoved {
			if hf, ok := headFiles[fc.Path]; ok {
				for i := range hf.Symbols {
					sym := hf.Symbols[i]
					k := entityKey{file: fc.Path, kind: sym.Kind, receiver: sym.Receiver, name: sym.Name}
					b := get(k)
					b.headSym = &hf.Symbols[i]
					if m, ok := lookupMetrics(headMetrics, fc.Path, sym.Name, sym.StartLine); ok {
						b.headM, b.hasHeadM = m, true
					}
				}
			}
		}

		baseFilePath := fc.Path
		if fc.Status == FileRenamed {
			baseFilePath = fc.OldPath
		}
		if fc.Status != FileAdded {
			if bf, ok := baseFiles[baseFilePath]; ok {
				for i := range bf.Symbols {
					sym := bf.Symbols[i]
					k := entityKey{file: fc.Path, kind: sym.Kind, receiver: sym.Receiver, name: sym.Name}
					b := get(k)
					b.baseFile = baseFilePath
					b.baseSym = &bf.Symbols[i]
					if m, ok := lookupMetrics(baseMetrics, baseFilePath, sym.Name, sym.StartLine); ok {
						b.baseM, b.hasBaseM = m, true
					}
				}
			}
		}
	}

	sort.Slice(order, func(i, j int) bool { return entityKeyLess(order[i], order[j]) })

	entities := make([]EntityChange, 0, len(order))
	var delta ComplexityDelta
	var apiChanged []APISymbolChange

	var baseOnly, headOnly []*entityBuilder

	for _, k := range order {
		b := builders[k]
		ec := EntityChange{
			File:     k.file,
			Kind:     k.kind,
			Receiver: k.receiver,
			Name:     k.name,
			BaseFile: b.baseFile,
		}

		switch {
		case b.baseSym != nil && b.headSym != nil:
			ec.Base = b.baseM
			ec.Head = b.headM
			ec.Delta = subtractMetrics(b.headM, b.baseM)
			ec.SignatureChanged = b.baseSym.Signature != b.headSym.Signature
			ec.SpanChanged = b.baseSym.StartLine != b.headSym.StartLine || b.baseSym.EndLine != b.headSym.EndLine
			ec.MatchConfidence = 1.0
			if ec.SignatureChanged || ec.SpanChanged {
				ec.Status = EntityModified
			} else {
				ec.Status = EntityUnchanged
			}

			if ec.SignatureChanged {
				lang := fileLang[k.file]
				if isExported(k.name, lang) {
					apiChanged = append(apiChanged, APISymbolChange{
						File: k.file, Kind: k.kind, Name: k.name, Receiver: k.receiver,
						Change:         APISignatureChanged,
						BaseSignature:  b.baseSym.Signature,
						HeadSignature:  b.headSym.Signature,
						BaseVisibility: visibilityLabel(k.name, lang),
						HeadVisibility: visibilityLabel(k.name, lang),
					})
				}
			}

			if b.hasBaseM && b.hasHeadM {
				delta.MatchedEntities++
				delta.BaseTotalCyclomatic += b.baseM.Cyclomatic
				delta.HeadTotalCyclomatic += b.headM.Cyclomatic
				delta.BaseTotalCognitive += b.baseM.Cognitive
				delta.HeadTotalCognitive += b.headM.Cognitive
				switch {
				case b.headM.Cyclomatic > b.baseM.Cyclomatic:
					delta.IncreasedFunctions++
				case b.headM.Cyclomatic < b.baseM.Cyclomatic:
					delta.DecreasedFunctions++
				default:
					delta.UnchangedFunctions++
				}
			}
		case b.headSym != nil:
			ec.Status = EntityAdded
			ec.Head = b.headM
			ec.Delta = b.headM
			ec.MatchConfidence = 0
			headOnly = append(headOnly, b)
		case b.baseSym != nil:
			ec.Status = EntityRemoved
			ec.Base = b.baseM
			ec.Delta = negateMetrics(b.baseM)
			ec.MatchConfidence = 0
			baseOnly = append(baseOnly, b)
		default:
			continue
		}

		entities = append(entities, ec)
	}

	if delta.MatchedEntities > 0 {
		delta.DeltaCyclomatic = delta.HeadTotalCyclomatic - delta.BaseTotalCyclomatic
		delta.DeltaCognitive = delta.HeadTotalCognitive - delta.BaseTotalCognitive
		delta.BaseAvgCyclomatic = float64(delta.BaseTotalCyclomatic) / float64(delta.MatchedEntities)
		delta.HeadAvgCyclomatic = float64(delta.HeadTotalCyclomatic) / float64(delta.MatchedEntities)
	}

	api := reconcileAPIDelta(baseOnly, headOnly, apiChanged, fileLang)
	return entities, delta, api
}

func entityKeyLess(a, b entityKey) bool {
	if a.file != b.file {
		return a.file < b.file
	}
	if a.kind != b.kind {
		return a.kind < b.kind
	}
	if a.receiver != b.receiver {
		return a.receiver < b.receiver
	}
	return a.name < b.name
}

func indexFilesByPath(idx *model.Index) map[string]model.FileSummary {
	out := map[string]model.FileSummary{}
	if idx == nil {
		return out
	}
	for _, f := range idx.Files {
		out[f.Path] = f
	}
	return out
}

// computeFunctionMetrics runs complexity analysis for every file in idx that
// has in-memory content available, using AnalyzeContent so base-ref content
// pulled via `git show` never needs to exist on disk.
func computeFunctionMetrics(idx *model.Index, content map[string][]byte) []complexity.FunctionMetrics {
	if idx == nil {
		return nil
	}
	cache := complexity.NewParserCache()
	var all []complexity.FunctionMetrics
	for _, f := range idx.Files {
		src, ok := content[f.Path]
		if !ok || len(src) == 0 {
			continue
		}
		all = append(all, complexity.AnalyzeContent(f, src, cache, complexity.Options{})...)
	}
	return all
}

// enrichFanInOut populates FanIn/FanOut on functions by matching them
// against idx's xref graph, then returns the same slice for chaining.
func enrichFanInOut(functions []complexity.FunctionMetrics, idx *model.Index) []complexity.FunctionMetrics {
	if idx == nil || len(functions) == 0 {
		return functions
	}
	graph, err := xref.Build(idx)
	if err != nil {
		return functions
	}
	report := &complexity.Report{Functions: functions}
	complexity.EnrichWithXref(report, graph)
	return functions
}

func functionMetricsKey(file, name string, startLine int) string {
	return fmt.Sprintf("%s\x00%s\x00%d", file, name, startLine)
}

func indexFunctionMetrics(functions []complexity.FunctionMetrics) map[string]complexity.FunctionMetrics {
	out := make(map[string]complexity.FunctionMetrics, len(functions))
	for _, fn := range functions {
		out[functionMetricsKey(fn.File, fn.Name, fn.StartLine)] = fn
	}
	return out
}

func lookupMetrics(idx map[string]complexity.FunctionMetrics, file, name string, startLine int) (EntityMetrics, bool) {
	fn, ok := idx[functionMetricsKey(file, name, startLine)]
	if !ok {
		return EntityMetrics{}, false
	}
	return EntityMetrics{
		Cyclomatic: fn.Cyclomatic,
		Cognitive:  fn.Cognitive,
		Lines:      fn.Lines,
		FanIn:      fn.FanIn,
		FanOut:     fn.FanOut,
	}, true
}

func subtractMetrics(head, base EntityMetrics) EntityMetrics {
	return EntityMetrics{
		Cyclomatic: head.Cyclomatic - base.Cyclomatic,
		Cognitive:  head.Cognitive - base.Cognitive,
		Lines:      head.Lines - base.Lines,
		FanIn:      head.FanIn - base.FanIn,
		FanOut:     head.FanOut - base.FanOut,
	}
}

func negateMetrics(m EntityMetrics) EntityMetrics {
	return EntityMetrics{
		Cyclomatic: -m.Cyclomatic,
		Cognitive:  -m.Cognitive,
		Lines:      -m.Lines,
		FanIn:      -m.FanIn,
		FanOut:     -m.FanOut,
	}
}

// isExported reports whether name is part of the public API surface for the
// given language. Go uses case-based visibility; every other language falls
// back to the leading-underscore convention (Python, and the closest
// approximation available for languages with no visibility keyword in the
// structural index).
func isExported(name, language string) bool {
	if name == "" {
		return false
	}
	if strings.EqualFold(language, "go") {
		r := []rune(name)[0]
		return unicode.IsUpper(r)
	}
	return !strings.HasPrefix(name, "_")
}

func visibilityLabel(name, language string) string {
	if isExported(name, language) {
		return "public"
	}
	return "private"
}
