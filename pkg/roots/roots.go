// Package roots provides language-aware reachability-root detection for dead code analysis.
// It determines whether a callable definition is "reachable without a static caller"
// (e.g. framework entrypoints, test functions, annotated handlers) so that dead-code
// tools can suppress false positives on Java/Python/TypeScript framework applications.
package roots

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"m31labs.dev/canopy/pkg/xref"
)

// Confidence expresses how reliable a dead-code finding is given the graph's
// call-resolution rate.
type Confidence string

const (
	// ConfidenceHigh means the resolution rate is high (>= 0.75) — the finding
	// is reliable; missing callers are unlikely.
	ConfidenceHigh Confidence = "high"

	// ConfidenceMedium means the resolution rate is moderate (>= 0.5) — the
	// finding is plausible but some callers may be unresolved.
	ConfidenceMedium Confidence = "medium"

	// ConfidenceLow means the resolution rate is low (< 0.5) — many calls are
	// unresolved and false positives are likely.
	ConfidenceLow Confidence = "low"
)

// ResolutionRate reports the fraction of call references that resolved to a
// definition (resolvedRefs / (resolvedRefs + unresolvedRefs)), counting
// individual references — not distinct edges — on both sides so the ratio is
// apples-to-apples. Returns 1.0 when there are no call references. Used to scale
// dead-finding confidence: a partially-indexed program (external deps absent)
// resolves fewer calls, so its dead findings are less certain.
func ResolutionRate(g xref.Graph) float64 {
	resolved := 0
	for _, e := range g.Edges {
		resolved += e.Count
	}
	total := resolved + len(g.Unresolved)
	if total == 0 {
		return 1.0
	}
	return float64(resolved) / float64(total)
}

// DeadConfidence rates how reliable a dead-code finding is.
//
// The dominant signal is symbol visibility: a private/unexported callable can
// only be called from within the repo, so once it is in the index a zero
// incoming count is strong evidence it is dead. An exported/public callable may
// be invoked from outside the indexed scope, so it is inherently less certain.
//
// The call-resolution rate is a floor: when a large fraction of calls did not
// resolve to a definition (typical when external dependencies are not indexed),
// even in-repo callers may have been missed, so confidence is downgraded a step.
func DeadConfidence(resolutionRate float64, def xref.Definition) Confidence {
	level := ConfidenceHigh
	if looksExported(def) {
		level = ConfidenceMedium
	}
	if resolutionRate < 0.5 {
		switch level {
		case ConfidenceHigh:
			level = ConfidenceMedium
		default:
			level = ConfidenceLow
		}
	}
	return level
}

// Options controls optional Analyzer behaviour.
type Options struct {
	// TreatExportedAsRoots marks any public/exported callable (not in a test
	// file) as a reachability root with reason "exported". Default: false.
	TreatExportedAsRoots bool

	// ExtraRootAnnotations lists additional annotation/decorator names (without
	// sigil) to treat as root indicators across all profiles.
	ExtraRootAnnotations []string
}

// Analyzer detects reachability roots for a given project root directory.
type Analyzer struct {
	root      string
	reg       *profileRegistry
	opts      Options
	fileCache map[string][]string // relative path -> source lines (1-indexed at [1])
}

// New returns an Analyzer rooted at dir using default options.
func New(root string) *Analyzer {
	return NewWithOptions(root, Options{})
}

// NewWithOptions returns an Analyzer with custom options.
func NewWithOptions(root string, opts Options) *Analyzer {
	return &Analyzer{
		root:      root,
		reg:       newRegistry(opts.ExtraRootAnnotations),
		opts:      opts,
		fileCache: map[string][]string{},
	}
}

