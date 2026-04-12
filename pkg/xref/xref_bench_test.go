package xref

import (
	"fmt"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func BenchmarkBuild(b *testing.B) {
	idx := buildSyntheticIndex(100, 500, 2000)
	b.ResetTimer()
	for b.Loop() {
		_, _ = Build(idx)
	}
}

func BenchmarkBuild_Small(b *testing.B) {
	idx := buildSyntheticIndex(10, 30, 100)
	b.ResetTimer()
	for b.Loop() {
		_, _ = Build(idx)
	}
}

func BenchmarkGraphFindDefinitions(b *testing.B) {
	idx := buildSyntheticIndex(100, 500, 2000)
	graph, err := Build(idx)
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = graph.FindDefinitions("func_", true)
	}
}

func BenchmarkGraphWalk(b *testing.B) {
	idx := buildSyntheticIndex(100, 500, 2000)
	graph, err := Build(idx)
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	roots, err := graph.FindDefinitions("func_0_0", false)
	if err != nil || len(roots) == 0 {
		b.Fatalf("FindDefinitions: err=%v roots=%d", err, len(roots))
	}
	rootIDs := []string{roots[0].ID}
	b.ResetTimer()
	for b.Loop() {
		graph.Walk(rootIDs, 3, false)
	}
}

// buildSyntheticIndex creates a realistic index with the specified number of
// files, symbols, and references distributed across those files.
func buildSyntheticIndex(numFiles, numSymbols, numRefs int) *model.Index {
	files := make([]model.FileSummary, numFiles)
	for i := range files {
		files[i] = model.FileSummary{
			Path:     fmt.Sprintf("pkg%d/file_%d.go", i%10, i),
			Language: "go",
		}
	}

	// Distribute symbols across files round-robin.
	symbolNames := make([]string, numSymbols)
	for s := 0; s < numSymbols; s++ {
		fileIdx := s % numFiles
		line := (s/numFiles)*20 + 1
		name := fmt.Sprintf("func_%d_%d", fileIdx, s)
		symbolNames[s] = name
		files[fileIdx].Symbols = append(files[fileIdx].Symbols, model.Symbol{
			File:      files[fileIdx].Path,
			Kind:      "function_definition",
			Name:      name,
			Signature: fmt.Sprintf("func %s()", name),
			StartLine: line,
			EndLine:   line + 15,
		})
	}

	// Distribute references across files, calling known symbol names.
	for r := 0; r < numRefs; r++ {
		fileIdx := r % numFiles
		targetName := symbolNames[r%numSymbols]
		// Place reference inside a symbol's line range in the same file.
		syms := files[fileIdx].Symbols
		if len(syms) == 0 {
			continue
		}
		enclosing := syms[r%len(syms)]
		refLine := enclosing.StartLine + 1
		if refLine > enclosing.EndLine {
			refLine = enclosing.EndLine
		}
		files[fileIdx].References = append(files[fileIdx].References, model.Reference{
			File:        files[fileIdx].Path,
			Kind:        "reference.call",
			Name:        targetName,
			StartLine:   refLine,
			EndLine:     refLine,
			StartColumn: 2,
			EndColumn:   2 + len(targetName),
		})
	}

	return &model.Index{
		Root:  "/synthetic",
		Files: files,
	}
}
