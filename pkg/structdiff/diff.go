// Package structdiff compares two structural indexes to detect added, removed, and modified symbols and imports.
package structdiff

import (
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

type SymbolRef struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type ModifiedSymbol struct {
	Before SymbolRef `json:"before"`
	After  SymbolRef `json:"after"`
	Fields []string  `json:"fields"`
}

type FileImportChange struct {
	File    string   `json:"file"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

type Stats struct {
	AddedSymbols    int `json:"added_symbols"`
	RemovedSymbols  int `json:"removed_symbols"`
	ModifiedSymbols int `json:"modified_symbols"`
	ChangedFiles    int `json:"changed_files"`
}

type Report struct {
	BeforeRoot      string             `json:"before_root"`
	AfterRoot       string             `json:"after_root"`
	AddedSymbols    []SymbolRef        `json:"added_symbols,omitempty"`
	RemovedSymbols  []SymbolRef        `json:"removed_symbols,omitempty"`
	ModifiedSymbols []ModifiedSymbol   `json:"modified_symbols,omitempty"`
	ImportChanges   []FileImportChange `json:"import_changes,omitempty"`
	Stats           Stats              `json:"stats"`
}

func Compare(before, after *model.Index) Report {
	report := Report{}
	if before != nil {
		report.BeforeRoot = before.Root
	}
	if after != nil {
		report.AfterRoot = after.Root
	}

	beforeSymbols := flattenSymbols(before)
	afterSymbols := flattenSymbols(after)

	for key, afterSymbol := range afterSymbols {
		beforeSymbol, exists := beforeSymbols[key]
		if !exists {
			report.AddedSymbols = append(report.AddedSymbols, toSymbolRef(afterSymbol))
			continue
		}

		fields := changedFields(beforeSymbol, afterSymbol)
		if len(fields) == 0 {
			continue
		}

		report.ModifiedSymbols = append(report.ModifiedSymbols, ModifiedSymbol{
			Before: toSymbolRef(beforeSymbol),
			After:  toSymbolRef(afterSymbol),
			Fields: fields,
		})
	}

	for key, beforeSymbol := range beforeSymbols {
		if _, exists := afterSymbols[key]; exists {
			continue
		}
		report.RemovedSymbols = append(report.RemovedSymbols, toSymbolRef(beforeSymbol))
	}

	report.ImportChanges = compareImports(before, after)
	sortSymbolRefs(report.AddedSymbols)
	sortSymbolRefs(report.RemovedSymbols)
	sort.Slice(report.ModifiedSymbols, func(i, j int) bool {
		left := report.ModifiedSymbols[i].After
		right := report.ModifiedSymbols[j].After
		if left.File == right.File {
			if left.StartLine == right.StartLine {
				if left.Kind == right.Kind {
					return left.Name < right.Name
				}
				return left.Kind < right.Kind
			}
			return left.StartLine < right.StartLine
		}
		return left.File < right.File
	})

	report.Stats = Stats{
		AddedSymbols:    len(report.AddedSymbols),
		RemovedSymbols:  len(report.RemovedSymbols),
		ModifiedSymbols: len(report.ModifiedSymbols),
		ChangedFiles:    countChangedFiles(report),
	}
	return report
}

func flattenSymbols(idx *model.Index) map[string]model.Symbol {
	flat := make(map[string]model.Symbol, symbolCapacity(idx))
	if idx == nil {
		return flat
	}

	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			flat[symbolKey(symbol)] = symbol
		}
	}
	return flat
}

func symbolCapacity(idx *model.Index) int {
	if idx == nil {
		return 0
	}
	total := 0
	for _, file := range idx.Files {
		total += len(file.Symbols)
	}
	return total
}

func symbolKey(symbol model.Symbol) string {
	return symbol.File + "|" + symbol.Kind + "|" + symbol.Receiver + "|" + symbol.Name
}

func toSymbolRef(symbol model.Symbol) SymbolRef {
	return SymbolRef{
		File:      symbol.File,
		Kind:      symbol.Kind,
		Name:      symbol.Name,
		Signature: symbol.Signature,
		Receiver:  symbol.Receiver,
		StartLine: symbol.StartLine,
		EndLine:   symbol.EndLine,
	}
}

func changedFields(before, after model.Symbol) []string {
	fields := make([]string, 0, 2)
	if before.Signature != after.Signature {
		fields = append(fields, "signature")
	}
	if before.StartLine != after.StartLine || before.EndLine != after.EndLine {
		fields = append(fields, "span")
	}
	return fields
}

func sortSymbolRefs(items []SymbolRef) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].File == items[j].File {
			if items[i].StartLine == items[j].StartLine {
				if items[i].Kind == items[j].Kind {
					return items[i].Name < items[j].Name
				}
				return items[i].Kind < items[j].Kind
			}
			return items[i].StartLine < items[j].StartLine
		}
		return items[i].File < items[j].File
	})
}

func compareImports(before, after *model.Index) []FileImportChange {
	beforeFiles := map[string]model.FileSummary{}
	afterFiles := map[string]model.FileSummary{}

	if before != nil {
		for _, file := range before.Files {
			beforeFiles[file.Path] = file
		}
	}
	if after != nil {
		for _, file := range after.Files {
			afterFiles[file.Path] = file
		}
	}

	keys := make([]string, 0, len(beforeFiles)+len(afterFiles))
	keySeen := map[string]bool{}
	for key := range beforeFiles {
		if !keySeen[key] {
			keySeen[key] = true
			keys = append(keys, key)
		}
	}
	for key := range afterFiles {
		if !keySeen[key] {
			keySeen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	changes := make([]FileImportChange, 0, len(keys))
	for _, key := range keys {
		beforeImports := importSet(beforeFiles[key].Imports)
		afterImports := importSet(afterFiles[key].Imports)

		added := make([]string, 0, 4)
		removed := make([]string, 0, 4)
		for imp := range afterImports {
			if !beforeImports[imp] {
				added = append(added, imp)
			}
		}
		for imp := range beforeImports {
			if !afterImports[imp] {
				removed = append(removed, imp)
			}
		}

		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		sort.Strings(added)
		sort.Strings(removed)

		changes = append(changes, FileImportChange{
			File:    key,
			Added:   added,
			Removed: removed,
		})
	}
	return changes
}

func importSet(imports []string) map[string]bool {
	set := make(map[string]bool, len(imports))
	for _, imp := range imports {
		trimmed := strings.TrimSpace(imp)
		if trimmed == "" {
			continue
		}
		set[trimmed] = true
	}
	return set
}

func countChangedFiles(report Report) int {
	seen := map[string]bool{}
	for _, item := range report.AddedSymbols {
		seen[item.File] = true
	}
	for _, item := range report.RemovedSymbols {
		seen[item.File] = true
	}
	for _, item := range report.ModifiedSymbols {
		seen[item.After.File] = true
	}
	for _, item := range report.ImportChanges {
		seen[item.File] = true
	}
	return len(seen)
}
