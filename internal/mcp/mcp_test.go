package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct{ last string }

func (f *fakeBackend) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	f.last = tool
	if tool == "download_add" {
		if v, _ := args["url"].(string); v == "" {
			return "", fmt.Errorf("url is required")
		}
	}
	return "did " + tool, nil
}

// call sends one request and returns the answer.
func call(t *testing.T, s *Server, body string) map[string]any {
	t.Helper()
	var out, in bytes.Buffer
	in.WriteString(body + "\n")
	if err := s.Serve(context.Background(), &in, &out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("answer %q: %v", out.String(), err)
	}
	return resp
}

func TestInitializeAndTools(t *testing.T) {
	s := &Server{Backend: &fakeBackend{}, Name: "downmygo", Version: "1.0.0"}
	resp := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	result, _ := resp["result"].(map[string]any)
	if result == nil || result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("resp = %v", resp)
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "downmygo" {
		t.Fatalf("serverInfo = %v", info)
	}

	resp = call(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(Tools()) {
		t.Fatalf("tools = %d", len(tools))
	}
	first := tools[0].(map[string]any)
	if first["name"] == "" || first["inputSchema"] == nil {
		t.Fatalf("tool = %v", first)
	}
}

func TestToolsCall(t *testing.T) {
	b := &fakeBackend{}
	s := &Server{Backend: b}
	resp := call(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"downloads_list","arguments":{}}}`)
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)[0].(map[string]any)
	if content["text"] != "did downloads_list" || b.last != "downloads_list" {
		t.Fatalf("result = %v", resp)
	}

	// A tool that fails says so, and the agent sees the reason.
	resp = call(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"download_add","arguments":{}}}`)
	result = resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("result = %v", resp)
	}
}

func TestUnknownMethodAndNotifications(t *testing.T) {
	s := &Server{Backend: &fakeBackend{}}
	resp := call(t, s, `{"jsonrpc":"2.0","id":5,"method":"nope"}`)
	if resp["error"] == nil {
		t.Fatalf("resp = %v", resp)
	}
	// A notification takes no answer: nothing is written at all.
	var out, in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	if err := s.Serve(context.Background(), &in, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("answered a notification: %q", out.String())
	}
}

func TestAPIBackend(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/downloads":
			_, _ = w.Write([]byte(`{"downloads":[{"id":"a","name":"a.zip","size":100,"done":50,"percent":0.5,"status":"Downloading","statusText":"Downloading","dir":"/tmp"}],"speed":1024}`))
		case "/api/add":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"id":"b","name":"b.zip"}`))
		case "/api/pause":
			if r.URL.Query().Get("id") != "a" {
				t.Errorf("id = %q", r.URL.Query().Get("id"))
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := &APIBackend{BaseURL: srv.URL, Token: "tok", Client: srv.Client()}
	ctx := context.Background()

	text, err := b.Call(ctx, "downloads_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "a.zip") || !strings.Contains(text, "50%") {
		t.Fatalf("text = %q", text)
	}
	if gotToken != "Bearer tok" {
		t.Fatalf("token = %q", gotToken)
	}

	text, err = b.Call(ctx, "download_add", map[string]any{"url": "https://x/b.zip"})
	if err != nil || !strings.Contains(text, "b.zip") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := b.Call(ctx, "download_add", map[string]any{}); err == nil {
		t.Fatal("an empty url was accepted")
	}

	if _, err := b.Call(ctx, "download_pause", map[string]any{"id": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Call(ctx, "nope", nil); err == nil {
		t.Fatal("an unknown tool ran")
	}
}

func TestAPIBackendSaysWhenTheAppIsClosed(t *testing.T) {
	b := NewAPIBackend(1, "tok")
	if _, err := b.Call(context.Background(), "downloads_list", nil); err == nil {
		t.Fatal("a closed app answered")
	}
}
