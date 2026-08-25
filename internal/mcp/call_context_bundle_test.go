package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/internal/contextpack"
	"m31labs.dev/canopy/pkg/model"
)

// TestServiceCallContext_BundleMode verifies the additive gts_context
// inputs (task, mode, selectors) from spec 10.13 route through
// pkg/contextbundle and return a bundleResponse, while the existing
// file/line/semantic inputs (covered by TestServiceCallContextAndDeps) keep
// returning the legacy contextpack.Report unchanged.
func TestServiceCallContext_BundleMode(t *testing.T) {
	tmpDir := t.TempDir()
	source := `package sample

func Greet(name string) string {
	return formatGreeting(name)
}

func formatGreeting(name string) string {
	return "Hello, " + name + "!"
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	service := NewService(tmpDir, "")
	resultRaw, err := service.Call("gts_context", map[string]any{
		"task": "improve the greeting",
		"mode": "implement",
		"selectors": []any{
			map[string]any{"file": "main.go", "symbol": "Greet", "required": true},
		},
		"tokens": 1500,
	})
	if err != nil {
		t.Fatalf("gts_context bundle-mode call failed: %v", err)
	}
	result, ok := resultRaw.(bundleResponse)
	if !ok {
		t.Fatalf("expected bundleResponse, got %T", resultRaw)
	}
	if result.Receipt.ID == "" {
		t.Fatal("expected a non-empty receipt ID")
	}
	if len(result.Manifest.Items) == 0 {
		t.Fatal("expected at least one manifest item")
	}
	if result.Content == "" {
		t.Fatal("expected non-empty rendered content")
	}
	if result.ParserHealth.Status != model.ParseCoverageClean || result.ParserHealth.TotalFiles != 1 {
		t.Fatalf("expected clean parser health for selected context, got %+v", result.ParserHealth)
	}
}

func TestServiceCallContext_BundleMode_LegacyRollback(t *testing.T) {
	tmpDir := t.TempDir()
	source := "package sample\n\nfunc Greet(name string) string {\n\treturn \"hi \" + name\n}\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	t.Setenv(contextpack.EnvLegacyOverride, "1")

	service := NewService(tmpDir, "")
	_, err := service.Call("gts_context", map[string]any{
		"task": "improve the greeting",
		"mode": "implement",
	})
	// Under the rollback override, bundle-mode inputs fall through to the
	// legacy path, which requires "file" and therefore errors here since
	// none was given — proving the override actually disabled bundle mode
	// rather than silently ignoring it.
	if err == nil {
		t.Fatal("expected an error: legacy rollback should require \"file\"")
	}
}
