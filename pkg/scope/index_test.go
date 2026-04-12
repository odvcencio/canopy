package scope

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/index"
)

func TestBuildFromIndexPopulatesPackages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package foo\n\nfunc Alpha() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package foo\n\nfunc Beta() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	builder := index.NewBuilder()
	idx, err := builder.BuildPath(dir)
	if err != nil {
		t.Fatal(err)
	}

	graph, err := BuildFromIndex(idx, dir)
	if err != nil {
		t.Fatal(err)
	}

	if graph.FileScope("a.go") == nil {
		t.Error("missing file scope for a.go")
	}
	if graph.FileScope("b.go") == nil {
		t.Error("missing file scope for b.go")
	}

	var pkgScope *Scope
	for _, ps := range graph.Packages {
		pkgScope = ps
		break
	}
	if pkgScope == nil {
		t.Fatal("no package scope created")
	}

	names := make(map[string]bool)
	for _, d := range pkgScope.Defs {
		names[d.Name] = true
	}
	if !names["Alpha"] {
		t.Error("package scope missing Alpha")
	}
	if !names["Beta"] {
		t.Error("package scope missing Beta")
	}
}

func TestBuildFromIndex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(
		"package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	), 0644)

	builder := index.NewBuilder()
	idx, err := builder.BuildPath(dir)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}

	graph, err := BuildFromIndex(idx, dir)
	if err != nil {
		t.Fatalf("build scope graph: %v", err)
	}

	// Should have a file scope for main.go
	fs := graph.FileScope("main.go")
	if fs == nil {
		t.Fatalf("no file scope for main.go")
	}

	// Should have defs for "main" and import "fmt"
	hasFn := false
	hasImport := false
	for _, d := range fs.Defs {
		if d.Name == "main" && d.Kind == DefFunction {
			hasFn = true
		}
		if d.Kind == DefImport && d.Name == "fmt" {
			hasImport = true
		}
	}
	if !hasFn {
		t.Error("expected function def 'main'")
	}
	if !hasImport {
		t.Error("expected import def 'fmt'")
	}
}
