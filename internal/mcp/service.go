// Package mcp exposes canopy tools as an MCP stdio server for AI agent integration via JSON-RPC.
package mcp

import (
	"fmt"
	"sort"
	"strings"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type Service struct {
	defaultRoot  string
	defaultCache string
	allowWrites  bool
}

type ServiceOptions struct {
	AllowWrites bool
}

func NewService(defaultRoot, defaultCache string) *Service {
	return NewServiceWithOptions(defaultRoot, defaultCache, ServiceOptions{})
}

func NewServiceWithOptions(defaultRoot, defaultCache string, opts ServiceOptions) *Service {
	root := strings.TrimSpace(defaultRoot)
	if root == "" {
		root = "."
	}
	return &Service{
		defaultRoot:  root,
		defaultCache: strings.TrimSpace(defaultCache),
		allowWrites:  opts.AllowWrites,
	}
}

func (s *Service) Tools() []Tool {
	var tools []Tool
	tools = append(tools, searchTools()...)
	tools = append(tools, graphTools()...)
	tools = append(tools, analyzeTools()...)
	tools = append(tools, transformTools()...)
	for i := range tools {
		finalizeToolSchema(&tools[i])
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}

func searchTools() []Tool {
	stringOrArray := []Property{
		{Type: "string"},
		{Type: "array", Items: &Property{Type: "string"}},
	}
	return []Tool{
		{
			Name:        "gts_grep",
			Description: "Run structural selector matches across indexed symbols",
			InputSchema: Schema{
				Properties: map[string]Property{
					"selector":          {Type: "string"},
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"selector"},
			}.ToMap(),
		},
		{
			Name:        "gts_map",
			Description: "Emit table-of-contents structural summaries for indexed files",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_query",
			Description: "Run a raw tree-sitter S-expression query across indexed files",
			InputSchema: Schema{
				Properties: map[string]Property{
					"pattern":           {Type: "string", Description: "tree-sitter query pattern"},
					"path":              {Type: "string", Description: "index root path override"},
					"cache":             {Type: "string", Description: "index cache path override"},
					"capture":           {OneOf: stringOrArray},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"pattern"},
			}.ToMap(),
		},
		{
			Name:        "gts_refs",
			Description: "Find indexed references by symbol name or regex",
			InputSchema: Schema{
				Properties: map[string]Property{
					"name":              {Type: "string"},
					"regex":             {Type: "boolean"},
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"name"},
			}.ToMap(),
		},
		{
			Name:        "gts_context",
			Description: "Pack focused context for a file and line",
			InputSchema: Schema{
				Properties: map[string]Property{
					"file":              {Type: "string"},
					"line":              {Type: "integer"},
					"tokens":            {Type: "integer"},
					"semantic":          {Type: "boolean"},
					"semantic_depth":    {Type: "integer"},
					"root":              {Type: "string"},
					"cache":             {Type: "string"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"file"},
			}.ToMap(),
		},
		{
			Name:        "gts_scope",
			Description: "Resolve symbols in scope for a file and line",
			InputSchema: Schema{
				Properties: map[string]Property{
					"file":              {Type: "string"},
					"line":              {Type: "integer"},
					"root":              {Type: "string"},
					"cache":             {Type: "string"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"file"},
			}.ToMap(),
		},
		{
			Name:        "gts_files",
			Description: "List indexed files with structural density filters",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"language":          {Type: "string"},
					"min_symbols":       {Type: "integer"},
					"sort":              {Type: "string"},
					"top":               {Type: "integer"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_chunk",
			Description: "Split code into AST-boundary chunks for retrieval/indexing",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"tokens":            {Type: "integer"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
	}
}

func graphTools() []Tool {
	return []Tool{
		{
			Name:        "gts_services",
			Description: "Build repo-to-repo dependency graph from federated .canopyindex files",
			InputSchema: Schema{
				Properties: map[string]Property{
					"federation": {Type: "string", Description: "directory containing .canopyindex files (required)"},
				},
				Required: []string{"federation"},
			}.ToMap(),
		},
		{
			Name:        "gts_deps",
			Description: "Analyze dependency graph from structural imports",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"by":                {Type: "string"},
					"top":               {Type: "integer"},
					"focus":             {Type: "string"},
					"depth":             {Type: "integer"},
					"reverse":           {Type: "boolean"},
					"edges":             {Type: "boolean"},
					"cycles_only":       {Type: "boolean", Description: "only return import cycle information"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_callgraph",
			Description: "Traverse resolved call graph from matching callable roots",
			InputSchema: Schema{
				Properties: map[string]Property{
					"name":              {Type: "string"},
					"regex":             {Type: "boolean"},
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"depth":             {Type: "integer"},
					"reverse":           {Type: "boolean"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"name"},
			}.ToMap(),
		},
		{
			Name:        "gts_dead",
			Description: "List callable definitions with zero incoming call references",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":                {Type: "string"},
					"cache":               {Type: "string"},
					"kind":                {Type: "string"},
					"include_entrypoints": {Type: "boolean"},
					"include_tests":       {Type: "boolean"},
					"include_generated":   {Type: "boolean", Description: "include generated files (default: false)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_bridge",
			Description: "Map cross-component dependency bridges",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"top":               {Type: "integer"},
					"focus":             {Type: "string"},
					"depth":             {Type: "integer"},
					"reverse":           {Type: "boolean"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_drift",
			Description: "Compare dependency graph between two git refs",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":  {Type: "string"},
					"cache": {Type: "string"},
					"base":  {Type: "string", Description: "git ref to compare against (default: origin/main)"},
				},
			}.ToMap(),
		},
	}
}

func analyzeTools() []Tool {
	stringOrArray := []Property{
		{Type: "string"},
		{Type: "array", Items: &Property{Type: "string"}},
	}
	return []Tool{
		{
			Name:        "gts_lint",
			Description: "Run structural lint rules and query-pattern rules against index",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"rule":              {OneOf: stringOrArray},
					"pattern":           {OneOf: stringOrArray},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_stats",
			Description: "Report structural codebase metrics from an index",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"top":               {Type: "integer"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_capa",
			Description: "Detect capabilities from structural API/import patterns with MITRE ATT&CK mapping",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"category":          {Type: "string", Description: "filter by category (e.g. crypto, network, process_injection)"},
					"min_confidence":    {Type: "string", Description: "minimum confidence level", Enum: []string{"low", "medium", "high"}},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_similarity",
			Description: "Find similar functions between codebases using exact hash and fuzzy n-gram matching",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path_a":            {Type: "string", Description: "first codebase path"},
					"path_b":            {Type: "string", Description: "second codebase path (omit for self-comparison)"},
					"cache_a":           {Type: "string", Description: "cache path for first index"},
					"cache_b":           {Type: "string", Description: "cache path for second index"},
					"threshold":         {Type: "number", Description: "similarity threshold 0.0-1.0 (default 0.7)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"path_a"},
			}.ToMap(),
		},
		{
			Name:        "gts_types",
			Description: "Type-level structural metrics: struct fields, interface width, method set size, nesting depth",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"sort":              {Type: "string", Description: "sort field: fields, interface_width, method_set, nesting (default: fields)"},
					"top":               {Type: "integer", Description: "limit to top N results (default: all)"},
					"min_fields":        {Type: "integer", Description: "minimum field count to include (default: 0)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":         {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_coupling",
			Description: "Package-level coupling, instability, abstractness, and cohesion (LCOM-4) metrics",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"sort":              {Type: "string", Description: "sort field: instability, distance, lcom, ca, ce (default: distance)"},
					"top":               {Type: "integer", Description: "limit to top N results (default: all)"},
					"min_distance":      {Type: "number", Description: "minimum distance from main sequence to include (default: 0)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":         {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_complexity",
			Description: "AST-based complexity metrics per function: cyclomatic, cognitive, nesting depth, fan-in/out",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"min_cyclomatic":    {Type: "integer", Description: "minimum cyclomatic complexity to include (default: 0)"},
					"sort":              {Type: "string", Description: "sort field: cyclomatic, cognitive, lines, nesting (default: cyclomatic)"},
					"top":               {Type: "integer", Description: "limit to top N results (default: all)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_testmap",
			Description: "Map test functions to implementation functions via call graph with coverage classification",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"untested_only":     {Type: "boolean", Description: "only show untested functions (default: false)"},
					"kind":              {Type: "string", Description: "filter by symbol kind (e.g. function, method)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_impact",
			Description: "Compute blast radius and risk scores for changed symbols via reverse call graph traversal",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"changed":           {OneOf: stringOrArray},
					"diff_ref":          {Type: "string", Description: "git ref for diff-based change detection (e.g. HEAD~1)"},
					"max_depth":         {Type: "integer", Description: "maximum traversal depth (default: 10)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_hotspot",
			Description: "Detect code hotspots from git churn, complexity, and call graph centrality",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"since":             {Type: "string", Description: "git log period (e.g. 90d, 6m, 1y; default: 90d)"},
					"top":               {Type: "integer", Description: "limit to top N results (default: 20)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_check",
			Description: "Run configurable quality gates (complexity, length, generated ratio) and report pass/fail for CI",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"base":              {Type: "string", Description: "git ref to diff against -- only report violations in changed files (e.g. main, HEAD~1)"},
					"max_cyclomatic":    {Type: "integer", Description: "max cyclomatic complexity per function (default: 50, 0 to disable)"},
					"max_cognitive":     {Type: "integer", Description: "max cognitive complexity per function (default: 80, 0 to disable)"},
					"max_lines":         {Type: "integer", Description: "max lines per function (default: 300, 0 to disable)"},
					"max_generated_pct":    {Type: "integer", Description: "max % of files that are generated (default: 60, 0 to disable)"},
					"max_fields":           {Type: "integer", Description: "max fields per type (default: 0, 0 to disable)"},
					"max_interface_width":  {Type: "integer", Description: "max interface width (default: 0, 0 to disable)"},
					"include_generated":    {Type: "boolean", Description: "include generated files in complexity analysis (default: false)"},
					"generator":            {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_smells",
			Description: "Detect structural code smells from complexity, coupling, and type metrics",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"id":                {Type: "string", Description: "comma-separated smell ID filter (e.g. god_function,feature_envy)"},
					"severity":          {Type: "string", Description: "filter by severity: error or warn"},
					"top":               {Type: "integer", Description: "limit to top N results (default: all)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":         {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_boundaries",
			Description: "Check module boundary rules from .canopyboundaries config",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"include_generated": {Type: "boolean"},
					"generator":         {Type: "string"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_reachability",
			Description: "Check whether a package transitively reaches sensitive capabilities (process exec, network, file I/O) via call graph",
			InputSchema: Schema{
				Properties: map[string]Property{
					"package":           {Type: "string", Description: "target package to analyze (required)"},
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"category":          {Type: "string", Description: "filter by capability category (e.g. process_execution, network_access, file_access)"},
					"attack_id":         {Type: "string", Description: "filter by MITRE ATT&CK ID (e.g. T1059)"},
					"depth":             {Type: "integer", Description: "max traversal depth (default: 10)"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":         {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"package"},
			}.ToMap(),
		},
		{
			Name:        "gts_guardrails",
			Description: "Return structured advisory for a file: generated status, boundary module, complexity, fan-in warnings",
			InputSchema: Schema{
				Properties: map[string]Property{
					"file":  {Type: "string", Description: "file path to analyze (required)"},
					"path":  {Type: "string", Description: "index root path"},
					"cache": {Type: "string", Description: "index cache path"},
				},
				Required: []string{"file"},
			}.ToMap(),
		},
		{
			Name:        "gts_report",
			Description: "Executive summary report aggregating all analyses: complexity, boundaries, import cycles, capabilities, dead code, hotspots",
			InputSchema: Schema{
				Properties: map[string]Property{
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":         {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
		{
			Name:        "gts_review",
			Description: "Aggregate review report for changed files: complexity, boundary violations, capabilities, blast radius",
			InputSchema: Schema{
				Properties: map[string]Property{
					"base":              {Type: "string", Description: "git ref to diff against (required, e.g. main, HEAD~1)"},
					"path":              {Type: "string", Description: "index root path"},
					"cache":             {Type: "string", Description: "index cache path"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":         {Type: "string", Description: "filter to specific generator"},
				},
				Required: []string{"base"},
			}.ToMap(),
		},
	}
}

func transformTools() []Tool {
	return []Tool{
		{
			Name:        "gts_refactor",
			Description: "Apply structural declaration renames (dry-run by default)",
			InputSchema: Schema{
				Properties: map[string]Property{
					"selector":          {Type: "string"},
					"new_name":          {Type: "string"},
					"path":              {Type: "string"},
					"cache":             {Type: "string"},
					"engine":            {Type: "string"},
					"callsites":         {Type: "boolean"},
					"cross_package":     {Type: "boolean"},
					"write":             {Type: "boolean"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
				Required: []string{"selector", "new_name"},
			}.ToMap(),
		},
		{
			Name:        "gts_diff",
			Description: "Structural diff between two snapshots (path or cache sources)",
			InputSchema: Schema{
				Properties: map[string]Property{
					"before_path":       {Type: "string"},
					"before_cache":      {Type: "string"},
					"after_path":        {Type: "string"},
					"after_cache":       {Type: "string"},
					"include_generated": {Type: "boolean", Description: "include generated files (default: false)"},
					"generator":          {Type: "string", Description: "filter to specific generator (e.g. protobuf, mockgen, human)"},
				},
			}.ToMap(),
		},
	}
}

func finalizeToolSchema(tool *Tool) {
	if tool == nil {
		return
	}

	schema := tool.InputSchema
	if schema == nil {
		schema = map[string]any{}
	}
	schema["type"] = "object"

	properties := normalizeSchemaProperties(schema["properties"])
	schema["properties"] = properties

	required := normalizeRequiredKeys(schema["required"], properties)
	if len(required) > 0 {
		schema["required"] = required
	} else {
		delete(schema, "required")
	}

	if _, ok := schema["additionalProperties"]; !ok {
		schema["additionalProperties"] = false
	}
	tool.InputSchema = schema
}

func normalizeSchemaProperties(raw any) map[string]any {
	switch typed := raw.(type) {
	case map[string]any:
		if typed == nil {
			return map[string]any{}
		}
		return typed
	default:
		return map[string]any{}
	}
}

func normalizeRequiredKeys(raw any, properties map[string]any) []string {
	if len(properties) == 0 {
		return nil
	}

	keys := make([]string, 0, len(properties))
	switch typed := raw.(type) {
	case string:
		keys = append(keys, typed)
	case []string:
		keys = append(keys, typed...)
	case []any:
		for _, item := range typed {
			value, ok := item.(string)
			if !ok {
				continue
			}
			keys = append(keys, value)
		}
	}

	seen := map[string]bool{}
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		if _, ok := properties[key]; !ok {
			continue
		}
		seen[key] = true
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	return normalized
}

func (s *Service) Call(name string, args map[string]any) (any, error) {
	switch strings.TrimSpace(name) {
	case "gts_grep":
		return s.callGrep(args)
	case "gts_map":
		return s.callMap(args)
	case "gts_query":
		return s.callQuery(args)
	case "gts_refs":
		return s.callRefs(args)
	case "gts_context":
		return s.callContext(args)
	case "gts_scope":
		return s.callScope(args)
	case "gts_deps":
		return s.callDeps(args)
	case "gts_callgraph":
		return s.callCallgraph(args)
	case "gts_dead":
		return s.callDead(args)
	case "gts_chunk":
		return s.callChunk(args)
	case "gts_lint":
		return s.callLint(args)
	case "gts_refactor":
		return s.callRefactor(args)
	case "gts_diff":
		return s.callDiff(args)
	case "gts_stats":
		return s.callStats(args)
	case "gts_files":
		return s.callFiles(args)
	case "gts_bridge":
		return s.callBridge(args)
	case "gts_capa":
		return s.callCapa(args)
	case "gts_similarity":
		return s.callSimilarity(args)
	case "gts_types":
		return s.callTypeMetrics(args)
	case "gts_coupling":
		return s.callCoupling(args)
	case "gts_complexity":
		return s.callComplexity(args)
	case "gts_testmap":
		return s.callTestmap(args)
	case "gts_impact":
		return s.callImpact(args)
	case "gts_hotspot":
		return s.callHotspot(args)
	case "gts_check":
		return s.callCheck(args)
	case "gts_smells":
		return s.callSmells(args)
	case "gts_boundaries":
		return s.callBoundaries(args)
	case "gts_reachability":
		return s.callReachability(args)
	case "gts_drift":
		return s.callDrift(args)
	case "gts_guardrails":
		return s.callGuardrails(args)
	case "gts_report":
		return s.callReport(args)
	case "gts_review":
		return s.callReview(args)
	case "gts_services":
		return s.callServices(args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
