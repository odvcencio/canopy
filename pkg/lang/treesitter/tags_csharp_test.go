package treesitter

import "testing"

const csharpNamespaceSample = `namespace Acme.Telemetry
{
    public interface IClock
    {
        void Tick();
    }

    public class Clock : IClock
    {
        public void Tick() {}
    }
}

namespace Utilities
{
    public class Parser {}
}
`

func TestCSharpNamespaceDeclarationsIndexedAsModules(t *testing.T) {
	entry := findEntryByExtension(t, ".cs")
	parser, err := NewParser(entry)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	summary, err := parser.Parse("sample.cs", []byte(csharpNamespaceSample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !hasSymbol(summary, "interface_definition", "IClock") {
		t.Error("missing C# interface IClock")
	}
	for _, cls := range []string{"Clock", "Parser"} {
		if !hasSymbol(summary, "class_definition", cls) {
			t.Errorf("missing C# class %s", cls)
		}
	}
	if !hasSymbol(summary, "method_definition", "Tick") {
		t.Error("missing C# methods Tick")
	}
	if !hasSymbol(summary, "module_definition", "Acme.Telemetry") {
		t.Error("missing C# namespace Acme.Telemetry")
	}
	if !hasSymbol(summary, "module_definition", "Utilities") {
		t.Error("missing C# namespace Utilities")
	}
}
