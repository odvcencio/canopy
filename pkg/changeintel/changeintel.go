// Package changeintel computes deterministic change receipts for a base/head
// pair of source snapshots: structural deltas, public API changes, boundary
// and capability drift, test impact, and a routing risk score.
//
// The package is the single owner of change intelligence in Canopy. The CLI
// (`canopy review`) and the MCP tool (`gts_review`) are thin adapters over
// Service.Analyze.
package changeintel

import (
	"context"
	"time"
)

// SchemaVersion identifies the shape of Receipt for consumers that persist
// or diff receipts across Canopy versions.
const SchemaVersion = "1.0.0"

// Service analyzes a base/head snapshot pair and produces a change receipt.
type Service interface {
	Analyze(ctx context.Context, req Request) (Receipt, error)
}

// SnapshotRef identifies one side of a comparison: a git ref/commit, or the
// empty ref meaning "the current working tree".
type SnapshotRef struct {
	// Ref is a git ref, commit-ish, or SHA. Empty means the working tree.
	Ref string `json:"ref"`
	// Label is an optional human-readable name (branch name, "HEAD", etc.).
	// It is informational only and never affects analysis.
	Label string `json:"label,omitempty"`
}

// Request describes a single change-intelligence analysis.
type Request struct {
	// Root is the repository root to run git operations against.
	Root string `json:"root"`
	// Base and Head identify the two snapshots being compared. Head.Ref ==""
	// means "working tree at Root".
	Base Snapshot
	Head Snapshot
	// ChangedPaths restricts analysis to an explicit path set instead of
	// discovering changes via `git diff --name-status`. Paths are relative
	// to Root and use forward slashes.
	ChangedPaths []string `json:"changed_paths,omitempty"`
	// IncludeTests enables test-mapping analysis (TestImpact). Disabled by
	// default because it requires a full xref graph over head content.
	IncludeTests bool `json:"include_tests"`
	// IncludeRisk enables risk-score computation (RiskDelta).
	IncludeRisk bool `json:"include_risk"`
	// PolicyVersion is recorded on the receipt for audit/reproducibility; it
	// does not currently change analysis behavior.
	PolicyVersion string `json:"policy_version,omitempty"`
}

// Snapshot is an alias kept distinct from SnapshotRef so callers can pass
// either a SnapshotRef value or (in the future) richer snapshot metadata
// without breaking Request's field type. Today it is exactly SnapshotRef.
type Snapshot = SnapshotRef

// Receipt is the deterministic output of a change-intelligence analysis. Two
// Analyze calls over identical base/head content and Request options MUST
// produce receipts with identical Digest, ID, and content fields; only
// CreatedAt may differ (see WithClock for pinning it in tests).
type Receipt struct {
	SchemaVersion string          `json:"schema_version"`
	ID            string          `json:"id"`
	Root          string          `json:"root"`
	Base          SnapshotRef     `json:"base"`
	Head          SnapshotRef     `json:"head"`
	Files         []FileChange    `json:"files"`
	Entities      []EntityChange  `json:"entities"`
	Imports       []ImportChange  `json:"imports"`
	Complexity    ComplexityDelta `json:"complexity"`
	APISurface    APIDelta        `json:"api_surface"`
	Boundaries    BoundaryDelta   `json:"boundaries"`
	Capabilities  CapabilityDelta `json:"capabilities"`
	Impact        ImpactSummary   `json:"impact"`
	Tests         TestImpact      `json:"tests"`
	Risk          RiskDelta       `json:"risk"`
	Digest        string          `json:"digest"`
	CreatedAt     time.Time       `json:"created_at"`
}
