// Package proxy manages backend LSP server processes and routes requests.
package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Backend represents a running backend LSP server process.
type Backend struct {
	Name         string
	Lang         string
	Command      string
	Args         []string
	Capabilities json.RawMessage

	stdin   io.WriteCloser
	stdout  io.Reader
	ready   chan struct{}
	mu      sync.Mutex
	nextID  atomic.Int64
	process *exec.Cmd
}

type lspMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *lspError       `json:"error,omitempty"`
}

type lspError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Request sends a JSON-RPC request and waits for the response.
func (b *Backend) Request(method string, params json.RawMessage) (json.RawMessage, error) {
	<-b.ready
	id := b.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b.mu.Lock()
	err := writeLSPMessage(b.stdin, req)
	b.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write to backend %s: %w", b.Name, err)
	}
	msg, err := readLSPMessage(b.stdout)
	if err != nil {
		return nil, fmt.Errorf("read from backend %s: %w", b.Name, err)
	}
	if msg.Error != nil {
		return nil, fmt.Errorf("backend %s error: %s", b.Name, msg.Error.Message)
	}
	return msg.Result, nil
}

// Notify sends a JSON-RPC notification (no response expected).
func (b *Backend) Notify(method string, params json.RawMessage) error {
	<-b.ready
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return writeLSPMessage(b.stdin, msg)
}

func readLSPMessage(r io.Reader) (lspMessage, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	var contentLen int
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return lspMessage{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLen, _ = strconv.Atoi(val)
		}
	}
	if contentLen == 0 {
		return lspMessage{}, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(br, body); err != nil {
		return lspMessage{}, err
	}
	var msg lspMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return lspMessage{}, err
	}
	return msg, nil
}

func writeLSPMessage(w io.Writer, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
