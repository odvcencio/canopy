package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/pkg/complexity"
)

// trendRecord is a single snapshot appended to .canopy/trends.jsonl.
type trendRecord struct {
	Timestamp string       `json:"timestamp"`
	Commit    string       `json:"commit"`
	Metrics   trendMetrics `json:"metrics"`
}

type trendMetrics struct {
	CyclomaticMax int `json:"cyclomatic_max"`
	CognitiveMax  int `json:"cognitive_max"`
	Violations    int `json:"violations"`
	Functions     int `json:"functions"`
	Files         int `json:"files"`
}

func newTrendsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trends",
		Short: "Track quality metrics over time",
	}
	cmd.AddCommand(
		newTrendsRecordCmd(),
		newTrendsShowCmd(),
	)
	return cmd
}

// --- trends record ---

func newTrendsRecordCmd() *cobra.Command {
	var (
		cachePath string
		noCache   bool
	)

	cmd := &cobra.Command{
		Use:   "record [path]",
		Short: "Record current quality metrics to .canopy/trends.jsonl",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			abs, err := filepath.Abs(target)
			if err != nil {
				return err
			}

			// Build or load the index.
			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			analysisIdx := applyGeneratedFilter(cmd, idx)

			// Run complexity analysis.
			report, err := complexity.Analyze(analysisIdx, analysisIdx.Root, complexity.Options{})
			if err != nil {
				return fmt.Errorf("complexity analysis: %w", err)
			}

			// Count violations using default thresholds.
			violations := 0
			for _, fn := range report.Functions {
				if fn.Cyclomatic > 50 || fn.Cognitive > 80 {
					violations++
				}
			}

			// Get current git commit hash.
			commit := gitHeadShort(abs)

			record := trendRecord{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Commit:    commit,
				Metrics: trendMetrics{
					CyclomaticMax: report.Summary.MaxCyclomatic,
					CognitiveMax:  report.Summary.MaxCognitive,
					Violations:    violations,
					Functions:     report.Summary.Count,
					Files:         idx.FileCount(),
				},
			}

			// Write to .canopy/trends.jsonl.
			canopyDir := filepath.Join(abs, ".canopy")
			if err := os.MkdirAll(canopyDir, 0755); err != nil {
				return fmt.Errorf("creating .canopy directory: %w", err)
			}

			trendsPath := filepath.Join(canopyDir, "trends.jsonl")
			f, err := os.OpenFile(trendsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("opening trends file: %w", err)
			}
			defer f.Close()

			data, err := json.Marshal(record)
			if err != nil {
				return fmt.Errorf("marshaling record: %w", err)
			}
			if _, err := f.Write(append(data, '\n')); err != nil {
				return fmt.Errorf("writing record: %w", err)
			}

			fmt.Printf("trends: recorded → %s\n", trendsPath)
			fmt.Printf("  commit:         %s\n", record.Commit)
			fmt.Printf("  cyclomatic_max: %d\n", record.Metrics.CyclomaticMax)
			fmt.Printf("  cognitive_max:  %d\n", record.Metrics.CognitiveMax)
			fmt.Printf("  violations:     %d\n", record.Metrics.Violations)
			fmt.Printf("  functions:      %d\n", record.Metrics.Functions)
			fmt.Printf("  files:          %d\n", record.Metrics.Files)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	return cmd
}

// --- trends show ---

func newTrendsShowCmd() *cobra.Command {
	var (
		jsonOutput bool
		since      string
	)

	cmd := &cobra.Command{
		Use:   "show [path]",
		Short: "Display trend summary from .canopy/trends.jsonl",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			abs, err := filepath.Abs(target)
			if err != nil {
				return err
			}

			trendsPath := filepath.Join(abs, ".canopy", "trends.jsonl")
			records, err := readTrends(trendsPath)
			if err != nil {
				return err
			}

			if len(records) == 0 {
				fmt.Println("trends: no records found (run 'gts analyze trends record' first)")
				return nil
			}

			// Filter by --since if specified.
			if since != "" {
				sinceTime, parseErr := time.Parse("2006-01-02", since)
				if parseErr != nil {
					return fmt.Errorf("invalid --since date (expected YYYY-MM-DD): %w", parseErr)
				}
				var filtered []trendRecord
				for _, r := range records {
					ts, err := time.Parse(time.RFC3339, r.Timestamp)
					if err != nil {
						continue
					}
					if !ts.Before(sinceTime) {
						filtered = append(filtered, r)
					}
				}
				records = filtered
				if len(records) == 0 {
					fmt.Printf("trends: no records since %s\n", since)
					return nil
				}
			}

			if jsonOutput {
				return emitJSON(records)
			}

			first := records[0]
			last := records[len(records)-1]

			firstDate := formatTrendDate(first.Timestamp)
			lastDate := formatTrendDate(last.Timestamp)

			fmt.Printf("trends: %d records (%s to %s)\n", len(records), firstDate, lastDate)
			printTrendLine("cyclomatic_max", first.Metrics.CyclomaticMax, last.Metrics.CyclomaticMax)
			printTrendLine("cognitive_max", first.Metrics.CognitiveMax, last.Metrics.CognitiveMax)
			printViolationTrendLine(first.Metrics.Violations, last.Metrics.Violations)
			printTrendLine("functions", first.Metrics.Functions, last.Metrics.Functions)
			printTrendLine("files", first.Metrics.Files, last.Metrics.Files)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&since, "since", "", "filter records from this date (YYYY-MM-DD)")
	return cmd
}

func readTrends(path string) ([]trendRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening trends file: %w", err)
	}
	defer f.Close()

	var records []trendRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r trendRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // skip malformed lines
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

func formatTrendDate(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("2006-01-02")
}

func printTrendLine(label string, first, last int) {
	if first == 0 && last == 0 {
		fmt.Printf("  %-16s  %d → %d\n", label+":", first, last)
		return
	}
	if first == 0 {
		fmt.Printf("  %-16s  %d → %d\n", label+":", first, last)
		return
	}
	pct := float64(last-first) / float64(first) * 100
	fmt.Printf("  %-16s  %d → %d  (%+.1f%%)\n", label+":", first, last, pct)
}

func printViolationTrendLine(first, last int) {
	if last == 0 {
		if first == 0 {
			fmt.Printf("  %-16s  %d → %d  (clean)\n", "violations:", first, last)
		} else {
			fmt.Printf("  %-16s  %d → %d  (clean)\n", "violations:", first, last)
		}
		return
	}
	printTrendLine("violations", first, last)
}

func gitHeadShort(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
