// Package mcp is the Model Context Protocol server of downmygo: an agent
// that speaks MCP over stdin and stdout can list, add, pause, resume and
// remove downloads, through the app's own local API.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Backend is what the tools do. The app's API client is one.
type Backend interface {
	// Call runs a tool and returns the text to show.
	Call(ctx context.Context, tool string, args map[string]any) (string, error)
}

// Server answers MCP requests.
type Server struct {
	Backend Backend
	Name    string
	Version string

	mu sync.Mutex
}

// JSON-RPC 2.0 messages.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool is one of the tools the server offers.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Tools are what an agent can do.
func Tools() []Tool {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	obj := func(props map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	return []Tool{
		{
			Name:        "downloads_list",
			Description: "List the downloads, with their progress and state.",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "download_add",
			Description: "Add a link to the downloads, and start it unless start is false.",
			InputSchema: obj(map[string]any{
				"url":   str("The http or https link."),
				"name":  str("The file name to save as."),
				"dir":   str("The folder to save in."),
				"queue": str("The queue to join."),
				"start": map[string]any{"type": "boolean", "description": "Start it now."},
			}, "url"),
		},
		{
			Name:        "download_pause",
			Description: "Pause a download, keeping its parts for a resume.",
			InputSchema: obj(map[string]any{"id": str("The download's id.")}, "id"),
		},
		{
			Name:        "download_resume",
			Description: "Resume a paused or failed download.",
			InputSchema: obj(map[string]any{"id": str("The download's id.")}, "id"),
		},
		{
			Name:        "download_remove",
			Description: "Remove a download from the list, and its file when delete_file is true.",
			InputSchema: obj(map[string]any{
				"id":          str("The download's id."),
				"delete_file": map[string]any{"type": "boolean", "description": "Delete the file too."},
			}, "id"),
		},
		{
			Name:        "queues_list",
			Description: "List the queues and whether each is running.",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "options_get",
			Description: "Read the download folder, the connections and the speed limit.",
			InputSchema: obj(map[string]any{}),
		},
	}
}

// Serve reads JSON-RPC messages from r until it ends, writing answers to
// w. Messages are one JSON object per line, as MCP's stdio transport
// asks.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp, reply := s.handle(ctx, req)
		if !reply {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(ctx context.Context, req request) (response, bool) {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	// A notification has no id and takes no answer.
	reply := len(req.ID) > 0

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    orDefault(s.Name, "downmygo"),
				"version": orDefault(s.Version, "1.0.0"),
			},
		}
	case "notifications/initialized", "initialized":
		return resp, false
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": Tools()}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "bad params"}
			break
		}
		text, err := s.Backend.Call(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}
			break
		}
		resp.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("no method %q", req.Method)}
	}
	return resp, reply
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
