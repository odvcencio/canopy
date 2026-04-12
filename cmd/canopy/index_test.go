package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadIndexIgnoreLines_MergesGraftAndGTSIgnore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".graftignore"), []byte("data/\nfrontend/dist/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.graftignore) failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".canopyignore"), []byte("custom-cache/\n!frontend/public/orchard.wasm\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.canopyignore) failed: %v", err)
	}

	got, err := loadIndexIgnoreLines(dir)
	if err != nil {
		t.Fatalf("loadIndexIgnoreLines returned error: %v", err)
	}

	want := []string{
		"data/",
		"frontend/dist/",
		"",
		"custom-cache/",
		"!frontend/public/orchard.wasm",
		"",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loadIndexIgnoreLines = %#v, want %#v", got, want)
	}
}

func TestLoadIndexIgnoreLines_MissingFiles(t *testing.T) {
	got, err := loadIndexIgnoreLines(t.TempDir())
	if err != nil {
		t.Fatalf("loadIndexIgnoreLines returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no ignore lines, got %#v", got)
	}
}
