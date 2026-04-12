// Package stats aggregates codebase metrics from a structural index: symbol kinds, language distribution, and file density.
package stats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

type Options struct {
	TopFiles int
}

type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type LanguageCount struct {
	Language string `json:"language"`
	Files    int    `json:"files"`
	Symbols  int    `json:"symbols"`
}

type GeneratorCount struct {
	Generator string `json:"generator"`
	Files     int    `json:"files"`
	Symbols   int    `json:"symbols"`
}

type FileMetric struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	Symbols   int    `json:"symbols"`
	Imports   int    `json:"imports"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type Report struct {
	Root               string          `json:"root"`
	FileCount          int             `json:"file_count"`
	GeneratedFileCount int             `json:"generated_file_count"`
	SymbolCount        int             `json:"symbol_count"`
	ParseErrorCount    int             `json:"parse_error_count"`
	KindCounts         []KindCount     `json:"kind_counts,omitempty"`
	Languages          []LanguageCount  `json:"languages,omitempty"`
	Generators         []GeneratorCount `json:"generators,omitempty"`
	TopFiles           []FileMetric     `json:"top_files,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}
	if opts.TopFiles <= 0 {
		opts.TopFiles = 10
	}

	kindCounts := map[string]int{}
	type langAgg struct {
		files   int
		symbols int
	}
	languages := map[string]*langAgg{}
	type genAgg struct {
		files   int
		symbols int
	}
	generators := map[string]*genAgg{}
	fileMetrics := make([]FileMetric, 0, len(idx.Files))

	for _, file := range idx.Files {
		lang := strings.TrimSpace(file.Language)
		if lang == "" {
			lang = "unknown"
		}
		entry, ok := languages[lang]
		if !ok {
			entry = &langAgg{}
			languages[lang] = entry
		}
		entry.files++
		entry.symbols += len(file.Symbols)

		if file.Generated != nil {
			g, ok := generators[file.Generated.Generator]
			if !ok {
				g = &genAgg{}
				generators[file.Generated.Generator] = g
			}
			g.files++
			g.symbols += len(file.Symbols)
		}

		for _, symbol := range file.Symbols {
			kindCounts[symbol.Kind]++
		}

		fileMetrics = append(fileMetrics, FileMetric{
			Path:      file.Path,
			Language:  lang,
			Symbols:   len(file.Symbols),
			Imports:   len(file.Imports),
			SizeBytes: file.SizeBytes,
		})
	}

	kindList := make([]KindCount, 0, len(kindCounts))
	for kind, count := range kindCounts {
		kindList = append(kindList, KindCount{Kind: kind, Count: count})
	}
	sort.Slice(kindList, func(i, j int) bool {
		if kindList[i].Count == kindList[j].Count {
			return kindList[i].Kind < kindList[j].Kind
		}
		return kindList[i].Count > kindList[j].Count
	})

	languageList := make([]LanguageCount, 0, len(languages))
	for lang, aggregate := range languages {
		languageList = append(languageList, LanguageCount{
			Language: lang,
			Files:    aggregate.files,
			Symbols:  aggregate.symbols,
		})
	}
	sort.Slice(languageList, func(i, j int) bool {
		if languageList[i].Files == languageList[j].Files {
			return languageList[i].Language < languageList[j].Language
		}
		return languageList[i].Files > languageList[j].Files
	})

	generatorList := make([]GeneratorCount, 0, len(generators))
	for gen, agg := range generators {
		generatorList = append(generatorList, GeneratorCount{Generator: gen, Files: agg.files, Symbols: agg.symbols})
	}
	sort.Slice(generatorList, func(i, j int) bool {
		if generatorList[i].Files == generatorList[j].Files {
			return generatorList[i].Generator < generatorList[j].Generator
		}
		return generatorList[i].Files > generatorList[j].Files
	})

	sort.Slice(fileMetrics, func(i, j int) bool {
		if fileMetrics[i].Symbols == fileMetrics[j].Symbols {
			return fileMetrics[i].Path < fileMetrics[j].Path
		}
		return fileMetrics[i].Symbols > fileMetrics[j].Symbols
	})
	if opts.TopFiles < len(fileMetrics) {
		fileMetrics = fileMetrics[:opts.TopFiles]
	}

	report := Report{
		Root:               idx.Root,
		FileCount:          len(idx.Files),
		GeneratedFileCount: idx.GeneratedFileCount(),
		SymbolCount:        idx.SymbolCount(),
		ParseErrorCount:    len(idx.Errors),
		KindCounts:         kindList,
		Languages:          languageList,
		Generators:         generatorList,
		TopFiles:           fileMetrics,
	}
	return report, nil
}
