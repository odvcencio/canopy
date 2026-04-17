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

	"github.com/odvcencio/canopy/pkg/ignore"
	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/structdiff"
)

func loadIndexIgnoreLines(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		target = filepath.Dir(target)
	}

	var lines []string
	for _, name := range []string{".graftignore", ".canopyignore"} {
		data, err := os.ReadFile(filepath.Join(target, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		lines = append(lines, strings.Split(string(data), "\n")...)
	}
	return lines, nil
}

type indexBuildOpts struct {
	outPath            string
	jsonOutput         bool
	incremental        bool
	watch              bool
	subfileIncremental bool
	poll               bool
	reportChanges      bool
	onceIfChanged      bool
	interval           time.Duration
	ignorePatterns     []string
}

func runIndexBuild(args []string, opts indexBuildOpts) error {
	if opts.watch && opts.interval <= 0 {
		return fmt.Errorf("interval must be > 0 in watch mode")
	}
	if opts.watch && opts.onceIfChanged {
		return fmt.Errorf("--once-if-changed cannot be used with --watch")
	}
	if opts.onceIfChanged && strings.TrimSpace(opts.outPath) == "" {
		return fmt.Errorf("--once-if-changed requires --out to provide a baseline cache path")
	}
	if opts.onceIfChanged {
		opts.reportChanges = true
	}

	target := "."
	if len(args) == 1 {
		target = args[0]
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	builder, err := index.NewBuilderWithWorkspaceIgnores(target)
	if err != nil {
		return err
	}

	// Merge CLI --ignore flags on top of workspace ignores.
	allIgnoreLines, err := loadIndexIgnoreLines(target)
	if err != nil {
		return err
	}
	allIgnoreLines = append(allIgnoreLines, opts.ignorePatterns...)
	if len(allIgnoreLines) > 0 {
		builder.SetIgnore(ignore.ParsePatterns(allIgnoreLines))
	}

	previous, hasBaseline, err := loadBaselineIndex(opts.outPath)
	if err != nil {
		return err
	}

	indexRoot, err := resolveIndexRoot(target)
	if err != nil {
		return err
	}

	buildOnce := func(base *model.Index, observer func(index.BuildEvent)) (*model.Index, index.BuildStats, error) {
		return builder.BuildPathIncrementalWithOptions(ctx, target, base, index.BuildOptions{
			Observer: observer,
		})
	}

	buildBase := (*model.Index)(nil)
	if opts.incremental {
		buildBase = previous
	}

	checkpointWriter := newIndexCheckpointWriter(opts.outPath, indexRoot, buildBase)

	idx, stats, err := buildOnce(buildBase, checkpointWriter.Observe)
	if err != nil {
		return handleBuildError(err, checkpointWriter, opts.outPath, stats)
	}

	report, changed := compareBaseline(previous, idx, hasBaseline)

	if strings.TrimSpace(opts.outPath) != "" && (!opts.onceIfChanged || changed || !hasBaseline || checkpointWriter.SavedAny()) {
		if err := index.Save(opts.outPath, idx); err != nil {
			return err
		}
	}

	if opts.jsonOutput {
		if err := emitJSON(idx); err != nil {
			return err
		}
	} else {
		printIndexSummary(idx, stats, opts.incremental)
		if strings.TrimSpace(opts.outPath) != "" {
			fmt.Printf("cache: %s\n", opts.outPath)
		}
		if opts.reportChanges {
			printChangeReport(report, hasBaseline)
		}
	}

	if opts.onceIfChanged {
		if changed {
			return exitCodeError{
				code: 2,
				err:  errors.New("structural changes detected"),
			}
		}
		if !opts.jsonOutput {
			fmt.Println("once-if-changed: no structural changes")
		}
		return nil
	}

	if !opts.watch {
		return nil
	}

	return runIndexWatch(ctx, target, builder, idx, buildOnce, opts)
}

func loadBaselineIndex(outPath string) (*model.Index, bool, error) {
	if strings.TrimSpace(outPath) == "" {
		return nil, false, nil
	}
	cached, err := index.Load(outPath)
	switch {
	case err == nil:
		return cached, true, nil
	case os.IsNotExist(err):
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("load cache %s: %w", outPath, err)
	}
}

func handleBuildError(err error, checkpointWriter *indexCheckpointWriter, outPath string, stats index.BuildStats) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if checkpointWriter != nil {
			if flushErr := checkpointWriter.Flush("interrupt", stats); flushErr != nil {
				fmt.Fprintf(os.Stderr, "index checkpoint save error: %v\n", flushErr)
			}
			return exitCodeError{
				code: 130,
				err:  fmt.Errorf("index interrupted; partial cache saved to %s", outPath),
			}
		}
		return exitCodeError{
			code: 130,
			err:  errors.New("index interrupted"),
		}
	}
	return err
}

