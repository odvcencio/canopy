package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/boundaries"
	"github.com/odvcencio/gts-suite/pkg/capa"
	"github.com/odvcencio/gts-suite/pkg/complexity"
	"github.com/odvcencio/gts-suite/pkg/hotspot"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

// Report is the top-level executive summary structure produced by `gts analyze report`.
type Report struct {
	// Codebase overview
	Files        int            `json:"files"`
	Languages    map[string]int `json:"languages"`
	TotalSymbols int            `json:"total_symbols"`
	GeneratedPct int            `json:"generated_pct"`

	// Complexity
	FunctionCount int `json:"function_count"`
	CyclomaticMax int `json:"cyclomatic_max"`
	CyclomaticP90 int `json:"cyclomatic_p90"`
	CognitiveMax  int `json:"cognitive_max"`

	// Architecture
	BoundaryViolations int `json:"boundary_violations"`
	ImportCycles       int `json:"import_cycles"`

	// Security
	Capabilities int `json:"capabilities"`

	// Dead code
	DeadFunctions int `json:"dead_functions"`

	// Hotspots (top 5)
	Hotspots []HotspotEntry `json:"hotspots,omitempty"`

	// Team breakdown (only when --by-team is set)
	Teams map[string]*TeamMetrics `json:"teams,omitempty"`
}

// HotspotEntry is a simplified hotspot record for the executive report.
type HotspotEntry struct {
	File       string  `json:"file"`
	Name       string  `json:"name"`
	Cyclomatic int     `json:"cyclomatic"`
	Score      float64 `json:"score"`
}

// TeamMetrics holds per-team breakdown of report metrics.
type TeamMetrics struct {
	Files            int `json:"files"`
	Functions        int `json:"functions"`
	CyclomaticMax    int `json:"cyclomatic_max"`
	CognitiveMax     int `json:"cognitive_max"`
	DeadFunctions    int `json:"dead_functions"`
	BoundaryViolations int `json:"boundary_violations"`
	Capabilities     int `json:"capabilities"`
}

// ownerRule maps a path pattern to a team name, from CODEOWNERS or .gtsowners.
type ownerRule struct {
	Pattern string
	Team    string
}

