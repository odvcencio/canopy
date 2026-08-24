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
