// Package typemetrics computes per-type structural metrics: field count, interface width, method set size, and nesting depth.
package typemetrics

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// TypeMetrics holds all computed structural metrics for a single type.
type TypeMetrics struct {
	File           string `json:"file"`
	Name           string `json:"name"`
	Kind           string `json:"kind"` // "struct", "interface", "class", etc.
	Language       string `json:"language"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	Fields         int    `json:"fields"`
	InterfaceWidth int    `json:"interface_width"`
	MethodSetSize  int    `json:"method_set_size"`
	NestingDepth   int    `json:"nesting_depth"`
}

// TypeSummary holds aggregate statistics across all analyzed types.
type TypeSummary struct {
	Count             int     `json:"count"`
	AvgFields         float64 `json:"avg_fields"`
	MaxFields         int     `json:"max_fields"`
	AvgInterfaceWidth float64 `json:"avg_interface_width"`
	MaxInterfaceWidth int     `json:"max_interface_width"`
	AvgMethodSet      float64 `json:"avg_method_set"`
	MaxMethodSet      int     `json:"max_method_set"`
}

// Report contains the full type metrics analysis result.
type Report struct {
	Types   []TypeMetrics `json:"types"`
	Summary TypeSummary   `json:"summary"`
}

// Analyze computes structural metrics for every type definition in the index.
func Analyze(idx *model.Index, root string, graph xref.Graph) (*Report, error) {
	if idx == nil {
		return &Report{}, nil
	}

	// Build a lookup: type name -> count of methods with that receiver.
	methodSetLookup := buildMethodSetLookup(graph)

	types := make([]TypeMetrics, 0, 64)

	// Cache one parser per language to avoid repeated expensive NewParser calls.
	parserCache := map[*gotreesitter.Language]*gotreesitter.Parser{}

	for _, file := range idx.Files {
		entry := grammars.DetectLanguage(file.Path)
		if entry == nil {
			continue
		}

		// Skip files with no type definitions before touching disk.
		hasType := false
		for _, sym := range file.Symbols {
			if isTypeSymbol(sym.Kind) {
				hasType = true
				break
			}
		}
		if !hasType {
			continue
		}

		absPath := file.Path
		if !filepath.IsAbs(absPath) && root != "" {
			absPath = filepath.Join(root, absPath)
		}
		source, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		lang := entry.Language()
		parser, ok := parserCache[lang]
		if !ok {
			parser = gotreesitter.NewParser(lang)
			parserCache[lang] = parser
		}

		for _, sym := range file.Symbols {
			if !isTypeSymbol(sym.Kind) {
				continue
			}

			body := extractBody(source, sym.StartLine, sym.EndLine)
			if len(body) == 0 {
				continue
			}

			var tree *gotreesitter.Tree
			var parseErr error
			if entry.TokenSourceFactory != nil {
				ts := entry.TokenSourceFactory(body, lang)
				tree, parseErr = parser.ParseWithTokenSource(body, ts)
			} else {
				tree, parseErr = parser.Parse(body)
			}
			if parseErr != nil || tree == nil {
				continue
			}

			rootNode := tree.RootNode()
			if rootNode == nil {
				tree.Release()
				continue
			}

			kind := detectTypeKind(sym.Signature, body)
			fields := countFields(rootNode, lang)
			ifaceWidth := 0
			if kind == "interface" {
				ifaceWidth = countInterfaceMethods(rootNode, lang)
			}
			nestingDepth := computeNestingDepth(rootNode, lang)
			tree.Release()

			methodSet := methodSetLookup[sym.Name]

			metrics := TypeMetrics{
				File:           file.Path,
				Name:           sym.Name,
				Kind:           kind,
				Language:       entry.Name,
				StartLine:      sym.StartLine,
				EndLine:        sym.EndLine,
				Fields:         fields,
				InterfaceWidth: ifaceWidth,
				MethodSetSize:  methodSet,
				NestingDepth:   nestingDepth,
			}
			types = append(types, metrics)
		}
	}

	summary := computeSummary(types)

	return &Report{
		Types:   types,
		Summary: summary,
	}, nil
}

// isTypeSymbol returns true for type definition symbol kinds.
func isTypeSymbol(kind string) bool {
	return strings.Contains(kind, "type_definition")
}

// detectTypeKind determines whether the type is a struct, interface, or generic type.
func detectTypeKind(signature string, body []byte) string {
	sig := strings.ToLower(signature)
	if strings.Contains(sig, "interface") {
		return "interface"
	}
	if strings.Contains(sig, "struct") {
		return "struct"
	}

	bodyStr := strings.ToLower(string(body))
	if strings.Contains(bodyStr, "interface") {
		return "interface"
	}
	if strings.Contains(bodyStr, "struct") {
		return "struct"
	}

	return "type"
}

// countFields counts field-like child nodes in the parsed AST.
func countFields(root *gotreesitter.Node, lang *gotreesitter.Language) int {
	count := 0
	var walk func(node *gotreesitter.Node, depth int)
	walk = func(node *gotreesitter.Node, depth int) {
		if node == nil {
			return
		}
		nodeType := node.Type(lang)
		if isFieldNode(nodeType) {
			count++
			return // Don't recurse into field children.
		}
		for _, child := range node.Children() {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return count
}

// isFieldNode returns true for AST node types representing struct/class fields.
func isFieldNode(nodeType string) bool {
	switch nodeType {
	case "field_declaration",
		"field_definition",
		"field",
		"property_declaration",
		"member_declaration":
		return true
	default:
		return false
	}
}

// countInterfaceMethods counts method signatures inside an interface body.
func countInterfaceMethods(root *gotreesitter.Node, lang *gotreesitter.Language) int {
	count := 0
	var walk func(node *gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		nodeType := node.Type(lang)
		if isInterfaceMethodNode(nodeType) {
			count++
			return
		}
		for _, child := range node.Children() {
			walk(child)
		}
	}
	walk(root)
	return count
}

// isInterfaceMethodNode returns true for AST node types representing interface method specs.
func isInterfaceMethodNode(nodeType string) bool {
	switch nodeType {
	case "method_spec",
		"method_elem",
		"method_signature",
		"abstract_method_declaration":
		return true
	default:
		return false
	}
}

// computeNestingDepth walks the AST and tracks the maximum depth of nested type nodes.
func computeNestingDepth(root *gotreesitter.Node, lang *gotreesitter.Language) int {
	maxDepth := 0
	var walk func(node *gotreesitter.Node, depth int)
	walk = func(node *gotreesitter.Node, depth int) {
		if node == nil {
			return
		}
		nodeType := node.Type(lang)
		if isNestedTypeNode(nodeType) {
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		for _, child := range node.Children() {
			walk(child, depth)
		}
	}
	// Start at -1 because the root type itself will match and increment to 0,
	// but we only want to count nested types.
	walk(root, -1)
	// If we never found a type node inside (maxDepth stayed 0 or went negative), return 0.
	if maxDepth < 0 {
		maxDepth = 0
	}
	return maxDepth
}

// isNestedTypeNode returns true for AST node types that indicate type nesting.
func isNestedTypeNode(nodeType string) bool {
	switch nodeType {
	case "type_declaration",
		"type_spec",
		"struct_type",
		"interface_type",
		"class_declaration",
		"class_definition",
		"enum_declaration":
		return true
	default:
		return false
	}
}

// buildMethodSetLookup builds a map from type name to count of methods with
// that type as a receiver, using the xref graph definitions.
func buildMethodSetLookup(graph xref.Graph) map[string]int {
	lookup := map[string]int{}
	for _, def := range graph.Definitions {
		if def.Kind != "method_definition" || def.Receiver == "" {
			continue
		}
		// The Receiver field for Go methods is like "s *Server" or "s Server".
		// Extract the type name (the last token, stripping the pointer prefix).
		typeName := extractReceiverType(def.Receiver)
		if typeName != "" {
			lookup[typeName]++
		}
	}
	return lookup
}

// extractReceiverType extracts the type name from a Go receiver expression.
// For "s *Server" returns "Server", for "s Server" returns "Server",
// for just "*Server" returns "Server".
func extractReceiverType(receiver string) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return ""
	}
	// Split on whitespace; the type is the last token.
	parts := strings.Fields(receiver)
	last := parts[len(parts)-1]
	// Strip pointer prefix.
	return strings.TrimLeft(last, "*")
}

// extractBody returns the source bytes between startLine and endLine
// (1-indexed, inclusive).
func extractBody(source []byte, startLine, endLine int) []byte {
	if len(source) == 0 || startLine > endLine {
		return nil
	}
	if startLine < 1 {
		startLine = 1
	}

	line := 1
	start := -1
	for i, b := range source {
		if line == startLine && start == -1 {
			start = i
		}
		if b == '\n' {
			if line == endLine {
				return source[start : i+1]
			}
			line++
		}
	}
	if start >= 0 && line >= startLine && line <= endLine {
		return source[start:]
	}
	return nil
}

// computeSummary calculates aggregate statistics for the type metrics.
func computeSummary(types []TypeMetrics) TypeSummary {
	n := len(types)
	if n == 0 {
		return TypeSummary{}
	}

	var sumFields, sumIfaceWidth, sumMethodSet int
	maxFields, maxIfaceWidth, maxMethodSet := 0, 0, 0

	for _, t := range types {
		sumFields += t.Fields
		sumIfaceWidth += t.InterfaceWidth
		sumMethodSet += t.MethodSetSize

		if t.Fields > maxFields {
			maxFields = t.Fields
		}
		if t.InterfaceWidth > maxIfaceWidth {
			maxIfaceWidth = t.InterfaceWidth
		}
		if t.MethodSetSize > maxMethodSet {
			maxMethodSet = t.MethodSetSize
		}
	}

	return TypeSummary{
		Count:             n,
		AvgFields:         float64(sumFields) / float64(n),
		MaxFields:         maxFields,
		AvgInterfaceWidth: float64(sumIfaceWidth) / float64(n),
		MaxInterfaceWidth: maxIfaceWidth,
		AvgMethodSet:      float64(sumMethodSet) / float64(n),
		MaxMethodSet:      maxMethodSet,
	}
}