func newReportCmd() *cobra.Command {
	var (
		cachePath string
		noCache   bool
		jsonOut   bool
		format    string
		compare   string
		byTeam    bool
	)

	cmd := &cobra.Command{
		Use:   "report [path]",
		Short: "Executive summary report aggregating all analyses",
		Long: `Run all analyses and produce a comprehensive report.
Supersedes 'analyze summary' with architecture, security, dead code, and hotspot data.

Examples:
  gts analyze report
  gts analyze report --format markdown
  gts analyze report --json
  gts analyze report --compare main
  gts analyze report --by-team`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			analysisIdx := applyGeneratedFilter(cmd, idx)

			rpt := Report{
				Languages: make(map[string]int),
			}

			// --- Codebase overview ---
			rpt.Files = len(idx.Files)
			for _, f := range idx.Files {
				lang := f.Language
				if lang == "" {
					lang = "unknown"
				}
				rpt.Languages[lang]++
			}
			for _, f := range idx.Files {
				rpt.TotalSymbols += len(f.Symbols)
			}
			totalFiles := idx.FileCount()
			genFiles := idx.GeneratedFileCount()
			if totalFiles > 0 {
				rpt.GeneratedPct = genFiles * 100 / totalFiles
			}

			// --- Complexity ---
			complexityReport, complexityErr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{})
			if complexityErr == nil {
				rpt.FunctionCount = complexityReport.Summary.Count
				rpt.CyclomaticMax = complexityReport.Summary.MaxCyclomatic
				rpt.CyclomaticP90 = complexityReport.Summary.P90Cyclomatic
				rpt.CognitiveMax = complexityReport.Summary.MaxCognitive
			}

			// --- Architecture: boundaries ---
			cfg, _ := boundaries.LoadConfig(target)
			if cfg != nil && len(cfg.Rules) > 0 {
				depReport, depErr := deps.Build(idx, deps.Options{
					Mode:         "package",
					IncludeEdges: true,
				})
				if depErr == nil {
					importEdges := make([]boundaries.ImportEdge, 0, len(depReport.Edges))
					for _, edge := range depReport.Edges {
						if edge.Internal {
							importEdges = append(importEdges, boundaries.ImportEdge{From: edge.From, To: edge.To})
						}
					}
					violations := boundaries.Evaluate(cfg, importEdges)
					rpt.BoundaryViolations = len(violations)
				}
			}

			// --- Architecture: import cycles ---
			depReport, depErr := deps.Build(analysisIdx, deps.Options{
				Mode:         "package",
				IncludeEdges: true,
			})
			if depErr == nil {
				graph := deps.GraphFromEdges(depReport.Edges)
				cycles := deps.DetectCycles(graph)
				rpt.ImportCycles = len(cycles)
			}

			// --- Security: capabilities ---
			rules := capa.BuiltinRules()
			capaMatches := capa.Detect(analysisIdx, rules)
			rpt.Capabilities = len(capaMatches)

			// --- Dead code ---
			var xrefGraph *xref.Graph
			if g, xrefErr := xref.Build(analysisIdx); xrefErr == nil {
				xrefGraph = &g
				deadCount := 0
				for _, definition := range g.Definitions {
					if !definition.Callable {
						continue
					}
					if isEntrypointDefinition(definition) {
						continue
					}
					if isTestSourceFile(definition.File) {
						continue
					}
					if g.IncomingCount(definition.ID) == 0 {
						deadCount++
					}
				}
				rpt.DeadFunctions = deadCount
			}

			// --- Hotspots (top 5) ---
			hotspotReport, hotspotErr := hotspot.Analyze(analysisIdx, hotspot.Options{
				Root:  target,
				Since: "90d",
				Top:   5,
			})
			if hotspotErr == nil {
				for _, h := range hotspotReport.Functions {
					rpt.Hotspots = append(rpt.Hotspots, HotspotEntry{
						File:       h.File,
						Name:       h.Name,
						Cyclomatic: h.Cyclomatic,
						Score:      h.Score,
					})
				}
			}

			// --- Team breakdown ---
			if byTeam {
				ownerRules := loadOwnerRules(target)
				if len(ownerRules) > 0 {
					rpt.Teams = buildTeamMetrics(ownerRules, idx, analysisIdx, complexityReport, xrefGraph, capaMatches, cfg, target)
				}
			}

			// --- Delta comparison ---
			var delta *Report
			if compare != "" {
				delta, err = buildCompareReport(compare, target, cmd)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: --compare %s failed: %v\n", compare, err)
				}
			}

			// --- Output ---
			outputFmt := format
			if jsonOut && outputFmt == "markdown" {
				outputFmt = "json"
			}

			switch outputFmt {
			case "json":
				if delta != nil {
					return emitJSON(struct {
						Current  Report  `json:"current"`
						Baseline Report  `json:"baseline"`
					}{
						Current:  rpt,
						Baseline: *delta,
					})
				}
				return emitJSON(rpt)
			default:
				printMarkdownReport(rpt, delta, target)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&format, "format", "markdown", "output format: markdown, json")
	cmd.Flags().StringVar(&compare, "compare", "", "git ref to compare against for delta reporting")
	cmd.Flags().BoolVar(&byTeam, "by-team", false, "break down metrics by team ownership (from CODEOWNERS/.gtsowners)")
	return cmd
}

