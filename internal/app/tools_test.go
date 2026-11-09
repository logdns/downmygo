package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/downmygo/downmygo/internal/engine"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/store"
	"github.com/downmygo/downmygo/internal/tools"
)

// appWithStore makes an app with a real data folder, for the features
// that write files.
func appWithStore(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	st := &store.State{
		Version:    1,
		Settings:   model.Defaults(dir),
		Categories: model.DefaultCategories(dir),
		Queues:     model.DefaultQueues(),
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(st, s)
}

func TestVideoAndGallerySites(t *testing.T) {
	for _, host := range []string{"www.youtube.com", "youtu.be", "vimeo.com", "bilibili.com"} {
		if !videoSite(host) {
			t.Errorf("%s is not a video site", host)
		}
	}
	for _, host := range []string{"pixiv.net", "www.deviantart.com", "imgur.com"} {
		if !gallerySite(host) {
			t.Errorf("%s is not a gallery site", host)
		}
	}
	for _, host := range []string{"example.com", "notyoutube.com"} {
		if videoSite(host) || gallerySite(host) {
			t.Errorf("%s is a media site", host)
		}
	}
}

func TestAddLinkStartsTheTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	a := appWithStore(t)
	// A fake yt-dlp in the app's own tools folder, so nothing is
	// downloaded and the routing is what the test sees.
	dir := filepath.Join(a.dataDir(), "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "yt-dlp")
	marker := filepath.Join(dir, "done")
	out := filepath.Join(dir, "video.mp4")
	body := "#!/bin/sh\n" +
		"echo \"downmygo-progress 10 10 0 0 0\"\n" +
		"echo \"downmygo-file:" + out + "\"\n" +
		": > " + out + "\n" +
		": > " + marker + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	d := a.AddLink("https://www.youtube.com/watch?v=abc")
	if d == nil {
		t.Fatal("the link was not added")
	}
	if d.Kind != model.KindVideo {
		t.Fatalf("kind = %v", d.Kind)
	}
	// The tool runs off the main thread; wait for it to finish, so the
	// test's folder is quiet when it is removed.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the program did not run")
	}
	time.Sleep(200 * time.Millisecond)
}

func TestTorrentNames(t *testing.T) {
	cases := map[string]string{
		"magnet:?xt=urn:btih:abc&dn=Some+File.iso": "Some File.iso",
		"magnet:?xt=urn:btih:abc":                  "magnet",
		"https://x.example/a.torrent":              "a",
		"/tmp/My Torrent.torrent":                  "My Torrent",
	}
	for raw, want := range cases {
		if got := torrentName(raw); got != want {
			t.Errorf("torrentName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestToolManagerLivesInTheDataFolder(t *testing.T) {
	a := appWithStore(t)
	m := a.toolManager()
	if m.Dir != filepath.Join(a.dataDir(), "tools") {
		t.Fatalf("dir = %s", m.Dir)
	}
	if _, ok := m.Path(tools.YtDlp); ok {
		t.Fatal("a tool was found in an empty folder")
	}
}

func TestTorrentNamesAreCleaned(t *testing.T) {
	// A magnet's display name is a file name, and it must not be able to
	// name a path.
	for _, raw := range []string{
		"magnet:?xt=urn:btih:abc&dn=../../etc/passwd",
		"magnet:?xt=urn:btih:abc&dn=a/b",
	} {
		got := engine.SanitizeName(torrentName(raw))
		if strings.ContainsAny(got, `/\`) || got == ".." {
			t.Errorf("%q became %q", raw, got)
		}
	}
}
