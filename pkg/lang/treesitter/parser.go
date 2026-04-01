// Package treesitter implements the lang.Parser interface using gotreesitter for multi-language structural parsing.
package treesitter

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/gts-suite/pkg/model"
)

type Parser struct {
	entry       grammars.LangEntry
	lang        *gotreesitter.Language
	parserPool  sync.Pool
	tagsQuery   *gotreesitter.Query
	treeLocksMu sync.Mutex
	treeLocks   map[*gotreesitter.Tree]*treeLockEntry
}

type treeLockEntry struct {
	mu   sync.Mutex
	refs int
}

// NewParser creates a Parser for the language described by the given LangEntry.
func NewParser(entry grammars.LangEntry) (*Parser, error) {
	if strings.TrimSpace(entry.Name) == "" {
		return nil, fmt.Errorf("language entry name is required")
	}
	if entry.Language == nil {
		return nil, fmt.Errorf("language loader is required for %q", entry.Name)
	}
	if strings.TrimSpace(entry.TagsQuery) == "" {
		return nil, fmt.Errorf("tags query is required for %q", entry.Name)
	}

	lang := entry.Language()
	if lang == nil {
		return nil, fmt.Errorf("language loader returned nil for %q", entry.Name)
	}

	tagsQuery, err := gotreesitter.NewQuery(entry.TagsQuery, lang)
	if err != nil {
		return nil, fmt.Errorf("compile tags query for %q: %w", entry.Name, err)
	}

	return &Parser{
		entry:     entry,
		lang:      lang,
		tagsQuery: tagsQuery,
		treeLocks: make(map[*gotreesitter.Tree]*treeLockEntry),
		parserPool: sync.Pool{
			New: func() any { return gotreesitter.NewParser(lang) },
		},
	}, nil
}

func (p *Parser) Language() string {
	return p.entry.Name
}

func (p *Parser) Parse(path string, src []byte) (model.FileSummary, error) {
	summary, tree, err := p.ParseWithTree(path, src)
	if tree != nil {
		tree.Release()
	}
	return summary, err
}

// ParseBoundTree builds a structural summary from an existing bound tree
// without reparsing the source.
func (p *Parser) ParseBoundTree(path string, tree *gotreesitter.BoundTree) (model.FileSummary, error) {
	summary := model.FileSummary{
		Path:     path,
		Language: p.Language(),
	}
	if tree == nil || tree.RootNode() == nil {
		return summary, nil
	}
	src := tree.Source()
	if len(src) == 0 {
		return summary, nil
	}
	return p.buildSummaryFromRoot(path, src, tree.RootNode()), nil
}

