package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

const serverName = "canopy"
const serverVersion = "0.1.0"
const protocolVersion = "2024-11-05"

type Server struct {
	service *Service
	reader  *bufio.Reader
	writer  io.Writer
	log     io.Writer
	outMu   sync.Mutex
}

func RunStdio(service *Service, in io.Reader, out io.Writer, log io.Writer) error {
	server := &Server{
		service: service,
		reader:  bufio.NewReader(in),
		writer:  out,
		log:     log,
	}
	return server.Run()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content           []toolContent  `json:"content,omitempty"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
	Meta              map[string]any `json:"_meta,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) Run() error {
	for {
		payload, err := readFramedMessage(s.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var request rpcRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			_ = s.sendError(json.RawMessage("null"), -32700, "parse error")
			continue
		}
		if strings.TrimSpace(request.Method) == "" {
			_ = s.sendError(request.ID, -32600, "invalid request: method is required")
			continue
		}

		// Notification path (no ID) except exit, which stops server.
		if len(bytes.TrimSpace(request.ID)) == 0 || string(bytes.TrimSpace(request.ID)) == "null" {
			if request.Method == "exit" {
				return nil
			}
			continue
		}

		if request.Method == "exit" {
			_ = s.sendResult(request.ID, map[string]any{})
			return nil
		}

		result, rpcErr := s.handleRequest(request)
		if rpcErr != nil {
			if err := s.sendError(request.ID, rpcErr.Code, rpcErr.Message); err != nil {
				return err
			}
			continue
		}
		if err := s.sendResult(request.ID, result); err != nil {
			return err
		}
	}
}

func (s *Server) handleRequest(request rpcRequest) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    serverName,
				"version": serverVersion,
			},
		}, nil
	case "initialized":
		return map[string]any{}, nil
	case "shutdown":
		return map[string]any{}, nil
	case "tools/list":
		return toolsListResult{Tools: s.service.Tools()}, nil
	case "tools/call":
		var params toolsCallParams
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		if strings.TrimSpace(params.Name) == "" {
			return nil, &rpcError{Code: -32602, Message: "missing tool name"}
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}

		started := time.Now()
		result, err := s.service.Call(params.Name, params.Arguments)
		durationMs := time.Since(started).Milliseconds()
		meta := map[string]any{
			"tool":        params.Name,
			"duration_ms": durationMs,
		}
		if err != nil {
			meta["ok"] = false
			return toolCallResult{
				IsError: true,
				Content: []toolContent{
					{
						Type: "text",
						Text: err.Error(),
					},
				},
				Meta: meta,
			}, nil
		}

		meta["ok"] = true
		encoded, encodeErr := json.MarshalIndent(result, "", "  ")
		if encodeErr != nil {
			encoded = []byte(`{"error":"failed to encode result"}`)
		}
		return toolCallResult{
			Content: []toolContent{
				{
					Type: "text",
					Text: string(encoded),
				},
			},
			StructuredContent: result,
			Meta:              meta,
		}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", request.Method)}
	}
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (s *Server) sendResult(id json.RawMessage, result any) error {
	return s.sendResponse(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(id json.RawMessage, code int, message string) error {
	return s.sendResponse(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	})
}

func (s *Server) sendResponse(response rpcResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return s.writeFramed(payload)
}

func (s *Server) writeFramed(payload []byte) error {
	s.outMu.Lock()
	defer s.outMu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(s.writer, header); err != nil {
		return err
	}
	_, err := s.writer.Write(payload)
	return err
}

func readFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key != "content-length" {
			continue
		}
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid Content-Length %q", value)
		}
		contentLength = parsed
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
