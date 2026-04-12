package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/odvcencio/canopy/pkg/index"
)

// gopackagesdriver LoadMode flags
const (
	NeedName            = 1 << 0
	NeedFiles           = 1 << 1
	NeedCompiledGoFiles = 1 << 2
	NeedImports         = 1 << 3
	NeedDeps            = 1 << 4
	NeedTypesSizes      = 1 << 9
	NeedModule          = 1 << 13
)

type DriverRequest struct {
	Mode       int               `json:"mode"`
	Env        []string          `json:"env"`
	BuildFlags []string          `json:"build_flags"`
	Tests      bool              `json:"tests"`
	Overlay    map[string][]byte `json:"overlay"`
}

type DriverResponse struct {
	NotHandled bool             `json:"NotHandled,omitempty"`
	Compiler   string           `json:"Compiler,omitempty"`
	Arch       string           `json:"Arch,omitempty"`
	Roots      []string         `json:"Roots,omitempty"`
	Packages   []*DriverPackage `json:"Packages,omitempty"`
}

type DriverPackage struct {
	ID              string            `json:"ID"`
	Name            string            `json:"Name"`
	PkgPath         string            `json:"PkgPath"`
	GoFiles         []string          `json:"GoFiles,omitempty"`
	CompiledGoFiles []string          `json:"CompiledGoFiles,omitempty"`
	Imports         map[string]string `json:"Imports,omitempty"`
	Errors          []DriverError     `json:"Errors,omitempty"`
}

type DriverError struct {
	Pos string `json:"Pos,omitempty"`
	Msg string `json:"Msg"`
}

// HandleDriverRequest processes a gopackagesdriver request.
func HandleDriverRequest(rootDir string, req DriverRequest, patterns []string) (*DriverResponse, error) {
	builder := index.NewBuilder()
	idx, err := builder.BuildPath(rootDir)
	if err != nil {
		return &DriverResponse{NotHandled: true}, nil
	}

	// Group Go files by directory (package)
	pkgMap := make(map[string]*DriverPackage)
	for _, f := range idx.Files {
		if f.Language != "go" {
			continue
		}
		dir := filepath.Dir(f.Path)
		pkg, ok := pkgMap[dir]
		if !ok {
			pkg = &DriverPackage{
				ID:      dir,
				Imports: make(map[string]string),
			}
			pkgMap[dir] = pkg
		}

		absPath := filepath.Join(rootDir, f.Path)
		pkg.GoFiles = append(pkg.GoFiles, absPath)
		pkg.CompiledGoFiles = append(pkg.CompiledGoFiles, absPath)

		// Extract package name from first symbol (package clause is in imports)
		if pkg.Name == "" {
			pkg.Name = packageNameFromFile(absPath)
		}

		for _, imp := range f.Imports {
			imp = strings.Trim(imp, "\"")
			pkg.Imports[imp] = imp
		}
	}

	// Build module path from go.mod if present
	modulePath := readModulePath(rootDir)

	var packages []*DriverPackage
	var roots []string
	for dir, pkg := range pkgMap {
		if modulePath != "" && dir == "." {
			pkg.PkgPath = modulePath
			pkg.ID = modulePath
		} else if modulePath != "" {
			pkg.PkgPath = modulePath + "/" + dir
			pkg.ID = pkg.PkgPath
		}
		packages = append(packages, pkg)
		roots = append(roots, pkg.ID)
	}

	return &DriverResponse{
		Compiler: runtime.Compiler,
		Arch:     runtime.GOARCH,
		Roots:    roots,
		Packages: packages,
	}, nil
}

// RunDriver reads a DriverRequest from stdin and writes a DriverResponse to stdout.
func RunDriver(rootDir string, patterns []string) error {
	var req DriverRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		// If no stdin input, use defaults
		req = DriverRequest{Mode: NeedName | NeedFiles | NeedImports}
	}
	resp, err := HandleDriverRequest(rootDir, req, patterns)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(resp)
}

func packageNameFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Quick extraction: find "package X" in first 1KB
	s := string(data)
	if len(s) > 1024 {
		s = s[:1024]
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

func readModulePath(rootDir string) string {
	data, err := os.ReadFile(filepath.Join(rootDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module ")
		}
	}
	return ""
}
