package query

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func BenchmarkParseSelector_Simple(b *testing.B) {
	for b.Loop() {
		_, _ = ParseSelector("function_definition")
	}
}

func BenchmarkParseSelector_WithName(b *testing.B) {
	for b.Loop() {
		_, _ = ParseSelector("function_definition[name=/^Test/]")
	}
}

func BenchmarkParseSelector_Complex(b *testing.B) {
	for b.Loop() {
		_, _ = ParseSelector("method_definition[name=/Serve/,signature=/ServeHTTP/,receiver=/Service/,file=/handler.go$/,start>=10,end<=50,line=20]")
	}
}

func BenchmarkSelectorMatch_Hit(b *testing.B) {
	sel, err := ParseSelector("method_definition[name=/Serve/,signature=/ServeHTTP/,receiver=/Service/,file=/handler.go$/,start>=10,end<=50]")
	if err != nil {
		b.Fatalf("ParseSelector: %v", err)
	}
	sym := model.Symbol{
		File:      "internal/http/handler.go",
		Kind:      "method_definition",
		Name:      "ServeHTTP",
		Signature: "func (s *Service) ServeHTTP()",
		Receiver:  "*Service",
		StartLine: 12,
		EndLine:   40,
	}
	b.ResetTimer()
	for b.Loop() {
		sel.Match(sym)
	}
}

func BenchmarkSelectorMatch_Miss(b *testing.B) {
	sel, err := ParseSelector("method_definition[name=/Serve/,signature=/ServeHTTP/,receiver=/Service/,file=/handler.go$/,start>=10,end<=50]")
	if err != nil {
		b.Fatalf("ParseSelector: %v", err)
	}
	sym := model.Symbol{
		File:      "internal/db/repo.go",
		Kind:      "function_definition",
		Name:      "Query",
		Signature: "func Query(ctx context.Context) error",
		StartLine: 100,
		EndLine:   120,
	}
	b.ResetTimer()
	for b.Loop() {
		sel.Match(sym)
	}
}
