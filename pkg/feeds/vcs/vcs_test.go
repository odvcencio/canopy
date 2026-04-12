package vcs

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/scope"
)

func TestParseGitBlame(t *testing.T) {
	// Minimal git blame --porcelain output
	porcelain := `abc123def456789012345678901234567890abcd 1 1 1
author Alice
author-mail <alice@example.com>
author-time 1710000000
author-tz +0000
committer Alice
committer-mail <alice@example.com>
committer-time 1710000000
committer-tz +0000
summary initial commit
filename main.go
	package main
def456789012345678901234567890abcdef1234 2 2 1
author Bob
author-mail <bob@example.com>
author-time 1710100000
author-tz +0000
committer Bob
committer-mail <bob@example.com>
committer-time 1710100000
committer-tz +0000
summary add function
filename main.go
	func Hello() {}
`

	result, err := parseGitBlame([]byte(porcelain))
	if err != nil {
		t.Fatalf("parseGitBlame error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	e1, ok := result[1]
	if !ok {
		t.Fatal("missing entry for line 1")
	}
	if e1.Author != "Alice" {
		t.Errorf("line 1 author = %q, want Alice", e1.Author)
	}
	if e1.Commit != "abc123def456789012345678901234567890abcd" {
		t.Errorf("line 1 commit = %q", e1.Commit)
	}

	e2, ok := result[2]
	if !ok {
		t.Fatal("missing entry for line 2")
	}
	if e2.Author != "Bob" {
		t.Errorf("line 2 author = %q, want Bob", e2.Author)
	}
}

func TestDetectNoVCS(t *testing.T) {
	dir := t.TempDir()
	f := Detect(dir)
	if f != nil {
		t.Error("expected nil Feed for directory without VCS")
	}
}

func TestFeedName(t *testing.T) {
	f := &Feed{vcsType: "git"}
	if f.Name() != "vcs" {
		t.Errorf("Name() = %q, want vcs", f.Name())
	}
	if f.Priority() != 50 {
		t.Errorf("Priority() = %d, want 50", f.Priority())
	}
	if !f.Supports("go") {
		t.Error("should support all languages")
	}
}

func TestFeedSkipsNoScope(t *testing.T) {
	graph := scope.NewGraph()
	f := &Feed{vcsType: "git", vcsRoot: "/tmp"}
	err := f.Feed(graph, "nonexistent.go", nil, nil)
	if err != nil {
		t.Errorf("expected nil error for missing scope, got %v", err)
	}
}
