// Package contextbundle builds task-conditioned, budget-aware context bundles
// from a Canopy structural index. Canopy selects evidence; rendering through
// LX turns selected evidence into model-facing text. See spec section 10 for
// the normative behavior this package implements.
package contextbundle

import "time"

// TaskKind names the kind of work a context bundle request supports.
type TaskKind string

const (
	TaskExplore   TaskKind = "explore"
	TaskImplement TaskKind = "implement"
	TaskDebug     TaskKind = "debug"
	TaskReview    TaskKind = "review"
	TaskTest      TaskKind = "test"
	TaskCommit    TaskKind = "commit"
	TaskResume    TaskKind = "resume"
)

// ProjectionMode names how much of an item Canopy renders, from full source
// down to a receipt-only placeholder.
type ProjectionMode string

const (
	ProjectionMap         ProjectionMode = "map"
	ProjectionSignature   ProjectionMode = "signature"
	ProjectionSkeleton    ProjectionMode = "skeleton"
	ProjectionBody        ProjectionMode = "body"
	ProjectionFull        ProjectionMode = "full"
	ProjectionHeadTail    ProjectionMode = "head_tail"
	ProjectionMetadata    ProjectionMode = "metadata"
	ProjectionReceiptOnly ProjectionMode = "receipt_only"
)

// downgradeOrder is the default variant downgrade sequence from richest to
// leanest projection (spec 10.7: full -> body -> body with elided interior ->
// skeleton -> signature -> metadata -> receipt-only). "Body with elided
// interior" maps onto ProjectionHeadTail, the closest enum value to a
// partially-collapsed body (spec 10.2 defines no separate elided-body mode).
var downgradeOrder = []ProjectionMode{
	ProjectionFull,
	ProjectionBody,
	ProjectionHeadTail,
	ProjectionSkeleton,
	ProjectionSignature,
	ProjectionMetadata,
	ProjectionReceiptOnly,
}

// Section names a canonical bundle section (spec 9.3). Section order in a
// rendered bundle MUST follow this list; absent sections are omitted.
type Section string

const (
	SectionCheckpoint Section = "checkpoint"
	SectionRepoMap    Section = "repository_map"
	SectionChanges    Section = "changed_entities"
	SectionFocus      Section = "focus"
	SectionCallers    Section = "callers"
	SectionCallees    Section = "callees"
	SectionTypes      Section = "types_contracts"
	SectionTests      Section = "tests_verification"
	SectionFailures   Section = "failures_runtime"
	SectionMemory     Section = "project_memory"
	SectionOmissions  Section = "omissions"
)

// sectionOrder is the canonical section rendering order (spec 9.3).
var sectionOrder = []Section{
	SectionCheckpoint,
	SectionRepoMap,
	SectionChanges,
	SectionFocus,
	SectionCallers,
	SectionCallees,
	SectionTypes,
	SectionTests,
	SectionFailures,
	SectionMemory,
	SectionOmissions,
}

// SectionOrder returns the canonical section rendering order (spec 9.3), for
// renderers outside this package (e.g. pkg/contextbundle/lxrender).
func SectionOrder() []Section {
	return append([]Section(nil), sectionOrder...)
}

// sectionRank returns the canonical ordering rank of a section, or len(sectionOrder)
// for an unrecognized section so it sorts last.
func sectionRank(s Section) int {
	for i, candidate := range sectionOrder {
		if candidate == s {
			return i
		}
	}
	return len(sectionOrder)
}

// Snapshot identifies the workspace state a bundle was built against (spec 10.5).
type Snapshot struct {
	Kind         string `json:"kind"` // commit, worktree, imported
	ID           string `json:"id"`
	Root         string `json:"root"`
	HeadCommit   string `json:"head_commit,omitempty"`
	DirtyDigest  string `json:"dirty_digest,omitempty"`
	ConfigDigest string `json:"config_digest,omitempty"`
}

// Selector identifies a single explicit unit of evidence a caller requests,
// either by file, symbol, entity ID, or line.
type Selector struct {
	File      string `json:"file,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	Kind      string `json:"kind,omitempty"`
	EntityID  string `json:"entity_id,omitempty"`
	Line      int    `json:"line,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Weight    int    `json:"weight,omitempty"`
}

// ExternalRef points at evidence that lives outside the structural index, for
// example a captured test log or command transcript held in Buckley's
// evidence store.
type ExternalRef struct {
	Kind string `json:"kind"` // e.g. "test_log", "command_output", "stack_trace"
	ID   string `json:"id,omitempty"`
	Path string `json:"path,omitempty"`
	Text string `json:"text,omitempty"`
}

// CompletionSignal names an event or evidence kind whose presence would move
// a task toward completion. Selection MAY use this to prioritize candidates
// but MUST NOT treat its own output as completion evidence (decision #6).
type CompletionSignal struct {
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
}

// TaskIntent describes what the requester is trying to accomplish.
type TaskIntent struct {
	Kind              TaskKind           `json:"kind"`
	OriginalRequest   string             `json:"original_request"`
	RequestDigest     string             `json:"request_digest"`
	DerivedQuery      string             `json:"derived_query,omitempty"`
	Focus             []Selector         `json:"focus,omitempty"`
	ChangedPaths      []string           `json:"changed_paths,omitempty"`
	FailureEvidence   []ExternalRef      `json:"failure_evidence,omitempty"`
	Constraints       []string           `json:"constraints,omitempty"`
	CompletionSignals []CompletionSignal `json:"completion_signals,omitempty"`
}

