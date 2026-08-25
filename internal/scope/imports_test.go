package scope

import (
	"reflect"
	"testing"
)

func TestImportBindingNames(t *testing.T) {
	tests := []struct {
		name        string
		language    string
		declaration string
		want        []string
	}{
		{name: "go path", language: "go", declaration: "github.com/acme/log", want: []string{"log"}},
		{name: "go alias", language: "go", declaration: `trace "github.com/acme/log"`, want: []string{"trace"}},
		{name: "go blank", language: "go", declaration: `_ "github.com/acme/driver"`},
		{name: "python modules", language: "python", declaration: "import os, pathlib as paths", want: []string{"os", "paths"}},
		{name: "python from", language: "python", declaration: "from pkg.models import User, Team as Group", want: []string{"User", "Group"}},
		{name: "javascript default and named", language: "javascript", declaration: `import React, {useState, useMemo as memo} from "react";`, want: []string{"useState", "memo", "React"}},
		{name: "typescript namespace", language: "typescript", declaration: `import * as schema from "./schema";`, want: []string{"schema"}},
		{name: "rust alias", language: "rust", declaration: "pub use crate::service::Worker as ServiceWorker;", want: []string{"ServiceWorker"}},
		{name: "rust group", language: "rust", declaration: "use std::io::{self, Read, Write as Writer};", want: []string{"io", "Read", "Writer"}},
		{name: "java type", language: "java", declaration: "import java.util.List;", want: []string{"List"}},
		{name: "java wildcard", language: "java", declaration: "import java.util.*;"},
		{name: "kotlin alias", language: "kotlin", declaration: "import foo.bar.Baz as Qux", want: []string{"Qux"}},
		{name: "cpp include", language: "cpp", declaration: "#include <vector>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := importBindingNames(tt.language, tt.declaration); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("importBindingNames(%q, %q) = %v, want %v", tt.language, tt.declaration, got, tt.want)
			}
		})
	}
}
