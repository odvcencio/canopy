package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/canopy/pkg/structdiff"
)

func TestNewRootCmd_HasGroups(t *testing.T) {
	root := newRootCmd()

	groups := []string{"index", "search", "graph", "analyze", "transform", "mcp"}
	for _, name := range groups {
		sub, _, err := root.Find([]string{name})
		if err != nil || sub == root {
			t.Fatalf("missing top-level command %q", name)
		}
	}

	// Verify a sampling of subcommands under groups.
	subcommands := []struct {
		group string
		name  string
	}{
		{"index", "build"},
		{"index", "map"},
		{"index", "stats"},
		{"search", "grep"},
		{"search", "refs"},
		{"graph", "calls"},
		{"graph", "dead"},
		{"analyze", "lint"},
		{"transform", "refactor"},
	}
	for _, sc := range subcommands {
		sub, _, err := root.Find([]string{sc.group, sc.name})
		if err != nil || sub == root {
			t.Fatalf("missing subcommand %q under group %q", sc.name, sc.group)
		}
	}
}

func TestRootCmd_RunUnknownCommand(t *testing.T) {
	cmd := newRootCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"unknown-command"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected unknown command to return error")
	}
}

func TestRootCmd_HelpSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"help", "search", "grep"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "Structural grep over source code using two complementary engines.") {
		t.Fatalf("expected command description in help output, got %q", text)
	}
}

func TestRunMCPRejectsPositionals(t *testing.T) {
	if err := runMCP([]string{"unexpected"}); err == nil {
		t.Fatal("expected runMCP to reject positional arguments")
	}
}

func TestRunMCPParsesAllowWritesFlag(t *testing.T) {
	// Command exits on EOF immediately; this verifies flag parsing + startup path.
	originalStdin := os.Stdin
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdin = readPipe
	defer func() {
		os.Stdin = originalStdin
	}()
	_ = writePipe.Close()

	if err := runMCP([]string{"--allow-writes"}); err != nil {
		t.Fatalf("runMCP returned error with --allow-writes: %v", err)
	}
}

func TestResolveDiffSources_Positional(t *testing.T) {
	before, after, err := resolveDiffSources([]string{"old", "new"}, "", "")
	if err != nil {
		t.Fatalf("resolveDiffSources returned error: %v", err)
	}
	if before != "old" || after != "new" {
		t.Fatalf("unexpected targets before=%q after=%q", before, after)
	}
}

func TestResolveDiffSources_CacheAndPositionalMix(t *testing.T) {
	before, after, err := resolveDiffSources([]string{"new"}, "before.json", "")
	if err != nil {
		t.Fatalf("resolveDiffSources returned error: %v", err)
	}
	if before != "" || after != "new" {
		t.Fatalf("unexpected targets before=%q after=%q", before, after)
	}
}

func TestResolveDiffSources_MissingInputs(t *testing.T) {
	if _, _, err := resolveDiffSources([]string{}, "", ""); err == nil {
		t.Fatal("expected error for missing before/after inputs")
	}
	if _, _, err := resolveDiffSources([]string{}, "before.json", ""); err == nil {
		t.Fatal("expected error when after source is missing")
	}
}

func TestShouldSkipWatchDir(t *testing.T) {
	root := filepath.Clean("/tmp/repo")
	cases := []struct {
		path string
		name string
		want bool
	}{
		{path: root, name: "repo", want: false},
		{path: filepath.Join(root, ".git"), name: ".git", want: true},
		{path: filepath.Join(root, "vendor"), name: "vendor", want: true},
		{path: filepath.Join(root, ".hidden"), name: ".hidden", want: true},
		{path: filepath.Join(root, "src"), name: "src", want: false},
	}

	for _, tc := range cases {
		got := shouldSkipWatchDir(root, tc.path, tc.name, nil)
		if got != tc.want {
			t.Fatalf("shouldSkipWatchDir(%q,%q)=%v want=%v", tc.path, tc.name, got, tc.want)
		}
	}
}

