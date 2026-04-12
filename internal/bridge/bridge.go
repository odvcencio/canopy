// Package bridge detects cross-component dependency edges between top-level directory groups in a codebase.
package bridge

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

type Options struct {
	Top     int
	Focus   string
	Depth   int
	Reverse bool
}

type ComponentMetric struct {
	Name            string `json:"name"`
	PackageCount    int    `json:"package_count"`
	FileCount       int    `json:"file_count"`
	InternalImports int    `json:"internal_imports"`
	ExternalImports int    `json:"external_imports"`
}

type BridgeEdge struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Count   int      `json:"count"`
	Samples []string `json:"samples,omitempty"`
}

type ExternalMetric struct {
	Component  string   `json:"component"`
	Count      int      `json:"count"`
	TopImports []string `json:"top_imports,omitempty"`
}

type Report struct {
	Root                string            `json:"root"`
	Module              string            `json:"module,omitempty"`
	PackageCount        int               `json:"package_count"`
	ComponentCount      int               `json:"component_count"`
	BridgeCount         int               `json:"bridge_count"`
	Components          []ComponentMetric `json:"components,omitempty"`
	TopBridges          []BridgeEdge      `json:"top_bridges,omitempty"`
	Focus               string            `json:"focus,omitempty"`
	FocusDirection      string            `json:"focus_direction,omitempty"`
	FocusDepth          int               `json:"focus_depth,omitempty"`
	FocusOutgoing       []string          `json:"focus_outgoing,omitempty"`
	FocusIncoming       []string          `json:"focus_incoming,omitempty"`
	FocusWalk           []string          `json:"focus_walk,omitempty"`
	ExternalByComponent []ExternalMetric  `json:"external_by_component,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}
	if opts.Top <= 0 {
		opts.Top = 20
	}
	if opts.Depth <= 0 {
		opts.Depth = 1
	}

	modulePath := modulePathFromRoot(idx.Root)

	packageSet := map[string]bool{}
	componentPackages := map[string]map[string]bool{}
	componentFiles := map[string]int{}
	componentInternalImports := map[string]int{}
	componentExternalImports := map[string]int{}
	componentExternalImportCounts := map[string]map[string]int{}

	type bridgeBucket struct {
		count   int
		samples map[string]bool
	}
	bridgeBuckets := map[string]*bridgeBucket{}

	for _, file := range idx.Files {
		fromPkg := packageFromFile(file.Path)
		fromComponent := componentForPackage(fromPkg)
		packageSet[fromPkg] = true

		if componentPackages[fromComponent] == nil {
			componentPackages[fromComponent] = map[string]bool{}
		}
		componentPackages[fromComponent][fromPkg] = true
		componentFiles[fromComponent]++

		seenImports := map[string]bool{}
		for _, imp := range file.Imports {
			imp = strings.TrimSpace(imp)
			if imp == "" || seenImports[imp] {
				continue
			}
			seenImports[imp] = true

			if internalPkg, ok := internalImportPackage(imp, modulePath); ok {
				componentInternalImports[fromComponent]++
				toComponent := componentForPackage(internalPkg)
				if toComponent == fromComponent {
					continue
				}

				key := fromComponent + "->" + toComponent
				bucket := bridgeBuckets[key]
				if bucket == nil {
					bucket = &bridgeBucket{
						samples: map[string]bool{},
					}
					bridgeBuckets[key] = bucket
				}
				bucket.count++
				bucket.samples[fromPkg+"->"+internalPkg] = true
				continue
			}

			componentExternalImports[fromComponent]++
			if componentExternalImportCounts[fromComponent] == nil {
				componentExternalImportCounts[fromComponent] = map[string]int{}
			}
			componentExternalImportCounts[fromComponent][imp]++
		}
	}

	components := make([]ComponentMetric, 0, len(componentPackages))
	for component, packages := range componentPackages {
		components = append(components, ComponentMetric{
			Name:            component,
			PackageCount:    len(packages),
			FileCount:       componentFiles[component],
			InternalImports: componentInternalImports[component],
			ExternalImports: componentExternalImports[component],
		})
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].FileCount == components[j].FileCount {
			return components[i].Name < components[j].Name
		}
		return components[i].FileCount > components[j].FileCount
	})

	bridges := make([]BridgeEdge, 0, len(bridgeBuckets))
	for key, bucket := range bridgeBuckets {
		from, to, _ := strings.Cut(key, "->")
		samples := make([]string, 0, len(bucket.samples))
		for sample := range bucket.samples {
			samples = append(samples, sample)
		}
		sort.Strings(samples)
		if len(samples) > 3 {
			samples = samples[:3]
		}
		bridges = append(bridges, BridgeEdge{
			From:    from,
			To:      to,
			Count:   bucket.count,
			Samples: samples,
		})
	}
	sort.Slice(bridges, func(i, j int) bool {
		if bridges[i].Count == bridges[j].Count {
			if bridges[i].From == bridges[j].From {
				return bridges[i].To < bridges[j].To
			}
			return bridges[i].From < bridges[j].From
		}
		return bridges[i].Count > bridges[j].Count
	})
	if opts.Top < len(bridges) {
		bridges = bridges[:opts.Top]
	}

	externalByComponent := make([]ExternalMetric, 0, len(componentExternalImportCounts))
	for component, counts := range componentExternalImportCounts {
		topImports := make([]string, 0, len(counts))
		type importMetric struct {
			path  string
			count int
		}
		metrics := make([]importMetric, 0, len(counts))
		total := 0
		for path, count := range counts {
			total += count
			metrics = append(metrics, importMetric{path: path, count: count})
		}
		sort.Slice(metrics, func(i, j int) bool {
			if metrics[i].count == metrics[j].count {
				return metrics[i].path < metrics[j].path
			}
			return metrics[i].count > metrics[j].count
		})
		limit := 3
		if len(metrics) < limit {
			limit = len(metrics)
		}
		for i := 0; i < limit; i++ {
			topImports = append(topImports, metrics[i].path)
		}

		externalByComponent = append(externalByComponent, ExternalMetric{
			Component:  component,
			Count:      total,
			TopImports: topImports,
		})
	}
	sort.Slice(externalByComponent, func(i, j int) bool {
		if externalByComponent[i].Count == externalByComponent[j].Count {
			return externalByComponent[i].Component < externalByComponent[j].Component
		}
		return externalByComponent[i].Count > externalByComponent[j].Count
	})
	if opts.Top < len(externalByComponent) {
		externalByComponent = externalByComponent[:opts.Top]
	}

	report := Report{
		Root:                idx.Root,
		Module:              modulePath,
		PackageCount:        len(packageSet),
		ComponentCount:      len(components),
		BridgeCount:         len(bridgeBuckets),
		Components:          components,
		TopBridges:          bridges,
		ExternalByComponent: externalByComponent,
	}

	if focusRaw := strings.TrimSpace(opts.Focus); focusRaw != "" {
		focus := componentForPackage(focusRaw)
		report.Focus = focus
		if opts.Reverse {
			report.FocusDirection = "reverse"
		} else {
			report.FocusDirection = "forward"
		}
		report.FocusDepth = opts.Depth

		outgoingSet := map[string]bool{}
		incomingSet := map[string]bool{}
		edgeList := make([]BridgeEdge, 0, len(bridgeBuckets))
		for key, bucket := range bridgeBuckets {
			from, to, _ := strings.Cut(key, "->")
			edgeList = append(edgeList, BridgeEdge{From: from, To: to, Count: bucket.count})
			if from == focus {
				outgoingSet[to] = true
			}
			if to == focus {
				incomingSet[from] = true
			}
		}
		report.FocusOutgoing = sortedSet(outgoingSet)
		report.FocusIncoming = sortedSet(incomingSet)
		report.FocusWalk = walkComponents(edgeList, focus, opts.Depth, opts.Reverse)
	}

	return report, nil
}

func packageFromFile(filePath string) string {
	cleaned := filepath.ToSlash(filepath.Clean(filePath))
	dir := filepath.ToSlash(filepath.Dir(cleaned))
	if dir == "." {
		return "."
	}
	return dir
}

func componentForPackage(pkg string) string {
	pkg = filepath.ToSlash(filepath.Clean(strings.TrimSpace(pkg)))
	if pkg == "." || pkg == "" {
		return "root"
	}

	parts := strings.Split(pkg, "/")
	if len(parts) == 1 {
		return parts[0]
	}

	switch parts[0] {
	case "cmd", "internal", "pkg":
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return parts[0]
	default:
		return parts[0]
	}
}

func internalImportPackage(importPath, modulePath string) (string, bool) {
	if strings.TrimSpace(modulePath) == "" {
		return "", false
	}
	if importPath == modulePath {
		return ".", true
	}
	if !strings.HasPrefix(importPath, modulePath+"/") {
		return "", false
	}

	trimmed := strings.TrimPrefix(importPath, modulePath+"/")
	trimmed = filepath.ToSlash(filepath.Clean(trimmed))
	if trimmed == "" || trimmed == "." {
		return ".", true
	}
	return trimmed, true
}

func walkComponents(edges []BridgeEdge, focus string, depth int, reverse bool) []string {
	if strings.TrimSpace(focus) == "" || depth <= 0 {
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

	type nodeDepth struct {
		node  string
		depth int
	}
	queue := []nodeDepth{{node: focus, depth: 0}}
	visited := map[string]bool{focus: true}
	out := make([]string, 0, 16)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= depth {
			continue
		}

		for _, next := range adjacency[current.node] {
			if visited[next] {
				continue
			}
			visited[next] = true
			out = append(out, next)
			queue = append(queue, nodeDepth{node: next, depth: current.depth + 1})
		}
	}
	return out
}

func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
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
	goModPath := filepath.Join(root, "go.mod")
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
