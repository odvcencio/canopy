package typemetrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

func TestStructFields(t *testing.T) {
	dir := t.TempDir()
	src := `package main

type Config struct {
	Host     string
	Port     int
	Debug    bool
	Timeout  int
	MaxConns int
}
`
	path := filepath.Join(dir, "config.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "type_definition",
						Name:      "Config",
						Signature: "type Config struct",
						StartLine: 3,
						EndLine:   9,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, xref.Graph{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(report.Types))
	}

	tm := report.Types[0]
	if tm.Name != "Config" {
		t.Errorf("expected name 'Config', got %q", tm.Name)
	}
	if tm.Kind != "struct" {
		t.Errorf("expected kind 'struct', got %q", tm.Kind)
	}
	if tm.Fields != 5 {
		t.Errorf("expected Fields=5, got %d", tm.Fields)
	}
	if tm.InterfaceWidth != 0 {
		t.Errorf("expected InterfaceWidth=0 for struct, got %d", tm.InterfaceWidth)
	}
}

func TestInterfaceWidth(t *testing.T) {
	dir := t.TempDir()
	src := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
	Reset()
}
`
	path := filepath.Join(dir, "reader.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "type_definition",
						Name:      "Reader",
						Signature: "type Reader interface",
						StartLine: 3,
						EndLine:   7,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, xref.Graph{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(report.Types))
	}

	tm := report.Types[0]
	if tm.Kind != "interface" {
		t.Errorf("expected kind 'interface', got %q", tm.Kind)
	}
	if tm.InterfaceWidth != 3 {
		t.Errorf("expected InterfaceWidth=3, got %d", tm.InterfaceWidth)
	}
}

func TestMethodSetSize(t *testing.T) {
	dir := t.TempDir()
	src := `package main

type Server struct {
	addr string
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {
}
`
	path := filepath.Join(dir, "server.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	// Use the real index builder to get proper symbols with receiver info.
	builder := index.NewBuilder()
	idx, err := builder.BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath failed: %v", err)
	}

	graph, err := xref.Build(idx)
	if err != nil {
		t.Fatalf("xref.Build failed: %v", err)
	}

	report, err := Analyze(idx, dir, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the Server type.
	var serverType *TypeMetrics
	for i := range report.Types {
		if report.Types[i].Name == "Server" {
			serverType = &report.Types[i]
			break
		}
	}
	if serverType == nil {
		t.Fatal("Server type not found in report")
	}

	if serverType.MethodSetSize != 2 {
		t.Errorf("expected MethodSetSize=2, got %d", serverType.MethodSetSize)
	}
}

func TestNestingDepth(t *testing.T) {
	dir := t.TempDir()
	src := `package main

type Outer struct {
	Inner struct {
		Value int
	}
}
`
	path := filepath.Join(dir, "nested.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "type_definition",
						Name:      "Outer",
						Signature: "type Outer struct",
						StartLine: 3,
						EndLine:   7,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, xref.Graph{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(report.Types))
	}

	tm := report.Types[0]
	if tm.NestingDepth < 1 {
		t.Errorf("expected NestingDepth >= 1, got %d", tm.NestingDepth)
	}
}

func TestEmptyIndex(t *testing.T) {
	// Nil index.
	report, err := Analyze(nil, "", xref.Graph{})
	if err != nil {
		t.Fatalf("unexpected error on nil index: %v", err)
	}
	if len(report.Types) != 0 {
		t.Fatalf("expected 0 types for nil index, got %d", len(report.Types))
	}
	if report.Summary.Count != 0 {
		t.Fatalf("expected summary count 0 for nil index, got %d", report.Summary.Count)
	}

	// Empty index.
	emptyIdx := &model.Index{
		Version:     "1",
		Root:        t.TempDir(),
		GeneratedAt: time.Now(),
		Files:       nil,
	}
	report, err = Analyze(emptyIdx, emptyIdx.Root, xref.Graph{})
	if err != nil {
		t.Fatalf("unexpected error on empty index: %v", err)
	}
	if len(report.Types) != 0 {
		t.Fatalf("expected 0 types for empty index, got %d", len(report.Types))
	}
}

func TestSummary(t *testing.T) {
	dir := t.TempDir()
	src := `package main

type Small struct {
	A int
	B string
}

type Big struct {
	A int
	B int
	C int
	D int
	E int
	F int
}

type Handler interface {
	Handle()
	Close()
}
`
	path := filepath.Join(dir, "types.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{
		Version:     "1",
		Root:        dir,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{
			{
				Path:     path,
				Language: "go",
				Symbols: []model.Symbol{
					{
						File:      path,
						Kind:      "type_definition",
						Name:      "Small",
						Signature: "type Small struct",
						StartLine: 3,
						EndLine:   6,
					},
					{
						File:      path,
						Kind:      "type_definition",
						Name:      "Big",
						Signature: "type Big struct",
						StartLine: 8,
						EndLine:   15,
					},
					{
						File:      path,
						Kind:      "type_definition",
						Name:      "Handler",
						Signature: "type Handler interface",
						StartLine: 17,
						EndLine:   20,
					},
				},
			},
		},
	}

	report, err := Analyze(idx, dir, xref.Graph{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Summary.Count != 3 {
		t.Fatalf("expected summary count=3, got %d", report.Summary.Count)
	}

	// Big has 6 fields, Small has 2 → max=6, avg=(2+6+0)/3 = 2.666...
	if report.Summary.MaxFields != 6 {
		t.Errorf("expected MaxFields=6, got %d", report.Summary.MaxFields)
	}
	if report.Summary.AvgFields < 2.0 || report.Summary.AvgFields > 3.0 {
		t.Errorf("expected AvgFields around 2.67, got %f", report.Summary.AvgFields)
	}

	// Handler has 2 interface methods → max=2, avg=2/3=0.666...
	if report.Summary.MaxInterfaceWidth != 2 {
		t.Errorf("expected MaxInterfaceWidth=2, got %d", report.Summary.MaxInterfaceWidth)
	}
	if report.Summary.AvgInterfaceWidth < 0.5 || report.Summary.AvgInterfaceWidth > 1.0 {
		t.Errorf("expected AvgInterfaceWidth around 0.67, got %f", report.Summary.AvgInterfaceWidth)
	}
}

func TestExtractReceiverType(t *testing.T) {
	tests := []struct {
		receiver string
		want     string
	}{
		{"s *Server", "Server"},
		{"s Server", "Server"},
		{"*Server", "Server"},
		{"Server", "Server"},
		{"", ""},
		{" s  *MyType ", "MyType"},
	}
	for _, tc := range tests {
		got := extractReceiverType(tc.receiver)
		if got != tc.want {
			t.Errorf("extractReceiverType(%q) = %q, want %q", tc.receiver, got, tc.want)
		}
	}
}
