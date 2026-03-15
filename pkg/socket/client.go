package socket

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
)

// Client connects to a running gtsls socket server.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

// Dial connects to the gtsls socket for the given workspace.
func Dial(workspaceRoot string) (*Client, error) {
	path := SocketPath(workspaceRoot)
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to gtsls socket %s: %w (is gtsls serve running?)", path, err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	return &Client{conn: conn, scanner: scanner}, nil
}

// Call sends a request and returns the response.
func (c *Client) Call(method string, params any) (json.RawMessage, error) {
	req := Request{Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		req.Params = data
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(append(reqData, '\n')); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from server")
	}

	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("server error: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
