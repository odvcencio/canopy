package treesitter

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestSafeParseCallRecoversPanics(t *testing.T) {
	tree, err := safeParseCall("rust", func() (*gotreesitter.Tree, error) {
		panic("boom")
	})
	if err == nil {
		t.Fatal("expected panic to be converted to error")
	}
	if tree != nil {
		t.Fatal("expected no tree on recovered panic")
	}
	if !strings.Contains(err.Error(), "gotreesitter panic in rust parser") {
		t.Fatalf("unexpected error %q", err)
	}
}

func TestParseBoundTreeMatchesParseWithTree(t *testing.T) {
	entry := findEntryByExtension(t, ".go")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	source := []byte(`package demo

import "fmt"

type Service struct{}

func (s *Service) Run() {
	fmt.Println("ok")
}
`)

	expected, tree, err := parser.ParseWithTree("main.go", source)
	if err != nil {
		t.Fatalf("ParseWithTree returned error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected tree")
	}
	defer tree.Release()

	got, err := parser.ParseBoundTree("main.go", gotreesitter.Bind(tree))
	if err != nil {
		t.Fatalf("ParseBoundTree returned error: %v", err)
	}

	if !reflect.DeepEqual(got.Imports, expected.Imports) {
		t.Fatalf("imports = %#v, want %#v", got.Imports, expected.Imports)
	}
	if !reflect.DeepEqual(got.Symbols, expected.Symbols) {
		t.Fatalf("symbols = %#v, want %#v", got.Symbols, expected.Symbols)
	}
	if !reflect.DeepEqual(got.References, expected.References) {
		t.Fatalf("references = %#v, want %#v", got.References, expected.References)
	}
}

func TestParseGoSymbolsAndImports(t *testing.T) {
	entry := findEntryByExtension(t, ".go")

	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	const source = `package demo

import (
	"fmt"
	"net/http"
)

type Service struct{}

func TestService() error {
	fmt.Println("ok")
	return nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
`

	summary, err := parser.Parse("main.go", []byte(source))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if summary.Language != "go" {
		t.Fatalf("expected language go, got %q", summary.Language)
	}
	if len(summary.Imports) != 2 {
		t.Fatalf("expected two imports, got %d", len(summary.Imports))
	}
	if !hasImport(summary, "fmt") || !hasImport(summary, "net/http") {
		t.Fatalf("unexpected go imports %v", summary.Imports)
	}

	if !hasSymbol(summary, "type_definition", "Service") {
		t.Fatal("expected type_definition Service")
	}
	if !hasSymbol(summary, "function_definition", "TestService") {
		t.Fatal("expected function_definition TestService")
	}
	method := findSymbol(summary, "method_definition", "ServeHTTP")
	if method == nil {
		t.Fatal("expected method_definition ServeHTTP")
	}
	if method.Receiver == "" {
		t.Fatal("expected receiver metadata for method definition")
	}
	if !hasReference(summary, "reference.call", "Println") {
		t.Fatal("expected reference.call Println")
	}
}

func TestParsePythonSymbols(t *testing.T) {
	entry := findEntryByExtension(t, ".py")

	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	const source = `class Worker:
    def run(self):
        return 1

def helper():
    return Worker().run()
`
	summary, err := parser.Parse("main.py", []byte(source))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if summary.Language != "python" {
		t.Fatalf("expected language python, got %q", summary.Language)
	}
	if !hasSymbol(summary, "class_definition", "Worker") && !hasSymbol(summary, "type_definition", "Worker") {
		t.Fatal("expected class/type definition Worker")
	}
	if !hasSymbol(summary, "function_definition", "helper") {
		t.Fatal("expected function_definition helper")
	}
	if !hasReference(summary, "reference.call", "run") {
		t.Fatal("expected reference.call run")
	}
}

