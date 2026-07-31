package changeintel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// digestPayload is every content field of Receipt except ID, Digest, and
// CreatedAt. Two Analyze calls over identical inputs produce identical
// payload bytes because every slice in the payload is sorted during
// construction and none of the payload types use maps — so
// encoding/json's deterministic struct-field and slice-index ordering is
// enough on its own; no canonicalization pass is required.
type digestPayload struct {
	SchemaVersion string          `json:"schema_version"`
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
}

// computeDigest returns the deterministic sha256 hex digest of a receipt's
// content, excluding ID, Digest, and CreatedAt. Only fields visible on
// Receipt feed the digest, so the digest is always reproducible from the
// receipt alone.
func computeDigest(r Receipt) string {
	payload := digestPayload{
		SchemaVersion: r.SchemaVersion,
		Root:          r.Root,
		Base:          r.Base,
		Head:          r.Head,
		Files:         r.Files,
		Entities:      r.Entities,
		Imports:       r.Imports,
		Complexity:    r.Complexity,
		APISurface:    r.APISurface,
		Boundaries:    r.Boundaries,
		Capabilities:  r.Capabilities,
		Impact:        r.Impact,
		Tests:         r.Tests,
		Risk:          r.Risk,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// Every field is a plain struct/slice/string/number; Marshal cannot
		// fail for this payload shape. Defend anyway rather than panic.
		data = []byte(err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
