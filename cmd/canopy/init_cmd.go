package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gotreesitter/grammars"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize canopy config files for a project",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runInit,
	}
	cmd.AddCommand(newInitCICmd())
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	target := "."
	if len(args) == 1 {
		target = args[0]
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	var created []string

	// 1. Detect languages present.
	langs := detectLanguages(abs)
	if len(langs) > 0 {
		fmt.Printf("init: detected languages: %s\n", strings.Join(langs, ", "))
	}

	// 2. Generate .canopyignore with sensible defaults.
	ignorePath := filepath.Join(abs, ".canopyignore")
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		if writeErr := writeGtsIgnore(ignorePath); writeErr != nil {
			return fmt.Errorf("writing .canopyignore: %w", writeErr)
		}
		created = append(created, ".canopyignore")
	} else {
		fmt.Println("init: .canopyignore already exists, skipping")
	}

	// 3. Generate .canopygenerated skeleton if generated files likely present.
	generatedPath := filepath.Join(abs, ".canopygenerated")
	if _, err := os.Stat(generatedPath); os.IsNotExist(err) {
		if hasGeneratedPatterns(abs) {
			if writeErr := writeGtsGenerated(generatedPath); writeErr != nil {
				return fmt.Errorf("writing .canopygenerated: %w", writeErr)
			}
			created = append(created, ".canopygenerated")
		}
	} else {
		fmt.Println("init: .canopygenerated already exists, skipping")
	}

	// 4. Generate .canopyboundaries skeleton from package structure.
	boundariesPath := filepath.Join(abs, ".canopyboundaries")
	if _, err := os.Stat(boundariesPath); os.IsNotExist(err) {
		pkgs := detectPackages(abs)
		if len(pkgs) > 1 {
			if writeErr := writeGtsBoundaries(boundariesPath, pkgs); writeErr != nil {
				return fmt.Errorf("writing .canopyboundaries: %w", writeErr)
			}
			created = append(created, ".canopyboundaries")
		}
	} else {
		fmt.Println("init: .canopyboundaries already exists, skipping")
	}

	// 5. Print summary.
	if len(created) == 0 {
		fmt.Println("init: nothing to generate (all config files already exist)")
	} else {
		fmt.Printf("init: created %s\n", strings.Join(created, ", "))
	}
	return nil
}

// detectLanguages scans file extensions in a directory tree and matches them
// against registered grammars to determine which languages are present.
func detectLanguages(root string) []string {
	extSet := make(map[string]bool)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(name); ext != "" {
			extSet[ext] = true
		}
		return nil
	})

	// Build extension-to-language lookup from gotreesitter grammars.
	extToLang := make(map[string]string)
	for _, entry := range grammars.AllLanguages() {
		for _, ext := range entry.Extensions {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extToLang[ext] = entry.Name
		}
	}

	langSet := make(map[string]bool)
	for ext := range extSet {
		if lang, ok := extToLang[ext]; ok {
			langSet[lang] = true
		}
	}

	langs := make([]string, 0, len(langSet))
	for l := range langSet {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

func writeGtsIgnore(path string) error {
	content := `# canopy ignore patterns (gitignore syntax)
vendor/
node_modules/
.git/
build/
dist/
.canopy/
`
	return os.WriteFile(path, []byte(content), 0644)
}

// hasGeneratedPatterns checks for common generated file indicators.
func hasGeneratedPatterns(root string) bool {
	patterns := []string{
		"*.pb.go",
		"*_generated.go",
		"*.gen.go",
		"*_gen.go",
		"generated_*.go",
		"*_mock.go",
		"mock_*.go",
		"*.generated.ts",
		"*.g.dart",
	}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, "**", pat))
		if len(matches) > 0 {
			return true
		}
		// Also check root level.
		matches, _ = filepath.Glob(filepath.Join(root, pat))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

func writeGtsGenerated(path string) error {
	content := `# canopy generated file patterns
# Syntax: generator_name: glob_pattern
# Lines without a colon use generator "config"

# protobuf: *.pb.go
# protobuf: *_grpc.pb.go
# mockgen: mock_*.go
# mockgen: *_mock.go
# sqlc: *.sql.go
`
	return os.WriteFile(path, []byte(content), 0644)
}

// detectPackages finds top-level directories that contain source files.
func detectPackages(root string) []string {
	pkgSet := make(map[string]bool)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
			continue
		}
		// Check if the directory contains source files.
		subEntries, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() && hasSourceExtension(se.Name()) {
				pkgSet[name] = true
				break
			}
		}
	}

	pkgs := make([]string, 0, len(pkgSet))
	for p := range pkgSet {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return pkgs
}

func hasSourceExtension(name string) bool {
	ext := filepath.Ext(name)
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java", ".c", ".cpp", ".h", ".rb", ".swift", ".kt":
		return true
	}
	return false
}

func writeGtsBoundaries(path string, pkgs []string) error {
	var b strings.Builder
	b.WriteString("# canopy boundary rules\n")
	b.WriteString("# Syntax: module <path-glob> allow|deny <target-globs...>\n")
	b.WriteString("#\n")
	b.WriteString("# Examples:\n")
	b.WriteString("#   module pkg/model allow *              # model can import anything\n")
	b.WriteString("#   module internal/* deny pkg/model      # internal cannot import model\n")
	b.WriteString("#\n")
	for _, pkg := range pkgs {
		fmt.Fprintf(&b, "# module %s/* allow *\n", pkg)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// --- canopy init ci ---

func newInitCICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Generate GitHub Actions workflow for canopy analysis",
		Args:  cobra.NoArgs,
		RunE:  runInitCI,
	}
	return cmd
}

func runInitCI(cmd *cobra.Command, args []string) error {
	target := "."
	// Inherit path from parent if invoked as 'canopy init ci' with path on parent.
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	workflowDir := filepath.Join(abs, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("creating workflow directory: %w", err)
	}

	workflowPath := filepath.Join(workflowDir, "canopy-check.yml")
	if _, err := os.Stat(workflowPath); err == nil {
		fmt.Println("init ci: .github/workflows/canopy-check.yml already exists, skipping")
		return nil
	}

	content := `name: Canopy Analysis
on: [pull_request]
jobs:
  canopy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: stable
      - name: Install canopy
        run: go install github.com/odvcencio/canopy/cmd/gts@latest
      - name: Build index
        run: canopy index build .
      - name: Run quality checks
        run: canopy analyze check --base origin/${{ github.base_ref }} --format sarif > results.sarif
      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v3
        if: always()
        with:
          sarif_file: results.sarif
`
	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing workflow: %w", err)
	}
	fmt.Println("init ci: created .github/workflows/canopy-check.yml")
	return nil
}
