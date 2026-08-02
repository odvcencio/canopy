package contextbundle

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

// candidateFlags records which of the spec 10.6/10.8 selection signals apply
// to a candidate. scoring.go turns these into an integer score and a
// SelectionReason trail.
type candidateFlags struct {
	ExplicitRequired    bool
	ExplicitSelector    bool
	Changed             bool
	Focus               bool
	FailureNamed        bool
	DirectCallerCallee  bool
	MappedTest          bool
	PublicAPI           bool
	TaskTermMatch       float64 // 0..1 normalized match strength
	TransitiveDepth2    bool
	FanInHighPercentile bool
	Generated           bool
}

// candidateItem is one unit of evidence under consideration for a bundle.
type candidateItem struct {
	ID        string
	EntityID  string
	Path      string
	Language  string
	Kind      string
	Name      string
	StartLine int
	EndLine   int
	Signature string
	Section   Section
	Symbol    *model.Symbol
	Required  bool
	Flags     candidateFlags

	// Populated by scoring.go.
	Score   int
	Reasons []SelectionReason
}

// sectionPriority orders candidate sections when a single candidate is
// eligible for more than one section (spec 9.3 lists an ordering for
// rendering; this package additionally needs one representative section per
// candidate so a PlanItem has an unambiguous home).
var sectionPriority = []Section{
	SectionFocus,
	SectionChanges,
	SectionCallers,
	SectionCallees,
	SectionTypes,
	SectionTests,
	SectionFailures,
	SectionRepoMap,
}

func generateCandidates(ctx context.Context, idx *model.Index, graph *xref.Graph, tests TestMapper, req Request) []*candidateItem {
	files := indexFileLookup(idx)
	byKey := make(map[string]*candidateItem)
	order := make([]string, 0, 64)

	upsert := func(c *candidateItem) *candidateItem {
		key := c.ID
		if existing, ok := byKey[key]; ok {
			mergeFlags(&existing.Flags, c.Flags)
			if !existing.Required && c.Required {
				existing.Required = true
			}
			return existing
		}
		byKey[key] = c
		order = append(order, key)
		return c
	}

	includeGenerated := req.IncludeGenerated
	explicitPaths := explicitlySelectedPaths(req.Intent.Focus)
	allowGenerated := func(path string) bool {
		return includeGenerated || explicitPaths[normalizeRelPath(path)]
	}

	// 1 & 4: explicit selectors and focus-line entities (spec 10.6 items 1, 3).
	for _, sel := range req.Intent.Focus {
		resolveSelector(idx, graph, files, sel, allowGenerated, upsert)
	}

	// 2: changed/staged entities (spec 10.6 item 2).
	changedIDs := make([]string, 0, len(req.Intent.ChangedPaths))
	for _, path := range req.Intent.ChangedPaths {
		rel := normalizeRelPath(path)
		file, ok := files[rel]
		if !ok || (file.Generated != nil && !allowGenerated(rel)) {
			continue
		}
		for i := range file.Symbols {
			sym := &file.Symbols[i]
			c := candidateFromSymbol(file, sym)
			c.Section = SectionChanges
			c.Flags.Changed = true
			c.Flags.Generated = file.Generated != nil
			c.Flags.PublicAPI = looksPublicAPI(sym.Name, sym.Kind)
			result := upsert(c)
			if result.EntityID != "" {
				changedIDs = append(changedIDs, result.EntityID)
			}
		}
	}

	// Collect the focus/changed root IDs used to seed graph traversal and
	// test mapping.
	rootIDs := make([]string, 0, len(order))
	for _, key := range order {
		c := byKey[key]
		if (c.Flags.Focus || c.Flags.Changed || c.Flags.ExplicitRequired || c.Flags.ExplicitSelector) && c.EntityID != "" {
			rootIDs = append(rootIDs, c.EntityID)
		}
	}
	rootIDs = dedupStrings(rootIDs)

	// 5, 6, 7: direct and transitive callers/callees (spec 10.6 items 4-6).
	if graph != nil && len(rootIDs) > 0 {
		addGraphNeighbors(graph, rootIDs, false, SectionCallees, allowGenerated, upsert)
		addGraphNeighbors(graph, rootIDs, true, SectionCallers, allowGenerated, upsert)
	}

	// 8: related types and interfaces (spec 10.6 item 7) — same-file type
	// definitions alongside every focus/changed entity.
	seenTypeFiles := map[string]bool{}
	for _, key := range order {
		c := byKey[key]
		if !(c.Flags.Focus || c.Flags.Changed || c.Flags.ExplicitRequired || c.Flags.ExplicitSelector) {
			continue
		}
		if seenTypeFiles[c.Path] {
			continue
		}
		seenTypeFiles[c.Path] = true
		file, ok := files[normalizeRelPath(c.Path)]
		if !ok || (file.Generated != nil && !allowGenerated(c.Path)) {
			continue
		}
		for i := range file.Symbols {
			sym := &file.Symbols[i]
			if !looksLikeTypeKind(sym.Kind) {
				continue
			}
			tc := candidateFromSymbol(file, sym)
			tc.Section = SectionTypes
			tc.Flags.PublicAPI = looksPublicAPI(sym.Name, sym.Kind)
			tc.Flags.Generated = file.Generated != nil
			upsert(tc)
		}
	}

	// 9: mapped tests (spec 10.6 item 8).
	if tests != nil && len(rootIDs) > 0 {
		related, err := tests.RelatedTests(ctx, idx, rootIDs)
		if err == nil {
			for i := range related {
				sym := related[i]
				file, ok := files[normalizeRelPath(sym.File)]
				if !ok || (file.Generated != nil && !allowGenerated(sym.File)) {
					continue
				}
				c := candidateFromSymbol(file, &sym)
				c.Section = SectionTests
				c.Flags.MappedTest = true
				upsert(c)
			}
		}
	}

	// 10: files referenced by current failure evidence (spec 10.6 item 9).
	for _, ref := range req.Intent.FailureEvidence {
		addFailureEvidenceCandidates(idx, files, ref, allowGenerated, upsert)
	}

	// 14: repository map skeleton (spec 10.6 item 13), restricted to modes
	// that need a broad map so the candidate set stays bounded.
	if wantsRepoMap(req.Intent.Kind) {
		addRepoMapCandidates(idx, files, allowGenerated, upsert)
	}

	// 11/12/13 (high fan-in/high-risk affected entities, recently
	// edited/read entities) and item 15 (removed base-snapshot entities) are
	// not implemented in this PR: fan-in is scored as a bonus signal in
	// scoring.go using the graph directly, high-risk and recency require
	// data sources (pkg/risk churn analysis, a run event log) this package
	// does not depend on, and base-snapshot deltas are pkg/changeintel's
	// responsibility (PR 3).

	out := make([]*candidateItem, 0, len(order))
	for _, key := range order {
		c := byKey[key]
		c.Section = pickSection(c)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return candidateLess(out[i], out[j]) })
	return out
}

