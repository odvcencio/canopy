Return ONLY an applicable unified git diff, ending with a newline. No Markdown fences, commentary, ellipses, placeholder hunks, or `index` lines. Hunk line counts must exactly match their contents.

This is the single correction pass for a bounded Canopy TypeScript tag fix at the exact clean commit named by the harness. Touch only `pkg/lang/treesitter/tags.go` and the new `pkg/lang/treesitter/typescript_tags_test.go`.

The prior response's production design was sound and should be preserved:

1. Add a small `supplementalTagsQueries` map containing this TypeScript query:

```go
"typescript": strings.Join([]string{
	"(variable_declarator name: (identifier) @name value: (arrow_function)) @definition.function",
}, "\n"),
```

2. In `ResolveTagsQuery`, preserve a nonblank `entry.TagsQuery` exactly. Otherwise resolve `base` from `curatedTagsQueries[entry.Name]`, falling back to `grammars.ResolveTagsQuery(entry)`. Read the language supplemental query. Return `base` unchanged when either base or supplement is blank; otherwise return `base + "\n" + supplemental`.

3. Add a focused TypeScript parser test proving exported arrow `fetchUser` and declared `normalize` are `function_definition` symbols, while calls to `normalize` and `fetchUser` remain `reference.call` entries.

The prior response was rejected only because:
- `git apply --check` reported `corrupt patch at line 46` from incorrect hunk counts;
- the new test called nonexistent `extractSummary`;
- the response lacked a final newline.

Do not invent helpers. Existing test code must instantiate and call the parser this way:

```go
entry := findEntryByExtension(t, ".ts")
parser, err := NewParser(entry)
if err != nil {
	t.Fatalf("NewParser: %v", err)
}
summary, err := parser.Parse("sample.ts", []byte(typescriptArrowSample))
if err != nil {
	t.Fatalf("Parse: %v", err)
}
```

Existing same-package helpers are exactly:

```go
func findEntryByExtension(tb testing.TB, extension string) grammars.LangEntry
func hasSymbol(summary model.FileSummary, kind, name string) bool
func hasReference(summary model.FileSummary, kind, name string) bool
```

Current end of `curatedTagsQueries` and complete resolver:

```go
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

Test sample:

```typescript
export const fetchUser = async (id: string): Promise<string> => {
  return normalize(id)
}

function normalize(value: string): string {
  return value.trim()
}

fetchUser("42")
```

Re-emit the same sound production logic with a real, applicable diff and real parser test plumbing. End the response with a newline.
