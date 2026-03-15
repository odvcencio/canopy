package proxy

import "testing"

func TestRouteCategory(t *testing.T) {
	tests := []struct {
		method string
		want   RouteCategory
	}{
		{"textDocument/definition", RouteBackendWins},
		{"textDocument/completion", RouteBackendWins},
		{"textDocument/rename", RouteBackendWins},
		{"textDocument/signatureHelp", RouteBackendWins},
		{"textDocument/formatting", RouteBackendWins},
		{"textDocument/codeAction", RouteBackendWins},
		{"textDocument/hover", RouteMerge},
		{"textDocument/references", RouteMerge},
		{"textDocument/codeLens", RouteMerge},
		{"textDocument/documentHighlight", RouteMerge},
		{"orchard/impactAnalysis", RouteNative},
		{"orchard/callGraph", RouteNative},
		{"orchard/entityBlame", RouteNative},
		{"orchard/feedStatus", RouteNative},
		{"textDocument/documentSymbol", RouteNative},
		{"workspace/symbol", RouteNative},
		{"unknown/method", RouteNative},
	}
	for _, tt := range tests {
		got := Categorize(tt.method)
		if got != tt.want {
			t.Errorf("Categorize(%q) = %v, want %v", tt.method, got, tt.want)
		}
	}
}
