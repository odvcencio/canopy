package refactor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/query"
)

// renameTargets holds the context collected during the target-matching phase.
type renameTargets struct {
	byFile      map[string][]model.Symbol
	kindsByName map[string]string
	dirs        map[string]bool
}

func renameDeclarationsTreeSitter(idx *model.Index, selector query.Selector, newName string, opts Options) (Report, error) {
	report := Report{
		Root:                  idx.Root,
		Selector:              selector.Raw,
		NewName:               newName,
		Engine:                "treesitter",
		Write:                 opts.Write,
		UpdateCallsites:       opts.UpdateCallsites,
		CrossPackageCallsites: opts.CrossPackageCallsites,
	}

	targets := collectRenameTargets(idx, selector, newName, &report)
	if len(targets.byFile) == 0 {
		return report, nil
	}

	plannedByFile, absByFile, sourceByFile, targetMatched, err := planRenameEdits(idx, targets, newName, opts, &report)
	if err != nil {
		return report, err
	}

	appendUnmatchedTargets(targets, targetMatched, newName, &report)

	if err := applyPlannedEdits(plannedByFile, absByFile, sourceByFile, opts, &report); err != nil {
		return report, err
	}

	sortReportEdits(&report)
	return report, nil
}

func collectRenameTargets(idx *model.Index, selector query.Selector, newName string, report *Report) renameTargets {
	targets := renameTargets{
		byFile:      map[string][]model.Symbol{},
		kindsByName: map[string]string{},
		dirs:        map[string]bool{},
	}

	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			if !selector.Match(symbol) {
				continue
			}
			report.MatchCount++
			if !supportsDeclarationRename(symbol.Kind) {
				report.Edits = append(report.Edits, Edit{
					File:     symbol.File,
					Kind:     symbol.Kind,
					Category: "declaration",
					OldName:  symbol.Name,
					NewName:  newName,
					Line:     symbol.StartLine,
					Column:   1,
					Skipped:  true,
					SkipNote: "unsupported kind for declaration rename",
				})
				continue
			}
			if symbol.Name == newName {
				report.Edits = append(report.Edits, Edit{
					File:     symbol.File,
					Kind:     symbol.Kind,
					Category: "declaration",
					OldName:  symbol.Name,
					NewName:  newName,
					Line:     symbol.StartLine,
					Column:   1,
					Skipped:  true,
					SkipNote: "already has target name",
				})
				continue
			}
			targets.byFile[symbol.File] = append(targets.byFile[symbol.File], symbol)
			targets.kindsByName[symbol.Name] = symbol.Kind
			targets.dirs[packageFromFilePath(symbol.File)] = true
		}
	}
	return targets
}

func planRenameEdits(idx *model.Index, targets renameTargets, newName string, opts Options, report *Report) (map[string][]Edit, map[string]string, map[string][]byte, map[string]bool, error) {
	entriesByExt := languageEntriesByExt()
	taggerByLanguage := map[string]*gotreesitter.Tagger{}

	plannedByFile := map[string][]Edit{}
	absByFile := map[string]string{}
	sourceByFile := map[string][]byte{}
	seen := map[string]bool{}
	targetMatched := map[string]bool{}

	for _, file := range idx.Files {
		relPath := filepath.ToSlash(filepath.Clean(file.Path))
		hasTargets := len(targets.byFile[relPath]) > 0
		inTargetDir := targets.dirs[packageFromFilePath(relPath)]
		if !hasTargets {
			if !opts.UpdateCallsites {
				continue
			}
			if !opts.CrossPackageCallsites && !inTargetDir {
				continue
			}
		}

		entry, ok := entriesByExt[strings.ToLower(filepath.Ext(relPath))]
		if !ok {
			continue
		}
		entry.TagsQuery = grammars.ResolveTagsQuery(entry)
		if strings.TrimSpace(entry.TagsQuery) == "" {
			continue
		}

		absPath := filepath.Join(idx.Root, filepath.FromSlash(relPath))
		source, err := os.ReadFile(absPath)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		absByFile[relPath] = absPath
		sourceByFile[relPath] = source

		tagger, err := treeSitterTagger(entry, taggerByLanguage)
		if err != nil {
			continue
		}
		collectTagEdits(tagger.Tag(source), relPath, hasTargets, targets, newName, opts, plannedByFile, seen, targetMatched, report)
	}

	return plannedByFile, absByFile, sourceByFile, targetMatched, nil
}

func collectTagEdits(tags []gotreesitter.Tag, relPath string, hasTargets bool, targets renameTargets, newName string, opts Options, plannedByFile map[string][]Edit, seen map[string]bool, targetMatched map[string]bool, report *Report) {
	for _, tag := range tags {
		if tag.NameRange.StartByte >= tag.NameRange.EndByte {
			continue
		}
		name := strings.TrimSpace(tag.Name)
		if name == "" || name == newName {
			continue
		}

		line := int(tag.NameRange.StartPoint.Row) + 1
		column := int(tag.NameRange.StartPoint.Column) + 1
		offset := int(tag.NameRange.StartByte)

		if kind, ok := declarationKindFromTag(tag.Kind); ok {
			if !hasTargets {
				continue
			}
			for _, target := range targets.byFile[relPath] {
				if target.Kind != kind || target.Name != name {
					continue
				}
				if line < target.StartLine || line > target.EndLine {
					continue
				}
				edit := Edit{
					File:     relPath,
					Kind:     target.Kind,
					Category: "declaration",
					OldName:  name,
					NewName:  newName,
					Line:     line,
					Column:   column,
					Offset:   offset,
				}
				if appendPlannedEdit(plannedByFile, seen, edit) {
					report.PlannedDeclEdits++
				}
				targetMatched[targetMatchKey(target)] = true
			}
			continue
		}

		if !opts.UpdateCallsites || !strings.HasPrefix(tag.Kind, "reference.") {
			continue
		}
		kind, ok := targets.kindsByName[name]
		if !ok {
			continue
		}

		edit := Edit{
			File:     relPath,
			Kind:     kind,
			Category: "callsite",
			OldName:  name,
			NewName:  newName,
			Line:     line,
			Column:   column,
			Offset:   offset,
		}
		if appendPlannedEdit(plannedByFile, seen, edit) {
			report.PlannedUseEdits++
		}
	}
}

