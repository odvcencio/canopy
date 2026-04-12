// Package scope resolves symbols visible at a given source line using tree-sitter AST traversal.
package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/canopy/pkg/model"
)

type Options struct {
	FilePath string
	Line     int
}

type Symbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Detail   string `json:"detail,omitempty"`
	DeclLine int    `json:"decl_line"`
}

type Report struct {
	File    string        `json:"file"`
	Line    int           `json:"line"`
	Package string        `json:"package"`
	Focus   *model.Symbol `json:"focus,omitempty"`
	Symbols []Symbol      `json:"symbols,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}
	if strings.TrimSpace(opts.FilePath) == "" {
		return Report{}, fmt.Errorf("file path is required")
	}
	if opts.Line <= 0 {
		opts.Line = 1
	}

	relPath, absPath, err := resolvePaths(idx.Root, opts.FilePath)
	if err != nil {
		return Report{}, err
	}

	fileSummary, err := findFileSummary(idx, relPath)
	if err != nil {
		return Report{}, err
	}

	source, err := os.ReadFile(absPath)
	if err != nil {
		return Report{}, err
	}

	entry := grammars.DetectLanguage(absPath)
	if entry == nil {
		return Report{}, fmt.Errorf("unsupported language for %s", absPath)
	}

	bound, err := grammars.ParseFile(absPath, source)
	if err != nil {
		return Report{}, fmt.Errorf("tree-sitter parse failed for %s: %w", absPath, err)
	}
	defer bound.Release()

	root := bound.RootNode()
	if root == nil {
		return Report{}, fmt.Errorf("tree-sitter produced nil root for %s", absPath)
	}

	report := Report{
		File:    fileSummary.Path,
		Line:    opts.Line,
		Package: inferPackageName(bound, root, fileSummary),
	}

	focus := findFocusSymbol(fileSummary.Symbols, opts.Line)
	if focus != nil {
		focusCopy := *focus
		report.Focus = &focusCopy
	}

	collector := newSymbolCollector()
	addImportsFromIndex(collector, fileSummary)
	addIndexedPackageSymbols(collector, idx, fileSummary)
	addLocalScope(collector, bound, root, source, opts.Line)

	report.Symbols = collector.symbols()
	return report, nil
}

func resolvePaths(root, inputPath string) (string, string, error) {
	cleaned := filepath.Clean(inputPath)
	candidate := cleaned
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}

	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", err
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}

	rel, relErr := filepath.Rel(rootAbs, absolute)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		rel = cleaned
	}

	return filepath.ToSlash(rel), absolute, nil
}

func findFileSummary(idx *model.Index, relPath string) (model.FileSummary, error) {
	normalized := filepath.ToSlash(filepath.Clean(relPath))
	for _, file := range idx.Files {
		if filepath.ToSlash(filepath.Clean(file.Path)) == normalized {
			return file, nil
		}
	}
	return model.FileSummary{}, fmt.Errorf("file %q not found in index", relPath)
}

func findFocusSymbol(symbols []model.Symbol, line int) *model.Symbol {
	var best *model.Symbol
	bestSpan := int(^uint(0) >> 1) // max int
	for i := range symbols {
		s := &symbols[i]
		if line >= s.StartLine && line <= s.EndLine {
			span := s.EndLine - s.StartLine
			if span < bestSpan {
				best = s
				bestSpan = span
			}
		}
	}
	return best
}

func inferPackageName(bound *gotreesitter.BoundTree, root *gotreesitter.Node, summary model.FileSummary) string {
	// For Go files, extract package name from package_clause
	if summary.Language == "go" {
		for i := 0; i < root.ChildCount(); i++ {
			child := root.Child(i)
			if bound.NodeType(child) == "package_clause" {
				for j := 0; j < child.ChildCount(); j++ {
					gc := child.Child(j)
					if bound.NodeType(gc) == "package_identifier" {
						return strings.TrimSpace(bound.NodeText(gc))
					}
				}
			}
		}
	}
	// Fallback: use directory name
	dir := filepath.Dir(filepath.Clean(summary.Path))
	if dir == "." {
		return filepath.Base(filepath.Dir(summary.Path))
	}
	return filepath.Base(dir)
}

func addImportsFromIndex(collector *symbolCollector, summary model.FileSummary) {
	for _, imp := range summary.Imports {
		name := importBase(imp)
		if name == "" || name == "_" {
			continue
		}
		collector.add(name, "import", imp, 0)
	}
}

func addIndexedPackageSymbols(collector *symbolCollector, idx *model.Index, fileSummary model.FileSummary) {
	dir := filepath.ToSlash(filepath.Dir(filepath.Clean(fileSummary.Path)))
	isTest := strings.HasSuffix(filepath.ToSlash(filepath.Clean(fileSummary.Path)), "_test.go")
	for _, file := range idx.Files {
		fileDir := filepath.ToSlash(filepath.Dir(filepath.Clean(file.Path)))
		if fileDir != dir {
			continue
		}
		if strings.HasSuffix(filepath.ToSlash(filepath.Clean(file.Path)), "_test.go") != isTest {
			continue
		}
		for _, symbol := range file.Symbols {
			switch symbol.Kind {
			case "function_definition":
				collector.add(symbol.Name, "package_function", symbol.Signature, symbol.StartLine)
			case "method_definition":
				collector.add(symbol.Name, "package_method", symbol.Signature, symbol.StartLine)
			case "type_definition":
				collector.add(symbol.Name, "package_type", symbol.Signature, symbol.StartLine)
			}
		}
	}
}

// addLocalScope walks the tree-sitter AST to find declarations visible at the target line.
// It finds the innermost scope containing the line and collects all declarations
// visible from that point: function parameters, local variables, and block-scoped names.
func addLocalScope(collector *symbolCollector, bound *gotreesitter.BoundTree, root *gotreesitter.Node, _ []byte, line int) {
	// Find the innermost function/method containing the target line
	funcNode := findContainingFunction(bound, root, line)
	if funcNode == nil {
		return
	}

	// Collect function parameters
	collectFunctionParams(collector, bound, funcNode)

	// Find the function body and walk it for local declarations
	body := findFunctionBody(bound, funcNode)
	if body != nil {
		collectBlockScope(collector, bound, body, line)
	}
}

// findContainingFunction finds the innermost function/method declaration containing the line.
func findContainingFunction(bound *gotreesitter.BoundTree, root *gotreesitter.Node, line int) *gotreesitter.Node {
	var best *gotreesitter.Node
	gotreesitter.Walk(root, func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		nodeType := bound.NodeType(node)
		if !isFunctionDecl(nodeType) {
			return gotreesitter.WalkContinue
		}
		start := int(node.StartPoint().Row) + 1
		end := int(node.EndPoint().Row) + 1
		if line >= start && line <= end {
			best = node
		}
		return gotreesitter.WalkContinue
	})
	return best
}

func isFunctionDecl(nodeType string) bool {
	switch nodeType {
	case "function_declaration", "method_declaration", "func_literal",
		"method_definition", "arrow_function",
		"function", "generator_function", "generator_function_declaration",
		"function_definition", "function_item",
		"constructor_declaration",
		"method",
		"function_definition_statement":
		return true
	}
	return false
}

// collectFunctionParams extracts parameter names from a function node.
// For Go methods, the first parameter_list is the receiver, the second is params,
// and the third is results. For regular functions, the first is params.
func collectFunctionParams(collector *symbolCollector, bound *gotreesitter.BoundTree, funcNode *gotreesitter.Node) {
	funcType := bound.NodeType(funcNode)
	isGoMethod := funcType == "method_declaration"

	paramListIndex := 0
	for i := 0; i < funcNode.ChildCount(); i++ {
		child := funcNode.Child(i)
		nodeType := bound.NodeType(child)

		switch nodeType {
		case "parameter_list":
			if isGoMethod && paramListIndex == 0 {
				// Go method receiver
				collectReceiverParam(collector, bound, child)
			} else {
				// Regular params or result params
				collectParamList(collector, bound, child)
			}
			paramListIndex++
		case "parameters", "formal_parameters",
			"function_params", "lambda_parameters":
			collectParamList(collector, bound, child)
		}
	}
}

func collectParamList(collector *symbolCollector, bound *gotreesitter.BoundTree, paramList *gotreesitter.Node) {
	gotreesitter.Walk(paramList, func(node *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		nodeType := bound.NodeType(node)
		switch nodeType {
		case "parameter_declaration", "parameter", "required_parameter",
			"optional_parameter", "rest_parameter":
			name, detail := extractParamNameAndType(bound, node)
			if name != "" && name != "_" {
				collector.add(name, "param", detail, int(node.StartPoint().Row)+1)
			}
		case "identifier":
			// For Python-style simple params (just identifiers in the param list)
			if depth == 1 {
				name := strings.TrimSpace(bound.NodeText(node))
				if name != "" && name != "self" && name != "cls" && name != "_" {
					collector.add(name, "param", "", int(node.StartPoint().Row)+1)
				}
			}
		}
		return gotreesitter.WalkContinue
	})
}

func extractParamNameAndType(bound *gotreesitter.BoundTree, paramNode *gotreesitter.Node) (string, string) {
	name := ""
	typeStr := ""
	for i := 0; i < paramNode.ChildCount(); i++ {
		child := paramNode.Child(i)
		if !child.IsNamed() {
			continue
		}
		childType := bound.NodeType(child)
		switch childType {
		case "identifier", "field_identifier", "name":
			if name == "" {
				name = strings.TrimSpace(bound.NodeText(child))
			}
		case "type_identifier", "pointer_type", "slice_type",
			"array_type", "map_type", "channel_type",
			"interface_type", "struct_type", "function_type",
			"qualified_type", "generic_type",
			"type_annotation", "type":
			typeStr = strings.TrimSpace(bound.NodeText(child))
		}
	}
	return name, typeStr
}

func collectReceiverParam(collector *symbolCollector, bound *gotreesitter.BoundTree, paramList *gotreesitter.Node) {
	for i := 0; i < paramList.ChildCount(); i++ {
		child := paramList.Child(i)
		if !child.IsNamed() {
			continue
		}
		nodeType := bound.NodeType(child)
		if nodeType == "parameter_declaration" {
			name, detail := extractParamNameAndType(bound, child)
			if name != "" && name != "_" {
				collector.add(name, "receiver", detail, int(child.StartPoint().Row)+1)
				return
			}
		}
	}
}

// findFunctionBody locates the body block of a function node.
func findFunctionBody(bound *gotreesitter.BoundTree, funcNode *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < funcNode.ChildCount(); i++ {
		child := funcNode.Child(i)
		nodeType := bound.NodeType(child)
		if isBlockNode(nodeType) {
			return child
		}
	}
	return nil
}

func isBlockNode(nodeType string) bool {
	switch nodeType {
	case "block", "block_statement", "compound_statement",
		"statement_block", "function_body", "suite",
		"body", "do_block", "class_body":
		return true
	}
	return false
}

// collectBlockScope walks a block node collecting declarations visible at the target line.
// It handles both direct statement children and statement_list wrappers.
func collectBlockScope(collector *symbolCollector, bound *gotreesitter.BoundTree, block *gotreesitter.Node, line int) {
	stmts := statementsOf(bound, block)
	for _, child := range stmts {
		start := int(child.StartPoint().Row) + 1
		end := int(child.EndPoint().Row) + 1

		if start > line {
			break
		}

		if line > end {
			collectDeclsFromStmt(collector, bound, child)
			continue
		}

		// We're inside this statement — collect its init-clause decls and recurse
		collectDeclsFromStmt(collector, bound, child)
		recurseIntoContainingBlock(collector, bound, child, line)
		return
	}
}

// statementsOf returns the named children that are actual statements.
// Some grammars wrap statements in a statement_list node; others have them directly.
func statementsOf(bound *gotreesitter.BoundTree, block *gotreesitter.Node) []*gotreesitter.Node {
	var stmts []*gotreesitter.Node
	for i := 0; i < block.ChildCount(); i++ {
		child := block.Child(i)
		if !child.IsNamed() {
			continue
		}
		nodeType := bound.NodeType(child)
		if nodeType == "statement_list" || nodeType == "statement_block_body" {
			// Unwrap: the real statements are children of statement_list
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				if gc.IsNamed() {
					stmts = append(stmts, gc)
				}
			}
			continue
		}
		stmts = append(stmts, child)
	}
	return stmts
}

// collectDeclsFromStmt extracts variable/const declarations from a statement node.
func collectDeclsFromStmt(collector *symbolCollector, bound *gotreesitter.BoundTree, stmt *gotreesitter.Node) {
	nodeType := bound.NodeType(stmt)
	switch nodeType {
	// Go short variable declarations
	case "short_var_declaration":
		collectShortVarDecl(collector, bound, stmt)
	// Go var/const declarations
	case "var_declaration", "const_declaration":
		collectVarConstDecl(collector, bound, stmt, nodeType)
	// Go type declarations inside functions
	case "type_declaration":
		collectTypeDecl(collector, bound, stmt)
	// JS/TS variable declarations
	case "variable_declaration", "lexical_declaration":
		collectJSVarDecl(collector, bound, stmt)
	// Python assignment
	case "assignment", "augmented_assignment":
		collectPythonAssignment(collector, bound, stmt)
	// Rust let bindings
	case "let_declaration":
		collectRustLetDecl(collector, bound, stmt)
	// Go range statements
	case "for_statement":
		collectGoForDecls(collector, bound, stmt)
	case "range_clause":
		collectRangeClauseDecls(collector, bound, stmt)
	// Labeled statements — recurse to inner stmt
	case "labeled_statement":
		for i := 0; i < stmt.ChildCount(); i++ {
			inner := stmt.Child(i)
			if inner.IsNamed() && bound.NodeType(inner) != "label_name" && bound.NodeType(inner) != "identifier" {
				collectDeclsFromStmt(collector, bound, inner)
				break
			}
		}
	}
}

func collectShortVarDecl(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	// In Go short var decl, the LHS identifiers come before `:=`
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		nodeType := bound.NodeType(child)
		if nodeType == "expression_list" {
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				if bound.NodeType(gc) == "identifier" {
					name := strings.TrimSpace(bound.NodeText(gc))
					if name != "" && name != "_" {
						collector.add(name, "local_var", "", int(gc.StartPoint().Row)+1)
					}
				}
			}
			return
		}
		if nodeType == "identifier" {
			name := strings.TrimSpace(bound.NodeText(child))
			if name != "" && name != "_" {
				collector.add(name, "local_var", "", int(child.StartPoint().Row)+1)
			}
		}
	}
}

func collectVarConstDecl(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node, declType string) {
	kind := "local_var"
	if declType == "const_declaration" {
		kind = "local_const"
	}
	gotreesitter.Walk(node, func(child *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		childType := bound.NodeType(child)
		if childType == "var_spec" || childType == "const_spec" {
			for i := 0; i < child.ChildCount(); i++ {
				gc := child.Child(i)
				if bound.NodeType(gc) == "identifier" {
					name := strings.TrimSpace(bound.NodeText(gc))
					if name != "" && name != "_" {
						collector.add(name, kind, "", int(gc.StartPoint().Row)+1)
					}
				}
			}
		}
		return gotreesitter.WalkContinue
	})
}

func collectTypeDecl(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	gotreesitter.Walk(node, func(child *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		if bound.NodeType(child) == "type_spec" {
			for i := 0; i < child.ChildCount(); i++ {
				gc := child.Child(i)
				if bound.NodeType(gc) == "type_identifier" || bound.NodeType(gc) == "identifier" {
					name := strings.TrimSpace(bound.NodeText(gc))
					if name != "" {
						collector.add(name, "local_type", "", int(gc.StartPoint().Row)+1)
					}
					break
				}
			}
		}
		return gotreesitter.WalkContinue
	})
}

func collectJSVarDecl(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	gotreesitter.Walk(node, func(child *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		childType := bound.NodeType(child)
		if childType == "variable_declarator" {
			for i := 0; i < child.ChildCount(); i++ {
				gc := child.Child(i)
				if bound.NodeType(gc) == "identifier" {
					name := strings.TrimSpace(bound.NodeText(gc))
					if name != "" {
						collector.add(name, "local_var", "", int(gc.StartPoint().Row)+1)
					}
					break
				}
			}
		}
		return gotreesitter.WalkContinue
	})
}

func collectPythonAssignment(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	// First child is the LHS
	if node.ChildCount() == 0 {
		return
	}
	lhs := node.Child(0)
	if lhs != nil && bound.NodeType(lhs) == "identifier" {
		name := strings.TrimSpace(bound.NodeText(lhs))
		if name != "" && name != "_" {
			collector.add(name, "local_var", "", int(lhs.StartPoint().Row)+1)
		}
	}
}

func collectRustLetDecl(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if bound.NodeType(child) == "identifier" {
			name := strings.TrimSpace(bound.NodeText(child))
			if name != "" && name != "_" {
				collector.add(name, "local_var", "", int(child.StartPoint().Row)+1)
			}
			return
		}
	}
}

func collectGoForDecls(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		nodeType := bound.NodeType(child)
		switch nodeType {
		case "range_clause":
			collectRangeClauseDecls(collector, bound, child)
		case "short_var_declaration":
			collectShortVarDecl(collector, bound, child)
		}
	}
}

func collectRangeClauseDecls(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	// range_clause children: expression_list `:=` `range` expression
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if bound.NodeType(child) == "expression_list" {
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				if bound.NodeType(gc) == "identifier" {
					name := strings.TrimSpace(bound.NodeText(gc))
					if name != "" && name != "_" {
						collector.add(name, "local_var", "", int(gc.StartPoint().Row)+1)
					}
				}
			}
			return
		}
	}
}

// recurseIntoContainingBlock finds inner blocks within a statement and recurses.
func recurseIntoContainingBlock(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node, line int) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			continue
		}

		start := int(child.StartPoint().Row) + 1
		end := int(child.EndPoint().Row) + 1
		if line < start || line > end {
			continue
		}

		nodeType := bound.NodeType(child)
		if isBlockNode(nodeType) {
			collectBlockScope(collector, bound, child, line)
			return
		}
		// Recurse deeper for compound statements (if, for, switch, etc.)
		recurseIntoContainingBlock(collector, bound, child, line)
	}
}

func importBase(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

type symbolCollector struct {
	items  []Symbol
	byName map[string]int
}

func newSymbolCollector() *symbolCollector {
	return &symbolCollector{
		items:  make([]Symbol, 0, 32),
		byName: make(map[string]int),
	}
}

func (c *symbolCollector) add(name, kind, detail string, line int) {
	name = strings.TrimSpace(name)
	if name == "" || name == "_" {
		return
	}
	symbol := Symbol{
		Name:     name,
		Kind:     kind,
		Detail:   strings.TrimSpace(detail),
		DeclLine: line,
	}
	if idx, ok := c.byName[name]; ok {
		c.items[idx] = symbol
		return
	}
	c.byName[name] = len(c.items)
	c.items = append(c.items, symbol)
}

func (c *symbolCollector) symbols() []Symbol {
	out := make([]Symbol, 0, len(c.items))
	for _, symbol := range c.items {
		if strings.TrimSpace(symbol.Name) == "" {
			continue
		}
		out = append(out, symbol)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeclLine == out[j].DeclLine {
			if out[i].Name == out[j].Name {
				return out[i].Kind < out[j].Kind
			}
			return out[i].Name < out[j].Name
		}
		return out[i].DeclLine < out[j].DeclLine
	})
	return out
}
