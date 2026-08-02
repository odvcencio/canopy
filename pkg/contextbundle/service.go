package contextbundle

import (
	"context"
	"strings"
	"time"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

// PolicyVersion identifies the deterministic scoring/packing policy this
// service implements (spec 10.8).
const PolicyVersion = "context-selection-v1"

// SchemaVersion identifies the receipt/manifest schema shape.
const SchemaVersion = "contextbundle-v1"

// IndexProvider supplies a structural index and its identifying snapshot for
// a workspace root, refreshing on demand.
type IndexProvider interface {
	Snapshot(ctx context.Context, root string) (*model.Index, Snapshot, error)
	Refresh(ctx context.Context, root string, paths []string) (*model.Index, Snapshot, index.BuildStats, error)
}

// GraphProvider supplies the cross-reference call graph for an index.
type GraphProvider interface {
	Graph(ctx context.Context, idx *model.Index) (*xref.Graph, error)
}

// TestMapper supplies mapped tests for a set of entity IDs.
type TestMapper interface {
	RelatedTests(ctx context.Context, idx *model.Index, entities []string) ([]model.Symbol, error)
}

// Renderer turns a budget-selected render plan into final bytes plus a
// Canopy-measured manifest (spec 9, 10.3). LX-backed implementations live in
// pkg/contextbundle/lxrender.
type Renderer interface {
	Render(ctx context.Context, plan RenderPlan, budget int) (RenderedBundle, error)
}

// Tokenizer estimates token count from content, matching the LX tokenizer
// contract (spec 9.6) so estimates and final renders use one scale.
type Tokenizer interface {
	Estimate(size int64, content any) int64
}

// Clock supplies the current time, overridable in tests for receipt
// determinism.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service builds context bundles from a workspace index, a call graph, and a
// deterministic candidate scoring and packing policy.
type Service struct {
	indexes  IndexProvider
	graphs   GraphProvider
	tests    TestMapper
	renderer Renderer
	tokens   Tokenizer
	clock    Clock
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithTestMapper overrides the default TestMapper.
func WithTestMapper(t TestMapper) Option {
	return func(s *Service) { s.tests = t }
}

// WithGraphProvider overrides the default GraphProvider.
func WithGraphProvider(g GraphProvider) Option {
	return func(s *Service) { s.graphs = g }
}

// WithTokenizer overrides the default Tokenizer used for pre-render
// estimates. The Renderer's own tokenizer, if any, governs final measurement.
func WithTokenizer(t Tokenizer) Option {
	return func(s *Service) { s.tokens = t }
}

// WithClock overrides the default system clock, for deterministic tests.
func WithClock(c Clock) Option {
	return func(s *Service) { s.clock = c }
}

// NewService builds a Service from required dependencies plus options.
func NewService(indexes IndexProvider, renderer Renderer, opts ...Option) *Service {
	s := &Service{
		indexes:  indexes,
		renderer: renderer,
		tokens:   defaultTokenizer{},
		clock:    systemClock{},
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.graphs == nil {
		s.graphs = xrefGraphProvider{}
	}
	if s.tests == nil {
		s.tests = testmapMapper{}
	}
	return s
}

// defaultTokenizer approximates tokens at four bytes per token, matching the
// heuristic already used by internal/contextpack, when no LX-backed
// tokenizer is configured.
type defaultTokenizer struct{}

func (defaultTokenizer) Estimate(size int64, content any) int64 {
	if size <= 0 {
		return 0
	}
	return (size + 3) / 4
}

// Build compiles a context bundle for req: it resolves the workspace
// snapshot and index, generates and scores candidates, packs variants within
// budget, renders through the configured Renderer, and produces a receipt.
func (s *Service) Build(ctx context.Context, req Request) (*Result, error) {
	start := s.clock.Now()
	if strings.TrimSpace(req.Root) == "" {
		return nil, ErrEmptyRoot
	}
	req = NormalizeRequest(req)
	req.Intent.RequestDigest = requestDigest(req)

	idx, snapshot, err := s.indexes.Snapshot(ctx, req.Root)
	if err != nil {
		return nil, err
	}
	if idx == nil {
		return nil, ErrIndexNotFound
	}
	if req.Snapshot.ID == "" {
		req.Snapshot = snapshot
	}

	graph, err := s.graphs.Graph(ctx, idx)
	if err != nil {
		return nil, err
	}

	metrics := Metrics{}

	candidates := generateCandidates(ctx, idx, graph, s.tests, req)
	metrics.CandidateCount = len(candidates)

	scoreCandidates(candidates, graph, req)

	variantGroups := buildVariants(idx, req.Root, candidates, req)

	outcome, err := packBudget(ctx, req, req.Snapshot, variantGroups, s.tokens, s.renderer)
	if err != nil {
		return nil, err
	}
	metrics.SelectedCount = len(outcome.Plan.Items)
	metrics.OmittedCount = outcome.Omissions.Omitted
	metrics.DowngradeCount = outcome.Downgrades
	metrics.RerenderCount = outcome.Rerenders

	manifest := Manifest{SchemaVersion: SchemaVersion, Items: outcome.Rendered.Items}
	receipt := buildReceipt(req, req.Snapshot, outcome.Rendered, outcome.Omissions, s.clock.Now())

	metrics.BuildDuration = s.clock.Now().Sub(start)

	return &Result{
		Content:  outcome.Rendered.Content,
		Manifest: manifest,
		Receipt:  receipt,
		Metrics:  metrics,
	}, nil
}

// xrefGraphProvider is the default GraphProvider, building the graph fresh
// from the index (spec 10.4: "the xref graph MUST be rebuilt only when the
// index changes" — callers that want caching should wrap this or supply
// their own GraphProvider, e.g. Workspace).
type xrefGraphProvider struct{}

func (xrefGraphProvider) Graph(ctx context.Context, idx *model.Index) (*xref.Graph, error) {
	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}
	return &graph, nil
}