func TestParsePythonImportsAndReceiver(t *testing.T) {
	entry := findEntryByExtension(t, ".py")

	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	const source = `import os, sys
from pathlib import Path

class Worker:
    def run(self):
        return Path(".")
`
	summary, err := parser.Parse("main.py", []byte(source))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !hasImport(summary, "import os, sys") || !hasImport(summary, "from pathlib import Path") {
		t.Fatalf("unexpected imports %v", summary.Imports)
	}

	run := findSymbol(summary, "function_definition", "run")
	if run == nil {
		run = findSymbol(summary, "method_definition", "run")
	}
	if run == nil {
		t.Fatalf("expected method/function symbol run")
	}
	if run.Receiver != "Worker" {
		t.Fatalf("expected python receiver Worker, got %q", run.Receiver)
	}
}

func TestParseJavaScriptAndTypeScriptImports(t *testing.T) {
	jsEntry := findEntryByExtension(t, ".js")
	tsEntry := findEntryByExtension(t, ".ts")

	jsParser, err := NewParser(jsEntry)
	if err != nil {
		t.Fatalf("NewParser(js) returned error: %v", err)
	}
	tsParser, err := NewParser(tsEntry)
	if err != nil {
		t.Fatalf("NewParser(ts) returned error: %v", err)
	}

	jsSummary, err := jsParser.Parse("main.js", []byte(`import fs from "node:fs"; import {join} from "./util.js";`))
	if err != nil {
		t.Fatalf("Parse JS returned error: %v", err)
	}
	if !hasImport(jsSummary, `import fs from "node:fs";`) || !hasImport(jsSummary, `import {join} from "./util.js";`) {
		t.Fatalf("unexpected JS imports %v", jsSummary.Imports)
	}

	tsSummary, err := tsParser.Parse("main.ts", []byte(`import type {Config} from "./types"; import React from "react";`))
	if err != nil {
		t.Fatalf("Parse TS returned error: %v", err)
	}
	if !hasImport(tsSummary, `import type {Config} from "./types";`) || !hasImport(tsSummary, `import React from "react";`) {
		t.Fatalf("unexpected TS imports %v", tsSummary.Imports)
	}
}

func TestParseRustAndJavaImports(t *testing.T) {
	rustEntry := findEntryByExtension(t, ".rs")
	javaEntry := findEntryByExtension(t, ".java")

	rustParser, err := NewParser(rustEntry)
	if err != nil {
		t.Fatalf("NewParser(rust) returned error: %v", err)
	}
	javaParser, err := NewParser(javaEntry)
	if err != nil {
		t.Fatalf("NewParser(java) returned error: %v", err)
	}

	rustSummary, err := rustParser.Parse("main.rs", []byte(`use std::io::{self, Read}; use crate::service::Worker;`))
	if err != nil {
		t.Fatalf("Parse Rust returned error: %v", err)
	}
	if !hasImport(rustSummary, `use std::io::{self, Read};`) || !hasImport(rustSummary, `use crate::service::Worker;`) {
		t.Fatalf("unexpected Rust imports %v", rustSummary.Imports)
	}

	javaSummary, err := javaParser.Parse("Main.java", []byte(`import java.util.List; import static java.lang.Math.max; class Main {}`))
	if err != nil {
		t.Fatalf("Parse Java returned error: %v", err)
	}
	if !hasImport(javaSummary, `import java.util.List;`) || !hasImport(javaSummary, `import static java.lang.Math.max;`) {
		t.Fatalf("unexpected Java imports %v", javaSummary.Imports)
	}
}