// ParseWithTree parses a source file and returns its structural summary along with the AST.
func (p *Parser) ParseWithTree(path string, src []byte) (model.FileSummary, *gotreesitter.Tree, error) {
	summary := model.FileSummary{
		Path:     path,
		Language: p.Language(),
	}
	if len(src) == 0 {
		return summary, gotreesitter.NewTree(nil, src, p.lang), nil
	}

	tree, err := p.parseTree(src)
	if err != nil {
		return summary, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if tree == nil || tree.RootNode() == nil {
		return summary, tree, nil
	}

	return p.buildSummaryFromTree(path, src, tree), tree, nil
}

// ParseIncrementalWithTree re-parses a source file incrementally using a previous tree and source.
func (p *Parser) ParseIncrementalWithTree(path string, src, oldSrc []byte, oldTree *gotreesitter.Tree) (model.FileSummary, *gotreesitter.Tree, error) {
	summary := model.FileSummary{
		Path:     path,
		Language: p.Language(),
	}
	if len(src) == 0 {
		return summary, gotreesitter.NewTree(nil, src, p.lang), nil
	}

	if oldTree == nil || len(oldSrc) == 0 {
		return p.ParseWithTree(path, src)
	}

	unlock := p.lockTree(oldTree)
	defer unlock()

	if oldTree.RootNode() == nil {
		return p.ParseWithTree(path, src)
	}

	if bytes.Equal(src, oldSrc) {
		return p.buildSummaryFromTree(path, src, oldTree), oldTree, nil
	}

	if edit, ok := singleEdit(oldSrc, src); ok {
		if edit.StartByte == uint32(len(oldSrc)) {
			// Appends/truncations at EOF can produce unstable incremental trees
			// with some token-source grammars; fall back to a full parse.
			return p.ParseWithTree(path, src)
		}
		oldTree.Edit(edit)
		summary, tree, err := p.parseIncrementalTree(path, src, oldTree)
		if err != nil {
			return model.FileSummary{}, nil, err
		}
		if tree == nil || tree.RootNode() == nil {
			return p.ParseWithTree(path, src)
		}
		return summary, tree, nil
	}

	return p.ParseWithTree(path, src)
}

func (p *Parser) parseIncrementalTree(path string, src []byte, oldTree *gotreesitter.Tree) (model.FileSummary, *gotreesitter.Tree, error) {
	summary := model.FileSummary{
		Path:     path,
		Language: p.Language(),
	}

	tree, err := p.parseTreeIncremental(src, oldTree)
	if err != nil {
		return summary, nil, fmt.Errorf("incremental parse %s: %w", path, err)
	}
	if tree == nil || tree.RootNode() == nil {
		return summary, tree, nil
	}
	return p.buildSummaryFromTree(path, src, tree), tree, nil
}

func (p *Parser) buildSummaryFromTree(path string, src []byte, tree *gotreesitter.Tree) model.FileSummary {
	if tree == nil {
		return model.FileSummary{
			Path:     path,
			Language: p.Language(),
		}
	}
	return p.buildSummaryFromRoot(path, src, tree.RootNode())
}

func (p *Parser) buildSummaryFromRoot(path string, src []byte, root *gotreesitter.Node) model.FileSummary {
	summary := model.FileSummary{
		Path:     path,
		Language: p.Language(),
	}
	tags := p.extractTags(root, src)
	summary.Imports = p.extractImports(root, src)
	summary.Symbols = p.extractSymbols(src, root, tags)
	summary.References = p.extractReferences(tags)
	return summary
}

func (p *Parser) extractTags(root *gotreesitter.Node, src []byte) []gotreesitter.Tag {
	if root == nil || p.tagsQuery == nil {
		return nil
	}

	matches := p.tagsQuery.ExecuteNode(root, p.lang, src)
	if len(matches) == 0 {
		return nil
	}

	tags := make([]gotreesitter.Tag, 0, len(matches))
	for _, match := range matches {
		var tag gotreesitter.Tag
		for _, capture := range match.Captures {
			if capture.Node == nil {
				continue
			}
			switch {
			case capture.Name == "name":
				tag.Name = capture.Node.Text(src)
				tag.NameRange = capture.Node.Range()
			case strings.HasPrefix(capture.Name, "definition."), strings.HasPrefix(capture.Name, "reference."):
				tag.Kind = capture.Name
				tag.Range = capture.Node.Range()
			}
		}

		if tag.Kind == "" {
			continue
		}
		if tag.Name == "" {
			start := int(tag.Range.StartByte)
			end := int(tag.Range.EndByte)
			if start < 0 {
				start = 0
			}
			if end < start {
				end = start
			}
			if end > len(src) {
				end = len(src)
			}
			tag.Name = string(src[start:end])
			tag.NameRange = tag.Range
		}
		tags = append(tags, tag)
	}

	if len(tags) == 0 {
		return nil
	}
	return tags
}

func (p *Parser) parseTree(src []byte) (*gotreesitter.Tree, error) {
	parser := p.acquireParser()
	defer p.releaseParser(parser)

	if p.entry.TokenSourceFactory != nil {
		ts := p.entry.TokenSourceFactory(src, p.lang)
		if ts != nil {
			tree, err := safeParseCall(p.Language(), func() (*gotreesitter.Tree, error) {
				return parser.ParseWithTokenSource(src, ts)
			})
			if err == nil && tree != nil && tree.RootNode() != nil && strings.TrimSpace(tree.RootNode().Type(p.lang)) != "" {
				return tree, nil
			}
			if tree != nil {
				tree.Release()
			}
		}
	}
	return safeParseCall(p.Language(), func() (*gotreesitter.Tree, error) {
		return parser.Parse(src)
	})
}

func (p *Parser) parseTreeIncremental(src []byte, oldTree *gotreesitter.Tree) (*gotreesitter.Tree, error) {
	if oldTree == nil {
		return p.parseTree(src)
	}

	parser := p.acquireParser()
	defer p.releaseParser(parser)

	if p.entry.TokenSourceFactory != nil {
		ts := p.entry.TokenSourceFactory(src, p.lang)
		if ts != nil {
			tree, err := safeParseCall(p.Language(), func() (*gotreesitter.Tree, error) {
				return parser.ParseIncrementalWithTokenSource(src, oldTree, ts)
			})
			if err == nil && tree != nil && tree.RootNode() != nil && strings.TrimSpace(tree.RootNode().Type(p.lang)) != "" {
				return tree, nil
			}
			if tree != nil {
				tree.Release()
			}
		}
	}
	return safeParseCall(p.Language(), func() (*gotreesitter.Tree, error) {
		return parser.ParseIncremental(src, oldTree)
	})
}

func safeParseCall(language string, fn func() (*gotreesitter.Tree, error)) (tree *gotreesitter.Tree, err error) {
	defer func() {
		if r := recover(); r != nil {
			if tree != nil {
				tree.Release()
				tree = nil
			}
			err = fmt.Errorf("gotreesitter panic in %s parser: %v", language, r)
		}
	}()
	return fn()
}

func (p *Parser) acquireParser() *gotreesitter.Parser {
	candidate := p.parserPool.Get()
	if parser, ok := candidate.(*gotreesitter.Parser); ok && parser != nil {
		return parser
	}
	return gotreesitter.NewParser(p.lang)
}

func (p *Parser) releaseParser(parser *gotreesitter.Parser) {
	if parser == nil {
		return
	}
	p.parserPool.Put(parser)
}

func (p *Parser) lockTree(tree *gotreesitter.Tree) func() {
	if tree == nil {
		return func() {}
	}

	p.treeLocksMu.Lock()
	entry, exists := p.treeLocks[tree]
	if !exists {
		entry = &treeLockEntry{}
		p.treeLocks[tree] = entry
	}
	entry.refs++
	p.treeLocksMu.Unlock()

	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()

		p.treeLocksMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(p.treeLocks, tree)
		}
		p.treeLocksMu.Unlock()
	}
}

