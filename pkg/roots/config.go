package roots

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ConfigFileName is the per-repo roots configuration file. It is discovered at
// the index root and merged over the built-in language profiles, letting teams
// teach canopy about their own frameworks (or whole new languages) without
// recompiling.
const ConfigFileName = ".canopyroots.json"

// Config is the on-disk schema for .canopyroots.json. Each entry is keyed by a
// file extension (".java", ".rb", …) and merges over the built-in profile for
// that extension — annotation/test sets are unioned; scalar fields
// (syntax, entrypoint, test_func_pattern) override when present. An extension
// with no built-in profile defines a brand-new language.
type Config struct {
	Profiles map[string]ProfileConfig `json:"profiles"`
}

// ProfileConfig is the user-supplied, sigil-tolerant form of a Profile. Empty
// fields leave the built-in value untouched.
type ProfileConfig struct {
	Syntax                string            `json:"syntax,omitempty"` // "@", "[", or "#["
	Extensions            []string          `json:"extensions,omitempty"`
	Entrypoint            *EntrypointConfig `json:"entrypoint,omitempty"`
	TestFile              *TestFileConfig   `json:"test_file,omitempty"`
	TestFuncPattern       string            `json:"test_func_pattern,omitempty"`
	TestAnnotations       []string          `json:"test_annotations,omitempty"`
	MethodRootAnnotations []string          `json:"method_root_annotations,omitempty"`
	ClassRootAnnotations  []string          `json:"class_root_annotations,omitempty"`
	FrameworkInterfaces   []string          `json:"framework_interfaces,omitempty"`
}

// EntrypointConfig mirrors entrypointRule for JSON.
type EntrypointConfig struct {
	Names         []string `json:"names,omitempty"`
	Kinds         []string `json:"kinds,omitempty"`
	RequireStatic bool     `json:"require_static,omitempty"`
}

// TestFileConfig mirrors testFileRule for JSON.
type TestFileConfig struct {
	Suffixes     []string `json:"suffixes,omitempty"`
	Prefixes     []string `json:"prefixes,omitempty"`
	PathContains []string `json:"path_contains,omitempty"`
}

// LoadConfig reads <root>/.canopyroots.json. It returns (nil, nil) when the file
// is absent — an unconfigured repo is the common case, not an error.
func LoadConfig(root string) (*Config, error) {
	return LoadConfigFile(filepath.Join(root, ConfigFileName))
}

// LoadConfigFile reads a roots config from an explicit path. A missing file
// yields (nil, nil); a malformed file is a hard error so typos surface loudly.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}

// applyTo merges the config's profiles over an existing registry.
func (c *Config) applyTo(reg *profileRegistry) error {
	if c == nil {
		return nil
	}
	for ext, pc := range c.Profiles {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}

		base := reg.byExt[ext]
		if base == nil {
			base = &Profile{Extensions: []string{ext}, Syntax: syntaxAt}
		} else {
			base = cloneProfile(base) // never mutate the shared default
		}

		if err := mergeProfileConfig(base, pc); err != nil {
			return fmt.Errorf("profile %q: %w", ext, err)
		}

		reg.byExt[ext] = base
		for _, alt := range base.Extensions {
			alt = strings.ToLower(strings.TrimSpace(alt))
			if alt != "" && alt != ext {
				reg.byExt[alt] = base
			}
		}
	}
	return nil
}

func mergeProfileConfig(p *Profile, pc ProfileConfig) error {
	switch pc.Syntax {
	case "":
		// keep existing
	case "@":
		p.Syntax = syntaxAt
	case "[":
		p.Syntax = syntaxBracket
	case "#[":
		p.Syntax = syntaxHashBracket
	default:
		return fmt.Errorf("unknown syntax %q (want \"@\", \"[\", or \"#[\")", pc.Syntax)
	}

	if len(pc.Extensions) > 0 {
		p.Extensions = append(p.Extensions, pc.Extensions...)
	}

	if pc.Entrypoint != nil {
		p.Entrypoint = entrypointRule{
			Names:         pc.Entrypoint.Names,
			Kinds:         pc.Entrypoint.Kinds,
			RequireStatic: pc.Entrypoint.RequireStatic,
		}
	}

	if pc.TestFile != nil {
		p.TestFile.FileSuffixes = append(p.TestFile.FileSuffixes, pc.TestFile.Suffixes...)
		p.TestFile.BasePrefixes = append(p.TestFile.BasePrefixes, pc.TestFile.Prefixes...)
		p.TestFile.PathContains = append(p.TestFile.PathContains, pc.TestFile.PathContains...)
	}

	if pc.TestFuncPattern != "" {
		re, err := regexp.Compile(pc.TestFuncPattern)
		if err != nil {
			return fmt.Errorf("test_func_pattern: %w", err)
		}
		p.TestFuncPattern = re
	}

	p.TestAnnotations = unionAnnotations(p.TestAnnotations, pc.TestAnnotations)
	p.MethodRootAnnotations = unionAnnotations(p.MethodRootAnnotations, pc.MethodRootAnnotations)
	p.ClassRootAnnotations = unionAnnotations(p.ClassRootAnnotations, pc.ClassRootAnnotations)
	p.FrameworkInterfaces = unionAnnotations(p.FrameworkInterfaces, pc.FrameworkInterfaces)
	return nil
}

// unionAnnotations adds sigil-tolerant entries ("@Bean", "Bean", "[Attr]",
// "#[get]") to a set, stripping sigils to the bare name.
func unionAnnotations(set map[string]bool, entries []string) map[string]bool {
	if len(entries) == 0 {
		return set
	}
	if set == nil {
		set = map[string]bool{}
	}
	for _, e := range entries {
		if name := normalizeAnnName(e); name != "" {
			set[name] = true
		}
	}
	return set
}

// normalizeAnnName strips annotation sigils to the bare name: "@Bean" → "Bean",
// "[HttpGet]" → "HttpGet", "#[get]" → "get".
func normalizeAnnName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#[")
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	return strings.TrimSpace(s)
}
