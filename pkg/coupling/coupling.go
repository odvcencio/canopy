// Package coupling computes package-level structural health metrics from a code index and cross-reference graph.
package coupling

import (
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// PackageMetrics holds computed coupling and cohesion metrics for a single package.
type PackageMetrics struct {
	Package      string  `json:"package"`
	Files        int     `json:"files"`
	Symbols      int     `json:"symbols"`
	Ca           int     `json:"ca"`
	Ce           int     `json:"ce"`
	Instability  float64 `json:"instability"`
	Abstractness float64 `json:"abstractness"`
	Distance     float64 `json:"distance"`
	LCOM         int     `json:"lcom"`
}

// Summary holds aggregate statistics across all analyzed packages.
type Summary struct {
	Count          int     `json:"count"`
	AvgInstability float64 `json:"avg_instability"`
	MaxInstability float64 `json:"max_instability"`
	AvgDistance     float64 `json:"avg_distance"`
	MaxDistance     float64 `json:"max_distance"`
	AvgLCOM        float64 `json:"avg_lcom"`
	MaxLCOM        int     `json:"max_lcom"`
}

// Report contains the full coupling analysis result.
type Report struct {
	Packages []PackageMetrics `json:"packages"`
	Summary  Summary          `json:"summary"`
}

// Analyze computes package-level coupling, abstractness, and cohesion metrics.
func Analyze(idx *model.Index, graph xref.Graph) (*Report, error) {
	if idx == nil {
		return &Report{}, nil
	}

	// Collect per-package info from the index.
	type pkgInfo struct {
		files   map[string]bool
		symbols int
		// For abstractness: count type_definitions and interfaces.
		typeDefs   int
		interfaces int
	}

	pkgs := map[string]*pkgInfo{}

	for _, file := range idx.Files {
		pkg := packageFromPath(file.Path)
		info, ok := pkgs[pkg]
		if !ok {
			info = &pkgInfo{files: map[string]bool{}}
			pkgs[pkg] = info
		}
		info.files[file.Path] = true
		info.symbols += len(file.Symbols)

		for _, sym := range file.Symbols {
			if strings.Contains(sym.Kind, "type_definition") {
				info.typeDefs++
			}
			if strings.Contains(sym.Kind, "interface") {
				info.interfaces++
			}
		}
	}

	if len(pkgs) == 0 {
		return &Report{}, nil
	}

	// Build a mapping from definition index to package for the xref graph.
	defPkg := make([]string, len(graph.Definitions))
	for i, def := range graph.Definitions {
		defPkg[i] = def.Package
	}

	// Compute Ca and Ce from cross-package edges.
	// Ca(p) = number of distinct other packages that call into p.
	// Ce(p) = number of distinct other packages that p calls out to.
	afferent := map[string]map[string]bool{} // pkg -> set of other packages calling in
	efferent := map[string]map[string]bool{} // pkg -> set of other packages called out to

	for _, edge := range graph.Edges {
		callerPkg := defPkg[edge.CallerIdx]
		calleePkg := defPkg[edge.CalleeIdx]
		if callerPkg == calleePkg {
			continue // intra-package, skip
		}

		// Callee package gains an afferent coupling from caller's package.
		if afferent[calleePkg] == nil {
			afferent[calleePkg] = map[string]bool{}
		}
		afferent[calleePkg][callerPkg] = true

		// Caller package gains an efferent coupling to callee's package.
		if efferent[callerPkg] == nil {
			efferent[callerPkg] = map[string]bool{}
		}
		efferent[callerPkg][calleePkg] = true
	}

	// Compute LCOM-4 per package.
	// For each package, build an undirected graph of intra-package callable definitions
	// connected by intra-package call edges, then count connected components.
	lcom := computeLCOM4(graph)

	// Assemble PackageMetrics.
	metrics := make([]PackageMetrics, 0, len(pkgs))
	for pkg, info := range pkgs {
		ca := len(afferent[pkg])
		ce := len(efferent[pkg])

		var instability float64
		if ca+ce > 0 {
			instability = float64(ce) / float64(ca+ce)
		}

		var abstractness float64
		if info.typeDefs > 0 {
			abstractness = float64(info.interfaces) / float64(info.typeDefs)
		}

		distance := math.Abs(abstractness + instability - 1.0)

		m := PackageMetrics{
			Package:      pkg,
			Files:        len(info.files),
			Symbols:      info.symbols,
			Ca:           ca,
			Ce:           ce,
			Instability:  instability,
			Abstractness: abstractness,
			Distance:     distance,
			LCOM:         lcom[pkg],
		}
		metrics = append(metrics, m)
	}

	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].Package < metrics[j].Package
	})

	summary := computeSummary(metrics)

	return &Report{
		Packages: metrics,
		Summary:  summary,
	}, nil
}

