package roots

import (
	"maps"
	"regexp"
	"strings"
)

// annotationSyntax describes how annotations are written in a language.
type annotationSyntax string

const (
	syntaxAt          annotationSyntax = "@"  // Java, Kotlin, Python, TypeScript, JavaScript
	syntaxBracket     annotationSyntax = "["  // C#
	syntaxHashBracket annotationSyntax = "#[" // Rust, PHP 8 attributes
)

// entrypointRule describes what makes a callable a program entrypoint.
type entrypointRule struct {
	// Names that are unconditional entrypoints for the given kinds.
	Names []string
	// Kinds that qualify. Empty means any callable.
	Kinds []string
	// If true, the Signature must contain "static" (for Java main).
	RequireStatic bool
}

// testFileRule describes how to detect test source files.
type testFileRule struct {
	// Suffixes of the base filename (e.g. "_test.go").
	FileSuffixes []string
	// Prefixes of the base filename (e.g. "test_" for Python).
	BasePrefixes []string
	// Path segment that indicates a test tree (e.g. "/src/test/", "/tests/").
	PathContains []string
}

// Profile holds language-specific reachability knowledge.
type Profile struct {
	// Extensions this profile applies to (lower-case, with dot: ".go", ".java").
	Extensions []string

	// Annotation/decorator syntax prefix.
	Syntax annotationSyntax

	// Entrypoint detection.
	Entrypoint entrypointRule

	// Test source file detection.
	TestFile testFileRule

	// Test function name prefixes/patterns (regex).
	TestFuncPattern *regexp.Regexp

	// Annotation names on the callable itself that make it a root.
	// Stored without sigil: "Test", "Bean", "GetMapping", …
	MethodRootAnnotations map[string]bool

	// Test lifecycle annotation names (without sigil).
	TestAnnotations map[string]bool

	// Class-level annotation names — if the enclosing class/interface carries
	// one of these, every callable inside it is a reachability root.
	ClassRootAnnotations map[string]bool

	// Framework interface names — if a method_definition lives inside an
	// interface_definition whose source declaration line contains
	// "extends"/"implements" and one of these names, the method is a root.
	FrameworkInterfaces map[string]bool
}

// strSet is a convenience constructor for a set from a space-separated list.
func strSet(words string) map[string]bool {
	m := map[string]bool{}
	for w := range strings.FieldsSeq(words) {
		if w != "" {
			m[w] = true
		}
	}
	return m
}

