# Canopy Ruby qualified-container correction

Return only an applicable Git unified diff. Its first bytes must be
`diff --git ` and its last byte must be a newline. No Markdown fences, prose,
placeholder index hashes, empty hunks, or omitted context. You have no tools.

The prior response was rejected without application because its hunk counts
were wrong and its test diff contained an empty/incomplete hunk. Produce a new
self-contained diff against exact base
`8ae0e520b9c286baa9f6935e02be93b69e2f08e0`.

Bounded change:

- Extend only the existing Ruby curated tag query so qualified declarations
  shaped as `(class (scope_resolution (constant) (constant)))` and
  `(module (scope_resolution (constant) (constant)))` emit the terminal
  constant as `@name`.
- Add one focused parser regression for `class Billing::Invoice` and
  `module API::V2`; require symbols `Invoice` and `V2`.
- Preserve bare containers, all other Ruby tags, and other languages.
- Change only `pkg/lang/treesitter/tags.go` and
  `pkg/lang/treesitter/ruby_tags_test.go`.

Exact current Ruby production block in `tags.go`:

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
```

Exact full current `ruby_tags_test.go`:

```go
package treesitter

import "testing"

const rubyCallableSample = `
module Payments
  class Invoice
    def initialize(total)
      @total = total
    end

    def total_with_tax(rate)
      tax_rate(rate)
      @total * rate + @total
    end

    def self.default_rate
      lookup_rate(:standard)
    end
  end
end
`

func TestRubyCallableExtraction(t *testing.T) {
	entry := findEntryByExtension(t, ".rb")
	if entry.Name != "ruby" {
		t.Fatalf("expected ruby entry, got %q", entry.Name)
	}
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.rb", []byte(rubyCallableSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Containers.
	if !hasSymbol(summary, "module_definition", "Payments") {
		t.Errorf("missing module definition Payments")
	}
	if !hasSymbol(summary, "class_definition", "Invoice") {
		t.Errorf("missing class definition Invoice")
	}

	// Ordinary instance methods.
	for _, name := range []string{"initialize", "total_with_tax"} {
		if !hasSymbol(summary, "method_definition", name) {
			t.Errorf("missing method definition %s", name)
		}
	}

	// Singleton / class method.
	if !hasSymbol(summary, "method_definition", "default_rate") {
		t.Errorf("missing singleton method definition default_rate")
	}

	// Call references.
	for _, name := range []string{"tax_rate", "lookup_rate"} {
		if !hasReference(summary, "reference.call", name) {
			t.Errorf("missing call reference %s", name)
		}
	}
}
```

Use complete, correctly counted hunks. The reviewer, not you, will run tests.
