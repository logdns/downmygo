package grab

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const page = `<html><body>
<a href="/files/a.zip">A</a>
<a href="https://cdn.example.com/v/b.mp4?x=1">B</a>
<a href="#top">top</a>
<a href="javascript:void(0)">no</a>
<img src="//img.example.com/c.png">
<a href="mailto:a@b.c">mail</a>
</body></html>`

func TestFromHTML(t *testing.T) {
	base, _ := url.Parse("https://example.com/dir/page.html")
	links, err := FromHTML(base, strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://example.com/files/a.zip",
		"https://cdn.example.com/v/b.mp4?x=1",
		"https://img.example.com/c.png",
	}
	if len(links) != len(want) {
		t.Fatalf("links = %v", links)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Fatalf("links = %v", links)
		}
	}
}

func TestKeepFilters(t *testing.T) {
	urls := []string{
		"https://example.com/a.zip",
		"https://example.com/b.mp4",
		"https://cdn.other.net/c.zip",
		"https://example.com/d.zip",
		"ftp://example.com/e.zip",
		"https://example.com/a.zip",
	}
	got := Keep(urls, Filter{Extensions: []string{"zip"}, Include: []string{"example.com"}, Exclude: []string{"other.net"}})
	if len(got) != 2 {
		t.Fatalf("kept %v", got)
	}
	if got[0].Name != "a.zip" || got[1].Name != "d.zip" {
		t.Fatalf("kept %v", got)
	}
	if got[0].Ext != "zip" {
		t.Fatalf("ext = %q", got[0].Ext)
	}
}

func TestFromTextAndFile(t *testing.T) {
	text := "grab https://a.example/x.zip and https://b.example/y.tar.gz, ok"
	got := FromText(text)
	if len(got) != 2 || got[0] != "https://a.example/x.zip" {
		t.Fatalf("got %v", got)
	}

	dir := t.TempDir()
	list := filepath.Join(dir, "list.txt")
	_ = os.WriteFile(list, []byte("https://a.example/1.zip\n# a comment\nhttps://a.example/2.zip\n"), 0o644)
	if urls, err := FromFile(list); err != nil || len(urls) != 2 {
		t.Fatalf("%v %v", urls, err)
	}

	js := filepath.Join(dir, "list.json")
	_ = os.WriteFile(js, []byte(`[{"url":"https://a.example/3.zip"},{"link":"https://a.example/4.zip"}]`), 0o644)
	if urls, err := FromFile(js); err != nil || len(urls) != 2 {
		t.Fatalf("%v %v", urls, err)
	}
}

func TestParseExtensions(t *testing.T) {
	got := ParseExtensions("zip, .mp4  mkv;rar")
	want := []string{"zip", "mp4", "mkv", "rar"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}

func TestNameOf(t *testing.T) {
	u, _ := url.Parse("https://example.com/a/b%20c.zip?x=1")
	if got := NameOf(u); got != "b c.zip" {
		t.Fatalf("name = %q", got)
	}
	u, _ = url.Parse("https://example.com/")
	if got := NameOf(u); got != "example.com" {
		t.Fatalf("name = %q", got)
	}
}
