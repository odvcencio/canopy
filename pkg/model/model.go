// Package model defines the core data types for structural code indexing: Symbol, Reference, FileSummary, and Index.
package model

import (
	"time"

	"github.com/odvcencio/canopy/pkg/ignore"
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
