package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/odvcencio/canopy/internal/federation"
)

func newExportCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var output string
	var name string

	cmd := &cobra.Command{
		Use:   "export [path]",
		Short: "Export structural index to a portable .canopyindex file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			absTarget, err := filepath.Abs(target)
			if err != nil {
				return err
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			repoName := strings.TrimSpace(name)
			if repoName == "" {
				repoName = filepath.Base(absTarget)
			}

			outPath := strings.TrimSpace(output)
			if outPath == "" {
				outPath = repoName + ".canopyindex"
			}

			exported := &federation.ExportedIndex{
				RepoURL:    gitRemoteURL(absTarget),
				RepoName:   repoName,
				CommitSHA:  gitHeadSHA(absTarget),
				ExportedAt: time.Now(),
				Index:      *idx,
			}

			if err := federation.Save(outPath, exported); err != nil {
				return err
			}

			fmt.Printf("exported: %s (repo=%s files=%d symbols=%d)\n",
				outPath, repoName, idx.FileCount(), idx.SymbolCount())
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output path (default: <repo-name>.canopyindex)")
	cmd.Flags().StringVar(&name, "name", "", "override repo name (default: directory basename)")
	return cmd
}

func newImportCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "import <file|glob>...",
		Short: "Load exported indexes and print summary",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var paths []string
			for _, arg := range args {
				matches, err := filepath.Glob(arg)
				if err != nil {
					return fmt.Errorf("invalid glob %q: %w", arg, err)
				}
				if len(matches) == 0 {
					return fmt.Errorf("no files match %q", arg)
				}
				paths = append(paths, matches...)
			}

			type importSummary struct {
				File      string `json:"file"`
				RepoName  string `json:"repo_name"`
				RepoURL   string `json:"repo_url,omitempty"`
				CommitSHA string `json:"commit_sha,omitempty"`
				Files     int    `json:"files"`
				Symbols   int    `json:"symbols"`
			}

			var summaries []importSummary
			for _, path := range paths {
				exported, err := federation.LoadFile(path)
				if err != nil {
					return fmt.Errorf("load %s: %w", path, err)
				}
				summaries = append(summaries, importSummary{
					File:      path,
					RepoName:  exported.RepoName,
					RepoURL:   exported.RepoURL,
					CommitSHA: exported.CommitSHA,
					Files:     exported.Index.FileCount(),
					Symbols:   exported.Index.SymbolCount(),
				})
			}

			if jsonOutput {
				return emitJSON(summaries)
			}

			for _, s := range summaries {
				extra := ""
				if s.CommitSHA != "" {
					extra = fmt.Sprintf(" commit=%s", s.CommitSHA)
				}
				fmt.Printf("import: %s repo=%s files=%d symbols=%d%s\n",
					s.File, s.RepoName, s.Files, s.Symbols, extra)
			}
			fmt.Printf("total: %d index(es) loaded\n", len(summaries))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}

func gitRemoteURL(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitHeadSHA(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