func (p *Parser) extractImports(root *gotreesitter.Node, src []byte) []string {
	if root == nil {
		return nil
	}

	imports := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		imports[value] = struct{}{}
	}

	gotreesitter.Walk(root, func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		if node == nil {
			return gotreesitter.WalkContinue
		}

		nodeType := node.Type(p.lang)
		if isImportNodeType(p.entry.Name, nodeType) {
			for _, imp := range extractImportsByLanguage(p.entry.Name, nodeType, strings.TrimSpace(node.Text(src)), node, p.lang, src) {
				add(imp)
			}
			return gotreesitter.WalkContinue
		}

		if p.entry.Name == "ruby" && nodeType == "call" {
			if imp := extractRubyRequireImport(strings.TrimSpace(node.Text(src))); imp != "" {
				add(imp)
			}
		}

		return gotreesitter.WalkContinue
	})

	if len(imports) == 0 {
		return nil
	}
	values := make([]string, 0, len(imports))
	for imp := range imports {
		values = append(values, imp)
	}
	sort.Strings(values)
	return values
}

func (p *Parser) extractSymbols(src []byte, root *gotreesitter.Node, tags []gotreesitter.Tag) []model.Symbol {
	if len(tags) == 0 {
		return nil
	}

	symbols := make([]model.Symbol, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		symbol, ok := symbolFromTag(src, root, p.lang, p.entry.Name, tag)
		if !ok {
			continue
		}

		key := symbol.Kind + "|" + symbol.Name + "|" + strconv.Itoa(symbol.StartLine) + "|" + strconv.Itoa(symbol.EndLine)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		symbols = append(symbols, symbol)
	}

	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].StartLine == symbols[j].StartLine {
			if symbols[i].EndLine == symbols[j].EndLine {
				if symbols[i].Kind == symbols[j].Kind {
					return symbols[i].Name < symbols[j].Name
				}
				return symbols[i].Kind < symbols[j].Kind
			}
			return symbols[i].EndLine < symbols[j].EndLine
		}
		return symbols[i].StartLine < symbols[j].StartLine
	})
	return symbols
}

