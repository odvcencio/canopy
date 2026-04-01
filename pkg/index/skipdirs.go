package index

// DefaultSkipDirs returns directory names that are skipped during indexing.
// These are well-known dependency/build/cache directories that never contain
// user-authored source code. Users can override with .gtsignore negation
// patterns (e.g. !vendor/) if needed.
func DefaultSkipDirs() map[string]bool {
	return map[string]bool{
		"node_modules":  true,
		"vendor":        true,
		".venv":         true,
		"venv":          true,
		"__pycache__":   true,
		"target":        true,
		".tox":          true,
		".mypy_cache":   true,
		".pytest_cache": true,
		"Pods":          true,
		".gradle":       true,
		".cargo":        true,
	}
}
