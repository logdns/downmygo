// Package nativehost is the browser's side of downmygo: the native
// messaging protocol Chrome and Firefox speak, the rules that decide
// which links to take, and the logins a site needs.
package nativehost

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Message is what the extension sends and the app answers.
type Message struct {
	Type      string            `json:"type"` // "add", "ping", "grab"
	URL       string            `json:"url,omitempty"`
	Referer   string            `json:"referer,omitempty"`
	FileName  string            `json:"filename,omitempty"`
	UserAgent string            `json:"userAgent,omitempty"`
	Cookies   []Cookie          `json:"cookies,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	// Page is the page a "grab" asks about, for the links it holds.
	Page string `json:"page,omitempty"`
}

// Reply is what the app answers.
type Reply struct {
	OK      bool   `json:"ok"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
	Version string `json:"version,omitempty"`
	// Links are the links a "grab" found.
	Links []string `json:"links,omitempty"`
}

// Cookie is one of the browser's cookies.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	Expires  float64 `json:"expirationDate"`
}

// Read reads one message: a 32-bit length, in the machine's byte order as
// Chrome sends it, then that many bytes of JSON.
func Read(r io.Reader) (Message, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return Message{}, err
	}
	if size == 0 || size > 8<<20 {
		return Message{}, fmt.Errorf("a message of %d bytes is not one", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return Message{}, err
	}
	var m Message
	if err := json.Unmarshal(body, &m); err != nil {
		return Message{}, err
	}
	return m, nil
}

// ReadReply reads one reply in the same framing.
func ReadReply(r io.Reader, v any) error {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return err
	}
	if size == 0 || size > 8<<20 {
		return fmt.Errorf("a message of %d bytes is not one", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// Write writes one message in the same framing.
func Write(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(body))); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// Serve reads messages until the browser closes the pipe, answering each
// with handle.
func Serve(r io.Reader, w io.Writer, handle func(Message) Reply) error {
	br := bufio.NewReader(r)
	for {
		m, err := Read(br)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := Write(w, handle(m)); err != nil {
			return err
		}
	}
}

// CookieHeader says the cookies that belong to host, as a Cookie header.
func CookieHeader(cookies []Cookie, host string) string {
	host = strings.ToLower(host)
	var parts []string
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		if c.Domain != "" && !domainMatches(host, strings.TrimPrefix(strings.ToLower(c.Domain), ".")) {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func domainMatches(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// Rule is a capture rule: a link the browser offers is taken when it
// matches.
type Rule struct {
	// Host keeps links whose host holds this string; empty keeps every
	// host.
	Host string `json:"host,omitempty"`
	// Extensions keeps links whose name ends in one of these.
	Extensions []string `json:"extensions,omitempty"`
	// MinSize keeps links at least this big, when the size is known.
	MinSize int64 `json:"minSize,omitempty"`
	// Exclude drops links whose URL holds this string.
	Exclude string `json:"exclude,omitempty"`
}

// Match says whether a rule takes a link.
func (r Rule) Match(u *url.URL, size int64) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if r.Host != "" && !strings.Contains(strings.ToLower(u.Hostname()), strings.ToLower(r.Host)) {
		return false
	}
	if r.Exclude != "" && strings.Contains(strings.ToLower(u.String()), strings.ToLower(r.Exclude)) {
		return false
	}
	if r.MinSize > 0 && size > 0 && size < r.MinSize {
		return false
	}
	if len(r.Extensions) > 0 {
		name := strings.ToLower(u.Path)
		ok := false
		for _, e := range r.Extensions {
			e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
			if e != "" && strings.HasSuffix(name, "."+e) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// Take says whether any rule takes a link; no rules take everything.
func Take(rules []Rule, u *url.URL, size int64) bool {
	if len(rules) == 0 {
		return true
	}
	for _, r := range rules {
		if r.Match(u, size) {
			return true
		}
	}
	return false
}

// Site is a login: the cookies and headers a host needs.
type Site struct {
	Host      string            `json:"host"`
	Cookies   string            `json:"cookies,omitempty"`
	Referer   string            `json:"referer,omitempty"`
	UserAgent string            `json:"userAgent,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// LoginFor finds the site a host belongs to: the longest host that
// matches, so cdn.example.com takes example.com's login.
func LoginFor(sites []Site, host string) (Site, bool) {
	host = strings.ToLower(host)
	var best Site
	found := false
	for _, s := range sites {
		h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s.Host)), ".")
		if h == "" {
			continue
		}
		if domainMatches(host, h) && (!found || len(h) > len(best.Host)) {
			best = s
			found = true
		}
	}
	return best, found
}

// HostManifest is the file a browser reads to find the app: Chrome wants
// allowed_origins, Firefox allowed_extensions.
type HostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
	AllowedExts    []string `json:"allowed_extensions,omitempty"`
}

// ManifestFor makes the host manifest for a browser.
func ManifestFor(executable, extensionID string) HostManifest {
	m := HostManifest{
		Name:        "app.downmygo.host",
		Description: "downmygo",
		Path:        executable,
		Type:        "stdio",
	}
	if strings.Contains(extensionID, "@") {
		m.AllowedExts = []string{extensionID}
	} else {
		m.AllowedOrigins = []string{"chrome-extension://" + extensionID + "/"}
	}
	return m
}

// ManifestDirs are the folders each browser reads host manifests from.
func ManifestDirs(home string) map[string]string {
	switch runtime.GOOS {
	case "darwin":
		return map[string]string{
			"chrome":  filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts"),
			"edge":    filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "NativeMessagingHosts"),
			"brave":   filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
			"firefox": filepath.Join(home, "Library", "Application Support", "Mozilla", "NativeMessagingHosts"),
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		return map[string]string{
			"chrome":  filepath.Join(appData, "Google", "Chrome", "NativeMessagingHosts"),
			"edge":    filepath.Join(appData, "Microsoft", "Edge", "NativeMessagingHosts"),
			"brave":   filepath.Join(appData, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
			"firefox": filepath.Join(appData, "Mozilla", "NativeMessagingHosts"),
		}
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	return map[string]string{
		"chrome":  filepath.Join(cfg, "google-chrome", "NativeMessagingHosts"),
		"edge":    filepath.Join(cfg, "microsoft-edge", "NativeMessagingHosts"),
		"brave":   filepath.Join(cfg, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
		"firefox": filepath.Join(cfg, "mozilla", "native-messaging-hosts"),
	}
}