func (p *Parser) extractReferences(tags []gotreesitter.Tag) []model.Reference {
	if len(tags) == 0 {
		return nil
	}

	references := make([]model.Reference, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		reference, ok := referenceFromTag(tag)
		if !ok {
			continue
		}

		key := reference.Kind + "|" + reference.Name + "|" + strconv.Itoa(reference.StartLine) + "|" + strconv.Itoa(reference.StartColumn)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		references = append(references, reference)
	}

	sort.Slice(references, func(i, j int) bool {
		if references[i].StartLine == references[j].StartLine {
			if references[i].StartColumn == references[j].StartColumn {
				if references[i].Kind == references[j].Kind {
					return references[i].Name < references[j].Name
				}
				return references[i].Kind < references[j].Kind
			}
			return references[i].StartColumn < references[j].StartColumn
		}
		return references[i].StartLine < references[j].StartLine
	})
	return references
}

func symbolFromTag(src []byte, root *gotreesitter.Node, lang *gotreesitter.Language, language string, tag gotreesitter.Tag) (model.Symbol, bool) {
	kind, ok := mapTagKind(tag.Kind)
	if !ok {
		return model.Symbol{}, false
	}

	name := strings.TrimSpace(tag.Name)
	if name == "" {
		return model.Symbol{}, false
	}

	start := int(tag.Range.StartPoint.Row) + 1
	end := int(tag.Range.EndPoint.Row) + 1
	if end < start {
		end = start
	}

	signature := summarizeSignature(rawRangeText(src, tag.Range))
	receiver := inferReceiver(language, kind, signature, root, lang, src, tag.Range)

	return model.Symbol{
		Kind:      kind,
		Name:      name,
		Signature: signature,
		Receiver:  receiver,
		StartLine: start,
		EndLine:   end,
	}, true
}

func referenceFromTag(tag gotreesitter.Tag) (model.Reference, bool) {
	if !strings.HasPrefix(tag.Kind, "reference.") {
		return model.Reference{}, false
	}

	name := strings.TrimSpace(tag.Name)
	if name == "" {
		return model.Reference{}, false
	}

	startLine := int(tag.NameRange.StartPoint.Row) + 1
	endLine := int(tag.NameRange.EndPoint.Row) + 1
	if endLine < startLine {
		endLine = startLine
	}
	startCol := int(tag.NameRange.StartPoint.Column) + 1
	endCol := int(tag.NameRange.EndPoint.Column) + 1
	if endCol < startCol {
		endCol = startCol
	}

	return model.Reference{
		Kind:        tag.Kind,
		Name:        name,
		StartLine:   startLine,
		EndLine:     endLine,
		StartColumn: startCol,
		EndColumn:   endCol,
	}, true
}

func mapTagKind(tagKind string) (string, bool) {
	if !strings.HasPrefix(tagKind, "definition.") {
		return "", false
	}

	switch strings.TrimPrefix(tagKind, "definition.") {
	case "function":
		return "function_definition", true
	case "method":
		return "method_definition", true
	case "constructor":
		return "constructor_definition", true
	case "class":
		return "class_definition", true
	case "interface":
		return "interface_definition", true
	case "struct":
		return "struct_definition", true
	case "enum":
		return "enum_definition", true
	case "constant":
		return "constant_definition", true
	case "variable", "field", "property":
		return "variable_definition", true
	case "module", "namespace", "package":
		return "module_definition", true
	case "type", "typedef", "alias", "union":
		return "type_definition", true
	default:
		return "type_definition", true
	}
}

