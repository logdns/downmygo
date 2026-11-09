package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIBackend runs the tools through the app's local API.
type APIBackend struct {
	BaseURL string // "http://127.0.0.1:8463"
	Token   string
	Client  *http.Client
}

// NewAPIBackend talks to the API of the app at port.
func NewAPIBackend(port int, token string) *APIBackend {
	return &APIBackend{
		BaseURL: "http://127.0.0.1:" + strconv.Itoa(port),
		Token:   token,
		Client:  &http.Client{Timeout: 20 * time.Second},
	}
}

// Call runs one tool.
func (b *APIBackend) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	switch tool {
	case "downloads_list":
		var body struct {
			Downloads []struct {
				ID      string  `json:"id"`
				Name    string  `json:"name"`
				Size    int64   `json:"size"`
				Done    int64   `json:"done"`
				Percent float64 `json:"percent"`
				Speed   float64 `json:"speed"`
				Status  string  `json:"status"`
				State   string  `json:"statusText"`
				Error   string  `json:"error"`
				Dir     string  `json:"dir"`
			} `json:"downloads"`
			Speed float64 `json:"speed"`
		}
		if err := b.get(ctx, "/api/downloads", &body); err != nil {
			return "", err
		}
		if len(body.Downloads) == 0 {
			return "No downloads.", nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d downloads, %.1f MB/s in all\n", len(body.Downloads), body.Speed/(1<<20))
		for _, d := range body.Downloads {
			state := d.State
			if state == "" {
				state = d.Status
			}
			progress := "—"
			if d.Percent >= 0 {
				progress = strconv.Itoa(int(d.Percent*100)) + "%"
			}
			fmt.Fprintf(&sb, "- %s  %s  %s  %s  %s  %s\n",
				d.ID, d.Name, progress, state, humanBytes(d.Done), d.Dir)
			if d.Error != "" {
				fmt.Fprintf(&sb, "  error: %s\n", d.Error)
			}
		}
		return sb.String(), nil
	case "download_add":
		raw, _ := args["url"].(string)
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("url is required")
		}
		body := map[string]any{"url": raw}
		for _, key := range []string{"name", "dir", "queue"} {
			if v, ok := args[key].(string); ok && v != "" {
				body[key] = v
			}
		}
		if v, ok := args["start"].(bool); ok {
			body["start"] = v
		}
		var out struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := b.post(ctx, "/api/add", body, &out); err != nil {
			return "", err
		}
		return fmt.Sprintf("Added %s (%s).", out.Name, out.ID), nil
	case "download_pause", "download_resume":
		id, _ := args["id"].(string)
		if id == "" {
			return "", fmt.Errorf("id is required")
		}
		action := strings.TrimPrefix(tool, "download_")
		if err := b.post(ctx, "/api/"+action+"?id="+url.QueryEscape(id), nil, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: %s.", capital(action), id), nil
	case "download_remove":
		id, _ := args["id"].(string)
		if id == "" {
			return "", fmt.Errorf("id is required")
		}
		del := "0"
		if v, ok := args["delete_file"].(bool); ok && v {
			del = "1"
		}
		if err := b.post(ctx, "/api/remove?id="+url.QueryEscape(id)+"&delete="+del, nil, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed %s.", id), nil
	case "queues_list":
		var queues []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Running bool   `json:"running"`
			Count   int    `json:"count"`
		}
		if err := b.get(ctx, "/api/queues", &queues); err != nil {
			return "", err
		}
		var sb strings.Builder
		for _, q := range queues {
			state := "stopped"
			if q.Running {
				state = "running"
			}
			fmt.Fprintf(&sb, "- %s (%s): %s, %d downloads\n", q.Name, q.ID, state, q.Count)
		}
		return sb.String(), nil
	case "options_get":
		var out map[string]any
		if err := b.get(ctx, "/api/options", &out); err != nil {
			return "", err
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		return string(body), nil
	}
	return "", fmt.Errorf("no tool %q", tool)
}

func (b *APIBackend) get(ctx context.Context, path string, dst any) error {
	return b.do(ctx, http.MethodGet, path, nil, dst)
}

func (b *APIBackend) post(ctx context.Context, path string, body, dst any) error {
	return b.do(ctx, http.MethodPost, path, body, dst)
}

func (b *APIBackend) do(ctx context.Context, method, path string, body, dst any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("the app is not answering: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("the app refused the token: is the local API on, and the token right?")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", strings.TrimSpace(string(payload)))
	}
	if dst == nil {
		return nil
	}
	return json.Unmarshal(payload, dst)
}

// capital writes a word with its first letter up, as "Paused: a1b2".
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
