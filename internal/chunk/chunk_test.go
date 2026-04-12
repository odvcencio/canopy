package chunk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/index"
)

func TestBuild_ASTBoundaryChunks(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "sample.go")
	source := `package sample

import "fmt"

type Service struct{}

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	report, err := Build(idx, Options{TokenBudget: 400})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.ChunkCount < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", report.ChunkCount)
	}
	if !hasChunkKind(report, "file_header") {
		t.Fatal("expected file_header chunk")
	}
	if !hasChunkKind(report, "function_definition") {
		t.Fatal("expected function_definition chunk")
	}
	if !hasChunkKind(report, "type_definition") {
		t.Fatal("expected type_definition chunk")
	}
}

func TestBuild_TruncatesToTokenBudget(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "sample.go")
	source := `package sample

func Long() {
	println("one")
	println("two")
	println("three")
	println("four")
	println("five")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	report, err := Build(idx, Options{TokenBudget: 5})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	foundTruncated := false
	for _, chunk := range report.Chunks {
		if chunk.Kind != "function_definition" {
			continue
		}
		if chunk.Truncated {
			foundTruncated = true
		}
	}
	if !foundTruncated {
		t.Fatal("expected at least one truncated function chunk")
	}
}

func hasChunkKind(report Report, kind string) bool {
	for _, chunk := range report.Chunks {
		if chunk.Kind == kind {
			return true
		}
	}
	return false
}
