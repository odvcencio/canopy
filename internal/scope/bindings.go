package scope

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
)

type lexicalBinding struct {
	name string
	line int
}

func collectParameterNode(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node, kind string) {
	detail := nodeFieldText(bound, node, "type")
	targets := parameterBindingNodes(bound, node)
	seen := make(map[string]struct{})
	for _, target := range targets {
		for _, binding := range bindingNames(bound, target) {
			if _, ok := seen[binding.name]; ok {
				continue
			}
			seen[binding.name] = struct{}{}
			collector.add(binding.name, kind, detail, binding.line)
		}
	}
}

func parameterBindingNodes(bound *gotreesitter.BoundTree, node *gotreesitter.Node) []*gotreesitter.Node {
	if node == nil {
		return nil
	}
	var targets []*gotreesitter.Node
	seen := make(map[*gotreesitter.Node]struct{})
	add := func(candidate *gotreesitter.Node) {
		if candidate == nil {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		targets = append(targets, candidate)
	}
	for _, field := range []string{"name", "pattern", "declarator"} {
		add(bound.ChildByField(node, field))
	}

	typeNode := bound.ChildByField(node, "type")
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() || child == typeNode {
			continue
		}
		if isBindingShape(bound.NodeType(child)) {
			add(child)
		}
	}
	return targets
}

func collectLocalDeclaration(collector *symbolCollector, bound *gotreesitter.BoundTree, declaration *gotreesitter.Node) {
	if declaration == nil {
		return
	}
	detail := nodeFieldText(bound, declaration, "type")
	found := false
	for i := 0; i < declaration.ChildCount(); i++ {
		child := declaration.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		switch bound.NodeType(child) {
		case "variable_declarator", "init_declarator":
			found = addDeclaratorBindings(collector, bound, child, detail) || found
		}
	}
	if found {
		return
	}
	addDeclaratorBindings(collector, bound, declaration, detail)
}

func addDeclaratorBindings(collector *symbolCollector, bound *gotreesitter.BoundTree, declarator *gotreesitter.Node, detail string) bool {
	if declarator == nil {
		return false
	}
	var target *gotreesitter.Node
	for _, field := range []string{"name", "declarator", "pattern"} {
		if target = bound.ChildByField(declarator, field); target != nil {
			break
		}
	}
	if target == nil {
		target = firstDirectBindingNode(bound, declarator)
	}
	bindings := bindingNames(bound, target)
	for _, binding := range bindings {
		collector.add(binding.name, "local_var", detail, binding.line)
	}
	return len(bindings) > 0
}

func collectEnclosingStmtDecls(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	if node == nil {
		return
	}
	switch bound.NodeType(node) {
	case "for_statement":
		collectGoForDecls(collector, bound, node)
		collectHeaderDeclarations(collector, bound, node)
	case "enhanced_for_statement":
		addFieldBinding(collector, bound, node, "name", "local_var", nodeFieldText(bound, node, "type"))
	case "for_range_loop":
		addFieldBinding(collector, bound, node, "declarator", "local_var", nodeFieldText(bound, node, "type"))
	case "for_expression":
		addFieldBinding(collector, bound, node, "pattern", "local_var", "")
	case "catch_clause":
		collectCatchBinding(collector, bound, node)
	case "if_statement", "while_statement", "switch_statement":
		collectHeaderDeclarations(collector, bound, node)
	case "if_expression", "while_expression", "match_expression":
		collectHeaderDeclarations(collector, bound, node)
	}
}

func collectHeaderDeclarations(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() || isBlockNode(bound.NodeType(child)) {
			continue
		}
		collectHeaderNode(collector, bound, child)
	}
}

func collectHeaderNode(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node) {
	if node == nil || isBlockNode(bound.NodeType(node)) {
		return
	}
	switch bound.NodeType(node) {
	case "short_var_declaration":
		collectShortVarDecl(collector, bound, node)
		return
	case "range_clause":
		collectRangeClauseDecls(collector, bound, node)
		return
	case "let_declaration":
		collectRustLetDecl(collector, bound, node)
		return
	case "local_variable_declaration", "declaration":
		collectLocalDeclaration(collector, bound, node)
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.IsNamed() {
			collectHeaderNode(collector, bound, child)
		}
	}
}

func collectCatchBinding(collector *symbolCollector, bound *gotreesitter.BoundTree, catch *gotreesitter.Node) {
	for i := 0; i < catch.ChildCount(); i++ {
		child := catch.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		switch bound.NodeType(child) {
		case "catch_formal_parameter", "formal_parameter", "parameter_declaration":
			collectParameterNode(collector, bound, child, "local_var")
			return
		}
	}
}

func addFieldBinding(collector *symbolCollector, bound *gotreesitter.BoundTree, node *gotreesitter.Node, field, kind, detail string) {
	target := bound.ChildByField(node, field)
	if target == nil {
		target = firstDirectBindingNode(bound, node)
	}
	for _, binding := range bindingNames(bound, target) {
		collector.add(binding.name, kind, detail, binding.line)
	}
}

func firstDirectBindingNode(bound *gotreesitter.BoundTree, node *gotreesitter.Node) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	typeNode := bound.ChildByField(node, "type")
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() || child == typeNode {
			continue
		}
		if isBindingShape(bound.NodeType(child)) {
			return child
		}
	}
	return nil
}

func bindingNames(bound *gotreesitter.BoundTree, node *gotreesitter.Node) []lexicalBinding {
	if node == nil {
		return nil
	}
	var out []lexicalBinding
	seen := make(map[string]struct{})
	var visit func(*gotreesitter.Node)
	visit = func(current *gotreesitter.Node) {
		if current == nil || !current.IsNamed() {
			return
		}
		nodeType := bound.NodeType(current)
		switch nodeType {
		case "identifier", "field_identifier", "shorthand_field_identifier_pattern",
			"shorthand_property_identifier_pattern":
			name := strings.TrimSpace(bound.NodeText(current))
			if name == "" || name == "_" {
				return
			}
			if _, ok := seen[name]; ok {
				return
			}
			seen[name] = struct{}{}
			out = append(out, lexicalBinding{name: name, line: int(current.StartPoint().Row) + 1})
			return
		}
		if isTypeOnlyNode(nodeType) {
			return
		}
		for i := 0; i < current.ChildCount(); i++ {
			visit(current.Child(i))
		}
	}
	visit(node)
	return out
}

func isBindingShape(nodeType string) bool {
	if nodeType == "identifier" || nodeType == "field_identifier" {
		return true
	}
	return strings.Contains(nodeType, "declarator") || strings.Contains(nodeType, "pattern")
}

func isTypeOnlyNode(nodeType string) bool {
	if strings.Contains(nodeType, "type") && !strings.Contains(nodeType, "pattern") {
		return true
	}
	switch nodeType {
	case "namespace_identifier", "primitive_type", "integral_type", "floating_point_type":
		return true
	}
	return false
}

func nodeFieldText(bound *gotreesitter.BoundTree, node *gotreesitter.Node, field string) string {
	child := bound.ChildByField(node, field)
	if child == nil {
		return ""
	}
	return strings.TrimSpace(bound.NodeText(child))
}
