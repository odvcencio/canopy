package scope

import (
	"path"
	"strings"

	"m31labs.dev/canopy/pkg/model"
)

func addImportsFromIndex(collector *symbolCollector, summary model.FileSummary) {
	for _, declaration := range summary.Imports {
		for _, name := range importBindingNames(summary.Language, declaration) {
			collector.add(name, "import", declaration, 0)
		}
	}
}

// importBindingNames extracts the names a source-level import introduces into
// lexical scope. Index imports preserve their original syntax, so treating the
// whole declaration as a name produces unusable scope results outside Go.
func importBindingNames(language, declaration string) []string {
	language = strings.ToLower(strings.TrimSpace(language))
	declaration = strings.TrimSpace(declaration)
	if declaration == "" {
		return nil
	}

	var names []string
	switch language {
	case "go":
		names = goImportBindings(declaration)
	case "python":
		names = pythonImportBindings(declaration)
	case "javascript", "typescript", "tsx":
		names = ecmaImportBindings(declaration)
	case "rust":
		names = rustImportBindings(declaration)
	case "java":
		names = javaImportBindings(declaration)
	case "kotlin":
		names = kotlinImportBindings(declaration)
	case "c", "cpp", "c++":
		// #include makes declarations available through preprocessing; it does
		// not introduce a lexical binding with the header spelling.
		return nil
	default:
		name := strings.Trim(strings.TrimSpace(declaration), "\"'`")
		if strings.ContainsAny(name, " \t") {
			return nil
		}
		names = []string{path.Base(name)}
	}
	return uniqueImportNames(names)
}

func goImportBindings(declaration string) []string {
	s := trimStatement(declaration)
	s = strings.TrimSpace(strings.TrimPrefix(s, "import"))
	fields := strings.Fields(s)
	if len(fields) > 1 {
		alias := fields[0]
		if alias == "_" || alias == "." {
			return nil
		}
		return []string{alias}
	}
	if len(fields) == 1 {
		name := strings.Trim(fields[0], "\"'`")
		return []string{path.Base(name)}
	}
	return nil
}

func pythonImportBindings(declaration string) []string {
	s := trimStatement(declaration)
	if strings.HasPrefix(s, "from ") {
		marker := strings.Index(s, " import ")
		if marker < 0 {
			return nil
		}
		return aliasedBindings(s[marker+len(" import "):], false)
	}
	if !strings.HasPrefix(s, "import ") {
		return nil
	}
	items := splitImportList(strings.TrimSpace(strings.TrimPrefix(s, "import ")))
	out := make([]string, 0, len(items))
	for _, item := range items {
		base, alias := splitAlias(item)
		if alias != "" {
			out = append(out, alias)
			continue
		}
		if first, _, ok := strings.Cut(base, "."); ok {
			base = first
		}
		out = append(out, base)
	}
	return out
}

func ecmaImportBindings(declaration string) []string {
	s := trimStatement(declaration)
	s = strings.TrimSpace(strings.TrimPrefix(s, "import"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "type"))
	if s == "" || strings.HasPrefix(s, "\"") || strings.HasPrefix(s, "'") {
		return nil
	}
	if before, _, ok := strings.Cut(s, " from "); ok {
		s = strings.TrimSpace(before)
	}

	var out []string
	if brace := strings.IndexByte(s, '{'); brace >= 0 {
		if close := strings.LastIndexByte(s, '}'); close > brace {
			out = append(out, aliasedBindings(s[brace+1:close], false)...)
		}
		prefix := strings.Trim(strings.TrimSpace(s[:brace]), ",")
		if prefix != "" {
			out = append(out, prefix)
		}
		return out
	}
	if marker := strings.Index(s, "* as "); marker >= 0 {
		return []string{strings.TrimSpace(s[marker+len("* as "):])}
	}
	if first, _, ok := strings.Cut(s, ","); ok {
		return []string{strings.TrimSpace(first)}
	}
	return []string{strings.TrimSpace(s)}
}

func rustImportBindings(declaration string) []string {
	s := trimStatement(declaration)
	if marker := strings.Index(s, "use "); marker >= 0 {
		s = strings.TrimSpace(s[marker+len("use "):])
	} else {
		return nil
	}

	if open := strings.IndexByte(s, '{'); open >= 0 {
		close := strings.LastIndexByte(s, '}')
		if close <= open {
			return nil
		}
		prefix := strings.TrimSuffix(strings.TrimSpace(s[:open]), "::")
		prefixName := lastPathSegment(prefix, "::")
		items := splitImportList(s[open+1 : close])
		out := make([]string, 0, len(items))
		for _, item := range items {
			base, alias := splitAlias(item)
			if alias != "" {
				out = append(out, alias)
				continue
			}
			if base == "self" {
				out = append(out, prefixName)
				continue
			}
			if base == "*" {
				continue
			}
			out = append(out, lastPathSegment(base, "::"))
		}
		return out
	}

	base, alias := splitAlias(s)
	if alias != "" {
		return []string{alias}
	}
	if strings.HasSuffix(base, "::*") {
		return nil
	}
	return []string{lastPathSegment(base, "::")}
}

func javaImportBindings(declaration string) []string {
	s := trimStatement(declaration)
	s = strings.TrimSpace(strings.TrimPrefix(s, "import"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "static"))
	if strings.HasSuffix(s, ".*") {
		return nil
	}
	return []string{lastPathSegment(s, ".")}
}

func kotlinImportBindings(declaration string) []string {
	s := trimStatement(declaration)
	s = strings.TrimSpace(strings.TrimPrefix(s, "import"))
	base, alias := splitAlias(s)
	if alias != "" {
		return []string{alias}
	}
	if strings.HasSuffix(base, ".*") {
		return nil
	}
	return []string{lastPathSegment(base, ".")}
}

func aliasedBindings(list string, firstSegment bool) []string {
	items := splitImportList(list)
	out := make([]string, 0, len(items))
	for _, item := range items {
		base, alias := splitAlias(item)
		if alias != "" {
			out = append(out, alias)
			continue
		}
		if base == "*" {
			continue
		}
		if firstSegment {
			if first, _, ok := strings.Cut(base, "."); ok {
				base = first
			}
		} else {
			base = lastPathSegment(base, ".")
		}
		out = append(out, base)
	}
	return out
}

func splitAlias(item string) (string, string) {
	item = strings.TrimSpace(strings.Trim(item, "()"))
	if before, after, ok := strings.Cut(item, " as "); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return item, ""
}

func splitImportList(list string) []string {
	list = strings.TrimSpace(strings.Trim(list, "()"))
	parts := strings.Split(list, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func trimStatement(s string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";"))
}

func lastPathSegment(value, separator string) string {
	value = strings.TrimSpace(value)
	if idx := strings.LastIndex(value, separator); idx >= 0 {
		value = value[idx+len(separator):]
	}
	return strings.TrimSpace(value)
}

func uniqueImportNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(strings.Trim(name, "{}()"))
		if name == "" || name == "_" || name == "." || name == "*" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
