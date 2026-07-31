package changeintel

// FileStatus classifies how a file changed between base and head.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileRemoved  FileStatus = "removed"
	FileModified FileStatus = "modified"
	FileRenamed  FileStatus = "renamed"
)

// FileChange records the change status of a single path and whether Canopy
// could structurally index it on each side.
type FileChange struct {
	Path        string     `json:"path"`
	OldPath     string     `json:"old_path,omitempty"` // set when Status == FileRenamed
	Status      FileStatus `json:"status"`
	Language    string     `json:"language,omitempty"`
	Generated   bool       `json:"generated,omitempty"`
	BaseIndexed bool       `json:"base_indexed"`
	HeadIndexed bool       `json:"head_indexed"`
	// Uncertain is true when a file that exists on a side could not be
	// structurally parsed (unsupported language, parse error) so entity and
	// complexity deltas touching it are incomplete.
	Uncertain bool `json:"uncertain,omitempty"`
}

// EntityStatus classifies how a matched structural entity (function, method,
// type, etc.) changed between base and head.
type EntityStatus string

const (
	EntityAdded     EntityStatus = "added"
	EntityRemoved   EntityStatus = "removed"
	EntityModified  EntityStatus = "modified"
	EntityUnchanged EntityStatus = "unchanged"
)

// EntityMetrics is the set of structural metrics recorded per side (base or
// head) for a matched entity.
type EntityMetrics struct {
	Cyclomatic int `json:"cyclomatic"`
	Cognitive  int `json:"cognitive"`
	Lines      int `json:"lines"`
	FanIn      int `json:"fan_in"`
	FanOut     int `json:"fan_out"`
}

// EntityChange is a single structural entity's before/after record. Delta is
// always literally Head - Base per field (0 on the side that does not exist
// for Added/Removed entities).
type EntityChange struct {
	File             string        `json:"file"`
	Kind             string        `json:"kind"`
	Name             string        `json:"name"`
	Receiver         string        `json:"receiver,omitempty"`
	Status           EntityStatus  `json:"status"`
	BaseFile         string        `json:"base_file,omitempty"` // differs from File only when the containing file was renamed
	Base             EntityMetrics `json:"base"`
	Head             EntityMetrics `json:"head"`
	Delta            EntityMetrics `json:"delta"`
	SignatureChanged bool          `json:"signature_changed"`
	ReceiverChanged  bool          `json:"receiver_changed"`
	SpanChanged      bool          `json:"span_changed"`
	// MatchConfidence is 1.0 for entities matched by an exact structural key
	// (file/kind/receiver/name, following renames) and 0 for entities that
	// exist on only one side (Added/Removed).
	MatchConfidence float64 `json:"match_confidence"`
}

