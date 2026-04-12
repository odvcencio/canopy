package index

// defaultSkipDirs is the shared set of directory names skipped during indexing.
// These are well-known dependency/build/cache directories that never contain
// user-authored source code. Users can override with .canopyignore negation
// patterns (e.g. !vendor/) if needed.
var defaultSkipDirs = map[string]bool{
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

// DefaultSkipDirs returns directory names that are skipped during indexing.
func DefaultSkipDirs() map[string]bool {
	return defaultSkipDirs
}