func TestShouldIgnoreWatchPath(t *testing.T) {
	ignored := map[string]bool{
		filepath.Clean("/tmp/repo/.canopy/index.json"): true,
	}

	if !shouldIgnoreWatchPath(filepath.Clean("/tmp/repo/.canopy/index.json"), ignored, "/tmp/repo", nil) {
		t.Fatal("expected explicit ignored path to be ignored")
	}
	if !shouldIgnoreWatchPath(filepath.Clean("/tmp/repo/.#file.go"), ignored, "/tmp/repo", nil) {
		t.Fatal("expected editor lockfile to be ignored")
	}
	if shouldIgnoreWatchPath(filepath.Clean("/tmp/repo/main.go"), ignored, "/tmp/repo", nil) {
		t.Fatal("did not expect regular source file to be ignored")
	}
}

func TestWatchRootsDirectoryAndFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	dirRoots, err := watchRoots(tmpDir)
	if err != nil {
		t.Fatalf("watchRoots(dir) returned error: %v", err)
	}
	if len(dirRoots) != 1 || filepath.Clean(dirRoots[0]) != filepath.Clean(tmpDir) {
		t.Fatalf("unexpected dir roots: %v", dirRoots)
	}

	fileRoots, err := watchRoots(filePath)
	if err != nil {
		t.Fatalf("watchRoots(file) returned error: %v", err)
	}
	if len(fileRoots) != 1 || filepath.Clean(fileRoots[0]) != filepath.Clean(tmpDir) {
		t.Fatalf("unexpected file roots: %v", fileRoots)
	}
}

func TestSummarizeChangesByFile(t *testing.T) {
	report := structdiff.Report{
		AddedSymbols: []structdiff.SymbolRef{
			{File: "a.go"},
			{File: "a.go"},
			{File: "b.go"},
		},
		RemovedSymbols: []structdiff.SymbolRef{
			{File: "a.go"},
		},
		ModifiedSymbols: []structdiff.ModifiedSymbol{
			{After: structdiff.SymbolRef{File: "b.go"}},
		},
		ImportChanges: []structdiff.FileImportChange{
			{File: "a.go", Added: []string{"fmt"}, Removed: []string{"os", "io"}},
		},
	}

	summaries := summarizeChangesByFile(report)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 file summaries, got %d", len(summaries))
	}
	if summaries[0].File != "a.go" || summaries[0].Added != 2 || summaries[0].Removed != 1 || summaries[0].ImportAdded != 1 || summaries[0].ImportRemoved != 2 {
		t.Fatalf("unexpected summary for a.go: %+v", summaries[0])
	}
	if summaries[1].File != "b.go" || summaries[1].Added != 1 || summaries[1].Modified != 1 {
		t.Fatalf("unexpected summary for b.go: %+v", summaries[1])
	}
}

func TestRunIndexOnceIfChanged(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, ".canopy", "index.json")
	sourcePath := filepath.Join(tmpDir, "main.go")

	writeSource := func(body string) {
		t.Helper()
		if err := os.WriteFile(sourcePath, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
	}

	writeSource("package sample\n\nfunc A() {}\n")

	// First run: no baseline cache means changed.
	err := runIndex([]string{tmpDir, "--out", outPath, "--once-if-changed"})
	if err == nil {
		t.Fatal("expected once-if-changed to return change exit on first run")
	}
	assertExitCode(t, err, 2)

	// Second run: unchanged input should return success.
	if err := runIndex([]string{tmpDir, "--out", outPath, "--once-if-changed"}); err != nil {
		t.Fatalf("expected unchanged second run to succeed, got %v", err)
	}

	// Third run: modify source so changes are detected again.
	time.Sleep(2 * time.Millisecond)
	writeSource("package sample\n\nfunc A() {}\nfunc B() {}\n")
	err = runIndex([]string{tmpDir, "--out", outPath, "--once-if-changed"})
	if err == nil {
		t.Fatal("expected changed third run to return change exit")
	}
	assertExitCode(t, err, 2)
}

