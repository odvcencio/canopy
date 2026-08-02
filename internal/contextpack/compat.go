package contextpack

// This file is the compatibility adapter spec 10.1 requires: "internal/
// contextpack MUST become a compatibility adapter over pkg/contextbundle. It
// MUST be deleted only after CLI and MCP callers migrate." Build (context.go)
// remains the exact legacy line/semantic implementation and stays the
// default `canopy search context` path, so existing behavior is unchanged.
// BuildBundle is the new entry point CLI and MCP call only when a caller
// opts into bundle mode (spec 10.13), with an environment-variable rollback
// switch back to the legacy path (spec PR 2 rollback).

import (
	"context"
	"os"
	"strconv"
	"strings"

	"m31labs.dev/canopy/pkg/contextbundle"
	"m31labs.dev/canopy/pkg/contextbundle/lxrender"
	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
)

// EnvLegacyOverride forces BuildBundle to run the legacy Build implementation
// even when the caller requested bundle mode. This is the rollback switch:
// operators can disable bundle mode fleet-wide without a code change or
// waiting on a redeploy of CLI/MCP callers.
const EnvLegacyOverride = "CANOPY_CONTEXT_LEGACY"

// UseLegacy reports whether the rollback override is active.
func UseLegacy() bool {
	v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(EnvLegacyOverride)))
	return err == nil && v
}

// BundleOptions carries the additive bundle-mode inputs from spec 10.13's
// CLI and MCP flags (--mode, --task, --tokens, --focus/--symbol,
// --previous-receipts). Callers that set none of these keep using the
// legacy Build path, so `canopy search context` with no new flags stays
// byte-for-byte unchanged.
type BundleOptions struct {
	Mode             contextbundle.TaskKind
	Task             string
	Tokens           int
	Focus            []contextbundle.Selector
	IncludeGenerated bool
	LineNumbers      bool
	OutputFormat     string
	PreviousReceipts []contextbundle.ReceiptRef
}

// WantsBundle reports whether opts requests the new bundle-mode path.
// Omitting every bundle-mode flag keeps legacy behavior (spec 10.13,
// acceptance: "Existing `canopy search context` behavior remains").
func (o BundleOptions) WantsBundle() bool {
	return o.Mode != "" || strings.TrimSpace(o.Task) != "" || len(o.Focus) > 0
}

// BuildBundle builds a context bundle over idx via pkg/contextbundle,
// rendering through LX. idx is the index the CLI or MCP caller already
// loaded (cmd/canopy/helpers.go, internal/mcp helpers), so this does not
// load or build a second copy.
func BuildBundle(ctx context.Context, idx *model.Index, root string, opts BundleOptions) (*contextbundle.Result, error) {
	req := contextbundle.Request{
		Root: root,
		Intent: contextbundle.TaskIntent{
			Kind:            opts.Mode,
			OriginalRequest: opts.Task,
			Focus:           opts.Focus,
		},
		Budget:           contextbundle.Budget{TotalTokens: opts.Tokens},
		IncludeGenerated: opts.IncludeGenerated,
		LineNumbers:      opts.LineNumbers,
		OutputFormat:     opts.OutputFormat,
		PreviousReceipts: opts.PreviousReceipts,
	}
	svc := contextbundle.NewService(preloadedIndexProvider{idx: idx}, lxrender.Renderer{})
	return svc.Build(ctx, req)
}

// preloadedIndexProvider adapts an already-loaded index to
// contextbundle.IndexProvider for one-shot CLI/MCP calls. It computes a real
// snapshot identity (spec 10.5) but does not itself rebuild the index on
// Refresh; long-lived callers that need warm-workspace refresh should use
// contextbundle.NewWorkspaceManager instead.
type preloadedIndexProvider struct {
	idx *model.Index
}

func (p preloadedIndexProvider) Snapshot(ctx context.Context, root string) (*model.Index, contextbundle.Snapshot, error) {
	snap, err := contextbundle.ComputeSnapshot(root)
	if err != nil {
		// Snapshot identity is best-effort outside a git repo (spec 10.5
		// only defines commit/worktree forms for a git tree); fall back to
		// an imported snapshot rather than failing the whole request.
		snap = contextbundle.Snapshot{Kind: "imported", ID: "imported:" + root, Root: root}
	}
	return p.idx, snap, nil
}

func (p preloadedIndexProvider) Refresh(ctx context.Context, root string, paths []string) (*model.Index, contextbundle.Snapshot, index.BuildStats, error) {
	idx, snap, err := p.Snapshot(ctx, root)
	return idx, snap, index.BuildStats{}, err
}
