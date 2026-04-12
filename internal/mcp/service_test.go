package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/odvcencio/canopy/internal/bridge"
	"github.com/odvcencio/canopy/internal/chunk"
	"github.com/odvcencio/canopy/internal/contextpack"
	"github.com/odvcencio/canopy/internal/deps"
	"github.com/odvcencio/canopy/internal/files"
	"github.com/odvcencio/canopy/pkg/refactor"
	"github.com/odvcencio/canopy/internal/stats"
	"github.com/odvcencio/canopy/pkg/structdiff"
	"github.com/odvcencio/canopy/pkg/xref"
)

func TestServiceToolsIncludesCoreRoadmapTools(t *testing.T) {
	service := NewService(".", "")
	tools := service.Tools()
	if len(tools) < 5 {
		t.Fatalf("expected at least 5 tools, got %d", len(tools))
	}

	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	for _, name := range []string{"gts_grep", "gts_map", "gts_query", "gts_refs", "gts_context", "gts_scope", "gts_deps", "gts_callgraph", "gts_dead", "gts_chunk", "gts_lint", "gts_refactor", "gts_diff", "gts_stats", "gts_files", "gts_bridge"} {
		if !seen[name] {
			t.Fatalf("expected tool %q to be present", name)
		}
	}
}

func TestServiceToolsAreSortedAndSchemasNormalized(t *testing.T) {
	service := NewService(".", "")
	tools := service.Tools()
	if len(tools) == 0 {
		t.Fatalf("expected tools to be non-empty")
	}

	toolNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolNames = append(toolNames, tool.Name)
		schema := tool.InputSchema
		if schema == nil {
			t.Fatalf("tool %q schema is nil", tool.Name)
		}
		if schema["type"] != "object" {
			t.Fatalf("tool %q schema type must be object, got %#v", tool.Name, schema["type"])
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q properties must be map[string]any, got %T", tool.Name, schema["properties"])
		}
		if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("tool %q must set additionalProperties=false, got %#v", tool.Name, schema["additionalProperties"])
		}
		requiredRaw, ok := schema["required"]
		if !ok {
			continue
		}
		required, ok := requiredRaw.([]string)
		if !ok {
			t.Fatalf("tool %q required must be []string, got %T", tool.Name, requiredRaw)
		}
		if !sort.StringsAreSorted(required) {
			t.Fatalf("tool %q required keys must be sorted, got %v", tool.Name, required)
		}
		for _, key := range required {
			if _, exists := properties[key]; !exists {
				t.Fatalf("tool %q required key %q missing from properties", tool.Name, key)
			}
		}
	}

	if !sort.StringsAreSorted(toolNames) {
		t.Fatalf("tools should be sorted alphabetically, got %v", toolNames)
	}
}

func TestFinalizeToolSchemaNormalizesRequired(t *testing.T) {
	tool := Tool{
		Name: "sample",
		InputSchema: map[string]any{
			"type": "unexpected",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"path": map[string]any{"type": "string"},
			},
			"required": []any{" path ", "name", "missing", 9, "name"},
		},
	}

	finalizeToolSchema(&tool)

	if tool.InputSchema["type"] != "object" {
		t.Fatalf("schema type should be normalized to object, got %#v", tool.InputSchema["type"])
	}
	if additional, ok := tool.InputSchema["additionalProperties"].(bool); !ok || additional {
		t.Fatalf("additionalProperties should default to false, got %#v", tool.InputSchema["additionalProperties"])
	}

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required should be []string, got %T", tool.InputSchema["required"])
	}
	expected := []string{"name", "path"}
	if !reflect.DeepEqual(required, expected) {
		t.Fatalf("expected required=%v, got %v", expected, required)
	}
}

func TestServiceCallRefsAndQuery(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func A() {}

func B() {
	A()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	service := NewService(tmpDir, "")
	refsResultRaw, err := service.Call("gts_refs", map[string]any{
		"name": "A",
	})
	if err != nil {
		t.Fatalf("gts_refs call failed: %v", err)
	}
	refsResult, ok := refsResultRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected refs result map, got %T", refsResultRaw)
	}
	if refsResult["count"].(int) != 1 {
		t.Fatalf("expected refs count=1, got %#v", refsResult["count"])
	}

	grepResultRaw, err := service.Call("gts_grep", map[string]any{
		"selector": "function_definition[name=/^A$/]",
	})
	if err != nil {
		t.Fatalf("gts_grep call failed: %v", err)
	}
	grepResult, ok := grepResultRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected grep result map, got %T", grepResultRaw)
	}
	if grepResult["count"].(int) != 1 {
		t.Fatalf("expected grep count=1, got %#v", grepResult["count"])
	}

	mapResultRaw, err := service.Call("gts_map", map[string]any{})
	if err != nil {
		t.Fatalf("gts_map call failed: %v", err)
	}
	mapResult, ok := mapResultRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected map result map, got %T", mapResultRaw)
	}
	if mapResult["file_count"].(int) != 1 {
		t.Fatalf("expected map file_count=1, got %#v", mapResult["file_count"])
	}

	queryResultRaw, err := service.Call("gts_query", map[string]any{
		"pattern": "(function_declaration (identifier) @name)",
	})
	if err != nil {
		t.Fatalf("gts_query call failed: %v", err)
	}
	queryResult, ok := queryResultRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected query result map, got %T", queryResultRaw)
	}
	if queryResult["count"].(int) != 2 {
		t.Fatalf("expected query count=2, got %#v", queryResult["count"])
	}
}

