package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/canopy/pkg/feeds"
	feedcompiler "github.com/odvcencio/canopy/pkg/feeds/compiler"
	feedparser "github.com/odvcencio/canopy/pkg/feeds/parser"
	feedvcs "github.com/odvcencio/canopy/pkg/feeds/vcs"
	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/proxy"
	"github.com/odvcencio/canopy/pkg/scope"
	"github.com/odvcencio/canopy/pkg/socket"
)

// Service holds workspace state and handles LSP requests.
type Service struct {
	mu               sync.RWMutex
	rootURI          string
	rootPath         string
	idx              *model.Index
	builder          *index.Builder
	scopeGraph       *scope.Graph
	feedEngine       *feeds.Engine
	proxyMgr         *proxy.Manager
	socketSrv        *socket.Server
	feedsInitialized bool
}

func NewService(proxyMgr *proxy.Manager) *Service {
	engine := feeds.NewEngine(slog.Default())
	engine.Register(feedparser.New())
	return &Service{
		builder:    index.NewBuilder(),
		feedEngine: engine,
		proxyMgr:   proxyMgr,
	}
}

// Register wires all LSP handlers onto a Server.
func (s *Service) Register(srv *Server) {
	srv.Handle("initialize", s.handleInitialize)
	srv.Handle("shutdown", s.handleShutdown)
	srv.Handle("textDocument/documentSymbol", s.handleDocumentSymbol)
	srv.Handle("workspace/symbol", s.handleWorkspaceSymbol)
	srv.Handle("textDocument/definition", s.handleDefinition)
	srv.Handle("textDocument/references", s.handleReferences)
	srv.Handle("textDocument/hover", s.handleHover)
	srv.Handle("textDocument/rename", s.handleRename)

	srv.OnNotify("initialized", func(params json.RawMessage) {
		s.buildIndex()
	})
	srv.OnNotify("textDocument/didOpen", s.handleDidOpen)
	srv.OnNotify("textDocument/didSave", s.handleDidSave)
	srv.OnNotify("textDocument/didChange", s.handleDidChange)
	srv.OnNotify("textDocument/didClose", s.handleDidClose)
	srv.OnNotify("exit", func(params json.RawMessage) {})
}

func (s *Service) handleInitialize(params json.RawMessage) (any, error) {
	var p InitializeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	s.rootURI = p.RootURI
	s.rootPath = uriToPath(p.RootURI)
	if s.rootPath == "" {
		s.rootPath = p.RootPath
	}

	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:        SyncFull,
			DocumentSymbolProvider:  true,
			WorkspaceSymbolProvider: true,
			DefinitionProvider:      true,
			ReferencesProvider:      true,
			HoverProvider:           true,
			RenameProvider:          true,
		},
		ServerInfo: &ServerInfo{Name: "canopyls", Version: "0.1.0"},
	}, nil
}

func (s *Service) handleShutdown(params json.RawMessage) (any, error) {
	if s.socketSrv != nil {
		s.socketSrv.Stop()
	}
	return nil, nil
}

// proxyRequest tries to forward a request to the backend LSP.
// Returns (result, true) if backend handled it, (nil, false) to fall back to native.
func (s *Service) proxyRequest(method string, params json.RawMessage, fileURI string) (any, bool) {
	if s.proxyMgr == nil {
		return nil, false
	}
	cat := proxy.Categorize(method)
	if cat != proxy.RouteBackendWins {
		return nil, false
	}
	file := uriToPath(fileURI)
	b := s.proxyMgr.BackendForFile(file)
	if b == nil {
		return nil, false
	}
	result, err := b.Request(method, params)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(result), true
}

// initFeeds registers VCS and compiler feeds once we know the workspace root.
func (s *Service) initFeeds() {
	if s.feedsInitialized {
		return
	}
	s.feedsInitialized = true
	if vcsFeed := feedvcs.Detect(s.rootPath); vcsFeed != nil {
		s.feedEngine.Register(vcsFeed)
	}
	if compilerFeed := feedcompiler.Detect(slog.Default()); compilerFeed != nil {
		s.feedEngine.Register(compilerFeed)
	}
}

