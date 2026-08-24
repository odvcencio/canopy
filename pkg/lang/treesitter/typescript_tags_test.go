package treesitter

import "testing"

const typescriptArrowSample = `export const fetchUser = async (id: string): Promise<string> => {
  return normalize(id)
}

function normalize(value: string): string {
  return value.trim()
}

fetchUser("42")
`

func TestTypeScriptArrowFunctionDefinition(t *testing.T) {
	entry := findEntryByExtension(t, ".ts")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.ts", []byte(typescriptArrowSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !hasSymbol(summary, "function_definition", "fetchUser") {
		t.Fatalf("missing exported arrow function definition fetchUser; symbols=%v", summary.Symbols)
	}
	if !hasSymbol(summary, "function_definition", "normalize") {
		t.Fatalf("missing ordinary function definition normalize; symbols=%v", summary.Symbols)
	}
	if !hasReference(summary, "reference.call", "normalize") {
		t.Fatalf("missing call reference normalize; references=%v", summary.References)
	}
	if !hasReference(summary, "reference.call", "fetchUser") {
		t.Fatalf("missing call reference fetchUser; references=%v", summary.References)
	}
}
