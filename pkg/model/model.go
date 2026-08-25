// Package model defines the core data types for structural code indexing: Symbol, Reference, FileSummary, and Index.
package model

import (
	"time"

	"m31labs.dev/canopy/pkg/ignore"
)

// Symbol represents a top-level declaration (function, method, type) in a source file.
type Symbol struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// Reference represents a usage of a symbol at a specific source location.
type Reference struct {
	File        string `json:"file"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

// GeneratedInfo describes why a file is considered generated and what produced it.
type GeneratedInfo struct {
	Generator string `json:"generator"`        // e.g. "protobuf", "sqlc", "antlr", "unknown"
	Reason    string `json:"reason"`           // how it was detected: "marker", "filename", "config"
	Marker    string `json:"marker,omitempty"` // the actual matched text
}

const (
	// ParseCoverageClean means the parser reported no actionable syntax gap.
	// It is a detection result, not a proof that every construct was extracted.
	ParseCoverageClean = "clean"
	// ParseCoveragePartial means the parser recovered a tree with one or more
	// actionable ERROR, MISSING, or unparsed source regions.
	ParseCoveragePartial = "partial"
	// ParseCoverageStopped means parsing stopped before the complete source was
	// accepted, for example because of a parser limit or an unparsed source tail.
	ParseCoverageStopped = "stopped"
	// ParseCoverageGenerated means Canopy intentionally used its generated-file
	// fast path instead of building a syntax tree.
	ParseCoverageGenerated = "generated"
	// ParseCoverageUnknown means an index entry predates parse receipts or came
	// from a parser that does not expose them.
	ParseCoverageUnknown = "unknown"
)

// ParseGap identifies one top-most source region that the syntax tree could
// not represent cleanly. Lines and columns are one-based; byte offsets are
// zero-based and use an exclusive end.
type ParseGap struct {
	Kind        string `json:"kind"`
	NodeType    string `json:"node_type,omitempty"`
	StartByte   uint32 `json:"start_byte"`
	EndByte     uint32 `json:"end_byte"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column"`
	EndColumn   int    `json:"end_column"`
}

// ParseCoverage is the parser-health receipt attached to an indexed file.
// The receipt reports known gaps and recovery decisions. A clean receipt does
// not claim that a grammar or tags query covers every source construct.
type ParseCoverage struct {
	Status                   string     `json:"status"`
	StopReason               string     `json:"stop_reason,omitempty"`
	ErrorNodes               int        `json:"error_nodes,omitempty"`
	MissingNodes             int        `json:"missing_nodes,omitempty"`
	RecoveredRegions         int        `json:"recovered_regions,omitempty"`
	IgnoredEOFMissingRegions int        `json:"ignored_eof_missing_regions,omitempty"`
	Truncated                bool       `json:"truncated,omitempty"`
	Gaps                     []ParseGap `json:"gaps,omitempty"`
}

// FileSummary contains the structural analysis of a single source file.
type FileSummary struct {
	Path            string         `json:"path"`
	Language        string         `json:"language"`
	SizeBytes       int64          `json:"size_bytes,omitempty"`
	ModTimeUnixNano int64          `json:"mod_time_unix_nano,omitempty"`
	Imports         []string       `json:"imports,omitempty"`
	Symbols         []Symbol       `json:"symbols,omitempty"`
	References      []Reference    `json:"references,omitempty"`
	Generated       *GeneratedInfo `json:"generated,omitempty"`
	ParseCoverage   *ParseCoverage `json:"parse_coverage,omitempty"`
}

// ParseError records a file that failed to parse.
type ParseError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Index is a structural snapshot of a codebase containing file summaries and parse errors.
type Index struct {
	Version      string            `json:"version"`
	Root         string            `json:"root"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Files        []FileSummary     `json:"files"`
	Errors       []ParseError      `json:"errors,omitempty"`
	ConfigHashes map[string]string `json:"config_hashes,omitempty"`
}

// FileCount returns the number of successfully parsed files in the index.
func (idx *Index) FileCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.Files)
}

// SymbolCount returns the total number of symbols across all files in the index.
func (idx *Index) SymbolCount() int {
	if idx == nil {
		return 0
	}

	total := 0
	for _, file := range idx.Files {
		total += len(file.Symbols)
	}
	return total
}

// ReferenceCount returns the total number of references across all files in the index.
func (idx *Index) ReferenceCount() int {
	if idx == nil {
		return 0
	}

	total := 0
	for _, file := range idx.Files {
		total += len(file.References)
	}
	return total
}

// GeneratedFileCount returns the number of files tagged as generated.
func (idx *Index) GeneratedFileCount() int {
	if idx == nil {
		return 0
	}
	count := 0
	for _, f := range idx.Files {
		if f.Generated != nil {
			count++
		}
	}
	return count
}

// WithoutGenerated returns a shallow copy of the index with generated files removed.
func (idx *Index) WithoutGenerated() *Index {
	if idx == nil {
		return nil
	}
	filtered := *idx
	filtered.Files = make([]FileSummary, 0, len(idx.Files))
	for _, f := range idx.Files {
		if f.Generated == nil {
			filtered.Files = append(filtered.Files, f)
		}
	}
	return &filtered
}

// ExcludePaths returns a shallow copy of the index with files matching any of
// the given gitignore-style patterns removed. This allows post-hoc exclusion
// on a cached index without rebuilding from scratch.
func (idx *Index) ExcludePaths(patterns []string) *Index {
	if idx == nil || len(patterns) == 0 {
		return idx
	}
	matcher := ignore.ParsePatterns(patterns)
	filtered := *idx
	filtered.Files = make([]FileSummary, 0, len(idx.Files))
	for _, f := range idx.Files {
		if !matcher.Match(f.Path, false) {
			filtered.Files = append(filtered.Files, f)
		}
	}
	return &filtered
}

// FilterByGenerator returns a copy with only files matching the given generator.
// "human" matches files with nil Generated.
func (idx *Index) FilterByGenerator(name string) *Index {
	if idx == nil {
		return nil
	}
	filtered := *idx
	filtered.Files = make([]FileSummary, 0)
	for _, f := range idx.Files {
		if name == "human" && f.Generated == nil {
			filtered.Files = append(filtered.Files, f)
		} else if f.Generated != nil && f.Generated.Generator == name {
			filtered.Files = append(filtered.Files, f)
		}
	}
	return &filtered
}
