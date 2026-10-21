package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/internal/outline"
)

func TestOutlineCmd_LiveBoundedJSON(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	name := "-source.go"
	if err := os.WriteFile(name, []byte("package p\nfunc First() {}\nfunc Second() {}\n"), 0640); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"search", "outline", "--json", "--limit", "1", "--", name})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report outline.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.FormatVersion != 1 || report.Path != name || len(report.Symbols) != 1 || report.Symbols[0].Name != "First" || report.NextCursor == "" || out.Len() > outline.DefaultOutputBytes+1 {
		t.Fatalf("CLI protocol: %+v bytes=%d", report, out.Len())
	}
	if _, err := os.Stat(filepath.Join(root, ".canopy")); !os.IsNotExist(err) {
		t.Fatalf("CLI created index/cache: %v", err)
	}
	t.Logf("cli_format=1 json_bytes=%d literal_option_path=true symbols=1 next_cursor=true index_files_created=0", out.Len())
}
