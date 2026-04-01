package lint

// SecretsPatterns returns built-in tree-sitter query patterns that detect
// potential hardcoded secrets in Go, JavaScript/TypeScript, and Python source
// files. The queries match variable/constant declarations whose names contain
// sensitive keywords (password, token, secret, api_key, etc.) and that are
// assigned string literal values.
//
// These patterns integrate with EvaluatePatterns -- queries that target the
// wrong language grammar fail to compile silently and are skipped.
func SecretsPatterns() []QueryPattern {
	return []QueryPattern{
		{
			ID:      "secrets/hardcoded-go",
			Query:   goSecretsQuery,
			Message: "potential hardcoded secret in Go source",
		},
		{
			ID:      "secrets/hardcoded-js",
			Query:   jsSecretsQuery,
			Message: "potential hardcoded secret in JavaScript/TypeScript source",
		},
		{
			ID:      "secrets/hardcoded-python",
			Query:   pythonSecretsQuery,
			Message: "potential hardcoded secret in Python source",
		},
	}
}

// goSecretsQuery detects short variable declarations and const specs where the
// identifier name matches sensitive patterns and the value is a string literal.
const goSecretsQuery = `
(short_var_declaration
  left: (expression_list
    (identifier) @name)
  right: (expression_list
    (interpreted_string_literal) @value)
  (#match? @name "(?i)(password|passwd|secret|token|api_key|apikey|private_key|privatekey|secret_key|secretkey|access_key|accesskey|auth_token|authtoken|credentials)")
) @violation

(var_spec
  name: (identifier) @name
  value: (expression_list
    (interpreted_string_literal) @value)
  (#match? @name "(?i)(password|passwd|secret|token|api_key|apikey|private_key|privatekey|secret_key|secretkey|access_key|accesskey|auth_token|authtoken|credentials)")
) @violation

(const_spec
  name: (identifier) @name
  value: (expression_list
    (interpreted_string_literal) @value)
  (#match? @name "(?i)(password|passwd|secret|token|api_key|apikey|private_key|privatekey|secret_key|secretkey|access_key|accesskey|auth_token|authtoken|credentials)")
) @violation
`

// jsSecretsQuery detects variable declarations (const/let/var) in JavaScript
// and TypeScript where the identifier matches sensitive patterns and the value
// is a string literal.
const jsSecretsQuery = `
(variable_declarator
  name: (identifier) @name
  value: (string) @value
  (#match? @name "(?i)(password|passwd|secret|token|api_key|apikey|private_key|privatekey|secret_key|secretkey|access_key|accesskey|auth_token|authtoken|credentials)")
) @violation
`

// pythonSecretsQuery detects assignments and annotated assignments in Python
// where the identifier matches sensitive patterns and the value is a string.
const pythonSecretsQuery = `
(assignment
  left: (identifier) @name
  right: (string) @value
  (#match? @name "(?i)(password|passwd|secret|token|api_key|apikey|private_key|privatekey|secret_key|secretkey|access_key|accesskey|auth_token|authtoken|credentials)")
) @violation
`