func compareBaseline(previous, idx *model.Index, hasBaseline bool) (structdiff.Report, bool) {
	report := structdiff.Report{}
	changed := true
	if hasBaseline {
		report = structdiff.Compare(previous, idx)
		changed = report.Stats.ChangedFiles > 0 || !parseErrorsEqual(previous.Errors, idx.Errors)
	}
	return report, changed
}

func runIndexWatch(ctx context.Context, target string, builder *index.Builder, current *model.Index, buildOnce func(*model.Index, func(index.BuildEvent)) (*model.Index, index.BuildStats, error), opts indexBuildOpts) error {
	fmt.Printf("watching: interval=%s target=%s subfile-incremental=%t\n", opts.interval.String(), target, opts.subfileIncremental)
	watchState := index.NewWatchState()
	defer watchState.Release()

	onChange := func(changedPaths []string) {
		base := (*model.Index)(nil)
		if opts.incremental {
			base = current
		}

		var (
			next      *model.Index
			nextStats index.BuildStats
			err       error
		)
		useSubfile := opts.subfileIncremental && len(changedPaths) > 0
		if useSubfile {
			next, nextStats, err = builder.ApplyWatchChanges(current, changedPaths, watchState, index.WatchUpdateOptions{
				SubfileIncremental: true,
			})
		} else {
			next, nextStats, err = buildOnce(base, nil)
			if opts.subfileIncremental {
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
		if strings.TrimSpace(opts.outPath) != "" {
			if err := index.Save(opts.outPath, next); err != nil {
				fmt.Fprintf(os.Stderr, "watch save error: %v\n", err)
			}
		}

		if opts.jsonOutput {
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
		printIndexSummary(next, nextStats, opts.incremental)
		if opts.reportChanges {
			printChangeReport(watchReport, true)
		}
	}

	ignorePaths := map[string]bool{}
	if strings.TrimSpace(opts.outPath) != "" {
		if absOut, err := filepath.Abs(opts.outPath); err == nil {
			ignorePaths[filepath.Clean(absOut)] = true
		}
	}

	if !opts.poll {
		if err := watchWithFSNotify(ctx, target, opts.interval, ignorePaths, builder.Ignore(), onChange); err == nil {
			fmt.Println("watch: stopped")
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "watch backend fallback to polling: %v\n", err)
		}
	}

	ticker := time.NewTicker(opts.interval)
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
}

func newIndexBuildCmd() *cobra.Command {
	var opts indexBuildOpts

	cmd := &cobra.Command{
		Use:     "build [path]",
		Aliases: []string{"canopyindex"},
		Short:   "Build a structural index and optionally cache it",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Merge the root-level persistent --exclude flag into this
			// subcommand's own --ignore list so both paths produce the same
			// set of ignored files during the build.
			opts.ignorePatterns = append(opts.ignorePatterns, cmdExcludes(cmd)...)
			return runIndexBuild(args, opts)
		},
	}

	cmd.Flags().StringVar(&opts.outPath, "out", ".canopy/index.json", "output path for index cache")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "emit index JSON to stdout")
	cmd.Flags().BoolVar(&opts.incremental, "incremental", true, "reuse unchanged files from previous index cache")
	cmd.Flags().BoolVar(&opts.watch, "watch", false, "watch for structural changes and rebuild continuously")
	cmd.Flags().BoolVar(&opts.subfileIncremental, "subfile-incremental", true, "reuse per-file parse trees for sub-file incremental updates in watch mode")
	cmd.Flags().BoolVar(&opts.poll, "poll", false, "force polling watch mode instead of fsnotify")
	cmd.Flags().BoolVar(&opts.reportChanges, "report-changes", false, "print grouped structural change summary against previous cache")
	cmd.Flags().BoolVar(&opts.onceIfChanged, "once-if-changed", false, "exit with code 2 when structural changes are detected")
	cmd.Flags().DurationVar(&opts.interval, "interval", 2*time.Second, "poll interval for watch mode")
	cmd.Flags().StringArrayVar(&opts.ignorePatterns, "ignore", nil, "additional ignore patterns (repeatable, merged with .graftignore and .canopyignore)")
	return cmd
}

func runIndex(args []string) error {
	cmd := newIndexBuildCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}