func TestParseCFamilyCSharpAndKotlinImports(t *testing.T) {
	cEntry := findEntryByExtension(t, ".c")
	cppEntry := findEntryByExtension(t, ".cpp")
	cSharpEntry := findEntryByExtension(t, ".cs")
	kotlinEntry := findEntryByExtension(t, ".kt")

	cParser, err := NewParser(cEntry)
	if err != nil {
		t.Fatalf("NewParser(c) returned error: %v", err)
	}
	cppParser, err := NewParser(cppEntry)
	if err != nil {
		t.Fatalf("NewParser(cpp) returned error: %v", err)
	}
	cSharpParser, err := NewParser(cSharpEntry)
	if err != nil {
		t.Fatalf("NewParser(c_sharp) returned error: %v", err)
	}
	kotlinParser, err := NewParser(kotlinEntry)
	if err != nil {
		t.Fatalf("NewParser(kotlin) returned error: %v", err)
	}

	cSummary, err := cParser.Parse("main.c", []byte("#include <stdio.h>\n#include \"local.h\"\nint main(void) { return 0; }\n"))
	if err != nil {
		t.Fatalf("Parse C returned error: %v", err)
	}
	if !hasImport(cSummary, "#include <stdio.h>") || !hasImport(cSummary, "#include \"local.h\"") {
		t.Fatalf("unexpected C imports %v", cSummary.Imports)
	}

	cppSummary, err := cppParser.Parse("main.cpp", []byte("#include <vector>\n#include \"util.hpp\"\n"))
	if err != nil {
		t.Fatalf("Parse C++ returned error: %v", err)
	}
	if !hasImport(cppSummary, "#include <vector>") || !hasImport(cppSummary, "#include \"util.hpp\"") {
		t.Fatalf("unexpected C++ imports %v", cppSummary.Imports)
	}

	cSharpSummary, err := cSharpParser.Parse("Main.cs", []byte("using System;\nusing System.Text;\nclass C {}\n"))
	if err != nil {
		t.Fatalf("Parse C# returned error: %v", err)
	}
	if !hasImport(cSharpSummary, "using System;") || !hasImport(cSharpSummary, "using System.Text;") {
		t.Fatalf("unexpected C# imports %v", cSharpSummary.Imports)
	}

	kotlinSummary, err := kotlinParser.Parse("Main.kt", []byte("import kotlin.collections.List;\nimport foo.bar.Baz;\nclass C\n"))
	if err != nil {
		t.Fatalf("Parse Kotlin returned error: %v", err)
	}
	if !hasImport(kotlinSummary, "import kotlin.collections.List;") || !hasImport(kotlinSummary, "import foo.bar.Baz;") {
		t.Fatalf("unexpected Kotlin imports %v", kotlinSummary.Imports)
	}
}

func TestParseImportSyntaxPreservedForMappedLanguages(t *testing.T) {
	testCases := []struct {
		name     string
		ext      string
		fileName string
		source   string
		imports  []string
	}{
		{
			name:     "python",
			ext:      ".py",
			fileName: "main.py",
			source: `from pkg.module import Name as Alias
import os as operating_system
`,
			imports: []string{
				"from pkg.module import Name as Alias",
				"import os as operating_system",
			},
		},
		{
			name:     "javascript",
			ext:      ".js",
			fileName: "main.js",
			source: `import "./polyfill.js";
import {join as j} from "./util.js";
`,
			imports: []string{
				`import "./polyfill.js";`,
				`import {join as j} from "./util.js";`,
			},
		},
		{
			name:     "typescript",
			ext:      ".ts",
			fileName: "main.ts",
			source: `import type {Config} from "./types";
import "./setup";
`,
			imports: []string{
				`import type {Config} from "./types";`,
				`import "./setup";`,
			},
		},
		{
			name:     "rust",
			ext:      ".rs",
			fileName: "main.rs",
			source: `pub use crate::service::Worker as ServiceWorker;
use std::fmt::Debug;
`,
			imports: []string{
				"pub use crate::service::Worker as ServiceWorker;",
				"use std::fmt::Debug;",
			},
		},
		{
			name:     "java",
			ext:      ".java",
			fileName: "Main.java",
			source: `import static java.util.Collections.emptyList;
import java.util.List;
class Main {}
`,
			imports: []string{
				"import static java.util.Collections.emptyList;",
				"import java.util.List;",
			},
		},
		{
			name:     "c",
			ext:      ".c",
			fileName: "main.c",
			source: `#include <stdio.h>
#include "local.h"
int main(void) { return 0; }
`,
			imports: []string{
				"#include <stdio.h>",
				"#include \"local.h\"",
			},
		},
		{
			name:     "cpp",
			ext:      ".cpp",
			fileName: "main.cpp",
			source: `#include <vector>
#include "util.hpp"
`,
			imports: []string{
				"#include <vector>",
				"#include \"util.hpp\"",
			},
		},
		{
			name:     "c_sharp",
			ext:      ".cs",
			fileName: "Main.cs",
			source: `global using System.Text;
using System;
class C {}
`,
			imports: []string{
				"global using System.Text;",
				"using System;",
			},
		},
		{
			name:     "kotlin",
			ext:      ".kt",
			fileName: "Main.kt",
			source: `import foo.bar.Baz as Qux;
import kotlin.collections.List;
class C
`,
			imports: []string{
				"import foo.bar.Baz as Qux;",
				"import kotlin.collections.List;",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			entry := findEntryByExtension(t, tc.ext)
			parser, err := NewParser(entry)
			if err != nil {
				t.Fatalf("NewParser(%s) returned error: %v", tc.name, err)
			}
			summary, err := parser.Parse(tc.fileName, []byte(tc.source))
			if err != nil {
				t.Fatalf("Parse(%s) returned error: %v", tc.name, err)
			}
			for _, expectedImport := range tc.imports {
				if !hasImport(summary, expectedImport) {
					t.Fatalf("expected import %q in %s, got %v", expectedImport, tc.name, summary.Imports)
				}
			}
		})
	}
}

