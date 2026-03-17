// Package deps analyzes import dependency graphs at the package or file level from a structural index.
package deps

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gts-suite/pkg/model"
)

type Options struct {
	Mode         string
	Top          int
	Focus        string
	Depth        int
	Reverse      bool
	IncludeEdges bool
}

type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Internal bool   `json:"internal"`
}

type NodeMetric struct {
	Node      string `json:"node"`
	Outgoing  int    `json:"outgoing"`
	Incoming  int    `json:"incoming"`
	IsProject bool   `json:"is_project"`
}

type Report struct {
	Root              string       `json:"root"`
	Mode              string       `json:"mode"`
	Module            string       `json:"module,omitempty"`
	NodeCount         int          `json:"node_count"`
	EdgeCount         int          `json:"edge_count"`
	InternalEdgeCount int          `json:"internal_edge_count"`
	ExternalEdgeCount int          `json:"external_edge_count"`
	TopOutgoing       []NodeMetric `json:"top_outgoing,omitempty"`
	TopIncoming       []NodeMetric `json:"top_incoming,omitempty"`
	Focus             string       `json:"focus,omitempty"`
	FocusDirection    string       `json:"focus_direction,omitempty"`
	FocusDepth        int          `json:"focus_depth,omitempty"`
	FocusOutgoing     []string     `json:"focus_outgoing,omitempty"`
	FocusIncoming     []string     `json:"focus_incoming,omitempty"`
	FocusWalk         []string     `json:"focus_walk,omitempty"`
	Edges             []Edge       `json:"edges,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}

	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = "package"
	}
	if mode != "package" && mode != "file" {
		return Report{}, fmt.Errorf("unsupported mode %q (expected package or file)", opts.Mode)
	}
	if opts.Top <= 0 {
		opts.Top = 10
	}
	if opts.Depth <= 0 {
		opts.Depth = 1
	}

	modulePath := modulePathFromRoot(idx.Root)
	projectNodes := collectProjectNodes(idx, mode)

	edgeSet := map[string]Edge{}
	for _, file := range idx.Files {
		from := fromNode(file.Path, mode)
		importSeen := map[string]bool{}
		for _, imp := range file.Imports {
			imp = strings.TrimSpace(imp)
			if imp == "" || importSeen[imp] {
				continue
			}
			importSeen[imp] = true

			to, internal := mapImportTarget(imp, mode, modulePath)
			edgeKey := from + "->" + to
			edgeSet[edgeKey] = Edge{
				From:     from,
				To:       to,
				Internal: internal,
			}
		}
	}

	edges := make([]Edge, 0, len(edgeSet))
	for _, edge := range edgeSet {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})

	outgoing := map[string]int{}
	incoming := map[string]int{}
	nodes := map[string]bool{}
	internalEdges := 0
	for _, edge := range edges {
		nodes[edge.From] = true
		nodes[edge.To] = true
		outgoing[edge.From]++
		incoming[edge.To]++
		if edge.Internal {
			internalEdges++
		}
	}

	outgoingList := make([]NodeMetric, 0, len(outgoing))
	for node, count := range outgoing {
		outgoingList = append(outgoingList, NodeMetric{
			Node:      node,
			Outgoing:  count,
			Incoming:  incoming[node],
			IsProject: projectNodes[node],
		})
	}
	sortNodeMetrics(outgoingList, func(item NodeMetric) int { return item.Outgoing })
	if opts.Top < len(outgoingList) {
		outgoingList = outgoingList[:opts.Top]
	}

	incomingList := make([]NodeMetric, 0, len(incoming))
	for node, count := range incoming {
		incomingList = append(incomingList, NodeMetric{
			Node:      node,
			Outgoing:  outgoing[node],
			Incoming:  count,
			IsProject: projectNodes[node],
		})
	}
	sortNodeMetrics(incomingList, func(item NodeMetric) int { return item.Incoming })
	if opts.Top < len(incomingList) {
		incomingList = incomingList[:opts.Top]
	}

	report := Report{
		Root:              idx.Root,
		Mode:              mode,
		Module:            modulePath,
		NodeCount:         len(nodes),
		EdgeCount:         len(edges),
		InternalEdgeCount: internalEdges,
		ExternalEdgeCount: len(edges) - internalEdges,
		TopOutgoing:       outgoingList,
		TopIncoming:       incomingList,
	}

	if focus := normalizeFocus(opts.Focus, mode, idx.Root); focus != "" {
		report.Focus = focus
		if opts.Reverse {
			report.FocusDirection = "reverse"
		} else {
			report.FocusDirection = "forward"
		}
		report.FocusDepth = opts.Depth
		out := make([]string, 0, 8)
		in := make([]string, 0, 8)
		for _, edge := range edges {
			if edge.From == focus {
				out = append(out, edge.To)
			}
			if edge.To == focus {
				in = append(in, edge.From)
			}
		}
		sort.Strings(out)
		sort.Strings(in)
		report.FocusOutgoing = dedupeSorted(out)
		report.FocusIncoming = dedupeSorted(in)

		report.FocusWalk = walkFromFocus(edges, focus, opts.Depth, opts.Reverse)
	}

	if opts.IncludeEdges {
		report.Edges = edges
	}
	return report, nil
}

func walkFromFocus(edges []Edge, start string, depth int, reverse bool) []string {
	if strings.TrimSpace(start) == "" || depth <= 0 {
		return nil
	}

	adjacency := map[string][]string{}
	for _, edge := range edges {
		from := edge.From
		to := edge.To
		if reverse {
			from, to = to, from
		}
		adjacency[from] = append(adjacency[from], to)
	}
	for key := range adjacency {
		sort.Strings(adjacency[key])
		adjacency[key] = dedupeSorted(adjacency[key])
	}

	type levelNode struct {
		name  string
		depth int
	}
	queue := []levelNode{{name: start, depth: 0}}
	visited := map[string]bool{start: true}
	out := make([]string, 0, 16)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= depth {
			continue
		}

		for _, next := range adjacency[current.name] {
			if visited[next] {
				continue
			}
			visited[next] = true
			out = append(out, next)
			queue = append(queue, levelNode{name: next, depth: current.depth + 1})
		}
	}
	return out
}

func sortNodeMetrics(items []NodeMetric, metric func(NodeMetric) int) {
	sort.Slice(items, func(i, j int) bool {
		left := metric(items[i])
		right := metric(items[j])
		if left == right {
			return items[i].Node < items[j].Node
		}
		return left > right
	})
}

func collectProjectNodes(idx *model.Index, mode string) map[string]bool {
	nodes := map[string]bool{}
	for _, file := range idx.Files {
		nodes[fromNode(file.Path, mode)] = true
	}
	return nodes
}

func fromNode(filePath, mode string) string {
	cleaned := filepath.ToSlash(filepath.Clean(filePath))
	if mode == "file" {
		return cleaned
	}
	dir := filepath.ToSlash(filepath.Dir(cleaned))
	if dir == "." {
		return "."
	}
	return dir
}

func mapImportTarget(importPath, mode, modulePath string) (string, bool) {
	if mode == "file" {
		internal := isInternalImport(importPath, modulePath)
		return importPath, internal
	}

	if isInternalImport(importPath, modulePath) {
		trimmed := strings.TrimPrefix(importPath, modulePath)
		trimmed = strings.TrimPrefix(trimmed, "/")
		if strings.TrimSpace(trimmed) == "" {
			return ".", true
		}
		return filepath.ToSlash(filepath.Clean(trimmed)), true
	}
	return importPath, false
}

func isInternalImport(importPath, modulePath string) bool {
	if modulePath == "" {
		return false
	}
	return importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")
}

func normalizeFocus(raw, mode, root string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if filepath.IsAbs(text) {
		if rel, err := filepath.Rel(root, text); err == nil {
			text = rel
		}
	}
	text = filepath.ToSlash(filepath.Clean(text))
	if mode == "package" && strings.HasSuffix(text, ".go") {
		text = filepath.ToSlash(filepath.Dir(text))
	}
	if text == "" {
		return "."
	}
	return text
}

func dedupeSorted(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	last := ""
	for i, item := range items {
		if i == 0 || item != last {
			out = append(out, item)
			last = item
		}
	}
	return out
}

func modulePathFromRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	dir, err := filepath.Abs(root)
	if err != nil {
		dir = root
	}
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if module := parseModuleLine(goModPath); module != "" {
			return module
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func parseModuleLine(goModPath string) string {
	file, err := os.Open(goModPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		module := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		module = strings.Trim(module, `"`)
		return module
	}
	return ""
}
