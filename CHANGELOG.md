# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

- Nothing yet.

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