func printMarkdownReport(rpt Report, delta *Report, target string) {
	name := filepath.Base(target)
	if name == "." {
		if wd, err := os.Getwd(); err == nil {
			name = filepath.Base(wd)
		}
	}

	fmt.Printf("# GTS Report -- %s\n\n", name)

	// Codebase Overview
	fmt.Println("## Codebase Overview")
	langList := sortedKeys(rpt.Languages)
	langStr := fmt.Sprintf("%d language", len(langList))
	if len(langList) != 1 {
		langStr += "s"
	}
	langStr += " (" + strings.Join(langList, ", ") + ")"
	fmt.Printf("- %d files across %s\n", rpt.Files, langStr)
	fmt.Printf("- %d functions, %d%% generated\n", rpt.FunctionCount, rpt.GeneratedPct)
	fmt.Printf("- %d symbols total\n", rpt.TotalSymbols)
	if delta != nil {
		printDelta("files", rpt.Files, delta.Files)
		printDelta("functions", rpt.FunctionCount, delta.FunctionCount)
	}
	fmt.Println()

	// Complexity
	fmt.Println("## Complexity")
	fmt.Printf("- Max cyclomatic: %d (p90: %d)\n", rpt.CyclomaticMax, rpt.CyclomaticP90)
	fmt.Printf("- Max cognitive: %d\n", rpt.CognitiveMax)
	if delta != nil {
		printDelta("max cyclomatic", rpt.CyclomaticMax, delta.CyclomaticMax)
		printDelta("max cognitive", rpt.CognitiveMax, delta.CognitiveMax)
	}
	fmt.Println()

	// Architecture Health
	fmt.Println("## Architecture Health")
	fmt.Printf("- %d boundary violations\n", rpt.BoundaryViolations)
	fmt.Printf("- %d import cycles\n", rpt.ImportCycles)
	if delta != nil {
		printDelta("boundary violations", rpt.BoundaryViolations, delta.BoundaryViolations)
		printDelta("import cycles", rpt.ImportCycles, delta.ImportCycles)
	}
	fmt.Println()

	// Security
	fmt.Println("## Security")
	fmt.Printf("- %d capability exposures\n", rpt.Capabilities)
	if delta != nil {
		printDelta("capabilities", rpt.Capabilities, delta.Capabilities)
	}
	fmt.Println()

	// Dead Code
	fmt.Println("## Dead Code")
	fmt.Printf("- %d unreferenced functions\n", rpt.DeadFunctions)
	if delta != nil {
		printDelta("dead functions", rpt.DeadFunctions, delta.DeadFunctions)
	}
	fmt.Println()

	// Hotspots
	if len(rpt.Hotspots) > 0 {
		fmt.Println("## Hotspots")
		for i, h := range rpt.Hotspots {
			fmt.Printf("%d. %s:%s (cyc=%d, score=%.3f)\n", i+1, h.File, h.Name, h.Cyclomatic, h.Score)
		}
		fmt.Println()
	}

	// Team Breakdown
	if len(rpt.Teams) > 0 {
		fmt.Println("## Team Breakdown")
		teamNames := make([]string, 0, len(rpt.Teams))
		for name := range rpt.Teams {
			teamNames = append(teamNames, name)
		}
		sort.Strings(teamNames)
		for _, name := range teamNames {
			tm := rpt.Teams[name]
			fmt.Printf("### %s\n", name)
			fmt.Printf("- %d files, %d functions\n", tm.Files, tm.Functions)
			fmt.Printf("- Max cyclomatic: %d, max cognitive: %d\n", tm.CyclomaticMax, tm.CognitiveMax)
			fmt.Printf("- %d dead functions, %d boundary violations, %d capabilities\n",
				tm.DeadFunctions, tm.BoundaryViolations, tm.Capabilities)
			fmt.Println()
		}
	}
}

func printDelta(label string, current, baseline int) {
	diff := current - baseline
	if diff == 0 {
		return
	}
	sign := "+"
	if diff < 0 {
		sign = ""
	}
	fmt.Printf("  > delta %s: %s%d\n", label, sign, diff)
}

