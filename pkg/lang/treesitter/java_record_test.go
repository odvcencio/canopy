package treesitter

import "testing"

const javaRecordSample = `public record User(String id) {
    public String label() {
        return normalize(id);
    }

    private static String normalize(String value) {
        return value.trim();
    }
}
`

func TestJavaRecordDefinition(t *testing.T) {
	entry := findEntryByExtension(t, ".java")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("User.java", []byte(javaRecordSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !hasSymbol(summary, "type_definition", "User") {
		t.Errorf("missing Java record definition User")
	}
	if !hasSymbol(summary, "method_definition", "label") {
		t.Errorf("missing Java method definition label")
	}
	if !hasSymbol(summary, "method_definition", "normalize") {
		t.Errorf("missing Java method definition normalize")
	}
	if !hasReference(summary, "reference.call", "normalize") {
		t.Errorf("missing call reference normalize")
	}
}