func TestParseIncrementalWithTree(t *testing.T) {
	entry := findEntryByExtension(t, ".go")

	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	oldSource := []byte(`package demo

func A() {}

func B() {
	A()
}
`)
	summaryOld, oldTree, err := parser.ParseWithTree("main.go", oldSource)
	if err != nil {
		t.Fatalf("ParseWithTree returned error: %v", err)
	}
	if oldTree == nil || oldTree.RootNode() == nil {
		t.Fatal("expected non-nil tree from ParseWithTree")
	}
	defer func() {
		if oldTree != nil {
			oldTree.Release()
		}
	}()

	newSource := []byte(`package demo

func A() {}

func B() {
	A()
	A()
}
`)
	summaryNew, newTree, err := parser.ParseIncrementalWithTree("main.go", newSource, oldSource, oldTree)
	if err != nil {
		t.Fatalf("ParseIncrementalWithTree returned error: %v", err)
	}
	if newTree == nil || newTree.RootNode() == nil {
		t.Fatal("expected non-nil tree from ParseIncrementalWithTree")
	}
	if summaryNew.Language != "go" || summaryOld.Language != "go" {
		t.Fatalf("unexpected language old=%q new=%q", summaryOld.Language, summaryNew.Language)
	}

	callCount := 0
	for _, reference := range summaryNew.References {
		if reference.Kind == "reference.call" && reference.Name == "A" {
			callCount++
		}
	}
	if callCount != 2 {
		t.Fatalf("expected 2 references to A after incremental parse, got %d", callCount)
	}

	if newTree != oldTree {
		newTree.Release()
	}
}

func TestSingleEdit(t *testing.T) {
	oldSrc := []byte("line1\nline2\nline3\n")
	newSrc := []byte("line1\nline-two\nline3\n")

	edit, ok := singleEdit(oldSrc, newSrc)
	if !ok {
		t.Fatal("expected singleEdit to report change")
	}
	if edit.StartByte == edit.OldEndByte {
		t.Fatalf("expected non-zero old edit span: %+v", edit)
	}
	if edit.StartPoint.Row != 1 {
		t.Fatalf("expected edit to start on row 1, got %+v", edit.StartPoint)
	}
	if pointAtOffset(oldSrc, len(oldSrc)).Row != 3 {
		t.Fatalf("unexpected pointAtOffset row for source end")
	}
}

func TestParseIncrementalWithTree_FileIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")

	entry := findEntryByExtension(t, ".go")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	original := []byte("package demo\n\nfunc A() {}\n")
	if err := os.WriteFile(sourcePath, original, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	summary, tree, err := parser.ParseWithTree(sourcePath, original)
	if err != nil {
		t.Fatalf("ParseWithTree returned error: %v", err)
	}
	if len(summary.Symbols) == 0 {
		t.Fatal("expected symbols from initial parse")
	}
	defer func() {
		if tree != nil {
			tree.Release()
		}
	}()

	edited := []byte("package demo\n\nfunc A() {}\nfunc B() { A() }\n")
	if err := os.WriteFile(sourcePath, edited, 0o644); err != nil {
		t.Fatalf("WriteFile edited failed: %v", err)
	}
	updated, newTree, err := parser.ParseIncrementalWithTree(sourcePath, edited, original, tree)
	if err != nil {
		t.Fatalf("ParseIncrementalWithTree returned error: %v", err)
	}
	if len(updated.Symbols) < 2 {
		t.Fatalf("expected updated symbols after edit, got %d", len(updated.Symbols))
	}
	if newTree != tree {
		newTree.Release()
	}
}

