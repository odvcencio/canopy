# canopy

Structural code analysis toolkit powered by [gotreesitter](https://github.com/odvcencio/gotreesitter). AST-based indexing, search, call graph analysis, security intelligence, architecture governance, and AI agent integration across 206+ languages.

## Install

```bash
go install github.com/odvcencio/canopy/cmd/canopy@latest
```

## Quickstart

```bash
# Build a structural index
canopy index build .

# Search for symbols
canopy search refs ParseConfig .

# Check code quality (CI gate)
canopy analyze check --max-cyclomatic 30

# Full executive report
canopy analyze report --format markdown

# Run MCP server for AI agents
canopy mcp --root .
```

## Current Release

`v0.16.0` upgrades gotreesitter to `v0.15.0` and tightens self-indexing for large repositories. Index walks now prune ignored directories before descent, skip unsupported/tagless grammars before parsing, and support `CANOPY_INDEX_GC_EVERY` for constrained containers. Call graph roots can also be narrowed with `--file` or `path/to/file.go:Name` when multiple definitions share a name.

## Commands

### Index — Build and manage structural indexes

| Command | Description |
|---------|-------------|
| `canopy index build [path]` | Build/incrementally update index with watch mode |
| `canopy index map` | Structural table-of-contents for indexed files |
| `canopy index files` | List files with density filters and sorting |
| `canopy index stats` | Codebase metrics: symbol counts, language breakdown |
| `canopy index diff` | Compare structural changes between two snapshots |
| `canopy index errors` | Show parse errors from indexing |
| `canopy index validate` | Validate index integrity |
| `canopy index export` | Export index to portable `.canopyindex` file for federation |
| `canopy index import` | Load and summarize exported indexes |

### Search — Find symbols, references, and patterns

| Command | Description |
|---------|-------------|
| `canopy search grep` | Structural selector queries (e.g. `function_definition[name=/^Test/]`) |
| `canopy search refs` | Find references by symbol name or regex |
| `canopy search query` | Raw tree-sitter S-expression queries |
| `canopy search scope` | Resolve symbols in scope at file + line |
| `canopy search context` | Pack focused context for agent token budgets. `--concept` for concept-aware packing |
| `canopy search symbols` | Search symbols by pattern |
| `canopy search imports` | Analyze import patterns |

### Graph — Call graph, dependency, and coverage analysis

| Command | Description |
|---------|-------------|
| `canopy graph calls` | Traverse call graph edges from matching roots |
| `canopy graph dead` | List callable definitions with zero incoming references |
| `canopy graph deps` | Import dependency graph with cycle detection (`--cycles`) |
| `canopy graph bridge` | Map cross-component dependency bridges |
| `canopy graph impact` | Blast radius via reverse call graph |
| `canopy graph testmap` | Map test functions to implementations |
| `canopy graph fanin` | Rank functions by incoming call count |
| `canopy graph unresolved` | Show unresolved call references |
| `canopy graph drift` | Compare dependency graph between two git refs |
| `canopy graph services` | Repo-to-repo dependency map from federated indexes |

### Analyze — Quality, complexity, security, and governance

| Command | Description |
|---------|-------------|
| `canopy analyze check` | CI quality gate with configurable thresholds. `--base` for diff-aware PR filtering. `--format sarif` for GitHub Advanced Security |
| `canopy analyze boundaries` | Module boundary enforcement from `.canopyboundaries`. `--format sarif` |
| `canopy analyze complexity` | Per-function cyclomatic, cognitive, nesting, fan-in/out metrics |
| `canopy analyze hotspot` | Code hotspots from git churn + complexity + centrality |
| `canopy analyze lint` | Structural lint with built-in rules, query patterns, and secrets detection. `--format sarif` |
| `canopy analyze capa` | Capability detection with MITRE ATT&CK mapping |
| `canopy analyze reachability` | Supply chain analysis: does package X reach capability Y? |
| `canopy analyze licenses` | Dependency license detection with SPDX matching and deny rules |
| `canopy analyze similarity` | Find similar functions between codebases |
| `canopy analyze duplication` | Detect code duplication |
| `canopy analyze report` | Executive summary: complexity, architecture, security, dead code, hotspots. `--by-team` for CODEOWNERS breakdown |
| `canopy analyze review` | Aggregated PR review: complexity delta, boundary violations, new capabilities, blast radius |
| `canopy analyze trends` | Track quality metrics over time (`record` / `show`) |

### Transform — Code transformations and output generation

| Command | Description |
|---------|-------------|
| `canopy transform refactor` | AST-aware declaration renames with cross-package callsite updates |
| `canopy transform chunk` | AST-boundary chunks for RAG/indexing. `--format embeddings` for vector DB |
| `canopy transform sbom` | CycloneDX 1.5 SBOM with optional capability enrichment |
| `canopy transform yara` | Generate YARA rules from structural analysis |
| `canopy transform normalize` | Normalize decompiler output |

### Other

| Command | Description |
|---------|-------------|
| `canopy init` | Guided project setup: generates `.canopyignore`, `.canopygenerated`, `.canopyboundaries` |
| `canopy init ci` | Generate GitHub Actions workflow for CI quality checks |
| `canopy mcp` | MCP stdio server exposing 30+ tools to AI agents (Claude, Cursor, VS Code) |

## Configuration Files

| File | Purpose |
|------|---------|
| `.canopyignore` | Gitignore-style patterns to exclude files from indexing |
| `.canopygenerated` | Declare generated file patterns with named generators |
| `.canopyboundaries` | Module boundary rules (allow/deny import relationships) |
| `.canopylint` | Lint thresholds, scoped overrides, package-level rules, ignore rules, license deny rules |

### `.canopyboundaries` example

```
# pkg/model has no internal dependencies
module pkg/model      allow -

# pkg/index can only import these packages
module pkg/index      allow pkg/model, pkg/lang, pkg/generated

# internal packages can use any pkg but not cross-import
module internal/*     allow pkg/*
module internal/*     deny  internal/*
```

### `.canopylint` example

```
# Override default thresholds
cyclomatic > 35 -> warn "function too complex"
cognitive > 60  -> warn "hard to reason about"

# Scoped rules
fan_out > 10 in pkg/* -> warn "high fan-out"

# Package-level rules
package import_depth > 5 -> error "dependency chain too deep"
package exported_symbols > 50 in pkg/* -> warn "API surface too large"
package no_import_cycles -> error "import cycle detected"

# Ignore specific functions
ignore cyclomatic in generated/

# License enforcement
license deny GPL-3.0, AGPL-3.0 -> error "copyleft license not permitted"
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--include-generated` | Include generated files in output (excluded by default) |
| `--generator <name>` | Filter to specific generator (e.g. `protobuf`, `human`) |
| `--federation <dir>` | Directory of `.canopyindex` files for cross-repo analysis |

## Multi-Repo Federation

Analyze across multiple repositories without a central server:

```bash
# In each repo's CI:
canopy index build . && canopy index export -o myrepo.canopyindex

# Collect all .canopyindex files, then:
canopy graph services --federation ./indexes/
canopy search refs "AuthService" --federation ./indexes/
canopy graph dead --federation ./indexes/
```

## CI Integration

Generate a GitHub Actions workflow:

```bash
canopy init ci
```

This creates `.github/workflows/canopy-check.yml` that runs quality checks on PRs with SARIF upload for inline annotations.

Manual SARIF integration:

```bash
canopy analyze check --base origin/main --format sarif > results.sarif
```

Track metrics over time:

```bash
canopy analyze trends record    # append current metrics to .canopy/trends.jsonl
canopy analyze trends show      # display trend summary with deltas
```

## MCP Server

The MCP stdio server exposes 30+ structural analysis tools to AI agents via JSON-RPC.

```bash
canopy mcp --root /path/to/repo
canopy mcp --root /path/to/repo --allow-writes  # enable refactoring tools
```

### Client setup

**Claude Desktop / Claude Code / Cursor / VS Code:**

```json
{
  "mcpServers": {
    "canopy": {
      "command": "canopy",
      "args": ["mcp", "--root", "/path/to/repo"]
    }
  }
}
```

### Key MCP tools

| Tool | Description |
|------|-------------|
| `gts_guardrails` | File-level advisory: generated status, complexity, fan-in warnings. Call before editing |
| `gts_review` | Aggregated PR review for changed files |
| `gts_reachability` | Supply chain capability analysis |
| `gts_boundaries` | Module boundary enforcement |
| `gts_report` | Executive summary of all analyses |
| `gts_callgraph` | Call graph traversal |
| `gts_dead` | Dead code detection |
| `gts_impact` | Blast radius computation |
| `gts_context` | Token-budgeted context packing |
| `gts_grep` | Structural selector search |

## Selector Syntax

Used by `canopy search grep` and `gts_grep`:

```
<kind>[filter1,filter2,...]
```

**Examples:**
- `function_definition[name=/^Test/]`
- `method_definition[receiver=/Service/,signature=/Serve/]`
- `*[file=/handlers\/.go$/,start>=20,end<=200]`

**Filters:** `name`, `signature`, `receiver`, `file` (regex); `start`, `end`, `line` (numeric comparisons).

## Language Support

206+ languages via gotreesitter grammars including Go, Python, JavaScript/TypeScript, Java, C/C++, Rust, C#, Ruby, PHP, Swift, Kotlin, Scala, SQL, HTML/CSS, YAML, JSON, Terraform, Dockerfile, and many more.

**Scope resolution** (symbol-in-scope at file+line): Go, Python, TypeScript.

## License

See [LICENSE](LICENSE) for details.