func rawRangeText(src []byte, rng gotreesitter.Range) string {
	if len(src) == 0 {
		return ""
	}

	start := int(rng.StartByte)
	end := int(rng.EndByte)
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(src) {
		start = len(src)
	}
	if end > len(src) {
		end = len(src)
	}
	return string(src[start:end])
}

func summarizeSignature(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	if idx := strings.Index(trimmed, "{"); idx > 0 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	trimmed = strings.TrimSuffix(trimmed, ";")
	return strings.Join(strings.Fields(trimmed), " ")
}

func inferGoReceiver(signature string) string {
	const prefix = "func ("
	if !strings.HasPrefix(signature, prefix) {
		return ""
	}
	rest := signature[len(prefix):]
	closing := strings.Index(rest, ")")
	if closing <= 0 {
		return ""
	}
	return strings.TrimSpace(rest[:closing])
}

func inferReceiver(language, kind, signature string, root *gotreesitter.Node, lang *gotreesitter.Language, src []byte, rng gotreesitter.Range) string {
	switch language {
	case "go":
		if kind == "method_definition" {
			return inferGoReceiver(signature)
		}
	case "python":
		return findEnclosingContainerName(root, lang, src, rng, map[string]bool{"class_definition": true})
	case "javascript", "typescript", "tsx":
		return findEnclosingContainerName(root, lang, src, rng, map[string]bool{"class_declaration": true, "class": true})
	case "java", "c_sharp":
		return findEnclosingContainerName(root, lang, src, rng, map[string]bool{"class_declaration": true})
	case "rust":
		implNode := findEnclosingContainerNode(root, lang, rng, map[string]bool{"impl_item": true})
		if implNode == nil {
			return ""
		}
		return extractRustImplReceiver(implNode.Text(src))
	}
	return ""
}

func findEnclosingContainerName(root *gotreesitter.Node, lang *gotreesitter.Language, src []byte, rng gotreesitter.Range, nodeTypes map[string]bool) string {
	node := findEnclosingContainerNode(root, lang, rng, nodeTypes)
	if node == nil {
		return ""
	}
	return firstIdentifierText(node, lang, src)
}

func findEnclosingContainerNode(root *gotreesitter.Node, lang *gotreesitter.Language, rng gotreesitter.Range, nodeTypes map[string]bool) *gotreesitter.Node {
	if root == nil || lang == nil {
		return nil
	}
	var best *gotreesitter.Node
	var bestSpan uint32
	gotreesitter.Walk(root, func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		if node == nil {
			return gotreesitter.WalkContinue
		}
		nodeType := node.Type(lang)
		if !nodeTypes[nodeType] {
			return gotreesitter.WalkContinue
		}
		if !nodeContainsRange(node, rng) {
			return gotreesitter.WalkContinue
		}
		span := node.EndByte() - node.StartByte()
		if best == nil || span < bestSpan {
			best = node
			bestSpan = span
		}
		return gotreesitter.WalkContinue
	})
	return best
}

func nodeContainsRange(node *gotreesitter.Node, rng gotreesitter.Range) bool {
	if node == nil {
		return false
	}
	return node.StartByte() <= rng.StartByte && node.EndByte() >= rng.EndByte
}

func firstIdentifierText(node *gotreesitter.Node, lang *gotreesitter.Language, src []byte) string {
	if node == nil {
		return ""
	}
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		nodeType := child.Type(lang)
		if NameIdentifierTypes[nodeType] {
			return strings.TrimSpace(child.Text(src))
		}
		if nested := firstIdentifierText(child, lang, src); nested != "" {
			return nested
		}
	}
	return ""
}

func extractRustImplReceiver(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "impl") {
		return ""
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "impl"))
	if strings.HasPrefix(text, "<") {
		if idx := strings.Index(text, ">"); idx >= 0 && idx+1 < len(text) {
			text = strings.TrimSpace(text[idx+1:])
		}
	}
	if idx := strings.Index(text, "{"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, " where "); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, " for "); idx >= 0 {
		text = strings.TrimSpace(text[idx+len(" for "):])
	}
	text = strings.TrimSpace(strings.Trim(text, "&*()"))
	if fields := strings.Fields(text); len(fields) > 0 {
		return strings.Trim(fields[0], ",")
	}
	return ""
}