func TestRunLint_MaxLinesViolation(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func short() {
	println("ok")
}

func long() {
	println("1")
	println("2")
	println("3")
	println("4")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err := runLint([]string{
		tmpDir,
		"--rule", "no function longer than 5 lines",
	})
	if err == nil {
		t.Fatal("expected lint rule to fail with violation")
	}
	assertExitCode(t, err, 3)
}

func TestRunLint_NoImportViolation(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

import "fmt"

func A() {
	fmt.Println("ok")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err := runLint([]string{
		tmpDir,
		"--rule", "no import fmt",
	})
	if err == nil {
		t.Fatal("expected lint import rule to fail with violation")
	}
	assertExitCode(t, err, 3)
}

func TestRunLint_QueryPatternViolation(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	patternPath := filepath.Join(tmpDir, "no-empty.scm")
	source := `package sample

func Empty() {}
`
	pattern := `(function_declaration (block) @violation)`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile source failed: %v", err)
	}
	if err := os.WriteFile(patternPath, []byte(pattern), 0o644); err != nil {
		t.Fatalf("WriteFile pattern failed: %v", err)
	}

	err := runLint([]string{
		tmpDir,
		"--pattern", patternPath,
	})
	if err == nil {
		t.Fatal("expected lint pattern to fail with violation")
	}
	assertExitCode(t, err, 3)
}

func TestRunStats(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

type Service struct{}

func Work() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runStats([]string{
		tmpDir,
		"--top", "5",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runStats returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"stats: files=1 symbols=2", "languages:", "kinds:", "top files"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunFiles(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

type Service struct{}

func Work() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runFiles([]string{
		tmpDir,
		"--language", "go",
		"--min-symbols", "1",
		"--sort", "symbols",
		"--top", "5",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runFiles returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"files: total=1 shown=1", "main.go language=go symbols=2"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunDeps(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "internal", "x"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	mainSource := `package main

import (
	"fmt"
	"sample/internal/x"
)

func main() {
	_ = fmt.Sprintf("%v", x.Value)
}
`
	xSource := `package x

const Value = 1
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module sample\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "internal", "x", "x.go"), []byte(xSource), 0o644); err != nil {
		t.Fatalf("WriteFile x.go failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runDeps([]string{
		tmpDir,
		"--by", "package",
		"--top", "5",
		"--focus", ".",
		"--depth", "2",
		"--reverse",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runDeps returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"deps: mode=package", "top outgoing", "top incoming", "focus: . direction=reverse depth=2"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunBridge(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "internal", "x"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	mainSource := `package main

import (
	"fmt"
	"sample/internal/x"
)

func main() {
	_ = fmt.Sprintf("%v", x.Value)
}
`
	xSource := `package x

const Value = 1
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module sample\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "internal", "x", "x.go"), []byte(xSource), 0o644); err != nil {
		t.Fatalf("WriteFile x.go failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runBridge([]string{
		tmpDir,
		"--top", "5",
		"--focus", "internal/x",
		"--depth", "2",
		"--reverse",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runBridge returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"bridge:", "components:", "top bridges", "focus: internal/x direction=reverse depth=2", "external pressure"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunGrepCount(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func A() {}
func B() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runGrep([]string{
		"function_definition[name=/./]",
		tmpDir,
		"--count",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runGrep returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "2" {
		t.Fatalf("unexpected count output %q", output.String())
	}
}

func TestRunRefsCount(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func A() {}

func Use() {
	A()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runRefs([]string{
		"A",
		tmpDir,
		"--count",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runRefs returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "1" {
		t.Fatalf("unexpected refs count output %q", output.String())
	}
}

func TestRunCallgraphCount(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func A() {}

func main() {
	A()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runCallgraph([]string{
		"main",
		tmpDir,
		"--depth",
		"2",
		"--count",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runCallgraph returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "1" {
		t.Fatalf("unexpected callgraph count output %q", output.String())
	}
}

func TestRunCallgraphFileQualifiedRoot(t *testing.T) {
	tmpDir := t.TempDir()
	files := map[string]string{
		"a.go": `package sample

func One() {}

func Build() {
	One()
}
`,
		"b.go": `package sample

func Two() {}

func Build() {
	Two()
}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", name, err)
		}
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runCallgraph([]string{
		"a.go:Build",
		tmpDir,
		"--depth",
		"1",
		"--count",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runCallgraph returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "1" {
		t.Fatalf("unexpected callgraph count output %q", output.String())
	}
}

func TestRunDeadCount(t *testing.T) {
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

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runDead([]string{
		tmpDir,
		"--kind",
		"function",
		"--count",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runDead returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "1" {
		t.Fatalf("unexpected dead count output %q", output.String())
	}
}

func TestRunQueryCount(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func A() {}
func B() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runQuery([]string{
		"(function_declaration (identifier) @name)",
		tmpDir,
		"--count",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runQuery returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "2" {
		t.Fatalf("unexpected query count output %q", output.String())
	}
}

func TestRunScope(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

import "fmt"

func work(input string) {
	value := input
	fmt.Println(value)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runScope([]string{
		sourcePath,
		"--root", tmpDir,
		"--line", "7",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runScope returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"package: sample", "input (param)", "value (local_var)", "fmt (import)"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunContextSemantic(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func helper() {}

func work() {
	helper()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runContext([]string{
		sourcePath,
		"--root", tmpDir,
		"--line", "6",
		"--tokens", "400",
		"--semantic",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runContext returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"semantic: true", "focus: function_definition func work()", "related:", "helper"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunContextSemanticDepth(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func leaf() {}

func mid() {
	leaf()
}

func work() {
	mid()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runContext([]string{
		sourcePath,
		"--root", tmpDir,
		"--line", "10",
		"--tokens", "400",
		"--semantic",
		"--semantic-depth", "2",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runContext returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"semantic: true", "semantic-depth: 2", "mid", "leaf"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunChunk(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func A() {}
func B() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := runChunk([]string{
		tmpDir,
		"--tokens", "200",
	})
	_ = writePipe.Close()
	if runErr != nil {
		t.Fatalf("runChunk returned error: %v", runErr)
	}

	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"chunks:", "function_definition", "func A()"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestRunRefactorDryRunAndWrite(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func OldName() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Dry-run should not mutate.
	if err := runRefactor([]string{
		"function_definition[name=/^OldName$/]",
		"NewName",
		tmpDir,
	}); err != nil {
		t.Fatalf("runRefactor dry-run returned error: %v", err)
	}

	afterDryRun, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile after dry-run failed: %v", err)
	}
	if !strings.Contains(string(afterDryRun), "OldName") {
		t.Fatalf("expected dry-run to preserve original name, got:\n%s", string(afterDryRun))
	}

	// Write mode should apply rename.
	if err := runRefactor([]string{
		"function_definition[name=/^OldName$/]",
		"NewName",
		tmpDir,
		"--write",
	}); err != nil {
		t.Fatalf("runRefactor write returned error: %v", err)
	}

	afterWrite, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile after write failed: %v", err)
	}
	if !strings.Contains(string(afterWrite), "NewName") {
		t.Fatalf("expected write to apply rename, got:\n%s", string(afterWrite))
	}
}

func TestRunRefactorCallsites(t *testing.T) {
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

	if err := runRefactor([]string{
		"function_definition[name=/^OldName$/]",
		"NewName",
		tmpDir,
		"--callsites",
		"--write",
	}); err != nil {
		t.Fatalf("runRefactor callsites write returned error: %v", err)
	}

	afterUse, err := os.ReadFile(usePath)
	if err != nil {
		t.Fatalf("ReadFile b.go failed: %v", err)
	}
	if !strings.Contains(string(afterUse), "NewName()") {
		t.Fatalf("expected callsite to be renamed, got:\n%s", string(afterUse))
	}
}

func TestRunRefactorCrossPackageCallsites(t *testing.T) {
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

	if err := runRefactor([]string{
		"function_definition[name=/^OldName$/]",
		"NewName",
		tmpDir,
		"--callsites",
		"--cross-package",
		"--write",
	}); err != nil {
		t.Fatalf("runRefactor cross-package write returned error: %v", err)
	}

	afterApp, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("ReadFile app.go failed: %v", err)
	}
	if !strings.Contains(string(afterApp), "lib.NewName()") {
		t.Fatalf("expected cross-package callsite rename, got:\n%s", string(afterApp))
	}
}

func TestRunRefactorTreeSitterEngine(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.js")
	source := `function OldName() {}

function Use() {
	OldName()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := runRefactor([]string{
		"function_definition[name=/^OldName$/]",
		"NewName",
		tmpDir,
		"--engine",
		"treesitter",
		"--callsites",
		"--write",
	}); err != nil {
		t.Fatalf("runRefactor treesitter write returned error: %v", err)
	}

	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile main.js failed: %v", err)
	}
	if !strings.Contains(string(after), "function NewName()") || !strings.Contains(string(after), "NewName()") {
		t.Fatalf("expected treesitter refactor rename, got:\n%s", string(after))
	}
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	withCode, ok := err.(interface{ ExitCode() int })
	if !ok {
		t.Fatalf("expected error with exit code, got %T (%v)", err, err)
	}
	if got := withCode.ExitCode(); got != want {
		t.Fatalf("unexpected exit code: got=%d want=%d err=%v", got, want, err)
	}
}
