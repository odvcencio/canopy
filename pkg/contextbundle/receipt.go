package contextbundle

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"time"
)

// Receipt is the immutable, content-addressed record of one context bundle
// build (spec 10.11). Every review and commit proposal MUST reference the
// context receipts it used (decision #8).
type Receipt struct {
	SchemaVersion   string          `json:"schema_version"`
	ID              string          `json:"id"`
	ParentID        string          `json:"parent_id,omitempty"`
	Snapshot        Snapshot        `json:"snapshot"`
	RequestDigest   string          `json:"request_digest"`
	PolicyVersion   string          `json:"policy_version"`
	OutputFormat    string          `json:"output_format"`
	BudgetTokens    int             `json:"budget_tokens"`
	EstimatedTokens int             `json:"estimated_tokens"`
	CandidateTokens int             `json:"candidate_tokens"`
	RenderedBytes   int64           `json:"rendered_bytes"`
	BundleSHA256    string          `json:"bundle_sha256"`
	Items           []ReceiptItem   `json:"items"`
	OmissionSummary OmissionSummary `json:"omission_summary"`
	CreatedAt       time.Time       `json:"created_at"`
}

// ReceiptItem is one selected item's accounting within a Receipt (spec 10.11).
type ReceiptItem struct {
	ItemID          string            `json:"item_id"`
	EntityID        string            `json:"entity_id,omitempty"`
	Path            string            `json:"path"`
	Section         Section           `json:"section"`
	Mode            ProjectionMode    `json:"mode"`
	StartLine       int               `json:"start_line,omitempty"`
	EndLine         int               `json:"end_line,omitempty"`
	ContentSHA256   string            `json:"content_sha256"`
	RawTokens       int               `json:"raw_tokens"`
	ProjectedTokens int               `json:"projected_tokens"`
	Score           int               `json:"score"`
	Reasons         []SelectionReason `json:"reasons"`
	Required        bool              `json:"required"`
}

// receiptIDPrefix names the receipt ID namespace (spec 10.11).
const receiptIDPrefix = "ctx_"

// receiptIDLength is the number of base32 characters kept after the prefix
// (spec 10.11: "ctx_<base32(sha256(...))[:26]>").
const receiptIDLength = 26

// buildReceipt assembles the immutable receipt for a rendered bundle. Actual
// provider token usage is deliberately excluded: it is a mutable observation
// Buckley stores separately (spec 10.11).
func buildReceipt(req Request, snapshot Snapshot, rendered RenderedBundle, omissions OmissionSummary, createdAt time.Time) Receipt {
	items := make([]ReceiptItem, 0, len(rendered.Items))
	candidateTokens := 0
	for _, rec := range rendered.Items {
		candidateTokens += int(rec.EstimatedTokens)
		items = append(items, ReceiptItem{
			ItemID:          rec.ItemID,
			EntityID:        rec.EntityID,
			Path:            rec.Path,
			Section:         Section(rec.Section),
			Mode:            rec.Mode,
			StartLine:       rec.StartLine,
			EndLine:         rec.EndLine,
			ContentSHA256:   rec.ContentSHA256,
			RawTokens:       int(rec.OriginalBytes),
			ProjectedTokens: int(rec.EstimatedTokens),
			Score:           rec.SelectionScore,
			Reasons:         rec.SelectionReasons,
			Required:        rec.Required,
		})
	}

	receipt := Receipt{
		SchemaVersion:   SchemaVersion,
		Snapshot:        snapshot,
		RequestDigest:   req.Intent.RequestDigest,
		PolicyVersion:   req.PolicyVersion,
		OutputFormat:    req.OutputFormat,
		BudgetTokens:    req.Budget.TotalTokens,
		EstimatedTokens: int(rendered.EstimatedTokens),
		CandidateTokens: candidateTokens,
		RenderedBytes:   int64(len(rendered.Content)),
		BundleSHA256:    sha256Hex(rendered.Content),
		Items:           items,
		OmissionSummary: omissions,
		CreatedAt:       createdAt,
	}
	receipt.ID = receiptID(receipt)
	return receipt
}

// receiptID computes the content-addressed receipt ID over the receipt with
// its ID field cleared, per spec 10.11.
func receiptID(r Receipt) string {
	r.ID = ""
	canonical, err := json.Marshal(r)
	if err != nil {
		// Marshal of this struct cannot fail in practice (no channels,
		// funcs, or cyclic types); a zero-length input still yields a
		// well-formed, if degenerate, ID rather than a panic.
		canonical = []byte{}
	}
	sum := sha256.Sum256(canonical)
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	if len(encoded) > receiptIDLength {
		encoded = encoded[:receiptIDLength]
	}
	return receiptIDPrefix + encoded
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	const hextable = "0123456789abcdef"
	out := make([]byte, len(sum)*2)
	for i, b := range sum {
		out[i*2] = hextable[b>>4]
		out[i*2+1] = hextable[b&0x0f]
	}
	return string(out)
}

// requestDigest hashes the parts of a request that determine candidate
// selection, so identical intents produce the same digest regardless of
// wall-clock time.
func requestDigest(req Request) string {
	type digestInput struct {
		Root             string     `json:"root"`
		Intent           TaskIntent `json:"intent"`
		Budget           Budget     `json:"budget"`
		Modes            []ProjectionMode
		IncludeGenerated bool
		LineNumbers      bool
		OutputFormat     string
		PolicyVersion    string
	}
	in := digestInput{
		Root:             req.Root,
		Intent:           req.Intent,
		Budget:           req.Budget,
		Modes:            req.Modes,
		IncludeGenerated: req.IncludeGenerated,
		LineNumbers:      req.LineNumbers,
		OutputFormat:     req.OutputFormat,
		PolicyVersion:    req.PolicyVersion,
	}
	in.Intent.RequestDigest = "" // never hash the digest into itself
	data, err := json.Marshal(in)
	if err != nil {
		data = []byte{}
	}
	return sha256Hex(data)
}

// UnchangedResult is the compact model-facing reply for a receipt or item
// reuse hit (spec 10.12): the model is told what did not change instead of
// being handed the same bytes again.
type UnchangedResult struct {
	ReceiptID string   `json:"receipt_id"`
	Unchanged bool     `json:"unchanged"`
	Items     []string `json:"items"`
	Hint      string   `json:"hint"`
}

// ReceiptReusable reports whether a previously issued receipt can stand in
// for a fresh build against snapshot, either because the workspace snapshot
// is unchanged or the receipt was built against the identical snapshot ID
// (spec 10.12).
func ReceiptReusable(prev Receipt, current Snapshot) bool {
	return prev.Snapshot.ID != "" && prev.Snapshot.ID == current.ID
}

// ReceiptItemReusable reports whether one receipt item's content hash still
// matches currentContentSHA256, allowing item-level reuse even after
// unrelated worktree changes (spec 10.5, 10.12).
func ReceiptItemReusable(item ReceiptItem, currentContentSHA256 string) bool {
	return item.ContentSHA256 != "" && item.ContentSHA256 == currentContentSHA256
}

// UnchangedReply builds the compact reuse reply for a reusable receipt.
func UnchangedReply(prev Receipt) UnchangedResult {
	ids := make([]string, 0, len(prev.Items))
	for _, item := range prev.Items {
		ids = append(ids, item.ItemID)
	}
	return UnchangedResult{
		ReceiptID: prev.ID,
		Unchanged: true,
		Items:     ids,
		Hint:      "Expand only a new entity or projection mode.",
	}
}
