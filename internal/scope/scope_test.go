package scope

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/index"
)

func TestBuild_CollectsFunctionScope(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "sample.go")
	source := `package sample

import "fmt"

const Pi = 3.14
type Service struct{}

func helper() {}

func (s *Service) Work(input string) (out int) {
	x := 1
	if x > 0 {
		y := input
		fmt.Println(y)
	}
	out = x
	return out
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	report, err := Build(idx, Options{
		FilePath: sourcePath,
		Line:     13, // fmt.Println(y)
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if report.Package != "sample" {
		t.Fatalf("expected package sample, got %q", report.Package)
	}
	if report.Focus == nil || report.Focus.Name != "Work" {
		t.Fatalf("expected focus Work, got %#v", report.Focus)
	}

	for _, name := range []string{"fmt", "Service", "helper", "s", "input", "out", "x", "y"} {
		if !hasSymbol(report, name) {
			t.Fatalf("expected symbol %q in scope report", name)
		}
	}
}

func TestBuild_RejectsUnsupportedLanguage(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "notes.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	_, err = Build(idx, Options{
		FilePath: mdPath,
		Line:     1,
	})
	if err == nil {
		t.Fatal("expected unsupported language file to fail")
	}
}

func TestBuild_PythonScope(t *testing.T) {
	tmpDir := t.TempDir()
	pyPath := filepath.Join(tmpDir, "demo.py")
	source := `import os

def helper():
    pass

def work(name):
    x = 1
    print(x)
`
	if err := os.WriteFile(pyPath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	report, err := Build(idx, Options{
		FilePath: pyPath,
		Line:     7, // x = 1
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify package-level symbols from index are visible
	for _, name := range []string{"helper", "work"} {
		if !hasSymbol(report, name) {
			t.Fatalf("expected symbol %q in python scope report", name)
		}
	}
}

func TestBuild_ExcludesCrossTestnessSymbols(t *testing.T) {
	tmpDir := t.TempDir()
	mainSource := `package sample

func MainOnly() {}
`
	testSource := `package sample

func TestOnly() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte(testSource), 0o644); err != nil {
		t.Fatalf("WriteFile main_test.go failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	report, err := Build(idx, Options{
		FilePath: filepath.Join(tmpDir, "main.go"),
		Line:     3,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if hasSymbol(report, "TestOnly") {
		t.Fatalf("did not expect TestOnly in non-test scope symbols: %+v", report.Symbols)
	}
}

func hasSymbol(report Report, name string) bool {
	for _, symbol := range report.Symbols {
		if symbol.Name == name {
			return true
		}
	}
	return false
}
