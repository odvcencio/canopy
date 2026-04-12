package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/model"
)

const (
	indexCheckpointEvery    = 32
	indexCheckpointInterval = 2 * time.Second
)

type indexCheckpointWriter struct {
	outPath      string
	partial      *index.PartialIndex
	stderr       io.Writer
	saveFn       func(string, *model.Index) error
	saveEvery    int
	saveInterval time.Duration
	lastSave     time.Time
	dirtyCount   int
	savedAny     bool
}

func resolveIndexRoot(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		target = "."
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	absTarget = filepath.Clean(absTarget)

	info, err := os.Stat(absTarget)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return absTarget, nil
	}
	return filepath.Dir(absTarget), nil
}

func newIndexCheckpointWriter(outPath, root string, baseline *model.Index) *indexCheckpointWriter {
	if strings.TrimSpace(outPath) == "" {
		return nil
	}
	return &indexCheckpointWriter{
		outPath:      outPath,
		partial:      index.NewPartialIndex(root, baseline),
		stderr:       os.Stderr,
		saveFn:       index.Save,
		saveEvery:    indexCheckpointEvery,
		saveInterval: indexCheckpointInterval,
		lastSave:     time.Now(),
	}
}

func (w *indexCheckpointWriter) Observe(event index.BuildEvent) {
	if w == nil || w.partial == nil {
		return
	}

	w.partial.Apply(event)
	w.dirtyCount++
	if w.dirtyCount < w.saveEvery && time.Since(w.lastSave) < w.saveInterval {
		return
	}
	if err := w.flush("stream", event.Stats); err != nil && w.stderr != nil {
		fmt.Fprintf(w.stderr, "index checkpoint save error: %v\n", err)
	}
}

func (w *indexCheckpointWriter) Flush(reason string, stats index.BuildStats) error {
	if w == nil {
		return nil
	}
	return w.flush(reason, stats)
}

func (w *indexCheckpointWriter) SavedAny() bool {
	return w != nil && w.savedAny
}

func (w *indexCheckpointWriter) flush(reason string, stats index.BuildStats) error {
	if w == nil || w.partial == nil || w.dirtyCount == 0 {
		return nil
	}

	snapshot := w.partial.Snapshot()
	if snapshot == nil {
		return nil
	}
	if err := w.saveFn(w.outPath, snapshot); err != nil {
		return err
	}

	w.dirtyCount = 0
	w.lastSave = time.Now()
	w.savedAny = true

	if w.stderr != nil {
		fmt.Fprintf(
			w.stderr,
			"index checkpoint: reason=%s files=%d errors=%d parsed=%d reused=%d cache=%s\n",
			reason,
			snapshot.FileCount(),
			len(snapshot.Errors),
			stats.ParsedFiles,
			stats.ReusedFiles,
			w.outPath,
		)
	}

	return nil
}
