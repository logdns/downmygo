package nativehost

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
)

func TestFraming(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Message{Type: "add", URL: "https://example.com/a.zip"}); err != nil {
		t.Fatal(err)
	}
	// Chrome's framing: 4 bytes of length, little endian.
	if n := len(buf.Bytes()); n < 4 {
		t.Fatalf("short frame: %d", n)
	}
	m, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "add" || m.URL != "https://example.com/a.zip" {
		t.Fatalf("m = %+v", m)
	}
}

func TestReadRefusesGarbage(t *testing.T) {
	body := []byte{0xff, 0xff, 0xff, 0x7f}
	if _, err := Read(bytes.NewReader(body)); err == nil {
		t.Fatal("a 2 GB message was accepted")
	}
	if _, err := Read(bytes.NewReader([]byte{1, 0, 0, 0, '{'})); err == nil {
		t.Fatal("broken JSON was accepted")
	}
}

func TestServe(t *testing.T) {
	var in, out bytes.Buffer
	_ = Write(&in, Message{Type: "ping"})
	_ = Write(&in, Message{Type: "add", URL: "https://x/y.zip"})
	err := Serve(&in, &out, func(m Message) Reply {
		if m.Type == "ping" {
			return Reply{OK: true, Version: "1.0.0"}
		}
		return Reply{OK: true, ID: "1", Name: "y.zip"}
	})
	if err != nil {
		t.Fatal(err)
	}
	var first Reply
	if err := ReadReply(&out, &first); err != nil {
		t.Fatal(err)
	}
	if first.Version != "1.0.0" {
		t.Fatalf("first = %+v", first)
	}
}

func TestCookieHeader(t *testing.T) {
	cookies := []Cookie{
		{Name: "sid", Value: "1", Domain: ".example.com"},
		{Name: "other", Value: "2", Domain: "elsewhere.com"},
		{Name: "host", Value: "3"},
	}
	got := CookieHeader(cookies, "cdn.example.com")
	if got != "sid=1; host=3" {
		t.Fatalf("header = %q", got)
	}
}

func TestRules(t *testing.T) {
	u, _ := url.Parse("https://cdn.example.com/files/movie.mkv")
	if !(Rule{Extensions: []string{"mkv"}}).Match(u, 0) {
		t.Fatal("extension did not match")
	}
	if (Rule{Extensions: []string{"zip"}}).Match(u, 0) {
		t.Fatal("wrong extension matched")
	}
	if !(Rule{Host: "example.com", MinSize: 1 << 20}).Match(u, 2<<20) {
		t.Fatal("host and size did not match")
	}
	if (Rule{Host: "example.com", MinSize: 1 << 20}).Match(u, 1<<10) {
		t.Fatal("a small file matched")
	}
	if (Rule{Exclude: "files"}).Match(u, 0) {
		t.Fatal("the exclusion did not apply")
	}
	if Take(nil, u, 0) != true {
		t.Fatal("no rules should take everything")
	}
	if Take([]Rule{{Extensions: []string{"zip"}}}, u, 0) {
		t.Fatal("a rule took the wrong link")
	}
}

func TestLoginFor(t *testing.T) {
	sites := []Site{
		{Host: "example.com", Cookies: "a=1"},
		{Host: "cdn.example.com", Cookies: "b=2"},
		{Host: "other.net", Cookies: "c=3"},
	}
	s, ok := LoginFor(sites, "cdn.example.com")
	if !ok || s.Cookies != "b=2" {
		t.Fatalf("site = %+v", s)
	}
	s, ok = LoginFor(sites, "www.example.com")
	if !ok || s.Cookies != "a=1" {
		t.Fatalf("site = %+v", s)
	}
	if _, ok := LoginFor(sites, "nope.test"); ok {
		t.Fatal("a login was found for another host")
	}
}

func TestManifest(t *testing.T) {
	m := ManifestFor("/apps/downmygo", "abcdefghijklmnop")
	if m.Name != "app.downmygo.host" || m.Type != "stdio" || m.Path != "/apps/downmygo" {
		t.Fatalf("m = %+v", m)
	}
	if len(m.AllowedOrigins) != 1 || !strings.HasPrefix(m.AllowedOrigins[0], "chrome-extension://") {
		t.Fatalf("origins = %v", m.AllowedOrigins)
	}
	f := ManifestFor("/apps/downmygo", "downmygo@example.com")
	if len(f.AllowedExts) != 1 || len(f.AllowedOrigins) != 0 {
		t.Fatalf("firefox manifest = %+v", f)
	}
	if len(ManifestDirs("/home/u")) == 0 {
		t.Fatal("no manifest folders")
	}
}
