package treesitter

import "testing"

const kotlinObjectSample = `object Registry {
    fun normalize(value: String): String {
        return value.trim()
    }
}

fun main() {
    Registry.normalize(" 42 ")
}
`

func TestKotlinObjectDeclarationIndexedAsTypeDefinition(t *testing.T) {
	entry := findEntryByExtension(t, ".kt")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.kt", []byte(kotlinObjectSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !hasSymbol(summary, "type_definition", "Registry") {
		t.Fatalf("missing Kotlin object definition Registry; symbols=%v", summary.Symbols)
	}
}
