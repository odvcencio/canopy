package treesitter

import "testing"

const rustTypeAliasSample = `pub type UserId = u64;

pub fn normalize(id: UserId) -> UserId {
    id
}

fn main() {
    normalize(42);
}
`

func TestRustTypeAliasDefinition(t *testing.T) {
	entry := findEntryByExtension(t, ".rs")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.rs", []byte(rustTypeAliasSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !hasSymbol(summary, "type_definition", "UserId") {
		t.Fatalf("missing type alias; symbols=%v", summary.Symbols)
	}
	if !hasSymbol(summary, "function_definition", "normalize") {
		t.Fatalf("missing function definition; symbols=%v", summary.Symbols)
	}
	if !hasReference(summary, "reference.call", "normalize") {
		t.Fatalf("missing call reference; references=%v", summary.References)
	}
}
