// Package indexcoverage builds parser-health reports from structural indexes.
package indexcoverage

import (
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/canopy/pkg/model"
)

const defaultMaxFiles = 100

type Options struct {
	IncludeClean     bool
	IncludeGenerated bool
	MaxFiles         int
}

type Summary struct {
	Clean       int `json:"clean"`
	Partial     int `json:"partial"`
	Stopped     int `json:"stopped"`
	Generated   int `json:"generated"`
	Unknown     int `json:"unknown"`
	ParseErrors int `json:"parse_errors"`
	Gaps        int `json:"gaps"`
	Recovered   int `json:"recovered_regions"`
	IgnoredEOF  int `json:"ignored_eof_missing_regions"`
	Truncated   int `json:"truncated_files"`
}

type File struct {
	Path      string              `json:"path"`
	Language  string              `json:"language,omitempty"`
	Generated bool                `json:"generated,omitempty"`
	Coverage  model.ParseCoverage `json:"coverage"`
}

type Report struct {
	Root             string             `json:"root"`
	TotalFiles       int                `json:"total_files"`
	Summary          Summary            `json:"summary"`
	Files            []File             `json:"files,omitempty"`
	Errors           []model.ParseError `json:"errors,omitempty"`
	DetailsTruncated bool               `json:"details_truncated,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultMaxFiles
	}

	report := Report{
		Root:       idx.Root,
		TotalFiles: len(idx.Files),
		Errors:     append([]model.ParseError(nil), idx.Errors...),
	}
	report.Summary.ParseErrors = len(report.Errors)
	sort.Slice(report.Errors, func(i, j int) bool { return report.Errors[i].Path < report.Errors[j].Path })

	details := make([]File, 0)
	for _, file := range idx.Files {
		coverage := normalizedCoverage(file.ParseCoverage)
		accumulate(&report.Summary, coverage)
		if !includeDetail(coverage.Status, opts) {
			continue
		}
		details = append(details, File{
			Path:      file.Path,
			Language:  file.Language,
			Generated: file.Generated != nil,
			Coverage:  coverage,
		})
	}

	sort.Slice(details, func(i, j int) bool {
		left := statusRank(details[i].Coverage.Status)
		right := statusRank(details[j].Coverage.Status)
		if left == right {
			return details[i].Path < details[j].Path
		}
		return left < right
	})
	if len(details) > opts.MaxFiles {
		details = details[:opts.MaxFiles]
		report.DetailsTruncated = true
	}
	report.Files = details
	return report, nil
}

// StrictFailure reports whether the index contains an actionable parse gap,
// a stopped parse, a missing receipt, or a complete parse failure.
func (r Report) StrictFailure() bool {
	return r.Summary.Partial > 0 ||
		r.Summary.Stopped > 0 ||
		r.Summary.Unknown > 0 ||
		r.Summary.ParseErrors > 0 ||
		r.Summary.Truncated > 0
}

func normalizedCoverage(input *model.ParseCoverage) model.ParseCoverage {
	if input == nil {
		return model.ParseCoverage{Status: model.ParseCoverageUnknown}
	}
	coverage := *input
	coverage.Gaps = append([]model.ParseGap(nil), input.Gaps...)
	coverage.Status = strings.ToLower(strings.TrimSpace(coverage.Status))
	switch coverage.Status {
	case model.ParseCoverageClean,
		model.ParseCoveragePartial,
		model.ParseCoverageStopped,
		model.ParseCoverageGenerated:
	default:
		coverage.Status = model.ParseCoverageUnknown
	}
	return coverage
}

func accumulate(summary *Summary, coverage model.ParseCoverage) {
	if summary == nil {
		return
	}
	switch coverage.Status {
	case model.ParseCoverageClean:
		summary.Clean++
	case model.ParseCoveragePartial:
		summary.Partial++
	case model.ParseCoverageStopped:
		summary.Stopped++
	case model.ParseCoverageGenerated:
		summary.Generated++
	default:
		summary.Unknown++
	}
	summary.Gaps += len(coverage.Gaps)
	summary.Recovered += coverage.RecoveredRegions
	summary.IgnoredEOF += coverage.IgnoredEOFMissingRegions
	if coverage.Truncated {
		summary.Truncated++
	}
}

func includeDetail(status string, opts Options) bool {
	switch status {
	case model.ParseCoverageClean:
		return opts.IncludeClean
	case model.ParseCoverageGenerated:
		return opts.IncludeGenerated
	default:
		return true
	}
}

func statusRank(status string) int {
	switch status {
	case model.ParseCoverageStopped:
		return 0
	case model.ParseCoveragePartial:
		return 1
	case model.ParseCoverageUnknown:
		return 2
	case model.ParseCoverageGenerated:
		return 3
	case model.ParseCoverageClean:
		return 4
	default:
		return 5
	}
}