func mergeFlags(dst *candidateFlags, src candidateFlags) {
	dst.ExplicitRequired = dst.ExplicitRequired || src.ExplicitRequired
	dst.ExplicitSelector = dst.ExplicitSelector || src.ExplicitSelector
	dst.Changed = dst.Changed || src.Changed
	dst.Focus = dst.Focus || src.Focus
	dst.FailureNamed = dst.FailureNamed || src.FailureNamed
	dst.DirectCallerCallee = dst.DirectCallerCallee || src.DirectCallerCallee
	dst.MappedTest = dst.MappedTest || src.MappedTest
	dst.PublicAPI = dst.PublicAPI || src.PublicAPI
	dst.TransitiveDepth2 = dst.TransitiveDepth2 || src.TransitiveDepth2
	dst.FanInHighPercentile = dst.FanInHighPercentile || src.FanInHighPercentile
	dst.Generated = dst.Generated || src.Generated
	if src.TaskTermMatch > dst.TaskTermMatch {
		dst.TaskTermMatch = src.TaskTermMatch
	}
}

func pickSection(c *candidateItem) Section {
	if c.Section != "" {
		for _, s := range sectionPriority {
			if s == c.Section {
				return c.Section
			}
		}
	}
	switch {
	case c.Flags.Focus, c.Flags.ExplicitRequired, c.Flags.ExplicitSelector:
		return SectionFocus
	case c.Flags.Changed:
		return SectionChanges
	default:
		if c.Section != "" {
			return c.Section
		}
		return SectionRepoMap
	}
}

func candidateLess(a, b *candidateItem) bool {
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.StartLine != b.StartLine {
		return a.StartLine < b.StartLine
	}
	return a.ID < b.ID
}

func indexFileLookup(idx *model.Index) map[string]model.FileSummary {
	m := make(map[string]model.FileSummary, len(idx.Files))
	for _, f := range idx.Files {
		m[normalizeRelPath(f.Path)] = f
	}
	return m
}

func normalizeRelPath(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}

func explicitlySelectedPaths(selectors []Selector) map[string]bool {
	out := make(map[string]bool, len(selectors))
	for _, s := range selectors {
		if s.File != "" {
			out[normalizeRelPath(s.File)] = true
		}
	}
	return out
}

