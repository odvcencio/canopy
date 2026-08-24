Fix one bounded Canopy structural-index defect at the exact clean commit named by the harness.

Return ONLY a valid unified git diff. Do not use Markdown fences, commentary, ellipses, placeholder hunks, fake index OIDs, or unrelated changes. End the response with a newline.

Scope:
- Touch only `pkg/lang/treesitter/tags.go` and one focused TypeScript tag test file named `pkg/lang/treesitter/typescript_tags_test.go`.
- No dependency, generated-file, CLI, Ruby, Go-query, parser, documentation, or formatting-wide changes.
- Keep the implementation small and preserve caller-provided explicit tag queries exactly.

Proven defect:
- Canopy's resolved TypeScript tag query indexes ordinary function declarations but misses arrow functions assigned by a lexical declaration, a common exported API form.
- On this exact commit, a focused public-parser probe parsed the source successfully but failed:
  `missing exported arrow function definition fetchUser; symbols=[{Kind:function_definition Name:normalize ...}]`
- The real parsed tree for the one-line arrow declaration is:
  `(program (export_statement (lexical_declaration (variable_declarator (identifier) (arrow_function (formal_parameters (required_parameter (identifier) (type_annotation (predefined_type)))) (type_annotation (generic_type (type_identifier) (type_arguments (predefined_type)))) (call_expression (identifier) (arguments (identifier))))))))`

Required behavior:
- Supplement the inferred TypeScript tag query with a precise `variable_declarator` + `arrow_function` definition capture so the declarator's identifier becomes `@name` and the declaration becomes `@definition.function`.
- Do not replace or narrow the existing inferred TypeScript query: ordinary function/class/method definitions and call references must continue to resolve.
- Preserve the existing priority contract: a nonblank `entry.TagsQuery` remains an exact caller override and is returned unchanged.
- Preserve the complete curated Go and Ruby queries unchanged.
- Add a focused parser regression test using the existing helpers. It must prove an exported async arrow function is a `function_definition`, an ordinary declared function remains a `function_definition`, and calls in the sample remain `reference.call` entries.
- Do not add abstractions beyond the smallest per-language supplemental-query mechanism needed for this case.

Current `pkg/lang/treesitter/tags.go` follows verbatim:

```go
package treesitter

import (
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"
)

// curatedTagsQueries holds canopy-owned, complete tree-sitter tag queries for
// core languages.
//
// Canopy prefers these over gotreesitter's inferred queries because the
// inferred per-language overrides can be narrower than canopy needs. In
// particular, gotreesitter's Go override emits only function/method/call
// captures and drops `type_declaration` capture entirely; canopy's type
// metrics, method-set mapping, and refactor rename all depend on type
// definition symbols. Owning the query for core languages keeps canopy's
// symbol extraction stable across upstream inference changes.
//
// The map is the seed of a per-language calibration layer; add an entry for a
// language whenever the inferred query proves too narrow for canopy's analyses.
var curatedTagsQueries = map[string]string{
	"go": strings.Join([]string{
		"(function_declaration name: (identifier) @name) @definition.function",
		"(method_declaration name: (field_identifier) @name) @definition.method",
		"(type_declaration (type_spec name: (type_identifier) @name)) @definition.type",
		"(type_declaration (type_alias name: (type_identifier) @name)) @definition.type",
		"(call_expression function: (identifier) @name) @reference.call",
		"(call_expression function: (selector_expression field: (field_identifier) @name)) @reference.call",
	}, "\n"),
	"ruby": strings.Join([]string{
		// Containers: class and module names are constants.
		"(class name: (constant) @name) @definition.class",
		"(module name: (constant) @name) @definition.module",
		// Ordinary instance methods.
		"(method name: (identifier) @name) @definition.method",
		// Singleton / class methods (def self.foo).
		"(singleton_method name: (identifier) @name) @definition.method",
		// Call references; method names may be identifiers or constants.
		"(call method: [(identifier) @name (constant) @name]) @reference.call",
	}, "\n"),
}

// ResolveTagsQuery returns the tree-sitter tags query canopy should use for a
// language entry, in priority order:
//
//  1. an explicit query already set on the entry (caller override),
//  2. canopy's curated query for the language,
//  3. gotreesitter's inferred query.
//
// It returns "" when none of the above yields a query.
func ResolveTagsQuery(entry grammars.LangEntry) string {
	if q := strings.TrimSpace(entry.TagsQuery); q != "" {
		return entry.TagsQuery
	}
	if q := curatedTagsQueries[entry.Name]; strings.TrimSpace(q) != "" {
		return q
	}
	return grammars.ResolveTagsQuery(entry)
}
```

Existing test helpers in the same package:

```go
func findEntryByExtension(tb testing.TB, extension string) grammars.LangEntry {
	tb.Helper()
	for _, entry := range grammars.AllLanguages() {
		for _, ext := range entry.Extensions {
			if ext == extension {
				entry.TagsQuery = ResolveTagsQuery(entry)
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

func hasReference(summary model.FileSummary, kind, name string) bool {
	for _, reference := range summary.References {
		if reference.Kind == kind && reference.Name == name {
			return true
		}
	}
	return false
}
```

A suitable regression sample is:

```typescript
export const fetchUser = async (id: string): Promise<string> => {
  return normalize(id)
}

function normalize(value: string): string {
  return value.trim()
}

fetchUser("42")
```

The new focused test file should remain self-contained except for those existing same-package helpers. Implement the smallest correct change and return only the diff.
