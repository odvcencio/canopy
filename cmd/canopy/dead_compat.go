package main

// dead_compat.go provides package-level wrappers retained for callers in
// report_cmd.go that have not yet been migrated to pkg/roots directly.
// They preserve the original Go-only heuristics.

import (
	"strings"

	"m31labs.dev/canopy/pkg/xref"
)

// isEntrypointDefinition returns true for Go main/init functions.
// Kept for report_cmd.go compatibility; new code should use roots.Analyzer.
func isEntrypointDefinition(definition xref.Definition) bool {
	if definition.Kind != "function_definition" {
		return false
	}
	return definition.Name == "main" || definition.Name == "init"
}

// isTestSourceFile returns true for Go _test.go files.
// Kept for report_cmd.go compatibility; new code should use roots.Analyzer.
func isTestSourceFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), "_test.go")
}
