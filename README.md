# gts-suite

Structural code analysis toolkit powered by [gotreesitter](https://github.com/odvcencio/gotreesitter). AST-based indexing, search, call graph analysis, security intelligence, architecture governance, and AI agent integration across 206+ languages.

## Install

```bash
go install github.com/odvcencio/gts-suite/cmd/gts@latest
```

## Quickstart

```bash
# Build a structural index
gts index build .

# Search for symbols
gts search refs ParseConfig .

# Check code quality (CI gate)
gts analyze check --max-cyclomatic 30

# Full executive report
gts analyze report --format markdown

# Run MCP server for AI agents
gts mcp --root .
```

## Commands

### Index — Build and manage structural indexes

| Command | Description |
|---------|-------------|
| `gts index build [path]` | Build/incrementally update index with watch mode |
| `gts index map` | Structural table-of-contents for indexed files |
| `gts index files` | List files with density filters and sorting |
| `gts index stats` | Codebase metrics: symbol counts, language breakdown |
| `gts index diff` | Compare structural changes between two snapshots |
| `gts index errors` | Show parse errors from indexing |
| `gts index validate` | Validate index integrity |
| `gts index export` | Export index to portable `.gtsindex` file for federation |
| `gts index import` | Load and summarize exported indexes |

### Search — Find symbols, references, and patterns

| Command | Description |
|---------|-------------|
| `gts search grep` | Structural selector queries (e.g. `function_definition[name=/^Test/]`) |
| `gts search refs` | Find references by symbol name or regex |
| `gts search query` | Raw tree-sitter S-expression queries |
| `gts search scope` | Resolve symbols in scope at file + line |
| `gts search context` | Pack focused context for agent token budgets. `--concept` for concept-aware packing |
| `gts search symbols` | Search symbols by pattern |
| `gts search imports` | Analyze import patterns |

### Graph — Call graph, dependency, and coverage analysis

| Command | Description |
|---------|-------------|
| `gts graph calls` | Traverse call graph edges from matching roots |
| `gts graph dead` | List callable definitions with zero incoming references |
| `gts graph deps` | Import dependency graph with cycle detection (`--cycles`) |
| `gts graph bridge` | Map cross-component dependency bridges |
| `gts graph impact` | Blast radius via reverse call graph |
| `gts graph testmap` | Map test functions to implementations |
| `gts graph fanin` | Rank functions by incoming call count |
| `gts graph unresolved` | Show unresolved call references |
| `gts graph drift` | Compare dependency graph between two git refs |
| `gts graph services` | Repo-to-repo dependency map from federated indexes |

### Analyze — Quality, complexity, security, and governance

| Command | Description |
|---------|-------------|
| `gts analyze check` | CI quality gate with configurable thresholds. `--base` for diff-aware PR filtering. `--format sarif` for GitHub Advanced Security |
| `gts analyze boundaries` | Module boundary enforcement from `.gtsboundaries`. `--format sarif` |
| `gts analyze complexity` | Per-function cyclomatic, cognitive, nesting, fan-in/out metrics |
| `gts analyze hotspot` | Code hotspots from git churn + complexity + centrality |
| `gts analyze lint` | Structural lint with built-in rules, query patterns, and secrets detection. `--format sarif` |
| `gts analyze capa` | Capability detection with MITRE ATT&CK mapping |
| `gts analyze reachability` | Supply chain analysis: does package X reach capability Y? |
| `gts analyze licenses` | Dependency license detection with SPDX matching and deny rules |
| `gts analyze similarity` | Find similar functions between codebases |
| `gts analyze duplication` | Detect code duplication |
| `gts analyze report` | Executive summary: complexity, architecture, security, dead code, hotspots. `--by-team` for CODEOWNERS breakdown |
| `gts analyze review` | Aggregated PR review: complexity delta, boundary violations, new capabilities, blast radius |
| `gts analyze trends` | Track quality metrics over time (`record` / `show`) |

### Transform — Code transformations and output generation

| Command | Description |
|---------|-------------|
| `gts transform refactor` | AST-aware declaration renames with cross-package callsite updates |
| `gts transform chunk` | AST-boundary chunks for RAG/indexing. `--format embeddings` for vector DB |
| `gts transform sbom` | CycloneDX 1.5 SBOM with optional capability enrichment |
| `gts transform yara` | Generate YARA rules from structural analysis |
| `gts transform normalize` | Normalize decompiler output |

### Other

| Command | Description |
|---------|-------------|
| `gts init` | Guided project setup: generates `.gtsignore`, `.gtsgenerated`, `.gtsboundaries` |
| `gts init ci` | Generate GitHub Actions workflow for CI quality checks |
| `gts mcp` | MCP stdio server exposing 30+ tools to AI agents (Claude, Cursor, VS Code) |

## Configuration Files

| File | Purpose |
|------|---------|
| `.gtsignore` | Gitignore-style patterns to exclude files from indexing |
| `.gtsgenerated` | Declare generated file patterns with named generators |
| `.gtsboundaries` | Module boundary rules (allow/deny import relationships) |
| `.gtslint` | Lint thresholds, scoped overrides, package-level rules, ignore rules, license deny rules |

### `.gtsboundaries` example

```
# pkg/model has no internal dependencies
module pkg/model      allow -

# pkg/index can only import these packages
module pkg/index      allow pkg/model, pkg/lang, pkg/generated

# internal packages can use any pkg but not cross-import
module internal/*     allow pkg/*
module internal/*     deny  internal/*
```

### `.gtslint` example

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
| `--federation <dir>` | Directory of `.gtsindex` files for cross-repo analysis |

## Multi-Repo Federation

Analyze across multiple repositories without a central server:

```bash
# In each repo's CI:
gts index build . && gts index export -o myrepo.gtsindex

# Collect all .gtsindex files, then:
gts graph services --federation ./indexes/
gts search refs "AuthService" --federation ./indexes/
gts graph dead --federation ./indexes/
```

## CI Integration

Generate a GitHub Actions workflow:

```bash
gts init ci
```

This creates `.github/workflows/gts-check.yml` that runs quality checks on PRs with SARIF upload for inline annotations.

Manual SARIF integration:

```bash
gts analyze check --base origin/main --format sarif > results.sarif
```

Track metrics over time:

```bash
gts analyze trends record    # append current metrics to .gts/trends.jsonl
gts analyze trends show      # display trend summary with deltas
```

## MCP Server

The MCP stdio server exposes 30+ structural analysis tools to AI agents via JSON-RPC.

```bash
gts mcp --root /path/to/repo
gts mcp --root /path/to/repo --allow-writes  # enable refactoring tools
```

### Client setup

**Claude Desktop / Claude Code / Cursor / VS Code:**

```json
{
  "mcpServers": {
    "gts": {
      "command": "gts",
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

Used by `gts search grep` and `gts_grep`:

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