func extractImportsByLanguage(language, nodeType, raw string, node *gotreesitter.Node, lang *gotreesitter.Language, src []byte) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	switch language {
	case "go":
		if nodeType != "import_spec" {
			return nil
		}
		if node != nil {
			if importPath := importPathFromSpec(node, lang, src); importPath != "" {
				return []string{importPath}
			}
		}
	case "python":
		if nodeType == "import_statement" || nodeType == "import_from_statement" {
			return []string{raw}
		}
	case "javascript", "typescript", "tsx":
		if nodeType == "import_statement" {
			return []string{raw}
		}
	case "rust":
		if nodeType == "use_declaration" {
			return []string{raw}
		}
	case "java":
		if nodeType == "import_declaration" {
			return []string{raw}
		}
	case "c", "cpp":
		if nodeType == "preproc_include" {
			return []string{raw}
		}
	case "c_sharp":
		if nodeType == "using_directive" || nodeType == "global_using_directive" {
			return []string{raw}
		}
	case "php":
		if nodeType == "use_declaration" || nodeType == "namespace_use_declaration" {
			return []string{raw}
		}
	case "kotlin":
		if nodeType == "import_header" {
			return []string{raw}
		}
	}
	if ImportNodeTypes[nodeType] {
		return []string{raw}
	}
	return nil
}

func importPathFromSpec(node *gotreesitter.Node, lang *gotreesitter.Language, src []byte) string {
	if node == nil {
		return ""
	}
	for i := node.ChildCount() - 1; i >= 0; i-- {
		child := node.Child(i)
		typeName := child.Type(lang)
		if typeName != "interpreted_string_literal" && typeName != "raw_string_literal" {
			continue
		}
		text := strings.TrimSpace(child.Text(src))
		text = strings.Trim(text, "\"`")
		if text != "" {
			return text
		}
	}
	return ""
}

func extractQuotedStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for i := 0; i < len(raw); i++ {
		quote := raw[i]
		if quote != '\'' && quote != '"' && quote != '`' {
			continue
		}
		start := i + 1
		for j := start; j < len(raw); j++ {
			if raw[j] == '\\' {
				j++
				continue
			}
			if raw[j] == quote {
				out = append(out, raw[start:j])
				i = j
				break
			}
		}
	}
	return out
}

func extractRubyRequireImport(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "require") {
		return ""
	}
	quoted := extractQuotedStrings(raw)
	if len(quoted) == 0 {
		return ""
	}
	return quoted[0]
}

func singleEdit(oldSrc, newSrc []byte) (gotreesitter.InputEdit, bool) {
	if bytes.Equal(oldSrc, newSrc) {
		return gotreesitter.InputEdit{}, false
	}

	start := 0
	maxPrefix := len(oldSrc)
	if len(newSrc) < maxPrefix {
		maxPrefix = len(newSrc)
	}
	for start < maxPrefix && oldSrc[start] == newSrc[start] {
		start++
	}

	oldEnd := len(oldSrc)
	newEnd := len(newSrc)
	for oldEnd > start && newEnd > start && oldSrc[oldEnd-1] == newSrc[newEnd-1] {
		oldEnd--
		newEnd--
	}

	return gotreesitter.InputEdit{
		StartByte:   uint32(start),
		OldEndByte:  uint32(oldEnd),
		NewEndByte:  uint32(newEnd),
		StartPoint:  pointAtOffset(oldSrc, start),
		OldEndPoint: pointAtOffset(oldSrc, oldEnd),
		NewEndPoint: pointAtOffset(newSrc, newEnd),
	}, true
}

func pointAtOffset(src []byte, offset int) gotreesitter.Point {
	var row uint32
	var col uint32
	for i := 0; i < offset && i < len(src); {
		r, size := utf8.DecodeRune(src[i:])
		if r == '\n' {
			row++
			col = 0
		} else {
			col++
		}
		i += size
	}
	return gotreesitter.Point{Row: row, Column: col}
}
