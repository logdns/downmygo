package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/downmygo/downmygo/internal/grab"
	"github.com/downmygo/downmygo/internal/mcp"
	"github.com/downmygo/downmygo/internal/nativehost"
	"github.com/downmygo/downmygo/internal/store"
	"github.com/downmygo/downmygo/internal/tools"
	"github.com/downmygo/downmygo/internal/version"
)

// appState reads the app's state, for the commands that talk to it.
func appState() (*store.State, error) {
	dir, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	return st.Load()
}

// backend is the app's API, as the commands reach it.
func backend() (*mcp.APIBackend, error) {
	st, err := appState()
	if err != nil {
		return nil, err
	}
	port := st.Settings.RemotePort
	if port == 0 {
		port = 8463
	}
	if st.Settings.APIToken == "" {
		return nil, fmt.Errorf("the app has no API token yet: start downmygo once")
	}
	return mcp.NewAPIBackend(port, st.Settings.APIToken), nil
}

// runMCP serves the Model Context Protocol on stdin and stdout.
func runMCP() int {
	b, err := backend()
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	s := &mcp.Server{Backend: b, Name: "downmygo", Version: version.Version}
	if err := s.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	return 0
}

// runNativeHost serves the browser's native messaging on stdin and stdout.
func runNativeHost() int {
	st, err := appState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	port := st.Settings.RemotePort
	if port == 0 {
		port = 8463
	}
	api := mcp.NewAPIBackend(port, st.Settings.APIToken)
	serveErr := nativehost.Serve(os.Stdin, os.Stdout, func(m nativehost.Message) nativehost.Reply {
		switch m.Type {
		case "ping":
			return nativehost.Reply{OK: true, Version: version.Version}
		case "grab":
			links, err := grabPage(m.Page)
			if err != nil {
				return nativehost.Reply{Message: err.Error()}
			}
			return nativehost.Reply{OK: true, Links: links}
		case "add", "":
			if strings.TrimSpace(m.URL) == "" {
				return nativehost.Reply{Message: "no url"}
			}
			u, err := url.Parse(m.URL)
			if err != nil {
				return nativehost.Reply{Message: "bad url"}
			}
			if !nativehost.Take(st.Rules, u, 0) {
				return nativehost.Reply{Message: "a capture rule left this link out"}
			}
			// A link the browser sends again is a new copy, as a paste is.
			args := map[string]any{"url": m.URL, "duplicate": true}
			if m.FileName != "" {
				args["name"] = m.FileName
			}
			if m.Referer != "" {
				args["referer"] = m.Referer
			}
			if cookie := nativehost.CookieHeader(m.Cookies, u.Hostname()); cookie != "" {
				args["cookies"] = cookie
			}
			if site, ok := nativehost.LoginFor(st.Sites, u.Hostname()); ok {
				if cookie := site.Cookies; cookie != "" && args["cookies"] == nil {
					args["cookies"] = cookie
				}
				if site.Referer != "" && args["referer"] == nil {
					args["referer"] = site.Referer
				}
			}
			text, err := api.Call(context.Background(), "download_add", args)
			if err != nil {
				return nativehost.Reply{Message: err.Error()}
			}
			return nativehost.Reply{OK: true, Message: text}
		}
		return nativehost.Reply{Message: "unknown message " + m.Type}
	})
	if serveErr != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", serveErr)
		return 1
	}
	return 0
}

// grabPage reads the links of a page.
func grabPage(page string) ([]string, error) {
	if !strings.HasPrefix(page, "http://") && !strings.HasPrefix(page, "https://") {
		return nil, fmt.Errorf("%q is not a page", page)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "downmygo/"+version.Version)
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	base, err := url.Parse(page)
	if err != nil {
		return nil, err
	}
	raw, err := grab.FromHTML(base, resp.Body)
	if err != nil {
		return nil, err
	}
	links := grab.Keep(raw, grab.Filter{})
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, l.URL)
	}
	return out, nil
}

// runGrab prints the links of a page.
func runGrab(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "downmygo: grab needs a page address")
		return 2
	}
	links, err := grabPage(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	for _, l := range links {
		fmt.Println(l)
	}
	return 0
}

// runImport adds every link of a file to the app.
func runImport(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "downmygo: import needs a file")
		return 2
	}
	urls, err := grab.FromFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	b, err := backend()
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	added := 0
	for _, u := range urls {
		if _, err := b.Call(context.Background(), "download_add", map[string]any{"url": u}); err != nil {
			fmt.Fprintf(os.Stderr, "downmygo: %s: %v\n", u, err)
			continue
		}
		added++
	}
	fmt.Printf("added %d of %d links\n", added, len(urls))
	if added == 0 && len(urls) > 0 {
		return 1
	}
	return 0
}

// runInstallHost writes the native messaging manifest for the browsers.
func runInstallHost(args []string) int {
	extID := "downmygo@logdns.github.io"
	if len(args) > 0 {
		extID = args[0]
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	manifest := nativehost.ManifestFor(exe, extID)
	body, err := jsonIndent(manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	for browser, dir := range nativehost.ManifestDirs(home) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "downmygo: %s: %v\n", browser, err)
			continue
		}
		name := "app.downmygo.host.json"
		if strings.Contains(extID, "@") && browser == "firefox" {
			name = "app.downmygo.host.json"
		}
		path := dir + string(os.PathSeparator) + name
		if err := os.WriteFile(path, body, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "downmygo: %s: %v\n", browser, err)
			continue
		}
		fmt.Printf("%s: %s\n", browser, path)
	}
	return 0
}

func jsonIndent(v any) ([]byte, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// runInfo prints the version, and the port the app listens on.
func runInfo() int {
	st, err := appState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	port := st.Settings.RemotePort
	if port == 0 {
		port = 8463
	}
	fmt.Printf("version %s\n", version.Version)
	fmt.Printf("api http://127.0.0.1:%s\n", strconv.Itoa(port))
	fmt.Printf("downloads %d\n", len(st.Downloads))
	fmt.Printf("feeds %d\n", len(st.Subscriptions))
	dir, err := store.DataDir()
	if err == nil {
		m := &tools.Manager{Dir: filepath.Join(dir, "tools")}
		for _, kind := range []tools.Kind{tools.YtDlp, tools.GalleryDL} {
			state := "not installed"
			if p, ok := m.Path(kind); ok {
				state = p
			}
			fmt.Printf("%s %s\n", kind, state)
		}
	}
	return 0
}