func appendUnmatchedTargets(targets renameTargets, targetMatched map[string]bool, newName string, report *Report) {
	for _, tgts := range targets.byFile {
		for _, target := range tgts {
			if targetMatched[targetMatchKey(target)] {
				continue
			}
			report.Edits = append(report.Edits, Edit{
				File:     target.File,
				Kind:     target.Kind,
				Category: "declaration",
				OldName:  target.Name,
				NewName:  newName,
				Line:     target.StartLine,
				Column:   1,
				Skipped:  true,
				SkipNote: "declaration tag not found in source",
			})
		}
	}
}

func applyPlannedEdits(plannedByFile map[string][]Edit, absByFile map[string]string, sourceByFile map[string][]byte, opts Options, report *Report) error {
	report.PlannedEdits = report.PlannedDeclEdits + report.PlannedUseEdits
	fileKeys := make([]string, 0, len(plannedByFile))
	for file := range plannedByFile {
		fileKeys = append(fileKeys, file)
	}
	sort.Strings(fileKeys)

	editIndexesByFile := map[string][]int{}
	for _, relPath := range fileKeys {
		edits := append([]Edit(nil), plannedByFile[relPath]...)
		sort.Slice(edits, func(i, j int) bool {
			if edits[i].Offset == edits[j].Offset {
				return edits[i].Category < edits[j].Category
			}
			return edits[i].Offset < edits[j].Offset
		})

		for _, edit := range edits {
			report.Edits = append(report.Edits, edit)
			editIndexesByFile[relPath] = append(editIndexesByFile[relPath], len(report.Edits)-1)
		}

		if !opts.Write || len(edits) == 0 {
			continue
		}
		updated, applied, err := applySourceEdits(sourceByFile[relPath], edits)
		if err != nil {
			return err
		}
		if applied == 0 {
			continue
		}
		if err := os.WriteFile(absByFile[relPath], updated, 0o644); err != nil {
			return err
		}
		report.ChangedFiles++
		report.AppliedEdits += applied
		for _, idx := range editIndexesByFile[relPath] {
			report.Edits[idx].Applied = true
		}
	}
	return nil
}

func sortReportEdits(report *Report) {
	sort.Slice(report.Edits, func(i, j int) bool {
		if report.Edits[i].File == report.Edits[j].File {
			if report.Edits[i].Line == report.Edits[j].Line {
				if report.Edits[i].Column == report.Edits[j].Column {
					return report.Edits[i].Category < report.Edits[j].Category
				}
				return report.Edits[i].Column < report.Edits[j].Column
			}
			return report.Edits[i].Line < report.Edits[j].Line
		}
		return report.Edits[i].File < report.Edits[j].File
	})
}

func treeSitterTagger(entry grammars.LangEntry, cache map[string]*gotreesitter.Tagger) (*gotreesitter.Tagger, error) {
	if tg, ok := cache[entry.Name]; ok {
		return tg, nil
	}
	if entry.Language == nil {
		return nil, fmt.Errorf("language loader unavailable for %s", entry.Name)
	}
	lang := entry.Language()
	if lang == nil {
		return nil, fmt.Errorf("language unavailable for %s", entry.Name)
	}

	var options []gotreesitter.TaggerOption
	if entry.TokenSourceFactory != nil {
		factory := entry.TokenSourceFactory
		options = append(options, gotreesitter.WithTaggerTokenSourceFactory(func(source []byte) gotreesitter.TokenSource {
			return factory(source, lang)
		}))
	}

	tagger, err := gotreesitter.NewTagger(lang, entry.TagsQuery, options...)
	if err != nil {
		return nil, err
	}
	cache[entry.Name] = tagger
	return tagger, nil
}

func appendPlannedEdit(plannedByFile map[string][]Edit, seen map[string]bool, edit Edit) bool {
	key := editKey(edit)
	if seen[key] {
		return false
	}
	seen[key] = true
	plannedByFile[edit.File] = append(plannedByFile[edit.File], edit)
	return true
}

func declarationKindFromTag(tagKind string) (string, bool) {
	if !strings.HasPrefix(tagKind, "definition.") {
		return "", false
	}
	switch strings.TrimPrefix(tagKind, "definition.") {
	case "function", "constructor":
		return "function_definition", true
	case "method":
		return "method_definition", true
	default:
		return "type_definition", true
	}
}

func targetMatchKey(symbol model.Symbol) string {
	return symbol.File + "|" + symbol.Kind + "|" + symbol.Name + "|" + strconv.Itoa(symbol.StartLine)
}

func languageEntriesByExt() map[string]grammars.LangEntry {
	entries := grammars.AllLanguages()
	byExt := make(map[string]grammars.LangEntry, len(entries))
	for _, entry := range entries {
		if entry.Language == nil {
			continue
		}
		for _, ext := range entry.Extensions {
			normalized := strings.ToLower(strings.TrimSpace(ext))
			if normalized == "" {
				continue
			}
			if normalized[0] != '.' {
				normalized = "." + normalized
			}
			if _, exists := byExt[normalized]; exists {
				continue
			}
			byExt[normalized] = entry
		}
	}
	return byExt
}
