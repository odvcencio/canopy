package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"m31labs.dev/canopy/pkg/model"
)

// The build guard makes index builds crash-resilient. A pathological file can
// take the whole process down mid-parse (runaway parser memory, OOM kill,
// hard timeout); without a record of what was being parsed, the next build
// walks straight back into the same file, and a checkpointed index that
// omits the killer file looks complete while silently missing it.
//
// Files at or above inflightTrackMinBytes are journaled to
// .canopy/inflight.json just before parsing and removed once their result is
// consumed. Entries left behind by a crashed build are promoted to
// .canopy/quarantine.json on the next run: those files are skipped — and
// recorded in Index.Skipped — until their size or mtime changes, or the
// quarantine file is removed (canopy index build --retry-quarantined).
const inflightTrackMinBytes = 64 * 1024

const (
	inflightFileName   = "inflight.json"
	quarantineFileName = "quarantine.json"
)

type quarantineEntry struct {
	Path            string    `json:"path"`
	SizeBytes       int64     `json:"size_bytes"`
	ModTimeUnixNano int64     `json:"mod_time_unix_nano"`
	Reason          string    `json:"reason"`
	QuarantinedAt   time.Time `json:"quarantined_at"`
}

type inflightEntry struct {
	Path            string    `json:"path"`
	SizeBytes       int64     `json:"size_bytes"`
	ModTimeUnixNano int64     `json:"mod_time_unix_nano"`
	StartedAt       time.Time `json:"started_at"`
}

type buildGuard struct {
	mu         sync.Mutex
	dir        string
	inflight   map[string]inflightEntry
	quarantine map[string]quarantineEntry
	skipped    []model.SkippedFile
}

// newBuildGuard loads the persisted quarantine set for root and promotes any
// in-flight entries a previous build left behind (it crashed or was killed
// while parsing them) into the quarantine set.
func newBuildGuard(root string) *buildGuard {
	g := &buildGuard{
		dir:        filepath.Join(root, ".canopy"),
		inflight:   map[string]inflightEntry{},
		quarantine: map[string]quarantineEntry{},
	}

	readJSON(filepath.Join(g.dir, quarantineFileName), &g.quarantine)

	leftover := map[string]inflightEntry{}
	readJSON(filepath.Join(g.dir, inflightFileName), &leftover)
	for path, entry := range leftover {
		if _, exists := g.quarantine[path]; exists {
			continue
		}
		g.quarantine[path] = quarantineEntry{
			Path:            path,
			SizeBytes:       entry.SizeBytes,
			ModTimeUnixNano: entry.ModTimeUnixNano,
			Reason:          "previous build terminated while parsing this file",
			QuarantinedAt:   time.Now().UTC(),
		}
	}
	if len(leftover) > 0 {
		g.persistQuarantineLocked()
	}
	g.persistInflightLocked()
	return g
}

// checkQuarantine reports whether relPath is quarantined and unchanged. A
// quarantined file whose size or mtime moved gets one fresh attempt: the
// entry is dropped so the file parses normally this run.
func (g *buildGuard) checkQuarantine(relPath string, sizeBytes, modTimeUnixNano int64) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.quarantine[relPath]
	if !ok {
		return "", false
	}
	if entry.SizeBytes != sizeBytes || entry.ModTimeUnixNano != modTimeUnixNano {
		delete(g.quarantine, relPath)
		g.persistQuarantineLocked()
		return "", false
	}
	g.skipped = append(g.skipped, model.SkippedFile{Path: relPath, Reason: entry.Reason})
	return entry.Reason, true
}

func (g *buildGuard) markStarted(relPath string, sizeBytes, modTimeUnixNano int64) {
	if sizeBytes < inflightTrackMinBytes {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inflight[relPath] = inflightEntry{
		Path:            relPath,
		SizeBytes:       sizeBytes,
		ModTimeUnixNano: modTimeUnixNano,
		StartedAt:       time.Now().UTC(),
	}
	g.persistInflightLocked()
}

func (g *buildGuard) markDone(relPath string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.inflight[relPath]; !ok {
		return
	}
	delete(g.inflight, relPath)
	g.persistInflightLocked()
}

// finish clears the in-flight journal after a build that ran to completion;
// anything still marked in-flight was never handed back by the walker (for
// example the walk was cancelled), which is not evidence of a killer file.
func (g *buildGuard) finish() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inflight = map[string]inflightEntry{}
	g.persistInflightLocked()
}

func (g *buildGuard) skippedFiles() []model.SkippedFile {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]model.SkippedFile, len(g.skipped))
	copy(out, g.skipped)
	return out
}

func (g *buildGuard) persistInflightLocked() {
	writeJSON(filepath.Join(g.dir, inflightFileName), g.inflight)
}

func (g *buildGuard) persistQuarantineLocked() {
	writeJSON(filepath.Join(g.dir, quarantineFileName), g.quarantine)
}

// ClearQuarantine removes the persisted quarantine set for root, so the next
// build retries every quarantined file.
func ClearQuarantine(root string) error {
	err := os.Remove(filepath.Join(root, ".canopy", quarantineFileName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readJSON(path string, v any) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, v)
}

func writeJSON(path string, v any) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
