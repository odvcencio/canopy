package treesitter

import "testing"

const cppNamespaceSample = `namespace telemetry {
void emit() {}
}

int main() {
    telemetry::emit();
}
`

func TestTagsCppNamespaceDefinition(t *testing.T) {
	entry := findEntryByExtension(t, ".cpp")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.cpp", []byte(cppNamespaceSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !hasSymbol(summary, "function_definition", "emit") {
		t.Fatalf("missing function_definition emit; symbols=%v", summary.Symbols)
	}
	if !hasSymbol(summary, "function_definition", "main") {
		t.Fatalf("missing function_definition main; symbols=%v", summary.Symbols)
	}
	if !hasSymbol(summary, "module_definition", "telemetry") {
		t.Fatalf("missing module_definition telemetry; symbols=%v", summary.Symbols)
	}
}