// IsRoot reports whether def is a reachability root (invokable without a static
// caller in the analyzed graph) and a short reason explaining why.
//
// defs must be the full slice of definitions from the same graph so that
// enclosing class/interface lookups work correctly.
func (a *Analyzer) IsRoot(def xref.Definition, defs []xref.Definition) (bool, string) {
	p := a.reg.lookupByFile(def.File)

	// --- Entrypoint rule ---
	if p != nil {
		if ok, reason := a.checkEntrypoint(def, p); ok {
			return true, reason
		}
	} else {
		// Unknown language: fall back to Go heuristics for safety.
		if def.Kind == "function_definition" &&
			(def.Name == "main" || def.Name == "init") {
			return true, "entrypoint"
		}
	}

	// --- Test file ---
	testFile := a.isTestFile(def.File, p)

	// --- Test annotation / test name ---
	if p != nil {
		if ok, reason := a.checkTest(def, p, testFile); ok {
			return true, reason
		}
	} else if testFile {
		return true, "test"
	}

	if testFile {
		// Already captured above, but guard against p==nil path falling through.
		return true, "test"
	}

	// --- Method root annotation (own annotation) ---
	if p != nil && len(p.MethodRootAnnotations) > 0 {
		if annName, ok := a.annotationInSet(def.Signature, p.Syntax, p.MethodRootAnnotations); ok {
			return true, "annotation:@" + annName
		}
		// Source-peek: canopy folds only the FIRST leading annotation into the
		// Java symbol Signature and folds nothing for Python/TS/C#, so read the
		// real annotation/decorator block from source (lines at and above the
		// declaration). Dotted decorators (@app.route) also yield their head
		// token, so "app"/"router" entries match.
		peeked := a.annotationNamesAbove(def.File, def.StartLine, p.Syntax)
		if annName, ok := setIntersects(peeked, p.MethodRootAnnotations); ok {
			return true, "annotation:@" + annName
		}
	}

	// --- Framework interface (checked BEFORE class-annotation so that
	//     interface methods get the more specific reason) ---
	if p != nil && len(p.FrameworkInterfaces) > 0 &&
		def.Kind == "method_definition" {
		if enclosing := findEnclosing(defs, def); enclosing != nil &&
			enclosing.Kind == "interface_definition" {
			if ifName, ok := a.checkFrameworkInterface(enclosing, p); ok {
				return true, "framework-interface:" + ifName
			}
		}
	}

	// --- Class-level annotation (enclosing class/interface) ---
	if p != nil && len(p.ClassRootAnnotations) > 0 {
		if enclosing := findEnclosing(defs, def); enclosing != nil {
			if annName, ok := a.annotationInSet(enclosing.Signature, p.Syntax, p.ClassRootAnnotations); ok {
				return true, "class-annotation:@" + annName
			}
			peeked := a.annotationNamesAbove(enclosing.File, enclosing.StartLine, p.Syntax)
			if annName, ok := setIntersects(peeked, p.ClassRootAnnotations); ok {
				return true, "class-annotation:@" + annName
			}
		}
	}

	// --- Exported roots (opt-in) ---
	if a.opts.TreatExportedAsRoots && !testFile {
		if looksExported(def) {
			return true, "exported"
		}
	}

	return false, ""
}

// checkEntrypoint tests the profile's entrypoint rule against def.
func (a *Analyzer) checkEntrypoint(def xref.Definition, p *Profile) (bool, string) {
	ep := p.Entrypoint
	if len(ep.Names) == 0 {
		return false, ""
	}
	if !slices.Contains(ep.Names, def.Name) {
		return false, ""
	}
	if len(ep.Kinds) > 0 && !slices.Contains(ep.Kinds, def.Kind) {
		return false, ""
	}
	if ep.RequireStatic && !strings.Contains(def.Signature, "static") {
		return false, ""
	}
	return true, "entrypoint"
}

// checkTest returns (true, "test") if def is a test function/method.
func (a *Analyzer) checkTest(def xref.Definition, p *Profile, testFile bool) (bool, string) {
	// Test file path.
	if testFile {
		return true, "test"
	}
	// Test annotation.
	if len(p.TestAnnotations) > 0 {
		if _, ok := a.annotationInSet(def.Signature, p.Syntax, p.TestAnnotations); ok {
			return true, "test"
		}
	}
	// Test function name pattern.
	if p.TestFuncPattern != nil && p.TestFuncPattern.MatchString(def.Name) {
		return true, "test"
	}
	return false, ""
}

