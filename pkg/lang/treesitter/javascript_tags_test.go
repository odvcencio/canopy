package treesitter

import "testing"

const javascriptArrowSample = `export const fetchUser = async (id) => {
  return normalize(id)
}

function normalize(value) {
  return value.trim()
}

fetchUser("42")
`

func TestJavaScriptArrowFunctionDefinition(t *testing.T) {
	entry := findEntryByExtension(t, ".js")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.js", []byte(javascriptArrowSample))
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
