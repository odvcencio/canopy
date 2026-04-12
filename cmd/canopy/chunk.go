package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/internal/chunk"
	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/model"
)

func newChunkCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var tokens int
	var jsonOutput bool
	var lang string
	var countOnly bool
	var format string

	cmd := &cobra.Command{
		Use:     "chunk [path]",
		Aliases: []string{"gtschunk"},
		Short:   "Split code into AST-boundary chunks for RAG/indexing",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tokens <= 0 {
				return fmt.Errorf("tokens must be > 0")
			}

			target := "."
			filter := ""
			if len(args) == 1 {
				target = args[0]
				if strings.TrimSpace(cachePath) != "" {
					filter = target
				}
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			idx = applyGeneratedFilter(cmd, idx)

			if lang != "" {
				filtered := idx.Files[:0]
				for _, f := range idx.Files {
					if strings.EqualFold(f.Language, lang) {
						filtered = append(filtered, f)
					}
				}
				idx.Files = filtered
			}

			report, err := chunk.Build(idx, chunk.Options{
				TokenBudget: tokens,
				FilterPath:  filter,
			})
			if err != nil {
				return err
			}

			if countOnly {
				fmt.Println(report.ChunkCount)
				return nil
			}

			// Embeddings format: JSONL with metadata per chunk.
			if format == "embeddings" {
				return emitEmbeddingsFormat(idx, report)
			}

			if jsonOutput {
				return emitJSON(report)
			}

			fmt.Printf("chunks: %d budget=%d root=%s\n", report.ChunkCount, report.TokenBudget, report.Root)
			for _, item := range report.Chunks {
				suffix := ""
				if item.Truncated {
					suffix = " truncated=true"
				}
				fmt.Printf(
					"%s:%d:%d %s %s tokens=%d%s\n",
					item.File,
					item.StartLine,
					item.EndLine,
					item.Kind,
					strings.TrimSpace(item.Name),
					item.Tokens,
					suffix,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().IntVar(&tokens, "tokens", 800, "token budget per chunk")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&lang, "lang", "", "filter by file language (e.g. go, python, typescript)")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print only the count of chunks")
	cmd.Flags().StringVar(&format, "format", "", "output format: embeddings (JSONL with metadata per chunk)")
	return cmd
}

type embeddingChunk struct {
	Content  string          `json:"content"`
	Metadata embeddingMeta   `json:"metadata"`
}

type embeddingMeta struct {
	File       string   `json:"file"`
	Language   string   `json:"language"`
	Symbols    []string `json:"symbols"`
	Complexity int      `json:"complexity,omitempty"`
}

func emitEmbeddingsFormat(idx *model.Index, report chunk.Report) error {
	// Build file language lookup.
	fileLang := make(map[string]string, len(idx.Files))
	for _, f := range idx.Files {
		fileLang[f.Path] = f.Language
	}

	// Build per-file complexity lookup for callable symbols.
	compMap := make(map[string]int) // "file\x00name\x00startLine" -> cyclomatic
	compReport, compErr := complexity.Analyze(idx, idx.Root, complexity.Options{})
	if compErr == nil && compReport != nil {
		for _, fn := range compReport.Functions {
			key := fmt.Sprintf("%s\x00%s\x00%d", fn.File, fn.Name, fn.StartLine)
			compMap[key] = fn.Cyclomatic
		}
	}

	enc := json.NewEncoder(os.Stdout)
	for _, c := range report.Chunks {
		symbols := []string{}
		name := strings.TrimSpace(c.Name)
		if name != "" && name != filepath.Base(c.File) {
			symbols = append(symbols, name)
		}

		// Look up complexity for this chunk's symbol.
		cyc := 0
		if name != "" {
			key := fmt.Sprintf("%s\x00%s\x00%d", c.File, name, c.StartLine)
			if v, ok := compMap[key]; ok {
				cyc = v
			}
		}

		entry := embeddingChunk{
			Content: c.Content,
			Metadata: embeddingMeta{
				File:       c.File,
				Language:   fileLang[c.File],
				Symbols:    symbols,
				Complexity: cyc,
			},
		}
		if err := enc.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}

func runChunk(args []string) error {
	cmd := newChunkCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
