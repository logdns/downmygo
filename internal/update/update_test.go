package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0", "1.0.0", 0},
		{"1.1", "1.0.9", 1},
		{"v2.0.0", "1.9.9", 1},
		{"1.0.0", "1.0.0-beta", 1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"0.9", "1.0", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckAndAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/logdns/downmygo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Release{
			Tag: "v1.2.0", URL: "https://example.com/rel", Notes: "notes",
			Assets: []Asset{
				{Name: "downmygo-1.2.0.dmg", URL: "https://example.com/a.dmg"},
				{Name: "downmygo-setup-1.2.0-amd64.exe", URL: "https://example.com/a.exe"},
				{Name: "downmygo-1.2.0-linux-amd64.tar.gz", URL: "https://example.com/a.tgz"},
			},
		})
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()

	rel, newer, err := Check(context.Background(), srv.Client(), "logdns/downmygo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !newer || rel.Version != "1.2.0" || rel.Tag != "v1.2.0" {
		t.Fatalf("rel = %+v, newer = %v", rel, newer)
	}
	if a := AssetFor(rel, "darwin", "arm64"); a.Name != "downmygo-1.2.0.dmg" {
		t.Errorf("macOS asset = %q", a.Name)
	}
	if a := AssetFor(rel, "windows", "amd64"); a.Name != "downmygo-setup-1.2.0-amd64.exe" {
		t.Errorf("Windows asset = %q", a.Name)
	}
	if a := AssetFor(rel, "linux", "amd64"); a.Name != "downmygo-1.2.0-linux-amd64.tar.gz" {
		t.Errorf("Linux asset = %q", a.Name)
	}
	if a := AssetFor(rel, "plan9", "mips"); a.Name != "" {
		t.Errorf("an unknown platform got %q", a.Name)
	}

	_, newer, err = Check(context.Background(), srv.Client(), "logdns/downmygo", "1.2.0")
	if err != nil || newer {
		t.Fatalf("the same version is newer: %v %v", newer, err)
	}
}

func TestCheckSaysWhenTheAnswerIsNotARelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()
	if _, _, err := Check(context.Background(), srv.Client(), "o/r", "1.0.0"); err == nil {
		t.Fatal("a 403 was accepted")
	}
}
