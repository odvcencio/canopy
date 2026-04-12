// Package reachability answers whether a package transitively reaches
// sensitive capabilities (process execution, network access, file I/O, etc.)
// by walking the cross-reference call graph forward from package roots.
package reachability

import (
	"fmt"
	"strings"

	"github.com/odvcencio/canopy/pkg/capa"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// Path is one hop in the call chain from a package root to a capability.
type Path struct {
	Package  string `json:"package"`
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// Finding records a single capability reachable from the target package.
type Finding struct {
	Capability string `json:"capability"`
	AttackID   string `json:"attack_id,omitempty"`
	Category   string `json:"category"`
	ReachPath  []Path `json:"reach_path"`
}

// Result is the output of a reachability analysis for one package.
type Result struct {
	Package  string    `json:"package"`
	Findings []Finding `json:"findings"`
}

// Options controls filtering and traversal limits.
type Options struct {
	Category string // filter findings to a single capability category
	AttackID string // filter findings to a single MITRE ATT&CK ID
	Depth    int    // max BFS depth (default 10)
}

// capabilityAPI maps a callee name to the capa rule that declares it sensitive.
type capabilityAPI struct {
	api  string
	rule capa.Rule
}

// Analyze walks the xref call graph forward from every callable in pkg,
// returning findings for each reachable capability API.
func Analyze(idx *model.Index, pkg string, opts Options) (*Result, error) {
	if idx == nil {
		return nil, fmt.Errorf("index is nil")
	}
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return nil, fmt.Errorf("package must not be empty")
	}

	depth := opts.Depth
	if depth <= 0 {
		depth = 10
	}

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, fmt.Errorf("build xref graph: %w", err)
	}

	// Build lookup: API name -> rule(s) that declare it sensitive.
	rules := capa.BuiltinRules()
	rules = append(rules, supplyChainRules()...)
	apiLookup := buildAPILookup(rules)

	// Find root definitions: all callables in the target package.
	roots := packageRoots(&graph, pkg)
	if len(roots) == 0 {
		return &Result{Package: pkg}, nil
	}

	// BFS forward from every root.
	result := &Result{Package: pkg}
	seen := map[string]bool{} // deduplicate findings by "rootID -> apiName"

	for _, root := range roots {
		bfsFindings := bfsForward(&graph, root, apiLookup, depth)
		for _, f := range bfsFindings {
			key := root.ID + "\x00" + f.Capability
			if seen[key] {
				continue
			}
			seen[key] = true

			// Apply filters.
			if opts.Category != "" && !strings.EqualFold(f.Category, opts.Category) {
				continue
			}
			if opts.AttackID != "" && !strings.EqualFold(f.AttackID, opts.AttackID) {
				continue
			}

			result.Findings = append(result.Findings, f)
		}
	}

	return result, nil
}

// packageRoots returns all callable definitions whose Package or File path
// matches the given package prefix.
func packageRoots(g *xref.Graph, pkg string) []xref.Definition {
	var roots []xref.Definition
	normalized := strings.TrimSuffix(pkg, "/")
	for _, def := range g.Definitions {
		if !def.Callable {
			continue
		}
		if def.Package == normalized || def.Package == pkg {
			roots = append(roots, def)
			continue
		}
		// Also match by file path prefix for relative paths.
		if strings.HasPrefix(def.File, normalized+"/") || strings.HasPrefix(def.File, pkg+"/") {
			roots = append(roots, def)
		}
	}
	return roots
}

// buildAPILookup indexes every API name from capa rules for O(1) lookup.
func buildAPILookup(rules []capa.Rule) map[string][]capabilityAPI {
	lookup := make(map[string][]capabilityAPI)
	for _, rule := range rules {
		for _, api := range rule.AnyAPIs {
			lookup[api] = append(lookup[api], capabilityAPI{api: api, rule: rule})
		}
		for _, api := range rule.AllAPIs {
			lookup[api] = append(lookup[api], capabilityAPI{api: api, rule: rule})
		}
	}
	return lookup
}

// bfsItem is a BFS queue entry that tracks the path from root to the current node.
type bfsItem struct {
	defID string
	path  []Path
}

