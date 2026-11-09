package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const rssBody = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Example feed</title>
<item>
  <title>Episode 12</title>
  <link>https://example.com/ep12.mp3</link>
  <guid>ep-12</guid>
  <pubDate>Mon, 09 Nov 2026 10:00:00 +0800</pubDate>
  <enclosure url="https://cdn.example.com/ep12.mp3" length="2048" type="audio/mpeg"/>
</item>
<item>
  <title>Notes</title>
  <link>https://example.com/notes.html</link>
  <guid>notes</guid>
</item>
</channel></rss>`

const atomBody = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<title>Atom feed</title>
<entry>
  <title>A file</title>
  <id>urn:a</id>
  <link rel="alternate" href="https://example.com/a.zip"/>
  <link rel="enclosure" href="https://example.com/a.zip" length="99"/>
  <updated>2026-11-08T12:00:00Z</updated>
</entry>
</feed>`

func TestParseRSS(t *testing.T) {
	f, err := Parse(strings.NewReader(rssBody))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Example feed" || len(f.Items) != 2 {
		t.Fatalf("feed = %+v", f)
	}
	first := f.Items[0]
	if first.ID != "ep-12" || first.Enclosure != "https://cdn.example.com/ep12.mp3" || first.Size != 2048 {
		t.Fatalf("item = %+v", first)
	}
	if first.Published.IsZero() || first.Published.Year() != 2026 {
		t.Fatalf("date = %v", first.Published)
	}
}

func TestParseAtom(t *testing.T) {
	f, err := Parse(strings.NewReader(atomBody))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Atom feed" || len(f.Items) != 1 {
		t.Fatalf("feed = %+v", f)
	}
	it := f.Items[0]
	if it.ID != "urn:a" || it.Link != "https://example.com/a.zip" || it.Size != 99 {
		t.Fatalf("item = %+v", it)
	}
}

func TestParseRejectsOtherDocuments(t *testing.T) {
	for _, body := range []string{`<html><body>hi</body></html>`, `not xml`} {
		if _, err := Parse(strings.NewReader(body)); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestWant(t *testing.T) {
	cases := []struct {
		item Item
		exts []string
		want string
		ok   bool
	}{
		{Item{Enclosure: "https://x/a"}, nil, "https://x/a", true},
		{Item{Link: "https://x/a.zip"}, []string{"zip"}, "https://x/a.zip", true},
		{Item{Link: "https://x/a.zip?x=1"}, []string{".ZIP"}, "https://x/a.zip?x=1", true},
		{Item{Link: "https://x/a.mp4"}, []string{"zip"}, "", false},
		{Item{Link: "https://x/a.mp4"}, nil, "https://x/a.mp4", true},
		{Item{}, nil, "", false},
	}
	for _, c := range cases {
		got, ok := Want(c.item, c.exts)
		if got != c.want || ok != c.ok {
			t.Errorf("Want(%+v, %v) = %q, %v", c.item, c.exts, got, ok)
		}
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "downmygo/test" {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(rssBody))
	}))
	defer srv.Close()
	f, err := Fetch(context.Background(), srv.Client(), srv.URL, "downmygo/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 2 {
		t.Fatalf("items = %d", len(f.Items))
	}
	if !f.Items[0].Published.After(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("not sorted: %v", f.Items[0].Published)
	}
}
