package lxrender

import "github.com/rasros/lx/pkg/lx/core"

// compactModelConfig returns the LX Config for the default model-facing
// output: compact Markdown with custom templates, not LX's human CLI
// defaults (spec 9.5). It:
//   - delimits every item with an evidence comment plus a heading holding the
//     item ID, path, projection mode, snapshot, and line range;
//   - drops LX's default row-count filler and "---" section dividers
//     (decorative separators the spec asks the model template to avoid);
//   - preserves language fences.
//
// The evidence comment and heading are emitted as a preceding prompt item by
// the renderer (see renderer.go), since core.FileContext carries no item-ID
// or snapshot field of its own; FileContentTemplate here only wraps the
// already-selected bytes in a fence.
func compactModelConfig() *core.Config {
	cfg := core.NewConfig()
	cfg.OutputFormat = "markdown"
	cfg.FileContentTemplate = "```{{ .Language }}\n{{ .Content | endNewline -}}```"
	cfg.SectionTemplate = "## {{ .Body | endNewline }}"
	cfg.PromptTemplate = "{{ .Body | endNewline }}"
	cfg.MetaTemplate = "{{ .Body | endNewline }}"
	cfg.OutputFooterTemplate = ""
	return cfg
}
