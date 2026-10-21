package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestServer_StdioFraming(t *testing.T) {
	for _, framed := range []bool{false, true} {
		name := "json_lines"
		if framed {
			name = "content_length"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "ledger.go"), []byte("package sample\ntype Ledger struct{}\nfunc (*Ledger) Add() {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			input := new(bytes.Buffer)
			requests := []map[string]any{
				{"jsonrpc": "2.0", "id": 1, "method": "server/discover"},
				{"jsonrpc": "2.0", "id": 2, "method": "initialize"},
				{"jsonrpc": "2.0", "method": "notifications/initialized"},
				{"jsonrpc": "2.0", "id": 3, "method": "ping"},
				{"jsonrpc": "2.0", "id": 4, "method": "tools/list"},
				{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "gts_outline", "arguments": map[string]any{"file": "ledger.go"}}},
			}
			for _, request := range requests {
				if framed {
					appendFramedJSON(t, input, request)
				} else if err := json.NewEncoder(input).Encode(request); err != nil {
					t.Fatal(err)
				}
			}
			output := new(bytes.Buffer)
			if err := RunStdio(NewService(root, ""), input, output, io.Discard); err != nil {
				t.Fatal(err)
			}
			var responses []map[string]any
			if framed {
				for remaining := output.Bytes(); len(remaining) > 0; {
					response, rest := decodeFramedJSON(t, remaining)
					responses = append(responses, response)
					remaining = rest
				}
			} else {
				if bytes.Count(output.Bytes(), []byte{'\n'}) != 5 {
					t.Fatal("responses are not one JSON object per line")
				}
				decoder := json.NewDecoder(output)
				for {
					var response map[string]any
					if err := decoder.Decode(&response); err == io.EOF {
						break
					} else if err != nil {
						t.Fatal(err)
					}
					responses = append(responses, response)
				}
			}
			if len(responses) != 5 {
				t.Fatalf("unexpected responses: %v", responses)
			}
			for i, response := range responses {
				if response["id"] != float64(i+1) || (response["error"] != nil) != (i == 0) {
					t.Fatalf("bad protocol response: %v", response)
				}
			}
			if responses[0]["error"].(map[string]any)["code"] != float64(-32601) {
				t.Fatal("modern probe did not receive method-not-found fallback")
			}
			call := responses[4]["result"].(map[string]any)
			if call["isError"] == true {
				t.Fatalf("outline failed: %v", call)
			}
			outline := call["structuredContent"].(map[string]any)
			if outline["total_symbols"] != float64(2) {
				t.Fatalf("unexpected outline: %v", outline)
			}
			if _, err := os.Stat(filepath.Join(root, ".canopy")); !os.IsNotExist(err) {
				t.Fatal("outline created an index")
			}
			t.Logf("framing=%s responses=5 notification_responses=0 ping_ok=true outline_symbols=2 index_created=false", name)
		})
	}
}

func TestServer_JSONLinesParseErrorRecovery(t *testing.T) {
	input := bytes.NewBufferString(" \n{broken}\n{\"jsonrpc\":\"2.0\",\"id\":8,\"method\":\"ping\"}")
	output := new(bytes.Buffer)
	if err := RunStdio(NewService(t.TempDir(), ""), input, output, io.Discard); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(output)
	var first, second map[string]any
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if first["error"].(map[string]any)["code"] != float64(-32700) || second["id"] != float64(8) || second["error"] != nil {
		t.Fatalf("parse recovery failed: %v %v", first, second)
	}
}