// bfsForward walks the graph forward from root up to maxDepth and returns
// findings for every capability API reached.
func bfsForward(g *xref.Graph, root xref.Definition, apiLookup map[string][]capabilityAPI, maxDepth int) []Finding {
	visited := map[string]bool{root.ID: true}
	queue := []bfsItem{{
		defID: root.ID,
		path: []Path{{
			Package:  root.Package,
			Function: root.Name,
			File:     root.File,
			Line:     root.StartLine,
		}},
	}}

	var findings []Finding

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if len(current.path) > maxDepth {
			continue
		}

		for _, edge := range g.OutgoingEdges(current.defID) {
			callee := g.EdgeCallee(edge)
			if callee == nil {
				continue
			}

			nextPath := make([]Path, len(current.path), len(current.path)+1)
			copy(nextPath, current.path)
			nextPath = append(nextPath, Path{
				Package:  callee.Package,
				Function: callee.Name,
				File:     callee.File,
				Line:     callee.StartLine,
			})

			// Check if callee is a capability API.
			if caps, ok := apiLookup[callee.Name]; ok {
				for _, cap := range caps {
					findings = append(findings, Finding{
						Capability: cap.rule.Name,
						AttackID:   cap.rule.AttackID,
						Category:   cap.rule.Category,
						ReachPath:  nextPath,
					})
				}
			}

			if !visited[callee.ID] {
				visited[callee.ID] = true
				queue = append(queue, bfsItem{
					defID: callee.ID,
					path:  nextPath,
				})
			}
		}
	}

	return findings
}

// supplyChainRules returns additional rules focused on supply-chain relevant
// APIs across Go, Python, JavaScript, and system-level calls.
func supplyChainRules() []capa.Rule {
	return []capa.Rule{
		{
			Name: "Process Execution", Description: "Spawns external processes",
			AttackID: "T1059", Category: "process_execution",
			AnyAPIs: []string{
				"Command", "CommandContext", "Run", "Start", "Output", "CombinedOutput",
				"StartProcess", "Exec", "Popen", "call", "check_output", "check_call",
				"execFile", "spawn", "fork",
			},
			Confidence: "high",
		},
		{
			Name: "Network Access", Description: "Makes outbound network requests",
			AttackID: "T1071", Category: "network_access",
			AnyAPIs: []string{
				"Get", "Post", "PostForm", "Do", "Head", "NewRequest",
				"Dial", "DialContext", "DialTLS", "ListenAndServe", "ListenAndServeTLS",
				"Fetch", "request", "urlopen", "urlretrieve",
			},
			Confidence: "high",
		},
		{
			Name: "File System Access", Description: "Reads, writes, or modifies files",
			AttackID: "T1005", Category: "file_access",
			AnyAPIs: []string{
				"Open", "Create", "OpenFile", "ReadFile", "WriteFile",
				"Remove", "RemoveAll", "Rename", "Mkdir", "MkdirAll",
				"Chmod", "Chown", "Symlink", "Link",
				"readFile", "writeFile", "readFileSync", "writeFileSync",
				"unlink", "unlinkSync",
			},
			Confidence: "medium",
		},
		{
			Name: "Environment Access", Description: "Reads or modifies environment variables",
			AttackID: "T1082", Category: "environment_access",
			AnyAPIs: []string{
				"Getenv", "LookupEnv", "Setenv", "Unsetenv", "Environ",
				"getenv", "setenv", "putenv",
			},
			Confidence: "low",
		},
		{
			Name: "Code Evaluation", Description: "Evaluates code at runtime",
			AttackID: "T1059", Category: "code_eval",
			AnyAPIs: []string{
				"eval", "Eval", "exec", "compile",
				"RunString", "Interpret",
			},
			Confidence: "high",
		},
		{
			Name: "Unsafe Memory", Description: "Uses unsafe pointer or memory operations",
			AttackID: "T1055", Category: "unsafe_memory",
			AnyAPIs: []string{
				"Pointer", "NewPointer", "Offsetof", "Sizeof",
				"Slice", "SliceData", "StringData",
			},
			Confidence: "medium",
		},
	}
}
