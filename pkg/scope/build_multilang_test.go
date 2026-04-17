package scope

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func mustParseLang(t *testing.T, filename, src string) (*gotreesitter.Tree, *gotreesitter.Language, string) {
	t.Helper()
	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		t.Fatalf("grammar not found for %s", filename)
	}
	lang := entry.Language()
	parser := gotreesitter.NewParser(lang)
	srcBytes := []byte(src)
	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		tree, err = parser.ParseWithTokenSource(srcBytes, entry.TokenSourceFactory(srcBytes, lang))
	} else {
		tree, err = parser.Parse(srcBytes)
	}
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	return tree, lang, entry.Name
}

func TestBuildPythonScope(t *testing.T) {
	src := "import os\nfrom pathlib import Path\n\ndef greet(name):\n    msg = \"hello\"\n    return msg\n\nclass Server:\n    def start(self):\n        pass\n"

	tree, lang, langName := mustParseLang(t, "test.py", src)
	rules, err := LoadRules(langName, lang)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	scope := BuildFileScope(tree, lang, []byte(src), rules, "test.py")

	names := map[string]bool{}
	for _, d := range scope.Defs {
		names[d.Name] = true
	}

	for _, want := range []string{"os", "Path", "greet", "Server"} {
		if !names[want] {
			t.Errorf("expected def %q, got defs: %+v", want, scope.Defs)
		}
	}
}

func TestBuildTypeScriptScope(t *testing.T) {
	// Note: the pure Go TypeScript parser does not handle 'class' declarations
	// correctly, so we use a type alias instead of a class for the App symbol.
	src := "import { useState } from 'react';\n\ninterface Props {\n    name: string;\n}\n\nfunction Greeting(props: Props) {\n    const count = 0;\n    return props.name;\n}\n\ntype App = Props;\n"

	tree, lang, langName := mustParseLang(t, "test.ts", src)
	rules, err := LoadRules(langName, lang)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}

	scope := BuildFileScope(tree, lang, []byte(src), rules, "test.ts")

	names := map[string]bool{}
	for _, d := range scope.Defs {
		names[d.Name] = true
	}

	for _, want := range []string{"useState", "Props", "Greeting", "App"} {
		if !names[want] {
			t.Errorf("expected def %q, got defs: %+v", want, scope.Defs)
		}
	}
}
