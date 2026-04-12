// Package files lists and filters indexed source files by language, symbol count, and other structural density metrics.
package files

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

type Options struct {
	Language   string
	MinSymbols int
	SortBy     string
	Top        int
}

type Entry struct {
	Path      string              `json:"path"`
	Language  string              `json:"language"`
	Symbols   int                 `json:"symbols"`
	Imports   int                 `json:"imports"`
	SizeBytes int64               `json:"size_bytes,omitempty"`
	Generated *model.GeneratedInfo `json:"generated,omitempty"`
}

type Report struct {
	Root       string  `json:"root"`
	TotalFiles int     `json:"total_files"`
	ShownFiles int     `json:"shown_files"`
	Entries    []Entry `json:"entries,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}
	if opts.MinSymbols < 0 {
		opts.MinSymbols = 0
	}
	if opts.Top <= 0 {
		opts.Top = 50
	}
	sortBy := strings.ToLower(strings.TrimSpace(opts.SortBy))
	if sortBy == "" {
		sortBy = "symbols"
	}
	switch sortBy {
	case "symbols", "imports", "size", "path":
	default:
		return Report{}, fmt.Errorf("unsupported sort %q", opts.SortBy)
	}

	languageFilter := strings.ToLower(strings.TrimSpace(opts.Language))
	entries := make([]Entry, 0, len(idx.Files))
	for _, file := range idx.Files {
		language := strings.ToLower(strings.TrimSpace(file.Language))
		if languageFilter != "" && language != languageFilter {
			continue
		}
		if len(file.Symbols) < opts.MinSymbols {
			continue
		}
		entries = append(entries, Entry{
			Path:      file.Path,
			Language:  file.Language,
			Symbols:   len(file.Symbols),
			Imports:   len(file.Imports),
			SizeBytes: file.SizeBytes,
			Generated: file.Generated,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		switch sortBy {
		case "path":
			return entries[i].Path < entries[j].Path
		case "imports":
			if entries[i].Imports == entries[j].Imports {
				return entries[i].Path < entries[j].Path
			}
			return entries[i].Imports > entries[j].Imports
		case "size":
			if entries[i].SizeBytes == entries[j].SizeBytes {
				return entries[i].Path < entries[j].Path
			}
			return entries[i].SizeBytes > entries[j].SizeBytes
		default:
			if entries[i].Symbols == entries[j].Symbols {
				return entries[i].Path < entries[j].Path
			}
			return entries[i].Symbols > entries[j].Symbols
		}
	})

	if opts.Top < len(entries) {
		entries = entries[:opts.Top]
	}

	return Report{
		Root:       idx.Root,
		TotalFiles: len(idx.Files),
		ShownFiles: len(entries),
		Entries:    entries,
	}, nil
}