var defaultProfiles = map[string]*Profile{
	".go": {
		Extensions: []string{".go"},
		Syntax:     syntaxAt,
		Entrypoint: entrypointRule{
			Names: []string{"main", "init"},
			Kinds: []string{"function_definition"},
		},
		TestFile: testFileRule{
			FileSuffixes: []string{"_test.go"},
		},
		TestFuncPattern: regexp.MustCompile(`^(Test|Benchmark|Example|Fuzz)`),
	},
	".java": {
		Extensions: []string{".java"},
		Syntax:     syntaxAt,
		Entrypoint: entrypointRule{
			Names:         []string{"main"},
			Kinds:         []string{"method_definition"},
			RequireStatic: true,
		},
		TestFile: testFileRule{
			PathContains: []string{"/src/test/"},
			FileSuffixes: []string{"Test.java", "Tests.java", "IT.java"},
		},
		TestAnnotations: strSet(
			"Test ParameterizedTest RepeatedTest BeforeEach AfterEach BeforeAll AfterAll TestFactory",
		),
		MethodRootAnnotations: strSet(
			"GetMapping PostMapping PutMapping DeleteMapping PatchMapping RequestMapping " +
				"Bean Scheduled EventListener PostConstruct PreDestroy " +
				"KafkaListener MessageMapping RabbitListener Async " +
				"ExceptionHandler InitBinder ModelAttribute Override",
		),
		ClassRootAnnotations: strSet(
			"RestController Controller Component Service Repository Configuration " +
				"ControllerAdvice RestControllerAdvice SpringBootApplication",
		),
		FrameworkInterfaces: strSet(
			"JpaRepository CrudRepository PagingAndSortingRepository MongoRepository Repository",
		),
	},
	".py": {
		Extensions: []string{".py"},
		Syntax:     syntaxAt,
		Entrypoint: entrypointRule{
			Names: []string{"main"},
			Kinds: []string{"function_definition"},
		},
		TestFile: testFileRule{
			FileSuffixes: []string{"_test.py"},
			BasePrefixes: []string{"test_"},
			PathContains: []string{"/tests/", "/test/"},
		},
		TestFuncPattern: regexp.MustCompile(`^test_`),
		TestAnnotations: strSet("pytest.fixture fixture"),
		// Python decorators: prefix-matched (e.g. "app" matches @app.route, @app.get, …).
		// We store the head token of the dotted name; single-token decorators stored as-is.
		MethodRootAnnotations: strSet(
			"app router task celery receiver",
		),
	},
	".ts": {
		Extensions: []string{".ts", ".tsx"},
		Syntax:     syntaxAt,
		TestFile: testFileRule{
			FileSuffixes: []string{".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx"},
			PathContains: []string{"__tests__/"},
		},
		MethodRootAnnotations: strSet(
			"Get Post Put Delete Patch Injectable Controller Component " +
				"EventPattern MessagePattern Cron Module",
		),
	},
	".js": {
		Extensions: []string{".js", ".jsx"},
		Syntax:     syntaxAt,
		TestFile: testFileRule{
			FileSuffixes: []string{".test.js", ".spec.js", ".test.jsx", ".spec.jsx"},
			PathContains: []string{"__tests__/"},
		},
		MethodRootAnnotations: strSet(
			"Get Post Put Delete Patch Injectable Controller Component " +
				"EventPattern MessagePattern Cron Module",
		),
	},
	".rs": {
		Extensions: []string{".rs"},
		Syntax:     syntaxHashBracket,
		Entrypoint: entrypointRule{
			Names: []string{"main"},
			Kinds: []string{"function_definition"},
		},
		TestFile: testFileRule{
			PathContains: []string{"/tests/"},
		},
		// #[test], #[tokio::test], #[async_std::test] (head tokens tokio/async_std
		// also match, but the test check runs first so they classify as test).
		TestAnnotations: strSet("test tokio::test async_std::test"),
		// Web-framework route macros (axum/actix/rocket), async-runtime entry
		// macros, and FFI export markers. Head tokens (tokio, actix_web) match
		// path attributes like #[tokio::main].
		MethodRootAnnotations: strSet(
			"get post put delete patch head options " +
				"main tokio actix_web async_std rocket " +
				"no_mangle export_name",
		),
	},
	".cs": {
		Extensions: []string{".cs"},
		Syntax:     syntaxBracket,
		Entrypoint: entrypointRule{
			Names: []string{"Main"},
			Kinds: []string{"method_definition"},
		},
		TestAnnotations: strSet("Test Fact Theory TestMethod"),
		MethodRootAnnotations: strSet(
			"HttpGet HttpPost HttpPut HttpDelete Route ApiController",
		),
	},
}

// profileRegistry maps each file extension to a Profile.
type profileRegistry struct {
	byExt map[string]*Profile
}

// newRegistry builds a registry from the default profiles, optionally merging
// extra root annotations.
func newRegistry(extraAnnotations []string) *profileRegistry {
	reg := &profileRegistry{byExt: map[string]*Profile{}}
	for ext, p := range defaultProfiles {
		clone := cloneProfile(p)
		for _, ann := range extraAnnotations {
			name := normalizeAnnName(ann)
			if name == "" {
				continue
			}
			if clone.MethodRootAnnotations == nil {
				clone.MethodRootAnnotations = map[string]bool{}
			}
			clone.MethodRootAnnotations[name] = true
		}
		reg.byExt[ext] = clone
		for _, altExt := range clone.Extensions {
			if altExt != ext {
				reg.byExt[altExt] = clone
			}
		}
	}
	return reg
}

// lookupByFile returns the Profile for a given file path, or nil if unknown.
func (r *profileRegistry) lookupByFile(filePath string) *Profile {
	// Walk suffixes: first try the full double-extension (.test.ts), then single.
	base := strings.ToLower(filePath)
	// Try longest match first (up to two dots from the end).
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		ext := base[idx:]
		if p := r.byExt[ext]; p != nil {
			return p
		}
		// Try one more level (e.g. ".test.ts" -> ".ts" already handled; ".tsx" -> ".tsx").
		if idx2 := strings.LastIndex(base[:idx], "."); idx2 >= 0 {
			ext2 := base[idx2:]
			if p := r.byExt[ext2]; p != nil {
				return p
			}
		}
	}
	return nil
}

func cloneProfile(p *Profile) *Profile {
	c := *p
	c.MethodRootAnnotations = cloneSet(p.MethodRootAnnotations)
	c.TestAnnotations = cloneSet(p.TestAnnotations)
	c.ClassRootAnnotations = cloneSet(p.ClassRootAnnotations)
	c.FrameworkInterfaces = cloneSet(p.FrameworkInterfaces)
	return &c
}

func cloneSet(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	out := make(map[string]bool, len(m))
	maps.Copy(out, m)
	return out
}
