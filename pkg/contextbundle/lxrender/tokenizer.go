// Package lxrender renders a contextbundle.RenderPlan through LX (decision
// #3: "Canopy selects; LX renders"). It uses github.com/rasros/lx/pkg/lx/streaming
// directly rather than the lx facade package, because the facade does not yet
// expose the tokenizer hook (spec 9.1, 9.6) — this is the documented
// fallback for PR 1 (an upstream lx observability surface) not being
// available: the adapter maintains its own projection manifest and imports
// the streaming tokenizer hook directly.
package lxrender

import (
	"github.com/rasros/lx/pkg/lx/core"
)

// Tokenizer adapts LX's own deterministic token estimator
// (core.DefaultTokenCounter) to contextbundle.Tokenizer, so pre-render
// estimates and the final LX-measured render share one scale (spec 9.6).
type Tokenizer struct{}

// Estimate implements contextbundle.Tokenizer.
func (Tokenizer) Estimate(size int64, content any) int64 {
	return core.DefaultTokenCounter(size, content)
}
