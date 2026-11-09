// Package grab finds the links a page, a feed or a list holds, and keeps
// the ones a filter wants: the "grab the links on this page" feature and
// the importer share it.
package grab

import (
	"bufio"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// Filter says which links are worth adding.
type Filter struct {
	// Extensions keeps links whose file name ends in one of these, as
	// "zip" or ".mp4"; empty keeps every link.
	Extensions []string
	// Include keeps links whose host holds one of these strings; empty
	// keeps every host.
	Include []string
	// Exclude drops links whose host holds one of these strings.
	Exclude []string
	// Schemes are the schemes to keep; empty keeps http and https.
	Schemes []string
}

// Link is a link a filter kept.
type Link struct {
	URL  string
	Name string
	Ext  string
}

// FromHTML reads the links of an HTML document: href and src attributes,
// resolved against base.
func FromHTML(base *url.URL, r io.Reader) ([]string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "href", "src", "data-src", "data-href", "content":
					if u := resolve(base, a.Val); u != "" {
						out = append(out, u)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out, nil
}

// FromText finds the http and https links in a text, one per line or
// loose in a sentence.
func FromText(s string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '"', '\'', '<', '>', '(', ')', '[', ']', ',', ';':
			return true
		}
		return false
	}) {
		field = strings.TrimRight(field, ".,:!?")
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			out = append(out, field)
		}
	}
	return out
}

// FromFile reads a list of links: a plain list, or JSON of an array of
// strings or of objects with a url field.
func FromFile(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.HasSuffix(strings.ToLower(name), ".json") {
		return fromJSON(f)
	}
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		out = append(out, FromText(sc.Text())...)
	}
	return out, sc.Err()
}

func fromJSON(r io.Reader) ([]string, error) {
	body, err := io.ReadAll(io.LimitReader(r, 32<<20))
	if err != nil {
		return nil, err
	}
	var list []string
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}
	var objects []struct {
		URL  string `json:"url"`
		Link string `json:"link"`
	}
	if err := json.Unmarshal(body, &objects); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(objects))
	for _, o := range objects {
		if o.URL != "" {
			out = append(out, o.URL)
		} else if o.Link != "" {
			out = append(out, o.Link)
		}
	}
	return out, nil
}

// Keep applies f to urls, dropping duplicates and the ones a filter does
// not want, sorted by name.
func Keep(urls []string, f Filter) []Link {
	seen := map[string]bool{}
	out := make([]Link, 0, len(urls))
	for _, raw := range urls {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" {
			continue
		}
		scheme := strings.ToLower(u.Scheme)
		if len(f.Schemes) > 0 {
			ok := false
			for _, s := range f.Schemes {
				if strings.EqualFold(s, scheme) {
					ok = true
				}
			}
			if !ok {
				continue
			}
		} else if scheme != "http" && scheme != "https" {
			continue
		}
		if !hostAllowed(u.Hostname(), f) {
			continue
		}
		name := NameOf(u)
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
		if len(f.Extensions) > 0 && !hasExt(ext, f.Extensions) {
			continue
		}
		key := u.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Link{URL: key, Name: name, Ext: ext})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// NameOf is the file name a link would save as.
func NameOf(u *url.URL) string {
	name := path.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		name = u.Hostname()
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	return name
}

func hasExt(ext string, list []string) bool {
	if ext == "" {
		return false
	}
	for _, e := range list {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(e), "."), ext) {
			return true
		}
	}
	return false
}

func hostAllowed(host string, f Filter) bool {
	host = strings.ToLower(host)
	for _, e := range f.Exclude {
		if e != "" && strings.Contains(host, strings.ToLower(e)) {
			return false
		}
	}
	if len(f.Include) == 0 {
		return true
	}
	for _, e := range f.Include {
		if e != "" && strings.Contains(host, strings.ToLower(e)) {
			return true
		}
	}
	return false
}

// resolve turns a link in a page into an absolute one, or "" when it is
// not a link a downloader wants (a fragment, javascript:, mailto:).
func resolve(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "mailto", "tel", "data", "about", "blob":
		return ""
	case "":
		if base == nil {
			return ""
		}
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

// ParseExtensions reads "zip, mp4 .mkv" into a list.
func ParseExtensions(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	}) {
		f = strings.TrimPrefix(strings.TrimSpace(f), ".")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
