package roots_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/canopy/pkg/roots"
	"m31labs.dev/canopy/pkg/xref"
)

// def builds a Definition with minimal required fields.
func def(file, kind, name, sig, receiver string, start, end int) xref.Definition {
	return xref.Definition{
		ID:        file + "\x00" + kind + "\x00" + name,
		File:      file,
		Kind:      kind,
		Name:      name,
		Signature: sig,
		Receiver:  receiver,
		StartLine: start,
		EndLine:   end,
		Callable:  kind == "function_definition" || kind == "method_definition",
	}
}

// writeSrc writes content to dir/relPath (creating parent dirs) and returns the relPath.
func writeSrc(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", relPath, err)
	}
	return relPath
}

// TestGoEntrypoints tests Go language entrypoint detection.
func TestGoEntrypoints(t *testing.T) {
	dir := t.TempDir()
	a := roots.New(dir)

	mainDef := def("main.go", "function_definition", "main", "func main()", "", 1, 5)
	initDef := def("main.go", "function_definition", "init", "func init()", "", 7, 10)
	helperDef := def("main.go", "function_definition", "helper", "func helper()", "", 12, 15)

	defs := []xref.Definition{mainDef, initDef, helperDef}

	t.Run("main is entrypoint", func(t *testing.T) {
		ok, reason := a.IsRoot(mainDef, defs)
		if !ok {
			t.Errorf("expected main to be root, got false")
		}
		if reason != "entrypoint" {
			t.Errorf("expected reason=entrypoint, got %q", reason)
		}
	})

	t.Run("init is entrypoint", func(t *testing.T) {
		ok, reason := a.IsRoot(initDef, defs)
		if !ok {
			t.Errorf("expected init to be root, got false")
		}
		if reason != "entrypoint" {
			t.Errorf("expected reason=entrypoint, got %q", reason)
		}
	})

	t.Run("helper is not root", func(t *testing.T) {
		ok, _ := a.IsRoot(helperDef, defs)
		if ok {
			t.Errorf("expected helper to NOT be root")
		}
	})
}

// TestGoTestFiles tests Go test file detection.
func TestGoTestFiles(t *testing.T) {
	dir := t.TempDir()
	a := roots.New(dir)

	testFuncDef := def("pkg/service_test.go", "function_definition", "TestFoo", "func TestFoo(t *testing.T)", "", 1, 5)
	benchDef := def("pkg/bench_test.go", "function_definition", "BenchmarkBar", "func BenchmarkBar(b *testing.B)", "", 1, 5)
	exampleDef := def("pkg/example_test.go", "function_definition", "ExampleBaz", "func ExampleBaz()", "", 1, 5)
	fuzzDef := def("pkg/fuzz_test.go", "function_definition", "FuzzQux", "func FuzzQux(f *testing.F)", "", 1, 5)

	cases := []struct {
		name   string
		d      xref.Definition
		reason string
	}{
		{"TestXxx", testFuncDef, "test"},
		{"BenchmarkXxx", benchDef, "test"},
		{"ExampleXxx", exampleDef, "test"},
		{"FuzzXxx", fuzzDef, "test"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := a.IsRoot(tc.d, []xref.Definition{tc.d})
			if !ok {
				t.Errorf("expected %s to be root (test), got false", tc.name)
			}
			if reason != tc.reason {
				t.Errorf("expected reason=%s, got %q", tc.reason, reason)
			}
		})
	}
}

