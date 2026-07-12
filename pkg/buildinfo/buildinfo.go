// Package buildinfo holds the single authoritative canopy version string.
// Symbol extraction output depends on both canopy's own code and the pinned
// gotreesitter grammar set, so cached index entries are only trustworthy when
// they were produced by the same canopy version. Index building stamps this
// version into every snapshot and refuses to reuse per-file summaries from a
// different builder version.
package buildinfo

// Version is the canopy release version. Bump on every release; the index
// cache uses it as a reuse fence.
const Version = "0.16.3"
