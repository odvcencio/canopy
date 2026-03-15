package scope

import (
	"embed"
	"fmt"
	"strings"

	"github.com/odvcencio/gotreesitter"
)

//go:embed rules/*.scm
var rulesFS embed.FS

// Rules holds a compiled scope query for a language.
type Rules struct {
	Language string
	Query    *gotreesitter.Query
}

// LoadRules loads and compiles the .scm scope rules for a language.
func LoadRules(langName string, lang *gotreesitter.Language) (*Rules, error) {
	data, err := rulesFS.ReadFile("rules/" + langName + ".scm")
	if err != nil {
		return nil, fmt.Errorf("scope rules not found for %s: %w", langName, err)
	}
	q, err := gotreesitter.NewQuery(string(data), lang)
	if err != nil {
		return nil, fmt.Errorf("compile scope rules for %s: %w", langName, err)
	}
	return &Rules{Language: langName, Query: q}, nil
}

// defKey identifies a definition by name and position, used to deduplicate
// overlapping query patterns that match the same AST node.
type defKey struct {
	name     string
	startRow uint32
	startCol uint32
}

// BuildFileScope constructs a scope tree from a parse tree using scope rules.
func BuildFileScope(
	tree *gotreesitter.Tree,
	lang *gotreesitter.Language,
	src []byte,
	rules *Rules,
	path string,
) *Scope {
	root := NewScope(ScopeFile, nil)
	seen := make(map[defKey]bool)

	cursor := rules.Query.Exec(tree.RootNode(), lang, src)
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		processMatch(match, root, src, path, seen)
	}
	return root
}

// addDefDedup adds a definition to the scope, deduplicating by name and position.
func addDefDedup(fileScope *Scope, def Definition, seen map[defKey]bool) {
	sp := defKey{
		name:     def.Name,
		startRow: uint32(def.Loc.StartLine),
		startCol: uint32(def.Loc.StartCol),
	}
	if seen[sp] {
		return
	}
	seen[sp] = true
	fileScope.AddDef(def)
}

// capData holds the extracted string data from a single capture,
// decoupled from tree-sitter nodes so tests can construct them directly.
type capData struct {
	name string
	text string
	loc  Location
}

func processMatch(
	match gotreesitter.QueryMatch,
	fileScope *Scope,
	src []byte,
	path string,
	seen map[defKey]bool,
) {
	// Extract capture data from tree-sitter nodes into plain structs.
	caps := make([]capData, len(match.Captures))
	for i, cap := range match.Captures {
		caps[i] = capData{
			name: cap.Name,
			text: cap.Node.Text(src),
			loc:  nodeLocation(cap.Node, path),
		}
	}
	processCaptures(fileScope, caps, seen)
}

// processCaptures handles all capture logic using plain string data,
// allowing both real tree-sitter matches and test-constructed data.
func processCaptures(fileScope *Scope, caps []capData, seen map[defKey]bool) {
	for _, c := range caps {
		name := c.name
		text := c.text
		loc := c.loc

		switch {
		case name == "def.function":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefFunction,
				Loc:  loc,
			}, seen)

		case name == "def.method":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefMethod,
				Loc:  loc,
			}, seen)

		case name == "def.variable" || name == "def.variable.notype":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefVariable,
				Loc:  loc,
			}, seen)

		case name == "def.variable.type":
			// Attach type annotation to most recent variable def
			if len(fileScope.Defs) > 0 {
				last := &fileScope.Defs[len(fileScope.Defs)-1]
				if last.Kind == DefVariable {
					last.TypeAnnot = text
				}
			}

		case name == "def.constant":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefConstant,
				Loc:  loc,
			}, seen)

		case name == "def.type":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefType,
				Loc:  loc,
			}, seen)

		case name == "def.class":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefClass,
				Loc:  loc,
			}, seen)

		case name == "def.interface":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefInterface,
				Loc:  loc,
			}, seen)

		case name == "def.import":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefImport,
				Loc:  loc,
			}, seen)

		case name == "def.param":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefParam,
				Loc:  loc,
			}, seen)

		case name == "def.import.path":
			addDefDedup(fileScope, Definition{
				Name:       importName(text),
				Kind:       DefImport,
				ImportPath: text,
				Loc:        loc,
			}, seen)

		case name == "def.import.alias":
			// Will be followed by def.import.aliased.path in same match

		case name == "def.import.aliased.path":
			// Find the alias capture in this same match
			alias := ""
			for _, sibling := range caps {
				if sibling.name == "def.import.alias" {
					alias = sibling.text
					break
				}
			}
			addDefDedup(fileScope, Definition{
				Name:       alias,
				Kind:       DefImport,
				ImportPath: text,
				Loc:        loc,
			}, seen)

		case name == "def.function.return":
			// Attach return type to most recent function/method def
			if len(fileScope.Defs) > 0 {
				last := &fileScope.Defs[len(fileScope.Defs)-1]
				if last.Kind == DefFunction || last.Kind == DefMethod {
					last.ReturnType = text
				}
			}

		case name == "def.method.receiver":
			// Attach receiver to most recent method def
			if len(fileScope.Defs) > 0 {
				last := &fileScope.Defs[len(fileScope.Defs)-1]
				if last.Kind == DefMethod {
					last.Receiver = text
				}
			}

		case name == "def.class.extends":
			// Attach base class to most recent class def
			if len(fileScope.Defs) > 0 {
				last := &fileScope.Defs[len(fileScope.Defs)-1]
				if last.Kind == DefClass {
					last.BaseClasses = append(last.BaseClasses, text)
				}
			}

		case name == "def.field":
			addDefDedup(fileScope, Definition{
				Name: text,
				Kind: DefField,
				Loc:  loc,
			}, seen)

		case name == "def.field.type":
			// Attach type to most recent field def
			if len(fileScope.Defs) > 0 {
				last := &fileScope.Defs[len(fileScope.Defs)-1]
				if last.Kind == DefField {
					last.TypeAnnot = text
				}
			}

		case name == "def.param.type":
			// Attach type to most recent param def
			if len(fileScope.Defs) > 0 {
				last := &fileScope.Defs[len(fileScope.Defs)-1]
				if last.Kind == DefParam {
					last.TypeAnnot = text
				}
			}

		case name == "ref.operand":
			fileScope.AddRef(Ref{
				Name: text,
				Loc:  loc,
			})

		case name == "ref.member":
			// Attach member to most recent ref
			if len(fileScope.Refs) > 0 {
				fileScope.Refs[len(fileScope.Refs)-1].Member = text
			}

		case name == "ref.call" || name == "ref":
			fileScope.AddRef(Ref{
				Name: text,
				Loc:  loc,
			})
		}
	}
}

func nodeLocation(node *gotreesitter.Node, path string) Location {
	sp := node.StartPoint()
	ep := node.EndPoint()
	return Location{
		File:      path,
		StartLine: int(sp.Row) + 1,
		EndLine:   int(ep.Row) + 1,
		StartCol:  int(sp.Column),
		EndCol:    int(ep.Column),
	}
}

// importName extracts the package name from an import path like "\"fmt\"".
func importName(path string) string {
	path = strings.Trim(path, "\"")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
