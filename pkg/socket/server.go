// Package socket provides a Unix socket server for CLI client queries
// against the running canopyls LSP server's enriched scope graph.
package socket

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
)

// Request is a CLI client query.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the server's reply.
type Response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Handler processes a socket request.
type Handler func(params json.RawMessage) (any, error)

// Server listens on a Unix socket and dispatches queries to handlers.
type Server struct {
	path     string
	listener net.Listener
	handlers map[string]Handler
	logger   *slog.Logger
	mu       sync.RWMutex
	done     chan struct{}
}

// SocketPath computes the socket path for a workspace root.
func SocketPath(workspaceRoot string) string {
	h := sha256.Sum256([]byte(workspaceRoot))
	return fmt.Sprintf("/tmp/canopyls-%x.sock", h[:8])
}

// NewServer creates a socket server for the given workspace.
func NewServer(workspaceRoot string, logger *slog.Logger) *Server {
	return &Server{
		path:     SocketPath(workspaceRoot),
		handlers: make(map[string]Handler),
		logger:   logger,
		done:     make(chan struct{}),
	}
}

// Handle registers a handler for a method name.
func (s *Server) Handle(method string, h Handler) {
	s.handlers[method] = h
}

// Path returns the socket file path.
func (s *Server) Path() string {
	return s.path
}

// Start begins listening on the Unix socket. Non-blocking — serves in a goroutine.
func (s *Server) Start() error {
	// Remove stale socket
	os.Remove(s.path)

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("socket listen: %w", err)
	}
	s.listener = ln
	s.logger.Info("socket server started", "path", s.path)

	go s.acceptLoop()
	return nil
}

// Stop closes the socket server.
func (s *Server) Stop() {
	close(s.done)
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.path)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.logger.Warn("socket accept error", "error", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	// Allow large responses
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeResponse(conn, Response{Error: "invalid JSON: " + err.Error()})
			continue
		}

		handler, ok := s.handlers[req.Method]
		if !ok {
			writeResponse(conn, Response{Error: "unknown method: " + req.Method})
			continue
		}

		result, err := handler(req.Params)
		if err != nil {
			writeResponse(conn, Response{Error: err.Error()})
		} else {
			writeResponse(conn, Response{Result: result})
		}
	}
}

func writeResponse(conn net.Conn, resp Response) {
	data, _ := json.Marshal(resp)
	conn.Write(data)
	conn.Write([]byte("\n"))
}
