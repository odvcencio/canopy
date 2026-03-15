package proxy

// RouteCategory determines how a request is handled relative to backend LSPs.
type RouteCategory int

const (
	RouteNative RouteCategory = iota
	RouteBackendWins
	RouteMerge
)

var backendWinsMethods = map[string]bool{
	"textDocument/definition":    true,
	"textDocument/completion":    true,
	"textDocument/rename":        true,
	"textDocument/signatureHelp": true,
	"textDocument/formatting":    true,
	"textDocument/codeAction":    true,
}

var mergeMethods = map[string]bool{
	"textDocument/hover":             true,
	"textDocument/references":        true,
	"textDocument/codeLens":          true,
	"textDocument/documentHighlight": true,
}

// Categorize returns the routing category for an LSP method.
func Categorize(method string) RouteCategory {
	if backendWinsMethods[method] {
		return RouteBackendWins
	}
	if mergeMethods[method] {
		return RouteMerge
	}
	return RouteNative
}

func (c RouteCategory) String() string {
	switch c {
	case RouteBackendWins:
		return "backend-wins"
	case RouteMerge:
		return "merge"
	default:
		return "native"
	}
}