func TestParserConcurrentParse(t *testing.T) {
	entry := findEntryByExtension(t, ".go")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	source := []byte(`package demo

type Service struct{}

func Handle() {
	println("ok")
}
`)

	const workers = 16
	const iterations = 40

	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				summary, parseErr := parser.Parse(fmt.Sprintf("w%d_%d.go", worker, i), source)
				if parseErr != nil {
					errCh <- parseErr
					return
				}
				if !hasSymbol(summary, "function_definition", "Handle") {
					errCh <- fmt.Errorf("missing Handle symbol for worker=%d iteration=%d", worker, i)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestParserConcurrentParseFirstUse(t *testing.T) {
	entry := findEntryByExtension(t, ".go")

	source := []byte(`package demo

type Service struct{}

func Handle() {
	println("ok")
}
`)

	const rounds = 12
	const workers = 24

	for round := 0; round < rounds; round++ {
		parser, err := NewParser(entry)
		if err != nil {
			t.Fatalf("NewParser returned error: %v", err)
		}

		start := make(chan struct{})
		errCh := make(chan error, workers)
		var wg sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func(round, worker int) {
				defer wg.Done()
				<-start

				summary, parseErr := parser.Parse(fmt.Sprintf("round_%d_worker_%d.go", round, worker), source)
				if parseErr != nil {
					errCh <- parseErr
					return
				}
				if !hasSymbol(summary, "function_definition", "Handle") {
					errCh <- fmt.Errorf("missing Handle symbol for round=%d worker=%d", round, worker)
					return
				}
			}(round, worker)
		}
		close(start)
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatal(err)
		}
	}
}

func TestParserConcurrentMixedParseModes(t *testing.T) {
	entry := findEntryByExtension(t, ".go")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	sourceA := []byte(`package demo

type Service struct{}

func Handle() {
	println("A")
}
`)
	sourceB := []byte(`package demo

type Service struct{}

func Handle() {
	println("B")
}
`)

	const workers = 10
	const iterations = 30

	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()

			_, oldTree, parseErr := parser.ParseWithTree(fmt.Sprintf("seed_%d.go", worker), sourceA)
			if parseErr != nil {
				errCh <- fmt.Errorf("seed ParseWithTree returned error for worker=%d: %w", worker, parseErr)
				return
			}
			if oldTree == nil || oldTree.RootNode() == nil {
				errCh <- fmt.Errorf("seed ParseWithTree returned nil tree for worker=%d", worker)
				return
			}

			defer func() {
				if oldTree != nil {
					oldTree.Release()
				}
			}()

			currentSrc := sourceA
			nextSrc := sourceB

			<-start

			for i := 0; i < iterations; i++ {
				switch i % 3 {
				case 0:
					summary, newTree, incrementalErr := parser.ParseIncrementalWithTree(
						fmt.Sprintf("inc_%d_%d.go", worker, i),
						nextSrc,
						currentSrc,
						oldTree,
					)
					if incrementalErr != nil {
						if newTree != nil && newTree != oldTree {
							newTree.Release()
						}
						errCh <- fmt.Errorf("ParseIncrementalWithTree returned error for worker=%d iteration=%d: %w", worker, i, incrementalErr)
						return
					}
					if newTree != nil && newTree != oldTree {
						oldTree.Release()
						oldTree = newTree
					}
					if !hasSymbol(summary, "function_definition", "Handle") {
						errCh <- fmt.Errorf("missing Handle symbol for worker=%d iteration=%d mode=incremental", worker, i)
						return
					}
					currentSrc, nextSrc = nextSrc, currentSrc
				case 1:
					summary, parseErr := parser.Parse(fmt.Sprintf("parse_%d_%d.go", worker, i), currentSrc)
					if parseErr != nil {
						errCh <- fmt.Errorf("Parse returned error for worker=%d iteration=%d: %w", worker, i, parseErr)
						return
					}
					if !hasSymbol(summary, "function_definition", "Handle") {
						errCh <- fmt.Errorf("missing Handle symbol for worker=%d iteration=%d mode=parse", worker, i)
						return
					}
				default:
					summary, tree, withTreeErr := parser.ParseWithTree(fmt.Sprintf("withtree_%d_%d.go", worker, i), currentSrc)
					if tree != nil {
						tree.Release()
					}
					if withTreeErr != nil {
						errCh <- fmt.Errorf("ParseWithTree returned error for worker=%d iteration=%d: %w", worker, i, withTreeErr)
						return
					}
					if !hasSymbol(summary, "function_definition", "Handle") {
						errCh <- fmt.Errorf("missing Handle symbol for worker=%d iteration=%d mode=parse_with_tree", worker, i)
						return
					}
				}
			}
		}(worker)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

