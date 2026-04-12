// Package chunk splits source files into AST-boundary chunks with token budget estimation for LLM context windows.
package chunk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

type Options struct {
	TokenBudget int
	FilterPath  string
}

type Chunk struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Tokens    int    `json:"tokens"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}

type Report struct {
	Root        string  `json:"root"`
	TokenBudget int     `json:"token_budget"`
	ChunkCount  int     `json:"chunk_count"`
	Chunks      []Chunk `json:"chunks,omitempty"`
}

func Build(idx *model.Index, opts Options) (Report, error) {
	if idx == nil {
		return Report{}, fmt.Errorf("index is nil")
	}
	if opts.TokenBudget <= 0 {
		opts.TokenBudget = 800
	}

	filter := normalizeFilter(opts.FilterPath)
	report := Report{
		Root:        idx.Root,
		TokenBudget: opts.TokenBudget,
	}

	for _, file := range idx.Files {
		if !matchesFilter(file.Path, filter) {
			continue
		}

		absPath := filepath.Join(idx.Root, filepath.FromSlash(file.Path))
		source, err := os.ReadFile(absPath)
		if err != nil {
			return Report{}, err
		}
		lines := splitLines(string(source))

		if len(file.Symbols) == 0 {
			single := makeChunk(file.Path, "file", filepath.Base(file.Path), lines, 1, len(lines), opts.TokenBudget)
			report.Chunks = append(report.Chunks, single)
			continue
		}

		firstStart := file.Symbols[0].StartLine
		for _, symbol := range file.Symbols {
			if symbol.StartLine > 0 && symbol.StartLine < firstStart {
				firstStart = symbol.StartLine
			}
		}

		if firstStart > 1 {
			header := makeChunk(file.Path, "file_header", filepath.Base(file.Path), lines, 1, firstStart-1, opts.TokenBudget)
			if strings.TrimSpace(header.Content) != "" {
				report.Chunks = append(report.Chunks, header)
			}
		}

		for _, symbol := range file.Symbols {
			name := symbol.Name
			if strings.TrimSpace(symbol.Signature) != "" {
				name = symbol.Signature
			}
			chunk := makeChunk(
				file.Path,
				symbol.Kind,
				name,
				lines,
				symbol.StartLine,
				symbol.EndLine,
				opts.TokenBudget,
			)
			report.Chunks = append(report.Chunks, chunk)
		}
	}

	sort.Slice(report.Chunks, func(i, j int) bool {
		if report.Chunks[i].File == report.Chunks[j].File {
			if report.Chunks[i].StartLine == report.Chunks[j].StartLine {
				return report.Chunks[i].Kind < report.Chunks[j].Kind
			}
			return report.Chunks[i].StartLine < report.Chunks[j].StartLine
		}
		return report.Chunks[i].File < report.Chunks[j].File
	})
	report.ChunkCount = len(report.Chunks)
	return report, nil
}

func normalizeFilter(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "." {
		return ""
	}
	normalized := filepath.ToSlash(filepath.Clean(trimmed))
	if normalized == "." {
		return ""
	}
	return normalized
}

func matchesFilter(filePath, filter string) bool {
	if filter == "" {
		return true
	}
	normalized := filepath.ToSlash(filepath.Clean(filePath))
	if normalized == filter {
		return true
	}
	return strings.HasPrefix(normalized, filter+"/")
}

func splitLines(src string) []string {
	lines := strings.Split(src, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func makeChunk(file, kind, name string, lines []string, start, end, budget int) Chunk {
	if len(lines) == 0 {
		lines = []string{""}
	}
	start = clampLine(start, len(lines))
	end = clampLine(end, len(lines))
	if end < start {
		end = start
	}

	render := func(startLine, endLine int, truncateLastLine bool) string {
		sliced := append([]string(nil), lines[startLine-1:endLine]...)
		if truncateLastLine && len(sliced) > 0 {
			last := len(sliced) - 1
			maxChars := budget * 4
			if maxChars > 0 && len(sliced[last]) > maxChars {
				sliced[last] = sliced[last][:maxChars]
			}
		}
		text := strings.Join(sliced, "\n")
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return text
	}

	content := render(start, end, false)
	tokens := estimateTokens(content)
	truncated := false

	for budget > 0 && start < end && tokens > budget {
		end--
		content = render(start, end, false)
		tokens = estimateTokens(content)
		truncated = true
	}

	if budget > 0 && tokens > budget {
		content = render(start, end, true)
		tokens = estimateTokens(content)
		truncated = true
	}

	return Chunk{
		File:      file,
		Kind:      kind,
		Name:      name,
		StartLine: start,
		EndLine:   end,
		Tokens:    tokens,
		Truncated: truncated,
		Content:   content,
	}
}

func clampLine(line, totalLines int) int {
	if line < 1 {
		return 1
	}
	if line > totalLines {
		return totalLines
	}
	return line
}

func estimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return (len(trimmed) + 3) / 4
}
