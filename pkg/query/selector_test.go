package query

import (
	"strings"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestParseSelector_WithNameFilter(t *testing.T) {
	selector, err := ParseSelector("function_definition[name=/^Test.+/]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	if selector.Kind != "function_definition" {
		t.Fatalf("expected kind function_definition, got %q", selector.Kind)
	}
	if selector.NameRE == nil {
		t.Fatal("expected selector.NameRE to be set")
	}
}

func TestParseSelector_InvalidKind(t *testing.T) {
	_, err := ParseSelector("FunctionDefinition[name=/test/]")
	if err == nil {
		t.Fatal("expected ParseSelector to fail")
	}
}

func TestParseSelector_MultipleFilters(t *testing.T) {
	selector, err := ParseSelector("method_definition[name=/Serve/,signature=/ServeHTTP/,receiver=/Service/,file=/handler.go$/,start>=10,end<=50,line=20]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}
	if selector.SignatureRE == nil || selector.ReceiverRE == nil || selector.FileRE == nil {
		t.Fatal("expected signature, receiver, and file filters")
	}
	if selector.StartMin == nil || *selector.StartMin != 10 {
		t.Fatalf("unexpected start min: %#v", selector.StartMin)
	}
	if selector.EndMax == nil || *selector.EndMax != 50 {
		t.Fatalf("unexpected end max: %#v", selector.EndMax)
	}
	if selector.Line == nil || *selector.Line != 20 {
		t.Fatalf("unexpected line filter: %#v", selector.Line)
	}
}

func TestParseSelector_InvalidStartRange(t *testing.T) {
	_, err := ParseSelector("function_definition[start>=10,start<=5]")
	if err == nil {
		t.Fatal("expected ParseSelector to fail for impossible start range")
	}
	if !strings.Contains(err.Error(), "invalid start range") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelectorMatch(t *testing.T) {
	selector, err := ParseSelector("method_definition[name=/Serve/,signature=/ServeHTTP/,receiver=/Service/,file=/handler.go$/,start>=10,end<=20,line=12]")
	if err != nil {
		t.Fatalf("ParseSelector returned error: %v", err)
	}

	ok := selector.Match(model.Symbol{
		File:      "internal/http/handler.go",
		Kind:      "method_definition",
		Name:      "ServeHTTP",
		Signature: "func (s *Service) ServeHTTP()",
		Receiver:  "*Service",
		StartLine: 10,
		EndLine:   14,
	})
	if !ok {
		t.Fatal("expected selector to match symbol")
	}

	miss := selector.Match(model.Symbol{
		File:      "internal/http/handler.go",
		Kind:      "method_definition",
		Name:      "ServeHTTP",
		Signature: "func (s *Service) ServeHTTP()",
		Receiver:  "*Service",
		StartLine: 22,
		EndLine:   30,
	})
	if miss {
		t.Fatal("expected selector not to match symbol outside filtered line range")
	}
}