func TestServiceCallContextAndDeps(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

import "fmt"

func helper() {
	fmt.Println("ok")
}

func work() {
	helper()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module sample\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}

	service := NewService(tmpDir, "")
	contextResultRaw, err := service.Call("gts_context", map[string]any{
		"file":           sourcePath,
		"line":           10,
		"semantic":       true,
		"semantic_depth": 2,
	})
	if err != nil {
		t.Fatalf("gts_context call failed: %v", err)
	}
	contextResult, ok := contextResultRaw.(contextpack.Report)
	if ok {
		if contextResult.File != "main.go" {
			t.Fatalf("expected context file main.go, got %q", contextResult.File)
		}
		if !contextResult.Semantic || contextResult.SemanticDepth != 2 {
			t.Fatalf("expected semantic context depth=2, got semantic=%t depth=%d", contextResult.Semantic, contextResult.SemanticDepth)
		}
		if len(contextResult.Related) == 0 {
			t.Fatalf("expected semantic related symbols, got none")
		}
	} else {
		t.Fatalf("expected contextpack.Report, got %T", contextResultRaw)
	}

	depsResultRaw, err := service.Call("gts_deps", map[string]any{
		"by": "package",
	})
	if err != nil {
		t.Fatalf("gts_deps call failed: %v", err)
	}
	depsResult, ok := depsResultRaw.(deps.Report)
	if !ok {
		t.Fatalf("expected deps.Report, got %T", depsResultRaw)
	}
	if depsResult.Mode != "package" {
		t.Fatalf("expected deps mode package, got %q", depsResult.Mode)
	}
}

func TestServiceCallgraphAndDead(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func Used() {}
func Dead() {}

func main() {
	Used()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	service := NewService(tmpDir, "")
	callgraphRaw, err := service.Call("gts_callgraph", map[string]any{
		"name":    "main",
		"depth":   2,
		"reverse": false,
	})
	if err != nil {
		t.Fatalf("gts_callgraph call failed: %v", err)
	}
	callgraph, ok := callgraphRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected callgraph map result, got %T", callgraphRaw)
	}
	edges, ok := callgraph["edges"].([]xref.MaterializedEdge)
	if !ok {
		t.Fatalf("expected callgraph edges slice, got %T", callgraph["edges"])
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 callgraph edge, got %d", len(edges))
	}

	deadRaw, err := service.Call("gts_dead", map[string]any{
		"kind": "function",
	})
	if err != nil {
		t.Fatalf("gts_dead call failed: %v", err)
	}
	dead, ok := deadRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected dead map result, got %T", deadRaw)
	}
	count, ok := dead["count"].(int)
	if !ok {
		t.Fatalf("expected dead count int, got %T", dead["count"])
	}
	if count != 1 {
		t.Fatalf("expected dead count=1, got %d", count)
	}
}