// computeLCOM4 computes LCOM-4 for each package: the number of connected components
// in an undirected graph of intra-package callable definitions connected by intra-package call edges.
func computeLCOM4(graph xref.Graph) map[string]int {
	// Group callable definitions by package.
	type callableEntry struct {
		idx int // index into graph.Definitions
		pkg string
	}

	pkgCallables := map[string][]int{} // pkg -> list of definition indices
	for i, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		pkgCallables[def.Package] = append(pkgCallables[def.Package], i)
	}

	result := map[string]int{}

	for pkg, callables := range pkgCallables {
		if len(callables) == 0 {
			result[pkg] = 0
			continue
		}

		// Map definition index to local node index.
		defToNode := map[int]int{}
		for i, defIdx := range callables {
			defToNode[defIdx] = i
		}

		// Union-Find for connected components.
		n := len(callables)
		parent := make([]int, n)
		rank := make([]int, n)
		for i := range parent {
			parent[i] = i
		}

		var find func(int) int
		find = func(x int) int {
			if parent[x] != x {
				parent[x] = find(parent[x])
			}
			return parent[x]
		}

		union := func(a, b int) {
			ra, rb := find(a), find(b)
			if ra == rb {
				return
			}
			if rank[ra] < rank[rb] {
				ra, rb = rb, ra
			}
			parent[rb] = ra
			if rank[ra] == rank[rb] {
				rank[ra]++
			}
		}

		// Connect callable nodes via intra-package edges.
		for _, edge := range graph.Edges {
			callerPkg := graph.Definitions[edge.CallerIdx].Package
			calleePkg := graph.Definitions[edge.CalleeIdx].Package
			if callerPkg != pkg || calleePkg != pkg {
				continue
			}
			callerNode, ok1 := defToNode[edge.CallerIdx]
			calleeNode, ok2 := defToNode[edge.CalleeIdx]
			if ok1 && ok2 {
				union(callerNode, calleeNode)
			}
		}

		// Count connected components.
		components := map[int]bool{}
		for i := 0; i < n; i++ {
			components[find(i)] = true
		}
		result[pkg] = len(components)
	}

	return result
}

// computeSummary calculates aggregate statistics for the package metrics.
func computeSummary(metrics []PackageMetrics) Summary {
	n := len(metrics)
	if n == 0 {
		return Summary{}
	}

	var sumInstability, sumDistance, sumLCOM float64
	var maxInstability, maxDistance float64
	var maxLCOM int

	for _, m := range metrics {
		sumInstability += m.Instability
		sumDistance += m.Distance
		sumLCOM += float64(m.LCOM)

		if m.Instability > maxInstability {
			maxInstability = m.Instability
		}
		if m.Distance > maxDistance {
			maxDistance = m.Distance
		}
		if m.LCOM > maxLCOM {
			maxLCOM = m.LCOM
		}
	}

	return Summary{
		Count:          n,
		AvgInstability: sumInstability / float64(n),
		MaxInstability: maxInstability,
		AvgDistance:     sumDistance / float64(n),
		MaxDistance:     maxDistance,
		AvgLCOM:        sumLCOM / float64(n),
		MaxLCOM:        maxLCOM,
	}
}

// packageFromPath extracts the package (directory) from a file path.
func packageFromPath(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	dir := filepath.ToSlash(filepath.Dir(cleaned))
	if dir == "." || dir == "/" {
		return "."
	}
	return dir
}