// Budget bounds how many tokens a bundle, and optionally each section, may use.
type Budget struct {
	TotalTokens int             `json:"total_tokens"`
	Sections    map[Section]int `json:"sections,omitempty"`
	Tolerance   float64         `json:"tolerance,omitempty"`
}

// ReceiptRef points at a previously issued context or change receipt, used to
// detect unchanged items and to avoid re-selecting them (spec 10.12).
type ReceiptRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind,omitempty"` // "context" or "change"
}

// Request describes one context bundle build.
type Request struct {
	Root             string           `json:"root"`
	Snapshot         Snapshot         `json:"snapshot"`
	Intent           TaskIntent       `json:"intent"`
	Budget           Budget           `json:"budget"`
	Modes            []ProjectionMode `json:"modes,omitempty"`
	IncludeGenerated bool             `json:"include_generated,omitempty"`
	LineNumbers      bool             `json:"line_numbers,omitempty"`
	OutputFormat     string           `json:"output_format,omitempty"`
	PolicyVersion    string           `json:"policy_version"`
	PreviousReceipts []ReceiptRef     `json:"previous_receipts,omitempty"`
}

// Result is the outcome of a context bundle build.
type Result struct {
	Content  []byte   `json:"-"`
	Manifest Manifest `json:"manifest"`
	Receipt  Receipt  `json:"receipt"`
	Metrics  Metrics  `json:"metrics"`
}

// Variant is one candidate projection of a candidate entity at a specific
// mode, with its estimated cost and value (spec 10.7).
type Variant struct {
	CandidateID      string         `json:"candidate_id"`
	Mode             ProjectionMode `json:"mode"`
	Section          Section        `json:"section"`
	Content          []byte         `json:"-"`
	EstimatedTokens  int            `json:"estimated_tokens"`
	InformationValue int            `json:"information_value"`
	RawBytes         int64          `json:"raw_bytes"`
	StartLine        int            `json:"start_line,omitempty"`
	EndLine          int            `json:"end_line,omitempty"`
}

// SelectionReason records one scoring signal that contributed to a candidate's
// score, for receipt transparency (spec 10.8).
type SelectionReason struct {
	Signal string `json:"signal"`
	Points int    `json:"points"`
}

// ProjectionRecord is Canopy's own account of what went into and came out of
// one rendered item, measured independently of any LX observability hook
// (spec 9.7). This is the fallback path: PR 1's upstream LX manifest is not a
// dependency of this package.
type ProjectionRecord struct {
	ItemID           string            `json:"item_id"`
	Section          string            `json:"section"`
	Path             string            `json:"path"`
	EntityID         string            `json:"entity_id,omitempty"`
	Language         string            `json:"language,omitempty"`
	Mode             ProjectionMode    `json:"mode"`
	StartLine        int               `json:"start_line,omitempty"`
	EndLine          int               `json:"end_line,omitempty"`
	OriginalBytes    int64             `json:"original_bytes"`
	ProjectedBytes   int64             `json:"projected_bytes"`
	EstimatedTokens  int64             `json:"estimated_tokens"`
	ContentSHA256    string            `json:"content_sha256"`
	SelectionScore   int               `json:"selection_score"`
	SelectionReasons []SelectionReason `json:"selection_reasons"`
	Required         bool              `json:"required"`
}

// Manifest is the ordered set of projection records that produced a rendered
// bundle, independent of the immutable receipt (spec 9.7, 10.11).
type Manifest struct {
	SchemaVersion string             `json:"schema_version"`
	Items         []ProjectionRecord `json:"items"`
}

// Metrics carries non-authoritative build telemetry. Nothing here may affect
// receipt determinism.
type Metrics struct {
	BuildDuration     time.Duration `json:"build_duration_ns"`
	CandidateCount    int           `json:"candidate_count"`
	SelectedCount     int           `json:"selected_count"`
	OmittedCount      int           `json:"omitted_count"`
	DowngradeCount    int           `json:"downgrade_count"`
	RerenderCount     int           `json:"rerender_count"`
	IndexRefreshed    bool          `json:"index_refreshed"`
	WorkspaceCacheHit bool          `json:"workspace_cache_hit"`
}

// OmissionSummary accounts for candidates that were generated but not
// selected into the final bundle, so the model can ask for them explicitly
// instead of assuming they do not exist.
type OmissionSummary struct {
	TotalCandidates int             `json:"total_candidates"`
	Omitted         int             `json:"omitted"`
	ByReason        map[string]int  `json:"by_reason,omitempty"`
	Entities        []OmittedEntity `json:"entities,omitempty"`
}

// OmittedEntity names one candidate that did not make it into the bundle.
type OmittedEntity struct {
	EntityID string  `json:"entity_id,omitempty"`
	Path     string  `json:"path"`
	Section  Section `json:"section"`
	Reason   string  `json:"reason"`
	Score    int     `json:"score"`
}

// RenderPlan is the ordered, budget-selected set of variants ready for
// rendering, grouped by canonical section order.
type RenderPlan struct {
	Snapshot     Snapshot
	OutputFormat string
	LineNumbers  bool
	Items        []PlanItem
}

// PlanItem pairs a selected variant with the identity needed to render and
// receipt it.
type PlanItem struct {
	ItemID   string
	EntityID string
	Path     string
	Language string
	Variant  Variant
	Required bool
	Score    int
	Reasons  []SelectionReason
}

// RenderedBundle is the output of a Renderer: the final bytes plus the
// Canopy-measured manifest for every rendered item.
type RenderedBundle struct {
	Content         []byte
	Items           []ProjectionRecord
	EstimatedTokens int64
}

// errRequiredEvidenceExceedsBudget and other sentinel errors live in errors.go.
