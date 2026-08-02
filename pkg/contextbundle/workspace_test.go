package contextbundle

import (
	"context"
	"sync"
	"testing"
)

// TestWorkspaceManager_ConcurrentAccess verifies spec 10.3's requirement
// that concurrent subagents against the same root do not build duplicate
// indexes and never race (run with -race).
func TestWorkspaceManager_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"main.go": "package sample\n\nfunc Greet() string { return \"hi\" }\n",
	})

	mgr := NewWorkspaceManager()

	const goroutines = 16
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	ids := make([]string, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			idx, snap, err := mgr.Snapshot(context.Background(), dir)
			if err != nil {
				errs[i] = err
				return
			}
			if idx == nil || idx.FileCount() == 0 {
				errs[i] = errNilOrEmptyIndex
				return
			}
			ids[i] = snap.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	first := ids[0]
	if first == "" {
		t.Fatal("expected a non-empty snapshot ID")
	}
	for i, id := range ids {
		if id != first {
			t.Fatalf("goroutine %d snapshot ID %q differs from %q; concurrent access must converge on one snapshot", i, id, first)
		}
	}
}

func TestWorkspaceManager_RefreshConcurrentWithSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"main.go": "package sample\n\nfunc Greet() string { return \"hi\" }\n",
	})
	mgr := NewWorkspaceManager()

	var wg sync.WaitGroup
	wg.Add(2)
	var snapErr, refreshErr error
	go func() {
		defer wg.Done()
		_, _, snapErr = mgr.Snapshot(context.Background(), dir)
	}()
	go func() {
		defer wg.Done()
		_, _, _, refreshErr = mgr.Refresh(context.Background(), dir, []string{"main.go"})
	}()
	wg.Wait()

	if snapErr != nil {
		t.Fatalf("Snapshot: %v", snapErr)
	}
	if refreshErr != nil {
		t.Fatalf("Refresh: %v", refreshErr)
	}
}

var errNilOrEmptyIndex = errIndexEmptyForTest{}

type errIndexEmptyForTest struct{}

func (errIndexEmptyForTest) Error() string { return "index was nil or empty" }
