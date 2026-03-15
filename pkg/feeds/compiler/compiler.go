// Package compiler implements a feed that runs language-specific compilers
// and harvests diagnostics into the scope graph.
package compiler

import (
	"bufio"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/odvcencio/gts-suite/pkg/feeds"
	"github.com/odvcencio/gts-suite/pkg/scope"
)

// Diagnostic represents a compiler diagnostic mapped to source.
type Diagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"` // "error", "warning", "info"
	Message  string `json:"message"`
	Source   string `json:"source"` // compiler name
}

// CompilerSpec defines how to invoke and parse a compiler.
type CompilerSpec struct {
	Command   string
	Args      []string
	Languages []string
	Parser    func(output []byte, file string) []Diagnostic
}

// Feed implements FeedProvider for compiler diagnostics.
type Feed struct {
	specs  map[string]*CompilerSpec // lang → spec
	logger *slog.Logger
}

// Detect checks which compilers are available and returns a Feed.
// Returns nil if no compilers are found.
func Detect(logger *slog.Logger) *Feed {
	specs := make(map[string]*CompilerSpec)

	if _, err := exec.LookPath("go"); err == nil {
		specs["go"] = &CompilerSpec{
			Command:   "go",
			Args:      []string{"vet", "./..."},
			Languages: []string{"go"},
			Parser:    parseGoVet,
		}
	}

	if _, err := exec.LookPath("mypy"); err == nil {
		specs["python"] = &CompilerSpec{
			Command:   "mypy",
			Args:      []string{"--no-color-output", "--show-error-codes"},
			Languages: []string{"python"},
			Parser:    parseColonDiag,
		}
	}

	if _, err := exec.LookPath("tsc"); err == nil {
		specs["typescript"] = &CompilerSpec{
			Command:   "tsc",
			Args:      []string{"--noEmit", "--pretty", "false"},
			Languages: []string{"typescript"},
			Parser:    parseTSC,
		}
	}

	if len(specs) == 0 {
		return nil
	}
	return &Feed{specs: specs, logger: logger}
}

func (f *Feed) Name() string { return "compiler" }
func (f *Feed) Supports(lang string) bool {
	_, ok := f.specs[lang]
	return ok
}
func (f *Feed) Priority() int { return 60 }

func (f *Feed) Feed(graph *scope.Graph, file string, src []byte, ctx *feeds.FeedContext) error {
	fs := graph.FileScope(file)
	if fs == nil || len(fs.Defs) == 0 {
		return nil
	}

	// Determine language from file extension
	lang := langFromFile(file)
	spec, ok := f.specs[lang]
	if !ok {
		return nil
	}

	// Run compiler
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = ctx.WorkspaceRoot
	output, _ := cmd.CombinedOutput() // compilers return non-zero on errors, that's expected

	diags := spec.Parser(output, file)
	if len(diags) == 0 {
		return nil
	}

	// Map diagnostics to definitions by line range
	for i := range fs.Defs {
		def := &fs.Defs[i]
		var defDiags []Diagnostic
		for _, d := range diags {
			if d.Line >= def.Loc.StartLine && d.Line <= def.Loc.EndLine {
				defDiags = append(defDiags, d)
			}
		}
		if len(defDiags) > 0 {
			scope.SetMeta(def, "compiler.diagnostics", defDiags)
		}
	}
	return nil
}

func langFromFile(file string) string {
	if strings.HasSuffix(file, ".go") {
		return "go"
	}
	if strings.HasSuffix(file, ".py") || strings.HasSuffix(file, ".pyi") {
		return "python"
	}
	if strings.HasSuffix(file, ".ts") || strings.HasSuffix(file, ".tsx") {
		return "typescript"
	}
	if strings.HasSuffix(file, ".rs") {
		return "rust"
	}
	if strings.HasSuffix(file, ".c") || strings.HasSuffix(file, ".h") {
		return "c"
	}
	return ""
}

// parseGoVet parses `go vet` output: "file.go:line:col: message"
func parseGoVet(output []byte, file string) []Diagnostic {
	return parseColonFormat(output, file, "go vet")
}

// parseColonDiag parses colon-separated diagnostic output (mypy, etc.)
func parseColonDiag(output []byte, file string) []Diagnostic {
	return parseColonFormat(output, file, "mypy")
}

// parseTSC parses TypeScript compiler output: "file(line,col): error TS1234: message"
var tscPattern = regexp.MustCompile(`^(.+)\((\d+),(\d+)\):\s*(error|warning)\s+\w+:\s*(.+)$`)

func parseTSC(output []byte, file string) []Diagnostic {
	var diags []Diagnostic
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		m := tscPattern.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		diagFile := m[1]
		if !strings.HasSuffix(diagFile, file) && diagFile != file {
			continue
		}
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		diags = append(diags, Diagnostic{
			File:     file,
			Line:     line,
			Column:   col,
			Severity: m[4],
			Message:  m[5],
			Source:   "tsc",
		})
	}
	return diags
}

// parseColonFormat parses "file:line:col: message" format.
var colonPattern = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.+)$`)

func parseColonFormat(output []byte, file string, source string) []Diagnostic {
	var diags []Diagnostic
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		m := colonPattern.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		diagFile := m[1]
		if !strings.HasSuffix(diagFile, file) && diagFile != file {
			continue
		}
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		severity := "error"
		msg := m[4]
		if strings.HasPrefix(msg, "warning:") {
			severity = "warning"
			msg = strings.TrimPrefix(msg, "warning:")
			msg = strings.TrimSpace(msg)
		}
		diags = append(diags, Diagnostic{
			File:     file,
			Line:     line,
			Column:   col,
			Severity: severity,
			Message:  msg,
			Source:   source,
		})
	}
	return diags
}
