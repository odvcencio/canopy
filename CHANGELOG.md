# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

- Nothing yet.

## [0.14.0] - 2026-04-01

Enterprise-grade structural analysis. Six feature phases adding architecture governance, security intelligence, CI/CD integration, multi-repo federation, AI agent enhancement, and executive reporting.

### Added

#### Architecture Governance (Phase 1)
- **`gts analyze boundaries`** — enforce module boundary rules from `.gtsboundaries` config. Supports allow/deny rules with glob patterns, diff-aware `--base` filtering, SARIF output, and MCP tool `gts_boundaries`.
- **`.gtsboundaries` config file** — line-oriented DSL for declaring allowed import relationships between modules.
- **`gts graph drift`** — compare dependency graphs between two git refs via temporary worktrees. Reports added/removed imports and new cycles. MCP tool `gts_drift`.
- **`.gtslint` scoped overrides** — `fan_out > 10 in pkg/* -> warn "high fan-out"` applies rules only to matching paths.
- **`.gtslint` package-level rules** — `package import_depth > 5 -> error "too deep"`, `package exported_symbols > 50 in pkg/*`, `package no_import_cycles`. New evaluation pathway for package-granularity metrics.
- **`.gtslint` config loading in `check` and `lint`** — both commands now load `.gtslint` and apply threshold overrides and ignore rules. CLI flags take precedence when explicitly set.

#### Security & Compliance (Phase 2)
- **`gts analyze reachability`** — supply chain analysis answering "does package X transitively reach capability Y?" via xref call graph traversal. Filterable by capability category and MITRE ATT&CK technique. MCP tool `gts_reachability`.
- **Secrets-in-AST detection** — built-in tree-sitter query patterns for Go, JS/TS, and Python that detect hardcoded secrets (password, token, api_key, etc. assigned to string literals). Ships as default lint rules.
- **`gts transform sbom`** — CycloneDX 1.5 SBOM generation from structural index. Resolves versions from `go.mod`, `package.json`, `requirements.txt`. Optional capability enrichment via `--include-capabilities`. MCP tool `gts_sbom`.
- **`gts analyze licenses`** — dependency license detection via manifest scanning and vendored LICENSE file header matching. 11 SPDX patterns (MIT, Apache-2.0, GPL, BSD, etc.). Configurable deny list via `.gtslint` (`license deny GPL-3.0 -> error "copyleft"`). MCP tool `gts_licenses`.

#### CI/CD Integration (Phase 3)
- **`pkg/sarif`** — SARIF 2.1.0 encoder for GitHub Advanced Security integration. No external dependencies.
- **`--format sarif`** on `analyze check`, `analyze boundaries`, `analyze lint` — upload results directly to GitHub code scanning.
- **`--format` flag migration** — new commands use `--format text|json|sarif`. Existing `--json` flag preserved as backward-compatible alias.
- **`gts init`** — guided project setup: detects languages, generates `.gtsignore`, `.gtsgenerated`, `.gtsboundaries` skeletons. `gts init ci` generates `.github/workflows/gts-check.yml` for GitHub Actions.
- **`gts analyze trends record`** — append quality metrics snapshot to `.gts/trends.jsonl` (cyclomatic max, cognitive max, violations, function/file counts).
- **`gts analyze trends show`** — display metric trends with percentage deltas. Supports `--since` date filtering and `--json` output.

#### Multi-Repo Federation (Phase 4)
- **`gts index export`** — export structural index to portable gzipped `.gtsindex` file with repo metadata (URL, commit SHA, timestamp). Auto-detects git remote and HEAD.
- **`gts index import`** — load and summarize exported indexes.
- **`--federation <dir>` global flag** — point at a directory of `.gtsindex` files to enable cross-repo analysis on federation-safe commands.
- **`internal/federation`** — index merging with repo-prefixed paths, module detection, service graph construction.
- **`gts graph services`** — build repo-to-repo dependency graph from federated indexes. Supports `--dot` for Graphviz output. MCP tool `gts_services`.

#### AI Agent Enhancement (Phase 5)
- **`gts_guardrails` MCP tool** — file-level advisory for agents: generated status, boundary module, complexity scores, hotspot flag, fan-in warnings. Agents call this before editing files.
- **`--concept` flag on `search context`** — concept-aware context packing. Searches symbol names and paths for a concept, traces call chains, packs results within token budget.
- **`gts analyze review`** — aggregated PR review report: complexity delta, boundary violations, new capabilities, blast radius for changed files. MCP tool `gts_review`.
- **`--format embeddings` on `transform chunk`** — RAG-optimized JSONL output with metadata (file, language, symbols, complexity) per chunk for vector DB ingestion.

#### Reporting & Developer Experience (Phase 6)
- **`gts analyze report`** — executive summary aggregating all analyses: codebase overview, complexity distribution, architecture health, security posture, dead code, hotspots. Supports `--format markdown|json`, `--compare <ref>` for delta reporting, `--by-team` for CODEOWNERS-based team breakdown. MCP tool `gts_report`.

### Summary

