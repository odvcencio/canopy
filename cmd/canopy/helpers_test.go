package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestLoadOrBuildChangedUsesChangedOnlyFallback(t *testing.T) {
	tmpDir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", name, err)
		}
	}
	write("changed.go", "package sample\n\nfunc Changed() {}\n")
	write("unchanged.go", "package sample\n\nfunc Unchanged() {}\n")

	cmd := &cobra.Command{}
	idx, changedScoped, err := loadOrBuildChanged(cmd, "", tmpDir, true, []string{"changed.go"})
	if err != nil {
		t.Fatalf("loadOrBuildChanged returned error: %v", err)
	}
	if !changedScoped {
		t.Fatal("loadOrBuildChanged did not report a changed-only index")
	}
	if got, want := idx.FileCount(), 1; got != want {
		t.Fatalf("changed-only file count = %d, want %d", got, want)
	}
	if got, want := idx.Files[0].Path, "changed.go"; got != want {
		t.Fatalf("changed-only path = %q, want %q", got, want)
	}
}

func TestLoadOrBuildCheckDerivesChangedPathsFromBase(t *testing.T) {
	tmpDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", tmpDir}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	runGit("init")
	runGit("config", "user.email", "canopy-test@example.com")
	runGit("config", "user.name", "Canopy Test")
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", name, err)
		}
	}
	write("changed.go", "package sample\n\nfunc Changed() {}\n")
	write("unchanged.go", "package sample\n\nfunc Unchanged() {}\n")
	runGit("add", "changed.go", "unchanged.go")
	runGit("commit", "-m", "initial")
	write("changed.go", "package sample\n\nfunc ChangedAgain() {}\n")

	cmd := &cobra.Command{}
	idx, err := loadOrBuildCheck(cmd, "", tmpDir, true, "HEAD")
	if err != nil {
		t.Fatalf("loadOrBuildCheck returned error: %v", err)
	}
	if got, want := analysisIndexScope(cmd), "changed"; got != want {
		t.Fatalf("analysis index scope = %q, want %q", got, want)
	}
	if got, want := idx.FileCount(), 1; got != want {
		t.Fatalf("check file count = %d, want %d", got, want)
	}
	if got, want := idx.Files[0].Path, "changed.go"; got != want {
		t.Fatalf("check path = %q, want %q", got, want)
	}
}