// ImportChange records import additions/removals for a single file.
type ImportChange struct {
	File    string   `json:"file"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// ComplexityDelta is the repository-wide rollup of complexity movement
// across every matched entity. Every *Total* and *Avg* pair is computed as
// head - base, never as a head-only score.
type ComplexityDelta struct {
	BaseTotalCyclomatic int     `json:"base_total_cyclomatic"`
	HeadTotalCyclomatic int     `json:"head_total_cyclomatic"`
	DeltaCyclomatic     int     `json:"delta_cyclomatic"`
	BaseAvgCyclomatic   float64 `json:"base_avg_cyclomatic"`
	HeadAvgCyclomatic   float64 `json:"head_avg_cyclomatic"`
	BaseTotalCognitive  int     `json:"base_total_cognitive"`
	HeadTotalCognitive  int     `json:"head_total_cognitive"`
	DeltaCognitive      int     `json:"delta_cognitive"`
	MatchedEntities     int     `json:"matched_entities"`
	IncreasedFunctions  int     `json:"increased_functions"`
	DecreasedFunctions  int     `json:"decreased_functions"`
	UnchangedFunctions  int     `json:"unchanged_functions"`
}

// APIChangeKind classifies a single exported-symbol change.
type APIChangeKind string

const (
	APIAdded             APIChangeKind = "added"
	APIRemoved           APIChangeKind = "removed"
	APISignatureChanged  APIChangeKind = "signature_changed"
	APIReceiverChanged   APIChangeKind = "receiver_changed"
	APIVisibilityChanged APIChangeKind = "visibility_changed"
)

// APISymbolChange is a single exported-symbol change entry.
type APISymbolChange struct {
	File           string        `json:"file"`
	Kind           string        `json:"kind"`
	Name           string        `json:"name"`
	Receiver       string        `json:"receiver,omitempty"`
	Change         APIChangeKind `json:"change"`
	BaseSignature  string        `json:"base_signature,omitempty"`
	HeadSignature  string        `json:"head_signature,omitempty"`
	BaseVisibility string        `json:"base_visibility,omitempty"`
	HeadVisibility string        `json:"head_visibility,omitempty"`
}

// APIDelta is the public API surface delta between base and head, restricted
// to exported (publicly visible) symbols.
type APIDelta struct {
	Added   []APISymbolChange `json:"added,omitempty"`
	Removed []APISymbolChange `json:"removed,omitempty"`
	Changed []APISymbolChange `json:"changed,omitempty"`
}

// BoundaryDelta partitions architecture-boundary violations into those newly
// introduced by this change, those resolved by it, and those that persist
// unchanged on both sides.
type BoundaryDelta struct {
	Introduced []BoundaryViolation `json:"introduced,omitempty"`
	Resolved   []BoundaryViolation `json:"resolved,omitempty"`
	Persisting []BoundaryViolation `json:"persisting,omitempty"`
}

// BoundaryViolation mirrors pkg/boundaries.Violation; changeintel defines
// its own copy so the receipt schema does not couple to boundaries' package
// layout.
type BoundaryViolation struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Rule    string `json:"rule"`
	Module  string `json:"module"`
	Message string `json:"message"`
}

// CapabilityMatch is a single detected capability, scoped to the file it was
// observed in so introduced/resolved/persisting classification is possible.
type CapabilityMatch struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Confidence string `json:"confidence"`
	AttackID   string `json:"attack_id,omitempty"`
	File       string `json:"file"`
}

// CapabilityDelta partitions detected capabilities (for example, network or
// filesystem access patterns) into introduced/resolved/persisting.
type CapabilityDelta struct {
	Introduced []CapabilityMatch `json:"introduced,omitempty"`
	Resolved   []CapabilityMatch `json:"resolved,omitempty"`
	Persisting []CapabilityMatch `json:"persisting,omitempty"`
}

// ImpactSummary is the repository-wide rollup of change metrics required by
// the spec's "Change metrics" section: counts, blast radius, index scope,
// and parse/provenance uncertainty.
type ImpactSummary struct {
	ChangedFiles     int `json:"changed_files"`
	ChangedEntities  int `json:"changed_entities"`
	AddedEntities    int `json:"added_entities"`
	RemovedEntities  int `json:"removed_entities"`
	ModifiedEntities int `json:"modified_entities"`
	ChangedPublicAPI int `json:"changed_public_api"`

	BlastRadius   int      `json:"blast_radius"`
	AffectedFiles []string `json:"affected_files,omitempty"`

	// IndexScope is "changed_only" (the only mode Analyze currently
	// implements — see package doc) or "full".
	IndexScope string `json:"index_scope"`

	// BaseAvailable is false when Base.Ref could not be resolved (for
	// example a shallow clone missing the base commit). Analyze degrades
	// gracefully in that case: every changed path is treated as added.
	BaseAvailable bool `json:"base_available"`

	ParseUncertainty  []string `json:"parse_uncertainty,omitempty"`
	GeneratedInvolved []string `json:"generated_involved,omitempty"`
}

// TestImpact summarizes test coverage for changed entities.
type TestImpact struct {
	MappedTests             []string `json:"mapped_tests,omitempty"`
	SelectedTests           []string `json:"selected_tests,omitempty"`
	UntestedChangedEntities []string `json:"untested_changed_entities,omitempty"`
	Coverage                float64  `json:"coverage"`
}

// RiskComponents is every weighted term of the routing-risk formula, each
// clamped to [0,1] before weighting. See RiskDelta.Score for the composite.
type RiskComponents struct {
	BlastRadius                      float64 `json:"blast_radius"`
	PublicAPIChange                  float64 `json:"public_api_change"`
	SecurityCapabilityChange         float64 `json:"security_capability_change"`
	BoundaryViolationChange          float64 `json:"boundary_violation_change"`
	ComplexityIncrease               float64 `json:"complexity_increase"`
	HighFaninChange                  float64 `json:"high_fanin_change"`
	TestGap                          float64 `json:"test_gap"`
	ConcurrencyOrStatefulArea        float64 `json:"concurrency_or_stateful_area"`
	GeneratedOrProvenanceUncertainty float64 `json:"generated_or_provenance_uncertainty"`
}

// RiskDelta is the routing-risk score for this change plus its components.
// This score routes review depth; it MUST NOT be treated as an approval
// gate (see spec section 11.5).
type RiskDelta struct {
	Score      float64        `json:"score"`
	Components RiskComponents `json:"components"`
}