func candidateFromSymbol(file model.FileSummary, sym *model.Symbol) *candidateItem {
	return &candidateItem{
		ID:        candidateKey(file.Path, sym.Kind, sym.Name, sym.StartLine),
		Path:      file.Path,
		Language:  file.Language,
		Kind:      sym.Kind,
		Name:      sym.Name,
		StartLine: sym.StartLine,
		EndLine:   sym.EndLine,
		Signature: sym.Signature,
		Symbol:    sym,
	}
}

func candidateKey(path, kind, name string, startLine int) string {
	return normalizeRelPath(path) + "\x00" + kind + "\x00" + name + "\x00" + itoa(startLine)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func resolveSelector(idx *model.Index, graph *xref.Graph, files map[string]model.FileSummary, sel Selector, allowGenerated func(string) bool, upsert func(*candidateItem) *candidateItem) {
	if sel.EntityID != "" && graph != nil {
		for i := range graph.Definitions {
			def := &graph.Definitions[i]
			if def.ID != sel.EntityID {
				continue
			}
			file, ok := files[normalizeRelPath(def.File)]
			if !ok || (file.Generated != nil && !allowGenerated(def.File)) {
				return
			}
			c := &candidateItem{
				ID:        candidateKey(def.File, def.Kind, def.Name, def.StartLine),
				EntityID:  def.ID,
				Path:      def.File,
				Language:  file.Language,
				Kind:      def.Kind,
				Name:      def.Name,
				StartLine: def.StartLine,
				EndLine:   def.EndLine,
				Signature: def.Signature,
				Required:  sel.Required,
			}
			c.Flags.ExplicitRequired = sel.Required
			c.Flags.ExplicitSelector = !sel.Required
			c.Flags.Focus = true
			c.Flags.Generated = file.Generated != nil
			c.Flags.PublicAPI = looksPublicAPI(def.Name, def.Kind)
			upsert(c)
			return
		}
		return
	}

	if sel.File == "" {
		return
	}
	rel := normalizeRelPath(sel.File)
	file, ok := files[rel]
	if !ok || (file.Generated != nil && !allowGenerated(sel.File)) {
		return
	}

	var sym *model.Symbol
	switch {
	case sel.Symbol != "":
		for i := range file.Symbols {
			if file.Symbols[i].Name == sel.Symbol && (sel.Kind == "" || file.Symbols[i].Kind == sel.Kind) {
				sym = &file.Symbols[i]
				break
			}
		}
	case sel.Line > 0:
		sym = findSymbolAtLine(file.Symbols, sel.Line)
	case sel.StartLine > 0:
		sym = findSymbolAtLine(file.Symbols, sel.StartLine)
	}

	if sym == nil {
		// Whole-file selection: represent every top-level symbol as an
		// explicit candidate so the file's content is reachable.
		for i := range file.Symbols {
			c := candidateFromSymbol(file, &file.Symbols[i])
			c.Required = sel.Required
			c.Flags.ExplicitRequired = sel.Required
			c.Flags.ExplicitSelector = !sel.Required
			c.Flags.Generated = file.Generated != nil
			c.Flags.PublicAPI = looksPublicAPI(file.Symbols[i].Name, file.Symbols[i].Kind)
			resolveEntityID(graph, c)
			upsert(c)
		}
		return
	}

	c := candidateFromSymbol(file, sym)
	c.Required = sel.Required
	c.Flags.ExplicitRequired = sel.Required
	c.Flags.ExplicitSelector = !sel.Required
	c.Flags.Focus = sel.Line > 0 || sel.StartLine > 0
	c.Flags.Generated = file.Generated != nil
	c.Flags.PublicAPI = looksPublicAPI(sym.Name, sym.Kind)
	resolveEntityID(graph, c)
	upsert(c)
}

func resolveEntityID(graph *xref.Graph, c *candidateItem) {
	if graph == nil {
		return
	}
	for i := range graph.Definitions {
		def := &graph.Definitions[i]
		if def.File == c.Path && def.Kind == c.Kind && def.Name == c.Name && def.StartLine == c.StartLine {
			c.EntityID = def.ID
			return
		}
	}
}

func findSymbolAtLine(symbols []model.Symbol, line int) *model.Symbol {
	for i := range symbols {
		if line >= symbols[i].StartLine && line <= symbols[i].EndLine {
			return &symbols[i]
		}
	}
	return nil
}

func addGraphNeighbors(graph *xref.Graph, rootIDs []string, reverse bool, section Section, allowGenerated func(string) bool, upsert func(*candidateItem) *candidateItem) {
	depth1 := graph.Walk(rootIDs, 1, reverse)
	depth2 := graph.Walk(rootIDs, 2, reverse)

	rootSet := make(map[string]bool, len(rootIDs))
	for _, id := range rootIDs {
		rootSet[id] = true
	}
	depth1Set := make(map[string]bool, len(depth1.Nodes))
	for _, n := range depth1.Nodes {
		depth1Set[n.ID] = true
	}

	for _, def := range depth1.Nodes {
		if rootSet[def.ID] {
			continue
		}
		addNeighborCandidate(def, section, false, allowGenerated, upsert)
	}
	for _, def := range depth2.Nodes {
		if rootSet[def.ID] || depth1Set[def.ID] {
			continue
		}
		addNeighborCandidate(def, section, true, allowGenerated, upsert)
	}
}

func addNeighborCandidate(def xref.Definition, section Section, transitive bool, allowGenerated func(string) bool, upsert func(*candidateItem) *candidateItem) {
	c := &candidateItem{
		ID:        candidateKey(def.File, def.Kind, def.Name, def.StartLine),
		EntityID:  def.ID,
		Path:      def.File,
		Kind:      def.Kind,
		Name:      def.Name,
		StartLine: def.StartLine,
		EndLine:   def.EndLine,
		Signature: def.Signature,
		Section:   section,
	}
	c.Flags.DirectCallerCallee = !transitive
	c.Flags.TransitiveDepth2 = transitive
	c.Flags.PublicAPI = looksPublicAPI(def.Name, def.Kind)
	upsert(c)
}

func addFailureEvidenceCandidates(idx *model.Index, files map[string]model.FileSummary, ref ExternalRef, allowGenerated func(string) bool, upsert func(*candidateItem) *candidateItem) {
	haystack := ref.Text
	matchPath := func(rel string) bool {
		if ref.Path != "" && normalizeRelPath(ref.Path) == rel {
			return true
		}
		if haystack != "" && strings.Contains(haystack, rel) {
			return true
		}
		return false
	}

	for rel, file := range files {
		if !matchPath(rel) {
			continue
		}
		if file.Generated != nil && !allowGenerated(rel) {
			continue
		}
		matchedAny := false
		for i := range file.Symbols {
			sym := &file.Symbols[i]
			if haystack == "" || strings.Contains(haystack, sym.Name) {
				c := candidateFromSymbol(file, sym)
				c.Section = SectionFailures
				c.Flags.FailureNamed = true
				upsert(c)
				matchedAny = true
			}
		}
		if !matchedAny {
			for i := range file.Symbols {
				sym := &file.Symbols[i]
				c := candidateFromSymbol(file, sym)
				c.Section = SectionFailures
				c.Flags.FailureNamed = true
				upsert(c)
			}
		}
	}
}

func wantsRepoMap(kind TaskKind) bool {
	switch kind {
	case TaskExplore, TaskResume, "":
		return true
	default:
		return false
	}
}

// maxRepoMapCandidates bounds how many file-level repository-map candidates
// generateCandidates emits, keeping the candidate set proportional to the
// workspace even before budget packing runs.
const maxRepoMapCandidates = 500

func addRepoMapCandidates(idx *model.Index, files map[string]model.FileSummary, allowGenerated func(string) bool, upsert func(*candidateItem) *candidateItem) {
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	if len(paths) > maxRepoMapCandidates {
		paths = paths[:maxRepoMapCandidates]
	}
	for _, rel := range paths {
		file := files[rel]
		if file.Generated != nil && !allowGenerated(rel) {
			continue
		}
		c := &candidateItem{
			ID:       candidateKey(file.Path, "file", file.Path, 0),
			Path:     file.Path,
			Language: file.Language,
			Kind:     "file",
			Name:     file.Path,
			Section:  SectionRepoMap,
		}
		c.Flags.Generated = file.Generated != nil
		upsert(c)
	}
}

func looksLikeTypeKind(kind string) bool {
	lower := strings.ToLower(kind)
	return strings.Contains(lower, "type") || strings.Contains(lower, "interface") || strings.Contains(lower, "struct") || strings.Contains(lower, "class")
}

// looksPublicAPI heuristically flags an exported/public symbol using the Go
// convention (capitalized identifier). Languages with different visibility
// rules (leading underscore for "private" in Python, "export" keywords in
// TypeScript) are not modeled in this PR; see the final report for scope.
func looksPublicAPI(name, kind string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)[0]
	return r >= 'A' && r <= 'Z'
}

func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