func TestParseIncrementalWithTree_ConcurrentSharedTree(t *testing.T) {
	entry := findEntryByExtension(t, ".go")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	oldSource := []byte(`package demo

func A() {
	println("old")
}
`)
	_, oldTree, err := parser.ParseWithTree("main.go", oldSource)
	if err != nil {
		t.Fatalf("ParseWithTree returned error: %v", err)
	}
	if oldTree == nil || oldTree.RootNode() == nil {
		t.Fatal("expected non-nil old tree")
	}
	defer oldTree.Release()

	newSource := []byte(`package demo

func A() {
	println("new")
}
`)

	const workers = 12

	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start

			summary, newTree, parseErr := parser.ParseIncrementalWithTree(fmt.Sprintf("worker_%d.go", worker), newSource, oldSource, oldTree)
			if newTree != nil && newTree != oldTree {
				defer newTree.Release()
			}
			if parseErr != nil {
				errCh <- parseErr
				return
			}
			if !hasSymbol(summary, "function_definition", "A") {
				errCh <- fmt.Errorf("missing A symbol for worker=%d", worker)
				return
			}
			if !hasReference(summary, "reference.call", "println") {
				errCh <- fmt.Errorf("missing println reference for worker=%d", worker)
				return
			}
		}(worker)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

// testFallbackTagsQueries mirrors the fallback queries in pkg/index/builder.go
// for RE-oriented languages whose grammars lack a standard tags query.
var testFallbackTagsQueries = map[string]string{
	"llvm": "(fn_define (function_header name: (global_var) @name)) @definition.function\n" +
		"(declare (function_header name: (global_var) @name)) @definition.function\n" +
		"(_ callee: (value (var (global_var) @name))) @reference.call",
	"asm":         "(label (ident) @name) @definition.function",
	"disassembly": "(code_location (identifier) @name) @definition.function",
}

func findEntryByExtension(tb testing.TB, extension string) grammars.LangEntry {
	tb.Helper()
	for _, entry := range grammars.AllLanguages() {
		for _, ext := range entry.Extensions {
			if ext == extension {
				entry.TagsQuery = grammars.ResolveTagsQuery(entry)
				if strings.TrimSpace(entry.TagsQuery) == "" {
					entry.TagsQuery = testFallbackTagsQueries[entry.Name]
				}
				if strings.TrimSpace(entry.TagsQuery) == "" {
					continue
				}
				return entry
			}
		}
	}
	tb.Fatalf("no language entry with tags query for extension %q", extension)
	return grammars.LangEntry{}
}

func hasSymbol(summary model.FileSummary, kind, name string) bool {
	return findSymbol(summary, kind, name) != nil
}

func findSymbol(summary model.FileSummary, kind, name string) *model.Symbol {
	for i := range summary.Symbols {
		symbol := summary.Symbols[i]
		if symbol.Kind == kind && symbol.Name == name {
			return &summary.Symbols[i]
		}
	}
	return nil
}

func hasReference(summary model.FileSummary, kind, name string) bool {
	for _, reference := range summary.References {
		if reference.Kind == kind && reference.Name == name {
			return true
		}
	}
	return false
}

func hasImport(summary model.FileSummary, importPath string) bool {
	for _, imp := range summary.Imports {
		if imp == importPath {
			return true
		}
	}
	return false
}
