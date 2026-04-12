package mcp

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/odvcencio/canopy/pkg/complexity"
)

type mcpCheckViolation struct {
	Check     string `json:"check"`
	File      string `json:"file"`
	Name      string `json:"name"`
	Line      int    `json:"line"`
	Value     int    `json:"value"`
	Threshold int    `json:"threshold"`
}

type mcpCheckResult struct {
	Status       string              `json:"status"`
	Checks       int                 `json:"checks"`
	Violations   int                 `json:"violations"`
	Base         string              `json:"base,omitempty"`
	ChangedFiles int                 `json:"changed_files,omitempty"`
	Details      []mcpCheckViolation `json:"details,omitempty"`
}

func (s *Service) callCheck(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	base := stringArg(args, "base")
	maxCyclomatic := intArg(args, "max_cyclomatic", 50)
	maxCognitive := intArg(args, "max_cognitive", 80)
	maxLines := intArg(args, "max_lines", 300)
	maxGeneratedPct := intArg(args, "max_generated_pct", 60)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	analysisIdx := applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	var violations []mcpCheckViolation
	checksRun := 0

	// Checks 1-3 share a single complexity report.
	if maxCyclomatic > 0 || maxCognitive > 0 || maxLines > 0 {
		report, analyzeErr := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{})

		if maxCyclomatic > 0 {
			checksRun++
			if analyzeErr == nil {
				for _, fn := range report.Functions {
					if fn.Cyclomatic > maxCyclomatic {
						violations = append(violations, mcpCheckViolation{
							Check:     "cyclomatic",
							File:      fn.File,
							Name:      fn.Name,
							Line:      fn.StartLine,
							Value:     fn.Cyclomatic,
							Threshold: maxCyclomatic,
						})
					}
				}
			}
		}

		if maxCognitive > 0 {
			checksRun++
			if analyzeErr == nil {
				for _, fn := range report.Functions {
					if fn.Cognitive > maxCognitive {
						violations = append(violations, mcpCheckViolation{
							Check:     "cognitive",
							File:      fn.File,
							Name:      fn.Name,
							Line:      fn.StartLine,
							Value:     fn.Cognitive,
							Threshold: maxCognitive,
						})
					}
				}
			}
		}

		if maxLines > 0 {
			checksRun++
			if analyzeErr == nil {
				for _, fn := range report.Functions {
					if fn.Lines > maxLines {
						violations = append(violations, mcpCheckViolation{
							Check:     "lines",
							File:      fn.File,
							Name:      fn.Name,
							Line:      fn.StartLine,
							Value:     fn.Lines,
							Threshold: maxLines,
						})
					}
				}
			}
		}
	}

	// Check 4: Generated ratio (uses full index).
	if maxGeneratedPct > 0 {
		checksRun++
		totalFiles := idx.FileCount()
		genFiles := idx.GeneratedFileCount()
		if totalFiles > 0 {
			pct := genFiles * 100 / totalFiles
			if pct > maxGeneratedPct {
				violations = append(violations, mcpCheckViolation{
					Check:     "generated-ratio",
					File:      "",
					Name:      fmt.Sprintf("%d%% generated (%d/%d files)", pct, genFiles, totalFiles),
					Value:     pct,
					Threshold: maxGeneratedPct,
				})
			}
		}
	}

	// When base is set, restrict violations to changed files only.
	var numChanged int
	if base != "" {
		changed, diffErr := mcpChangedFiles(base, target)
		if diffErr != nil {
			return nil, diffErr
		}
		numChanged = len(changed)
		var filtered []mcpCheckViolation
		for _, v := range violations {
			if v.File == "" || changed[v.File] {
				filtered = append(filtered, v)
			}
		}
		violations = filtered
	}

	result := mcpCheckResult{
		Status:       "PASS",
		Checks:       checksRun,
		Violations:   len(violations),
		Base:         base,
		ChangedFiles: numChanged,
		Details:      violations,
	}
	if len(violations) > 0 {
		result.Status = "FAIL"
	}

	return result, nil
}

// mcpChangedFiles runs git diff --name-only against the given base ref.
func mcpChangedFiles(base, repoDir string) (map[string]bool, error) {
	cmd := exec.Command("git", "-C", repoDir, "diff", "--name-only", base)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w", base, err)
	}
	files := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files[line] = true
		}
	}
	return files, nil
}