// TestJavaAnnotatedMethods tests Java annotation-based root detection.
func TestJavaAnnotatedMethods(t *testing.T) {
	dir := t.TempDir()

	javaFile := "BookmarkController.java"
	src := strings.Join([]string{
		"@RestController",
		"@RequestMapping(\"/api/bookmarks\")",
		"public class BookmarkController {",
		"    @PostMapping",
		"    public ResponseEntity<BookmarkResponse> create() {",
		"        return null;",
		"    }",
		"    @GetMapping",
		"    public List<BookmarkResponse> list() {",
		"        return null;",
		"    }",
		"    public void notAnnotated() {",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "BookmarkController", "@RestController", "RestController", 1, 14)
	createDef := def(javaFile, "method_definition", "create", "@PostMapping", "RestController", 4, 7)
	listDef := def(javaFile, "method_definition", "list", "@GetMapping", "RestController", 8, 11)
	notAnnotatedDef := def(javaFile, "method_definition", "notAnnotated", "public void notAnnotated()", "RestController", 12, 13)

	defs := []xref.Definition{classDef, createDef, listDef, notAnnotatedDef}

	t.Run("PostMapping is root", func(t *testing.T) {
		ok, reason := a.IsRoot(createDef, defs)
		if !ok {
			t.Errorf("expected @PostMapping method to be root")
		}
		if !strings.HasPrefix(reason, "annotation:") {
			t.Errorf("expected annotation: reason, got %q", reason)
		}
	})

	t.Run("GetMapping is root", func(t *testing.T) {
		ok, reason := a.IsRoot(listDef, defs)
		if !ok {
			t.Errorf("expected @GetMapping method to be root")
		}
		if !strings.HasPrefix(reason, "annotation:") {
			t.Errorf("expected annotation: reason, got %q", reason)
		}
	})

	t.Run("method in RestController class is root via class-annotation", func(t *testing.T) {
		ok, reason := a.IsRoot(notAnnotatedDef, defs)
		if !ok {
			t.Errorf("expected method in @RestController to be root via class annotation")
		}
		if !strings.HasPrefix(reason, "class-annotation:") {
			t.Errorf("expected class-annotation: reason, got %q", reason)
		}
	})
}

// TestJavaBean tests @Bean detection.
func TestJavaBean(t *testing.T) {
	dir := t.TempDir()
	javaFile := "SecurityConfig.java"
	src := strings.Join([]string{
		"@Configuration",
		"public class SecurityConfig {",
		"    @Bean",
		"    public SecurityFilterChain securityFilterChain(HttpSecurity http) throws Exception {",
		"        return null;",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "SecurityConfig", "@Configuration", "Configuration", 1, 7)
	beanDef := def(javaFile, "method_definition", "securityFilterChain", "@Bean", "Configuration", 3, 6)
	defs := []xref.Definition{classDef, beanDef}

	ok, reason := a.IsRoot(beanDef, defs)
	if !ok {
		t.Errorf("expected @Bean method to be root")
	}
	if !strings.HasPrefix(reason, "annotation:") {
		t.Errorf("expected annotation: reason, got %q", reason)
	}
}

// TestJavaScheduled tests @Scheduled detection.
func TestJavaScheduled(t *testing.T) {
	dir := t.TempDir()
	javaFile := "LinkCheckerService.java"
	src := strings.Join([]string{
		"@Service",
		"public class LinkCheckerService {",
		"    @Scheduled(fixedRate = 3600000L)",
		"    public void checkAll() {",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "LinkCheckerService", "@Service", "Service", 1, 6)
	scheduledDef := def(javaFile, "method_definition", "checkAll", "@Scheduled(fixedRate = 3600000L)", "Service", 3, 5)
	defs := []xref.Definition{classDef, scheduledDef}

	ok, reason := a.IsRoot(scheduledDef, defs)
	if !ok {
		t.Errorf("expected @Scheduled method to be root")
	}
	if !strings.HasPrefix(reason, "annotation:") {
		t.Errorf("expected annotation: reason, got %q", reason)
	}
}

// TestJavaMain tests Java main method detection.
func TestJavaMain(t *testing.T) {
	dir := t.TempDir()
	javaFile := "LinkrotApplication.java"
	src := strings.Join([]string{
		"@SpringBootApplication",
		"public class LinkrotApplication {",
		"    public static void main(String[] args) {",
		"        SpringApplication.run(LinkrotApplication.class, args);",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "LinkrotApplication", "@SpringBootApplication", "SpringBootApplication", 1, 6)
	mainDef := def(javaFile, "method_definition", "main", "public static void main(String[] args)", "SpringBootApplication", 3, 5)
	defs := []xref.Definition{classDef, mainDef}

	ok, reason := a.IsRoot(mainDef, defs)
	if !ok {
		t.Errorf("expected Java main to be root (entrypoint)")
	}
	if reason != "entrypoint" {
		t.Errorf("expected reason=entrypoint, got %q", reason)
	}
}

// TestJavaRestControllerClassAnnotation tests enclosing class annotation detection.
func TestJavaRestControllerClassAnnotation(t *testing.T) {
	dir := t.TempDir()
	javaFile := "HealthController.java"
	src := strings.Join([]string{
		"@RestController",
		"public class HealthController {",
		"    public String health() {",
		"        return \"ok\";",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "HealthController", "@RestController", "RestController", 1, 6)
	healthDef := def(javaFile, "method_definition", "health", "public String health()", "RestController", 3, 5)
	defs := []xref.Definition{classDef, healthDef}

	ok, reason := a.IsRoot(healthDef, defs)
	if !ok {
		t.Errorf("expected method in @RestController to be root via class annotation")
	}
	if !strings.HasPrefix(reason, "class-annotation:") {
		t.Errorf("expected class-annotation: reason, got %q", reason)
	}
}

// TestJavaJpaRepository tests framework interface root detection.
func TestJavaJpaRepository(t *testing.T) {
	dir := t.TempDir()
	javaFile := "BookmarkRepository.java"
	src := strings.Join([]string{
		"@Repository",
		"public interface BookmarkRepository extends JpaRepository<Bookmark, Long> {",
		"    Optional<Bookmark> findByShortCode(String shortCode);",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	ifaceDef := def(javaFile, "interface_definition", "BookmarkRepository", "@Repository", "", 1, 4)
	findDef := def(javaFile, "method_definition", "findByShortCode", "Optional<Bookmark> findByShortCode(String shortCode)", "", 3, 3)
	defs := []xref.Definition{ifaceDef, findDef}

	ok, reason := a.IsRoot(findDef, defs)
	if !ok {
		t.Errorf("expected JpaRepository method to be root")
	}
	if !strings.HasPrefix(reason, "framework-interface:") {
		t.Errorf("expected framework-interface: reason, got %q", reason)
	}
}

// TestJavaTestAnnotation tests @Test detection in test files.
func TestJavaTestAnnotation(t *testing.T) {
	dir := t.TempDir()
	javaFile := "src/test/java/BookmarkServiceTest.java"
	writeSrc(t, dir, javaFile, strings.Join([]string{
		"@ExtendWith(MockitoExtension.class)",
		"class BookmarkServiceTest {",
		"    @Test",
		"    void create_savesBookmark() {",
		"    }",
		"    @BeforeEach",
		"    void setUp() {",
		"    }",
		"}",
	}, "\n"))

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "BookmarkServiceTest", "@ExtendWith(MockitoExtension.class)", "ExtendWith", 1, 9)
	testDef := def(javaFile, "method_definition", "create_savesBookmark", "@Test", "ExtendWith", 3, 5)
	beforeDef := def(javaFile, "method_definition", "setUp", "@BeforeEach", "ExtendWith", 6, 8)
	defs := []xref.Definition{classDef, testDef, beforeDef}

	t.Run("@Test in test path is test", func(t *testing.T) {
		ok, reason := a.IsRoot(testDef, defs)
		if !ok {
			t.Errorf("expected @Test to be root")
		}
		if reason != "test" {
			t.Errorf("expected reason=test, got %q", reason)
		}
	})

	t.Run("@BeforeEach in test path is test", func(t *testing.T) {
		ok, reason := a.IsRoot(beforeDef, defs)
		if !ok {
			t.Errorf("expected @BeforeEach to be root")
		}
		if reason != "test" {
			t.Errorf("expected reason=test, got %q", reason)
		}
	})
}

// TestJavaPlainUnusedMethod tests that plain unannotated methods are not roots.
func TestJavaPlainUnusedMethod(t *testing.T) {
	dir := t.TempDir()
	javaFile := "LegacyExporter.java"
	src := strings.Join([]string{
		"public class LegacyExporter {",
		"    public String exportCsv(List<Bookmark> bookmarks) {",
		"        return \"\";",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "LegacyExporter", "public class LegacyExporter", "LegacyExporter", 1, 5)
	exportDef := def(javaFile, "method_definition", "exportCsv", "public String exportCsv(List<Bookmark> bookmarks)", "LegacyExporter", 2, 4)
	defs := []xref.Definition{classDef, exportDef}

	ok, _ := a.IsRoot(exportDef, defs)
	if ok {
		t.Errorf("expected plain unannotated method in plain class to NOT be root")
	}
}

// TestJavaExceptionHandler tests @ExceptionHandler detection.
func TestJavaExceptionHandler(t *testing.T) {
	dir := t.TempDir()
	javaFile := "GlobalExceptionHandler.java"
	src := strings.Join([]string{
		"@RestControllerAdvice",
		"public class GlobalExceptionHandler {",
		"    @ExceptionHandler(BookmarkNotFoundException.class)",
		"    public ProblemDetail handleNotFound(BookmarkNotFoundException ex) {",
		"        return null;",
		"    }",
		"}",
	}, "\n")
	writeSrc(t, dir, javaFile, src)

	a := roots.New(dir)

	classDef := def(javaFile, "class_definition", "GlobalExceptionHandler", "@RestControllerAdvice", "RestControllerAdvice", 1, 7)
	handlerDef := def(javaFile, "method_definition", "handleNotFound", "@ExceptionHandler(BookmarkNotFoundException.class)", "RestControllerAdvice", 3, 6)
	defs := []xref.Definition{classDef, handlerDef}

	ok, reason := a.IsRoot(handlerDef, defs)
	if !ok {
		t.Errorf("expected @ExceptionHandler to be root")
	}
	if !strings.HasPrefix(reason, "annotation:") {
		t.Errorf("expected annotation: reason, got %q", reason)
	}
}

// TestPythonRouteDecorators tests Python route decorator detection.
func TestPythonRouteDecorators(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		sig    string
		isRoot bool
	}{
		{"@app.route(\"/\")", true},
		{"@router.get(\"/items\")", true},
		{"@router.post(\"/items\")", true},
		{"@task", true},
		{"@celery.task", true},
		{"def plain_func()", false},
	}

	for _, tc := range cases {
		t.Run(tc.sig, func(t *testing.T) {
			a := roots.New(dir)
			d := def("views.py", "function_definition", "my_func", tc.sig, "", 1, 5)
			defs := []xref.Definition{d}
			ok, _ := a.IsRoot(d, defs)
			if ok != tc.isRoot {
				t.Errorf("IsRoot(%q) = %v, want %v", tc.sig, ok, tc.isRoot)
			}
		})
	}
}

// TestPythonTestFunctions tests Python test function detection.
func TestPythonTestFunctions(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		file   string
		name   string
		isTest bool
	}{
		{"test_views.py", "test_home_page", true},
		{"views_test.py", "test_something", true},
		{"tests/test_api.py", "test_create", true},
		{"views.py", "test_helper", true}, // test_ name prefix counts
		{"views.py", "helper", false},
	}

	for _, tc := range cases {
		t.Run(tc.file+":"+tc.name, func(t *testing.T) {
			a := roots.New(dir)
			d := def(tc.file, "function_definition", tc.name, "def "+tc.name+"()", "", 1, 5)
			defs := []xref.Definition{d}
			ok, reason := a.IsRoot(d, defs)
			if tc.isTest {
				if !ok {
					t.Errorf("expected %s to be root (test)", tc.name)
				}
				if reason != "test" {
					t.Errorf("expected reason=test, got %q", reason)
				}
			} else {
				if ok {
					t.Errorf("expected %s to NOT be root", tc.name)
				}
			}
		})
	}
}

// TestTypeScriptDecorators tests TypeScript/NestJS decorator detection.
func TestTypeScriptDecorators(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		sig    string
		isRoot bool
	}{
		{"@Get('/users')", true},
		{"@Post('/users')", true},
		{"@Delete('/users/:id')", true},
		{"@Injectable()", true},
		{"@Controller('/api')", true},
		{"regularMethod(): void", false},
	}

	for _, tc := range cases {
		t.Run(tc.sig, func(t *testing.T) {
			a := roots.New(dir)
			d := def("users.controller.ts", "method_definition", "findAll", tc.sig, "", 1, 5)
			defs := []xref.Definition{d}
			ok, _ := a.IsRoot(d, defs)
			if ok != tc.isRoot {
				t.Errorf("IsRoot(%q) = %v, want %v", tc.sig, ok, tc.isRoot)
			}
		})
	}
}

// TestDeadConfidence tests the confidence function: visibility is the dominant
// signal, the resolution rate is a downgrade floor.
func TestDeadConfidence(t *testing.T) {
	cases := []struct {
		name string
		rate float64
		file string
		sym  string
		sig  string
		want roots.Confidence
	}{
		// Private/unexported: callers are all in-repo.
		{"go-private-resolved", 0.9, "foo.go", "bar", "func bar()", roots.ConfidenceHigh},
		{"go-private-boundary", 0.5, "foo.go", "bar", "func bar()", roots.ConfidenceHigh},
		{"go-private-poor", 0.2, "foo.go", "bar", "func bar()", roots.ConfidenceMedium},
		// Exported/public: external callers possible — never fully certain.
		{"go-exported-resolved", 0.9, "foo.go", "Bar", "func Bar()", roots.ConfidenceMedium},
		{"go-exported-poor", 0.2, "foo.go", "Bar", "func Bar()", roots.ConfidenceLow},
		{"java-public-poor", 0.1, "Svc.java", "doIt", "public void doIt()", roots.ConfidenceLow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := def(tc.file, "method_definition", tc.sym, tc.sig, "", 1, 5)
			c := roots.DeadConfidence(tc.rate, d)
			if c != tc.want {
				t.Errorf("DeadConfidence(%v, %s/%s) = %s, want %s", tc.rate, tc.file, tc.sym, c, tc.want)
			}
		})
	}
}

// TestExportedRootsOption tests the TreatExportedAsRoots option.
func TestExportedRootsOption(t *testing.T) {
	dir := t.TempDir()
	a := roots.NewWithOptions(dir, roots.Options{TreatExportedAsRoots: true})

	// Java public method in non-test file
	d := def("Service.java", "method_definition", "publicMethod", "public void publicMethod()", "", 1, 5)
	defs := []xref.Definition{d}

	ok, reason := a.IsRoot(d, defs)
	if !ok {
		t.Errorf("expected public method to be root with TreatExportedAsRoots=true")
	}
	if reason != "exported" {
		t.Errorf("expected reason=exported, got %q", reason)
	}
}
