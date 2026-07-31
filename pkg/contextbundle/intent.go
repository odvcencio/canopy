package contextbundle

import "strings"

// NormalizeRequest fills in request-level defaults so callers (CLI, MCP,
// Buckley's controller) do not have to repeat this policy: an unset task
// kind defaults to implement, and an unset policy version pins the current
// deterministic scoring policy (spec 10.8).
func NormalizeRequest(req Request) Request {
	req.Root = strings.TrimSpace(req.Root)
	if req.Intent.Kind == "" {
		req.Intent.Kind = TaskImplement
	}
	if req.PolicyVersion == "" {
		req.PolicyVersion = PolicyVersion
	}
	if req.OutputFormat == "" {
		req.OutputFormat = "markdown"
	}
	return req
}

// modeDescription documents the per-task-kind default context shape from
// spec 10.10; initialMode (variants.go) is the executable form of this
// table. Exported for CLI help text and MCP tool descriptions.
func modeDescription(kind TaskKind) string {
	switch kind {
	case TaskExplore:
		return "broad repository map; signatures/types for task-matching files; call graph depth 1"
	case TaskImplement:
		return "full/body for target entities; signatures for direct callers/callees; contracts and mapped tests"
	case TaskDebug:
		return "latest failure evidence first; error path, focus body, callers, tests, recent edits"
	case TaskReview:
		return "structural change receipt; changed bodies; public API deltas; incoming callers; mapped tests"
	case TaskCommit:
		return "change receipt; staged entity summary; high-signal hunks; verification summary"
	case TaskResume:
		return "current checkpoint; snapshot delta since checkpoint; active evidence receipts; blockers"
	case TaskTest:
		return "target entities and their mapped tests at body detail; direct callers as signatures"
	default:
		return "implement-mode defaults"
	}
}