// buildCompareReport builds a Report for a baseline git ref by checking out
// the ref into a temporary worktree, building the index, and running the same
// analyses. Returns nil if the comparison cannot be performed.
func buildCompareReport(ref, target string, cmd *cobra.Command) (*Report, error) {
	// Create temp worktree directory.
	tmpDir, err := os.MkdirTemp("", "gts-compare-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Git worktree add.
	absTarget, _ := filepath.Abs(target)
	gitCmd := newGitCmd(absTarget, "worktree", "add", "--detach", tmpDir, ref)
	if out, err := gitCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %s: %w", string(out), err)
	}
	defer func() {
		cleanup := newGitCmd(absTarget, "worktree", "remove", "--force", tmpDir)
		_ = cleanup.Run()
	}()

	// Build index at the baseline ref.
	baseIdx, err := loadOrBuild(cmd, "", tmpDir, true)
	if err != nil {
		return nil, fmt.Errorf("building baseline index: %w", err)
	}
	baseAnalysisIdx := applyGeneratedFilter(cmd, baseIdx)

	rpt := Report{
		Languages: make(map[string]int),
	}

	rpt.Files = len(baseIdx.Files)
	for _, f := range baseIdx.Files {
		lang := f.Language
		if lang == "" {
			lang = "unknown"
		}
		rpt.Languages[lang]++
	}
	for _, f := range baseIdx.Files {
		rpt.TotalSymbols += len(f.Symbols)
	}
	totalFiles := baseIdx.FileCount()
	genFiles := baseIdx.GeneratedFileCount()
	if totalFiles > 0 {
		rpt.GeneratedPct = genFiles * 100 / totalFiles
	}

	// Complexity
	complexityReport, complexityErr := complexity.Analyze(baseAnalysisIdx, baseAnalysisIdx.Root, complexity.Options{})
	if complexityErr == nil {
		rpt.FunctionCount = complexityReport.Summary.Count
		rpt.CyclomaticMax = complexityReport.Summary.MaxCyclomatic
		rpt.CyclomaticP90 = complexityReport.Summary.P90Cyclomatic
		rpt.CognitiveMax = complexityReport.Summary.MaxCognitive
	}

	// Boundaries
	baseCfg, _ := boundaries.LoadConfig(tmpDir)
	if baseCfg != nil && len(baseCfg.Rules) > 0 {
		depReport, depErr := deps.Build(baseIdx, deps.Options{
			Mode:         "package",
			IncludeEdges: true,
		})
		if depErr == nil {
			importEdges := make([]boundaries.ImportEdge, 0, len(depReport.Edges))
			for _, edge := range depReport.Edges {
				if edge.Internal {
					importEdges = append(importEdges, boundaries.ImportEdge{From: edge.From, To: edge.To})
				}
			}
			violations := boundaries.Evaluate(baseCfg, importEdges)
			rpt.BoundaryViolations = len(violations)
		}
	}

	// Import cycles
	depReport, depErr := deps.Build(baseAnalysisIdx, deps.Options{
		Mode:         "package",
		IncludeEdges: true,
	})
	if depErr == nil {
		graph := deps.GraphFromEdges(depReport.Edges)
		cycles := deps.DetectCycles(graph)
		rpt.ImportCycles = len(cycles)
	}

	// Capabilities
	capaRules := capa.BuiltinRules()
	capaMatches := capa.Detect(baseAnalysisIdx, capaRules)
	rpt.Capabilities = len(capaMatches)

	// Dead code
	if g, xrefErr := xref.Build(baseAnalysisIdx); xrefErr == nil {
		deadCount := 0
		for _, definition := range g.Definitions {
			if !definition.Callable {
				continue
			}
			if isEntrypointDefinition(definition) {
				continue
			}
			if isTestSourceFile(definition.File) {
				continue
			}
			if g.IncomingCount(definition.ID) == 0 {
				deadCount++
			}
		}
		rpt.DeadFunctions = deadCount
	}

	return &rpt, nil
}

// newGitCmd creates an exec.Cmd for git operations in the given directory.
func newGitCmd(dir string, gitArgs ...string) *exec.Cmd {
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = dir
	return cmd
}

// loadOwnerRules reads CODEOWNERS (GitHub format) or .gtsowners (simpler format)
// from the target directory. Returns nil if neither file exists.
func loadOwnerRules(target string) []ownerRule {
	// Try .gtsowners first (simpler: "path team-name")
	if rules := readGTSOwnersFile(filepath.Join(target, ".gtsowners")); rules != nil {
		return rules
	}
	// Try standard CODEOWNERS locations
	for _, candidate := range []string{
		filepath.Join(target, "CODEOWNERS"),
		filepath.Join(target, ".github", "CODEOWNERS"),
		filepath.Join(target, "docs", "CODEOWNERS"),
	} {
		if rules := readCodeOwnersFile(candidate); rules != nil {
			return rules
		}
	}
	return nil
}

