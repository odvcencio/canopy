package outline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putSource(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0640); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestReadFile_LanguagesAndCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		owner           string
	}{
		{"sample.go", "package sample\ntype Service struct{}\nfunc (s *Service) Read() string { return \"ok\" }\nfunc Other() {}\n", "Read", "Service"},
		{"sample.py", "class Service:\n    def read(self):\n        def inner():\n            return 42\n        return inner()\n", "inner", ""},
		{"sample.ts", "class Service { read(): number { return 42; } }\nfunction other() { return 1; }\n", "other", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := ReadFile(context.Background(), putSource(t, tc.name, tc.src), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if report.Coverage.TreeHasError || report.Coverage.DeclineReason != "" || report.Coverage.QueryTruncated || len(report.Coverage.DefinitionKinds) == 0 {
				t.Fatalf("coverage: %+v", report.Coverage)
			}
			found := false
			for index, sym := range report.Symbols {
				if sym.StartLine == 0 || sym.EndLine < sym.StartLine || sym.ParentIndex >= index {
					t.Fatalf("invalid location/parent: %+v", sym)
				}
				if sym.Name == tc.want {
					found = true
					if tc.owner != "" && sym.Owner != tc.owner {
						t.Fatalf("owner=%q want %q", sym.Owner, tc.owner)
					}
				}
			}
			if !found {
				t.Fatalf("missing %q: %+v", tc.want, report.Symbols)
			}
			t.Logf("language=%s symbols=%d wanted=%s found=true owner=%s parse_error=false kinds=%v", report.Language, report.TotalSymbols, tc.want, tc.owner, report.Coverage.DefinitionKinds)
		})
	}
	broken, err := ReadFile(context.Background(), putSource(t, "broken.go", "package bad\nfunc Broken( {\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !broken.Coverage.TreeHasError {
		t.Fatalf("malformed source not reported: %+v", broken.Coverage)
	}
	empty, err := ReadFile(context.Background(), putSource(t, "empty.go", "package empty\n"), Options{})
	if err != nil || empty.TotalSymbols != 0 || empty.Coverage.DeclineReason != "" {
		t.Fatalf("empty outline: %+v %v", empty, err)
	}
	jsonFile, err := ReadFile(context.Background(), putSource(t, "data.json", "{\"a\": 1}\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if jsonFile.Coverage.DeclineReason != "query_empty" || !jsonFile.Coverage.ParseSkipped {
		t.Fatalf("unsupported tags silently complete: %+v", jsonFile)
	}
}
func TestReadFile_PagesAndSourceIdentity(t *testing.T) {
	src := "package page\n"
	for i := 0; i < 120; i++ {
		src += fmt.Sprintf("func Function%03d() {}\n", i)
	}
	path := putSource(t, "pages.go", src)
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	firstCursor := ""
	for {
		report, err := ReadFile(context.Background(), path, Options{Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		raw, _ := json.Marshal(report)
		if len(raw) > DefaultOutputBytes || len(report.Symbols) > 7 || report.Coverage.QueryTruncated {
			t.Fatalf("unbounded/incomplete page: bytes=%d %+v", len(raw), report.Coverage)
		}
		for _, sym := range report.Symbols {
			if seen[sym.Name] {
				t.Fatalf("duplicate %s", sym.Name)
			}
			seen[sym.Name] = true
		}
		cursor = report.NextCursor
		if firstCursor == "" {
			firstCursor = cursor
		}
		if cursor == "" {
			break
		}
		if pages > 30 {
			t.Fatal("cursor did not advance")
		}
	}
	if len(seen) != 120 {
		t.Fatalf("returned %d want 120", len(seen))
	}
	if err := os.WriteFile(path, []byte(src+"func Added() {}\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(context.Background(), path, Options{Cursor: firstCursor}); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("stale cursor accepted: %v", err)
	}
	other := putSource(t, "other.go", src)
	if _, err := ReadFile(context.Background(), other, Options{Cursor: firstCursor}); err == nil {
		t.Fatal("cross-file cursor accepted")
	}
	t.Logf("symbols=120 returned_unique=%d pages=%d stale_cursor_rejected=true cross_file_cursor_rejected=true", len(seen), pages)
}
func TestReadFile_BudgetsAndSmallOutline(t *testing.T) {
	src := "package compact\nfunc Read() string {\n" + strings.Repeat("// irrelevant implementation detail\n", 10000) + "return \"answer\"\n}\n"
	path := putSource(t, "large.go", src)
	report, err := ReadFile(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	if len(raw) > DefaultOutputBytes || len(raw)*100 > len(src) || report.TotalSymbols != 1 || strings.Contains(string(raw), "implementation detail") {
		t.Fatalf("source leaked instead of outline: source=%d outline=%d", len(src), len(raw))
	}
	t.Logf("source_bytes=%d outline_json_bytes=%d symbols=%d implementation_bodies_omitted=true", len(src), len(raw), report.TotalSymbols)
	long := putSource(t, "names.go", "package long\nfunc "+strings.Repeat("界", 1000)+"() {}\n")
	report, err = ReadFile(context.Background(), long, Options{})
	if err != nil || len(report.Symbols) != 1 || !report.Symbols[0].NameTruncated || len(report.Symbols[0].Name) > maxNameBytes {
		t.Fatalf("long name: %+v %v", report, err)
	}
	if _, err := ReadFile(context.Background(), putSource(t, "oversized.go", strings.Repeat("x", DefaultFileBytes+1)), Options{}); err == nil {
		t.Fatal("oversized file accepted")
	}
	if _, err := ReadFile(context.Background(), t.TempDir(), Options{}); err == nil {
		t.Fatal("directory accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadFile(ctx, "nonexistent", Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
