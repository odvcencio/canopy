package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gts-suite/pkg/model"
)

type stubStreamingParser struct{}

func (stubStreamingParser) Language() string {
	return "go"
}

func (stubStreamingParser) Parse(path string, src []byte) (model.FileSummary, error) {
	text := string(src)
	if strings.Contains(text, "PARSE_ERROR") {
		return model.FileSummary{}, errors.New("parse failure")
	}
	return model.FileSummary{
		Symbols: []model.Symbol{
			{
				Kind: "function",
				Name: filepath.Base(path),
			},
		},
	}, nil
}

func TestBuildPathIncrementalWithOptions_ObserverReceivesStreamedEvents(t *testing.T) {
	tmpDir := t.TempDir()

	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", name, err)
		}
	}

	write("a.go", "package sample\n\nfunc A() {}\n")
	write("b.go", "package sample\n\nfunc B() {}\n")

	builder := NewBuilder()
	builder.Register(".go", stubStreamingParser{})

	baseline, _, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		t.Fatalf("BuildPathIncremental baseline returned error: %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	write("a.go", "package sample\n\nfunc A2() {}\n")
	write("c.go", "PARSE_ERROR\n")

	var events []BuildEvent
	next, stats, err := builder.BuildPathIncrementalWithOptions(context.Background(), tmpDir, baseline, BuildOptions{
		Observer: func(event BuildEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("BuildPathIncrementalWithOptions returned error: %v", err)
	}

	if stats.CandidateFiles != 3 || stats.ParsedFiles != 1 || stats.ReusedFiles != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if next.FileCount() != 2 {
		t.Fatalf("expected 2 indexed files, got %d", next.FileCount())
	}
	if len(next.Errors) != 1 || next.Errors[0].Path != "c.go" {
		t.Fatalf("expected parse error for c.go, got %+v", next.Errors)
	}

	gotKinds := map[string]BuildEventKind{}
	for _, event := range events {
		gotKinds[string(event.Kind)+":"+event.Path] = event.Kind
	}

	want := []string{
		"parsed:a.go",
		"reused:b.go",
		"error:c.go",
	}
	for _, key := range want {
		if _, ok := gotKinds[key]; !ok {
			t.Fatalf("missing build event %q from %+v", key, events)
		}
	}
}

func TestPartialIndexSnapshotTracksFilesAndErrors(t *testing.T) {
	base := &model.Index{
		Version: "0.2.0",
		Root:    "/repo",
		Files: []model.FileSummary{
			{Path: "a.go", Language: "go"},
			{Path: "keep.go", Language: "go"},
			{Path: "stale.go", Language: "go"},
		},
		Errors: []model.ParseError{
			{Path: "broken.go", Error: "old failure"},
		},
	}

	partial := NewPartialIndex("/repo", base)
	partial.Apply(BuildEvent{
		Kind: BuildEventParsed,
		Path: "a.go",
		Summary: model.FileSummary{
			Path:     "a.go",
			Language: "go",
			Symbols: []model.Symbol{
				{Name: "A2", Kind: "function"},
			},
		},
	})
	partial.Apply(BuildEvent{
		Kind: BuildEventError,
		Path: "stale.go",
		ParseError: model.ParseError{
			Path:  "stale.go",
			Error: "parse failure",
		},
	})
	partial.Apply(BuildEvent{
		Kind: BuildEventReused,
		Path: "new.go",
		Summary: model.FileSummary{
			Path:     "new.go",
			Language: "go",
		},
	})

	snapshot := partial.Snapshot()
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}

	gotFiles := make([]string, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		gotFiles = append(gotFiles, file.Path)
	}
	wantFiles := []string{"a.go", "keep.go", "new.go"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("unexpected files got=%v want=%v", gotFiles, wantFiles)
	}

	gotErrors := make([]string, 0, len(snapshot.Errors))
	for _, parseErr := range snapshot.Errors {
		gotErrors = append(gotErrors, parseErr.Path)
	}
	wantErrors := []string{"broken.go", "stale.go"}
	if !reflect.DeepEqual(gotErrors, wantErrors) {
		t.Fatalf("unexpected errors got=%v want=%v", gotErrors, wantErrors)
	}
}

func TestSaveOverwritesExistingCache(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "nested", "index.json")

	first := &model.Index{
		Version: "0.2.0",
		Root:    "/first",
		Files: []model.FileSummary{
			{Path: "a.go", Language: "go"},
		},
		GeneratedAt: time.Unix(1, 0).UTC(),
	}
	second := &model.Index{
		Version: "0.2.0",
		Root:    "/second",
		Files: []model.FileSummary{
			{Path: "b.go", Language: "go"},
		},
		Errors: []model.ParseError{
			{Path: "c.go", Error: "boom"},
		},
		GeneratedAt: time.Unix(2, 0).UTC(),
	}

	if err := Save(cachePath, first); err != nil {
		t.Fatalf("Save(first) failed: %v", err)
	}
	if err := Save(cachePath, second); err != nil {
		t.Fatalf("Save(second) failed: %v", err)
	}

	loaded, err := Load(cachePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !reflect.DeepEqual(loaded, second) {
		t.Fatalf("unexpected cache contents got=%+v want=%+v", loaded, second)
	}
}
