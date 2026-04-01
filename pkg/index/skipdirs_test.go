package index

import "testing"

func TestDefaultSkipDirs(t *testing.T) {
	dirs := DefaultSkipDirs()
	expected := []string{"node_modules", "vendor", ".venv", "venv", "__pycache__", "target", ".tox", ".mypy_cache", ".pytest_cache", "Pods", ".gradle", ".cargo"}
	for _, d := range expected {
		if !dirs[d] {
			t.Errorf("expected %q in skip dirs", d)
		}
	}
	for _, d := range []string{"build", "dist", "pkg/mod"} {
		if dirs[d] {
			t.Errorf("%q should not be in skip dirs", d)
		}
	}
}