// isTestFile returns true if the file path matches the profile's test file rules.
func (a *Analyzer) isTestFile(filePath string, p *Profile) bool {
	if p == nil {
		// Default: Go _test.go heuristic.
		lower := strings.ToLower(strings.TrimSpace(filePath))
		return strings.HasSuffix(lower, "_test.go")
	}
	lower := strings.ToLower(filepath.ToSlash(filePath))
	for _, suffix := range p.TestFile.FileSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) {
			return true
		}
	}
	if len(p.TestFile.BasePrefixes) > 0 {
		base := filepath.Base(lower)
		for _, prefix := range p.TestFile.BasePrefixes {
			if strings.HasPrefix(base, strings.ToLower(prefix)) {
				return true
			}
		}
	}
	for _, seg := range p.TestFile.PathContains {
		if strings.Contains(lower, strings.ToLower(seg)) {
			return true
		}
	}
	return false
}

// annotationInSet checks whether the Signature starts with the language sigil
// followed by an annotation name that is in the given set.
// Returns (name, true) on match.
func (a *Analyzer) annotationInSet(signature string, syntax annotationSyntax, set map[string]bool) (string, bool) {
	sig := strings.TrimSpace(signature)
	sigil := string(syntax)
	if !strings.HasPrefix(sig, sigil) {
		return "", false
	}
	// Strip sigil, read the identifier up to the first non-ident rune.
	rest := sig[len(sigil):]
	name := identToken(rest)
	if name == "" {
		return "", false
	}
	if set[name] {
		return name, true
	}
	// Dotted decorator (@app.route): match on the head token (app).
	if dot := strings.IndexByte(name, '.'); dot > 0 && set[name[:dot]] {
		return name[:dot], true
	}
	return "", false
}

// annotationNamesAbove reads the real annotation/decorator block attached to a
// declaration at startLine (1-indexed) directly from source. It scans the
// contiguous run of annotation/comment/blank lines immediately above startLine,
// and — when startLine itself is the first annotation line (as canopy reports
// for Java) — downward across further annotation lines to the declaration.
//
// Names are returned without the sigil; a dotted decorator (@app.route) also
// yields its head token ("app"), so prefix-style profile entries match. Returns
// nil when the source is unavailable (callers fall back to the Signature check).
func (a *Analyzer) annotationNamesAbove(file string, startLine int, syntax annotationSyntax) map[string]bool {
	lines := a.loadLines(file)
	if len(lines) == 0 || startLine < 1 {
		return nil
	}
	sigil := byte(syntax[0]) // '@' or '['
	out := map[string]bool{}

	collect := func(line string) {
		t := strings.TrimSpace(line)
		if t == "" || t[0] != sigil {
			return
		}
		name := identToken(t[1:])
		if name == "" {
			return
		}
		out[name] = true
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			out[name[:dot]] = true
		}
	}

	isBlockLine := func(line string) bool {
		t := strings.TrimSpace(line)
		if t == "" || t[0] == sigil {
			return true
		}
		return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "#")
	}

	// Upward from the line above the declaration; stop at the first code line.
	for i := startLine - 1; i >= 1 && i < len(lines); i-- {
		if !isBlockLine(lines[i]) {
			break
		}
		collect(lines[i])
	}

	// Java: startLine often points at the first annotation; walk down across
	// further annotations until the declaration keyword line.
	if startLine < len(lines) {
		if t := strings.TrimSpace(lines[startLine]); t != "" && t[0] == sigil {
			for i := startLine; i < len(lines); i++ {
				tt := strings.TrimSpace(lines[i])
				if tt == "" || tt[0] == sigil || strings.HasPrefix(tt, "//") || strings.HasPrefix(tt, "*") {
					collect(lines[i])
					continue
				}
				break
			}
		}
	}

	return out
}

// setIntersects returns the first key of have that is present in want.
func setIntersects(have, want map[string]bool) (string, bool) {
	for k := range have {
		if want[k] {
			return k, true
		}
	}
	return "", false
}

