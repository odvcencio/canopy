package proxy

import (
	"encoding/json"
	"io"
	"testing"
)

func TestBackendSendReceive(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	b := &Backend{
		Name:   "test",
		stdin:  clientWrite,
		stdout: clientRead,
		ready:  make(chan struct{}),
	}
	close(b.ready)

	go func() {
		msg, err := readLSPMessage(serverRead)
		if err != nil {
			return
		}
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(msg.ID),
			"result":  map[string]string{"name": "mock-server"},
		}
		writeLSPMessage(serverWrite, resp)
	}()

	result, err := b.Request("initialize", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Request error: %v", err)
	}

	var res map[string]string
	if err := json.Unmarshal(result, &res); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if res["name"] != "mock-server" {
		t.Errorf("result.name = %q, want mock-server", res["name"])
	}
}

func TestBackendNotify(t *testing.T) {
	_, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	b := &Backend{
		Name:  "test",
		stdin: clientWrite,
		ready: make(chan struct{}),
	}
	close(b.ready)

	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Notify("textDocument/didSave", json.RawMessage(`{"textDocument":{"uri":"file:///test.go"}}`))
	}()

	msg, err := readLSPMessage(serverRead)
	if err != nil {
		t.Fatalf("readLSPMessage: %v", err)
	}
	if msg.Method != "textDocument/didSave" {
		t.Errorf("method = %q, want textDocument/didSave", msg.Method)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	_ = serverWrite
}