func (s *Service) buildIndex() {
	if s.rootPath == "" {
		return
	}

	s.initFeeds()

	if s.socketSrv == nil {
		s.socketSrv = s.StartSocket()
	}

	idx, err := s.builder.BuildPath(s.rootPath)
	if err != nil {
		return
	}

	graph := scope.NewGraph()
	ctx := &feeds.FeedContext{
		WorkspaceRoot: s.rootPath,
		Logger:        slog.Default(),
	}
	for _, f := range idx.Files {
		src, readErr := os.ReadFile(filepath.Join(s.rootPath, f.Path))
		if readErr != nil {
			continue
		}
		s.feedEngine.RunFile(graph, f.Path, src, f.Language, ctx)
	}

	for _, fs := range graph.FileScopes {
		scope.ResolveAllGraph(fs, graph)
	}

	s.mu.Lock()
	s.idx = idx
	s.scopeGraph = graph
	s.mu.Unlock()
}

// StartSocket starts the Unix socket server for CLI client queries.
// Call after handleInitialize sets rootPath.
func (s *Service) StartSocket() *socket.Server {
	if s.rootPath == "" {
		return nil
	}
	srv := socket.NewServer(s.rootPath, slog.Default())

	srv.Handle("feeds", func(params json.RawMessage) (any, error) {
		var result []map[string]any
		for _, f := range s.feedEngine.Feeds() {
			h := s.feedEngine.Health(f.Name())
			entry := map[string]any{
				"name":     f.Name(),
				"priority": f.Priority(),
				"active":   !h.Disabled,
				"health":   "healthy",
			}
			if h.Disabled {
				entry["health"] = "disabled"
			}
			if h.LastError != nil {
				entry["error"] = h.LastError.Error()
				entry["health"] = "degraded"
			}
			if !h.LastRun.IsZero() {
				entry["lastRun"] = h.LastRun.Format(time.RFC3339)
			}
			result = append(result, entry)
		}
		return result, nil
	})

	srv.Handle("refs", func(params json.RawMessage) (any, error) {
		var p struct {
			Symbol string `json:"symbol"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Symbol == "" {
			return nil, fmt.Errorf("symbol required")
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.idx == nil {
			return []any{}, nil
		}
		var refs []map[string]any
		for _, f := range s.idx.Files {
			for _, ref := range f.References {
				if ref.Name == p.Symbol {
					refs = append(refs, map[string]any{
						"file":   f.Path,
						"line":   ref.StartLine,
						"column": ref.StartColumn,
					})
				}
			}
		}
		return refs, nil
	})

	srv.Handle("blame", func(params json.RawMessage) (any, error) {
		var p struct {
			File string `json:"file"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.File == "" {
			return nil, fmt.Errorf("file required")
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.scopeGraph == nil {
			return []any{}, nil
		}
		fs := s.scopeGraph.FileScope(p.File)
		if fs == nil {
			return []any{}, nil
		}
		var entities []map[string]any
		for _, d := range fs.Defs {
			entry := map[string]any{
				"name":      d.Name,
				"kind":      d.Kind,
				"startLine": d.Loc.StartLine,
				"endLine":   d.Loc.EndLine,
			}
			if author, ok := scope.GetMeta[string](&d, "vcs.last_author"); ok {
				entry["author"] = author
			}
			if commit, ok := scope.GetMeta[string](&d, "vcs.last_commit"); ok {
				entry["commit"] = commit
			}
			entities = append(entities, entry)
		}
		return entities, nil
	})

	srv.Handle("impact", func(params json.RawMessage) (any, error) {
		var p struct {
			Symbol string `json:"symbol"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Symbol == "" {
			return nil, fmt.Errorf("symbol required")
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.idx == nil {
			return map[string]any{"symbol": p.Symbol, "affected": []any{}}, nil
		}
		var affected []map[string]any
		for _, f := range s.idx.Files {
			for _, ref := range f.References {
				if ref.Name == p.Symbol {
					affected = append(affected, map[string]any{
						"name": ref.Name,
						"file": f.Path,
						"line": ref.StartLine,
					})
				}
			}
		}
		return map[string]any{"symbol": p.Symbol, "affected": affected}, nil
	})

	if err := srv.Start(); err != nil {
		slog.Error("failed to start socket server", "error", err)
		return nil
	}
	return srv
}

func (s *Service) handleDocumentSymbol(params json.RawMessage) (any, error) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	path := uriToPath(p.TextDocument.URI)
	relPath := relativeTo(path, s.rootPath)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.idx == nil {
		return []DocumentSymbol{}, nil
	}

	for _, f := range s.idx.Files {
		if f.Path == relPath {
			return symbolsToDocumentSymbols(f.Symbols), nil
		}
	}
	return []DocumentSymbol{}, nil
}

func (s *Service) handleWorkspaceSymbol(params json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.idx == nil {
		return []SymbolInformation{}, nil
	}

	query := strings.ToLower(p.Query)
	var results []SymbolInformation
	for _, f := range s.idx.Files {
		for _, sym := range f.Symbols {
			if query == "" || strings.Contains(strings.ToLower(sym.Name), query) {
				results = append(results, SymbolInformation{
					Name: sym.Name,
					Kind: symbolKindFromModel(sym.Kind),
					Location: LSPLocation{
						URI:   pathToURI(f.Path, s.rootPath),
						Range: symbolRange(sym),
					},
				})
			}
		}
	}
	return results, nil
}

func (s *Service) handleDidOpen(params json.RawMessage) {
	if s.proxyMgr == nil {
		return
	}
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &p) == nil {
		file := uriToPath(p.TextDocument.URI)
		if b := s.proxyMgr.BackendForFile(file); b != nil {
			b.Notify("textDocument/didOpen", params)
		}
	}
}

func (s *Service) handleDidSave(params json.RawMessage) {
	// Forward to backend
	if s.proxyMgr != nil {
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if json.Unmarshal(params, &p) == nil {
			file := uriToPath(p.TextDocument.URI)
			if b := s.proxyMgr.BackendForFile(file); b != nil {
				b.Notify("textDocument/didSave", params)
			}
		}
	}

	// Existing index rebuild code follows...
	if s.rootPath == "" {
		return
	}
	s.mu.RLock()
	prev := s.idx
	s.mu.RUnlock()

	newIdx, _, err := s.builder.BuildPathIncremental(context.Background(), s.rootPath, prev)
	if err != nil {
		return
	}

	graph := scope.NewGraph()
	ctx := &feeds.FeedContext{
		WorkspaceRoot: s.rootPath,
		Logger:        slog.Default(),
	}
	for _, f := range newIdx.Files {
		src, readErr := os.ReadFile(filepath.Join(s.rootPath, f.Path))
		if readErr != nil {
			continue
		}
		s.feedEngine.RunFile(graph, f.Path, src, f.Language, ctx)
	}

	for _, fs := range graph.FileScopes {
		scope.ResolveAllGraph(fs, graph)
	}

	s.mu.Lock()
	s.idx = newIdx
	s.scopeGraph = graph
	s.mu.Unlock()
}

func (s *Service) handleDidChange(params json.RawMessage) {
	if s.proxyMgr == nil {
		return
	}
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &p) == nil {
		file := uriToPath(p.TextDocument.URI)
		if b := s.proxyMgr.BackendForFile(file); b != nil {
			b.Notify("textDocument/didChange", params)
		}
	}
}

func (s *Service) handleDidClose(params json.RawMessage) {
	if s.proxyMgr == nil {
		return
	}
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &p) == nil {
		file := uriToPath(p.TextDocument.URI)
		if b := s.proxyMgr.BackendForFile(file); b != nil {
			b.Notify("textDocument/didClose", params)
		}
	}
}

func (s *Service) handleDefinition(params json.RawMessage) (any, error) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Try proxy backend first
	if result, ok := s.proxyRequest("textDocument/definition", params, p.TextDocument.URI); ok {
		return result, nil
	}

	path := uriToPath(p.TextDocument.URI)
	relPath := relativeTo(path, s.rootPath)
	line := p.Position.Line + 1 // LSP is 0-based, model is 1-based

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.idx == nil {
		return nil, nil
	}

	// Try scope graph resolution first
	if s.scopeGraph != nil {
		fs := s.scopeGraph.FileScope(relPath)
		if fs != nil {
			for i := range fs.Refs {
				ref := &fs.Refs[i]
				if ref.Loc.StartLine == line && ref.Resolved != nil {
					return LSPLocation{
						URI: pathToURI(ref.Resolved.Loc.File, s.rootPath),
						Range: Range{
							Start: Position{Line: ref.Resolved.Loc.StartLine - 1, Character: ref.Resolved.Loc.StartCol},
							End:   Position{Line: ref.Resolved.Loc.EndLine - 1, Character: ref.Resolved.Loc.EndCol},
						},
					}, nil
				}
			}
		}
	}

	// Fall back to name-based resolution
	symbolName := s.symbolNameAtPosition(relPath, line, p.Position.Character)
	if symbolName == "" {
		return nil, nil
	}

	// Search for matching definition across the index
	for _, f := range s.idx.Files {
		for _, sym := range f.Symbols {
			if sym.Name == symbolName {
				return LSPLocation{
					URI:   pathToURI(f.Path, s.rootPath),
					Range: symbolRange(sym),
				}, nil
			}
		}
	}
	return nil, nil
}

func (s *Service) handleReferences(params json.RawMessage) (any, error) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Try proxy backend first
	if result, ok := s.proxyRequest("textDocument/references", params, p.TextDocument.URI); ok {
		return result, nil
	}

	path := uriToPath(p.TextDocument.URI)
	relPath := relativeTo(path, s.rootPath)
	line := p.Position.Line + 1

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.idx == nil {
		return []LSPLocation{}, nil
	}

	symbolName := s.symbolNameAtPosition(relPath, line, p.Position.Character)
	if symbolName == "" {
		return []LSPLocation{}, nil
	}

	var locs []LSPLocation
	for _, f := range s.idx.Files {
		for _, ref := range f.References {
			if ref.Name == symbolName {
				locs = append(locs, LSPLocation{
					URI: pathToURI(f.Path, s.rootPath),
					Range: Range{
						Start: Position{Line: ref.StartLine - 1, Character: ref.StartColumn},
						End:   Position{Line: ref.EndLine - 1, Character: ref.EndColumn},
					},
				})
			}
		}
	}
	return locs, nil
}

func (s *Service) handleHover(params json.RawMessage) (any, error) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Get native hover
	nativeResult := s.nativeHover(p.TextDocument.URI, p.Position)

	// Try to merge with backend hover
	if s.proxyMgr != nil {
		file := uriToPath(p.TextDocument.URI)
		if b := s.proxyMgr.BackendForFile(file); b != nil {
			backendResult, err := b.Request("textDocument/hover", params)
			if err == nil && backendResult != nil {
				var backendHover Hover
				if json.Unmarshal(backendResult, &backendHover) == nil && backendHover.Contents.Value != "" {
					if nativeResult != nil {
						backendHover.Contents.Value += "\n\n---\n\n" + nativeResult.Contents.Value
					}
					return backendHover, nil
				}
			}
		}
	}

	if nativeResult != nil {
		return *nativeResult, nil
	}
	return nil, nil
}

func (s *Service) nativeHover(uri string, pos Position) *Hover {
	path := uriToPath(uri)
	relPath := relativeTo(path, s.rootPath)
	line := pos.Line + 1

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.idx == nil {
		return nil
	}

	for _, f := range s.idx.Files {
		if f.Path != relPath {
			continue
		}
		for _, sym := range f.Symbols {
			if sym.StartLine <= line && line <= sym.EndLine {
				content := sym.Kind + " " + sym.Name
				if sym.Signature != "" {
					content = sym.Name + sym.Signature
				}
				return &Hover{
					Contents: MarkupContent{Kind: "markdown", Value: "```" + f.Language + "\n" + content + "\n```"},
				}
			}
		}
	}
	return nil
}

func (s *Service) handleRename(params json.RawMessage) (any, error) {
	var p RenameParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Try proxy backend first
	if result, ok := s.proxyRequest("textDocument/rename", params, p.TextDocument.URI); ok {
		return result, nil
	}

	path := uriToPath(p.TextDocument.URI)
	relPath := relativeTo(path, s.rootPath)
	line := p.Position.Line + 1

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.idx == nil {
		return nil, fmt.Errorf("index not ready")
	}

	symbolName := s.symbolNameAtPosition(relPath, line, p.Position.Character)
	if symbolName == "" {
		return nil, fmt.Errorf("no symbol at position")
	}

	// Collect all edits: definitions + references
	changes := make(map[string][]TextEdit)
	for _, f := range s.idx.Files {
		uri := pathToURI(f.Path, s.rootPath)
		for _, sym := range f.Symbols {
			if sym.Name == symbolName {
				changes[uri] = append(changes[uri], TextEdit{
					Range:   symbolNameRange(sym),
					NewText: p.NewName,
				})
			}
		}
		for _, ref := range f.References {
			if ref.Name == symbolName {
				changes[uri] = append(changes[uri], TextEdit{
					Range: Range{
						Start: Position{Line: ref.StartLine - 1, Character: ref.StartColumn},
						End:   Position{Line: ref.EndLine - 1, Character: ref.EndColumn},
					},
					NewText: p.NewName,
				})
			}
		}
	}

	return WorkspaceEdit{Changes: changes}, nil
}

func symbolNameRange(sym model.Symbol) Range {
	// Approximate: use the start line, first column
	return Range{
		Start: Position{Line: sym.StartLine - 1, Character: 0},
		End:   Position{Line: sym.StartLine - 1, Character: len(sym.Name)},
	}
}

// symbolNameAtPosition finds the symbol/reference name at a given cursor position.
func (s *Service) symbolNameAtPosition(relPath string, line, col int) string {
	for _, f := range s.idx.Files {
		if f.Path != relPath {
			continue
		}
		// Check symbols
		for _, sym := range f.Symbols {
			if sym.StartLine <= line && line <= sym.EndLine {
				return sym.Name
			}
		}
		// Check references
		for _, ref := range f.References {
			if ref.StartLine == line {
				return ref.Name
			}
		}
	}
	return ""
}

// --- Helpers ---

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}

func pathToURI(relPath, root string) string {
	if root != "" {
		return "file://" + root + "/" + relPath
	}
	return "file://" + relPath
}

func relativeTo(abs, root string) string {
	if root != "" && strings.HasPrefix(abs, root) {
		rel := strings.TrimPrefix(abs, root)
		return strings.TrimPrefix(rel, "/")
	}
	return abs
}

func symbolRange(sym model.Symbol) Range {
	return Range{
		Start: Position{Line: sym.StartLine - 1, Character: 0},
		End:   Position{Line: sym.EndLine - 1, Character: 0},
	}
}

func symbolsToDocumentSymbols(syms []model.Symbol) []DocumentSymbol {
	result := make([]DocumentSymbol, 0, len(syms))
	for _, sym := range syms {
		r := symbolRange(sym)
		result = append(result, DocumentSymbol{
			Name:           sym.Name,
			Kind:           symbolKindFromModel(sym.Kind),
			Range:          r,
			SelectionRange: r,
		})
	}
	return result
}

func symbolKindFromModel(kind string) int {
	switch kind {
	case "function_definition":
		return SKFunction
	case "method_definition":
		return SKMethod
	case "class_definition":
		return SKClass
	case "interface_definition":
		return SKInterface
	case "struct_definition":
		return SKStruct
	case "enum_definition":
		return SKEnum
	case "type_definition":
		return SKClass
	case "constant_definition":
		return SKConstant
	case "variable_definition":
		return SKVariable
	case "module_definition":
		return SKModule
	case "constructor_definition":
		return SKConstructor
	default:
		return SKVariable
	}
}
