package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BackendSpec describes how to launch a backend LSP.
type BackendSpec struct {
	Command    string
	Args       []string
	Extensions []string
}

// Manager manages backend LSP processes and routes requests.
type Manager struct {
	backends map[string]*Backend
	specs    map[string]BackendSpec
	logger   *slog.Logger
}

// NewManager creates a proxy manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		backends: make(map[string]*Backend),
		specs:    DefaultBackendSpecs(),
		logger:   logger,
	}
}

// Register adds a backend for a language.
func (m *Manager) Register(b *Backend) {
	m.backends[b.Lang] = b
}

// BackendForLang returns the backend for a language, or nil.
func (m *Manager) BackendForLang(lang string) *Backend {
	return m.backends[lang]
}

// BackendForFile returns the backend for a file based on extension.
func (m *Manager) BackendForFile(file string) *Backend {
	lang := langFromExt(file)
	if lang == "" {
		return nil
	}
	return m.backends[lang]
}

// SpawnBackend starts a backend LSP process for the given language.
func (m *Manager) SpawnBackend(lang string, workspaceRoot string) (*Backend, error) {
	spec, ok := m.specs[lang]
	if !ok {
		return nil, fmt.Errorf("no backend spec for language %s", lang)
	}

	binPath, err := exec.LookPath(spec.Command)
	if err != nil {
		return nil, fmt.Errorf("backend %s not found on PATH: %w", spec.Command, err)
	}

	cmd := exec.Command(binPath, spec.Args...)
	cmd.Dir = workspaceRoot

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe for %s: %w", lang, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe for %s: %w", lang, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Command, err)
	}

	b := &Backend{
		Name:    spec.Command,
		Lang:    lang,
		Command: spec.Command,
		Args:    spec.Args,
		stdin:   stdin,
		stdout:  stdout,
		ready:   make(chan struct{}),
		process: cmd,
	}

	// Send initialize request
	initParams := map[string]any{
		"processId":    os.Getpid(),
		"rootUri":      "file://" + workspaceRoot,
		"capabilities": map[string]any{},
	}
	paramsJSON, _ := json.Marshal(initParams)

	go func() {
		// Send initialize request directly (bypass b.Request which waits on b.ready)
		id := b.nextID.Add(1)
		initReq := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "initialize",
			"params":  json.RawMessage(paramsJSON),
		}
		if err := writeLSPMessage(b.stdin, initReq); err != nil {
			m.logger.Error("backend initialize write failed", "lang", lang, "error", err)
			cmd.Process.Kill()
			return
		}
		msg, err := readLSPMessage(b.stdout)
		if err != nil {
			m.logger.Error("backend initialize read failed", "lang", lang, "error", err)
			cmd.Process.Kill()
			return
		}
		if msg.Error != nil {
			m.logger.Error("backend initialize error", "lang", lang, "error", msg.Error.Message)
			cmd.Process.Kill()
			return
		}
		b.Capabilities = msg.Result

		// Send initialized notification directly (bypass b.Notify which waits on b.ready)
		initedMsg := map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialized",
			"params":  json.RawMessage(`{}`),
		}
		_ = writeLSPMessage(b.stdin, initedMsg)

		close(b.ready)
		m.logger.Info("backend ready", "lang", lang, "command", spec.Command)
	}()

	m.backends[lang] = b
	return b, nil
}

// DetectAndSpawn checks which backends are available on PATH and spawns them.
// Returns the number of backends spawned.
func (m *Manager) DetectAndSpawn(workspaceRoot string) int {
	spawned := 0
	for lang, spec := range m.specs {
		if _, exists := m.backends[lang]; exists {
			continue
		}
		_, err := exec.LookPath(spec.Command)
		if err != nil {
			continue
		}
		m.logger.Info("spawning backend LSP", "lang", lang, "command", spec.Command)
		if _, err := m.SpawnBackend(lang, workspaceRoot); err != nil {
			m.logger.Warn("failed to spawn backend", "lang", lang, "error", err)
			continue
		}
		spawned++
	}
	return spawned
}

// Shutdown sends shutdown/exit to all backends.
func (m *Manager) Shutdown() {
	for lang, b := range m.backends {
		m.logger.Info("shutting down backend", "lang", lang)
		_ = b.Notify("shutdown", nil)
		_ = b.Notify("exit", nil)
		if b.stdin != nil {
			b.stdin.Close()
		}
		if b.process != nil && b.process.Process != nil {
			b.process.Process.Kill()
			b.process.Wait()
		}
	}
}

// DefaultBackendSpecs returns the built-in backend specifications.
func DefaultBackendSpecs() map[string]BackendSpec {
	return map[string]BackendSpec{
		"go": {
			Command:    "gopls",
			Args:       []string{"serve"},
			Extensions: []string{".go"},
		},
		"python": {
			Command:    "pyright-langserver",
			Args:       []string{"--stdio"},
			Extensions: []string{".py", ".pyi"},
		},
		"typescript": {
			Command:    "typescript-language-server",
			Args:       []string{"--stdio"},
			Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
		},
		"rust": {
			Command:    "rust-analyzer",
			Extensions: []string{".rs"},
		},
		"c": {
			Command:    "clangd",
			Extensions: []string{".c", ".h", ".cpp", ".hpp", ".cc"},
		},
		"java": {
			Command:    "jdtls",
			Extensions: []string{".java"},
		},
	}
}

func langFromExt(file string) string {
	ext := strings.ToLower(filepath.Ext(file))
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".c", ".h", ".cpp", ".hpp", ".cc":
		return "c"
	case ".java":
		return "java"
	default:
		return ""
	}
}