// readGTSOwnersFile parses a .gtsowners file (format: "path team-name").
func readGTSOwnersFile(path string) []ownerRule {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var rules []ownerRule
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		rules = append(rules, ownerRule{
			Pattern: parts[0],
			Team:    parts[1],
		})
	}
	return rules
}

// readCodeOwnersFile parses a GitHub CODEOWNERS file (format: "path @team").
func readCodeOwnersFile(path string) []ownerRule {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var rules []ownerRule
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		// First field is the pattern, remaining are owners.
		// Use the first owner as the team name, stripping @.
		team := strings.TrimPrefix(parts[1], "@")
		rules = append(rules, ownerRule{
			Pattern: parts[0],
			Team:    team,
		})
	}
	return rules
}

// resolveOwner returns the team name for a file path based on owner rules.
// Rules are matched last-match-wins (like CODEOWNERS).
func resolveOwner(rules []ownerRule, filePath string) string {
	matched := ""
	for _, rule := range rules {
		ok, _ := filepath.Match(rule.Pattern, filePath)
		if !ok {
			// Try prefix match for directory patterns (e.g. "pkg/model/" matches "pkg/model/foo.go").
			pattern := strings.TrimSuffix(rule.Pattern, "/")
			if strings.HasPrefix(filePath, pattern+"/") || strings.HasPrefix(filePath, pattern) {
				ok = true
			}
		}
		if ok {
			matched = rule.Team
		}
	}
	return matched
}

// buildTeamMetrics computes per-team metric breakdowns.
func buildTeamMetrics(
	ownerRules []ownerRule,
	idx, analysisIdx *model.Index,
	complexityReport *complexity.Report,
	xrefGraph *xref.Graph,
	capaMatches []capa.Match,
	boundaryCfg *boundaries.Config,
	target string,
) map[string]*TeamMetrics {
	teams := make(map[string]*TeamMetrics)

	getTeam := func(filePath string) *TeamMetrics {
		team := resolveOwner(ownerRules, filePath)
		if team == "" {
			team = "(unowned)"
		}
		if teams[team] == nil {
			teams[team] = &TeamMetrics{}
		}
		return teams[team]
	}

	// Files
	for _, f := range idx.Files {
		tm := getTeam(f.Path)
		tm.Files++
	}

	// Functions and complexity
	if complexityReport != nil {
		for _, fn := range complexityReport.Functions {
			tm := getTeam(fn.File)
			tm.Functions++
			if fn.Cyclomatic > tm.CyclomaticMax {
				tm.CyclomaticMax = fn.Cyclomatic
			}
			if fn.Cognitive > tm.CognitiveMax {
				tm.CognitiveMax = fn.Cognitive
			}
		}
	}

	// Dead functions
	if xrefGraph != nil {
		for _, definition := range xrefGraph.Definitions {
			if !definition.Callable {
				continue
			}
			if isEntrypointDefinition(definition) {
				continue
			}
			if isTestSourceFile(definition.File) {
				continue
			}
			if xrefGraph.IncomingCount(definition.ID) == 0 {
				tm := getTeam(definition.File)
				tm.DeadFunctions++
			}
		}
	}

	// Capabilities
	for _, m := range capaMatches {
		if len(m.Files) > 0 {
			tm := getTeam(m.Files[0])
			tm.Capabilities++
		}
	}

	// Boundary violations
	if boundaryCfg != nil && len(boundaryCfg.Rules) > 0 {
		depReport, depErr := deps.Build(idx, deps.Options{
			Mode:         "package",
			IncludeEdges: true,
		})
		if depErr == nil {
			importEdges := make([]boundaries.ImportEdge, 0, len(depReport.Edges))
			for _, edge := range depReport.Edges {
				if edge.Internal {
					importEdges = append(importEdges, boundaries.ImportEdge{From: edge.From, To: edge.To})
				}
			}
			violations := boundaries.Evaluate(boundaryCfg, importEdges)
			for _, v := range violations {
				tm := getTeam(v.From)
				tm.BoundaryViolations++
			}
		}
	}

	return teams
}
