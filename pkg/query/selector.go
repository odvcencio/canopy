// Package query implements a selector DSL for matching symbols by kind, name, signature, receiver, file, and line range.
package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

var validKind = regexp.MustCompile(`^(?:\*|[a-z_][a-z0-9_]*)$`)
var lineFilterPattern = regexp.MustCompile(`^(start|end|line)\s*(<=|>=|=)\s*(\d+)$`)

type Selector struct {
	Kind        string
	NameRE      *regexp.Regexp
	SignatureRE *regexp.Regexp
	ReceiverRE  *regexp.Regexp
	FileRE      *regexp.Regexp
	StartMin    *int
	StartMax    *int
	EndMin      *int
	EndMax      *int
	Line        *int
	Raw         string
}

func ParseSelector(raw string) (Selector, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Selector{}, fmt.Errorf("selector cannot be empty")
	}

	selector := Selector{
		Kind: "*",
		Raw:  text,
	}

	kind := text
	filter := ""
	if open := strings.Index(text, "["); open >= 0 {
		if !strings.HasSuffix(text, "]") {
			return Selector{}, fmt.Errorf("invalid selector %q: missing closing bracket", text)
		}
		kind = strings.TrimSpace(text[:open])
		filter = strings.TrimSpace(text[open+1 : len(text)-1])
	}

	if kind != "" {
		selector.Kind = kind
	}
	if !validKind.MatchString(selector.Kind) {
		return Selector{}, fmt.Errorf("invalid selector kind %q: must be a tree-sitter node type (e.g. function_definition, method_definition, type_definition, *); see 'canopy grep --help'", selector.Kind)
	}

	if filter == "" {
		return selector, nil
	}

	filters, err := splitFilterClauses(filter)
	if err != nil {
		return Selector{}, err
	}

	for _, clause := range filters {
		if err := applyFilterClause(&selector, clause); err != nil {
			return Selector{}, err
		}
	}
	if err := validateNumericFilters(selector); err != nil {
		return Selector{}, err
	}
	return selector, nil
}

func splitFilterClauses(filter string) ([]string, error) {
	clauses := make([]string, 0, 4)
	start := 0
	inRegex := false
	escaped := false

	for i := 0; i < len(filter); i++ {
		ch := filter[i]
		if inRegex && ch == '\\' && !escaped {
			escaped = true
			continue
		}
		if ch == '/' && !escaped {
			inRegex = !inRegex
		}
		if ch == ',' && !inRegex {
			segment := strings.TrimSpace(filter[start:i])
			if segment == "" {
				return nil, fmt.Errorf("invalid selector filter %q: empty clause", filter)
			}
			clauses = append(clauses, segment)
			start = i + 1
		}
		if escaped {
			escaped = false
		}
	}

	last := strings.TrimSpace(filter[start:])
	if last == "" {
		return nil, fmt.Errorf("invalid selector filter %q: empty clause", filter)
	}
	clauses = append(clauses, last)
	return clauses, nil
}

func applyFilterClause(selector *Selector, clause string) error {
	regexFilters := []struct {
		prefix string
		setter func(*regexp.Regexp)
	}{
		{
			prefix: "name=",
			setter: func(value *regexp.Regexp) {
				selector.NameRE = value
			},
		},
		{
			prefix: "signature=",
			setter: func(value *regexp.Regexp) {
				selector.SignatureRE = value
			},
		},
		{
			prefix: "receiver=",
			setter: func(value *regexp.Regexp) {
				selector.ReceiverRE = value
			},
		},
		{
			prefix: "file=",
			setter: func(value *regexp.Regexp) {
				selector.FileRE = value
			},
		},
	}

	for _, filter := range regexFilters {
		if !strings.HasPrefix(clause, filter.prefix) {
			continue
		}
		value, err := compileRegexFilter(clause[len(filter.prefix):])
		if err != nil {
			return fmt.Errorf("invalid %s filter: %w", strings.TrimSuffix(filter.prefix, "="), err)
		}
		filter.setter(value)
		return nil
	}

	matches := lineFilterPattern.FindStringSubmatch(clause)
	if matches == nil {
		return fmt.Errorf("unsupported selector filter %q", clause)
	}

	field := matches[1]
	op := matches[2]
	value, err := strconv.Atoi(matches[3])
	if err != nil || value <= 0 {
		return fmt.Errorf("line filters require positive integers: %q", clause)
	}

	switch field {
	case "start":
		applyBound(&selector.StartMin, &selector.StartMax, op, value)
	case "end":
		applyBound(&selector.EndMin, &selector.EndMax, op, value)
	case "line":
		if op != "=" {
			return fmt.Errorf("line filter supports only '=' operator: %q", clause)
		}
		selector.Line = intPtr(value)
	default:
		return fmt.Errorf("unsupported line filter %q", clause)
	}
	return nil
}

func compileRegexFilter(raw string) (*regexp.Regexp, error) {
	value := strings.TrimSpace(raw)
	if len(value) < 2 || value[0] != '/' || value[len(value)-1] != '/' {
		return nil, fmt.Errorf("must be a /regex/ literal")
	}

	expression := value[1 : len(value)-1]
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid regex %q: %w", expression, err)
	}
	return compiled, nil
}

func applyBound(min **int, max **int, operator string, value int) {
	switch operator {
	case ">=":
		*min = intPtr(value)
	case "<=":
		*max = intPtr(value)
	case "=":
		*min = intPtr(value)
		*max = intPtr(value)
	}
}

func intPtr(value int) *int {
	copied := value
	return &copied
}

func validateNumericFilters(selector Selector) error {
	if selector.StartMin != nil && selector.StartMax != nil && *selector.StartMin > *selector.StartMax {
		return fmt.Errorf("invalid start range: min %d is greater than max %d", *selector.StartMin, *selector.StartMax)
	}
	if selector.EndMin != nil && selector.EndMax != nil && *selector.EndMin > *selector.EndMax {
		return fmt.Errorf("invalid end range: min %d is greater than max %d", *selector.EndMin, *selector.EndMax)
	}
	return nil
}

func (s Selector) Match(symbol model.Symbol) bool {
	if s.Kind != "*" && symbol.Kind != s.Kind {
		return false
	}
	if s.NameRE != nil && !s.NameRE.MatchString(symbol.Name) {
		return false
	}
	if s.SignatureRE != nil && !s.SignatureRE.MatchString(symbol.Signature) {
		return false
	}
	if s.ReceiverRE != nil && !s.ReceiverRE.MatchString(symbol.Receiver) {
		return false
	}
	if s.FileRE != nil && !s.FileRE.MatchString(symbol.File) {
		return false
	}
	if s.StartMin != nil && symbol.StartLine < *s.StartMin {
		return false
	}
	if s.StartMax != nil && symbol.StartLine > *s.StartMax {
		return false
	}
	if s.EndMin != nil && symbol.EndLine < *s.EndMin {
		return false
	}
	if s.EndMax != nil && symbol.EndLine > *s.EndMax {
		return false
	}
	if s.Line != nil && (*s.Line < symbol.StartLine || *s.Line > symbol.EndLine) {
		return false
	}
	return true
}