| Category | Count |
|----------|-------|
| New commands | 12 |
| New MCP tools | 9 |
| New packages | 4 (`pkg/sarif`, `pkg/boundaries`, `internal/reachability`, `internal/federation`) |
| New config files | `.gtsboundaries` |
| Extended config | `.gtslint` (scoped rules, package rules, license rules) |
| New global flags | `--federation`, `--format` |
| New external dependencies | 0 |

## [0.13.1] - 2026-04-01

### Changed
- **Codebase passes its own quality gate.** Refactored all functions that exceeded complexity thresholds: `newIndexBuildCmd` (cyc 55→38), `BuildPathIncrementalWithOptions` (cyc 51→36, cog 81→65), `renameDeclarationsTreeSitter` (cog 107→78), `newQueryCmd` (cog 85→52), `Tools` (379→70 lines). Max cyclomatic dropped from 55 to 38, max cognitive from 107 to 78.
- Split `internal/mcp/service.go` `Tools()` into domain-grouped helpers: `searchTools()`, `graphTools()`, `analyzeTools()`, `transformTools()`.

## [0.13.0] - 2026-04-01

### Added
- **`gts analyze check`** — CI quality gate command. Runs configurable checks (max cyclomatic, cognitive, lines, generated ratio) and exits non-zero on violations. Supports `--json` output.
- **`gts analyze check --base <ref>`** — diff-aware PR gate. Only reports violations on files changed since the base ref. Use in CI with `--base origin/main` to catch regressions without noise from existing code.
- **`gts graph deps --cycles`** — import cycle detection via DFS with rotational deduplication.
- **MCP `gts_check` tool** — quality gate accessible to AI agents with `base`, `max_cyclomatic`, `max_cognitive`, `max_lines`, `max_generated_pct` parameters.
- **MCP `cycles_only` parameter** on `gts_deps` tool for cycle-focused analysis.

### Fixed
- **Cache invalidation no longer forces 84-second rebuilds.** Old caches without `ConfigHashes` are used (with a suggestion to rebuild) instead of triggering a full re-index on every command.
- **`index build` now uses workspace ignores.** Previously used `NewBuilder()` directly, missing `.gtsignore`/`.gtsgenerated` config and generated file detection.
- **`matchGlob` handles multiple `**` segments.** Patterns like `src/**/gen/**/*.pb.go` now work correctly instead of silently failing.
- **`extractHeader` has bounded preamble scan.** Prevents unbounded scanning on files with thousands of license header lines.
- **`dead` command respects `--generator` flag.** Previously only honored `--include-generated`.
- **Removed false-positive-prone patterns:** `*_string.go` (stringer) and `sqlalchemy-alembic` (`Revision ID:`) removed from built-in registry. Both relied on overly broad matching.
- **`NewDetector` panics on invalid registry regexps** instead of silently continuing.
- **`DefaultSkipDirs` no longer allocates on every call.**
- **`inferKind` unused parameter removed.**

### Performance
- Cached index stats: **24ms** (was 84s due to unnecessary rebuild).
- Cached complexity analysis: **1.5s** (was 84s).
- `gts analyze check`: **~2s** with cached index — viable for CI on every commit.

## [0.12.0] - 2026-03-31

### Added
- **Generated file detection** — new `pkg/generated` package with 3-phase detector (user config > filename patterns > header markers) and 40+ built-in signatures across Go, Python, JS/TS, Java/Kotlin, Rust, C/C++, Ruby, C#/.NET, and Swift.
- **`.gtsgenerated` config file** — workspace-level config for declaring generated file patterns with named generators. Supports `@scan-depth` directive.
- **`--include-generated` flag** — global CLI flag to include generated files in analysis output (excluded by default).
- **`--generator` flag** — global CLI flag to filter any command by generator name (e.g. `--generator protobuf`, `--generator human`).
- **Per-generator statistics** — `gts index stats` now shows a `generators:` breakdown with file and symbol counts per generator.
- **`[gen:X]` tags** — file listings, search results, and graph output tag generated files with their generator name.
- **MCP integration** — `include_generated` and `generator` parameters on all 23 MCP tool schemas.
- **Fast regex extraction** — large generated files (>100KB) use fast regex-based symbol extraction instead of tree-sitter parsing. Reduces protobuf indexing from 88 minutes to under 1 second.
- **Auto-skip dependency dirs** — 12 well-known dirs (node_modules, vendor, .venv, target, etc.) skipped during walk.
- **Cache invalidation** — SHA-256 hashes of config files stored in index; stale caches auto-rebuild when config changes.
- **Preamble-aware scanning** — header marker detection skips license/copyright boilerplate and scans 40 lines (up from 20).

### Changed
- **Parallel parsing** — removed `MaxConcurrent=1` bottleneck; indexing now uses `GOMAXPROCS` concurrency with lazy grammar loading for OOM safety.
- **Index schema version** bumped to `0.2.0` for `ConfigHashes` support. Old cached indexes auto-rebuild.
- Upgraded `gotreesitter` to `v0.13.0` (adds `SkipTreeParse` gateway hook).

### Performance
- Arbiter repo (234 files, 5 languages): now indexes in 36 seconds (was: would not finish).
- Arbiter protobuf dir (4 files, 257KB largest): 0.65 seconds (was: 88 minutes).
