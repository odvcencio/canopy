// Package outline builds bounded, live file outlines without a repository index.
package outline

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"m31labs.dev/canopy/pkg/lang/treesitter"
)

const (
	DefaultFileBytes   = 1 << 20
	DefaultOutputBytes = 6 << 10
	DefaultLimit       = 40
	MaxLimit           = 100
	maxNameBytes       = 256
)

type Options struct {
	Limit  int
	Cursor string
}

type Symbol struct {
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	NameTruncated  bool   `json:"name_truncated,omitempty"`
	Owner          string `json:"owner,omitempty"`
	OwnerTruncated bool   `json:"owner_truncated,omitempty"`
	StartLine      uint32 `json:"start_line"`
	EndLine        uint32 `json:"end_line"`
	Depth          int    `json:"depth"`
	ParentIndex    int    `json:"parent_index"`
}

type Coverage struct {
	ParseSkipped      bool           `json:"parse_skipped,omitempty"`
	ParseStoppedEarly bool           `json:"parse_stopped_early"`
	ParseStopReason   string         `json:"parse_stop_reason,omitempty"`
	DefinitionKinds   []string       `json:"definition_kinds"`
	TreeHasError      bool           `json:"tree_has_error"`
	QueryTruncated    bool           `json:"query_truncated"`
	DeclineReason     string         `json:"decline_reason,omitempty"`
	Omissions         map[string]int `json:"omissions,omitempty"`
	OwnerRuleMisses   int            `json:"owner_rule_misses,omitempty"`
}

type Report struct {
	FormatVersion int      `json:"format_version"`
	Path          string   `json:"path"`
	Language      string   `json:"language"`
	SourceSHA256  string   `json:"source_sha256"`
	SourceBytes   int      `json:"source_bytes"`
	Coverage      Coverage `json:"coverage"`
	TotalSymbols  int      `json:"total_symbols"`
	Offset        int      `json:"offset"`
	Symbols       []Symbol `json:"symbols"`
	NextCursor    string   `json:"next_cursor,omitempty"`
}

func ReadFile(ctx context.Context, path string, opts Options) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat outline file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("outline requires a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open outline file: %w", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat outline file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("outline requires a regular file")
	}
	if info.Size() > DefaultFileBytes {
		return nil, fmt.Errorf("outline file exceeds %d bytes; narrow the source file", DefaultFileBytes)
	}
	src, err := io.ReadAll(io.LimitReader(file, DefaultFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read outline file: %w", err)
	}
	if len(src) > DefaultFileBytes {
		return nil, fmt.Errorf("outline file exceeds %d bytes", DefaultFileBytes)
	}
	entry := grammars.DetectLanguage(path)
	if entry == nil {
		return nil, fmt.Errorf("outline language not supported for %q", path)
	}
	language := entry.Language()
	query := treesitter.ResolveTagsQuery(*entry)
	outliner, err := gts.NewOutliner(language, query, gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(*entry)), gts.WithOutlineMatchLimit(4096), gts.WithOutlineMatchWorkBudget(20000))
	if err != nil {
		return nil, fmt.Errorf("build outline query: %w", err)
	}
	var tree *gts.Tree
	if !outliner.QueryEmpty() {
		tree, err = parse(ctx, src, *entry, language)
		if tree != nil {
			defer tree.Release()
		}
		if err != nil {
			return nil, fmt.Errorf("parse outline file: %w", err)
		}
	}
	symbols, receipt := outliner.OutlineTree(tree)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	sum := sha256.Sum256(src)
	report := &Report{FormatVersion: 1, Path: path, Language: entry.Name, SourceSHA256: hex.EncodeToString(sum[:]), SourceBytes: len(src), Coverage: Coverage{DefinitionKinds: outliner.DefinitionKinds(), TreeHasError: receipt.TreeHasError, QueryTruncated: receipt.Truncated, DeclineReason: receipt.DeclineReason, OwnerRuleMisses: receipt.OwnerRuleMisses}}
	report.Coverage.ParseSkipped = outliner.QueryEmpty()
	if tree != nil && tree.ParseStoppedEarly() {
		report.Coverage.ParseStoppedEarly = true
		report.Coverage.ParseStopReason = string(tree.ParseRuntime().StopReason)
	}
	report.Coverage.Omissions = map[string]int{}
	for name, count := range map[string]int{"no_name": receipt.OmittedNoName, "duplicate": receipt.OmittedDuplicate, "name_conflict": receipt.OmittedNameConflict, "kind_conflict": receipt.OmittedConflict, "overlap": receipt.OmittedOverlap, "invalid_name_range": receipt.OmittedInvalidNameRange, "multiple_definitions": receipt.OmittedMultipleDefinitions} {
		if count > 0 {
			report.Coverage.Omissions[name] = count
		}
	}
	var flat []Symbol
	var flatten func([]gts.OutlineSymbol, int, int)
	flatten = func(nodes []gts.OutlineSymbol, depth, parent int) {
		for _, sym := range nodes {
			name, nameCut := boundedName(sym.Name)
			owner, ownerCut := boundedName(sym.Owner)
			end := sym.Range.EndPoint.Row + 1
			if sym.Range.EndPoint.Column == 0 && sym.Range.EndPoint.Row > sym.Range.StartPoint.Row {
				end--
			}
			index := len(flat)
			flat = append(flat, Symbol{Kind: sym.Kind, Name: name, NameTruncated: nameCut, Owner: owner, OwnerTruncated: ownerCut, StartLine: sym.Range.StartPoint.Row + 1, EndLine: end, Depth: depth, ParentIndex: parent})
			flatten(sym.Children, depth+1, index)
		}
	}
	flatten(symbols, 0, -1)
	report.TotalSymbols = len(flat)
	identity := sha256.Sum256([]byte(filepath.Clean(path) + "\x00" + entry.Name + "\x00" + query + "\x00" + report.SourceSHA256))
	prefix := base64.RawURLEncoding.EncodeToString(identity[:16]) + "."
	if opts.Cursor != "" {
		if !strings.HasPrefix(opts.Cursor, prefix) {
			return nil, fmt.Errorf("outline cursor does not match this file, source, or query; restart without cursor")
		}
		report.Offset, err = strconv.Atoi(strings.TrimPrefix(opts.Cursor, prefix))
		if err != nil || report.Offset < 0 || report.Offset >= len(flat) {
			return nil, fmt.Errorf("invalid outline cursor offset")
		}
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	report.Symbols = make([]Symbol, 0, min(limit, len(flat)-report.Offset))
	for next := report.Offset; next < len(flat) && len(report.Symbols) < limit; next++ {
		report.Symbols = append(report.Symbols, flat[next])
		report.NextCursor = ""
		if next+1 < len(flat) {
			report.NextCursor = prefix + strconv.Itoa(next+1)
		}
		encoded, _ := json.Marshal(report)
		if len(encoded) > DefaultOutputBytes {
			report.Symbols = report.Symbols[:len(report.Symbols)-1]
			if len(report.Symbols) == 0 {
				return nil, fmt.Errorf("outline metadata or symbol exceeds output budget")
			}
			report.NextCursor = prefix + strconv.Itoa(next)
			break
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if len(encoded) > DefaultOutputBytes {
		return nil, fmt.Errorf("outline metadata exceeds output budget")
	}
	return report, nil
}

func boundedName(name string) (string, bool) {
	name = strings.ToValidUTF8(name, "�")
	if len(name) <= maxNameBytes {
		return name, false
	}
	keep := maxNameBytes
	for keep > 0 && !utf8.RuneStart(name[keep]) {
		keep--
	}
	return strings.Clone(name[:keep]), true
}