// checkFrameworkInterface peeks the source line of the interface definition to
// detect "extends"/"implements" clauses naming a framework interface.
func (a *Analyzer) checkFrameworkInterface(enclosing *xref.Definition, p *Profile) (string, bool) {
	lines := a.loadLines(enclosing.File)
	if len(lines) == 0 {
		return "", false
	}
	// The interface_definition StartLine points at the declaration.
	// Lines are 1-indexed; stored in lines[1..].
	startLine := enclosing.StartLine
	if startLine < 1 || startLine >= len(lines) {
		return "", false
	}
	// Peek up to 3 lines starting at StartLine to handle multi-line declarations.
	for offset := 0; offset < 3 && startLine+offset < len(lines); offset++ {
		line := lines[startLine+offset]
		if containsExtendsImplements(line) {
			for ifName := range p.FrameworkInterfaces {
				if strings.Contains(line, ifName) {
					return ifName, true
				}
			}
		}
	}
	return "", false
}

// containsExtendsImplements returns true if the line has an extends or implements clause.
func containsExtendsImplements(line string) bool {
	return strings.Contains(line, "extends") || strings.Contains(line, "implements")
}

// findEnclosing returns the narrowest class_definition or interface_definition
// in defs (same file) whose [StartLine, EndLine] span contains def.
func findEnclosing(defs []xref.Definition, def xref.Definition) *xref.Definition {
	var best *xref.Definition
	bestSpan := -1
	for i := range defs {
		d := &defs[i]
		if d.File != def.File {
			continue
		}
		if d.Kind != "class_definition" && d.Kind != "interface_definition" {
			continue
		}
		if d.StartLine > def.StartLine || d.EndLine < def.EndLine {
			continue
		}
		span := d.EndLine - d.StartLine
		if best == nil || span < bestSpan {
			best = d
			bestSpan = span
		}
	}
	return best
}

// loadLines reads the source file (absolute path = root + file) and caches
// its lines. The returned slice is 1-indexed: lines[1] is line 1.
// Returns nil on error.
func (a *Analyzer) loadLines(relPath string) []string {
	if cached, ok := a.fileCache[relPath]; ok {
		return cached
	}
	absPath := filepath.Join(a.root, filepath.FromSlash(relPath))
	f, err := os.Open(absPath)
	if err != nil {
		a.fileCache[relPath] = nil
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// lines[0] unused; lines[1] = first line.
	lines := []string{""}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	a.fileCache[relPath] = lines
	return lines
}

// identToken reads the Go/Java/TS/Python identifier that starts at the beginning
// of s (stops at first non-identifier rune: '(', ' ', '\t', '\r', '\n', '[', ']', '.').
func identToken(s string) string {
	for i, r := range s {
		if r == '(' || r == ' ' || r == '\t' || r == '\r' || r == '\n' ||
			r == '[' || r == ']' {
			return s[:i]
		}
	}
	return s
}

// looksExported reports whether def appears to be a public/exported callable —
// one that could be invoked from outside the indexed scope.
// Go: uppercase first letter. Java/C#: "public" keyword in signature.
// Python/TS/JS: any name not prefixed with "_".
func looksExported(def xref.Definition) bool {
	if def.Name == "" {
		return false
	}
	switch {
	case hasGoExt(def.File):
		return def.Name[0] >= 'A' && def.Name[0] <= 'Z'
	case hasJavaExt(def.File), hasCsExt(def.File):
		return strings.Contains(def.Signature, "public")
	default:
		// Python, TS, JS: assume exported unless name starts with _.
		return !strings.HasPrefix(def.Name, "_")
	}
}

func hasGoExt(f string) bool   { return strings.HasSuffix(strings.ToLower(f), ".go") }
func hasJavaExt(f string) bool { return strings.HasSuffix(strings.ToLower(f), ".java") }
func hasCsExt(f string) bool   { return strings.HasSuffix(strings.ToLower(f), ".cs") }
