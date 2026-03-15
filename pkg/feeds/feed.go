// Package feeds defines the FeedProvider interface and the FeedEngine
// that orchestrates multiple feeds enriching a shared scope graph.
package feeds

import (
	"log/slog"
	"sort"
	"time"

	"github.com/odvcencio/gts-suite/pkg/scope"
)

// FeedProvider enriches a scope graph with additional intelligence.
type FeedProvider interface {
	Name() string
	Supports(lang string) bool
	Priority() int
	Feed(graph *scope.Graph, file string, src []byte, ctx *FeedContext) error
}

// FeedContext provides workspace context to feeds.
type FeedContext struct {
	WorkspaceRoot string
	VCSRoot       string
	ImportPaths   map[string]string
	Config        map[string]string
	Logger        *slog.Logger
}

// FeedHealth tracks a feed's error state for circuit breaking.
type FeedHealth struct {
	ConsecutiveErrors int
	LastError         error
	LastRun           time.Time
	Disabled          bool
}

// Engine orchestrates multiple feeds, running them sequentially
// in priority order per-file.
type Engine struct {
	feeds   []FeedProvider
	sorted  bool
	health  map[string]*FeedHealth
	timeout time.Duration
	logger  *slog.Logger
}

// NewEngine creates a feed engine with default settings.
func NewEngine(logger *slog.Logger) *Engine {
	return &Engine{
		health:  make(map[string]*FeedHealth),
		timeout: 10 * time.Second,
		logger:  logger,
	}
}

// Register adds a feed provider to the engine.
func (e *Engine) Register(f FeedProvider) {
	e.feeds = append(e.feeds, f)
	e.health[f.Name()] = &FeedHealth{}
	e.sorted = false
}

func (e *Engine) ensureSorted() {
	if e.sorted {
		return
	}
	sort.Slice(e.feeds, func(i, j int) bool {
		return e.feeds[i].Priority() < e.feeds[j].Priority()
	})
	e.sorted = true
}

// RunFile runs all applicable feeds for a single file, sequentially in priority order.
func (e *Engine) RunFile(graph *scope.Graph, file string, src []byte, lang string, ctx *FeedContext) {
	e.ensureSorted()
	for _, f := range e.feeds {
		h := e.health[f.Name()]
		if h.Disabled {
			continue
		}
		if !f.Supports(lang) {
			continue
		}
		err := f.Feed(graph, file, src, ctx)
		h.LastRun = time.Now()
		if err != nil {
			h.ConsecutiveErrors++
			h.LastError = err
			e.logger.Warn("feed error",
				"feed", f.Name(),
				"file", file,
				"error", err,
				"consecutive_errors", h.ConsecutiveErrors,
			)
			if h.ConsecutiveErrors >= 3 {
				h.Disabled = true
				e.logger.Error("feed disabled after repeated errors",
					"feed", f.Name(),
				)
			}
		} else {
			h.ConsecutiveErrors = 0
			h.LastError = nil
		}
	}
}

// Feeds returns all registered feed providers.
func (e *Engine) Feeds() []FeedProvider {
	e.ensureSorted()
	return e.feeds
}

// Health returns the health state for a named feed.
func (e *Engine) Health(name string) *FeedHealth {
	return e.health[name]
}

// EnableFeed re-enables a disabled feed.
func (e *Engine) EnableFeed(name string) {
	if h, ok := e.health[name]; ok {
		h.Disabled = false
		h.ConsecutiveErrors = 0
	}
}
