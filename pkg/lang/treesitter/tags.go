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

// supplementalTagsQueries holds small per-language capture additions appended
// to a resolved inferred or curated query. Each entry patches a single known
// gap without replacing the base query, which must keep resolving all of its
// existing captures.
var supplementalTagsQueries = map[string]string{
	// gotreesitter's inferred TypeScript tag query indexes ordinary function
	// declarations but misses arrow functions bound by lexical declarations,
	// a common exported API form. Capture the declarator's identifier as the
	// definition name.
	"typescript": strings.Join([]string{
		"(variable_declarator name: (identifier) @name value: (arrow_function)) @definition.function",
	}, "\n"),
	// The same gap exists for JavaScript, where exported arrow functions are
	// an equally common API form.
	"javascript": strings.Join([]string{
		"(variable_declarator name: (identifier) @name value: (arrow_function)) @definition.function",
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
	if strings.TrimSpace(entry.TagsQuery) != "" {
		return entry.TagsQuery
	}
	base := curatedTagsQueries[entry.Name]
	if strings.TrimSpace(base) == "" {
		base = grammars.ResolveTagsQuery(entry)
	}
	supplemental := supplementalTagsQueries[entry.Name]
	if strings.TrimSpace(base) == "" || strings.TrimSpace(supplemental) == "" {
		return base
	}
	return base + "\n" + supplemental
}