func TestServiceChunkAndLint(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	patternPath := filepath.Join(tmpDir, "empty.scm")
	source := `package sample

func Empty() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile source failed: %v", err)
	}
	if err := os.WriteFile(patternPath, []byte(`(function_declaration (block) @violation)`), 0o644); err != nil {
		t.Fatalf("WriteFile pattern failed: %v", err)
	}

	service := NewService(tmpDir, "")

	chunkRaw, err := service.Call("gts_chunk", map[string]any{
		"tokens": 200,
	})
	if err != nil {
		t.Fatalf("gts_chunk call failed: %v", err)
	}
	chunkReport, ok := chunkRaw.(chunk.Report)
	if !ok {
		t.Fatalf("expected chunk.Report, got %T", chunkRaw)
	}
	if chunkReport.ChunkCount == 0 {
		t.Fatalf("expected non-zero chunks")
	}

	lintRaw, err := service.Call("gts_lint", map[string]any{
		"pattern": patternPath,
	})
	if err != nil {
		t.Fatalf("gts_lint call failed: %v", err)
	}
	lintResult, ok := lintRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected lint map result, got %T", lintRaw)
	}
	count, ok := lintResult["count"].(int)
	if !ok {
		t.Fatalf("expected lint count int, got %T", lintResult["count"])
	}
	if count == 0 {
		t.Fatalf("expected lint violations > 0")
	}
}

func TestServiceRefactorAndDiff(t *testing.T) {
	tmpDir := t.TempDir()
	refactorDir := filepath.Join(tmpDir, "refactor")
	beforeDir := filepath.Join(tmpDir, "before")
	afterDir := filepath.Join(tmpDir, "after")
	if err := os.MkdirAll(refactorDir, 0o755); err != nil {
		t.Fatalf("MkdirAll refactorDir failed: %v", err)
	}
	if err := os.MkdirAll(beforeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll beforeDir failed: %v", err)
	}
	if err := os.MkdirAll(afterDir, 0o755); err != nil {
		t.Fatalf("MkdirAll afterDir failed: %v", err)
	}

	refactorSourcePath := filepath.Join(refactorDir, "main.go")
	refactorSource := `package sample

func OldName() {}

func Use() {
	OldName()
}
`
	if err := os.WriteFile(refactorSourcePath, []byte(refactorSource), 0o644); err != nil {
		t.Fatalf("WriteFile refactor source failed: %v", err)
	}

	beforeSource := `package sample

func A() {}
`
	afterSource := `package sample

func A() {}
func B() {}
`
	if err := os.WriteFile(filepath.Join(beforeDir, "main.go"), []byte(beforeSource), 0o644); err != nil {
		t.Fatalf("WriteFile before source failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(afterDir, "main.go"), []byte(afterSource), 0o644); err != nil {
		t.Fatalf("WriteFile after source failed: %v", err)
	}

	readOnlyService := NewService(refactorDir, "")
	if _, err := readOnlyService.Call("gts_refactor", map[string]any{
		"selector":  "function_definition[name=/^OldName$/]",
		"new_name":  "NewName",
		"callsites": true,
		"write":     true,
	}); err == nil {
		t.Fatalf("expected write refactor to fail when writes are disabled")
	}

	service := NewServiceWithOptions(refactorDir, "", ServiceOptions{AllowWrites: true})
	refactorRaw, err := service.Call("gts_refactor", map[string]any{
		"selector":  "function_definition[name=/^OldName$/]",
		"new_name":  "NewName",
		"callsites": true,
		"write":     true,
	})
	if err != nil {
		t.Fatalf("gts_refactor call failed: %v", err)
	}
	refactorReport, ok := refactorRaw.(refactor.Report)
	if !ok {
		t.Fatalf("expected refactor.Report, got %T", refactorRaw)
	}
	if refactorReport.AppliedEdits == 0 {
		t.Fatalf("expected applied refactor edits, got report %+v", refactorReport)
	}
	updatedSource, err := os.ReadFile(refactorSourcePath)
	if err != nil {
		t.Fatalf("ReadFile refactor source failed: %v", err)
	}
	if !strings.Contains(string(updatedSource), "NewName()") {
		t.Fatalf("expected refactor output to contain NewName(), got:\n%s", string(updatedSource))
	}

	diffRaw, err := service.Call("gts_diff", map[string]any{
		"before_path": beforeDir,
		"after_path":  afterDir,
	})
	if err != nil {
		t.Fatalf("gts_diff call failed: %v", err)
	}
	diffReport, ok := diffRaw.(structdiff.Report)
	if !ok {
		t.Fatalf("expected structdiff.Report, got %T", diffRaw)
	}
	if diffReport.Stats.AddedSymbols == 0 {
		t.Fatalf("expected added symbols in diff report, got %+v", diffReport.Stats)
	}
}

func TestServiceStatsFilesBridge(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module sample\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "internal", "x"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	mainSource := `package main

import "sample/internal/x"

func main() { x.Value() }
`
	xSource := `package x

func Value() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "internal", "x", "x.go"), []byte(xSource), 0o644); err != nil {
		t.Fatalf("WriteFile x.go failed: %v", err)
	}

	service := NewService(tmpDir, "")

	statsRaw, err := service.Call("gts_stats", map[string]any{
		"top": 5,
	})
	if err != nil {
		t.Fatalf("gts_stats call failed: %v", err)
	}
	statsReport, ok := statsRaw.(stats.Report)
	if !ok {
		t.Fatalf("expected stats.Report, got %T", statsRaw)
	}
	if statsReport.FileCount == 0 || statsReport.SymbolCount == 0 {
		t.Fatalf("expected non-empty stats report, got %+v", statsReport)
	}

	filesRaw, err := service.Call("gts_files", map[string]any{
		"sort": "symbols",
		"top":  10,
	})
	if err != nil {
		t.Fatalf("gts_files call failed: %v", err)
	}
	filesReport, ok := filesRaw.(files.Report)
	if !ok {
		t.Fatalf("expected files.Report, got %T", filesRaw)
	}
	if filesReport.TotalFiles == 0 || len(filesReport.Entries) == 0 {
		t.Fatalf("expected non-empty files report, got %+v", filesReport)
	}

	bridgeRaw, err := service.Call("gts_bridge", map[string]any{
		"top": 5,
	})
	if err != nil {
		t.Fatalf("gts_bridge call failed: %v", err)
	}
	bridgeReport, ok := bridgeRaw.(bridge.Report)
	if !ok {
		t.Fatalf("expected bridge.Report, got %T", bridgeRaw)
	}
	if bridgeReport.ComponentCount == 0 {
		t.Fatalf("expected non-empty bridge report, got %+v", bridgeReport)
	}
}
