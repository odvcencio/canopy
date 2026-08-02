package contextpack

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/pkg/contextbundle"
	"m31labs.dev/canopy/pkg/index"
)

func TestBundleOptions_WantsBundle(t *testing.T) {
	cases := []struct {
		name string
		opts BundleOptions
		want bool
	}{
		{"empty", BundleOptions{}, false},
		{"mode set", BundleOptions{Mode: contextbundle.TaskExplore}, true},
		{"task set", BundleOptions{Task: "fix the bug"}, true},
		{"focus set", BundleOptions{Focus: []contextbundle.Selector{{File: "a.go"}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.WantsBundle(); got != tc.want {
				t.Fatalf("WantsBundle() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUseLegacy(t *testing.T) {
	t.Setenv(EnvLegacyOverride, "")
	if UseLegacy() {
		t.Fatal("expected UseLegacy() false when unset")
	}
	t.Setenv(EnvLegacyOverride, "true")
	if !UseLegacy() {
		t.Fatal("expected UseLegacy() true when set to \"true\"")
	}
	t.Setenv(EnvLegacyOverride, "1")
	if !UseLegacy() {
		t.Fatal("expected UseLegacy() true when set to \"1\"")
	}
}

// TestExistingCLIBehaviorUnchanged is a regression guard for the acceptance
// criterion "Existing `canopy search context` behavior remains": the legacy
// Build entry point (context.go) must not change shape or behavior just
// because compat.go now exists alongside it.
func TestExistingCLIBehaviorUnchanged(t *testing.T) {
	dir := t.TempDir()
	src := "package sample\n\nfunc Greet(name string) string {\n\treturn \"hi \" + name\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	idx, err := index.NewBuilder().BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath: %v", err)
	}

	report, err := Build(idx, Options{FilePath: "main.go", Line: 3, TokenBudget: 200})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if report.Focus == nil || report.Focus.Name != "Greet" {
		t.Fatalf("expected legacy Build to focus on Greet, got %+v", report.Focus)
	}
}

func TestBuildBundle_ProducesReceipt(t *testing.T) {
	dir := t.TempDir()
	src := "package sample\n\nfunc Greet(name string) string {\n\treturn \"hi \" + name\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	idx, err := index.NewBuilder().BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath: %v", err)
	}

	result, err := BuildBundle(context.Background(), idx, dir, BundleOptions{
		Mode:   contextbundle.TaskImplement,
		Tokens: 1000,
		Focus:  []contextbundle.Selector{{File: "main.go", Symbol: "Greet", Required: true}},
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if result.Receipt.ID == "" {
		t.Fatal("expected a non-empty receipt ID")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty rendered content")
	}
}
