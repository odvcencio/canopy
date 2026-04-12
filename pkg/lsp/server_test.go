package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestReadMessage(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	r := strings.NewReader(input)

	msg, err := readMessage(r)
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if msg.Method != "initialize" {
		t.Errorf("expected initialize, got %q", msg.Method)
	}
}

func TestWriteResponse(t *testing.T) {
	var buf bytes.Buffer
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  map[string]string{"name": "canopyls"},
	}
	err := writeMessage(&buf, resp)
	if err != nil {
		t.Fatalf("writeMessage: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Content-Length:") {
		t.Error("missing Content-Length header")
	}
	if !strings.Contains(got, `"name":"canopyls"`) {
		t.Error("missing response body")
	}
}

func TestRoundTrip(t *testing.T) {
	// Simulate client -> server -> client
	body := `{"jsonrpc":"2.0","id":1,"method":"shutdown"}`
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)

	var out bytes.Buffer
	s := NewServer(strings.NewReader(input), &out, io.Discard)
	// Register a handler
	s.Handle("shutdown", func(params json.RawMessage) (any, error) {
		return nil, nil
	})

	err := s.ServeOnce()
	if err != nil && err != io.EOF {
		t.Fatalf("serve: %v", err)
	}
	if !strings.Contains(out.String(), `"result":null`) {
		t.Errorf("expected null result, got: %s", out.String())
	}
}
