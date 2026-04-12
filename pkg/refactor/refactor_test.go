package refactor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/query"
)

func TestRenameDeclarations_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func OldName() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	selector, err := query.ParseSelector("function_definition[name=/^OldName$/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	report, err := RenameDeclarations(idx, selector, "NewName", Options{})
	if err != nil {
		t.Fatalf("RenameDeclarations returned error: %v", err)
	}
	if report.PlannedEdits != 1 {
		t.Fatalf("expected 1 planned edit, got %d", report.PlannedEdits)
	}
	if report.AppliedEdits != 0 || report.ChangedFiles != 0 {
		t.Fatalf("dry run should not apply edits: %+v", report)
	}

	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(after), "OldName") {
		t.Fatalf("dry run should not mutate file, got:\n%s", string(after))
	}
}

func TestRenameDeclarations_Write(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

type OldType struct{}

func OldName() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	selector, err := query.ParseSelector("*[name=/^Old(Name|Type)$/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	report, err := RenameDeclarations(idx, selector, "Renamed", Options{Write: true})
	if err != nil {
		t.Fatalf("RenameDeclarations returned error: %v", err)
	}
	if report.AppliedEdits != 2 {
		t.Fatalf("expected 2 applied edits, got %d", report.AppliedEdits)
	}
	if report.ChangedFiles != 1 {
		t.Fatalf("expected 1 changed file, got %d", report.ChangedFiles)
	}

	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	text := string(after)
	if !strings.Contains(text, "type Renamed struct{}") {
		t.Fatalf("expected type rename, got:\n%s", text)
	}
	if !strings.Contains(text, "func Renamed() {}") {
		t.Fatalf("expected function rename, got:\n%s", text)
	}
}

func TestRenameDeclarations_WriteCallsites(t *testing.T) {
	tmpDir := t.TempDir()
	defPath := filepath.Join(tmpDir, "a.go")
	usePath := filepath.Join(tmpDir, "b.go")

	defSource := `package sample

func OldName() {}
`
	useSource := `package sample

func Use() {
	OldName()
}
`
	if err := os.WriteFile(defPath, []byte(defSource), 0o644); err != nil {
		t.Fatalf("WriteFile a.go failed: %v", err)
	}
	if err := os.WriteFile(usePath, []byte(useSource), 0o644); err != nil {
		t.Fatalf("WriteFile b.go failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	selector, err := query.ParseSelector("function_definition[name=/^OldName$/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	report, err := RenameDeclarations(idx, selector, "NewName", Options{
		Write:           true,
		UpdateCallsites: true,
	})
	if err != nil {
		t.Fatalf("RenameDeclarations returned error: %v", err)
	}
	if report.AppliedEdits != 2 {
		t.Fatalf("expected declaration + callsite edits, got %d", report.AppliedEdits)
	}
	if report.PlannedUseEdits == 0 {
		t.Fatalf("expected callsite edits to be planned, got %+v", report)
	}

	updatedUse, err := os.ReadFile(usePath)
	if err != nil {
		t.Fatalf("ReadFile b.go failed: %v", err)
	}
	if !strings.Contains(string(updatedUse), "NewName()") {
		t.Fatalf("expected callsite rename, got:\n%s", string(updatedUse))
	}
}

func TestRenameDeclarations_DeclarationsOnly(t *testing.T) {
	tmpDir := t.TempDir()
	defPath := filepath.Join(tmpDir, "a.go")
	usePath := filepath.Join(tmpDir, "b.go")

	defSource := `package sample

func OldName() {}
`
	useSource := `package sample

func Use() {
	OldName()
}
`
	if err := os.WriteFile(defPath, []byte(defSource), 0o644); err != nil {
		t.Fatalf("WriteFile a.go failed: %v", err)
	}
	if err := os.WriteFile(usePath, []byte(useSource), 0o644); err != nil {
		t.Fatalf("WriteFile b.go failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	selector, err := query.ParseSelector("function_definition[name=/^OldName$/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	_, err = RenameDeclarations(idx, selector, "NewName", Options{
		Write:           true,
		UpdateCallsites: false,
	})
	if err != nil {
		t.Fatalf("RenameDeclarations returned error: %v", err)
	}

	updatedUse, err := os.ReadFile(usePath)
	if err != nil {
		t.Fatalf("ReadFile b.go failed: %v", err)
	}
	if strings.Contains(string(updatedUse), "NewName()") {
		t.Fatalf("did not expect callsite rename when callsites disabled, got:\n%s", string(updatedUse))
	}
}

func TestRenameDeclarations_CrossPackageCallsites(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "lib"), 0o755); err != nil {
		t.Fatalf("MkdirAll lib failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "app"), 0o755); err != nil {
		t.Fatalf("MkdirAll app failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module sample\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}

	libSource := `package lib

func OldName() {}
`
	appSource := `package app

import "sample/lib"

func Use() {
	lib.OldName()
}
`
	libPath := filepath.Join(tmpDir, "lib", "lib.go")
	appPath := filepath.Join(tmpDir, "app", "app.go")
	if err := os.WriteFile(libPath, []byte(libSource), 0o644); err != nil {
		t.Fatalf("WriteFile lib.go failed: %v", err)
	}
	if err := os.WriteFile(appPath, []byte(appSource), 0o644); err != nil {
		t.Fatalf("WriteFile app.go failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	selector, err := query.ParseSelector("function_definition[name=/^OldName$/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	report, err := RenameDeclarations(idx, selector, "NewName", Options{
		Write:                 true,
		UpdateCallsites:       true,
		CrossPackageCallsites: true,
	})
	if err != nil {
		t.Fatalf("RenameDeclarations returned error: %v", err)
	}
	if report.PlannedUseEdits == 0 {
		t.Fatalf("expected cross-package callsite edits, got %+v", report)
	}

	updatedApp, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("ReadFile app.go failed: %v", err)
	}
	if !strings.Contains(string(updatedApp), "lib.NewName()") {
		t.Fatalf("expected cross-package callsite rename, got:\n%s", string(updatedApp))
	}
}

func TestRenameDeclarations_InvalidIdentifier(t *testing.T) {
	_, err := RenameDeclarations(nil, query.Selector{}, "not-valid-name!", Options{})
	if err == nil {
		t.Fatal("expected RenameDeclarations to fail")
	}
}

func TestRenameDeclarations_TreeSitterEngine_JavaScript(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.js")
	source := `function OldName() {}

function Use() {
	OldName()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main.js failed: %v", err)
	}

	idx, err := index.NewBuilder().BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	selector, err := query.ParseSelector("function_definition[name=/^OldName$/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	report, err := RenameDeclarations(idx, selector, "NewName", Options{
		Write:           true,
		UpdateCallsites: true,
		Engine:          "treesitter",
	})
	if err != nil {
		t.Fatalf("RenameDeclarations returned error: %v", err)
	}
	if report.Engine != "treesitter" {
		t.Fatalf("expected treesitter engine report, got %q", report.Engine)
	}
	if report.AppliedEdits != 2 {
		t.Fatalf("expected declaration + callsite edits, got %+v", report)
	}

	updated, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile main.js failed: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, "function NewName()") {
		t.Fatalf("expected declaration rename, got:\n%s", text)
	}
	if !strings.Contains(text, "NewName()") {
		t.Fatalf("expected callsite rename, got:\n%s", text)
	}
}
