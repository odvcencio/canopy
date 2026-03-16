package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/ignore"
	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/structdiff"
)

func newIndexCmd() *cobra.Command {
	var outPath string
	var jsonOutput bool
	var incremental bool
	var watch bool
	var subfileIncremental bool
	var poll bool
	var reportChanges bool
	var onceIfChanged bool
	var interval time.Duration
	var ignorePatterns []string

	cmd := &cobra.Command{
		Use:     "index [path]",
		Aliases: []string{"gtsindex"},
		Short:   "Build a structural index and optionally cache it",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if watch && interval <= 0 {
				return fmt.Errorf("interval must be > 0 in watch mode")
			}
			if watch && onceIfChanged {
				return fmt.Errorf("--once-if-changed cannot be used with --watch")
			}
			if onceIfChanged && strings.TrimSpace(outPath) == "" {
				return fmt.Errorf("--once-if-changed requires --out to provide a baseline cache path")
			}
			if onceIfChanged {
				reportChanges = true
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			builder := index.NewBuilder()

			// Load .gtsignore from target directory, merge with --ignore flags
			var allIgnoreLines []string
			if data, err := os.ReadFile(filepath.Join(target, ".gtsignore")); err == nil {
				allIgnoreLines = append(allIgnoreLines, strings.Split(string(data), "\n")...)
			}
			allIgnoreLines = append(allIgnoreLines, ignorePatterns...)
			if len(allIgnoreLines) > 0 {
				builder.SetIgnore(ignore.ParsePatterns(allIgnoreLines))
			}

			var previous *model.Index
			hasBaseline := false
			if strings.TrimSpace(outPath) != "" {
				cached, err := index.Load(outPath)
				switch {
				case err == nil:
					previous = cached
					hasBaseline = true
				case os.IsNotExist(err):
				default:
					return fmt.Errorf("load cache %s: %w", outPath, err)
				}
			}

			buildOnce := func(base *model.Index) (*model.Index, index.BuildStats, error) {
				if incremental {
					return builder.BuildPathIncremental(context.Background(), target, base)
				}
				idx, err := builder.BuildPath(target)
				return idx, index.BuildStats{}, err
			}

			buildBase := (*model.Index)(nil)
			if incremental {
				buildBase = previous
			}

			idx, stats, err := buildOnce(buildBase)
			if err != nil {
				return err
			}

			report := structdiff.Report{}
			changed := true
			if hasBaseline {
				report = structdiff.Compare(previous, idx)
				changed = report.Stats.ChangedFiles > 0 || !parseErrorsEqual(previous.Errors, idx.Errors)
			}

			if strings.TrimSpace(outPath) != "" && (!onceIfChanged || changed || !hasBaseline) {
				if err := index.Save(outPath, idx); err != nil {
					return err
				}
			}

			if jsonOutput {
				if err := emitJSON(idx); err != nil {
					return err
				}
			}

			if !jsonOutput {
				printIndexSummary(idx, stats, incremental)
				if strings.TrimSpace(outPath) != "" {
					fmt.Printf("cache: %s\n", outPath)
				}
				if reportChanges {
					printChangeReport(report, hasBaseline)
				}
			}

			if onceIfChanged {
				if changed {
					return exitCodeError{
						code: 2,
						err:  errors.New("structural changes detected"),
					}
				}
				if !jsonOutput {
					fmt.Println("once-if-changed: no structural changes")
				}
				return nil
			}

			if !watch {
				return nil
			}

			fmt.Printf("watching: interval=%s target=%s subfile-incremental=%t\n", interval.String(), target, subfileIncremental)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			watchState := index.NewWatchState()
			defer watchState.Release()

			current := idx
			onChange := func(changedPaths []string) {
				base := (*model.Index)(nil)
				if incremental {
					base = current
				}

				var (
					next      *model.Index
					nextStats index.BuildStats
					err       error
				)
				useSubfile := subfileIncremental && len(changedPaths) > 0
				if useSubfile {
					next, nextStats, err = builder.ApplyWatchChanges(current, changedPaths, watchState, index.WatchUpdateOptions{
						SubfileIncremental: true,
					})
				} else {
					next, nextStats, err = buildOnce(base)
					if subfileIncremental {
						watchState.Clear()
					}
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "watch build error: %v\n", err)
					return
				}

				watchReport := structdiff.Compare(current, next)
				watchChanged := watchReport.Stats.ChangedFiles > 0 || !parseErrorsEqual(current.Errors, next.Errors)
				if !watchChanged {
					return
				}

				current = next
				if strings.TrimSpace(outPath) != "" {
					if err := index.Save(outPath, next); err != nil {
						fmt.Fprintf(os.Stderr, "watch save error: %v\n", err)
					}
				}

				if jsonOutput {
					if err := emitJSON(next); err != nil {
						fmt.Fprintf(os.Stderr, "watch json error: %v\n", err)
					}
					return
				}

				fmt.Printf("watch: changed files=%d symbols=+%d -%d ~%d\n",
					watchReport.Stats.ChangedFiles,
					watchReport.Stats.AddedSymbols,
					watchReport.Stats.RemovedSymbols,
					watchReport.Stats.ModifiedSymbols)
				printIndexSummary(next, nextStats, incremental)
				if reportChanges {
					printChangeReport(watchReport, true)
				}
			}

			ignorePaths := map[string]bool{}
			if strings.TrimSpace(outPath) != "" {
				if absOut, err := filepath.Abs(outPath); err == nil {
					ignorePaths[filepath.Clean(absOut)] = true
				}
			}

			if !poll {
				if err := watchWithFSNotify(ctx, target, interval, ignorePaths, builder.Ignore(), onChange); err == nil {
					fmt.Println("watch: stopped")
					return nil
				} else {
					fmt.Fprintf(os.Stderr, "watch backend fallback to polling: %v\n", err)
				}
			}

			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					fmt.Println("watch: stopped")
					return nil
				case <-ticker.C:
					onChange(nil)
				}
			}
		},
	}

	cmd.Flags().StringVar(&outPath, "out", ".gts/index.json", "output path for index cache")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit index JSON to stdout")
	cmd.Flags().BoolVar(&incremental, "incremental", true, "reuse unchanged files from previous index cache")
	cmd.Flags().BoolVar(&watch, "watch", false, "watch for structural changes and rebuild continuously")
	cmd.Flags().BoolVar(&subfileIncremental, "subfile-incremental", true, "reuse per-file parse trees for sub-file incremental updates in watch mode")
	cmd.Flags().BoolVar(&poll, "poll", false, "force polling watch mode instead of fsnotify")
	cmd.Flags().BoolVar(&reportChanges, "report-changes", false, "print grouped structural change summary against previous cache")
	cmd.Flags().BoolVar(&onceIfChanged, "once-if-changed", false, "exit with code 2 when structural changes are detected")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "poll interval for watch mode")
	cmd.Flags().StringArrayVar(&ignorePatterns, "ignore", nil, "additional ignore patterns (repeatable, merged with .gtsignore)")
	return cmd
}

func runIndex(args []string) error {
	cmd := newIndexCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
